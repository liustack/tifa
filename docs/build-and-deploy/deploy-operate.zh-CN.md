---
summary: '管理已部署的应用：`tifa deploy env`、没有终端时设置密钥、`logs`、`status`、`remove`，以及应用在服务器上把文件放在哪里'
audience: app
order: 15
translatedFrom: 9458d988ee39
read_when:
  - '用 tifa deploy env 设置已部署应用 .env 里的密钥或设置'
  - 'agent 运行 tifa deploy env set，或带 --cache-purge-token 的 tifa deploy setup，浏览器里打开了一个页面用来输入密钥'
  - '读取已部署应用的日志、检查哪个版本在运行以及它是否健康，或者用 tifa deploy logs、status 或 remove 把应用从服务器、Cloudflare 和 GitHub 上拿掉'
  - '一台服务器上放多个应用，或者找已部署应用的日志、.env 或数据'
---

# 管理已部署的应用 {#run-a-deployed-app}

设置密钥，读日志，查看运行的是哪个版本，把应用从服务器上拿掉，以及找到应用在服务器上的文件。

## 服务器上的设置和 secret {#settings-and-secrets-on-the-server}

```sh
printf %s "$VALUE" | go tool tifa deploy env set API_KEY   # or on a terminal for a hidden prompt, or else a page
go tool tifa deploy env unset API_KEY
go tool tifa deploy env list                               # names only
```

