---
summary: 'tifa dev 怎么用：每类改动重建哪些步骤、日志怎么读、构建失败时页面和应用怎样、开发产物放在哪'
read_when:
  - 第一次在本地跑 tifa 应用
  - 改 internal/devserver、cmd/tifa 的 dev 命令或 internal/bundle 的增量构建
  - tifa dev 改了文件却没刷新，或报出看不懂的错误
---

[English](dev.md) · 简体中文

# tifa dev

```sh
tifa dev [--dir 应用根目录] [--addr 127.0.0.1:9527]
```

启动后先打印 `tifa dev: serving http://<地址>`，然后做一次完整构建（步骤同 [tifa build](build.zh-CN.md)），启动应用。浏览器连 `--addr`，tifa dev 把请求转给应用，应用重启期间请求会等它起来。Ctrl-C 或 SIGTERM 停掉应用和 tifa dev。

打包用 tifa 内置的 esbuild。用了 Tailwind CSS 的样式表交给应用的 Tailwind 包在 Node 上编译，见 [app.zh-CN.md](app.zh-CN.md#tailwind-css)。

## 改动触发什么

tifa dev 监视应用根目录，跳过 `.git`、`.tifa`、`node_modules` 和放开发可执行文件的 `bin/`。同一批改动等文件安静下来再一起处理。

| 改了什么 | 执行的步骤 |
| --- | --- |
| `.ts`、`.tsx`、`.js`、`.css`、`.json` | 打包 |
| `.go`、`go.mod`、`go.sum` | 静态分析、go build |
| 上一行且路由清单变了（路由、Load 和 Layout 的返回类型、方法函数的入参和结果类型、钩子等） | 静态分析、生成代码、打包、go build |
| 新增或删除 `route.tsx`、`layout.tsx`，或改 `tifa.yaml`、`tifa.json` | 全部步骤 |

路由清单有没有变，看的是静态分析后生成的 `.tifa/` 内容。只改函数体时生成内容不变，跳过生成和打包。

每次重建后重启应用，打开着的页面自动刷新。只改前端时不需要 go build：开发用的程序以 `serve --dist .tifa/dist` 启动，重启后直接读新的前端产物。

公共目录里的图片、字体等文件不触发重建，下一次打包时才复制过去。

用了 Tailwind 的样式表每次打包都重新编译，因为改 TSX 可能加上或去掉类名。Tailwind 也从不触发打包的文件里找类名，比如 Go 或 HTML 文件，只写在这些文件里的类名要到下一次打包才生效。

## 日志

每个执行的步骤一行，带耗时，最后一行是从开始重建到应用可用的总耗时：

```text
tifa dev: 07:35:36 app/home/route.tsx modified
tifa dev: 07:35:36 bundle done in 51ms
tifa dev: 07:35:37 ready in 201ms
```

步骤名是 `static analysis`、`code generation`、`bundle`、`go build`。样式表用了 Tailwind 时，bundle 这一行还带上 Tailwind 的耗时，如 `bundle done in 320ms (tailwind 80ms)`。静态分析之后还有一行 `route manifest changed` 或 `route manifest unchanged`。应用自己的输出照常打印。

## 构建失败

- 失败时旧版本继续运行，日志打印 `build failed: <原因>`，打开的页面盖上错误浮层，显示编译器或 esbuild 的原文。
- 修好后浮层消失，页面刷新。失败那次没做完的步骤会并入下一次重建。
- 首次构建就失败时没有旧版本可用，所有请求答 502 和错误原文，等修好后自动恢复。
- 应用启动后立刻退出时，浮层标题是 `The app did not start`，内容是应用的退出原因。

浮层可以关掉，关掉后不影响后续刷新。

## 文件放在哪

| 路径 | 内容 |
| --- | --- |
| `.tifa/` | 生成代码，与 `tifa build` 相同 |
| `.tifa/dist/` | 前端产物，按增量构建更新 |
| `bin/tifa-dev` | 开发用的可执行文件 |

停止应用时最多等配置里 `server.shutdown_timeout` 的 3 倍再加 1 秒，超时就强行结束整个进程组。

## 监视的文件太多

macOS 上监视一个目录要为里面每个文件占一个文件描述符。报错提示描述符不够时，用 `ulimit -n` 调高上限。Linux 上报监视数量到上限时，调高 `fs.inotify.max_user_watches`。
