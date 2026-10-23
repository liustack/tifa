---
summary: 'Every setting of a tifa app: where each one can be set, which source wins, the default, the flag and environment variable names, the value formats and the errors a bad value gives, and how to serve HTTPS with certificates the program gets itself or with your own'
read_when:
  - Deploying a tifa app, or changing its port, body limit, compression, VM count, VM stack depth, render timeout, render queue or page cache
  - Serving HTTPS from the program itself, with Let's Encrypt or your own certificate, or deciding whether to
  - The program cannot listen on port 80 or 443, or gets no certificate
  - Setting the browser target, the static files folder or the profile of a build
  - The program refuses to start with a config error
  - Changing internal/config
---

English · [简体中文](config.zh-CN.md)

# Configuration

A tifa app runs with sensible defaults for a small machine. Change a setting only when you need to.

## Where settings come from

From highest to lowest priority:

1. Command-line flags of `serve`, such as `--server-addr 0.0.0.0:9527`.
2. `TIFA_*` environment variables, such as `TIFA_SERVER_ADDR`.
3. The deployment file given with `--config path.yaml` (`.yaml`, `.yml` or `.json`).
4. The project file `tifa.yaml` or `tifa.json` in the app root, embedded into the program by `tifa build`.
5. The defaults below.

The project file is read at build time and is the only place for `build.*` settings. Keep one project file: having both `tifa.yaml` and `tifa.json` fails the build.

```yaml
# tifa.yaml
build:
  target: es2020,safari14
server:
  addr: 0.0.0.0:9527
  body_limit: 4MiB
ssr:
  timeout: 2s
cache:
  size: 128MiB
```

Run `./bin/app config` to print the merged settings of all sources as a commented file. It is a good starting point for a deployment file.

## Settings

