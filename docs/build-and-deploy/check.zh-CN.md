---
summary: '检查应用，读出 Tifa 对它的了解：`tifa check`、`gen`、`routes`、`version`、`info`'
audience: app
order: 7
translatedFrom: 2feb77c805bd
read_when:
  - '修改 `tifa check`，或者想知道它运行哪些步骤、需要什么'
  - '想知道应用用哪个版本的 Tifa、Go、Node 或 pnpm 构建，或者要写 bug 报告'
  - '在刚克隆下来的仓库里用 `tifa gen` 写出 `.tifa/`，或者用 `tifa routes` 列出应用的路由和内置地址'
---

# 检查应用 {#check-the-app}

这些命令只查看应用，不构建程序：改完之后要跑的那一个检查，代码生成这一步，路由列表和正在用的版本。

## `tifa check` {#tifa-check}

```sh
tifa check [--dir app-root]
```

用一条命令运行应用的所有检查，在第一个失败的步骤处停下：

1. `analyze`：`tifa build` 的静态分析。每个违反约定的地方都打印成 `file:line: reason`。它还会读代码里其他框架的惯用写法，比如 `next/link`、`'use client'` 或导出的 `loader`，并说明在 Tifa 里该怎么写（见[从其他框架过来](../start/app.zh-CN.md#coming-from-another-framework)）。TypeScript 里的惯用写法在 `generate` 步骤里读，那一步有 Node。
2. `generate`：和 `tifa build` 一样写出 `.tifa/`，让 TypeScript 步骤需要的类型保持最新。它只对有页面的应用启动 Node。当应用里有 `tifa dev` 或 `tifa build` 正在运行时，它们持有 `.tifa/` 的锁并让文件保持最新，而构建产生的文件不是 `tifa dev` 需要的。所以这一步这时什么也不写，并打印一行说明这一点，检查继续使用它在内存里生成的文件。它从不等待锁。
3. `go vet ./...`
4. `go test ./...`
5. `typescript (tsc --noEmit)`：应用安装的 `typescript` 包里的 `tsc`，在应用根目录用 `node` 运行，使用应用的 `tsconfig.json`。Tifa 找这个包的方式和 Node 一样，在应用或它上层文件夹的 `node_modules` 里找，也会跟着 pnpm 的链接找，所以 npm 和 pnpm 都能用，而且它不会安装任何东西。没有安装 `typescript` 的应用会在这里失败，并被告知去运行它的包管理器的安装命令。没有页面的应用会跳过这一步，并打印 `typescript: skipped, the app has no pages (no Node needed)`。

每一步都会打印一行以 `==> ` 开头的内容，然后是它的工具打印的任何东西。Go 以 `CGO_ENABLED=0` 运行，和 `tifa build` 一样。某一步失败时，命令会以非零状态结束，并给出一条指出该步骤的错误。后面的步骤不会运行。

```text
==> analyze
==> generate
==> go vet ./...
vetbad.go:5:29: fmt.Printf format %d has arg "text" of wrong type string
tifa: check failed at go vet ./...: exit status 1
```

`tifa new` 会把 `go tool tifa check` 写进新应用的 `AGENTS.md`，作为完成一次修改之前要运行的唯一命令。

## `tifa gen` {#tifa-gen}

```sh
tifa gen [--dir app-root]
```

运行分析并写出 `.tifa/`，也就是 `tifa check` 的前两步，不构建任何东西。`tifa dev` 和 `tifa build` 也会写它，所以只有在它们都还没运行过的地方才需要它，比如刚克隆下来的仓库：应用的 tsconfig 继承 `.tifa/tsconfig.json`，路由的类型也在 `.tifa/` 里。违反约定时它会失败，和构建失败一样。它只对有页面的应用启动 Node。当应用里有 `tifa dev` 或 `tifa build` 正在运行时，`tifa gen` 不动 `.tifa/`：它会打印一行，说明正在运行的命令会让它保持最新，然后成功退出，所以它不会让安装失败。

`tifa new` 写出的 `package.json` 在安装时运行它：

```json
"scripts": {
  "prepare": "go tool tifa gen"
}
```

pnpm 和 npm 会在 `pnpm install` 和 `npm install` 之后运行项目的 `prepare` 脚本，所以安装一个克隆仓库的 npm 包时也会写出 `.tifa/`。请先运行 `go mod download`（或 `go mod tidy`），因为 `go tool tifa` 需要这些模块。带 `--ignore-scripts` 的安装会跳过 `prepare`，所以安装之后请运行 `go tool tifa gen`。

同一个文件里的 `typecheck` 脚本是 `go tool tifa gen && tsc --noEmit`。`.tifa/` 里的类型来自 `route.go`，所以改了路由的返回类型，或者增删了页面和布局之后，它们就旧了。先运行 `gen`，`tsc` 读到的就是新类型，而且 Go 代码编译不过时它会停在这一步，并打印 Go 文件和行号。`tifa check` 在运行 `tsc` 之前已经会生成，所以不需要这个脚本。更早做出来的应用可以把自己的 `typecheck` 脚本改成同样的一行。

`route.gen.ts` 或 `session.gen.ts` 里每个由 Go 结构体生成的类型，前面都有一行注释，写明它来自哪里：Go 包的导入路径、类型名，以及文件和行号，比如 `/** Go: example.com/app/projects.Project (projects/project.go:12) */`。在编辑器里把鼠标放到类型上就能看到，要改的就是那个文件。别的模块里的类型只写包和名字，因为模块缓存里的路径在每台机器上不一样。这行注释只在生成的文件里：类型不会留在包里，构建 ID 来自包里的文件，所以把一个结构体挪到另一行，只会改变 `.tifa/`，不会改变这次构建。

## Tifa routes {#tifa-routes}

```sh
tifa routes [--dir app-root] [--json]
```

运行和 `tifa build` 相同的静态分析，不生成也不构建任何东西，并打印三张表。第一张是路由：文件夹、URL、布局、方法（页面用 `page`），以及 `SSR` 和 `Memo` 开关。第二张是各个布局。第三张 `BUILT IN` 是程序会响应、但不是 `app/` 下某个文件夹的地址，以及每个地址在什么时候存在。违反约定的错误和 `tifa build` 的打印方式相同。`--json` 改为打印路由清单，其中有每个 `Load` 结果以及每个处理函数的输入和结果的 Go 类型。清单里没有内置地址。

```
BUILT IN                             WHEN
/sitemap.xml                         site.url is set
/sitemap-<n>.xml                     site.url is set and the sitemap has more than 50000 URLs or 50 MiB
/robots.txt                          site.url is set, but public/robots.txt answers instead
/llms.txt                            site.url is set
/llms-full.txt                       site.url is set
/<path>.md                           site.url is set and tifa.SetSitemap lists a page with Content
/__tifa/session                      the app calls tifa.SetSession
/assets/<file>                       the app has a page
/<file>                              public/ has 4 files (2 dot files left out)
/.well-known/acme-challenge/<token>  server.tls_domains is set, on server.http_addr only
```

- `site.url` 可以在程序启动时用选项或环境变量设置，`tifa routes` 看不到这一点。所以站点相关的行始终会列出来，`tifa.yaml` 里没有 `site.url` 时，`site.url is set` 后面会跟 `(not in tifa.yaml)`。
- `public/` 里的文件，或应用在同一路径上的路由，会代替 Tifa 来响应，这一行会写明是哪一个。应用没有内容集合时，`.md` 那一行末尾是 `(no content collections)`。
- `/<file>` 这一行会数出 `public/` 里被复制的文件有几个，以及有几个以点开头的文件或文件夹没有被复制（见 `build.public_dotfiles`）。文件夹里只有这类文件时也会有这一行，写成 `public/ has no files (1 dot file left out)`。
- `/__tifa/session`、`/assets/<file>` 和 `/<file>` 只在成立时才列出：应用调用了 `tifa.SetSession`，有 `page.tsx`，或者 `public/`（由 `build.public` 指定的文件夹）里有文件。`/<file>` 代表该文件夹里的每个文件，在它自己的路径上。
- `/.well-known/acme-challenge/<token>` 在程序自己申请证书时，由 `server.http_addr` 响应（见 [HTTPS](https.zh-CN.md#https)）。它取决于运行中程序的设置，所以 `tifa routes` 始终会列出它。

## 版本和环境 {#version-and-info}

```sh
tifa version
tifa --version
tifa info [--dir app-root] [--json]
```

`tifa version` 打印 `tifa v0.6.0`，`tifa --version` 打印同样的一行。在应用里以 `go tool tifa` 运行时，它是应用的 `go.mod` 所要求的版本。你全局安装的 `tifa` 会把命令交给应用锁定的那个 `tifa`（见[全局安装命令](create-app.zh-CN.md#installing-the-command-globally)），所以它打印的也是那个版本。在 Tifa 的检出里构建的，或者给这个模块写了 `replace` 的构建，打印 `tifa devel`。

`tifa info` 打印写 bug 报告或者让编码 agent 了解这台机器时需要的信息：

```text
Tifa       v0.6.0
Go         go1.26.1
Node       v24.13.0
pnpm       10.33.0
System     darwin/arm64
tifa.yaml  found
```

Go、Node 和 pnpm 的版本是 `PATH` 上那几个程序的版本，也就是 `tifa build` 和 `tifa dev` 运行的那几个。找不到的程序，或者 5 秒内没有回答的程序，写成 `not found`，不会让命令失败。没有页面的应用不需要 Node，所以那里写 `not found` 没有问题。`tifa.yaml` 一行说的是 `--dir` 指的文件夹（默认是当前文件夹）里有没有这个文件。加 `--json` 时命令打印一个对象，键是 `tifa`、`go`、`node`、`pnpm`、`os`、`arch` 和 `tifaYaml`（值为 `true` 或 `false`）。缺少的程序在那里同样是字符串 `"not found"`。
