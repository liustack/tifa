---
summary: '生成的程序：它的命令（`serve`、`config`、`version`），设置的来源，以及答复的压缩'
audience: app
order: 14
translatedFrom: 8a99619fd606
read_when:
  - '想知道构建出的程序有哪些命令，或者它怎样压缩答复'
---

# 生成的程序 {#the-generated-program}

`server.Main(app)` 构建命令行：

| 命令 | 作用 |
| --- | --- |
| `serve`（默认） | 加载配置，开启时设置 [HTTPS](../build-and-deploy/https.zh-CN.md#https)，运行 `OnStart`，监听 `server.addr`（开启 HTTPS 时还有 `server.http_addr`），并在 SIGINT 或 SIGTERM 时优雅地关闭并运行 `OnShutdown`。两个地址会一起停止接受连接，并一起完成它们的请求 |
| `config` | 打印每一个设置，包括 Tifa 的和应用的，带它的值和这个值的来源，输出为带注释的 YAML 文件，部署时可以用 `-c` 传入 |
| `version` | 打印应用模块、Tifa 模块和 Go 的版本 |

设置的来源，按优先级从高到低：命令行参数，环境变量，工作目录的 `.env` 文件，用 `-c`（或 `--config`）给出的文件，构建进程序的 `tifa.yaml` 的值，以及默认值。程序自己不读 `tifa.yaml`。不是设置的参数是错误。不是设置的变量和文件键会被放着不管。见[配置](../build-and-deploy/config.zh-CN.md#where-settings-come-from)。

在 VM 里，`Date` 的本地时间总是 UTC，不管程序是在哪个时区启动的，所以页面在每台机器上渲染的结果都相同。进程的时区不动：应用的 Go 代码里的 `time.Local` 是机器的时区或 `TZ`，和任何 Go 程序一样。VM 里的 Intl 和 Temporal 使用 beastjs 内置的时区数据，从不读系统的 zoneinfo。生成的 `main.go` 导入了 `time/tzdata`，所以应用的 Go 代码里的 `time.LoadLocation` 即使在系统没有 zoneinfo 的地方，比如 `scratch` 容器里，也能找到每个时区。它让程序大了约 450 KB。

`serve` 还有一个开发用的参数 `--dist <dir>`：它从那个文件夹读取前端输出，而不是嵌入的 `dist/`。[`tifa dev`](../build-and-deploy/dev.zh-CN.md) 把它指向 `.tifa/dist`。只重新构建 SSR 包之后，开发用的程序（`server.DevMain`）会从那里重新加载这个包，不用重启，在其他的重新构建之后，`tifa dev` 会重启它。它不是设置，也没有环境变量。启动时，程序读取 `ssr.js`，编译它并预热 VM 池，并读取 `build.json` 来得到构建 ID 和 head 标签。

## 压缩 {#compression}

当 [`server.compress`](settings.zh-CN.md#settings) 开启（默认）时，响应会压缩后发给接受压缩的客户端，所以 Tifa 程序不需要代理来做这件事：

- 页面、导航数据、JSON 和其他文本在发送时被压缩，用 gzip 或 zstd，各用它的默认级别，和 Caddy 和 Traefik 一样，由 [klauspost/compress](https://github.com/klauspost/compress) 的 gzhttp 完成。由客户端的 `Accept-Encoding` 决定：只要客户端接受 gzip 就用 gzip，只有当客户端不接受 gzip，或把 zstd 的 `q` 排得更高时才用 zstd。发送 `gzip, deflate, br, zstd` 的浏览器得到 gzip。响应从 1024 字节起才压缩，并且只压缩文本类型：除服务器发送事件（`text/event-stream`，见[流式传输](../guides/streaming.zh-CN.md)）之外的 `text/*`、JavaScript、JSON、XML、`+json` 和 `+xml` 类型、WebAssembly、图标，以及 TTF、OTF 或 EOT 的字体。HEAD、已经带 `Content-Encoding` 的响应，以及 `Content-Range` 响应，原样发出。
- 浏览器包里的文件，对接受 gzip 的 GET，以构建出的 `.gz` 文件发出，带着文件本身的类型和缓存，和 nginx 的 `gzip_static` 做的一样。Range 请求得到 `.gz` 文件的一个范围。没有 `.gz` 的文件，当它的类型在上面的列表里时，会在发送时压缩。public 文件夹里的 `.gz` 文件是它自己的文件，不会替代另一个文件被发送。
- 渲染 memo 里的页面在做出来的时候压缩一次，memo 只保存这份 gzip 形式，所以示例商店的一个页面只占它的 HTML 的约五分之一，`ssr.memo_size` 精确地统计它保存的字节数。对接受 gzip 的客户端，一次命中就发送存着的字节，带它们的 `Content-Length`。不接受压缩的客户端（没有针对 gzip 或 zstd 的 `Accept-Encoding`，HEAD 或 `No-Gzip-Compression`）得到的是未压缩的 HTML，就是它被写出来的样子，带 HTML 的 `Content-Length`。接受 zstd 但不接受 gzip，或把 zstd 排得更高的客户端，得到由存着的页面按它被写出来的样子分块做出的 zstd。没有保存别的东西，所以命中从不压缩，也从不持有第二份副本。不到 1024 字节的页面、gzip 不能让它变小的页面，以及 `server.compress` 关闭时的每个页面，都按 HTML 保存。即使按那种形式也放不进 `ssr.memo_size` 的页面，比如 `ssr.memo_size` 为 0 时，在发送时压缩。
- 每个响应都带 `Vary: Accept-Encoding`，未压缩的也一样，应用自己设置了 `Vary` 时也一样。
- 压缩后的响应的强 ETag，包括 public 文件的，会变成弱的（`W/"…"`），和 nginx 做的一样，因为强 ETag 承诺字节完全相同。`If-None-Match` 做弱比较，所以 304 仍然有效。页面的 ETag 从一开始就是弱的，在每种编码里，在它的 304 里也是，而 304 是在编码还不知道的时候回答的。
- 不使用 Brotli。在级别 4 和 5 时，它的纯 Go 编码器耗时是 gzip 的 4 到 6 倍，换来的页面只小 3% 到 16%。
- `tifa dev` 以同样的方式压缩它的页面。它的浏览器包来自 Vite 的开发服务器。

压缩可能通过响应的长度泄漏秘密（BREACH 攻击）。当响应通过 HTTPS 发出，并且同一个响应体既重复了请求里的某些东西（比如搜索词），又带着用户的秘密（比如 CSRF 令牌）时，就有这个风险。请给这样的响应设置 `No-Gzip-Compression` 头，让它不压缩发出，这个头会在响应发出之前被移除，或者关闭 `server.compress`。

当前面的代理做压缩时，比如带 brotli 的 nginx，请关闭它：代理对已经带 `Content-Encoding` 的响应不会去动。压缩的代价见[性能](performance.zh-CN.md#compression)。
