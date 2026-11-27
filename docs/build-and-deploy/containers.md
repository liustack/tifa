---
summary: 'Run a Tifa app as a container on Fly.io, Railway, Render, Kubernetes or a similar platform: the image, the port, the health check, migrations before a release, SQLite or Postgres, and what does not work'
audience: app
order: 24
read_when:
  - Putting a Tifa app in a container image, or deploying it with the tools of Fly.io, Railway, Render or another container platform
  - The platform says nothing listens on its port, or a deploy goes live on the wrong port
  - Choosing where to run the migrations of an app on such a platform, or the migration cannot find the SQLite file
  - Keeping the SQLite file of an app on a platform volume, or running more than one instance
  - Asking whether a serverless platform such as Vercel functions can run Tifa
---

# Run on a container platform

A Tifa app is one program, so its image holds that program and nothing else. This page is for platforms that run your image as a long-lived container and deploy it with their own tools: Fly.io, Railway, Render, Kubernetes and the like. [`tifa deploy`](deploy.md#deploy-with-tifa-deploy) is for your own Linux server and is not used on these platforms. For Kubernetes manifests, see [Deploy to Kubernetes](kubernetes.md).

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

- `pnpm install --ignore-scripts` skips the `prepare` script, as the CI that `tifa new` writes does: `tifa build` writes `.tifa/` itself. An app that uses npm runs `npm ci --ignore-scripts` and copies `package-lock.json` instead.
- `go tool tifa build` runs the Go and Node steps of [Build the program](build.md#tifa-build) and writes one file. Build the image on the system it runs on, or with `docker buildx --platform`. A build on an ARM Mac for an x86 platform without `--platform` makes an image that does not start.
- The program is static (`CGO_ENABLED=0`), so a distroless image is enough. `distroless/static` has the CA certificates the program needs for outgoing HTTPS, and a user `nonroot` with the number 65532. `FROM scratch` works if you copy `/etc/ssl/certs/ca-certificates.crt` from the build stage yourself.
- `HOST=0.0.0.0` makes the program listen on every interface, as the default of `127.0.0.1` cannot be reached from outside the container. See [The port](#the-port) for why this is not `SERVER_ADDR`.
- `/data` is the working folder, so a relative `db.path` such as the `app.db` of a new app lands there. Mount a volume on it when the app keeps files.
- When you hand the image to others, ship the license notices with it (see [Notices to ship with the program](build.md#notices-to-ship-with-the-program)).

## The port

The program reads `PORT` and `HOST` for `server.addr` when `SERVER_ADDR` and `--server-addr` are not set (see [PORT and HOST](config.md#port-and-host)):

- `PORT` alone listens on every interface: `PORT=10000` is `:10000`.
- `HOST` alone keeps the port of `server.addr` from the sources below it, `9527` by default. `HOST=0.0.0.0` listens on `0.0.0.0:9527`.
- With both, the address is `HOST:PORT`. Empty ones count as not set.

So the image above sets `HOST=0.0.0.0` and no address: a platform that sets `PORT` gets that port, and one that does not gets 9527. Do not set `SERVER_ADDR` in the image, since it wins over `PORT` and the platform's port would be ignored.

The platform ends HTTPS and sends plain HTTP to the container, so leave the program's own HTTPS off. Two things to check behind any proxy: `Host` and `Origin` must reach the program as the browser sent them, because Tifa's check of [cross-site writes](headers-and-origins.md#cross-site-writes) compares them, and the program does not read `X-Forwarded-For` (see [Behind a CDN or proxy](deploy-proxy.md#behind-a-cdn-or-proxy)).

## Health check and stop

- **Health check.** Add the `/healthz` route of [Health check](deploy-by-hand.md#health-check), which a new app does not have yet, and point the platform's check at it. The program listens only once `OnStart` is done, so a 204 means it can serve.
- **Stop.** On SIGTERM (and SIGINT) the program stops listening, gives requests in flight `server.shutdown_timeout` (10 seconds by default), cancels the ones left and waits as long again, then runs `OnShutdown` for as long once more. That is up to 30 seconds with the default. Give the platform a stop time at least as long, or lower `SERVER_SHUTDOWN_TIMEOUT`.
- **Logs.** The program writes its logs to stdout and stderr, which is where platforms collect them. Nothing is written to a log file.
- **Settings and secrets.** Settings are environment variables: the key with dots turned into underscores, in upper case (see [Names](config.md#names)). Put them and the secrets in the platform's variables, not in the image.

## Migrations

Run the migrations once, with the new image, before a release goes live: the `migrate` command of the app does it (`./app migrate`, or `args: [migrate]` on Kubernetes), and a failure stops the release while the old one keeps serving. `OnStart` runs the migrations as well, and finds nothing to do afterwards. Write each migration so that the release before it keeps working (see [Keep a rollback working](../guides/database.md#keep-a-rollback-working) and [At start or before the switch](../guides/database.md#at-start-or-before-the-switch)).

| Platform | Where to put `migrate` |
| --- | --- |
| Fly.io | `release_command = "/app migrate"` in the `[deploy]` section of `fly.toml`. It runs in a temporary Machine with the new image, once per deploy, and a non-zero exit stops the deploy ([Run one-off commands before releasing](https://docs.fly.io/reference/configuration/#run-one-off-commands-before-releasing-a-deployment)). |
| Railway | The pre-deploy command of the service, `/app migrate`. It runs between build and deploy, and a failure stops the deploy ([Pre-deploy command](https://docs.railway.com/deployments/pre-deploy-command)). |
| Render | The pre-deploy command of the service, `/app migrate`. It runs after the build and before the deploy, and a failure fails the deploy while the old one keeps running ([Deploys](https://render.com/docs/deploys)). |
| Kubernetes | A Job, or a Helm `pre-upgrade` hook (see [Migrations](kubernetes.md#migrations) and [Helm hooks](https://helm.sh/docs/topics/charts_hooks/)). |

These steps run in a separate container from the one that serves, and Fly.io, Railway and Render do not mount the volume in it. So with SQLite on a volume, leave the pre-release command out and let `OnStart` run the migrations: the SQLite file is only there once the app runs. A Postgres app can use the command on any platform.

## Where the data lives

- **SQLite** needs a persistent volume, mounted on the folder of the file (`/data` in the image above), and exactly one instance. Two instances would each write their own file, and the platforms agree: a Fly.io volume attaches to one Machine, a Railway service has one volume and cannot have replicas with it, and a Render service with a disk cannot be scaled out. A redeploy of a service with a volume stops the old instance first, so each deploy has a short pause. Turn on [`db.backup`](../guides/database.md#back-it-up) so that a lost volume does not lose the data.
- **Several instances** need a database server such as Postgres, and sessions kept in it (see [Where to store sessions](../pages-and-data/sessions.md#where-to-store-sessions) and [Postgres](../guides/database.md#postgres)). The render memo and a background loop started in `OnStart` live in each instance, so write the loop so that two instances can run it at once.
- **Railway volumes** are mounted as root. The image runs as user 65532, so set `RAILWAY_RUN_UID=0` on the service when the app cannot write to the volume ([Volumes](https://docs.railway.com/volumes)).

## Each platform

| | Fly.io | Railway | Render | Kubernetes |
| --- | --- | --- | --- | --- |
| Port | `internal_port` in `[http_service]` of `fly.toml`, 8080 unless you set it: set `9527` ([reference](https://docs.fly.io/reference/configuration/#the-http_service-section)) | Railway injects `PORT` ([health checks](https://docs.railway.com/deployments/healthchecks)) | `PORT` is 10000 and the service must bind `0.0.0.0` ([web services](https://render.com/docs/web-services)) | `containerPort` of the container and `targetPort` of the Service |
| Health check | `path` in `[[http_service.checks]]` ([reference](https://docs.fly.io/reference/configuration/#http_servicechecks)) | The health check path in the service settings ([health checks](https://docs.railway.com/deployments/healthchecks)) | The health check path in the settings of the service, or `healthCheckPath` in `render.yaml`. Any 2xx or 3xx counts ([health checks](https://render.com/docs/health-checks)) | `startupProbe`, `readinessProbe` and `livenessProbe` with `httpGet` ([probes](https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/)) |
| Stop time | `kill_timeout`, 5 seconds by default, up to 300 ([reference](https://docs.fly.io/reference/configuration/#the-kill_timeout-option)) | Not covered here | The shutdown delay, 30 seconds by default, up to 300 ([deploys](https://render.com/docs/deploys)) | `terminationGracePeriodSeconds` |
| Before release | `release_command` in `[deploy]` | Pre-deploy command | Pre-deploy command | Job or Helm hook |
| Volume | `[mounts]` with `source` and `destination`, made with `fly volumes create` ([volumes](https://docs.fly.io/volumes/overview/)) | A volume with a mount path in the service ([volumes](https://docs.railway.com/volumes)) | A persistent disk with a mount path ([disks](https://render.com/docs/disks)) | A `PersistentVolumeClaim` (see [Where the data lives](kubernetes.md#where-the-data-lives)) |

Fly.io sends SIGINT to stop a Machine by default, which the program handles like SIGTERM. Its 5 seconds of `kill_timeout` is shorter than the stop above, so set `kill_timeout = 30` or lower `SERVER_SHUTDOWN_TIMEOUT`.

## Not supported

Platforms that start the app for each request, such as serverless functions (Vercel functions, AWS Lambda). Tifa is a long-running program that keeps a pool of warm JavaScript VMs and a render memo in memory, and a function that starts and stops per request has neither.
