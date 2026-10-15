// Package devserver is the core of tifa dev. It watches the application directory, rebuilds
// what a change needs through a Pipeline, restarts the application or has it load a rebuilt SSR
// bundle, and proxies the browser to it. Browsers learn about rebuilds and build errors over SSE from the paths under /__tifa/dev/.
package devserver

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/liustack/tifa/js"
)

// Defaults for the Options durations left at zero.
const (
	DefaultDebounce    = 100 * time.Millisecond
	DefaultStopTimeout = 10 * time.Second
	DefaultProxyWait   = 30 * time.Second
)

// Options configures Run.
type Options struct {
	// Dir is the application directory. Every directory under it is watched except .git, .tifa,
	// node_modules and the ones in Ignore, given relative to Dir, such as the build output.
	Dir    string
	Ignore []string
	// App runs the application that Pipeline.GoBuild builds: the executable, then its arguments.
	// The application gets the address to listen on in TIFA_SERVER_ADDR. After a rebuild of the
	// SSR bundle alone it gets POST /__tifa/dev/reload instead of a restart, which server.DevMain
	// answers with 204 once it loaded the new bundle.
	App      []string
	Pipeline Pipeline
	// Vite is the address of Vite's dev server, which Pipeline.Bundle starts. Requests under
	// VitePath, its module and HMR websocket requests, go there. Empty sends them to the app too.
	Vite string
	// Outside, when not nil, brings Run the files outside Dir that changed, such as a package of
	// the workspace that Vite's dev server watches. Each counts as a modified front end file.
	Outside *Outside

	// Debounce is how long the files must stay unchanged before a rebuild starts.
	Debounce time.Duration
	// StopTimeout is how long the application gets after an interrupt to run OnShutdown and exit
	// before it is killed.
	StopTimeout time.Duration
	// ProxyWait is how long a browser request waits for a restarting application.
	ProxyWait time.Duration
	// Stdout and Stderr receive the application's output. Stderr also gets the dev server's log.
	// Nil means os.Stdout and os.Stderr.
	Stdout, Stderr io.Writer
}

var errOptions = errors.New("tifa dev: incomplete options")