这些命令通过 ssh 以 `tifa` 身份编辑 `/srv/tifa/<app>/shared/.env`，也就是应用从它运行所在文件夹里读的那个文件（见 [.env](config.zh-CN.md#env)）。值在终端里从隐藏的提示读取，否则从标准输入的管道或文件读取（空的输入会把值设为空），从不从参数读取，所以它不会出现在你的 shell 历史和进程列表里。什么也不会打印值。文件保持 600 权限，你的其他行和注释保持原样。改动之后，应用通过它的 socket 重启，并且必须通过部署的健康检查。如果没通过，旧文件会恢复，应用再重启一次，命令会说明这一点，并以 1 退出。这个文件只作为配置被应用读取：它不会放进进程或 systemd 的环境里，所以服务器上的每个应用保留它自己的。

没有终端、管道或文件时，比如编码 agent 通常给的 `/dev/null`，`env set` 会在一个页面上询问（见下）。

## 没有终端时的 secret {#secrets-without-a-terminal}

编码 agent 在没有终端的情况下运行命令，而 secret 不该经过它：不经过它的聊天、它的输入和它的输出。所以 Tifa 在你浏览器里的一个页面上请你输入 secret：

- `tifa deploy env set NAME`，当它的标准输入既不是终端，也不是管道或文件时。
- `tifa deploy setup --cache-purge-token`，加了 `--yes` 也一样。这个参数就是请求：没有它时，没有终端的 setup 会把令牌列在你要做的事里，附上命令，并且从不等待。

Tifa 打开页面并打印它的 URL，`http://127.0.0.1:<port>/<code>`，这样没有浏览器打开时，agent 可以把它转告你。页面说明需要什么，并链接到你去哪里创建它。页面只在这台机器上监听，只在 URL 里那个随机码处回答，只接收一个值，然后关闭。10 分钟内没有人提交时，命令以错误停止：再运行一次，得到新的页面。

## 日志、状态和移除 {#logs-status-and-remove}

```sh
go tool tifa deploy logs        # the last 100 lines of the log, -n 500 for more
go tool tifa deploy logs -f     # and then the new ones, until Ctrl-C
go tool tifa deploy status
go tool tifa deploy status --all   # every server of ~/.tifa/config.yaml, once
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

`status --all` 不需要应用的检出目录。它对 `~/.tifa/config.yaml` 里的每台服务器看一次，并打印每台的系统、CPU 数、内存、磁盘、运行时长和负载，然后是用户 `tifa` 在这台服务器上的每个应用，从另一台机器设置的应用也算。每个应用显示发布和它的提交、有没有通过健康检查、服务，以及 systemd 自己重启过它几次、`GET /healthz` 现在的回答和程序的内存。没有发布的应用会说它还没部署。

```
vps
  system    Ubuntu 24.04.1 LTS, x86_64, 2 CPUs
  memory    2.1 GB total, 1.0 GB available
  disk      12.3 GB used of 25.0 GB (49%)
  uptime    up 12d 6h, load 0.10 0.20 0.30

  shop
    release   20261120T120000Z  commit ccc333  passed its health check
    service   active since 2026-11-20 12:00:05 UTC, 2h3m ago, 0 restarts
    health    GET /healthz answered 200
    memory    38.7 MB

Only the servers this machine has set up or deployed to, the ones in ~/.tifa/config.yaml, are shown.
```

- 服务器并行询问，按文件里的顺序打印，一台服务器上的应用按名字排序。不回答的服务器会打印它的错误，其他服务器照常打印。现在回答自己是另一台机器的主机，会被报告成这样，它的应用不显示。
- 每台服务器都回答，而且每个已部署的应用都在运行并通过健康检查时，退出状态是 0。否则会先打印全部内容，再以 1 退出，错误信息会说有几台服务器没能检查、有几个应用状态不好。脚本或 agent 可以依赖这一点。
- `--json` 把同样的内容作为一个 JSON 文档打印到 stdout：一个服务器列表。每台服务器有 `name`、`id`、`host`、`reached`、`error`（没连上时）和 `apps`，连上时还有 `facts`：`os`、`arch`、`cpus`、`memoryTotalKiB`、`memoryAvailableKiB`、`diskTotalKiB`、`diskUsedKiB`（`tifa` 的家目录所在的磁盘）、`uptimeSeconds`、`load1`、`load5` 和 `load15`。服务器没说的数字是 0。每个应用有 `name`、`host`、`deployed`、`release`、`commit`、`checkPassed`、`service`、`activeSince`、`restarts`、`health`（`GET /healthz` 的状态码，服务没运行时为空，没有回答时为 `000`）、`memoryKiB`、`ok` 和 `error`。
- 它只看某一个时刻，不是监控：不刷新、不报警、不留历史。它只覆盖那个文件里的服务器，也就是这台机器设置过或部署过的服务器。要报警，请用服务器之外的探测，比如 UptimeRobot 或 Cloudflare 的健康检查，因为服务器上的探测会和服务器一起挂掉。
- `--all` 不接受 `--host` 和 `--dir`。

`remove` 把应用从服务器上拿掉，收回 Tifa 为它做的东西，别的不动：

- 用 `front: nginx` 时：以 root 删除应用在 nginx 里的站点（重新加载之前先 `nginx -t`）、它在 certbot 里的证书，以及最后一个用这个 nginx 的应用被删时，还有 nginx、certbot（setup 安装过的话）、它们的配置、`/etc/tifa/nginx` 和注释为 `tifa nginx front` 的 ufw 规则（见[在 nginx 后面](deploy-proxy.zh-CN.md#behind-nginx)）。站点拿不掉时，证书和 nginx 都留着。其他应用继续回答。
- 在服务器上，以 `tifa` 身份：它停止并禁用 `tifa-<app>.socket`、`tifa-<app>-http.socket` 和 `.service`，删除它们的单元文件和 `/srv/tifa/<app>`，包括它的发布、`shared/.env`、`shared/` 里的数据、`certs/` 里的证书和 `deploys.log`。想保留数据的话，请先复制出来。对自己接待访客的应用，还以 root 删除 `/etc/sysctl.d/60-tifa-ports.conf`，用户能打开的最小端口改回原来的值，有 ufw 时还删掉 setup 加的、带注释 `tifa <app>` 的规则，计划里会列出它们。其他应用、用户 `tifa`、防火墙的其他规则和 sshd 保持原样。
- 用 `front: cloudflare` 时：域名的路由、DNS 记录和规则，用 `connect: cloudflare` 时（有没有 CI 都一样），还有 ssh 主机名的 Access 应用和它放行人的策略、它的路由和 DNS 记录，以及仓库的 service token 和策略。所有需要 ssh 的事先做（应用、缓存令牌），然后停掉 tunnel，最后才移除 ssh 主机名：先移除路由和记录，再移除应用和它的策略，所以部分失败也绝不会留下一条没有 Access 保护的路由，而且只在前面的步骤成功时才这样做。当应用已经没了时，再运行一次会从 tunnel 的路由里找到 ssh 主机名。当你的 ssh 经由 tunnel 时，服务器上最后一个应用会在 ssh 离开几秒之后才停掉 tunnel，所以命令不会切断它自己的连接。tunnel 和 cloudflared 服务服务器上的每个应用，所以只有服务器上最后一个 Tifa 应用移除时它们才会一起走：那时 remove 以 root 停止 cloudflared，删除 `/etc/tifa`，在 cloudflared 是 setup 装的时候卸载它，并删除 tunnel。
- 服务器上的缓存令牌，`/srv/tifa/.config/tifa`，也服务服务器上的每个应用，随服务器上最后一个 Tifa 应用一起走。remove 在删除时会在那个文件的锁下再找一次应用，所以在 remove 等你回答时刚设置好的应用，会保住它们。Cloudflare 控制台里的令牌 `tifa cache purge <zone>` 请你自己删：Tifa 做不到。
- 有 `auto` 块时：`authorized_keys` 里仓库的密钥，setup 设置的 secret 和变量（`TIFA_DEPLOY_FINGERPRINT` 和 Tailscale 身份的那两个），以及旧标记担保的旧名字。身份本身在你的 Tailscale 控制台里：请在那里删掉它。`.github/workflows/tifa-deploy.yml` 是你仓库里的文件，所以它留下：在你删除它之前，每次 push 都会部署失败。
- 在你的机器上：应用在 `~/.ssh/config` 里的块，最后才删，这样中途停下的一次运行仍然能连到服务器。没有 cf 登录、而你是用 ssh 主机名连到服务器时，这个块会留下，remove 会说明。cloudflared 仍然装着，`.env` 里的 `TIFA_DEPLOY_HOST` 也保持原样。

和 setup 一样，remove 先打印一份方案。在终端里，它接着要你输入应用的名字，加了 `--yes` 也一样。没有终端时，除非你传 `--yes`，否则它什么也不移除。没有 cf 登录、gh 或令牌就移除不了的东西，会列在最后，带上它需要的登录。它移除其余的，并以错误退出：登录后再运行一次。某一步失败不会阻止其他步骤，但 tunnel 会留着，直到再运行一次把其余的移除。一次运行发现什么也没剩下时，会用一行说明。

## 在服务器上 {#on-the-server}

- 应用的日志：`go tool tifa deploy logs`，或者以 `tifa` 身份（`sudo -iu tifa`）运行 `journalctl --user -u tifa-<app>`。
- 启动、停止或查看它：以 `tifa` 身份运行 `systemctl --user status tifa-<app>.service`。重启服务，永远不要重启 socket。
- 应用在开机时启动，早于第一个访客。
- 多个应用：每个都有自己的文件夹、端口和单元。在一次 `tifa deploy setup` 之后，在每个应用里运行 `tifa deploy`。其中只有一个能自己在 80 和 443 端口上接待访客，其他的放到 Cloudflare Tunnel 或 nginx 后面。

[手动部署](deploy-by-hand.zh-CN.md)讲手动做这些事的方式，以及 `tifa deploy` 在底层做了什么。
