# Build, configure and deploy

Contents: tifa dev, tifa build, go.mod and package setup, settings, building for Linux, running the program, HTTPS, running behind a CDN or proxy, sizing, license notices.

Full docs: [dev.md](https://github.com/liustack/tifa/blob/main/docs/dev.md), [build.md](https://github.com/liustack/tifa/blob/main/docs/build.md), [config.md](https://github.com/liustack/tifa/blob/main/docs/config.md), [performance.md](https://github.com/liustack/tifa/blob/main/docs/performance.md).

## tifa dev

```sh
go tool tifa dev [--dir app-root] [--addr 127.0.0.1:9527]
```

- The first build compiles QuickJS and SQLite, so it takes several seconds up to a minute. Later builds use Go's build cache. A page opened during the first build waits 2 seconds, then shows "building" and reloads on its own.
- What an edit triggers: `.ts`, `.tsx`, `.css`, `.json` update open pages in place through Vite (React Fast Refresh) and rebuild the SSR bundle without a restart. `.go`, `go.mod` and `go:embed` files rebuild the Go program and reload the page. A new or removed `page.tsx`, `layout.tsx` or `layout_.tsx`, or a changed `tifa.yaml`, runs every step.
- A failed build keeps the old version running and shows the original compiler or Vite error as an overlay. Fix it and the overlay goes away.
- If the app exits at start or does not listen within 30 seconds, the overlay says `The app did not start` with the last stderr lines. An `OnStart` error and a panic are there.
- It does not watch `node_modules`, `.git`, `.tifa`, `bin/`, or `schema.sql` of a `tifa new` app (restart it after changing the schema). It serves plain HTTP and ignores the HTTPS settings.
- After you install a different TanStack Router, React or React DOM, restart `tifa dev`.
- Stop it with Ctrl-C or SIGTERM. If you started it in the background, kill only that PID.

## tifa build

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

- `go.mod` has `tool github.com/liustack/tifa/cmd/tifa` (`go get -tool github.com/liustack/tifa/cmd/tifa`), so `go tool tifa` uses the version in `go.mod`. `tifa build` does not change `go.mod` or `go.sum`. After upgrading tifa run `go mod tidy` once.
- `go.mod` also has the line `ignore node_modules`, so `go test ./...` and `go vet ./...` skip Go files inside npm packages (`go mod edit -ignore=node_modules` adds it to an older app).
- `tsconfig.json` extends `./.tifa/tsconfig.json` and sets `paths` for `@/*` and `tifa/client`. `vite.config.ts` uses `plugins: [tifa(), react()]` with `tifa` from `./.tifa/vite.js`. Copy both from `tifa new`. Add other Vite plugins after these two.
- `package.json` needs `react`, `react-dom`, `@tanstack/react-router` and, as dev dependencies, `vite`, `@vitejs/plugin-react`, `typescript`, `@types/react`, `@types/react-dom`. React and React DOM must be 19.3.0 or later. `@tanstack/react-router` must be the version `tifa new` writes. The build stops with an error that names the versions otherwise. To move to a newer TanStack Router, upgrade tifa.
- `tifa new` picks pnpm when it is on PATH. Use pnpm and `pnpm dlx` in new commands if the app has a `pnpm-lock.yaml`.

## Settings

Order of precedence, highest first: command-line flags of the program, `TIFA_*` environment variables, a deployment file passed with `--config path.yaml`, the `tifa.yaml` (or `tifa.json`) in the app root (embedded at build time), the defaults. Keep one project file.

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
| `server.addr` | `127.0.0.1:9527` | `--server-addr`, `TIFA_SERVER_ADDR` | Use `0.0.0.0:9527` in a container |
| `server.body_limit` | `1MiB` | `--server-body-limit`, `TIFA_SERVER_BODY_LIMIT` | Larger body gets 413 |
| `server.compress` | `true` | `--server-compress=false`, `TIFA_SERVER_COMPRESS` | Set false when a proxy or CDN compresses |
| `server.shutdown_timeout` | `10s` | `TIFA_SERVER_SHUTDOWN_TIMEOUT` | Time for in-flight requests after SIGTERM, then for `OnShutdown` |
| `ssr.vms` | CPU count | `--ssr-vms`, `TIFA_SSR_VMS` | How many pages render at once. Leave it at the default |
| `ssr.vm_memory` | `128MiB` | `TIFA_SSR_VM_MEMORY` | Limit per VM |
| `ssr.timeout` | `3s` | `TIFA_SSR_TIMEOUT` | One render. Over it is an unexpected error |
| `ssr.queue_limit`, `ssr.wait_timeout` | `32`, `1s` | `TIFA_SSR_QUEUE_LIMIT`, `TIFA_SSR_WAIT_TIMEOUT` | Renders allowed to wait for a VM. Beyond that, 503 with `Retry-After: 1` |
| `cache.size` | `64MiB` | `--cache-size`, `TIFA_CACHE_SIZE` | HTML cache of rendered pages, 0 turns it off |
| `build.target` | `es2020` | project file only | Browser bundle target, such as `es2019,chrome90,safari14` |
| `build.public` | `public` | project file only | Folder of static files served from `/` |

Sizes are whole numbers with `B`, `KiB`, `MiB` or `GiB`. Durations use Go's format (`500ms`, `3s`). An unknown key or a `TIFA_*` variable stops the program, and a deployment file cannot set `build.*` keys. `./bin/app config` prints the merged settings as a commented file, a good start for a deployment file. `./bin/app version` prints versions.

Cache headers: every answer that goes through routing (pages, 304s, 404 pages, navigation data, method handlers, redirects) is sent with `Cache-Control: private, no-cache` unless your code sets `Cache-Control` itself. JSON errors, the plain error page from Go, the page with an error and the busy 503 are sent with `no-store`. Files under `/assets/` are content-hashed with `Cache-Control: public, max-age=31536000, immutable`. Files from `public/` get `no-cache` and an ETag. The render cache keeps the render inside the process only and does not change the header.

## Building for Linux and running it

The server render engine runs on linux/amd64 and darwin/arm64 only. Build on your machine for the server:

```sh
GOOS=linux GOARCH=amd64 $(go tool -n tifa) build -o bin/myapp-linux
scp bin/myapp-linux server:/srv/myapp/myapp
```

`GOOS=linux go tool tifa build` does not work, because `GOOS` then also applies to building the tifa command itself.

On the server:

```sh
TIFA_SERVER_ADDR=0.0.0.0:9527 ./myapp
```

- Run it from the folder it keeps data in. A relative path such as the `app.db` of the `tifa new` template (or `$DB_PATH`) is resolved against the working directory. Back up or mount that folder.
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
- Do not rewrite `Host` or `Origin`. The cross-site write check compares them, and a rewritten `Host` makes your own POST requests look foreign (403 with `cross-origin request detected`). Modern browsers are judged by `Sec-Fetch-Site`, which proxies pass on.
- Compression: the program compresses text answers with gzip or zstd itself. If the CDN or proxy compresses (brotli, for example), set `server.compress: false`. A proxy leaves an answer that already has a `Content-Encoding` alone.
- Caching: pages and navigation data are `private, no-cache` by default, so a CDN does not store them. Keep it that way. The navigation data of a page is the page URL plus the query parameter `_tifa`, so even a cache that keys by URL alone keeps the two apart, but the answers are per visitor all the same. Let the CDN cache only `/assets/` (hashed, cached for a year) and the files in `public/`. Do not set `Cache-Control: public` on pages or on handlers that serve the same URL as a page. `/__tifa/session` answers per visitor and is `private, no-store`: do not cache it either.
- Do not make the CDN ignore the origin's cache headers. Use a cache policy that follows them for the HTML. CloudFront: not `CachingOptimized` for pages (its minimum TTL of 1 second overrides `private` and `no-store`), use a policy with a minimum TTL of 0, or no caching for everything except `/assets/*` and the `public/` paths. Cloudflare: no Edge TTL rule or cache rule that ignores origin headers for pages. Google Cloud CDN: not `FORCE_CACHE_ALL`, use `USE_ORIGIN_HEADERS`.
- A page with per-user data is already safe by default. Do not override `Cache-Control` on it with a public value.
- Cross-site writes: `server.trusted_origins` (comma-separated origins such as `https://admin.example.com`) lets chosen sites send POST, PUT, PATCH and DELETE. `server.cross_site_paths` (patterns such as `POST /auth/apple/callback`) skips the check for a provider callback, and that handler must prove the request is real itself.
- Over HTTPS, compression can leak a secret through the length of an answer (BREACH) when an answer repeats something from the request and also carries a secret such as a CSRF token. Set the header `No-Gzip-Compression` on such an answer, or turn `server.compress` off.

## Sizing

- CPU: about 73 page renders a second per physical core for workbench-style pages, about 34 for heavier storefront pages. A cloud vCPU is often one hyperthread, so plan on about 0.63 of that per vCPU. Leave `ssr.vms` at the default (the CPU count, which follows a container's CPU limit). More VMs than CPUs only slows each render.
- Memory: about 45 MiB for the process plus 12 to 16 MiB per VM. Heavier pages need up to about 21 MiB per VM.
- Only a full page load renders. Browser navigation fetches JSON that Go writes without a VM. API routes, render cache hits and 304 answers do not render either.
- Overload answers 503 with `Retry-After: 1` from Go, without a VM and without calling `OnError`. `errors.Is(err, tifa.ErrSSRBusy)` matches it in `OnRequest`.
- Code that recurses without end can add up to about 26 MB per VM for a moment. On a small machine lower `ssr.vm_stack_depth` (2000 keeps normal pages working).

## Notices to ship with the program

The executable embeds locale and time zone data and other code under licenses that ask for their notices to go with every copy you give to others. For your own server, nothing is needed. When you hand the program out, follow [Notices to ship with the program](https://github.com/liustack/tifa/blob/main/docs/build.md#notices-to-ship-with-the-program).
