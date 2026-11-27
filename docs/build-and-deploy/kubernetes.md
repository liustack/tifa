---
summary: 'Run a Tifa app on Kubernetes: a Dockerfile, a Deployment with probes and a stop time, a Service and an Ingress, migrations as a Job or at start, and storage for SQLite or Postgres'
audience: app
order: 21
read_when:
  - Putting a Tifa app in a container image, or running it on Kubernetes, k3s or a managed cluster
  - Choosing between `tifa deploy` and Kubernetes
  - Writing the probes, the stop time or the settings of a Tifa pod
  - Running the migrations of an app that runs in several pods
  - A rolling update of a Tifa app shows errors, or a pod is killed while it starts or stops
---

# Deploy to Kubernetes

A Tifa app is one static program, so its image holds that program and nothing else. This page has the Dockerfile, the manifests around it and the choices that are Tifa's: the probe, the stop time, the migrations and where the data lives.

## When to use it

[`tifa deploy`](deploy.md#deploy-with-tifa-deploy) is the shorter way for one app on one server, and it needs no container. Use Kubernetes when your team already runs a cluster, when several copies of the app must serve at once, or when a platform rule says everything ships as an image. Nothing in Tifa needs it.

## The image

The build stage needs Go, and Node with pnpm because the front end is bundled with Vite. The final image holds only the program: it has no Node, because the program never needs it.

```dockerfile
# Dockerfile
FROM golang:1.27 AS go

FROM node:24-bookworm AS build
COPY --from=go /usr/local/go /usr/local/go
ENV PATH=/usr/local/go/bin:$PATH CGO_ENABLED=0
RUN npm install -g pnpm@10
WORKDIR /src
# The files that name the dependencies come first, so a change to the code reuses these layers.
COPY go.mod go.sum ./
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
RUN go mod download && pnpm install --frozen-lockfile --ignore-scripts
COPY . .
RUN go tool tifa build -o /out/app && mkdir /data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/app /app
COPY --from=build --chown=65532:65532 /data /data
WORKDIR /data
ENV SERVER_ADDR=0.0.0.0:9527
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

- `pnpm install --ignore-scripts` skips the `prepare` script, as the CI that `tifa new` writes does: `tifa build` writes `.tifa/` itself. An app that uses npm runs `npm ci --ignore-scripts` and copies `package-lock.json` instead.
- `go tool tifa build` runs the Go and Node steps of [Build the program](build.md#tifa-build) and writes one file. Build the image on the system it runs on, or with `docker buildx --platform`. A build on an ARM Mac for an x86 cluster without `--platform` makes an image that does not start.
- The program is static (`CGO_ENABLED=0`), so a distroless image is enough. `distroless/static` has the CA certificates the program needs for outgoing HTTPS, and a user `nonroot` with the number 65532. `FROM scratch` works if you copy `/etc/ssl/certs/ca-certificates.crt` from the build stage yourself.
- `SERVER_ADDR=0.0.0.0:9527` makes the program listen on every interface, as the default of `127.0.0.1` cannot be reached from outside the pod.
- `/data` is the working folder, so a relative `db.path` such as the `app.db` of a new app lands there. Mount a volume on it when the app keeps files.
- When you hand the image to others, ship the license notices with it (see [Notices to ship with the program](build.md#notices-to-ship-with-the-program)).

## The Deployment

Settings are environment variables: the key with dots turned into underscores, in upper case, so `server.shutdown_timeout` is `SERVER_SHUTDOWN_TIMEOUT` and the option `db.url` of your app is `DB_URL` (see [Names](config.md#names)). Put plain values in the manifest and secrets in a `Secret`.

The app needs a `/healthz` route, which a new app does not have yet: add the one in [Health check](deploy-by-hand.md#health-check). The program listens only once `OnStart` is done, so a 204 means it can serve.

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
      # A stop takes up to 3 times server.shutdown_timeout, plus the preStop pause below.
      terminationGracePeriodSeconds: 45
      # See "Names" in the configuration page.
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

- **Probes.** The three probes ask the same path. The startup probe gives the program 60 seconds to finish `OnStart`, which includes migrations that run at start. The liveness probe only starts once the startup probe passed. Keep its `failureThreshold` high: `/healthz` answers 503 while the database is down, and restarting every pod does not bring the database back. When the readiness probe fails, the pod leaves the Service and keeps running.
- **Stop time.** On SIGTERM the program stops listening, gives requests in flight `server.shutdown_timeout`, cancels the ones left and waits as long again, then runs `OnShutdown` for as long once more (see [Run it](deploy-by-hand.md#run-it)). That is up to 3 times the setting: 30 seconds with the default of 10. `terminationGracePeriodSeconds` must be longer than that plus the `preStop` pause, or the kubelet sends SIGKILL in the middle of it. The `preStop` pause (Kubernetes 1.30 and later) keeps the pod in the Service for 5 more seconds, so that the proxies that still send it requests have dropped it by the time the program stops listening.
- **CPU and memory.** `ssr.vms` follows the CPU limit, up to 4 VMs. Set it with the number of CPUs you give the pod, and size the memory by [Memory](../reference/performance.md#memory): about 50 MiB for the process, up to 40 MiB for each VM and `ssr.memo_size` (64 MiB) for the render memo. The example sets a memory limit and no CPU limit, so the pod is not throttled.
- **Several pods.** Each pod has its own render memo in memory, and sessions kept in the default in-memory store would not be shared. The store of a new app is the database, which every pod shares.
- A loop that the app starts in `OnStart` (see [Background work](../guides/jobs.md)) runs in each pod. Write it so that two pods can run it at the same time.

## A Service and an Ingress

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
    # ingress-nginx refuses bodies over 1m by default. Match server.body_limit.
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

The Ingress ends TLS, so leave the program's own HTTPS off. The class and the annotations belong to your controller: the ones above are for ingress-nginx and cert-manager. Two things to check behind any proxy: `Host` and `Origin` must reach the program as the browser sent them, because Tifa's check of [cross-site writes](headers-and-origins.md#cross-site-writes) compares them, and the program does not read `X-Forwarded-For` (see [Behind a CDN or proxy](deploy-proxy.md#behind-a-cdn-or-proxy)).

## Migrations

A rolling update starts new pods while old ones still serve, so for a while the old release runs on the tables the new one made. Write each migration so that the release before it keeps working, as [Keep a rollback working](../guides/database.md#keep-a-rollback-working) says: add a column and let the old code ignore it, and drop it only in a later release. The same rule keeps a rollback safe.

There are two ways to run them.

**At start, with a lock.** Every pod runs the migrations in `OnStart`. On Postgres, give goose its session lock so that one pod migrates and the others wait (see [Postgres](../guides/database.md#postgres)). A waiting pod is not ready, so the startup probe needs `failureThreshold` times `periodSeconds` longer than the longest migration. This needs no extra object, and it is the one that works for SQLite.

**Once, before the rollout.** The `migrate` command of the app runs them with the new image, as a Job, and the Deployment is updated when the Job is complete. The image is the same, and the command is the argument:

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

A Job is not changed once it exists, so give it a name for the release. With Helm, make the same Job a `pre-upgrade` hook (`helm.sh/hook: pre-install,pre-upgrade`) and Helm waits for it before it touches the Deployment. When the Job fails, nothing rolls out and the old pods keep serving. The `migrate` command of a new app opens the SQLite file. For Postgres, change it as in [Postgres](../guides/database.md#postgres), and it stays the same command (see [Commands of the app](../guides/commands.md)).

Keep `OnStart` running the migrations as well: it finds nothing to do after the Job, and a pod started by hand or by an old manifest still gets its tables.

## Where the data lives

What works depends on the database, and the choice is yours.

**Postgres.** Pods hold no state, so any number of replicas works, with the Deployment above. Uploads that handlers write to a folder need a volume that all pods mount for writing (`ReadWriteMany`), or object storage (see [Uploads](../guides/uploads.md)).

**SQLite.** The file belongs to one pod. Use one replica, a `PersistentVolumeClaim` on `/data`, and the `Recreate` strategy, which stops the old pod before it starts the new one. A rolling update would run two pods on one SQLite file, which is not what SQLite is made for, and a `ReadWriteOnce` volume often cannot be mounted by both anyway. The price is a short pause on each update. Visitors get errors from the Service for as long as the new pod needs to start.

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

In the Deployment, change `replicas` to 1, the strategy to `type: Recreate` (without `rollingUpdate`), and add the volume:

```yaml
      containers:
        - name: notes
          volumeMounts:
            - {name: data, mountPath: /data}
      volumes:
        - name: data
          persistentVolumeClaim: {claimName: notes-data}
```

`fsGroup: 65532` in the pod's `securityContext` makes the volume writable for the program. Run the migrations at start, since a Job cannot open the file while the pod holds the volume. Set [`db.backup`](../guides/database.md#back-it-up) so that a lost volume does not lose the data: the app copies each commit to an S3 store and restores from it when the file is missing.

## Check it

- `kubectl rollout status deployment/notes` waits for the update. A pod that restarts again and again with the startup probe failing has an `OnStart` that does not finish: `kubectl logs` shows the error.
- `kubectl exec` has no shell in a distroless image. Run a command of the app as a Job, as above, or open a shell with `kubectl debug`.
- `kubectl port-forward deployment/notes 9527` and `curl -i localhost:9527/healthz` ask the program directly.
