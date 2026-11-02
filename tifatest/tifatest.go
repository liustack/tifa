// Package tifatest builds the tifa.Ctx of a test request, so the Load, Layout, method handler and
// OnRequest functions of an app can be called in a Go test like any other function. It tests the
// function, not the HTTP answer: input decoding, Validate, turning a result or an error into a
// response, OnError and the navigation data protocol run only in the server.
package tifatest

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/internal/config"
	"github.com/liustack/tifa/internal/core"
)

// NewCtx returns the Ctx of r and the recorder that holds what is written through it. A returned
// result or error is not written, so the recorder stays an empty 200 unless the function writes. Path
// parameters are the path values of r, set with r.SetPathValue, and Ctx.Pattern is r.Pattern as it is when NewCtx is called. The
// body is limited to the default server.body_limit, 1 MiB, unless Ctx.SetBodyLimit changes it.
//
// The Ctx has a session store that belongs to the test, in memory, so tifa.SetSession and
// tifa.ClearSession work and write their Set-Cookie to the recorder. Every Ctx of the same t shares
// the store, so a cookie from one can sign in the next. If r carries the session cookie, the Ctx
// starts signed in. WithSession sets up a signed-in request without any of that.
//
// The Ctx is the one a method handler receives. Run a Load or a Layout function through
// tifa.Use(c, Load) to get the rules it runs under in the server: it runs once per request, and
// writing the response body fails.
func NewCtx(t testing.TB, r *http.Request) (*tifa.Ctx, *httptest.ResponseRecorder) {
	t.Helper()
	w := httptest.NewRecorder()
	c := core.New(&committer{ResponseWriter: w}, r, r.Pattern, config.Default().Server.BodyLimit)
	if err := core.AttachSession(c, sessionEnv(t)); err != nil {
		t.Fatalf("tifatest: reading the session: %v", err)
	}
	t.Cleanup(func() {
		// The server leaves the files of r itself to net/http, which no test runs.
		var errs []error
		if r.MultipartForm != nil {
			errs = append(errs, r.MultipartForm.RemoveAll())
		}
		if err := errors.Join(append(errs, core.Finish(c))...); err != nil {
			t.Errorf("tifatest: removing the multipart files of the request: %v", err)
		}
	})
	return c, w
}

var stores sync.Map // testing.TB to *core.SessionEnv

// sessionEnv returns the session environment of t, made on first use with a store that is closed
// when t ends. It follows the defaults of the server.
func sessionEnv(t testing.TB) *core.SessionEnv {
	cfg := config.Default().Session
	store := core.NewMemoryStore(cfg.MemoryLimit)
	env := &core.SessionEnv{Store: store, Cookie: cfg.Cookie, Secure: true, MaxAge: cfg.MaxAge}
	if got, loaded := stores.LoadOrStore(t, env); loaded {
		store.Close()
		return got.(*core.SessionEnv)
	}
	t.Cleanup(func() {
		stores.Delete(t)
		store.Close()
	})
	return env
}

// WithSession makes the request of c signed in with s, as tifa.Session[S] returns it, without a
// cookie and without the store. Use it to set up the signed-in state of a Load, a method handler or
// OnRequest. It does not check that the cookie and store flow works, which the framework's own
// tests do. S needs no Browser method or anything else to be used here.
func WithSession[S any](c *tifa.Ctx, s *S) { core.InjectSession(c, s) }

// committer tells the session functions whether the response is out, as the server's writer does:
// a body write, a final status or a flush commits it. The recorder behind it stays what the test reads.
type committer struct {
	http.ResponseWriter
	wrote bool
}

func (w *committer) Committed() bool { return w.wrote }

func (w *committer) WriteHeader(code int) {
	w.ResponseWriter.WriteHeader(code)
	if code >= 200 || code == http.StatusSwitchingProtocols {
		w.wrote = true
	}
}

func (w *committer) Write(b []byte) (int, error) {
	w.wrote = true
	return w.ResponseWriter.Write(b)
}

func (w *committer) Flush() {
	w.wrote = true
	http.NewResponseController(w.ResponseWriter).Flush()
}

func (w *committer) Unwrap() http.ResponseWriter { return w.ResponseWriter }
