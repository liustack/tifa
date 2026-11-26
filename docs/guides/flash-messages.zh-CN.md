---
summary: '在 Tifa 应用里重定向之后只显示一次的消息（「已保存」）：处理函数设一个短 cookie，下一个页面读出并清掉它，以及带 Cache 的页面怎么办'
audience: app
order: 16
translatedFrom: c9b57cf1ef83
read_when:
  - '写入后重定向，要只显示一次「已保存」「已删除」或「已退出」'
  - '消息显示了两次，在带 `Cache` 的页面上不显示，或者客户端导航之后不显示'
  - '在 cookie、查询参数和 router state 之间选一个来带重定向之后的消息'
---

# 重定向之后的一条消息 {#a-message-after-a-redirect}

一次写入以重定向结束，下一个页面应该说一次「Item saved」。消息不是地址的一部分，所以得有东西把它从处理函数带到下一个请求。常见的办法，也是这篇讲的办法，是一个短 cookie：处理函数设上它，下一个页面读出它再清掉。

Tifa 没有 flash 功能，也不需要。用到的零件是 `c.SetCookie`（可以在方法处理函数和 `Load` 里用，见 [Ctx](../reference/ctx.zh-CN.md)）、`c.Cookie` 和重定向。代码在一个 `tifa new` 建的临时应用里跑过：`go tool tifa check`、一个 Go 测试、用 curl 跑构建出的程序，还有浏览器。

## cookie {#the-cookie}

根包里的 `flash.go`：

```go
package flash

import (
	"net/http"

	"github.com/liustack/tifa"
)

const flashCookie = "flash"

// Messages are the texts a flash can show. The cookie holds a key of this table, never the
// text, so a visitor who writes their own cookie can show nothing but these.
var Messages = map[string]string{
	"saved":   "Item saved.",
	"removed": "Item removed.",
}

// SetFlash leaves a message for the next page the browser asks for. Call it in a method handler,
// before the redirect.
func SetFlash(c *tifa.Ctx, key string) {
	c.SetCookie(&http.Cookie{Name: flashCookie, Value: key, Path: "/", MaxAge: 60,
		Secure: c.Request().TLS != nil, SameSite: http.SameSiteLaxMode})
}

// TakeFlash returns the message left by SetFlash, and clears it so it shows once. Call it in the
// Load of a page that is not for everyone alike.
func TakeFlash(c *tifa.Ctx) string {
	ck, err := c.Cookie(flashCookie)
	if err != nil {
		return ""
	}
	c.SetCookie(&http.Cookie{Name: flashCookie, Value: "", Path: "/", MaxAge: -1,
		Secure: c.Request().TLS != nil, SameSite: http.SameSiteLaxMode})
	return Messages[ck.Value]
}

```

