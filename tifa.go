// Package tifa is the API of Tifa, the full-stack Go framework, that application code imports: the request context, errors and Layout data reuse.
package tifa

import (
	"context"
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

// ShuttingDown returns a channel that is closed when the server that took the request starts to
// shut down: it has stopped taking new connections, and gives the requests in flight
// server.shutdown_timeout to finish. ctx is the context of the request, c.Context(), or one made
// from it. A handler that streams with no end of its own, such as server-sent events, returns
// when the channel is closed, so the program can stop at once. A request that ends by itself,
// such as a long upload or an answer still being written, can ignore it and finish.
//
// For a context that no server made, as in a test, it returns nil, which never fires in a select.
func ShuttingDown(ctx context.Context) <-chan struct{} { return core.ShuttingDown(ctx) }

// HTML is markup the app trusts, such as an article the build turned from Markdown into HTML or
// HTML a CMS returns, which a page shows as it is with <HTML value={...}> of tifa/client. It may be
// in the data of Load and Layout and in the result of a method handler, and is written as a JSON
// string, but it cannot be a map key or part of an input or the session. Only put markup you trust
// into it: sanitize HTML a user wrote before converting it, as with template.HTML of html/template.
//
// A page the server renders carries such a value once, in its markup: its hydration data holds a
// marker in place of the value, and <HTML> reads the markup back from the page when the browser
// hydrates it. Navigation data and a page with SSR = false carry the string.
type HTML string
