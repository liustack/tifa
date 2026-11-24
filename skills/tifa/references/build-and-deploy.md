# Build, configure and deploy

Contents: `tifa dev`, `tifa build`, go.mod and package setup, settings, search engines and language models, `tifa deploy`, building for Linux, running the program, HTTPS, running behind a CDN or proxy, sizing, license notices.

Full docs (in the folder `go tool tifa docs` prints): `docs/build-and-deploy/dev.md`, `docs/build-and-deploy/build.md`, `docs/build-and-deploy/config.md`, `docs/reference/performance.md`.

## `tifa dev`

```sh
go tool tifa dev [--dir app-root] [--addr 127.0.0.1:9527] [--background]
```

A terminal that cannot stay open, such as yours as an agent, needs `go tool tifa dev --background`. It starts the server detached and returns once the build ended and `GET /` answers, printing the address and PID. Then `go tool tifa dev status` (`--json`, exit 1 when none runs) says where it runs and how the last build went, `go tool tifa dev logs` (`-n 100`, `-f`) prints `.tifa/dev.log`, which holds all output, and `go tool tifa dev stop` stops it, also when none runs. Stop it when you finish. A second `tifa dev` in the same app fails and names the one that runs. The first `tifa dev` or `tifa build` on a machine downloads Go and npm packages and compiles Tifa, which can take a few minutes: give the command time before you decide it hangs.

- The first build compiles beastjs and SQLite, so it takes several seconds up to a minute. Later builds use Go's build cache. A page opened during the first build waits 2 seconds, then shows "building" and reloads on its own.
- What an edit triggers: `.ts`, `.tsx`, `.css`, `.json` update open pages in place through Vite (React Fast Refresh) and rebuild the SSR bundle without a restart. `.go`, `go.mod` and `go:embed` files rebuild the Go program and reload the page. A new or removed `page.tsx`, `layout.tsx` or `layout_.tsx`, or a changed `tifa.yaml`, runs every step. A changed `.env` only restarts the app.
- A failed build keeps the old version running and shows the original compiler or Vite error as an overlay. Fix it and the overlay goes away.
- If the app exits at start or does not listen within 30 seconds, the overlay says `The app did not start` with the last stderr lines. An `OnStart` error and a panic are there.
- It does not watch `node_modules`, `.git`, `.tifa`, `bin/`, or `schema.sql` of a `tifa new` app (restart it after changing the schema). It serves plain HTTP and ignores the HTTPS settings.
- `go tool tifa errors` prints the errors since the last build: a failed build or an app that did not start, unexpected request errors with their stack in your sources, errors the open pages had in the browser and pages whose two renders differ. Run it after a change and after you open or reload the pages you changed, since browser errors come only from open pages. It waits for a build that runs, exits with 1 when there are errors and 2 when no `tifa dev` runs, and `--json` prints JSON (see `docs/build-and-deploy/dev.md#tifa-errors`).
- After you install a different TanStack Router, React or React DOM, restart `tifa dev`.
- Stop it with Ctrl-C or SIGTERM, or with `go tool tifa dev stop` when it runs in the background.

## `tifa build`

```sh
go tool tifa build [--dir app-root] [-o output-path]
```

1. Analyzes `app/` and `hooks.go`. A broken convention prints `file:line: reason` and stops.
2. Writes generated code into `.tifa/`.
3. Bundles with the app's own Vite on Node into `.tifa/dist/`. An app without pages builds without Node.
4. `CGO_ENABLED=0 go build` into `bin/<last segment of the module path>` (not counting a major version suffix such as `/v2`, and with `.exe` added when building for Windows), with `.tifa/dist/` embedded and debug info left out (`-ldflags=-w`).

The output is one executable. It needs no Node, no `node_modules` and no files next to it. Keep `build.debug_info: true` in `tifa.yaml` only if you need Delve.

Everything under `.tifa/` is generated. Do not edit it, and keep it out of git (`tifa new` writes a `.gitignore` for it).

### Setting up an existing app

