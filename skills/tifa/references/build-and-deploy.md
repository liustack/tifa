# Build, configure and deploy

Contents: `tifa dev`, `tifa build`, go.mod and package setup, settings, building for Linux, running the program, HTTPS, running behind a CDN or proxy, sizing, license notices.

Full docs: [dev.md](https://github.com/liustack/tifa/blob/main/docs/dev.md), [build.md](https://github.com/liustack/tifa/blob/main/docs/build.md), [config.md](https://github.com/liustack/tifa/blob/main/docs/config.md), [performance.md](https://github.com/liustack/tifa/blob/main/docs/performance.md).

## `tifa dev`

```sh
go tool tifa dev [--dir app-root] [--addr 127.0.0.1:9527]
```

- The first build compiles QuickJS and SQLite, so it takes several seconds up to a minute. Later builds use Go's build cache. A page opened during the first build waits 2 seconds, then shows "building" and reloads on its own.
- What an edit triggers: `.ts`, `.tsx`, `.css`, `.json` update open pages in place through Vite (React Fast Refresh) and rebuild the SSR bundle without a restart. `.go`, `go.mod` and `go:embed` files rebuild the Go program and reload the page. A new or removed `page.tsx`, `layout.tsx` or `layout_.tsx`, or a changed `tifa.yaml`, runs every step. A changed `.env` only restarts the app.
- A failed build keeps the old version running and shows the original compiler or Vite error as an overlay. Fix it and the overlay goes away.
- If the app exits at start or does not listen within 30 seconds, the overlay says `The app did not start` with the last stderr lines. An `OnStart` error and a panic are there.
- It does not watch `node_modules`, `.git`, `.tifa`, `bin/`, or `schema.sql` of a `tifa new` app (restart it after changing the schema). It serves plain HTTP and ignores the HTTPS settings.
- `go tool tifa errors` prints the errors since the last build: a failed build or an app that did not start, unexpected request errors with their stack in your sources, errors the open pages had in the browser and pages whose two renders differ. Run it after a change and after you open or reload the pages you changed, since browser errors come only from open pages. It waits for a build that runs, exits with 1 when there are errors and 2 when no `tifa dev` runs, and `--json` prints JSON (see [Tifa errors](https://github.com/liustack/tifa/blob/main/docs/dev.md#tifa-errors)).
- After you install a different TanStack Router, React or React DOM, restart `tifa dev`.
- Stop it with Ctrl-C or SIGTERM. If you started it in the background, kill only that PID.

## `tifa build`

```sh
go tool tifa build [--dir app-root] [-o output-path]
```

1. Analyzes `app/` and `hooks.go`. A broken convention prints `file:line: reason` and stops.
2. Writes generated code into `.tifa/`.
3. Bundles with the app's own Vite on Node into `.tifa/dist/`. An app without pages builds without Node.
4. `CGO_ENABLED=0 go build` into `bin/<last segment of the module path>`, with `.tifa/dist/` embedded and debug info left out (`-ldflags=-w`).

The output is one executable. It needs no Node, no `node_modules` and no files next to it. Keep `build.debug_info: true` in `tifa.yaml` only if you need Delve.

Everything under `.tifa/` is generated. Do not edit it, and keep it out of git (`tifa new` writes a `.gitignore` for it).

### Setting up an existing app

- `go.mod` has `tool github.com/liustack/tifa/cmd/tifa` (`go get -tool github.com/liustack/tifa/cmd/tifa`), so `go tool tifa` uses the version in `go.mod`. `tifa build` does not change `go.mod` or `go.sum`. After upgrading Tifa run `go mod tidy` once.
- `go.mod` also has the line `ignore node_modules`, so `go test ./...` and `go vet ./...` skip Go files inside npm packages (`go mod edit -ignore=node_modules` adds it to an older app).
- `tsconfig.json` extends `./.tifa/tsconfig.json` and sets `paths` for `@/*` and `tifa/client`. `vite.config.ts` uses `plugins: [tifa(), react()]` with `tifa` from `./.tifa/vite.js`. Copy both from `tifa new`. Add other Vite plugins after these two.
- `package.json` needs `react`, `react-dom`, `@tanstack/react-router` and, as dev dependencies, `vite`, `@vitejs/plugin-react`, `typescript`, `@types/react`, `@types/react-dom`. React and React DOM must be 19.3.0 or later. `@tanstack/react-router` must be the version `tifa new` writes. The build stops with an error that names the versions otherwise. To move to a newer TanStack Router, upgrade Tifa.
- `tifa new` picks pnpm when it is on PATH. Use pnpm and `pnpm dlx` in new commands if the app has a `pnpm-lock.yaml`.

## Settings

Order of precedence, highest first: command-line flags of the program, environment variables, the `.env` file of the folder the program runs in, a deployment file passed with `-c path.yaml`, the `tifa.yaml` in the app root (its values are built into the program), the defaults. A key `server.body_limit` is the flag `--server-body-limit` and the variable `SERVER_BODY_LIMIT`, with no prefix. `PORT` and `HOST` of hosting platforms set `server.addr` below `SERVER_ADDR`, and `PORT` alone listens on every interface.

```yaml
# tifa.yaml
server:
  addr: 0.0.0.0:9527
  body_limit: 4MiB
ssr:
  timeout: 2s
cache:
  size: 128MiB
```

Common settings (full table in config.md):

| Key | Default | Flag and env | Notes |
| --- | --- | --- | --- |
| `server.addr` | `127.0.0.1:9527` | `--server-addr`, `SERVER_ADDR` | Use `0.0.0.0:9527` in a container |
| `server.body_limit` | `1MiB` | `--server-body-limit`, `SERVER_BODY_LIMIT` | Larger body gets 413. `BodyLimit` in a `route.go` overrides it for that route |
| `server.compress` | `true` | `--server-compress=false`, `SERVER_COMPRESS` | Set false when a proxy or CDN compresses |
| `server.shutdown_timeout` | `10s` | `SERVER_SHUTDOWN_TIMEOUT` | Time for in-flight requests after SIGTERM, then for `OnShutdown` |
| `ssr.vms` | CPU count | `--ssr-vms`, `SSR_VMS` | How many pages render at once. Leave it at the default |
| `ssr.vm_memory` | `128MiB` | `SSR_VM_MEMORY` | Limit per VM |
| `ssr.timeout` | `3s` | `SSR_TIMEOUT` | One render. Over it is an unexpected error |
| `ssr.queue_limit`, `ssr.wait_timeout` | `32`, `1s` | `SSR_QUEUE_LIMIT`, `SSR_WAIT_TIMEOUT` | Renders allowed to wait for a VM. Beyond that, 503 with `Retry-After: 1` |
| `cache.size` | `64MiB` | `--cache-size`, `CACHE_SIZE` | One size for the render cache and the pages of the whole-page cache (`Cache`), 0 turns both off |
| `cache.whole_page` | `true` | `--cache-whole-page`, `CACHE_WHOLE_PAGE` | `false` answers the pages with `Cache` like other pages, to measure or debug them. The render cache stays on |
| `cache.ignore_query` | `utm_*,gclid,fbclid` and the like | `CACHE_IGNORE_QUERY` | Tracking parameters the whole-page cache leaves out of its key. `name` adds, `-name` removes, a trailing `*` is a prefix. A parameter a `Load` reads is always kept |
| `session.cookie`, `session.max_age`, `session.memory_limit` | `__Host-tifa_session`, `720h`, `64MiB` | `SESSION_COOKIE`, `SESSION_MAX_AGE`, `SESSION_MEMORY_LIMIT` | Name and lifetime of the session cookie, and the memory of the default session store |
| `build.target` | `es2020` | project file only | Browser bundle target, such as `es2019,chrome90,safari14` |
| `build.public` | `public` | project file only | Folder of static files served from `/` |

Sizes are whole numbers with `B`, `KiB`, `MiB` or `GiB`. Durations use Go's format (`500ms`, `3s`). A bad value stops the program before `OnStart`, listing every one. A flag that is no setting stops the program. Keys and variables that are no setting are left alone, so a misspelt name there changes nothing. `build.*` keys are read only from `tifa.yaml`. `./bin/app config` prints every setting with its value and its source, as a commented file that is a good start for a deployment file.

The app defines its own options in `options.go` at the root, as package-level variables, and reads them as plain values:

```go
var DBURL = tifa.String("db.url", "app.db", "Database file") // --db-url, DB_URL
```

`tifa.Strings` (comma-separated), `tifa.Int`, `tifa.Bool`, `tifa.Duration` and `tifa.Size` work the same way. They come from the same sources (not `tifa.yaml`), keep their defaults in `go test`, and may only be written there: `tifa build` reports any other place, a key that is no string literal and names that clash. Packages the root package imports get the values from `OnStart`. `./bin/app version` prints versions.

Cache headers: a page with `const Cache = <duration>` in its `route.go` sends `Cache-Control: public, max-age=0, s-maxage=<Cache in seconds>, stale-if-error=86400`, `Age` and `Cache-Status` on the answers it keeps (GET and HEAD, status 200, 301, 302, 308). Every other answer that goes through routing (pages, 304s, 404 pages, navigation data, method handlers, redirects) is sent with `Cache-Control: private, no-cache` unless your code sets `Cache-Control` itself. JSON errors, the plain error page from Go, the page with an error and the busy 503 are sent with `no-store`. Files under `/assets/` are content-hashed with `Cache-Control: public, max-age=31536000, immutable`. Files from `public/` get `no-cache` and an ETag. The render cache keeps the render inside the process only and does not change the header.

## Building for Linux and running it

The server render engine runs on linux and darwin, on amd64 and arm64, and windows/amd64. Build on your machine for the server:

```sh
GOOS=linux GOARCH=amd64 $(go tool -n tifa) build -o bin/myapp-linux
scp bin/myapp-linux server:/srv/myapp/myapp
```

`GOOS=linux go tool tifa build` does not work, because `GOOS` then also applies to building the Tifa command itself.

On the server:

```sh
SERVER_ADDR=0.0.0.0:9527 ./myapp
```

- Run it from the folder it keeps data in. A relative path such as the `app.db` of the `tifa new` template (or the setting `db.path`, `DB_PATH`) is resolved against the working directory. Back up or mount that folder.
- SIGINT and SIGTERM stop it gracefully: new connections stop, in-flight requests finish within `server.shutdown_timeout`, then `OnShutdown` runs. A systemd unit or a container needs nothing more than the executable, the environment and a writable data folder.
- `FROM scratch` containers work: the program embeds the time zone database.
- Ports below 1024 on Linux need `sudo setcap cap_net_bind_service=+ep ./myapp` after each copy, or `AmbientCapabilities=CAP_NET_BIND_SERVICE` in a systemd unit, or high ports published by the container (`-p 443:8443`).

## HTTPS

Off by default. Leave it off behind a CDN or a proxy that ends TLS. Turn it on when the program faces the internet:

```yaml
server:
  addr: :443
  tls_domains: example.com,www.example.com
  tls_email: ops@example.com   # optional
  tls_cache: /var/lib/myapp/certs
```

- `tls_cache` must survive restarts and deployments (mount a volume). Without it every start asks for new certificates and Let's Encrypt limits that to 5 a week per set of names.
- DNS must point at the machine, and port 443 or 80 must be reachable. Port 80 (`server.http_addr`) redirects to HTTPS. Wildcards and IP addresses are not supported.
- Your own certificate instead: `tls_cert` and `tls_key` (PEM, reloaded when they change). Setting both ways fails.
- With HTTPS on, `server.addr` must be set (the default loopback address refuses to start). HTTP/2 comes with it, which loads pages with many files faster than HTTP/1.1.
- `server.hsts: true` adds `Strict-Transport-Security` once HTTPS works.

## Behind a CDN or proxy

- Leave HTTPS off in the program and let the CDN end TLS. Listen on `0.0.0.0:<port>`.
- Do not rewrite `Host` or `Origin`. CloudFront sends the origin's domain as `Host` unless an origin request policy such as `AllViewer` forwards the viewer's. Cloudflare keeps the host unless an Enterprise Origin Rule overrides it. The cross-site write check compares them, and a rewritten `Host` makes your own POST requests look foreign (403 with `cross-origin request detected`). Modern browsers are judged by `Sec-Fetch-Site`, which proxies pass on.
- Compression: the program compresses text answers with gzip or zstd itself. If the CDN or proxy compresses (brotli, for example), set `server.compress: false`. A proxy leaves an answer that already has a `Content-Encoding` alone.
- Caching: pages and navigation data are `private, no-cache` by default, so a CDN does not store them. Keep it that way, apart from the pages that opt in with `Cache`. The navigation data of a page is the page URL plus the query parameter `_tifa`, so even a cache that keys by URL alone keeps the two apart, and it is always `private, no-cache`. Let the CDN cache `/assets/` (hashed, cached for a year), the files in `public/` and the pages with `Cache`. Do not set `Cache-Control: public` yourself on other pages or on handlers that serve the same URL as a page. `/__tifa/session` answers per visitor and is `private, no-store`: do not cache it either.
- Pages with `Cache`: they are anonymous, so the CDN may keep one answer for all visitors, and the HTML request needs no cookie. Tell the CDN to follow the origin's `Cache-Control`: it takes `s-maxage` (the whole time of the page) and subtracts `Age` (the seconds since Tifa made it), so Tifa and the CDN together keep a page no longer than `Cache`, and Tifa answers a revalidation with a 304 from its own entry. The navigation data of the same path (the request with `?_tifa=`) is different: it needs the session cookie to carry the session of the browser, and it must not be shared. So the cache key must include the query string (at least `_tifa`), or the CDN must bypass the cache for requests with `_tifa`. Drop the cookies only by path for the HTML of the page, never for the same path with `_tifa`. CloudFront: not the managed policy `CachingOptimized` for these pages, since its minimum TTL of 1 second overrides the headers. Make a cache policy with a minimum TTL of 0, no cookies and the query string (at least `_tifa`) in the key. The managed `UseOriginCacheControlHeaders` has a minimum TTL of 0 but puts all cookies in the key. Cloudflare: HTML is not cached by default, so add one Cache Rule "Eligible for cache" for these paths, with Edge TTL set to "Use cache-control header if present, bypass cache if not", never "Ignore cache-control header and use this TTL". Its docs do not say how `s-maxage=N` combines with `max-age=0`, so check that a second request is a hit. Google Cloud CDN: cache mode `USE_ORIGIN_HEADERS` (the default `CACHE_ALL_STATIC` is also safe), not `FORCE_CACHE_ALL`, and no query string list that leaves out `_tifa`. Azure Front Door: caching is off until you turn it on in the route. Use the Rules Engine behavior "Honor origin" (not "Override always"), and the query string behavior "Use Query String". It never caches `private`, `no-cache` or `no-store`, supports `Last-Modified` but not ETag, and caches 1 to 3 days when no `Cache-Control` is sent. `stale-if-error` helps a CDN only when Tifa itself is down, because Tifa serves its own old page when it is up. CloudFront and Cloudflare honor it, Google Cloud CDN's docs say it has no effect. After a deploy, clear the CDN's pages, or a visitor can get an old page whose scripts are gone (see "Pages open while you deploy" in docs/app.md).
- Do not make the CDN ignore the origin's cache headers. Use a cache policy that follows them for the HTML. CloudFront: not `CachingOptimized` for pages (its minimum TTL of 1 second overrides `private` and `no-store`), use a policy with a minimum TTL of 0, or no caching for everything except `/assets/*` and the `public/` paths. Cloudflare: no Cache Rule that sets "Ignore cache-control header and use this TTL" for pages. Google Cloud CDN: not `FORCE_CACHE_ALL`, use `USE_ORIGIN_HEADERS`. Settings and sources: docs/deploy.md, section "CDN settings".
- A page with per-user data is already safe by default. Do not override `Cache-Control` on it with a public value.
- After a write that changes what pages with `Cache` show, call `tifa.ExpirePages(paths...)` with the path of every such page (the item, its lists, the home page, each language), decoded as `c.Request().URL.Path` has it. It expires every query string, host and scheme of each path in the memory of this program only: not in a CDN, and not in other copies of the program. A page made from data read before the call is not kept. See "Making a page again after a write" in docs/app.md.
- Cross-site writes: `server.trusted_origins` (comma-separated origins such as `https://admin.example.com`) lets chosen sites send POST, PUT, PATCH and DELETE. `server.cross_site_paths` (patterns such as `POST /auth/apple/callback`) skips the check for a provider callback, and that handler must prove the request is real itself.
- Over HTTPS, compression can leak a secret through the length of an answer (BREACH) when an answer repeats something from the request and also carries a secret such as a CSRF token. Set the header `No-Gzip-Compression` on such an answer, or turn `server.compress` off.

## Sizing

- CPU: about 73 page renders a second per physical core for workbench-style pages, about 34 for heavier storefront pages. A cloud vCPU is often one hyperthread, so plan on about 0.63 of that per vCPU. Leave `ssr.vms` at the default (the CPU count, which follows a container's CPU limit). More VMs than CPUs only slows each render.
- Memory: about 45 MiB for the process plus 12 to 16 MiB per VM. Heavier pages need up to about 21 MiB per VM.
- Only a full page load renders. Browser navigation fetches JSON that Go writes without a VM. API routes, render cache hits, pages answered by `Cache` and 304 answers do not render either. A hit of `Cache` runs no `Load` either.
- Overload answers 503 with `Retry-After: 1` from Go, without a VM and without calling `OnError`. `errors.Is(err, tifa.ErrSSRBusy)` matches it in `OnRequest`.
- `tifa.Stats()` returns the VMs loaded, rendering and waited for, the renders, failures, render time and 503 refusals, and the hits, misses and entries of both caches. Tifa has no health or metrics route: write a `GET /healthz` that answers 204, and a `GET /metrics` that writes the stats in the Prometheus text format. Code and what to read in the numbers: docs/deploy.md, section "Health checks and metrics".
- Code that recurses without end can add up to about 26 MB per VM for a moment. On a small machine lower `ssr.vm_stack_depth` (2000 keeps normal pages working).

## Notices to ship with the program

The executable embeds locale and time zone data and other code under licenses that ask for their notices to go with every copy you give to others. For your own server, nothing is needed. When you hand the program out, follow [Notices to ship with the program](https://github.com/liustack/tifa/blob/main/docs/build.md#notices-to-ship-with-the-program).