| Key | Default | Flag and environment variable | What it does |
| --- | --- | --- | --- |
| `build.target` | `es2020` | project file only | JavaScript target of the browser bundle that Vite builds, a comma-separated list: one ECMAScript version (`es2015` to `es2029`, `esnext`) plus browsers with versions, from chrome, edge, firefox, ios, opera and safari, such as `es2019,chrome90,safari14`. The SSR bundle always targets ES2022 |
| `build.public` | `public` | project file only | Folder of static files copied into the program as they are and served from `/` |
| `build.skip_tanstack_check` | `false` | project file only | `true` builds an app whose TanStack Router is not the version tifa is tested with, which may leave pages blank in the browser. See [build.md](build.md#the-apps-tsconfig-and-vite-config) |
| `build.pgo` | `auto` | project file only | Profile `go build` optimizes the program with. `auto` takes `default.pgo` in the app root, or else, for an app with pages, the profile of page renders tifa ships. `off` builds without a profile. Any other value is a file, given relative to the app root. `tifa dev` never uses one. See [build.md](build.md#profile-guided-optimization) |
| `build.debug_info` | `false` | project file only | `true` keeps the DWARF debug info in the program, for debuggers such as Delve. Without it, `tifa build` leaves the debug info out with `-ldflags=-w`, which makes the example shop's program about 20% smaller. Panics still give the file and line of each frame either way. `tifa dev` always keeps it. See [build.md](build.md#debug-info) |
| `build.i18next_cache` | `false` | project file only | `true` makes the server render keep the strings i18next translates and give them again for the same key and options, so pages that translate the same texts on every render take less time: the shop's storefront pages 0.741 of it. The app must change i18next's resources and options only through i18next's API, and its formatters and post-processors must give the same result for the same input, or pages may show stale translations. See [build.md](build.md#i18next-cache) |
| `server.addr` | `127.0.0.1:9527` | `--server-addr`, `TIFA_SERVER_ADDR` | Address to listen on. Use `0.0.0.0:9527` in a container. With [HTTPS](#https) on it serves HTTPS and must be set, usually to `:443` |
| `server.body_limit` | `1MiB` | `--server-body-limit`, `TIFA_SERVER_BODY_LIMIT` | Largest request body. A bigger one is answered with 413 |
| `server.shutdown_timeout` | `10s` | `--server-shutdown-timeout`, `TIFA_SERVER_SHUTDOWN_TIMEOUT` | After a stop signal, the time in-flight requests get to finish, and then the time `OnShutdown` gets. Requests still running when it ends are canceled and get one more `shutdown_timeout` to return. `OnShutdown` runs once they have returned and is skipped if they have not. Either way the program exits with status 1 |
| `server.compress` | `true` | `--server-compress`, `TIFA_SERVER_COMPRESS` | Sends text answers with gzip or zstd to clients that accept them, and the browser bundle as the gzip files `tifa build` wrote, see [Compression](build.md#compression). Set it to `false` when a proxy in front compresses, such as nginx with brotli |
| `server.tls_domains` | none | `--server-tls-domains`, `TIFA_SERVER_TLS_DOMAINS` | Turns on [HTTPS](#https) with certificates from Let's Encrypt for these domains, comma-separated, such as `example.com,www.example.com` |
| `server.tls_email` | none | `--server-tls-email`, `TIFA_SERVER_TLS_EMAIL` | Contact address given to Let's Encrypt with `tls_domains`. Optional |
| `server.tls_cache` | none | `--server-tls-cache`, `TIFA_SERVER_TLS_CACHE` | Folder that keeps the certificates of `tls_domains` across restarts. Needed with `tls_domains`, and must survive new deployments |
| `server.tls_ca` | `https://acme-v02.api.letsencrypt.org/directory` | `--server-tls-ca`, `TIFA_SERVER_TLS_CA` | ACME directory that `tls_domains` get their certificates from. Let's Encrypt by default |
| `server.tls_cert` | none | `--server-tls-cert`, `TIFA_SERVER_TLS_CERT` | Turns on [HTTPS](#https) with your own certificate file instead, in PEM, loaded again when it changes |
| `server.tls_key` | none | `--server-tls-key`, `TIFA_SERVER_TLS_KEY` | Private key file of `tls_cert`, in PEM |
| `server.http_addr` | `:80` | `--server-http-addr`, `TIFA_SERVER_HTTP_ADDR` | With HTTPS on, the plain HTTP address that answers the checks of Let's Encrypt and redirects everything else to HTTPS. `off` for none. Not used without HTTPS |
| `server.hsts` | `false` | `--server-hsts`, `TIFA_SERVER_HSTS` | Adds `Strict-Transport-Security: max-age=31536000` to the answers of `server.addr`, so browsers keep to HTTPS for a year |
| `ssr.vms` | the CPU count (`GOMAXPROCS`) | `--ssr-vms`, `TIFA_SSR_VMS` | Prewarmed JavaScript VMs, which is how many pages render at once. In a container it follows the CPU limit. [Sizing a machine](performance.md#sizing-a-machine) says when to change it |
| `ssr.vm_memory` | `128MiB` | `--ssr-vm-memory`, `TIFA_SSR_VM_MEMORY` | Memory limit of each VM. A render that runs out of memory fails, and the VM is replaced with a fresh one. Page code cannot catch it. The shapes the engine keeps for reuse count here too, up to about 6 MiB per VM at worst (2048 shapes of 32 keys of about ten characters, more with longer keys), so leave room for them when you lower it (see [Memory](performance.md#memory)) |
| `ssr.vm_stack_depth` | `10000` | `--ssr-vm-stack-depth`, `TIFA_SSR_VM_STACK_DEPTH` | How deep JavaScript calls may nest in each VM, about one level per call. Deeper calls throw `RangeError: Maximum call stack size exceeded`, at about the depth Node does. From 1 to 10000, see [Stack depth](#stack-depth) |
| `ssr.timeout` | `3s` | `--ssr-timeout`, `TIFA_SSR_TIMEOUT` | Time budget of one render. A render that runs longer is stopped and handled as an unexpected error, which goes to `OnError`. Page code cannot catch it |
| `ssr.queue_limit` | `32` | `--ssr-queue-limit`, `TIFA_SSR_QUEUE_LIMIT` | Renders allowed to wait for a VM, excluding renders already running. `0` rejects immediately when no VM is idle. A full queue answers 503 with `Retry-After: 1` |
| `ssr.wait_timeout` | `1s` | `--ssr-wait-timeout`, `TIFA_SSR_WAIT_TIMEOUT` | Maximum wait for a VM. Expiry answers 503 with `Retry-After: 1`, without starting or replacing a VM. Separate from `ssr.timeout`, which starts after a VM is acquired |
| `cache.size` | `64MiB` | `--cache-size`, `TIFA_CACHE_SIZE` | Size of the HTML cache of pages with `Cache = true`, counting the gzip and zstd forms it keeps with `server.compress` on. `0` turns it off, though requests that miss the same page at the same time still share one render (see [SSR and cache](app.md#ssr-and-cache)) |

## Render queue

When every VM is busy, at most `ssr.queue_limit` renders wait for up to `ssr.wait_timeout`. The first limit reached rejects the render. The default is 32 waiting renders and a 1 second wait. `ssr.timeout` still limits only execution in a VM, to 3 seconds by default. A client cancel or pool shutdown also ends a wait.

Overload answers use a small HTML page written by Go, status 503, `Retry-After: 1` and `Cache-Control: no-store`, without an ETag. They are not cached, rendered again or sent to `OnError`. `OnRequest` can handle the error returned by `Next`. A render of a 404 or an error page uses the same limits. An actual render failure or render timeout still goes to `OnError`.

App code can match a rejected render with `errors.Is(err, tifa.ErrSSRBusy)`, including wrapped errors. For example, record overload in `OnRequest`:

```go
import (
    "errors"
    "log"

    "github.com/liustack/tifa"
)

func OnRequest(c *tifa.Ctx) error {
    err := c.Next()
    if errors.Is(err, tifa.ErrSSRBusy) {
        log.Printf("SSR queue busy: %s", c.Request().URL.Path)
    }
    return err
}
```

Returning the original error keeps the default 503 and its headers. Other render failures do not match `tifa.ErrSSRBusy`.

The limits apply only when a render needs a VM. Cache hits, 304 responses, navigation data, API routes and pages with SSR off do not wait for one. Requests sharing a cache miss count as one render. `Load` runs before admission, and shared requests still hold their own HTTP state. A queue limit bounds render jobs, not total process memory or all HTTP requests. See [Overload](performance.md#overload) for sizing and measurements.

## HTTPS

HTTPS is off by default, and the program serves plain HTTP on `server.addr` as it always did. Leave it off behind a CDN or a proxy that ends TLS, such as Cloudflare, nginx or Caddy: they already speak HTTPS and HTTP/2 to browsers, and they would be the only ones to see a certificate of the program.

Turn it on when the program faces the internet by itself. Browsers then also get HTTP/2, which they speak only over TLS: every file of a page shares one connection, where over HTTP/1.1 a browser opens at most 6 to a host, see [Preloading the chunks of a page](build.md#preloading-the-chunks-of-a-page). There are two ways, and setting both fails. With either, set `server.addr`: with HTTPS on, the default `127.0.0.1:9527` refuses to start, since only this machine reaches it. Writing it out in the config is the same. Another loopback address, such as `127.0.0.1:8443`, starts with a warning in the log, for a test on this machine.

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

- Compression, the `.gz` files of the build, the HTML cache, ETags, redirects and server-sent events work as over HTTP/1.1, and a flush sends what was written at once.
- `Hijack` fails with `http.ErrNotSupported` on an HTTP/2 connection, since that connection carries other requests too. Unless `GODEBUG=http2xconnect=1` is set, the server does not offer WebSockets over HTTP/2, so browsers open them over a separate HTTP/1.1 connection, where `Hijack` works.
- Over HTTPS, read what compression may leak in [Compression](build.md#compression).

`tifa dev` always serves plain HTTP and ignores these settings, also when the project file sets them.

## Stack depth

With the default `ssr.vm_stack_depth` of 10000, plain recursion goes as deep in a render as in Node, which stops at about 10,400 calls. Each level takes room on the stack of the Go goroutine that renders, not in the memory `ssr.vm_memory` limits. A normal page needs a few hundred levels and a few MB of stack. Code that recurses without end goes all the way down before it throws, and the stack of that render then grows to about 16 MB. Go takes it back at the next garbage collection. The stack grows by doubling, and when a garbage collection is running as it grows, the old stack is freed only once the collection ends, so for a moment the render can hold about 24 MB.

Every VM can do this at the same moment, so the worst case is `ssr.vms` times 24 MB on top of the memory of the VMs. On a small machine, lower the depth. Measured on darwin/arm64 and linux/amd64, where a level takes about 1 KB:

| `ssr.vm_stack_depth` | Stack of a render that recurses without end |
| --- | --- |
| `10000` | about 16 MB, up to about 24 MB while it grows |
| `5000` | about 8 MB |
| `2000` | about 2 MB |
| `1000` | about 1 MB |

On Linux, each level also keeps up to about 2 KB of memory the engine uses for its own local variables, so a VM that once recursed 10000 levels deep holds about 20 MB more. This memory is not in `ssr.vm_memory` either, and the VM keeps it until it closes. A render that fails closes the VM and starts a new one, so the memory comes back then. When the page catches the `RangeError` and the render succeeds, the VM stays and keeps it.

What a lower depth breaks:

- Plain recursive code that goes deeper than the limit, such as a function walking a tree of comments or categories, in your code or in a library. It throws the `RangeError`, and the render fails unless the code catches it.
- `JSON.parse` of data nested deeper than the limit, the data Go hands to the render included. Node has no such limit.
- Not React: when a deep component tree hits the limit, React renders the rest of that tree from a fresh stack. Only far below 1000 do pages fail, with odd errors from libraries that ran out of stack halfway through their work, and with a few dozen the bundle fails to load when the program starts. Every page of the example shop still renders at 200.

`2000` keeps such pages working and holds a render that recurses without end to about 2 MB.

## Value formats

- Sizes are whole numbers with an optional unit: `B`, `KiB`, `MiB` or `GiB`, such as `512KiB`. A plain number is bytes. Only `cache.size` may be 0.
- Durations use Go's format and must be positive: `500ms`, `3s`, `1m30s`.
- `ssr.vms` is a whole number of at least 1.
- `ssr.queue_limit` is a whole number of at least 0. `ssr.wait_timeout` must be positive even with a queue limit of 0.
- `ssr.vm_stack_depth` is a whole number from 1 to 10000. A deeper stack would let code that recurses without end grow the goroutine stack past Go's limit, and the whole process would exit.
- `build.skip_tanstack_check`, `build.debug_info`, `build.i18next_cache`, `server.compress` and `server.hsts` are `true` or `false`, without quotes. The flag takes its value after `=`: `--server-compress=false`.
- `server.tls_domains` is a comma-separated list of host names, without scheme, port or path. Each needs a dot. `server.tls_ca` is an `https` URL. `server.http_addr` is an address with a port, such as `:80` or `0.0.0.0:80`, or `off` in lower case. Words that read as a switch, such as `OFF`, `false` or `none`, fail with what to write.
- `server.tls_domains`, `server.tls_email`, `server.tls_cache`, `server.tls_cert` and `server.tls_key` may be empty, which turns off what a lower source set: `TIFA_SERVER_TLS_DOMAINS=` with `--server-tls-cert` serves your own certificate where the project file asks for Let's Encrypt.
- `build.pgo` is `auto`, `off` or a path relative to the app root. YAML reads a plain `off` as text, so it needs no quotes. Words that read as a switch, such as `false`, `OFF` or `yes`, fail with what to write instead, and so does an absolute path.

## Errors

`tifa build` fails on a bad project file. The program refuses to start and names the source and the key when:

- a file or variable names an unknown key, such as `TIFA_SERVER_PORT`,
- a value has the wrong type or format, such as `ssr.timeout: 3`,
- a deployment file sets a `build.*` key. There are no `build.*` flags or `TIFA_BUILD_*` variables.

It also refuses to start, before `OnStart`, and names the settings when:

- HTTPS is on and `server.addr` is the default `127.0.0.1:9527`, left out or written out,
- the HTTPS settings do not go together once all sources are merged: `tls_domains` with `tls_cert`, `tls_cert` without `tls_key` or the other way round, `tls_domains` without `tls_cache`, or `tls_email` or `tls_cache` without `tls_domains`. The project file alone may leave out what the deployment adds, so `tifa build` does not check this,
- the files of `tls_cert` and `tls_key` do not load, or `tls_cache` cannot be created.

When `server.addr` or `server.http_addr` cannot be listened on, the error names the setting, and for a port only root may take it says what to do.

`serve --dist <folder>` is a flag for [`tifa dev`](dev.md) only. It is not a setting and has no environment variable.