- `go.mod` has `tool github.com/liustack/tifa/cmd/tifa` (`go get -tool github.com/liustack/tifa/cmd/tifa`), so `go tool tifa` uses the version in `go.mod`. `tifa build` does not change `go.mod` or `go.sum`. After upgrading Tifa run `go mod tidy` once.
- `go.mod` also has the line `ignore node_modules`, so `go test ./...` and `go vet ./...` skip Go files inside npm packages (`go mod edit -ignore=node_modules` adds it to an older app).
- `tsconfig.json` extends `./.tifa/tsconfig.json` and sets `paths` for `@/*` and `tifa/*` (`./.tifa/tifa/*`, which holds `tifa/client` and `tifa/i18n`). `vite.config.ts` uses `plugins: [tifa(), react()]` with `tifa` from `./.tifa/vite.js`. Copy both from `tifa new`. Add other Vite plugins after these two.
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
| `server.body_limit` | `1MiB` | `--server-body-limit`, `SERVER_BODY_LIMIT` | Larger body gets 413. `c.SetBodyLimit` in `OnRequest` overrides it for one request |
| `server.compress` | `true` | `--server-compress=false`, `SERVER_COMPRESS` | Set false when a proxy or CDN compresses |
| `server.shutdown_timeout` | `10s` | `SERVER_SHUTDOWN_TIMEOUT` | Time for in-flight requests after SIGTERM, then for `OnShutdown` |
| `ssr.vms` | CPU count, at most 4 | `--ssr-vms`, `SSR_VMS` | How many pages render at once. Leave it at the default |
| `ssr.vm_memory` | `128MiB` | `SSR_VM_MEMORY` | Limit per VM |
| `ssr.timeout` | `3s` | `SSR_TIMEOUT` | One render. Over it is an unexpected error |
| `ssr.queue_limit`, `ssr.wait_timeout` | `32`, `3s` | `SSR_QUEUE_LIMIT`, `SSR_WAIT_TIMEOUT` | Renders allowed to wait for a VM. Beyond that, 503 with `Retry-After: 1` |
| `ssr.memo_size` | `64MiB` | `--ssr-memo-size`, `SSR_MEMO_SIZE` | Size of the render memo, 0 turns it off |
| `cache.s_maxage` | `1m` | `--cache-s-maxage`, `CACHE_S_MAXAGE` | The `s-maxage` of a page that writes `const Cache = true`. A duration in `Cache` sets its own time |
| `session.cookie`, `session.max_age`, `session.memory_limit` | `__Host-tifa_session`, `720h`, `64MiB` | `SESSION_COOKIE`, `SESSION_MAX_AGE`, `SESSION_MEMORY_LIMIT` | Name and lifetime of the session cookie, and the memory of the default session store |
| `build.target` | `es2020` | project file only | Browser bundle target, such as `es2019,chrome90,safari14` |
| `build.public` | `public` | project file only | Folder of static files that need a fixed name, served from `/` with `no-cache`. Import images and fonts instead (docs/start/app.md, "Static files") |

Sizes are whole numbers with `B`, `KiB`, `MiB` or `GiB`. Durations use Go's format (`500ms`, `3s`). A bad value stops the program before `OnStart`, listing every one. A flag that is no setting stops the program. Variables and keys of the file of `-c` that are no setting are left alone, so a misspelt name there changes nothing. A key in `tifa.yaml` that is no setting of Tifa fails the build, and the options of the app never go in that file. `build.*` keys are read only from `tifa.yaml`. `./bin/app config` prints every setting with its value and its source, as a commented file that is a good start for a deployment file.

The app defines its own options in `options.go` at the root, as package-level variables, and reads them as plain values:

```go
var DBURL = tifa.String("db.url", "app.db", "Database file") // --db-url, DB_URL
```

`tifa.Strings` (comma-separated), `tifa.Int`, `tifa.Bool`, `tifa.Duration` and `tifa.Size` work the same way. They come from the same sources (not `tifa.yaml`), keep their defaults in `go test`, and may only be written there: `tifa build` reports any other place, a key that is no string literal and names that clash. Packages the root package imports get the values from `OnStart`. `./bin/app version` prints versions.

Cache headers: a page with `const Cache = true` or `const Cache = <duration>` in its `route.go` sends `Cache-Control: public, max-age=0, s-maxage=<seconds>, stale-if-error=86400` on its HTML, unless `OnRequest`, a `Load` or a `Layout` made the answer private. Its requests are anonymous and its `Load`s run on every request. Every other answer that goes through routing (pages, 304s, 404 pages, navigation data, method handlers, redirects) is sent with `Cache-Control: private, no-cache` unless your code sets `Cache-Control` itself. JSON errors, the plain error page from Go, the page with an error and the busy 503 are sent with `no-store`. Files under `/assets/` are content-hashed with `Cache-Control: public, max-age=31536000, immutable`. Files from `public/` get `no-cache` and an ETag. The render memo keeps the render inside the process only and does not change the header.

