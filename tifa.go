// Package tifa is the API that application code imports: the request context, errors and Layout data reuse.
package tifa

import (
	"net/http"

	"github.com/liustack/tifa/internal/core"
)

// Ctx wraps one request and its response. Load, the method handlers and OnRequest receive it.
type Ctx = core.Ctx

// Error is a failure that is safe to show to the client. Returned from Load, a method handler or OnRequest,
// it is answered with its own status and message without calling OnError. OnError also returns it.
type Error = core.Error

// Fields is a message per input field, by its json name. Returned from Validate or a handler, it is
// answered with 422 and the messages as "fields" in the JSON, without calling OnError.
type Fields = core.Fields

// RedirectError is what Redirect returns. OnRequest and tests can match it with errors.As.
type RedirectError = core.RedirectError

// Redirect returns an error that makes the server redirect to url with status, such as
// http.StatusSeeOther. Return it from Load, a method handler or OnRequest. It panics when status is
// not 3xx, which is a mistake in the code and not a condition at run time.
func Redirect(url string, status int) error { return core.Redirect(url, status) }

// ErrNotFound makes the server answer 404 without calling OnError.
var ErrNotFound = core.ErrNotFound

// ErrSSRBusy means SSR was rejected because its queue was full or its wait expired.
// OnRequest can match it with errors.Is. The default response is 503 with Retry-After.
var ErrSSRBusy = core.ErrSSRBusy

// Use returns the result of a Layout function for this request, the Layout of the caller's
// directory or of a directory above it: tifa.Use(c, project.Layout).
// Each function runs at most once per request, a second caller waits for the first run.
func Use[T any](c *Ctx, load func(*Ctx) (T, error)) (T, error) { return core.Use(c, load) }

// SetHTTPHandler wraps the handler of the whole server with wrap, which gets tifa's handler and
// returns the handler to serve. Use it for plain net/http middleware, such as pprof or OpenTelemetry. The
// wrapper sees every request, static files and the 403 of a cross-site write included, with the
// original http.ResponseWriter, and does not go through OnRequest, OnError or the cross-site check.
// A shutdown waits for the requests it wraps. Call it in OnStart: it panics once the server listens.
// nil removes the wrapper. It runs outside the anonymous request of a page with Cache: the values it
// puts into the context of the request reach OnRequest and Load of such a page, so do not recognize
// visitors in it, do it in OnRequest.
func SetHTTPHandler(wrap func(app http.Handler) http.Handler) { core.SetHTTPHandler(wrap) }
