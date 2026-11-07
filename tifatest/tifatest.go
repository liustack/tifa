// Package tifatest tests a tifa app in go test, in two ways.
//
// NewCtx builds the tifa.Ctx of a test request, so the Load, Layout, method handler and OnRequest
// functions of an app can be called like any other function. That tests the function, not the HTTP
// answer: input decoding, Validate, turning a result or an error into a response, OnError and the
// navigation data protocol run only in the server.
//
// Start runs the whole app in the test process, from tifaapp.App, and returns a server to send
// requests to, so a test can sign in, post a form, follow the redirect and read the page.
// StartPages also renders the pages with the front end of tifa build.
package tifatest

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/internal/config"
	"github.com/liustack/tifa/internal/core"
)

// NewCtx returns the Ctx of r and the recorder that holds what is written through it. A returned
// result or error is not written, so the recorder stays an empty 200 unless the function writes.
// Read the recorder once the function returned: its Result keeps the headers of the first call, so
// make one NewCtx per request and do not call the function again on the same Ctx. Path
// parameters are the path values of r, set with r.SetPathValue, and Ctx.Pattern is r.Pattern as it is when NewCtx is called. The
// body is limited to the default server.body_limit, 1 MiB, unless Ctx.SetBodyLimit changes it.
// A handler that streams may flush and move its deadlines with http.ResponseController, as on the
// server.
//
// On a site with languages, Ctx.Language is the language the path of r starts with, such as en-US
// for /en-US/store, and the default language for a path without one.
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
	core.SetLanguage(c, languageOf(tifa.Languages(), r.URL.Path))
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

// languageOf is the language of a request to path on a site with languages, the default one
// first: the language its first segment names, else the default. A site without has none.
func languageOf(languages []string, path string) string {
	if len(languages) == 0 {
		return ""
	}
	first, _, _ := strings.Cut(strings.TrimPrefix(path, "/"), "/")
	if slices.Contains(languages, first) {
		return first
	}
	return languages[0]
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
// tests do. S needs no Exposed method or anything else to be used here.
func WithSession[S any](c *tifa.Ctx, s *S) { core.InjectSession(c, s) }

// committer tells the session functions whether the response is out, as the server's writer does:
// a body write, a final status or a flush commits it. The recorder behind it stays what the test reads.
type committer struct {
	http.ResponseWriter
	wrote bool
}

func (w *committer) Committed() bool { return w.wrote }

// WriteHeader keeps a 1xx other than 101 from the recorder. On the server such a status goes out
// before the response and leaves its headers open, but httptest.ResponseRecorder takes any status
// as the final one and fixes the headers of Result at it.
func (w *committer) WriteHeader(code int) {
	if code >= 100 && code <= 199 && code != http.StatusSwitchingProtocols {
		return
	}
	w.ResponseWriter.WriteHeader(code)
	w.wrote = true
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

// SetReadDeadline and SetWriteDeadline take the deadlines a handler moves with
// http.ResponseController, as the server does, so the handler runs here as it runs there. Nothing
// in a test times out, so they do nothing.
func (w *committer) SetReadDeadline(time.Time) error  { return nil }
func (w *committer) SetWriteDeadline(time.Time) error { return nil }
