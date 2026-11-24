---
summary: '怎样使用 `tifa dev`：页面、构建错误、`tifa errors`、`--background`、`status`、`logs`、`stop`'
audience: app
order: 1
translatedFrom: ea765c14e00f
read_when:
  - '第一次在本地运行 Tifa 应用'
  - '修改 internal/devserver、cmd/tifa 里的 dev 命令，或者 internal/bundle 里的 dev 构建'
  - '`tifa dev` 在文件改动后没有更新页面，或者报出你看不懂的错误'
  - '在脚本或编程 agent 里检查一次改动有没有弄坏构建、某个请求或浏览器里的某个页面，或者修改 `tifa errors`'
  - '在脚本或编程 agent 里启动、观察或停止 `tifa dev`，或者修改 `tifa dev --background`、`status`、`logs` 或 `stop`'
  - '在 Windows 上运行 `tifa dev`，或者修改它在那里启动、停止或结束子进程的方式'
---

# tifa dev {#tifa-dev}

```sh
tifa dev [--dir app-root] [--addr 127.0.0.1:9527] [--background]
```

启动时它先打印 `tifa dev: serving http://<address>`，然后做一次完整构建（步骤与 [`tifa build`](build.zh-CN.md) 相同，应用的第一次构建要几秒到一分钟，见[日志](#log)），再启动应用。浏览器只连 `--addr`。`tifa dev` 把页面请求和数据请求转发给应用，把 `/__tifa/vite/` 下的请求（浏览器模块和 Vite 的 HMR websocket）转发给 Vite 的开发服务器。应用重启或加载新的 SSR 包时，页面请求会等着。Ctrl-C、SIGTERM 和 SIGHUP（终端关闭时会发出）会停掉应用、Vite 和 `tifa dev`。用 `nohup` 启动的 `tifa dev` 会忽略 SIGHUP。Vite 的构建或插件迟迟不结束时，给它 5 秒，然后停掉 Vite。如果 `tifa dev` 本身被杀掉，应用和 Vite 会自己停：应用像收到 SIGTERM 一样关闭，Vite 在同样的 5 秒内退出（在 Windows 上两者同时结束，见[在 Windows 上](#on-windows)）。`tifa dev` 只把明确指定的文件交给应用和 Vite，不会把自己继承来的文件交给它们，比如脚本用 `flock` 持有的锁。

带 `package.json` 的应用要先装好 npm 包。刚克隆下来时没有 `node_modules`，`tifa dev`（和 `tifa build` 一样）会在启动任何东西之前停下，并打印要运行的命令：PATH 里有 pnpm 就是 `pnpm install`，否则是 `npm install`。没有页面的应用没有 `package.json`，也不需要 Node。

`tifa dev` 在 Windows 10 1809 及以后的版本上和其他系统一样运行，包括 `--background` 和[智能体与脚本](#agents-and-scripts)里的命令。Windows 上有哪些不同，见[在 Windows 上](#on-windows)。

同一个应用里的第二个 `tifa dev` 不会启动。它会打印 `tifa: tifa dev is already running in this app at http://<address> (pid <N>)` 并报错退出，这发生在它启动 Vite 之前。有 `tifa dev` 在运行时，`tifa build` 也会同样停下，因为两者都写 `.tifa/dist`。锁是 `.tifa/dev.lock`，持有它的进程结束后它就消失。不带 `--addr` 时，9527 端口被占用，`tifa dev` 会依次试 9528 及以上，一共十个端口，`serving` 那一行会写出它用了哪一个。带 `--addr` 时，`tifa dev` 只用那个地址，被占用就失败。

`tifa dev` 提供的是普通 HTTP。项目文件里的 [HTTPS 设置](config.zh-CN.md#https)只对构建出来的程序生效，开发程序会忽略它们。

## Vite {#vite}

`tifa dev` 在 Node 上运行应用自己的 Vite，用的是应用的 `vite.config.ts`（见 [build.md](build.zh-CN.md#the-apps-tsconfig-and-vite-config)）：

- Vite 的开发服务器提供浏览器模块。它随带页面的应用的第一次构建一起启动，监听 127.0.0.1 上一个空闲端口，只有 `tifa dev` 会连它。页面从它那里加载 Vite 客户端、`@vitejs/plugin-react` 的 React Fast Refresh 运行时和 `.tifa/client.ts`。
- SSR 包仍然是一个文件 `.tifa/dist/ssr.js`，应用把它加载进自己的 beastjs VM。它在开发服务器所在的同一个 Node 进程里构建，放在它自己的工作线程上：浏览器拿到的是 React 的开发版，beastjs 跑的是生产版，而 Vite 每个线程只选一次版本。Tifa 自己设置 `NODE_ENV`（开发服务器用 development，SSR 包用 production），所以 shell 里的 `NODE_ENV` 两边都不影响。SSR 包每构建 10 次（失败的也算）就换一个新的工作线程，因为 Node 不会把 Vite 构建留在线程里的东西全部释放。线程结束过的那次构建（比如插件没捕获的错误）之后也会换。把东西放在该线程 `globalThis` 上的插件，到那时会从头开始。

## 每种改动会触发什么 {#what-each-change-triggers}

`tifa dev` 监视应用根目录，跳过 `.git`、`.tifa`、`node_modules` 和 `bin/`（开发用的可执行文件在那里）。改动按批处理。一个文件保持 100 毫秒不变才会加入一批，所以构建不会在保存到一半时启动，而一批要等它的所有文件都就绪才开始。一直在变的文件（比如某个工具反复写入的文件）不管怎样一秒后就加入，这时已就绪的文件不再等其余的，所以没有什么会被拖过一秒多太久。

| 改了什么 | 运行的步骤 | 已打开的页面 |
| --- | --- | --- |
| `content/` 之外的 `.ts`、`.tsx`、`.js`、`.css`、`.json`，比如一个组件，或者只导出组件和 `ErrorBoundary` 的 `page.tsx` | bundle（SSR 包） | 通过 Vite 原地更新 |
| SSR 包里某个模块导入的 `.css` 文件，在 `content/` 和 public 目录之外，编辑时没有错误在显示，并且 SSR 包导入每个样式表都只为了它的副作用（没有 CSS modules，也没有用 `?inline`、`?url` 或 `?raw` 导入的） | styles：Vite 开发服务器编译页面链接的样式表，失败时显示为构建错误。SSR 包里没有 CSS，所以保持不变 | 通过 Vite 原地更新 |
| 导出内容不止组件的模块，比如导出 `head` 的 `page.tsx` 或 `layout.tsx`，或者带 `documentHead` 的根 `layout.tsx` | bundle（SSR 包） | 重新加载，由 Vite 触发 |
| 应用根目录里上一个 SSR 包读过的文件，任何类型，比如用 `?raw` 导入的 Markdown 或 YAML 文件：被编辑或删除 | bundle（SSR 包） | 通过 Vite，和任何前端改动一样 |
| 在 public 文件夹里增加或删除一个文件 | bundle（先重新生成代码） | 通过 Vite，和任何前端改动一样 |
| 应用根目录里的 `.env` | 无：应用重新启动并读取它 | 重新加载 |
| `.go`、`go.mod`、`go.sum` | 静态分析，go build | 重新加载 |
| Go 代码用 `go:embed` 嵌入的文件，任何类型，比如一篇 Markdown 文章：被编辑 | go build（如果是 `.ts`、`.tsx`、`.js`、`.css` 或 `.json` 文件，还要 bundle） | 重新加载 |
| Go 代码嵌入的文件，被增加或删除 | 静态分析，go build | 重新加载 |
| 同上，并且路由清单变了（路由、Load 和 Layout 的结果类型、处理函数的输入和结果类型、hook 等） | 静态分析，代码生成，bundle，go build | 重新加载 |
| 增加或删除 `page.tsx`、`layout.tsx` 或 `layout_.tsx`，增加、删除或重命名 `_locales/<language>.json`（草稿也算），或者 `tifa.yaml` 变了 | 所有步骤 | 重新加载 |
| `content/` 下某个集合的 Markdown 文件，正文或 front matter 变了，也包括增加或删除，或者有了新的 `slug` 或 `translationKey` | content：用 Go 扫描 `content/`，并转换那个条目 | 重新加载 |
| `content/` 下的其他文件，任何类型，比如图片、`.json` 或页面包里的 `.css` | content：它所在文件夹里的条目，以及回退到该文件夹的翻译 | 重新加载 |
| 在 `content/` 里增加或删除一个文件夹，空的也算 | content：扫描 `content/`，它会报告没有任何集合声明的文件夹 | 重新加载 |
| `content.go` 或某个 schema 类型 | 静态分析，go build（页面的类型变了还要代码生成和 bundle），所有条目的 content | 重新加载 |
| `vite.config.ts` 或它导入的插件文件 | bundle，然后在 Vite 的开发服务器重启后做所有条目的 content。在没有页面、因而不运行开发服务器的应用里，任何前端文件（包括 `package.json`）都会转换所有条目 | 重新加载 |

- 前端改动通过 Vite 到达已打开的页面：React Fast Refresh 替换被编辑的组件并保留它们的状态，新的 CSS 替换旧的。模块无法替换时（比如导出内容不止组件的模块），Vite 自己重新加载页面。改 `vite.config.ts` 会重启 Vite 的开发服务器，改 `tsconfig.json` 会重新加载页面。在 `tifa dev` 重启之前，Vite 的开发服务器可能还会继续导入一个被改过的 `paths` 条目的旧目标。
- 同时 `tifa dev` 会重新构建 SSR 包（不跑 go build），运行中的应用从 `.tifa/dist` 加载它来替换旧的。应用不会重启：`OnStart` 不会再运行，应用放在内存里的东西保持不变。如果新的 `ssr.js` 和它的 source map 跟应用正在运行的那份逐字节相同（比如只改了一行注释），应用连 VM 也保留。在重建出新 SSR 包的期间到达的页面请求会等它，最多 30 秒，所以重新加载看到的是新的服务端渲染结果。保存之后、文件安静下来但重建还没开始的那段时间到达的请求也会等：Vite 一看到保存就会重新加载页面，而这次重新加载拿到的是新的服务端渲染结果。如果 SSR 包在 Vite 开发服务器重启期间构建，它会等重启结束，重启导致构建失败的话就在新服务器上再构建一次。这从重建开始时就生效，也包括重做一个被上次失败的重建留下未完成的包，对 Go 改动来说，则从静态分析发现新路由清单的那一刻起，早于代码生成。
- 开发程序调用 `server.DevMain` 而不是 `server.Main`。只有它会响应 `tifa dev` 的 `POST /__tifa/dev/reload`，并且加载包用的代码和启动时是同一份。也只有它会在 `tifa dev` 结束时停止，方式是读取 `tifa dev` 用 `--lifeline-fd` 传给它的管道。`tifa build` 生成的程序调用 `server.Main`，没有这个路径，也没有这个参数。
- 路由清单有没有变，由静态分析之后生成的 `.tifa/` 内容来判断。如果只有函数体变了，生成的内容保持不变，就会跳过生成和 bundle。
- 每次 bundle 和每次 go build 都会把 `.tifa/` 里生成的文件和磁盘对照，被别的东西改过或删掉的会写回去。内容正确的文件不会被动。
- 应用根目录里 SSR 包读过的文件（比如用 `?raw` 导入的文本文件）会被监视，尽管 `tifa dev` 本来会跳过它的类型：SSR 包会列出它读过的文件，`tifa dev` 只监视这些文件，根目录里的其他文件不看。新包开始读的文件在那个包之后加入。包没读过的文件（比如没有任何地方导入的 Markdown 文件）仍然不会触发任何构建。
- 应用根目录之外、Vite 开发服务器加载的文件（比如工作区里另一个包的源码，或用 `?raw` 导入的文本文件），由 Vite 而不是 `tifa dev` 监视。它变化时，`tifa dev` 也会重建 SSR 包，不管是什么类型的文件，和应用自己的前端文件一样，日志里会用相对应用根目录的路径写出它。这类文件可能恰好在 Vite 开发服务器重启期间变化，这时旧的监视器和新的监视器都看不到。所以每次开发服务器的重启结束时，不管是什么引起的、有没有成功，`tifa dev` 都会重建一次 SSR 包（重叠的重启算作一次），日志里会写 `Vite's dev server restarted`。新的监视器会继续监视旧监视器监视的根目录之外的文件。
- Vite 重启开发服务器时不会等新的文件监视器读完应用根目录，监视器还没读到的文件没有起点：在那之前的编辑永远看不到，所以开发服务器已经提供过的模块会一直是旧的，直到这个文件再次变化。因此重启之后，开发服务器要等监视器读完文件才响应请求，在大应用里这会更久。
- `go:embed` 放进程序里的文件是程序的一部分，所以改动会重新构建它，尽管这个文件不是 Go。`tifa dev` 通过静态分析得知是哪些文件，静态分析会向 go 命令询问模块里每个包嵌入的文件和模式，从此开始监视它们，也包括 `//go:embed posts` 或 `posts/*.md` 这样的模式新匹配到的文件。不带 `all:` 命名的文件夹会像 go 命令一样，忽略以 `.` 或 `_` 开头的名字，带自己 `go.mod` 的文件夹（那是另一个模块）也一样忽略。文件出现或消失时会重新跑分析，因为这可能改变哪些文件被嵌入，而编辑只需要 go build。既被嵌入又是前端文件的文件（比如页面导入的 `.json`）两个步骤都会跑。位于 `tifa dev` 跳过的文件夹里的嵌入文件（见上）不会被监视。第一次分析成功之前，嵌入的文件是不知道的。
- 改动 public 文件夹里图片、字体或其他文件的内容不会触发重建。下一次 bundle 只复制变过的文件，并删掉已经没了的，所以没动过大 public 文件的重建不花任何代价。在 public 文件夹里增加或删除文件会运行 bundle 步骤，它会重新生成代码，因为生成的路由代码会列出 public 文件（见[路由树](build.zh-CN.md#route-tree)）。日志里写出这个文件的方式和任何其他改动一样。`tifa.yaml` 里 `build.public` 变了会换掉被监视的文件夹，不需要重启。
- content 不需要 go build：开发程序从 `.tifa/dist` 读 `content.json` 和正文（见[内容](../pages-and-data/content.zh-CN.md)）。content 步骤会用 Go 重新扫描 `content/`，在 Vite 开发服务器的 Node 进程里转换变过的条目，并写出 `.tifa/dist/content/` 和 `content.json`。每次转换都把正文写进 `.tifa/dist/content/` 下它自己的文件夹，所以应用正在显示的正文不会在它底下被改掉。然后运行中的应用重新加载内容，对每个条目运行 `Validate`，换上新的数据和正文，此后你加载的页面显示的就是新内容。应用不会重启，`OnStart` 也不会再运行。第一次启动会把所有条目转换一遍。在有集合的应用里，Go 改动之后 content 步骤也会跑，因为集合或它们的 schema 类型可能变了，变了的话就转换所有条目。
- 在 `tifa dev` 下，内容里包含草稿（`Draft` 为 true）和日期还没到的条目（带它们的 `Date`），所以你写一篇文章时能在列表和它自己的页面上看到它。页面可以给它们打上标记。
- front matter 不符合 schema，`content/` 下有没有任何集合声明的文件夹，或者插件出错，都会显示带有 `tifa build` 那条消息的覆盖层，应用保留它已有的内容。条目没通过 `Validate` 时，会显示标题为 `The app did not load the content` 的覆盖层，应用同样保留已有内容，正文也还是旧的。
- 样式表通过 Vite 的开发服务器，所以 `@tailwindcss/vite` 编译 Tailwind CSS 的方式和在任何 Vite 应用里一样。SSR 包里没有 CSS。它构建的同时，开发服务器会编译页面链接的样式表，所以编译不过的样式表会让 bundle 这一步失败，和它让 `tifa build` 失败一样。
- 页面的 head 里链接着服务端渲染导入的样式表，来自 Vite 的开发服务器，所以页面在脚本运行之前就有样式，和构建出来的页面一样。样式表变化时，Vite 会更新这些链接。只有 `.client` 模块导入的样式表会随它们的脚本一起到达。
- 页面的 head 里带有 Vite 客户端和 React refresh 运行时，没有 Vite 插件用 `transformIndexHtml` 添加的任何其他标签（统计代码、manifest 链接、favicon 之类）。`tifa build` 没有 `index.html`，也从不运行这个 hook，所以这些标签在 `tifa dev` 下会出现在页面里，在生产环境里却没有。请把这样的标签放进根 `layout.tsx` 的 `head` 导出里。`tifa dev` 每省略一个，启动时都会打印一次警告，写出这个标签。
- 模块按 URL 导入的图片或其他文件，在服务端渲染里也拿到 Vite 开发服务器的 URL，和浏览器模块用的是同一个，所以 hydration 能对上。`tifa build` 会给它 `/assets/` 下一个带哈希的文件。
- `build.target` 只对 `tifa build` 生效。Vite 的开发服务器发送的是现代 JavaScript。

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

当一次渲染不带 locale 调用 Intl 或 `toLocale*` 方法，或者对日期不带 `timeZone` 时，应用会记一条 `tifa: ssr:` 警告，写出你源码里的位置，每个位置一次。页面照常渲染。见 [`tifa dev` 里的警告](../pages-and-data/server-render.zh-CN.md#warnings-in-tifa-dev)。

`tifa dev` 把每个页面渲染两次，第二次把时钟拨快 36 小时。当同样的数据两次渲染结果不同，说明页面在渲染时读了时间或随机数，渲染记忆 memo 会把这个值冻住。终端和页面上的覆盖层都会指出这一点，附上两次渲染最先分开的位置周围的 HTML，以及你代码里读时间（`Date.now()`、`new Date()`、`Date()`）或随机数（`Math.random()`、`crypto`）的位置。包内部的读取会记在调用它们的那一行你的代码上，React、TanStack Router 和 Tifa 的读取不列出。请修改组件，或者在该页面的 `route.go` 里写 `Memo = false`（见 [SSR 与缓存](../pages-and-data/ssr-and-cache.zh-CN.md#ssr-and-cache)）。这样的页面在修好之前不会被缓存。构建出来的程序不会渲染两次。

## 构建失败时 {#when-a-build-fails}

- 旧版本继续运行。日志会打印 `build failed: <reason>`，已打开的页面会出现错误覆盖层，显示 Go 编译器或 Vite 的原始消息。
- 修好之后覆盖层消失。如果修复需要 go build，页面会重新加载，否则不会。失败的构建没做完的步骤，会并入下一次重建。
- 在浏览器里编译失败的模块会显示 Vite 自己的覆盖层。
- 如果安装的 TanStack Router 不是 Tifa 测试过的版本（见 [build.md](build.zh-CN.md#the-apps-tsconfig-and-vite-config)），bundle 步骤会带着那条错误失败。`tifa dev` 不监视 `node_modules`，所以装好测试过的版本之后，要重启 `tifa dev`。
- 如果安装的 React 或 React DOM 低于 19.3.0（见 [build.md](build.zh-CN.md#the-apps-tsconfig-and-vite-config)），bundle 步骤会带着那条错误失败。装上更新的版本后要重启 `tifa dev`。
- 如果最开始的第一次构建就失败，就没有旧版本可以提供。每个请求都得到 502 和原始错误，修好之后会自己恢复。
- 如果应用启动后马上退出，或者 30 秒内没有监听，覆盖层的标题是 `The app did not start`。正文显示退出状态和应用写到 stderr 的最后 40 行，`OnStart` 的错误和 panic 都在那里。只要输出以那个 panic 的报告结尾，panic 那一行即使堆栈更长也会保留。应用记下日志后继续运行的 `panic: ` 行则不会。如果运行中的应用退出了（比如请求之外发生了 panic），标题是 `The app stopped`，正文相同。到下一次构建之前，页面请求会得到 502 和同样的文字。`tifa dev` 的日志只说应用退出了，因为应用自己的输出已经打印在它上面。
- 如果运行中的应用无法加载重建后的 SSR 包（比如 beastjs 编译不了，或者它的顶层代码抛了错），覆盖层标题是 `The app did not load the SSR bundle`。应用继续使用它已有的包。堆栈给出源文件里的位置，调用了 beastjs 没有的 `require` 的包，会连同该怎么做一起指出来。

你可以关掉覆盖层。关掉它不影响以后的重新加载。

## 浏览器里的错误 {#errors-in-the-browser}

浏览器里发生的错误会显示在右下角的一个小面板里。它不妨碍页面使用，构建错误的覆盖层会盖住它并保持在最上面。

- `Hydration mismatch`：React 没法复用服务器的 HTML，在浏览器里把页面重新渲染了一遍。面板显示 React 的消息，带上差异，以及组件堆栈。React 自己能恢复的其他错误，标题是 `Recoverable error`。
- `Uncaught error`：某个组件抛了错，而它外面没有错误边界，或者其他错误冒到了 `window`。
- `Unhandled rejection`：某个 promise 失败了，没有人处理。

同样的错误再次出现时，只会增加计数（`×3`），不会新增条目。面板保留最近 20 个不同的错误。每个都可以关掉，重新加载会清空它们。被 `ErrorBoundary` 组件捕获的错误不显示，和以前一样它们只会到控制台。`tifa dev` 为 [Tifa 错误](#tifa-errors)保存同样的错误。构建出来的应用没有这些：这些代码只在开发构建里。

## 页面渲染失败时 {#when-a-page-fails-to-render}

服务端渲染页面时如果组件抛了错，应用会把页面重新渲染一遍，用 `ErrorBoundary` 组件显示错误（见[页面上的错误](../pages-and-data/pages.zh-CN.md#errors-on-a-page)）。在 `tifa dev` 下，这个页面上会有一个覆盖层，写出这个请求，在公开消息和状态文字不同时显示公开消息，并显示完整的错误，堆栈指向你的源文件。如果没有 `ErrorBoundary` 渲染，或者页面请求在应用能渲染页面之前就失败了（比如 `OnRequest` 返回了意料之外的错误），应用会用 Go 返回一个朴素的页面，在 `tifa dev` 下它显示同样的内容。两种页面都带着 `OnError` 选的状态码，所以 `curl -i` 也能看到，并且在你保存修复之后重新加载，用 TSX 做的、不会重新加载其他页面的修复也一样。构建出来的应用只显示状态和公开消息。

这些页面会显示内部错误。`tifa dev` 默认监听 `127.0.0.1`。用 `--addr 0.0.0.0:<port>` 时，网络上的其他机器能看到它们。

重新加载和覆盖层走的是 `tifa dev` 自己的连接 `/__tifa/dev/events`，不是 Vite 的 HMR websocket。Vite 的开发服务器只在第一次 bundle 步骤里启动，在静态分析和代码生成之后，没有页面的应用则从不启动它。没有应用在运行时回答请求的那个页面（第一次构建失败或崩溃之后），也必须显示错误，并在修好后重新加载，而这只有靠 `tifa dev` 自己从一开始就提供的连接才做得到。浏览器的所有标签页通过一个 SharedWorker 共用这个连接。

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

告诉智能体这些命令的 skill 和说明，由 [`tifa agents`](build.zh-CN.md#tifa-agents) 写出。

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
  build:   failed: go build: app/store.go:3:2: undefined: x
  log:     /work/shop/.tifa/dev.log
```

`--json` 打印一个对象。`build.state` 是 `starting`（第一次构建在跑）、`building`、`ok`、`failed`（这时 `build.error` 是错误的第一行）或 `unknown`（服务器没有响应，`build.error` 说明原因）。对于打印到自己终端的 `tifa dev`，`log` 为空。没有运行时它打印 `{"running": false}`。

```json
{
  "running": true,
  "pid": 41231,
  "url": "http://127.0.0.1:9527",
  "started": "2026-11-16T14:22:50Z",
  "build": {"state": "ok"},
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

## 在 Windows 上 {#on-windows}

`tifa dev` 在 Windows 上的工作方式相同，只有这些不同：

- Ctrl-C、Ctrl-Break 和关闭控制台窗口都会停掉它。Windows 没有一个程序能发给另一个程序的信号，所以 `tifa dev stop` 会设置一个 `tifa dev` 在等的事件 `Local\tifa-dev-stop-<pid>`。这个事件属于 Windows 登录会话，所以 `stop` 只能到达在同一个会话里启动的 `tifa dev`。
- 重启应用时，`tifa dev` 关闭应用的标准输入（应用会一直读到它结束），然后像 SIGTERM 那样关闭：`OnShutdown` 会运行。应用的代码不需要为此做任何事。
- `tifa dev` 运行在一个 job object 里（Windows 把一组进程当作一个整体来处理的方式），应用在它里面自己的一个 job object 里。`tifa dev` 被杀掉时（从任务管理器或 `taskkill /F`），Windows 会同时结束 Node、Vite、`go build`、应用以及应用启动的东西。这时应用得不到 `OnShutdown`。如果 Windows 拒绝建立 job object，`tifa dev` 会立刻停下，报一个写出失败调用名的错误，什么也不启动。
- `tifa dev` 要等写文件的程序把文件关掉才看得到已保存的文件。写完后仍然保持文件打开的编辑器，可能让下一个页面请求拿不到改动。
- 保存之后，`tifa dev` 马上会读这个文件，看它有没有变。Windows 不允许别的程序把另一个文件改名覆盖到一个打开着的文件上，所以恰好在这一刻到来的保存可能失败，提示 "Access is denied"。编辑器会重试，用改名来保存的脚本也应该重试。
- Windows 不允许覆盖正在运行的程序。每次 Go 构建之前，`tifa dev` 都会把应用运行所用的程序 `bin\tifa-dev.exe` 改名为 `bin\tifa-dev.old-<number>.exe`，并删除前几次构建留下、现在已经不在运行的那些。
- `--background` 运行 `tifa dev` 时不带控制台窗口。Node、`go` 和应用共用它那个隐藏的控制台，所以不会弹出窗口。
- 不带 `--addr` 时，Windows 为 Hyper-V、WSL 或 Docker 保留的端口（`netsh interface ipv4 show excludedportrange protocol=tcp`）算作被占用，`tifa dev` 会换到下一个。

## Tifa 错误 {#tifa-errors}

```sh
tifa errors [--dir app-root] [--json]
```

`tifa dev` 运行期间，`go tool tifa errors` 会打印上一次构建开始以来的错误。既看不到浏览器、也看不到 `tifa dev` 终端的编码智能体，靠它得知改动有没有用。改完代码之后，以及打开或重新加载你改过的页面之后，都要运行它：浏览器的错误只来自已打开的页面。

它列出：

- 覆盖层显示的内容：`Build failed`，带 Go 编译器或 Vite 的原始消息，还有 `The app did not start`、`The app stopped` 或 `The app did not load the SSR bundle`。它保留到下一次构建结束。
- 请求的意外错误，也就是交给 `OnError` 的那些（没有 `OnError` 时则按 500 记入日志）：请求、状态和用户看到的消息，以及错误本身，堆栈指向你的源文件，和 `tifa dev` 下错误页面显示的一样。API 路由的请求也算，没有页面的应用里同样如此。
- 页面报告的浏览器错误：`Hydration mismatch`、`Recoverable error`、`Uncaught error` 和 `Unhandled rejection`，名字和面板里一样，先列报告它的页面。
- 两次渲染不同的页面（见[日志](#log)）。
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

## 文件放在哪里 {#where-files-go}

| 路径 | 内容 |
| --- | --- |
| `.tifa/` | 生成的代码，和 `tifa build` 一样 |
| `.tifa/dev.lock` | `tifa dev` 和 `tifa build` 的锁，带持有者的进程 ID、地址和启动时间 |
| `.tifa/dev.log` | `tifa dev --background` 打印的全部内容，见[智能体与脚本](#agents-and-scripts) |
| `.tifa/dist/` | SSR 包、`build.json`、public 文件，以及有集合的应用的 `content.json` 和 `content/` 里的正文。浏览器模块改由 Vite 的开发服务器提供 |
| `bin/tifa-dev` | 开发用的可执行文件 |

应用在应用根目录里运行，即使 `tifa dev` 是用 `--dir` 从别处启动的，这和你从应用根目录运行时 `tifa build` 生成的程序一样。应用按相对路径打开的文件，比如 [`tifa new`](build.zh-CN.md#tifa-new) 模板的 `app.db`，就落在那里。它读取的 `.env` 也是（见 [.env](config.zh-CN.md#env)）。`tifa dev` 通过 `SERVER_ADDR` 把应用要监听的地址传给它，这个值优先于你 shell 里的 `PORT` 和 `.env`。

停止应用时，`tifa dev` 最多等配置的 `server.shutdown_timeout` 的 3 倍再加 1 秒，然后杀掉整个进程组。

## 大型应用 {#large-apps}

`tifa dev` 用系统的文件事件监视文件夹。当应用的文件夹或文件多到系统不让一个程序轻松监视时，`tifa dev` 会对其余的做轮询：大约每秒看一次，照常重建。什么都不用设置。日志会说明这一点：

```text
tifa dev: 07:35:36 watching app/assets would use up the system's file watches, so it and the directories found after it are polled for changes
```

- 在 macOS、FreeBSD、OpenBSD、NetBSD 和 DragonFly 上，`tifa dev` 用 kqueue 监视。一个被监视的文件夹占一个文件描述符，里面属于上表所列类型的每个文件、每个嵌入的文件和 `content/` 下的每个文件也各占一个，之后才出现的也一样。其他文件，比如 `content/` 之外的图片，不占。`tifa dev` 拿这些去和一个进程可以打开的数量的一半比（打开文件上限和 `kern.maxfilesperproc` 中较小的那个），超出的文件夹改为轮询，被监视的文件夹里文件增长超过这个数时也一样。
- 在 Linux 上，监视一个文件夹占用户的一个 inotify watch，编辑器和其他工具共用这些 watch。`tifa dev` 最多用 `fs.inotify.max_user_watches` 的一半。
- 如果系统先耗尽了，因为其他程序占着很多 watch 或文件，`tifa dev` 会对它没法监视的文件夹做轮询，交还它持有的几个 watch，并继续工作。如果它在 `tifa dev` 运行期间耗尽，比如某个文件打不开，`tifa dev` 就停止用系统事件监视，轮询每个文件夹，并重新比较每个文件。
- 一次轮询会列出被轮询的文件夹，只对大小、修改时间或身份变过的文件算哈希。两次轮询之间，`tifa dev` 至少等一秒，并且至少等上一次轮询耗时的十倍。一个被监视、条目有变化的文件夹，按同样的规则重新列出，不受一秒下限限制，所以一个不断增长的文件夹不会花得更多。因此轮询和列出各自最多占一个 CPU 核的十分之一左右，这还在处理文件事件和重建之外。
- 一次轮询要多久，随文件数增长，也取决于机器和它的磁盘。在 Apple M4 上，文件都在系统缓存里时，我们的运行中对 20,000 个文件的一次轮询用了 45 到 65 毫秒。只要一次轮询用时不到十分之一秒，被轮询文件夹里的改动大约一秒内就会被发现，然后照常构建。
- 被轮询的文件夹在 `tifa dev` 重启之前一直保持轮询。
- 在被轮询文件夹里保存之后，轮询发现改动之前立刻请求的页面，拿到的是旧的服务端渲染结果，而 Vite 随后触发的重新加载会 hydrate 失败。在 `ready` 那一行之后再重新加载一次。
- Vite 的开发服务器也监视应用根目录。一旦 `tifa dev` 因为系统资源不足而轮询，它也会让 Vite 的开发服务器同样轮询，除非开发服务器用的是 FSEvents（在 macOS 上通常如此）。FSEvents 不为每个文件占文件描述符，所以保持不变。
- 要轮询，Vite 的开发服务器会重启，之后每秒查看每个文件一次，在 Apple M4 上 10,000 个文件约占一个 CPU 核的 8%。重启成功、并且没有其他开发服务器重启（比如改 `vite.config.ts` 之后的那种）同时在跑时，这只是一次重启。正在运行的重启会先被等待，如果它成功了而且它的新服务器已经在轮询，就不会再重启别的。
- 重启可能失败。无法创建新服务器的（比如 `vite.config.ts` 坏了），保留旧服务器和它的监视器。新服务器没法监听的（因为另一个程序这时占了端口），已经关掉了旧服务器，所以在下一次重启之前没有 Vite 开发服务器。不管哪种，日志都会说明，`tifa dev` 和应用继续运行。开发服务器在它的监视器下一次出错时再试，下一次成功的重启（比如改 `vite.config.ts` 之后的那种）会轮询。
- 当 Vite 自己的监视器先耗尽 watch 或文件描述符（ENOSPC、EMFILE 或 ENFILE）时，开发服务器会以同样的方式切换到轮询，并说明一次。用 FSEvents 的监视器也是如此。
