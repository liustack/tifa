---
summary: 'Every setting of a Tifa app and how an app defines its own, and the security headers every answer carries, the Content-Security-Policy of server.csp among them: where each one can be set (flags, environment variables, .env, the file of -c, tifa.yaml), which source wins, how PORT and HOST are read, the default, the flag and environment variable names, the value formats and the errors a bad value gives, how a handler that needs longer lifts the read and write timeouts, how to serve HTTPS with certificates the program gets itself or with your own, and how the program refuses writes that a browser sends from another site and how to let chosen origins or paths through'
read_when:
  - Defining an option of the app with tifa.String and the like, or reading one in OnStart
  - Writing a .env file, or deploying on a platform that sets PORT
  - Changing how long a session lasts, the session cookie name, or how much memory the default session store may use
  - An upload, a download or a stream is cut off after about 10 seconds, or a handler needs longer than the read and write timeouts
  - Deploying a Tifa app, or changing its port, body limit, read and write timeouts, compression, VM count, VM stack depth, render timeout, render queue, render memo, or the Cache-Control a page with `Cache` sends
  - Serving HTTPS from the program itself, with Let's Encrypt or your own certificate, or deciding whether to
  - The program cannot listen on port 80 or 443, or gets no certificate
  - A POST, PUT, PATCH or DELETE gets 403 with `cross-origin request detected`, or a page of another site or a provider's callback must be allowed to send one
  - Setting the browser target, or the static files folder of a build
  - Setting the default language of an app, the language prefix in its URLs, or the address of the site
  - The program refuses to start with a config error
  - A page must be shown in a frame on another site, or a header such as Referrer-Policy or Content-Security-Policy needs another value
  - Turning on a Content-Security-Policy, or a script or style of a page is blocked by one
  - Changing internal/config
---

English · [简体中文](config.zh-CN.md)

# Configuration

A Tifa app runs with sensible defaults for a small machine. Change a setting only when you need to.

## Where settings come from

From highest to lowest priority:

1. Command-line flags, such as `--server-addr 0.0.0.0:9527`.
2. Environment variables, such as `SERVER_ADDR=0.0.0.0:9527`.
3. The `.env` file in the folder the program runs in.
4. The deployment file given with `-c path.yaml` (or `--config path.yaml`).
5. The project file `tifa.yaml` in the app root. `tifa build` reads it and builds its values into the program.
6. The defaults: those in the table below for the settings of Tifa, those in `options.go` for the [options of the app](#options-of-the-app).

A source sets only the keys it names, so a deployment file holds just what it changes. The program reads only its own keys: other keys of a file and other environment variables are left alone. A flag that is neither a setting of Tifa nor one of the app, such as `--addr` for `--server-addr`, stops the program with `unknown flag`.

`tifa.yaml` holds values of the settings of Tifa. `build.*`, `i18n.*` and `site.*` settings can only be set here, and `tifa build` fails on a value of the wrong type or format. The program does not read the file: change it and build again.

```yaml
# tifa.yaml
build:
  target: es2020,safari14
server:
  addr: 0.0.0.0:9527
  body_limit: 4MiB
ssr:
  timeout: 2s
  memo_size: 128MiB
```

Run `./bin/app config` to print every setting, of Tifa and of the app, with its value and where the value comes from (`default`, `tifa.yaml`, `file prod.yaml`, `.env SSR_VMS`, `env PORT`, `flag --ssr-vms` and so on). The output is a commented YAML file, a good starting point for a deployment file. It leaves out the values of the app's secrets ([`tifa.Secret`](#options-of-the-app)).

### Names

Each setting has a key, a flag and an environment variable, all from one rule:

| Key | Flag | Environment variable |
| --- | --- | --- |
| `db.url` | `--db-url` | `DB_URL` |
| `server.body_limit` | `--server-body-limit` | `SERVER_BODY_LIMIT` |

- A key is lower case letters, digits and underscores, in at least two parts joined with dots.
- The flag replaces dots and underscores with hyphens. The environment variable replaces dots with underscores and is in upper case. There is no prefix.
- In a YAML file each part of the key is one level: `db.url` is `url` under `db:`.
- A flag takes its value after a space or after `=`: `--ssr-vms 4` or `--ssr-vms=4`. A word that starts with `-` is always a flag and never a value, so a value that starts with `-`, such as a negative number, goes after `=`: `--ssr-vms=-1`. A flag without its value fails. The program reads no flag after `--`.

### .env

The program reads the file `.env` in the folder it runs in, with lines of `NAME=value`. A real environment variable wins over the same name in `.env`. Without the file nothing happens. `tifa build` does not put it into the program, and there is one `.env` per machine, with no `.env.local` or `.env.production`: keep it out of git.

```sh
# .env
DB_URL=/var/lib/shop/shop.db
SERVER_ADDR=0.0.0.0:9527
export SESSION_MAX_AGE=24h   # a line may start with export
GREETING="Hello # not a comment\n"
```

- Lines starting with `#` and blank lines are skipped. An unquoted value ends at a `#` after a space or a tab and loses the spaces around it, so `TLS_CERT= # none` is empty and `COLOR=#fff` is `#fff`.
- A value in double quotes may hold `\n`, `\t`, `\"` and `\\`. A value in single quotes is taken as it is. A quoted value stays on one line.
- A line it cannot read stops the program with its line number.

`tifa dev` runs the app in the app root, so the app reads the `.env` there, and a change of `.env` starts the app again. Vite does not read `.env` (`envDir: false`): a page gets the values it needs from its `Load`.

### PORT and HOST

Hosting platforms such as Cloud Run, Heroku, Render, Railway and Fly tell the program its port in `PORT`. The program reads `PORT` and `HOST` for `server.addr` (not `HOSTNAME`, which shells and Docker set to the machine name):

- Their place is below `SERVER_ADDR`: `--server-addr` > `SERVER_ADDR` > `PORT` and `HOST` > the file of `-c` > `tifa.yaml` > the default. `.env` may hold them too, below the real variables.
- `PORT` alone listens on every interface: `PORT=8080` is `:8080`, as those platforms need.
- `HOST` alone changes the host and keeps the port of `server.addr` from the sources below.
- Empty ones count as not set.

`tifa dev` gives the app its address in `SERVER_ADDR`, so a `PORT` in your shell does not move it. `tifa dev` itself listens on `--addr` and does not read `PORT`.

## Settings

| Key | Default | Flag and environment variable | What it does |
| --- | --- | --- | --- |
| `build.target` | `es2020` | project file only | JavaScript target of the browser bundle that Vite builds, a comma-separated list: one ECMAScript version (`es2015` to `es2029`, `esnext`) plus browsers with versions, from chrome, edge, firefox, ios, opera and safari, such as `es2019,chrome90,safari14`. The SSR bundle always targets ES2022 |
| `build.public` | `public` | project file only | Folder of static files copied into the program as they are and served from `/` |
| `build.skip_tanstack_check` | `false` | project file only | `true` builds an app whose TanStack Router is not the version Tifa is tested with, which may leave pages blank in the browser. See [build.md](build.md#the-apps-tsconfig-and-vite-config) |
| `build.debug_info` | `false` | project file only | `true` keeps the DWARF debug info in the program, for debuggers such as Delve. Without it, `tifa build` leaves the debug info out with `-ldflags=-w`, which makes the example shop's program about 20% smaller. Panics still give the file and line of each frame either way. `tifa dev` always keeps it. See [build.md](build.md#debug-info) |
| `build.i18next_cache` | `true` | project file only | The server render keeps the strings i18next translates and gives them again for the same key and options, so pages that translate the same texts on every render take less time: the shop's storefront pages 0.778 of it. `false` turns it off. Formatters and post-processors of the app must give the same text for the same input. Turn it off when the app changes i18next's resources or options after the first render without i18next's API, or gives i18next other functions that read something of the request, or pages may show stale translations. See [build.md](build.md#i18next-cache) |
| `i18n.default` | none | project file only | The default language of an app with `_locales/` folders, a BCP 47 tag in its usual spelling such as `zh-CN` or `en-US`. URLs in this language have no prefix (unless `i18n.prefix` is `always`), and it comes first in `tifa.Languages()`. An app with one language may leave it out. With several, `tifa build` fails without it, and when no area has a file for it. Every area must have the default language. See [A site in several languages](app.md#a-site-in-several-languages) |
| `i18n.prefix` | `off` | project file only | `always` puts the language in the URL of every route that has languages, the default one too: `/store` answers 308 to `/zh-CN/store`. `off` leaves the default language without a prefix. A route outside every area never has a prefix, in both modes |
| `site.url` | none | project file only | The scheme and host the site is served at, such as `https://shop.example.com`, with no path. A site with more than one language needs it, and `tifa build` fails without it |
| `server.addr` | `127.0.0.1:9527` | `--server-addr`, `SERVER_ADDR` | Address to listen on. Use `0.0.0.0:9527` in a container. With [HTTPS](#https) on it serves HTTPS and must be set, usually to `:443` |
| `server.body_limit` | `1MiB` | `--server-body-limit`, `SERVER_BODY_LIMIT` | Largest request body. A bigger one is answered with 413. A `route.go` can set its own with `BodyLimit`, see [app.md](app.md#body-limit-and-read-time-of-a-route) |
| `server.read_timeout` | `10s` | `--server-read-timeout`, `SERVER_READ_TIMEOUT` | Time to read a whole request, its body included, from the moment the connection is taken. A body still coming after it fails to read. See [Long requests](#long-requests) |
| `server.write_timeout` | `10s` | `--server-write-timeout`, `SERVER_WRITE_TIMEOUT` | Time from the end of a request header to the end of its answer. An answer still being written after it is cut off. See [Long requests](#long-requests) |
| `server.shutdown_timeout` | `10s` | `--server-shutdown-timeout`, `SERVER_SHUTDOWN_TIMEOUT` | After a stop signal, the time in-flight requests get to finish, and then the time `OnShutdown` gets. Requests still running when it ends are canceled and get one more `shutdown_timeout` to return. `OnShutdown` runs once they have returned and is skipped if they have not. Either way the program exits with status 1 |
| `server.compress` | `true` | `--server-compress`, `SERVER_COMPRESS` | Sends text answers with gzip or zstd to clients that accept them (gzip unless the client ranks zstd higher or takes no gzip), and the browser bundle as the gzip files `tifa build` wrote, see [Compression](build.md#compression). Set it to `false` when a proxy in front compresses, such as nginx with brotli |
| `server.tls_domains` | none | `--server-tls-domains`, `SERVER_TLS_DOMAINS` | Turns on [HTTPS](#https) with certificates from Let's Encrypt for these domains, comma-separated, such as `example.com,www.example.com` |
| `server.tls_email` | none | `--server-tls-email`, `SERVER_TLS_EMAIL` | Contact address given to Let's Encrypt with `tls_domains`. Optional |
| `server.tls_cache` | none | `--server-tls-cache`, `SERVER_TLS_CACHE` | Folder that keeps the certificates of `tls_domains` across restarts. Needed with `tls_domains`, and must survive new deployments |
| `server.tls_ca` | `https://acme-v02.api.letsencrypt.org/directory` | `--server-tls-ca`, `SERVER_TLS_CA` | ACME directory that `tls_domains` get their certificates from. Let's Encrypt by default |
| `server.tls_cert` | none | `--server-tls-cert`, `SERVER_TLS_CERT` | Turns on [HTTPS](#https) with your own certificate file instead, in PEM, loaded again when it changes |
| `server.tls_key` | none | `--server-tls-key`, `SERVER_TLS_KEY` | Private key file of `tls_cert`, in PEM |
| `server.http_addr` | `:80` | `--server-http-addr`, `SERVER_HTTP_ADDR` | With HTTPS on, the plain HTTP address that answers the checks of Let's Encrypt and redirects everything else to HTTPS. `off` for none. Not used without HTTPS |
| `server.hsts` | `false` | `--server-hsts`, `SERVER_HSTS` | Adds `Strict-Transport-Security: max-age=31536000` to the answers of `server.addr`, so browsers keep to HTTPS for a year |
| `server.csp` | `off` | `--server-csp`, `SERVER_CSP` | `on` sends `server.csp_policy` as the `Content-Security-Policy` of every answer, and of each page with the hashes of the inline scripts Tifa writes into it. `report-only` sends it as `Content-Security-Policy-Report-Only`, which only reports. See [Content-Security-Policy](#content-security-policy) |
| `server.csp_policy` | `script-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'self'` | `--server-csp-policy`, `SERVER_CSP_POLICY` | The policy of `server.csp`: directives separated by semicolons. It is checked only for what keeps it one header: printable ASCII, no comma, directive names of letters, digits and dashes |
| `server.trusted_origins` | none | `--server-trusted-origins`, `SERVER_TRUSTED_ORIGINS` | Origins whose pages may send writes (POST, PUT, PATCH, DELETE) from another site, comma-separated, such as `https://admin.example.com,http://localhost:3000`. See [Cross-site writes](#cross-site-writes) |
| `server.cross_site_paths` | none | `--server-cross-site-paths`, `SERVER_CROSS_SITE_PATHS` | Path patterns whose writes are not checked for the site they come from, comma-separated, such as `POST /auth/apple/callback`. See [Cross-site writes](#cross-site-writes) |
| `ssr.vms` | the CPU count (`GOMAXPROCS`) | `--ssr-vms`, `SSR_VMS` | Prewarmed JavaScript VMs, which is how many pages render at once. In a container it follows the CPU limit. [Sizing a machine](performance.md#sizing-a-machine) says when to change it. Measured with every P rendering (one VM per P, `GOMAXPROCS=1` on 1 CPU), short requests waited about 20 ms behind the renders, because of Go's scheduler (its 10 ms monitor cycle, twice, see quickjs's [A long call and the Go scheduler](https://github.com/liustack/quickjs/blob/main/docs/architecture.md#a-long-call-and-the-go-scheduler)). With `GOMAXPROCS=2` on that 1 CPU they took 0.016 ms at P50. Keep `GOMAXPROCS` above `ssr.vms` when they must stay fast, though a spare P helps only when there is CPU left for it |
| `ssr.vm_memory` | `128MiB` | `--ssr-vm-memory`, `SSR_VM_MEMORY` | Memory limit of each VM. A render that runs out of memory fails, and the VM is replaced with a fresh one. Page code cannot catch it. The shapes the engine keeps for reuse count here too, up to about 6 MiB per VM at worst (2048 shapes of 32 keys of about ten characters, more with longer keys), so leave room for them when you lower it (see [Memory](performance.md#memory)) |
| `ssr.vm_stack_depth` | `10000` | `--ssr-vm-stack-depth`, `SSR_VM_STACK_DEPTH` | How deep JavaScript calls may nest in each VM, about one level per call. Deeper calls throw `RangeError: Maximum call stack size exceeded`, at about the depth Node does. From 1 to 10000, see [Stack depth](#stack-depth) |
| `ssr.timeout` | `3s` | `--ssr-timeout`, `SSR_TIMEOUT` | Time budget of one render. A render that runs longer is stopped and handled as an unexpected error, which goes to `OnError`. Page code cannot catch it |
| `ssr.queue_limit` | `32` | `--ssr-queue-limit`, `SSR_QUEUE_LIMIT` | Renders allowed to wait for a VM, excluding renders already running. `0` rejects immediately when no VM is idle. A full queue answers 503 with `Retry-After: 1` |
| `ssr.wait_timeout` | `3s` | `--ssr-wait-timeout`, `SSR_WAIT_TIMEOUT` | Maximum wait for a VM. Expiry answers 503 with `Retry-After: 1`, without starting or replacing a VM. Separate from `ssr.timeout`, which starts after a VM is acquired |
| `ssr.memo_size` | `64MiB` | `--ssr-memo-size`, `SSR_MEMO_SIZE` | Size of the render memo, counting the bytes it keeps: one form of each page, its gzip form with `server.compress` on and its HTML otherwise (see [Compression](build.md#compression)). When it is full the least recently used pages go. `0` turns it off, though requests that miss the same page at the same time still share one render. What the render memo is and why it is safe: [The render memo](memo.md). A page turns the render memo off for itself with `Memo = false` (see [SSR and cache](app.md#ssr-and-cache)) |
| `cache_control.s_maxage` | `1m` | `--cache-control-s-maxage`, `CACHE_CONTROL_S_MAXAGE` | The `s-maxage` of a page that writes `const Cache = true`: how long a shared cache in front of the program, such as a CDN, may keep it. At least one second. A page with `const Cache = 10 * time.Minute` uses its own time. See [Cache for CDNs](app.md#cache-for-cdns) |
| `session.cookie` | `__Host-tifa_session` | `--session-cookie`, `SESSION_COOKIE` | Name of the session cookie. Under `tifa dev` the `__Host-` prefix is dropped, see [Sessions](sessions.md) |
| `session.max_age` | `720h` | `--session-max-age`, `SESSION_MAX_AGE` | How long a session lasts from the moment `tifa.SetSession` makes it, at least one second. It does not renew |
| `session.memory_limit` | `64MiB` | `--session-memory-limit`, `SESSION_MEMORY_LIMIT` | Bytes the in-memory session store may hold, used when `OnStart` sets no store with `tifa.SetSessionStore`. A full store drops the sessions that expire first |

## Options of the app

An app defines its own options in `options.go` at its root, as package-level variables:

```go
// options.go
package shop

import (
	"time"

	"github.com/liustack/tifa"
)

var (
	DBURL   = tifa.String("db.url", "app.db", "Database file")
	Workers = tifa.Int("jobs.workers", 4, "Background workers")
	Hosts   = tifa.Strings("feed.hosts", nil, "Hosts the feed is read from")
	Open    = tifa.Bool("shop.open", false, "Let visitors in without signing in")
	Idle    = tifa.Duration("db.idle", time.Minute, "How long an idle connection stays open")
	Upload  = tifa.Size("upload.limit", 8<<20, "Largest upload")
	APIKey  = tifa.Secret("payments.api_key", "Key of the payment provider")
)
```

- The arguments are the key, the default and a line of help. Each returns a plain value: read `shop.DBURL`, and a test sets `shop.DBURL = "test.db"`.
- The value comes from the same sources as the settings of Tifa: the flag `--db-url`, the variable `DB_URL`, `.env`, the file of `-c`, then the default. `tifa.yaml` holds only settings of Tifa.
- The types: `String` (`string`), `Strings` (`[]string`), `Int` (`int`), `Bool` (`bool`), `Duration` (`time.Duration`) and `Size` (`int64` bytes). Their values are written as for the settings of Tifa, see [Value formats](#value-formats). `Strings` is comma-separated text, such as `FEED_HOSTS=a.example,b.example`.
- `Secret` (`string`) is for a password, a token or a key. It has no default and is empty when no source sets it. It comes from the environment variable, `.env` or the file of `-c`, and not from a flag: a flag shows in the process list to every user of the machine, so the program refuses one and says where to put the value. `./bin/app config` writes where it came from and leaves the value out, as the comment `# api_key: (hidden)`. An error about a secret, such as an unquoted number in the file, names the key and the source but not the value. Put a secret that starts with `*`, `!`, `&` or another YAML sign in quotes.
- Values are single values. Structured data goes into a file of the app, and an option holds its path.
- An invalid value gives the default, and the program lists it with the other invalid settings and options and stops before `OnStart`.
- In `go test` the options keep their defaults, whatever the flags, variables and `.env` are.
- The packages that the root package imports cannot read these variables (it would be an import cycle): `OnStart` passes the values to them, such as `store.Open(DBURL)`.
- An app that needs more can run its own configuration in `OnStart`.

`tifa build` fails, naming the file and line, when `tifa.String` and the like are called anywhere but as the value of a package-level `var` in `options.go` at the root, when a key is not a string literal or does not follow the rule of [Names](#names), and when two settings, of the app or of Tifa, would have the same flag or environment variable, such as `a.b_c` and `a_b.c`, and when one key is a section of another, such as `db.url` next to `db.url.timeout`, or `server.addr.port` next to `server.addr` of Tifa. The program panics on the same mistakes if they get past the build. Apps from before the file was renamed have `config.go`: the build also fails, naming it, when `tifa.String` and the like are still there, so rename it to `options.go`.

## Render queue

When every VM is busy, at most `ssr.queue_limit` renders wait for up to `ssr.wait_timeout`. The first limit reached rejects the render. The default is 32 waiting renders and a 3 second wait. `ssr.timeout` still limits only execution in a VM, to 3 seconds by default. A client cancel or pool shutdown also ends a wait. The worst case for one render is the two limits added: up to 3 seconds waiting for a VM, then up to 3 seconds running (`ssr.timeout`), 6 seconds in all for the SSR part, inside the 10 second `server.write_timeout`. `OnRequest`, `Load`, encoding, compression and writing the answer share the same write timeout, so leave room for them when you raise either value.

Overload answers use a small HTML page written by Go, status 503, `Retry-After: 1` and `Cache-Control: no-store`, without an ETag. They are not cached, rendered again or sent to `OnError`. `OnRequest` can handle the error returned by `next(c)`. A render of a 404 or an error page uses the same limits. An actual render failure or render timeout still goes to `OnError`.

App code can match a rejected render with `errors.Is(err, tifa.ErrSSRBusy)`, including wrapped errors. For example, record overload in `OnRequest`:

```go
import (
    "errors"
    "log/slog"

    "github.com/liustack/tifa"
)

func OnRequest(c *tifa.Ctx, next func(*tifa.Ctx) error) error {
    err := next(c)
    if errors.Is(err, tifa.ErrSSRBusy) {
        slog.WarnContext(c.Context(), "SSR queue busy", "path", c.Request().URL.Path)
    }
    return err
}
```

Returning the original error keeps the default 503 and its headers. Other render failures do not match `tifa.ErrSSRBusy`.

### Handling a rejected render

Use `tifa.ErrSSRBusy` when you want to do something of your own with an overload: count it, alert on it, or answer in another way than the default 503 page. If the default is fine, do nothing. The server already answers for you.

- The error comes out of `next(c)` in `OnRequest`. Its text is `tifa: render queue busy: queue is full` or `tifa: render queue busy: wait exceeded 3s`. Match it with `errors.Is`, not by text.
- It never reaches `OnError`. A render that fails, and a render that runs past `ssr.timeout`, are different errors: they do not match, and they go to `OnError`.
- A request whose own deadline comes before `ssr.wait_timeout` fails with that deadline, not with `tifa.ErrSSRBusy`. A client that goes away gives `context.Canceled`.
- Return the error unchanged to keep the default answer: 503, `Retry-After: 1` and `Cache-Control: no-store`. If you return a `tifa.Error` of your own instead, the answer uses your status and message, and the default headers are not added. Set the ones you want with `c.SetHeader`.

The test app `testdata/overloadapp` changes the answer and sets `Retry-After` itself:

```go
func OnRequest(c *tifa.Ctx, next func(*tifa.Ctx) error) error {
	err := next(c)
	if errors.Is(err, tifa.ErrSSRBusy) {
		c.SetHeader("Retry-After", "1")
		return tifa.Error{Status: 503, Message: "Please try again shortly"}
	}
	return err
}
```

To test it, call `OnRequest` with a `next` that returns the error. A wrapped error must match too:

```go
c, w := tifatest.NewCtx(t, httptest.NewRequest("GET", "/", nil))
got := OnRequest(c, func(*tifa.Ctx) error { return fmt.Errorf("render: %w", tifa.ErrSSRBusy) })
var public tifa.Error
if !errors.As(got, &public) || public.Status != 503 || w.Header().Get("Retry-After") != "1" {
	t.Fatalf("overload: %v, headers %v", got, w.Header())
}
```

`tifatest.NewCtx` does not run SSR, so the queue itself is never full in such a test. Pass the error in by hand as above.

The limits apply only when a render needs a VM. Memo hits, 304 responses, navigation data, API routes and pages with SSR off do not wait for one. Requests sharing a memo miss count as one render. `Load` runs before admission, and shared requests still hold their own HTTP state. A queue limit bounds render jobs, not total process memory or all HTTP requests. See [Overload](performance.md#overload) for sizing and measurements.

## Long requests

`server.read_timeout` and `server.write_timeout` give every request 10 seconds to arrive and 10 seconds to be answered, so a client that sends or reads very slowly cannot hold connections open. Keep them short, and lift them in the handlers that need longer: an upload from a phone, the export of a large file, a stream of server-sent events or of a model's answer. With [HTTPS](#https) on, the plain HTTP address `server.http_addr` answers under the same two limits.

```go
func POST(c *tifa.Ctx) error {
	rc := http.NewResponseController(c.Writer())
	// This request may take 5 minutes to send its body, and as long for the answer.
	if err := rc.SetReadDeadline(time.Now().Add(5 * time.Minute)); err != nil {
		return err
	}
	if err := rc.SetWriteDeadline(time.Now().Add(5 * time.Minute)); err != nil {
		return err
	}
	// ...
}
```

- The new time counts from the moment of the call and holds for this request only. `time.Time{}` removes the limit.
- Move a deadline before it passes, at the start of the handler. Over HTTP/2 a write deadline that passed has already ended the answer, and a read deadline that passed has cut off the body: a new one does not bring them back. The same code over HTTP/1.1 works, so the mistake only shows with HTTPS on.
- A stream that waits between writes, for events or for a language model, clears the write deadline while it waits and sets one around each write, so a client that stops reading does not keep the handler forever, see [Streaming](guides/streaming.md).
- The write timeout counts from the end of the request header, so a handler that reads a long body lifts both: its answer is written after the body.
- A handler with a typed input has its body read before it runs, so a deadline set in the handler comes too late for the body. Write `ReadTimeout` in its `route.go` instead, which lifts both deadlines before `OnRequest` runs, see [Body limit and read time of a route](app.md#body-limit-and-read-time-of-a-route) and [Uploads](guides/uploads.md#the-settings-and-the-body-limit).
- `c.Writer()` reaches the connection through the writers of Tifa and through compression.
- A proxy in front has timeouts of its own, such as the 60 seconds of nginx's `proxy_read_timeout`. Raise them for the same paths.

## HTTPS

HTTPS is off by default, and the program serves plain HTTP on `server.addr` as it always did. Leave it off behind a CDN or a proxy that ends TLS, such as Cloudflare, nginx or Caddy: they already speak HTTPS and HTTP/2 to browsers, and they would be the only ones to see a certificate of the program.

Turn it on when the program faces the internet by itself. Browsers then also get HTTP/2, which they speak only over TLS: every file of a page shares one connection, where over HTTP/1.1 a browser opens at most 6 to a host, see [Preloading the chunks of a page](build.md#preloading-the-chunks-of-a-page). There are two ways, and setting both fails. With either, set `server.addr`: with HTTPS on, the default `127.0.0.1:9527` refuses to start, since only this machine reaches it. Writing it out in the config is the same. When systemd passes the socket of `server.addr` ([socket activation](deploy.md#restart-without-dropping-requests)), the program listens on that socket and not on the address, so the address may stay the default. Another loopback address, such as `127.0.0.1:8443`, starts with a warning in the log, for a test on this machine.

### Certificates from Let's Encrypt

```yaml
server:
  addr: :443
  tls_domains: example.com,www.example.com
  tls_email: ops@example.com     # optional
  tls_cache: /var/lib/myapp/certs
```

- At start the program asks Let's Encrypt for a certificate of each domain, or takes it from `tls_cache`, and logs until when it is valid. It renews each one on its own before it runs out (30 days before, or with a third of its life left for shorter certificates), without a restart. Setting `tls_domains` means agreeing to the terms of the ACME server, for Let's Encrypt its [subscriber agreement](https://letsencrypt.org/repository/).
- DNS must point each domain to this machine, and the internet must reach it on port 443 or 80. Let's Encrypt checks that you control the domain over TLS on port 443 (`server.addr`) or over plain HTTP on port 80 (`server.http_addr`). The program tries the first, then the second.
- `tls_cache` must survive restarts and new deployments: in a container, mount a volume on it. It holds the account key and the certificates with their private keys. A folder the program creates gets mode 0700 and each file 0600. A folder that already exists keeps its mode, so give it to the program's user alone. Without it every start would ask for new certificates, and Let's Encrypt gives the same set of names at most 5 a week: the sixth start in a week would get none.
- Wildcards (`*.example.com`) and IP addresses are not supported, since they need another kind of check. Names are kept in lower case and ASCII, so `bücher.example` becomes `xn--bcher-kva.example`, and a final dot is dropped. A name that is not a valid host name, such as `a_b.example.com` or `-x.example.com`, fails.
- A name not in `tls_domains` gets no certificate: its TLS handshake fails, and Let's Encrypt is not asked.
- When getting a certificate fails, for example because DNS points elsewhere, the log says why, and a connection a minute later tries again. Let's Encrypt allows 5 failed checks an hour for a name.
- To try it without these limits, set `tls_ca` to `https://acme-staging-v02.api.letsencrypt.org/directory`, the staging server of Let's Encrypt. Browsers do not trust its certificates. Empty `tls_cache` when you switch back.

### Your own certificate

```yaml
server:
  addr: :443
  tls_cert: /etc/letsencrypt/live/example.com/fullchain.pem
  tls_key: /etc/letsencrypt/live/example.com/privkey.pem
```

- Both files are PEM. The certificate file holds the certificate followed by its intermediate certificates, as certbot's `fullchain.pem` does.
- Before each new connection the program looks at both files and loads them again when one changed, so a renewed certificate takes effect without a restart or a signal. Links are followed, so the `live/` folder of certbot and a Kubernetes secret mounted as a volume work. While the new files do not load, for example when the certificate is written but its key not yet, the program keeps the certificate it had and logs why once.
- Files that do not load at start stop the program before `OnStart`.
- When certbot renews with the http-01 challenge, it needs port 80, which the program holds for `server.http_addr`. Renew with a DNS challenge instead, or set `http_addr: off` and let certbot listen on port 80 itself in standalone mode.

### Port 80 and HSTS

- With HTTPS on, `server.http_addr` (`:80` by default) serves plain HTTP. It answers the checks of Let's Encrypt, and redirects every other request to the same URL over HTTPS with a 308, which keeps the method. It redirects only names HTTPS has a certificate for: those of `tls_domains`, or those in your own certificate as last loaded. Other hosts, such as the bare IP address of the machine, get 421. The redirect has no port, so it goes to 443 also when `server.addr` listens on another port that 443 is mapped to. Set it to `off` for none: with `tls_domains`, Let's Encrypt can then check only over TLS on port 443.
- `server.hsts: true` adds `Strict-Transport-Security: max-age=31536000` to every answer of `server.addr`. A browser that saw it goes over HTTPS for a year, even when a link says `http://`, and turning it off again does not reach that browser. Turn it on once HTTPS works. It leaves out `includeSubDomains` and `preload`. Browsers ignore it over plain HTTP, so the redirect does not carry it. Behind a proxy that ends TLS it works too, since the proxy passes it on.

### Ports below 1024

On Linux only root may listen on ports 80 and 443. Give the program that one right instead:

```sh
sudo setcap cap_net_bind_service=+ep ./bin/myapp
```

Copying or building the file again drops it, so do it after each deployment. Under systemd, `AmbientCapabilities=CAP_NET_BIND_SERVICE` in the unit does the same. In a container that does not run as root, listen on high ports, such as `server.addr: :8443` and `server.http_addr: :8080`, and publish 443 and 80 on them (`-p 443:8443 -p 80:8080`). Without the right the program does not start, and the error says what to do. On macOS any user may listen on them.

### What changes over HTTP/2

- Compression, the `.gz` files of the build, the render memo, ETags, redirects and server-sent events work as over HTTP/1.1, and a flush sends what was written at once.
- Deadlines run out over HTTP/2 even while the handler is not reading or writing: a write deadline that passes ends the answer, and a read deadline that passes makes the rest of the body fail to read. Moving either afterwards does not help. Over HTTP/1.1 a deadline that passed can still be moved before the next read or write. Move deadlines before they pass, see [Long requests](#long-requests).
- `Hijack` fails with `http.ErrNotSupported` on an HTTP/2 connection, since that connection carries other requests too. Unless `GODEBUG=http2xconnect=1` is set, the server does not offer WebSockets over HTTP/2, so browsers open them over a separate HTTP/1.1 connection, where `Hijack` works.
- Over HTTPS, read what compression may leak in [Compression](build.md#compression).

`tifa dev` always serves plain HTTP and ignores these settings, also when the project file sets them.

## Security headers

Every answer of the program carries three headers, pages, JSON, errors and public files alike. None of them changes what a page loads or how it works:

| Header | Value | What it does |
| --- | --- | --- |
| `X-Content-Type-Options` | `nosniff` | The browser takes the `Content-Type` as it is, so a file that is no script never runs as one |
| `Referrer-Policy` | `strict-origin-when-cross-origin` | A link to another site tells it the origin, not the path and query, which may hold a token. Current browsers do this anyway |
| `Content-Security-Policy` | `frame-ancestors 'self'` | Only pages of the same origin may show a page in a frame, so another site cannot show it under its own buttons |

They are set before `OnRequest`, so change or remove them there for the paths that need something else, such as pages a partner shows in a frame. Static files do not pass through `OnRequest` and always carry them.

```go
func OnRequest(c *tifa.Ctx, next func(*tifa.Ctx) error) error {
	if strings.HasPrefix(c.Request().URL.Path, "/embed/") {
		c.SetHeader("Content-Security-Policy", "frame-ancestors https://partner.example")
	}
	return next(c)
}
```

`Strict-Transport-Security` is not among them: it is `server.hsts`, see [Port 80 and HSTS](#port-80-and-hsts). A proxy in front passes the headers on, so do not add them there again.

### Content-Security-Policy

A Content-Security-Policy tells the browser which scripts may run. With one that allows only the files of the site, a script that gets into a page through data, such as a comment that carries `<script>`, does not run. Turn it on with `server.csp: on`, or try it first with `server.csp: report-only`, which only reports what it would block in the browser console (and to the address of `report-uri` or `report-to`, when the policy has one).

The default policy, `server.csp_policy`, is `script-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'self'`:

- scripts load only from the site itself, and an inline script runs only when the policy lists its hash
- no plugins (`<object>`, `<embed>`), and no `<base>` that sends relative URLs elsewhere
- only pages of the same origin show the pages in a frame

Styles are left alone: React writes `style` attributes, and emotion and MUI add style elements while the page runs.

Every page has inline scripts of Tifa's own: the router hands the data of the Loads to the browser in one and restores the scroll position with another. Tifa hashes each inline script and style it writes into a page (SHA-256) and adds the hashes to the policy in the header of that page, so they run. These are the scripts and styles of the head of the build (Vite's preamble under `tifa dev`), of the session head, of the router, and of the `scripts` and `styles` of `head` functions, and under `tifa dev` its own error overlay. The same page always gets the same header, so the render memo and ETags work as before. Other answers, JSON and files, carry the policy as it is.

The hashes go into `script-src` and `script-src-elem`, and into `style-src` and `style-src-elem` when the policy has them. A policy with only `default-src` gets a `script-src` with the sources of `default-src` and the hashes. A directive whose `'unsafe-inline'` still lets every inline script run gets no hashes, since browsers ignore `'unsafe-inline'` once a directive lists one. With a hash, a nonce or, for scripts, `'strict-dynamic'` in the directive, browsers already ignore it, so the hashes go in.

Leave `'strict-dynamic'` out of the script directives. Browsers then ignore `'self'` and host sources, so they block the script files of the pages (the entry of the build and its preloads), and pages do not hydrate.

A policy of your own, with an analytics host and a report address:

```yaml
server:
  csp: on
  csp_policy: "script-src 'self' https://plausible.io; object-src 'none'; base-uri 'none'; frame-ancestors 'self'; report-uri /csp-reports"
```

A policy that `OnRequest`, a `Load` or a `Layout` sets on an answer goes out as they set it, without hashes. The 304 of a page carries no policy: the browser keeps the one that came with its copy, which has the hashes of that copy.

An inline script of `head` that first appears after a navigation in the browser, and one of a page with `SSR = false`, is not in the policy of the document that is open, so the browser blocks it.

## Cross-site writes

A program refuses a POST, PUT, PATCH or DELETE that a browser sends from another site. A page of `evil.test` can make a browser send a form or a multipart body to your app with the user's cookies, and a handler that trusts the cookie would act for the user. The check runs first, before `OnRequest` and before any route, for pages, API routes and URLs that match nothing. It is on for every app, and no setting turns it off. It is Go's `http.CrossOriginProtection`:

- A request with `Sec-Fetch-Site: same-origin` passes, and so does `none` (the user typed the address). `same-site`, which is another subdomain of the same domain, and `cross-site` are refused.
- A browser that sends no `Sec-Fetch-Site` (all current ones do) is judged by its `Origin`, which must name the `Host` of the request.
- A request with neither header does not come from a browser, such as curl, a webhook or another server, and passes.
- GET, HEAD and OPTIONS are never checked, so a GET must not change anything.

A refused request gets 403 and `{"error":"cross-origin request detected from Sec-Fetch-Site header"}` (or the same with `Sec-Fetch-Site is missing, and Origin does not match Host`), with `Cache-Control: no-store`. `OnRequest` does not run for it, so a log or a counter there does not see it. The server logs one line for it with the method, the path, the reason and the `Origin`, `Sec-Fetch-Site` and `Host` it read, which tells a proxy that rewrites `Host` from a real attack. Calls from your own pages, with the functions of `route.gen` or `fetch`, and `router.invalidate()` are same-origin and unaffected. So is `tifa dev`, where the browser talks to one address.

Behind a proxy, let `Host` and `Origin` through as the browser sent them. A modern browser is judged by `Sec-Fetch-Site`, which proxies pass on, but an old one is judged by `Origin` against `Host`, so a proxy that rewrites `Host` makes its writes look foreign.

To allow some:

- `server.trusted_origins` lists origins whose pages may write, such as a separate admin site or a front end on `http://localhost:3000` during development. An origin is a scheme, a host and, if it is not the default of the scheme, a port, with no path: `https://admin.example.com`. It is compared as text with the `Origin` header, so `http://` and `https://` are different origins. Write it as a browser sends it: the program lowers ASCII letters and drops `:80` and `:443`, and refuses any other way of writing the host, naming what to write. That is a domain name in ASCII, its letters outside ASCII written as `xn--` (`https://xn--bcher-kva.example` for `bücher.example`), an IPv4 address as four decimal numbers, an IPv6 address in its shortest form, and a port without leading zeros. Trust only sites you control, since a page of a trusted origin can act for your users.
- `server.cross_site_paths` lists patterns, in the syntax of `http.ServeMux`, such as `POST /auth/apple/callback` or `/hooks/{name}`, whose requests are not checked at all. Use it for a provider's browser callback, such as a sign-in that posts a form from the provider's page. A pattern without a method takes every method. A request that ServeMux would only redirect to the pattern, such as one with a final slash, is not let through. A handler on such a path has to prove the request is real itself, for example with the `state` it put in the cookie or the signature of the provider.

## Stack depth

With the default `ssr.vm_stack_depth` of 10000, plain recursion goes as deep in a render as in Node, which stops at about 10,400 calls. Each level takes room on the stack of the Go goroutine that renders and on the engine's own stack, not in the memory `ssr.vm_memory` limits. A normal page needs a few hundred levels and a few MB. Code that recurses without end goes all the way down before it throws, and the process grows by about 14 MB on darwin/arm64 and up to about 26 MB on linux/amd64 for that render. The render fails, the VM is replaced, and Go takes the goroutine stack back at the next garbage collection. The goroutine stack grows by doubling, and when a garbage collection is running as it grows, the old stack is freed only once the collection ends.

Every VM can do this at the same moment, so the worst case is `ssr.vms` times that on top of the memory of the VMs. On a small machine, lower the depth. How much the process grew in one render that recurses without end, measured on darwin/arm64 and linux/amd64:

| `ssr.vm_stack_depth` | Growth of the process |
| --- | --- |
| `10000` | 14 MB on darwin/arm64, up to 26 MB on linux/amd64 |
| `5000` | 7 MB |
| `2000` | 2 to 4 MB |
| `1000` | 1 to 2 MB |

When the page catches the `RangeError` and the render succeeds, the VM stays. The pages of its engine stack that the recursion touched go back to the system when the VM has been idle for a second, as the other free memory of an idle VM does (see [Memory](performance.md#memory)).

What a lower depth breaks:

- Plain recursive code that goes deeper than the limit, such as a function walking a tree of comments or categories, in your code or in a library. It throws the `RangeError`, and the render fails unless the code catches it.
- `JSON.parse` of data nested deeper than the limit, the data Go hands to the render included. Node has no such limit.
- Not React: when a deep component tree hits the limit, React renders the rest of that tree from a fresh stack. Only far below 1000 do pages fail, with odd errors from libraries that ran out of stack halfway through their work, and with a few dozen the bundle fails to load when the program starts. Every page of the example shop still renders at 200.

`2000` keeps such pages working and holds a render that recurses without end to about 2 MB.

## Value formats

- Sizes are whole numbers with an optional unit: `B`, `KiB`, `MiB` or `GiB`, such as `512KiB`. A plain number is bytes. Only `ssr.memo_size` may be 0.
- Durations use Go's format and must be positive: `500ms`, `3s`, `1m30s`. A `tifa.Duration` of the app may also be 0 or negative.
- In a YAML file a whole number setting is a number (`vms: 2`, not `"2"`), a `true` or `false` setting a bool, and every other setting text. A size may be a number of bytes or text with a unit.
- `ssr.vms` is a whole number of at least 1.
- `ssr.queue_limit` is a whole number of at least 0. `ssr.wait_timeout` must be positive even with a queue limit of 0.
- `ssr.vm_stack_depth` is a whole number from 1 to 10000, the deepest the engine lets calls nest.
- `build.skip_tanstack_check`, `build.debug_info`, `build.i18next_cache`, `server.compress` and `server.hsts` are `true` or `false`, without quotes in a file. The flag alone is `true` and takes another value only after `=`: `--server-compress=false`. It never takes the next word.
- `server.trusted_origins` and `server.cross_site_paths` are comma-separated lists, without brackets or quotes inside, and may be empty. A higher source can empty what a lower one set: `SERVER_TRUSTED_ORIGINS=` clears the origins of the project file. An origin needs `http` or `https`, a host and no path, query or user name, and a bad one fails with what is wrong. A pattern that `http.ServeMux` rejects, or that conflicts with another of the list, fails too.
- `server.tls_domains` is a comma-separated list of host names, without scheme, port or path. Each needs a dot. `server.tls_ca` is an `https` URL. `server.http_addr` is an address with a port, such as `:80` or `0.0.0.0:80`, or `off` in lower case. Words that read as a switch, such as `OFF`, `false` or `none`, fail with what to write.
- `server.csp` is `off`, `on` or `report-only`, in lower case. `true`, `false`, `yes` and the like fail with what to write. `server.csp_policy` may not be empty: to send no policy, set `server.csp` to `off`.
- `server.tls_domains`, `server.tls_email`, `server.tls_cache`, `server.tls_cert` and `server.tls_key` may be empty, which turns off what a lower source set: `SERVER_TLS_DOMAINS=` with `--server-tls-cert` serves your own certificate where the project file asks for Let's Encrypt.

## Errors

`tifa build` fails on a value of the wrong type or format in `tifa.yaml`, naming its line. A setting written as a section or a list, such as `ssr: {vms: {count: 2}}`, is a value of the wrong type too. The program refuses to start and lists every problem, each with its source and key, when:

- a value of a setting of Tifa or of the app has the wrong type or format, such as `ssr.timeout: 3`, `SHOP_OPEN=maybe` or a section under `ssr.vms` in the file of `-c`,
- a secret of the app (`tifa.Secret`) is given as a flag,
- the file of `-c` cannot be read or is not YAML, or a line of `.env` cannot be read.

A flag that is no setting stops the program with `unknown flag`. A key or variable that is no setting is not an error, so a misspelt one such as `SERVER_PORT` changes nothing: `./bin/app config` shows what each setting ended up as. `build.*` settings are read only from `tifa.yaml`: a deployment file, a `BUILD_*` variable or a `--build-*` flag does not change them.

It also refuses to start, before `OnStart`, and names the settings when:

- HTTPS is on and `server.addr` is the default `127.0.0.1:9527`, left out or written out, and systemd passed no socket for it,
- the HTTPS settings do not go together once all sources are merged: `tls_domains` with `tls_cert`, `tls_cert` without `tls_key` or the other way round, `tls_domains` without `tls_cache`, or `tls_email` or `tls_cache` without `tls_domains`. The project file alone may leave out what the deployment adds, so `tifa build` does not check this,
- the files of `tls_cert` and `tls_key` do not load, or `tls_cache` cannot be created.

When `server.addr` or `server.http_addr` cannot be listened on, the error names the setting, and for a port only root may take it says what to do.

`serve --dist <folder>` and `serve --lifeline-fd <fd>` are flags for [`tifa dev`](dev.md) only. They are not settings and have no environment variable. The program `tifa build` makes has no `--lifeline-fd`.