## Search engines and language models

`site.url` in `tifa.yaml` (scheme and host, no path) marks the site as public. With it every page gets a canonical link and an `og:url` meta with the same URL, and the program serves `/sitemap.xml`, a default `/robots.txt` (`Allow: /` and a `Sitemap:` line, unless `public/robots.txt` exists), `/llms.txt`, `/llms-full.txt` and a Markdown version at `<path>.md` of each page that shows a content entry. Without `site.url` there is none of this. A file in `public/` or a route at the same path wins.

The pages without parameters are in the sitemap on their own. Pages with parameters need `tifa.SetSitemap` in `OnStart`, otherwise the program logs one line at start naming the routes it left out:

```go
func OnStart(ctx context.Context) error {
	tifa.SetSitemap(func(c *tifa.Ctx) (tifa.Sitemap, error) {
		sm := tifa.Sitemap{Name: "Acme Blog", Summary: "Notes on Go web apps.", Exclude: []string{"/account", "/login"}}
		for _, p := range Blog.All() {
			sm.Pages = append(sm.Pages, tifa.SitemapPage{
				Language: p.Language, Path: "/blog/" + p.Slug, Modified: p.Date,
				Title: p.Title, Description: p.Description, Content: p,
			})
		}
		return sm, nil
	})
	return nil
}
```

- The function runs on each request for those files, so keep it fast. `OnRequest` runs for them too.
- `Path` is written like the `Path` of `tifa.Alternate`: escaped, without the language prefix or query. `Exclude` takes route patterns and leaves out the pages below them too. `Content` is an entry read from a collection, and gives the page its `.md` version.
- `Alternates` lists the versions of a page in each language when they differ from the default (same path in every language of its folder).
- A mistake in what the function returns never fails the request: a route with parameters that is neither listed nor excluded (a blog with no posts yet), a `Path` that matches no route or has a language prefix, a language the page does not have, or wrong `Alternates` leave that page out. A page listed twice keeps the first, `Exclude` wins over `Pages`, a stale `Exclude` pattern is ignored and a `Content` that is no collection entry is dropped. The files still answer, and each request logs one `ERROR` line per kind of mistake naming them. Only an error the function itself returns makes the request answer 500 through `OnError`. Test it with `tifatest.Start` and a GET of `/sitemap.xml`, and look at the log.

See `docs/guides/seo.md`.

## `tifa deploy`

Puts the app on a Linux server with systemd over ssh, several apps per server. `tifa.yaml` says what you want, and `tifa deploy setup` makes the server, Cloudflare and GitHub match:

```yaml
# tifa.yaml
site:
  url: https://shop.example.com
deploy:
  front: none      # none | cloudflare: with cloudflare, visitors arrive through a Cloudflare Tunnel at the host of site.url, and deploy and rollback clear the CDN cache of it
  connect: direct  # direct | cloudflare | tailscale: how deploys reach the server over ssh. cloudflare covers CI and this machine through the tunnel (needs front: cloudflare, no auto block needed). tailscale is for CI: the runner joins your tailnet, your machine does not change, needs the auto block, and setup takes the client ID of a Tailscale identity (not secret, `--tailscale-client-id`)
  auto:            # the block turns deploys from GitHub Actions on (auto: {} takes the defaults). No block: you deploy by hand only
    branch: main   # the branch that deploys. Left out: the default branch of the repository on GitHub
    needs: ci.yml  # the workflow file in .github/workflows that must pass first, with a name: line. Left out: ci.yml. none: do not wait
```

```sh
go tool tifa deploy setup      # prints a plan (add, change, remove, a count of what is right, and what you must do), asks, applies. --yes skips the question, --all lists what is right. Exits non-zero when something is left for you. Run it again after changing tifa.yaml
go tool tifa deploy            # build for the server, upload, restart, check /healthz, clear the CDN cache
go tool tifa deploy rollback   # back to the release before
go tool tifa deploy logs       # the journal of the app on the server, -n 500 for more lines, -f to follow until Ctrl-C
go tool tifa deploy status     # the release that runs and its commit, the releases kept, since when the service runs, GET /healthz now, memory
go tool tifa deploy remove     # take the app off the server and take back what tifa made for it in Cloudflare and GitHub. Prints a plan, then asks for the name of the app on a terminal. Without a terminal it needs --yes
```

