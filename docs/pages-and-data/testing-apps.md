---
summary: 'How to test an app with go test: calling Load, a method handler or OnRequest with a Ctx from tifatest.NewCtx, testing a whole request with tifatest.Start, and fixing the clock of a render'
audience: app
order: 9
read_when:
  - Writing Go tests for a Load, a Layout function, a method handler or OnRequest
  - Testing a whole request in go test, such as signing in and following the redirect, with tifatest.Start or tifatest.StartPages
  - '`tifatest.Start` fails with a message about `.tifa/tifaapp` or `.tifa/dist`'
  - A test needs a fixed time in the render, with `tifa.WithRenderClock`
---

# Testing

Tests run with `go test`, next to the code, in two ways. A test of one function calls `Load`, a method handler or `OnRequest` with a `Ctx` from `tifatest.NewCtx`. A test of a whole request starts the app with `tifatest.Start` and sends it HTTP requests, so it can sign in, post a form, follow the redirect and read the page.

## Testing a function

`Load`, `Layout`, the method handlers and `OnRequest` are plain Go functions. `tifatest.NewCtx(t, r)` turns an `httptest` request into the `*tifa.Ctx` the function takes, and returns the recorder that holds what the function writes:

```go
// app/projects/project+/orders/order+/route_test.go
func TestLoad(t *testing.T) {
	r := httptest.NewRequest("GET", "/projects/studio/orders/SH-1041", nil)
	r.Pattern = "/projects/{project}/orders/{order}"
	r.SetPathValue("project", "studio")
	r.SetPathValue("order", "SH-1041")
	c, _ := tifatest.NewCtx(t, r)
	o, err := tifa.Use(c, Load)
	// check o and err
}
```

