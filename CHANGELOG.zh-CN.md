# 更新记录

记录 tifa 里应用和应用的用户看得到的变化。tifa 还没有发过版本，所以按进入 `main` 的日期分组。格式参照 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)。每项提速怎么测的见 [性能](docs/performance.zh-CN.md)。

## [Unreleased]

### 变更

- 有页面的应用装的 React 或 React DOM 低于 19.3.0 时，`tifa build` 和 `tifa dev` 报错停下。
- React DOM 以外的模块代码里出现 `$$FORM_ACTION` 时，构建打印警告并写明是哪个模块，因为这样的表单拿到的键和没有插件时 React 写的不一样。
- 浏览器里，导出 `head` 的路由模块和其他路由模块一样随路由加载，不再放在入口脚本里。示例 shop 的入口 gzip 后从 224 KB 降到 146 KB。这类模块顶层的代码改在路由第一次加载时运行，每个页面都需要的初始化应放在根 Layout 里。入口里的模块 import 了路由模块时，它留在入口里，构建打印打包器的 `INEFFECTIVE_DYNAMIC_IMPORT` 警告。
- 地址监听不了时报错会指出是哪项配置，端口只有 root 能用时提示用 `setcap cap_net_bind_service=+ep`。

### 新增

- 页面的 HTML 紧跟在构建的标签后面，用 `<link rel="modulepreload">` 预加载它的 Layout 和路由的 chunk，这些文件和入口脚本一起下载。`Load` 失败时只预加载路由器显示的那几层。`build.json` 在 `preload` 下列出这些 chunk。
- 程序自己提供 HTTPS，默认关闭。`server.tls_domains` 向 Let's Encrypt 申请并续期证书，存在 `server.tls_cache`。`server.tls_cert` 和 `server.tls_key` 用你自己的证书，文件变了不用重启就重新加载。开了 HTTPS 时浏览器能用 HTTP/2，`server.http_addr`（`:80`）回应 Let's Encrypt 的验证，并把 HTTPS 有证书的名字用 308 跳转到 HTTPS，`server.hsts` 加上 Strict-Transport-Security。开了 HTTPS 还用默认的 `server.addr` 时拒绝启动，并提示设成 `addr: :443`，设成别的回环地址能启动但会打一行警告。`tifa dev` 仍走明文 HTTP。
- 示例应用多了给顾客的中英文店面，在 `/store` 下：商品搜索和筛选、商品详情、购物车、结算表单，以及用 Markdown 写、代码带高亮的手记。
- 路由的 `head` 可以返回 `htmlAttributes`，即这些页面 `<html>` 的属性，比如多语言网站的 `lang`。它们逐个属性合并到根 Layout 的 `htmlAttributes` 上，最深的路由优先，`null` 去掉属性。浏览器每次跳转后同步这些属性，不走 SSR 的页面也一样。tifa 只在它要的值变了时才写某个属性，也只去掉自己写过的属性，所以页面脚本设的 class 或 `lang` 一直保留，直到某个路由要另一个值。不用这个功能的应用 HTML 不变，在浏览器里 tifa 也和以前一样，首次渲染之后不再碰 `<html>`，页面脚本在上面改什么都保留。示例店面这样设置自己的语言，店面没有的路径用 URL 里的语言回答 404。

### 性能

- 每个页面都要加载的代码放进入口脚本这一个文件，不再是四个。走 HTTP/1.1、往返 150 ms 时，shop 的页面完成 hydration 比以前早 11 到 192 ms，往返 40 ms 时早 4 到 77 ms，首次绘制晚 8 到 24 ms（走 HTTP/2 时晚 8 到 48 ms）。由 tifa 自己走 HTTP/1.1 提供服务时，每页要 17 到 20 个文件的应用在往返 150 ms 时完成 hydration 晚 58 到 100 ms，前面放一个讲 HTTP/2 的代理或 CDN 就能避免。
- Vite 插件对 SSR bundle 再做四项改写：React 转义 HTML 改用 `replaceAll`，不再构建组件栈，命名空间拿到的是普通值而不是 getter，key path 里不再放组件名。合起来示例 shop 渲染快 6.5% 到 7.5%，纯文字页面快 17.6% 到 18.3%，HTML 不变。
- 返回数组（比如切片）的 `Load`，数据现在也以 Go 写的 JSON 文本进页面，和对象一样。1,000 行的表格渲染用时少 39%。seroval 的代码换了写法时，构建打印警告，页面照旧渲染。
- Go 交给渲染的数据里，U+FFFF 以上的字符（比如 emoji）写成代理对的转义，shop 订单列表的数据里有一个 emoji 时不再慢 9%。浏览器读到的值不变。
- quickjs v0.26.16：不开 PGO 时，其中两项改动让渲染耗时变成 v0.26.15 的 0.971，另外三项再变成这个的 0.955，HTML 不变。每个 VM 留住最近对象的形状和释放的小块再用，所以垃圾回收后多占约 0.7 MiB，其中 647 KiB 是形状。页面建很多键和别的对象都不一样的对象时，形状最多能占约 6 MiB。形状算在 `ssr.vm_memory` 里。

