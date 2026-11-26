---
summary: '在 CI 通过之后从 GitHub Actions 部署：`deploy.auto`、分支和工作流，以及 runner 怎样用 `connect: direct` 或 `tailscale` 连到服务器'
audience: app
order: 12
translatedFrom: 0f5b709402bd
read_when:
  - '在 CI 之后用 GitHub Actions 部署，选择由哪个分支部署或它等待哪个工作流（`auto: branch` 和 `needs`），或者收回 CI 用来部署的密钥'
  - '用 `connect: tailscale` 从 CI 部署，创建运行器用来加入你的 tailnet 的 Tailscale 身份，或者某次 CI 运行加入不了 tailnet'
---

# 从 GitHub Actions 部署 {#deploy-from-github-actions}

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

## connect: direct {#connect-direct}

默认。CI 登录到 `TIFA_DEPLOY_HOST` 的地址和端口。这个端口必须向 GitHub 的 runner 开放，它们的地址会变，所以你没法只允许其中一部分。请只允许密钥登录（`PasswordAuthentication no`）：仓库的密钥是唯一的入口。

## connect: tailscale {#connect-tailscale}

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
