---
summary: '把 Tifa 应用放到服务器上：`tifa deploy`、tifa.yaml 的 `deploy` 块、指定服务器、部署、回滚、更多开发者'
audience: app
order: 10
translatedFrom: 26f4d7e137bf
read_when:
  - '第一次把 Tifa 应用放到 VPS 或容器里'
  - '运行 tifa deploy、tifa deploy setup 或 tifa deploy rollback，或者某次部署没通过健康检查而回退了'
  - '用 TIFA_DEPLOY_HOST 给 tifa deploy 指定服务器，或者在没有指定时想知道它用的是哪台服务器（~/.tifa/config.yaml、TIFA_HOME），或者在 tifa.yaml 里选择 deploy.front、deploy.connect 或 deploy.auto'
  - '给慢启动更多时间（deploy.start_timeout），或者在部署前跑测试、部署后发一条消息'
  - '让第二台开发机有权部署'
---

# 用 tifa deploy 部署 {#deploy-with-tifa-deploy}

Tifa 应用就是一个可执行文件。服务器需要这个文件、环境里的设置，以及一个可以写数据的文件夹。它不需要 Node、进程管理器和 Redis。默认的会话存储放在内存里，所以如果要运行多份程序，请给它[你自己的存储](../pages-and-data/sessions.zh-CN.md#where-to-store-sessions)。

`tifa deploy` 把 Tifa 应用放到你自己的服务器上，一条命令就设置好从 push 到上线的整条 CI/CD 路径。

`go tool tifa deploy` 通过 ssh 把应用放到带 systemd 的 Linux 服务器上：它在你的机器上为服务器构建，上传程序，重启应用而不拒绝任何请求，在服务器上检查它，检查失败时回到之前的发布。应用自己接待访客并用自己的 HTTPS，或者放在 Cloudflare Tunnel 或 Tifa 安装的 nginx 后面，这时多个应用可以共用一台服务器。服务器不需要 Docker、Node，也不需要自己的部署程序。

`tifa.yaml` 写明你要什么。`go tool tifa deploy setup` 让服务器、Cloudflare 和 GitHub 与它一致。`tifa.yaml` 是要提交的，所以密钥和服务器的位置都不放进去。

```yaml
site:
  url: https://shop.example.com   # the domain is the host of this URL
deploy:
  front: none      # what visitors come through before the server: none (the app serves them itself on ports 80 and 443) | cloudflare | nginx
  connect: direct  # how deploys reach the server over ssh: direct | cloudflare | tailscale
  start_timeout: 30s  # how long a new release may take to start and answer /healthz: 1s to 10m
  auto:            # deploy by itself from GitHub Actions. No block: you deploy by hand only
    branch: main   # the branch that deploys. Left out: the default branch of the repository on GitHub
    needs: ci.yml  # the workflow file in .github/workflows that must pass first. Left out: ci.yml. none: do not wait
```

- 服务器在哪里不放进 `tifa.yaml`，因为公开的仓库会把它显示出来。在你的机器上指定它，先匹配到的优先：
  1. 参数 `--host`
  2. 环境变量 `TIFA_DEPLOY_HOST`
  3. 应用根目录里的 `.env` 文件中的 `TIFA_DEPLOY_HOST`，git 会忽略这个文件（见 [.env](config.zh-CN.md#env)。程序自己不理会这个名字）
  4. `tifa deploy setup` 和 `tifa deploy` 记在 `~/.tifa/config.yaml` 里的服务器（见[Tifa 记住的服务器](#the-servers-tifa-remembers)）

  ```sh
  # .env
  TIFA_DEPLOY_HOST=ssh://203.0.113.7:2222
  ```

  这个值就是 `ssh` 接受的目标：可以是 `~/.ssh/config` 里的一个 Host（比如别名 `vps`，这样地址、端口、密钥和 `ProxyJump` 都由 `~/.ssh/config` 提供）、一个主机名，或者一个 `ssh://host[:port]` 的 URL。用户由 Tifa 自己选，改动系统的步骤用 root，其余用 `tifa`，所以除非 root 登录不了，否则不要写用户（见[root 登录不了时](#when-root-cannot-log-in)）。带路径、查询或别的协议的值会被拒绝。四种都没设置时，每个 `tifa deploy` 命令都会停下并说明。
- `front` 是访客到达服务器之前经过的东西。选 `cloudflare` 时，就是 `site.url` 的主机上的一个 Cloudflare Tunnel，带它的 DNS 记录和规则，`deploy` 和 `rollback` 会清除那个主机名的 CDN 缓存。必须设置 `site.url`，并且不带端口。选 `none` 时，应用自己在 80 端口上接待访客，`site.url` 里有域名时还在 443 端口上用自己的 HTTPS，也不清缓存（见[应用自己接待访客](deploy-setup.zh-CN.md#the-app-serves-visitors-itself)）。选 `nginx` 时，setup 安装的一个 nginx 按 `site.url` 的主机把访客送到应用，并用 certbot 为它拿证书，这样几个应用不用 Cloudflare 也能共用一台服务器（见[在 nginx 后面](deploy-proxy.zh-CN.md#behind-nginx)）。
- `connect` 是 ssh 连到服务器走的那一条路，从你的机器和从 GitHub Actions 的 runner 都一样。选 `direct` 时，ssh 用你给的地址。选 `cloudflare` 时，两边都经由 Cloudflare Tunnel，所以服务器不用开放 ssh 端口（见 [`connect: cloudflare`](deploy-cloudflare.zh-CN.md#connect-cloudflare)）。它需要 `front: cloudflare`，因为 ssh 的主机名是由域名生成的，有没有 `auto` 都行。选 `tailscale` 时，runner 加入你的 tailnet，它需要 `auto` 这个块（见 [`connect: tailscale`](deploy-github-actions.zh-CN.md#connect-tailscale)）。你自己的机器照常连到服务器，用 `TIFA_DEPLOY_HOST` 里的地址，tailnet 和服务器都由你自己设置。
- `start_timeout` 是新发布启动并通过健康检查最多可以花多长时间，写成 Go 的时长，比如 `45s` 或 `2m`。它是整秒数，从 `1s` 到 `10m`，不写就是 `30s`。部署失败后回退到的那个发布的检查，以及 `deploy rollback` 启动的发布的检查，用同样的时间。见[慢启动](#a-slow-start)。
- `auto` 只要写上就会开启从 GitHub Actions 的部署。没有这个块，就只能手动部署。`auto: {}` 用默认值，写成后面什么都没有的 `auto:` 是错误。它只有两个键，没有别的：
  - `branch` 是会部署的分支：往它 push，并且 `needs` 里的工作流通过之后就部署。不写时，它是 GitHub 上仓库的默认分支。
  - `needs` 是 `.github/workflows` 里必须先通过的那个文件，写成文件名，比如 `ci.yml`（不带文件夹）。不写时，它是 `tifa new` 写出的 `ci.yml`。写 `none` 就什么也不等：每次往这个分支 push 都会部署。见[从 GitHub Actions 部署](deploy-github-actions.zh-CN.md#deploy-from-github-actions)。
- 只支持 GitHub。

**输入。** 属于你而不属于仓库的值，每次都用同样的方式读取：输入有参数就先用参数，否则用环境变量，再否则用应用根目录里 `.env` 文件中的那一行。Tifa 会读这个文件。setup 从不往里写令牌，也从不打印令牌。它在那里写的唯一一行是 `TIFA_DEPLOY_HOST`：经由 tunnel 的 ssh 通了之后，`connect: cloudflare` 会把它改成 ssh 主机名（见 [`connect: cloudflare`](deploy-cloudflare.zh-CN.md#connect-cloudflare)），而且只在这个值本来就在 `.env` 里时才改。

| 输入 | 参数 | 它是什么 | 来自 |
| --- | --- | --- | --- |
| `TIFA_DEPLOY_HOST` | `--host` | 服务器在哪里 | 参数、环境变量、`.env`，然后是 `~/.tifa` |
| `TIFA_DEPLOY_CF_PURGE_TOKEN` | 无 | 只能清除一个 zone 的缓存的 Cloudflare 令牌。它优先于 setup 存在服务器上的那个（见[清除 CDN 缓存](deploy-cloudflare.zh-CN.md#clearing-the-cdn-cache)） | 环境变量、`.env` |
| `TIFA_DEPLOY_CF_ZONE_ID` | 无 | 那个 zone 的 ID，和上面的令牌一起需要 | 环境变量、`.env` |
| `TIFA_DEPLOY_GITHUB_TOKEN` | 无 | 一个细粒度的 GitHub 令牌，在没有 gh 时用于 setup 和 remove | 环境变量、`.env` |

`.env` 里有 tifa deploy 不读的 `TIFA_DEPLOY_` 名字时，命令同样会停下，错误里写出最接近的那个名字。

你需要：一台带 systemd 的 Linux 服务器，x86_64 或 aarch64，你能用 `ssh root@<TIFA_DEPLOY_HOST>` 连上它一次，或者能以一个有免密码 sudo 的用户连上它（在 Ubuntu 24.04 上测试过）。在你的机器上，需要 `tifa build` 需要的东西（Go，带页面的应用还要 Node）和 `ssh`。对于 `front: cloudflare` 和 `connect: cloudflare`，需要 [cf](https://developers.cloudflare.com/cf/get-started/)，即 Cloudflare CLI（测试版），并且已安装（`pnpm add --global cf`）、已登录（`cf auth login`）。setup 和 remove 能分清这两种情况：PATH 里没有 cf 时，它们说要先安装，装了 cf 就请你登录。对于 `connect: cloudflare`，你的机器上还需要 [cloudflared](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/downloads/)：setup 在 macOS 上用 `brew install cloudflared` 安装它，在 Windows 上用 `winget install --id Cloudflare.cloudflared -e`，在 Linux 上，或者没有 brew 和 winget 时，它会告诉你去哪里下载。对于 `front: nginx`，需要一台带 apt 的服务器（Debian 或 Ubuntu），setup 以 root 用 apt 在上面安装 nginx 和 certbot。对于 `auto`，需要已登录的 [gh](https://cli.github.com/)（`gh auth login`），没有 gh 的话，需要把细粒度令牌放在 `TIFA_DEPLOY_GITHUB_TOKEN` 里（见[从 GitHub Actions 部署](deploy-github-actions.zh-CN.md#deploy-from-github-actions)）。对于 `connect: tailscale`，需要一台已在你的 tailnet 里的服务器（装了 Tailscale 并登录），以及一个你在 Tailscale 控制台里创建的 Tailscale 凭证（见 [`connect: tailscale`](deploy-github-actions.zh-CN.md#connect-tailscale)）。你的机器上不需要任何 Tailscale 的东西。在终端里，setup 会替你安装并登录 cf 和 gh（见[按方案设置](deploy-setup.zh-CN.md#set-up-with-a-plan)）。

第一次要先运行 `go tool tifa deploy setup`，见[配好服务器](deploy-setup.zh-CN.md)。之后只需要部署，某个版本有问题时再回滚。

没有终端时（比如 agent 或脚本运行命令），ssh 什么都不问：服务器的主机密钥还不在你的 `known_hosts` 里时，命令会停下，报错里给出接受这个密钥的命令。ssh 直接连到的服务器有 15 秒的应答时间，除非你的 ssh 配置自己设了 `ConnectTimeout`。经过 `ProxyCommand` 或 `ProxyJump` 连到的服务器（比如在浏览器里登录 Cloudflare Access）不限时间，等多久都行。

## root 登录不了时 {#when-root-cannot-log-in}

很多云镜像，比如 AWS、Google Cloud 和 Azure 的，不让任何人以 root 登录。这时把你登录用的用户写在主机前面：`ubuntu@203.0.113.7`，或者 `ssh://ubuntu@203.0.113.7:2222`。这个用户需要免密码 sudo。需要 root 的步骤（第一次设置、端口、ufw、nginx、隧道），Tifa 以这个用户登录，再用 `sudo -n` 运行。部署、日志和其他步骤仍然以 `tifa` 登录。没有免密码 sudo 时，setup 在第一次查看服务器时就停下，那时服务器上还什么都没改，并告诉你要加什么。不写用户，或者写 `root@`，Tifa 就以 root 登录。

setup 把这个用户 `~/.ssh/authorized_keys` 里的密钥交给 `tifa`，所以你登录用的密钥也能用来部署。用户跟着主机走：`~/.tifa/config.yaml` 会记住它，[`connect: cloudflare`](deploy-cloudflare.zh-CN.md#connect-cloudflare) 把 `TIFA_DEPLOY_HOST` 改成 ssh 主机名时也会保留它。

如果服务商给了你一个密钥文件，比如 AWS 密钥对的 `.pem`，就把它写进 `~/.ssh/config`，因为 Tifa 运行的是系统的 `ssh`：

```
Host myserver
    HostName 203.0.113.7
    IdentityFile ~/.ssh/aws-key.pem
```

然后在 `.env` 里设置 `TIFA_DEPLOY_HOST=ubuntu@myserver`，或者传 `--host ubuntu@myserver`。在 AWS 上，Ubuntu 的用户是 `ubuntu`，Amazon Linux 的是 `ec2-user`。别人能读的密钥文件 ssh 会拒绝使用，所以先运行 `chmod 600 ~/.ssh/aws-key.pem`。

## 部署 {#deploy}

```sh
go tool tifa deploy
```

1. 向服务器询问它的系统，并在你的机器上为它构建应用，做法和[为另一个系统构建](build.zh-CN.md#building-for-another-system)一样。服务器上什么也不构建。在 setup 已经准备好、但还没有这个应用的文件夹的服务器上，它先以 `tifa` 身份（不需要 root）创建文件夹、端口和单元。自己接待访客的应用，如果 `tifa` 还不能打开 80 端口，要先在那里运行 setup，部署会这样提示。在没有准备好的服务器上，它会停下，并说明要运行 `go tool tifa deploy setup`。
2. 把程序压缩成 gzip，上传到 `/srv/tifa/<app>/releases/<id>/`，其中 `<id>` 是部署时的 UTC 时间，并在那里检查它的 SHA-256。
3. 把 `/srv/tifa/<app>/current` 指向它，并重启服务。socket 保持打开，所以这期间到来的请求会等着，然后得到回答（见[重启而不丢请求](deploy-by-hand.zh-CN.md#restart-without-dropping-requests)）。
4. 在服务器上检查新程序：每秒一次 `GET /healthz`，最长 `start_timeout`（不设置就是 30 秒），直到它以低于 500 的状态回答。它在回环地址上访问，有自己 HTTPS 的应用则像访客一样，用它的域名走 HTTPS。没有 `/healthz` 路由的应用，一有响应就带着它的 404 通过。写一个会看数据库的[健康检查](deploy-by-hand.zh-CN.md#health-check)，让这项检查更有意义。`OnStart`（包括迁移）必须在这段时间内完成（见[慢启动](#a-slow-start)和[迁移能花多长时间](../guides/database.zh-CN.md#how-long-a-migration-may-take)）。
5. 检查通过后，把这次部署记下来，并保留最近 5 个发布。检查失败时，它会打印应用日志的最后几行，重新启动之前运行的那个发布，并以 1 退出。如果从来没有通过的发布，它会停掉应用。
6. 用 `front: cloudflare` 时，会清除那个主机名的 CDN 缓存（见[清除 CDN 缓存](deploy-cloudflare.zh-CN.md#clearing-the-cdn-cache)）。
7. 说明访客从哪里访问应用：`visitors: https://shop.example.com`，没有域名时是 `http://<服务器的地址>`。用 `front: none` 或 `front: nginx` 时，它接着从你的机器访问应用的端口，有端口没回答时，会提示你服务商的防火墙或安全组可能挡住了它，这些 Tifa 不改。

每次部署和回滚都是 `/srv/tifa/<app>/deploys.log` 里的一行：时间、发布、git 提交（应用有未提交的改动时带 `-dirty`）、`deploy` 或 `rollback`，以及 `ok` 或 `failed`。上传从不包含 `.env`。一个应用同一时间只运行一次部署。

## 慢启动 {#a-slow-start}

`tifa deploy` 停掉旧程序，在 systemd 的 socket 后面启动新程序，所以站点从不拒绝请求。但新发布启动期间（包括 `OnStart` 和其中的迁移），访客的请求都在 socket 上等着。`start_timeout` 设得越长，允许的启动就越慢，而慢启动就是整个站点停住那么久：设成 `start_timeout: 5m` 后，一个要花四分钟的迁移会让每位访客等四分钟。

新发布在 `start_timeout` 内没有回答 `/healthz`，就算失败。这时旧发布重新运行。没做完的如果是迁移，正在运行的那个文件会被撤销（每个文件在自己的事务里运行），它之前的文件留下。要么把设置调大到够这项工作用，要么把慢的工作变小：拆分迁移，或者在安静的时候手动建一个很长的索引（见[迁移能花多长时间](../guides/database.zh-CN.md#how-long-a-migration-may-take)）。

```yaml
deploy:
  start_timeout: 2m
```

## 部署前后的步骤 {#before-and-after-a-deploy}

`tifa.yaml` 里没有放部署前后要运行的命令的地方。把它们串在你本来就运行命令的地方。

在你的机器上，用 shell。发布没通过检查时 `tifa deploy` 以 1 退出，所以它后面的步骤能知道。用 `sh`：

```sh
go test ./... &&
  go tool tifa deploy &&
  curl -fsS -X POST -d 'shop deployed' "$NOTIFY_URL"
```

用 PowerShell 时，在每条命令之后检查退出状态，因为 Windows PowerShell 5.1 没有 `&&`（PowerShell 7 有，所以那里可以用 `sh` 的写法）：

```powershell
go test ./...
if ($LASTEXITCODE -ne 0) { exit 1 }
go tool tifa deploy
if ($LASTEXITCODE -ne 0) { exit 1 }
Invoke-RestMethod -Method Post -Uri $env:NOTIFY_URL -Body 'shop deployed'
```

在 GitHub Actions 里，`tifa new` 和 `tifa deploy setup` 写的两个工作流就是这两个地方（见[从 GitHub Actions 部署](deploy-github-actions.zh-CN.md#deploy-from-github-actions)）。部署前必须通过的步骤写进 `.github/workflows/ci.yml`：部署会等 `auto.needs` 指定的工作流，所以那里失败的步骤会拦下部署。

```yaml
      - run: go tool tifa check
      - run: go test ./...
```

部署后运行的写进 `.github/workflows/tifa-deploy.yml`，放在 `Deploy` 这一步之后。编辑之前先删掉那个文件的第一行（以 `# Written by go tool tifa deploy setup.` 开头的注释），否则 setup 会把这个文件重新写一遍。`Deploy` 这一步失败时，后面的步骤会被跳过，除非它们写了 `if: always()`：

```yaml
      - name: Deploy
        # ... as setup wrote it
      - name: Tell the team
        if: always()
        env:
          NOTIFY_URL: ${{ secrets.NOTIFY_URL }}
          RESULT: ${{ job.status }}
        run: curl -fsS -X POST -d "shop deploy $RESULT" "$NOTIFY_URL"
```

在仓库的 Settings、Secrets and variables、Actions 里加上 `NOTIFY_URL`。你的机器和 CI 的密钥是分开的，所以各自都要设一份。

## 回滚 {#roll-back}

```sh
go tool tifa deploy rollback
```

它启动最近一个通过了检查的更早发布，用同样的方式检查它，并清除 CDN 缓存。再运行一次就能回到更早的。下一次 `tifa deploy` 会用你的代码做出一个新的发布。

要去你自己挑的发布，给出 `tifa deploy status` 列出的发布 ID：

```sh
go tool tifa deploy rollback 20261120T110000Z
```

那个发布可以比正在运行的更旧，也可以更新。它必须还在服务器上，并且部署时通过了健康检查。不满足时什么都不改，错误信息会列出能去的发布。它就是正在运行的那个时，也什么都不改，命令会说明这一点。

数据库保持原样。回滚在更新的发布留下的表上启动更旧的发布，访客在这之后写的东西都还在，更旧的发布会记一条警告，写出数据库运行过的最新迁移。只要每个发布都能在下一个发布的表上运行，这就行得通，见[让回滚一直能用](../guides/database.zh-CN.md#keep-a-rollback-working)。要连数据也撤销，就用备份回到更早的时间：[撤销一次错误的部署](../guides/database.zh-CN.md#undo-a-bad-deploy)。

## Tifa 记住的服务器 {#the-servers-tifa-remembers}

setup 或部署成功之后，Tifa 会把服务器写进 `~/.tifa/config.yaml`（Windows 上是 `%USERPROFILE%\.tifa\config.yaml`），所以这台机器上的下一个应用不再需要 `TIFA_DEPLOY_HOST`。在别的机器上设置的服务器，或者在有这个文件之前设置的服务器，会在这台机器第一次部署到它时得到自己的条目。在 GitHub Actions 里部署什么都不写。这个文件里没有任何 secret，只有这台机器设置过或部署过的服务器：

```yaml
# Servers this machine has set up or deployed to with tifa deploy. tifa deploy uses the only one when an app names none.
# No secrets are kept here. To forget a server, delete its entry.
servers:
  - id: vultr-1a2b3c4d
    host: ssh://203.0.113.7:22
    apps:
      - {name: shop, path: /Users/me/shop, host: 'ssh://203.0.113.7:22'}
      - {name: blog, path: /Users/me/blog, host: 'ssh://203.0.113.7:22'}
```

- `id` 是服务器的主机名加上它的机器 ID 的开头，setup 和部署从服务器上读取机器 ID。服务器的连接方式变了，它的条目也还在。`host` 是新应用会用的那个：最近一次连通的。
- 一个应用就是它在这台机器上的检出文件夹（`path`），`name` 只用于提示信息。同名应用的另一个检出目录算新的应用。每个应用保留对它连通过的 host：用 `connect: cloudflare` 时，在经由 tunnel 的检查通过之后，它是应用域名的 ssh 主机名，否则是你给的地址。当服务器的 `host` 原本是被移除的那个应用的 host 时，`remove` 会把它换成留下来的某个应用的 host。
- 当一个应用没有指定服务器时（参数、环境变量和 `.env` 优先），Tifa 使用文件里唯一的那台服务器，并打印一行说明用的是哪一台，以及要指定另一台该在 `.env` 里放什么。文件夹列在某台服务器名下的应用，会去那台服务器，即使有好几台。有好几台服务器、而应用不在任何一台上时，命令会停下，列出它们，并说请在 `.env` 里设置 `TIFA_DEPLOY_HOST=<host>` 或传 `--host`。`setup` 和 `remove` 的方案会把这个文件显示为服务器的来源。
- 取自这个文件的 host，会在命令第一次向服务器询问时被检查：如果那里的机器不是条目里的那台（别名或地址现在指向了别处），命令会停下，并说出两个 ID 和这个 host。`tifa deploy logs` 不先询问任何东西，所以不做检查。
- `tifa deploy remove` 把应用从它的条目上拿掉，最后一个应用走了，条目也跟着走。只有当 remove 做完、没有留给你的事时才会这样做，所以重试会找到同一台服务器。
- `TIFA_HOME` 可以移动这个文件夹。文件权限是 0600，所在文件夹是 0700，通过临时文件加重命名写入，旁边的锁文件避免两个 Tifa 命令互相丢掉对方的条目。你加的注释会保留。同一台服务器（同一个 `id`）列了两次是错误，会指出这个文件。
- 想手动忘掉一台服务器，就删掉它的条目，或者删掉整个文件。没有别的东西依赖它。CI 从不读它：在 GitHub Actions 里，缺少 `TIFA_DEPLOY_HOST` 是一个关于 secret 的错误，工作流从那个 secret 得到它的服务器。

## 更多开发者 {#more-developers}

每台要部署的机器都需要它自己到服务器的访问权限，setup 不会把它带过去。在一台能登录的机器上，把新机器的公钥加到用户 `tifa` 上（用 `connect: cloudflare` 时，请用 ssh 主机名，它经由 tunnel）：

```sh
ssh-copy-id -i ~/.ssh/id_ed25519.pub tifa@vps
```

用 `connect: cloudflare` 时，新来的人还要用他自己的 cf 登录运行 `go tool tifa deploy setup`，并在他的 `.env` 里放 `TIFA_DEPLOY_HOST=<ssh host>`：setup 会把他的邮箱加进 Access 策略，安装 cloudflared，并写好他的 `~/.ssh/config` 里的块，第一次 ssh 会在他的浏览器里打开 Access 的登录页。ssh 主机名是第一个人的 `.env` 里的那个，或者他的 setup 方案里打印出来的那个。新机器还需要知道服务器在哪里：把 `TIFA_DEPLOY_HOST=` 放进它的检出目录的 `.env`（或者传一次 `--host`，因为成功的部署会把服务器记在 `~/.tifa` 里），在没有 gh 的情况下运行 setup 时，也要在那里放 `TIFA_DEPLOY_GITHUB_TOKEN=`，如果这个值是别名，还要在它的 `~/.ssh/config` 里有同样的别名。机器上不保存别的东西：`tifa.yaml` 在仓库里，其余的在服务器、Cloudflare 和 GitHub 上。
