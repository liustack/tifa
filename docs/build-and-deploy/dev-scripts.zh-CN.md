---
summary: '在脚本或编程 agent 里运行 `tifa dev`：`--background`、`status`、`logs`、`stop` 和 `tifa errors`'
audience: app
order: 5
translatedFrom: b359d9934d95
read_when:
  - '在脚本或编程 agent 里检查一次改动有没有弄坏构建、某个请求或浏览器里的某个页面，或者修改 `tifa errors`'
  - '在脚本或编程 agent 里启动、观察或停止 `tifa dev`，或者修改 `tifa dev --background`、`status`、`logs` 或 `stop`'
---

# 在脚本或编程 agent 里运行 `tifa dev` {#run-tifa-dev-from-a-script-or-a-coding-agent}

脚本或编程 agent 没有终端可看。这些命令让 `tifa dev` 在后台运行，并告诉你改动有没有成功。

## 智能体与脚本 {#agents-and-scripts}

```sh
go tool tifa dev --background    # start, and return once the app answers
go tool tifa dev status [--json] # is it running, where, and did the last build work
go tool tifa dev logs [-n 100] [-f]
go tool tifa dev stop
```

`status`、`logs` 和 `stop` 也接受 `--dir app-root`。

编码智能体或脚本没法让 `tifa dev` 一直开在终端里。`tifa dev --background` 会让它脱离终端启动，在构建结束且 `GET /` 有响应后返回。它打印地址和进程 ID，然后是怎么看日志、怎么停止：

```text
tifa dev: ready at http://127.0.0.1:9527 (pid 41231)
  log:    go tool tifa dev logs
  status: go tool tifa dev status
  stop:   go tool tifa dev stop
```

关闭终端不会停掉它。`--background` 是唯一的开关：Tifa 不会去探测智能体，普通的 `tifa dev` 仍然留在前台。

