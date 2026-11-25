---
summary: '你返回的错误会变成什么，谁能看到它的文字，404 和 ErrorBoundary'
audience: app
order: 7
translatedFrom: d4754856516b
read_when:
  - '想知道访客能不能看到你返回的错误，以及怎么给他们一句话'
  - '从 `Load` 或处理函数返回错误，比如 `tifa.Fields`、`tifa.Redirect` 或 `tifa.ErrNotFound`'
  - '在页面里显示 404 或错误，或者想知道哪个 `ErrorBoundary` 会显示'
---

# 错误和 404 {#errors-and-404}

## 你返回的错误会变成什么 {#what-an-error-you-return-becomes}

Tifa 为一个请求调用的每个函数都返回 `error`：`Load`、`Layout`、方法处理函数、`Validate` 方法和 `OnRequest`。它们的错误都走同一处。错误只有在它的类型表明可以公开时才会到访客那里。其他任何错误都算意外错误：它的文字只进日志，访客得到 500 和普通的状态文字。所以哪里都可以直接 `return err`。

| 返回的内容 | 状态码 | 访客看到的 | 是否交给 `OnError` |
| --- | --- | --- | --- |
| `tifa.ErrNotFound` | 404 | `Not Found` | 否 |
| `tifa.Redirect(url, status)` | 重定向 | 重定向本身。页面加载和导航都会跟随它，从浏览器调用的处理函数也会（见[在浏览器里调用方法处理函数](handlers.zh-CN.md#calling-method-handlers-from-the-browser)）。`status` 不是 3xx 时会 panic | 否 |
| `tifa.Error{Status, Message}` | 它的 `Status` | 它的 `Message`，`Message` 为空时是状态文字。状态码在 400 到 599 之外时，按意外错误处理 | 否 |
| `tifa.Fields{"carrier": "pick a carrier"}` | 422 | 按 json 名给每个输入字段一条消息 | 否 |
| 其他任何错误，或 panic | 500 | 状态文字，或 `OnError` 返回的内容 | 是 |

数据库或别的服务返回的错误，常常带着地址、用户名或查询语句。只有上面这些类型是公开的，所以这些文字不会意外到访客眼前。

### 给访客一句话 {#giving-the-visitor-a-sentence}

返回 `tifa.Error`，状态码和消息由你来写。消息属于某一个输入框时，返回 `tifa.Fields`。不要把 `err.Error()` 放进 `Message`，那样任何错误的文字都成了公开的。

```go
func POST(c *tifa.Ctx, in ShipInput) (Shipment, error) {
	order, err := findOrder(c.Context(), in.OrderID)
	if err != nil {
		return Shipment{}, err // the visitor sees 500
	}
	if order.Shipped {
		return Shipment{}, tifa.Error{Status: 409, Message: "This order has already shipped."}
	}
	return ship(c.Context(), order)
}
```

- 包一层不改变结果。Tifa 用 `errors.Is` 和 `errors.As` 找这些类型，所以 `fmt.Errorf("ship: %w", tifa.Fields{...})` 仍然是 422。包了一层的 `tifa.Error`，访客只看到它的 `Message`，看不到外面那串文字。
- `tifa.Fields` 是带 `Error` 方法的 map，所以空的 `tifa.Fields{}` 当作 `error` 返回时不是 nil。校验函数用 `return fields.Err()` 收尾（见[方法处理函数](handlers.zh-CN.md#method-handlers-with-input-and-result)）。
- `Validate` 方法返回的普通错误是意外错误，和处理函数返回的一样。只有 `tifa.Error` 和 `tifa.Fields` 会到访客那里。
- 请求体或查询串解码失败时，处理函数不会运行。Tifa 用固定的短句回答（400、413、415 或 408），里面没有 Go 的类型名（见[方法处理函数](handlers.zh-CN.md#method-handlers-with-input-and-result)）。

### OnError {#onerror}

`hooks.go` 里的 `OnError` 对每个意外错误只看到一次，`Load`、处理函数或 `OnRequest` 里的 panic 也一样。它返回访客看到的 `tifa.Error`。上报到错误跟踪服务和写日志都写在这里（见 [Hooks](hooks.zh-CN.md#hooksgo)）。没有 `OnError` 时，Tifa 记日志并以 500 响应。`OnError` 返回的状态码在 400 到 599 之外时，响应是 500，`Message` 为空时用状态文字。

### 浏览器拿到什么 {#what-the-browser-gets}

失败的方法处理函数响应 JSON：`{"error": "message"}`，对于 `tifa.Fields`，还会有 `"fields": {"carrier": "pick a carrier"}`。从页面发起的调用会抛出 `TifaError`，带 `status`、`message` 和 `fields`。失败的 `Load` 或 `Layout` 以同样的 `TifaError` 到达 `ErrorBoundary`（见[页面上的错误](#errors-on-a-page)）。`tifa dev` 会在失败的页面上显示原始错误，构建后的程序从不显示。

## 404 {#404}

404 显示在路由的 `ErrorBoundary` 里（见[页面上的错误](#errors-on-a-page)），它收到的是 `status` 为 404 的 `TifaError`：

```tsx
import { TifaError } from 'tifa/client'

export function ErrorBoundary({ error }: { error: unknown }) {
  if (error instanceof TifaError && error.status === 404) return <p>No such order</p>
  return <p>Something went wrong</p>
}
```

| 情形 | 显示什么 |
| --- | --- |
| 页面的 `Load` 返回 `tifa.ErrNotFound` | 从该页面往上数的第一个 `ErrorBoundary`。外层的布局照常显示 |
| `Layout` 函数返回 `tifa.ErrNotFound` | 从该布局的上一层布局往上数的第一个 `ErrorBoundary` |
| URL 没有匹配任何页面 | 从 URL 匹配到的最深布局往上数的第一个 `ErrorBoundary`，显示在那个布局里面。从最外层到它的各个布局照常运行和显示：从根布局开始，或者从它上面的 `layout_.tsx` 开始 |
| 没有任何路由有 `ErrorBoundary` | 框架默认的 404 页面 |

在所有这些情形里，状态码都是 404，在浏览器里导航之后也一样，那时它的数据请求用各布局的数据和状态 404 来回答。

最后一段带文件扩展名（结尾是一个点加 1 到 8 个字母或数字，比如 `/wp-login.php`）的、没匹配上的 URL，或者有以点开头的段（`/.env`、`/.git/config`、`/.well-known/missing`）的 URL，不会得到 404 页面。它得到的是和 `/assets/` 下缺失文件一样的纯文本 404，没有 `Load`，也不渲染，所以请求这类路径的扫描器几乎不花什么代价。`OnRequest` 仍然运行，并从 `next` 看到 `tifa.ErrNotFound`。路由匹配的 URL 不受影响，包括捕获所有路径的路由和带点的参数（`/users/{name}` 的 `/users/john.doe`），导航数据请求也不受影响。`/.well-known/` 下的路由或 public 文件仍然正常响应。

要找到没有匹配任何页面的 URL 所属的布局，Tifa 把 URL 和每个布局与页面的路径从左到右逐段比较。字面段胜过参数，参数胜过路径的结尾。匹配得最多的路径胜出，页面算作它所在的那个布局。有这些文件夹时：

```text
app/layout.tsx
app/teams/team+/layout.tsx
app/teams/team+/members/page.tsx
app/teams/new/page.tsx
```

| URL | 404 显示在哪个布局里 |
| --- | --- |
| `/nowhere` | 根布局 |
| `/teams/blue/missing` | team 布局，它的 `Layout` 读到的 `c.Param("team")` 是 `blue` |
| `/teams/blue/members/x` | team 布局 |
| `/teams/new/x` | 根布局：`new` 是字面段，所以 `teams/new` 胜过 `teams/team+`，而那个页面在根布局里 |

- `name++` 文件夹里的页面接管它下面的每个 URL，所以这样的 URL 匹配的就是那个页面，由它的 `Load` 决定怎么回答。
- 对于没有匹配任何页面的 URL，`OnRequest` 运行时 `c.Pattern()` 为空。`next(c)` 运行各布局并返回 `tifa.ErrNotFound`。当 `OnRequest` 自己返回 `tifa.ErrNotFound`、没有调用 `next` 时，不会运行任何布局，页面的显示和那个 URL 最外层的 `Layout` 返回 `tifa.ErrNotFound` 时一样：根布局，或者它上面的 `layout_.tsx` 的布局。
- 布局的 `ErrorBoundary` 只会显示在它下面某个路由的位置上，也就是布局的 `Outlet` 里，所以它能读到布局的数据和布局组件提供的上下文。布局失败时，显示的是上层布局的 `ErrorBoundary`，根布局或 `layout_.tsx` 里的布局失败时，显示框架的默认页面。
- 在 `page.tsx` 或 `layout.tsx` 里导出 `NotFound` 会让构建失败。把它显示的内容移到 `ErrorBoundary` 里，放在 `error.status === 404` 下面。导出 `Error` 也会让构建失败：把它改名为 `ErrorBoundary`。

## 页面上的错误 {#errors-on-a-page}

`page.tsx` 和 `layout.tsx` 可以导出 `ErrorBoundary`，当路由的 `Load` 或 `Layout` 失败、当 404 显示在那里（见 [404](#404))，或者当它下面的某个组件抛错时，显示在该路由的位置上：

```tsx
import { TifaError } from 'tifa/client'

export function ErrorBoundary({ error }: { error: unknown }) {
  if (error instanceof TifaError && error.status === 403) return <p>No access</p>
  return <p>Something went wrong</p>
}
```

- 失败的 `Load` 或 `Layout` 以 `TifaError` 的形式到达 `ErrorBoundary`，带 `status`、公开消息和空的 `fields`（`tifa.Error` 的消息，或 `OnError` 返回的内容）。在服务器上和 hydration 之后都一样。
- 没有自己 `ErrorBoundary` 的路由，用最近的、导出了 `ErrorBoundary` 的祖先的那个，再往上是框架的默认值。祖先到 `layout_.tsx` 为止。
- 页面的状态码是最外层失败路由的状态码。
- 页面在服务器上渲染时抛错的组件，是意外错误：`OnError` 看到它一次，并选定状态（没有 `OnError` 时是 500）和公开消息。然后页面带着这个错误重新渲染，错误作为 `TifaError` 放在页面的位置上，所以从页面往上数的第一个 `ErrorBoundary` 把它显示在各布局里面。如果这次渲染也失败了，因为 `ErrorBoundary` 或它外面的某个布局抛了错，错误就上移一个路由，放在页面所在布局的位置上，依此类推，直到最外层的布局，也就是根布局或某个 `layout_.tsx`。只有放在那个布局位置上的错误也失败时，浏览器才会得到 Go 返回的、带状态和公开消息的纯错误页面。浏览器按服务器放置错误的位置来 hydrate 页面，不会再运行抛错的那个路由。浏览器或 CDN 都不保存这样的响应（`Cache-Control: no-store`）。构建出来的应用会改为把它记住 5 秒：这段时间内，对同一页面、同样的数据和会话的请求，得到同样的响应，不再渲染，`X-Tifa-Render` 是 `memo`。同时在等这个页面的请求共用放置错误的那些渲染。`OnError` 仍然会看到每个请求，第一个看到完整的错误，其他的看到它的第一行，以及一条说明没有再次渲染的注释。`tifa dev` 什么也不记，所以修复在下一次重新加载时就能看到。堆栈只在 `tifa dev` 下显示，盖在带 `ErrorBoundary` 的页面上，你保存修复后页面会重新加载（见 [`tifa dev`](../build-and-deploy/dev.zh-CN.md#when-a-page-fails-to-render)）。
- 用光 `ssr.vm_memory` 或超过 `ssr.timeout` 的服务端渲染（见 [config.md](../build-and-deploy/config.zh-CN.md)）会当场停止。你代码里或库里的 `catch`、`finally` 或 promise 处理函数都不会为它运行，所以页面没法捕获它去显示别的东西。VM 会被换成一个全新的。用光 `ssr.vm_memory` 的渲染，会在全新的 VM 上带着这个错误重新渲染一次，放在最外层布局的位置上，因为同样的数据在任何一层都会用光内存。它的响应像上面那样被记住。超过 `ssr.timeout` 或发现每个 VM 都忙的渲染则不会：Go 立刻返回它的纯错误页面。一个请求的所有渲染共用一个 `ssr.timeout`，从第一次算起，带着错误的渲染如果超出它，尝试就到此为止。无限递归的代码会得到 `RangeError: Maximum call stack size exceeded`，和在 Node 里一样，`catch` 可以接住它（见[栈深度](../build-and-deploy/config.zh-CN.md#stack-depth)）。没有人接住时，它和其他任何错误一样让渲染失败。
- 浏览器接管之后，抛错的组件会显示最近的 `ErrorBoundary`，和失败的 `Load` 一样。
- 不管什么失败，文档外壳都保持完整，所以页面仍然会加载它的脚本和样式。
- 显示错误的页面拿到的是 `Load` 失败的页面的 head，见[404 页面和错误页面的 head](head.zh-CN.md#the-head-of-a-404-or-an-error-page)。
