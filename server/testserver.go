package server

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"sync"

	"github.com/liustack/tifa/internal/config"
	"github.com/liustack/tifa/internal/content"
	"github.com/liustack/tifa/internal/core"
	"github.com/liustack/tifa/internal/ssr"
)

// testVMs is the most VMs the pages of a test server render with. A test sends a request or a few
// at a time, and go test ./... runs the packages of an app at once, each with VMs of its own.
const testVMs = 2

// tests is the state StartTest keeps for the whole test binary.
var tests struct {
	mu      sync.Mutex
	running bool                   // a server of StartTest has not stopped
	pools   map[[32]byte]*ssr.Pool // by the SSR bundle and its settings, kept until the process ends
}

// StartTest runs app in the process of a test, for tifatest.Start and tifatest.StartPages. Apart from
// it, only generated code uses this package. It reads the settings of tifa as the program does from
// the defaults and app.Settings alone, without flags, environment or files, and without HTTPS,
// since the test server has its own address and certificate. It builds the server, runs OnStart
// with ctx and sets the handler, the timeouts and the base context of srv, which the caller then
// serves.
//
// With app.Dist set, its content becomes the content of the collections, and pages render in VMs
// that every StartTest of the process with the same bundle shares, at most testVMs of them. Without
// it, pages answer the results of their Loads as JSON.
//
// stop ends what StartTest began: it closes the channel of tifa.ShuttingDown, calls closeServer,
// which stops srv and waits for the requests in flight, runs OnShutdown and puts back what the
// server changed in the process. One server runs at a time: StartTest fails while another has
// not stopped, as the session store, SetHTTPHandler and the content are those of the process.
func StartTest(ctx context.Context, app App, srv *http.Server) (stop func(closeServer func()) error, err error) {
	tests.mu.Lock()
	if tests.running {
		tests.mu.Unlock()
		return nil, errors.New("another test server of this process is still running: tests that start the app cannot run in parallel")
	}
	tests.running = true
	tests.mu.Unlock()
	// undo runs, last first, what puts the process back.
	var undo []func()
	unwind := func() {
		for i := len(undo) - 1; i >= 0; i-- {
			undo[i]()
		}
		tests.mu.Lock()
		tests.running = false
		tests.mu.Unlock()
	}
	defer func() {
		if err != nil {
			unwind()
		}
	}()
	if err := checkServeMux(); err != nil {
		return nil, err
	}
	cfg, _, err := loadConfig(app, config.Read(nil, nil, ""))
	if err != nil {
		return nil, err
	}
	cfg.Server = withoutHTTPS(cfg.Server)
	cfg.SSR.VMs = min(cfg.SSR.VMs, testVMs)
	s := newServer(cfg, app.Hooks)
	s.register(app.Layouts, app.Routes)
	if app.Session != nil {
		s.enableSessions(app.Session)
	}
	undo = append(undo, s.close)
	var dist fs.FS
	if app.Dist != nil {
		if dist, err = fs.Sub(app.Dist, "dist"); err != nil {
			return nil, err
		}
		for _, c := range content.Collections() {
			undo = append(undo, c.Save())
		}
		if err := content.Load(dist); err != nil {
			return nil, err
		}
	}
	undo = append(undo, content.Watch(s.contentChanged))
	undo = append(undo, core.Running(s))
	if dist != nil {
		if err := s.openSharedFront(dist, cfg.SSR); err != nil {
			return nil, err
		}
		undo = append(undo, func() { _ = s.closeFront() })
	}
	if s.hooks.OnStart != nil {
		if err := s.hooks.OnStart(ctx); err != nil {
			return nil, fmt.Errorf("OnStart: %w", err)
		}
	}
	root, release := core.SealHTTPHandler(s)
	undo = append(undo, release)
	s.root = root
	undo = append(undo, s.startSessions())

	stopping := make(chan struct{})
	base, cancelRequests := context.WithCancel(core.WithShutdown(context.Background(), stopping))
	srv.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.setHSTS(w)
		root.ServeHTTP(w, r)
	})
	srv.BaseContext = func(net.Listener) context.Context { return base }
	srv.ReadHeaderTimeout, srv.ReadTimeout, srv.WriteTimeout, srv.IdleTimeout = readHeaderTimeout, s.cfg.ReadTimeout, s.cfg.WriteTimeout, idleTimeout
	var once sync.Once
	return func(closeServer func()) (err error) {
		once.Do(func() {
			close(stopping)
			closeServer()
			cancelRequests()
			if s.hooks.OnShutdown != nil {
				hookCtx, cancel := context.WithTimeout(context.Background(), s.cfg.ShutdownTimeout)
				defer cancel()
				if hookErr := s.hooks.OnShutdown(hookCtx); hookErr != nil {
					err = fmt.Errorf("OnShutdown: %w", hookErr)
				}
			}
			unwind()
		})
		return err
	}, nil
}

// openSharedFront is openFront with the VM pool of the process for the bundle in dist and cfg,
// made on first use. Closing the front end leaves the pool alone.
func (s *server) openSharedFront(dist fs.FS, cfg config.SSR) error {
	source, err := fs.ReadFile(dist, "ssr.js")
	if errors.Is(err, fs.ErrNotExist) {
		return s.setFront(dist, nil)
	}
	if err != nil {
		return err
	}
	key := sha256.Sum256(fmt.Appendf([]byte(source), "\x00%+v", cfg))
	tests.mu.Lock()
	defer tests.mu.Unlock()
	pool := tests.pools[key]
	if pool == nil {
		sourceMap, err := fs.ReadFile(dist, "ssr.js.map")
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		if pool, err = ssr.NewPool(string(source), sourceMap, cfg, false); err != nil {
			return err
		}
		if tests.pools == nil {
			tests.pools = map[[32]byte]*ssr.Pool{}
		}
		tests.pools[key] = pool
	}
	f, err := s.readFront(dist, pool.Render)
	if err != nil {
		return err
	}
	f.pool, f.shared, f.timeout = pool, true, cfg.Timeout
	return s.swapFront(f)
}
