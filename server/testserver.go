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
	running bool      // a server of StartTest has not stopped
	held    bool      // running, by a stopped server whose handlers have not returned
	pool    *ssr.Pool // the VMs of the last bundle, kept for the next server until another bundle comes
	poolKey [32]byte  // the SSR bundle and the settings of pool
}

// StartTest runs app in the process of a test, for tifatest.Start and tifatest.StartPages. Apart from
// it, only generated code uses this package. It reads the settings of tifa as the program does from
// the defaults and app.Settings alone, without flags, environment or files, and without HTTPS,
// since the test server has its own address and certificate. It builds the server, runs OnStart
// with ctx and sets the handler, the timeouts and the base context of srv, which the caller then
// serves.
//
// With app.Dist set, its content becomes the content of the collections, and pages render on at
// most testVMs VMs, which the process keeps for the next StartTest with the same bundle and
// replaces for another. Without it, pages answer the results of their Loads as JSON.
//
// stop ends what StartTest began: it closes the channel of tifa.ShuttingDown, cancels the
// contexts of the requests in flight, stops srv and waits up to server.shutdown_timeout for the
// handlers to return. Then it runs OnShutdown, calls closeServer and puts back what the server
// changed in the process. A handler still running then skips OnShutdown, and stop reports it: the
// server keeps what it holds in the process until the handler returns, and only then calls
// closeServer, which waits for it, and puts the process back. One server runs at a time:
// StartTest fails while another has not stopped, or a handler of one still runs, as the session
// store, SetHTTPHandler and the content are those of the process.
func StartTest(ctx context.Context, app App, srv *http.Server) (stop func(closeServer func()) error, err error) {
	tests.mu.Lock()
	if tests.held {
		tests.mu.Unlock()
		return nil, errors.New("a handler of an earlier test is still running: it went on after its request ended and server.shutdown_timeout passed, and the app stays with it until it returns. Make the handler return when c.Context() is done")
	}
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
		tests.running, tests.held = false, false
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
	// What OnStart sets for the process, such as SetHTTPHandler, goes back when the server stops,
	// or when it does not start.
	undo = append(undo, core.SaveWiring())
	if s.hooks.OnStart != nil {
		if err := s.hooks.OnStart(ctx); err != nil {
			return nil, fmt.Errorf("OnStart: %w", err)
		}
	}
	root, release := core.SealHTTPHandler(s)
	undo = append(undo, release)
	s.root = root
	undo = append(undo, s.startSessions())

	g := new(gate)
	stopping := make(chan struct{})
	base, cancelRequests := context.WithCancel(core.WithShutdown(context.Background(), stopping))
	srv.Handler = s.gated(g, root)
	srv.BaseContext = func(net.Listener) context.Context { return base }
	limits := s.httpServer(nil)
	srv.ReadHeaderTimeout, srv.ReadTimeout, srv.WriteTimeout, srv.IdleTimeout = limits.ReadHeaderTimeout, limits.ReadTimeout, limits.WriteTimeout, limits.IdleTimeout
	var once sync.Once
	return func(closeServer func()) (err error) {
		once.Do(func() {
			// The stop of the program, on srv, but the test that sent the requests is over, so their
			// contexts are cancelled at once and not after server.shutdown_timeout. OnShutdown runs
			// once the handlers have returned.
			drain := func(ctx context.Context) error {
				cancelRequests()
				return srv.Shutdown(ctx)
			}
			err = s.shutdown(stopping, g, cancelRequests, drain, func() { _ = srv.Close() })
			if errors.Is(err, errHandlersRunning) {
				// The handlers still use the session store, the content and the rest the process
				// holds for this server, so it keeps them, and StartTest fails, until they return.
				tests.mu.Lock()
				tests.held = true
				tests.mu.Unlock()
				go func() {
					g.running.Wait()
					closeServer()
					unwind()
				}()
				return
			}
			closeServer()
			unwind()
		})
		return err
	}, nil
}

// openSharedFront is openFront with the VM pool of the process, so the next server with the same
// bundle and cfg renders on VMs that are loaded already. The process keeps one pool: another
// bundle or other settings close it and make a new one. No server uses the old one then, as one
// runs at a time. Closing the front end leaves the pool alone.
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
	if tests.pool != nil && tests.poolKey != key {
		err := tests.pool.Close()
		tests.pool = nil
		if err != nil {
			return fmt.Errorf("closing the VMs of the bundle of an earlier test: %w", err)
		}
	}
	pool := tests.pool
	if pool == nil {
		sourceMap, err := fs.ReadFile(dist, "ssr.js.map")
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		if pool, err = ssr.NewPool(string(source), sourceMap, cfg, false); err != nil {
			return err
		}
		tests.pool, tests.poolKey = pool, key
	}
	f, err := s.readFront(dist, pool.RenderPage)
	if err != nil {
		return err
	}
	f.pool, f.shared, f.timeout = pool, true, cfg.Timeout
	return s.swapFront(f)
}
