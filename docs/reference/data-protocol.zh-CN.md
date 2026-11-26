---
summary: 'Go 服务器、渲染虚拟机和浏览器之间的数据协议：Load 结果、导航请求、构建 ID 和脱水'
audience: app
order: 11
translatedFrom: 773cfdbb5260
read_when:
  - '实现导航数据请求，需要数据协议'
---

# 数据协议 {#data-protocol}

定义在 `js/runtime/data.ts` 里。Go 一侧实现它：

- 每个 `Load` 的结果是 `{status, data?, error?}`：成功时是 200 并带 `data`，`tifa.ErrNotFound` 是 404，其他错误是状态码加上公开的消息。
- 一个路由 ID 指的是一个 `Load` 或 `Layout` 函数：页面或 API 路由（文件夹里的 `page.tsx` 和 `route.go`）是它的文件夹加 `/route`，布局是加 `/layout`，比如 `app/layout` 和 `app/projects/project+/route`。
- 整页渲染的 VM 输入是 `{url, buildId, data: {route ID: result}, preload}`，和 `internal/ssr.Input` 对应。结果里有 `status`、`error` 和 `location`，对成功的 `Load` 还有它的 JSON 文本的编号 `text`。文本不在输入里。它们是跟在后面的 `startRender(json, ...texts)` 的参数，按路由 ID 的顺序排列，所以 Go 不用把它们转义进输入的 JSON，VM 也不用从里面把它们解析出来。
- 在客户端导航时，同一个事件循环回合里对同一个 URL 的 loader 调用，会合并成对这个页面 URL 的一次 GET，带保留的查询参数 `_tifa=<build ID>.<digits>`，比如 `/projects/studio?_tifa=1f3a9c.011`。这些数字对 URL 的链上每个 `Load` 或 `Layout` 各占一位，从根布局一路到页面（没有函数的布局或页面不占位），`1` 表示要那一个的数据。当数字的个数不是这个 URL 的链上 `Load` 的个数时，服务器回答 400。请求没有 Tifa 的请求头，并且 `fetch` 不会自己跟随重定向（`redirect: "manual"`）。当路由器预加载一个页面时（指针下的链接，`defaultPreload: 'intent'`），值以 `.p` 结尾，如 `_tifa=1f3a9c.011.p`。服务器像回答其他请求一样回答它，只有 URL 能把它区分出来（日志里可以看出哪些请求是预加载）。这个标记不会让请求算作一次页面浏览：预加载的结果之后可以在不再发请求的情况下用于导航，而失效之后的重新加载会再发一个请求。要精确统计页面浏览，请统计浏览器完成的客户端导航（路由器的一个事件），以及整页请求。框架在你的任何代码运行之前，就会把 `_tifa` 从请求里拿掉：`c.Query`、`c.QueryValues`、`c.Request().URL`、页面渲染所用的 URL、ETag 和渲染 memo 的键里都不会有它，所以 `OnRequest` 看到的页面路径和它的数据请求的路径相同。读取名为 `_tifa` 的查询参数的 `Load` 会让构建失败。用 `tifa.SetHTTPHandler` 设置的中间件看到的是原样的请求，带着 `_tifa`。有三种回答：
  - `{"routes": {route ID: result}, "session": ...}`：不管 HTTP 状态码是什么，浏览器都会分发 `routes` 里的每个条目。应用有会话时就有 `session`，见[回答里的会话](page-requests.zh-CN.md#sessions-in-answers)。
  - `{"redirect": "<URL>", "status": <3xx>}`（应用有会话时，带着这个请求的 `session`）：当整个请求必须重定向时，比如 `OnRequest` 返回了 `tifa.Redirect`，或 URL 写成了非规范的形式（ServeMux 的重定向，状态是 308）。HTTP 状态码是 200。请求里的每个路由都得到这个重定向，loader 抛出 TanStack 的 `redirect()`。
  - 其他情况：`OnRequest` 拒绝了整个请求。回答是 `{"error": message}`，带着这个错误的状态码，这一批里所有的 loader 都以那个状态失败。

  当构建 ID 不同时，服务器回答 409，并在响应头 `X-Tifa-Build` 里带上它自己的构建 ID，浏览器会为那个构建把当前页面重新加载一次（几个 409 只重新加载一次，预加载迟到的 409 不会跳到预加载的目标）。已经为那个构建重新加载过、又收到 409 的页面不会再重新加载：这一批里的每个 loader 都以 409 失败。浏览器把这一点记在 `sessionStorage` 里，键是 `tifa-reload:<build ID>`，见[部署时已打开的页面](../build-and-deploy/deploy-by-hand.zh-CN.md#pages-open-while-you-deploy)。如果浏览器仍然收到了 3xx（比如来自代理），它会把那个 URL 当作整页加载。
- `route.gen` 模块的处理函数（见 [handlers.md](../pages-and-data/handlers.zh-CN.md#calling-method-handlers-from-the-browser)）会发送请求头 `X-Tifa-Call: 1`，并在 `X-Tifa-Build` 里带上它的页面的构建 ID，并且不让 `fetch` 跟随重定向。在 `OnRequest` 之后、处理函数之前，当两者不同时，带页面的服务器会回答 409，并在响应头 `X-Tifa-Build` 里带上它自己的构建 ID，浏览器会把页面重新加载一次，和数据请求一样。这个函数会抛出 `TifaError`，并且不会再次发送这个调用。应用的处理函数也可以返回 409，而那个回答没有 `X-Tifa-Build` 头，所以函数不会因它而重新加载任何东西。没有 `X-Tifa-Build` 的请求不会被检查。当处理函数或 `OnRequest` 返回 `tifa.Redirect` 时，这样的请求会得到 200、`Cache-Control: no-store`、`X-Tifa-Redirect: <3xx>`，以及和数据请求一样的 `{"redirect": "<URL>", "status": <3xx>}` 的回答。函数只在有那个头时才把它读作重定向，因为处理函数的结果可能有同样的形状。对成功的调用，其他每种回答都是 `{"result"?: ..., "session"?: ...}`（见[回答里的会话](page-requests.zh-CN.md#sessions-in-answers)），自己写响应的处理函数则原样传下去。来自 curl 或其他程序、没有 `X-Tifa-Call` 的请求，仍然得到 3xx。两种回答都带 `Vary: X-Tifa-Call`。
- 整页把每个 `Load` 的数据放在路由器的脱水脚本里带给浏览器。Go 把每个成功的 `Load` 的数据，以它写出的 JSON 文本的形式交给渲染，作为它自己的 `startRender` 的一个参数（输入里它的结果的 `text` 给它编号，`js/runtime/server.tsx` 里的 `RenderResult`），渲染再解析那段文本。在这段文本里，Go 把 U+FFFF 以上的每个字符写成它的代理对的转义，比如 😀 写成 `😀`，它们代表同一个字符：把文本写进页面的 seroval 只对不含代理项的字符串使用 `JSON.stringify`，对其他任何字符串都一个码元一个码元地处理，这在 beastjs 里要慢得多。没有这些转义时，`examples/shop` 的订单列表数据里只要有一个 emoji，它的渲染就会慢 9%，在 1,000 行的表格里有一个就会慢 2.3 倍。对象或数组以同样的文本进入页面，作为一个 JSON 字符串，通过 TanStack 的序列化适配器（键 `tifa.data`），浏览器用 `JSON.parse` 把它读回来。VM 自己不把这个字符串写进页面。从 4,096 个码元起，它在原位置写一个槽，由一个 `~`、这次渲染的 nonce、一个点、文本的编号和一个点组成，`Pool.Render` 在 VM 重新空闲之后填入每个槽，文本按 seroval 在脚本里写字符串的方式写出：引号和反斜杠被转义，换行、制表符、退格和换页符写成字母，`<` 写成 `\x3C`，这样没有 `</script` 或 `<!--` 能结束或隐藏这个元素，U+2028 和 U+2029 写成转义，每个不是 UTF-8 的字节写成 U+FFFD。页面离开 VM 只有 `Pool.Render` 这一条路，所以每一条发送、缓存或压缩页面的路径得到的都是已经填好的页面。nonce 是 `Pool.Render` 在 Load 运行之后为每次渲染抽取的 128 个随机比特，所以没有任何数据能拼出一个槽，在不是完整的槽的任何地方含有 `~` 和 nonce 的页面不会被提供（`ssr.ErrSlot`）。槽只在 seroval 写它的地方才会被填：作为适配器调用 `$_TSR.t.get("tifa.data")("<slot>")` 的整个字符串参数，位于一个含有 JavaScript、并且还没有结束的 `<script>` 元素里面。JavaScript 字符串的转义在其他任何地方都是不对的，所以属性里的真正的槽，或组件把它复制进去的页面文本里的槽，或脚本的其他位置的槽，同样是 `ssr.ErrSlot`。这就是信任边界：页面的代码仍然可以写任何 HTML，就像它一直能做的那样，但它没办法让数据的文本被写进它选择的上下文里。这个检查查看每个槽之前的几 KB，在 221 KB 的页面上用时不到 1 µs。没有带 nonce 调用的包会像以前一样自己写文本。在 VM 里写这个字符串，每个字节的数据要用 100 到 180 条指令（扫描要转义的字符，带转义的复制，以及页面回到 Go 时又多一次复制）。Go 填一个槽要把页面再复制一次，130 KB 的页面约 60 µs，这就是为什么不到 4,096 个码元的文本留在 VM 里。路由器对超过 16K 个码元的文本会自己开一个脚本标签，所以带这样文本的页面比以前少两个脚本标签，因为短槽不再需要它们：页面小了 80 个字节，浏览器按同样的顺序运行同样的语句。
 所以浏览器读到的恰好是服务器渲染时的值，包括 `-0` 和 `__proto__` 键，用它们格式化出的文本，比如 Intl 给出的 `-¥0.00`，水合出来的和渲染时一样。对同样的数据，TanStack 的默认序列化器在 VM 里要花好几倍的时间。代价是页面稍微大一点，因为 JSON 字符串要转义它的引号：在 `examples/shop` 的页面上，压缩之前约大 3%。字符串、数字和其他单个的值走默认的序列化器。路由器的构造函数类型把适配器留给了 TanStack Start，在它自己的路由器上设置它们没有文档（TanStack/router#7796），所以 `TestGeneratedRouterBehaves` 检查页面使用 `tifa.data` 并且数据能经受住水合，而 `tifa build` 检查应用安装的是这些测试运行时用的那个 TanStack Router（见[应用的 tsconfig 和 Vite 配置](generated-files.zh-CN.md#the-apps-tsconfig-and-vite-config)）。
- 浏览器从 TanStack 的脱水数据里得到构建 ID。
- loader 为失败的 `Load` 抛出的 `TifaError`，通过 TanStack 的序列化适配器（键 `tifa.TifaError`）写进页面。水合之后它仍然是带 `status` 的 `TifaError`，所以 `ErrorBoundary` 组件在服务器和浏览器里看到的是同样的错误。
