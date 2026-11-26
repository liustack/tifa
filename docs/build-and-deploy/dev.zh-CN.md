---
summary: '运行 `tifa dev`：它启动什么，Vite 怎么接入，端口，它写出的文件，大型应用和 Windows'
audience: app
order: 2
translatedFrom: 871f61bc03db
read_when:
  - '第一次在本地运行 Tifa 应用'
  - '修改 internal/devserver、cmd/tifa 里的 dev 命令，或者 internal/bundle 里的 dev 构建'
  - '在 Windows 上运行 `tifa dev`，或者修改它在那里启动、停止或结束子进程的方式'
---

# tifa dev {#tifa-dev}

```sh
tifa dev [--dir app-root] [--addr 127.0.0.1:9527] [--background]
```

启动时它先打印 `tifa dev: serving http://<address>`，然后做一次完整构建（步骤与 [`tifa build`](build.zh-CN.md) 相同，应用的第一次构建要几秒到一分钟，见[日志](dev-errors.zh-CN.md#log)），再启动应用。浏览器只连 `--addr`。`tifa dev` 把页面请求和数据请求转发给应用，把 `/__tifa/vite/` 下的请求（浏览器模块和 Vite 的 HMR websocket）转发给 Vite 的开发服务器。应用重启或加载新的 SSR 包时，页面请求会等着。Ctrl-C、SIGTERM 和 SIGHUP（终端关闭时会发出）会停掉应用、Vite 和 `tifa dev`。用 `nohup` 启动的 `tifa dev` 会忽略 SIGHUP。Vite 的构建或插件迟迟不结束时，给它 5 秒，然后停掉 Vite。如果 `tifa dev` 本身被杀掉，应用和 Vite 会自己停：应用像收到 SIGTERM 一样关闭，Vite 在同样的 5 秒内退出（在 Windows 上两者同时结束，见[在 Windows 上](#on-windows)）。`tifa dev` 只把明确指定的文件交给应用和 Vite，不会把自己继承来的文件交给它们，比如脚本用 `flock` 持有的锁。

带 `package.json` 的应用要先装好 npm 包。刚克隆下来时没有 `node_modules`，`tifa dev`（和 `tifa build` 一样）会在启动任何东西之前停下，并打印要运行的命令：PATH 里有 pnpm 就是 `pnpm install`，否则是 `npm install`。没有页面的应用没有 `package.json`，也不需要 Node。

`tifa dev` 在 Windows 10 1809 及以后的版本上和其他系统一样运行，包括 `--background` 和[智能体与脚本](dev-scripts.zh-CN.md#agents-and-scripts)里的命令。Windows 上有哪些不同，见[在 Windows 上](#on-windows)。

同一个应用里的第二个 `tifa dev` 不会启动。它会打印 `tifa: tifa dev is already running in this app at http://<address> (pid <N>)` 并报错退出，这发生在它启动 Vite 之前。有 `tifa dev` 在运行时，`tifa build` 也会同样停下，因为两者都写 `.tifa/dist`。锁是 `.tifa/dev.lock`，持有它的进程结束后它就消失。不带 `--addr` 时，9527 端口被占用，`tifa dev` 会依次试 9528 及以上，一共十个端口，`serving` 那一行会写出它用了哪一个。带 `--addr` 时，`tifa dev` 只用那个地址，被占用就失败。

`tifa dev` 提供的是普通 HTTP。项目文件里的 [HTTPS 设置](https.zh-CN.md#https)只对构建出来的程序生效，开发程序会忽略它们。

## Vite {#vite}

`tifa dev` 在 Node 上运行应用自己的 Vite，用的是应用的 `vite.config.ts`（见 [生成的文件和应用的配置](../reference/generated-files.zh-CN.md#the-apps-tsconfig-and-vite-config)）：

- Vite 的开发服务器提供浏览器模块。它随带页面的应用的第一次构建一起启动，监听 127.0.0.1 上一个空闲端口，只有 `tifa dev` 会连它。页面从它那里加载 Vite 客户端、`@vitejs/plugin-react` 的 React Fast Refresh 运行时和 `.tifa/client.ts`。
- SSR 包仍然是一个文件 `.tifa/dist/ssr.js`，应用把它加载进自己的 beastjs VM。它在开发服务器所在的同一个 Node 进程里构建，放在它自己的工作线程上：浏览器拿到的是 React 的开发版，beastjs 跑的是生产版，而 Vite 每个线程只选一次版本。Tifa 自己设置 `NODE_ENV`（开发服务器用 development，SSR 包用 production），所以 shell 里的 `NODE_ENV` 两边都不影响。SSR 包每构建 10 次（失败的也算）就换一个新的工作线程，因为 Node 不会把 Vite 构建留在线程里的东西全部释放。线程结束过的那次构建（比如插件没捕获的错误）之后也会换。把东西放在该线程 `globalThis` 上的插件，到那时会从头开始。

## 文件放在哪里 {#where-files-go}

| 路径 | 内容 |
| --- | --- |
| `.tifa/` | 生成的代码，和 `tifa build` 一样 |
| `.tifa/dev.lock` | `tifa dev` 和 `tifa build` 的锁，带持有者的进程 ID、地址和启动时间 |
| `.tifa/dev.log` | `tifa dev --background` 打印的全部内容，见[智能体与脚本](dev-scripts.zh-CN.md#agents-and-scripts) |
| `.tifa/dist/` | SSR 包、`build.json`、public 文件，以及有集合的应用的 `content.json` 和 `content/` 里的正文。浏览器模块改由 Vite 的开发服务器提供 |
| `bin/tifa-dev` | 开发用的可执行文件 |

应用在应用根目录里运行，即使 `tifa dev` 是用 `--dir` 从别处启动的，这和你从应用根目录运行时 `tifa build` 生成的程序一样。应用按相对路径打开的文件，比如 [`tifa new`](create-app.zh-CN.md#tifa-new) 模板的 `app.db`，就落在那里。它读取的 `.env` 也是（见 [.env](config.zh-CN.md#env)）。`tifa dev` 通过 `SERVER_ADDR` 把应用要监听的地址传给它，这个值优先于你 shell 里的 `PORT` 和 `.env`。

停止应用时，`tifa dev` 最多等配置的 `server.shutdown_timeout` 的 3 倍再加 1 秒，然后杀掉整个进程组。

## 大型应用 {#large-apps}

`tifa dev` 用系统的文件事件监视文件夹。当应用的文件夹或文件多到系统不让一个程序轻松监视时，`tifa dev` 会对其余的做轮询：大约每秒看一次，照常重建。什么都不用设置。日志会说明这一点：

```text
tifa dev: 07:35:36 watching app/assets would use up the system's file watches, so it and the directories found after it are polled for changes
```

- 在 macOS、FreeBSD、OpenBSD、NetBSD 和 DragonFly 上，`tifa dev` 用 kqueue 监视。一个被监视的文件夹占一个文件描述符，里面属于[各种改动触发什么的表](dev-reloads.zh-CN.md#what-each-change-triggers)所列类型的每个文件、每个嵌入的文件和 `content/` 下的每个文件也各占一个，之后才出现的也一样。其他文件，比如 `content/` 之外的图片，不占。`tifa dev` 拿这些去和一个进程可以打开的数量的一半比（打开文件上限和 `kern.maxfilesperproc` 中较小的那个），超出的文件夹改为轮询，被监视的文件夹里文件增长超过这个数时也一样。
- 在 Linux 上，监视一个文件夹占用户的一个 inotify watch，编辑器和其他工具共用这些 watch。`tifa dev` 最多用 `fs.inotify.max_user_watches` 的一半。
- 如果系统先耗尽了，因为其他程序占着很多 watch 或文件，`tifa dev` 会对它没法监视的文件夹做轮询，交还它持有的几个 watch，并继续工作。如果它在 `tifa dev` 运行期间耗尽，比如某个文件打不开，`tifa dev` 就停止用系统事件监视，轮询每个文件夹，并重新比较每个文件。
- 一次轮询会列出被轮询的文件夹，只对大小、修改时间或身份变过的文件算哈希。两次轮询之间，`tifa dev` 至少等一秒，并且至少等上一次轮询耗时的十倍。一个被监视、条目有变化的文件夹，按同样的规则重新列出，不受一秒下限限制，所以一个不断增长的文件夹不会花得更多。因此轮询和列出各自最多占一个 CPU 核的十分之一左右，这还在处理文件事件和重建之外。
- 一次轮询要多久，随文件数增长，也取决于机器和它的磁盘。在 Apple M4 上，文件都在系统缓存里时，我们的运行中对 20,000 个文件的一次轮询用了 45 到 65 毫秒。只要一次轮询用时不到十分之一秒，被轮询文件夹里的改动大约一秒内就会被发现，然后照常构建。
- 被轮询的文件夹在 `tifa dev` 重启之前一直保持轮询。
- 在被轮询文件夹里保存之后，轮询发现改动之前立刻请求的页面，拿到的是旧的服务端渲染结果，而 Vite 随后触发的重新加载会 hydrate 失败。在 `ready` 那一行之后再重新加载一次。
- Vite 的开发服务器也监视应用根目录。一旦 `tifa dev` 因为系统资源不足而轮询，它也会让 Vite 的开发服务器同样轮询，除非开发服务器用的是 FSEvents（在 macOS 上通常如此）。FSEvents 不为每个文件占文件描述符，所以保持不变。
- 要轮询，Vite 的开发服务器会重启，之后每秒查看每个文件一次，在 Apple M4 上 10,000 个文件约占一个 CPU 核的 8%。重启成功、并且没有其他开发服务器重启（比如改 `vite.config.ts` 之后的那种）同时在跑时，这只是一次重启。正在运行的重启会先被等待，如果它成功了而且它的新服务器已经在轮询，就不会再重启别的。
- 重启可能失败。无法创建新服务器的（比如 `vite.config.ts` 坏了），保留旧服务器和它的监视器。新服务器没法监听的（因为另一个程序这时占了端口），已经关掉了旧服务器，所以在下一次重启之前没有 Vite 开发服务器。不管哪种，日志都会说明，`tifa dev` 和应用继续运行。开发服务器在它的监视器下一次出错时再试，下一次成功的重启（比如改 `vite.config.ts` 之后的那种）会轮询。
- 当 Vite 自己的监视器先耗尽 watch 或文件描述符（ENOSPC、EMFILE 或 ENFILE）时，开发服务器会以同样的方式切换到轮询，并说明一次。用 FSEvents 的监视器也是如此。

## 在 Windows 上 {#on-windows}

`tifa dev` 在 Windows 上的工作方式相同，只有这些不同：

- Ctrl-C、Ctrl-Break 和关闭控制台窗口都会停掉它。Windows 没有一个程序能发给另一个程序的信号，所以 `tifa dev stop` 会设置一个 `tifa dev` 在等的事件 `Local\tifa-dev-stop-<pid>`。这个事件属于 Windows 登录会话，所以 `stop` 只能到达在同一个会话里启动的 `tifa dev`。
- 重启应用时，`tifa dev` 关闭应用的标准输入（应用会一直读到它结束），然后像 SIGTERM 那样关闭：`OnShutdown` 会运行。应用的代码不需要为此做任何事。
- `tifa dev` 运行在一个 job object 里（Windows 把一组进程当作一个整体来处理的方式），应用在它里面自己的一个 job object 里。`tifa dev` 被杀掉时（从任务管理器或 `taskkill /F`），Windows 会同时结束 Node、Vite、`go build`、应用以及应用启动的东西。这时应用得不到 `OnShutdown`。如果 Windows 拒绝建立 job object，`tifa dev` 会立刻停下，报一个写出失败调用名的错误，什么也不启动。
- `tifa dev` 要等写文件的程序把文件关掉才看得到已保存的文件。写完后仍然保持文件打开的编辑器，可能让下一个页面请求拿不到改动。
- 保存之后，`tifa dev` 马上会读这个文件，看它有没有变。Windows 不允许别的程序把另一个文件改名覆盖到一个打开着的文件上，所以恰好在这一刻到来的保存可能失败，提示 "Access is denied"。编辑器会重试，用改名来保存的脚本也应该重试。
- Windows 不允许覆盖正在运行的程序。每次 Go 构建之前，`tifa dev` 都会把应用运行所用的程序 `bin\tifa-dev.exe` 改名为 `bin\tifa-dev.old-<number>.exe`，并删除前几次构建留下、现在已经不在运行的那些。
- `--background` 运行 `tifa dev` 时不带控制台窗口。Node、`go` 和应用共用它那个隐藏的控制台，所以不会弹出窗口。
- 不带 `--addr` 时，Windows 为 Hyper-V、WSL 或 Docker 保留的端口（`netsh interface ipv4 show excludedportrange protocol=tcp`）算作被占用，`tifa dev` 会换到下一个。
