---
summary: '用 `tifa deploy harden` 加固服务器：只许密钥登录 ssh、ufw、自动安装安全更新，它绝不碰什么，怎样避免把你锁在门外，以及怎样撤销'
audience: app
order: 22
translatedFrom: d29ab2cd0014
read_when:
  - '加固 Tifa 应用所在的服务器，或者服务器还接受 ssh 密码登录而你想把它关掉'
  - '运行 `tifa deploy harden`，或者在看它的计划或报告'
  - 'harden 不肯打开 ufw，点名了一个程序和一个端口，或者你不确定它在 ssh 上改了什么'
  - '撤销 `tifa deploy harden` 做过的事'
  - '服务器上装了 Docker，你想知道 ufw 对它起什么作用'
---

# 加固服务器 {#harden-the-server}

`tifa deploy harden` 关上新 VPS 上大多数攻击走的几扇门：关掉 ssh 密码登录，在没有别的东西把守服务器时打开防火墙，并让安全更新自己安装。它管的是服务器，不是某个应用，所以每台服务器运行一次就够。

```sh
go tool tifa deploy harden --host vps
```

和 setup 一样，它先读服务器，打印它看到的情况和一份计划，你同意之前什么都不改。在终端里它会问你。没有终端时，加 `--yes`。可以随时再运行：已经正确的部分不会动。重复写 `--host` 可以指定多台服务器，每台各有自己的报告、计划和询问。不写 `--host` 时，它用 `TIFA_DEPLOY_HOST`，否则用 `~/.tifa` 里唯一的那台服务器。

