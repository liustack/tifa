---
summary: 'tifa new 怎么新建应用、tifa build 做什么、.tifa/ 里生成了哪些文件和产物、应用的 tsconfig 与 go.mod 怎么配、生成的程序怎么启动和回答页面请求、怎么按剖析编译（PGO），以及要随程序附带哪些许可声明'
read_when:
  - 改 internal/codegen、cmd/tifa 的 build 命令或 server.Main
  - 新建一个 tifa 应用，或给 test/apps 加应用
  - 看不懂 .tifa/ 下某个文件从哪来
  - 实现页面渲染、导航数据请求，需要知道路由树与数据协议
  - 改 internal/bundle、js/vite、js/host，或升级 SSR 宿主补丁的依赖
  - 要把构建出的程序交给别人，想知道要附带哪些声明
  - 要关掉 PGO，或用自己的剖析构建
---

[English](build.md) · 简体中文

# 构建与生成代码

## tifa new

```sh
tifa new <目录> [--module 模块路径]
```

在不存在或为空的目录里写一个能跑的小应用：一个存在 SQLite 里的待办清单，用来展示 tifa 应用怎么组织。

| 文件 | 内容 |
| --- | --- |
| `go.mod` | tifa、登记为工具的 tifa 命令、纯 Go 的 SQLite 驱动 `modernc.org/sqlite`，以及 `ignore node_modules` |
| `db.go`、`schema.sql` | 在根目录：`var DB`、打开 WAL、`busy_timeout` 和外键检查的 `OpenDB`，以及所有领域的表，每次启动都用 `CREATE TABLE IF NOT EXISTS` 建。数据库文件是应用运行目录下的 `app.db`，或者 `$DB_PATH` |
| `hooks.go` | `OnStart` 打开数据库，`OnShutdown` 关闭它。`OnRequest` 和 `OnError` 只在注释里说明，因为写了 `OnError` 就会替换默认处理 |
| `app/layout.tsx` | 带导航的根 Layout、未知地址的 `NotFound` 和一个 `Error` |
| `app/route.go`、`app/route.tsx` | `/`：清单，加一个表单，它的 `POST` 收一个带 `Validate` 方法的输入。页面显示返回的 `tifa.Fields` 里每个字段的提示 |
| `app/todos/todo.go` | 待办领域，`package todos`：`ListTodos`、`GetTodo`、`AddTodo`、`SetDone`。它 import 根包来用 `DB`，两个页面都 import 它 |
| `app/todos/todo+/` | `/todos/{todo}`：找不到时返回 `tifa.ErrNotFound` 的 `Load`、标记完成的 `POST`，以及 `NotFound` |
| `app/about/route.tsx` | 没有 `route.go` 的页面 |
| `*_test.go` | 测试和被测代码放在一起：待办领域、数据库和钩子的测试，以及用 `tifatest` 写的路由代码测试，每个测试用临时目录里的一个新数据库 |
| `tifa.yaml` | [config.zh-CN.md](config.zh-CN.md) 里的全部设置，按默认值写好并注释掉。`ssr.vms` 的默认值是 GOMAXPROCS，随机器而变，所以写的是示例值 |
| `AGENTS.md`、`CLAUDE.md` | 给编程 agent 的说明：tifa 应用怎么运作，并链接到这些文档。`CLAUDE.md` 只指向 `AGENTS.md` |
| `package.json`、`tsconfig.json`、`vite.config.ts`、`.gitignore`、`README.md` | npm 包、TypeScript 与 Vite 配置、要忽略的构建产物和数据库文件，以及常用命令 |

- 模块路径默认是目录名，`--module` 可以另给。应用要 import 根目录的包，所以路径必须是 go 命令能 import 的。tifa new 拒绝含 `vendor` 段的路径，也拒绝首段不带点、且（不分大小写）和标准库包或 go 命令包模式的首段相同的路径（比如 `fmt`、`net`、`all`），即使并没有同名的包。改传 `--module example.com/fmt`。
- 根包以模块路径最后一段命名（不算 `/v2` 这类主版本后缀），转成小写，只留字母和数字。它必须是合法的 Go 标识符，不是关键字，也不是 `main` 或 `init`（Go 不接受以 `init` 作为被 import 包的名字）。`app/` 下的 Go 文件直接用这个名字 import 它，所以在这些文件用到它的地方，这个名字不能另有所指：它们 import 的包，比如领域 `todos` 或 `tifa`，它们用到的预声明名字，比如 `error`，以及在其作用域里用到根包的参数或变量，比如 `db`、`ctx`。tifa new 维护一份这样的名单，遇到就报错，所以 `tifa new todos` 会失败，提示换一个目录名或用 `--module`，而 `--module example.com/todos` 会失败，提示换 `--module` 的最后一段。这些文件里的其他名字，比如 `c`、`rows`，不受影响。`package.json` 里的名字也是它，超过 npm 上限 214 个字符时截断。
- `go.mod` 要求的 tifa 版本，是 go 命令下载来运行的这个命令的版本：`go run github.com/liustack/tifa/cmd/tifa@<版本> new myapp`，或者在应用里 `go tool tifa new`。在 tifa 源码目录里构建、或被源码目录 replace 的命令写 `v0.0.0`，并多给一步用 `go mod edit -replace` 把 `go.mod` 指向源码目录。
- `modernc.org/sqlite` 和 tifa 的 QuickJS 都跑在 `modernc.org/libc` 上，而每个 SQLite 版本只对应一个确切的 libc 版本。`go.mod` 固定了一个和 tifa 所需 libc 对应的 SQLite 版本。以后升级其中任何一个，都要让这个 libc 版本保持一致。
- `tifa new` 只写文件，不跑 go 或 npm 命令，也不联网。它最后打印接下来的步骤：`cd` 进目录、`go mod tidy`、`pnpm install` 和 `go tool tifa dev`，开发服务在 http://127.0.0.1:9527。
- 它会在 PATH 里找 `pnpm`，但不运行它。找到时，安装步骤和 `README.md`、`AGENTS.md` 里的命令用 pnpm（`pnpm install`、`pnpm typecheck`），找不到就用 npm（`npm install`、`npm run typecheck`）。`package.json` 不写 `packageManager` 字段，所以同一个应用两种都能用。

## tifa build

```sh
tifa build [--dir 应用根目录] [-o 输出路径]
```

