---
summary: '怎样把 Tifa 应用放到服务器上：`tifa deploy`、ssh、systemd、GitHub Actions、Cloudflare、健康检查'
audience: app
order: 3
translatedFrom: 88f2722317b2
read_when:
  - '第一次把 Tifa 应用放到 VPS 或容器里'
  - '运行 tifa deploy、tifa deploy setup 或 tifa deploy rollback，或者某次部署没通过健康检查而回退了'
  - '用 TIFA_DEPLOY_HOST 给 tifa deploy 指定服务器，或者在没有指定时想知道它用的是哪台服务器（~/.tifa/config.yaml、TIFA_HOME），或者在 tifa.yaml 里选择 deploy.front、deploy.connect 或 deploy.auto'
  - '在 CI 之后用 GitHub Actions 部署，选择由哪个分支部署或它等待哪个工作流（`auto: branch` 和 `needs`），或者收回 CI 用来部署的密钥'
  - '用 cf 把应用放到 Cloudflare Tunnel 后面，或者想知道 tifa deploy setup 在 Cloudflare 里改了什么、怎样撤销'
  - '某次部署提示 CDN 缓存没有清除，或者你想添加或替换清除缓存用的令牌（setup 把它保存在服务器上）'
  - '在没有 gh 的机器上运行 tifa deploy setup 或 remove，用 TIFA_DEPLOY_GITHUB_TOKEN 里的细粒度令牌'
  - '让第二台开发机有权部署'
  - '在自己的机器上用 `connect: cloudflare` 通过 Cloudflare Tunnel 部署，这样服务器不需要开放 ssh 端口，或者某次部署在浏览器里等 Cloudflare Access 登录'
  - '用 `connect: tailscale` 从 CI 部署，创建运行器用来加入你的 tailnet 的 Tailscale 身份，或者某次 CI 运行加入不了 tailnet'
  - '用 tifa deploy env 设置已部署应用 .env 里的密钥或设置'
  - 'agent 运行 tifa deploy env set，或带 --cache-purge-token 的 tifa deploy setup，浏览器里打开了一个页面用来输入密钥'
  - '读取已部署应用的日志、检查哪个版本在运行以及它是否健康，或者用 tifa deploy logs、status 或 remove 把应用从服务器、Cloudflare 和 GitHub 上拿掉'
  - '一台服务器上放多个应用，或者找已部署应用的日志、.env 或数据'
  - '`front: none` 的已部署应用从外面访问不到，setup 因为 80 或 443 端口被占而停下，你想知道要开放哪些端口，或者想知道 setup 和 remove 在 ufw 里改了什么'
  - '不用 Cloudflare，用 `deploy.front: nginx` 把几个 Tifa 应用放在同一台服务器上，或者 setup 因为服务器上有一个不是 Tifa 装的 nginx 而停下'
  - '在 Tifa 应用前面放 CDN 或 nginx、Cloudflare、CloudFront 之类的反向代理'
  - 'POST 只在代理后面得到 403，信息是 `cross-origin request detected`'
  - '部署之后页面显示旧内容或缺少脚本'
  - '决定 Tifa 应用的 systemd 单元或 Dockerfile 需要什么'
  - '每次部署时访客的连接被拒绝，或者被登出'
  - '访客在你部署时一直开着一个页面，想知道浏览器会怎么做，或者出现带 `X-Tifa-Build` 的 409'
  - '给负载均衡、容器或 Kubernetes 加健康检查'
  - '用 Prometheus 或其他监控系统观察虚拟机池和渲染记忆，或者据此判断机器是否需要更多 CPU 或更大的 `ssr.memo_size`'
---

# 部署 {#deploying}