- **cookie 里放的是键，不是文本。** `Messages` 把键对应到文本。访客可以写任何他们想写的 cookie，有了键表，最坏也不过是让自己看到你自己写的某一条消息。来自 cookie 的自由文本，是访客写的文本。要显示带值的消息（「Item 'Tea' saved」），值放在数据库里或者按 id 去取，在页面里拼出文本。
- **它很短。** 60 秒。没人读过的消息，不该一小时后才冒出来。
- **脚本能读它**（没有 `HttpOnly`），因为 [浏览器这一边](#a-page-with-cache) 可能要读。像 `saved` 这样的键不是秘密。
- **清除时用同样的 `Path`。** 一个 cookie 只有被同名、同路径、同域的 `Set-Cookie` 才能清掉。

## 设上，再显示 {#set-it-then-show-it}

处理函数在重定向之前设上它。`app/items/new/route.go`：

```go
package new

import (
	"github.com/liustack/tifa"

	"example.com/flash"
)

func Load(c *tifa.Ctx) (struct{}, error) { return struct{}{}, nil }

type Form struct {
	Title string `json:"title"`
}

// POST saves the item, leaves a message and goes to the list.
func POST(c *tifa.Ctx, in Form) error {
	// ... save the item ...
	flash.SetFlash(c, "saved")
	return tifa.Redirect("/items", 303)
}

```

随后的页面在它的 `Load` 里取走它。`app/items/route.go`：

```go
package items

import (
	"github.com/liustack/tifa"

	"example.com/flash"
)

type View struct {
	Notice string `json:"notice"`
}

func Load(c *tifa.Ctx) (View, error) {
	return View{Notice: flash.TakeFlash(c)}, nil
}

```

页面在 `notice` 不为空时显示它，放在带 `role="status"` 的元素里，屏幕阅读器会读出来。

在浏览器里：在 `/items/new` 上，按钮调用生成的 `POST`，重定向经过路由器，`/items` 立刻显示「Item saved.」。刷新之后没有消息。用 curl 的话，POST 拿到 cookie，下一个 `/items` 的 GET 显示消息，没有脚本的表单看到的也是这样。

- **重定向到你正在看的页面。** 重定向的地址就是页面已有的地址时，路由器没有重新加载页面，消息要刷新之后才出现。请重定向到别的页面，就像表单之后去列表页那样。
- **304 也会清掉它。** `Load` 设的 `Set-Cookie` 会随 304、memo 命中和一次渲染一起发出，所以不管回答是怎么产生的，消息都会被取走。

### 配合渲染 memo {#with-the-render-memo}

渲染 memo（[渲染 memo](../reference/memo.zh-CN.md)）按数据给页面做键，消息是数据的一部分。带着 `flash=saved` 的请求拿到有消息的页面，同样的消息再来就拿到同一次渲染：第二次请求的 `X-Tifa-Render` 是 `memo`。没有 cookie 的请求拿到没有消息的页面。没有东西会从一个访客漏给另一个，你的代码也不用改。memo 命中时 `Set-Cookie` 仍然保留。不要为了这个把 memo 关掉。

## 带 Cache 的页面 {#a-page-with-cache}

带 `const Cache` 的页面对所有人都一样：它的请求在你的代码运行之前就丢掉了 cookie，它的 HTML 上的 `Set-Cookie` 会被去掉（[给 CDN 用的 Cache](../pages-and-data/ssr-and-cache.zh-CN.md#cache-for-cdns)）。由此有两件事：

- 它的 `Load` 看不到 flash cookie。里面直接写 `c.Cookie("flash")`，`tifa check` 会报「c.Cookie is always empty here」。通过另一个包里的辅助函数来读，比如上面的 `TakeFlash`，检查**不会**报，它只是什么也显示不出来。
- 它的回答什么也清不掉（curl 里看到它没有 `Set-Cookie`）。为这样一个页面设的 flash 会留在浏览器里，60 秒之内第一个不带 `Cache`、又调用 `TakeFlash` 的页面会把它显示出来，晚了一个页面。

两条出路：

1. **重定向到一个没有 `Cache` 的页面。** 写入之后的页面通常是访客刚改过的那个，这样做也更省事。
2. **在浏览器里读 cookie。** 页面是为所有人渲染的，所以消息要在页面加载之后再加，用一个读出并清掉 cookie 的 hook。`app/flash.ts`：

```ts
import { useEffect, useState } from 'react'

// The texts of the keys that flash.go may set. Keep it in step with Messages in flash.go.
const messages: Record<string, string> = {
  saved: 'Item saved.',
  removed: 'Item removed.',
}

// useFlash returns the message the last write left, once the page has loaded in the browser, and
// clears the cookie. It is for pages with Cache, whose server render cannot know the visitor.
export function useFlash(): string {
  const [text, setText] = useState('')
  useEffect(() => {
    const found = document.cookie.match(/(?:^|; )flash=([^;]*)/)
    if (!found) return
    document.cookie = 'flash=; Path=/; Max-Age=0; SameSite=Lax'
    setText(messages[decodeURIComponent(found[1])] ?? '')
  }, [])
  return text
}

```

页面调用 `const notice = useFlash()`，不为空时显示它。这个 effect 只在浏览器里、hydration 之后运行，所以服务器的 HTML 对所有人一样，也不会不匹配。在浏览器里，从 `/report/edit` 重定向到一个带 `Cache` 的页面，会显示「Item removed.」，刷新之后就没有了。`flash.ts` 里的表要和 `flash.go` 里的 `Messages` 保持一致。

## 其他办法 {#other-ways}

- **查询参数**，`/items?saved=1`。刷新和分享链接时它会再次显示，带 `Cache` 的页面还会为每个值保留一份副本。适合可以重复的消息。
- **Router state。** 路由器可以为一次导航带一个值，但整页加载（比如没有脚本的表单提交）会丢掉它。cookie 两种情况都管用。
- **处理函数的结果。** 有结果的处理函数可以返回消息，调用它的页面显示出来。页面里的调用可以，普通表单不行。

## 测试 {#test-it}

`app/items/flash_test.go` 用 `tifatest.Start` 启动应用。它的客户端保存 cookie，不跟随重定向，所以测试能看到每一步：

- POST 回答 303，带 `Set-Cookie: flash=saved`
- 第一次 GET `/items` 有「Item saved.」和一个清掉它的 `Set-Cookie`，第二次两者都没有
- 键不在表里的 cookie，比如 `<b>hello</b>`，什么也显示不出来
