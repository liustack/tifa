# 更新记录

记录 tifa 里应用和应用的用户看得到的变化。tifa 还没有发过版本，所以按进入 `main` 的日期分组。格式参照 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)。每项提速怎么测的见 [性能](docs/performance.zh-CN.md)。

## [Unreleased]

### 变更

- 服务端渲染改用 slowjs v0.29.4，HTML 不变。调用 C 写的内置函数少走解释器的一步，字面量对象的属性放进对象自己的块里：一次渲染少 0.1% 到 2.3% 的指令，渲染后每个 VM 少占 1% 到 2.4% 的内存。递归能到的深度和以前完全一样。
- 服务端渲染改用 slowjs v0.29.3，HTML 不变。页面卡在一次很长的调用里时，渲染超时（`ssr.timeout`）现在几次轮询之内就能让它停下，包括：遍历字符串、数组、Map 或 Set 而每项不调用别的函数（`Object.fromEntries`、展开 Set、`Iterator.prototype.toArray`、`Set` 的各个方法），用字符串做模式的 `replace` 或 `replaceAll`，切出一百万字符以上的 `slice`、`substring`、`trim` 或 `split`，以及键有几百万字符的 Map 或 Set 操作。以前这样的调用要跑到结束，或跑到 VM 被强制终止。
- 服务端渲染改用 slowjs v0.29.2，HTML 不变。页面卡在一次很长的 `split`、`padStart`、`padEnd`、`trim`、`isWellFormed`、`toWellFormed`、`encodeURI`、`decodeURI` 一类调用、`Array.from` 或类型化数组构造里，或在拼接、比较用 `+` 拼出的长字符串时，渲染超时（`ssr.timeout`）现在几千个字符或元素之内就能让它停下，以前要跑到这次调用结束。对象展开（`{...o}`）、解构的剩余部分和 `Object.assign` 和 Node 一样，轮到每个键时再查一次：前面的 getter 删掉后面的键或把它改成不可枚举，这个键不再被复制，把后面的键改成可枚举，这个键会被复制。
- 服务端渲染改用 slowjs v0.29 的 C 引擎，它是唯一的引擎：用 C 写的 QuickJS，编译成随模块发布的 Go 汇编。构建不需要 C 编译器，`CGO_ENABLED=0` 照旧。页面渲染出的 HTML 一样，用的时间更少（见下面的性能）。应用能看到的变化：
  - 应用只能为 linux/amd64 和 darwin/arm64 构建。为别的目标构建，比如 windows、darwin/amd64 或 linux/arm64，会停在 slowjs 报的错上，错误里写明这两个平台。
  - 一次渲染里 `JSON.parse` 最多嵌套 2000 层，数据嵌套更深的页面原来能渲染，现在会失败，报 `RangeError: JSON nesting too deep`。
  - `ssr.vm_stack_depth` 默认仍是 10000，最大也仍是 10000。它现在数的是调用次数，原来的引擎数的是自己栈上的槽位。渲染里普通递归能到的深度仍和 Node 一样。无限递归的渲染让进程在 linux/amd64 上最多涨 26 MB，在 darwin/arm64 上涨 14 MB，原来最多涨 24 MB 的 goroutine 栈（见[栈深度](docs/config.zh-CN.md#栈深度)）。
  - `ssr.vm_memory` 限制的是引擎自己的分配器持有的内存。同一份堆现在数出来少 13% 到 15%，所以同样的限制能多装这么多。
  - `modernc.org/libc` 不再是 tifa 的依赖。`tifa new` 固定的 SQLite 版本不必再和某个 libc 版本对齐。
  - 项目文件里的 `build.goasm` 没有了，因为已经没有可以切换过去的 Go 解释器。项目文件里再写它，会因为未知的键而失败。
- `tifa build` 不再用 CPU 剖析（PGO）编译应用。`build.pgo` 配置项和应用里的 `default.pgo` 文件都没有了，项目文件里再写 `build.pgo` 会因为未知的键而失败。引擎是 PGO 改不了的 Go 汇编，剖析也没有可量出的差别：在 linux/amd64 上，商店、MUI 应用和 bench 应用的页面用它和不用它的耗时比是 0.998、0.999 和 1.004，一次渲染执行的指令数是不用的 0.9987 到 1.0003，在 darwin/arm64 上商店的页面是 1.002（见[性能](docs/performance.zh-CN.md#构建)）。用 C 引擎之前的引擎时，它让页面在 linux/amd64 上快约 3%，在 darwin/arm64 上快约 7%。升级 slowjs 时不用再重采剖析。
- 空闲的 VM 会把空闲内存还给系统。一个 VM 一秒没有渲染，就把渲染留下的空闲页和碰过的栈还回去，两次渲染之间的完整回收原来什么也没还，现在也会还。程序里示例商店的一个 VM 空闲时占 9.6 MiB，原来是 12.8 MiB，MUI 测试应用的一个 VM 占 5.6 MiB，原来是 7.7 MiB，约少四分之一（见[内存](docs/performance.zh-CN.md#内存)）。至少每秒渲染一次的 VM 留着自己的内存，所以按负载估算容量不变。等待之后的第一次渲染多花 1 到 2.5 ms，渲染时间不变。
- 示例商店换了外观，页面也重到和真实网店相当。外观照着印刷的商品目录来做，英文标题用 Newsreader，随应用打包，中文标题和正文用系统字体。每件商品有三张 AVIF 图，各有三种宽度。店面加了分类菜单、购物车抽屉、轮播、商品图库、分页的带图评价、问答和搭配购买。商品列表有带数量的筛选、常用筛选、已选条件、网格和列表视图和快速查看，商品页有完整规格和同类商品对比。工作台的订单列表每页 25 行，带商品图，可以按日期、快递和金额筛选，有行操作菜单和批量操作。订单详情有操作记录、内部备注、付款、库存检查、拣货单和物流轨迹，销售统计有每件商品的明细表、下单时段热力图、快递表现和常客。页面内容多了，渲染也就更久：新数字见[性能](docs/performance.zh-CN.md#改版后的商店)，这不代表 tifa 变慢了。打包进去的字体和图片让 darwin/arm64 上的 `bin/shop` 从 31.1 MB 变成 35.9 MB。
- 带大块 `Load` 数据的页面渲染更快。每个 `Load` 的 JSON 文本现在作为交给渲染的一个单独参数，Go 不用再把它转义进输入的 JSON，VM 也不用解析两遍。一个 1,000 行、每行两个 UUID 的表格，渲染时间是原来的 0.986（0.965 到 0.991，6 轮全部更快），服务端准备渲染输入的时间是原来的 0.665（0.655 到 0.682）。示例商店的工作台和店面页面数据只有几 KB，变化在各轮的波动之内（一组页面在原来的 0.987 到 1.008 之间）。HTML 不变，浏览器包也不变。
- 装了 rolldown 1.2.15 或 seroval 1.7 的应用，也能拿到 tifa 插件的提速。rolldown 1.2.15 在每个模块开头写的是 `// 路径` 注释，不再是 `//#region 路径`，seroval 1.7 把要问的插件传给 `parsePlugin`，所以原来插件会报警告，React 的命名空间留着 getter，路由数据里的数组按 seroval 自己的写法写出，渲染更慢。新建的应用装的就是这两个版本。页面 HTML 不变。
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

### 新增

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
- 路由的 `head` 可以返回 `htmlAttributes`，即这些页面 `<html>` 的属性，比如多语言网站的 `lang`。它们逐个属性合并到根 Layout 的 `htmlAttributes` 上，最深的路由优先，`null` 去掉属性。浏览器每次跳转后同步这些属性，不走 SSR 的页面也一样。tifa 只在它要的值变了时才写某个属性，也只去掉自己写过的属性，所以页面脚本设的 class 或 `lang` 一直保留，直到某个路由要另一个值。不用这个功能的应用 HTML 不变，在浏览器里 tifa 也和以前一样，首次渲染之后不再碰 `<html>`，页面脚本在上面改什么都保留。示例店面这样设置自己的语言，店面没有的路径用 URL 里的语言回答 404。

### 性能

- React 的 `createElement` 和 `cloneElement` 把 config 拷进元素 props 的活改在引擎里跑。slowjs 里有做这件事的循环，是一个 C 函数（`RegisterCopyProps`），池里每个 VM 都以 `__tifa_copy_props` 的名字拥有它，构建让这两个函数先问它，再走自己的循环。它只拷循环不可能做出别的结果的 config，其余的（Proxy、有 getter 的、类的实例、数组、超过 32 个键的）仍走 React 的循环。渲染一次，示例应用 shop 的工作台页面少跑 2.9% 的指令，店面页面少 2.1%，MUI 测试应用少 4.1%，HTML 不变。在 linux/amd64 上按 8 个代码位置计时，页面渲染花原来的 0.988、0.989 和 0.980，在各位置的差距之内，所以结论看指令数。没有这个函数的 VM，以及插件不认识的 React，仍走 React 的循环（[build.zh-CN.md](docs/build.zh-CN.md#前端产物)）。它需要带 `RegisterCopyProps` 的 slowjs 版本。
- 用 react-i18next 翻译的页面渲染更快：每次调用 `useTranslation` 都要做的 i18n 对象副本，在对象只有普通属性时按记下的键列表来做，别的对象仍照原样用描述符做。shop 的店面页面渲染时间是原来的 0.967，执行的指令少 6.0%，HTML 不变。react-i18next 某个版本改了 `createI18nWrapper` 时，构建打印警告，那个函数保持原样（[build.zh-CN.md](docs/build.zh-CN.md#前端产物)）。
- 用了 shadcn/ui 的 `asChild` 组件的页面渲染略快：`@radix-ui/react-slot` 的 `Slot` 合并 props 时改用循环，按名字的字符码区分事件处理函数，原来每个 prop 都要测一次正则。shop 的工作台页面执行的指令少 0.58%，店面页面少 0.24%，HTML 不变。react-slot 某个版本改了 `mergeProps` 时，构建打印警告，那个函数保持原样（[build.zh-CN.md](docs/build.zh-CN.md#前端产物)）。
- React DOM 对每段文本和每个属性值的转义改在引擎里跑。slowjs 里有用 C 写的 `escapeTextForBrowser`（`RegisterEscapeText`），池里每个 VM 都以 `__tifa_escape` 的名字拥有它，构建让 React 的字符串渲染器调用它，不再走 JavaScript 写的函数加缓存和 `replaceAll`。渲染一次，示例应用 shop 的工作台页面少跑 2.5% 的指令，店面页面少 2.4%，MUI 测试应用少 0.7%，bench 应用少 4.5%，HTML 不变。在 linux/amd64 上按 8 个代码位置计时，工作台页面渲染只花原来的 0.974，店面页面 0.975，bench 页面 0.938。没有这个函数的 VM，以及插件不认识的 React DOM，仍用缓存和 `replaceAll`（[build.zh-CN.md](docs/build.zh-CN.md#前端产物)）。它需要带 `RegisterEscapeText` 的 slowjs 版本。
- React DOM 的 `pushAttribute` 写 `class`、`tabindex`、`dir`、`role`、`viewBox`、`width`、`height` 以及 XLink 和 XML 的属性时不再调用 `pushStringAttribute`，别的属性名先查有效名字的缓存，查不到才调 `isAttributeNameSafe`。渲染一次，示例应用 shop 的工作台页面少跑 0.8% 的指令，店面页面少 0.7%，bench 应用少 1.0%，MUI 测试应用少 0.1%，HTML 不变。在 linux/amd64 上按 8 个代码位置计时，页面耗时是原来的 0.993 到 0.997，在各位置的波动之内。这几个函数中有一个和 React DOM 19.3 不同时，构建打印警告，页面和以前一样快（[build.zh-CN.md](docs/build.zh-CN.md#前端产物)）。
- 服务端渲染改用 slowjs v0.29.1，HTML 一样。slowjs 在 linux/amd64 上不开剖析、按 8 个代码位置，逐项和前一步比。只有普通键的对象字面量一次按最终形状建好：示例商店的工作台页面耗时是原来的 0.956，MUI 页面 0.969，bench 应用的表格页 0.942。在此之上，函数调用设置和释放值的步骤更少，React 里配合 `hasOwnProperty.call` 的 `for in` 循环不再真的调用这两个函数：工作台页面耗时是 0.982，MUI 页面 0.976，bench 页面 0.952。普通对象（比如 React 元素的 props）从 72 字节缩到 48 字节，长 Markdown 页面渲染时的内存峰值低 3.8%。
- slowjs 的 C 引擎渲染示例商店的工作台页面、MUI 测试应用和 bench 应用的页面，耗时是 v0.29.0 之前的 slowjs 加 tifa 当时自带剖析的 0.834 到 0.860，HTML 一样，而且不开剖析（11 个页面，一个 VM 一个核，8 个代码位置全部更快，见[引擎](docs/performance.zh-CN.md#引擎)）。渲染时 Go 垃圾回收让程序停的时间一样短：1 个 VM 时 P99 是 131 到 164 µs，3 个 VM 时是 197 µs，最长一次停 3.1 ms 和 0.9 ms，原来是 4.2 和 2.1 ms。示例商店的一个 VM 加载后占进程 8.5 到 8.6 MiB，渲染后 12.8 MiB，空闲后 9.6 MiB，MUI 测试应用的一个 VM 是 3.9、7.4 到 7.6 和 5.3 到 5.5 MiB（见[内存](docs/performance.zh-CN.md#内存)）。

- lucide-react 的图标渲染更快：每个 VM 记住 lucide-react 给 props 相同的图标构建出的结果，再次用到时直接交出去。shop 的工作台页面渲染时间少 4.3%，店面页面少 5.8%，HTML 不变。lucide-react 某个版本改了缓存依赖的文件时，构建打印警告，图标照旧渲染（[build.zh-CN.md](docs/build.zh-CN.md#前端产物)）。
- 用 i18next 翻译的页面渲染更快：`getFixedT` 和 `useTranslation` 的 `t` 只在键或键前缀写成函数时才复制 i18next 的选项，i18next 也记住分辨带命名空间的键和一句话用的正则。shop 的店面页面渲染时间少 5.2%，HTML 不变。i18next 某个版本改了这两个函数之一时，构建打印警告，那个函数保持原样（[build.zh-CN.md](docs/build.zh-CN.md#前端产物)）。
- SSR 池里的 VM 借助 slowjs 的 `SharedBytecode` 共用一份 bundle 函数源码和给错误栈提供行号的表，所以示例商店的每个 VM 加载后少占 2.35 MiB（9.0 MB，原来 11.5 MB），MUI 测试应用的每个 VM 少占 1.22 MiB。`toString`、错误栈和 HTML 都不变。这份共用的副本不算进 `ssr.vm_memory`。渲染时间不变。
- SSR 包执行的字节码指令更少：压缩之后，`tifa build` 和 `tifa dev` 把 SSR 包里的语句 `a && b;`、`a || b;` 和 `a ? b : c;` 写成 `if` 语句，`!0` 和 `!1` 写成 `true` 和 `false`，浏览器包不动。MUI 测试应用的页面渲染时间为原来的 0.989（6 轮全部更快），商店工作台页面为 0.994，在各轮的离散度以内，HTML 和错误栈里的位置都不变。
- quickjs v0.27.2 渲染商店工作台页面和 MUI 测试应用页面的耗时和 v0.27.1 一样，开不开 PGO 都在 1% 以内（不开 PGO 时为 1.006 和 0.991，开 PGO 时为 0.993 和 0.996），HTML 不变。GC 之后，VM 从分配器占的内存最多少 0.6 MiB。
- quickjs v0.27.1 渲染商店工作台页面，不开 PGO 时耗时为 v0.27.0 的 0.991，开 PGO 时 0.982，MUI 测试应用的页面是 0.986 和 0.980，HTML 和内存不变。加上 emotion 的哈希改在 Go 里算，MUI 页面的耗时是这项改动之前（还在 v0.27.0 上时）的 0.70，开不开 PGO 都一样，商店不开 PGO 时是 0.987，开 PGO 时是 0.984。
- quickjs v0.27.0 渲染商店工作台页面，不开 PGO 时耗时为 v0.26.17 的 0.982，开 PGO 时一样，因为它的改动带来的那部分 PGO 原本就拿到了。每次渲染都新建同样正则的页面（比如高亮代码的页面）每个 VM 只编译一次：商店里高亮 CSS 的那篇文章不开 PGO 时渲染耗时为原来的 0.893。
- 每个页面都要加载的代码放进入口脚本这一个文件，不再是四个。走 HTTP/1.1、往返 150 ms 时，shop 的页面完成 hydration 比以前早 11 到 192 ms，往返 40 ms 时早 4 到 77 ms，首次绘制晚 8 到 24 ms（走 HTTP/2 时晚 8 到 48 ms）。由 tifa 自己走 HTTP/1.1 提供服务时，每页要 17 到 20 个文件的应用在往返 150 ms 时完成 hydration 晚 58 到 100 ms，前面放一个讲 HTTP/2 的代理或 CDN 就能避免。
- Vite 插件对 SSR bundle 再做四项改写：React 转义 HTML 改用 `replaceAll`，不再构建组件栈，命名空间拿到的是普通值而不是 getter，key path 里不再放组件名。合起来示例 shop 渲染快 6.5% 到 7.5%，纯文字页面快 17.6% 到 18.3%，HTML 不变。
- 返回数组（比如切片）的 `Load`，数据现在也以 Go 写的 JSON 文本进页面，和对象一样。1,000 行的表格渲染用时少 39%。seroval 的代码换了写法时，构建打印警告，页面照旧渲染。
- Go 交给渲染的数据里，U+FFFF 以上的字符（比如 emoji）写成代理对的转义，shop 订单列表的数据里有一个 emoji 时不再慢 9%。浏览器读到的值不变。
- quickjs v0.26.16：不开 PGO 时，其中两项改动让渲染耗时变成 v0.26.15 的 0.971，另外三项再变成这个的 0.955，HTML 不变。每个 VM 留住最近对象的形状和释放的小块再用，所以垃圾回收后多占约 0.7 MiB，其中 647 KiB 是形状。页面建很多键和别的对象都不一样的对象时，形状最多能占约 6 MiB。形状算在 `ssr.vm_memory` 里。
- `Cache = true` 的页面同时有多个请求没命中缓存、数据也相同时，它们共用一次渲染，所以冷页面上的突增（比如刚部署完）只渲染一次。每个请求保留自己的响应头，拿到自己接受的编码，页面只为它们压缩一次，渲染失败时等它的每个请求都拿到这个错误，等别人渲染的请求带 `X-Tifa-Render: shared`。等待中客户端断开的请求，连接被中断，不发响应，也不调 `OnError`。没有请求再等时才失败的渲染写进日志。对 shop 一个冷的订单页同时发 50 个请求，以前渲染 50 次、146 到 221 ms 答完，现在渲染一次、30 到 52 ms 答完。
- 池子每次渲染后跑一次年轻垃圾回收，只看这次渲染新建的对象，只在 VM 比上次完整回收后长到 1.5 倍、引擎在渲染中途回收过、或满 256 次渲染时才做完整回收。一个 VM 时，示例 shop 渲染的 P99 是原来的 0.748，MUI 的页面是 0.940，两个 VM 时是 0.796 和 0.919。P50 和 CPU 的变化都在 2.1% 以内，HTML 不变，内存峰值是原来的 0.82 到 0.88。
- 渲染留下的垃圾由池子在两次渲染之间、请求答完以后回收，不再由引擎在渲染中途回收。一个 VM 时，示例 shop 渲染的 P99 低 12% 到 41%，MUI 的页面低 4% 到 7%，P50、吞吐和 HTML 都不变，渲染后一个 VM 少占 3% 到 14% 的内存。所有 VM 都忙、请求在排队时，下一个请求仍要等这次回收。

### 修复

- `tifa dev`：应用和 Vite 不再比它活得久。关掉运行 `tifa dev` 的终端（SIGHUP）现在和 Ctrl-C 一样停掉它。`tifa dev` 被强杀（SIGKILL）时，它启动的应用像收到 SIGTERM 一样关停，以前应用会带着端口一直跑，直到有人手动杀掉。用 `nohup` 启动的 `tifa dev` 仍然忽略 SIGHUP。应用和 Vite 也不再持有 `tifa dev` 继承来的文件，比如脚本用 `flock` 持有的锁。
- `tifa dev`：Vite 开发服务器重启后（改了 `vite.config.ts` 或 `.env` 文件，或切换成轮询），紧接着的一次修改可能永远漏掉，开发服务器已经给出的模块会一直是旧的，直到这个文件再次改动。现在开发服务器等文件监视读完应用的文件才回应请求。
- 公共目录的文件有了 ETag，浏览器重新验证时拿到 304，不再整个重下。这些文件带 `Cache-Control: no-cache`，但嵌进程序的副本没有修改时间，也没有 ETag，每次验证都把文件完整下载一遍。现在 `tifa build` 把每个公共文件的内容哈希写进 `build.json`，服务器把它作为强 ETag 发出，文件压缩后发出时变成弱 ETag（`W/"…"`）。浏览器包的文件不变，`tifa dev` 照旧用它所服务文件的修改时间。
- quickjs v0.26.17：Intl 按 ECMA-402 转换选项、值和语言列表，该给字符串或数字的地方给了 Symbol 或 BigInt 会抛 `TypeError`，`Intl.NumberFormat` 格式化包装起来的 BigInt 时不再经过 double 舍入，而是原样精确输出。页面改了 `Array.prototype`、`JSON` 或别的内置对象，不再影响 Intl。Intl 的函数和内置函数的样子一样，格式化器由进程里所有 VM 共用，所以用过 Intl 的 VM 少占几 KB。
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
