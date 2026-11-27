---
summary: '在 Kubernetes 上运行 Tifa 应用：带探针和停止时间的 Deployment、Service 和 Ingress、用 Job 或在启动时做迁移、SQLite 或 Postgres 的存储'
audience: app
order: 21
translatedFrom: 9256978d7a61
read_when:
  - '把 Tifa 应用打进容器镜像，或者在 Kubernetes、k3s、托管集群上运行它'
  - '在 `tifa deploy` 和 Kubernetes 之间做选择'
  - '给 Tifa 的 pod 写探针、停止时间或设置'
  - '给跑在多个 pod 里的应用做迁移'
  - 'Tifa 应用滚动更新时出现错误，或者 pod 在启动或停止时被杀掉'
---

# 部署到 Kubernetes {#deploy-to-kubernetes}

Tifa 应用是一个静态程序，所以它的镜像里只有这个程序。这一篇给出围着[容器平台那一篇](containers.zh-CN.md)里镜像的清单，以及属于 Tifa 的几个选择：探针、停止时间、迁移和数据放在哪里。

## 什么时候用它 {#when-to-use-it}

一个应用放在一台服务器上，[`tifa deploy`](deploy.zh-CN.md#deploy-with-tifa-deploy) 更省事，也不需要容器。你的团队已经在运行集群、需要几份应用同时提供服务，或者平台规定一切都以镜像发布时，才用 Kubernetes。Tifa 本身不需要它。

## 镜像 {#the-image}

Dockerfile 和 `.dockerignore` 在容器平台那一篇的[镜像](containers.zh-CN.md#the-image)里。镜像监听所有网卡（`HOST=0.0.0.0`），端口是 9527，除非 `PORT` 另有指定，所以下面的 `containerPort: 9527` 与它一致。`/data` 是镜像的工作文件夹：应用要保存文件时，在它上面挂一个卷。

## Deployment {#the-deployment}

设置就是环境变量：键里的点换成下划线，再转成大写，所以 `server.shutdown_timeout` 是 `SERVER_SHUTDOWN_TIMEOUT`，你应用的选项 `db.url` 是 `DB_URL`（见[命名](config.zh-CN.md#names)）。普通的值写在清单里，密钥放进 `Secret`。

应用需要一个 `/healthz` 路由，新应用还没有：加上[健康检查](deploy-by-hand.zh-CN.md#health-check)里的那个。程序要等 `OnStart` 完成才监听，所以收到 204 就表示它能提供服务。

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: notes
spec:
  replicas: 2
  selector:
    matchLabels: {app: notes}
  strategy:
    type: RollingUpdate
    rollingUpdate: {maxUnavailable: 0, maxSurge: 1}
  template:
    metadata:
      labels: {app: notes}
    spec:
      # 一次停止最多是 server.shutdown_timeout 的 3 倍，再加上下面 preStop 的停顿。
      terminationGracePeriodSeconds: 45
      # 见配置页的“命名”一节。
      enableServiceLinks: false
      securityContext:
        runAsNonRoot: true
        runAsUser: 65532
        fsGroup: 65532
      containers:
        - name: notes
          image: registry.example.com/notes:1.4.0
          ports:
            - {name: http, containerPort: 9527}
          env:
            - {name: SERVER_SHUTDOWN_TIMEOUT, value: 10s}
            - {name: SSR_VMS, value: "2"}
            - name: DB_URL
              valueFrom:
                secretKeyRef: {name: notes, key: db-url}
          resources:
            requests: {cpu: "2", memory: 512Mi}
            limits: {memory: 512Mi}
          startupProbe:
            httpGet: {path: /healthz, port: http}
            periodSeconds: 2
            failureThreshold: 30
          readinessProbe:
            httpGet: {path: /healthz, port: http}
            periodSeconds: 5
          livenessProbe:
            httpGet: {path: /healthz, port: http}
            periodSeconds: 10
            failureThreshold: 6
          lifecycle:
            preStop:
              sleep: {seconds: 5}
```

- **探针。** 三个探针问的是同一个路径。启动探针给程序 60 秒完成 `OnStart`，在启动时跑的迁移也算在内。存活探针要等启动探针通过后才开始。它的 `failureThreshold` 要设得高：数据库挂掉时 `/healthz` 回答 503，而重启所有 pod 并不能让数据库恢复。就绪探针失败时，pod 会离开 Service，但继续运行。
- **停止时间。** 收到 SIGTERM 后，程序停止监听，给正在处理的请求 `server.shutdown_timeout` 的时间，取消剩下的请求并再等同样长的时间，然后再用同样长的时间运行 `OnShutdown`（见[运行它](deploy-by-hand.zh-CN.md#run-it)）。这最多是该设置的 3 倍：默认的 10 秒就是 30 秒。`terminationGracePeriodSeconds` 必须比这个时间加上 `preStop` 的停顿更长，否则 kubelet 会在中途发 SIGKILL。`preStop` 的停顿（Kubernetes 1.30 及以上）让 pod 在 Service 里再留 5 秒，这样仍在给它发请求的代理，在程序停止监听时已经把它移除了。
- **CPU 和内存。** `ssr.vms` 跟随 CPU 限制，最多 4 个 VM。请按你给 pod 的 CPU 数设置它，内存按[内存](../reference/performance.zh-CN.md#memory)来定：进程约 50 MiB，每个 VM 最多 40 MiB，再加上渲染记忆的 `ssr.memo_size`（64 MiB）。示例设置了内存限制，没有设 CPU 限制，所以 pod 不会被限速。
- **多个 pod。** 每个 pod 在内存里有自己的渲染记忆，放在默认内存存储里的会话不会在 pod 之间共享。新应用的存储就是数据库，所有 pod 共用它。
- 应用在 `OnStart` 里启动的循环（见[后台任务](../guides/jobs.zh-CN.md)）会在每个 pod 里运行。请把它写成两个 pod 同时运行也没问题。

## Service 和 Ingress {#a-service-and-an-ingress}

```yaml
apiVersion: v1
kind: Service
metadata:
  name: notes
spec:
  selector: {app: notes}
  ports:
    - {name: http, port: 80, targetPort: http}
---
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: notes
  annotations:
    # ingress-nginx 默认拒绝超过 1m 的请求体。请和 server.body_limit 保持一致。
    nginx.ingress.kubernetes.io/proxy-body-size: 4m
    cert-manager.io/cluster-issuer: letsencrypt
spec:
  ingressClassName: nginx
  tls:
    - hosts: [notes.example.com]
      secretName: notes-tls
  rules:
    - host: notes.example.com
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service: {name: notes, port: {name: http}}
```

Ingress 负责结束 TLS，所以程序自己的 HTTPS 保持关闭。Ingress 的类和注解属于你的控制器：上面的是给 ingress-nginx 和 cert-manager 用的。在任何代理后面都要检查两件事：`Host` 和 `Origin` 必须原样到达程序，因为 Tifa 的[跨站写入](headers-and-origins.zh-CN.md#cross-site-writes)检查会比较它们，而且程序不读 `X-Forwarded-For`（见[在 CDN 或代理后面](deploy-proxy.zh-CN.md#behind-a-cdn-or-proxy)）。

## 迁移 {#migrations}

滚动更新会在旧 pod 还在服务时启动新 pod，所以有一段时间，旧版本跑在新版本建好的表上。写每个迁移时，要让它之前的那个版本继续能用，如[让回滚继续可用](../guides/database.zh-CN.md#keep-a-rollback-working)所说：加一列，让旧代码忽略它，到后面的版本再删。同一条规则也让回滚保持安全。

有两种方式来运行它们。

**在启动时，加锁。** 每个 pod 在 `OnStart` 里运行迁移。在 Postgres 上，给 goose 它的会话锁，这样一个 pod 去迁移，其他的等着（见 [Postgres](../guides/database.zh-CN.md#postgres)）。等待中的 pod 不会就绪，所以启动探针的 `failureThreshold` 乘以 `periodSeconds` 要比最长的迁移更长。它不需要额外的对象，也是 SQLite 能用的那一种。

**在发布之前只运行一次。** 应用的 `migrate` 命令用新镜像作为 Job 运行它们，Job 完成后再更新 Deployment。镜像还是同一个，命令作为参数：

```yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: notes-migrate-1-4-0
spec:
  backoffLimit: 0
  ttlSecondsAfterFinished: 86400
  template:
    spec:
      restartPolicy: Never
      containers:
        - name: migrate
          image: registry.example.com/notes:1.4.0
          args: [migrate]
          env:
            - name: DB_URL
              valueFrom:
                secretKeyRef: {name: notes, key: db-url}
```

```sh
kubectl apply -f migrate-job.yaml
kubectl wait --for=condition=complete --timeout=10m job/notes-migrate-1-4-0
kubectl set image deployment/notes notes=registry.example.com/notes:1.4.0
```

Job 创建之后就不能改，所以请给它起一个带版本的名字。用 Helm 时，把同一个 Job 做成 `pre-upgrade` 钩子（`helm.sh/hook: pre-install,pre-upgrade`），Helm 会等它完成才动 Deployment。Job 失败时什么都不会发布，旧 pod 继续提供服务。新应用的 `migrate` 命令打开的是 SQLite 文件。用 Postgres 时，按 [Postgres](../guides/database.zh-CN.md#postgres) 改它，它仍然是同一个命令（见[应用的命令](../guides/commands.zh-CN.md)）。

请让 `OnStart` 也继续运行迁移：Job 之后它没有事可做，而手动启动的 pod 或用旧清单启动的 pod 仍然能拿到它们的表。

## 数据放在哪里 {#where-the-data-lives}

哪种做法行得通取决于数据库，选择权在你。

**Postgres。** pod 不保存状态，所以任意数量的副本都行，用上面的 Deployment。处理函数写到文件夹里的上传文件，需要一个所有 pod 都能写入挂载的卷（`ReadWriteMany`），或者用对象存储（见[上传](../guides/uploads.zh-CN.md)）。

**SQLite。** 文件属于一个 pod。用一个副本，把 `PersistentVolumeClaim` 挂在 `/data`，策略用 `Recreate`：先停旧 pod，再启动新的。滚动更新会让两个 pod 用同一个 SQLite 文件，这不是 SQLite 设计的用法，而且 `ReadWriteOnce` 的卷往往也没法被两个 pod 同时挂载。代价是每次更新都有一小段停顿。新 pod 启动需要多久，访客就会从 Service 那里收到多久的错误。

```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: notes-data
spec:
  accessModes: [ReadWriteOnce]
  resources:
    requests: {storage: 5Gi}
```

在 Deployment 里，把 `replicas` 改成 1，把策略改成 `type: Recreate`（不带 `rollingUpdate`），再加上卷：

```yaml
      containers:
        - name: notes
          volumeMounts:
            - {name: data, mountPath: /data}
      volumes:
        - name: data
          persistentVolumeClaim: {claimName: notes-data}
```

pod 的 `securityContext` 里的 `fsGroup: 65532` 让程序能写这个卷。请在启动时运行迁移，因为 pod 占着卷的时候，Job 打不开这个文件。请设置 [`db.backup`](../guides/database.zh-CN.md#back-it-up)，这样卷丢了数据也不会丢：应用把每次提交复制到 S3 存储，文件不在时从那里恢复。

## 检查 {#check-it}

- `kubectl rollout status deployment/notes` 会等到更新完成。pod 一再重启、启动探针不断失败，说明 `OnStart` 没有结束：`kubectl logs` 里有错误。
- distroless 镜像里没有 shell，`kubectl exec` 用不了。像上面那样把应用的命令作为 Job 运行，或者用 `kubectl debug` 打开一个 shell。
- `kubectl port-forward deployment/notes 9527` 加 `curl -i localhost:9527/healthz`，直接问程序。
