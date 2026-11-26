---
summary: '找出 `tifa dev` 报错的原因：日志、失败的构建、浏览器里的错误和渲染失败的页面'
audience: app
order: 4
translatedFrom: 297860b03e5f
read_when:
  - '读 `tifa dev` 的日志，或者找出在它下面某次构建、请求或渲染为什么失败'
---

# 找出 `tifa dev` 报错的原因 {#find-out-why-tifa-dev-shows-an-error}

`tifa dev` 会在日志里、运行它的终端里和浏览器里告诉你哪里出了错。这一篇说该去哪里看。

## 日志 {#log}

每个运行的步骤都会打印一行，带上耗时。最后一行是从重建开始到应用就绪的总时间：

```text
tifa dev: 07:35:36 app/home/page.tsx modified
tifa dev: 07:35:36 bundle done in 90ms
tifa dev: 07:35:36 ready in 260ms
```

运行超过 5 秒的步骤会说明这一点，之后每 15 秒再说一次。应用的第一次 go build 要编译 beastjs、SQLite 和其他依赖，在 Apple M4 上构建缓存为空时大约 7 秒，机器慢些就更久，之后的构建会用 Go 的构建缓存。第一条 ready 行还会写出要打开的地址：

```text
tifa dev: 07:35:41 go build still running (5s). The first build compiles beastjs and the other dependencies, later builds use Go's build cache
tifa dev: 07:35:56 go build still running (20s)
tifa dev: 07:36:30 go build done in 54.1s
tifa dev: 07:36:30 ready in 58.3s, open http://127.0.0.1:9527
```

浏览器在第一次构建期间打开的页面（接受 `text/html` 的 GET 或 HEAD）会等它 2 秒。如果构建还在跑，页面会返回 503，并说明 `tifa dev` 正在构建应用，应用跑起来后会自己重新加载。其他请求，比如脚本调用 API，会像应用重启时那样等应用。

步骤的名字是 `static analysis`、`code generation`、`bundle`、`styles`、`content` 和 `go build`。静态分析之后还有一行 `route manifest changed` 或 `route manifest unchanged`。Vite 的警告和错误也会打印成 `tifa dev:` 行。应用自己的输出照常打印，但没有构建出来的程序那行 `tifa: listening on`，因为应用监听的端口只有 `tifa dev` 会连。它的 stderr 经过 `tifa dev`，`tifa dev` 会留下最后几行用于覆盖层（见下），所以只在终端里才给输出上色的日志库，在这里写的是纯文本。