### 修复

- SSR bundle 保持 React DOM 每一行的位置不变，渲染报错栈里的位置指向正确的行。

## 2026-10-19

### 新增

- `server.compress`，默认开启：页面、导航数据、JSON 和其他文本对接受压缩的客户端用 zstd 或 gzip 发出，tifa 程序不需要代理来压缩。前面有代理负责压缩时设成 `false`。
- `tifa build` 给浏览器 bundle 的每个文本文件旁边写一个 gzip 文件，程序把它发给接受 gzip 的客户端，每次请求不花 CPU。
- 页面和 JSON 响应不压缩发出时带 `Content-Length`。
- 一篇 [性能指南](docs/performance.zh-CN.md)：页面渲染多快，吞吐怎么随核数增长，CPU、`ssr.vms` 和内存怎么定。

### 变更

- quickjs v0.26.14：上游的改动可能让引擎构建把生成代码改错时，构建会停下来。`NewVM` 建不出 context 时不再泄漏。页面渲染和之前一样。

### 性能

- quickjs v0.26.15：不开 PGO 时渲染耗时是 v0.26.14 的 0.843，换上新 profile 是 0.859。3 个核上 6 个 VM 的整请求多服务 1.17 倍。Apple M4 上 10 个 VM 能服务之前的 1.80 到 1.88 倍，因为它们不再等分配器的锁。
- 缓存的页面保存 gzip 和 zstd 两种压缩形式，压缩的缓存命中不再重新压缩，比不压缩的命中还省 CPU。
- 写出页面时不再复制一份。

## 2026-10-18

### 新增

- `tifa build` 用页面渲染的 CPU profile 编译有页面的应用（PGO），`build.pgo` 选 profile：`auto`、`off` 或一个文件。在 quickjs v0.26.15 上，linux/amd64 的页面渲染快约 2.4%。

### 修复

- quickjs v0.26.8：tifa 在 linux 和 freebsd 上又能编译了，v0.26.6 和 v0.26.7 曾让它编译不过。字符串在 emoji 中间被截断时，渲染结果和 Node 一样。
- quickjs v0.26.9：Intl 在所有系统上都按 ICU 的方式命名夏令时。之前在 Linux 上，Europe/Dublin 等少数时区的夏季和冬季名字是反的。
- quickjs v0.26.10：传给 JavaScript 的 Go 字符串里的非法字节变成 U+FFFD，不再把字符串截短。

### 性能

- quickjs v0.26.12：JavaScript 调用不再在 libc 的全局锁下分配内存。linux/amd64 上渲染少用约 11% 的时间，darwin/arm64 上少约 14%，多个 VM 之间也不再互相拖慢。
- quickjs v0.26.13：解释器主循环读写栈的工作更少，给普通对象加属性走快路径。linux/amd64 上渲染少用约 18% 的时间。
- Vite 插件对 SSR bundle 做三项改写：React 把写出的块拼起来再交出，长 `switch` 前先查 `Set`，seroval 对短字符串用 `JSON.stringify` 转义。合起来示例 shop 渲染快 11% 到 14%，纯文字页面快 17.5% 到 22%，HTML 不变。

## 2026-10-17

### 新增

- `ssr.vm_stack_depth` 限制一次渲染里 JavaScript 调用嵌套多深。
- `tifa build` 拒绝 tifa 没测过的 TanStack Router 版本。`build.skip_tanstack_check` 关掉这项检查。

### 变更

- **破坏性变更：** 生成的客户端函数把处理函数的结果类型写成 `Out | undefined`。
- quickjs v0.26.4：Intl 按 2021 年以来的 ECMA-402 读小数位数，货币格式不再需要同时给出两个位数上限。
- quickjs v0.26.6：内存耗尽和渲染超时不能再被页面代码捕获，无限递归抛 `RangeError`，不再让进程崩掉。
- quickjs v0.26.7：渲染超时改用单调时钟。

