---
summary: 'Where the logs of a Tifa app are in `tifa dev`, on a server and in a container, what Tifa itself logs, how to log from your own code with `log/slog`, how to set the format and the level, and how to log each request with `tifa.SetHTTPHandler`'
audience: app
order: 11
read_when:
  - Looking for the logs of an app, in `tifa dev`, on a server or in a container
  - Wondering what Tifa logs on its own, or why a request leaves no line
  - Logging from a route, a Load or a job, or choosing the format and the level of the log
  - Logging each request with its route, status and time
---

# Logs

Tifa writes its own messages with `log/slog`, and your code uses the same logger. One call to `slog.SetDefault` in `OnStart` changes where all of it goes, in what format and from which level. Tifa does not log each request and has no log level setting of its own. The [request log](#logging-each-request) below is about twenty lines of your own.

## What Tifa logs

Every message starts with `tifa:`. The fields are the `slog` attributes of the line.

- `ERROR` `tifa: request failed`, with `method`, `path` and `err`: a route returned an error and the app has no `OnError`. With an `OnError`, Tifa logs nothing for it. Report the error there (see [Hooks](../pages-and-data/hooks.md)).
- `ERROR` `tifa: request failed after the response was written`, with `method`, `path` and `err`: the error came too late to change the answer. This one is logged with or without `OnError`.
- `ERROR` lines for a page that could not be rendered for anyone (`a render failed and no request waits for it`, `the page with the error failed too`), for a session that could not be written to the answer, and for a mistake in `tifa.SetSitemap` (`SetSitemap: <kind>`).
- `WARN` `tifa: refused a cross-site write`, with `method`, `path`, `reason`, `origin`, `sec_fetch_site` and `host`. See [Cross-site writes](../build-and-deploy/config.md#cross-site-writes).
- `WARN` `tifa: dropping a session`, with `why`, `method` and `path`: the stored session of a request could not be used (it expired, is damaged or does not fit the type that reads it), so the visitor is signed out.
- `WARN` once, when a sign-in comes over plain HTTP and the browser will drop the Secure cookie.
- `WARN` lines from the render (`tifa: ssr:`): a call to `Intl` without a locale (see [Warnings in `tifa dev`](../pages-and-data/server-render.md#warnings-in-tifa-dev)), and a VM that was replaced, with the reason.
- `WARN` and `INFO` lines about HTTPS: the certificate that was obtained or loaded (`domain`, `valid_until`), a certificate that is not there yet, and HTTPS on a loopback address.
- `INFO` `tifa: listening on <address>`. It comes after `OnStart` returns, so a handler set there gets it. Under `tifa dev` the app does not print it.
- `INFO` one-time notes: sessions are kept in memory, `sitemap.xml` lists only pages without parameters, and a page of `content/` was published.

Lines about a request use the `Context` forms, so a handler that reads the context of the request sees it.

## Logging from your code

Use `log/slog` and pass the context of the request, so that a handler that reads it, for a request ID or a trace, gets it:

```go
func POST(c *tifa.Ctx) error {
	// ...
	slog.InfoContext(c.Context(), "order placed", "order", id, "total", total)
	return c.JSON(201, order)
}
```

`slog.WarnContext`, `slog.ErrorContext` and `slog.DebugContext` work the same. Outside a request, such as in `OnStart` or a job, use `slog.Info` and the others.

## Format and level

`slog.SetDefault` in `OnStart` sets the handler for Tifa and for your code. The level is a setting of the handler. Here it comes from an option of the app (see [App options](../build-and-deploy/config.md#options-of-the-app)), so `LOG_LEVEL=debug` or `--log-level debug` changes it without a build.

`options.go`:

```go
var LogLevel = tifa.String("log.level", "info", "Log level: debug, info, warn or error")
```

`hooks.go`:

```go
func OnStart(ctx context.Context) error {
	var level slog.Level
	if err := level.UnmarshalText([]byte(LogLevel)); err != nil {
		return fmt.Errorf("log.level: %w", err)
	}
	opts := &slog.HandlerOptions{Level: level}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, opts)))
	return nil
}
```

`slog.NewTextHandler` writes `key=value` lines for reading in a terminal. Without `SetDefault`, the log goes to stderr as text, at level `INFO` and above. To change the level while the program runs, keep a `slog.LevelVar` and pass `&slog.HandlerOptions{Level: &levelVar}`, then call `levelVar.Set(...)` from your own code.

## Logging each request

Tifa has no request log. Wrap the app with `tifa.SetHTTPHandler` in `OnStart` (see [Hooks](../pages-and-data/hooks.md#setting-up-in-onstart)). The wrapper times the request and reads the route template from `r.Pattern` once the app has answered:

```go
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// Unwrap lets http.ResponseController reach Flush, SetWriteDeadline and Hijack below.
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func logRequests(app http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			app.ServeHTTP(w, r) // bundle files: no line
			return
		}
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		app.ServeHTTP(sw, r)
		slog.InfoContext(r.Context(), "request",
			"method", r.Method, "route", r.Pattern, "status", sw.status, "duration", time.Since(start))
	})
}
```

Then in `OnStart`: `tifa.SetHTTPHandler(logRequests)`.

- `route` is the template, such as `/projects/{projectId}`, not the URL, so lines of one page group together and carry no ids. It is empty when no route matched: an unmatched URL (404) and a file of the `public` folder. Add `r.URL.Path` to the line if you want the path.
- A client navigation asks for its data with the `_tifa` query parameter, and `.p` marks a preload. Each is a request with a line of its own, so the number of lines is not the number of page views (see [Hooks](../pages-and-data/hooks.md#setting-up-in-onstart)).
- Files under `/assets/` are the bundle of the browser, and the example skips them. Files of the `public` folder have no prefix to tell them apart, so they get a line.
- A stream, such as server-sent events, is logged when it ends, with its whole duration (see [Streaming](streaming.md)). Streaming flushes through `http.ResponseController`, which finds `Flush` by the `Unwrap` method. Without `Unwrap` the wrapper holds the stream back.
- The status is what the app wrote. The wrapper sees the 403 of a refused cross-site write too.

## Where the logs go

- **`tifa dev`**: the terminal. With `--background`, all output goes to `.tifa/dev.log`: read it with `go tool tifa dev logs`, and follow it with `-f` (see [Logs](../build-and-deploy/dev.md#logs)).
- **A server set up with `tifa deploy`**: stdout and stderr go to systemd's journal. `go tool tifa deploy logs` prints the last 100 lines, `-n 500` for more and `-f` for the new ones. On the server, `journalctl --user -u tifa-<app>` as the user `tifa` does the same (see [Logs, status and remove](../build-and-deploy/deploy.md#logs-status-and-remove)).
- **How long the server keeps them**: that is the journald setup of the server, not Tifa. The limits are in `/etc/systemd/journald.conf`, such as `SystemMaxUse` for the disk it may use and `MaxRetentionSec` for the age. `journalctl --disk-usage` shows what it uses now. A journal kept in memory (`Storage=volatile`) is gone after a restart of the server.
- **A container**: stdout and stderr. The runtime of the container, such as Docker or Kubernetes, collects them. Send the log to stdout, as the example of [Format and level](#format-and-level) does, and keep it out of files in the container.