// Run builds and starts the application, then serves browsers on ln until ctx is done: requests
// go to the application, a file change rebuilds it and restarts it or reloads its SSR bundle. A failed build leaves the
// running application alone and shows the error in the browser. When ctx is done Run stops the
// application and returns. Errors from builds are reported to the browser, Run only returns
// errors that stop tifa dev itself, such as the file watcher failing.
func Run(ctx context.Context, ln net.Listener, o Options) error {
	defer ln.Close()
	if runtime.GOOS == "windows" {
		// Restarting relies on an interrupt so the application runs OnShutdown, and Windows
		// cannot send one to another process.
		return errors.New("tifa dev: Windows is not supported yet, use WSL")
	}
	switch {
	case o.Dir == "":
		return fmt.Errorf("%w: Dir is empty", errOptions)
	case len(o.App) == 0:
		return fmt.Errorf("%w: App is empty", errOptions)
	case o.Pipeline == nil:
		return fmt.Errorf("%w: Pipeline is nil", errOptions)
	}
	if o.Debounce == 0 {
		o.Debounce = DefaultDebounce
	}
	if o.StopTimeout == 0 {
		o.StopTimeout = DefaultStopTimeout
	}
	if o.ProxyWait == 0 {
		o.ProxyWait = DefaultProxyWait
	}
	if o.Stdout == nil {
		o.Stdout = os.Stdout
	}
	if o.Stderr == nil {
		o.Stderr = os.Stderr
	}
	appAddr, err := freeAddr()
	if err != nil {
		return err
	}
	logger := log.New(o.Stderr, "tifa dev: ", log.Ltime)
	w, err := newWatcher(o.Dir, o.Ignore, func(msg string) { logger.Print(msg) })
	if err != nil {
		return err
	}
	d := &dev{
		o:       o,
		appAddr: appAddr,
		b:       builder{p: o.Pipeline, log: logger.Printf},
		hub:     newHub(),
		log:     logger,
		nonce:   strconv.FormatInt(time.Now().UnixNano(), 36),
	}

	mux := http.NewServeMux()
	mux.Handle("GET "+eventsPath, d.hub)
	mux.Handle("GET "+clientPath, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Tells the page its version where it cannot read Server-Timing.
		served := "window.__tifaDevServed = " + strconv.Quote(d.version()) + ";\n"
		script(served+js.DevClient).ServeHTTP(w, r)
	}))
	mux.Handle("GET "+workerPath, script(js.DevWorker))
	mux.Handle(devPrefix, http.NotFoundHandler())
	if o.Vite != "" {
		mux.Handle(VitePath, viteProxy(o.Vite))
	}
	app := proxy(appAddr, o.ProxyWait, d.down, d.version)
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A page loaded while the SSR bundle is rebuilt would show the old server render. Wait
		// until the app with the new one runs.
		if busy := d.busy(); busy != nil {
			select {
			case <-busy:
			case <-r.Context().Done():
				return
			case <-time.After(o.ProxyWait):
			}
		}
		app.ServeHTTP(w, r)
	}))
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	served := make(chan error, 1)
	go func() {
		err := srv.Serve(ln)
		if !errors.Is(err, http.ErrServerClosed) {
			err = fmt.Errorf("tifa dev: serving: %w", err)
			cancel() // without the proxy there is nothing left to do
		}
		served <- err
	}()
	watched := make(chan error, 1)
	go func() {
		err := w.run(ctx)
		cancel()
		watched <- err
	}()
	if o.Outside != nil {
		o.Outside.attach(w.rootReal, w.changes)
		defer o.Outside.detach()
	}
	go func() {
		// The system is short of watches: Vite's watcher would run out as well.
		select {
		case <-ctx.Done():
		case <-w.short:
			polling, err := o.Pipeline.PollFiles(ctx)
			switch {
			case err != nil && ctx.Err() == nil:
				logger.Printf("telling Vite's dev server to poll: %v", err)
			case polling:
				logger.Print("Vite's dev server polls the files too")
			}
		}
	}()

	w.changes.setBuilding(true)
	d.rebuild(ctx, everything)
	w.changes.setBuilding(false)
	loop(ctx, w.changes, o.Debounce, func(batch []change) {
		for _, c := range batch {
			d.log.Printf("%s %s", c.path, c.op)
		}
		d.rebuild(ctx, plan(batch))
	})

	d.mu.Lock()
	a := d.app
	d.app, d.live = nil, nil
	d.mu.Unlock()
	if a != nil {
		if err := a.stop(o.StopTimeout); err != nil {
			d.log.Print(err)
		}
	}
	d.hub.close()
	shutdown, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelShutdown()
	err = srv.Shutdown(shutdown)
	if serveErr := <-served; !errors.Is(serveErr, http.ErrServerClosed) {
		err = errors.Join(err, serveErr)
	}
	return errors.Join(<-watched, err)
}

// Outside brings Run the files outside its Dir that changed. Vite's dev server watches them, as
// it loaded them, and the driver reports their changes from the goroutine that reads its answers.
// Changed never blocks, so it never holds up that goroutine: it records the file for the next
// batch, or drops it while no Run runs, before Run starts and once it returned.
type Outside struct {
	mu   sync.Mutex
	root string   // real path of Dir while Run runs
	c    *changes // where Run collects changes, nil while no Run runs
}

// Changed records that the file at the absolute path file changed.
func (o *Outside) Changed(file string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.c == nil {
		return
	}
	if rel, err := filepath.Rel(o.root, file); err == nil {
		o.c.touch(filepath.ToSlash(rel))
	}
}

func (o *Outside) attach(root string, c *changes) {
	o.mu.Lock()
	o.root, o.c = root, c
	o.mu.Unlock()
}

func (o *Outside) detach() { o.attach("", nil) }

func script(source string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = io.WriteString(w, source)
	})
}

// dev is the state of one Run. Rebuilds and restarts happen on the loop goroutine, mu guards
// what the proxy and the exit watchers read.
type dev struct {
	o       Options
	appAddr string
	b       builder
	hub     *hub
	log     *log.Logger
	nonce   string // makes versions unique across runs of tifa dev

	mu      sync.Mutex
	gen     int    // generation of the newest app started
	serving string // version of the newest app started, the one that answers requests
	app     *app   // the application process, nil when none runs
	live    *app   // app once it listens, its exit from then on is a crash
	downErr error  // why no application runs and none will until the next build
	// bundling is closed once the rebuild of the SSR bundle that runs now has an app serving it,
	// nil when none runs.
	bundling chan struct{}
}

// busy returns a channel that is closed once the SSR bundle being rebuilt is served, nil when
// no rebuild of it runs.
func (d *dev) busy() chan struct{} {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.bundling
}

// version returns the version of the app that answers requests now, empty before the first one.
func (d *dev) version() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.serving
}