它登录的方式和 setup 一样：用 root，或者用写在主机前面的用户并通过 `sudo -n`（见[root 登不上时](deploy.md#when-root-cannot-log-in)）。它只改 Debian 和 Ubuntu 服务器（有 apt 的）。其他系统只打印报告，什么都不改。

## 它改什么 {#what-it-changes}

三件事，按这个顺序。

**1. ssh 只许密钥登录。** harden 写入 `/etc/ssh/sshd_config.d/00-tifa-harden.conf`，内容是 `PasswordAuthentication no` 和 `KbdInteractiveAuthentication no`。只有 sshd 当前的 `PermitRootLogin` 是 `yes` 时，它才加上 `PermitRootLogin prohibit-password`，这样 root 仍然可以用密钥登录。文件名故意以 `00` 开头：sshd 对每个设置只取它读到的第一个值，而且按文件名顺序读这个目录里的文件，所以像 `50-cloud-init.conf` 这样的文件（很多 VPS 镜像带着它，里面是 `PasswordAuthentication yes`）会盖过名字排在后面的文件。写完之后，harden 运行 `sshd -t`，用 `sshd -T` 确认新值是生效的值，然后重载 sshd。已经打开的会话不受影响。

这之后，用密码登录的人会被拒绝，需要用密钥。云服务商的网页控制台仍然能用，所以丢了密钥也有路可走。服务器如果本来就拒绝密码，这一项就是「已经正确」。

**2. ufw，在它关着或没装的时候。** harden 在 ufw 没装时用 apt 装上，默认拒绝进来的连接，允许出去的连接，并为下面这些端口（tcp）加规则，规则的注释是 `tifa harden`：

- sshd 用的 ssh 端口，以及这台机器连接时用的端口，
- 服务器上 Tifa 应用接待访客的端口：`front: none` 的应用用 80 和 443，`front: nginx` 的 nginx 也是。`front: cloudflare` 的应用（在 Cloudflare Tunnel 后面）不在服务器的端口上接待访客，所以不加任何端口。

打开 ufw 之前，harden 会列出所有监听在非回环地址上的东西。如果有程序不是 sshd、不是 Tifa 的 socket、也不是 Tifa 的 nginx，harden 就停下：它点名这个程序和端口，不打开 ufw，ufw 里什么都不改。它不去猜这个程序是不是该继续对外开放。请你自己放行它的端口（`sudo ufw allow <port>/tcp`），或者停掉这个程序，再运行 harden。ufw 的某条规则已经放行的端口，算作有交代。

Tifa 的 socket 里有一个例外：`front: external` 的应用。它的 `server.addr` 端口只该让你的负载均衡器或代理访问，所以 harden 不会把这个端口对所有人放行，ufw 也保持关闭。请先从负载均衡器的地址放行这个端口（`sudo ufw allow from <address> to any port <port> proto tcp`），再运行 harden。有了这条规则，这个端口就算有交代，harden 会在它生效的前提下打开 ufw。

ufw 对每个端口只留一条规则，再为它添加规则时只会改掉原规则的注释。所以 harden 只为还没有任何规则放行的端口加规则，不管那条规则的注释是什么，你的规则和 Tifa 的 nginx 的规则都保持原样。计划里会写明它要加的端口，以及已经有规则的端口。拒绝或限速其中某个端口的规则会保留，计划里也会点出来。要是它拒绝的是 ssh 端口，ufw 就保持关闭。

ufw 已经开着时，harden 只做报告：它绝不改别人设好的防火墙规则。要是没有规则放行 ssh，它会说一声。它绝不关闭或限制 ssh 端口。

**3. 自动安装安全更新。** harden 在缺 `unattended-upgrades` 时装上它，在 `/etc/apt/apt.conf.d/20auto-upgrades` 里 `Unattended-Upgrade` 是 0 或文件不存在时把它设为 1，并启用 `apt-daily-upgrade.timer`。Ubuntu Server 默认就是开着的，所以通常是「已经正确」。你有意设成的 `0` 会在计划里照实显示，你同意之后才会改。更新要求重启（`/var/run/reboot-required`）时，harden 会告诉你。它绝不重启服务器。

**只给建议，不改动。** 没装 `fail2ban` 时，计划里会用一行话说明。Tifa 从不安装它：只许密钥登录之后，猜中密码也进不来，它只是减少日志里的噪音。

## 它绝不碰什么 {#what-it-never-touches}

内核设置（sysctl、BBR）、ssh 端口、云服务商的防火墙、已经开着的 ufw 的规则，以及上面清单之外的一切。

## 它不会把你锁在门外 {#it-does-not-lock-you-out}

可能把你锁在门外的两项改动，也就是改 ssh 和打开 ufw，都在一个保持打开的会话里运行，直到一次新的登录成功为止。harden 改完服务器，就从你的机器用一条全新的连接再登录一次（绝不借用 `ControlMaster` 保持着的连接，那样什么也证明不了）。这次登录从不询问密码，所以它成功就说明是密钥登录。登录失败时，还开着的会话会撤销这项改动，harden 报告 ssh 说了什么，服务器恢复原样。harden 在回答之前自己退出时，会话同样会撤销。

## Docker 和 ufw {#docker-and-ufw}

ufw 不会过滤 Docker 发布的端口：Docker 把自己的规则写在 ufw 的规则前面。装了 Docker 时，计划里会说明这一点。只想让服务器本机访问的端口，请发布在 `127.0.0.1` 上（`-p 127.0.0.1:8080:80`）。

## 云服务商的防火墙 {#the-firewall-of-your-provider}

和 setup 一样，harden 不动云服务商的防火墙和安全组。如果它们挡住了访客需要的端口，请在那里放行。

## 撤销 {#undo}

harden 把它改了什么，以及每项改动之前的状态，写进服务器上的 `/etc/tifa/harden`（属于 root）。`tifa deploy remove` 不会撤销加固：加固属于服务器，不属于某个应用。要恢复原样：

```sh
go tool tifa deploy harden --host vps --undo
```

它只读这份记录，别的都不看。它打印计划，用同样的方式询问，然后删掉 ssh 的文件并重载 sshd（密码登录恢复，并且会再检查一次新的登录），关掉 ufw，删掉 harden 加的规则（记录里写着它们的端口，别的规则一概不碰），ufw 是 harden 装的就卸载它，再把自动更新恢复成原来的样子，软件包是 harden 装的就卸载它。最后记录文件被删掉。没有记录时，它会说没有 harden 的东西可撤销。

## setup 会提到它 {#setup-mentions-it}

服务器接受 ssh 密码登录时，`tifa deploy setup` 会打印一行话说明，并点出这条命令。setup 在这件事上不做别的改动。
