---
summary: 'How to set up the whole app in hooks.go and setup.ts: OnStart, OnShutdown, OnRequest, OnError, middleware'
audience: app
order: 6
read_when:
  - Opening a database, starting work or setting a logger when the program starts, or stopping it cleanly
  - Writing `OnRequest` or `OnError`, or wrapping the app in `net/http` middleware with `tifa.SetHTTPHandler`
  - Setting up something once for the whole app on the JavaScript side, such as i18next formatters or dayjs plugins, in setup.ts
  - Finding out where a resource the whole app shares, such as a database handle, lives
---

# Hooks

## hooks.go

`hooks.go` in the project root. All four functions are optional:

```go
func OnStart(ctx context.Context) error         // before listening: open the database, logs, etc.
func OnShutdown(ctx context.Context) error      // after an exit signal, once in-flight requests finish
func OnRequest(c *tifa.Ctx, next func(*tifa.Ctx) error) error // the only middleware, call next(c) to continue
func OnError(c *tifa.Ctx, err error) tifa.Error // unexpected errors: report, return an error users may see
```

- `OnStart` gets the context of the program: it is done when the program gets SIGINT or SIGTERM. Pass it to the calls that can wait, such as `db.PingContext(ctx)`, so Ctrl-C does not wait for a database that never answers. If `OnStart` returns an error, the program stops. `ctx` stays valid after `OnStart` returns, so a goroutine may watch it, but it is done as soon as the stop signal comes: before the requests in flight finish and before `OnShutdown` runs. Work that requests feed, such as a job queue, runs on a context of its own that `OnShutdown` cancels, so the jobs the last requests queue still run (see [Background work](../guides/jobs.md)).
- `OnShutdown` runs once the requests in flight have returned. A handler that streams with no end of its own, such as server-sent events, would hold that for `server.shutdown_timeout`: it returns when `tifa.ShuttingDown(c.Context())` closes, as the stop starts (see [Streaming](../guides/streaming.md#when-the-program-stops)).
- `OnRequest` runs for pages, navigation data requests, API routes and unmatched URLs. It does not run for static files, and it does not see a write that a browser sends from another site: the program refuses that with 403 first (see [Cross-site writes](../build-and-deploy/config.md#cross-site-writes)). It sees the [security headers](../build-and-deploy/config.md#security-headers) already set and can change them. For an unmatched URL, `c.Pattern()` is empty, and `next(c)` runs the Layouts its 404 shows inside (see [404](pages.md#404)). On a page with `Cache` it sees an anonymous request, with no cookie, session or most headers (see [Cache for CDNs](ssr-and-cache.md#cache-for-cdns)).
- Resources the whole app shares, such as a database connection or a logger, go in files at the app root next to `hooks.go`, for example `db.go` or `middleware.go`. Each one builds a package-level variable or a function: set it up in `OnStart`, release it in `OnShutdown`, and use it directly anywhere, for example `myapp.DB` after importing `example.com/myapp`. Domains under `app/` import the root package for this. Models and queries stay in their domains. Route code uses these resources only inside functions, never while package-level variables are initialized. Do not use `init()` for setup. See [which way imports go](../start/structure.md#which-way-imports-go).
- `next` runs the matched route, once. A second call returns an error. Pass it the `*tifa.Ctx` the route should see: `next(c)`, or `next(c.WithContext(ctx))` to store request-scoped values in the standard context, as `r.WithContext(ctx)` does for a request. `c.WithContext` returns a copy and leaves `c` alone. Business packages provide typed getters.
- The error `next` returns can be recognized with `errors.Is` and `errors.As`, a redirect as `*tifa.RedirectError`, and a render turned away because every VM is busy as `tifa.ErrSSRBusy` (see [Handling a rejected render](../build-and-deploy/config.md#handling-a-rejected-render)).

### Changing the log handler

Tifa logs with `log/slog` and looks up `slog.Default()` for each message, so `slog.SetDefault` in `OnStart` changes where its lines go and in what format. See [Logs](../guides/logs.md) for what Tifa logs, the format and the level, and logging each request.

### Setting up in OnStart

`OnStart` is where the app wires what the whole process shares: the log handler, plain `net/http` middleware, the [session store](sessions.md#where-to-store-sessions) and the pages of the [sitemap](../guides/seo.md) with `tifa.SetSitemap`.

```go
import (
	"log/slog"
	"net/http"
	"net/http/pprof"
	"os"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

func OnStart(ctx context.Context) error {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	tifa.SetHTTPHandler(func(app http.Handler) http.Handler {
		mux := http.NewServeMux()
		mux.HandleFunc("/debug/pprof/", pprof.Index)
		mux.Handle("/", otelhttp.NewHandler(app, "tifa"))
		return mux
	})
	return nil
}
```

`tifa.SetHTTPHandler(wrap)` gets Tifa's handler and returns the one to serve. The wrapper is outside everything else: static files, the cross-site check and the routes. It sees every request with the original `http.ResponseWriter`, including the 403 of a refused cross-site write. Once the handler it wraps has returned, `r.Pattern` of that request is the route template, the same as `c.Pattern()` (such as `/projects/{project}`, and empty when no route matched), so a metrics middleware can count by route and not by URL. The browser asks for the data of a navigation with the `_tifa` parameter, and `.p` marks a preload. Do not count page views from these requests: a preload can serve a navigation without another request, and a reload adds one. Count the client navigations the browser finishes, plus the full page requests (see [Page requests](../build-and-deploy/build.md#page-requests)). It is plain `net/http`, so `OnRequest`, `OnError` and the cross-site check do not apply to the paths it serves itself, such as `/debug/pprof/` above. Put such paths behind your own check, or keep them on another listener. A shutdown still waits for the requests it wraps. Call it in `OnStart` only: it panics once the server listens. `tifa.SetHTTPHandler(nil)` removes the wrapper. It runs outside the anonymous request of a page with `Cache` (see [Cache for CDNs](ssr-and-cache.md#cache-for-cdns)): the values it puts into the context of the request reach `OnRequest` and `Load` of such a page, so do not recognize visitors in it, do it in `OnRequest`.

## setup.ts

`setup.ts` in the project root is the JavaScript side of `hooks.go`. It is optional: without it, the app renders as it would with an empty one. It is plain top-level code, with no function to export:

```ts
// setup.ts
import { i18n } from 'tifa/i18n'
import dayjs from 'dayjs'
import relativeTime from 'dayjs/plugin/relativeTime'

dayjs.extend(relativeTime)
i18n.services.formatter?.addCached('currency', (lng, options) => {
  const f = new Intl.NumberFormat(lng, { currency: 'USD', ...options, style: 'currency' })
  return (value) => f.format(value)
})
```

- Its top level runs once in each JavaScript runtime before any route module evaluates: in each VM of the server when the SSR bundle loads, and in the browser before hydration. A route module that reads at its own top level what `setup.ts` set sees it. It is for what the whole app sets up once, such as i18next formatters, dayjs plugins, chart types or an error reporter.
- It runs on both sides, so the server and the browser render with the same setup. Tifa has no JavaScript that runs only on the server: data and I/O are in Go.
- It is synchronous. Work it starts and does not wait for, such as a dynamic `import()`, may finish after the first render.
- If it throws on the server, the program stops before it serves. In the browser the page is not hydrated: it stays the HTML the server sent, links load whole pages, and the error is in the console.
- Tifa ignores what it exports. An app without pages has no JavaScript, and a `setup.ts` there fails the build.
- `setup.ts` is in the main bundle of the browser, with what it imports. Keep it small.

## Sessions

A session is the record of who is signed in, kept on the server and found by a cookie. Sign in with `tifa.SetSession(c, &s)` in a method handler, read it with `tifa.Session[S](c)` anywhere that has the Ctx, and sign out with `tifa.ClearSession(c)`. [Sessions](sessions.md) has the rest: the cookie, a store of your own on Redis or SQL, where each call may be made, `useSession` and `SignedIn` in the browser, testing and the errors.

