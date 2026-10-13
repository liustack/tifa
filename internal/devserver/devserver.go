// Package devserver is the core of tifa dev. It watches the application directory, rebuilds
// what a change needs through a Pipeline, restarts the application, and proxies the browser to
// it. Browsers learn about rebuilds and build errors over SSE from the paths under /__tifa/dev/.
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
	// The application gets the address to listen on in TIFA_SERVER_ADDR.
	App      []string
	Pipeline Pipeline
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
// go to the application, a file change rebuilds and restarts it. A failed build leaves the
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
	w, err := newWatcher(o.Dir, o.Ignore)
	if err != nil {
		return err
	}
	d := &dev{
		o:       o,
		appAddr: appAddr,
		b:       builder{p: o.Pipeline},
		hub:     newHub(),
		log:     log.New(o.Stderr, "tifa dev: ", log.Ltime),
		nonce:   strconv.FormatInt(time.Now().UnixNano(), 36),
	}

	mux := http.NewServeMux()
	mux.Handle(eventsPath, d.hub)
	mux.Handle("GET "+clientPath, script(js.DevClient))
	mux.Handle("GET "+workerPath, script(js.DevWorker))
	mux.Handle(devPrefix, http.NotFoundHandler())
	mux.Handle("/", proxy(appAddr, o.ProxyWait, d.down))
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	watched := make(chan error, 1)
	go func() {
		err := w.run(ctx)
		cancel()
		watched <- err
	}()

	d.rebuild(ctx, everything)
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
	builds  int

	mu      sync.Mutex
	app     *app  // the application process, nil when none runs
	live    *app  // app once it listens, its exit from then on is a crash
	downErr error // why no application runs and none will until the next build
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
	d.log.Printf("%s: %v", strings.ToLower(title), err)
	d.mu.Lock()
	if d.app == nil {
		d.downErr = err
	}
	d.mu.Unlock()
	d.hub.publishError(title, err.Error())
}

// rebuild runs the steps, restarts the application and tells the browsers to reload.
func (d *dev) rebuild(ctx context.Context, s steps) {
	if s == (steps{}) {
		return
	}
	start := time.Now()
	if err := d.b.build(ctx, s); err != nil {
		if ctx.Err() == nil {
			d.fail("Build failed", err)
		}
		return
	}
	if err := d.restart(ctx); err != nil {
		if ctx.Err() == nil {
			d.fail("The app did not start", err)
		}
		return
	}
	d.builds++
	d.hub.publish(status{Version: d.nonce + "-" + strconv.Itoa(d.builds)})
	d.log.Printf("ready in %s", time.Since(start).Round(time.Millisecond))
}

// restart stops the running application and starts the new build, waiting until it listens.
func (d *dev) restart(ctx context.Context) error {
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
	d.app = a
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
// reported by restart, exits after a stop are expected.
func (d *dev) watchExit(a *app) {
	<-a.done
	d.mu.Lock()
	crashed := d.live == a
	if crashed {
		d.app, d.live = nil, nil
	}
	d.mu.Unlock()
	if crashed {
		d.fail("The app stopped", a.exitError())
	}
}
