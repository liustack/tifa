---
summary: '测试与验证：提交前要跑的命令、先红后绿、默认跳过的 SSR 实验测试'
read_when:
  - 准备提交代码，确认要跑哪些检查
  - 写新测试或修 bug
  - 某个 SSR 测试显示 skip，想知道怎么打开
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

原型代码（`web/`、`scripts/` 里的构建脚本、`internal/ssr` 等）还在仓库里时，还要跑原型自己的整套检查。它会依次执行前端构建、`tsc --noEmit`、Node 测试和 `go test ./...`：

```sh
pnpm install --frozen-lockfile
pnpm test
```

`pnpm test` 的输出里会出现一行 `✘ [ERROR] Node APIs are not supported`，那是测试故意构造的失败场景，测试本身通过。

## 写测试

- 新行为与原因已知的 bug：先写测试，亲眼看到它失败，再写实现。
- 测试和被测代码放在同一个包里，文件名 `*_test.go`。
- 一个断言只证明一件事。循环里跑多个用例时，每个用例用独立的状态，并确认被测代码真的执行了。缓存、去重一类逻辑最容易让后面的用例拿到前面的结果，测试照样通过。
- 浏览器行为（hydration 有没有多余请求、导航只发一次请求、窄屏有没有横向溢出）用 `playwright-cli` 验证，结果截图。

## 默认跳过的 SSR 实验测试

`internal/ssr` 里有一批性能与内存实验，默认 skip，不影响 `go test ./...` 的耗时。要跑时设置对应环境变量，比如：

| 变量 | 打开的测试 |
| --- | --- |
| `SSR_COMPARE_CONTROL=1` | `TestSSRControlledComparison`：从 stdin 接收命令，按批次计时，供外部脚本交替测量多个构建 |
| `SSR_PROFILE_SOURCE=<插桩包>` | `TestSSRStageProfile`：分阶段计时，需要用 `buildSSR({ profile: true })` 构建的包 |
| `SSR_PAIR_BASELINE=<旧包>` | `TestSSRPairedComparison`：同一进程里新旧两个包成对比较 |
| `SSR_MEMORY_PROBES`、`SSR_OOM_AUDIT` 等 | 内存与 OOM 回收实验 |

完整列表在 `internal/ssr/*_test.go` 里搜 `os.Getenv("SSR_`。预热次数、请求数等参数通过 `SSR_WARMUP`、`SSR_REQUESTS` 这类变量调整。

性能对比要在同一台机器上交替跑新旧构建，记录机器负载。机器负载变化会让绝对耗时差几倍，只有同轮交替测得的比值可以拿来比较。
