---
summary: '把一个应用放在你的负载均衡器后面的几台服务器上：服务器列表、setup、逐台部署、其他命令，以及应用要放在共享位置的东西'
audience: app
order: 23
translatedFrom: f83c72e0a9f2
read_when:
  - '一台服务器不够用了，或者一台服务器宕机不能让整个站点停掉'
  - '在 TIFA_DEPLOY_HOST 或 --host 里写几台服务器，或者命令因为 deploy.front 是 none、nginx 或 cloudflare 而拒绝了几台服务器'
  - '部署到几台服务器时在其中一台失败，把其他服务器挪了回去，或者几台服务器运行的版本不一样'
  - '应用在不止一台服务器上运行时，决定会话、数据、上传的文件和后台任务放在哪里'
---

# 在几台服务器上运行应用 {#run-an-app-on-several-servers}

一台服务器是应用的起点。需要更多时，`tifa deploy` 把同一个应用放在几台服务器上，前面是你在服务商那里建的负载均衡器。每台服务器运行一份程序，部署一台接一台地进行，一台服务器宕机时站点照常可用。

应用要把共享的东西放在每台服务器都能访问到的地方：加第二台服务器之前，先看[应用要做的事](#what-the-app-must-do)。

## 设置 {#the-settings}

几台服务器需要 `front: external`，也就是前面有一个你自己的负载均衡器（见[在你的负载均衡器后面](deploy-proxy.zh-CN.md#behind-your-load-balancer)）：

```yaml
deploy:
  front: external
```

服务器的写法和一台时一样，几台之间用逗号隔开，按[用 tifa deploy 部署](deploy.zh-CN.md#deploy-with-tifa-deploy)里的顺序，先找到的为准：

```sh
# .env
TIFA_DEPLOY_HOST=vps1,ssh://203.0.113.8:2222,ubuntu@vps3
```

也可以多次给 `--host`，或者在里面用逗号：`go tool tifa deploy setup --host vps1 --host vps2`。每一项都按一台服务器的写法来写。同一台服务器写两次会被拒绝。列表里的第一台服务器有它自己的角色：`deploy.release_command` 在它上面运行，`deploy run` 也去它那里。

只有一台服务器时，一切和以前一样。

**其他 front 只能用于一台服务器。** 有几台服务器时，`none`、`nginx` 和 `cloudflare` 在改动任何东西之前就被拒绝，错误信息会让你用 `external`：

- `none`：每台服务器会自己接待访客，用自己的证书，没有东西来分摊负载。
- `nginx`：Tifa 的 nginx 服务一台服务器上的应用。要从它把访客送到别的服务器，需要服务器之间的网络，这个网络是你的，每家服务商都不一样。
- `cloudflare`：在几台服务器上运行的同一个隧道，会把每个请求发给最近的那台服务器，只有那台失败时才发给别的（[Tunnel availability](https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/configure-tunnels/tunnel-availability/)）。几台服务器不会分摊负载。

## setup {#set-up}

`go tool tifa deploy setup` 依次设置每台服务器，先设置第一台，每台都有自己的方案：`Plan for shop on vps2 (2 of 3)`。它给应用挑一个每台服务器上都空着的端口，这样负载均衡器只需要一个端口。应用在一台服务器上的端口在另一台上被占用时，应用会换到一个每台都空着的端口。

setup 在第一台失败或需要你处理的服务器那里停下。按它说的修好，再运行一次：已经正确的服务器各显示一行，setup 从停下的地方接着做。

有 `deploy.auto` 时，GitHub Actions 放在最后，所有服务器一个方案：仓库的一把密钥，能登录每一台，每一台的主机密钥，以及 secret `TIFA_DEPLOY_HOST` 里的服务器列表（见[从 GitHub Actions 部署](deploy-github-actions.zh-CN.md#deploy-from-github-actions)）。

setup 最后列出要填进负载均衡器的目标和健康检查，`~/.tifa/config.yaml` 记住服务器列表，所以之后不带 `--host` 的命令会作用于所有服务器（见 [Tifa 记住的服务器](deploy.zh-CN.md#the-servers-tifa-remembers)）。要加一台服务器，就把它加进列表，运行 setup，再把目标加进负载均衡器。要去掉一台，先把它从负载均衡器里拿出去，然后运行 `go tool tifa deploy remove --host <那台服务器>`：它只把应用从那一台上拿掉，`~/.tifa/config.yaml` 把其他几台留作这个应用的服务器。然后把那台服务器从 `TIFA_DEPLOY_HOST` 里删掉。有 `deploy.auto` 时，再运行一次 setup，让 GitHub Actions 拿到剩下的服务器。部署写的服务器比 `~/.tifa/config.yaml` 为这个应用记住的少时，会在改动任何东西之前停下，并说出要拿掉的是哪台。

## 部署 {#deploy}

`go tool tifa deploy`：

1. 读取每台服务器。它们的系统必须一样，因为一次构建要在每台上运行，应用的端口也必须一样。某台服务器上没有这个应用，或者端口不一样，部署会在改动任何东西之前停下：运行 setup。
2. 构建一次，把这个版本上传到每台服务器。
3. 设置了 `deploy.release_command` 时，在任何服务器切换之前，在第一台服务器上运行它一次（见[发布命令](deploy.zh-CN.md#the-release-command)）。它失败时，部署停下，新版本从每台服务器上删掉，第一台的 `deploys.log` 写着原因。
4. 一次切换一台服务器：`switching vps2 (2 of 3)`。每台服务器启动新版本并通过健康检查之后，下一台才开始。负载均衡器继续把访客送到没在切换的服务器。

一台服务器没通过检查时，它会自己回到原来的版本，和只有一台服务器时一样，部署随即停下。它前面那些已经通过的服务器会被挪回它们原来运行的版本，从最后一台开始，这样所有服务器又运行同一个版本。之前没有运行过版本的服务器（比如第一次部署时）会改为停掉应用，旧版本这时没通过检查的服务器也会停掉：没有哪台服务器会继续运行在另一台上失败了的版本。几台服务器运行的不一样时，错误信息会说哪台运行哪个版本。这时再部署一次，或者运行 `go tool tifa deploy rollback <release>` 把它们带回同一个版本。

失败的版本会在每台有它的服务器的 `deploys.log` 里记成失败，包括用它通过了检查的和从没切换的，所以 `status` 显示它失败了，rollback 也不会去它那里。

## 其他命令 {#the-other-commands}

- `rollback` 和 `rollback <release>`：版本在第一台服务器上选，每台服务器都必须有这个版本，一次挪一台，健康检查和挪回都和部署一样。
- `status`：一台接一台显示每台服务器。`status --json` 是一个列表，每台服务器一项。
- `logs`：每台服务器的日志，前面有一行写着是哪台。`logs -f` 只跟一台服务器：用 `--host` 指定。
- `env set` 和 `env unset`：一台接一台改每台服务器。出错时会说哪些服务器已经改好了。`env list` 只显示一次名字，几台服务器的名字不一样时分别显示每台的。
- `run`：第一台服务器。要用别的就用 `--host` 指定。
- `remove`：每台服务器一个方案，只问一次，先处理其他服务器，最后处理第一台。只有第一台的方案会收回 GitHub 里的东西。带 `--host` 时，它只把应用从那一台上拿掉，见 [setup](#set-up)。

## 应用要做的事 {#what-the-app-must-do}

每台服务器运行自己的一份程序，有自己的内存和磁盘。Tifa 对这些都不做检查。一份程序写下的东西，其他几份必须能找到：

- **会话。** 默认的存储把会话放在程序的内存里，所以访客在一台服务器上登录了，到下一台就是未登录。给应用一个每台服务器都能访问的[自己的存储](../pages-and-data/sessions.zh-CN.md#where-to-store-sessions)，比如 Postgres 或 Redis。
- **数据。** SQLite 是一台机器上的一个文件。每台服务器会有自己的一份，订单和用户各不相同。用一个每台服务器都能通过网络访问的数据库，比如 Postgres（见 [Postgres](../guides/database.zh-CN.md#postgres)）。
- **上传的文件。** 写在一台服务器磁盘上的文件，其他服务器上没有。把文件放在对象存储里，比如 S3 或 R2（见[上传文件](../guides/uploads.zh-CN.md#uploads)）。
- **后台任务。** 在 `OnStart` 里启动的循环会在每台服务器上运行。只能运行一次的工作，比如每天一封邮件，需要在共享的数据库里加锁或者用任务表（见[后台任务](../guides/jobs.zh-CN.md#background-work)）。
- **迁移。** 迁移放在 `OnStart` 里时，第一台启动的服务器做迁移，其他服务器看到的表已经改好了。有几台服务器时，用 `release_command: migrate` 在切换之前运行一次更清楚。
- **`.env` 里的设置。** `tifa deploy env` 把同一个值写到每台服务器。在一台服务器上手动设的值只留在那台。
