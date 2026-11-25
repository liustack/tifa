---
summary: 'Tifa 应用在一个核上能提供多少，以及怎样估算资源：Next.js、TanStack Start、`ssr.vms`、内存、过载、压缩'
audience: app
order: 2
translatedFrom: ed52a781d609
read_when:
  - '决定 Tifa 应用需要多少 CPU 和内存，或者设置 `ssr.vms`'
  - '请求到达的速度比页面渲染的速度快，或者需要给渲染队列定大小'
  - '问 Tifa 有多快，或者它和 Next.js 或 TanStack Start 相比怎样'
  - '决定要不要关掉 `server.compress`'
---

# 性能 {#performance}

这篇文档给出为机器定大小用的数字：一个 Tifa 应用每核能回答多少请求、要多少内存，以及在同一个应用上和 Next.js、TanStack Start 比如何。完整的表格和方法见 [容量测量](../studies/capacity.md)，渲染是怎么变快的见 [优化](../studies/optimizations.md)。

## 概览 {#at-a-glance}

- **单核上的整个站点**（2026-11-18）：Tifa 每秒响应 118.8 次页面浏览，Next.js 是 28.3，TanStack Start 是 94.7，都是出厂设置。见 [对商店的整次访问](#whole-visits-to-the-shop-2026-11-18)。
- **每个请求都渲染，一次一个**（2026-11-18）：Next.js 响应的页面请求是 Tifa 的 1.17 倍，TanStack Start 是 1.57 倍，而它们的峰值内存是 Tifa 的 7 倍和 6 倍。见 [这项研究](../studies/nextjs-and-tanstack-start.md#conclusions)。
- **每个请求都渲染时的容量规划**（关掉渲染记忆 memo）：对像 shop 工作台这样的页面，每个物理核心每秒大约按 42 个页面请求来规划。
- **核数**：3 个物理核心响应的请求数是 1 个的 2.94 倍。
- **内存**：一个 VM 服务整个 shop 时，进程的内存稳定在大约 105 MiB。每多一个 VM，启动时增加 9 MiB，渲染过很多页面之后最多增加到约 40 MiB。

## 对比 Next.js 和 TanStack Start {#against-nextjs-and-tanstack-start}

shop 用三种方式构建：Tifa，以及 Next.js 和 TanStack Start 应用，都用同样的 React 组件。Node 应用运行加载每个页面数据的那段 Go 代码的 TypeScript 移植版。测试检查三者在每个页面上显示相同的文字，每个请求做的工作也相同。[这项研究](../studies/nextjs-and-tanstack-start.md) 里有方法和 [哪些地方并不相等](../studies/nextjs-and-tanstack-start.md#what-is-not-equal)。要重新测量，见 [测试](../internals/testing.md#comparison-with-nextjs-and-tanstack-start-testcompare)。

### 对商店的整次访问（2026-11-18） {#whole-visits-to-the-shop-2026-11-18}

我们录下了真实浏览器在每个框架上访问 shop 的过程，再把录下的访问对每个服务器回放。店面和后台是同一个站点：买家浏览、搜索、加入购物车和结账，员工编辑后台，爬虫抓取公开页面，这些同时发生。每个框架都按出厂设置运行，在一台 Google Cloud `t2d-standard-4` VM（AMD EPYC 7B13）的一个核心上跑一个进程，经过同样的预热。Tifa v0.1.0 搭配 slowjs v0.31.6，Next.js 16.4.6，TanStack Start 1.169.3，Node 24.13.0。[流量和方法](../studies/capacity.md#whole-visits-the-traffic-and-the-method)。

吞吐量，8 个并发会话，没有思考时间，3 轮的中位数：

| | 页面浏览/秒 | 整页加载/秒 | 客户端导航/秒 | 每次页面浏览的 CPU | 每次页面浏览的字节数 | 峰值 RSS |
| --- | --: | --: | --: | --: | --: | --: |
| Tifa | 118.8 | 38.3 | 80.5 | 8.4 ms | 12 KB | 245 MiB |
| Next.js | 28.3 | 9.1 | 19.2 | 35.3 ms | 14 KB | 613 MiB |
| TanStack Start | 94.7 | 30.5 | 64.2 | 10.6 ms | 178 KB，未压缩 | 451 MiB |
| 参考：关掉渲染记忆 memo 的 Tifa | 78.0 | 25.1 | 52.8 | 12.8 ms | 12 KB | 84 MiB |
| 参考：开 gzip 的 TanStack Start | 64.5 | 20.8 | 43.7 | 15.5 ms | 29 KB | 492 MiB |
| 参考：关掉预取的 Next.js | 61.7 | 19.9 | 41.8 | 16.2 ms | 12 KB | 632 MiB |

每秒 2.75 次访问时的延迟，一轮 3,060 次访问的 P50 / P99：

| | 整页加载 | 客户端导航 |
| --- | --: | --: |
| Tifa | 31.3 / 65 ms | 2.3 / 30 ms |
| Next.js | 45.1 / 125 ms | 18.0 / 118 ms |
| TanStack Start | 27.5 / 62 ms | 3.4 / 31 ms |
| 参考：关掉渲染记忆 memo 的 Tifa | 36.5 / 69 ms | 2.6 / 37 ms |
| 参考：开 gzip 的 TanStack Start | 40.7 / 107 ms | 4.3 / 37 ms |
| 参考：关掉预取的 Next.js | 36.2 / 108 ms | 7.7 / 59 ms |

- 整页加载是用一整个页面来响应的请求。客户端导航是路由只用数据来响应的请求。大约三分之一的页面浏览是整页加载。
- 主要的行是出厂设置的各个框架。每个参考行只改一个设置，并说明这个设置花掉了什么或换来了什么。
- Next.js 会预取进入视野的链接，服务器也要响应这些请求。TanStack Start 发送的页面没有压缩。
- Tifa 的渲染记忆 memo 响应了 41% 的请求（15,780 个里的 6,545 个）。其余是第一次被请求的页面，或被写入改动过的页面。

## 为机器定大小 {#sizing-a-machine}

### CPU 和 `ssr.vms` {#cpu-and-ssrvms}

整页加载的 CPU 大部分花在渲染上，而一次渲染比在 Node 里慢：Tifa 在 beastjs 里渲染，beastjs 是一个没有 JIT 的纯 Go JavaScript 引擎。

- [`ssr.vms`](../build-and-deploy/config.zh-CN.md#settings) 默认是 Go 看到的 CPU 数，最多 4。超过 4 个 CPU 时，内存够的话就把它设成 CPU 数。
- 对像 shop 工作台这样的页面，每个物理核心每秒大约按 42 个页面请求来规划，并关掉渲染记忆 memo 让每个请求都渲染。吞吐量跟着核数走：两核是 1.99 倍，三核是 2.94 倍。
- 云上的一个 vCPU 往往是一个超线程。所有超线程都忙的时候，每个 vCPU 大约按 0.6 个物理核心来规划。
- VM 比 CPU 多不会增加吞吐量，反而让每次渲染变慢。
- 只有整页加载才会渲染。在浏览器里导航取的是 Go 不用 VM 写出的数据，API 路由也不渲染。渲染记忆 memo 响应的页面大约花 0.15 ms 的 CPU，比一次渲染少约 80 倍。
- 同时到达的、请求同一个页面的请求共用一次渲染，所以冷页面上的一阵突发只花一次渲染。

### 过载 {#overload}

让平时的到达速率低于机器的渲染能力。默认最多有 32 个渲染可以等 VM，每个最多等 3 秒。超过之后，Go 不占用 VM，直接响应 503，带 `Retry-After: 1`。见 [渲染队列](../build-and-deploy/config.zh-CN.md#render-queue)。

在 1.5 倍于它容量的负载下，shop 拒绝了 34% 的请求，其余请求的 P50 大约是 390 ms。在 3 倍下，它拒绝了 68%，其余请求的 P50 仍约为 395 ms，内存没有增长。要看成功请求的延迟：大多数请求被很快拒绝时，所有请求的延迟看起来会很低。[具体步骤](../studies/capacity.md#overload)。

### 内存 {#memory}

- **进程**：启动后带一个 VM 时大约 47 MiB，每多一个 VM 多 9 MiB。
- **VM 渲染的页面种类越多就越大，然后稳定下来。** shop 的一个 VM 加载后占 9 MB，渲染完它所有页面之后占 17 到 25 MB，每次渲染的数据都是新的时最多占 39 MB。页面很多的应用，每个 VM 按最多 40 MiB 来规划。
- **实测**：一个 VM 以每秒 58 个请求服务 shop 的 525 个页面，持续 40 分钟，进程的内存稳定在大约 105 MiB（Tifa v0.0.13）。在一个核心满负载下，整次访问的测试里，带渲染记忆 memo 时峰值是 245 MiB，不带时是 84 MiB。
- **渲染记忆 memo** 最多保存 [`ssr.memo_size`](../build-and-deploy/config.zh-CN.md#settings)，默认 64 MiB。
- **`ssr.vm_memory`**（默认 128 MiB）限制每个 VM。一次回收之后，VM 占用超过它的一半时，池会在后台替换这个 VM，所以泄漏内存的应用，每个 VM 里最多保留 64 MiB。设得更低，就会更早替换。如果日志说新的 VM 放不下，就把它调高。
- **空闲的 VM 会把空闲内存还给系统**，在一秒没有渲染之后。

VM 是怎么被回收和替换的，以及每个数字是用什么测的：[内存](../studies/capacity.md#memory)。

## 压缩 {#compression}

[`server.compress`](../build-and-deploy/config.zh-CN.md#settings) 默认开启。压缩一个页面花掉它渲染时间的 1% 到 2%，让它变小约 6 倍。浏览器的包以构建时写好的 gzip 文件发出，不占 CPU。来自渲染记忆 memo 的页面以它的 gzip 形式保存，所以以压缩形式响应比不压缩还省。[这些表格](../studies/capacity.md#compression)。

`server.csp` 会给一次渲染增加约 0.1%。

## 重新测量 {#measuring-again}

[测试](../internals/testing.md#performance-comparisons) 里有命令，[容量测量](../studies/capacity.md#measuring-again) 里有让数字可比的规则。在同一台机器上，用交替的轮次比较两个构建：只有同一批轮次得出的比值才能比较。
