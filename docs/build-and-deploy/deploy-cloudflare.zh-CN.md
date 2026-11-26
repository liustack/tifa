---
summary: '用 `connect: cloudflare` 通过 Cloudflare Tunnel 部署，并在部署之后清除 CDN 缓存'
audience: app
order: 13
translatedFrom: c664b57c9eb0
read_when:
  - '用 cf 把应用放到 Cloudflare Tunnel 后面，或者想知道 tifa deploy setup 在 Cloudflare 里改了什么、怎样撤销'
  - '某次部署提示 CDN 缓存没有清除，或者你想添加或替换清除缓存用的令牌（setup 把它保存在服务器上）'
  - '在自己的机器上用 `connect: cloudflare` 通过 Cloudflare Tunnel 部署，这样服务器不需要开放 ssh 端口，或者某次部署在浏览器里等 Cloudflare Access 登录'
---

# 通过 Cloudflare 部署 {#deploy-through-cloudflare}

用 Cloudflare 时，服务器不需要开放 ssh 端口，部署也能清除你的主机名的缓存。这一篇讲这两件事。

## connect: cloudflare {#connect-cloudflare}

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
3. 检查 `ssh tifa@<ssh host> true` 能通过这个块连上。**第一次，cloudflared 会在你的浏览器里打开 Cloudflare Access 的登录页，并等你**，最长 4 分钟。setup 会在开始前这样说明。cloudflared 也会打印登录地址，没有打开浏览器时，编码代理可以把它给你看。登录是唯一要你自己做的一步。你的公钥也必须在服务器上（Access 的登录让你连到 sshd，sshd 仍然会检查你的密钥：见[更多开发者](deploy.zh-CN.md#more-developers)）。
4. 到这时才把 `TIFA_DEPLOY_HOST` 指向 ssh 主机。全新服务器的第一次 setup 仍然需要你给的地址，用来在 tunnel 存在之前以 root 登录，所以这一步放在最后。当这个值是应用 `.env` 里的一行时，setup 会改这一行（方案里显示 `TIFA_DEPLOY_HOST in .env, vps -> <ssh host>`，文件的其他行都保留），从那以后 `tifa deploy`、`logs`、`status` 和 `env` 都经由 tunnel。只有值会变：前面的 `export`、引号、后面的注释、换行符和结尾缺少的换行都保持原样。不管 `TIFA_DEPLOY_HOST` 来自哪里，这个检查都会运行。当值来自环境变量或参数 `--host` 时，setup 什么也不改，检查通过后会告诉你该设置什么。如果检查失败，什么也不会变，setup 会说明原因，下次运行再试。当它来自 `~/.tifa` 时，检查通过后 setup 自己把 ssh 主机记在那里。只有 ssh 走到了认证这一步（输出里有 `Permission denied`）时，setup 才会说你的密钥必须在 `/srv/tifa/.ssh/authorized_keys` 里。其他任何失败，都会显示 ssh 和 cloudflared 打印的内容，并说检查失败，不会怪到密钥头上。

如果你用 ssh 主机名连接服务器，而这台机器还没有设置过（另一个人，或一个新的检出目录），那么要先有这个块，setup 才能读到任何东西。setup 会先规划这些：Access 策略、cloudflared 和这个块，然后再就其余的事项询问。没有 cf 登录时，它会说先登录（在终端里它会替你登录 cf），并且不会去读服务器。

在 `TIFA_DEPLOY_HOST` 还是 ssh 主机名的时候把 `connect` 改回 `direct`，setup 会停下，给出一个事项，说要先把它设成服务器的地址（它的 ssh 端口必须对你开放）。再下一次 setup 会移除路由、记录、Access 应用、它的策略，以及你的 ssh 配置里的那个块。

## 清除 CDN 缓存 {#clearing-the-cdn-cache}

用 `front: cloudflare` 时，部署和回滚会清除 Cloudflare 里那个主机名的缓存，按主机名，不会更宽：zone 里的其余部分保留它的缓存。否则带 `Cache` 的页面会一直显示旧的 HTML，而它的脚本已经没了，直到它的 `s-maxage` 到期。

清除是可选的。没有任何 CDN 和任何框架能让每个访客在新构建上线的那一刻就看到它：没清除的缓存会保留它的副本，直到副本到期。没有办法清除时，带 `Cache` 的页面在部署之后，最长可能有它的 `Cache` 时间那么旧，而那份旧副本可能会请求新构建里已经没有的脚本。这是用 CDN 的代价，Tifa 在每次部署时都会这样说明，但不把它当成错误。

部署会用它找到的第一种办法清除缓存，在你的机器上或在 CI 里都一样：

1. 输入 `TIFA_DEPLOY_CF_PURGE_TOKEN`，一个只能清除那一个 zone 的缓存的令牌，和 `TIFA_DEPLOY_CF_ZONE_ID`，zone 的 ID：环境变量，否则是应用根目录的 `.env`。它们优先于服务器上的那个。也要给出 zone ID，因为只能清除缓存的令牌通常没法读取 zone 来找到它。设置了令牌而没有设置 zone ID 时，部署会在它的警告里说明。setup 不会把它们写进 `.env`：请自己放进去。
2. setup 存在服务器上的令牌（见[按方案设置](deploy-setup.zh-CN.md#set-up-with-a-plan)）：`/srv/tifa/.config/tifa/cloudflare-purge`，每个 zone 一行，写着 zone 的名字、它的 ID 和令牌。Tifa 把令牌放在你的服务器上，只有 `tifa` 能读：文件属于 `tifa`，权限是 600，所在文件夹的权限是 700。新发布通过检查之后，部署通过它用来部署的 ssh，以 `tifa` 身份读取它。令牌只放在内存里，从不打印，不写进日志，也不放在命令行上。所以来自 CI 的部署，以及来自任何能以 `tifa` 登录的机器的部署，都会清除缓存，一个令牌服务服务器上这个 zone 的每个应用。主机和 zone 的名字不分大小写地匹配。写这个文件时会持有它旁边的一把锁（`cloudflare-purge.lock`，用 `flock`），所以同一台服务器上的两次 setup 会保留彼此的那一行。文件读不了时，部署用一行说明，然后换下一种办法。应用也以 `tifa` 运行，所以它们的代码能读这个文件：这个令牌只能清除这个 zone 的缓存。
3. 在你的机器上，cf 用你的登录。

三种都没有时，部署会打印 `cache: not cleared for <host>`，说明这样的代价，并说明运行 `go tool tifa deploy setup --cache-purge-token`，它不是警告。
- 清除失败不会让部署失败：它打印一条警告，以 0 退出，这次运行的摘要里会显示它。请在 Cloudflare 的 Caching、Configuration、Custom Purge 里，按主机名手动清除缓存。

按主机名清除在每个 Cloudflare 套餐上都能用。