// down tells the proxy not to wait for an application, and why.
func (d *dev) down() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.downErr
}

// fail shows err in the browser under title. The version stays, so pages do not reload.
// Without a running application, the proxy answers with err instead of waiting.
func (d *dev) fail(title string, err error) {
	d.mu.Lock()
	if d.app == nil {
		d.downErr = err
	}
	d.hub.publishError(title, err.Error())
	d.mu.Unlock()
	d.log.Printf("%s: %v", strings.ToLower(title), err)
}

// rebuild runs the steps, restarts the application and tells the browsers to reload. When only
// the bundle was rebuilt, the running application loads it instead of restarting, so its state
// stays. Vite has updated the pages already and they stay too: the version stays the same, and
// only the server render changes.
//
// Page requests wait for the end of a rebuild that makes a new SSR bundle, from the moment the
// bundle is known to be needed: at the start, also for a bundle a failed rebuild left undone, or
// once the analysis finds a new route manifest, before code generation.
func (d *dev) rebuild(ctx context.Context, s steps) {
	if s == (steps{}) {
		return
	}
	start := time.Now()
	var done chan struct{}
	holdPages := func() {
		if done == nil {
			done = make(chan struct{})
			d.mu.Lock()
			d.bundling = done
			d.mu.Unlock()
		}
	}
	defer func() {
		if done != nil {
			d.mu.Lock()
			d.bundling = nil
			d.mu.Unlock()
			close(done)
		}
	}()
	ran, err := d.b.build(ctx, s, holdPages)
	if err != nil {
		if ctx.Err() == nil {
			d.fail("Build failed", err)
		}
		return
	}
	if ran == (steps{bundle: true}) && d.running() {
		if err := d.reloadBundle(ctx); err != nil {
			if ctx.Err() == nil {
				d.fail("The app did not load the SSR bundle", err)
			}
			return
		}
	} else if err := d.restart(ctx, d.version() == "" || ran != steps{bundle: true}); err != nil {
		if ctx.Err() == nil {
			d.fail("The app did not start", err)
		}
		return
	}
	d.mu.Lock()
	d.hub.publish(status{Version: d.serving})
	d.mu.Unlock()
	d.log.Printf("ready in %s", time.Since(start).Round(time.Millisecond))
}

// running reports whether an application listens.
func (d *dev) running() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.live != nil
}

// reloadBundle asks the running application to load the rebuilt SSR bundle, see
// server.DevMain. It keeps running with its state, and its version stays.
func (d *dev) reloadBundle(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, d.o.ProxyWait)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "POST", "http://"+d.appAddr+reloadPath, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		return fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

// restart stops the running application and starts the new build, waiting until it listens.
// With reload the new app gets a new version, which reloads open pages.
func (d *dev) restart(ctx context.Context, reload bool) error {
	d.mu.Lock()
	old := d.app
	d.app, d.live, d.downErr = nil, nil, nil
	d.mu.Unlock()
	if old != nil {
		if err := old.stop(d.o.StopTimeout); err != nil {
			d.log.Print(err)
		}
	}
	a, err := startApp(d.o.App, d.appAddr, d.o.Stdout, d.o.Stderr)
	if err != nil {
		return err
	}
	d.mu.Lock()
	d.gen++
	a.gen = d.gen
	d.app = a
	if reload {
		d.serving = d.nonce + "-" + strconv.Itoa(d.gen)
	}
	d.mu.Unlock()
	go d.watchExit(a)
	if err := a.waitReady(ctx, d.appAddr, d.o.ProxyWait); err != nil {
		d.mu.Lock()
		d.app = nil
		d.mu.Unlock()
		if stopErr := a.stop(d.o.StopTimeout); stopErr != nil {
			d.log.Print(stopErr)
		}
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	select {
	case <-a.done:
		d.app = nil
		return a.exitError()
	default:
		d.live = a
		return nil
	}
}

// watchExit reports an application that exits while it serves. Exits before it listens are
// reported by restart, exits after a stop are expected. The check and the report happen under
// d.mu, so the exit of an app that a newer generation replaced is dropped.
func (d *dev) watchExit(a *app) {
	<-a.done
	err := a.exitError()
	d.mu.Lock()
	crashed := a.gen == d.gen && d.live == a
	if crashed {
		d.app, d.live, d.downErr = nil, nil, err
		d.hub.publishError("The app stopped", err.Error())
	}
	d.mu.Unlock()
	if crashed {
		d.log.Printf("the app stopped: %v", err)
	}
}