在 `tifa dev` 下，应用还会在每个路由请求答完之后写一行：方法、URL、状态码、耗时、路由，以及[渲染记忆](../pages-and-data/ssr-and-cache.zh-CN.md#ssr-and-cache)做了什么：

```text
2026/11/26 08:00:00 INFO GET /projects/studio 200 38ms /projects/{project} memo miss
2026/11/26 08:00:01 INFO GET /projects/studio 200 1ms /projects/{project} memo hit
2026/11/26 08:00:03 INFO GET /projects/studio?page=2 200 9ms /projects/{project} data
2026/11/26 08:00:04 INFO POST /api/orders 201 4ms /api/orders
```

`memo miss` 是在 VM 里渲染的页面，`memo hit` 是记忆里已有的页面，`memo shared` 是等了另一个请求已经开始的渲染的页面，`memo off` 是记忆不保存的页面（`Memo = false`、404 或错误页）。`data` 是客户端跳转请求的导航数据，它不渲染任何东西。路由是页面或处理函数的模板，没有路由匹配时就没有这一项。浏览器包的文件、`public/` 里的文件、`tifa dev` 自己的请求和 Vite 的模块都没有这一行。构建出的程序不写这些行，`tifa dev` 也不给响应加任何头：memo 那个词读自页面本来就带着的 `X-Tifa-Render`。想在构建出的程序里记请求，见[记录每个请求](../guides/logs.zh-CN.md#logging-each-request)。

当一次渲染不带 locale 调用 Intl 或 `toLocale*` 方法，或者对日期不带 `timeZone` 时，应用会记一条 `tifa: ssr:` 警告，写出你源码里的位置，每个位置一次。页面照常渲染。见 [`tifa dev` 里的警告](../pages-and-data/server-render.zh-CN.md#warnings-in-tifa-dev)。

`tifa dev` 把每个页面渲染两次，第二次把时钟拨快 36 小时。当同样的数据两次渲染结果不同，说明页面在渲染时读了时间或随机数，渲染记忆 memo 会把这个值冻住。终端和页面上的覆盖层都会指出这一点，附上两次渲染最先分开的位置周围的 HTML，以及你代码里读时间（`Date.now()`、`new Date()`、`Date()`）或随机数（`Math.random()`、`crypto`）的位置。包内部的读取会记在调用它们的那一行你的代码上，React、TanStack Router 和 Tifa 的读取不列出。请修改组件，或者在该页面的 `route.go` 里写 `Memo = false`（见 [SSR 与缓存](../pages-and-data/ssr-and-cache.zh-CN.md#ssr-and-cache)）。这样的页面在修好之前不会被缓存。构建出来的程序不会渲染两次。

在 `tifa dev` 下，页面在服务器上渲染时用 `console.log` 和其他 console 方法打印的内容也会被记录，带着页面和它在你源码中的位置。见 [`tifa dev` 里的 console](../pages-and-data/server-render.zh-CN.md#console-in-tifa-dev)。想单步调试 Go 代码或某个组件，见[调试](debugging.zh-CN.md)。

## 构建失败时 {#when-a-build-fails}

- 旧版本继续运行。日志会打印 `build failed: <reason>`，已打开的页面会出现错误覆盖层，显示 Go 编译器或 Vite 的原始消息。
- 修好之后覆盖层消失。如果修复需要 go build，页面会重新加载，否则不会。失败的构建没做完的步骤，会并入下一次重建。
- 在浏览器里编译失败的模块会显示 Vite 自己的覆盖层。
- 如果安装的 TanStack Router 不是 Tifa 测试过的版本（见 [生成的文件和应用的配置](../reference/generated-files.zh-CN.md#the-apps-tsconfig-and-vite-config)），bundle 步骤会带着那条错误失败。`tifa dev` 不监视 `node_modules`，所以装好测试过的版本之后，要重启 `tifa dev`。
- 如果安装的 React 或 React DOM 低于 19.3.0（见 [生成的文件和应用的配置](../reference/generated-files.zh-CN.md#the-apps-tsconfig-and-vite-config)），bundle 步骤会带着那条错误失败。装上更新的版本后要重启 `tifa dev`。
- 如果最开始的第一次构建就失败，就没有旧版本可以提供。每个请求都得到 502 和原始错误，修好之后会自己恢复。
- 如果应用启动后马上退出，或者 30 秒内没有监听，覆盖层的标题是 `The app did not start`。正文显示退出状态和应用写到 stderr 的最后 40 行，`OnStart` 的错误和 panic 都在那里。只要输出以那个 panic 的报告结尾，panic 那一行即使堆栈更长也会保留。应用记下日志后继续运行的 `panic: ` 行则不会。如果运行中的应用退出了（比如请求之外发生了 panic），标题是 `The app stopped`，正文相同。到下一次构建之前，页面请求会得到 502 和同样的文字。`tifa dev` 的日志只说应用退出了，因为应用自己的输出已经打印在它上面。
- 如果运行中的应用无法加载重建后的 SSR 包（比如 beastjs 编译不了，或者它的顶层代码抛了错），覆盖层标题是 `The app did not load the SSR bundle`。应用继续使用它已有的包。堆栈给出源文件里的位置，调用了 beastjs 没有的 `require` 的包，会连同该怎么做一起指出来。

你可以关掉覆盖层。关掉它不影响以后的重新加载。

## 浏览器里的错误 {#errors-in-the-browser}

浏览器里发生的错误会显示在右下角的一个小面板里。它不妨碍页面使用，构建错误的覆盖层会盖住它并保持在最上面。

- `Hydration mismatch`：React 没法复用服务器的 HTML，在浏览器里把页面重新渲染了一遍。面板显示 React 的消息，带上差异，以及组件堆栈。React 自己能恢复的其他错误，标题是 `Recoverable error`。
- `Uncaught error`：某个组件抛了错，而它外面没有错误边界，或者其他错误冒到了 `window`。
- `Unhandled rejection`：某个 promise 失败了，没有人处理。

同样的错误再次出现时，只会增加计数（`×3`），不会新增条目。面板保留最近 20 个不同的错误。每个都可以关掉，重新加载会清空它们。被 `ErrorBoundary` 组件捕获的错误不显示，和以前一样它们只会到控制台。`tifa dev` 为 [Tifa 错误](dev-scripts.zh-CN.md#tifa-errors)保存同样的错误。构建出来的应用没有这些：这些代码只在开发构建里。

## 页面渲染失败时 {#when-a-page-fails-to-render}

服务端渲染页面时如果组件抛了错，应用会把页面重新渲染一遍，用 `ErrorBoundary` 组件显示错误（见[页面上的错误](../pages-and-data/errors.zh-CN.md#errors-on-a-page)）。在 `tifa dev` 下，这个页面上会有一个覆盖层，写出这个请求，在公开消息和状态文字不同时显示公开消息，并显示完整的错误，堆栈指向你的源文件。如果没有 `ErrorBoundary` 渲染，或者页面请求在应用能渲染页面之前就失败了（比如 `OnRequest` 返回了意料之外的错误），应用会用 Go 返回一个朴素的页面，在 `tifa dev` 下它显示同样的内容。两种页面都带着 `OnError` 选的状态码，所以 `curl -i` 也能看到，并且在你保存修复之后重新加载，用 TSX 做的、不会重新加载其他页面的修复也一样。构建出来的应用只显示状态和公开消息。

这些页面会显示内部错误。`tifa dev` 默认监听 `127.0.0.1`。用 `--addr 0.0.0.0:<port>` 时，网络上的其他机器能看到它们。

重新加载和覆盖层走的是 `tifa dev` 自己的连接 `/__tifa/dev/events`，不是 Vite 的 HMR websocket。Vite 的开发服务器只在第一次 bundle 步骤里启动，在静态分析和代码生成之后，没有页面的应用则从不启动它。没有应用在运行时回答请求的那个页面（第一次构建失败或崩溃之后），也必须显示错误，并在修好后重新加载，而这只有靠 `tifa dev` 自己从一开始就提供的连接才做得到。浏览器的所有标签页通过一个 SharedWorker 共用这个连接。
