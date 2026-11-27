---
summary: '在 Fly.io、Railway、Render、Kubernetes 等平台上把 Tifa 应用当容器运行：镜像、端口、健康检查、发布前迁移、SQLite 还是 Postgres，以及哪些平台跑不了'
audience: app
order: 24
translatedFrom: 097d970bd310
read_when:
  - '把 Tifa 应用打进容器镜像，或者用 Fly.io、Railway、Render 等容器平台自己的工具部署它'
  - '平台说没有东西在监听它的端口，或者部署后上线的端口不对'
  - '在这类平台上决定迁移该在哪里跑，或者迁移找不到 SQLite 文件'
  - '把应用的 SQLite 文件放在平台的卷上，或者想跑不止一个实例'
  - '想知道 Vercel functions 这类无服务器平台能不能跑 Tifa'
---

# 在容器平台上运行 {#run-on-a-container-platform}

Tifa 应用是一个程序，所以它的镜像里只有这个程序。这一篇写给把你的镜像当作长期运行的容器、并用自己的工具部署的平台：Fly.io、Railway、Render、Kubernetes 等。[`tifa deploy`](deploy.zh-CN.md#deploy-with-tifa-deploy) 用于你自己的 Linux 服务器，在这些平台上不用它。Kubernetes 的清单见[部署到 Kubernetes](kubernetes.zh-CN.md)。

## 镜像 {#the-image}

构建阶段需要 Go，还需要带 pnpm 的 Node，因为前端用 Vite 打包。最终镜像里只有程序：没有 Node，因为程序从不需要它。

```dockerfile
# Dockerfile
FROM golang:1.27 AS go

FROM node:24-bookworm AS build
COPY --from=go /usr/local/go /usr/local/go
ENV PATH=/usr/local/go/bin:$PATH CGO_ENABLED=0
RUN npm install -g pnpm@10
WORKDIR /src
# 先复制写明依赖的文件，这样改代码时可以复用这些层。
COPY go.mod go.sum ./
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
RUN go mod download && pnpm install --frozen-lockfile --ignore-scripts
COPY . .
RUN go tool tifa build -o /out/app && mkdir /data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/app /app
COPY --from=build --chown=65532:65532 /data /data
WORKDIR /data
ENV HOST=0.0.0.0
EXPOSE 9527
USER 65532:65532
ENTRYPOINT ["/app"]
```

```
# .dockerignore
.git
.tifa
bin
node_modules
*.db
*.db-*
.env
```

- `pnpm install --ignore-scripts` 跳过 `prepare` 脚本，`tifa new` 写的 CI 也是这样：`tifa build` 自己会写 `.tifa/`。用 npm 的应用改成运行 `npm ci --ignore-scripts`，并复制 `package-lock.json`。
- `go tool tifa build` 运行[构建程序](build.zh-CN.md#tifa-build)里说的 Go 和 Node 步骤，写出一个文件。请在运行它的系统上构建镜像，或者用 `docker buildx --platform`。在 ARM Mac 上不带 `--platform` 为 x86 平台构建，得到的镜像启动不了。
- 程序是静态的（`CGO_ENABLED=0`），所以 distroless 镜像就够了。`distroless/static` 带着程序对外发 HTTPS 请求所需的 CA 证书，还有编号 65532 的用户 `nonroot`。`FROM scratch` 也行，只要你自己从构建阶段复制 `/etc/ssl/certs/ca-certificates.crt`。
- `HOST=0.0.0.0` 让程序监听所有网卡，因为默认的 `127.0.0.1` 从容器外面连不上。为什么这里不用 `SERVER_ADDR`，见[端口](#the-port)。
- `/data` 是工作文件夹，所以相对路径的 `db.path`（比如新应用的 `app.db`）会落在这里。应用要保存文件时，在它上面挂一个卷。
- 把镜像交给别人时，请把许可证声明一起带上（见[随程序一起发出的声明](build.zh-CN.md#notices-to-ship-with-the-program)）。

## 端口 {#the-port}

没有设置 `SERVER_ADDR` 和 `--server-addr` 时，程序读 `PORT` 和 `HOST` 作为 `server.addr`（见 [PORT 和 HOST](config.zh-CN.md#port-and-host)）：

- 只有 `PORT` 时监听所有网卡：`PORT=10000` 就是 `:10000`。
- 只有 `HOST` 时保留下面各来源给出的 `server.addr` 的端口，默认是 `9527`。`HOST=0.0.0.0` 监听 `0.0.0.0:9527`。
- 两个都有时，地址是 `HOST:PORT`。空值算没设置。

所以上面的镜像只设 `HOST=0.0.0.0`，不设地址：设了 `PORT` 的平台得到它给的端口，没设的得到 9527。不要在镜像里设 `SERVER_ADDR`，它的优先级高于 `PORT`，平台给的端口会被忽略。

平台负责 HTTPS，再把普通 HTTP 转给容器，所以不要打开程序自己的 HTTPS。在任何代理后面都要核对两件事：`Host` 和 `Origin` 必须原样到达程序，因为 Tifa 的[跨站写入](headers-and-origins.zh-CN.md#cross-site-writes)检查要比较它们，而且程序不读 `X-Forwarded-For`（见[在 CDN 或代理后面](deploy-proxy.zh-CN.md#behind-a-cdn-or-proxy)）。

## 健康检查和停止 {#health-check-and-stop}

- **健康检查。** 加上[健康检查](deploy-by-hand.zh-CN.md#health-check)里的 `/healthz` 路由（新应用还没有它），再让平台的检查指向它。程序在 `OnStart` 完成之后才开始监听，所以收到 204 就说明它能提供服务。
- **停止。** 收到 SIGTERM（和 SIGINT）后，程序停止监听，给处理中的请求 `server.shutdown_timeout`（默认 10 秒），取消剩下的并再等同样长的时间，然后运行 `OnShutdown`，同样最多这么久。默认设置下最多 30 秒。请给平台设一个至少一样长的停止时间，或者调低 `SERVER_SHUTDOWN_TIMEOUT`。
- **日志。** 程序把日志写到 stdout 和 stderr，平台正是从那里收集。不会写日志文件。
- **设置和密钥。** 设置是环境变量：键里的点换成下划线，用大写（见[名字](config.zh-CN.md#names)）。把它们和密钥放在平台的变量里，不要放进镜像。

## 迁移 {#migrations}

在新版本上线之前，用新镜像把迁移跑一次：应用的 `migrate` 命令做这件事（`./app migrate`，在 Kubernetes 上是 `args: [migrate]`）。失败会拦住这次发布，旧版本继续服务。`OnStart` 也会跑迁移，之后它会发现没有要做的。每个迁移都要写成上一个版本还能继续工作（见[让回滚继续可用](../guides/database.zh-CN.md#keep-a-rollback-working)和[在启动时，还是在切换之前](../guides/database.zh-CN.md#at-start-or-before-the-switch)）。

| 平台 | `migrate` 放在哪里 |
| --- | --- |
| Fly.io | `fly.toml` 的 `[deploy]` 里写 `release_command = "/app migrate"`。它在一台用新镜像起的临时 Machine 里运行，每次部署一次，退出码非零就停止部署（[Run one-off commands before releasing](https://docs.fly.io/reference/configuration/#run-one-off-commands-before-releasing-a-deployment)）。 |
| Railway | 服务的 pre-deploy command，写 `/app migrate`。它在构建和部署之间运行，失败会停止部署（[Pre-deploy command](https://docs.railway.com/deployments/pre-deploy-command)）。 |
| Render | 服务的 pre-deploy command，写 `/app migrate`。它在构建之后、部署之前运行，失败则这次部署失败，旧版本继续运行（[Deploys](https://render.com/docs/deploys)）。 |
| Kubernetes | 一个 Job，或者 Helm 的 `pre-upgrade` 钩子（见[迁移](kubernetes.zh-CN.md#migrations)和 [Helm hooks](https://helm.sh/docs/topics/charts_hooks/)）。 |

这些步骤在和提供服务的容器分开的容器里运行，而 Fly.io、Railway 和 Render 不会把卷挂到那里。所以 SQLite 放在卷上时，不要设发布前命令，让 `OnStart` 去跑迁移：SQLite 文件要等应用运行起来才在那里。Postgres 应用在任何平台上都可以用这个命令。

## 数据放在哪里 {#where-the-data-lives}

- **SQLite** 需要一个持久卷，挂在文件所在的文件夹上（上面的镜像里是 `/data`），并且只能有一个实例。两个实例会各写各的文件，几个平台的限制也是这样：Fly.io 的卷只能挂到一台 Machine，Railway 的一个服务只有一个卷、有卷就不能用副本，Render 带磁盘的服务不能横向扩展。带卷的服务重新部署时，会先停旧实例，所以每次部署都有一小段停顿。请打开 [`db.backup`](../guides/database.zh-CN.md#back-it-up)，卷丢了数据也不会丢。
- **多个实例** 需要 Postgres 这样的数据库服务器，会话也放进去（见[会话存在哪里](../pages-and-data/sessions.zh-CN.md#where-to-store-sessions)和 [Postgres](../guides/database.zh-CN.md#postgres)）。渲染记忆和 `OnStart` 里启动的后台循环在每个实例里各有一份，所以要把循环写成两个实例同时跑也没问题。
- **归 root 所有的卷。** 应用打开数据库时报 "permission denied"，说明卷归 root 所有，而镜像以用户 65532 运行。请在那个平台上让容器以 root 运行，或者用平台上能把卷挂给该用户的设置（如果有的话）。在 Railway 上，请在服务上设 `RAILWAY_RUN_UID=0`（[Volumes](https://docs.railway.com/volumes)）。

## 各个平台 {#each-platform}

| | Fly.io | Railway | Render | Kubernetes |
| --- | --- | --- | --- | --- |
| 端口 | `fly.toml` 里 `[http_service]` 的 `internal_port`，不设就是 8080：请设成 `9527`（[参考](https://docs.fly.io/reference/configuration/#the-http_service-section)） | Railway 注入 `PORT`（[健康检查](https://docs.railway.com/deployments/healthchecks)） | `PORT` 是 10000，服务必须绑定 `0.0.0.0`（[web services](https://render.com/docs/web-services)） | 容器的 `containerPort` 和 Service 的 `targetPort` |
| 健康检查 | `[[http_service.checks]]` 里的 `path`（[参考](https://docs.fly.io/reference/configuration/#http_servicechecks)） | 服务设置里的健康检查路径（[健康检查](https://docs.railway.com/deployments/healthchecks)） | 服务设置里的健康检查路径，或者 `render.yaml` 里的 `healthCheckPath`。任何 2xx 或 3xx 都算健康（[health checks](https://render.com/docs/health-checks)） | 带 `httpGet` 的 `startupProbe`、`readinessProbe` 和 `livenessProbe`（[探针](https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/)） |
| 停止时间 | `kill_timeout`，默认 5 秒，最多 300（[参考](https://docs.fly.io/reference/configuration/#the-kill_timeout-option)） | 这里不涉及 | 关闭延迟（shutdown delay），默认 30 秒，最多 300（[deploys](https://render.com/docs/deploys)） | `terminationGracePeriodSeconds` |
| 发布前 | `[deploy]` 里的 `release_command` | Pre-deploy command | Pre-deploy command | Job 或 Helm 钩子 |
| 卷 | `[mounts]` 里的 `source` 和 `destination`，用 `fly volumes create` 创建（[卷](https://docs.fly.io/volumes/overview/)） | 在服务里给卷设一个挂载路径（[卷](https://docs.railway.com/volumes)） | 带挂载路径的持久磁盘（[磁盘](https://render.com/docs/disks)） | `PersistentVolumeClaim`（见[数据放在哪里](kubernetes.zh-CN.md#where-the-data-lives)） |

Fly.io 默认用 SIGINT 停止 Machine，程序对它和 SIGTERM 一样处理。它 5 秒的 `kill_timeout` 比上面说的停止时间短，所以请设 `kill_timeout = 30`，或者调低 `SERVER_SHUTDOWN_TIMEOUT`。

## 不支持 {#not-supported}

为每个请求单独启动应用的平台，比如无服务器函数（Vercel functions、AWS Lambda）。Tifa 是一个长期运行的程序，在内存里保持一池预热好的 JavaScript 虚拟机和渲染记忆，而按请求启动、结束的函数两样都留不住。
