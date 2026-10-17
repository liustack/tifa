---
summary: 'tifa 应用的全部配置：每项能在哪设置、谁优先、默认值、参数和环境变量名、值的写法，以及写错时的报错'
read_when:
  - 部署 tifa 应用，或要改端口、请求体上限、VM 数量、VM 栈深度、渲染超时、页面缓存
  - 设置构建的浏览器目标或静态文件目录
  - 程序因为配置错误起不来
  - 改 internal/config
---

[English](config.md) · 简体中文

# 配置

tifa 应用的默认值按小机器定好了，需要时才改。

## 配置从哪来

优先级从高到低：

1. `serve` 的命令行参数，比如 `--server-addr 0.0.0.0:9527`。
2. `TIFA_*` 环境变量，比如 `TIFA_SERVER_ADDR`。
3. `--config path.yaml` 指定的部署文件（`.yaml`、`.yml` 或 `.json`）。
4. 应用根目录的项目文件 `tifa.yaml` 或 `tifa.json`，`tifa build` 时嵌进程序。
5. 下表的默认值。

项目文件在构建时读取，`build.*` 只能写在这里。项目文件只留一个：`tifa.yaml` 和 `tifa.json` 同时存在会构建报错。

```yaml
# tifa.yaml
build:
  target: es2020,safari14
server:
  addr: 0.0.0.0:9527
  body_limit: 4MiB
ssr:
  timeout: 2s
cache:
  size: 128MiB
```

运行 `./bin/app config` 会把各来源合并后的配置打印成带注释的文件，可以拿来当部署文件的起点。

## 配置项

| 键 | 默认值 | 参数与环境变量 | 作用 |
| --- | --- | --- | --- |
| `build.target` | `es2020` | 只能写在项目文件 | Vite 构建的浏览器包的 JavaScript 目标，逗号分隔：一个 ECMAScript 版本（`es2015` 到 `es2029`、`esnext`）加若干带版本的浏览器，浏览器可选 chrome、edge、firefox、ios、opera 和 safari，如 `es2019,chrome90,safari14`。SSR 包固定是 ES2022 |
| `build.public` | `public` | 只能写在项目文件 | 静态文件目录，原样打进程序，从 `/` 提供 |
| `build.skip_tanstack_check` | `false` | 只能写在项目文件 | 设为 `true` 时，TanStack Router 不是 tifa 测过的版本也照样构建，页面在浏览器里可能白屏。见 [build.zh-CN.md](build.zh-CN.md#应用的-tsconfig-与-vite-配置) |
| `server.addr` | `127.0.0.1:9527` | `--server-addr`、`TIFA_SERVER_ADDR` | 监听地址。容器里用 `0.0.0.0:9527` |
| `server.body_limit` | `1MiB` | `--server-body-limit`、`TIFA_SERVER_BODY_LIMIT` | 请求体上限，超过答 413 |
| `server.shutdown_timeout` | `10s` | `--server-shutdown-timeout`、`TIFA_SERVER_SHUTDOWN_TIMEOUT` | 收到退出信号后，先给在途请求的时间，再给 `OnShutdown` 的时间。到时仍在运行的请求会被取消，再给它们一个 `shutdown_timeout` 返回。都返回了就执行 `OnShutdown`，没返回就跳过它。两种情况程序都以状态码 1 退出 |
| `ssr.vms` | CPU 数（`GOMAXPROCS`） | `--ssr-vms`、`TIFA_SSR_VMS` | 预热的 JavaScript VM 数，也就是同时能渲染几个页面。容器里跟随 CPU 限额 |
| `ssr.vm_memory` | `128MiB` | `--ssr-vm-memory`、`TIFA_SSR_VM_MEMORY` | 每个 VM 的内存上限。渲染时内存用尽则这次渲染失败，VM 换成新的。页面代码接不住它 |
| `ssr.vm_stack_depth` | `10000` | `--ssr-vm-stack-depth`、`TIFA_SSR_VM_STACK_DEPTH` | 每个 VM 里 JavaScript 调用最多嵌套多深，大约一次调用一层。更深的调用抛出 `RangeError: Maximum call stack size exceeded`，和 Node 停下的深度差不多。可设 1 到 10000，见[栈深度](#栈深度) |
| `ssr.timeout` | `3s` | `--ssr-timeout`、`TIFA_SSR_TIMEOUT` | 一次渲染的时间预算。超时的渲染被停下，按意外错误处理，交给 `OnError`。页面代码接不住它 |
| `cache.size` | `64MiB` | `--cache-size`、`TIFA_CACHE_SIZE` | `Cache = true` 页面的 HTML 缓存大小，`0` 关闭 |

## 栈深度

`ssr.vm_stack_depth` 默认是 10000，渲染里普通递归能到的深度和 Node 一样，Node 大约在 10400 层调用时停下。每一层占的是负责渲染的 Go goroutine 的栈，不算在 `ssr.vm_memory` 限制的内存里。普通页面只要几百层、几 MB 栈。无限递归的代码会一直走到底才抛错，这次渲染的栈随之涨到约 128 MB。Go 在下一次垃圾回收时收回。栈按翻倍增长，涨栈时如果正赶上垃圾回收，旧栈要等这轮回收结束才释放，所以这次渲染会短暂占到约 192 MB。

每个 VM 都可能同时这样，所以最坏情况是在 VM 本身的内存之外再加 `ssr.vms` 乘 192 MB。小内存机器上把深度调低。以下在 darwin/arm64 上测得，那里一层最多约 13 KB：

| `ssr.vm_stack_depth` | 无限递归的渲染占的栈 |
| --- | --- |
| `10000` | 约 128 MB，涨栈时最多约 192 MB |
| `5000` | 约 64 MB |
| `2000` | 约 16 MB |
| `1000` | 约 8 MB |

调低之后会出问题的：

- 递归超过上限的普通代码，比如遍历评论树或分类树的函数，不论在你的代码里还是库里。它抛出 `RangeError`，代码没接住时渲染失败。
- 嵌套超过上限的数据的 `JSON.parse`，包括 Go 交给渲染的数据。Node 没有这个限制。
- React 不受影响：组件树深到碰上限时，React 换一个新栈接着渲染剩下的部分。要远低于 1000 页面才会失败，报的是库做到一半栈不够时的怪错误。只有几十层时，程序启动时包就加载不了。示例商店的每个页面在 200 时都还能渲染。

`2000` 能让这样的页面照常工作，并把无限递归的渲染压在约 16 MB。

## 值的写法

- 大小写成整数加可选单位：`B`、`KiB`、`MiB` 或 `GiB`，比如 `512KiB`。不带单位就是字节。只有 `cache.size` 可以是 0。
- 时长用 Go 的写法，必须为正：`500ms`、`3s`、`1m30s`。
- `ssr.vms` 是不小于 1 的整数。
- `ssr.vm_stack_depth` 是 1 到 10000 的整数。再深的话，无限递归的代码会让 goroutine 栈超过 Go 的上限，整个进程直接退出。
- `build.skip_tanstack_check` 是 `true` 或 `false`，不加引号。

## 报错

项目文件写错时 `tifa build` 直接失败。下面几种情况程序拒绝启动，并指出来源和键：

- 文件或环境变量里有未知的键，比如 `TIFA_SERVER_PORT`。
- 值的类型或格式不对，比如 `ssr.timeout: 3`。
- 部署文件里写了 `build.*`。`build.*` 没有对应的参数，也没有 `TIFA_BUILD_*` 环境变量。

`serve --dist <目录>` 只给 [`tifa dev`](dev.zh-CN.md) 用，不是配置项，也没有环境变量。