This tests the function, not the HTTP answer. Check what the function returns: the value and the error. Only the server decodes the input, runs `Validate`, turns a result into JSON or 204 and an error into 404, 422, a redirect or a call to `OnError`, and answers navigation data requests. A returned value or error is not written into the recorder: when the function writes nothing itself, the recorder stays an empty 200. To check the HTTP answer, [test the whole request](#testing-a-whole-request).

- Path parameters are what `c.Param` reads: set the decoded values with `r.SetPathValue`. `c.Pattern()` is `r.Pattern` as it is when `NewCtx` is called, and `c.Path()` is the escaped path of the request URL, so write the language prefix in the path of the request to test it (`/en-US/store`). Under `tifatest.Start` the server builds the Ctx and `c.Path()` is what it is for a real request.
- Run `Load` and `Layout` through `tifa.Use(c, Load)`. It runs them under the rules of the server: once per request, and writing the response body fails. A `tifa.Use` of a Layout inside `Load` runs that Layout for the same request. Only the Layouts the function asks for with `tifa.Use` run.
- Call a method handler directly: `POST(c)`, or `POST(c, in)` with a typed input. The input is the value you pass, so test `Validate` on its own, and if `Validate` changes the input, pass the changed input.
- Call `OnRequest` directly with a closure for `next`: `OnRequest(c, func(c *tifa.Ctx) error { got = c.Context(); return nil })`. The closure stands in for the route, so it neither recovers a panic nor writes a result.
- The body limit is the default `server.body_limit`, 1 MiB, unless `c.SetBodyLimit` changes it. `c.SetReadTimeout` does nothing, since a test has no connection. The test removes the multipart files of the request when it ends.
- `tifatest.File(t, "sea.png", data)` gives the `*multipart.FileHeader` of an uploaded file, for an input you pass to a handler or check with `Validate`. Its `Content-Type` follows the extension of the name. Its temporary files are removed when the test ends.
- Read the recorder once the function has returned, and make a new `NewCtx` for each request: `rec.Result()` keeps the headers it saw the first time it was called.
- `tifa build` reads no `_test.go` files, so tests may sit in any folder under `app/`.

## Testing a whole request

`tifatest.Start(t, tifaapp.App)` runs the app inside the test and returns an `*httptest.Server`. `tifaapp` is the package `.tifa/tifaapp` that `tifa check`, `tifa build` and `tifa dev` generate: the routes, Layouts, hooks and options of the app, as the program serves them. It imports every route package, so import it from a test in a package of its own, named after the folder with `_test` (`package login_test`). From the shop, `app/login/request_test.go`:

```go
package login_test

import (
	"strings"
	"testing"

	"github.com/liustack/tifa/examples/shop"
	"github.com/liustack/tifa/examples/shop/.tifa/tifaapp"
	"github.com/liustack/tifa/tifatest"
)

func TestSignInLeadsBackToTheWorkbench(t *testing.T) {
	srv := tifatest.Start(t, tifaapp.App)
	client := srv.Client()

	res, _ := client.Get(srv.URL + "/projects/studio/stats")
	// 302 to /login?next=%2Fprojects%2Fstudio%2Fstats

	body := `{"email":"` + shop.DemoEmail + `","password":"` + shop.DemoPassword + `","next":"/projects/studio/stats"}`
	res, _ = client.Post(srv.URL+"/login", "application/json", strings.NewReader(body))
	// 303 to /projects/studio/stats, and the client keeps the session cookie

	res, _ = client.Get(srv.URL + "/projects/studio/stats")
	// 200, with the data of the page
}
```

- Everything the program runs takes part: `OnStart` with the context of the test, `OnRequest`, decoding the input, `Validate` and its 422, sessions, redirects and `OnError`. When the test ends, the server waits for the requests in flight and runs `OnShutdown`.
- `srv.Client()` trusts the certificate of the server, keeps the cookies it sets and does not follow redirects, so the test sees each 3xx with its `Location`.
- The server listens on HTTPS on this machine. The settings of Tifa are those of the program, from the defaults and `tifa.yaml`, apart from the address and HTTPS. Options of the app keep their defaults, as flags and environment variables do not reach a test, so set the variable before `Start`: `myapp.DBPath = filepath.Join(t.TempDir(), "test.db")`.
- Without a front end, a page answers the results of its `Load` and `Layout` functions as JSON, by route ID: `{"app/layout": …, "app/projects/project+/stats/route": …}`. Method handlers answer as in the program.
- A number above 2^53 is refused only where a page reads it (see [JSON rules](routes.md#json-rules)). With `Start`, a `Load` answer is checked as for a page, and a handler is checked only for a request that sets the header `X-Tifa-Call: 1`, as the generated function of a page does. To pin that a result reaches the browser whole, post to the handler with that header and expect 200 and the string. A test with `StartPages` gets the check for pages.
- Each `Start` makes a new server, with sessions of its own unless `OnStart` sets a store. The app is one per test binary at a time, as the session store, `SetHTTPHandler` and the content belong to the process, so a test that calls `Start` cannot call `t.Parallel`.
- `Start` fails the test when `.tifa/tifaapp` was made from other routes than the code has now. Run `go tool tifa check` again, or keep `tifa dev` running. In a fresh checkout `.tifa` is missing, and `go vet`, `go test` and `go mod tidy` fail on the import until `tifa check` writes it. `tifa check` generates before it runs `go vet` and `go test`, so it needs nothing first.

`tifatest.StartPages(t, tifaapp.App)` also renders the pages to HTML, with the front end that `tifa build` or `tifa dev` wrote to `.tifa/dist`, and serves its scripts, stylesheets and public files. `tifa check` does not bundle, so run `tifa build` before such tests, or keep `tifa dev` running. `StartPages` fails the test when `.tifa/dist` is missing, and when a file the front end was built from has changed since: it names the files, such as a page, a component, a stylesheet, `package.json`, the lock file or the Vite config. Pages render on two VMs whatever `ssr.vms` says, and the test binary never holds more. The first `StartPages` starts them, which takes a moment. Later ones with the same build use them, and one with another build closes them and starts two new ones.

## Render clock

Set the clock through the context in `OnRequest`, or give an `httptest` request the same context in a test:

```go
at := time.Date(2026, 10, 22, 12, 0, 0, 123456789, time.UTC)
ctx := tifa.WithRenderClock(c.Context(), func() time.Time { return at })
return next(c.WithContext(ctx))
```

Date and Intl round down to milliseconds. Temporal keeps nanoseconds. The clock covers the whole render, including promises and queueMicrotask, and is cleared before VM reuse or replacement. Pass nil to restore the live clock. The callback must not block and must be safe for concurrent calls when renders share it. It does not change Go's time.Now, timeouts or the browser clock.

Without `WithRenderClock` the clock stays live. A fixed clock does not make a render repeatable when it also uses randomness or outside data.

Module top-level code runs during VM prewarming, before a request, on the live clock. Time values captured there are not recomputed. A Date.now function, Temporal.Now method or Intl formatter saved at module load reads the current render's clock when called during that render.

The clock is not a cache key, so a request with a clock skips the render memo and shared renders and gets no ETag: it always renders with its own clock. Return displayed times from `Load` so SSR, caching and browser hydration use the same data. `tifatest.NewCtx` preserves the request context but does not run SSR itself. Call `OnRequest` with a closure for `next` to test middleware passing this context, and use server requests to check full HTML.

