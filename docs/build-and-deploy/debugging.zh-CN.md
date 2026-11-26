---
summary: '在 tifa dev 下调试应用：通过 `tifa dev status` 里的进程 ID 用 Delve 调试 Go 一侧，在浏览器里调试 React 组件，以及服务端渲染会打印什么'
audience: app
order: 5
translatedFrom: 56a61d8d1893
read_when:
  - '`tifa dev` 运行时，用调试器单步调试路由、Load 或处理函数的 Go 代码'
  - '想知道组件在服务端渲染时做了什么，或者页面为什么在服务端渲染得不一样'
  - '找 `tifa dev status` 里的 `appPid`，或者 Delve 的断点一直不命中、看不到变量'
---

# 调试 {#debugging}

在 [`tifa dev`](dev.zh-CN.md) 下，有三个地方会运行你的代码，每个地方各有各的入口：Go 应用、浏览器里的 React 组件，以及服务端渲染里的 React 组件。

## Go：Delve {#go-delve}

`tifa dev` 把应用作为单独的进程运行，并保留它的调试信息，所以 [Delve](https://github.com/go-delve/delve) 可以附加到它上面。`tifa dev status` 把进程 ID 写成 `app pid`，加 `--json` 时写成 `appPid`。它是应用的进程 ID，不是 `tifa dev` 的，后者是它上面那一行的 `pid`：

```sh
go tool tifa dev --background
go tool tifa dev status --json   # "appPid": 41290
go run github.com/go-delve/delve/cmd/dlv@latest attach 41290
```

（也可以先用 `go install github.com/go-delve/delve/cmd/dlv@latest` 装一次，再运行 `dlv attach 41290`。）断点写成包路径加函数，或者文件加行号。打开页面，请求就会停在那里：

```text
(dlv) break demo/app/todos.ListTodos
(dlv) continue
> [Breakpoint 1] demo/app/todos.ListTodos() ./app/todos/todo.go:25
(dlv) bt 3
(dlv) print ctx
(dlv) continue
```

- 应用停住期间页面在等，浏览器在你 `continue` 之前什么也不显示。
- `go build` 会做优化，所以 Delve 会说 `debugging optimized function`，有的变量可能看不到。想不带优化地构建，就用 `GOFLAGS` 里的标志启动 `tifa dev`。引号是为了让中间的空格留在同一个标志里：

  ```sh
  GOFLAGS="'-gcflags=all=-N -l'" go tool tifa dev --background
  ```

- 改了 Go 文件会重新构建应用并启动一个新进程，调试器就丢了它。重新读一次 `appPid`，再附加一次。改 TSX 文件或样式表不会动这个进程：应用会原地加载新的包。
- 在 Delve 里 `exit` 会问要不要杀掉进程。回答 `n`，应用就继续运行。
- 用 `tifa build` 构建的程序没有调试信息，除非设置了 `build.debug_info`。见[调试信息](build.zh-CN.md#debug-info)。

## 浏览器里的 React {#react-in-the-browser}

浏览器从 Vite 的开发服务器取你页面的模块，带着 source map，所以浏览器的 Sources 面板里能看到你的 `.tsx` 文件，在组件里下的断点会停在你设的地方。React Developer Tools 能显示组件和它们的 props。`tifa dev` 会在页面上用一个覆盖层显示浏览器里的错误，`tifa errors` 会把它们打印出来（见[浏览器里的错误](dev.zh-CN.md#errors-in-the-browser)）。

## 服务端的 React {#react-on-the-server}

服务端渲染跑在一个没有调试器的 JavaScript VM 里，所以组件在那里渲染时，你没法给它下断点。在 `tifa dev` 下，组件用 `console.log` 打印的内容会进入应用的日志，带着页面和调用所在的行，`tifa dev logs` 会把它们打印出来：

```text
2026/11/26 08:00:00 INFO console.log: rendering about {"visits":3} path=/about at=app/about/page.tsx:3:11
2026/11/26 08:00:00 INFO GET /about 200 28ms /about memo miss
```

它下面那一行是 `tifa dev` 的请求行：路由，以及渲染记忆里有没有这个页面。打印了什么、怎么打印，见 [`tifa dev` 里的 console](../pages-and-data/server-render.zh-CN.md#console-in-tifa-dev)，请求行见[日志](dev.zh-CN.md#log)。
