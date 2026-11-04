---
summary: 'How to put a built tifa program on a server: building for Linux, running it with its settings and data folder, stopping it cleanly, containers, ports and HTTPS, what to set when a CDN or proxy stands in front (Host and Origin, compression, which answers to cache), a health check route, metrics for Prometheus from tifa.Stats and what to read in them, and what happens to open pages when you deploy'
read_when:
  - Putting a tifa app on a VPS or in a container for the first time
  - Putting a CDN or a reverse proxy such as nginx, Cloudflare or CloudFront in front of a tifa app
  - A POST gets 403 with `cross-origin request detected` only behind the proxy
  - Pages show old content or a missing script after a deploy
  - Deciding what a systemd unit or a Dockerfile for a tifa app needs
  - Adding a health check for a load balancer, a container or Kubernetes
  - Watching the VM pool and the caches with Prometheus or another monitoring system, or deciding from them whether the machine needs more CPU or a larger `cache.size`
---

English · [简体中文](deploy.zh-CN.md)

# Deploying

A tifa app is one executable. The server needs that file, the settings in its environment, and a folder it may write data to. It needs no Node, no process manager and no Redis. The default session store lives in memory, so for more than one copy of the program give it a [store of your own](sessions.md#where-to-store-sessions).

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
- SIGINT and SIGTERM stop it gracefully: new connections stop, requests in flight get `server.shutdown_timeout` to finish, then `OnShutdown` runs. A systemd unit or a container needs nothing more than the executable, the environment and a writable data folder.
- A `FROM scratch` container works. The program holds the time zone data its renders need.
- Size the machine with [Performance](performance.md#sizing-a-machine). `ssr.vms` follows the CPU limit of a container.

## Ports and HTTPS

- The program can get its own certificates from Let's Encrypt, or use yours: [HTTPS](config.md#https).
- Ports 80 and 443 need a right on Linux, or a container that publishes them from high ports: [Ports below 1024](config.md#ports-below-1024).
- Behind a CDN or a proxy that ends TLS, leave HTTPS off and listen on `0.0.0.0:<port>`.

## Behind a CDN or proxy

- **Host and Origin.** Pass `Host` and `Origin` through as the browser sent them. The program refuses a write that a browser sends from another site by comparing them, and a proxy that rewrites `Host` makes your own forms look foreign (403 with `cross-origin request detected`). See [Cross-site writes](config.md#cross-site-writes).
- **The client address.** tifa does not read `Forwarded` or `X-Forwarded-For`. `c.Request().RemoteAddr` is the address of the proxy. If `OnRequest` needs the visitor's address, read the header your own proxy sets, and only when the request really comes from that proxy.
- **Compression.** The program compresses text answers itself. When the CDN or proxy compresses (brotli, for example), set `server.compress` to `false`. CloudFront, Cloudflare and Google Cloud CDN all leave an answer that already has a `Content-Encoding` alone. See [Compression](build.md#compression).
- **Caching.** Pages, navigation data and handler answers are `private, no-cache` by default, so a CDN does not store them. Keep it that way. Let the CDN cache these:
  - `/assets/`: content-hashed, `public, max-age=31536000, immutable`.
  - The files in `public/`: `no-cache` with an ETag, so the CDN asks and gets a 304.
  - Pages with `Cache`: they are anonymous and carry `Cache-Control: public, max-age=0, s-maxage=<seconds>, stale-if-error=86400` and `Age`. A CDN that follows the origin's `Cache-Control` keeps a page for the time of `Cache` and no longer. See [The whole-page cache](app.md#the-whole-page-cache).
- **Do not make the CDN ignore the origin's cache headers.** Use a cache setting that follows them, and never one that forces a time or caches everything. The settings for each CDN are in [CDN settings](#cdn-settings).
- **The navigation data of a page with `Cache` is not shared.** It is the page URL plus `?_tifa=...`, it carries the session of the browser, and it is always `private, no-cache`. The cache key must include the query string (at least `_tifa`), or the CDN must skip the cache for requests with `_tifa`. Drop cookies by path only for the HTML of the page, never for the same path with `_tifa`.
- **Do not cache `/__tifa/session`.** It answers per visitor and is `private, no-store`.
- **Do not set `Cache-Control: public` yourself** on other pages, or on a handler that answers the URL of a page. A page with per-user data is safe by default.
- **Over HTTPS, compression can leak a secret** through the length of an answer (BREACH) when the answer repeats something from the request and also carries a secret such as a CSRF token. Set the header `No-Gzip-Compression` on such an answer, or turn `server.compress` off.

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

tifa has no health route or metrics route of its own. Add the ones you need as API routes.

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

`tifa.Stats()` returns the state of the VM pool, the render cache and the whole-page cache, and what they did since the program started. The counts are atomic, and reading the number of entries and VMs takes a short lock, so reading it every few seconds costs nothing you can measure. Under `tifa dev` the counts of the VM pool start again from zero each time the front end is rebuilt. This route writes it in the text format Prometheus reads, with no library:

```go
// app/metrics/route.go
package metrics

import (
	"fmt"
	"io"
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
	for _, cache := range []struct {
		name  string
		stats tifa.CacheStats
	}{{"render_cache", s.RenderCache}, {"page_cache", s.PageCache}} {
		metric("tifa_"+cache.name+"_entries", "gauge", "Entries kept.", float64(cache.stats.Entries))
		metric("tifa_"+cache.name+"_hits_total", "counter", "Answered from an entry.", float64(cache.stats.Hits))
		metric("tifa_"+cache.name+"_misses_total", "counter", "Made by the request.", float64(cache.stats.Misses))
		metric("tifa_"+cache.name+"_shared_total", "counter", "Waited for another request.", float64(cache.stats.Shared))
	}
	metric("tifa_page_cache_stale_total", "counter", "Old pages sent for a failed one.", float64(s.PageCache.Stale))
	metric("tifa_cache_bytes", "gauge", "Bytes the caches hold.", float64(s.CacheBytes))
	metric("tifa_cache_size_bytes", "gauge", "Bytes the caches may hold.", float64(s.CacheSize))
	c.SetHeader("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, err := io.WriteString(c.Writer(), b.String())
	return err
}
```

- An app that already uses `client_golang` can write a `prometheus.Collector` instead, whose `Collect` reads `tifa.Stats()` once and sends each value with `prometheus.MustNewConstMetric`.
- The numbers say nothing about visitors, but they say how loaded the program is. Keep the route away from the public: let the proxy in front refuse `/metrics` from outside, or check a token in the handler.
- Before the server starts and after it stops, `tifa.Stats()` returns zeros. An app without pages has no VMs. The counts start at zero with each start of the program, which Prometheus reads as a restart.

What to read, and what to change:

| What you see | What it means | What to do |
| --- | --- | --- |
| `tifa_renders_rejected_total` grows | Renders found the queue full or waited past `ssr.wait_timeout`, and got 503 | More CPU and `ssr.vms`, or more caching (see [Sizing a machine](performance.md#sizing-a-machine) and [Render queue](config.md#render-queue)) |
| `tifa_vms_busy` is `tifa_vms` most of the time, `tifa_renders_queued` above 0 | Every VM renders and others wait | The same: the program is at its limit |
| `rate(tifa_render_seconds_total[5m]) / rate(tifa_renders_total[5m])` | The mean time of a render | When it grows, find the page that got slower |
| `tifa_renders_failed_total` grows | Renders throw, run past `ssr.timeout` or use up `ssr.vm_memory` | The log has each failure |
| Hits low next to misses, `tifa_cache_bytes` near `tifa_cache_size_bytes` | The caches drop pages before they are asked for again | A larger `cache.size` |
| `tifa_page_cache_stale_total` grows | Pages with `Cache` fail to make again and the old page is sent | The log has the error of each failed make |

## After a deploy

- A visitor who had a page open keeps working: the next click or handler call sees the new build, and the browser reloads the page once. See [Pages open while you deploy](app.md#pages-open-while-you-deploy).
- If your `OnRequest` or a CDN rule lets a CDN keep pages other than the ones with `Cache`, clear them on every deploy. Otherwise a visitor can get an old page whose scripts are gone.
- A CDN that keeps pages with `Cache` hands out the old HTML until `s-maxage` runs out, and the scripts that HTML names may be gone. Clear those pages in the CDN too when you deploy. A browser that asks the program itself gets the new page at once.
- The whole-page cache and the render cache of the program live in memory and start empty after a restart.
