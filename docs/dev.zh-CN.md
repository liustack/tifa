---
summary: 'tifa dev 怎么用：每类改动后页面怎么更新、执行哪些步骤、Vite 在其中做什么、日志怎么读、构建失败时页面和应用怎样、开发产物放在哪、大应用怎么监视'
read_when:
  - 第一次在本地跑 tifa 应用
  - 改 internal/devserver、cmd/tifa 的 dev 命令或 internal/bundle 的开发构建
  - tifa dev 改了文件页面却没更新，或报出看不懂的错误
---

[English](dev.md) · 简体中文

# tifa dev

```sh
tifa dev [--dir 应用根目录] [--addr 127.0.0.1:9527]
```

启动后先打印 `tifa dev: serving http://<地址>`，然后做一次完整构建（步骤同 [tifa build](build.zh-CN.md)，应用的第一次构建要一两分钟，见[日志](#日志)），启动应用。浏览器只连 `--addr`。tifa dev 把页面和数据请求转给应用，把 `/__tifa/vite/` 下的请求（浏览器模块和 Vite 的 HMR websocket）转给 Vite 的开发服务器。应用重启或换 SSR 包期间，页面请求会等它。Ctrl-C 或 SIGTERM 停掉应用、Vite 和 tifa dev。Vite 的构建或插件一直不结束时，最多再等 5 秒就停掉 Vite。tifa dev 自己被强行杀掉时，Vite 也会在这 5 秒内自行退出。

tifa dev 暂不支持 Windows，请用 WSL。

## Vite

tifa dev 在 Node 上跑应用自己装的 Vite，用应用的 `vite.config.ts`（见 [build.zh-CN.md](build.zh-CN.md#应用的-tsconfig-与-vite-配置)）：

- Vite 开发服务器提供浏览器模块。有页面的应用第一次构建时启动它，它监听 127.0.0.1 上的一个空闲端口，只有 tifa dev 跟它通信。页面从它加载 Vite 客户端、`@vitejs/plugin-react` 的 React Fast Refresh 运行时和 `.tifa/client.ts`。
- SSR 包仍是文件 `.tifa/dist/ssr.js`，应用把它载入 QuickJS VM。它和开发服务器在同一个 Node 进程里构建，但跑在单独的工作线程上：浏览器用 React 的开发版，QuickJS 跑生产版，而 Vite 每个线程只定一次用哪个版本。`NODE_ENV` 由 tifa 自己设（开发服务器是 development，SSR 包是 production），shell 里的 `NODE_ENV` 两边都不影响。

## 改动触发什么

tifa dev 监视应用根目录，跳过 `.git`、`.tifa`、`node_modules` 和放开发可执行文件的 `bin/`。改动分批处理。一个文件 100 毫秒没再变才进入一批，所以不会在保存到一半时开始构建，一批里的文件都进来了才开始处理。一直在变的文件（比如某个工具反复写的文件）满一秒也会进入一批，这时已经就绪的文件不再等其余的，所以任何改动都不会被拖住太久，大约一秒。

| 改了什么 | 执行的步骤 | 打开着的页面 |
| --- | --- | --- |
| `.ts`、`.tsx`、`.js`、`.css`、`.json` | 打包（SSR 包） | 由 Vite 原地更新 |
| 应用根目录的 `.env` 或 `.env.*` | 打包 | 刷新（Vite 会重启它的开发服务器） |
| `.go`、`go.mod`、`go.sum` | 静态分析、go build | 刷新 |
| 上一行且路由清单变了（路由、Load 和 Layout 的返回类型、方法函数的入参和结果类型、钩子等） | 静态分析、生成代码、打包、go build | 刷新 |
| 新增或删除 `route.tsx`、`layout.tsx`，或改 `tifa.yaml`、`tifa.json` | 全部步骤 | 刷新 |

- 前端改动经 Vite 送到打开着的页面：React Fast Refresh 换掉改过的组件并保留它们的状态，新 CSS 替换旧的。模块换不了时（比如除了组件还导出了别的东西），Vite 自己刷新页面。改 `vite.config.ts` 会让 Vite 重启开发服务器，改 `tsconfig.json` 会刷新页面。改了 `paths` 里的某一项后，Vite 开发服务器可能仍导入旧的目标，直到 tifa dev 重启。
- 同时 tifa dev 重建 SSR 包（不做 go build），运行中的应用从 `.tifa/dist` 读入新包，换掉旧包。应用不重启：`OnStart` 不会再跑，应用放在内存里的东西都还在。重建要产出新 SSR 包时，这期间到达的页面请求会等新包换上（最多等 30 秒），所以刷新看到的是新的服务端渲染。Vite 开发服务器重启期间要构建的 SSR 包会等重启完成，如果重启让这次构建失败，就在新的服务器上再构建一次。从重建开始就等（补做上次失败留下的打包时也一样），Go 改动则从静态分析发现路由清单变了那一刻开始等，早于生成代码。
- 开发用的程序调用 `server.DevMain` 而不是 `server.Main`。只有它响应 tifa dev 发来的 `POST /__tifa/dev/reload`，读包用的是和启动时同一段代码。`tifa build` 生成的程序调用 `server.Main`，没有这个路径。
- 路由清单有没有变，看的是静态分析后生成的 `.tifa/` 内容。只改函数体时生成内容不变，跳过生成和打包。
- 应用根目录以外、被 Vite 开发服务器加载的文件（比如工作区里另一个包的源码，或用 `?raw` 导入的文本文件）由 Vite 监视，不归 tifa dev。它变了时，不论是哪种文件，tifa dev 都会重建 SSR 包，和应用里的前端文件一样，日志里用从应用根目录出发的路径列出它。Vite 开发服务器重启期间这样的文件也可能变，这时旧的监视和新的监视都看不到。所以开发服务器的重启每次结束后（重叠的几次重启算一次），不管是谁发起的、成没成功，tifa dev 都重建一次 SSR 包，日志里写 `Vite's dev server restarted`。新的监视接着盯旧的监视盯过的根目录以外的文件。
- 公共目录里的图片、字体等文件不触发重建，下一次打包时才复制过去。
- 样式表经过 Vite 开发服务器，`@tailwindcss/vite` 编译 Tailwind CSS 的方式和在任何 Vite 应用里一样。
- 页面 head 里链接了服务端渲染导入的样式表，地址指向 Vite 开发服务器，所以脚本跑起来前页面就有样式，和构建出的页面一样。样式表改了，Vite 会更新这些链接。只被 `.client` 模块导入的样式表随它们的脚本一起到。
- 模块按 URL 导入的图片等文件，在服务端渲染里也用 Vite 开发服务器的 URL，和浏览器模块用的一样，水合不会不一致。`tifa build` 则给它一个 `/assets/` 下带哈希的文件。
- `build.target` 只对 `tifa build` 生效，Vite 开发服务器发的是现代 JavaScript。

## 日志

每个执行的步骤一行，带耗时，最后一行是从开始重建到应用可用的总耗时：

```text
tifa dev: 07:35:36 app/home/route.tsx modified
tifa dev: 07:35:36 bundle done in 90ms
tifa dev: 07:35:36 ready in 260ms
```

步骤超过 5 秒还没结束时打一行说明它还在跑，之后每 15 秒一行。应用的第一次 go build 要编译 QuickJS、SQLite 等依赖，可能要一两分钟，之后的构建用 Go 的构建缓存。第一行 ready 还会带上该打开的地址：

```text
tifa dev: 07:35:41 go build still running (5s). The first build compiles QuickJS and the other dependencies, later builds use Go's build cache
tifa dev: 07:35:56 go build still running (20s)
tifa dev: 07:36:30 go build done in 54.1s
tifa dev: 07:36:30 ready in 58.3s, open http://127.0.0.1:9527
```

第一次构建期间浏览器打开的页面（接受 `text/html` 的 GET 或 HEAD）会先等 2 秒。构建还没完成时，页面答 503，说明 tifa dev 正在构建，应用跑起来后自动刷新。其他请求（比如脚本调用 API）和应用重启时一样，等应用起来再答。

步骤名是 `static analysis`、`code generation`、`bundle`、`go build`。静态分析之后还有一行 `route manifest changed` 或 `route manifest unchanged`。Vite 的警告和错误也以 `tifa dev:` 行打印。应用自己的输出照常打印，只是没有构建出的程序会打的 `tifa: listening on` 那一行，因为应用监听的端口只有 tifa dev 在连。它的 stderr 经过 tifa dev，tifa dev 留下最后几行给浮层用（见下文），所以只在终端里上色的日志库在 stderr 上输出的是不带颜色的文字。

渲染时调用 Intl 或 `toLocale*` 方法却没传 locale，或者格式化日期没传 `timeZone`，应用会打一条 `tifa: ssr:` 警告，指出它在源码里的位置，每个位置只打一次。页面照常渲染。见 [tifa dev 下的报警](app.zh-CN.md#tifa-dev-下的报警)。

## 构建失败

- 失败时旧版本继续运行，日志打印 `build failed: <原因>`，打开的页面盖上错误浮层，显示 Go 编译器或 Vite 的原文。
- 修好后浮层消失。修复需要 go build 时页面刷新，否则页面保持不动。失败那次没做完的步骤会并入下一次重建。
- 浏览器里编译失败的模块显示 Vite 自己的浮层。
- 首次构建就失败时没有旧版本可用，所有请求答 502 和错误原文，等修好后自动恢复。
- 应用启动后立刻退出，或者 30 秒内没有开始监听时，浮层标题是 `The app did not start`。内容是退出状态和应用最后写到 stderr 的 40 行，`OnStart` 的错误和 panic 都写在那里。栈比 40 行长时，panic 那一行也会留着，前提是输出以这次 panic 的报告结尾。应用打印过、之后又继续运行的 `panic: ` 行不算。运行中的应用退出时（比如请求之外的 panic），标题是 `The app stopped`，内容一样。下一次构建之前，页面请求得到 502 和同样的内容。tifa dev 的日志只说应用退出了，因为应用自己的输出已经打在上面。
- 运行中的应用读不进重建的 SSR 包时（比如 QuickJS 编译不过，或包的顶层代码抛错），浮层标题是 `The app did not load the SSR bundle`，应用继续用原来的包。错误栈给的是源文件里的位置，调用了 QuickJS 没有的 `require` 的包会被点名，并说明怎么办。

浮层可以关掉，关掉后不影响后续刷新。

刷新和浮层走 tifa dev 自己的连接 `/__tifa/dev/events`，不走 Vite 的 HMR websocket。Vite 开发服务器要到第一次 bundle 步骤才启动，排在静态分析和代码生成之后，没有页面的应用根本不启动它。没有应用在跑的时候（首次构建失败或应用崩溃）返回的那个页面，照样要显示错误、修好后自动刷新，这只有 tifa dev 从一开始就自己提供的连接做得到。同一个浏览器的所有标签页通过 SharedWorker 共用这一条连接。

## 文件放在哪

| 路径 | 内容 |
| --- | --- |
| `.tifa/` | 生成代码，与 `tifa build` 相同 |
| `.tifa/dist/` | SSR 包、`build.json` 和公共文件。浏览器模块改由 Vite 开发服务器提供 |
| `bin/tifa-dev` | 开发用的可执行文件 |

停止应用时最多等配置里 `server.shutdown_timeout` 的 3 倍再加 1 秒，超时就强行结束整个进程组。

## 大应用

tifa dev 用系统的文件事件监视目录。应用的目录或文件多到超出系统适合给一个程序监视的量时，tifa dev 改为轮询剩下的部分：大约每秒看一次，照常重建。不需要任何设置。日志会说明：

```text
tifa dev: 07:35:36 watching app/assets would use up the system's file watches, so it and the directories found after it are polled for changes
```

- 在 macOS、FreeBSD、OpenBSD、NetBSD 和 DragonFly 上，tifa dev 用 kqueue 监视。一个被监视的目录占一个文件描述符，目录里属于上表那几类的文件也各占一个，之后新出现的也一样。其他文件（比如图片）不占。tifa dev 把这些算进一个进程能打开数量的一半（取打开文件上限和 `kern.maxfilesperproc` 中较小的那个），超出的目录改为轮询，已监视的目录后来文件变多超出时也一样。
- Linux 上监视一个目录占用户的一个 inotify watch，编辑器等工具共用这个额度。tifa dev 最多用 `fs.inotify.max_user_watches` 的一半。
- 系统先用完时（其他程序占了大量 watch 或文件），tifa dev 轮询没能监视的目录，再还回自己占着的一小部分额度，继续工作。运行中才用完时（比如打不开文件），tifa dev 停掉系统事件监视，改为轮询所有目录，并把所有文件重新比对一遍。
- 每轮轮询列出被轮询的目录，只对大小、修改时间或文件身份变了的文件重新算校验和。两轮之间至少隔一秒，且至少是上一轮耗时的十倍。被监视的目录里有文件增删时，也按同样的规则重新列出（只是没有一秒的下限），所以一个不停变大的目录不会多花开销。轮询和重新列出因此各自最多占大约一个 CPU 核的十分之一，处理文件事件和重建的开销另算。
- 一轮轮询的耗时随文件数增长，也取决于机器和磁盘。在一台 Apple M4 上，文件都在系统缓存里时，我们几次测得轮询 2 万个文件一轮在 45 到 65 毫秒之间。只要一轮不到十分之一秒，被轮询目录里的改动大约一秒内就会被发现，然后照常构建。
- 改为轮询的目录在 tifa dev 重启前一直轮询。
- Vite 开发服务器也监视应用根目录。tifa dev 因为系统额度不够改为轮询后，会让 Vite 开发服务器也改为轮询，除非开发服务器用 FSEvents 监视（macOS 上通常如此）。FSEvents 不为每个文件占文件描述符，这时保持不变。
- 改为轮询时，Vite 开发服务器重启，之后每秒看一遍每个文件，在 Apple M4 上 1 万个文件大约占一个 CPU 核的 8%。重启成功、且同一时间没有开发服务器的其他重启（比如改 `vite.config.ts` 引起的）时，只重启一次。正在进行的重启会先等它完成，如果它成功了且新服务器已经在轮询，就不再重启。
- 重启可能失败。建不出新服务器时（比如 `vite.config.ts` 写坏了），保留旧的服务器和它的监视。新服务器监听不了端口时（比如这期间别的程序占了这个端口），旧服务器已经关掉，直到下一次重启都没有 Vite 开发服务器。两种情况日志都会说明，tifa dev 和应用照常运行。开发服务器的监视下次报错时会再试，之后第一次成功的重启（比如改了 `vite.config.ts` 之后）也会改为轮询。
- Vite 自己的监视先用完 watch 或文件描述符时（ENOSPC、EMFILE 或 ENFILE），开发服务器也这样改为轮询，并说明一次。用 FSEvents 的监视也一样。
