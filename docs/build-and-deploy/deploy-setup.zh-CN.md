---
summary: '用 `tifa deploy setup` 配好服务器、Cloudflare 和 GitHub：计划、每种 `front` 改了什么、在没有 gh 的机器上运行 setup'
audience: app
order: 11
translatedFrom: 6585206c4e54
read_when:
  - '在没有 gh 的机器上运行 tifa deploy setup 或 remove，用 TIFA_DEPLOY_GITHUB_TOKEN 里的细粒度令牌'
  - '`front: none` 的已部署应用从外面访问不到，setup 因为 80 或 443 端口被占而停下，你想知道要开放哪些端口，或者想知道 setup 和 remove 在 ufw 里改了什么'
---

# 配好服务器 {#set-up-the-server}

`tifa deploy setup` 让服务器、Cloudflare 和 GitHub 和 `tifa.yaml` 一致。它先打印一份计划，你同意之前什么都不改。

## 按方案设置 {#set-up-with-a-plan}

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
| front: nginx | 以 root 用 apt 安装的 nginx，有域名的应用还有 certbot 和它的 nginx 插件，用来标记这个 nginx 是 Tifa 的 `/etc/tifa/nginx`，应用在 `/etc/nginx/conf.d/tifa-<app>.conf` 里的站点，它的证书，以及 ufw 里的 80 和 443 端口（见[在 nginx 后面](deploy-proxy.zh-CN.md#behind-nginx)） | `ssh root@<TIFA_DEPLOY_HOST>` |
| ci | 仓库的一把密钥、secret（CI 在哪里登录、主机密钥），以及工作流 | gh 或 `TIFA_DEPLOY_GITHUB_TOKEN` |
| connect: cloudflare | tunnel 上的一个 ssh 主机名、一个放行你的邮箱的 Access 应用（用 CI 时还有一个 service token），以及你机器上的 cloudflared 和 `~/.ssh/config` 里的一个块 | cf |

`<app>` 是 `go.mod` 里模块路径的最后一段，也就是 `tifa build` 给程序起的名字。在服务器上以 `tifa` 或 root 填好 `.env`：应用每次启动时都会读它。

**Cloudflare Tunnel**（`front: cloudflare`）。服务器不开任何端口：服务器上的 cloudflared 主动连出到 Cloudflare，一个 tunnel 服务服务器上的每个应用。setup 用你的 cf 登录。Tifa 用到的 Cloudflare 令牌都留在你的服务器上：tunnel 的令牌只从 cf 到服务器，清除缓存的令牌只从你的粘贴到服务器。

1. 在你的账号里找到这个域名的 zone。
2. 服务器还没有 tunnel 时，创建服务器的 tunnel，名字是 `tifa-<host name>-<start of its machine ID>`，并把它的 ID 保存在 `/etc/tifa/tunnel`。
3. 服务器上没有 cloudflared 时安装它，用 apt 从 [Cloudflare 的软件包仓库](https://pkg.cloudflare.com/)安装：apt 会用 Cloudflare 的签名密钥检查软件包，设置文件 `/etc/apt/apt.conf.d/52tifa-cloudflared` 让 unattended-upgrades 安装新版本。Tifa 自己不下载任何程序：在没有 apt 的服务器上，请先用你系统的软件包安装 cloudflared。把 cloudflared 作为系统服务 `tifa-tunnel` 运行，令牌放在 `/etc/tifa/tunnel-token`（仅 root 可读），作为 systemd credential 交给它，cloudflared 变化时重启它。
4. 往 tunnel 里添加路由 `<domain> -> http://127.0.0.1:<port>`，保留其他应用的路由。
5. 添加一条被代理的 CNAME 记录，把域名指向 `<tunnel ID>.cfargotunnel.com`。域名上已经有指向别处的记录时，不会动它，setup 会停下。
6. 添加只对这个主机名生效的规则，如 [CDN 设置](deploy-proxy.zh-CN.md#cdn-settings)所说：一条缓存规则，缓存应用的 `Cache-Control` 允许缓存的内容，没有它的就不缓存，一条缓存规则，永远不缓存 `/__tifa/` 或导航数据（`?_tifa=`），还有一条配置规则，关掉 Rocket Loader、Email Address Obfuscation、Cloudflare Fonts、Zaraz 和 Web Analytics 脚本，它们都会往 HTML 里写东西。整个 zone 的设置不会改动。
7. 请求一个令牌，这是可选的，它只能清除 zone 的缓存，并把它和 zone 的 ID 一起保存在服务器上，每次部署都能在那里找到它（见[清除 CDN 缓存](deploy-cloudflare.zh-CN.md#clearing-the-cdn-cache)）。cf 的登录做不了 API 令牌，所以你要在 Cloudflare 控制台里做，在终端里 setup 会带你走一遍：它打印一个链接，指向 Cloudflare 的表单，已经填好了 Cache Purge 权限和名字 `tifa cache purge <zone>`。把 Zone Resources 设为只包含这个 zone，创建令牌，并在隐藏的提示处粘贴它。setup 通过清除一个不存在的 URL 来检查它，最多重试 3 次，然后通过 ssh 以 `tifa` 身份，用 ssh 的输入把它写到服务器上。输入空行会跳过它，setup 继续并成功。setup 在这个域名第一次设置时询问，在第一次设置 CI 时询问。服务器上已经有这个 zone 的令牌后，setup 每次运行都用同样的方式检查它，只要 Cloudflare 接受就什么也不问：同一台服务器、同一个 zone 上的第二个应用什么也不用做。Cloudflare 拒绝的令牌，比如已过期或已被删除的，会再问一次。没有令牌时，之后带 `auto` 的运行，以及没有终端的第一次运行，都把它列为可选。以后要加这个令牌或替换它，请运行 `go tool tifa deploy setup --cache-purge-token`：即使服务器上已经有一个，它也会问，在终端里用隐藏的提示，没有终端时用浏览器里的一个页面，加了 `--yes` 也一样（见[没有终端时的 secret](deploy-operate.zh-CN.md#secrets-without-a-terminal)）。

对服务器上最后一个应用运行 `tifa deploy remove`，也会删掉 tunnel 和缓存令牌（见[日志、状态和移除](deploy-operate.zh-CN.md#logs-status-and-remove)）。要手动移除它们，运行 `systemctl disable --now tifa-tunnel tifa-tunnel-upgrade.path`，并删除服务器上的 `/etc/tifa`、`/etc/systemd/system/tifa-tunnel*` 和 `/srv/tifa/.config/tifa`，然后在 Cloudflare 里删除 tunnel 和令牌 `tifa cache purge <zone>`。cloudflared 仍然是装着的：`apt-get purge cloudflared` 再删除 `/etc/apt/sources.list.d/cloudflared.list`、`/usr/share/keyrings/cloudflare-main.gpg` 和 `/etc/apt/apt.conf.d/52tifa-cloudflared`，也能把它移除。

### 应用自己接待访客 {#the-app-serves-visitors-itself}

用 `front: none` 时，程序直接面对访客，前面没有 nginx、Caddy 或 certbot：

- **有域名时**，也就是 `site.url` 的主机，比如 `https://shop.example.com`：systemd 为应用打开 443 和 80 端口，应用运行它[内置的 HTTPS](https.zh-CN.md#https)。setup 把 `SERVER_TLS_DOMAINS=<domain>` 和 `SERVER_TLS_CACHE=%h/<app>/certs` 写进服务的环境，这样程序从 Let's Encrypt 获取证书，并保存在 `/srv/tifa/<app>/certs`，每个发布都共用它。80 端口回答 Let's Encrypt 的检查，其他请求都转到 HTTPS。第一次部署之前，DNS 必须把域名指向这台服务器：程序拿到证书之前不回答 HTTPS，部署的健康检查会失败，并说明原因。
- **没有域名时**：没有 `site.url`，或者它的主机是 IP 地址或 `localhost`。应用在 80 端口上提供普通 HTTP。

端口由 systemd 拿着，所以应用以 `tifa` 身份运行，不需要 root，也不需要任何 capability，重启时也不拒绝连接。在 Linux 上，用户的 systemd 只能打开 1024 起的端口，所以 setup 每台服务器以 root 做一次：把 `net.ipv4.ip_unprivileged_port_start = 80` 写到 `/etc/sysctl.d/60-tifa-ports.conf`。之后 80 到 1023 的端口对服务器上的每个用户都开放，不再只有 root 能用。对这个应用运行 `tifa deploy remove` 会删掉这个文件，并改回原来的值。

**一台服务器一个应用。** 一个端口只能有一个程序监听，所以一台服务器上只能有一个应用自己接待访客。setup 在改动任何东西之前，先看应用需要的端口（80，有域名时还有 443）上有谁在监听：`ss -ltnp`，以及 `tifa` 的 systemd 拿着的 socket。端口被 Tifa 的另一个应用占着，或者被 nginx、Caddy、Apache 之类的程序占着时，setup 会停下，说出是谁、哪个端口，什么也不改。请停掉那个程序并让它开机不再启动，或者移除另一个应用，或者给这个应用和另一个应用都用 `front: cloudflare` 或 `front: nginx`，那样多个应用可以共用一台服务器（见[在 nginx 后面](deploy-proxy.zh-CN.md#behind-nginx)）。

**ufw。** 如果 ufw 开着，并且不让访客访问应用需要的端口（80 和 443，没有域名时是 80），setup 的计划会放行它们：

```
  change  firewall: allow ports 80 and 443 (tcp) in ufw, so visitors reach shop, in rules with the comment 'tifa shop', as root
```

你同意之后，或者带了 `--yes`，setup 以 root 为每个端口运行 `ufw allow 80/tcp comment 'tifa <app>'`。以下情况 ufw 里什么都不改：ufw 关着或没装，你自己的规则已经放行了这个端口，或者它的默认策略放行所有连接。你自己拒绝这个端口的规则会保留：setup 会说是哪条，以及怎样删掉它。注释 `tifa <app>` 标明这些规则是 Tifa 加的：`tifa deploy remove` 会删掉它们，应用不再在某个端口上接待访客时（比如改成 `front: cloudflare` 之后），setup 也会删掉。其他规则一概不碰，ssh 的规则也不碰。只有 root 能读 ufw 的规则：root 登录不了时，setup 会告诉你改去服务器上查什么。

**服务商的防火墙归你管。** Tifa 不改服务商的防火墙和安全组，也不改服务器上不是 ufw 的防火墙（firewalld，或者不是 ufw 写的 nftables、iptables 规则）。计划会说访客需要哪些端口，setup 结束时也会再说一遍：如果它们挡住了这些端口，请在那里开放。每次部署之后，Tifa 从你的机器访问应用的这些端口，有端口没回答时会告诉你。

用更早版本设置的服务器，应用在回环地址上。下一次 `tifa deploy setup` 会把搬迁列进计划：先以 root 设置端口，再换上新的 socket，之后应用在新 socket 上重启，并且必须通过和部署一样的健康检查。

如果有 CDN，按 [CDN 设置](deploy-proxy.zh-CN.md#cdn-settings)设置它，并按[部署之后](deploy-by-hand.zh-CN.md#after-a-deploy)所说，在每次部署时清除它的缓存。
