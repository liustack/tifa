// Package tifatest builds the tifa.Ctx of a test request, so the Load, Layout, method handler and
// OnRequest functions of an app can be called in a Go test like any other function. It tests the
// function, not the HTTP answer: input decoding, Validate, turning a result or an error into a
// response, OnError and the navigation data protocol run only in the server.
package tifatest

import (
	"errors"
	"net/http"
	"net/http/httptest"
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
// The Ctx is the one a method handler receives. Run a Load or a Layout function through
// tifa.Use(c, Load) to get the rules it runs under in the server: it runs once per request, and
// writing the response body fails.
func NewCtx(t testing.TB, r *http.Request) (*tifa.Ctx, *httptest.ResponseRecorder) {
	t.Helper()
	w := httptest.NewRecorder()
	c := core.New(w, r, r.Pattern, config.Default().Server.BodyLimit)
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

// SetNext makes c the Ctx of OnRequest: Ctx.Next calls next once, as it is, and returns its
// error. next stands in for the route, so it does not recover a panic or write a result.
func SetNext(c *tifa.Ctx, next func() error) { core.SetNext(c, next) }
