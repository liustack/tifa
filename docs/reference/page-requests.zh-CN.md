---
summary: '构建出的程序怎样回答页面请求：静态文件、渲染、记忆和 ETag、错误，以及会话怎样发送'
audience: app
order: 12
translatedFrom: 46eb1237b674
read_when:
  - '想知道对页面或导航数据的请求在程序里做了什么，以什么顺序做'
---

# 页面请求 {#page-requests}

静态文件在路由之前处理，不经过 `OnRequest`（哪些文件放在哪里：[静态文件](../start/app.zh-CN.md#static-files)）：`/assets/` 之下的文件带 `Cache-Control: public, max-age=31536000, immutable`，public 文件夹里的文件带 `no-cache` 和 ETag，并且只响应 GET 和 HEAD。public 文件的 ETag 是 `tifa build` 写进 `build.json` 的内容哈希，因为嵌入程序里的副本没有修改时间。带有匹配的 `If-None-Match` 的请求得到 304，HEAD 也一样。ETag 是强的，当文件以压缩形式发出时是弱的（`W/"…"`），见[压缩](server-program.zh-CN.md#compression)。`tifa dev` 不写哈希，它提供的文件保留它们的修改时间。这时不在浏览器包里的 `/assets/` 之下的文件会在 public 文件夹里查找，所以 `public/assets/logo.svg` 仍然是 `/assets/logo.svg`。有页面的应用里，`/assets/` 之下的任何其他路径得到一个纯文本的 404，带 `Cache-Control: no-store`，不经过 `OnRequest`、`Load` 或渲染，比如部署之前打开的页面请求的旧构建的某个代码块。

对页面 URL 的一次 GET，在 `OnRequest` 之后：

| 请求 | 响应 |
| --- | --- |
| 没有 `_tifa` | 并发运行匹配的链上的每个 `Load`。然后由渲染 memo 和 ETag 决定 VM 要不要运行：匹配的 `If-None-Match` 得到 304，memo 命中就发出保存的页面，没命中就渲染一次，同时没命中同一个键的所有请求共用这一次（见 [渲染记忆 memo](memo.zh-CN.md)，VM 渲染的那个 URL 见 [查询参数](memo.zh-CN.md#query-parameters)） |
| 同上，但匹配的链上有 `SSR = false` | 跳过 VM，返回一个最小的文档：资源标签和注入的数据（每个 `Load` 的结果，各带它的 JSON 文本放在 `json` 里，以及访问者的会话放在 `session` 里），放在 `<script type="application/json" id="tifa-data">` 里。浏览器渲染它，不用再请求数据 |
| 带有 `_tifa=<build ID>.<digits>[.p]` | 只运行数字为 `1` 的路由的 `Load` 或 `Layout`（`tifa.Use` 在请求内会去重），并以 200 回答 `{"routes": {route ID: result}, "session": ...}`，每个路由有它自己的状态。当构建 ID 不是当前的构建时回答 409，当值不是这个形式，或数字的个数和匹配的链上 `Load` 的个数不一样时回答 400。错误和重定向在下面说明 |

在 `route.go` 里有 `Cache` 的页面和任何页面一样被回答，只是 GET、HEAD 和带 `_tifa` 的请求是匿名的，并且 HTML 会说明程序前面的共享缓存可以保存什么。`Load` 仍然在每个请求上运行，`X-Tifa-Render` 和 ETag 是渲染 memo 的。规则见 [给 CDN 用的缓存](../pages-and-data/ssr-and-cache.zh-CN.md#cache-for-cdns)，每种响应的头见 [响应的 Cache-Control](../pages-and-data/ssr-and-cache.zh-CN.md#cache-control-of-an-answer)。框架在最终的头发出时，只在一个地方加上默认的 `Cache-Control`。

页面和 JSON 响应在不压缩发出时带 `Content-Length`。

关于错误，`Load` 的错误会先通过 `next` 回到 `OnRequest`，框架只在 `OnRequest` 返回之后才处理它：

- 当某个 `Load` 失败时，`next` 返回已运行的 `Load` 函数中最外层的那个错误，整页请求和数据请求都一样（数据请求只运行列出的路由，所以是其中最外层的）。成功的 `Load` 函数不往响应里写任何东西，所以中间件可以据此拒绝整个请求。
- `OnRequest` 原样返回这个错误（用 `%w` 包装一次也算）：只有这样，框架才会对每个错误分类。意外的错误交给 `OnError`，当几个 `Load` 函数通过 `tifa.Use` 共享同一个错误时，它只会交过去一次。整页请求会用所有的结果和最外层错误的状态码（404，`tifa.Error` 的状态码，或对意外错误是 `OnError` 的结果）来渲染整个页面，路由器在对应的层级显示 `ErrorBoundary`。如果最外层的错误是重定向，响应就是那个重定向。数据请求以 200 回答 `{"routes": {route ID: result}}`，每个条目有它自己的状态。
- `OnRequest` 返回了另一个错误：中间件说了算，这个错误按它自己的方式处理（重定向、`ErrNotFound` 和 `tifa.Error` 被直接处理，其他任何错误交给 `OnError`）。整页请求得到由 Go 写出的简单错误页面。数据请求得到 `{"error": message}` 和状态码，没有每个路由的数据。如果 `OnRequest` 自己写了响应，框架就不再写任何东西。
- 当数据请求遇到针对整个请求的重定向时（`OnRequest` 返回 `tifa.Redirect`，或 URL 写成了非规范的形式），它回答 200 和 `{"redirect", "status"}`，不发送 3xx。这个 JSON 里的 URL，以及 `Load` 要求重定向的路由的 `location`，就是 3xx 会带的那个：像 `../list` 这样的相对 URL 会相对于请求的路径解析，和 `http.Redirect` 做的一样。
- 不匹配的 URL，对有页面的应用的 GET 或 HEAD：Go 找到这个 URL 所匹配的最深的布局（规则见 [404](../pages-and-data/errors.zh-CN.md#404)），并把它的路径的参数设置到请求上。`next(c)` 从根布局一路运行 `Layout` 函数到它，并返回 `tifa.ErrNotFound`。整页请求用它们的结果和状态 404 渲染，或者在某个布局失败时用那个布局的状态，和在页面上一样。导航数据请求运行其中被列出的那些，并以 404 回答它们的结果，当其中一个失败时回答 200，和在页面上一样。列出不在其中的路由会回答 400。其他方法，以及没有页面的应用，得到 JSON 404。
- VM 渲染失败（包括组件在渲染时抛出异常：字符串渲染器把它吞进 Suspense 边界，并留下一个 `<!--$!-->` 标记，SSR 入口看到这个标记时就认为渲染失败了）算作意外错误。它会交给 `OnError` 一次，并且从不存进渲染 memo。然后页面会把错误放进路由树里再渲染一次，见下一点。当没有任何尝试渲染成功时，Go 写出一个简单的错误页面，带状态和公开的消息，不暴露内部的错误。在 `tifa dev`（`DevMain`）下，这个页面还会点出请求，显示完整的错误，并加载开发客户端，一旦重新构建提供了修复，它就会重新加载一次。`OnError` 得到的错误就是代码抛出的那个，即使 React 把它吞掉了也一样，它的堆栈跟踪的前 64 帧由 `dist/ssr.js.map` 给出源文件里的位置。当代码调用了 beastjs 没有的 `require` 时，错误还会点出这个包和位置，并说明该怎么做：从 `.client` 文件导入这个包，在 `useEffect` 里调用它，或使用这个包的一个不需要 Node 的构建。当包在加载时就失败了，也就是在任何页面渲染之前，导入本身就失败，把调用移进 `useEffect` 没有用，所以预热的错误会说要从 `.client` 文件导入这个包，或使用另一个构建。一个会因一直不会完成的工作而挂起的组件，不管是抛出 promise，还是作为加载不了的 `React.lazy` 组件，都会以同样的方式让渲染失败，因为服务器没有定时器，也不做 I/O。它的错误会说要用 `Load` 把数据给页面，并且以静态方式导入组件，或者设置 `SSR = false`。服务器看不到某个懒导入为什么失败。显示它的 `Load` 函数的错误（包括 404）的页面，在它的渲染失败时，也会经过同样的几次尝试。
- 出现失败时，SSR 入口会报告 `levels`：匹配的路由里，从它显示到的最深的那个一直到根路由，有多少个能显示这个错误。服务器会再次渲染，输入里带着 `error: {status, message, level}`，也就是 `OnError` 给出的状态和公开消息。然后入口在路由器脱水之前，把在显示到的最深路由之上 `level` 层的匹配，换成一个持有 `TifaError` 的失败的匹配，这样那个路由的 `errorComponent` 就会显示这个错误，浏览器会水合同样的树。第一次尝试的 level 是 0，每次失败的尝试向上移一层。响应带 `OnError` 的状态、`Cache-Control: no-store`，没有 ETag。在 `tifa dev` 下，开发客户端会把错误显示在页面上。失败的渲染会让它的 VM 退役，所以每次尝试都在一个全新的 VM 上运行。所有的尝试共享第一次渲染开始之后 `ssr.timeout` 的期限，当其中一次用完了时间、发现每个 VM 都忙，或请求被取消时，它们就会停止，第一次渲染也一样。用光了 `ssr.vm_memory` 的渲染不报告 levels，所以服务器会尝试页面有多少个路由就尝试多少次，再加上根路由和一个添加的路由的两次。两者都不是的失败会立刻停止。
- 当路由器把 URL 写成不同的样子时（`/%61` 写成 `/a`），VM 返回一个重定向，服务器用状态 308 回答那个重定向，不带 ETag。当 URL 匹配某个页面时，渲染 memo 会像保存页面那样保存这个重定向，所以对这个 URL 的下一个请求不用渲染。这个重定向会把渲染没有看到的查询参数按请求里的样子放回去。末尾的斜杠（`/a/` 对应 `/a`）也会被重定向，但这样的 URL 不匹配任何页面，会渲染某个布局的 404，所以它的重定向不会被保存，和 404 一样。这个 URL 只是页面的另一种写法，所以重定向是永久的：搜索引擎把永久重定向当作规范的 URL，而不是临时的，并且 308 保留请求方法。ServeMux 对它清理过的路径（`/a//b`、`/a/./b`）或补上斜杠的路径写 307，服务器对这些也回答 308。`Load` 或 `OnRequest` 用 `tifa.Redirect` 要求的重定向保留它给出的状态。渲染只取决于构建 ID、SSR 包、它渲染的 URL 和数据，所以同一个 ETag 永远不会既属于 200 又属于重定向，客户端也永远不会得到会把这个重定向变成 304 的 ETag。
- 非 GET 请求、API 路由和没有页面的应用仍然回答 JSON。

## 回答里的会话 {#sessions-in-answers}

调用了 `tifa.SetSession` 的应用，会把请求的会话以 `{"signedIn": bool, "data"?: ...}` 的形式发给浏览器，其中 `data` 是会话类型的 `Exposed` 方法返回的东西（清单会把类型告诉服务器，生成的 `tifaapp/app.go` 把它注册为 `Session: server.SessionOf[T]`，所以存储里已有会话时启动的程序，在第一个请求之前就知道这个类型。当 `T` 不能写在那里时构建会失败：它必须是已导出的、有名字的结构体类型，不能是泛型。在没有注册类型时带着会话的请求会以错误失败）。只有面向某一个访问者的响应才带它：

- 整页：`ssr.Input` 有 `session`（上面的 JSON）、`sessionKey`（cookie 的名字加 `:v1`，浏览器在这里保存它的副本）和 `sessionHead`（head 开头的脚本和样式，见[会话](../pages-and-data/sessions.zh-CN.md#in-the-browser)）。渲染会在 `<html>` 上写 `data-session="in"` 或 `"out"`，通过路由器的脱水数据把会话传给浏览器（`session` 和 `sessionKey` 放在 `buildId` 旁边），并在服务器上把它交给 `useSession`。`SSR = false` 的页面在它的启动数据里带 `session` 和 `sessionKey`，Go 会自己写出 `<html data-session="in">` 或 `"out"` 和 head 脚本。ETag 和渲染 memo 的键会对会话做哈希，所以两个访问者永远不会共享它们，除非浏览器得到的是同一个会话。有 `Cache` 的页面，它的 HTML 里没有这些东西，因为 HTML 对每个人都相同（见[给 CDN 用的缓存](../pages-and-data/ssr-and-cache.zh-CN.md#cache-for-cdns)）：head 开头的脚本在浏览器里设置 `data-session`，它的导航数据带着那个浏览器的会话。
- 导航数据：`{"routes": ..., "session": ...}`，404 以及某个 `Load` 失败的响应也一样。
- 带 `X-Tifa-Call` 的调用：`Output` 或 `InputOutput` 处理函数回答 `{"result": ..., "session"?: ...}`。`Input` 处理函数回答 204，当这次调用让某人登录或退出时（`SetSession` 或 `ClearSession` 无错误返回）回答带 `{"session": ...}` 的 200。重定向在同样的情况下会把 `session` 加到它的 JSON 里。没有输入、没有结果、什么都没写的处理函数，在这种情况下回答 200、`X-Tifa-Session: 1` 和 `{"session": ...}`，页面的函数会读它。错误响应没有这些。没有这个头的请求，像以前一样只得到结果、204 或重定向。这些响应会加上 `Vary: X-Tifa-Call`，因为两种形态不同。
- `GET /__tifa/session`，HEAD 也一样：请求的会话，`Cache-Control: private, no-store`，没有校验器。它的每个响应，包括错误、405、409 和 `OnRequest` 的提前响应，都是 `Cache-Control: private, no-store`，没有 `ETag` 或 `Last-Modified`。当 `X-Tifa-Build` 头指向另一个构建时，它回答带 `X-Tifa-Build` 的 409，其他方法回答 405，在没有会话的应用里回答 404。它在 `OnRequest` 里运行，模式是 `/__tifa/session`。

浏览器在发出这些请求中的每一个时都给它编号，并取最近发出的那个请求的响应里的会话（`js/runtime/session.tsx`）。`data-session` 是 `<html>` 上保留的：设置它的 `htmlAttributes` 会抛出异常。