### 修复

- `Load` 数据里的 `-0` 水合后还是 `-0`：页面带的是 Go 写出的 JSON。
- quickjs v0.26.5：接近内存上限的 VM 报内存不足，不再报 JSON 解码错误。
- 引擎被中断算作渲染用完了时间预算。
- `tifa dev` 在应用根目录里运行应用。

### 性能

- Vite 插件在每个 VM 里缓存 React 的 HTML 转义结果。示例 shop 的 Tailwind 类名里满是要转义的字符，在 Apple M4 上渲染快 8% 到 10%。

## 2026-10-16

### 新增

- 每个 SSR VM 都有 Intl，带所有现代 CLDR 语言区域（quickjs v0.26.2）。
- `tifa dev` 对依赖服务器默认语言区域或时区的 Intl 调用给出警告。
- 时区数据库嵌进程序。
- `Ctx.QueryAll`，作为 `Load` 的查询参数收集。
- `tifa dev` 显示带调用栈的渲染错误，修好后自动刷新一次。
- `tifa dev` 记录慢的构建步骤，首次构建时显示一个等待页面。

### 变更

- quickjs v0.26.3：每个 VM 里脚本的本地时间都是 UTC，页面在笔记本和服务器上渲染出来一样。

### 修复

- 渲染时 panic 的 VM 会被换掉。
- 数据请求的相对重定向按 3xx 的方式解析。
- 处理函数调用会跟随重定向，并在页面显示新数据后才完成。
- 系统的文件监听数用完时 `tifa dev` 照常工作：监听不了的目录改为轮询。
- 构建拒绝在 `Load` 和它的辅助函数里用 `c.BindQuery` 和 `c.Bind`。

### 性能

- `Load` 数据以 Go 写出的 JSON 文本放进页面，不再走 TanStack 的序列化器，后者要花几倍的时间。
- quickjs v0.26.1 只在读 `Error` 的调用栈时才构建它，React 的开发版渲染更快。

## 2026-10-15

### 新增

- `tifa new` 在空目录里起一个应用：一个 SQLite 待办列表，写好所有设置的 `tifa.yaml`，以及给编程 agent 看的 `AGENTS.md` 和 `CLAUDE.md`。`PATH` 里没有 pnpm 时用 npm。
- 通过 `@tailwindcss/vite` 使用 Tailwind CSS，`@/` 指向应用根目录，样式表引用的字体和图片会被复制。
- `tifa dev` 用 Vite 的开发服务器，支持热更新和 React refresh，并把重建的 SSR bundle 加载进正在运行的应用。
- 渲染错误会指出 `require` 失败的包和位置，并定位到源码。

### 变更

- **破坏性变更：** 默认端口改为 9527，`tifa dev` 和构建出的应用都是。
- **破坏性变更：** 前端改由应用自己的 Vite 8 打包，不再用 esbuild。有页面的应用需要 Node、`vite`、`@vitejs/plugin-react` 和 `vite.config.ts`，`@/` 和 `tifa/client` 按应用 tsconfig 的 paths 解析。构建出的程序仍然不需要 Node。
- SSR bundle 里剩下的 `require` 只给出带位置的警告，不再让构建失败。

### 修复

- 页面的 ETag 覆盖 SSR bundle，新构建不会用 304 回一个旧页面。

### 移除

- esbuild 打包器。

## 2026-10-14

第一个版本。

### 新增

- `tifa` 包：`Ctx`、`Error`、`ErrNotFound` 和 `Use`。
- 按目录生成路由，Layout，`Load` 的结果在 TypeScript 里有类型，按 HTTP 方法写的处理函数有类型化的输入和结果，并生成客户端调用函数。
- 对路由代码做静态检查：`Load` 里 `Ctx` 能传到哪，允许哪些 JSON 结构。
- `tifa build` 在 `bin/` 里生成一个可执行文件，前端嵌在里面。还有 `tifa dev`、`tifa routes`，以及测试路由代码用的 `tifatest`。
- 分层配置，来自文件、命令行参数和环境变量。服务器带 `OnRequest` 和错误处理流程。
- 在预热的 QuickJS VM 池里做服务端渲染，引擎是 fork github.com/liustack/quickjs v0.26.0，VM 内存耗尽后不再泄漏内存。
- `Cache = true` 的页面缓存、ETag 和 304，导航数据请求，只在浏览器渲染的组件。
- 示例应用 shop。
