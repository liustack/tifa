---
summary: 'How to put a built Tifa program on a server: building for Linux, running it with its settings and data folder, stopping it cleanly, restarting it under systemd without refusing a connection and without signing users out, containers, ports and HTTPS, what to set when a CDN or proxy stands in front (Host and Origin, compression, which answers to cache), a health check route, metrics for Prometheus from tifa.Stats and what to read in them, and what happens to open pages when you deploy'
read_when:
  - Putting a Tifa app on a VPS or in a container for the first time
  - Putting a CDN or a reverse proxy such as nginx, Cloudflare or CloudFront in front of a Tifa app
  - A POST gets 403 with `cross-origin request detected` only behind the proxy
  - Pages show old content or a missing script after a deploy
  - Deciding what a systemd unit or a Dockerfile for a Tifa app needs
  - Visitors get refused connections, or are signed out, each time you deploy
  - Adding a health check for a load balancer, a container or Kubernetes
  - Watching the VM pool and the render memo with Prometheus or another monitoring system, or deciding from them whether the machine needs more CPU or a larger `ssr.memo_size`
  - Finding which routes miss the render memo, or render again for nothing
---

English · [简体中文](deploy.zh-CN.md)

# Deploying

A Tifa app is one executable. The server needs that file, the settings in its environment, and a folder it may write data to. It needs no Node, no process manager and no Redis. The default session store lives in memory, so for more than one copy of the program give it a [store of your own](sessions.md#where-to-store-sessions).

## Build for the server

The server render engine runs on linux and darwin, on amd64 and arm64, and windows/amd64. Build on your machine for the server with the target set, and copy the file over:

```sh
GOOS=linux GOARCH=amd64 $(go tool -n tifa) build -o bin/myapp-linux
scp bin/myapp-linux server:/srv/myapp/myapp
```

`GOOS=linux go tool tifa build` does not work, and [Building for another system](build.md#building-for-another-system) says why. The program holds code under several licenses, so when you hand it to others, ship the notices with it (see [Notices to ship with the program](build.md#notices-to-ship-with-the-program)).

## Run it

```sh
SERVER_ADDR=0.0.0.0:9527 ./myapp
```

- Every setting has a flag and an environment variable, and the program also reads a `.env` file of its working folder and a file given with `-c`. [Configuration](config.md) has the list and which source wins. `./myapp config` prints every setting with its value and where it came from, and `./myapp version` prints the versions.
- Platforms that tell the program its port in `PORT`, such as Cloud Run or Fly, work without more settings (see [PORT and HOST](config.md#port-and-host)).
- Run it from the folder it keeps data in. A relative path such as the `app.db` of the `tifa new` template (the setting `db.path`) is resolved against the working directory. Back that folder up, or mount it in a container.
- SIGINT and SIGTERM stop it gracefully: new connections stop, requests in flight get `server.shutdown_timeout` to finish, then `OnShutdown` runs. A stream with no end of its own, such as server-sent events, returns at once when it watches `tifa.ShuttingDown` (see [Streaming](guides/streaming.md#when-the-program-stops)). A systemd unit or a container needs nothing more than the executable, the environment and a writable data folder.
- A `FROM scratch` container works. The program holds the time zone data its renders need.
- Size the machine with [Performance](performance.md#sizing-a-machine). `ssr.vms` follows the CPU limit of a container.

## Restart without dropping requests

A restart stops the program, and the next one prewarms its VMs before it listens. A connection that arrives in between is refused, unless systemd holds the socket: with socket activation, systemd listens on the port and hands the socket to the program, and keeps it open while the program restarts. The system takes the connections that arrive meanwhile, and the next program answers them. A visitor only waits a little longer: on an M4 Mac, the program of the example shop answers its first page 0.42 seconds after it starts with 10 VMs, and 0.27 seconds with `ssr.vms: 1`. The first start after a build, with the file not yet in the cache of the system, took 0.79 seconds. On a Linux machine with systemd 261, the 300 requests a small app got every 10 ms while `systemctl restart` ran all answered 200, the slowest in 0.14 seconds.

Users stay signed in across the restart only when sessions are kept outside the program. The default store lives in memory, so every restart signs everyone out (see [Where to store sessions](sessions.md#where-to-store-sessions)). An app from `tifa new` keeps them in its SQLite database (`sessions.go`).

Two units, for an app behind a proxy, or served over plain HTTP:

```ini
# /etc/systemd/system/shop.socket
[Unit]
Description=Shop socket

[Socket]
ListenStream=0.0.0.0:9527

[Install]
WantedBy=sockets.target
```

```ini
# /etc/systemd/system/shop.service
[Unit]
Description=Shop
Requires=shop.socket
After=shop.socket

[Service]
User=shop
WorkingDirectory=/var/lib/shop
ExecStart=/srv/shop/shop
Restart=on-failure

[Install]
WantedBy=multi-user.target
```

```sh
sudo systemctl daemon-reload
sudo systemctl enable --now shop.socket shop.service
# each deploy: copy the new program, then
sudo systemctl restart shop.service
```

- Restart the service, not the socket. Restarting or stopping `shop.socket` closes the port.
- `shop.service` is enabled too, so the program starts and prewarms at boot instead of on the first visitor.
- The program reads the socket from `LISTEN_PID`, `LISTEN_FDS` and `LISTEN_FDNAMES`, which systemd sets, and removes them from its environment, so the programs it starts do not see them. `server.addr` is not listened on then. The log line says `socket=systemd`. Without these variables the program listens itself, as before.
- systemd opens the port as root, so a port below 1024 needs no `setcap` or `AmbientCapabilities`.
- With the default `server.shutdown_timeout`, a stop takes at most about 30 seconds: 10 for the requests, 10 more for those still running after they are canceled, and 10 for `OnShutdown`. systemd waits 90 seconds (`TimeoutStopSec`) before it kills the program.

With HTTPS on, the program serves two addresses, and each socket says which by its name. One socket unit gives the same name to all its sockets, so use a unit for each:

```ini
# /etc/systemd/system/shop-https.socket
[Socket]
ListenStream=443
FileDescriptorName=server.addr
Service=shop.service

[Install]
WantedBy=sockets.target
```

```ini
# /etc/systemd/system/shop-http.socket
[Socket]
ListenStream=80
FileDescriptorName=server.http_addr
Service=shop.service

[Install]
WantedBy=sockets.target
```

In `shop.service`, `Requires=` and `After=` name both units, and `[Service]` adds `Sockets=shop-https.socket shop-http.socket`. `server.addr` may stay unset: [HTTPS](config.md#https) asks for it only when the program listens on the address itself. A socket named `server.addr` or `server.http_addr` serves that setting, and a single socket with any other name, or none, serves `server.addr`. Several must be named `server.addr` and `server.http_addr`, and a socket for `server.http_addr` without HTTPS on stops the program with an error. The program listens itself on an address systemd passed no socket for.

## Ports and HTTPS

- The program can get its own certificates from Let's Encrypt, or use yours: [HTTPS](config.md#https).
- Ports 80 and 443 need a right on Linux, or a container that publishes them from high ports: [Ports below 1024](config.md#ports-below-1024).
- Behind a CDN or a proxy that ends TLS, leave HTTPS off and listen on `0.0.0.0:<port>`.

## Behind a CDN or proxy

- **Host and Origin.** Pass `Host` and `Origin` through as the browser sent them. The program refuses a write that a browser sends from another site by comparing them, and a proxy that rewrites `Host` makes your own forms look foreign (403 with `cross-origin request detected`). See [Cross-site writes](config.md#cross-site-writes).
- **The client address.** Tifa does not read `Forwarded` or `X-Forwarded-For`. `c.Request().RemoteAddr` is the address of the proxy. If `OnRequest` needs the visitor's address, read the header your own proxy sets, and only when the request really comes from that proxy.
- **Compression.** The program compresses text answers itself. When the CDN or proxy compresses (brotli, for example), set `server.compress` to `false`. CloudFront, Cloudflare and Google Cloud CDN all leave an answer that already has a `Content-Encoding` alone. See [Compression](build.md#compression).
- **Caching.** Pages, navigation data and handler answers are `private, no-cache` by default, so a CDN does not store them. Keep it that way. Let the CDN cache these:
  - `/assets/`: content-hashed, `public, max-age=31536000, immutable`.
  - The files in `public/`: `no-cache` with an ETag, so the CDN asks and gets a 304.
  - Pages with `Cache`: they are anonymous and carry `Cache-Control: public, max-age=0, s-maxage=<seconds>, stale-if-error=86400`. A CDN that follows the origin's `Cache-Control` keeps a page for the time of `Cache` and no longer. See [Cache for CDNs](app.md#cache-for-cdns).
- **Do not make the CDN ignore the origin's cache headers.** Use a cache setting that follows them, and never one that forces a time or caches everything. The settings for each CDN are in [CDN settings](#cdn-settings).
- **The navigation data of a page with `Cache` is not shared.** It is the page URL plus `?_tifa=...`, it carries the session of the browser, and it is always `private, no-cache`. The cache key must include the query string (at least `_tifa`), or the CDN must skip the cache for requests with `_tifa`. Drop cookies by path only for the HTML of the page, never for the same path with `_tifa`.
- **Do not cache `/__tifa/session`.** It answers per visitor and is `private, no-store`.
- **Do not set `Cache-Control: public` yourself** on other pages, or on a handler that answers the URL of a page. A page with per-user data is safe by default.
- **Over HTTPS, compression can leak a secret** through the length of an answer (BREACH) when the answer repeats something from the request and also carries a secret such as a CSRF token. Set the header `No-Gzip-Compression` on such an answer, or turn `server.compress` off.
- **With `server.csp`, a script the CDN writes into a page does not run.** Its hash is not in the policy Tifa sent with the page. Turn off the features that change the HTML, such as Rocket Loader and Email Address Obfuscation of Cloudflare. See [Content-Security-Policy](config.md#content-security-policy).

### CDN settings

Checked against the vendors' own documentation on 2026-11-04. Names and defaults change, so open the linked page if a setting is missing.

**CloudFront**

- **Host and Origin.** CloudFront sends the domain name of the origin as `Host`, not the one the browser used ([request headers](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/RequestAndResponseBehaviorCustomOrigin.html)). Attach an origin request policy that forwards the viewer's headers, such as the managed `AllViewer`. Not `AllViewerExceptHostHeader`, which drops `Host` ([managed origin request policies](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/using-managed-origin-request-policies.html)).
- **Cache policy for pages.** Not the managed `CachingOptimized`: its minimum TTL is 1 second, and CloudFront then caches the answer even when it says `private`, `no-cache` or `no-store` ([managed cache policies](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/using-managed-cache-policies.html)). With a minimum TTL of 0, CloudFront respects those directives and takes `s-maxage` as the time to keep a page ([expiration](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/Expiration.html)). The managed `UseOriginCacheControlHeaders` has a minimum TTL of 0, but it puts all cookies in the cache key, so a page with `Cache` would not be shared. Make your own cache policy with a minimum TTL of 0, no cookies, and all query strings (or at least `_tifa`) in the key.
- **Client address.** CloudFront appends the address of the connection to `X-Forwarded-For`. What is before it came from the visitor and cannot be trusted. `CloudFront-Viewer-Address` arrives only with an origin request policy that includes it, such as `AllViewerAndCloudFrontHeaders-2022-06` ([client IP addresses](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/RequestAndResponseBehaviorCustomOrigin.html#RequestCustomIPAddresses)).
- **Compression.** Off until "Compress objects automatically" is on. CloudFront compresses only listed content types, between 1,000 and 10,000,000 bytes, only when the origin answer has a `Content-Length`, and never when it has a `Content-Encoding` ([serve compressed files](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/ServingCompressedFiles.html)).
- `stale-if-error` is supported, and capped by the maximum TTL ([expiration](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/Expiration.html#stale-content)).

**Cloudflare**

- **Caching.** HTML and JSON are not cached by default, because Cloudflare decides by file extension ([default cache behavior](https://developers.cloudflare.com/cache/concepts/default-cache-behavior/)). To cache pages with `Cache`, add a Cache Rule for their paths with "Eligible for cache" and set Edge TTL to "Use cache-control header if present, bypass cache if not". Never "Ignore cache-control header and use this TTL" ([cache rule settings](https://developers.cloudflare.com/cache/how-to/cache-rules/settings/)).
- **`private` and `no-store`** are never cached. `s-maxage` is honored. The documentation does not say how `s-maxage=N` works next to `max-age=0`, which is what a page with `Cache` sends, so check that the second request for such a page is a cache hit ([Cache-Control directives](https://developers.cloudflare.com/cache/concepts/cache-control/)). `stale-if-error` is honored for 5xx answers.
- **Cache key.** The default key holds the scheme, the host and the URI with its query string, so the navigation data (`?_tifa=...`) is a different entry from the page ([cache keys](https://developers.cloudflare.com/cache/how-to/cache-keys/)).
- **Host.** Do not rewrite it with an Origin Rule "Override Host header" (Enterprise plans only) ([Origin Rules](https://developers.cloudflare.com/rules/origin-rules/)).
- **Client address.** `CF-Connecting-IP` and `X-Forwarded-For`. `True-Client-IP` is for Enterprise plans and off by default ([HTTP headers](https://developers.cloudflare.com/fundamentals/reference/http-headers/)).
- **Compression.** Cloudflare compresses with gzip, Brotli and Zstandard, and keeps the encoding of the origin when the origin sends a `Content-Encoding` the visitor accepts ([compression](https://developers.cloudflare.com/speed/optimization/content/compression/)).

**Google Cloud CDN**

- **Cache mode.** Use `USE_ORIGIN_HEADERS`. The default `CACHE_ALL_STATIC` also keeps `private` and `no-store` answers out. Not `FORCE_CACHE_ALL`: it ignores `private` and `no-store` and may cache the pages of single users ([cache modes](https://docs.cloud.google.com/cdn/docs/using-cache-modes)).
- **Headers.** `private` and `no-store` are not cached, `s-maxage` beats `max-age`, an answer with `Set-Cookie` is not cached, and `no-cache` is cached but checked with the origin each time. `stale-if-error` has no effect as a response header ([caching](https://docs.cloud.google.com/cdn/docs/caching)).
- **Cache key.** For a backend service the default key is the whole URI with its query string. Do not set a query string include or exclude list that leaves `_tifa` out ([caching](https://docs.cloud.google.com/cdn/docs/caching)).
- **Host and client address.** The load balancer keeps the `Host` of the request. It appends `<client address>,<load balancer address>` to `X-Forwarded-For`, and does not check what was there before ([external Application Load Balancer](https://docs.cloud.google.com/load-balancing/docs/https)).
- **Compression.** Dynamic compression is off by default, needs a `Content-Length`, and skips answers that have a `Content-Encoding` ([dynamic compression](https://docs.cloud.google.com/cdn/docs/dynamic-compression)).

## Health checks and metrics

Tifa has no health route or metrics route of its own. Add the ones you need as API routes.

### Health check

A `GET` that answers 204 while the program can serve. Check what it cannot work without, such as the database of [the database guide](guides/database.md), and answer 503 when that is down:

```go
// app/healthz/route.go
package healthz

func GET(c *tifa.Ctx) error {
	if err := notes.DB.PingContext(c.Context()); err != nil {
		return tifa.Error{Status: 503, Message: "database unreachable"}
	}
	return c.NoContent(204)
}
```

- Point the load balancer, the `HEALTHCHECK` of a container or a Kubernetes probe at `/healthz`. The program listens only once `OnStart` is done and the VMs are loaded, so the first 204 means it can render. When it is told to stop, new connections fail and the check goes red.
- The route renders nothing, so a full render queue does not fail it, and a busy program is not restarted for being busy. Watch `tifa_renders_rejected_total` in the metrics below for that.

### Metrics

`tifa.Stats()` returns the state of the VM pool, the render memo, and what it did since the program started. The counts are atomic, and reading the number of entries and VMs takes a short lock, so reading it every few seconds costs nothing you can measure. Under `tifa dev` the counts of the VM pool start again from zero each time the front end is rebuilt. This route writes it in the text format Prometheus reads, with no library:

```go
// app/metrics/route.go
package metrics

import (
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"

	"github.com/liustack/tifa"
)

func GET(c *tifa.Ctx) error {
	s := tifa.Stats()
	var b strings.Builder
	metric := func(name, kind, help string, value float64) {
		fmt.Fprintf(&b, "# HELP %s %s\n# TYPE %s %s\n%s %g\n", name, help, name, kind, name, value)
	}
	metric("tifa_vms", "gauge", "VMs loaded.", float64(s.SSR.VMs))
	metric("tifa_vms_busy", "gauge", "VMs rendering.", float64(s.SSR.Busy))
	metric("tifa_renders_queued", "gauge", "Renders waiting for a VM.", float64(s.SSR.Queued))
	metric("tifa_renders_total", "counter", "Renders that ran in a VM.", float64(s.SSR.Renders))
	metric("tifa_renders_failed_total", "counter", "Renders that failed.", float64(s.SSR.Failed))
	metric("tifa_render_seconds_total", "counter", "Time the renders took.", s.SSR.RenderTime.Seconds())
	metric("tifa_renders_rejected_total", "counter", "Renders answered 503 as busy.", float64(s.SSR.Rejected))
	metric("tifa_memo_entries", "gauge", "Entries kept.", float64(s.Memo.Entries))
	metric("tifa_memo_hits_total", "counter", "Answered from an entry.", float64(s.Memo.Hits))
	metric("tifa_memo_misses_total", "counter", "Rendered by the request.", float64(s.Memo.Misses))
	metric("tifa_memo_shared_total", "counter", "Waited for another request.", float64(s.Memo.Shared))
	metric("tifa_memo_unchanged_total", "counter", "Rendered again with the same page as before.", float64(s.Memo.Unchanged))
	metric("tifa_memo_evictions_total", "counter", "Dropped to make room for another.", float64(s.Memo.Evictions))
	metric("tifa_memo_bytes", "gauge", "Bytes the render memo holds.", float64(s.MemoBytes))
	metric("tifa_memo_size_bytes", "gauge", "Bytes the render memo may hold.", float64(s.MemoSize))
	routes := slices.Sorted(maps.Keys(s.Memo.Routes))
	byRoute := func(name, help string, value func(tifa.MemoRouteStats) uint64) {
		fmt.Fprintf(&b, "# HELP %s %s\n# TYPE %s counter\n", name, help, name)
		for _, route := range routes {
			fmt.Fprintf(&b, "%s{route=%q} %d\n", name, route, value(s.Memo.Routes[route]))
		}
	}
	byRoute("tifa_memo_route_hits_total", "Answered from an entry, by route.", func(r tifa.MemoRouteStats) uint64 { return r.Hits })
	byRoute("tifa_memo_route_misses_total", "Rendered by the request, by route.", func(r tifa.MemoRouteStats) uint64 { return r.Misses })
	byRoute("tifa_memo_route_shared_total", "Waited for another request, by route.", func(r tifa.MemoRouteStats) uint64 { return r.Shared })
	byRoute("tifa_memo_route_unchanged_total", "Rendered again with the same page as before, by route.", func(r tifa.MemoRouteStats) uint64 { return r.Unchanged })
	c.SetHeader("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, err := io.WriteString(c.Writer(), b.String())
	return err
}
```

- The counts by route carry the route pattern as the label `route`, such as `route="/store/products/{product}"`, one for all the languages of a route. URLs that match no page are `route=""`, which Prometheus shows as a series without the label. The routes are the ones of the app, so the series stay few. See [Counts by route](memo.md#counts-by-route).
- An app that already uses `client_golang` can write a `prometheus.Collector` instead, whose `Collect` reads `tifa.Stats()` once and sends each value with `prometheus.MustNewConstMetric`.
- The numbers say nothing about visitors, but they say how loaded the program is. Keep the route away from the public: let the proxy in front refuse `/metrics` from outside, or check a token in the handler.
- Before the server starts and after it stops, `tifa.Stats()` returns zeros. An app without pages has no VMs. The counts start at zero with each start of the program, which Prometheus reads as a restart.

What to read, and what to change:

| What you see | What it means | What to do |
| --- | --- | --- |
| `tifa_renders_rejected_total` grows | Renders found the queue full or waited past `ssr.wait_timeout`, and got 503 | More CPU and `ssr.vms`, or fewer renders (see [Sizing a machine](performance.md#sizing-a-machine) and [Render queue](config.md#render-queue)) |
| `tifa_vms_busy` is `tifa_vms` most of the time, `tifa_renders_queued` above 0 | Every VM renders and others wait | The same: the program is at its limit |
| `rate(tifa_render_seconds_total[5m]) / rate(tifa_renders_total[5m])` | The mean time of a render | When it grows, find the page that got slower |
| `tifa_renders_failed_total` grows | Renders throw, run past `ssr.timeout` or use up `ssr.vm_memory` | The log has each failure |
| Hits low next to misses, `tifa_memo_evictions_total` grows, `tifa_memo_bytes` near `tifa_memo_size_bytes` | The render memo drops pages before they are asked for again | A larger `ssr.memo_size` |
| `tifa_memo_route_hits_total` of one route low next to its misses, and no evictions | Its URLs do not come back, or the data its page shows changes between requests | See which in [Counts by route](memo.md#counts-by-route) |
| `tifa_memo_route_unchanged_total` of a route grows with its misses | Its page renders again and comes out the same: its data changed in a part the page does not show | Return from the `Load` only what the page shows, and look at the Layout above it when many routes grow at once ([Counts by route](memo.md#counts-by-route)) |

## After a deploy

- A visitor who had a page open keeps working: the next click or handler call sees the new build, and the browser reloads the page once. See [Pages open while you deploy](app.md#pages-open-while-you-deploy).
- If your `OnRequest` or a CDN rule lets a CDN keep pages other than the ones with `Cache`, clear them on every deploy. Otherwise a visitor can get an old page whose scripts are gone.
- A CDN that keeps pages with `Cache` hands out the old HTML until `s-maxage` runs out, and the scripts that HTML names may be gone. Clear those pages in the CDN too when you deploy. A browser that asks the program itself gets the new page at once.
- The render memo of the program lives in memory and starts empty after a restart.
