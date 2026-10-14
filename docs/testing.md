---
summary: '测试与验证：提交前要跑的命令、示例应用的检查、端到端测试、先红后绿、默认跳过的 SSR 长测'
read_when:
  - 准备提交代码，确认要跑哪些检查
  - 写新测试或修 bug
  - 某个 SSR 测试显示 skip，想知道怎么打开
  - 改 `internal/bundle`，或打包测试显示 skip
  - 跑端到端测试，或改 `internal/codegen`、`test/apps`、`examples/shop`
---

# 测试与验证

## 提交前

Go 改动全部要过：

```sh
go test ./...
go vet ./...
CGO_ENABLED=0 go build ./...
```

涉及并发的包（请求上下文、`tifa.Use`、配置加载、服务器）再加一次竞态检测：

```sh
go test -race ./<包路径>
```

根目录的命令只覆盖框架。示例应用 `examples/shop` 是独立模块，改到它时在该目录里跑它自己的检查：

```sh
cd examples/shop
pnpm install --frozen-lockfile
go tool tifa build
pnpm typecheck
go vet ./...
go test ./...
```

## 端到端测试（`test/e2e`）

`test/e2e` 构建 `tifa` CLI，对 `test/apps/` 里的应用和 `examples/shop` 跑 `tifa build`，启动产物并从外部检查。默认跳过。先在 `test/apps/pages` 与 `examples/shop` 里 `pnpm install --frozen-lockfile`，再打开：

```sh
TIFA_E2E=1 go test ./test/e2e/
```

- `test/apps/api`：只有接口路由，带全部四个钩子。检查 `OnStart`、各 `OnXxx`、404 JSON、`tifa.Error` 状态码、`OnError`，以及收到 SIGINT 后 `OnShutdown` 执行。
- 每个应用构建前先确认 `go mod tidy -diff` 没有改动，再在 PATH 里去掉 node、设 `GOFLAGS=-mod=readonly` 构建，构建后 `go.mod`、`go.sum` 不变。
- `test/apps/pages`：页面、嵌套 Layout、pathless Layout、查询参数、`tifa.Use`、`SSR = false`、`Cache = true`、ETag 与 304、导航数据请求、三种 404。
- `TestShopApp`：示例应用的整页渲染、`Cache = true`、304、三种 404 落在哪一层、发货（跨站请求 403、重复发货 409）、发货后导航数据里的统计、构建版本不一致的 409，以及应用里 `go vet ./...`、`go test ./...` 通过且不扫 `.tifa/`。
- `TestShopBuildDoesNotRunInit`：在示例应用的临时副本里加一个 `init()` 会写文件的包，`tifa build` 后文件不存在，运行产物后才出现。
- `TestBuildReportsViolations`：对 `internal/analyze/testdata` 里 Path 重复、页面定义 `OnGet`、Layout 参数不在公共前缀、`tifa.Use` 引用非祖先、`Load` 返回类型不能转 TS 这几个应用跑 `tifa build`，构建失败并给出文件与行号。
- pages 与示例应用另有 `tsc` 子测试：`tsc --noEmit`，生成的 TS 和应用的 TSX 一起类型检查。应用没装 npm 依赖或没有 node 时这个子测试 skip。
- `TestDev`：在 `test/apps/pages` 的临时副本上跑 `tifa dev`（PATH 里去掉 node），逐个改 TSX、Go 函数体、路由 Path，按日志检查每次只执行该执行的步骤，再制造编译错误，检查旧版本继续服务、SSE 推送错误、修好后恢复。它借用 `test/apps/pages/node_modules`，没装时 skip。
- `TestErrorFlowInTheVM`：在 `test/apps/pages` 的临时副本里加上会出错的页面与钩子，用真实 QuickJS 检查错误流向：组件抛错算意外错误且不进 HTML 缓存，错误页渲染超时交给 `OnError`，路由器重定向不带 ETag，中间件对数据请求的拒绝与重定向，`RouteError` 带状态码写进页面。同样借用 `test/apps/pages/node_modules`。

