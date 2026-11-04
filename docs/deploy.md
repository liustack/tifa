---
summary: 'How to put a built tifa program on a server: building for Linux, running it with its settings and data folder, stopping it cleanly, containers, ports and HTTPS, what to set when a CDN or proxy stands in front (Host and Origin, compression, which answers to cache), and what happens to open pages when you deploy'
read_when:
  - Putting a tifa app on a VPS or in a container for the first time
  - Putting a CDN or a reverse proxy such as nginx, Cloudflare or CloudFront in front of a tifa app
  - A POST gets 403 with `cross-origin request detected` only behind the proxy
  - Pages show old content or a missing script after a deploy
  - Deciding what a systemd unit or a Dockerfile for a tifa app needs
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
- **Compression.** The program compresses text answers itself. When the CDN or proxy compresses (brotli, for example), set `server.compress` to `false`. A proxy leaves an answer that already has a `Content-Encoding` alone. See [Compression](build.md#compression).
- **Caching.** Pages, navigation data and handler answers are `private, no-cache` by default, so a CDN does not store them. Keep it that way. Let the CDN cache these:
  - `/assets/`: content-hashed, `public, max-age=31536000, immutable`.
  - The files in `public/`: `no-cache` with an ETag, so the CDN asks and gets a 304.
  - Pages with `Cache`: they are anonymous and carry `Cache-Control: public, max-age=0, s-maxage=<seconds>, stale-if-error=86400` and `Age`. A CDN that follows the origin's `Cache-Control` keeps a page for the time of `Cache` and no longer. See [The whole-page cache](app.md#the-whole-page-cache).
- **Do not make the CDN ignore the origin's cache headers.** CloudFront: not the managed policy `CachingOptimized` for pages, since its minimum TTL of 1 second overrides `private` and `no-store`. Use a policy with a minimum TTL of 0 that follows the origin, and forward no cookies for pages with `Cache`. Cloudflare: HTML is not cached by default. For pages with `Cache`, add a Cache Rule "Eligible for cache" with Edge TTL set to "Use cache-control header if present", never "Ignore cache-control header". Google Cloud CDN: cache mode `USE_ORIGIN_HEADERS`, not `FORCE_CACHE_ALL`.
- **The navigation data of a page with `Cache` is not shared.** It is the page URL plus `?_tifa=...`, it carries the session of the browser, and it is always `private, no-cache`. The cache key must include the query string (at least `_tifa`), or the CDN must skip the cache for requests with `_tifa`. Drop cookies by path only for the HTML of the page, never for the same path with `_tifa`.
- **Do not cache `/__tifa/session`.** It answers per visitor and is `private, no-store`.
- **Do not set `Cache-Control: public` yourself** on other pages, or on a handler that answers the URL of a page. A page with per-user data is safe by default.
- **Over HTTPS, compression can leak a secret** through the length of an answer (BREACH) when the answer repeats something from the request and also carries a secret such as a CSRF token. Set the header `No-Gzip-Compression` on such an answer, or turn `server.compress` off.

## After a deploy

- A visitor who had a page open keeps working: the next click or handler call sees the new build, and the browser reloads the page once. See [Pages open while you deploy](app.md#pages-open-while-you-deploy).
- If your `OnRequest` or a CDN rule lets a CDN keep pages other than the ones with `Cache`, clear them on every deploy. Otherwise a visitor can get an old page whose scripts are gone.
- A CDN that keeps pages with `Cache` hands out the old HTML until `s-maxage` runs out, and the scripts that HTML names may be gone. Clear those pages in the CDN too when you deploy. A browser that asks the program itself gets the new page at once.
- The whole-page cache and the render cache of the program live in memory and start empty after a restart.
