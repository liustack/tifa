---
summary: 'Tifa 应用的每一项设置：名称、默认值、参数、环境变量和作用'
audience: app
order: 7
translatedFrom: 20a24e4a78c3
read_when:
  - '修改会话的有效时长、会话 Cookie 名，或者默认会话存储可以用多少内存'
  - '部署 Tifa 应用，或者修改它的端口、请求体上限、读写超时、压缩、虚拟机数量、虚拟机栈深度、渲染超时、渲染队列、渲染记忆，或者带 `Cache` 的页面发出的 Cache-Control'
  - '设置浏览器目标，或者构建的静态文件文件夹'
  - '设置应用的默认语言、URL 里的语言前缀，或者站点的地址'
  - '设置 `tifa deploy` 的服务器或域名'
  - '查一项设置的名称、默认值、参数或环境变量'
---

# 设置项 {#settings}

表示大小的设置（`server.body_limit`、`ssr.vm_memory`、`ssr.memo_size`、`session.memory_limit`）接受一个整数加单位 `B`、`KiB`、`MiB` 或 `GiB`，比如 `64MiB`。单位区分大小写，前面不能有空格。`4MB`、`4mb` 和 `1.5MiB` 会被拒绝，程序不会启动。见[值的格式](../build-and-deploy/config.zh-CN.md#value-formats)。

| 键 | 默认值 | 参数和环境变量 | 作用 |
| --- | --- | --- | --- |
| `build.target` | `es2020` | 仅项目文件 | Vite 构建的浏览器包的 JavaScript 目标，用逗号分隔的列表：一个 ECMAScript 版本（`es2015` 到 `es2029`、`esnext`），加上带版本的浏览器，可选 chrome、edge、firefox、ios、opera 和 safari，比如 `es2019,chrome90,safari14`。SSR 包始终以 ES2022 为目标 |
| `build.public` | `public` | 仅项目文件 | 静态文件的文件夹，这些文件原样复制进程序，从 `/` 提供 |
| `build.public_dotfiles` | 无 | 仅项目文件 | 用逗号分隔的路径，在静态文件的文件夹里，最后一个名字以点开头，比如 `.nojekyll,docs/.assets`。名字以点开头的文件和文件夹本来不会被复制，写在这里的会被复制。路径相对于那个文件夹，用 `/` 分隔。写在这里的文件夹会连同里面的内容一起复制，更深处以点开头的名字仍然要各自写一项。`.well-known` 不用写。路径不存在、走出这个文件夹，或者不是以点开头的名字结尾，`tifa build` 和 `tifa check` 会失败。`tifa build` 会列出它没有复制的内容，`.DS_Store` 除外 |
| `build.skip_tanstack_check` | `false` | 仅项目文件 | `true` 会构建 TanStack Router 不是 Tifa 测试过的版本的应用，这可能让页面在浏览器里变成空白。见 [生成的文件和应用的配置](generated-files.zh-CN.md#the-apps-tsconfig-and-vite-config) |
| `build.debug_info` | `false` | 仅项目文件 | `true` 把 DWARF 调试信息留在程序里，供 Delve 这样的调试器使用。不设时，`tifa build` 用 `-ldflags=-w` 去掉调试信息，这让示例 shop 的程序小约 20%。无论哪种，panic 仍然会给出每一帧的文件和行号。`tifa dev` 始终保留它。见 [build.md](../build-and-deploy/build.zh-CN.md#debug-info) |
| `build.i18next_cache` | `true` | 仅项目文件 | 服务端渲染会保留 i18next 翻译出来的字符串，同样的键和选项再来时直接给出，所以每次渲染都翻译同样文本的页面耗时更少：shop 的店面页面只要它的 0.778。`false` 会关掉它。应用的 formatter 和后处理器对同样的输入必须给出同样的文本。应用在第一次渲染之后不通过 i18next 的 API 改动 i18next 的资源或选项，或者给 i18next 传了读取请求内容的其他函数时，请关掉它，否则页面可能显示过期的翻译。见 [Tifa 里的 i18next](i18next.zh-CN.md#i18next-cache) |
| `i18n.default` | 无 | 仅项目文件 | 带 `_locales/` 文件夹的应用的默认语言，是常见写法的 BCP 47 标签，比如 `zh-CN` 或 `en-US`。这种语言的 URL 没有前缀（除非 `i18n.prefix` 是 `always`），它在 `tifa.Languages()` 里排第一。只有一种语言的应用可以省略它。有多种语言时，没有它 `tifa build` 会失败，没有任何区域有它的文件时也会失败。每个区域都必须有默认语言。见[多语言站点](../guides/i18n.zh-CN.md) |
| `i18n.prefix` | `off` | 仅项目文件 | `always` 会把语言放进每个有语言的路由的 URL，默认语言也不例外：`/store` 会以 308 跳转到 `/zh-CN/store`。`off` 让默认语言没有前缀。不在任何区域里的路由，两种模式下都没有前缀 |
| `site.url` | 无 | 仅项目文件 | 站点对外提供服务的协议和主机，比如 `https://shop.example.com`，不带路径。主机是由字母、数字、连字符和点组成的名字，或者 IP 地址，或者 `localhost`：它会写进 `deploy.front: nginx` 的配置里。有多种语言的站点需要它，没有它 `tifa build` 会失败。它把站点标记为公开：每个页面得到一个 canonical 链接和一个 `og:url` meta，程序提供 `/sitemap.xml`、默认的 `/robots.txt`、`/llms.txt`、`/llms-full.txt` 和内容页面的 Markdown 版本。没有它就没有这些。见[搜索引擎与语言模型](../guides/seo.zh-CN.md) |
| `server.addr` | `127.0.0.1:9527` | `--server-addr`、`SERVER_ADDR` | 监听的地址。在容器里用 `0.0.0.0:9527`。开启 [HTTPS](../build-and-deploy/https.zh-CN.md#https) 时，它提供的是 HTTPS，必须设置，通常是 `:443` |
| `server.body_limit` | `1MiB` | `--server-body-limit`、`SERVER_BODY_LIMIT` | 请求体的最大大小。更大的会以 413 回答。`OnRequest` 里的 `c.SetBodyLimit` 可以为一个请求设置自己的，见 [handlers.md](../pages-and-data/handlers.zh-CN.md#body-limit-and-read-time-of-a-route) |
| `server.read_timeout` | `10s` | `--server-read-timeout`、`SERVER_READ_TIMEOUT` | 读完整个请求（包括请求体）的时间，从连接被接受起算。超过之后仍在到来的请求体会读取失败，带输入的处理函数答 408。请求头必须在 5 秒内到齐，这个时间没有设置可改。见[长请求](../build-and-deploy/tuning.zh-CN.md#long-requests) |
| `server.write_timeout` | `10s` | `--server-write-timeout`、`SERVER_WRITE_TIMEOUT` | 从请求头结束到响应结束的时间。超过之后仍在写的响应会被切断。见[长请求](../build-and-deploy/tuning.zh-CN.md#long-requests) |
| `server.shutdown_timeout` | `10s` | `--server-shutdown-timeout`、`SERVER_SHUTDOWN_TIMEOUT` | 收到停止信号后，正在处理的请求用来完成的时间，之后是 `OnShutdown` 得到的时间。时间到了仍在运行的请求会被取消，并再得到一个 `shutdown_timeout` 的时间来返回。`OnShutdown` 在它们返回之后运行，没有返回就跳过。不管哪种，程序都以状态 1 退出 |
| `server.compress` | `true` | `--server-compress`、`SERVER_COMPRESS` | 对接受的客户端用 gzip 或 zstd 发送文本响应（用 gzip，除非客户端把 zstd 排得更高或不接受 gzip），浏览器包则用 `tifa build` 写出的 gzip 文件发送，见[压缩](server-program.zh-CN.md#compression)。前面有代理做压缩时（比如带 brotli 的 nginx），把它设为 `false` |
| `server.tls_domains` | 无 | `--server-tls-domains`、`SERVER_TLS_DOMAINS` | 为这些域名开启 [HTTPS](../build-and-deploy/https.zh-CN.md#https)，证书来自 Let's Encrypt，用逗号分隔，比如 `example.com,www.example.com` |
| `server.tls_email` | 无 | `--server-tls-email`、`SERVER_TLS_EMAIL` | 和 `tls_domains` 一起交给 Let's Encrypt 的联系邮箱。可选 |
| `server.tls_cache` | 无 | `--server-tls-cache`、`SERVER_TLS_CACHE` | 在重启之间保存 `tls_domains` 证书的文件夹。有 `tls_domains` 时需要它，并且必须在新部署之后仍然保留 |
| `server.tls_ca` | `https://acme-v02.api.letsencrypt.org/directory` | `--server-tls-ca`、`SERVER_TLS_CA` | `tls_domains` 获取证书的 ACME 目录。默认是 Let's Encrypt |
| `server.tls_cert` | 无 | `--server-tls-cert`、`SERVER_TLS_CERT` | 改用你自己的证书文件开启 [HTTPS](../build-and-deploy/https.zh-CN.md#https)，PEM 格式，变化时会重新加载 |
| `server.tls_key` | 无 | `--server-tls-key`、`SERVER_TLS_KEY` | `tls_cert` 的私钥文件，PEM 格式 |
| `server.http_addr` | `:80` | `--server-http-addr`、`SERVER_HTTP_ADDR` | 开启 HTTPS 时，普通 HTTP 的地址，它回答 Let's Encrypt 的检查，并把其他一切重定向到 HTTPS。`off` 表示不要。没有 HTTPS 时不用 |
| `server.hsts` | `false` | `--server-hsts`、`SERVER_HSTS` | 给 `server.addr` 的响应加上 `Strict-Transport-Security: max-age=31536000`，让浏览器一年内都只用 HTTPS |
| `server.csp` | `off` | `--server-csp`、`SERVER_CSP` | `on` 把 `server.csp_policy` 作为每个响应的 `Content-Security-Policy` 发送，每个页面还带上 Tifa 写进页面的内联脚本的哈希。`report-only` 把它作为 `Content-Security-Policy-Report-Only` 发送，只报告。见 [Content-Security-Policy](../build-and-deploy/headers-and-origins.zh-CN.md#content-security-policy) |
| `server.csp_policy` | `script-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'self'` | `--server-csp-policy`、`SERVER_CSP_POLICY` | `server.csp` 的策略：用分号分隔的指令。它只检查能让它保持为一个头的条件：可打印 ASCII，没有逗号，指令名由字母、数字和连字符组成 |
| `server.trusted_origins` | 无 | `--server-trusted-origins`、`SERVER_TRUSTED_ORIGINS` | 允许它们的页面从另一个站点发送写入（POST、PUT、PATCH、DELETE）的 origin，用逗号分隔，比如 `https://admin.example.com,http://localhost:3000`。见[跨站写入](../build-and-deploy/headers-and-origins.zh-CN.md#cross-site-writes) |
| `server.cross_site_paths` | 无 | `--server-cross-site-paths`、`SERVER_CROSS_SITE_PATHS` | 路径模式，对这些路径的写入不检查它来自哪个站点，用逗号分隔，比如 `POST /auth/apple/callback`。见[跨站写入](../build-and-deploy/headers-and-origins.zh-CN.md#cross-site-writes) |
| `ssr.vms` | CPU 数（`GOMAXPROCS`），最多 4 | `--ssr-vms`、`SSR_VMS` | 预热好的 JavaScript VM，也就是同时能渲染多少个页面。在容器里它跟随 CPU 限制。在 1、2、4 和 8 个 CPU 上，默认值分别是 1、2、4 和 4 个 VM：每个 VM 占内存，而一个有内存限制、没有 CPU 限制的容器能看到它宿主机的每个 CPU。在 CPU 更多、内存也够的机器上，请自己设置，比如 `ssr.vms: 8` 或 `SSR_VMS=8`。你设置的值原样使用。什么时候该改它，见[给机器定规格](performance.zh-CN.md#sizing-a-machine)。在每个 P 都在渲染（每个 P 一个 VM，1 个 CPU 上 `GOMAXPROCS=1`）的情况下实测，短请求要在渲染后面等大约 20 毫秒，原因在 Go 的调度器（它 10 毫秒的监控周期，两次，见 beastjs 的 [A long call and the Go scheduler](https://github.com/liustack/beastjs/blob/main/docs/internals/architecture.md#a-long-call-and-the-go-scheduler)）。在同一个 1 CPU 上设 `GOMAXPROCS=2` 时，它们的 P50 是 0.016 毫秒。要让它们保持快速，就让 `GOMAXPROCS` 大于 `ssr.vms`，不过多出来的 P 只有在还有 CPU 给它用时才有帮助。默认值在最多 4 个 CPU 时让两者相等，从 5 个起让 `GOMAXPROCS` 更大 |
| `ssr.vm_memory` | `128MiB` | `--ssr-vm-memory`、`SSR_VM_MEMORY` | 每个 VM 的内存上限。内存用光的渲染会失败，VM 会被换成全新的。页面代码捕获不了它。引擎为复用而保留的 shape 也算在这里，最坏每个 VM 约 6 MiB（2048 个 shape，每个 32 个键，每个键约十个字符，键更长则更多），所以调低它时要给它们留出空间（见[内存](performance.zh-CN.md#memory)）。一次回收后占用超过它一半的 VM 会在后台被换掉，所以 VM 稳定在那之上的应用需要更高的上限（见[内存](performance.zh-CN.md#memory)） |
| `ssr.vm_stack_depth` | `10000` | `--ssr-vm-stack-depth`、`SSR_VM_STACK_DEPTH` | 每个 VM 里 JavaScript 调用最多能嵌套多深，大约每次调用一层。更深的调用会抛出 `RangeError: Maximum call stack size exceeded`，深度和 Node 差不多。取值从 1 到 10000，见[栈深度](../build-and-deploy/tuning.zh-CN.md#stack-depth) |
| `ssr.timeout` | `3s` | `--ssr-timeout`、`SSR_TIMEOUT` | 一次渲染的时间预算。运行更久的渲染会被停止，并作为意外错误处理，交给 `OnError`。页面代码捕获不了它 |
| `ssr.queue_limit` | `32` | `--ssr-queue-limit`、`SSR_QUEUE_LIMIT` | 允许等待 VM 的渲染数，不含已经在运行的渲染。`0` 表示没有空闲 VM 时立刻拒绝。队列满了会以 503 回答，带 `Retry-After: 1` |
| `ssr.wait_timeout` | `3s` | `--ssr-wait-timeout`、`SSR_WAIT_TIMEOUT` | 等待 VM 的最长时间。到期会以 503 回答，带 `Retry-After: 1`，不启动也不替换 VM。它和 `ssr.timeout` 是分开的，后者在拿到 VM 之后才开始计时 |
| `ssr.memo_size` | `64MiB` | `--ssr-memo-size`、`SSR_MEMO_SIZE` | 渲染记忆 memo 的大小，按它保存的字节数算：每个页面和每个站点文件（比如 `llms-full.txt`）一种形式，开了 `server.compress` 时是它的 gzip 形式，否则是它的 HTML（见[压缩](server-program.zh-CN.md#compression)）。满了之后，只被命中过一次的条目先丢，所以只来一次的一批地址挤不掉被反复访问的页面。`0` 会关掉它，不过同时没命中同一个页面或文件的请求仍然共用一次渲染。memo 是什么、为什么安全：[渲染记忆 memo](memo.zh-CN.md)。页面可以用 `Memo = false` 为自己关掉 memo（见 [SSR 与缓存](../pages-and-data/ssr-and-cache.zh-CN.md#ssr-and-cache)） |
| `cache.s_maxage` | `1m` | `--cache-s-maxage`、`CACHE_S_MAXAGE` | 写了 `const Cache = true` 的页面的 `s-maxage`：程序前面的共享缓存（比如 CDN）可以保存它多久。见[给 CDN 用的缓存](../pages-and-data/ssr-and-cache.zh-CN.md#cache-for-cdns) |
| `session.cookie` | `__Host-tifa_session` | `--session-cookie`、`SESSION_COOKIE` | 会话 cookie 的名字。在 `tifa dev` 下会去掉 `__Host-` 前缀，见[会话](../pages-and-data/sessions.zh-CN.md) |
| `session.max_age` | `720h` | `--session-max-age`、`SESSION_MAX_AGE` | 会话从 `tifa.SetSession` 创建它的那一刻起能持续多久，至少一秒。它不会续期 |
| `session.memory_limit` | `64MiB` | `--session-memory-limit`、`SESSION_MEMORY_LIMIT` | 内存会话存储最多能保存的字节数，在 `OnStart` 没有用 `tifa.SetSessionStore` 设置存储时使用。存储满了会丢掉最先过期的会话 |
| `deploy.front` | `none` | 仅项目文件 | 访客在到达服务器之前经过什么：`none`、`cloudflare` 或 `nginx`。选 `none` 时，应用自己在 80 端口上接待访客，`site.url` 里有域名时还在 443 端口上用自己的 HTTPS。选 `cloudflare` 时，`tifa deploy setup` 会用 `cf` 把应用放在 Cloudflare Tunnel 后面，主机是 `site.url` 的主机，`deploy` 和 `rollback` 会清除那个主机名的 CDN 缓存。需要不带端口的 `site.url`。选 `nginx` 时，`tifa deploy setup` 会安装一个由它自己管的 nginx，按 `site.url` 的主机把访客送到应用，证书来自 certbot，这样几个应用可以共用一台服务器。见[按方案设置](../build-and-deploy/deploy-setup.zh-CN.md#set-up-with-a-plan)和[在 nginx 后面](../build-and-deploy/deploy-proxy.zh-CN.md#behind-nginx) |
| `deploy.connect` | `direct` | 仅项目文件 | 部署如何经 ssh 连到服务器，从你的机器和从 CI 都一样：`direct`、`cloudflare`（经 Cloudflare Tunnel）或 `tailscale`（CI 的 runner 加入你的 tailnet）。`cloudflare` 需要 `deploy.front: cloudflare`，`tailscale` 需要 `deploy.auto` 这个块 |
| `deploy.auto.branch` | 仓库的默认分支 | 仅项目文件 | 有 `deploy.auto` 块时：从 GitHub Actions 部署的分支。见[从 GitHub Actions 部署](../build-and-deploy/deploy-github-actions.zh-CN.md#deploy-from-github-actions) |
| `deploy.auto.needs` | `ci.yml` | 仅项目文件 | 有 `deploy.auto` 块时：`.github/workflows` 里必须先通过才会部署的那个文件，比如 `ci.yml`，写 `none` 就什么也不等 |

`deploy.auto` 这个块只要写上就会开启从 GitHub Actions 的部署：`auto: {}` 用默认值，写了它不认识的键，或者写成后面什么都没有的 `auto:`，都是错误。没有这个块，就没有从 GitHub Actions 的部署。`tifa deploy setup` 会设置好它们。