告诉智能体这些命令的 skill 和说明，由 [`tifa agents`](upgrade.zh-CN.md#tifa-agents) 写出。

- 命令的其他每个参数和每个环境变量都原样传给服务器，包括 `--dir` 和 `--addr`。
- 服务器的所有输出都进 `.tifa/dev.log`：`tifa dev:` 行、应用的输出和 Vite 的消息。新的 `tifa dev --background` 会让这个文件重新开始。`tifa gen` 和 `tifa build` 不动它。
- 命令最多等 5 分钟。在服务器先结束（会附上它日志的结尾）、第一次构建失败，或者 `GET /` 返回 5xx 状态时，它以状态 1 退出并说明原因。第一次构建失败之后，服务器继续运行，你保存文件时它会再构建，所以先修代码，再运行 `tifa errors` 或 `tifa dev status`。`tifa dev stop` 会停掉它。
- 第二个 `tifa dev`，不管在前台还是带 `--background`，都和以前一样失败。它的消息会写出正在运行的那个的地址和进程 ID，并说明用 `tifa dev status` 和 `tifa dev stop`。

### 状态 {#status}

`tifa dev status` 会说明应用里有没有 `tifa dev` 在运行，它的地址、进程 ID、启动时间，以及上一次构建的状态。有一个在运行时它以状态 0 退出，没有时以 1 退出。

```text
tifa dev is running in this app
  url:     http://127.0.0.1:9527
  pid:     41231
  started: 2026-11-16 22:22:50 (2m3s ago)
  app pid: 41290
  build:   failed: go build: app/store.go:3:2: undefined: x
  log:     /work/shop/.tifa/dev.log
```

`app pid` 是 `tifa dev` 所运行的应用的进程 ID，调试器要附加到它上面（见[调试](debugging.zh-CN.md)）。每次因为改了 Go 代码而重新启动应用，它都会变，没有应用在运行时则不显示。`--json` 打印一个对象。`build.state` 是 `starting`（第一次构建在跑）、`building`、`ok`、`failed`（这时 `build.error` 是错误的第一行）或 `unknown`（服务器没有响应，`build.error` 说明原因）。对于打印到自己终端的 `tifa dev`，`log` 为空。没有运行时它打印 `{"running": false}`。

```json
{
  "running": true,
  "pid": 41231,
  "url": "http://127.0.0.1:9527",
  "started": "2026-11-16T14:22:50Z",
  "build": {"state": "ok"},
  "appPid": 41290,
  "log": "/work/shop/.tifa/dev.log"
}
```

### 日志 {#logs}

`tifa dev logs` 打印 `.tifa/dev.log` 的最后 100 行，用 `-n <lines>` 可以指定行数。带 `-f` 时，它接着在新行出现时打印，并在 `tifa dev` 结束时结束。服务器结束后日志仍在，所以 `tifa dev logs` 也能告诉你它为什么停了。没有用 `--background` 启动的 `tifa dev` 打印到它的终端，不留日志：`logs` 会这样告诉你。应用在这里写什么，以及怎样设置它的格式和级别，见[日志](../guides/logs.zh-CN.md)。

### 停止 {#stop}

`tifa dev stop` 请服务器像按 Ctrl-C 那样停止，并在它消失、锁被释放后返回（应用最多得到 3 倍的 `server.shutdown_timeout` 再加 1 秒）。没有 `tifa dev` 在运行时它也以状态 0 退出，所以脚本可以用它来确保没有 `tifa dev` 在跑。它 60 秒后放弃并说明这一点，不会杀掉任何东西。

### 它怎么判断有什么在运行 {#how-it-tells-what-runs}

`tifa dev` 在持有锁 `.tifa/dev.lock` 时就算在运行，进程结束时系统会释放这把锁，不管它是怎么结束的。这个文件还记录进程 ID、地址和启动时间，但已结束的服务器留下的记录什么也证明不了，它的进程 ID 现在可能属于另一个程序。所以 `status` 和 `stop` 先问锁：有文件但没有持有者，就是 `not running`，什么也不会发信号。

`stop` 也不会单凭记录里的进程 ID 就信。只有在记录的地址上的程序在 `/__tifa/dev/status` 响应了，并且 `X-Tifa-Dev-Pid` 里是同一个进程 ID 之后，它才发信号。持有者如果不这样响应，比如新持有者还没写好记录，就不动它：命令会这样说明，并以状态 1 退出。它从不碰 `tifa build`，后者用的是同一把锁。

## Tifa 错误 {#tifa-errors}

```sh
tifa errors [--dir app-root] [--json]
```

`tifa dev` 运行期间，`go tool tifa errors` 会打印上一次构建开始以来的错误。既看不到浏览器、也看不到 `tifa dev` 终端的编码智能体，靠它得知改动有没有用。改完代码之后，以及打开或重新加载你改过的页面之后，都要运行它：浏览器的错误只来自已打开的页面。

它列出：

- 覆盖层显示的内容：`Build failed`，带 Go 编译器或 Vite 的原始消息，还有 `The app did not start`、`The app stopped` 或 `The app did not load the SSR bundle`。它保留到下一次构建结束。
- 请求的意外错误，也就是交给 `OnError` 的那些（没有 `OnError` 时则按 500 记入日志）：请求、状态和用户看到的消息，以及错误本身，堆栈指向你的源文件，和 `tifa dev` 下错误页面显示的一样。API 路由的请求也算，没有页面的应用里同样如此。
- 页面报告的浏览器错误：`Hydration mismatch`、`Recoverable error`、`Uncaught error` 和 `Unhandled rejection`，名字和面板里一样，先列报告它的页面。
- 两次渲染不同的页面（见[日志](dev-errors.zh-CN.md#log)）。
- 上一次构建的警告，列在错误之后的 `Warnings` 下：来自分析的（比如把一个大于 2^53 的数放进页面会读的字段），来自对其他框架代码的检查的，来自内容的，以及来自 Vite 里 Tifa 插件的（比如读取 `import.meta.env.VITE_*`）。Vite 和 Rolldown 自己的警告只在 `tifa dev` 的日志里。它们不是错误，从不改变退出状态。一次保存修好某个警告，它就从列表里移除。

每一类保留最近 20 个不同的错误，同样的错误再次出现会增加它的计数，和面板里一样。构建开始时，之前的错误会被丢掉。在构建期间询问时，命令会等构建结束。

```text
Request failed: GET /orders?page=2, 503 Service Unavailable (×2)
  Users see: Come back later
  tifa: render page: ...

Hydration mismatch in the browser: http://127.0.0.1:9527/orders (×3)
  ...
```

没有错误时它打印 `No errors since the last build.` 没有错误时以状态 0 退出，有错误时以 1 退出，应用里没有 `tifa dev` 在运行时以 2 退出：`.tifa/dev.lock` 指向的进程已经不在了，它还没有开始监听，或者它的地址上没有东西替它回答。`--json` 以 JSON 打印同样的内容，每个字段都有，即使是空的：

```json
{
  "failure": {"title": "Build failed", "error": "..."},
  "requests": [{"method": "GET", "url": "/orders?page=2", "status": 503, "message": "Come back later", "error": "...", "count": 2}],
  "browser": [{"kind": "recoverable", "title": "Hydration mismatch", "url": "http://127.0.0.1:9527/orders", "text": "...", "count": 3}],
  "renders": [{"method": "GET", "url": "/stats", "text": "...", "count": 1}],
  "warnings": ["app/api/route.go:22:55: Mine.ID gets a value from time.Time.UnixNano, a number above 2^53. ... (warning)"]
}
```

一切都能构建和运行时，`failure` 是 `null`。`kind` 是 `recoverable`、`uncaught` 或 `rejection`。

命令读取 `tifa dev` 写进 `.tifa/dev.lock` 的进程 ID 和地址，不去拿锁，所以 `tifa dev` 或 `tifa build` 可以同时启动。它向那个地址请求 `/__tifa/dev/errors`，只有 `X-Tifa-Dev-Pid` 里写的是同一个进程时才采信答案。`tifa dev` 只在 `localhost` 或 IP 地址下回答那里，并且只接受来自它自己页面的浏览器报告，每条最多 128 KiB。应用为自己的错误回答 `tifa dev`。构建出来的应用没有这些。
