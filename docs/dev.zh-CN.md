---
summary: '`tifa dev` 怎么用：每类改动后页面怎么更新、执行哪些步骤、Vite 在其中做什么、日志怎么读、构建失败时页面和应用怎样、浏览器里的错误怎么显示、`tifa errors` 怎么打印上次构建以来的错误、开发产物放在哪、大应用怎么监视'
read_when:
  - 第一次在本地跑 Tifa 应用
  - 改 internal/devserver、cmd/tifa 的 dev 命令或 internal/bundle 的开发构建
  - `tifa dev` 改了文件页面却没更新，或报出看不懂的错误
  - 想在脚本或编程 agent 里查改动有没有弄坏构建、请求或浏览器里的页面，或者要改 `tifa errors`
---

[English](dev.md) · 简体中文

# `tifa dev`

```sh
tifa dev [--dir 应用根目录] [--addr 127.0.0.1:9527]
```

启动后先打印 `tifa dev: serving http://<地址>`，然后做一次完整构建（步骤同 [`tifa build`](build.zh-CN.md)，应用的第一次构建要几秒到一分钟，见[日志](#日志)），启动应用。浏览器只连 `--addr`。`tifa dev` 把页面和数据请求转给应用，把 `/__tifa/vite/` 下的请求（浏览器模块和 Vite 的 HMR websocket）转给 Vite 的开发服务器。应用重启或换 SSR 包期间，页面请求会等它。Ctrl-C、SIGTERM 和 SIGHUP（终端关闭时会发出）停掉应用、Vite 和 `tifa dev`。用 `nohup` 启动的 `tifa dev` 会忽略 SIGHUP。Vite 的构建或插件一直不结束时，最多再等 5 秒就停掉 Vite。`tifa dev` 自己被强行杀掉时，应用和 Vite 也会自行退出：应用像收到 SIGTERM 一样关停，Vite 在这 5 秒内退出。`tifa dev` 只把点名要给应用和 Vite 的文件交给它们，自己继承来的文件（比如脚本用 `flock` 持有的锁）不会传下去。

有 `package.json` 的应用要先装好 npm 包。刚克隆下来时没有 `node_modules`，`tifa dev`（`tifa build` 也一样）会在启动任何东西之前停下，并打印要运行的命令：PATH 里有 pnpm 就是 `pnpm install`，否则是 `npm install`。没有页面的应用没有 `package.json`，也不需要 Node。

`tifa dev` 暂不支持 Windows，请用 WSL。

同一个应用里再启动一个 `tifa dev` 会启动不了：它在启动 Vite 之前打印 `tifa: tifa dev is already running in this app at http://<地址> (pid <N>)` 并报错退出。有 `tifa dev` 在运行时 `tifa build` 也会这样停下，因为两者都写 `.tifa/dist`。锁是 `.tifa/dev.lock`，持有它的进程一结束锁就没了。没传 `--addr` 时，9527 被占用，`tifa dev` 就依次试 9528 往后，一共十个端口，`serving` 那一行写出实际用的地址。传了 `--addr` 就严格用这个地址，被占用就报错。

`tifa dev` 提供明文 HTTP。项目文件里的 [HTTPS 配置](config.zh-CN.md#https)只对构建出的程序生效，开发程序不管它们。

## Vite

`tifa dev` 在 Node 上跑应用自己装的 Vite，用应用的 `vite.config.ts`（见 [build.zh-CN.md](build.zh-CN.md#应用的-tsconfig-与-vite-配置)）：

- Vite 开发服务器提供浏览器模块。有页面的应用第一次构建时启动它，它监听 127.0.0.1 上的一个空闲端口，只有 `tifa dev` 跟它通信。页面从它加载 Vite 客户端、`@vitejs/plugin-react` 的 React Fast Refresh 运行时和 `.tifa/client.ts`。
- SSR 包仍是文件 `.tifa/dist/ssr.js`，应用把它载入 QuickJS VM。它和开发服务器在同一个 Node 进程里构建，但跑在单独的工作线程上：浏览器用 React 的开发版，QuickJS 跑生产版，而 Vite 每个线程只定一次用哪个版本。`NODE_ENV` 由 Tifa 自己设（开发服务器是 development，SSR 包是 production），shell 里的 `NODE_ENV` 两边都不影响。

## 改动触发什么

`tifa dev` 监视应用根目录，跳过 `.git`、`.tifa`、`node_modules` 和放开发可执行文件的 `bin/`。改动分批处理。一个文件 100 毫秒没再变才进入一批，所以不会在保存到一半时开始构建，一批里的文件都进来了才开始处理。一直在变的文件（比如某个工具反复写的文件）满一秒也会进入一批，这时已经就绪的文件不再等其余的，所以任何改动都不会被拖住太久，大约一秒。

| 改了什么 | 执行的步骤 | 打开着的页面 |
| --- | --- | --- |
| `content/` 以外的 `.ts`、`.tsx`、`.js`、`.css`、`.json`，比如组件，或只导出组件和 `Error` 的 `page.tsx` | 打包（SSR 包） | 由 Vite 原地更新 |
| 除了组件还导出别的东西的模块，比如导出 `head` 的 `page.tsx` 或 `layout.tsx`，或带 `htmlAttributes`、`documentHead` 的根 `layout.tsx` | 打包（SSR 包） | 刷新，由 Vite 发起 |
| 公共目录里新增或删除文件 | 打包（先重新生成代码） | 经过 Vite，和别的前端改动一样 |
| 应用根目录的 `.env` | 不构建，应用重启后重新读它 | 刷新 |
| `.go`、`go.mod`、`go.sum` | 静态分析、go build | 刷新 |
| Go 代码用 `go:embed` 嵌进程序的文件（不论哪种，比如 Markdown 文章）被编辑 | go build（`.ts`、`.tsx`、`.js`、`.css`、`.json` 文件再加打包） | 刷新 |
| 被 Go 代码嵌入的文件新增或删除 | 静态分析、go build | 刷新 |
| 上一行且路由清单变了（路由、Load 和 Layout 的返回类型、方法函数的入参和结果类型、钩子等） | 静态分析、生成代码、打包、go build | 刷新 |
| 新增或删除 `page.tsx`、`layout.tsx`、`layout_.tsx`，或改 `tifa.yaml` | 全部步骤 | 刷新 |
| `content/` 下集合里的 Markdown 文件，改正文或 front matter，也包括新增、删除，或改了 `slug`、`translationKey` | 内容：Go 扫描 `content/`，转换这一篇 | 刷新 |
| `content/` 下的其他文件，不论哪种，比如页面包里的图片、`.json` 或 `.css` | 内容：它所在文件夹里的条目，以及回退到这个文件夹的译文 | 刷新 |
| `content/` 下新增或删除文件夹，空的也算 | 内容：重新扫描 `content/`，没有集合声明的文件夹会报出来 | 刷新 |
| `content.go` 或 schema 类型 | 静态分析、go build（页面的类型变了时再加生成代码和打包），内容全部转换 | 刷新 |
| `vite.config.ts` 或它导入的插件文件 | 打包，等 Vite 开发服务器重启后内容全部转换。没有页面的应用不跑开发服务器，任何前端文件（包括 `package.json`）改了都全部转换 | 刷新 |

- 前端改动经 Vite 送到打开着的页面：React Fast Refresh 换掉改过的组件并保留它们的状态，新 CSS 替换旧的。模块换不了时（比如除了组件还导出了别的东西），Vite 自己刷新页面。改 `vite.config.ts` 会让 Vite 重启开发服务器，改 `tsconfig.json` 会刷新页面。改了 `paths` 里的某一项后，Vite 开发服务器可能仍导入旧的目标，直到 `tifa dev` 重启。
- 同时 `tifa dev` 重建 SSR 包（不做 go build），运行中的应用从 `.tifa/dist` 读入新包，换掉旧包。应用不重启：`OnStart` 不会再跑，应用放在内存里的东西都还在。重建要产出新 SSR 包时，这期间到达的页面请求会等新包换上（最多等 30 秒），所以刷新看到的是新的服务端渲染。刚保存完、文件还没安静下来、重建还没开始时到达的请求也会等：Vite 一看到保存就刷新页面，这次刷新拿到的也是新的服务端渲染。Vite 开发服务器重启期间要构建的 SSR 包会等重启完成，如果重启让这次构建失败，就在新的服务器上再构建一次。从重建开始就等（补做上次失败留下的打包时也一样），Go 改动则从静态分析发现路由清单变了那一刻开始等，早于生成代码。
- 开发用的程序调用 `server.DevMain` 而不是 `server.Main`。只有它响应 `tifa dev` 发来的 `POST /__tifa/dev/reload`，读包用的是和启动时同一段代码。也只有它在 `tifa dev` 结束时跟着停止：它读 `tifa dev` 用 `--lifeline-fd` 传来的管道。`tifa build` 生成的程序调用 `server.Main`，没有这个路径，也没有这个开关。
- 路由清单有没有变，看的是静态分析后生成的 `.tifa/` 内容。只改函数体时生成内容不变，跳过生成和打包。
- 应用根目录以外、被 Vite 开发服务器加载的文件（比如工作区里另一个包的源码，或用 `?raw` 导入的文本文件）由 Vite 监视，不归 `tifa dev`。它变了时，不论是哪种文件，`tifa dev` 都会重建 SSR 包，和应用里的前端文件一样，日志里用从应用根目录出发的路径列出它。Vite 开发服务器重启期间这样的文件也可能变，这时旧的监视和新的监视都看不到。所以开发服务器的重启每次结束后（重叠的几次重启算一次），不管是谁发起的、成没成功，`tifa dev` 都重建一次 SSR 包，日志里写 `Vite's dev server restarted`。新的监视接着盯旧的监视盯过的根目录以外的文件。
- Vite 重启开发服务器时不会等新的文件监视读完应用根目录，而监视还没读到的文件没有起点，在那之前的修改永远看不到，开发服务器已经给出的模块会一直是旧的，直到这个文件再次改动。所以重启之后，开发服务器要等监视读完文件才回应请求，应用越大等得越久。
- `go:embed` 嵌进程序的文件是程序的一部分，所以它变了就要重新构建，哪怕它不是 Go 文件。`tifa dev` 从静态分析得知有哪些：分析向 go 命令要模块里每个包嵌入的文件和模式，之后 `tifa dev` 就监视它们，也包括 `//go:embed posts` 或 `posts/*.md` 这样的模式新带进来的文件。不带 `all:` 嵌入的目录会跳过以 `.` 或 `_` 开头的名字，和 go 命令一致，带自己 `go.mod` 的目录（另一个模块）也跳过。文件新增或删除时分析会再跑一次，因为这可能改变嵌入了哪些文件，只是编辑则只需要 go build。既是嵌入文件又是前端文件的（比如页面导入的 `.json`）两个步骤都会跑。被 `tifa dev` 跳过的目录（见上文）里的嵌入文件不被监视。第一次分析成功之前，嵌入的文件还不知道。
- 内容不需要 go build：开发程序从 `.tifa/dist` 读 `content.json` 和正文（见[内容](content.zh-CN.md)）。内容这一步在 Go 里重新扫描 `content/`，在 Vite 开发服务器的 Node 进程里转换变了的条目，写 `.tifa/dist/content/` 和 `content.json`。每次转换把正文写进 `.tifa/dist/content/` 下自己的文件夹，所以应用正在显示的正文不会被改掉。正在运行的应用随后重新载入内容，对每个条目跑 `Validate`，换上新的数据和正文，之后打开的页面显示新内容。应用不重启，`OnStart` 不再跑。整页缓存里的页面全部过期。第一次启动时全部转换一次。有集合的应用改了 Go 代码，内容这一步也会跑，因为集合或 schema 类型可能变了，变了就全部转换。
- `tifa dev` 下内容里有草稿（`Draft` 为 true）和日期还没到的条目（带着它的 `Date`），写作时就能在列表和文章页里看到它。页面可以给它们加标记。
- front matter 和 schema 对不上、`content/` 下有没声明的文件夹、插件报错时，浮层显示和 `tifa build` 一样的消息，应用继续用原来的内容。有条目没通过 `Validate` 时，浮层标题是 `The app did not load the content`，应用同样继续用原来的内容和原来的正文。
- 公共目录里的图片、字体等文件改内容不触发重建，下一次打包时才复制过去。公共目录里新增或删除文件会跑打包这一步，它会重新生成代码，因为生成的路由代码列着公共文件（见[路由树](build.zh-CN.md#路由树)）。日志里像别的改动一样写出文件名。`tifa.yaml` 里的 `build.public` 改了，监视的目录随之换掉，不用重启。
- 样式表经过 Vite 开发服务器，`@tailwindcss/vite` 编译 Tailwind CSS 的方式和在任何 Vite 应用里一样。
- 页面 head 里只有 Vite 客户端和 React 刷新运行时，Vite 插件用 `transformIndexHtml` 加的其他标签（统计脚本、manifest 链接、favicon 之类）一概不放。`tifa build` 没有 `index.html`，从不跑这个钩子，这些标签在 `tifa dev` 里会有、上线后却没有。要放这类标签，写在根 `layout.tsx` 导出的 `head` 里。`tifa dev` 每省掉一个标签，启动时打印一次警告并写出标签。
- 页面 head 里链接了服务端渲染导入的样式表，地址指向 Vite 开发服务器，所以脚本跑起来前页面就有样式，和构建出的页面一样。样式表改了，Vite 会更新这些链接。只被 `.client` 模块导入的样式表随它们的脚本一起到。
- 模块按 URL 导入的图片等文件，在服务端渲染里也用 Vite 开发服务器的 URL，和浏览器模块用的一样，水合不会不一致。`tifa build` 则给它一个 `/assets/` 下带哈希的文件。
- `build.target` 只对 `tifa build` 生效，Vite 开发服务器发的是现代 JavaScript。

## 日志

每个执行的步骤一行，带耗时，最后一行是从开始重建到应用可用的总耗时：

```text
tifa dev: 07:35:36 app/home/page.tsx modified
tifa dev: 07:35:36 bundle done in 90ms
tifa dev: 07:35:36 ready in 260ms
```

步骤超过 5 秒还没结束时打一行说明它还在跑，之后每 15 秒一行。应用的第一次 go build 要编译 QuickJS、SQLite 等依赖，在 Apple M4 上空的构建缓存约 7 秒，慢的机器更久，之后的构建用 Go 的构建缓存。第一行 ready 还会带上该打开的地址：

```text
tifa dev: 07:35:41 go build still running (5s). The first build compiles QuickJS and the other dependencies, later builds use Go's build cache
tifa dev: 07:35:56 go build still running (20s)
tifa dev: 07:36:30 go build done in 54.1s
tifa dev: 07:36:30 ready in 58.3s, open http://127.0.0.1:9527
```

第一次构建期间浏览器打开的页面（接受 `text/html` 的 GET 或 HEAD）会先等 2 秒。构建还没完成时，页面答 503，说明 `tifa dev` 正在构建，应用跑起来后自动刷新。其他请求（比如脚本调用 API）和应用重启时一样，等应用起来再答。

步骤名是 `static analysis`、`code generation`、`bundle`、`go build`。静态分析之后还有一行 `route manifest changed` 或 `route manifest unchanged`。Vite 的警告和错误也以 `tifa dev:` 行打印。应用自己的输出照常打印，只是没有构建出的程序会打的 `tifa: listening on` 那一行，因为应用监听的端口只有 `tifa dev` 在连。它的 stderr 经过 `tifa dev`，`tifa dev` 留下最后几行给浮层用（见下文），所以只在终端里上色的日志库在 stderr 上输出的是不带颜色的文字。

渲染时调用 Intl 或 `toLocale*` 方法却没传 locale，或者格式化日期没传 `timeZone`，应用会打一条 `tifa: ssr:` 警告，指出它在源码里的位置，每个位置只打一次。页面照常渲染。见 [`tifa dev` 下的报警](app.zh-CN.md#tifa-dev-下的报警)。

`tifa dev` 把每个页面渲染两次，第二次把时钟往后拨 36 小时。同样的数据两次渲染不同，说明页面渲染时读了时间或随机数，渲染缓存会把这个值固定下来。终端和页面上的浮层会提示，给出两次渲染第一处不同的位置附近的 HTML，并列出你的代码里读时间（`Date.now()`、`new Date()`、`Date()`）或随机数（`Math.random()`、`crypto`）的位置。包里的读取按调用它的那行你的代码列出，React、TanStack Router 和 Tifa 自己的读取不列。请改组件，或者在这个页面的 `route.go` 里写 `RenderCache = false`（见 [SSR 与缓存](app.zh-CN.md#ssr-与缓存)）。改好之前这个页面不进缓存。构建出的程序不会渲染两次。

## 构建失败

- 失败时旧版本继续运行，日志打印 `build failed: <原因>`，打开的页面盖上错误浮层，显示 Go 编译器或 Vite 的原文。
- 修好后浮层消失。修复需要 go build 时页面刷新，否则页面保持不动。失败那次没做完的步骤会并入下一次重建。
- 浏览器里编译失败的模块显示 Vite 自己的浮层。
- 装的 TanStack Router 不是 Tifa 测过的版本时（见 [build.zh-CN.md](build.zh-CN.md#应用的-tsconfig-与-vite-配置)），打包这一步报这个错。`tifa dev` 不监视 `node_modules`，装好测过的版本后要重启 `tifa dev`。
- 装的 React 或 React DOM 低于 19.3.0 时（见 [build.zh-CN.md](build.zh-CN.md#应用的-tsconfig-与-vite-配置)），打包这一步报这个错。装好新版本后要重启 `tifa dev`。
- 首次构建就失败时没有旧版本可用，所有请求答 502 和错误原文，等修好后自动恢复。
- 应用启动后立刻退出，或者 30 秒内没有开始监听时，浮层标题是 `The app did not start`。内容是退出状态和应用最后写到 stderr 的 40 行，`OnStart` 的错误和 panic 都写在那里。栈比 40 行长时，panic 那一行也会留着，前提是输出以这次 panic 的报告结尾。应用打印过、之后又继续运行的 `panic: ` 行不算。运行中的应用退出时（比如请求之外的 panic），标题是 `The app stopped`，内容一样。下一次构建之前，页面请求得到 502 和同样的内容。`tifa dev` 的日志只说应用退出了，因为应用自己的输出已经打在上面。
- 运行中的应用读不进重建的 SSR 包时（比如 QuickJS 编译不过，或包的顶层代码抛错），浮层标题是 `The app did not load the SSR bundle`，应用继续用原来的包。错误栈给的是源文件里的位置，调用了 QuickJS 没有的 `require` 的包会被点名，并说明怎么办。

浮层可以关掉，关掉后不影响后续刷新。

## 浏览器里的错误

浏览器里发生的错误显示在右下角的小面板里。面板不挡页面，构建错误的浮层会盖住它并保持在最上层。

- `Hydration mismatch`：React 没法复用服务端的 HTML，在浏览器里把页面重新渲染了一遍。面板显示 React 给出的消息（含差异）和组件栈。React 自己能恢复的其他错误标题是 `Recoverable error`。
- `Uncaught error`：组件抛出了错误且外面没有错误边界，或其他错误冒到了 `window`。
- `Unhandled rejection`：一个 promise 失败了，没有人处理。

同一个错误再次出现时，计数加一（`×3`），不会多出一条。面板保留最近 20 个不同的错误。每条都可以关掉，刷新页面会清空。`Error` 组件接住的错误不会显示，它们和以前一样只进控制台。`tifa dev` 也会留下同样的错误，供 [Tifa errors](#tifa-errors) 查看。构建出的应用没有这些代码，它们只在开发构建里。

## 页面渲染失败

服务器渲染页面时组件抛错，应用会带着错误再渲染一次，由某个 `Error` 组件显示这个错误（见[页面上的错误](app.zh-CN.md#页面上的错误)）。在 `tifa dev` 下，这张页上盖着一层浮层，写出请求，公开的消息和状态文字不同时写出公开的消息，并显示完整的错误和它在源文件里的错误栈。没有 `Error` 渲染得出来，或者页面请求在应用渲染出页面之前就失败（比如 `OnRequest` 返回了意外错误）时，应用返回 Go 写的简单页，在 `tifa dev` 下它也显示这些。两种页的状态码都是 `OnError` 定的，`curl -i` 也看得到，保存修复后都自动刷新，只改 TSX、其他页面不刷新的修复也一样。构建出的应用只显示状态码和公开的消息。

这些页会露出内部错误。`tifa dev` 默认只听 `127.0.0.1`，用 `--addr 0.0.0.0:<端口>` 时局域网里的其他机器也看得到。

刷新和浮层走 `tifa dev` 自己的连接 `/__tifa/dev/events`，不走 Vite 的 HMR websocket。Vite 开发服务器要到第一次 bundle 步骤才启动，排在静态分析和代码生成之后，没有页面的应用根本不启动它。没有应用在跑的时候（首次构建失败或应用崩溃）返回的那个页面，照样要显示错误、修好后自动刷新，这只有 `tifa dev` 从一开始就自己提供的连接做得到。同一个浏览器的所有标签页通过 SharedWorker 共用这一条连接。

## Tifa errors

```sh
tifa errors [--dir 应用根目录] [--json]
```

`tifa dev` 运行时，`go tool tifa errors` 打印上次构建开始以来的错误。看不到浏览器、也看不到 `tifa dev` 终端的编程 agent 靠它知道改动有没有出错。改完代码、打开或刷新过改动的页面之后运行它：浏览器里的错误只来自打开着的页面。

它列出：

- 浮层显示的内容：`Build failed` 加 Go 编译器或 Vite 的原文，`The app did not start`、`The app stopped` 或 `The app did not load the SSR bundle`。它一直留着，直到下一次构建结束。
- 请求的意外错误，也就是交给 `OnError` 的那些（没有 `OnError` 时记日志并答 500 的那些）：请求、状态和用户看到的消息，以及错误本身和映射回源码的堆栈，和 `tifa dev` 下错误页显示的一样。API 路由的请求也算，没有页面的应用也一样。
- 页面报上来的浏览器错误：`Hydration mismatch`、`Recoverable error`、`Uncaught error` 和 `Unhandled rejection`，名字和面板一致，每条带最先报告它的页面地址。
- 两次渲染不一致的页面（见[日志](#日志)）。

每类保留最近 20 个不同的错误，同一个错误再次出现时计数加一，和面板一样。构建一开始，之前的错误就清掉。构建进行中运行这个命令，它会等构建结束再回答。

```text
Request failed: GET /orders?page=2, 503 Service Unavailable (×2)
  Users see: Come back later
  tifa: render page: ...

Hydration mismatch in the browser: http://127.0.0.1:9527/orders (×3)
  ...
```

没有错误时打印 `No errors since the last build.`。退出码：没有错误是 0，有错误是 1，应用里没有 `tifa dev` 在运行是 2：`.tifa/dev.lock` 记下的进程已经不在，或者还没开始监听，或者它的地址上没有它的应答。`--json` 以 JSON 打印同样的内容，空的字段也都在：

```json
{
  "failure": {"title": "Build failed", "error": "..."},
  "requests": [{"method": "GET", "url": "/orders?page=2", "status": 503, "message": "Come back later", "error": "...", "count": 2}],
  "browser": [{"kind": "recoverable", "title": "Hydration mismatch", "url": "http://127.0.0.1:9527/orders", "text": "...", "count": 3}],
  "renders": [{"method": "GET", "url": "/stats", "text": "...", "count": 1}]
}
```

构建和运行都正常时 `failure` 是 `null`。`kind` 是 `recoverable`、`uncaught` 或 `rejection`。

命令读取 `tifa dev` 写进 `.tifa/dev.lock` 的进程号和地址，不去拿锁，所以同时启动的 `tifa dev` 或 `tifa build` 不受影响。它向这个地址请求 `/__tifa/dev/errors`，回答的 `X-Tifa-Dev-Pid` 是同一个进程才采用。`tifa dev` 只在 `localhost` 或 IP 地址下回答这个路径，也只收自己页面报上来的浏览器错误，每条最多 128 KiB。应用自己的错误由应用回答给 `tifa dev`。构建出的应用没有这些。

## 文件放在哪

| 路径 | 内容 |
| --- | --- |
| `.tifa/` | 生成代码，与 `tifa build` 相同 |
| `.tifa/dist/` | SSR 包、`build.json`、公共文件，有集合的应用还有 `content.json` 和 `content/` 里的正文。浏览器模块改由 Vite 开发服务器提供 |
| `bin/tifa-dev` | 开发用的可执行文件 |

应用在应用根目录里运行，从别的目录用 `--dir` 启动 `tifa dev` 时也一样，和在应用根目录里运行 `tifa build` 出的程序一致。应用按相对路径打开的文件，比如 [`tifa new`](build.zh-CN.md#tifa-new) 模板里的 `app.db`，都落在这里。它读的 `.env` 也在这里（见 [.env](config.zh-CN.md#env)）。`tifa dev` 用 `SERVER_ADDR` 告诉应用监听哪个地址，它优先于 shell 里的 `PORT` 和 `.env`。

停止应用时最多等配置里 `server.shutdown_timeout` 的 3 倍再加 1 秒，超时就强行结束整个进程组。

## 大应用

`tifa dev` 用系统的文件事件监视目录。应用的目录或文件多到超出系统适合给一个程序监视的量时，`tifa dev` 改为轮询剩下的部分：大约每秒看一次，照常重建。不需要任何设置。日志会说明：

```text
tifa dev: 07:35:36 watching app/assets would use up the system's file watches, so it and the directories found after it are polled for changes
```

- 在 macOS、FreeBSD、OpenBSD、NetBSD 和 DragonFly 上，`tifa dev` 用 kqueue 监视。一个被监视的目录占一个文件描述符，目录里属于上表那几类的文件也各占一个，嵌入的文件和 `content/` 下的文件也是，之后新出现的也一样。其他文件（比如 `content/` 以外的图片）不占。`tifa dev` 把这些算进一个进程能打开数量的一半（取打开文件上限和 `kern.maxfilesperproc` 中较小的那个），超出的目录改为轮询，已监视的目录后来文件变多超出时也一样。
- Linux 上监视一个目录占用户的一个 inotify watch，编辑器等工具共用这个额度。`tifa dev` 最多用 `fs.inotify.max_user_watches` 的一半。
- 系统先用完时（其他程序占了大量 watch 或文件），`tifa dev` 轮询没能监视的目录，再还回自己占着的一小部分额度，继续工作。运行中才用完时（比如打不开文件），`tifa dev` 停掉系统事件监视，改为轮询所有目录，并把所有文件重新比对一遍。
- 每轮轮询列出被轮询的目录，只对大小、修改时间或文件身份变了的文件重新算校验和。两轮之间至少隔一秒，且至少是上一轮耗时的十倍。被监视的目录里有文件增删时，也按同样的规则重新列出（只是没有一秒的下限），所以一个不停变大的目录不会多花开销。轮询和重新列出因此各自最多占大约一个 CPU 核的十分之一，处理文件事件和重建的开销另算。
- 一轮轮询的耗时随文件数增长，也取决于机器和磁盘。在一台 Apple M4 上，文件都在系统缓存里时，我们几次测得轮询 2 万个文件一轮在 45 到 65 毫秒之间。只要一轮不到十分之一秒，被轮询目录里的改动大约一秒内就会被发现，然后照常构建。
- 改为轮询的目录在 `tifa dev` 重启前一直轮询。
- 在被轮询的目录里保存文件后，轮询发现改动之前请求的页面拿到的是旧的服务端渲染，这时 Vite 发起的刷新会水合失败。看到 `ready` 那一行后再刷新一次。
- Vite 开发服务器也监视应用根目录。`tifa dev` 因为系统额度不够改为轮询后，会让 Vite 开发服务器也改为轮询，除非开发服务器用 FSEvents 监视（macOS 上通常如此）。FSEvents 不为每个文件占文件描述符，这时保持不变。
- 改为轮询时，Vite 开发服务器重启，之后每秒看一遍每个文件，在 Apple M4 上 1 万个文件大约占一个 CPU 核的 8%。重启成功、且同一时间没有开发服务器的其他重启（比如改 `vite.config.ts` 引起的）时，只重启一次。正在进行的重启会先等它完成，如果它成功了且新服务器已经在轮询，就不再重启。
- 重启可能失败。建不出新服务器时（比如 `vite.config.ts` 写坏了），保留旧的服务器和它的监视。新服务器监听不了端口时（比如这期间别的程序占了这个端口），旧服务器已经关掉，直到下一次重启都没有 Vite 开发服务器。两种情况日志都会说明，`tifa dev` 和应用照常运行。开发服务器的监视下次报错时会再试，之后第一次成功的重启（比如改了 `vite.config.ts` 之后）也会改为轮询。
- Vite 自己的监视先用完 watch 或文件描述符时（ENOSPC、EMFILE 或 ENFILE），开发服务器也这样改为轮询，并说明一次。用 FSEvents 的监视也一样。