1. 静态分析 `app/` 与根目录 `hooks.go`，约定不满足时逐条打印 `文件:行号: 原因` 并退出。
2. 把生成代码写进 `.tifa/`。只改内容变了的文件，删掉不再生成的文件。
3. 用应用自己装的 Vite 打包，它跑在 Node 上，产物写进 `.tifa/dist/`，见下文「前端产物」。整个构建只起一个 Node 进程，不管 shell 里设了什么，它的 `NODE_ENV` 都是 `production`，所以构建总用 React 的生产版。第 2 步读 `route.tsx`、`layout.tsx` 的导出也用 Vite 的解析器。没有页面的应用构建时不需要 Node。可执行文件始终不需要 Node。
4. `CGO_ENABLED=0 go build -o <输出> ./.tifa`，默认输出 `bin/<模块路径最后一段>`。生成的 main 用 `//go:embed all:dist` 把 `.tifa/dist/` 嵌进可执行文件。有页面的应用用一份页面渲染的剖析来编译，见下文「按剖析优化」。

### 按剖析优化

`go build` 可以拿程序的一份 CPU 剖析来优化它（[PGO](https://go.dev/doc/pgo)）：剖析里显示调用得多的地方，它就内联。`tifa build` 把剖析写进 `.tifa/default.pgo`，`go build` 默认的 `-pgo=auto` 会在 main 包旁边找到它。用[项目文件](config.zh-CN.md#配置项)里的 `build.pgo` 选剖析：

| `build.pgo` | 剖析 |
| --- | --- |
| `auto`（默认） | 应用根目录的 `default.pgo`。没有的话，有页面的应用用 tifa 自带的剖析，没有页面的应用不用剖析 |
| `off` | 不用 |
| 一个路径，比如 `profiles/prod.pgo` | 这个文件，相对应用根目录 |

tifa 自带的剖析采自示例商店的页面在 linux/amd64 上的热 VM 里渲染。它的样本几乎全在 JavaScript 引擎、libc 和 Go 运行时里，每个页面都要经过这些代码，商店自己的 Go 代码一行也不在里面。用上它，商店的页面在 linux/amd64 上渲染快约 2.4%，在 darwin/arm64 上快约 7%，HTML 逐字节不变。

`tifa dev` 构建时从不用剖析：它给 `go build` 传 `-pgo=off`。`tifa build` 时，`GOFLAGS` 里的 `-pgo` 比 `build.pgo` 优先，和任何 `go build` 一样。

从空的构建缓存开始构建，带剖析比不带最多多花约 6% 的时间。这是在示例商店上用早先的一份剖析量的：Apple M4 上量的是实际耗时，linux/amd64 上量的是 CPU 时间。但换一份剖析，`go build` 会把每个包重新编译一遍，标准库也在内，所以换了剖析后的第一次 `tifa build` 用不上 `tifa dev` 或 `go test` 留在构建缓存里的东西。示例商店在 Apple M4 上约 5 秒，而走缓存的构建不到一秒。之后剖析不变的构建都从缓存里取。商店的可执行文件在 linux/amd64 上大了约 0.13 MB，在 darwin/arm64 上大了约 0.15 MB，都不到 1%。

自己的剖析更贴合自己的应用。在真实流量下给构建好的程序采一份 CPU 剖析，比如用 `pprof.StartCPUProfile` 采 30 秒，存成应用根目录的 `default.pgo`。页面在同一个进程里渲染，所以它也覆盖了引擎。想让流量没走到的页面也留着 tifa 剖析的效果，就把两份合并。样本是相加的，所以时间长的那份分量更重：

```sh
go tool pprof -proto prod.pprof "$(go list -m -f '{{.Dir}}' github.com/liustack/tifa)/internal/codegen/engine.pgo" > default.pgo
```

### 给别的系统构建

静态分析和 `go build` 都按环境里的 `GOOS`、`GOARCH` 运行，所以它们选的文件和 go 命令为这个目标选的一样，比如 `limit_windows.go`，或者带 `//go:build` 行的文件。用为本机构建的 tifa 命令，再设上目标平台：

```sh
GOOS=linux GOARCH=amd64 $(go tool -n tifa) build -o bin/myapp-linux
```

`go tool -n tifa` 只打印本机 tifa 命令的路径，不运行它。`GOOS=linux go tool tifa build` 不行：这时 `GOOS` 也作用于构建 tifa 命令本身，它启动时就报 `exec format error`。

### 要随程序附带的声明

程序通过 `github.com/liustack/quickjs/intl` 包嵌入了 [Intl](app.zh-CN.md#服务端渲染里的-intl) 用的语言数据，来自 Unicode CLDR 和 Chromium 自带的 ICU 数据。这些数据采用 Unicode License V3，Chromium 改过的部分采用它的 BSD 许可，两者都要求程序的每个副本或它的文档里带上它们的声明。声明放在这个模块的 `intl/LICENSE-DATA` 文件里。把程序交给别人时，把这个文件一起附上：

```sh
cp "$(go list -m -f '{{.Dir}}' github.com/liustack/quickjs)/intl/LICENSE-DATA" bin/LICENSE-DATA
```

在应用目录里运行，`go list` 才会找到应用构建时用的版本。`tifa build` 不会替你复制这个文件。

## tifa routes

```sh
tifa routes [--dir 应用根目录] [--json]
```

做和 `tifa build` 一样的静态分析，不生成也不构建，打印路由表：目录、URL、Layout、方法（页面记作 `page`）、`SSR` 和 `Cache` 开关，再打印 Layout 表。违反约定时的报错和 `tifa build` 一样。`--json` 改为打印路由清单，带每个 `Load` 返回值和每个方法函数入参、结果的 Go 类型。

### go.mod

应用把 tifa 命令登记成 Go 工具：

```sh
go get -tool github.com/liustack/tifa/cmd/tifa
go tool tifa build
```

- `go tool tifa` 用的是 `go.mod` 里那一版 tifa，命令与运行库的版本总是一致。
- 生成的 main 引用 `github.com/liustack/tifa/server`，`go mod tidy` 跳过 `.tifa/` 看不到这层引用。tifa 命令自己也引用 server，`go mod tidy` 会保留工具需要的全部模块，所以 server 的依赖（cobra、yaml、quickjs 等）一直留在 `go.mod` 与 `go.sum` 里。
- `tifa build` 不改 `go.mod` 与 `go.sum`。跑过 `go mod tidy` 的应用直接能构建，`GOFLAGS=-mod=readonly` 的 CI 也一样。
- 缺依赖时构建失败，并提示执行上面的 `go get -tool` 与 `go mod tidy`。
- 升级 tifa（`go get -tool github.com/liustack/tifa/cmd/tifa@<版本>`）后跑一次 `go mod tidy`，新版本的命令或运行库多了依赖时，它会补进 `go.mod`。
- 有些 npm 包带 `.go` 文件，比如 eslint 间接装上的 `flatted`。`ignore node_modules` 这一行让任何深度的 `node_modules` 目录都不进 `./...`，`go test ./...`、`go vet ./...`、`go mod tidy` 和 `tifa build` 的静态分析都跳过它们。`tifa new` 会写上这一行，旧应用用 `go mod edit -ignore=node_modules` 补上。

## .tifa/ 里的文件

| 文件 | 内容 |
| --- | --- |
| `main.go` | 调用 `server.Main`（[tifa dev](dev.zh-CN.md) 下是 `server.DevMain`），只填 `hooks.go` 里实际存在的钩子。`go:embed` 前端产物 `dist/`，有项目配置时也嵌入它 |
| `routes.go` | `layouts` 与 `routes` 两个包级变量，每个 `Load` 与 `Layout` 函数包成 `func(c *tifa.Ctx) (any, error) { return tifa.Use(c, pkg.Load) }`，Layout 用 `pkg.Layout`。带入参或返回值的方法函数经过 `server.Input`、`server.Output` 或 `server.InputOutput`，它们用 `(*tifa.Ctx).Bind`（GET、DELETE 用 `(*tifa.Ctx).BindQuery`）解码入参，执行 `Validate`，写出结果 |
| `default.pgo` | `go build` 用来优化程序的剖析，见上文「按剖析优化」。[tifa dev](dev.zh-CN.md) 下没有 |
| `tifa.yaml` 或 `tifa.json` | 项目配置的副本（`go:embed` 不能引用上级目录）。构建时先按运行时规则校验一遍 |
| `routes.tsx` | TanStack 路由树与 `Register` 类型注册 |
| `app/<目录>/+route.ts`、`app/<目录>/+layout.ts` | 每个页面一份 `+route.ts`，每个 Layout 一份 `+layout.ts`，都导出 `route`、`Data`、`Params`。`+route.ts` 还按方法函数各导出一个调用函数，有方法函数的 API 路由也生成 `+route.ts`，只含 `Params` 和这些函数。它们调用 `runtime/call.ts` |
| `server.ts`、`client.ts` | SSR 入口与浏览器入口。`tifa dev` 的浏览器入口另外引入 `runtime/dev-client.js` |
| `tsconfig.json` | 见下文 |
| `vite.js`、`vite.d.ts`、`vite/plugin.js` | 给 `vite.config.ts` 用的 tifa Vite 插件，见下文。`vite/plugin.js` 是仓库 `js/vite/plugin.js` 的原样拷贝 |
| `runtime/` | 仓库 `js/runtime/` 的原样副本 |
| `dist/` | 前端产物，见下文 |

没有页面（没有任何 `route.tsx`）的应用只生成 Go 文件，`dist/` 里只有 `build.json` 和公共文件。

## 前端产物

| 路径 | 内容 |
| --- | --- |
| `dist/assets/` | Vite 打出的浏览器包：入口 `client.ts`（`app-<哈希>.js`）、共享 chunk（`chunk-<哈希>.js`）、装着应用全部 CSS 的一个样式表（`style-<哈希>.css`），以及 CSS 引用的字体和图片，小文件 Vite 会内联成 data URL。语法目标取配置项 `build.target`（默认 `es2020`：一个 ECMAScript 版本加若干带版本的浏览器，如 `es2019,chrome90,safari14`）。按路由拆分：除根 Layout 和导出 `head` 的模块外，每个路由模块用 `lazyRouteComponent` 懒加载，各自一个 chunk。1024 字节及以上的 JavaScript、CSS、HTML、JSON、SVG、XML 和 WebAssembly 文件旁边还有一个按 gzip 最高级别压缩的 `<文件名>.gz`，gzip 压不小的文件没有。服务端用它代替原文件发出，见[压缩](#压缩) |
| `dist/ssr.js` | SSR 包，入口 `server.ts`：一个 IIFE 文件，所有 npm 包都打在里面。包解析到它的主文件（先 `module` 后 `main`），条件导出 `exports` 按 `worker`、`import`、`module`、`default` 这几个条件取。不用包的浏览器文件：很多浏览器文件直接取浏览器里的东西，比如 `self` 或 `window`，QuickJS 里没有。想在服务端用某个包的浏览器版，按路径引入那个文件。语法目标固定为 ES2022（QuickJS 原样能跑的版本，调低只会让渲染变慢，调高可能写出 QuickJS 不支持的语法）。页面代码引入 Node 模块时构建失败。对 npm 包，构建只给警告：列出包里运行时可能用 `require` 或 `import()` 加载模块的位置，给出源文件、行、列和包名，因为服务端渲染走到那里会在 QuickJS 里失败。它在最终代码的一份压缩副本里找：凡是代码没有自己声明 `require` 的地方，`typeof require` 都当作 `"undefined"`。所以死代码里的 `require`（比如 `if (false)` 分支）和被 `typeof require` 判断守住的 `require`（比如 `typeof require === 'function' ? require(name) : 备用值`）不会有警告：QuickJS 没有 `require`，会走另一条分支。这份清单只是提示。它可能列出渲染走不到的代码，比如包按 `typeof process` 选的分支（debug 就这样做）、`try`/`catch` 里的 `require`、被代码赋值的 `require`。它也可能漏掉写在 `eval` 字符串里的 `require`、先设置 `globalThis.require` 再删掉的代码，以及后面的插件加进来的代码。没有源码对应的位置（比如某个包引入的 Node 模块）给的是 `ssr.js` 里的位置。`define` 把浏览器包的 head 标签写进 `__TIFA_ASSETS__` |
| `dist/ssr.js.map` | SSR 包的 sourcemap，不带源码副本。服务端加载 SSR 包时读入这个文件，这个包第一次出错时（预热失败也算）才解码，把错误栈里的位置换成源文件里的位置，路径相对于应用根目录。读不全的 map 整个不用，位置留在 `ssr.js` 里。体积看应用，`examples/shop` 约 1.9 MB，不到 `ssr.js` 的两倍。它和 `dist/` 里其他文件一起嵌进可执行文件。不想要它，就在 `vite.config.ts` 里把 `environments.ssr.build.sourcemap` 设为 `false`：`examples/shop` 的可执行文件会小约 1.9 MB，构建警告和渲染错误给的是 `ssr.js` 里的位置。没有写出 `ssr.js.map` 的构建会删掉之前构建留下的那份（`tifa dev` 下 `.tifa/dist` 会保留旧文件，它对应的是别的代码）。服务端只读这个文件，所以设成 `inline` 时写在 `ssr.js` 里的 map 不会被用到 |
| `dist/public/` | 配置项 `build.public` 指向的目录（默认 `public`）原样复制，不存在就没有 |
| `dist/build.json` | `{buildId, head}`。`head` 是加载浏览器包的 HTML，取自 Vite 的 manifest：样式表、入口引入的每个 chunk 一条 `modulepreload`、入口脚本。构建 ID 是 `dist/` 里其他所有文件（含文件名）的哈希，文件名与内容各自带长度前缀，不同的文件树不会拼出同一串字节。同样的输入得到同样的 ID |

SSR 包开头引入 `runtime/host.js`，它补上 QuickJS 缺的 `URL`、`TextEncoder`/`TextDecoder`、`AbortController`、`Headers`、`Response`、`queueMicrotask`，以及一个什么也不输出、`error` 时抛错的 `console`，不提供 `fetch` 与 `Request`。页面在服务端能用到的全部东西见[服务端渲染时有什么](app.zh-CN.md#服务端渲染时有什么)。这份脚本由 `go generate ./js` 从 `js/host/host.ts` 预打包后入库，依赖版本锁在 `js/host/package.json` 与 `pnpm-lock.yaml`。升级时在 `js/host` 里 `pnpm install --frozen-lockfile`（或改版本后 `pnpm install`），再跑 `go generate ./js`。`internal/bundle` 的 `TestHostScriptIsUpToDate` 检查重新生成的结果与入库文件一致，`js/host/node_modules` 不存在时 skip。应用因此不需要安装这些补丁包。`abort-controller` 与它依赖的 `event-target-shim` 打了补丁（`js/host/patches`，登记在 `js/host/pnpm-workspace.yaml`）：每个对象的内部状态改存在对象自己的 symbol 属性上，不用 WeakMap。这个属性在建对象时定义一次，之后不再重新定义，所以应用 seal 或 freeze 了 signal 也照样能取消（`TestHostScriptAbortsFrozenSignals`）。唯一会再变的 signal 取消标记放在一个盒子里，只改盒子里的值。其余状态不装盒子，每个状态都装盒子时 P99 多出约 1.5 ms。QuickJS 回收穿过 WeakMap 值的死环要多轮 GC，有的环一直回收不掉，每次渲染留下的路由、AbortController 与监听器因此要等好几轮 GC，VM 的堆更大。改成属性后一次 GC 就能整体回收，`TestHostScriptFreesDeadAbortSignalsInOneCollection` 守住这一点。

tifa 插件在 SSR 包里改造三个 npm 包。它按代码的结构或文字找到要改的地方，不看包的版本，所以 `pnpm update` 不会让构建失败。某个版本改了那段代码时，构建打印警告，写明包的版本和文件，那段代码保持原样，页面照常渲染。对 React DOM 的改造让它文件里的每一行都留在原来的行号上，所以渲染错误的调用栈给出的 React DOM 行号和 `node_modules` 里的文件一致。这些改造都不改变 HTML，所以没有关掉它们的设置。

- React DOM 有七处改造：
  - `react-dom/server.edge` 会加载 React 的两个服务端渲染器。插件读这个文件的结构，只留 tifa 用的字符串渲染器。在示例应用上，SSR 包因此小 25%，4 个 VM 的池 63 ms 启动完（不裁是 117 ms），单次渲染耗时不变。没有这处改造时，包里保留两个渲染器。
  - `renderToString` 会忽略 Suspense 边界里组件抛出的错误。插件给 `renderToString` 和 `renderToStaticMarkup` 加上 React 其他渲染器都有的 `onError` 选项，服务端因此能报出这个错误（见[页面请求](#页面请求)）。没有这处改造时，这样的渲染照样失败，只是错误说不出原因。
  - React 渲染每个元素时都把它放上组件栈，每个元素一个对象。字符串渲染器只把这个栈交给 `renderToString` 的错误处理函数，即 `errorInfo.componentStack`，而插件给 `renderToString` 的处理函数只往外传错误本身。所以插件让 React 不再建这个栈。它只和那个处理函数一起生效：插件在 React DOM 里找不到 `function onError() {}` 时，栈照建。它还数一数读这个栈的地方，也就是 `getThrownInfo` 的调用和 `componentStack` 的读取，和 React DOM 19.3 的不一样时栈也照建。错误和它的调用栈都不变。没有这处改造时，页面和以前一样快。
  - React 写出的每段文本和每个属性值都要转义。含 `&`、`<`、`>`、`"` 或 `'` 的字符串要逐个字符过一遍循环，这在 QuickJS 里很慢，而 Tailwind CSS 和 shadcn/ui 的 class 里这些字符很多（`[&_svg]:size-4`、`has-[>svg]:px-3`），每次渲染又都一样。插件在这个转义前面加一层缓存，以字符串为键，每个 VM 各有一份。它只存需要转义的字符串，字符串和结果合计最多 262,144 个 UTF-16 码元，每存一个字符串另算 64 个码元，所以大量短串占的内存不会比几个长串多。存满时约占 VM 内存 0.5 到 2 MB。下一个字符串放不下时清空重来，所以每次请求都不同的数据文本不会长时间挤掉每次渲染都写的字符串。比整个缓存还大的字符串不存。在示例应用上，缓存里是 31 个字符串，合计 35,657 个码元，热渲染耗时少 8% 到 10%。在纯文本和数字为主、属性很少的页面上，渲染耗时不变，每次请求的文本都不一样时也不变。没有这处改造时，页面和以前一样快。
  - 缓存里没有的字符串仍要过 React 逐字符的循环。插件把这个循环换成五次 `replaceAll`，每个要转义的字符一次，`&` 最先，这样其他实体里的 `&` 不会被再转义一次。任何实体里都没有另外四个字符，所以写出的文字不变：测试拿 0 到 0xFFFF 的每个 UTF-16 码元（单独出现和连在一起）以及 3,000 个随机字符串，和 React 的循环逐一对比。缓存找要转义的字符时也改用 `exec`，不用 `test`，QuickJS 的 `test` 要先查出 `exec` 再调用它。每次请求文本都不同、缓存命中不了的页面得益最多。没有这处改造时，页面和以前一样快。
  - React 把一页的 HTML 分成若干段，每段是一串片段，片段之间夹着子段，子段来自 Suspense 边界、渲染时挂起的部分和 preamble。React 把片段一个一个交给一个函数接到结果后面。一页有几千个片段，每调用一次在 QuickJS 里都很慢。插件让 React 把一个段里两个子段之间的片段先拼好，一次交过去。HTML 不变：那个函数会跳过 `null` 片段，拼接时它变成空串。在示例应用上，热渲染耗时少 5% 到 7%，纯文本页面少 6% 到 11%。没有这处改造时，页面和以前一样快。
  - React 写属性和标签时用一个按名字的 `switch` 决定怎么写，QuickJS 会拿名字和 case 一个一个比。属性的 `switch` 有 71 个 case，不在其中的名字（比如 `id`、`type`、`aria-label`）要比 71 次才走到 default。插件读渲染器的语法树，在每个拿一个变量和至少 8 个字符串（且只有字符串）比较的 `switch` 前面先查一次 `Set`，这样的名字直接进 default。只在结果不变时才改：有 default 的话它在最后，前面的 case 不会接着跑进它，任何 case 里都没有声明 `let`、`const`、类或函数。React DOM 19.3 里是 5 个 `switch`，按属性名和按标签名。在示例应用上，热渲染耗时少 6% 到 7%，纯文本页面少 9% 到 12%。示例应用的字节码多 5 KB，每个 VM 多几 KiB。React DOM 里没有这样的 `switch` 时，构建打印警告，页面和以前一样快。
- TanStack Router 用 seroval 把页面的数据写进页面。seroval 对短于 64 个 UTF-16 码元的字符串逐个字符转义，更长的用 `JSON.stringify`，除非里面有两种写法结果不同的字符。在 QuickJS 里从 2 个码元起逐字符的循环就比 `JSON.stringify` 慢，所以插件把这个长度降到 2。写出的文字不变：0 到 0xFFFF 的每个码元，单独出现或夹在别的字符中间，以及代理对和孤立代理项，两种写法结果都一样。小页面渲染快 6% 到 10%，大页面最多快 2.5%。插件只在 seroval 的主文件 `dist/index.js` 和 `dist/index.cjs` 里找这一行。seroval 改了这一行，或者包从别的文件加载 seroval 时，构建打印警告，页面和以前一样快。
- 应用打包了 `core-js` 时，插件把它的内部状态从 WeakMap 换成属性，因为 QuickJS 永远不回收值又指回键的 WeakMap 条目。core-js 某个版本没有了插件替换的那个模块时，构建打印警告，保留 WeakMap，每个 VM 的内存可能变大。

## 路由树

- 框架自己的根路由没有组件，`errorComponent` 是框架默认组件，`notFoundComponent` 是根 Layout 的 `NotFound`（没有就用框架默认组件），根 Layout 导出 `documentHead` 时它的 `head` 返回这个常量。React 只渲染 `<body>` 里的 `<div id="tifa-root">`。SSR 入口在渲染后把文档拼成字符串：`<html>` 带根 Layout 的 `htmlAttributes`，head 依次是 charset、`build.json` 的 `head`，以及 `<!--tifa-head-->` 与 `<!--/tifa-head-->` 之间按匹配到的路由的 `head` 结果写出的标签（取哪些标签同 TanStack 的 `HeadContent`，属性按 React DOM 的规则写出，见 [app.zh-CN.md](app.zh-CN.md)），body 是应用根节点加上路由器的脱水脚本。所以任何一层出错、404、根 `Layout` 失败时，页面仍是完整文档。浏览器订阅路由器的 matches（TanStack 的 `HeadContent` 读的也是它），导航完成、`router.invalidate()` 取完数据，只要 matches 变了就按同样的规则比对两个注释之间的元素：保留不变的标签（比较 nonce 时读元素的 `nonce` 属性，有 CSP 的页面里 nonce 的 HTML 属性读出来是空的），顺序变了就像 React 移动有 key 的子节点那样只移动不在原位的元素（移动 style 会重建样式表，移动样式表 link 会重新处理一遍），新标签用 DOM 方法创建（不解析 HTML）并按文档顺序插入，hydration 时不动服务器写的 head。不走 SSR 的页面由 Go 写只有 charset 和 `build.json` 的 `head` 的文档，浏览器设置 `htmlAttributes` 并写入 head。
- 根 Layout（`app/layout.*`）是根路由下的 pathless 路由 `_app`。
- 其他 Layout 是一条带路径的路由，路径是它的目录在父 Layout 目录之下的部分，所以路径参数在它的匹配身份里。根 Layout 下的 `app/projects/project+` 路径是 `projects/$project`，路由 ID 是 `/_app/projects/$project`。
- 页面自己导出的 `NotFound` 挂在页面路由上。Layout 导出的 `NotFound` 不另加路由，而是挂在它下一层、子树里有 `Load` 的路由上（根 Layout 的已在根路由上）。TanStack 从抛出 404 的路由往上找第一个带 `notFoundComponent` 的路由，在它的位置显示，所以下层的 404 显示在该 Layout 的 `Outlet` 里。
- 页面路径是它的目录在所属 Layout 目录之下的部分。和 Layout 同目录的页面是这个 Layout 的 index 路由 `/`，路由 ID 是 `/_app/projects/$project/`。`name+` 写成 `$name`，`name++` 写成 `$`（参数名 `_splat`）。字面量目录名只含 ServeMux、路由器和 URL 读法一致的字符，所以原样写出。
- 生成后任意两条路由的 TanStack ID 相同时构建报错。
- 有 `Load` 的路由生成 `loader`、`staleTime: Infinity` 与 `preloadStaleTime: Infinity`，数据只在 `router.invalidate()` 之后重取。路由器设了 `defaultStaleReloadMode: 'blocking'`：这次重取，以及导航到缓存数据被 `invalidate` 标为过期的页面时的重取，都在 `invalidate()` 或导航兑现之前完成，期间旧数据留在页面上。TanStack 的默认做法是立刻兑现、在后台取数据。`loaderDeps` 只放 `AllQuery` 里的查询参数，也就是用 `c.Query` 或 `c.QueryAll` 读的。`remountDeps` 是 `Load` 读的路径参数（含经 `tifa.Use` 用到的 Layout 读的）加同一份查询参数，路径里有、但没有 `Load` 读的参数变了，组件不重新挂载，状态保留。没有 `Load` 的路由不重新挂载。
- `Load` 返回 404 时 loader 抛出不带目标的 `notFound()`。按上一条的挂法，页面的 404 显示它自己的或最近的祖先 Layout 的 `NotFound`。Layout 的 404 显示父 Layout 往上第一个 `NotFound`，因为 Layout 自己的 `NotFound` 只挂在它下层的路由上。
- 路由器区分路径大小写，和 ServeMux 一致。查询参数按 `c.Query` 的语义解析：值都是字符串，重复的键是字符串数组，不做 JSON 解析。写回 URL 时按浏览器和表单的写法（空格写成 `+`），常见的 URL 保持原样。Go 的 `url.ParseQuery` 会另作解释的字符（`;&=+%#`）一律百分号编码，重取时 `c.Query` 拿到同一个值。
- 没有路由匹配的 URL 直接显示根路由的 `NotFound`（`notFoundMode: 'root'`），不加载任何路由，不请求服务器。这时根 Layout 的界面不显示，因为它的数据没有加载。
- `route.tsx`、`layout.tsx` 必须有默认导出。可选导出 `head`、`Error`、`NotFound` 接到 `head`、`errorComponent`、`notFoundComponent`，根 Layout 另可导出常量 `htmlAttributes` 与 `documentHead`，导出 `Document` 构建报错。没有导出 `Error` 的路由显式使用最近的祖先导出的 `Error`，都没有就用框架默认组件。服务器渲染出错路由自己的 `errorComponent`，浏览器由最近的错误边界接住，两边因此显示同一个组件。这些导出必须在文件里直接声明或命名导出（`export *` 转出的不算）。

## 应用的 tsconfig 与 Vite 配置

有页面的应用在根目录放这两个文件，`tifa new` 会写好：

```json
{
  "extends": "./.tifa/tsconfig.json",
  "compilerOptions": {
    "paths": {
      "@/*": ["./*"],
      "tifa/client": ["./.tifa/runtime/client.ts"]
    }
  },
  "include": ["**/*", ".tifa/**/*"]
}
```

```ts
// vite.config.ts
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tifa from './.tifa/vite.js'

export default defineConfig({
  plugins: [tifa(), react()],
})
```

- 生成的 tsconfig 设了 `rootDirs: ["..", "."]`：TypeScript 把 `.tifa/app/x/+route.ts` 当成 `app/x/+route.ts`，源码里的 `import { route } from './+route'` 因此能解析，Layout 模块 `'./+layout'`、`'../../+layout'` 也一样。应用的 `include` 单独写了 `.tifa/**/*`，因为 TypeScript 的通配符会跳过以点开头的目录。生成的 tsconfig 还载入 `vite/client` 的类型，导入图片、样式表和使用 `import.meta.env` 都能通过类型检查。
- `paths` 写在应用自己的 tsconfig 里，shadcn CLI 这类从这里读的工具看到的就是应用根目录。`@/` 指向应用根目录：`@/components/ui/button` 就是 `components/ui/button.tsx`。`tifa/client` 是页面代码引入 `clientOnly`、`RouteError` 用的固定模块名。两者在第一次构建前就能用。
- Vite 按同一份 `paths` 解析导入（它的 `resolve.tsconfigPaths`），所以你加的或改的路径，比如 `"@/components/*": ["./src/components/*"]`，类型检查和打包都照它走。Vite 只对 `include` 匹配到的文件用 `paths`，所以 `include` 里要保留 `**/*`。
- `.tifa/vite.js` 的 `tifa()` 返回 tifa 的 Vite 插件，项目配置的 `build.target` 已经传进去。它们按 TypeScript 的方式解析 `./+route` 和 `./+layout`：最后一段是 `+route` 或 `+layout`（可带 `.js` 或 `.ts`）、指向 `app/` 下某个目录的相对导入，不论中间经过几层（`'../../page/+route'` 也行），都解析到 `.tifa/` 里对应的文件。目录没有生成的模块时构建报错。它们还设好下文的入口与产物，并构建 SSR 包。
- `@vitejs/plugin-react` 编译 JSX，在 `tifa dev` 下加上 React Fast Refresh。别的 Vite 插件加在这两个后面，比如 `@tailwindcss/vite`（见 [app.zh-CN.md](app.zh-CN.md#tailwind-css)）。入口、`base`、产物和 `ssr` 环境交给 tifa。
- `vite.config.ts` 引入 `./.tifa/vite.js`，所以 tifa 生成 `.tifa/` 之后它才能通过类型检查。`tifa build` 和 `tifa dev` 会在 Vite 读配置前生成它。

应用的 `package.json` 需要 React、React DOM 与 TanStack Router，开发依赖要有 `vite` 和 `@vitejs/plugin-react`。类型检查另需 `typescript`、`@types/react`、`@types/react-dom`。参照 `test/apps/pages/package.json`，用 Tailwind CSS 参照 `test/apps/tailwind/package.json`。

TanStack Router 必须是这个 tifa 测过的版本：`tifa new` 写进 `package.json` 的 `@tanstack/react-router`，以及它装上的 `@tanstack/router-core`。tifa 把页面数据交给浏览器，靠的是 TanStack Router 一个在 TanStack Start 之外没有文档的选项（见[数据协议](#数据协议)），换个版本浏览器端可能就不读它了，这时所有带对象数据的页面都是白屏。所以 `tifa build` 和 `tifa dev` 会按 Node 查找包的方式读出这两个包装上的版本，不一致就报错停下，错误里写出装的版本和测过的版本。要用更新的 TanStack Router，升级 tifa。一定要用别的版本构建（风险自负），在 `tifa.yaml` 里设 `build.skip_tanstack_check: true`（见 [config.zh-CN.md](config.zh-CN.md)）。没有页面的应用不检查。

## 数据协议

定义在 `js/runtime/data.ts`，Go 侧按它实现：

- 每个 `Load` 的结果是 `{status, data?, error?}`：成功为 200 带 `data`，`tifa.ErrNotFound` 为 404，其他错误带状态码与公开消息。
- 路由 ID 指一个 `Load` 或 `Layout` 函数：页面是目录加 `/route`，Layout 是目录加 `/layout`，比如 `app/layout`、`app/projects/project+/route`。
- 整页渲染的 VM 输入是 `{url, buildId, data: {路由 ID: 结果}}`，与 `internal/ssr.Input` 一致。
- 客户端导航时，同一轮事件循环里对同一 URL 的 loader 调用合并成一个 GET：请求头 `X-Tifa-Route: <路由 ID>,<路由 ID>` 与 `X-Tifa-Build: <构建 ID>`，`fetch` 不自动跟随重定向（`redirect: "manual"`）。回答有三种：
  - `{路由 ID: 结果}`：不论 HTTP 状态码，浏览器逐项分发。
  - `{"redirect": "<URL>", "status": <3xx>}`：`OnRequest` 返回 `c.Redirect`、URL 的写法不规范（ServeMux 的重定向）等整个请求要跳转的情况，HTTP 状态码是 200。请求里每个路由都收到这个重定向，loader 抛出 TanStack 的 `redirect()`。
  - 其他：`OnRequest` 拒绝了整个请求，回答是 `{"error": 消息}` 与错误的状态码，同批 loader 一起以这个状态失败。
  
  构建 ID 不一致时服务器答 409，浏览器刷新当前页面一次（多个 409 只刷新一次，迟到的预加载 409 不会跳到预加载的目标）。仍然收到 3xx（比如来自代理）时，浏览器按整页加载这个 URL。
- `+route` 模块里的处理函数（见 [app.zh-CN.md](app.zh-CN.md#在浏览器里调用方法函数)）发请求头 `X-Tifa-Call: 1`，不让 `fetch` 跟随重定向。处理函数或 `OnRequest` 返回 `c.Redirect` 时，这样的请求答 200、`Cache-Control: no-store`、`X-Tifa-Redirect: <3xx>`，正文和数据请求一样是 `{"redirect": "<URL>", "status": <3xx>}`。处理函数的结果可能长得一样，所以调用函数只在有这个响应头时才当作重定向。不带 `X-Tifa-Call` 的请求（curl 或别的程序）照旧收到 3xx。两种回答都带 `Vary: X-Tifa-Call`。
- 整页渲染时，每个 `Load` 的数据随路由的 dehydrate 脚本进入浏览器。Go 把每个成功的 `Load` 的数据按它写出的 JSON 文本放进渲染输入（输入里的 `json`，即 `js/runtime/data.ts` 的 `InjectedResult`），渲染时解析这段文本。对象和数组原样带着这段文本进页面，经 TanStack 的 serialization adapter（键 `tifa.data`）写成一个 JSON 字符串，浏览器用 `JSON.parse` 读回。所以浏览器读到的值和服务端渲染时完全一样，`-0` 和 `__proto__` 键都保留，用它们格式化出的文字（比如 Intl 写出的 `-¥0.00`）水合时也和渲染时一致。TanStack 默认的序列化器处理同样的数据，在 VM 里要多花几倍时间。代价是页面略大，因为 JSON 字符串要转义引号：`examples/shop` 的页面压缩前大约大 3%。字符串、数字等单个值仍走默认序列化器。路由构造函数的类型把 adapter 留给 TanStack Start，单独使用路由时设置它没有文档（TanStack/router#7796），所以 `TestGeneratedRouterBehaves` 检查页面用的是 `tifa.data`，并且数据经过 hydration 后不变，`tifa build` 再检查应用装的 TanStack Router 正是这些测试用的版本（见[应用的 tsconfig 与 Vite 配置](#应用的-tsconfig-与-vite-配置)）。
- 浏览器从 TanStack 的 dehydrate 数据里拿到构建 ID。
- loader 为失败的 `Load` 抛出的 `RouteError` 经 TanStack 的 serialization adapter（键 `tifa.RouteError`）写进页面，hydration 后仍是带 `status` 的 `RouteError`，`Error` 组件在服务器和浏览器看到同一个错误。

## 生成的程序

`server.Main(app)` 组装命令行：

| 命令 | 作用 |
| --- | --- |
| `serve`（默认） | 加载配置，执行 `OnStart`，监听，收到 SIGINT 或 SIGTERM 后优雅退出并执行 `OnShutdown` |
| `config` | 按全部配置层合并后，输出带注释的部署配置文件 |
| `version` | 输出应用模块、tifa 模块与 Go 的版本 |

配置优先级从高到低：命令行参数、`TIFA_*` 环境变量、`--config` 指定的文件、嵌入的项目配置、默认值。未知的 `TIFA_*` 环境变量会让程序报错退出。

不管程序启动时在哪个时区，VM 里 `Date` 的本地时间恒为 UTC，所以同一个页面在每台机器上渲染得一样。进程的时区不受影响：应用 Go 代码里的 `time.Local` 是机器或 `TZ` 指定的时区，和普通 Go 程序一样。生成的 `main.go` 导入了 `time/tzdata`，所以系统没有 zoneinfo 时（比如 `scratch` 容器），Intl 和 `time.LoadLocation` 也能找到每个时区。程序因此大约增加 450 KB。

`serve` 另有开发开关 `--dist <目录>`：从这个目录读前端产物，替代嵌入的 `dist/`。[`tifa dev`](dev.zh-CN.md) 把它指向 `.tifa/dist`。只重建了 SSR 包时，开发用的程序（`server.DevMain`）从这里重新读包，不重启。其他重建之后 tifa dev 重启它。它不是部署配置，不进配置分层，也没有对应的 `TIFA_*` 变量。启动时读 `ssr.js` 编译并预热 VM 池，读 `build.json` 拿到构建 ID 与 head 标签。

## 页面请求

静态文件在路由之前处理，不经过 `OnRequest`：`/assets/` 下的文件带 `Cache-Control: public, max-age=31536000, immutable`，公共目录的文件带 `no-cache`，只响应 GET 与 HEAD。`/assets/` 下浏览器包里没有的文件再到公共目录里找，所以 `public/assets/logo.svg` 仍是 `/assets/logo.svg`。

页面 URL 的 GET 经过 `OnRequest` 之后：

| 请求 | 回答 |
| --- | --- |
| 无 `X-Tifa-Route` | 并发执行匹配链上所有 `Load`。构建 ID、SSR 包、URL 与结果的哈希作 ETag，`If-None-Match` 命中答 304。`Cache = true` 的页面先查 HTML 缓存（按字节计的 LRU，容量 `cache.size`，0 关闭），未命中才进 VM |
| 同上，匹配链上有 `SSR = false` | 不进 VM，返回最简文档：资源标签与 `<script type="application/json" id="tifa-data">` 里的注入数据（和渲染输入的形式相同），浏览器直接渲染，不再请求数据 |
| 有 `X-Tifa-Route` | 只执行所列路由的 `Load` 或 `Layout`（`tifa.Use` 在本次请求内去重），答 200 与 `{路由 ID: 结果}`，每项带自己的状态。`X-Tifa-Build` 不是当前构建时答 409，所列路由不在匹配链上时答 400。出错与重定向见下文 |

数据请求的回答带 `Cache-Control: no-cache`，应用在 `OnRequest` 或 `Load` 里设置过 `Cache-Control` 时保留应用的值。整页回答框架不设 `Cache-Control`。

页面响应都在应用设置的 `Vary`（比如 `OnRequest` 里的 `Vary: Cookie`）之外再加一行 `Vary: X-Tifa-Route`，并用 `X-Tifa-Render: vm|cache|none` 说明这次是 VM 渲染、缓存命中还是没进 VM。页面和 JSON 回答不压缩发出时带 `Content-Length`。

出错时，`Load` 的错误先沿 `c.Next()` 交给 `OnRequest`，`OnRequest` 返回之后框架才处理：

- 有 `Load` 失败时，整页请求与数据请求的 `c.Next()` 都返回所执行 `Load` 里最外层的那个错误（数据请求只执行所列路由，所以是所列路由里最外层的）。成功的 `Load` 不写任何响应，中间件可以据此拒绝整个请求。
- `OnRequest` 原样返回这个错误（包括用 `%w` 包一层）：框架这时才把每个错误分类，意外错误交给 `OnError`，多个 `Load` 经 `tifa.Use` 共享同一个错误时只交一次。整页请求用全部结果和最外层错误的状态码（404 或 `tifa.Error` 的状态码，意外错误用 `OnError` 的结果）渲染整页，路由器显示对应层级的 `NotFound` 或 `Error`。最外层错误是重定向时答重定向。数据请求答 200 与 `{路由 ID: 结果}`，每项带自己的状态。
- `OnRequest` 返回别的错误：以中间件为准，按错误本身处理（重定向、`ErrNotFound`、`tifa.Error` 直接处理，其他交给 `OnError`）。整页请求返回 Go 写的简单错误页，数据请求答 `{"error": 消息}` 与状态码，不再输出各项数据。`OnRequest` 自己写了响应时框架不再写。
- 数据请求遇到整个请求的重定向（`OnRequest` 返回 `c.Redirect`，或 URL 写法不规范）答 200 与 `{"redirect", "status"}`，不发 3xx。这个 JSON 里的 URL，以及 `Load` 重定向的路由结果里的 `location`，和 3xx 会带的一样：`../list` 这样的相对 URL 按请求的路径解析，与 `http.Redirect` 相同。
- 未匹配的 URL：`OnRequest` 之后以 404 渲染根 Layout 的 `NotFound`，不执行任何 `Load`。根 Layout 是 `LayoutSSR = false` 时返回最简文档。
- VM 渲染失败（包括组件在渲染中抛错：`renderToString` 会把它吞进 Suspense 边界，留下 `<!--$!-->` 标记，SSR 入口见到这个标记就把这次渲染当失败）：算意外错误，不进 HTML 缓存，交给 `OnError`，返回 Go 写的简单错误页，不暴露内部错误。在 `tifa dev` 下（`DevMain`），这张页还写出请求、完整的错误，并加载 dev 客户端，重建带来修复后它自动刷新。`OnError` 拿到的是代码实际抛出的错误（被 React 吞掉的也一样），错误栈前 64 帧的位置借 `dist/ssr.js.map` 换成源文件里的位置。代码调用了 QuickJS 没有的 `require` 时，错误还会给出包名和位置，并说明怎么办：从 `.client` 文件引入这个包，把调用放进 `useEffect`，或者换一个不需要 Node 的构建。包在加载时就失败（任何页面渲染之前）时，失败的是引入本身，把调用挪进 `useEffect` 没用，所以预热的错误只建议从 `.client` 文件引入这个包或者换一个构建。组件挂起（抛出 promise，或者是还没加载完的 `React.lazy` 组件）也会这样让渲染失败，因为 `renderToString` 不等待。错误会建议用 `Load` 给页面数据并静态引入组件，或者设 `SSR = false`。懒加载的引入为什么失败，服务端看不到。渲染错误页（`NotFound`、`Error`）时失败也一样，状态码和消息取自 `OnError` 的结果，不再尝试渲染错误页。
- 路由器对 URL 有不同写法（`/%61` 写成 `/a`）时 VM 返回重定向，服务器照样答重定向，不带 ETag。渲染只取决于构建 ID、SSR 包、URL 与数据，同一个 ETag 不会既属于 200 又属于重定向，所以客户端拿不到能把这个重定向变成 304 的 ETag。
- 非 GET 请求、接口路由与没有页面的应用仍答 JSON。

### 压缩

[`server.compress`](config.zh-CN.md#配置项) 默认开着，回答按客户端接受的方式压缩后发出，所以 tifa 程序不需要为此加代理：

- 页面、导航数据、JSON 和其他文本在发出时压缩，用 zstd 或 gzip，各取默认级别，和 Caddy、Traefik 一样，由 [klauspost/compress](https://github.com/klauspost/compress) 的 gzhttp 完成。按客户端的 `Accept-Encoding` 选：`q` 大的优先，相同时用 zstd。回答从 1024 字节起才压缩，而且只压文本类型：`text/*`、JavaScript、JSON、XML、`+json` 和 `+xml` 类型、WebAssembly、图标，以及 TTF、OTF、EOT 字体。HEAD、已经带 `Content-Encoding` 的回答和带 `Content-Range` 的回答原样发出。
- 浏览器包里的文件，对接受 gzip 的 GET 直接发构建写好的 `.gz` 文件，类型和缓存头沿用原文件，和 nginx 的 `gzip_static` 一样。Range 请求取的是 `.gz` 文件的一段。没有 `.gz` 的文件，类型在上面的列表里就在发出时压缩。公共目录里的 `.gz` 文件是它自己，不会代替别的文件发出。
- HTML 缓存里的页面（`Cache = true`）只在存进去时压缩一次：缓存在 HTML 旁边存它的 gzip 和 zstd 版本，存它的那个请求和之后每次命中都直接发存好的版本，带 `Content-Length`。压缩版算进 `cache.size`：加上它们，示例 shop 的一个页面大约占 HTML 的 1.4 倍。不接受压缩的请求（`Accept-Encoding` 里没有 gzip 或 zstd、HEAD、带 `No-Gzip-Compression`）没命中时只存 HTML，之后第一个接受压缩的命中再压出压缩版。压缩版放不下的页面只存 HTML，HTML 都放不下的页面（比如 `cache.size` 为 0）在发出时压缩。
- 每个回答都带 `Vary: Accept-Encoding`，没压缩的也带，应用自己设了 `Vary` 也一样。
- 压缩后的回答，强 ETag 改成弱 ETag（`W/"…"`），和 nginx 一样，因为强 ETag 承诺的是同样的字节。`If-None-Match` 按弱比较，所以 304 照常有效。页面的 ETag 一开始就是弱的，各种编码和它的 304 都一样，因为 304 在知道编码之前就答了。
- 不用 brotli。纯 Go 的 brotli 编码器在 4 级和 5 级要花 gzip 的 4 到 6 倍时间，页面只小 3% 到 16%。
- `tifa dev` 的页面同样压缩。它的浏览器包来自 Vite 的开发服务器。

压缩可能通过回答的长度泄露机密（BREACH 攻击）。回答走 HTTPS，而且同一个正文既回显请求里的内容（比如搜索词），又带着用户的机密（比如 CSRF token）时，就有这个风险。给这样的回答设 `No-Gzip-Compression` 头，它就不压缩，这个头在回答发出前会被去掉。也可以关掉 `server.compress`。

前面有代理负责压缩时关掉它，比如用 brotli 的 nginx：代理不会再碰已经带 `Content-Encoding` 的回答。压缩的开销见[性能](performance.zh-CN.md#压缩)。
