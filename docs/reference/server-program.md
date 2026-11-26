---
summary: 'The generated program: its commands (`serve`, `config`, `version`), where settings come from, and compression of answers'
audience: app
order: 14
read_when:
  - 'Finding out which commands the built program has, or how it compresses answers'
---

# The generated program

`server.Main(app)` builds the command line:

| Command | What it does |
| --- | --- |
| `serve` (default) | Loads the config, sets up [HTTPS](../build-and-deploy/https.md#https) when it is on, runs `OnStart`, listens on `server.addr` (and with HTTPS on `server.http_addr`), and on SIGINT or SIGTERM shuts down gracefully and runs `OnShutdown`. Both addresses stop accepting and finish their requests together |
| `config` | Prints every setting, of Tifa and of the app, with its value and where the value comes from, as a commented YAML file a deployment can pass with `-c` |
| `version` | Prints the versions of the app module, the Tifa module and Go |

Settings come from, highest first: command-line flags, environment variables, the `.env` file of the working folder, the file given with `-c` (or `--config`), the values of `tifa.yaml` built into the program, and the defaults. The program does not read `tifa.yaml` itself. A flag that is no setting is an error. Variables and file keys that are no setting are left alone. See [Configuration](../build-and-deploy/config.md#where-settings-come-from).

In the VMs the local time of `Date` is always UTC, whatever zone the program was started in, so a page renders the same on every machine. The process's zone is left alone: `time.Local` in the app's Go code is the zone of the machine or of `TZ`, as in any Go program. Intl and Temporal in the VMs use the time zone data built into beastjs and never read the system's zoneinfo. The generated `main.go` imports `time/tzdata`, so `time.LoadLocation` in the app's Go code finds every time zone even where the system has no zoneinfo, as in a `scratch` container. It makes the program about 450 KB larger.

`serve` also has a dev flag, `--dist <dir>`: it reads the frontend output from that folder instead of the embedded `dist/`. [`tifa dev`](../build-and-deploy/dev.md) points it at `.tifa/dist`. After a rebuild of the SSR bundle alone, the dev program (`server.DevMain`) loads the bundle again from there without a restart, and after other rebuilds `tifa dev` restarts it. It is not a setting and has no environment variable. At startup the program reads `ssr.js`, compiles it and prewarms the VM pool, and reads `build.json` for the build ID and the head tags.

## Compression

With [`server.compress`](settings.md#settings) on, the default, answers go out compressed to clients that accept it, so a Tifa program needs no proxy for it:

- Pages, navigation data, JSON and other text are compressed as they are sent, with gzip or zstd at the default level of each, as Caddy and Traefik do, by gzhttp of [klauspost/compress](https://github.com/klauspost/compress). The client's `Accept-Encoding` picks: gzip whenever the client takes it, and zstd only when the client takes no gzip or ranks zstd higher by `q`. A browser that sends `gzip, deflate, br, zstd` gets gzip. An answer is compressed from 1024 bytes on, and only for text types: `text/*` but server-sent events (`text/event-stream`, see [Streaming](../guides/streaming.md)), JavaScript, JSON, XML, the `+json` and `+xml` types, WebAssembly, icons and fonts in TTF, OTF or EOT. HEAD, an answer that already has a `Content-Encoding` and a `Content-Range` answer go out as they are.
- A file of the browser bundle goes out as the `.gz` file of the build to a GET that accepts gzip, with the type and cache of the file itself, as nginx's `gzip_static` does. A Range request gets a range of the `.gz` file. A file without a `.gz` is compressed as it is sent when its type is in the list above. A `.gz` file in the public folder is a file of its own and is not sent in place of another.
- A page of the render memo is compressed once, when it is made, and the memo keeps that gzip form alone, so a page of the example shop takes about a fifth of its HTML and `ssr.memo_size` counts exactly the bytes it keeps. A hit for a client that takes gzip sends the stored bytes with their `Content-Length`. A client that takes no compression (no `Accept-Encoding` for gzip or zstd, HEAD or `No-Gzip-Compression`) gets the HTML, unzipped as it is written, with the `Content-Length` of the HTML. A client that takes zstd and no gzip, or ranks zstd higher, gets zstd made from the stored page as it is written, in chunks. Nothing else is kept, so a hit never compresses and never holds a second copy. A page under 1024 bytes, one that gzip does not make smaller and every page with `server.compress` off are kept as HTML. A page that does not fit in `ssr.memo_size` even in that form, as with `ssr.memo_size` 0, is compressed as it is sent.
- Every answer carries `Vary: Accept-Encoding`, uncompressed ones too, also when the app sets `Vary` itself.
- A strong ETag of a compressed answer, a public file's included, turns weak (`W/"…"`), as nginx does, since a strong ETag promises the same bytes. `If-None-Match` compares weakly, so a 304 keeps working. The ETag of a page is weak from the start, in every encoding and in its 304, which is answered before the encoding is known.
- Brotli is not used. At levels 4 and 5 its pure Go encoder takes 4 to 6 times as long as gzip for pages 3% to 16% smaller.
- `tifa dev` compresses its pages the same way. Its browser bundle comes from Vite's dev server.

Compression can leak a secret through the length of an answer (the BREACH attack). The risk is there when an answer goes out over HTTPS and the same body both repeats something from the request, such as a search term, and carries a secret of the user, such as a CSRF token. Send such an answer uncompressed by setting the `No-Gzip-Compression` header on it, which is removed before the answer goes out, or turn `server.compress` off.

Turn it off when a proxy in front compresses, such as nginx with brotli: a proxy leaves an answer that already has a `Content-Encoding` alone. [Performance](performance.md#compression) has what compressing costs.
