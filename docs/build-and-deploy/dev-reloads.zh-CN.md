---
summary: '`tifa dev` 在每一种文件改动之后做什么：运行哪些步骤，已打开的页面怎么变'
audience: app
order: 3
translatedFrom: d51a4006594f
read_when:
  - '`tifa dev` 在文件改动后没有更新页面，或者报出你看不懂的错误'
---

# 每种改动会触发什么 {#what-each-change-triggers}

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
- 改动 public 文件夹里图片、字体或其他文件的内容不会触发重建。下一次 bundle 只复制变过的文件，并删掉已经没了的，所以没动过大 public 文件的重建不花任何代价。在 public 文件夹里增加或删除文件会运行 bundle 步骤，它会重新生成代码，因为生成的路由代码会列出 public 文件（见[路由树](../reference/route-tree.zh-CN.md#route-tree)）。日志里写出这个文件的方式和任何其他改动一样。`tifa.yaml` 里 `build.public` 变了会换掉被监视的文件夹，不需要重启。
- content 不需要 go build：开发程序从 `.tifa/dist` 读 `content.json` 和正文（见[内容](../pages-and-data/content.zh-CN.md)）。content 步骤会用 Go 重新扫描 `content/`，在 Vite 开发服务器的 Node 进程里转换变过的条目，并写出 `.tifa/dist/content/` 和 `content.json`。每次转换都把正文写进 `.tifa/dist/content/` 下它自己的文件夹，所以应用正在显示的正文不会在它底下被改掉。然后运行中的应用重新加载内容，对每个条目运行 `Validate`，换上新的数据和正文，此后你加载的页面显示的就是新内容。应用不会重启，`OnStart` 也不会再运行。第一次启动会把所有条目转换一遍。在有集合的应用里，Go 改动之后 content 步骤也会跑，因为集合或它们的 schema 类型可能变了，变了的话就转换所有条目。
- 在 `tifa dev` 下，内容里包含草稿（`Draft` 为 true）和日期还没到的条目（带它们的 `Date`），所以你写一篇文章时能在列表和它自己的页面上看到它。页面可以给它们打上标记。
- front matter 不符合 schema，`content/` 下有没有任何集合声明的文件夹，或者插件出错，都会显示带有 `tifa build` 那条消息的覆盖层，应用保留它已有的内容。条目没通过 `Validate` 时，会显示标题为 `The app did not load the content` 的覆盖层，应用同样保留已有内容，正文也还是旧的。
- 样式表通过 Vite 的开发服务器，所以 `@tailwindcss/vite` 编译 Tailwind CSS 的方式和在任何 Vite 应用里一样。SSR 包里没有 CSS。它构建的同时，开发服务器会编译页面链接的样式表，所以编译不过的样式表会让 bundle 这一步失败，和它让 `tifa build` 失败一样。
- 页面的 head 里链接着服务端渲染导入的样式表，来自 Vite 的开发服务器，所以页面在脚本运行之前就有样式，和构建出来的页面一样。样式表变化时，Vite 会更新这些链接。只有 `.client` 模块导入的样式表会随它们的脚本一起到达。
- 页面的 head 里带有 Vite 客户端和 React refresh 运行时，没有 Vite 插件用 `transformIndexHtml` 添加的任何其他标签（统计代码、manifest 链接、favicon 之类）。`tifa build` 没有 `index.html`，也从不运行这个 hook，所以这些标签在 `tifa dev` 下会出现在页面里，在生产环境里却没有。请把这样的标签放进根 `layout.tsx` 的 `head` 导出里。`tifa dev` 每省略一个，启动时都会打印一次警告，写出这个标签。
- 模块按 URL 导入的图片或其他文件，在服务端渲染里也拿到 Vite 开发服务器的 URL，和浏览器模块用的是同一个，所以 hydration 能对上。`tifa build` 会给它 `/assets/` 下一个带哈希的文件。
- `build.target` 只对 `tifa build` 生效。Vite 的开发服务器发送的是现代 JavaScript。
