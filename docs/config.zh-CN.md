---
summary: 'tifa 应用的全部配置：每项能在哪设置、谁优先、默认值、参数和环境变量名、值的写法，以及写错时的报错'
read_when:
  - 部署 tifa 应用，或要改端口、请求体上限、VM 数量、渲染超时、页面缓存
  - 设置构建的 esbuild 目标或静态文件目录
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
| `build.target` | `es2020` | 只能写在项目文件 | 浏览器包的 JavaScript 目标，写法同 esbuild 的 `--target`：一个 ECMAScript 版本加若干带版本的浏览器，浏览器可选 chrome、edge、firefox、ios、opera 和 safari。SSR 包固定是 ES2022 |
| `build.public` | `public` | 只能写在项目文件 | 静态文件目录，原样打进程序，从 `/` 提供 |
| `server.addr` | `127.0.0.1:9527` | `--server-addr`、`TIFA_SERVER_ADDR` | 监听地址。容器里用 `0.0.0.0:9527` |
| `server.body_limit` | `1MiB` | `--server-body-limit`、`TIFA_SERVER_BODY_LIMIT` | 请求体上限，超过答 413 |
| `server.shutdown_timeout` | `10s` | `--server-shutdown-timeout`、`TIFA_SERVER_SHUTDOWN_TIMEOUT` | 收到退出信号后，先给在途请求的时间，再给 `OnShutdown` 的时间。到时仍在运行的请求会被取消，再给它们一个 `shutdown_timeout` 返回。都返回了就执行 `OnShutdown`，没返回就跳过它。两种情况程序都以状态码 1 退出 |
| `ssr.vms` | CPU 数（`GOMAXPROCS`） | `--ssr-vms`、`TIFA_SSR_VMS` | 预热的 JavaScript VM 数，也就是同时能渲染几个页面。容器里跟随 CPU 限额 |
| `ssr.vm_memory` | `128MiB` | `--ssr-vm-memory`、`TIFA_SSR_VM_MEMORY` | 每个 VM 的内存上限。渲染时内存用尽则这次渲染失败，VM 换成新的 |
| `ssr.timeout` | `3s` | `--ssr-timeout`、`TIFA_SSR_TIMEOUT` | 一次渲染的时间预算。超时的渲染被停下，按意外错误处理，交给 `OnError` |
| `cache.size` | `64MiB` | `--cache-size`、`TIFA_CACHE_SIZE` | `Cache = true` 页面的 HTML 缓存大小，`0` 关闭 |

## 值的写法

- 大小写成整数加可选单位：`B`、`KiB`、`MiB` 或 `GiB`，比如 `512KiB`。不带单位就是字节。只有 `cache.size` 可以是 0。
- 时长用 Go 的写法，必须为正：`500ms`、`3s`、`1m30s`。
- `ssr.vms` 是不小于 1 的整数。

## 报错

项目文件写错时 `tifa build` 直接失败。下面几种情况程序拒绝启动，并指出来源和键：

- 文件或环境变量里有未知的键，比如 `TIFA_SERVER_PORT`。
- 值的类型或格式不对，比如 `ssr.timeout: 3`。
- 部署文件里写了 `build.*`。`build.*` 没有对应的参数，也没有 `TIFA_BUILD_*` 环境变量。

`serve --dist <目录>` 只给 [`tifa dev`](dev.zh-CN.md) 用，不是配置项，也没有环境变量。
