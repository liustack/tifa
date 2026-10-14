---
summary: 'tifa dev 怎么用：每类改动后页面怎么更新、执行哪些步骤、Vite 在其中做什么、日志怎么读、构建失败时页面和应用怎样、开发产物放在哪'
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

启动后先打印 `tifa dev: serving http://<地址>`，然后做一次完整构建（步骤同 [tifa build](build.zh-CN.md)），启动应用。浏览器只连 `--addr`。tifa dev 把页面和数据请求转给应用，把 `/__tifa/vite/` 下的请求（浏览器模块和 Vite 的 HMR websocket）转给 Vite 的开发服务器。应用重启期间请求会等它起来。Ctrl-C 或 SIGTERM 停掉应用、Vite 和 tifa dev。

## Vite

tifa dev 在 Node 上跑应用自己装的 Vite，用应用的 `vite.config.ts`（见 [build.zh-CN.md](build.zh-CN.md#应用的-tsconfig-与-vite-配置)）：

- Vite 开发服务器提供浏览器模块。有页面的应用第一次构建时启动它，它监听 127.0.0.1 上的一个空闲端口，只有 tifa dev 跟它通信。页面从它加载 Vite 客户端、`@vitejs/plugin-react` 的 React Fast Refresh 运行时和 `.tifa/client.ts`。
- SSR 包仍是文件 `.tifa/dist/ssr.js`，应用把它载入 QuickJS VM。它和开发服务器在同一个 Node 进程里构建，但跑在单独的工作线程上：浏览器用 React 的开发版，QuickJS 跑生产版，而 Vite 每个线程只定一次用哪个版本。`NODE_ENV` 由 tifa 自己设（开发服务器是 development，SSR 包是 production），shell 里的 `NODE_ENV` 两边都不影响。

## 改动触发什么

tifa dev 监视应用根目录，跳过 `.git`、`.tifa`、`node_modules` 和放开发可执行文件的 `bin/`。同一批改动等文件安静下来再一起处理。

| 改了什么 | 执行的步骤 | 打开着的页面 |
| --- | --- | --- |
| `.ts`、`.tsx`、`.js`、`.css`、`.json` | 打包（SSR 包） | 由 Vite 原地更新 |
| `.go`、`go.mod`、`go.sum` | 静态分析、go build | 刷新 |
| 上一行且路由清单变了（路由、Load 和 Layout 的返回类型、方法函数的入参和结果类型、钩子等） | 静态分析、生成代码、打包、go build | 刷新 |
| 新增或删除 `route.tsx`、`layout.tsx`，或改 `tifa.yaml`、`tifa.json` | 全部步骤 | 刷新 |

- 前端改动经 Vite 送到打开着的页面：React Fast Refresh 换掉改过的组件并保留它们的状态，新 CSS 替换旧的。模块换不了时（比如除了组件还导出了别的东西），Vite 自己刷新页面。
- 同时 tifa dev 重建 SSR 包并重启应用，不做 go build：开发用的程序以 `serve --dist .tifa/dist` 启动，重启后读新的包。这期间到达的页面请求会等重启后的应用，所以刷新看到的总是新的服务端渲染。
- 路由清单有没有变，看的是静态分析后生成的 `.tifa/` 内容。只改函数体时生成内容不变，跳过生成和打包。
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

步骤名是 `static analysis`、`code generation`、`bundle`、`go build`。静态分析之后还有一行 `route manifest changed` 或 `route manifest unchanged`。Vite 的警告和错误也以 `tifa dev:` 行打印。应用自己的输出照常打印。

## 构建失败

- 失败时旧版本继续运行，日志打印 `build failed: <原因>`，打开的页面盖上错误浮层，显示 Go 编译器或 Vite 的原文。
- 修好后浮层消失。修复需要 go build 时页面刷新，否则页面保持不动。失败那次没做完的步骤会并入下一次重建。
- 浏览器里编译失败的模块显示 Vite 自己的浮层。
- 首次构建就失败时没有旧版本可用，所有请求答 502 和错误原文，等修好后自动恢复。
- 应用启动后立刻退出时，浮层标题是 `The app did not start`，内容是应用的退出原因。

浮层可以关掉，关掉后不影响后续刷新。

## 文件放在哪

| 路径 | 内容 |
| --- | --- |
| `.tifa/` | 生成代码，与 `tifa build` 相同 |
| `.tifa/dist/` | SSR 包、`build.json` 和公共文件。浏览器模块改由 Vite 开发服务器提供 |
| `bin/tifa-dev` | 开发用的可执行文件 |

停止应用时最多等配置里 `server.shutdown_timeout` 的 3 倍再加 1 秒，超时就强行结束整个进程组。

## 监视的文件太多

macOS 上监视一个目录要为里面每个文件占一个文件描述符。报错提示描述符不够时，用 `ulimit -n` 调高上限。Linux 上报监视数量到上限时，调高 `fs.inotify.max_user_watches`。