Tifa 应用就是一个可执行文件。服务器需要这个文件、环境里的设置，以及一个可以写数据的文件夹。它不需要 Node、进程管理器和 Redis。默认的会话存储放在内存里，所以如果要运行多份程序，请给它[你自己的存储](../pages-and-data/sessions.zh-CN.md#where-to-store-sessions)。

## 用 tifa deploy 部署 {#deploy-with-tifa-deploy}

`tifa deploy` 把 Tifa 应用放到你自己的服务器上，一条命令就设置好从 push 到上线的整条 CI/CD 路径。

`go tool tifa deploy` 通过 ssh 把应用放到带 systemd 的 Linux 服务器上：它在你的机器上为服务器构建，上传程序，重启应用而不拒绝任何请求，在服务器上检查它，检查失败时回到之前的发布。应用自己接待访客并用自己的 HTTPS，或者放在 Cloudflare Tunnel 或 Tifa 安装的 nginx 后面，这时多个应用可以共用一台服务器。服务器不需要 Docker、Node，也不需要自己的部署程序。

`tifa.yaml` 写明你要什么。`go tool tifa deploy setup` 让服务器、Cloudflare 和 GitHub 与它一致。`tifa.yaml` 是要提交的，所以密钥和服务器的位置都不放进去。

```yaml
site:
  url: https://shop.example.com   # the domain is the host of this URL
deploy:
  front: none      # what visitors come through before the server: none (the app serves them itself on ports 80 and 443) | cloudflare | nginx
  connect: direct  # how deploys reach the server over ssh: direct | cloudflare | tailscale
  auto:            # deploy by itself from GitHub Actions. No block: you deploy by hand only
    branch: main   # the branch that deploys. Left out: the default branch of the repository on GitHub
    needs: ci.yml  # the workflow file in .github/workflows that must pass first. Left out: ci.yml. none: do not wait
```

- 服务器在哪里不放进 `tifa.yaml`，因为公开的仓库会把它显示出来。在你的机器上指定它，先匹配到的优先：
  1. 参数 `--host`
  2. 环境变量 `TIFA_DEPLOY_HOST`
  3. 应用根目录里的 `.env` 文件中的 `TIFA_DEPLOY_HOST`，git 会忽略这个文件（见 [.env](config.zh-CN.md#env)。程序自己不理会这个名字）
  4. `tifa deploy setup` 记在 `~/.tifa/config.yaml` 里的服务器（见[Tifa 记住的服务器](#the-servers-tifa-remembers)）

  ```sh
  # .env
  TIFA_DEPLOY_HOST=ssh://203.0.113.7:2222
  ```

  这个值就是 `ssh` 接受的目标，不带用户：可以是 `~/.ssh/config` 里的一个 Host（比如别名 `vps`，这样地址、端口、密钥和 `ProxyJump` 都由 `~/.ssh/config` 提供）、一个主机名，或者一个 `ssh://host[:port]` 的 URL。用户由 Tifa 自己选，第一次设置用 root，之后用 `tifa`，所以带用户、路径、查询或别的协议的值会被拒绝。四种都没设置时，每个 `tifa deploy` 命令都会停下并说明。
- `front` 是访客到达服务器之前经过的东西。选 `cloudflare` 时，就是 `site.url` 的主机上的一个 Cloudflare Tunnel，带它的 DNS 记录和规则，`deploy` 和 `rollback` 会清除那个主机名的 CDN 缓存。必须设置 `site.url`，并且不带端口。选 `none` 时，应用自己在 80 端口上接待访客，`site.url` 里有域名时还在 443 端口上用自己的 HTTPS，也不清缓存（见[应用自己接待访客](#the-app-serves-visitors-itself)）。选 `nginx` 时，setup 安装的一个 nginx 按 `site.url` 的主机把访客送到应用，并用 certbot 为它拿证书，这样几个应用不用 Cloudflare 也能共用一台服务器（见[在 nginx 后面](#behind-nginx)）。
- `connect` 是 ssh 连到服务器走的那一条路，从你的机器和从 GitHub Actions 的 runner 都一样。选 `direct` 时，ssh 用你给的地址。选 `cloudflare` 时，两边都经由 Cloudflare Tunnel，所以服务器不用开放 ssh 端口（见 [`connect: cloudflare`](#connect-cloudflare)）。它需要 `front: cloudflare`，因为 ssh 的主机名是由域名生成的，有没有 `auto` 都行。选 `tailscale` 时，runner 加入你的 tailnet，它需要 `auto` 这个块（见 [`connect: tailscale`](#connect-tailscale)）。你自己的机器照常连到服务器，用 `TIFA_DEPLOY_HOST` 里的地址，tailnet 和服务器都由你自己设置。
- `auto` 只要写上就会开启从 GitHub Actions 的部署。没有这个块，就只能手动部署。`auto: {}` 用默认值，写成后面什么都没有的 `auto:` 是错误。它只有两个键，没有别的：
  - `branch` 是会部署的分支：往它 push，并且 `needs` 里的工作流通过之后就部署。不写时，它是 GitHub 上仓库的默认分支。
  - `needs` 是 `.github/workflows` 里必须先通过的那个文件，写成文件名，比如 `ci.yml`（不带文件夹）。不写时，它是 `tifa new` 写出的 `ci.yml`。写 `none` 就什么也不等：每次往这个分支 push 都会部署。见[从 GitHub Actions 部署](#deploy-from-github-actions)。
- 只支持 GitHub。

**输入。** 属于你而不属于仓库的值，每次都用同样的方式读取：输入有参数就先用参数，否则用环境变量，再否则用应用根目录里 `.env` 文件中的那一行。Tifa 会读这个文件。setup 从不往里写令牌，也从不打印令牌。它在那里写的唯一一行是 `TIFA_DEPLOY_HOST`：经由 tunnel 的 ssh 通了之后，`connect: cloudflare` 会把它改成 ssh 主机名（见 [`connect: cloudflare`](#connect-cloudflare)），而且只在这个值本来就在 `.env` 里时才改。

| 输入 | 参数 | 它是什么 | 来自 |
| --- | --- | --- | --- |
| `TIFA_DEPLOY_HOST` | `--host` | 服务器在哪里 | 参数、环境变量、`.env`，然后是 `~/.tifa` |
| `TIFA_DEPLOY_CF_PURGE_TOKEN` | 无 | 只能清除一个 zone 的缓存的 Cloudflare 令牌。它优先于 setup 存在服务器上的那个（见[清除 CDN 缓存](#clearing-the-cdn-cache)） | 环境变量、`.env` |
| `TIFA_DEPLOY_CF_ZONE_ID` | 无 | 那个 zone 的 ID，和上面的令牌一起需要 | 环境变量、`.env` |
| `TIFA_DEPLOY_GITHUB_TOKEN` | 无 | 一个细粒度的 GitHub 令牌，在没有 gh 时用于 setup 和 remove | 环境变量、`.env` |

`.env` 里有 tifa deploy 不读的 `TIFA_DEPLOY_` 名字时，命令同样会停下，错误里写出最接近的那个名字。

你需要：一台带 systemd 的 Linux 服务器，x86_64 或 aarch64，你能用 `ssh root@<TIFA_DEPLOY_HOST>` 连上它一次（在 Ubuntu 24.04 上测试过）。在你的机器上，需要 `tifa build` 需要的东西（Go，带页面的应用还要 Node）和 `ssh`。对于 `front: cloudflare` 和 `connect: cloudflare`，需要 [cf](https://developers.cloudflare.com/cf/get-started/)，即 Cloudflare CLI（测试版），并且已安装（`pnpm add --global cf`）、已登录（`cf auth login`）。setup 和 remove 能分清这两种情况：PATH 里没有 cf 时，它们说要先安装，装了 cf 就请你登录。对于 `connect: cloudflare`，你的机器上还需要 [cloudflared](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/downloads/)：setup 在 macOS 上用 `brew install cloudflared` 安装它，在 Windows 上用 `winget install --id Cloudflare.cloudflared -e`，在 Linux 上，或者没有 brew 和 winget 时，它会告诉你去哪里下载。对于 `front: nginx`，需要一台带 apt 的服务器（Debian 或 Ubuntu），setup 以 root 用 apt 在上面安装 nginx 和 certbot。对于 `auto`，需要已登录的 [gh](https://cli.github.com/)（`gh auth login`），没有 gh 的话，需要把细粒度令牌放在 `TIFA_DEPLOY_GITHUB_TOKEN` 里（见[从 GitHub Actions 部署](#deploy-from-github-actions)）。对于 `connect: tailscale`，需要一台已在你的 tailnet 里的服务器（装了 Tailscale 并登录），以及一个你在 Tailscale 控制台里创建的 Tailscale 凭证（见 [`connect: tailscale`](#connect-tailscale)）。你的机器上不需要任何 Tailscale 的东西。在终端里，setup 会替你安装并登录 cf 和 gh（见下文）。

### 按方案设置 {#set-up-with-a-plan}

```sh
go tool tifa deploy setup
```

setup 把 `tifa.yaml` 和服务器、Cloudflare、GitHub 做比较，并打印一份方案。方案只列出将要 `add`、`change` 或 `remove` 的东西。已经正确的东西用一行计数（`--all` 会列出它们）。最后是 setup 没法替你做的事，编成带编号的列表，每项下面写原因：

```
Plan for shop on vps

  add     app: /srv/tifa/shop with an empty shared/.env, port 9527, the systemd socket and service tifa-shop
  add     cloudflare: the tunnel of the server, tifa-vps-1234
  add     server: install cloudflared when missing and run the tunnel as the service tifa-tunnel, as root
  add     cloudflare: route shop.example.com -> http://127.0.0.1:9527
  add     cloudflare: DNS shop.example.com CNAME the tunnel, proxied
  add     cloudflare: cache rules for shop.example.com
  add     cloudflare: HTML rewriting off for shop.example.com
  add     github: a key for acme/shop that may log in as tifa, kept as the secret TIFA_DEPLOY_SSH_KEY
  add     github: where CI logs in (direct) and the host keys it trusts, the secrets TIFA_DEPLOY_HOST and TIFA_DEPLOY_KNOWN_HOSTS
  add     github: .github/workflows/tifa-deploy.yml, which you commit and push

Already right: server 1 (--all lists them)

You need to do 1 thing that setup cannot:
  1. server: a token that can only clear the cache of example.com, pasted by you and kept in /srv/tifa/.config/tifa/cloudflare-purge, which only tifa can read (optional)
     There is no terminal to paste on.
     go tool tifa deploy setup --yes --cache-purge-token opens a page in your browser for it.
     Without it a deploy still works, but ... (the rest says what going without costs, and to add it later with tifa deploy setup --cache-purge-token)

Apply this plan? [y/N]
```

在终端里，它会询问，而且只在有东西要改时才问。没有终端时，setup 打印方案，并以错误停下，除非你传 `--yes`。得到答复后，它应用方案，并为每一步打印一行。当一切都正确、也没有留给你的事时，setup 打印一行，什么也不问：

```
shop on vps matches tifa.yaml: 9 things checked (--all lists them).
```

**cf 和 gh。** 当 `tifa.yaml` 需要 cf（`front: cloudflare`）或 gh（没有 `TIFA_DEPLOY_GITHUB_TOKEN` 的 `auto` 块），而它缺失或没有登录时，第一份方案会安装并登录它，其余的等它好了再做，因为 setup 要靠它们读取 Cloudflare 和 GitHub：

```
Plan for shop on vps

  add     this machine: install cf with pnpm add --global cf
  add     this machine: log in to cf (opens a browser)

Setup plans the rest, with the Cloudflare part, once cf is ready, and asks again.

Apply this plan? [y/N]
```

你输入的 y 就是同意：setup 不会再就这件事多问。它用 `pnpm add --global cf` 安装 cf，在 macOS 上用 `brew install gh`、在 Windows 上用 `winget install --id GitHub.cli -e` 安装 gh，然后在你的终端里运行 `cf auth login` 和 `gh auth login --web`，每个都会打开浏览器。gh 登录时用 git 协议 https，不动你其他的仓库，只问一个问题：要不要也用 GitHub 登录 git。两个答案对 Tifa 都行。选是的话，git 可以用 gh 的登录来 push，工作流文件也包括在内。setup 用的是包管理器把程序放下的那个位置，即使你终端的 PATH 里还没有那个文件夹。之后它规划其余的部分，再问一次。它只用机器上已有的包管理器来安装，自己从不下载程序。带 `--yes` 时，即使在终端里，或者没有终端时，它什么也不安装、不登录，而是把命令列在你要做的事里：用 `--yes` 运行 setup 的智能体，从不会安装全局包或打开浏览器。没有 pnpm，没有 brew 或 winget，以及在 Linux 上（那里 gh 需要 root 和 [GitHub 的软件包仓库](https://github.com/cli/cli/blob/trunk/docs/install_linux.md)），它同样这么做。安装或登录失败只是一个警告，里面写着怎么手动做（pnpm 在全局安装任何东西之前，需要先运行一次 `pnpm setup`），setup 会继续做不需要那个工具的部分。

当有留给你的事，或者某个可能失败的步骤失败了（比如 Cloudflare 拒绝了三次的缓存清除令牌）时，setup 在做完其余的一切之后，以错误退出。标着 `(optional)` 的事项（比如缓存清除令牌）不会让它失败。脚本可以依赖退出码：照列表做完之后，再运行一次 setup。`tifa.yaml` 每次变化后都再运行它：它只做有差别的事。

setup 只删除 Tifa 自己创建并做了标记的东西（它没法删掉你创建的 Cloudflare 令牌：方案会说明要在控制台里删掉它）：`authorized_keys` 里注释为 `tifa-github <owner>/<repo>` 的行，nginx 站点 `/etc/nginx/conf.d/tifa-<app>.conf` 和 certbot 的证书 `tifa-<app>`，注释为 `tifa <app>` 或 `tifa nginx front` 的 ufw 规则，名字以 `tifa` 开头的 Cloudflare 对象（以及 Access 应用 `tifa deploy ssh <host>`），你的 `~/.ssh/config` 里以 `# tifa <app>:` 开头的那个块，它添加的域名的路由和 DNS 记录，仓库变量 `TIFA_DEPLOY_FINGERPRINT`、`TIFA_DEPLOY_TAILSCALE_ID` 和 `TIFA_DEPLOY_TAILSCALE_AUDIENCE`，以及随它们设置的 secret（`TIFA_DEPLOY_HOST`、`TIFA_DEPLOY_KNOWN_HOSTS` 和其他的），以及以 Tifa 那一行开头的工作流文件。设置 `front: none`，或者把 `auto` 块去掉后，方案会在动手之前列出这些删除。

方案的各层：

| 层 | 内容 | 需要 |
| --- | --- | --- |
| server | 用户 `tifa`，它拥有并运行服务器上的每个应用，可以用 root 的密钥登录（home 是 `/srv/tifa`），以及 linger，让它的 systemd 在开机时运行 | `ssh root@<TIFA_DEPLOY_HOST>`，只在第一次 |
| app | `/srv/tifa/<app>`、它空的 `shared/.env`、数据文件夹 `shared`、它在 `port` 里的端口（`front: none` 时是 80，有 HTTPS 时是 443，否则是回环地址上从 9527 起第一个空闲的），以及用户单元 `tifa-<app>.socket` 和 `.service`，有 HTTPS 时还有 `tifa-<app>-http.socket`。`front: none` 时，每台服务器还要以 root 做一次设置，让 `tifa` 的 systemd 能打开 80 和 443 端口 | 以 `tifa` 身份 ssh，以及一次 root |
| front: cloudflare | Cloudflare Tunnel、服务器上的 cloudflared、`site.url` 主机的路由、被代理的 DNS 记录和缓存规则，以及服务器上清除这些缓存的令牌 | cf |
| front: nginx | 以 root 用 apt 安装的 nginx，有域名的应用还有 certbot 和它的 nginx 插件，用来标记这个 nginx 是 Tifa 的 `/etc/tifa/nginx`，应用在 `/etc/nginx/conf.d/tifa-<app>.conf` 里的站点，它的证书，以及 ufw 里的 80 和 443 端口（见[在 nginx 后面](#behind-nginx)） | `ssh root@<TIFA_DEPLOY_HOST>` |
| ci | 仓库的一把密钥、secret（CI 在哪里登录、主机密钥），以及工作流 | gh 或 `TIFA_DEPLOY_GITHUB_TOKEN` |
| connect: cloudflare | tunnel 上的一个 ssh 主机名、一个放行你的邮箱的 Access 应用（用 CI 时还有一个 service token），以及你机器上的 cloudflared 和 `~/.ssh/config` 里的一个块 | cf |

`<app>` 是 `go.mod` 里模块路径的最后一段，也就是 `tifa build` 给程序起的名字。在服务器上以 `tifa` 或 root 填好 `.env`：应用每次启动时都会读它。

**Cloudflare Tunnel**（`front: cloudflare`）。服务器不开任何端口：服务器上的 cloudflared 主动连出到 Cloudflare，一个 tunnel 服务服务器上的每个应用。setup 用你的 cf 登录。Tifa 用到的 Cloudflare 令牌都留在你的服务器上：tunnel 的令牌只从 cf 到服务器，清除缓存的令牌只从你的粘贴到服务器。

1. 在你的账号里找到这个域名的 zone。
2. 服务器还没有 tunnel 时，创建服务器的 tunnel，名字是 `tifa-<host name>-<start of its machine ID>`，并把它的 ID 保存在 `/etc/tifa/tunnel`。
3. 服务器上没有 cloudflared 时安装它，用 apt 从 [Cloudflare 的软件包仓库](https://pkg.cloudflare.com/)安装：apt 会用 Cloudflare 的签名密钥检查软件包，设置文件 `/etc/apt/apt.conf.d/52tifa-cloudflared` 让 unattended-upgrades 安装新版本。Tifa 自己不下载任何程序：在没有 apt 的服务器上，请先用你系统的软件包安装 cloudflared。把 cloudflared 作为系统服务 `tifa-tunnel` 运行，令牌放在 `/etc/tifa/tunnel-token`（仅 root 可读），作为 systemd credential 交给它，cloudflared 变化时重启它。
4. 往 tunnel 里添加路由 `<domain> -> http://127.0.0.1:<port>`，保留其他应用的路由。
5. 添加一条被代理的 CNAME 记录，把域名指向 `<tunnel ID>.cfargotunnel.com`。域名上已经有指向别处的记录时，不会动它，setup 会停下。
6. 添加只对这个主机名生效的规则，如 [CDN 设置](#cdn-settings)所说：一条缓存规则，缓存应用的 `Cache-Control` 允许缓存的内容，没有它的就不缓存，一条缓存规则，永远不缓存 `/__tifa/` 或导航数据（`?_tifa=`），还有一条配置规则，关掉 Rocket Loader、Email Address Obfuscation、Cloudflare Fonts、Zaraz 和 Web Analytics 脚本，它们都会往 HTML 里写东西。整个 zone 的设置不会改动。
7. 请求一个令牌，这是可选的，它只能清除 zone 的缓存，并把它和 zone 的 ID 一起保存在服务器上，每次部署都能在那里找到它（见[清除 CDN 缓存](#clearing-the-cdn-cache)）。cf 的登录做不了 API 令牌，所以你要在 Cloudflare 控制台里做，在终端里 setup 会带你走一遍：它打印一个链接，指向 Cloudflare 的表单，已经填好了 Cache Purge 权限和名字 `tifa cache purge <zone>`。把 Zone Resources 设为只包含这个 zone，创建令牌，并在隐藏的提示处粘贴它。setup 通过清除一个不存在的 URL 来检查它，最多重试 3 次，然后通过 ssh 以 `tifa` 身份，用 ssh 的输入把它写到服务器上。输入空行会跳过它，setup 继续并成功。setup 在这个域名第一次设置时询问，在第一次设置 CI 时询问。服务器上已经有这个 zone 的令牌后，setup 每次运行都用同样的方式检查它，只要 Cloudflare 接受就什么也不问：同一台服务器、同一个 zone 上的第二个应用什么也不用做。Cloudflare 拒绝的令牌，比如已过期或已被删除的，会再问一次。没有令牌时，之后带 `auto` 的运行，以及没有终端的第一次运行，都把它列为可选。以后要加这个令牌或替换它，请运行 `go tool tifa deploy setup --cache-purge-token`：即使服务器上已经有一个，它也会问，在终端里用隐藏的提示，没有终端时用浏览器里的一个页面，加了 `--yes` 也一样（见[没有终端时的 secret](#secrets-without-a-terminal)）。

对服务器上最后一个应用运行 `tifa deploy remove`，也会删掉 tunnel 和缓存令牌（见[日志、状态和移除](#logs-status-and-remove)）。要手动移除它们，运行 `systemctl disable --now tifa-tunnel tifa-tunnel-upgrade.path`，并删除服务器上的 `/etc/tifa`、`/etc/systemd/system/tifa-tunnel*` 和 `/srv/tifa/.config/tifa`，然后在 Cloudflare 里删除 tunnel 和令牌 `tifa cache purge <zone>`。cloudflared 仍然是装着的：`apt-get purge cloudflared` 再删除 `/etc/apt/sources.list.d/cloudflared.list`、`/usr/share/keyrings/cloudflare-main.gpg` 和 `/etc/apt/apt.conf.d/52tifa-cloudflared`，也能把它移除。

#### 应用自己接待访客 {#the-app-serves-visitors-itself}

用 `front: none` 时，程序直接面对访客，前面没有 nginx、Caddy 或 certbot：

- **有域名时**，也就是 `site.url` 的主机，比如 `https://shop.example.com`：systemd 为应用打开 443 和 80 端口，应用运行它[内置的 HTTPS](config.zh-CN.md#https)。setup 把 `SERVER_TLS_DOMAINS=<domain>` 和 `SERVER_TLS_CACHE=%h/<app>/certs` 写进服务的环境，这样程序从 Let's Encrypt 获取证书，并保存在 `/srv/tifa/<app>/certs`，每个发布都共用它。80 端口回答 Let's Encrypt 的检查，其他请求都转到 HTTPS。第一次部署之前，DNS 必须把域名指向这台服务器：程序拿到证书之前不回答 HTTPS，部署的健康检查会失败，并说明原因。
- **没有域名时**：没有 `site.url`，或者它的主机是 IP 地址或 `localhost`。应用在 80 端口上提供普通 HTTP。

端口由 systemd 拿着，所以应用以 `tifa` 身份运行，不需要 root，也不需要任何 capability，重启时也不拒绝连接。在 Linux 上，用户的 systemd 只能打开 1024 起的端口，所以 setup 每台服务器以 root 做一次：把 `net.ipv4.ip_unprivileged_port_start = 80` 写到 `/etc/sysctl.d/60-tifa-ports.conf`。之后 80 到 1023 的端口对服务器上的每个用户都开放，不再只有 root 能用。对这个应用运行 `tifa deploy remove` 会删掉这个文件，并改回原来的值。

**一台服务器一个应用。** 一个端口只能有一个程序监听，所以一台服务器上只能有一个应用自己接待访客。setup 在改动任何东西之前，先看应用需要的端口（80，有域名时还有 443）上有谁在监听：`ss -ltnp`，以及 `tifa` 的 systemd 拿着的 socket。端口被 Tifa 的另一个应用占着，或者被 nginx、Caddy、Apache 之类的程序占着时，setup 会停下，说出是谁、哪个端口，什么也不改。请停掉那个程序并让它开机不再启动，或者移除另一个应用，或者给这个应用和另一个应用都用 `front: cloudflare` 或 `front: nginx`，那样多个应用可以共用一台服务器（见[在 nginx 后面](#behind-nginx)）。

**ufw。** 如果 ufw 开着，并且不让访客访问应用需要的端口（80 和 443，没有域名时是 80），setup 的计划会放行它们：

```
  change  firewall: allow ports 80 and 443 (tcp) in ufw, so visitors reach shop, in rules with the comment 'tifa shop', as root
```

你同意之后，或者带了 `--yes`，setup 以 root 为每个端口运行 `ufw allow 80/tcp comment 'tifa <app>'`。以下情况 ufw 里什么都不改：ufw 关着或没装，你自己的规则已经放行了这个端口，或者它的默认策略放行所有连接。你自己拒绝这个端口的规则会保留：setup 会说是哪条，以及怎样删掉它。注释 `tifa <app>` 标明这些规则是 Tifa 加的：`tifa deploy remove` 会删掉它们，应用不再在某个端口上接待访客时（比如改成 `front: cloudflare` 之后），setup 也会删掉。其他规则一概不碰，ssh 的规则也不碰。只有 root 能读 ufw 的规则：root 登录不了时，setup 会告诉你改去服务器上查什么。

**服务商的防火墙归你管。** Tifa 不改服务商的防火墙和安全组，也不改服务器上不是 ufw 的防火墙（firewalld，或者不是 ufw 写的 nftables、iptables 规则）。计划会说访客需要哪些端口，setup 结束时也会再说一遍：如果它们挡住了这些端口，请在那里开放。每次部署之后，Tifa 从你的机器访问应用的这些端口，有端口没回答时会告诉你。

用更早版本设置的服务器，应用在回环地址上。下一次 `tifa deploy setup` 会把搬迁列进计划：先以 root 设置端口，再换上新的 socket，之后应用在新 socket 上重启，并且必须通过和部署一样的健康检查。

如果有 CDN，按 [CDN 设置](#cdn-settings)设置它，并按[部署之后](#after-a-deploy)所说，在每次部署时清除它的缓存。

#### 在 nginx 后面 {#behind-nginx}

用 `front: nginx` 时，由 Tifa 安装的一个 nginx 接待服务器上所有用这个 front 的应用的访客，所以它们不用 Cloudflare 就能共用 80 和 443 端口。每个应用监听回环地址上的一个端口，和在 Cloudflare Tunnel 后面一样，nginx 把请求送给它所点的主机对应的应用，也就是该应用 `site.url` 的主机。

- **这个 nginx 是 Tifa 的。** setup 用 apt 安装它，作为你同意的方案里的一行，并写下 `/etc/tifa/nginx` 来标记它。这个文件列出 apt 装上的、原来没有的包，所以 remove 只删这些包，别的不动：apt 只是升级了的包，或者服务器上本来就有的 certbot，都会留着。从那以后，这个 nginx 的全部配置都由 Tifa 来写，每个应用一个文件（`/etc/nginx/conf.d/tifa-<app>.conf`），也没有别的东西用它。请不要改这些文件，它们不同时 setup 会重新写。请留着 `/etc/tifa/nginx`：没有它，setup 会把这个 nginx 当成你自己的而停下。对于这个文件从没记过的 nginx 包，比如在一次中途停下的 setup 之后你自己装的，它也一样处理。
- **你自己的 nginx 绝不会被动。** 服务器上有一个不是 Tifa 装的 nginx 时，setup 会停下，说出它和它监听的端口，什么也不改。别的程序，或者自己接待访客的 Tifa 应用，占着 80 或 443 端口时，它同样会停下。请移除那个 nginx，停掉那个程序，或者用 `front: cloudflare`。
- **有域名时**，也就是 `site.url` 的主机，比如 `https://shop.example.com`，setup 还会用 apt 安装 certbot 和它的 nginx 插件，并从 Let's Encrypt 拿一个证书，名字是 `tifa-shop`。80 端口回答 Let's Encrypt 的检查，其余一切用 308 送到 HTTPS。certbot 自己的定时器负责续期，一个钩子会让 nginx 重新加载。DNS 必须在 setup 之前就把域名指向服务器：在那之前 certbot 会被拒绝，setup 会说明，站点停留在普通 HTTP 上。DNS 对了之后再运行一次 setup。
- **没有域名时**：没有 `site.url`，或者它的主机是 IP 地址或 `localhost`。应用在 80 端口上提供普通 HTTP，并拿到所有没有点名其他应用的请求。一台服务器上只能有一个应用这样：已经有另一个应用这样时，setup 会停下并说明。请给其中一个设一个域名。
- **HSTS** 和 `front: none` 一样：默认关闭，除非你设置 `server.hsts: true`。程序发出这个头，nginx 原样传过去。
- **每次改动都先检查。** setup 写好一个站点，运行 `nginx -t`，之后才让 nginx 重新加载。重新加载不会断开其他应用的连接。`nginx -t` 或重新加载失败时，之前的文件会放回去，报错里说明 nginx 说了什么。再运行一次 setup 时，如果 nginx 没在运行，它也会把它启动。站点文件和包的改动要在服务器上拿同一把锁，`/etc/tifa/nginx.lock`，所以不同应用的两个 setup 或 remove 不会同时写。

有域名且已拿到证书的应用，它的站点是这样：

```nginx
upstream tifa-shop {
	server 127.0.0.1:9527;
	keepalive 16;
	keepalive_timeout 50s;
}
server {
	listen 80;
	listen [::]:80;
	server_name shop.example.com;
	location / {
		return 308 https://shop.example.com$request_uri;
	}
}
server {
	listen 443 ssl http2;
	listen [::]:443 ssl http2;
	server_name shop.example.com;
	ssl_certificate /etc/letsencrypt/live/tifa-shop/fullchain.pem;
	ssl_certificate_key /etc/letsencrypt/live/tifa-shop/privkey.pem;
	ssl_protocols TLSv1.2 TLSv1.3;
	location / {
		proxy_pass http://tifa-shop;
		proxy_http_version 1.1;
		proxy_set_header Connection "";
		proxy_set_header Host $http_host;
		proxy_set_header X-Forwarded-For $remote_addr;
		proxy_read_timeout 1d;
		proxy_send_timeout 1d;
		proxy_request_buffering off;
		client_max_body_size 0;
		gzip off;
	}
}
```

没有域名时只有一个 server，带 `listen 80 default_server;`。没有 IPv6 的服务器不会有 `[::]` 那几行。站点做了[在 CDN 或代理后面](#behind-a-cdn-or-proxy)要求的事：

- `Host` 和 `Origin` 按浏览器发送的样子到达应用，所以[跨站写入](config.zh-CN.md#cross-site-writes)的检查有效。`X-Forwarded-For` 是访客的地址。它由 nginx 设置，绝不会在访客发来的内容后面追加，所以编不出假的。
- nginx 最多等应用一天，所以它不会结束应用允许继续运行的请求：由程序自己的超时来决定（见[长请求](config.zh-CN.md#long-requests)）。
- 请求体一到就送给应用，nginx 不设任何限制，也不往自己的磁盘上写任何东西：由 `server.body_limit`，或者某个路由设的限制，用 413 来回答。
- 带 `X-Accel-Buffering: no` 的响应，比如服务器发送事件，会立刻传过去，因为没有叫 nginx 忽略这个头，它就会照着做。
- nginx 不做压缩（`gzip off`），所以程序对压缩的决定有效，`No-Gzip-Compression` 也包括在内。请让 `server.compress` 保持开启。
- 到应用的连接空闲时保持打开 50 秒，比程序关闭空闲连接的 60 秒短。

**ufw。** ufw 开着，并且不让访客访问 80 和 443 端口时，计划会放行这两个，规则的注释是 `tifa nginx front`，并且在 nginx 申请证书之前就放行。用更早版本的 Tifa 设置的服务器上，它们的注释是 `tifa nginx`，setup 会接手它们，不会关掉端口。最后一个用这个 nginx 的应用被 remove 时，它们会被删掉。上面**ufw** 和**服务商的防火墙归你管**下说的一切，在这里同样适用。

**搬迁。** setup 把 `none`、`cloudflare` 和 `nginx` 之间的搬迁，当作其他改动一样列进计划。搬到 nginx 的应用，先搬到回环地址的端口上，这会为 nginx 腾出 80 端口，所以访客要等一小会儿。搬走的应用把它的站点和证书从 nginx 里拿掉，没有别的应用有站点时，nginx 和 certbot 也一起走。其他应用还在 nginx 后面时，应用不能搬到 `front: none`：nginx 占着端口，setup 会停下并说明。

`tifa deploy remove` 把应用的站点和证书从 nginx 里拿掉，最后一个应用时还会卸载 nginx，以及 setup 安装过的 certbot，连同它们的配置，并删掉 `/etc/tifa/nginx`。如果 setup 只是给原来就有的 certbot 加了 nginx 插件，remove 只拿走插件，certbot、它的定时器和你的证书都不动。nginx 不肯放掉的站点会留下，它的证书和 nginx 也一起留下：报错里说明 nginx 说了什么，下一次 remove 接着做。其他应用继续回答。想手动让 nginx 不再归 Tifa 管，请对 `/etc/tifa/nginx` 里列出的包运行 `apt-get purge`，删掉 `/etc/nginx`、`/etc/letsencrypt` 和那个文件，并删掉注释为 `tifa nginx front` 的 ufw 规则。

### 部署 {#deploy}

```sh
go tool tifa deploy
```

1. 向服务器询问它的系统，并在你的机器上为它构建应用，做法和[为另一个系统构建](build.zh-CN.md#building-for-another-system)一样。服务器上什么也不构建。在 setup 已经准备好、但还没有这个应用的文件夹的服务器上，它先以 `tifa` 身份（不需要 root）创建文件夹、端口和单元。自己接待访客的应用，如果 `tifa` 还不能打开 80 端口，要先在那里运行 setup，部署会这样提示。在没有准备好的服务器上，它会停下，并说明要运行 `go tool tifa deploy setup`。
2. 把程序压缩成 gzip，上传到 `/srv/tifa/<app>/releases/<id>/`，其中 `<id>` 是部署时的 UTC 时间，并在那里检查它的 SHA-256。
3. 把 `/srv/tifa/<app>/current` 指向它，并重启服务。socket 保持打开，所以这期间到来的请求会等着，然后得到回答（见[重启而不丢请求](#restart-without-dropping-requests)）。
4. 在服务器上检查新程序：每秒一次 `GET /healthz`，最长 30 秒，直到它以低于 500 的状态回答。它在回环地址上访问，有自己 HTTPS 的应用则像访客一样，用它的域名走 HTTPS。没有 `/healthz` 路由的应用，一有响应就带着它的 404 通过。写一个会看数据库的[健康检查](#health-check)，让这项检查更有意义。
5. 检查通过后，把这次部署记下来，并保留最近 5 个发布。检查失败时，它会打印应用日志的最后几行，重新启动之前运行的那个发布，并以 1 退出。如果从来没有通过的发布，它会停掉应用。
6. 用 `front: cloudflare` 时，会清除那个主机名的 CDN 缓存（见下）。
7. 说明访客从哪里访问应用：`visitors: https://shop.example.com`，没有域名时是 `http://<服务器的地址>`。用 `front: none` 或 `front: nginx` 时，它接着从你的机器访问应用的端口，有端口没回答时，会提示你服务商的防火墙或安全组可能挡住了它，这些 Tifa 不改。

每次部署和回滚都是 `/srv/tifa/<app>/deploys.log` 里的一行：时间、发布、git 提交（应用有未提交的改动时带 `-dirty`）、`deploy` 或 `rollback`，以及 `ok` 或 `failed`。上传从不包含 `.env`。一个应用同一时间只运行一次部署。

### 回滚 {#roll-back}

```sh
go tool tifa deploy rollback
```

它启动最近一个通过了检查的更早发布，用同样的方式检查它，并清除 CDN 缓存。再运行一次就能回到更早的。下一次 `tifa deploy` 会用你的代码做出一个新的发布。

### 清除 CDN 缓存 {#clearing-the-cdn-cache}

用 `front: cloudflare` 时，部署和回滚会清除 Cloudflare 里那个主机名的缓存，按主机名，不会更宽：zone 里的其余部分保留它的缓存。否则带 `Cache` 的页面会一直显示旧的 HTML，而它的脚本已经没了，直到它的 `s-maxage` 到期。

清除是可选的。没有任何 CDN 和任何框架能让每个访客在新构建上线的那一刻就看到它：没清除的缓存会保留它的副本，直到副本到期。没有办法清除时，带 `Cache` 的页面在部署之后，最长可能有它的 `Cache` 时间那么旧，而那份旧副本可能会请求新构建里已经没有的脚本。这是用 CDN 的代价，Tifa 在每次部署时都会这样说明，但不把它当成错误。

部署会用它找到的第一种办法清除缓存，在你的机器上或在 CI 里都一样：

1. 输入 `TIFA_DEPLOY_CF_PURGE_TOKEN`，一个只能清除那一个 zone 的缓存的令牌，和 `TIFA_DEPLOY_CF_ZONE_ID`，zone 的 ID：环境变量，否则是应用根目录的 `.env`。它们优先于服务器上的那个。也要给出 zone ID，因为只能清除缓存的令牌通常没法读取 zone 来找到它。设置了令牌而没有设置 zone ID 时，部署会在它的警告里说明。setup 不会把它们写进 `.env`：请自己放进去。
2. setup 存在服务器上的令牌（见[按方案设置](#set-up-with-a-plan)）：`/srv/tifa/.config/tifa/cloudflare-purge`，每个 zone 一行，写着 zone 的名字、它的 ID 和令牌。Tifa 把令牌放在你的服务器上，只有 `tifa` 能读：文件属于 `tifa`，权限是 600，所在文件夹的权限是 700。新发布通过检查之后，部署通过它用来部署的 ssh，以 `tifa` 身份读取它。令牌只放在内存里，从不打印，不写进日志，也不放在命令行上。所以来自 CI 的部署，以及来自任何能以 `tifa` 登录的机器的部署，都会清除缓存，一个令牌服务服务器上这个 zone 的每个应用。主机和 zone 的名字不分大小写地匹配。写这个文件时会持有它旁边的一把锁（`cloudflare-purge.lock`，用 `flock`），所以同一台服务器上的两次 setup 会保留彼此的那一行。文件读不了时，部署用一行说明，然后换下一种办法。应用也以 `tifa` 运行，所以它们的代码能读这个文件：这个令牌只能清除这个 zone 的缓存。
3. 在你的机器上，cf 用你的登录。

三种都没有时，部署会打印 `cache: not cleared for <host>`，说明这样的代价，并说明运行 `go tool tifa deploy setup --cache-purge-token`，它不是警告。
- 清除失败不会让部署失败：它打印一条警告，以 0 退出，这次运行的摘要里会显示它。请在 Cloudflare 的 Caching、Configuration、Custom Purge 里，按主机名手动清除缓存。

按主机名清除在每个 Cloudflare 套餐上都能用。

### 从 GitHub Actions 部署 {#deploy-from-github-actions}

在 `tifa.yaml` 的 `deploy` 部分加上 `auto: {}` 这个块，运行 `go tool tifa deploy setup`，然后提交并 push 它写出的工作流。setup 需要访问 GitHub（gh 已登录，或者有令牌，见下）和已经 push 到 GitHub 仓库的应用。之后 Actions 会构建应用并运行 `go tool tifa deploy`。

有两个工作流。`.github/workflows/ci.yml` 由 `tifa new`（或 `tifa agents`）写出，在 pull request 和 push 时运行 `go tool tifa check`。`.github/workflows/tifa-deploy.yml` 由 setup 写出：

- 它在往 `auto.branch`（不写就是仓库的默认分支）的 push 上，`auto.needs` 里的工作流（不写就是 `ci.yml`）通过之后部署，部署的是被检查过的那次提交。它只为这个仓库的 push 运行，从不为来自 fork 的 pull request 运行，也只在 `needs` 里写的那个文件之后运行：两个工作流可以同名，所以这个 job 还会检查刚结束的那次运行的路径（`github.event.workflow_run.path`）。
- setup 会读 `needs` 里的文件，遇到这些情况就拒绝继续：文件不存在，没有 `name:` 这一行（GitHub 的 `workflow_run` 靠名字找工作流），或者它不在往这个分支 push 时运行（它的 `on:` 里没有 `push`，或者 `branches`、`branches-ignore` 的过滤把这个分支排除在外）。方案会把它列为留给你的事，setup 以错误退出：修改那个文件，改 `needs`，或者写 `needs: none`。它从不在没有等你指定的工作流的情况下部署。
- 写了 `needs: none` 时，工作流在往这个分支 push 时运行，什么也不等。
- 不管哪个分支部署，GitHub 都只从默认分支上的文件启动 `workflow_run` 工作流，所以要先把文件合并到那里，才能指望它运行。`branch` 不是默认分支时，工作流那一行的方案会说明这一点。`push` 工作流（`needs: none`）则从被 push 的那个分支上的文件运行。
- 在 Actions 标签页里，Run workflow 提供 `deploy` 或 `rollback`，作用于你运行它的那个 ref。
- 同一时间只运行一个部署，正在运行的部署从不被取消。工作流只有 `contents: read` 权限。
- 不以 Tifa 那一行开头的文件是你的：setup 不会动它。在编辑文件之前，请先删掉那一行。
- 带页面的应用，仓库里需要有 `pnpm-lock.yaml`，CI 从它安装依赖。工作流用 `package.json` 里 `packageManager` 指定的 pnpm，没有时用 pnpm 10。

对仓库，setup 会：

1. 做一把只属于这个仓库的密钥，把它的公钥加到服务器上的 `/srv/tifa/.ssh/authorized_keys`，注释为 `tifa-github <owner>/<repo>`。这把密钥不能打开终端，也不能转发端口。请让 secret `TIFA_DEPLOY_SSH_KEY` 保持它被创建时的样子：要轮换密钥，把 `auto` 块去掉，运行 setup，再把它放回去，再运行一次 setup。
2. 通过你自己的 ssh 读取服务器的主机密钥，并保存为 secret `TIFA_DEPLOY_KNOWN_HOSTS`，这样 CI 信任这台服务器，而不信任别的。CI 从不运行 `ssh-keyscan`。
3. 把 `TIFA_DEPLOY_HOST`（CI 要登录的完整目标）设为 secret，这样工作流文件既不显示地址，也不显示端口。用 `connect: direct` 时，它是 `ssh://<address>:<port>`，取自 `ssh -G <your TIFA_DEPLOY_HOST>` 给出的内容，所以 `~/.ssh/config` 里的别名可以用，端口总是写出来，即使是 22。ssh 通过 `ProxyJump` 或 `ProxyCommand` 才能到达的主机，没法复制进 CI：请用下面两种方式之一。主机密钥也同样处理，端口不是 22 时前面加上 `[host]:port`。
4. 保存 `TIFA_DEPLOY_FINGERPRINT`，一个仓库变量，它把这些 secret 标记为 Tifa 的，并保存连接的指纹，这样第二次运行能看出什么是最新的。

用 `front: cloudflare` 时，CI 用 setup 存在服务器上的令牌清除 CDN 缓存，部署通过 ssh 读取它：仓库里没有 Cloudflare 令牌，也没有 zone ID。

**访问 GitHub。** setup 和 remove 会读取仓库，设置和删除它的 secret 和变量。gh 优先：它已安装并登录（`gh auth login`）时，一切都由 gh 来做。没有 gh 时，Tifa 用输入 `TIFA_DEPLOY_GITHUB_TOKEN`（环境变量，否则是 `.env`）里的细粒度个人访问令牌调用 GitHub REST API。仓库就是应用的 git remote（`origin`，否则是 github.com 上的另一个 remote）里的那个，它的默认分支来自 API。secret 的值在离开你的机器之前，会用仓库的公钥封装好，令牌只放在 `Authorization` 头里发送，从不出现在命令行上，也从不打印。

**没有 gh：用细粒度令牌。** 做一个只限于这一个仓库的令牌，带这些仓库权限：

| 权限 | 级别 | 为什么 |
| --- | --- | --- |
| Secrets | Read and write | 列出、设置和删除 secret |
| Variables | Read and write | 列出、设置和删除变量 `TIFA_DEPLOY_FINGERPRINT` |
| Metadata | Read | 随上面两个而来 |

不需要别的。把 `TIFA_DEPLOY_GITHUB_TOKEN=<token>` 放进 `.env`。当 gh 和令牌都用不了时，setup 会把 GitHub 这部分列在它做不了的事里，并打印一个链接，打开已经填好名字、所有者和这些权限的表单，`https://github.com/settings/personal-access-tokens/new?name=...&target_name=<owner>&secrets=write&actions_variables=write&metadata=read`（GitHub 的[模板 URL](https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/managing-your-personal-access-tokens)）。链接没法选择仓库：在 Repository access 下选 Only select repositories，再选这个仓库。GitHub 拒绝的令牌（401）或够不到仓库的令牌（404），会说明这一点并写出这些权限。

secret 的值通过 gh 的输入，或者封装后通过 API 交给 GitHub：它们从不打印，从不写进文件，也从不出现在命令行上。在一次运行的日志里，`tifa deploy` 和 `rollback` 先打印 `::add-mask::<host name or address>`，让 GitHub 把它隐藏，因为 ssh 的错误会打印它解析出的地址，而这并不是 secret 的原文。

#### connect: direct {#connect-direct}

默认。CI 登录到 `TIFA_DEPLOY_HOST` 的地址和端口。这个端口必须向 GitHub 的 runner 开放，它们的地址会变，所以你没法只允许其中一部分。请只允许密钥登录（`PasswordAuthentication no`）：仓库的密钥是唯一的入口。

#### connect: cloudflare {#connect-cloudflare}

用于不为 ssh 开任何端口的服务器，有没有 CI 都行。它需要 `front: cloudflare` 和 tunnel。CI 和你自己的机器都经由它连到服务器，所以一旦它能用，你就可以关掉服务器的 ssh 端口。Tifa 从不关它：请在一次经由 tunnel 的部署成功之后，自己去关。用你的 cf 登录，setup 会：

1. 先创建 Access 应用和它的策略，然后才往 tunnel 里添加路由 `<ssh host> -> ssh://localhost:22`，保留其他路由，并为它加一条被代理的 DNS 记录。当现在还没有任何东西能放行任何人时（cf 给不出邮箱而且没有 CI，或者策略建不出来），setup 不发布路由和记录，并说明原因：ssh 主机名绝不会对所有人开放。ssh 主机是域名的第一段后面加上 `-ssh-` 和 12 个随机字符，因为 Cloudflare 的免费证书只覆盖一级子域名：`deploy-a.example.com` 变成 `deploy-a-ssh-k3j9x2m5q7wt.example.com`，顶级域名 `example.com` 变成 `ssh-k3j9x2m5q7wt.example.com`。随机的部分让这个名字没法从域名猜出来。setup 只生成一次，之后的运行从 tunnel 的路由和它创建的 Access 应用里读回来，所以它保持不变，有没有 CI 都一样。没有随机部分的、更早版本的名字，会在方案里显示为替换：新的路由、记录和 Access 应用被添加，`TIFA_DEPLOY_HOST` 改变，旧的被移除。
2. 为 ssh 主机创建一个 self-hosted Access 应用，名字是 `tifa deploy ssh <ssh host>`，再创建一条策略 `tifa-people-<ssh host>`，放行你的 cf 登录的邮箱（用 `cf auth whoami` 读取，它只读）。只动这个应用和这条策略，zone 和账号里的别的东西都不碰。用同一个邮箱再运行，什么也不会变。另一个人用自己的 cf 登录运行 setup，会把他的邮箱加到同一条策略里，方案会说明：`change  cloudflare: the Access application of <host> admits ana@example.com, and keeps dev@example.com`。用 API 令牌登录的 cf 没有邮箱：没有 CI 时，这是一件留给你的事，有 CI 时只是一条提示。
3. 有 `auto` 块时，还会创建一个叫 `tifa-github-<owner>-<repo>` 的、不会过期的 service token，一条只放行这个令牌且不要求登录的策略，并把它挂到同一个应用上，排在放行人的策略之前。同一台服务器上的另一个仓库，会加上它自己的令牌和策略。更早版本设置过的应用保留它的应用和令牌：setup 只加上放行人的策略，不更新任何令牌。
4. 有 CI 时，设置 `TIFA_DEPLOY_CF_ACCESS_ID` 和 `TIFA_DEPLOY_CF_ACCESS_SECRET`，`TIFA_DEPLOY_HOST` 是 ssh 主机。

工作流从 cloudflared 的发布页面安装一个固定版本，并检查软件包的 SHA-256（apt 仓库只保留最新的，所以没法固定版本，而更新的 cloudflared 曾经不再读取 service token）。Tifa 只在经由 tunnel 的部署用这个版本通过之后，才提高版本。它用 `ProxyCommand cloudflared access ssh` 连接，把令牌放在 `TUNNEL_SERVICE_TOKEN_ID` 和 `TUNNEL_SERVICE_TOKEN_SECRET` 里交给它。

**setup 在你的机器上做的事**（方案里的事项，你输入 y 或加 `--yes` 后执行，不需要终端）：

1. PATH 里没有 cloudflared 时安装它：macOS 上用 `brew install cloudflared`，Windows 上用 `winget install --id Cloudflare.cloudflared -e`。在 Linux 上，或者没有 brew 和 winget 时，这一项会给出命令或[下载页面](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/downloads/)，setup 不会去尝试。setup 从不卸载 cloudflared。
2. 把下面这个块写在 `~/.ssh/config` 的最前面（Windows 上是 `%USERPROFILE%\.ssh\config`），文件不存在时用 600 权限创建它：

   ```
   # tifa shop deploy-a-ssh-k3j9x2m5q7wt.example.com: through the Cloudflare tunnel (written by tifa deploy setup)
   Host deploy-a-ssh-k3j9x2m5q7wt.example.com
     ProxyCommand /opt/homebrew/bin/cloudflared access ssh --hostname %h
     StrictHostKeyChecking accept-new
   Match all
   # end tifa shop deploy-a-ssh-k3j9x2m5q7wt.example.com
   ```

   文件里其他的每一行都原样保留，包括它的换行符（用 CRLF 的文件，块也用 CRLF）。块放在最前面，因为 ssh 采用它找到的第一个值，所以你写在后面的 `Host *` 换不掉这个 ProxyCommand。`Match all` 用来结束这个块，所以你放在最前面的 `Include` 或全局选项仍然是全局的。setup 写入 cloudflared 的完整路径，路径里有空格时加引号，在 Windows 上用正斜杠（`C:/Tools/cloudflared.exe`），Windows OpenSSH 和 Git for Windows 的 ssh 都读得懂。标记行写着应用名和 ssh 主机，并且要整行完全匹配：你自己写的、只是以 `# tifa` 开头的注释，仍然是你的。一个块开在另一个块里面、一直没有结束，或者它的 `Host` 行和第一行对不上，都是错误，会指出是哪一行，setup 什么也不写。加 `StrictHostKeyChecking accept-new` 是因为第一次经由 tunnel 的连接还没有已知的主机密钥，而没有终端时 ssh 没法询问。之后密钥变了会被拒绝。没有 `User` 这一行：用户由 Tifa 选，第一次设置用 root，之后用 `tifa`。以后再运行只会替换这一个块，`tifa deploy remove` 会删掉它。
3. 检查 `ssh tifa@<ssh host> true` 能通过这个块连上。**第一次，cloudflared 会在你的浏览器里打开 Cloudflare Access 的登录页，并等你**，最长 4 分钟。setup 会在开始前这样说明。cloudflared 也会打印登录地址，没有打开浏览器时，编码代理可以把它给你看。登录是唯一要你自己做的一步。你的公钥也必须在服务器上（Access 的登录让你连到 sshd，sshd 仍然会检查你的密钥：见[更多开发者](#more-developers)）。
4. 到这时才把 `TIFA_DEPLOY_HOST` 指向 ssh 主机。全新服务器的第一次 setup 仍然需要你给的地址，用来在 tunnel 存在之前以 root 登录，所以这一步放在最后。当这个值是应用 `.env` 里的一行时，setup 会改这一行（方案里显示 `TIFA_DEPLOY_HOST in .env, vps -> <ssh host>`，文件的其他行都保留），从那以后 `tifa deploy`、`logs`、`status` 和 `env` 都经由 tunnel。只有值会变：前面的 `export`、引号、后面的注释、换行符和结尾缺少的换行都保持原样。不管 `TIFA_DEPLOY_HOST` 来自哪里，这个检查都会运行。当值来自环境变量或参数 `--host` 时，setup 什么也不改，检查通过后会告诉你该设置什么。如果检查失败，什么也不会变，setup 会说明原因，下次运行再试。当它来自 `~/.tifa` 时，检查通过后 setup 自己把 ssh 主机记在那里。只有 ssh 走到了认证这一步（输出里有 `Permission denied`）时，setup 才会说你的密钥必须在 `/srv/tifa/.ssh/authorized_keys` 里。其他任何失败，都会显示 ssh 和 cloudflared 打印的内容，并说检查失败，不会怪到密钥头上。

如果你用 ssh 主机名连接服务器，而这台机器还没有设置过（另一个人，或一个新的检出目录），那么要先有这个块，setup 才能读到任何东西。setup 会先规划这些：Access 策略、cloudflared 和这个块，然后再就其余的事项询问。没有 cf 登录时，它会说先登录（在终端里它会替你登录 cf），并且不会去读服务器。

在 `TIFA_DEPLOY_HOST` 还是 ssh 主机名的时候把 `connect` 改回 `direct`，setup 会停下，给出一个事项，说要先把它设成服务器的地址（它的 ssh 端口必须对你开放）。再下一次 setup 会移除路由、记录、Access 应用、它的策略，以及你的 ssh 配置里的那个块。

#### connect: tailscale {#connect-tailscale}

适合已经在你的 tailnet 里、由 GitHub Actions 经它连上的服务器。setup 不碰你机器上的任何 Tailscale 设置，CI 时也只有 runner 加入 tailnet。它需要 `auto` 这个块，和哪种 `front` 都能一起用：访客可以经由 Cloudflare 来，而 ssh 走 tailnet。

Tailscale 要你自己在服务器上和你的 tailnet 里设置：Tifa 不安装也不改它的任何东西。`TIFA_DEPLOY_HOST` 必须是 runner 在 tailnet 里能连到的地址，比如服务器的 MagicDNS 名字（`ssh://vps.tail1234.ts.net:22`）。Tifa 像对待别的服务器一样用 ssh 连它，setup 会把同样的值连同端口复制到 CI 的 secret `TIFA_DEPLOY_HOST` 里。

**CI** 用 `tailscale/github-action@v4` 和工作负载身份联合（workload identity federation）加入 tailnet。GitHub 给 runner 一个短期令牌，说明它是哪个仓库，Tailscale 用它换一把密钥，让 runner 带着标签 `tag:ci` 加入。不存在长期有效的 Tailscale secret。工作流里有：

```yaml
permissions:
  contents: read
  id-token: write
...
      - name: Connect to Tailscale
        uses: tailscale/github-action@v4
        with:
          oauth-client-id: ${{ vars.TIFA_DEPLOY_TAILSCALE_ID }}
          audience: ${{ vars.TIFA_DEPLOY_TAILSCALE_AUDIENCE }}
          tags: tag:ci
```

客户端 ID 和 audience 不是秘密，所以 setup 把它们保存为仓库变量 `TIFA_DEPLOY_TAILSCALE_ID` 和 `TIFA_DEPLOY_TAILSCALE_AUDIENCE`。`id-token: write` 只是让这个 action 能向 GitHub 要那个令牌。只有 Tailscale 管理控制台能创建这个身份，因为 Tifa 没有能做这件事的 Tailscale 登录。setup 会说明在控制台里这样做：

1. 打开 [Trust credentials](https://console.tailscale.com/admin/settings/trust-credentials)，选 Credential，再选 OpenID Connect。
2. 选 GitHub 的 issuer（`https://token.actions.githubusercontent.com`），把 subject 设为 `repo:<owner>/<repo>:ref:refs/heads/<branch>`，其中的分支是部署 job 运行时所用的分支。在 `needs` 通过之后才启动的 job（`workflow_run`）的 ref 是仓库的默认分支，不管是哪个分支在部署，所以 subject 里写的就是默认分支。用了 `needs: none` 时，job 在 push 时启动，它的 ref 是 `auto.branch`。setup 会按你的设置打印 subject（[`workflow_run` 的 `GITHUB_REF`](https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows)）。在别的分支上手动运行会被拒绝。

   这是 2026 年 7 月 15 日之前创建的仓库的形式。在那天之后创建的仓库、选择了不可变 subject 声明的仓库，以及在那天之后改过名或转让过的仓库，GitHub 给的 subject 带着所有者和仓库的 ID：`repo:<owner>@<owner ID>/<repo>@<repository ID>:ref:refs/heads/<branch>`（[GitHub：不可变 subject 声明](https://docs.github.com/en/actions/reference/security/oidc)）。没有哪个字段能确切说明一个仓库会得到哪种形式，所以 setup 两种都打印。要看 GitHub 实际发的是哪一种，可以在工作流里运行 [`github/actions-oidc-debugger`](https://github.com/github/actions-oidc-debugger) 并读 `sub`，或者读 `GET /repos/<owner>/<repo>/actions/oidc/customization/sub` 里的 `use_immutable_subject`（[REST：OIDC](https://docs.github.com/en/rest/actions/oidc)），它说明仓库有没有选择加入。ID 可以用 `gh api repos/<owner>/<repo> --jq '.owner.id, .id'` 得到。Tailscale 允许 subject 里带 `*`，它匹配任意字符（[工作负载身份联合](https://tailscale.com/kb/1581/workload-identity-federation)），所以 `repo:<owner>/<repo>:*` 和它的 ID 形式会覆盖所有的 ref。但那也会放行仓库的每个分支和每个 pull request，Tifa 不依赖它：只有你接受这一点时才用。
3. 给它范围 `auth_keys`，对应标签 `tag:ci`，然后生成。
4. 复制它的客户端 ID，运行 `go tool tifa deploy setup --tailscale-client-id <client ID>`，或者在终端里等 setup 问的时候粘贴。客户端 ID 不是秘密，所以 agent 可以传它。audience 由它生成（`api.tailscale.com/<client ID>`）。

你的 tailnet 策略里，`tagOwners` 必须有 `tag:ci`，并让它能连到服务器的 ssh 端口。setup 读不到你的策略，所以没法检查这一点。如果某次运行没能加入，Tailscale 那一步的日志会说明（`requested tags [tag:ci] are invalid or not permitted` 表示标签或凭证的标签不对）。

在 Actions 里，设置了变量 `GITHUB_STEP_SUMMARY` 时，`tifa deploy` 和 `rollback` 会往运行页面写一份简短的摘要：应用、发布、提交、时间、健康检查，以及缓存有没有清除。

### Tifa 记住的服务器 {#the-servers-tifa-remembers}

setup 成功之后，Tifa 会把服务器写进 `~/.tifa/config.yaml`（Windows 上是 `%USERPROFILE%\.tifa\config.yaml`），所以这台机器上的下一个应用不再需要 `TIFA_DEPLOY_HOST`。这个文件里没有任何 secret，只有这台机器设置过的服务器：

```yaml
# Servers tifa deploy setup has set up from this machine. tifa deploy uses the only one when an app names none.
# No secrets are kept here. To forget a server, delete its entry.
servers:
  - id: vultr-1a2b3c4d
    host: ssh://203.0.113.7:22
    apps:
      - {name: shop, path: /Users/me/shop, host: 'ssh://203.0.113.7:22'}
      - {name: blog, path: /Users/me/blog, host: 'ssh://203.0.113.7:22'}
```

- `id` 是服务器的主机名加上它的机器 ID 的开头，setup 从服务器上读取机器 ID。服务器的连接方式变了，它的条目也还在。`host` 是新应用会用的那个：最近一次连通的。
- 一个应用就是它在这台机器上的检出文件夹（`path`），`name` 只用于提示信息。同名应用的另一个检出目录算新的应用。每个应用保留对它连通过的 host：用 `connect: cloudflare` 时，在经由 tunnel 的检查通过之后，它是应用域名的 ssh 主机名，否则是你给的地址。当服务器的 `host` 原本是被移除的那个应用的 host 时，`remove` 会把它换成留下来的某个应用的 host。
- 当一个应用没有指定服务器时（参数、环境变量和 `.env` 优先），Tifa 使用文件里唯一的那台服务器，并打印一行说明用的是哪一台，以及要指定另一台该在 `.env` 里放什么。文件夹列在某台服务器名下的应用，会去那台服务器，即使有好几台。有好几台服务器、而应用不在任何一台上时，命令会停下，列出它们，并说请在 `.env` 里设置 `TIFA_DEPLOY_HOST=<host>` 或传 `--host`。`setup` 和 `remove` 的方案会把这个文件显示为服务器的来源。
- 取自这个文件的 host，会在命令第一次向服务器询问时被检查：如果那里的机器不是条目里的那台（别名或地址现在指向了别处），命令会停下，并说出两个 ID 和这个 host。`tifa deploy logs` 不先询问任何东西，所以不做检查。
- `tifa deploy remove` 把应用从它的条目上拿掉，最后一个应用走了，条目也跟着走。只有当 remove 做完、没有留给你的事时才会这样做，所以重试会找到同一台服务器。
- `TIFA_HOME` 可以移动这个文件夹。文件权限是 0600，所在文件夹是 0700，通过临时文件加重命名写入，旁边的锁文件避免两个 Tifa 命令互相丢掉对方的条目。你加的注释会保留。同一台服务器（同一个 `id`）列了两次是错误，会指出这个文件。
- 想手动忘掉一台服务器，就删掉它的条目，或者删掉整个文件。没有别的东西依赖它。CI 从不读它：在 GitHub Actions 里，缺少 `TIFA_DEPLOY_HOST` 是一个关于 secret 的错误，工作流从那个 secret 得到它的服务器。

### 更多开发者 {#more-developers}

每台要部署的机器都需要它自己到服务器的访问权限，setup 不会把它带过去。在一台能登录的机器上，把新机器的公钥加到用户 `tifa` 上（用 `connect: cloudflare` 时，请用 ssh 主机名，它经由 tunnel）：

```sh
ssh-copy-id -i ~/.ssh/id_ed25519.pub tifa@vps
```

用 `connect: cloudflare` 时，新来的人还要用他自己的 cf 登录运行 `go tool tifa deploy setup`，并在他的 `.env` 里放 `TIFA_DEPLOY_HOST=<ssh host>`：setup 会把他的邮箱加进 Access 策略，安装 cloudflared，并写好他的 `~/.ssh/config` 里的块，第一次 ssh 会在他的浏览器里打开 Access 的登录页。ssh 主机名是第一个人的 `.env` 里的那个，或者他的 setup 方案里打印出来的那个。新机器还需要知道服务器在哪里：把 `TIFA_DEPLOY_HOST=` 放进它的检出目录的 `.env`（或者传 `--host`，或者运行一次 setup，它会把服务器记在 `~/.tifa` 里），在没有 gh 的情况下运行 setup 时，也要在那里放 `TIFA_DEPLOY_GITHUB_TOKEN=`，如果这个值是别名，还要在它的 `~/.ssh/config` 里有同样的别名。机器上不保存别的东西：`tifa.yaml` 在仓库里，其余的在服务器、Cloudflare 和 GitHub 上。

### 服务器上的设置和 secret {#settings-and-secrets-on-the-server}

```sh
printf %s "$VALUE" | go tool tifa deploy env set API_KEY   # or on a terminal for a hidden prompt, or else a page
go tool tifa deploy env unset API_KEY
go tool tifa deploy env list                               # names only
```

这些命令通过 ssh 以 `tifa` 身份编辑 `/srv/tifa/<app>/shared/.env`，也就是应用从它运行所在文件夹里读的那个文件（见 [.env](config.zh-CN.md#env)）。值在终端里从隐藏的提示读取，否则从标准输入的管道或文件读取（空的输入会把值设为空），从不从参数读取，所以它不会出现在你的 shell 历史和进程列表里。什么也不会打印值。文件保持 600 权限，你的其他行和注释保持原样。改动之后，应用通过它的 socket 重启，并且必须通过部署的健康检查。如果没通过，旧文件会恢复，应用再重启一次，命令会说明这一点，并以 1 退出。这个文件只作为配置被应用读取：它不会放进进程或 systemd 的环境里，所以服务器上的每个应用保留它自己的。

没有终端、管道或文件时，比如编码 agent 通常给的 `/dev/null`，`env set` 会在一个页面上询问（见下）。

### 没有终端时的 secret {#secrets-without-a-terminal}

编码 agent 在没有终端的情况下运行命令，而 secret 不该经过它：不经过它的聊天、它的输入和它的输出。所以 Tifa 在你浏览器里的一个页面上请你输入 secret：

- `tifa deploy env set NAME`，当它的标准输入既不是终端，也不是管道或文件时。
- `tifa deploy setup --cache-purge-token`，加了 `--yes` 也一样。这个参数就是请求：没有它时，没有终端的 setup 会把令牌列在你要做的事里，附上命令，并且从不等待。

Tifa 打开页面并打印它的 URL，`http://127.0.0.1:<port>/<code>`，这样没有浏览器打开时，agent 可以把它转告你。页面说明需要什么，并链接到你去哪里创建它。页面只在这台机器上监听，只在 URL 里那个随机码处回答，只接收一个值，然后关闭。10 分钟内没有人提交时，命令以错误停止：再运行一次，得到新的页面。

### 日志、状态和移除 {#logs-status-and-remove}

```sh
go tool tifa deploy logs        # the last 100 lines of the log, -n 500 for more
go tool tifa deploy logs -f     # and then the new ones, until Ctrl-C
go tool tifa deploy status
go tool tifa deploy remove
```

它们通过 ssh 以 `tifa` 身份登录，找服务器的方式和 `tifa deploy` 一样。

`logs` 打印 `tifa-<app>.service` 和 `tifa-<app>.socket` 的 journal：应用打印了什么，以及 systemd 说了它什么，比如一次启动、退出或失败。journald 把 journal 保存在磁盘上时（Ubuntu 和 Debian 的默认做法），这些行保存在用户 `tifa` 的 journal 文件里。journal 只保存在内存里的服务器，会让 `tifa` 看不到任何行。应用记什么日志、格式和级别怎么设、journald 把这些行保留多久，见[日志](../guides/logs.zh-CN.md)。

`status` 打印正在运行的发布和它的提交、保留的其他发布和它们各自有没有通过健康检查、服务从什么时候开始运行、`GET /healthz` 现在的回答，以及程序的内存：

```
shop on vps, port 9527
  release  20261120T120000Z  commit ccc333        passed its health check
  kept     20261120T110000Z  commit bbb222-dirty  failed its health check
           20261120T100000Z  commit aaa111        passed its health check
  service  active since 2026-11-20 12:00:05 UTC, 2h3m ago
  health   GET /healthz answered 200
  memory   38.7 MB
```

它只在服务运行时才请求 `/healthz`，因为向 socket 发请求会把它启动起来。

`remove` 把应用从服务器上拿掉，收回 Tifa 为它做的东西，别的不动：

- 用 `front: nginx` 时：以 root 删除应用在 nginx 里的站点（重新加载之前先 `nginx -t`）、它在 certbot 里的证书，以及最后一个用这个 nginx 的应用被删时，还有 nginx、certbot（setup 安装过的话）、它们的配置、`/etc/tifa/nginx` 和注释为 `tifa nginx front` 的 ufw 规则（见[在 nginx 后面](#behind-nginx)）。站点拿不掉时，证书和 nginx 都留着。其他应用继续回答。
- 在服务器上，以 `tifa` 身份：它停止并禁用 `tifa-<app>.socket`、`tifa-<app>-http.socket` 和 `.service`，删除它们的单元文件和 `/srv/tifa/<app>`，包括它的发布、`shared/.env`、`shared/` 里的数据、`certs/` 里的证书和 `deploys.log`。想保留数据的话，请先复制出来。对自己接待访客的应用，还以 root 删除 `/etc/sysctl.d/60-tifa-ports.conf`，用户能打开的最小端口改回原来的值，有 ufw 时还删掉 setup 加的、带注释 `tifa <app>` 的规则，计划里会列出它们。其他应用、用户 `tifa`、防火墙的其他规则和 sshd 保持原样。
- 用 `front: cloudflare` 时：域名的路由、DNS 记录和规则，用 `connect: cloudflare` 时（有没有 CI 都一样），还有 ssh 主机名的 Access 应用和它放行人的策略、它的路由和 DNS 记录，以及仓库的 service token 和策略。所有需要 ssh 的事先做（应用、缓存令牌），然后停掉 tunnel，最后才移除 ssh 主机名：先移除路由和记录，再移除应用和它的策略，所以部分失败也绝不会留下一条没有 Access 保护的路由，而且只在前面的步骤成功时才这样做。当应用已经没了时，再运行一次会从 tunnel 的路由里找到 ssh 主机名。当你的 ssh 经由 tunnel 时，服务器上最后一个应用会在 ssh 离开几秒之后才停掉 tunnel，所以命令不会切断它自己的连接。tunnel 和 cloudflared 服务服务器上的每个应用，所以只有服务器上最后一个 Tifa 应用移除时它们才会一起走：那时 remove 以 root 停止 cloudflared，删除 `/etc/tifa`，在 cloudflared 是 setup 装的时候卸载它，并删除 tunnel。
- 服务器上的缓存令牌，`/srv/tifa/.config/tifa`，也服务服务器上的每个应用，随服务器上最后一个 Tifa 应用一起走。remove 在删除时会在那个文件的锁下再找一次应用，所以在 remove 等你回答时刚设置好的应用，会保住它们。Cloudflare 控制台里的令牌 `tifa cache purge <zone>` 请你自己删：Tifa 做不到。
- 有 `auto` 块时：`authorized_keys` 里仓库的密钥，setup 设置的 secret 和变量（`TIFA_DEPLOY_FINGERPRINT` 和 Tailscale 身份的那两个），以及旧标记担保的旧名字。身份本身在你的 Tailscale 控制台里：请在那里删掉它。`.github/workflows/tifa-deploy.yml` 是你仓库里的文件，所以它留下：在你删除它之前，每次 push 都会部署失败。
- 在你的机器上：应用在 `~/.ssh/config` 里的块，最后才删，这样中途停下的一次运行仍然能连到服务器。没有 cf 登录、而你是用 ssh 主机名连到服务器时，这个块会留下，remove 会说明。cloudflared 仍然装着，`.env` 里的 `TIFA_DEPLOY_HOST` 也保持原样。

和 setup 一样，remove 先打印一份方案。在终端里，它接着要你输入应用的名字，加了 `--yes` 也一样。没有终端时，除非你传 `--yes`，否则它什么也不移除。没有 cf 登录、gh 或令牌就移除不了的东西，会列在最后，带上它需要的登录。它移除其余的，并以错误退出：登录后再运行一次。某一步失败不会阻止其他步骤，但 tunnel 会留着，直到再运行一次把其余的移除。一次运行发现什么也没剩下时，会用一行说明。

### 在服务器上 {#on-the-server}

- 应用的日志：`go tool tifa deploy logs`，或者以 `tifa` 身份（`sudo -iu tifa`）运行 `journalctl --user -u tifa-<app>`。
- 启动、停止或查看它：以 `tifa` 身份运行 `systemctl --user status tifa-<app>.service`。重启服务，永远不要重启 socket。
- 应用在开机时启动，早于第一个访客。
- 多个应用：每个都有自己的文件夹、端口和单元。在一次 `tifa deploy setup` 之后，在每个应用里运行 `tifa deploy`。其中只有一个能自己在 80 和 443 端口上接待访客，其他的放到 Cloudflare Tunnel 或 nginx 后面。

本页的其余部分是手动做这些事的方式，以及 `tifa deploy` 在底层做了什么。

## 为服务器构建 {#build-for-the-server}

服务端渲染引擎能在 linux 和 darwin 上运行（amd64 和 arm64），也能在 amd64 和 arm64 的 windows 上运行。在你的机器上或 CI 里设好目标再构建，然后把文件复制过去。不要在 1 GB VPS 这样的小服务器上构建：构建需要的内存比运行中的应用多，可能会内存不足。

```sh
GOOS=linux GOARCH=amd64 $(go tool -n tifa) build -o bin/myapp-linux
scp bin/myapp-linux server:/srv/myapp/myapp
```

`GOOS=linux go tool tifa build` 不行，原因见[为另一个系统构建](build.zh-CN.md#building-for-another-system)。程序里带有几种许可证下的代码，所以交给别人时，要把声明一起带上（见[随程序一起发布的声明](build.zh-CN.md#notices-to-ship-with-the-program)）。

## 运行它 {#run-it}

```sh
SERVER_ADDR=0.0.0.0:9527 ./myapp
```

- 每项设置都有一个参数和一个环境变量，程序还会读它工作文件夹里的 `.env` 文件和用 `-c` 指定的文件。[配置](config.zh-CN.md)里有完整的列表，以及哪个来源优先。`./myapp config` 会打印每项设置的值和它的来源，`./myapp version` 打印版本。
- 通过 `PORT` 告诉程序端口的平台，比如 Cloud Run 或 Fly，不需要更多设置就能用（见 [PORT 和 HOST](config.zh-CN.md#port-and-host)）。
- 从它保存数据的文件夹里运行它。相对路径，比如 `tifa new` 模板的 `app.db`（设置 `db.path`），是相对工作目录解析的。请备份那个文件夹，或者在容器里把它挂载出来。
- SIGINT 和 SIGTERM 会让它优雅地停止：新连接停止，正在处理的请求有 `server.shutdown_timeout` 的时间来完成，然后 `OnShutdown` 运行。自己没有终点的流，比如 server-sent events，在监视 `tifa.ShuttingDown` 时会立刻返回（见[流式传输](../guides/streaming.zh-CN.md#when-the-program-stops)）。systemd 单元或容器只需要可执行文件、环境和一个可写的数据文件夹。
- `FROM scratch` 的容器能用。程序里带着它的渲染需要的时区数据。
- 用[性能](../reference/performance.zh-CN.md#sizing-a-machine)来给机器定规格。`ssr.vms` 跟随容器的 CPU 限制，最多 4 个 VM。CPU 更多时，请自己设置。

## 重启而不丢请求 {#restart-without-dropping-requests}

重启会让程序停下，下一个程序在监听之前先预热它的 VM。在这期间到来的连接会被拒绝，除非 systemd 持有 socket：用 socket 激活时，systemd 监听端口，把 socket 交给程序，程序重启期间也保持它打开。系统接下来收下这期间到来的连接，由下一个程序回答它们。访客只是多等一会儿：在 M4 Mac 上，示例 shop 的程序用 10 个 VM 启动后，0.42 秒就回答第一个页面，用 `ssr.vms: 1` 是 0.27 秒。构建之后的第一次启动，文件还不在系统缓存里，用了 0.79 秒。在装着 systemd 261 的 Linux 机器上，`systemctl restart` 运行期间，一个小应用每 10 毫秒收到的 300 个请求全部以 200 回答，最慢的用了 0.14 秒。

只有当会话保存在程序之外时，用户才会在重启之后保持登录。默认的存储在内存里，所以每次重启都会让所有人退出登录（见[会话存放在哪里](../pages-and-data/sessions.zh-CN.md#where-to-store-sessions)）。从 `tifa new` 得到的应用把它们放在自己的 SQLite 数据库里（`sessions.go`）。

两个单元，用于代理后面的应用，或者通过普通 HTTP 提供服务的应用：

```ini
# /etc/systemd/system/shop.socket
[Unit]
Description=Shop socket

[Socket]
ListenStream=0.0.0.0:9527

[Install]
WantedBy=sockets.target
```

```ini
# /etc/systemd/system/shop.service
[Unit]
Description=Shop
StartLimitIntervalSec=0
Requires=shop.socket
After=shop.socket

[Service]
User=shop
WorkingDirectory=/var/lib/shop
ExecStart=/srv/shop/shop
Restart=on-failure
RestartSec=2s

[Install]
WantedBy=multi-user.target
```

```sh
sudo systemctl daemon-reload
sudo systemctl enable --now shop.socket shop.service
# each deploy: copy the new program, then
sudo systemctl restart shop.service
```

- 重启服务，不要重启 socket。重启或停止 `shop.socket` 会关掉端口。
- `StartLimitIntervalSec=0` 让 systemd 在程序一直失败时也不停地重启它。没有它，systemd 会在 10 秒内启动 5 次之后停手，把服务留在 failed 状态，于是一个能让程序反复崩溃的请求就能把站打停。`tifa deploy` 写的单元就是这样的。
- `shop.service` 也是启用的，所以程序在开机时就启动并预热，而不是等第一个访客。
- 程序从 systemd 设置的 `LISTEN_PID`、`LISTEN_FDS` 和 `LISTEN_FDNAMES` 读取 socket，并把它们从自己的环境里去掉，所以它启动的程序看不到它们。这时不会监听 `server.addr`。日志行里写的是 `socket=systemd`。没有这些变量时，程序像以前一样自己监听。
- systemd 以 root 身份打开端口，所以 1024 以下的端口不需要 `setcap` 或 `AmbientCapabilities`。
- 用默认的 `server.shutdown_timeout`，一次停止最多需要大约 30 秒：10 秒给请求，再 10 秒给取消后仍在运行的请求，再 10 秒给 `OnShutdown`。systemd 等 90 秒（`TimeoutStopSec`）才杀掉程序。

开启 HTTPS 时，程序服务两个地址，每个 socket 用它的名字说明是哪一个。一个 socket 单元会给它所有的 socket 同一个名字，所以每个地址用一个单元：

```ini
# /etc/systemd/system/shop-https.socket
[Socket]
ListenStream=443
FileDescriptorName=server.addr
Service=shop.service

[Install]
WantedBy=sockets.target
```

```ini
# /etc/systemd/system/shop-http.socket
[Socket]
ListenStream=80
FileDescriptorName=server.http_addr
Service=shop.service

[Install]
WantedBy=sockets.target
```

在 `shop.service` 里，`Requires=` 和 `After=` 写出两个单元，`[Service]` 里加上 `Sockets=shop-https.socket shop-http.socket`。`server.addr` 可以不设：只有程序自己监听地址时，[HTTPS](config.zh-CN.md#https) 才要求设置它。名为 `server.addr` 或 `server.http_addr` 的 socket 服务那项设置，名字是别的或者没有名字的单个 socket 服务 `server.addr`。有多个时，必须分别命名为 `server.addr` 和 `server.http_addr`，没有开 HTTPS 却有给 `server.http_addr` 的 socket，会让程序报错停止。systemd 没有传入 socket 的地址，程序自己监听。

## 端口和 HTTPS {#ports-and-https}

- 程序可以自己从 Let's Encrypt 获取证书，也可以用你的：[HTTPS](config.zh-CN.md#https)。
- 80 和 443 端口在 Linux 上需要一项权限，或者用一个从高端口发布它们的容器：[1024 以下的端口](config.zh-CN.md#ports-below-1024)。
- 在终结 TLS 的 CDN 或代理后面，保持 HTTPS 关闭，并监听 `0.0.0.0:<port>`。

## 在 CDN 或代理后面 {#behind-a-cdn-or-proxy}

- **用 `front: nginx` 时**，这些设置由 Tifa 替你写（见[在 nginx 后面](#behind-nginx)）。本节其余部分是给你自己的代理或 CDN 的。
- **Host 和 Origin。** 让 `Host` 和 `Origin` 按浏览器发送的样子通过。程序通过比较它们，来拒绝浏览器从另一个站点发来的写入，而改写 `Host` 的代理会让你自己的表单看起来像外来的（403，带 `cross-origin request detected`）。见[跨站写入](config.zh-CN.md#cross-site-writes)。
- **客户端地址。** Tifa 不读 `Forwarded` 或 `X-Forwarded-For`。`c.Request().RemoteAddr` 是代理的地址。如果 `OnRequest` 需要访客的地址，请读你自己的代理设置的头，并且只在请求确实来自那个代理时才读。 `front: nginx` 的 nginx 会把 `X-Forwarded-For` 设成访客的地址。
- **压缩。** 程序自己压缩文本响应。当 CDN 或代理做压缩时（比如 brotli），把 `server.compress` 设为 `false`。CloudFront、Cloudflare 和 Google Cloud CDN 都不会动已经带 `Content-Encoding` 的响应。见[压缩](build.zh-CN.md#compression)。
- **缓存。** 页面、导航数据和处理函数的响应默认是 `private, no-cache`，所以 CDN 不会保存它们。请保持这样。让 CDN 缓存这些：
  - `/assets/`：内容带哈希，`public, max-age=31536000, immutable`。
  - `public/` 里的文件：`no-cache` 加 ETag，所以 CDN 来问，得到 304。它们的名字是固定的，所以只有需要固定名字的文件才属于那里。其余的请导入，得到 `/assets/`（见[静态文件](../start/app.zh-CN.md#static-files)）。
  - 带 `Cache` 的页面：它们是匿名的，带 `public` 的 `Cache-Control` 和一个 `s-maxage`（确切的值见 [响应的 Cache-Control](../pages-and-data/ssr-and-cache.zh-CN.md#cache-control-of-an-answer)）。遵循源站 `Cache-Control` 的 CDN，会把页面保存 `Cache` 的时间，不会更久。见[给 CDN 用的缓存](../pages-and-data/ssr-and-cache.zh-CN.md#cache-for-cdns)。
- **不要让 CDN 忽略源站的缓存头。** 用遵循它们的缓存设置，绝不要用强制一个时间或缓存一切的设置。每个 CDN 的设置在 [CDN 设置](#cdn-settings)里。
- **带 `Cache` 的页面的导航数据不共享。** 它是页面 URL 加 `?_tifa=...`，带着浏览器的会话，并且始终是 `private, no-cache`。缓存键必须包含查询字符串（至少 `_tifa`），否则 CDN 必须对带 `_tifa` 的请求跳过缓存。只对页面的 HTML 按路径丢弃 cookie，绝不对带 `_tifa` 的同一路径这样做。
- **不要缓存 `/__tifa/session`。** 它按访客回答，是 `private, no-store`。
- **不要自己设置 `Cache-Control: public`**，不管是在其他页面上，还是在回答某个页面 URL 的处理函数上。带有每个用户数据的页面默认是安全的。
- **在 HTTPS 上，压缩可能通过响应的长度泄露秘密**（BREACH），当响应重复了请求里的某些内容，同时又带着 CSRF 令牌这样的秘密时。对这样的响应设置头 `No-Gzip-Compression`，或者把 `server.compress` 关掉。
- **用了 `server.csp`，CDN 写进页面的脚本不会运行。** 它的哈希不在 Tifa 随页面发送的策略里。请关掉会改动 HTML 的功能，比如 Cloudflare 的 Rocket Loader 和 Email Address Obfuscation。见 [Content-Security-Policy](config.zh-CN.md#content-security-policy)。

### CDN 设置 {#cdn-settings}

已在 2026-11-04 对照各厂商自己的文档核对过。名字和默认值会变，所以如果某项设置找不到，请打开链接的页面。

**CloudFront**

- **Host 和 Origin。** CloudFront 把源站的域名作为 `Host` 发送，而不是浏览器用的那个（[请求头](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/RequestAndResponseBehaviorCustomOrigin.html)）。请附上一个转发访客请求头的 origin request policy，比如托管的 `AllViewer`。不要用会丢掉 `Host` 的 `AllViewerExceptHostHeader`（[托管的 origin request policy](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/using-managed-origin-request-policies.html)）。
- **页面的缓存策略。** 不要用托管的 `CachingOptimized`：它的最小 TTL 是 1 秒，CloudFront 那时即使响应写着 `private`、`no-cache` 或 `no-store` 也会缓存它（[托管的缓存策略](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/using-managed-cache-policies.html)）。最小 TTL 为 0 时，CloudFront 会遵守这些指令，并把 `s-maxage` 当作页面的保存时间（[过期](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/Expiration.html)）。托管的 `UseOriginCacheControlHeaders` 的最小 TTL 是 0，但它把所有 cookie 放进缓存键，所以带 `Cache` 的页面不会共享。请做你自己的缓存策略：最小 TTL 为 0，不带 cookie，缓存键里带所有查询字符串（或至少 `_tifa`）。
- **客户端地址。** CloudFront 把连接的地址追加到 `X-Forwarded-For`。在它之前的内容来自访客，不能信任。`CloudFront-Viewer-Address` 只有在 origin request policy 包含它时才会到达，比如 `AllViewerAndCloudFrontHeaders-2022-06`（[客户端 IP 地址](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/RequestAndResponseBehaviorCustomOrigin.html#RequestCustomIPAddresses)）。
- **压缩。** 在开启 "Compress objects automatically" 之前是关的。CloudFront 只压缩列出的内容类型，大小在 1,000 到 10,000,000 字节之间，只在源站响应带 `Content-Length` 时压缩，响应带 `Content-Encoding` 时绝不压缩（[提供压缩文件](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/ServingCompressedFiles.html)）。
- 支持 `stale-if-error`，并受最大 TTL 的限制（[过期](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/Expiration.html#stale-content)）。

**Cloudflare**

`tifa deploy setup` 带 cf 时，会自己为它的域名添加这些规则（见[按方案设置](#set-up-with-a-plan)）。

- **缓存。** HTML 和 JSON 默认不缓存，因为 Cloudflare 按文件扩展名来决定（[默认缓存行为](https://developers.cloudflare.com/cache/concepts/default-cache-behavior/)）。要缓存带 `Cache` 的页面，请为它们的路径添加一条 Cache Rule，选 "Eligible for cache"，并把 Edge TTL 设为 "Use cache-control header if present, bypass cache if not"。绝不要选 "Ignore cache-control header and use this TTL"（[缓存规则设置](https://developers.cloudflare.com/cache/how-to/cache-rules/settings/)）。在同一条规则里把 Browser TTL 设为 "Respect origin TTL"：否则 zone 的 Browser Cache TTL（默认 4 小时），会在浏览器拿到的内容里替换掉带 `Cache` 的页面的 `max-age=0` 和 public 文件的 `no-cache`，浏览器就会把它们保存那么久，任何清除都够不到那里。
- **`private` 和 `no-store`** 从不缓存。`s-maxage` 会被遵守。文档没有说明 `s-maxage=N` 和 `max-age=0`（带 `Cache` 的页面发的就是这个）放在一起时怎么工作，所以请检查这样的页面第二次请求是不是缓存命中（[Cache-Control 指令](https://developers.cloudflare.com/cache/concepts/cache-control/)）。`stale-if-error` 对 5xx 响应有效。
- **缓存键。** 默认的键包含协议、主机和带查询字符串的 URI，所以导航数据（`?_tifa=...`）和页面是不同的条目（[缓存键](https://developers.cloudflare.com/cache/how-to/cache-keys/)）。
- **Host。** 不要用 Origin Rule 的 "Override Host header" 来改写它（仅限 Enterprise 套餐）（[Origin Rules](https://developers.cloudflare.com/rules/origin-rules/)）。
- **客户端地址。** `CF-Connecting-IP` 和 `X-Forwarded-For`。`True-Client-IP` 是给 Enterprise 套餐的，默认关闭（[HTTP 头](https://developers.cloudflare.com/fundamentals/reference/http-headers/)）。
- **压缩。** Cloudflare 用 gzip、Brotli 和 Zstandard 压缩，源站发送了访客接受的 `Content-Encoding` 时，保留源站的编码（[压缩](https://developers.cloudflare.com/speed/optimization/content/compression/)）。

**Google Cloud CDN**

- **缓存模式。** 用 `USE_ORIGIN_HEADERS`。默认的 `CACHE_ALL_STATIC` 也会把 `private` 和 `no-store` 的响应挡在外面。不要用 `FORCE_CACHE_ALL`：它会忽略 `private` 和 `no-store`，可能缓存单个用户的页面（[缓存模式](https://docs.cloud.google.com/cdn/docs/using-cache-modes)）。
- **头。** `private` 和 `no-store` 不缓存，`s-maxage` 优先于 `max-age`，带 `Set-Cookie` 的响应不缓存，`no-cache` 会缓存，但每次都要向源站检查。`stale-if-error` 作为响应头没有效果（[缓存](https://docs.cloud.google.com/cdn/docs/caching)）。
- **缓存键。** 对于后端服务，默认的键是带查询字符串的完整 URI。不要设置会漏掉 `_tifa` 的查询字符串包含或排除列表（[缓存](https://docs.cloud.google.com/cdn/docs/caching)）。
- **Host 和客户端地址。** 负载均衡器保留请求的 `Host`。它把 `<client address>,<load balancer address>` 追加到 `X-Forwarded-For`，并且不检查在那之前有什么（[外部 Application Load Balancer](https://docs.cloud.google.com/load-balancing/docs/https)）。
- **压缩。** 动态压缩默认关闭，需要 `Content-Length`，并且会跳过带 `Content-Encoding` 的响应（[动态压缩](https://docs.cloud.google.com/cdn/docs/dynamic-compression)）。

## 健康检查和指标 {#health-checks-and-metrics}

Tifa 自己没有健康检查路由或指标路由。把你需要的作为 API 路由加上。

### 健康检查 {#health-check}

一个 `GET`，程序能提供服务时回答 204。检查它离了就没法工作的东西，比如[数据库指南](../guides/database.zh-CN.md)里的数据库，它挂了就回答 503：

```go
// app/healthz/route.go
package healthz

func GET(c *tifa.Ctx) error {
	if err := notes.DB.PingContext(c.Context()); err != nil {
		return tifa.Error{Status: 503, Message: "database unreachable"}
	}
	return c.NoContent(204)
}
```

- `tifa deploy` 在每次部署之后请求 `/healthz`，如果它在 30 秒内没有以低于 500 的状态回答，就回滚（见[部署](#deploy)）。
- 把负载均衡器、容器的 `HEALTHCHECK` 或 Kubernetes 探针指向 `/healthz`。程序只有在 `OnStart` 完成、VM 都加载好之后才监听，所以第一个 204 就表示它能渲染。被告知停止时，新连接会失败，检查就变红。
- 这个路由什么也不渲染，所以满了的渲染队列不会让它失败，繁忙的程序也不会因为忙而被重启。要看这一点，请看下面指标里的 `tifa_renders_rejected_total`。

### 指标 {#metrics}

`tifa.Stats()` 返回 VM 池的状态、渲染记忆 memo，以及它自程序启动以来做了什么。计数是原子的，读取条目数和 VM 数需要一把很短的锁，所以每隔几秒读一次，花销小到测不出来。在 `tifa dev` 下，每次重新构建前端，VM 池的计数都会从零重新开始。这个路由用 Prometheus 读取的文本格式把它写出来，不用任何库：

```go
// app/metrics/route.go
package metrics

import (
	"fmt"
	"io"
	"strings"

	"github.com/liustack/tifa"
)

func GET(c *tifa.Ctx) error {
	s := tifa.Stats()
	var b strings.Builder
	metric := func(name, kind, help string, value float64) {
		fmt.Fprintf(&b, "# HELP %s %s\n# TYPE %s %s\n%s %g\n", name, help, name, kind, name, value)
	}
	metric("tifa_vms", "gauge", "VMs loaded.", float64(s.SSR.VMs))
	metric("tifa_vms_busy", "gauge", "VMs rendering.", float64(s.SSR.Busy))
	metric("tifa_renders_queued", "gauge", "Renders waiting for a VM.", float64(s.SSR.Queued))
	metric("tifa_renders_total", "counter", "Renders that ran in a VM.", float64(s.SSR.Renders))
	metric("tifa_renders_failed_total", "counter", "Renders that failed.", float64(s.SSR.Failed))
	metric("tifa_render_seconds_total", "counter", "Time the renders took.", s.SSR.RenderTime.Seconds())
	metric("tifa_renders_rejected_total", "counter", "Renders answered 503 as busy.", float64(s.SSR.Rejected))
	metric("tifa_vms_replaced_total", "counter", "VMs replaced because they grew.", float64(s.SSR.Replaced))
	metric("tifa_vms_memory_bytes", "gauge", "Bytes the VMs hold after their last GC.", float64(s.SSR.Memory))
	metric("tifa_memo_entries", "gauge", "Entries kept.", float64(s.Memo.Entries))
	metric("tifa_memo_hits_total", "counter", "Answered from an entry.", float64(s.Memo.Hits))
	metric("tifa_memo_misses_total", "counter", "Rendered by the request.", float64(s.Memo.Misses))
	metric("tifa_memo_shared_total", "counter", "Waited for another request.", float64(s.Memo.Shared))
	metric("tifa_memo_evictions_total", "counter", "Dropped to make room for another.", float64(s.Memo.Evictions))
	metric("tifa_memo_bytes", "gauge", "Bytes the render memo holds.", float64(s.MemoBytes))
	metric("tifa_memo_size_bytes", "gauge", "Bytes the render memo may hold.", float64(s.MemoSize))
	c.SetHeader("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, err := io.WriteString(c.Writer(), b.String())
	return err
}
```

- 已经在用 `client_golang` 的应用，可以改写一个 `prometheus.Collector`，它的 `Collect` 读一次 `tifa.Stats()`，用 `prometheus.MustNewConstMetric` 发送每个值。
- 这些数字不说明访客的情况，但能说明程序有多忙。不要把这个路由暴露给公众：让前面的代理拒绝来自外部的 `/metrics`，或者在处理函数里检查一个令牌。
- 服务器启动之前和停止之后，`tifa.Stats()` 返回零。没有页面的应用没有 VM。每次程序启动，计数都从零开始，Prometheus 把这读作一次重启。

读什么，改什么：

| 你看到什么 | 它意味着什么 | 该怎么做 |
| --- | --- | --- |
| `tifa_renders_rejected_total` 在增长 | 渲染发现队列满了，或者等得超过了 `ssr.wait_timeout`，得到 503 | 增加 CPU 和 `ssr.vms`，或者减少渲染（见[给机器定规格](../reference/performance.zh-CN.md#sizing-a-machine)和[渲染队列](config.zh-CN.md#render-queue)） |
| `tifa_vms_busy` 大部分时间等于 `tifa_vms`，`tifa_renders_queued` 大于 0 | 每个 VM 都在渲染，别的在等 | 同上：程序到了它的极限 |
| `rate(tifa_render_seconds_total[5m]) / rate(tifa_renders_total[5m])` | 一次渲染的平均时间 | 它增长时，找出变慢的那个页面 |
| `tifa_renders_failed_total` 在增长 | 渲染抛错、超过 `ssr.timeout` 或用光 `ssr.vm_memory` | 日志里有每一次失败 |
| `tifa_vms_memory_bytes` 一直在涨，或者 `tifa_vms_replaced_total` 在增长 | VM 在每次回收之后占用得更多。占用超过 `ssr.vm_memory` 一半的 VM 会被换掉 | 找出页面保留了什么，或者应用里的泄漏。如果 VM 在几分钟之内又被换掉，请调高 `ssr.vm_memory`（见[内存](../reference/performance.zh-CN.md#memory)） |
| 命中数低于未命中数，`tifa_memo_evictions_total` 在增长，`tifa_memo_bytes` 接近 `tifa_memo_size_bytes` | 渲染记忆 memo 在页面被再次请求之前就把它们丢掉了 | 调大 `ssr.memo_size` |

## 部署之后 {#after-a-deploy}

- 打开着页面的访客可以继续使用：下一次点击或处理函数调用会看到新构建，浏览器会把页面重新加载一次。见[部署期间打开的页面](deploy.zh-CN.md#pages-open-while-you-deploy)。
- 如果你的 `OnRequest` 或 CDN 规则让 CDN 保存了带 `Cache` 的页面之外的页面，请在每次部署时清除它们。否则访客可能拿到脚本已经没了的旧页面。
- 保存带 `Cache` 页面的 CDN，在 `s-maxage` 到期之前会发出旧的 HTML，而那个 HTML 里写的脚本可能已经没了。用 `front: cloudflare` 时，只要有令牌或 cf 登录，`tifa deploy` 会替你清除这个主机名（见[清除 CDN 缓存](#clearing-the-cdn-cache)）。没有它们时，旧页面会留到 `s-maxage` 到期，这是用 CDN 公认的代价。在别的 CDN 上，请在部署时清除这些页面。直接向程序请求的浏览器会立刻得到新页面。
- 程序的渲染记忆 memo 放在内存里，重启后从空的开始。

## 部署期间打开的页面 {#pages-open-while-you-deploy}

访客可以在你部署新版本时一直开着一个页面，这时页面来自旧构建，而服务器是新的。页面会把它渲染时用的构建，随它的数据请求（放在 `_tifa` 查询参数里）和处理函数调用（`X-Tifa-Build`）一起发送，服务器对别的构建回答 409。访客看到的是：

- 点击链接：浏览器重新加载当前页面，由新构建显示它。不管有多少个请求得到 409，都只重新加载一次。
- 调用处理函数：调用以状态 409 的 `TifaError` 失败，浏览器重新加载页面。调用在处理函数运行之前就被拒绝，也不会再次发送，因为新页面可能对它的输入有别的含义。访客在新页面上重新做一次。你自己的处理函数返回的 409 没有 `X-Tifa-Build` 头，什么也不会重新加载。没有这个头的调用（来自 curl 或别的程序）不会被检查。
- 加载代码：懒加载的路由或 `import()` 请求了新构建里已经没有的分块，浏览器重新加载页面（这是 Vite 的 `vite:preloadError`）。

每个构建只重新加载一次。当它带回来的页面仍然和服务器对不上时（缓存又把旧页面发出来就会这样），它不会重新加载第二次。数据请求带着 409 失败，由路由的 `ErrorBoundary` 显示它，调用把它抛出来，失败的 import 则交给路由的 `ErrorBoundary` 或导入它的代码。标签页把重新加载记在 `sessionStorage` 里，页面只有在写下标记并读回来之后才会重新加载。浏览器屏蔽存储，或者不保留写进去的内容时，页面不会重新加载：上面的失败照原样显示，带着 "the application was updated, reload the page" 这条消息，访客手动重新加载。无法被记住的重新加载，在构建一直对不上的期间可能会重复下去。

HTML 是旧的、而入口脚本已经没了的页面，页面自己没法修复：没有任何代码在运行。Tifa 发送页面时带 `Cache-Control: private, no-cache`，所以 CDN 不会保存它们，这种情况只会发生在你的 `OnRequest` 让 CDN 缓存页面的时候。那么请在部署时清除 CDN 里的这些页面。
