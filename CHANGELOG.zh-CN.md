# 更新记录

记录 Tifa 里应用和应用的用户看得到的变化。从 v0.0.1 起按版本分组，更早的变化按进入 `main` 的日期分组。格式参照 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)。每项提速怎么测的见 [性能](docs/performance.zh-CN.md)。

## [Unreleased]

### 新增

- 用 `go install github.com/liustack/tifa/cmd/tifa@latest` 装的 `tifa`，在应用里运行时会把命令交给应用锁定的版本，效果和 `go tool tifa` 一样，不会按别的 Tifa 版本生成代码。`go tool tifa` 仍是主要方式（见[全局安装命令](docs/build.zh-CN.md#全局安装命令)）。
- `tifa gen` 把生成的代码写进 `.tifa/`，不做构建，新克隆的项目在编辑器和 `tsc` 里就能看到类型。`tifa new` 写出的 `package.json` 在 `prepare` 脚本里跑它，所以 `pnpm install` 或 `npm install` 之后就有了。已有的应用：在 `package.json` 的 `scripts` 里加上 `"prepare": "go tool tifa gen"`（见 [`tifa gen`](docs/build.zh-CN.md#tifa-gen)）。
- 内置多语言。`app/` 里带 `_locales` 目录的目录，只要里面有 `zh-CN.json`、`en-US.json` 这样的文件，它的页面和 API 路由就有这些语言。默认语言由 `tifa.yaml` 的 `i18n.default` 指定，URL 保持原样（`/store/cart`），其他语言放在路径第一段（`/en-US/store/cart`）。`i18n.prefix: always` 让默认语言也带前缀。在 `tifa.yaml` 里写上 `site.url`，Tifa 就会写 `<html lang>`、`hreflang` 和 `x-default` 链接以及 canonical 链接。代码里，`c.Language()` 和 `useLanguage()` 给出语言，`useAlternates()` 给出页面每种语言的 URL，用来做语言切换，页面的 slug 不同时用 `c.SetAlternates` 说明它有哪些版本，`tifa.Languages()` 给内容集合用（`Languages: tifa.Languages()`），新的 `tifa/i18n` 适配器用一个覆盖所有语言的 i18next 实例和 react-i18next 翻译，格式化函数写在 `hooks.ts` 的 `onStart` 里。`<Link to="/store/cart">` 这样的链接和 Markdown 里以 `/` 开头的链接会自己加上前缀。见[多语言网站](docs/guides/i18n.zh-CN.md)。原来把语言放在自己的 `lang+` 目录里、又没有 `_locales` 目录的应用照旧能用。要迁过来：把文案放进 `_locales`，用 `lang+` 目录里的内容取代它，删掉检查语言的 `Layout` 和手写的 `hreflang`、`lang`，不再需要的话也删掉 `Accept-Language` 的重定向。`app/` 顶层有目录和网站某种语言同名，以及这样的网站在 `htmlAttributes` 里写 `lang`，现在都会让构建失败。
- 应用根目录的 `hooks.ts` 导出 `onStart()`，它在服务器每个 VM 里第一次渲染前、浏览器里水合前各运行一次，用来在 JavaScript 这边为整个应用准备一次东西，比如 i18next 的格式化函数或 dayjs 插件。它是 `hooks.go` 在 JavaScript 这边的对应文件。导出别的名字，构建失败并报出这个名字。`onStart` 抛错时程序不启动，浏览器里页面保持服务器发来的 HTML（[hooks.ts](docs/app.zh-CN.md#hooksts)）。
- 交给 `add` 或 `addCached` 的 i18next 格式化函数如果是就地写的，并且自己调了 `Date.now()`、`Date()`、不带参数的 `new Date()`、`Math.random()`、`performance.now()`、`crypto.randomUUID()` 或 `crypto.getRandomValues()`，构建会失败并指出文件和行号。这样的格式化函数对同样的调用会写出不同的文字，服务器会把一个请求的文字留给后面的请求，浏览器又写出另一种（[i18next 缓存](docs/build.zh-CN.md#i18next-缓存)）。
- 示例商店的两面共用一份数据。工作台的第一家店铺在 `/projects/studio/products` 管着店面的商品，店员可以改价格、库存和中英文的标题与介绍，店面的下一次请求就能看到：数据变了，渲染记忆重新渲染这一页，之后又从渲染记忆里给。第二家店铺也有自己的商品列表和编辑页。店面结账后，订单成为第一家店铺的下一单（从 `SH-1043` 起，原来是随机的 `SS-` 编号）。每张订单都从所在店铺的库存里扣掉相应件数，店面结账和店员建单都一样，买的比剩下的多会返回 409。对比用的三个应用也照此改了。
- 示例商店有了 `/metrics`，按 [部署](docs/deploy.zh-CN.md) 的写法以 Prometheus 文本格式给出 `tifa.Stats()` 的计数，只对登录的店员开放。商品的 PATCH 只改它发来的字段（一个字段都没发的返回 422），商店最多保留 100,000 个购物车，原来是 10,000 个，对比用的应用在这两点上也一样。
- 应用代码在渲染时读到服务端渲染它时没有的查询参数，`tifa build` 会警告并给出文件和行号：从 `useLocation`、`useRouterState`、`useSearch({ strict: false })` 或路由的 `useSearch` 按名字读的参数，以及整个 location 的 `search`、`searchStr` 或 `href`。`page.tsx` 和 `layout.tsx` 按它们的 `Load` 和各层 Layout 读的参数检查，这张表现在由 `.tifa/vite.js` 交给插件，其他模块的这类读法一律警告，`useEffect` 或 `useLayoutEffect` 里的读取不警告。服务端渲染只拿到各个 `Load` 读的参数，所以这样读可能水合不一致。构建照常继续（[服务端渲染里的查询参数](docs/app.zh-CN.md#服务端渲染里的查询参数)）。
- `tifa.Stats().Memo.Evictions` 数渲染记忆为腾地方挤掉的页面，最久没用的先挤，渲染记忆装不下被请求的页面时就看得出来。示例商店在 `/metrics` 里把它写成 `tifa_memo_evictions_total`（[部署](docs/deploy.zh-CN.md)）。
- 示例商店有了 `robots.txt`，放在它的 `public` 目录里：不让爬虫进搜索结果和带筛选的商品列表，分类列表、排序和翻页照常可爬。对比用的三个应用提供同一份文件。

### 变更

- 服务端渲染的 `URL` 和 `URLSearchParams` 改由引擎提供，在 Go 里解析 URL，取代宿主脚本里带的 core-js polyfill，宿主脚本因此变小，每个 VM 占的内存变少。有多语言的网站，路由每写一个链接就建一个 `URL`，这些页面渲染更快，HTML 不变：在 AMD EPYC 7B13 的一个核上，示例商店的三个店面页面用时是原来的 0.703，六个工作台页面是 0.886。没有多语言的网站渲染时不建 `URL`，没有提升。页面能看到的变化有三处，都和浏览器一致：`URL` 对象没有自有属性，违反 UTS 46 Bidi 规则的主机名无效，无效的 URL 抛 `TypeError: Failed to construct 'URL': Invalid URL`。没有了 `URL.createObjectURL` 和 `URL.revokeObjectURL`（这里没有 `Blob`）（[服务端渲染时有什么](docs/app.zh-CN.md#服务端渲染时有什么)）。
- 示例商店的店面卖 150 件商品，原来是 28 件。新加的是前 28 件换了颜色和名字的款，和原款共用照片：商品在数据里写明照片所在的文件夹（`photo`），页面从那里取图，不再按 slug 取。商品放在一个文件里，`app/store/products/catalogue.json`，商店把它打包进程序，对比用的应用也读这个文件。商品列表有 7 页，每种语言有 150 个商品页。
- 从 v0.0.4 升级：带 `const Cache` 的页面不再存在程序里。它的 `Load` 每个请求都跑，数据没变时渲染记忆省掉渲染，所以源站从不给出旧数据。页面保留匿名请求和构建检查，HTML 仍带 `Cache-Control: public, max-age=0, s-maxage=<秒数>, stale-if-error=86400` 给前面的 CDN 用，除非 `OnRequest`、`Load` 或 `Layout` 把它设成私有。`SSR = false` 又带 `Cache` 的页面不再有 ETag。`Cache` 现在可以是 `true`、`false` 或一个时长：`const Cache = true` 的时间取自新配置 `cache_control.s_maxage`（默认 `1m`，`--cache-control-s-maxage`，`CACHE_CONTROL_S_MAXAGE`），写时长仍是这个页面自己的时间。删除了 `cache.whole_page`、`cache.ignore_query`（连同 `--cache-whole-page`、`--cache-ignore-query` 和对应的环境变量）、`tifa.ExpirePages`（改在 CDN 里清除，Tifa 没有要让它过期的副本），以及这类页面的 `Age` 和 `Cache-Status` 响应头。改名，不留旧名：渲染缓存叫渲染记忆。`RenderCache = false` 改成 `Memo = false`，`cache.size` 改成 `ssr.memo_size`（`--cache-size` 和 `CACHE_SIZE` 改成 `--ssr-memo-size` 和 `SSR_MEMO_SIZE`），`tifa.Stats().RenderCache` 改成 `tifa.Stats().Memo`（`CacheBytes` 和 `CacheSize` 改成 `MemoBytes` 和 `MemoSize`，类型 `CacheStats` 改成 `MemoStats`，`PageCache` 删除），`X-Tifa-Render: cache` 改成 `X-Tifa-Render: memo`，`docs/render-cache.zh-CN.md` 改成 [docs/memo.zh-CN.md](docs/memo.zh-CN.md)旧名字会报错，不会悄悄用默认值：`route.go` 里写 `RenderCache` 会构建失败，`cache.size`、`cache.whole_page` 和 `cache.ignore_query` 会让程序启动失败（写在 `tifa.yaml` 里则是 `tifa build` 失败），不论它们来自文件、参数还是环境变量。带 `Cache` 的页面的 `Load` 返回的重定向也带共享策略（[给 CDN 用的 Cache](docs/app.zh-CN.md#给-cdn-用的-cache)、[渲染记忆](docs/memo.zh-CN.md)）。
- `ssr.wait_timeout` 默认值从 `1s` 改为 `3s`，与 `ssr.timeout` 相同。一个请求最多等 3 秒拿 VM，再最多渲染 3 秒，共 6 秒，这只是 SSR 部分，仍在 10 秒的 `server.write_timeout` 以内。`OnRequest`、`Load`、编码、压缩和写出响应共用同一个超时，调大其中任何一项时要给它们留出余地。32 个排队的渲染要跑超过 1 秒的机器上（比如单核、页面渲染超过 31 毫秒），突发请求原来等 1 秒就回 503，现在最多等 3 秒，除非先碰到 `ssr.queue_limit`。想保留原来的限制，设 `ssr.wait_timeout: 1s`（[渲染队列](docs/config.zh-CN.md#渲染队列)）。
- 用 react-i18next 18 的应用，SSR 包不再打印 `tifa rewrite not recognized`。react-i18next 18 用 `Proxy` 包住 i18n 对象，不再复制，所以插件不动 `useTranslation`。i18n 应用的端到端测试现在用 i18next 27.0.1 和 react-i18next 18.0.0（[i18next 适配器](docs/build.zh-CN.md#i18next-适配器)）。
- 服务端渲染的 i18next 缓存现在也记下运行了应用格式化函数或后处理器的调用的字符串，以前这些调用每次渲染都重新翻译。示例商店的店面页面里，金额和日期都经过它自己的格式化函数，渲染时间是原来的 0.937，是不开缓存时的 0.778，HTML 不变。这类代码对同样的语言、选项和值必须给出同样的文字，就像 React 组件对同样的 props 给出同样的输出。要读请求或当前时间的格式化函数或后处理器，改成从 `t` 的选项里拿这个值（[i18next 缓存](docs/build.zh-CN.md#i18next-缓存)）。
- 应用用 `tifa.String` 等定义自己的选项的文件，现在叫 `options.go`，不再是 `config.go`（见[应用的选项](docs/config.zh-CN.md#应用的选项)）。已有的应用：用一次 `git mv` 把 `config.go` 改名为 `options.go`。`config.go` 里还有 `tifa.String` 等调用时，构建会失败，报出文件名并说明要改名。应用在这里定义的东西现在叫应用的选项，Tifa 的配置指 `tifa.yaml` 里的那些。
- [性能文档](docs/performance.zh-CN.md#和三个-node-框架比)和 README 现在除了 Next.js 和 TanStack Start，也和 React Router 比，并且像浏览器那样带上 gzip 来测。新的[渲染记忆文档](docs/memo.zh-CN.md)讲清渲染记忆是什么，为什么对所有页面（包括登录后的页面）都安全，以及每类响应里 Tifa 怎么处理 Cookie、会话和 `Authorization`。
- 客户端同时接受 gzip 和 zstd 时（浏览器发 `Accept-Encoding: gzip, deflate, br, zstd`），现在得到 gzip。只有客户端不接受 gzip，或者用 `q` 给 zstd 更高优先级时才发 zstd。页面、缓存的页面、导航数据和 `server.compress` 压缩的其他回答都一样。
- 渲染记忆对每个页面只存一种形式：`server.compress` 开着时存 gzip，否则存 HTML。以前一个页面存 HTML 加 gzip 和 zstd 两份压缩，而且每次没命中都压两次，占一次没命中约 8% 的时间。现在只压一次，页面在缓存里大约只占 HTML 的五分之一，接受 gzip 的客户端命中时直接发存好的字节。不接受压缩的客户端得到边写边解压的 HTML，带 `Content-Length`。不接受 gzip 只接受 zstd、或给 zstd 更高 `q` 的客户端，得到边写边做出的 zstd。
- 服务端渲染改用 `github.com/liustack/quickjs` v0.30.1，也就是引擎的新路径，v0.28.0 到 v0.29.11 它叫 `github.com/liustack/slowjs`。HTML 不变。应用构建时如果用了引擎的构建标签，`slowjs_intl_nonames`、`slowjs_intl_nocjdict`、`slowjs_anygo` 现在是 `quickjs_intl_nonames`、`quickjs_intl_nocjdict`、`quickjs_anygo`。引擎根包的错误信息以 `quickjs:` 开头，原来是 `slowjs:`。从 v0.30.1 起，渲染超时（`ssr.timeout`）也能停下正在展平很长字符串、执行 `String.prototype.repeat`、编译很长的正则或很大的字符类的页面。以前在超时下编译特别长的正则可能让 VM 崩溃。
- 服务端渲染改用 quickjs v0.30.2，引擎换成了寄存器式解释器，取代 QuickJS 原来的栈式解释器。页面的 HTML 不变，渲染更快，每个 VM 占的内存变多，因为引擎会留着每个跑过的函数的寄存器代码。`WeakRef` 现在把目标留到当前任务结束，和 Node 一样。
- 服务端渲染改用 quickjs v0.30.3，寄存器式解释器的第二批改进。页面的 HTML 不变。`Intl` 和 `Temporal` 遇到特别长的语言标签或选项时现在抛 `RangeError`，以前会整个读进来。解构数组不再产生迭代器。闭包对没人改过的变量直接存值，所以只被挂起的 async 函数持有的对象，指向它的 `WeakRef` 可能更早清空。
- 服务端渲染改用 quickjs v0.30.5，寄存器式解释器的第三批改进，引擎现在按页面渲染的 profile 生成。页面的 HTML 不变，渲染更快：在 AMD EPYC 7B13 的一个核上，示例商店的六个工作台页面用时是 v0.30.3 的 0.894，三个店面页面是 0.905，一个 MUI 应用的三个页面是 0.885。`{..."ab"}` 现在得到各个字符，和 Node 一样。`ssr.timeout` 现在也能停住在超长文本上的 `Intl` 调用，语言列表里有超过 16,384 个字符的标签时抛 `RangeError`，VM 申请一大块内存会超过上限时先做一次垃圾回收。
- `tifa new` 固定 `github.com/liustack/sqlite` v0.1.4。v0.1.3 修了一次提交合并超过 64 条不同 SQL 时的空指针崩溃，把值直接拼进 SQL 文本时很常见。v0.1.4 带来分组提交，以及一次全面审查和 Windows 上的修复，还改了一条规则：用命名参数时，从 1 到最大编号的每个参数都要有值，和位置参数一样（`SELECT ?2, :x` 也要给 `?1`）。已有应用执行 `go get github.com/liustack/sqlite@v0.1.4`。
- 示例 shop 的店面不再使用 `const Cache`（五个页面的已删除），browse 对比因此只跑一份 Tifa。页面只由渲染记忆回答，每个请求都会运行自己的 `Load`。shop 的 README 已同步说明。
- browse 对比只剩一个场景：顾客和员工在同一个配比里的商店。员工访问和商品编辑都翻倍的场景已删除，`SCENARIOS` 和 `-scenarios` 也一起删除。员工占比仍是报告配比表里的一行。
- browse 对比的吞吐表和延迟表多了第五行：关掉渲染记忆的 Tifa（`--ssr-memo-size 0`），它不再保存已完成的渲染，但仍可能不渲染就回答导航数据、共用进行中的渲染和返回 304，标成参照、不参与比较。它默认就跑，准备的数据和各项检查都和其他几家一样。
- browse 报告的吞吐表和延迟表用粗体标出没有压缩任何回答的服务器，并列出它每次页面浏览发出的正文和参与比较、压缩了的几家对比，数字都来自各轮结果。TanStack Start 的 Nitro 服务器原样不压缩回答，对比也按原样测。
- browse 报告的附录把 Tifa 的回答按页面种类拆成来自渲染记忆、共用进行中的渲染、在虚拟机里渲染三类，渲染的再拆成本轮第一次请求这个 URL、这个 URL 之前回答过（客户端按 URL 判断）、个人页（带或不带购物车 cookie），并写明客户端拆分的局限。
- browse 对比的每次访问平均看 4 个页面：一次整页加载和三次点击，每个分块都精确如此，依据是约 4.3 页每会话（Contentsquare 2025），点击也算一次页面浏览。访问长度服从几何分布，从 1 到 12。以前每次访问 2.6 页，最多 5 页。加入购物车和下单占访问的比例不变。
- browse 运行在第一轮之前，把每个 Node 应用的回放轨迹里的文档和导航数据各请求两次，只要有一个回答带着可被缓存的标记（`ETag`、`Last-Modified`、正的 `max-age` 或 Next.js 的缓存头），运行就停下。
- 不是正式运行的 browse 报告（设置不是默认值、没绑核、不是 browse.sh 跑的，或者没跑全部服务器）会在开头标明，并列出每一处不同。
- 示例商店给订单配上索引和随每次写入更新的合计，订单列表、订单页和统计页读数据的耗时在 50 单和 5000 单时差不多。以前每次读都要复制并排序店里的全部订单。筛选、搜索或按金额排序时仍会走一遍全部订单。订单页不再重复显示各家快递的表现，统计页上有。
- 页面的服务端渲染只看到路径和它的 `Load` 及上面各层 Layout 读的查询参数，按名字排序，渲染记忆和 ETag 也按这些来算。`utm_source`、`fbclid` 或随机参数这类别的参数不再多出一个记忆条目、多渲染一次，参数换个顺序也还是同一个页面。地址栏保留每个参数和它的值，`OnRequest`、各个 `Load` 和导航数据看到的仍是完整的 URL。升级时注意：组件渲染时读了没有 `Load` 读的参数，服务端现在拿不到它，页面会水合不一致。把这个参数放到 `Load` 里读，组件里按名字读（[服务端渲染里的查询参数](docs/app.zh-CN.md#服务端渲染里的查询参数)）。

### 修复

- SSR 包写开的小函数调用，不再在调用先算的有副作用实参之前读变量、属性或全局对象上的名字。`function f(x) { return n + x }` 以 `f((n = 2, 0))` 调用时，写开后得 1，调用本该得 2。函数体里的 getter 也会跑在实参前面，或者在实参算之前就抛错。这样的调用现在保持原样：在最后一个有副作用的实参之前，函数体只能用参数、用 `===` 或 `!==` 比较，以及读模块里只赋值一次、调用时一定已经有值的名字（[前端产物](docs/build.zh-CN.md#前端产物)）。
- 示例商店中文店面页头的搜索框搜的是中文列表（`/zh-CN/store/products?q=`），以前会跳到英文列表。
- 示例商店中文店面的商品页，评论翻页链接指向中文网址（`/zh-CN/store/products/<slug>?reviews=2`），以前指向英文页。
- 渲染页面时路由器发出的重定向（`/%61` 转到 `/a`）现在和别的页面一样进渲染记忆，再来一次就是命中（`X-Tifa-Render: memo`），不再渲染。404 页和 `Load` 失败时显示的错误页每个请求都渲染，不存下，所以随机 URL 不会把记忆填满，同时到达的同一个错误页请求共用一次渲染。它们不带 `ETag`，`OnError` 看到每次失败（[渲染记忆](docs/memo.zh-CN.md#为什么对所有页面都安全)）。
- `/assets/` 下没有的文件，比如部署前打开的标签页来要旧构建的代码块，现在答纯文本 404，带 `Cache-Control: no-store`。以前它要经过 `OnRequest`，跑根 Layout 的 `Load`，再渲染应用的 404 页（[页面请求](docs/build.zh-CN.md#页面请求)）。
- 比 `ssr.memo_size` 还大的页面只压缩一次：收 gzip 的客户端拿到为缓存压好的那份，以前那份被扔掉再压一遍。每次渲染的键也不再复制它要哈希的数据，92 KB 数据的请求少产生约 98 KB 垃圾（[优化记录](docs/studies/optimizations.md#server)）。
- 用 i18next 27 的应用，SSR 包不再为 `fixedT`、`looksLikeObjectPath` 和 `createCachedFormatter` 打印 `tifa rewrite not recognized`。前两处和 26 一样提速。i18next 27.0.1 自己限制了格式化函数的数量，插件不再动它，27.0.0 仍用插件的上限。`build.i18next_cache` 现在在 i18next 27.0.0 和 27.0.1 上和在 26 上一样工作，`hydrate` 会清空它那个实例的缓存（[i18next 缓存](docs/build.zh-CN.md#i18next-缓存)）。
- 服务端渲染的 i18next 缓存不再把前一次调用的文字给另一次只在 Symbol 键下的选项不同的调用，后处理器可以读这样的选项。带这种选项的调用不记（[i18next 缓存](docs/build.zh-CN.md#i18next-缓存)）。
- 用 i18next 格式化大量不同金额、日期或名字的服务器，内存不再一直涨。i18next 通过 `number`、`currency`、`datetime` 等格式和 `addCached` 加的格式格式化时，每个值都存一个格式化函数，而且从不释放（i18next/i18next#2227）：一个 VM 里 20,000 个新值占了 28 MB，之后服务器只好换掉这个 VM。现在 SSR 包里每个格式最多存 1,024 个，文字不变（[i18next](docs/build.zh-CN.md#前端产物)）。
- 一个路由在 `head` 里写了多个同名 meta 时，比如浅色和深色模式各一个 `theme-color`，或者多个 `og:image`，现在页面上全都有。以前只留最后一个，浅色模式的页面用的也是深色的 `theme-color`。更深的路由写了同名 meta 时，仍然整组替换。
- `server.compress` 开着时，渲染记忆占的内存最多是 `ssr.memo_size` 的两倍：它按压缩页面的长度计数，存的却是压缩时用的更大的缓冲区，每个页面的 Content-Security-Policy 也没有计入。现在存大小刚好的副本，按实际存下的字节计数，缓存的活跃堆不再超过 `ssr.memo_size`。一直有写入、把缓存填满的程序，内存峰值也随之下降。
- URL 换一种写法（`/%6Eews?utm_source=mail&page=2`、`/news/?page=2`）时服务器发的 308 会带上渲染没看到的查询参数。渲染记忆照样为它看到的每种参数存下这个重定向，每个请求拿回的是自己的参数。
- 多语言站点上，打开查询参数写法和路由器不一样的页面（比如 `?q=a%20b`、`?next=%2Fa`，或同一个参数隔着别的参数出现两次），水合后不再马上重新请求数据，带语言前缀的 URL 在地址栏里也保留原来的写法。以前路由器会换成自己的写法（`?q=a+b`、`?next=/a`）。单语言站点本来就不会这样。路径不同的 URL 照旧换成路由器自己的写法。
- `SSR = false` 的页面，打开时同一个参数隔着别的参数出现两次（`?x=1&y=2&x=3`），首次加载重新用文档里带的数据。以前会再向服务器要一次，单语言和多语言都一样。

## [0.0.4] - 2026-11-05

### 新增

- POST、PUT、PATCH 处理函数的入参里，类型为 `*multipart.FileHeader` 或 `[]*multipart.FileHeader` 的字段装上传的文件。生成的函数把它们当 `Blob | null` 和 `Blob[]` 收下，把入参当 multipart 表单发出，所以页面上传文件和发送别的入参是一个写法，`Validate`、`tifa.Fields` 和 `TifaError` 照旧。这样的入参收到 JSON 请求体答 415（见[入参里的文件](docs/app.zh-CN.md#入参里的文件)和[上传](docs/guides/uploads.zh-CN.md)）。
- `route.go` 里的 `BodyLimit` 和 `ReadTimeout`（常量，或者变量，比如应用的配置）对它的方法函数回答的请求代替 `server.body_limit` 和 `server.read_timeout`。读取时间在 `OnRequest` 之前放宽，回答在它之后再有 `server.write_timeout` 的时间（见[路由自己的请求体上限和读取时间](docs/app.zh-CN.md#路由自己的请求体上限和读取时间)）。
- 请求体在读期限之前没传完时答 408，不再答 400。
- `tifatest.File(t, name, data)` 给出一个文件的 `*multipart.FileHeader`，给用装文件的入参调用处理函数或 `Validate` 的测试用。
- `route.gen` 导出 `PageSearch` 和 `LayoutSearch`，即这个目录的 `Load` 或 `Layout` 读的查询参数，路由也挂上它们，所以 `Link`、`navigate` 和 `useSearch` 知道它们：URL 里一个键可以出现多次，所以读的时候每个值都是 `string` 或 `string[]`（Go 的 `c.Query` 取第一个值），每个键都可以不写，其他键放开。只多了类型：浏览器里不对查询参数运行任何代码（见[Link 和 navigate 里的查询参数](docs/app.zh-CN.md#link-和-navigate-里的查询参数)）。
- `tifa.Secret("smtp.password", "SMTP password")` 定义应用的密码、token 或密钥配置。它从环境变量、`.env` 或 `-c` 的文件读，用命令行参数给值时程序报错并说明该放在哪里，`./bin/app config` 只写它的来源，不写值。[邮件指南](docs/guides/email.zh-CN.md)改用了它。
- `server.csp: on` 发出只让本站脚本运行的 Content-Security-Policy。Tifa 把它写进页面的内联脚本的哈希加进这个页面的策略，页面照常 hydrate，渲染缓存、整页缓存和 ETag 也照常工作。`server.csp_policy` 改策略，`report-only` 只报告不拦截（见 [Content-Security-Policy](docs/config.zh-CN.md#content-security-policy)）。
- `tifatest.Start(t, tifaapp.App)` 在 `go test` 里跑起整个应用，返回一个 `*httptest.Server`，测试可以照浏览器的路子走一遍：打开受保护的页面拿到重定向、登录、看到会话 cookie、读页面。`OnStart`、`OnRequest`、解码、`Validate`、会话、重定向和 `OnError` 都和程序里一样跑，测试结束时跑 `OnShutdown`，client 会记下 cookie，不跟随重定向。没有前端时，页面按路由 ID 以 JSON 回答 Load 的数据。`tifatest.StartPages` 还会用 `tifa build` 或 `tifa dev` 的前端渲染页面，测试二进制里最多两个 VM，之后同一份构建的测试接着用，`.tifa/dist` 不存在或构建它用到的文件之后改过时，让测试失败。要在单独的测试包里导入应用，比如 `package login_test`（见[测整个请求](docs/app.zh-CN.md#测整个请求)）。
- `tifa build` 和 `tifa dev` 写出 `.tifa/dist/inputs.json`：构建前端用到的每个文件的哈希，`StartPages` 用它检查。

### 变更

- `multipart.FileHeader` 只要不在 POST、PUT、PATCH 入参的字段里，比如在结果、`Load` 的数据或会话里，都会构建报错。`route.go` 里把 `BodyLimit` 或 `ReadTimeout` 声明成别的意思，或者没有方法函数却声明了它们，也会构建报错。
- 给 Go 读的查询参数传字符串以外的值，比如 `search={{ page: 2 }}`，现在是类型错误。写成 `page: String(n)`：路由器本来也会把它写成文本。
- 每个回答现在都带 `Referrer-Policy: strict-origin-when-cross-origin` 和 `Content-Security-Policy: frame-ancestors 'self'`，加上原有的 `X-Content-Type-Options: nosniff`。别的网站不能再把页面放进 iframe：合作方要放的页面在 `OnRequest` 里改这个头（见[安全头](docs/config.zh-CN.md#安全头)）。
- `tifa new` 固定 `github.com/liustack/sqlite` v0.1.2，它支持 Go 1.26.6 到 1.27，遇到更新的 Go 会停止编译。升级 Go 之后也要升级驱动。
- 生成的路由表从 `.tifa/routes.go` 移到包 `.tifa/tifaapp` 里，叫 `tifaapp.App`，测试可以导入它。`.tifa/main.go` 服务的就是 `tifaapp.App`。导入它的测试需要 `.tifa`，所以刚 clone 下来的仓库里，在跑过 `tifa check`、`tifa build` 或 `tifa dev` 之前，`go vet`、`go test` 和 `go mod tidy` 都会失败。

### 修复

- 文档说过 `Load` 读的查询参数变了，页面或 Layout 会重新挂载。实际上从来没有，现在文档和生成的 `remountDeps` 都照实际来：路由读的路径参数变了才重新挂载，查询参数变了只重新取数据，组件和它的状态保留，和 TanStack Router 的默认一致。用 `Load` 数据初始化的状态，要靠应用自己的 `key` 或 effect 才会跟着新的查询参数变。
- 自己用文本读写的切片类型，比如带 `MarshalText` 和 `UnmarshalText` 的 `type Tags []string`，放在 GET、DELETE 入参或带文件的表单入参里，以前能通过构建：TypeScript 把它当一个字符串发出，比如 `"a,b"`，Go 不调用 `UnmarshalText`，把整个字符串放进一个元素，数字切片还会答 400。现在它和元素带这类方法的字段一样，构建报错。`c.BindQuery` 和带文件入参的解码遇到这种字段，不管请求里有什么都返回错误。也收 JSON 的入参收到表单时，这种字段有值就答 400，不再填错（见[带入参和返回值的方法函数](docs/app.zh-CN.md#带入参和返回值的方法函数)）。
- 用 systemd socket activation 开 HTTPS 时，systemd 交来 `server.addr` 的套接字后可以不设 `server.addr`。之前程序会因为这个它根本不监听的默认地址拒绝启动。
- 开了 HTTPS 时，`server.http_addr` 的明文 HTTP 地址现在和 `server.addr` 一样按 `server.read_timeout`、`server.write_timeout` 读写。之前不管它们怎么设都是 10 秒。
- 在 `tifatest.NewCtx` 下，处理函数先写 1xx 状态（比如 `103 Early Hints`）再 `SetSession` 和回答时，recorder 停在 1xx 上，也没有 `Set-Cookie`：`httptest.ResponseRecorder` 把任何状态都当成最终状态。现在 recorder 不收 1xx 状态，和服务器在回答之前发出它们一致。
- [登录](docs/guides/sign-in.zh-CN.md) 里的测试从一个 recorder 读两次请求，而它的 `Result()` 留下的是第一次调用时的头，所以永远看不到第二次的 cookie。现在每个请求新建一个 `NewCtx`。

## [0.0.3] - 2026-11-05

### 变更

- `tifa new` 模板改用 SQLite 驱动 `github.com/liustack/sqlite`，不再用 `modernc.org/sqlite`。它是纯 Go 的，需要 Go 1.27 或更高版本，支持 Linux 和 macOS（amd64 和 arm64）以及 Windows（amd64）。已有应用从 `modernc.org/sqlite` 迁过来要查两处。驱动会拒绝不认识的 DSN 参数，所以 `_foreign_keys=1` 之类要改成 `_pragma=foreign_keys(1)`。SQL 里的双引号只表示列名和表名，写成 `"text"` 的字符串值要改成 `'text'`。两个驱动都注册名字 `sqlite`，旧的导入必须删干净。

## [0.0.2] - 2026-11-04

### 新增

- `tifa.ExpirePages("/blog/"+slug, "/blog")` 让整页缓存在这些页面的下一个请求时重新生成，每个路径的所有查询串、Host 和协议都算。一次写入改了开了 `Cache` 的页面显示的内容，就在写入后调它。用调用之前读到的数据做出来的页面不会存下，重新生成失败时旧页照常顶上。它只清程序内存里的，不清前面的 CDN（见[写入后重新生成页面](docs/app.zh-CN.md#写入后重新生成页面)）。
- `tifa.Stats()` 返回 VM 池的情况（加载了几个 VM、几个在渲染、几个渲染在等，渲染次数、失败次数、总耗时，以及回了 503 的渲染），还有渲染缓存和整页缓存的命中、未命中、共用生成次数和条目数，以及两者占的字节。渲染和缓存查找用原子计数，不加新锁。[部署](docs/deploy.zh-CN.md#健康检查和指标)里给了健康检查路由，和一个不用库就给 Prometheus 写出这些数的路由，以及该看哪些数。
- `server.read_timeout` 和 `server.write_timeout`（参数 `--server-read-timeout` 和 `--server-write-timeout`，环境变量 `SERVER_READ_TIMEOUT` 和 `SERVER_WRITE_TIMEOUT`）设读完一个请求的时间和写完回答的时间，默认都还是 10s。流式输出或收大文件的处理函数用 `http.NewResponseController(w)` 只给自己的请求放宽，它能穿过 Tifa 和压缩够到连接（见[耗时长的请求](docs/config.zh-CN.md#耗时长的请求)）。
- `tifa.ShuttingDown(ctx)` 返回一个在程序开始停下时关闭的 channel。没有自己终点的流，比如 server-sent events，看到它关闭就返回，停程序不再为它等满 `server.shutdown_timeout`。
- 在 systemd 套接字激活下，程序在 systemd 交给它的套接字上服务（`LISTEN_FDS`），重启时端口一直开着，新连接排队而不是被拒。开 HTTPS 时两个套接字用 `FileDescriptorName=server.addr` 和 `FileDescriptorName=server.http_addr` 区分。没有这些变量时什么都不变。[部署](docs/deploy.zh-CN.md#重启时不丢请求)里有 `.socket` 和 `.service` 配置。
- `tifa new` 模板把会话存进它的 SQLite 数据库（`sessions.go`，在 `OnStart` 里用 `tifa.SetSessionStore` 设上），重启不再让所有人登出。
- `tifatest.NewCtx` 接受流式输出的处理函数通过 `http.NewResponseController` 设的截止时间。
- [流式输出](docs/guides/streaming.zh-CN.md)讲 server-sent events 和一段一段推出去的回答，带浏览器端代码、缓存、程序停下时的做法和测试。

### 变更

- `Content-Type: text/event-stream` 的回答不再压缩。压缩的流只要开着就占一个编码器，gzip 约 1 MiB。别的流式回答照旧压缩，也照旧及时发出。

### 修复

- 开了 `Cache` 的页面被内容条目提前作废后，如果重新生成失败、发出的是旧页，它的 `s-maxage` 仍是整段时间，前面的 CDN 会把它当成新鲜的，一直存到那段时间结束。现在它的 `s-maxage` 不大于 `Age`。
- 构建出的程序遇到不认识的参数，比如把 `--server-addr` 写成 `--addr`，会当它不存在，照默认设置启动。现在程序报 `unknown flag: --addr` 并退出。应用设置的参数照常可用，别的环境变量和文件里别的键仍然不管。

## [0.0.1] - 2026-11-04

### 新增

- 内容集合：应用把文章、文档等写作内容作为 Markdown 文件放在 `content/` 下，在 Go 里当作有类型的值来读。根目录的 `content.go` 声明每个集合，`var Blog = content.Define[Post](content.Source{Dir: "blog", Languages: []string{"zh-CN", "en"}})`，`Post` 嵌入 `content.Entry`，字段的 json 名就是 front matter 的键。有 `index.md` 的文件夹连同它下面的文件是一个条目（页面包），其余每个 `.md` 文件各是一个条目，`Match` 可以再缩小范围。`Blog.All()`、`Blog.In(lang)`、`Blog.Get(lang, id)`（没有或还看不到时返回 `tifa.ErrNotFound`）和 `Blog.Languages()` 返回条目，带 ID、slug、path、日期、译文和字数。`tifa build` 和 `tifa check` 检查声明、文件和每个 front matter（必填键、类型、不是字段的键并给出最接近的名字、slug、重复的 ID 和 path、指到集合外面的链接、`.mdx` 文件），`content/` 下没有集合读的文件夹让构建失败，没有 `content.go` 的应用也一样。某种语言缺一个版本的条目给出提示。程序在 `OnStart` 之前对每个条目跑 `Validate` 方法，有一个失败就不启动。测试用 `tifatest.LoadContent(t)` 读内容，或用 `tifatest.SetContent` 设置内容（见[内容](docs/content.zh-CN.md)）。
- 内容条目的正文在构建时转成 HTML。`entry.Body()` 以 `tifa.HTML` 返回条目的 HTML，交给 `<HTML>` 显示，`entry.Headings()` 返回它的标题和每个标题在 HTML 里的 `id`，用来做目录。转换在 Vite 插件里用 unified 进行，应用要装 `unified@11 remark-parse@11 remark-gfm@4 remark-rehype@11 rehype-raw@7 rehype-stringify@10`，缺哪个构建会说。它按 GitHub 风格读 Markdown，保留其中写的 HTML，去掉 HTML 注释，按 GitHub 的规则给标题生成 id。应用在 `vite.config.ts` 里用 `tifa({ markdown: { remarkPlugins, rehypePlugins } })` 加自己的插件。正文用相对 URL 指向的图片等文件，先在条目旁边找，再到它在默认语言的版本旁边找，从 `/assets/` 提供，文件名带哈希。指向 `.md` 文件的相对链接、找不到的文件和集合外的文件都让构建失败。正文放进程序的 `dist/content/`，从不单独提供。没有页面的应用（比如唯一的路由写出全文订阅源）也会转换，用的是它 `vite.config.ts` 里的插件。`go test` 里 `Body()` 返回写着条目名的占位，`Headings()` 返回 nil（见 [Markdown 转 HTML](docs/content.zh-CN.md#markdown-转-html)）。
- 内容条目的草稿和日期。写了 `draft: true` 的条目不转换，也不在程序里。`date` 还没到的条目在程序里但隐藏：它在程序运行中到了日期就出现，不用重新构建，也不用重启，从那一刻起 `All`、`In`、`Get` 返回它，它其他语言版本的 `Translations` 也列出它。只写日期按 UTC 零点算，一个至少每分钟叫醒一次的定时器也能赶上被调整过的时钟或睡眠醒来的机器。这一刻之前做好的带 `Cache` 的页面由下一个请求重新生成，所以文章列表马上就有新的一篇。程序打日志 `tifa: content: blog en/202611/release-notes is published (date 2026-11-20 09:00 +08:00)`，`tifa build` 和 `tifa check` 列出等待发布的条目。`tifatest.SetContentTime(t, tm)` 让测试看到 `tm` 时刻的条目。`tifa dev` 下草稿（`Draft` 为 true）和日期没到的条目都显示，写作时能在列表和文章页里看到它们（见[草稿和日期](docs/content.zh-CN.md#草稿和日期)）。
- `tifa dev` 跟着 `content/` 走：Markdown 文件、它的 front matter 或旁边的文件（比如图片）改了，就在 Go 里重新扫描内容，只转换涉及的条目，让正在运行的应用载入，不跑 go build，也不重启。应用重新跑 `Validate`，换上新内容，整页缓存过期，打开着的页面刷新。改 `content.go` 或 schema 类型，或者改 `vite.config.ts` 和它的插件，会全部重新转换。front matter 写错、文件夹没声明或插件报错时，浮层显示和 `tifa build` 一样的消息，有条目没通过 `Validate` 时显示 `The app did not load the content`，应用继续用原来的内容和原来的正文（见 [`tifa dev`](docs/content.zh-CN.md#tifa-dev)）。
- `tifa.HTML` 类型的字段装应用信任的标记，比如从 Markdown 转成 HTML 的文章，用 `tifa/client` 的 `<HTML value={post.body} as="article" />` 显示，`as` 只接受装普通内容的元素。它的 TypeScript 类型是 `HTML`，不是 `string`。服务器渲染的页面里每个值只在标记里带一份，水合数据里只放一个小标记，长文章不会发两遍。导航数据和 `SSR = false` 的页面带字符串。`tifa.HTML` 不能做 map 的键，不能放在方法函数的入参或会话里，也不能是 `Load` 或 `Layout` 的整个结果。用户提交的 HTML 先过滤再转换（见[显示应用信任的 HTML](docs/app.zh-CN.md#显示应用信任的-html)）。
- `<HTML>` 里的链接和 `Link` 一样走路由：点击指向应用页面的链接，不重新加载整个文档就显示那个页面，悬停或获得焦点 50 ms 后，或者手指一碰，就预加载它。按着修饰键、带 `target`、`download` 或 `rel="external"` 的链接，指向别的网站和协议的链接，同一页面的 `#锚点`，public 文件，`/assets/`、`/__tifa/` 和没有页面的路径（比如 API 路由）仍由浏览器自己跳转。应用写在 `<HTML>` 上的处理函数先执行，调用 `preventDefault` 就不走路由（见[显示应用信任的 HTML](docs/app.zh-CN.md#显示应用信任的-html)）。
- 新命令 `go tool tifa errors` 打印应用里正在运行的 `tifa dev` 上次构建以来的错误，看不到浏览器也看不到终端的编程 agent 可以用它检查改动：构建失败，或应用没起来、停了、没能加载 SSR 包，请求的意外错误（带状态、用户看到的消息和映射回源码的堆栈），打开着的页面在浏览器里的错误（水合不一致、未捕获的错误、未处理的 rejection，带页面地址），以及两次渲染不一致的页面。每类保留最近 20 条，重复的计次数，构建一开始就清掉之前的错误，构建进行中运行它会等构建结束。没有错误时退出码 0，有错误时 1，没有 `tifa dev` 在运行时 2，`--json` 输出全部字段。开发客户端现在也把面板里的错误报给 `tifa dev`。构建出的应用没有这些。`tifa new` 写的 `AGENTS.md` 会让 agent 运行它（见 [Tifa errors](docs/dev.zh-CN.md#tifa-errors)）。
- 应用在根目录的 `config.go` 里定义自己的配置：`var DBURL = tifa.String("db.url", "app.db", "Database file")`，另有 `tifa.Strings`（逗号分隔的一行）、`tifa.Int`、`tifa.Bool`、`tifa.Duration` 和 `tifa.Size`。每个都返回普通值，和 Tifa 的配置一样从参数 `--db-url`、环境变量 `DB_URL`、`.env` 和 `-c` 的文件读取，`go test` 里是默认值。值不合法时程序在 `OnStart` 之前退出，并一次列出全部不合法的值。`./bin/app config` 列出 Tifa 的和应用的配置、最终值和来源。在别处调用、键不是字符串字面量或不合规、两项配置的参数或环境变量相同，`tifa build` 都会报错（见[应用的配置](docs/config.zh-CN.md#应用的配置)）。
- 程序读运行目录下的 `.env` 文件，排在真实环境变量后面。每台机器一份，没有变体，不打包进程序。`tifa dev` 下应用读应用根目录的 `.env`，改了它应用会重启，不重新构建（见 [.env](docs/config.zh-CN.md#env)）。
- 程序为 `server.addr` 读托管平台的 `PORT` 和 `HOST`，排在 `SERVER_ADDR` 后面。只有 `PORT` 时监听所有网卡，Cloud Run 这类平台要求这样（见 [PORT 和 HOST](docs/config.zh-CN.md#port-和-host)）。
- `-c` 是 `--config` 的短参数。
- 同一个应用里再启动第二个 `tifa dev`，不再先起 Vite 再报一句光秃秃的 `address already in use`。现在它打印 `tifa: tifa dev is already running in this app at http://<地址> (pid <N>)` 并报错退出。有 `tifa dev` 在运行时 `tifa build` 也会这样停下，因为两者都写 `.tifa/dist`。没传 `--addr` 时，9527 被占用就依次试 9528 往后（一共十个端口），传了 `--addr` 而被占用则报错，信息里带着这个地址。锁是文件 `.tifa/dev.lock`（见 [`tifa dev`](docs/dev.zh-CN.md)）。
- `tifa dev` 把浏览器里的错误显示在页面角落的小面板里：`Hydration mismatch`（带 React 给出的差异和组件栈）、`Recoverable error`、`Uncaught error` 和 `Unhandled rejection`。同一个错误再次出现只增加计数，面板保留最近 20 个，每条都可以关掉。面板不挡页面，构建错误的浮层仍在它上面。构建出的应用没有这些代码（见 [`tifa dev`](docs/dev.zh-CN.md#浏览器里的错误)）。
- 刚克隆下来的应用现在会告诉你下一步做什么。应用有 `package.json` 而 Vite 没装时，`tifa dev` 和 `tifa build` 立刻停下，并给出要运行的命令（PATH 里有 pnpm 就是 `pnpm install`，否则是 `npm install`）。原来 `tifa build` 打印的是 `Vite is not installed in the application, install it with: pnpm add -D vite @vitejs/plugin-react`，对已经列了 Vite 的应用这条建议是错的，`tifa dev` 则把同一段话显示在浮层里。没有页面的应用没有 `package.json`，不检查。`tifa new` 生成的应用 README 多了一行，说明克隆后要运行什么（见 [`tifa dev`](docs/dev.zh-CN.md)）。
- 新增 `tifa docs`，打印应用所用 Tifa 版本的文档在哪个目录（用 `go list -m` 查，所以 `replace` 也对），再按 `scripts/docs-list.sh` 的格式打印索引：每篇文档的摘要和什么时候读。`tifa new` 写出的 `AGENTS.md` 让编程 agent 先运行 `go tool tifa docs`，不再链接 GitHub main 分支上的文档，那可能是和应用所用不同的版本。`AGENTS.md` 里由 Tifa 写的部分夹在 `<!-- tifa:begin -->` 和 `<!-- tifa:end -->` 之间，应用自己的说明在它后面的 `## This app` 下（见 [Tifa docs](docs/build.zh-CN.md#tifa-docs)）。
- 新增 `tifa check`，一条命令跑完应用的全部检查：路由约定分析、代码生成、`go vet ./...`、`go test ./...` 和 TypeScript 类型检查（用应用自己装的 `tsc`，npm 和 pnpm 都行）。遇到第一个失败的步骤就停，报错写明是哪一步，退出码非零。没有页面的应用跳过 TypeScript 这一步，也不需要 Node（见 [`tifa check`](docs/build.zh-CN.md#tifa-check)）。`tifa new` 写出的 `AGENTS.md` 把它作为改完之后要跑的唯一检查命令，不再分别列测试和类型检查命令。
- 一个目录可以放 `layout_.tsx` 代替 `layout.tsx`。它的页面和它下面的页面外面不套任何上层 Layout，根 Layout 也不套，上层的 `Layout` 函数也不为它们执行：整页渲染、浏览器导航的数据请求、它下面没有页面的 URL 的 404，都一样。根 Layout 的 `htmlAttributes`、`documentHead` 和 `OnRequest` 照常生效。同目录的 `Layout` 照常给它数据，它下面的 Layout 照常套在它里面，它失败时显示框架默认页面。`app/` 根目录放 `layout_.tsx`、同一个目录同时有 `layout.tsx` 和 `layout_.tsx`、用 `tifa.Use` 引用被 `layout_.tsx` 去掉的 Layout，都会构建报错（见[不套外层的 Layout](docs/app.zh-CN.md#不套外层的-layout)）。示例商店的店面用了它，所以店面页面不再带根 Layout 的空工作台数据，没有匹配页面的 URL 在工作台侧栏里显示店铺，店面没有的语言显示中英双语的店面 404 页面。
- docs/app.zh-CN.md 新增[多语言网站](docs/app.zh-CN.md#多语言网站)：语言目录、在它的 `Layout` 里检查语言、在它的 `head` 里写 `lang` 和 `hreflang`、不带语言的 URL 怎么跳转、翻译同步加载。示例店铺的店面照它写：`/store` 按浏览器的 `Accept-Language` 跳到 `/store/zh-CN` 或 `/store/en-US`，接住剩余路径的页面删掉了。
- 包可以引入只在 Node 上用的 Node 模块（比如渲染永远走不到的代码里用的 `node:fs`），应用照样加载、渲染。引入的每个名字都是一个替身，渲染用到它时才抛错，错误里有用到的导出、模块、包和它源码里的位置。以前打包时这样的 import 变成 `require`，每个虚拟机加载时就失败，哪怕只有 `import()` 才走到那个模块。`tifa build` 仍会列出留在 SSR 包里的 Node 模块（见[引入 Node 模块的包](docs/app.zh-CN.md#引入-node-模块的包)）。
- 挂起等待 promise 自己就能完成的事的组件（比如模块在包里的 `React.lazy` 组件，或对 promise 调 `use`）可以在服务端渲染：服务端等它，写出完整的页面，就是浏览器加载完后显示的样子。以前这样的页面会失败，错误说有组件挂起了。等定时器或请求的仍然报这个错。不挂起的页面 HTML 不变，渲染一样快（见[会等待的组件](docs/app.zh-CN.md#会等待的组件)）。
- 服务端渲染里有 `AsyncLocalStorage`，既是全局变量，也和 `AsyncResource` 一起由 `node:async_hooks` 导出，和 Cloudflare Workers 一样：有 `run`、`exit`、`getStore`、`bind` 和 `snapshot`。store 跟着 `await` 和 promise 回调走。fumadocs 把页面状态放在里面，现在除了它自己的 `collections` 别名，不用任何设置就能构建和渲染。
- 服务端渲染里，包和页面代码可以用顶层 `await`，fumadocs-mdx 的集合就这么用。以前有它的构建会失败。每个虚拟机加载时等它完成，再做第一次渲染。它只能靠 promise 完成，比如对包内模块的 `await import()`：等定时器或请求的永远等不到，虚拟机加载失败。
- 服务端渲染记住 i18next 翻译出的字符串，同样的键和选项再来时直接给出。示例商店的店面页面渲染时间是原来的 0.798，HTML 不变。默认打开，在项目文件里写上 `build.i18next_cache: false` 就关掉。运行了应用的格式化函数或后处理器的调用每次都重新翻译，`cloneInstance` 做出的实例的调用也一样，所以这些代码可以读请求里的东西。应用在第一次渲染之后绕开 i18next 的 API 改资源或选项，或者交给 i18next 别的会读请求的函数（比如 `overloadTranslationOptionHandler`）时，应该关掉它，否则页面可能显示过期的翻译。插件不认识某个 i18next 的代码时，构建打印警告，SSR 包和没有缓存时一样（[build.zh-CN.md](docs/build.zh-CN.md#i18next-缓存)）。
- SSR 池会主动替换回收后存活内存超过预热基线上界的 VM。渲染队列无人等待时，后台一次加载一个备用 VM，只与空闲 VM 交换，加载期间保留渲染容量。加载失败时旧 VM 继续服务，并按退避间隔重试。替换期间需要多留一个 VM 的内存，见[内存](docs/performance.zh-CN.md#内存)。

- `tifa.WithRenderClock(ctx, now)` 为一次服务端渲染指定固定或受控的时钟，Date、Temporal.Now 和 Intl 共用它。VM 复用前会清除时钟，替换后的 VM 也会使用当前渲染的时钟。生产环境默认保持实时，模块初始化仍在预热时执行。

- SSR VM 提供 Temporal 和 `Date.prototype.toTemporalInstant`，首次使用时安装完整实现。`Temporal.Now` 读取实时系统时钟，默认时区是 UTC，与 Date 和 Intl 一致。Temporal 的语言格式化通过 Intl 完成。浏览器支持仍由应用负责。

- SSR VM 提供 `atob`、`btoa`、`DOMException`、`crypto.getRandomValues`、`crypto.randomUUID` 和 `structuredClone`。随机字节直接从 Go 读取，渲染前、渲染时和模块加载时都能调用。宿主 API 在 VM 复用时沿用初始化保存的内建函数。getRandomValues 对共享或可调整大小缓冲区的视图抛 TypeError。克隆保留循环引用、共享引用和常见 JS 内建类型，不支持 transfer、SharedArrayBuffer 和可调整大小的 ArrayBuffer 视图克隆。与浏览器不同，普通对象和数组的透明 Proxy 通过 traps 克隆。不提供 `crypto.subtle`。

- SSR 等待受 `ssr.queue_limit` 和 `ssr.wait_timeout` 限制，默认最多排队 32 次，最多等 1 秒。队列满或等待超时返回 503，带 `Retry-After: 1` 和 `Cache-Control: no-store`，不占用 VM，也不调用 `OnError`。应用可用 `errors.Is(err, tifa.ErrSSRBusy)` 识别拒绝。`ssr.timeout` 仍然只限制执行时间。
- 页面的 HTML 紧跟在构建的标签后面，用 `<link rel="modulepreload">` 预加载它的 Layout 和路由的 chunk，这些文件和入口脚本一起下载。`Load` 失败时只预加载路由器显示的那几层。`build.json` 在 `preload` 下列出这些 chunk。
- 程序自己提供 HTTPS，默认关闭。`server.tls_domains` 向 Let's Encrypt 申请并续期证书，存在 `server.tls_cache`。`server.tls_cert` 和 `server.tls_key` 用你自己的证书，文件变了不用重启就重新加载。开了 HTTPS 时浏览器能用 HTTP/2，`server.http_addr`（`:80`）回应 Let's Encrypt 的验证，并把 HTTPS 有证书的名字用 308 跳转到 HTTPS，`server.hsts` 加上 Strict-Transport-Security。开了 HTTPS 还用默认的 `server.addr` 时拒绝启动，并提示设成 `addr: :443`，设成别的回环地址能启动但会打一行警告。`tifa dev` 仍走明文 HTTP。
- 示例应用多了给顾客的中英文店面，在 `/store` 下：商品搜索和筛选、商品详情、购物车、结算表单，以及用 Markdown 写、代码带高亮的手记。
- 路由的 `head` 可以返回 `htmlAttributes`，即这些页面 `<html>` 的属性，比如多语言网站的 `lang`。它们逐个属性合并到根 Layout 的 `htmlAttributes` 上，最深的路由优先，`null` 去掉属性。浏览器每次跳转后同步这些属性，不走 SSR 的页面也一样。Tifa 只在它要的值变了时才写某个属性，也只去掉自己写过的属性，所以页面脚本设的 class 或 `lang` 一直保留，直到某个路由要另一个值。不用这个功能的应用 HTML 不变，在浏览器里 Tifa 也和以前一样，首次渲染之后不再碰 `<html>`，页面脚本在上面改什么都保留。示例店面这样设置自己的语言，店面没有的路径用 URL 里的语言回答 404。

### 变更

- **不兼容：**Tifa 改用 `encoding/json/v2` 读写应用的 JSON（`Load` 和 `Layout` 的数据、处理函数的入参和结果、`c.JSON`、`c.Bind` 和会话），生成的 TypeScript 类型跟着变。应用会看到这些变化（见 [JSON 规则](docs/app.zh-CN.md#json-规则)）：
  - nil 切片写成 `[]`，nil map 写成 `{}`，以前两者都是 `null`。它们的 TypeScript 类型去掉了 `| null`，`days?: string[] | null` 变成 `days: string[]`。
  - `[N]byte` 数组和 `[]byte` 一样写成 base64 字符串，以前是数字数组。元素是具名 byte 类型的切片或数组仍是数字数组。
  - 指针接收者上有 `MarshalText` 的类型，不论放在哪里都写成字符串，字段或切片里的值也一样，以前只有指向它的指针才这样。这类类型可以做 map 的键。
  - `omitempty` 只省掉 `null`、`""`、`[]` 和 `{}`，不再省掉 `false` 和 `0`。要省掉零值用 `omitzero`。
  - `string` 选项只能用在数字字段上。
  - 构建拒绝 `time.Duration`，做 map 的键也不行，json v2 写不了它。请返回你要的单位的整数，或者字符串。
  - 构建拒绝 json v2 会出错的结构体：同一个结构体里两个字段 JSON 名字相同，未导出字段带 json tag，有字段却没有导出字段也没有 tag，嵌入非结构体类型却没写 JSON 名字，嵌入的结构体带 tag 选项或带 JSON、文本方法。
  - JSON 回答和页面数据里的 `<`、`>`、`&` 原样写出，以前写成 `<`、`>`、`&`。Tifa 把 JSON 放进页面 HTML 的地方照旧转义它们。
  - 字符串里不是 UTF-8 的字节照旧写成 U+FFFD。
  - 读取变严格。`c.Bind` 和处理函数的入参遇到这些情况回 400：名字和字段只是大小写不同，一个对象里同一个名字出现两次，字符串不是 UTF-8，时间不是 RFC 3339，base64 里有换行，Go 数组的长度对不上。
  - 存在外部存储里的会话记录按这些规则读不回来时，访客会被登出。
  - 不再支持 `GOEXPERIMENT=nojsonv2`。
  - `tifa routes --json` 和 `tifa errors --json` 输出的 JSON 用 `[]` 代替 `null`，也不再有 `<` 转义。
- **不兼容：**配置的环境变量去掉了 `TIFA_` 前缀：`TIFA_SERVER_ADDR` 改为 `SERVER_ADDR`，`TIFA_SSR_VMS` 改为 `SSR_VMS`。`tifa dev` 用 `SERVER_ADDR` 把地址传给应用。升级部署时把每个变量的前缀去掉。
- **不兼容：**程序只读自己的配置。不是配置的文件键、环境变量和参数一律不管，以前未知的键或 `TIFA_*` 变量会让程序退出。拼错的名字现在什么也不改，用 `./bin/app config` 检查。`-c` 的文件里的 `build.*` 也不管。
- **不兼容：**配置只支持 YAML。不再读 `tifa.json` 和 JSON 部署文件：把 `tifa.json` 改名为 `tifa.yaml`，按 YAML 写（JSON 也是合法的 YAML）。`tifa build` 校验 `tifa.yaml`，把它的值写成 Go 代码编进程序，不再嵌入这个文件。
- **不兼容：**Vite 不读 `.env`（`envDir: false`），`.env` 文件里的 `VITE_` 变量不再进浏览器包，这类值改由 `Load` 传。`tifa dev` 不再为 `.env.*` 文件重新打包。
- 示例商店把 `SHOP_OPEN` 改成它 `config.go` 里的配置 `shop.open`，`./bin/shop config` 能列出它，`--shop-open` 也能用。
- **不兼容：**处理函数的调用在 POST、PUT、PATCH、DELETE 成功之后不再重新取页面数据，调用的 `invalidate` 选项也没有了。调用只发出请求，并应用回答里的会话。重定向只做导航，重定向到正在显示的页面时页面留在原处。Tifa 不猜测一次写操作改了什么，TanStack Router 也不猜。想让页面显示写操作的结果，在调用之后调用 `router.invalidate()`（`await router.invalidate({ sync: true })` 会等到页面显示出新数据），或者更新你自己的状态。升级时删掉调用里的 `invalidate: false`，并在页面该显示写入结果的调用之后加上 `await router.invalidate({ sync: true })`。
- **不兼容：**`tifa build` 生成的路由改用 TanStack Router 默认的新鲜度，以前是 `staleTime: Infinity` 和 `preloadStaleTime: Infinity`，路由器也不再设 `defaultStaleReloadMode: 'blocking'`。数据立刻算作过期，所以回到访问过的页面时先显示缓存的数据，同时在后台发一个 `?_tifa=` 请求重新取，预加载的数据复用 30 秒。登出之后不该让下一个人看到旧数据的应用，调用 `router.clearCache()`。
- **不兼容：**`tifa/client` 的 `Session.In` 和 `Session.Out` 换成 `SignedIn` 和 `SignedOut`。它们各接收正好一个元素作子节点，自己不产生节点，只在有人登录或没人登录时显示这个元素，而旧组件会用一个 `<span>` 包住子内容。组件名拼错会让构建失败，而手写的 `data-session` 属性拼错是无声的，所以这个属性现在由 Tifa 私有，文档也不再教你手写它。开发模式下，不把 props 传给自己 DOM 元素的子组件会在挂载时报错并说明原因。更新应用时，把 `<Session.In className="x">…</Session.In>` 换成 `<SignedIn><div className="x">…</div></SignedIn>`，`Session.Out` 同理（见[在浏览器里](docs/sessions.zh-CN.md#在浏览器里)）。
- **不兼容：**会话类型里交给浏览器那部分的方法改名为 `Exposed() M`，原来是 `Browser() M`。新名字说明了它的作用：返回的内容会写进页面 HTML，并保存在访客的 `localStorage` 里，所以不能放凭据和需要保密的信息。更新应用时重命名方法即可，构建检查不变（见[会话](docs/app.zh-CN.md#会话)）。
- `tifa.ErrSSRBusy` 的文本改为 `tifa: render queue busy`，原来是 `ssr: render queue busy`，这样 Tifa 的所有错误值都以 `tifa: ` 开头。按旧文本匹配的脚本或告警要改成新文本。
- **不兼容：**`c.QueryAll(name)` 改名为 `c.QueryValues(name)`。它返回切片，而 Go 1.23 起带 `All` 的函数返回迭代器，比如 `maps.All` 和 `slices.All`。标准库里最接近的名字是 `http.Header.Values`。更新应用时重命名调用即可，构建检查不变（见[Load 里 Ctx 的用法](docs/app.zh-CN.md#load-里-ctx-的用法)）。
- **不兼容：**重定向改成包级函数 `tifa.Redirect(url, status)`，原来的 `c.Redirect(url, code)` 是 `Ctx` 的方法，却从不用 `c`。`status` 不是 3xx 时它会 panic，和 `WriteHeader` 遇到发不出去的状态码一样，以前它返回一个普通错误，最后变成 500。`tifa.RedirectError` 的 `Code` 字段改叫 `Status`，和 `tifa.Error` 一致。`c.Redirect` 删除。更新应用时把 `c.Redirect(` 换成 `tifa.Redirect(`，把 `redirect.Code` 换成 `redirect.Status`。给浏览器的回答不变（见 [route.go](docs/app.zh-CN.md#routego)）。
- **不兼容：**`OnStart` 的签名是 `func OnStart(ctx context.Context) error`，和 `OnShutdown` 一致。这个 context 是程序运行用的那个，收到 SIGINT 或 SIGTERM 时结束，所以等数据库的启动过程可以在程序被停止时中断。更新应用时加上这个参数。构建会拒绝旧签名（见 [hooks.go](docs/app.zh-CN.md#hooksgo)）。
- 新增 `Fields.Err()`，把 `Fields` 当作错误返回，没有任何条目时返回 nil。校验函数把消息收进 `tifa.Fields{}`，最后写 `return fields.Err()` 即可，不用再判断 `len`。直接返回空的 `tifa.Fields` 时服务端仍然答 422，因为它是一个错误（见[带入参和返回值的方法函数](docs/app.zh-CN.md#带入参和返回值的方法函数)）。
- 示例 shop 的店面用上了整页缓存。首页、手记列表和商品页缓存 10 分钟，商品列表缓存 5 分钟，手记文章缓存 1 小时，访客打开缓存的页面时直接从内存得到答案，带 `Cache-Status: tifa; hit`，不跑任何 `Load`。页面里不再有属于访客的东西：店面 Layout 只有语言、菜单和包邮价，顶栏的购物车和「最近看过」来自新的 `/store/{lang}/mine` 路由，由浏览器在页面加载后去要。商品页会告诉它现在打开的是哪个商品。见[店面的整页缓存](examples/shop/README.zh-CN.md#店面和渲染记忆)。
- 页面可以在 `route.go` 里写 `const Cache = 10 * time.Minute` 开启整页缓存。在这段时间里所有访客从内存拿到同一份回答，不跑 `Load`，也不渲染，前面的 CDN 会用 `s-maxage` 减去 `Age` 再保存：`Cache-Control: public, max-age=0, s-maxage=<Cache 的秒数>, stale-if-error=86400`、`Age` 和 `Cache-Status: tifa; hit`。这样的页面是匿名的：它的 GET、HEAD 和导航到它时的数据请求在进 `OnRequest` 之前去掉 `Cookie` 和 `Authorization`，`tifa.Session` 是 nil，`SetSession` 和 `ClearSession` 返回错误，HTML 上的 `Set-Cookie` 会被去掉。它的 `Load`、外层 Layout 或它们调用的函数读了 `c.Cookie`、`c.Header` 或 `tifa.Session`，构建会失败。它的请求只留一份短名单上的头（`Host`、`Accept-Encoding`、`If-None-Match`、`Origin`、`Sec-Fetch-*` 等），所以 `X-User-ID` 这样的头传不到 `Load`。键是协议、`Host` 请求头、路径和去掉跟踪参数（`utm_*`、`gclid`、`fbclid` 等，名单是新增的设置 `cache.ignore_query`）后排好序的查询串。时间到了以后，下一个请求重新生成，其他请求等它，生成失败（`Load` 出错、渲染失败或超时、池子忙）时给旧页，最多一天。只存状态码 200、301、302、308 的 GET 和 HEAD 回答，说了 `private`、`no-store`、`no-cache` 或 `Vary` 里有 `Accept-Encoding` 以外值的回答不存。导航的回答仍是 `private, no-cache`。条目和渲染缓存共用 `cache.size`。`tifa routes` 多了 `CACHE` 列，`--json` 的清单里多了 `cache`。`Cache` 必须是至少一秒的 `time.Duration`，`Cache = true` 仍然会构建失败。见[整页缓存](docs/app.zh-CN.md#整页缓存)。
- 浏览器请求一次导航的数据时，用页面 URL 加查询参数 `_tifa=<构建>.<位图>`（链上每个 `Load` 一位，要的写 1），不再发请求头 `X-Tifa-Route` 和 `X-Tifa-Build`。页面和它的数据现在是两个 URL，只按 URL 做键的缓存不会把它们混起来，`Vary: X-Tifa-Route` 也去掉了。参数在你的代码运行前就从请求里拿掉，`c.Query`、`OnRequest` 和渲染用的 URL 都看不到它，`Load` 读名为 `_tifa` 的查询参数会让构建失败。路由器预加载发出的请求在值后加 `.p`，日志里能区分它（它不能用来统计页面浏览量）。导航请求的回答永远不是 public：应用设的 `Cache-Control` 如果不是 `no-store` 或 `private, no-cache`，就换成 `private, no-cache`。用 `tifa.SetHTTPHandler` 挂的中间件里，处理函数返回后 `r.Pattern` 是路由模板，指标可以按路由计数。位数不等于链上 `Load` 的个数时答 400，构建不同仍答 409。处理函数调用不变。见[页面请求](docs/build.zh-CN.md#页面请求)。
- 所有页面默认开渲染缓存，`Cache = true` 删除。`Load` 和 Layout 的数据、URL、构建版本和会话都相同的页面只渲染一次，之后从 `cache.size` 大小的缓存里答（默认 64 MiB，0 关闭），同时没命中同一个页面的请求共用一次渲染，以前只有 `Cache = true` 的页面这样。命中时仍然跑 `Load` 和 Layout 并算数据的哈希，一个 VM、一个在途请求时每秒答 2,865 个请求，渲染是 38 个（[研究](docs/studies/nextjs-and-tanstack-start.md#cache-hit-against-cache-hit)里的机器），是同一处 Next.js ISR 命中的 4.6 倍。新的 `const RenderCache = false` 写在 `route.go` 里，给故意显示时间或随机数的页面关掉缓存。**破坏性变更：** 构建遇到 `Cache` 会报错并提示删掉，遇到小写的 `cache`、`rendercache`、`ssr`、`layoutssr`（以前会被悄悄跳过）也会报错。请删掉所有 `const Cache = true`。渲染时读了时间或随机数的页面，现在之后的请求会拿到第一次渲染的值，请检查这类页面：`tifa dev` 把每个页面渲染两次，两次不同时在终端和浮层里提示，并指出位置。带 `tifa.WithRenderClock` 的请求跳过缓存。`tifa routes` 的 `CACHE` 列改名为 `RENDERCACHE`，`--json` 清单里的 `cache` 改为 `renderCache`。见 [SSR 与缓存](docs/app.zh-CN.md#ssr-与缓存)。
- 示例店铺有了登录。/projects 下的工作台要用演示账号 `owner@shanhai.example` 和密码 `shanhai-demo` 登录，店面不用。没登录的访客会到 `/login`，登录后回到原来要看的页面。顶栏显示名字和退出按钮，或者登录链接。店铺把会话放在内存里，没有配置存储。用 `SHOP_OPEN=1` 启动可以不登录就看工作台，和 Next.js、TanStack Start 的对比以及页面压测用的就是它，见[登录](examples/shop/README.zh-CN.md#登录)。
- 会话传到浏览器。页面带着谁登录了，服务器给登录的人写 `<html data-session="in">`。`tifa/client` 的 `useSession()`、`Session.In` 和 `Session.Out` 显示它，任何带 `data-session="in"` 或 `"out"` 的元素从第一次绘制起就按它显示或隐藏，浏览器在 `localStorage` 里存一份副本。导航数据的回答从 `{路由 ID: 结果}` 变成 `{"routes": ..., "session": ...}`。处理函数调用答 `{"result": ..., "session": ...}`，只有入参的处理函数原来答 204，现在如果这次调用让某人登录或登出，就答 200 与 `{"session": ...}`。不带 `X-Tifa-Call` 的请求得到的回答和以前一样。新增 `GET /__tifa/session`。`<html>` 上的 `data-session` 由 Tifa 管。见[会话](docs/sessions.zh-CN.md#在浏览器里)。
- 构建会检查应用的会话。`tifa.SetSession` 和 `tifa.Session` 用了不同类型、有 `Session` 或 `ClearSession` 却没有 `SetSession`、会话类型不是能从 JSON 读回来的 struct、`Load` 或 `Layout` 能走到 `SetSession` 或 `ClearSession`、页面或 API 路由或 `public/` 里的文件用了 `/__tifa/` 路径，`tifa build` 和 `tifa check` 都会失败。会话类型有方法 `Browser() M` 时，`SessionData` 类型写在 `.tifa/session.gen.ts`（见[构建时的检查](docs/sessions.zh-CN.md#构建时的检查)）。
- 新增会话。`tifa.SetSession(c, &s)` 用新的随机 token 和 30 天的 cookie 让请求登录，`tifa.Session[S](c)` 在 `OnRequest`、`Load`、`Layout` 和方法函数里返回谁登录了（没有就是 nil），`tifa.ClearSession(c)` 登出。记录是不超过 4 KiB 的 JSON，存在 token 的 SHA-256 下面。不设存储时服务器把会话放在内存里，上限是 `session.memory_limit`，重启后所有人要重新登录。在 `OnStart` 里调 `tifa.SetSessionStore(s)` 换成你自己的 `tifa.SessionStore`，用 `tifatest.TestSessionStore` 检查它。cookie 叫 `__Host-tifa_session`（`tifa dev` 下是不带 `Secure` 的 `tifa_session`），用 `session.cookie` 和 `session.max_age` 设置。设置 cookie 的回答是 `no-store`，永远不会变成 304。`tifatest.NewCtx` 带一个每个测试一份的会话存储，`tifatest.WithSession` 造出已登录的请求（见[会话](docs/app.zh-CN.md#会话)）。
- 新增 `tifa.SetHTTPHandler(wrap)`，在 `OnStart` 里调用，用普通的 `net/http` 中间件或 handler 包住整个服务的 handler，比如 `/debug/pprof/` 或 OpenTelemetry 的 `otelhttp`。这层包在静态文件、跨站检查和路由的外面，所以它看得到每个请求，包括被拒绝的跨站写请求的 403，拿到的是原始的 `http.ResponseWriter`。它不走 `OnRequest`、`OnError` 和跨站检查，关停时仍会等它的请求。服务开始监听后再调会 panic（见[在 OnStart 里装配](docs/app.zh-CN.md#在-onstart-里装配)）。
- Tifa 的日志从 `log` 改走 `log/slog`，带级别和字段。请求失败是 `ERROR`，带 `method`、`path` 和 `err`，拒绝跨站写请求是 `WARN`，监听地址是 `INFO`，有请求时把请求的 context 一起交给 Handler，Handler 可以据此加 trace id。启动期的日志在 `OnStart` 返回之后才写，所以在 `OnStart` 里调 `slog.SetDefault`，它们都走新 Handler，Tifa 每条日志都现取 `slog.Default()`。日志行的格式变成 `msg key=value`，按旧格式读日志的脚本要改，但监听那行仍然含 `tifa: listening on http://地址`（见[换日志 Handler](docs/app.zh-CN.md#换日志-handler)）。
- **破坏性变更：** `OnRequest` 的签名改为 `func OnRequest(c *tifa.Ctx, next func(*tifa.Ctx) error) error`。原来的 `c.Next()` 换成 `next(c)`，执行匹配到的路由，只能调一次。`c.SetContext` 换成 `c.WithContext(ctx)`：它返回带着 `ctx` 的 `c` 的副本，`c` 本身不变，和 `http.Request.WithContext` 一样，写法是 `return next(c.WithContext(ctx))`。`c.Next`、`c.SetContext` 和 `tifatest.SetNext` 都删掉了，测试时直接调 `OnRequest(c, func(c *tifa.Ctx) error { ... })`，`next` 传自己的闭包。升级应用时，改签名，把 `c.Next()` 换成 `next(c)`，把传给 `c.SetContext` 的 context 改成传给 `next(c.WithContext(ctx))`。旧签名会让构建报错（见 [hooks.go](docs/app.zh-CN.md#hooksgo)）。
- 导出 `tifa.RedirectError`，即 `c.Redirect` 返回的错误的类型。`OnRequest` 和应用的测试可以用 `errors.As(err, &redirect)` 认出重定向，并读到它的 `URL` 和 `Code`。
- 访客开着的页面碰到部署后，不管是哪种不一致，都为新构建刷新一次。导航数据请求的 409 本来就会刷新，现在它还在响应头 `X-Tifa-Build` 里写上服务器运行的构建。处理函数带上页面的构建，服务器在处理函数运行前用同样的 409 拒绝来自别的构建的调用，所以旧页面不会再按新处理函数已经读不懂的类型去写。调用抛出状态 409 的 `TifaError`，页面刷新，调用不会再发一次。你自己的处理函数返回的 409 不会刷新任何东西，不带 `X-Tifa-Build` 的调用（比如 curl）不检查。代码块加载失败（Vite 的 `vite:preloadError`）也会刷新页面。每个构建只刷新一次，记在 `sessionStorage` 里，页面写下标记并读回来之后才会刷新。浏览器禁用存储时页面不刷新，直接显示失败，消息会说刷新一下页面。刷新后回来的页面仍然是旧的，就不再刷新：导航数据带着 409 失败，调用抛出这个 409，失败的 import 交给路由的 `Error`（见[部署时还开着的页面](docs/app.zh-CN.md#部署时还开着的页面)）。
- 应用的每个回答默认带 `Cache-Control: private, no-cache`，应用自己设了 `Cache-Control` 的除外。页面、304、404 页面、导航数据、方法处理函数的回答和重定向以前都不带这个头（导航数据带的是 `no-cache`）。CloudFront 默认策略这类 CDN 会把没有 `Cache-Control` 的回答存一天以上，Azure Front Door 会存好几天，属于某位访客的页面可能被发给别人。现在共享缓存什么都不存，浏览器仍可以存下页面，下次带着 ETag 回来问，没变就答 304。想用别的值，在 `OnRequest`、`Load`、`Layout` 或处理函数里设 `Cache-Control`。出错的回答（JSON 错误、Go 写的纯错误页、带错误的页面和过载回答）发 `no-store`，应用在出错之前设过 `Cache-Control` 的除外。应用的 404 页面用默认值。`/assets/` 和 `public/` 下的文件头不变（见 [SSR 与缓存](docs/app.zh-CN.md#ssr-与缓存)）。
- Vite 插件对 SSR 包少做三项改写，HTML 不变，渲染时间也不变。React 转义 HTML 用的 JavaScript 缓存和 `replaceAll` 链是给没有引擎转义函数的 VM 准备的，池里每个 VM 都有这个函数，所以没有哪次渲染跑过它们。只在有东西要读时才算元素的 key，省下的比同一个包两次运行相差的 0.11% 还少。其余每项改写都在 slowjs v0.29.8 的 C 引擎上重新测过，仍然划算，见[优化记录](docs/studies/optimizations.md#build)。
- **破坏性变更：** 浏览器从别的网站发来的 POST、PUT、PATCH 和 DELETE，在 `OnRequest` 和任何路由之前就被 403 拒绝，每个应用都一样（见[跨站写请求](docs/config.zh-CN.md#跨站写请求)）。用的是 Go 的 `http.CrossOriginProtection`：看 `Sec-Fetch-Site`，旧浏览器看 `Origin` 是否等于 `Host`，两个头都没有的请求，比如 curl 或 webhook，照常通过。回答和别的错误一样是 JSON，带 `Cache-Control: no-store`，`OnRequest` 看不到这个请求。没有设置能关掉它。新增 `server.trusted_origins`，让指定来源的页面可以写，`server.cross_site_paths` 接受 `POST /auth/apple/callback` 这样的路径模式，这些路径的请求不检查。升级应用时，把 `OnRequest` 里只做这件事的 Origin 检查删掉。别的来源的页面要往应用写的，比如跑在 `http://localhost:3000` 的前端，把该来源写进 `server.trusted_origins`。要接收别的网站发来的浏览器回调的，比如用 POST 表单回调的登录，把它们的路径写进 `server.cross_site_paths`，请求自己校验。代理改写了 `Host` 的，让 `Host` 原样通过。示例 shop 删掉了自己写的检查。
- 服务端渲染把小函数的调用直接写成函数体，HTML 不变。在压缩器之前，SSR bundle 里函数体是一个表达式、几条赋值或都 return 的 if（最多 32 个语法节点）的函数，只要名字只赋值一次、不用 `this` 和 `arguments`，调用处实参被计算的次数和顺序也和以前一样，它的调用就换成函数体，你的代码和每个包一视同仁。插件只看代码的形状，不认任何库。一次渲染少跑的指令：示例 shop 的工作台页面 0.63%，店面页面 2.63%，MUI 测试应用 1.58%，bench 应用 0.83%。SSR bundle 大了约 1%，这样的函数抛出的错误显示的是调用处的位置，栈里少一帧。见[前端产物](docs/build.zh-CN.md#前端产物)和[优化记录](docs/studies/optimizations.md#build)。
- 服务端渲染把每个节点交给 `retryNode` 时不再调用 `renderNodeDestructive`，HTML 不变。React DOM 渲染的每个节点都要经过这个函数，它只负责转手，一次调用比它干的活还贵。Tifa 插件把它的函数体直接写在调用处。示例 shop 的工作台页面一次渲染少 1.15% 的指令，店面页面少 0.76%，MUI 测试应用少 0.89%，bench 应用少 2.33%。渲染出错时的调用栈里没有这个函数的一帧。这项改写适用于 React DOM 19.3.0 和 19.3.1 的字符串渲染器，别的 React DOM 构建打印警告，保留调用。
- 服务端渲染对 React DOM 以前写过的属性，只查一次名字，HTML 不变。React DOM 每写一个属性，都要把它的名字和各个 case、别名和合法性再检查一遍。现在模块记住每个名字的结果，字符串或数字直接写成值前面那一块、值和一个引号。示例 shop 的工作台页面一次渲染少 2.54% 的指令，店面页面少 2.28%，MUI 测试应用少 0.43%，bench 应用少 1.81%。这项改写适用于 React DOM 19.3.0 和 19.3.1 的字符串渲染器，别的 React DOM 构建打印警告，沿用老办法。
- 用 `clsx` 拼类名的页面渲染略快：插件改写 clsx 2.0.1 到 2.1.1，不超过六个参数的调用（MUI 和 class-variance-authority 的调用都在内）不再遍历 `arguments` 对象，也不再为每个字符串调用一次函数。MUI 测试应用执行的指令少 0.25%，示例 shop 的店面少 0.15%，工作台页面少 0.08%，HTML 不变。某个 clsx 版本的文本不同时，构建打印警告，文件保持原样（[build.zh-CN.md](docs/build.zh-CN.md#前端产物)）。
- 服务端渲染改用 slowjs v0.29.7，HTML 不变。linux/amd64 上引擎的 C 文件链接之后再优化一遍，文件之间的调用都内联了。一个函数里从不同时存活的局部变量共用一个槽位，调用时要置空和释放的变少。`JSON.parse` 遇到不带转义的键和字符串直接从文本里读，示例 shop 的订单少跑 31% 的指令，1000 行的表格少 60%。共用槽位让 shop 工作台页面一次渲染少 1.4% 的指令，store 页面少 1.0%，MUI 测试应用少 0.8%。`JSON.parse` 让 bench 应用的表格页少 3.9%。linux/amd64 上表格页用时是原来的 0.968，`/inject/1` 是 0.831，其他页面变化都在 3% 以内。
- **破坏性变更：** `page.tsx` 和 `layout.tsx` 不再导出 `NotFound`。404 显示在 `Error` 里，它收到状态码为 404 的 `RouteError`，导出 `NotFound` 构建报错，错误信息会说明这一点。升级应用时，把每个 `NotFound` 显示的内容挪进同一文件的 `Error`，放在 `error instanceof RouteError && error.status === 404` 的分支里。自己和上面都没有 `Error` 的路由显示框架默认的 404 页面，和以前一样。
- 没有匹配页面的 URL 在它能匹配上的最深一层 Layout 里显示 404，以前只单独显示根 Layout 的 `NotFound`。从根到这一层的 `Layout` 函数照常执行，所以 404 页面保留应用的导航，在语言目录下面还保留 URL 里的语言。浏览器里跳转显示得一样，数据请求回答这些 Layout 的数据和状态码 404。`OnRequest` 看到的 `c.Pattern()` 仍为空，它的 `c.Next()` 现在会执行这些 Layout。想在 Layout 里显示 404，不再需要接住剩余路径的页面（见 [404](docs/app.zh-CN.md#404)）。Layout 失败的页面不再预加载这个 Layout 的 chunk，不管是什么状态码。
- 服务器渲染页面时组件抛错，显示页面的 `Error`，外面照常是各层 Layout，以前浏览器拿到的是 Go 写的简单错误页。这次渲染也失败时，错误一次往上挪一层路由，错误放在根 Layout 的位置也失败时才给 Go 的简单错误页。`OnError` 只看到这个错误一次，照旧由它决定状态码和公开消息，这种回答不进缓存，浏览器按服务器放错误的位置 hydrate。在 `tifa dev` 下错误栈显示在页面上的浮层里，保存修复后页面自动刷新。用完 `ssr.vm_memory` 的渲染照样在新 VM 上重试，超过 `ssr.timeout` 的不重试，同一个请求的几次渲染共用一个 `ssr.timeout`（见[页面上的错误](docs/app.zh-CN.md#页面上的错误)）。
- **破坏性变更：** 一个目录最多三个约定文件，`layout.go` 和 `route.tsx` 不再是约定文件。`route.go` 放这个目录的全部 Go 名字：`Layout`、`LayoutSSR`、`Load`、`SSR`、`Cache` 和方法函数，写在包里别的文件（包括 `layout.go`）会构建报错。页面组件改叫 `page.tsx`，原来叫 `route.tsx`，出现 `route.tsx` 时构建报错并提示改名。`layout.tsx` 不变。只写了 `Layout` 的 `route.go` 给下面的页面提供 Layout 数据，不是 API 路由，`Load` 只能和 `page.tsx` 一起出现。生成的 `+route.ts` 和 `+layout.ts` 换成每个有页面、Layout 或方法函数的目录一个 `route.gen.ts`。它导出 `page` 和 `layout`（代替 `route`）、`PageData` 和 `LayoutData`（代替 `Data`）、`Params`，以及方法函数和它们的 `<METHOD>Input`、`<METHOD>Output` 类型，用 `./route.gen` 引入。升级应用：把每个 `route.tsx` 改名为 `page.tsx`，把每个 `layout.go` 里的 `Layout` 和 `LayoutSSR` 移进同目录的 `route.go`，把 `./+route` 和 `./+layout` 的 import 改成 `./route.gen`。页面 HTML 不变。
- 服务端渲染把 `jsx` 和 `jsxs` 的调用直接写成它们返回的元素，HTML 不变。在 SSR bundle 里，config 是对象字面量、没有 spread、`ref`、`key` 和计算属性名，key 没有、是字面量或是普通变量的调用，直接变成那个对象。应用里的 React 是 19.3.0 或 19.3.1 时才这样做，别的 React 构建打印警告，调用保持原样。示例 shop 的工作台页面一次渲染少 1.6% 的指令，店面页面少 1.0%，bench 应用少 3.0%，MUI 测试应用少 0.5%。在 linux/amd64 上，工作台页面用时是原来的 0.987，店面页面 0.992，bench 页面 0.959，MUI 页面 0.995。示例 shop 的 bundle 大了 4.5%。
- 服务端渲染改用 slowjs v0.29.6，HTML 不变。生成的引擎里函数之间调用时从寄存器拿结果，不再从栈上读回。编译器能看出 `let`、`const`、`class` 变量已经写过时，读之前不再检查。示例 shop 和 MUI 测试应用的页面一次渲染少 0.8% 到 1.6% 的指令，shop 工作台页面在 linux/amd64 上用时是原来的 0.978。
- 服务端渲染改用 slowjs v0.29.5，HTML 不变。渲染更快：示例 shop 的工作台页面用时是原来的 0.976，商店页面 0.980，bench 应用的页面 0.943。函数的局部变量越多，调用省得越多。键相同的字面量对象共用一个形状。常见的两步合成一步，比如先读局部变量再读它的属性，以及 `typeof` 判断。渲染超时（`ssr.timeout`）现在也能停下这几种情况：恰好 65,536 个字符的字符串之间的长比较、在几千个等长长字符串组成的数组里查找，以及 `JSON.parse`、`JSON.stringify` 里的长拷贝。并入了上游 QuickJS 的 23 个修复，几个内置函数现在和 Node 给出一样的结果（比如负 BigInt 的 `BigInt.asUintN`）。
- 带长 `Load` 结果的页面渲染更快。某个 `Load` 数据的 JSON 文本不短于 4,096 个码元时，渲染不再自己把它写进水合脚本，而是在原处写一个短槽，等 VM 空出来之后由 Go 把槽填成文本，转义方式和 seroval 一样，所以数据里的引号、`</script>`、U+2028 和 U+2029 依然无害。示例 shop 和 MUI 测试应用的页面一次渲染少跑 0.3% 到 0.4% 的指令，1,000 行、每行两个 UUID 的表格（数据 92 KB）少 5.9%，数据 1 MB 的页面少 7.5%，数据 0.5 MB 的页面少 29%。没有长文本的页面渲染和以前一样。浏览器读到的数据不变。文本超过 16K 个码元时，router 以前把水合脚本拆成三个 script 标签，现在写成一个，和小页面一样，所以这样的页面短 80 字节。浏览器 bundle 变了几个字节，所以它的文件名变了。
- 服务端渲染改用 slowjs v0.29.4，HTML 不变。调用 C 写的内置函数少走解释器的一步，字面量对象的属性放进对象自己的块里：一次渲染少 0.1% 到 2.3% 的指令，渲染后每个 VM 少占 1% 到 2.4% 的内存。递归能到的深度和以前完全一样。
- 服务端渲染改用 slowjs v0.29.3，HTML 不变。页面卡在一次很长的调用里时，渲染超时（`ssr.timeout`）现在几次轮询之内就能让它停下，包括：遍历字符串、数组、Map 或 Set 而每项不调用别的函数（`Object.fromEntries`、展开 Set、`Iterator.prototype.toArray`、`Set` 的各个方法），用字符串做模式的 `replace` 或 `replaceAll`，切出一百万字符以上的 `slice`、`substring`、`trim` 或 `split`，以及键有几百万字符的 Map 或 Set 操作。以前这样的调用要跑到结束，或跑到 VM 被强制终止。
- 服务端渲染改用 slowjs v0.29.2，HTML 不变。页面卡在一次很长的 `split`、`padStart`、`padEnd`、`trim`、`isWellFormed`、`toWellFormed`、`encodeURI`、`decodeURI` 一类调用、`Array.from` 或类型化数组构造里，或在拼接、比较用 `+` 拼出的长字符串时，渲染超时（`ssr.timeout`）现在几千个字符或元素之内就能让它停下，以前要跑到这次调用结束。对象展开（`{...o}`）、解构的剩余部分和 `Object.assign` 和 Node 一样，轮到每个键时再查一次：前面的 getter 删掉后面的键或把它改成不可枚举，这个键不再被复制，把后面的键改成可枚举，这个键会被复制。
- 服务端渲染改用 slowjs v0.29 的 C 引擎，它是唯一的引擎：用 C 写的 QuickJS，编译成随模块发布的 Go 汇编。构建不需要 C 编译器，`CGO_ENABLED=0` 照旧。页面渲染出的 HTML 一样，用的时间更少（见下面的性能）。应用能看到的变化：
- `tifa dev` 第二次渲染页面时把时钟往后拨 36 小时，显示日期、精确到分钟的时间或「3 小时前」的页面也查得出来，以前连着渲染两次几乎查不出。提示里还按文件和行列出你的代码读时间或随机数（`Date.now()`、`new Date()`、`Date()`、`Math.random()`、`crypto`）的位置，React、TanStack Router 和 Tifa 自己的读取不列（见[日志](docs/dev.zh-CN.md#日志)）。
  - 应用可以为 linux 和 darwin 的 amd64、arm64，以及 windows/amd64 构建。为别的目标构建会停在 slowjs 报的错上，错误里列出这些平台。
  - 一次渲染里 `JSON.parse` 最多嵌套 2000 层，数据嵌套更深的页面原来能渲染，现在会失败，报 `RangeError: JSON nesting too deep`。
  - `ssr.vm_stack_depth` 默认仍是 10000，最大也仍是 10000。它现在数的是调用次数，原来的引擎数的是自己栈上的槽位。渲染里普通递归能到的深度仍和 Node 一样。无限递归的渲染让进程在 linux/amd64 上最多涨 26 MB，在 darwin/arm64 上涨 14 MB，原来最多涨 24 MB 的 goroutine 栈（见[栈深度](docs/config.zh-CN.md#栈深度)）。
  - `ssr.vm_memory` 限制的是引擎自己的分配器持有的内存。同一份堆现在数出来少 13% 到 15%，所以同样的限制能多装这么多。
  - `modernc.org/libc` 不再是 Tifa 的依赖。`tifa new` 固定的 SQLite 版本不必再和某个 libc 版本对齐。
  - 项目文件里的 `build.goasm` 没有了，因为已经没有可以切换过去的 Go 解释器。项目文件里再写它，会因为未知的键而失败。
- `tifa build` 不再用 CPU 剖析（PGO）编译应用。`build.pgo` 配置项和应用里的 `default.pgo` 文件都没有了，项目文件里再写 `build.pgo` 会因为未知的键而失败。引擎是 PGO 改不了的 Go 汇编，剖析也没有可量出的差别：在 linux/amd64 上，商店、MUI 应用和 bench 应用的页面用它和不用它的耗时比是 0.998、0.999 和 1.004，一次渲染执行的指令数是不用的 0.9987 到 1.0003，在 darwin/arm64 上商店的页面是 1.002（见[优化记录](docs/studies/optimizations.md#build)）。用 C 引擎之前的引擎时，它让页面在 linux/amd64 上快约 3%，在 darwin/arm64 上快约 7%。升级 slowjs 时不用再重采剖析。
- 空闲的 VM 会把空闲内存还给系统。一个 VM 一秒没有渲染，就把渲染留下的空闲页和碰过的栈还回去，两次渲染之间的完整回收原来什么也没还，现在也会还。程序里示例商店的一个 VM 空闲时占 9.6 MiB，原来是 12.8 MiB，MUI 测试应用的一个 VM 占 5.6 MiB，原来是 7.7 MiB，约少四分之一（见[内存](docs/performance.zh-CN.md#内存)）。至少每秒渲染一次的 VM 留着自己的内存，所以按负载估算容量不变。等待之后的第一次渲染多花 1 到 2.5 ms，渲染时间不变。
- 示例商店换了外观，页面也重到和真实网店相当。外观照着印刷的商品目录来做，英文标题用 Newsreader，随应用打包，中文标题和正文用系统字体。每件商品有三张 AVIF 图，各有三种宽度。店面加了分类菜单、购物车抽屉、轮播、商品图库、分页的带图评价、问答和搭配购买。商品列表有带数量的筛选、常用筛选、已选条件、网格和列表视图和快速查看，商品页有完整规格和同类商品对比。工作台的订单列表每页 25 行，带商品图，可以按日期、快递和金额筛选，有行操作菜单和批量操作。订单详情有操作记录、内部备注、付款、库存检查、拣货单和物流轨迹，销售统计有每件商品的明细表、下单时段热力图、快递表现和常客。页面内容多了，渲染也就更久：新数字见[优化记录](docs/studies/optimizations.md#the-redesigned-shop)，这不代表 Tifa 变慢了。打包进去的字体和图片让 darwin/arm64 上的 `bin/shop` 从 31.1 MB 变成 35.9 MB。
- 带大块 `Load` 数据的页面渲染更快。每个 `Load` 的 JSON 文本现在作为交给渲染的一个单独参数，Go 不用再把它转义进输入的 JSON，VM 也不用解析两遍。一个 1,000 行、每行两个 UUID 的表格，渲染时间是原来的 0.986（0.965 到 0.991，6 轮全部更快），服务端准备渲染输入的时间是原来的 0.665（0.655 到 0.682）。示例商店的工作台和店面页面数据只有几 KB，变化在各轮的波动之内（一组页面在原来的 0.987 到 1.008 之间）。HTML 不变，浏览器包也不变。
- 装了 rolldown 1.2.15 或 seroval 1.7 的应用，也能拿到 Tifa 插件的提速。rolldown 1.2.15 在每个模块开头写的是 `// 路径` 注释，不再是 `//#region 路径`，seroval 1.7 把要问的插件传给 `parsePlugin`，所以原来插件会报警告，React 的命名空间留着 getter，路由数据里的数组按 seroval 自己的写法写出，渲染更慢。新建的应用装的就是这两个版本。页面 HTML 不变。
- 服务端渲染改用 slowjs v0.28.6。在 linux/amd64 上，服务端渲染改用 slowjs 的 Go 汇编解释器执行 JavaScript，slowjs 现在在这个平台上默认用它。页面渲染出的 HTML 一样，用的时间更少：一个 VM 时，示例商店的工作台页面耗时是原来的 0.940，MUI 测试应用的页面是 0.850，8 个超线程上跑 8 个 VM 时，程序每秒渲染的商店页面是原来的 1.085 倍，MUI 页面是 1.211 倍。
- 服务端渲染改用 slowjs v0.28.5，它让异步上下文跟着 promise 任务走，服务端的 `AsyncLocalStorage` 靠的就是这个，还会直接从 shape 列出普通对象的自有键，用于 `Object.keys`、`for in`、对象展开和 `JSON.stringify`。
- 服务端渲染改用 slowjs v0.28.4，模块跑完后它会释放模块的顶层函数和它留住的变量。
- SSR 包和浏览器包一样在严格模式下运行：它现在是一个 ES 模块，每个虚拟机把它当模块运行（见[顶层 await 和严格模式](docs/app.zh-CN.md#顶层-await-和严格模式)）。只能在非严格模式下运行的包，在服务端也会失败。`tifa build` 像服务端那样把 SSR 包编译一遍，但不运行它，所以只有非严格模式允许的语法（比如 CommonJS 文件里的 `with` 语句）会让构建失败，错误里有源文件中的位置，不会等到服务端启动才失败。给从没声明过的变量赋值，运行到那里时抛 `ReferenceError`。页面渲染出的 HTML 不变。
- 服务端渲染改用 slowjs v0.28.3。带 v 标志的正则接受规范允许、Node 也接受的写法，比如 `[\&\;]` 和 `/-/v`，以前会抛 SyntaxError。shiki 生成的正则都能编译，捕获组超过 254 个的除外。
- 服务端渲染改用 slowjs v0.28.0，也就是 quickjs 引擎的新名字（`github.com/liustack/slowjs`，原来是 `github.com/liustack/quickjs`）。它就是 quickjs v0.27.3 的引擎换了名字，页面渲染出的 HTML 不变。slowjs 支持 amd64 和 arm64 上的 Linux 与 macOS，以及 amd64 上的 Windows，所以应用不能再为 FreeBSD、32 位平台或其他 CPU 构建。
- 服务端渲染改用 quickjs v0.27.3。池子在渲染之间调用 `VM.RunYoungGC`。字符串查找（`includes`、`indexOf` 和 `lastIndexOf`）用 Go 的 `bytes` 包跳过不匹配的内容，查过的文本计入中断检查。
- 服务端渲染改用 quickjs v0.27.2。页面在循环里编译正则或拼字符串时，渲染超时（`ssr.timeout`）现在几毫秒内就能把它停下，以前这样的循环会越过超时再跑好几秒：编译正则，以及解析源码或拼字符串的内置函数（`JSON.parse`、`JSON.stringify`、`join`、`repeat`、`replaceAll` 等）都计入引擎的中断检查。这类内置函数的单次长调用仍会跑完。渲染之间的 GC 一次就能释放键已不可达的 WeakMap 条目，值反过来指向键时也一样，以前这样的条目会一直留在复用的 VM 里。HTML 不变（见下面的性能）。
- 服务端渲染改用 quickjs v0.27.1，HTML 不变，渲染更快（见下面的性能）。
- 用 emotion 写样式的页面（比如 MUI）渲染更快：emotion 给每段样式算的哈希改在 Go 里算，结果不变。MUI 测试应用的页面渲染时间少 28% 到 29%，HTML 不变。
- 服务端渲染改用 quickjs v0.27.0。页面渲染时可以用全部 `Intl` 构造器和 Chromium 有的全部历法，`localeCompare` 按语言规则排序，和浏览器一样，以前按码点比较。程序现在带上了 go-intl 的数据和代码，分发时要附上六个声明文件，以前只有 `LICENSE-DATA` 一个（见 [build.zh-CN.md](docs/build.zh-CN.md#要随程序附带的声明)）。
- 有页面的应用装的 React 或 React DOM 低于 19.3.0 时，`tifa build` 和 `tifa dev` 报错停下。
- React DOM 以外的模块代码里出现 `$$FORM_ACTION` 时，构建打印警告并写明是哪个模块，因为这样的表单拿到的键和没有插件时 React 写的不一样。
- 浏览器里，导出 `head` 的路由模块和其他路由模块一样随路由加载，不再放在入口脚本里。示例 shop 的入口 gzip 后从 224 KB 降到 146 KB。这类模块顶层的代码改在路由第一次加载时运行，每个页面都需要的初始化应放在根 Layout 里。入口里的模块 import 了路由模块时，它留在入口里，构建打印打包器的 `INEFFECTIVE_DYNAMIC_IMPORT` 警告。
- 地址监听不了时报错会指出是哪项配置，端口只有 root 能用时提示用 `setcap cap_net_bind_service=+ep`。
- `tifa build` 用 `-ldflags=-w` 去掉程序里的 DWARF 调试信息，`GOFLAGS` 里 `-ldflags` 的参数放在它前面照样生效。示例商店在 darwin/arm64 上的程序从 26.4 MB 降到 21.2 MB。panic、`runtime.Caller` 和 pprof 照样给出文件和行号，但 Delve 这类调试器没法单步调试这个程序。在项目文件里写 `build.debug_info: true` 可以保留调试信息。`tifa dev` 和以前一样保留。
- `Cache = true` 页面渲染时的 panic，现在作为渲染错误 `tifa: render page: tifa: panic: ...` 交给 `OnRequest` 和 `OnError`，带渲染时的栈，不再是以前那样的普通 `tifa: panic: ...`（`Load` 里的 panic 仍然是这样）。`tifa dev` 下它的错误页写的是请求在服务端渲染时失败，而不只是请求失败。

### 性能

- 页面的服务端渲染更快：大多数元素的开标签和闭标签改由 slowjs 用 C 写，原来 React DOM 每写一个标签要调十几次 JavaScript 函数。引擎把整个开标签写成一个字符串，遇到不能保证写得和 React 一样的元素就交回 React，示例应用页面上的 469,530 个标签一个也没交回。页面耗时减少：shop 工作台 7.9%，店面 7.5%，MUI 测试应用 4.4%，bench 应用 32%。 页面逐字节不变。这处改造只用于 React DOM 19.3.0 和 19.3.1：C 函数替代的 28 个部分里任何一个不同，构建打印警告，所有标签由 React 写（[build.zh-CN.md](docs/build.zh-CN.md#前端产物)）。
- MUI 页面的服务端渲染更快：`@mui/system` 的 `memoTheme` 按主题生成的样式在多次渲染之间沿用。`ThemeProvider` 每次渲染都把主题展开成一个新对象，`memoTheme` 只对同一个对象沿用样式，所以每次都重新生成。现在插件把自有键和值都与见过的某个主题相同的普通对象主题当作那个主题。MUI 测试应用的 107 份这样的样式每个 VM 只生成一次，页面耗时是原来的 0.917，执行的指令少 9.3%（linux/amd64，16 个代码位置），HTML 不变。应用在渲染之后改了主题里面的某个对象，再传一个装着同一批对象的新主题对象时，会一直用旧样式，所以要换主题就造一个新的。`memoTheme` 的文本不同时，构建打印警告，代码保持原样（[build.zh-CN.md](docs/build.zh-CN.md#前端产物)）。
- 只有包里有某个模块的代码带 `$$FORM_ACTION` 时，React 才建叫 key path 的数组，原来它渲染的每个元素都要建一个。页面渲染出的 HTML 一样，shop 工作台页面少执行 1.2% 的指令，店面页面和 MUI 测试应用少 0.7%，bench 应用少 1.8%。这项改动只用于插件读过原文的 React DOM 19.3.0 和 19.3.1 的字符串渲染器，换成别的文件，构建会打印警告，key path 照建。包里有模块的文本含 `$$FORM_ACTION` 时（插件就靠这个判断），`useActionState` 写进带 server action 的表单的键和以前一样。运行时才拼出来的属性名认不出来，这样的键按空 key path 算哈希。Tifa 自己没有 server action（见[构建](docs/build.zh-CN.md#前端产物)）。
- 页面渲染略快一点，HTML 不变。React DOM 对几乎每个属性都要问三次值是不是函数、symbol 或布尔值，这三种它都不写。Tifa 插件先问值是不是字符串，几乎每次都是，别的类型才接着问。渲染一次，示例 shop 工作台页面少跑 0.30% 的指令，店面页面少 0.24%，bench 应用少 0.19%，MUI 测试应用少 0.07%。React DOM 里的代码和 19.3 不同时，构建打印警告，渲染和以前一样。
- React 的 `createElement` 和 `cloneElement` 把 config 拷进元素 props 的活改在引擎里跑。slowjs 里有做这件事的循环，是一个 C 函数（`RegisterCopyProps`），池里每个 VM 都以 `__tifa_copy_props` 的名字拥有它，构建让这两个函数先问它，再走自己的循环。它只拷循环不可能做出别的结果的 config，其余的（Proxy、有 getter 的、类的实例、数组、超过 32 个键的）仍走 React 的循环。渲染一次，示例应用 shop 的工作台页面少跑 2.9% 的指令，店面页面少 2.1%，MUI 测试应用少 4.1%，HTML 不变。在 linux/amd64 上按 8 个代码位置计时，页面渲染花原来的 0.988、0.989 和 0.980，在各位置的差距之内，所以结论看指令数。没有这个函数的 VM，以及插件不认识的 React，仍走 React 的循环（[build.zh-CN.md](docs/build.zh-CN.md#前端产物)）。它需要带 `RegisterCopyProps` 的 slowjs 版本。
- 用 react-i18next 翻译的页面渲染更快：每次调用 `useTranslation` 都要做的 i18n 对象副本，在对象只有普通属性时按记下的键列表来做，别的对象仍照原样用描述符做。shop 的店面页面渲染时间是原来的 0.967，执行的指令少 6.0%，HTML 不变。react-i18next 某个版本改了 `createI18nWrapper` 时，构建打印警告，那个函数保持原样（[build.zh-CN.md](docs/build.zh-CN.md#前端产物)）。
- 用了 shadcn/ui 的 `asChild` 组件的页面渲染略快：`@radix-ui/react-slot` 的 `Slot` 合并 props 时改用循环，按名字的字符码区分事件处理函数，原来每个 prop 都要测一次正则。shop 的工作台页面执行的指令少 0.58%，店面页面少 0.24%，HTML 不变。react-slot 某个版本改了 `mergeProps` 时，构建打印警告，那个函数保持原样（[build.zh-CN.md](docs/build.zh-CN.md#前端产物)）。
- React DOM 对每段文本和每个属性值的转义改在引擎里跑。slowjs 里有用 C 写的 `escapeTextForBrowser`（`RegisterEscapeText`），池里每个 VM 都以 `__tifa_escape` 的名字拥有它，构建让 React 的字符串渲染器调用它，不再走 JavaScript 写的函数加缓存和 `replaceAll`。渲染一次，示例应用 shop 的工作台页面少跑 2.5% 的指令，店面页面少 2.4%，MUI 测试应用少 0.7%，bench 应用少 4.5%，HTML 不变。在 linux/amd64 上按 8 个代码位置计时，工作台页面渲染只花原来的 0.974，店面页面 0.975，bench 页面 0.938。没有这个函数的 VM，以及插件不认识的 React DOM，仍用缓存和 `replaceAll`（[build.zh-CN.md](docs/build.zh-CN.md#前端产物)）。它需要带 `RegisterEscapeText` 的 slowjs 版本。
- React DOM 的 `pushAttribute` 写 `class`、`tabindex`、`dir`、`role`、`viewBox`、`width`、`height` 以及 XLink 和 XML 的属性时不再调用 `pushStringAttribute`，别的属性名先查有效名字的缓存，查不到才调 `isAttributeNameSafe`。渲染一次，示例应用 shop 的工作台页面少跑 0.8% 的指令，店面页面少 0.7%，bench 应用少 1.0%，MUI 测试应用少 0.1%，HTML 不变。在 linux/amd64 上按 8 个代码位置计时，页面耗时是原来的 0.993 到 0.997，在各位置的波动之内。这几个函数中有一个和 React DOM 19.3 不同时，构建打印警告，页面和以前一样快（[build.zh-CN.md](docs/build.zh-CN.md#前端产物)）。
- 服务端渲染改用 slowjs v0.29.1，HTML 一样。slowjs 在 linux/amd64 上不开剖析、按 8 个代码位置，逐项和前一步比。只有普通键的对象字面量一次按最终形状建好：示例商店的工作台页面耗时是原来的 0.956，MUI 页面 0.969，bench 应用的表格页 0.942。在此之上，函数调用设置和释放值的步骤更少，React 里配合 `hasOwnProperty.call` 的 `for in` 循环不再真的调用这两个函数：工作台页面耗时是 0.982，MUI 页面 0.976，bench 页面 0.952。普通对象（比如 React 元素的 props）从 72 字节缩到 48 字节，长 Markdown 页面渲染时的内存峰值低 3.8%。
- slowjs 的 C 引擎渲染示例商店的工作台页面、MUI 测试应用和 bench 应用的页面，耗时是 v0.29.0 之前的 slowjs 加 Tifa 当时自带剖析的 0.834 到 0.860，HTML 一样，而且不开剖析（11 个页面，一个 VM 一个核，8 个代码位置全部更快，见[引擎](docs/studies/optimizations.md#engine)）。渲染时 Go 垃圾回收让程序停的时间一样短：1 个 VM 时 P99 是 131 到 164 µs，3 个 VM 时是 197 µs，最长一次停 3.1 ms 和 0.9 ms，原来是 4.2 和 2.1 ms。示例商店的一个 VM 加载后占进程 8.5 到 8.6 MiB，渲染后 12.8 MiB，空闲后 9.6 MiB，MUI 测试应用的一个 VM 是 3.9、7.4 到 7.6 和 5.3 到 5.5 MiB（见[内存](docs/performance.zh-CN.md#内存)）。

- lucide-react 的图标渲染更快：每个 VM 记住 lucide-react 给 props 相同的图标构建出的结果，再次用到时直接交出去。shop 的工作台页面渲染时间少 4.3%，店面页面少 5.8%，HTML 不变。lucide-react 某个版本改了缓存依赖的文件时，构建打印警告，图标照旧渲染（[build.zh-CN.md](docs/build.zh-CN.md#前端产物)）。
- 用 i18next 翻译的页面渲染更快：`getFixedT` 和 `useTranslation` 的 `t` 只在键或键前缀写成函数时才复制 i18next 的选项，i18next 也记住分辨带命名空间的键和一句话用的正则。shop 的店面页面渲染时间少 5.2%，HTML 不变。i18next 某个版本改了这两个函数之一时，构建打印警告，那个函数保持原样（[build.zh-CN.md](docs/build.zh-CN.md#前端产物)）。
- SSR 池里的 VM 借助 slowjs 的 `SharedBytecode` 共用一份 bundle 函数源码和给错误栈提供行号的表，所以示例商店的每个 VM 加载后少占 2.35 MiB（9.0 MB，原来 11.5 MB），MUI 测试应用的每个 VM 少占 1.22 MiB。`toString`、错误栈和 HTML 都不变。这份共用的副本不算进 `ssr.vm_memory`。渲染时间不变。
- SSR 包执行的字节码指令更少：压缩之后，`tifa build` 和 `tifa dev` 把 SSR 包里的语句 `a && b;`、`a || b;` 和 `a ? b : c;` 写成 `if` 语句，`!0` 和 `!1` 写成 `true` 和 `false`，浏览器包不动。MUI 测试应用的页面渲染时间为原来的 0.989（6 轮全部更快），商店工作台页面为 0.994，在各轮的离散度以内，HTML 和错误栈里的位置都不变。
- quickjs v0.27.2 渲染商店工作台页面和 MUI 测试应用页面的耗时和 v0.27.1 一样，开不开 PGO 都在 1% 以内（不开 PGO 时为 1.006 和 0.991，开 PGO 时为 0.993 和 0.996），HTML 不变。GC 之后，VM 从分配器占的内存最多少 0.6 MiB。
- quickjs v0.27.1 渲染商店工作台页面，不开 PGO 时耗时为 v0.27.0 的 0.991，开 PGO 时 0.982，MUI 测试应用的页面是 0.986 和 0.980，HTML 和内存不变。加上 emotion 的哈希改在 Go 里算，MUI 页面的耗时是这项改动之前（还在 v0.27.0 上时）的 0.70，开不开 PGO 都一样，商店不开 PGO 时是 0.987，开 PGO 时是 0.984。
- quickjs v0.27.0 渲染商店工作台页面，不开 PGO 时耗时为 v0.26.17 的 0.982，开 PGO 时一样，因为它的改动带来的那部分 PGO 原本就拿到了。每次渲染都新建同样正则的页面（比如高亮代码的页面）每个 VM 只编译一次：商店里高亮 CSS 的那篇文章不开 PGO 时渲染耗时为原来的 0.893。
- 每个页面都要加载的代码放进入口脚本这一个文件，不再是四个。走 HTTP/1.1、往返 150 ms 时，shop 的页面完成 hydration 比以前早 11 到 192 ms，往返 40 ms 时早 4 到 77 ms，首次绘制晚 8 到 24 ms（走 HTTP/2 时晚 8 到 48 ms）。由 Tifa 自己走 HTTP/1.1 提供服务时，每页要 17 到 20 个文件的应用在往返 150 ms 时完成 hydration 晚 58 到 100 ms，前面放一个讲 HTTP/2 的代理或 CDN 就能避免。
- Vite 插件对 SSR bundle 再做四项改写：React 转义 HTML 改用 `replaceAll`，不再构建组件栈，命名空间拿到的是普通值而不是 getter，key path 里不再放组件名。合起来示例 shop 渲染快 6.5% 到 7.5%，纯文字页面快 17.6% 到 18.3%，HTML 不变。
- 返回数组（比如切片）的 `Load`，数据现在也以 Go 写的 JSON 文本进页面，和对象一样。1,000 行的表格渲染用时少 39%。seroval 的代码换了写法时，构建打印警告，页面照旧渲染。
- Go 交给渲染的数据里，U+FFFF 以上的字符（比如 emoji）写成代理对的转义，shop 订单列表的数据里有一个 emoji 时不再慢 9%。浏览器读到的值不变。
- quickjs v0.26.16：不开 PGO 时，其中两项改动让渲染耗时变成 v0.26.15 的 0.971，另外三项再变成这个的 0.955，HTML 不变。每个 VM 留住最近对象的形状和释放的小块再用，所以垃圾回收后多占约 0.7 MiB，其中 647 KiB 是形状。页面建很多键和别的对象都不一样的对象时，形状最多能占约 6 MiB。形状算在 `ssr.vm_memory` 里。
- `Cache = true` 的页面同时有多个请求没命中缓存、数据也相同时，它们共用一次渲染，所以冷页面上的突增（比如刚部署完）只渲染一次。每个请求保留自己的响应头，拿到自己接受的编码，页面只为它们压缩一次，渲染失败时等它的每个请求都拿到这个错误，等别人渲染的请求带 `X-Tifa-Render: shared`。等待中客户端断开的请求，连接被中断，不发响应，也不调 `OnError`。没有请求再等时才失败的渲染写进日志。对 shop 一个冷的订单页同时发 50 个请求，以前渲染 50 次、146 到 221 ms 答完，现在渲染一次、30 到 52 ms 答完。
- 池子每次渲染后跑一次年轻垃圾回收，只看这次渲染新建的对象，只在 VM 比上次完整回收后长到 1.5 倍、引擎在渲染中途回收过、或满 256 次渲染时才做完整回收。一个 VM 时，示例 shop 渲染的 P99 是原来的 0.748，MUI 的页面是 0.940，两个 VM 时是 0.796 和 0.919。P50 和 CPU 的变化都在 2.1% 以内，HTML 不变，内存峰值是原来的 0.82 到 0.88。
- 渲染留下的垃圾由池子在两次渲染之间、请求答完以后回收，不再由引擎在渲染中途回收。一个 VM 时，示例 shop 渲染的 P99 低 12% 到 41%，MUI 的页面低 4% 到 7%，P50、吞吐和 HTML 都不变，渲染后一个 VM 少占 3% 到 14% 的内存。所有 VM 都忙、请求在排队时，下一个请求仍要等这次回收。

### 修复

- `tifa dev` 下刚保存完就加载的页面，现在拿到的是新的服务端渲染。之前在大约头 100 毫秒里（文件还没安静下来、重建还没开始）到达的请求拿到的是旧的。保存导出了 `head`、`htmlAttributes` 或 `documentHead` 的 `page.tsx` 或 `layout.tsx` 时，Vite 会立刻刷新页面，浏览器因此常常撞上这段时间：页面还是旧文字，React 报 `Hydration failed`。现在这样的请求会等重建完成（见[改动触发什么](docs/dev.zh-CN.md#改动触发什么)）。
- `tifa new` 写出的应用的页面现在会立刻显示新加的待办和标记完成后的状态。之前不会：注释说写入之后页面数据会重新加载，但 Tifa 从不自己重新加载，列表要刷新页面才更新。现在添加表单和完成按钮在写入之后调用 `router.invalidate()`，注释也改成了实际情况（见[在浏览器里调用方法函数](docs/app.zh-CN.md#在浏览器里调用方法函数)）。
- `tifa dev` 的 head 里不再放 Vite 插件用 `transformIndexHtml` 加的标签，只保留 Vite 客户端和 React 刷新运行时。`tifa build` 从不跑这个钩子，这类标签以前在 `tifa dev` 里有、上线后悄悄消失。现在 `tifa dev` 会为每个省掉的标签打印警告并写出标签。要在两边都有，把标签写进根 `layout.tsx` 的 `head`。
- `tifa dev` 的 SSR 包也像 `tifa build` 一样把 React 和 React DOM 命名空间的导出换成直接的值，不再打印 `tifa rewrite not recognized: react, react-dom`。`tifa dev` 的包不压缩，留着 React 的许可注释，改写因此认不出 React 的模块，dev 下页面每读一个导出都要调一次 getter。
- `app/` 根下有参数文件夹（比如 `app/lang+`）的应用里，指向公共目录文件的链接不再去请求导航数据。浏览器里的路由把 `/favicon.svg` 当成页面 `/{lang}`，而服务器是先回答公共文件的，所以 `<Link to="/favicon.svg">` 和 `router.navigate` 会为它取页面数据。现在生成的路由列着公共文件，导航到其中任何一个、或到 `/assets/`、`/__tifa/` 下的路径，都会整页加载。每条页面路由还带一个标记，运行时新增的 `isPage(router, pathname)` 能把页面和这些路径、显示 404 的 URL 区分开（见[路由树](docs/build.zh-CN.md#路由树)）。
- 公共目录里新增或删除文件时，`tifa dev` 会跑打包这一步，因为生成的代码列着这些文件。只改文件内容仍然不构建（见[改动触发什么](docs/dev.zh-CN.md#改动触发什么)）。
- 组件在服务器渲染时抛错之后的错误页，现在带的是错误页的 head。它的 head 以前还是为那个渲染失败的页面算出来的，所以只属于成功页面的标签留在了 500 页上：`hreflang` 和 canonical 链接、`title`，以及 `lang` 这类 `htmlAttributes`。浏览器 hydrate 时会按 matches 重新算一遍 head，两边可能不一致。现在被错误顶替的路由在没有数据的情况下运行它的 `head`，它下面的路由不再给标签，它上面各层路由的 `head` 会在 `matches` 里带着失败的路由重新运行，和 `Load` 失败的页面一样。见[页面上的错误](docs/app.zh-CN.md#页面上的错误)。
- 示例店面不再为不存在的页面写其他语言的互链。`/store/<语言>/` 下的 404（商品不存在，或没有页面的 URL）以前带着指向这个 URL 的各语言 `hreflang` 链接，等于告诉搜索引擎这个不存在的 URL 是真页面。[多语言网站](docs/app.zh-CN.md#多语言网站)里的写法同样改了：页面显示错误时，语言 Layout 的 `head` 不写这些链接。
- `tifa dev` 在 `go:embed` 嵌入的文件（比如 Markdown 文章）变了时，重新构建并重启应用。以前它只看 Go、TypeScript、CSS 和 JSON 文件，改嵌入的 `.md` 文件什么都不会发生，要等别的文件变了才会生效。嵌入的文件新增或删除时还会跑静态分析，嵌入的前端文件则同时跑打包。
- 只是同一个页面另一种写法的 URL，现在答 308 永久重定向，以前答 307。包括末尾斜杠（`/store/en-US/` 跳到 `/store/en-US`）、双斜杠、点号段和百分号编码的字母（`/%61` 对 `/a`）。搜索引擎把永久重定向当作规范 URL 的信号，临时重定向则不当回事，而且 308 和 307 一样保留请求方法。`c.Redirect` 或 `Load` 要求的重定向仍用它自己的状态码。
- 服务端渲染改用 slowjs v0.29.8，HTML 不变。`JSON.stringify` 内存不足时会抛错，不再留着一个元素不释放，也不再返回缺了 replacer 名单里键的文本。用拼接出来的字符串当 `space` 参数时，缩进写的是它的文字，不再是引擎内部的字节。把数组的 `length` 改小后再往后写，结果和 Node 一样，10 万个元素时快 17%。`Iterator.prototype.reduce` 和 `Iterator.concat` 按规范关闭迭代器。
- 服务端渲染改用 slowjs v0.29.9，HTML 不变。页面在用另一个类的原型创建数组（`Reflect.construct(Array, [], F)`），或给共用形状的对象设置原型（`Object.setPrototypeOf`）时内存不足，现在得到内存不足的错误。前一种原来会写到空指针上，使进程结束，后一种会让新原型一直不释放。
- `tifa dev`：应用和 Vite 不再比它活得久。关掉运行 `tifa dev` 的终端（SIGHUP）现在和 Ctrl-C 一样停掉它。`tifa dev` 被强杀（SIGKILL）时，它启动的应用像收到 SIGTERM 一样关停，以前应用会带着端口一直跑，直到有人手动杀掉。用 `nohup` 启动的 `tifa dev` 仍然忽略 SIGHUP。应用和 Vite 也不再持有 `tifa dev` 继承来的文件，比如脚本用 `flock` 持有的锁。
- `tifa dev`：Vite 开发服务器重启后（改了 `vite.config.ts` 或 `.env` 文件，或切换成轮询），紧接着的一次修改可能永远漏掉，开发服务器已经给出的模块会一直是旧的，直到这个文件再次改动。现在开发服务器等文件监视读完应用的文件才回应请求。
- 公共目录的文件有了 ETag，浏览器重新验证时拿到 304，不再整个重下。这些文件带 `Cache-Control: no-cache`，但嵌进程序的副本没有修改时间，也没有 ETag，每次验证都把文件完整下载一遍。现在 `tifa build` 把每个公共文件的内容哈希写进 `build.json`，服务器把它作为强 ETag 发出，文件压缩后发出时变成弱 ETag（`W/"…"`）。浏览器包的文件不变，`tifa dev` 照旧用它所服务文件的修改时间。
- quickjs v0.26.17：Intl 按 ECMA-402 转换选项、值和语言列表，该给字符串或数字的地方给了 Symbol 或 BigInt 会抛 `TypeError`，`Intl.NumberFormat` 格式化包装起来的 BigInt 时不再经过 double 舍入，而是原样精确输出。页面改了 `Array.prototype`、`JSON` 或别的内置对象，不再影响 Intl。Intl 的函数和内置函数的样子一样，格式化器由进程里所有 VM 共用，所以用过 Intl 的 VM 少占几 KB。
- SSR bundle 保持 React DOM 每一行的位置不变，渲染报错栈里的位置指向正确的行。
- 页面里不再带数据加载的时间。TanStack Router 把渲染时的时间写进每个页面，所以 `tifa dev` 说每个开了渲染缓存的页面都读了时间，渲染缓存还把第一次渲染的时间给了之后的访客，他们回到页面时浏览器会丢掉本来能留着的数据。现在页面里写固定的时间，响应在 `Server-Timing: tifa-age;dur=<毫秒>` 里说明数据可能有多旧，浏览器把发出请求的时间减去这个时长当作数据加载的时间。只有每个缓存都得先问服务器才能重用的响应才带它，所以整页缓存给的页面，以及 `Cache-Control` 允许缓存存下来的页面，都不带。响应不带它或浏览器读不到它时（Firefox 走普通 HTTP，或者从前进后退历史打开的页面），数据按旧数据处理，回到页面时重新加载（见 [SSR 与缓存](docs/app.zh-CN.md#ssr-与缓存)）。

## 2026-10-19

### 新增

- `server.compress`，默认开启：页面、导航数据、JSON 和其他文本对接受压缩的客户端用 zstd 或 gzip 发出，Tifa 程序不需要代理来压缩。前面有代理负责压缩时设成 `false`。
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

- quickjs v0.26.8：Tifa 在 linux 和 freebsd 上又能编译了，v0.26.6 和 v0.26.7 曾让它编译不过。字符串在 emoji 中间被截断时，渲染结果和 Node 一样。
- quickjs v0.26.9：Intl 在所有系统上都按 ICU 的方式命名夏令时。之前在 Linux 上，Europe/Dublin 等少数时区的夏季和冬季名字是反的。
- quickjs v0.26.10：传给 JavaScript 的 Go 字符串里的非法字节变成 U+FFFD，不再把字符串截短。

### 性能

- quickjs v0.26.12：JavaScript 调用不再在 libc 的全局锁下分配内存。linux/amd64 上渲染少用约 11% 的时间，darwin/arm64 上少约 14%，多个 VM 之间也不再互相拖慢。
- quickjs v0.26.13：解释器主循环读写栈的工作更少，给普通对象加属性走快路径。linux/amd64 上渲染少用约 18% 的时间。
- Vite 插件对 SSR bundle 做三项改写：React 把写出的块拼起来再交出，长 `switch` 前先查 `Set`，seroval 对短字符串用 `JSON.stringify` 转义。合起来示例 shop 渲染快 11% 到 14%，纯文字页面快 17.5% 到 22%，HTML 不变。

## 2026-10-17

### 新增

- `ssr.vm_stack_depth` 限制一次渲染里 JavaScript 调用嵌套多深。
- `tifa build` 拒绝 Tifa 没测过的 TanStack Router 版本。`build.skip_tanstack_check` 关掉这项检查。

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