- Tifa runs the system `ssh`, so `~/.ssh/config`, a Cloudflare Tunnel `ProxyCommand` and Tailscale names work. The server is not in `tifa.yaml`, which is committed: `--host`, else `TIFA_DEPLOY_HOST` in the environment, else `TIFA_DEPLOY_HOST=<ssh alias or ssh://host[:port]>` in the `.env` of the app (git ignores it), with no user in it, else the servers `tifa deploy setup` remembered in `~/.tifa/config.yaml` (no secrets in it, `TIFA_HOME` moves the folder). With exactly one server there, or with the folder of the app listed under one, deploy uses it and prints one line saying which (an app is its folder, so another checkout of the same name counts as new). The host is checked against the machine ID in the first probe. In GitHub Actions the file is never read. With several and none named it stops and lists them: write the line `TIFA_DEPLOY_HOST=<host>` in the `.env` of the app, or pass `--host`. Setup records the server (and `remove` forgets the app), so a second app on the same machine needs no host. In CI, `TIFA_DEPLOY_HOST` and the host keys are secrets, so a public repository shows nothing about the server. Every input of tifa deploy (`TIFA_DEPLOY_HOST`, `TIFA_DEPLOY_CF_PURGE_TOKEN`, `TIFA_DEPLOY_CF_ZONE_ID`, `TIFA_DEPLOY_GITHUB_TOKEN`) is read the same way: the flag if it has one, else the environment, else `.env` at the root of the app. Tifa only reads that file and never writes a token into it.
- Setup makes the user `tifa` (the first time as root), then `/srv/tifa/<app>/` (`<app>` is the last part of the module path) with an empty `shared/.env` and the data folder `shared/` the app runs in, a free port from 9527 on `127.0.0.1`, and the user units `tifa-<app>.socket` and `.service`. On a prepared server, `tifa deploy` makes the app part by itself, as `tifa`. Fill `.env` on the server: deploys never upload it.
- With `deploy.front: cloudflare` (the domain is the host of `site.url`, no port) and cf (the Cloudflare CLI) logged in (`cf auth login`), setup puts the app behind the Cloudflare Tunnel of the server: tunnel, cloudflared on the server, route, proxied CNAME, and rules for that host name only. Setting `front: none` shows the removal of those in the plan. Without cf the plan lists it among what you must do: `needs cf logged in`.
- With the block `deploy.auto` and gh logged in (or, without gh, a fine-grained token limited to the repository with Secrets and Variables set to Read and write, in `TIFA_DEPLOY_GITHUB_TOKEN` of `.env`), setup writes `.github/workflows/tifa-deploy.yml` (on a push to `auto.branch` once the workflow of `auto.needs` passed, `ci.yml` of `tifa new` by default, `needs: none` to wait for nothing. Setup stops with an error when that file is missing, has no `name:` or does not run on a push to the branch. GitHub starts the workflow from the file on the default branch only, so merge it there), makes a key for the repository, and sets the secrets. With `front: cloudflare`, setup asks you to paste a token that can only clear the cache of the zone, from a prefilled Cloudflare form, and keeps it with the zone ID on the server in `/srv/tifa/.config/tifa/cloudflare-purge` (only `tifa` can read it). Every `tifa deploy` and rollback, from CI or any machine, reads it there over ssh and clears the cache. The token is optional: without it a page with `Cache` can stay old on the CDN for up to its `Cache` time after a deploy. Setup asks on the first setup of the domain or of CI, and not again once the server has a token for the zone. `tifa deploy setup --cache-purge-token` adds or replaces it later. Never tell the user to run gh for it. `TIFA_DEPLOY_CF_PURGE_TOKEN` and `TIFA_DEPLOY_CF_ZONE_ID` in the environment or `.env` override the server. Taking the block `deploy.auto` out takes them away. `deploy.connect: tailscale` needs the block `deploy.auto`, and `cloudflare` needs `front: cloudflare`. With `tailscale`, the workflow joins the tailnet with a Tailscale identity (workload identity federation) that the user makes in the Tailscale admin console: setup prints the steps and the subject to use, and takes its client ID (not secret) from a question on a terminal or from `go tool tifa deploy setup --yes --tailscale-client-id <ID>`, which an agent may pass. Tailscale must be installed and up on the server, and `TIFA_DEPLOY_HOST` must be an address the runner reaches on the tailnet, such as its MagicDNS name. The user sets Tailscale up: setup installs and changes nothing of it. See `docs/build-and-deploy/deploy.md#deploy-from-github-actions`.
- `go tool tifa deploy env set NAME` (value on a terminal's hidden prompt, or piped on stdin by a script, never an argument), `env unset NAME` and `env list` (names only) edit `/srv/tifa/<app>/shared/.env` on the server, restart the app and check it, and put the old file back when the check fails.
- A secret never goes through you. Run `go tool tifa deploy env set NAME` with nothing on its stdin, or `go tool tifa deploy setup --yes --cache-purge-token` when setup lists that token as left to do: tifa opens a page on `http://127.0.0.1:<port>/<code>` in the user's browser and prints that URL on stderr. Give the user the URL and say what to type there, then wait: the command goes on once they send it, or stops after 10 minutes. Never ask for the secret in chat and never pipe it into the command yourself. See `docs/build-and-deploy/deploy.md#secrets-without-a-terminal`.
- A deploy builds on this machine (never on the server), uploads to `releases/<id>/`, switches `current`, restarts through the socket (no request is refused) and asks `GET /healthz` on the server for up to 30 seconds. Below 500 passes, so an app without the route passes with 404. Otherwise it prints the log of the app, starts the release before again and exits with 1. Write a `/healthz` that checks the database.
- `deploy.connect: cloudflare` puts your own machine behind the tunnel too, so the server needs no open ssh port (Tifa never changes the firewall: the person closes it after a deploy through the tunnel worked). With cf logged in, setup adds the email of the cf login to the Access application it made for the ssh host name (a second person's setup adds theirs), installs cloudflared when missing (brew, winget, else it says where to get it, and `--yes` applies it), writes a marked block at the top of `~/.ssh/config`, checks `ssh tifa@<ssh host>`, and then sets `TIFA_DEPLOY_HOST` in `.env` to the ssh host (when the value came from the environment or `--host`, it only says what to set once the check passed). The first ssh opens the Cloudflare Access login in the browser and waits up to 4 minutes: it is the human's one step, so tell the user to finish it there and read the address from the stderr of setup when no browser opened. `tifa deploy remove` deletes the policy and the block. See `docs/build-and-deploy/deploy.md#connect-cloudflare`.
- The log of the app: `go tool tifa deploy logs`, which reads it over ssh as `tifa`. Never tell the user to log in to the server for it. Records: `/srv/tifa/<app>/deploys.log`, which `go tool tifa deploy status` reads. The last 5 releases stay.
- `go tool tifa deploy remove` deletes `/srv/tifa/<app>` with `shared/.env` and the data in `shared/`, so ask the user before you run it with `--yes`. It removes only what Tifa made, the tunnel and cloudflared only with the last app of the server, and keeps `.github/workflows/tifa-deploy.yml`, which fails each push until the user deletes it. Without cf or gh logged in it removes the rest, lists what is left and exits non-zero: run it again after the login. See `docs/build-and-deploy/deploy.md#logs-status-and-remove`.

See `docs/build-and-deploy/deploy.md#deploy-with-tifa-deploy`.

## Building for Linux and running it

The server render engine runs on linux and darwin, on amd64 and arm64, and windows on amd64 and arm64. Build on your machine for the server:

```sh
GOOS=linux GOARCH=amd64 $(go tool -n tifa) build -o bin/myapp-linux
scp bin/myapp-linux server:/srv/myapp/myapp
```

`GOOS=linux go tool tifa build` does not work, because `GOOS` then also applies to building the Tifa command itself. Build on your machine or in CI and copy the binary. Do not build on a 1 GB VPS: the build needs more memory than the running app.

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
- Pages with `Cache`: they are anonymous, so the CDN may keep one answer for all visitors, and the HTML request needs no cookie. Tell the CDN to follow the origin's `Cache-Control`: it takes `s-maxage` (the whole time of the page) and subtracts `Age` (the seconds since Tifa made it), so Tifa and the CDN together keep a page no longer than `Cache`, and Tifa answers a revalidation with a 304 from its own entry. The navigation data of the same path (the request with `?_tifa=`) is different: it needs the session cookie to carry the session of the browser, and it must not be shared. So the cache key must include the query string (at least `_tifa`), or the CDN must bypass the cache for requests with `_tifa`. Drop the cookies only by path for the HTML of the page, never for the same path with `_tifa`. CloudFront: not the managed policy `CachingOptimized` for these pages, since its minimum TTL of 1 second overrides the headers. Make a cache policy with a minimum TTL of 0, no cookies and the query string (at least `_tifa`) in the key. The managed `UseOriginCacheControlHeaders` has a minimum TTL of 0 but puts all cookies in the key. Cloudflare: HTML is not cached by default, so add one Cache Rule "Eligible for cache" for these paths, with Edge TTL set to "Use cache-control header if present, bypass cache if not", never "Ignore cache-control header and use this TTL". Its docs do not say how `s-maxage=N` combines with `max-age=0`, so check that a second request is a hit. Google Cloud CDN: cache mode `USE_ORIGIN_HEADERS` (the default `CACHE_ALL_STATIC` is also safe), not `FORCE_CACHE_ALL`, and no query string list that leaves out `_tifa`. Azure Front Door: caching is off until you turn it on in the route. Use the Rules Engine behavior "Honor origin" (not "Override always"), and the query string behavior "Use Query String". It never caches `private`, `no-cache` or `no-store`, supports `Last-Modified` but not ETag, and caches 1 to 3 days when no `Cache-Control` is sent. `stale-if-error` helps a CDN only when Tifa itself is down, because Tifa serves its own old page when it is up. CloudFront and Cloudflare honor it, Google Cloud CDN's docs say it has no effect. After a deploy, clear the CDN's pages, or a visitor can get an old page whose scripts are gone (see "Pages open while you deploy" in docs/start/app.md).
- Do not make the CDN ignore the origin's cache headers. Use a cache policy that follows them for the HTML. CloudFront: not `CachingOptimized` for pages (its minimum TTL of 1 second overrides `private` and `no-store`), use a policy with a minimum TTL of 0, or no caching for everything except `/assets/*` and the `public/` paths. Cloudflare: no Cache Rule that sets "Ignore cache-control header and use this TTL" for pages, and Browser TTL "Respect origin TTL" in the rule, or the zone's Browser Cache TTL (4 hours by default) makes browsers keep pages with `Cache` and public files that long. Google Cloud CDN: not `FORCE_CACHE_ALL`, use `USE_ORIGIN_HEADERS`. Settings and sources: docs/build-and-deploy/deploy.md, section "CDN settings".
- A page with per-user data is already safe by default. Do not override `Cache-Control` on it with a public value.
- After a write that changes what pages with `Cache` show, purge those pages in the CDN. Tifa keeps no copy of them, so there is nothing to expire in the program.
- Cross-site writes: `server.trusted_origins` (comma-separated origins such as `https://admin.example.com`) lets chosen sites send POST, PUT, PATCH and DELETE. `server.cross_site_paths` (patterns such as `POST /auth/apple/callback`) skips the check for a provider callback, and that handler must prove the request is real itself.
- Over HTTPS, compression can leak a secret through the length of an answer (BREACH) when an answer repeats something from the request and also carries a secret such as a CSRF token. Set the header `No-Gzip-Compression` on such an answer, or turn `server.compress` off.

## Sizing

- CPU: about 73 page renders a second per physical core for workbench-style pages, about 34 for heavier storefront pages. A cloud vCPU is often one hyperthread, so plan on about 0.63 of that per vCPU. Leave `ssr.vms` at the default (the CPU count, which follows a container's CPU limit). More VMs than CPUs only slows each render.
- Memory: about 45 MiB for the process plus 12 to 16 MiB per VM. Heavier pages need up to about 21 MiB per VM.
- Only a full page load renders. Browser navigation fetches JSON that Go writes without a VM. API routes, render memo hits, and 304 answers do not render either.
- Overload answers 503 with `Retry-After: 1` from Go, without a VM and without calling `OnError`. `errors.Is(err, tifa.ErrSSRBusy)` matches it in `OnRequest`.
- `tifa.Stats()` returns the VMs loaded, rendering and waited for, the renders, failures, render time and 503 refusals, and the hits, misses and entries of the render memo. Tifa has no health or metrics route: write a `GET /healthz` that answers 204, and a `GET /metrics` that writes the stats in the Prometheus text format. Code and what to read in the numbers: docs/build-and-deploy/deploy.md, section "Health checks and metrics".
- Code that recurses without end can add up to about 26 MB per VM for a moment. On a small machine lower `ssr.vm_stack_depth` (2000 keeps normal pages working).

## Notices to ship with the program

The executable embeds locale and time zone data and other code under licenses that ask for their notices to go with every copy you give to others. For your own server, nothing is needed. When you hand the program out, follow Notices to ship with the program (`docs/build-and-deploy/build.md#notices-to-ship-with-the-program`).