`test/apps` 下每个应用是独立模块，根目录的 `go test ./...` 不会扫到它们。构建会在应用目录写 `.tifa/` 与 `node_modules/`，两者都已忽略。改了 `internal/codegen` 的输出后，在 `internal/codegen` 里跑 `go test -update` 刷新 golden 文件，再看 diff。

`internal/codegen` 的 `TestGeneratedRouterBehaves` 为 `testdata/router` 生成路由树，用 Node 跑 `testdata/router/router.test.tsx`：经 SSR 入口整页渲染、在内存 history 上做客户端加载、对替换掉的 `fetch` 检查数据收集器，最后连同运行时一起 `tsc --noEmit`。它借用 `test/apps/pages/node_modules`，没装 Node 或没在该目录 `pnpm install --frozen-lockfile` 时 skip。改路由树生成或 `js/runtime` 时先在这里补用例。

## 写测试

- 新行为与原因已知的 bug：先写测试，亲眼看到它失败，再写实现。
- 测试和被测代码放在同一个包里，文件名 `*_test.go`。
- 一个断言只证明一件事。循环里跑多个用例时，每个用例用独立的状态，并确认被测代码真的执行了。缓存、去重一类逻辑最容易让后面的用例拿到前面的结果，测试照样通过。
- 浏览器行为（hydration 有没有多余请求、导航只发一次请求、窄屏有没有横向溢出）用 `playwright-cli` 验证，结果截图。

## 打包测试（`internal/bundle`）

需要 React、core-js 的打包测试从 `internal/bundle/testdata/node_modules` 解析依赖，版本锁在同目录的 `package.json` 与 `pnpm-lock.yaml`。没装时这些测试显示 skip，先在 `internal/bundle/testdata` 里跑 `pnpm install --frozen-lockfile`。打包本身不需要 Node，可以在 PATH 里去掉 node 后验证：

```sh
env PATH=/usr/bin:/bin:$(go env GOROOT)/bin go test ./internal/bundle
```

## 默认跳过的 SSR 长测

`internal/ssr` 的默认测试只用 `testdata/app.js` 这个手写小包，几秒内跑完。下面几组默认 skip：

| 命令 | 内容 |
| --- | --- |
| `TIFA_SSR_SOAK=1 go test -tags memory.counters -run Soak -v ./internal/ssr` | 正常复用长测（2 个 VM、2 个调用方、10,000 次渲染）与 OOM 回收测试（8 次耗尽内存并替换 VM），关闭后原生分配数要回到基线 |
| `TIFA_SSR_SHOP=1 go test -run Shop -v ./internal/ssr` | 用 `examples/shop` 的真实 React 包渲染两家店铺的列表页和详情页，检查页面内容、脚本转义、VM 复用不串数据，并打印热渲染 P50/P95/P99 |

两个变量可以一起设，长测就会再用真实 React 包跑一遍。用真实包前先在 `examples/shop` 里 `pnpm install --frozen-lockfile && go tool tifa build`：测试读 `.tifa/dist/ssr.js`，并启动构建出的 `dist/shop`，按导航数据请求取每个页面的数据，与整页渲染时注入的数据相同。改了示例应用后重新构建再跑。真实包的订单详情页数据含中文，OOM 会落在 `JSON.parse` 转换非 ASCII 字符串的路径上，这正是 github.com/liustack/quickjs 修掉的泄漏。这项测试失败时，先确认依赖的是这个 fork 而不是 modernc.org/quickjs。

## 性能对比

原型的性能与内存实验（交替计时、分阶段计时、内存探针）没有迁入，留在标签 `prototype-baseline` 的 `internal/ssr` 里，需要时从该标签导出构建。

性能对比要在同一台机器上交替跑新旧构建，记录机器负载。机器负载变化会让绝对耗时差几倍，只有同轮交替测得的比值可以拿来比较。
