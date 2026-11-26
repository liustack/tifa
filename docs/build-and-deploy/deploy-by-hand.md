---
summary: 'Deploy without `tifa deploy`: build for the server, run the program, restart without dropping requests, health checks and metrics'
audience: app
order: 16
read_when:
  - Pages show old content or a missing script after a deploy
  - Deciding what a systemd unit or a Dockerfile for a Tifa app needs
  - Visitors get refused connections, or are signed out, each time you deploy
  - A visitor keeps a page open while you deploy, and you want to know what the browser does, or a 409 with `X-Tifa-Build` shows up
  - Adding a health check for a load balancer, a container or Kubernetes
  - Watching the VM pool and the render memo with Prometheus or another monitoring system, or deciding from them whether the machine needs more CPU or a larger `ssr.memo_size`
---

# Deploy by hand

The way to put a Tifa program on a server yourself, in a container or under your own process manager, and what `tifa deploy` does underneath.

## Build for the server

The server render engine runs on linux and darwin, on amd64 and arm64, and windows on amd64 and arm64. Build on your machine or in CI with the target set, and copy the file over. Do not build on a small server such as a 1 GB VPS: the build needs more memory than the running app, and it can run out.

```sh
GOOS=linux GOARCH=amd64 $(go tool -n tifa) build -o bin/myapp-linux
scp bin/myapp-linux server:/srv/myapp/myapp
```

