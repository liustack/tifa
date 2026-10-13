---
summary: '测试与验证：提交前要跑的命令、先红后绿、默认跳过的 SSR 长测与实验测试'
read_when:
  - 准备提交代码，确认要跑哪些检查
  - 写新测试或修 bug
  - 某个 SSR 测试显示 skip，想知道怎么打开
  - 改 `internal/bundle`，或打包测试显示 skip
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

根目录的命令只覆盖框架。原型示例 `examples/shop` 是独立模块，改到它时要在该目录里跑它自己的整套检查。它会依次执行前端构建、`tsc --noEmit`、Node 测试和 `go test ./...`：

```sh
cd examples/shop
pnpm install --frozen-lockfile
pnpm test
go vet ./...
CGO_ENABLED=0 go build ./...
```

`pnpm test` 的输出里会出现一行 `✘ [ERROR] Node APIs are not supported`，那是测试故意构造的失败场景，测试本身通过。

## 写测试

- 新行为与原因已知的 bug：先写测试，亲眼看到它失败，再写实现。
- 测试和被测代码放在同一个包里，文件名 `*_test.go`。
- 一个断言只证明一件事。循环里跑多个用例时，每个用例用独立的状态，并确认被测代码真的执行了。缓存、去重一类逻辑最容易让后面的用例拿到前面的结果，测试照样通过。
- 浏览器行为（hydration 有没有多余请求、导航只发一次请求、窄屏有没有横向溢出）用 `playwright-cli` 验证，结果截图。

## 打包测试（`internal/bundle`）

需要 React、core-js 的打包测试从 `examples/shop/node_modules` 解析依赖。没装时这些测试显示 skip，先在 `examples/shop` 里跑 `pnpm install --frozen-lockfile`。打包本身不需要 Node，可以在 PATH 里去掉 node 后验证：

```sh
env PATH=/usr/bin:/bin:$(go env GOROOT)/bin go test ./internal/bundle
```

`TestShopBundleRendersInQuickJS` 默认跳过。它用 Go 打包器重新打包原型，在原型 Go 模块的临时副本里跑原型的 `go test ./...`（QuickJS 渲染订单、检查 hydration 脚本），不改动 `examples/shop`。它依赖原型构建生成的文件：

```sh
(cd examples/shop && pnpm install --frozen-lockfile && pnpm build)
TIFA_SHOP_E2E=1 go test ./internal/bundle -run TestShopBundle -v
```

## 默认跳过的 SSR 长测

`internal/ssr` 的默认测试只用 `testdata/app.js` 这个手写小包，几秒内跑完。下面几组默认 skip：

| 命令 | 内容 |
| --- | --- |
| `TIFA_SSR_SOAK=1 go test -tags memory.counters -run Soak -v ./internal/ssr` | 正常复用长测（2 个 VM、2 个调用方、10,000 次渲染）与 OOM 回收测试（8 次耗尽内存并替换 VM），关闭后原生分配数要回到基线 |
| `TIFA_SSR_SHOP=1 go test -run Shop -v ./internal/ssr` | 用 `examples/shop` 的真实 React 包渲染，检查页面内容、脚本转义，并打印热渲染 P50/P95/P99 |

两个变量可以一起设，长测就会再用真实 React 包跑一遍。用真实包前先在 `examples/shop` 里 `pnpm install --frozen-lockfile && pnpm build`。真实包的订单详情页数据含中文，OOM 会落在 `JSON.parse` 转换非 ASCII 字符串的路径上，这正是 github.com/liustack/quickjs 修掉的泄漏。这项测试失败时，先确认依赖的是这个 fork 而不是 modernc.org/quickjs。

## 默认跳过的 SSR 实验测试

`examples/shop/internal/ssr` 里有一批性能与内存实验，默认 skip，不影响 `go test ./...` 的耗时。要跑时设置对应环境变量，比如：

| 变量 | 打开的测试 |
| --- | --- |
| `SSR_COMPARE_CONTROL=1` | `TestSSRControlledComparison`：从 stdin 接收命令，按批次计时，供外部脚本交替测量多个构建 |
| `SSR_PROFILE_SOURCE=<插桩包>` | `TestSSRStageProfile`：分阶段计时，需要用 `buildSSR({ profile: true })` 构建的包 |
| `SSR_PAIR_BASELINE=<旧包>` | `TestSSRPairedComparison`：同一进程里新旧两个包成对比较 |
| `SSR_MEMORY_PROBES`、`SSR_OOM_AUDIT` 等 | 内存与 OOM 回收实验 |

测试要在 `examples/shop` 目录里运行（如 `SSR_MEMORY_PROBES=1 go test ./internal/ssr -run TestSSRMemoryProbes -v`）。完整列表在 `examples/shop/internal/ssr/*_test.go` 里搜 `os.Getenv("SSR_`。预热次数、请求数等参数通过 `SSR_WARMUP`、`SSR_REQUESTS` 这类变量调整。

性能对比要在同一台机器上交替跑新旧构建，记录机器负载。机器负载变化会让绝对耗时差几倍，只有同轮交替测得的比值可以拿来比较。