`GOOS=linux go tool tifa build` does not work, and [Building for another system](build.md#building-for-another-system) says why. The program holds code under several licenses, so when you hand it to others, ship the notices with it (see [Notices to ship with the program](build.md#notices-to-ship-with-the-program)).

## Run it

```sh
SERVER_ADDR=0.0.0.0:9527 ./myapp
```

- Every setting has a flag and an environment variable, and the program also reads a `.env` file of its working folder and a file given with `-c`. [Configuration](config.md) says which source wins and [Settings](../reference/settings.md) has the list. `./myapp config` prints every setting with its value and where it came from, and `./myapp version` prints the versions.
- Platforms that tell the program its port in `PORT`, such as Cloud Run or Fly, work without more settings (see [PORT and HOST](config.md#port-and-host)).
- Run it from the folder it keeps data in. A relative path such as the `app.db` of the `tifa new` template (the setting `db.path`) is resolved against the working directory. Back that folder up, or mount it in a container.
- SIGINT and SIGTERM stop it gracefully: new connections stop, requests in flight get `server.shutdown_timeout` to finish, then `OnShutdown` runs. A stream with no end of its own, such as server-sent events, returns at once when it watches `tifa.ShuttingDown` (see [Streaming](../guides/streaming.md#when-the-program-stops)). A systemd unit or a container needs nothing more than the executable, the environment and a writable data folder.
- A `FROM scratch` container works. The program holds the time zone data its renders need.
- Size the machine with [Performance](../reference/performance.md#sizing-a-machine). `ssr.vms` follows the CPU limit of a container, up to 4 VMs. On more CPUs, set it.

## Restart without dropping requests

A restart stops the program, and the next one prewarms its VMs before it listens. A connection that arrives in between is refused, unless systemd holds the socket: with socket activation, systemd listens on the port and hands the socket to the program, and keeps it open while the program restarts. The system takes the connections that arrive meanwhile, and the next program answers them. A visitor only waits a little longer: on an M4 Mac, the program of the example shop answers its first page 0.42 seconds after it starts with 10 VMs, and 0.27 seconds with `ssr.vms: 1`. The first start after a build, with the file not yet in the cache of the system, took 0.79 seconds. On a Linux machine with systemd 261, the 300 requests a small app got every 10 ms while `systemctl restart` ran all answered 200, the slowest in 0.14 seconds.

Users stay signed in across the restart only when sessions are kept outside the program. The default store lives in memory, so every restart signs everyone out (see [Where to store sessions](../pages-and-data/sessions.md#where-to-store-sessions)). An app from `tifa new` keeps them in its SQLite database (`sessions.go`).

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

In `shop.service`, `Requires=` and `After=` name both units, and `[Service]` adds `Sockets=shop-https.socket shop-http.socket`. `server.addr` may stay unset: [HTTPS](https.md#https) asks for it only when the program listens on the address itself. A socket named `server.addr` or `server.http_addr` serves that setting, and a single socket with any other name, or none, serves `server.addr`. Several must be named `server.addr` and `server.http_addr`, and a socket for `server.http_addr` without HTTPS on stops the program with an error. The program listens itself on an address systemd passed no socket for.

## Ports and HTTPS

- The program can get its own certificates from Let's Encrypt, or use yours: [HTTPS](https.md#https).
- Ports 80 and 443 need a right on Linux, or a container that publishes them from high ports: [Ports below 1024](https.md#ports-below-1024).
- Behind a CDN or a proxy that ends TLS, leave HTTPS off and listen on `0.0.0.0:<port>`.

## After a deploy

- A visitor who had a page open keeps working: the next click or handler call sees the new build, and the browser reloads the page once. See [Pages open while you deploy](#pages-open-while-you-deploy).
- If your `OnRequest` or a CDN rule lets a CDN keep pages other than the ones with `Cache`, clear them on every deploy. Otherwise a visitor can get an old page whose scripts are gone.
- A CDN that keeps pages with `Cache` hands out the old HTML until `s-maxage` runs out, and the scripts that HTML names may be gone. With `front: cloudflare`, `tifa deploy` clears the host name for you when it has the token or a cf login (see [Clearing the CDN cache](deploy-cloudflare.md#clearing-the-cdn-cache)). Without them the old page stays until `s-maxage` runs out, the accepted cost of a CDN. On another CDN, clear those pages when you deploy. A browser that asks the program itself gets the new page at once.
- The render memo of the program lives in memory and starts empty after a restart.

## Pages open while you deploy

A visitor can keep a page open while you deploy a new version, so the page is from the old build and the server is the new one. The page sends the build it was rendered with along with its data requests (in the `_tifa` query parameter) and its handler calls (`X-Tifa-Build`), and the server answers 409 for another build. What the visitor sees:

- Clicking a link: the browser reloads the current page, and the new build shows it. One reload, whatever number of requests got the 409.
- Calling a handler function: the call fails with a `TifaError` of status 409 and the browser reloads the page. The call is refused before the handler runs and never sent again, because the new page may mean another input by it. The visitor does it again on the new page. A 409 that your own handler returns has no `X-Tifa-Build` header and reloads nothing. A call without the header, from curl or another program, is not checked.
- Loading code: a lazy route or an `import()` asks for a chunk that the new build no longer has, and the browser reloads the page (this is Vite's `vite:preloadError`).

Each reload is made once for a build. When the page it brings back still disagrees with the server, which happens when a cache hands out the old page again, it does not reload a second time. The data request fails with the 409, which the `ErrorBoundary` of the route shows, the call throws it, and a failed import goes to the `ErrorBoundary` of the route or to the code that imported. The tab remembers the reloads in `sessionStorage`, and a page reloads only after it has written the mark and read it back. When the browser blocks the storage or does not keep what is written, the page does not reload: the failures above show as they are, with the message "the application was updated, reload the page", and the visitor reloads by hand. A reload that cannot be remembered could repeat for as long as the build disagrees.

A page whose HTML is old and whose entry script is gone cannot be fixed by the page: no code runs. Tifa sends pages with `Cache-Control: private, no-cache`, so a CDN does not keep them, and this can only happen when your `OnRequest` lets a CDN cache pages. Then clear the pages in the CDN when you deploy.

## Health checks and metrics

Tifa has no health route or metrics route of its own. Add the ones you need as API routes.

### Health check

A `GET` that answers 204 while the program can serve. Check what it cannot work without, such as the database of [the database guide](../guides/database.md), and answer 503 when that is down:

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

- `tifa deploy` asks for `/healthz` after each deploy and goes back when it does not answer below 500 within 30 seconds (see [Deploy](deploy.md#deploy)).
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
	metric("tifa_vms_replaced_total", "counter", "VMs replaced because they grew.", float64(s.SSR.Replaced))
	metric("tifa_vms_memory_bytes", "gauge", "Bytes the VMs hold after their last GC.", float64(s.SSR.Memory))
	metric("tifa_memo_entries", "gauge", "Entries kept.", float64(s.Memo.Entries))
	metric("tifa_memo_hits_total", "counter", "Answered from an entry.", float64(s.Memo.Hits))
	metric("tifa_memo_misses_total", "counter", "Rendered by the request.", float64(s.Memo.Misses))
	metric("tifa_memo_shared_total", "counter", "Waited for another request.", float64(s.Memo.Shared))
	metric("tifa_memo_evictions_total", "counter", "Dropped to make room for another.", float64(s.Memo.Evictions))
	metric("tifa_memo_bytes", "gauge", "Bytes the render memo holds.", float64(s.MemoBytes))
	metric("tifa_memo_size_bytes", "gauge", "Bytes the render memo may hold.", float64(s.MemoSize))
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
| `tifa_renders_rejected_total` grows | Renders found the queue full or waited past `ssr.wait_timeout`, and got 503 | More CPU and `ssr.vms`, or fewer renders (see [Sizing a machine](../reference/performance.md#sizing-a-machine) and [Render queue](tuning.md#render-queue)) |
| `tifa_vms_busy` is `tifa_vms` most of the time, `tifa_renders_queued` above 0 | Every VM renders and others wait | The same: the program is at its limit |
| `rate(tifa_render_seconds_total[5m]) / rate(tifa_renders_total[5m])` | The mean time of a render | When it grows, find the page that got slower |
| `tifa_renders_failed_total` grows | Renders throw, run past `ssr.timeout` or use up `ssr.vm_memory` | The log has each failure |
| `tifa_vms_memory_bytes` keeps climbing, or `tifa_vms_replaced_total` grows | The VMs hold more after each collection. A VM that holds more than half of `ssr.vm_memory` is replaced | Find what the pages keep, or the leak in the app. If VMs are replaced again within minutes, raise `ssr.vm_memory` (see [Memory](../reference/performance.md#memory)) |
| Hits low next to misses, `tifa_memo_evictions_total` grows, `tifa_memo_bytes` near `tifa_memo_size_bytes` | The render memo drops pages before they are asked for again | A larger `ssr.memo_size` |
