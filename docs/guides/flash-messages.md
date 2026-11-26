---
summary: 'Show a message once after a redirect ("Item saved") in a Tifa app: a short cookie set by the handler, read and cleared by the next page, and what to do on a page with Cache'
audience: app
order: 16
read_when:
  - Showing "saved", "deleted" or "signed out" once after a write that redirects
  - The message shows twice, does not show on a page with `Cache`, or does not show after a client-side navigation
  - Deciding between a cookie, a query parameter and router state for a message after a redirect
---

# A message after a redirect

A write ends in a redirect, and the next page should say "Item saved" once. The message is not part of the address, so something has to carry it from the handler to the next request. The usual way, and the one this guide shows, is a short cookie: the handler sets it, the next page reads it and clears it.

Tifa has no flash feature, and does not need one. The pieces are `c.SetCookie` (allowed in a method handler and in a `Load`, see [Ctx](../reference/ctx.md)), `c.Cookie` and a redirect.

## The cookie

`flash.go` in the root package:

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

- **The cookie holds a key, not the text.** `Messages` maps keys to texts. A visitor can write any cookie they like, and with a key table the worst they can show to themselves is one of your own messages. Free text from a cookie is text the visitor wrote. For a message with a value in it ("Item 'Tea' saved"), keep the value in the database or ask for it by id, and build the text in the page.
- **It is short.** 60 seconds. A message that nobody read should not turn up an hour later.
- **It is readable by scripts** (no `HttpOnly`), because [the browser side](#a-page-with-cache) may need it. A key such as `saved` is no secret.
- **It is cleared with the same `Path`.** A cookie is cleared only by a `Set-Cookie` with the same name, path and domain.

## Set it, then show it

The handler sets it before the redirect. `app/items/new/route.go`:

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

The page that follows takes it in its `Load`. `app/items/route.go`:

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

The page shows `notice` when it is not empty, in an element with `role="status"` so a screen reader says it.

From `/items/new`, the button calls the generated `POST`, the redirect goes through the router, and `/items` shows "Item saved." at once. A reload shows no message. A form without script gets the same: the POST answers with the cookie and the next GET of `/items` shows the message.

- **A redirect to the page you are on.** When the redirect goes to the address the page already has, the router did not load the page again, and the message showed only after a reload. Redirect to another page, as the list after a form does.
- **A 304 clears it too.** The `Set-Cookie` of a `Load` goes out on a 304, on a memo hit and on a render alike, so the message is taken whichever way the answer is made.

### With the render memo

The render memo ([The render memo](../reference/memo.md)) keys a page by its data, and the message is part of the data. A request with `flash=saved` gets the page that has the message, and the same message gets the same render again: `X-Tifa-Render` said `memo` on the second request. A request without the cookie gets the page without it. Nothing leaks from one visitor to another, and nothing in your code changes. The `Set-Cookie` is kept on a memo hit. Do not turn the memo off for this.

## A page with Cache

A page with `const Cache` is the same for everyone: its requests lose their cookies before your code runs, and a `Set-Cookie` on its HTML is removed ([Cache for CDNs](../pages-and-data/ssr-and-cache.md#cache-for-cdns)). Two things follow:

- Its `Load` sees no flash cookie. A direct `c.Cookie("flash")` in it fails `tifa check` with "c.Cookie is always empty here". `TakeFlash` is not reported, because pages without `Cache` call it too ([The build checks it](../pages-and-data/ssr-and-cache.md#cache-for-cdns)): on this page it just shows nothing.
- Its answer clears nothing. A flash set for such a page stays in the browser, and the first page without `Cache` that calls `TakeFlash` within the 60 seconds shows it, a page late.

Two ways out:

1. **Redirect to a page that has no `Cache`.** The page after a write is usually one a visitor has just changed, and it is the cheaper choice.
2. **Read the cookie in the browser.** The page is rendered for everyone, so the message is added after it loads, with a hook that reads and clears the cookie. `app/flash.ts`:

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

A page calls `const notice = useFlash()` and shows it when it is not empty. The effect runs only in the browser, after hydration, so the server HTML is the same for everyone and nothing mismatches. A redirect from `/report/edit` to a page with `Cache` shows "Item removed." and a reload does not. Keep the table in `flash.ts` in step with `Messages` in `flash.go`.

## Other ways

- **A query parameter**, `/items?saved=1`. It shows again on a reload and in a shared link, and a page with `Cache` keeps a copy for each value. Fine for a message that may repeat.
- **Router state.** The router can hold a value for one navigation, but a full page load, such as a form that posts without script, loses it. The cookie works for both.
- **The result of the handler.** A handler with a result can return the message and the page that called it shows it. It works for a call from the page and not for a plain form.

## Test it

`app/items/flash_test.go` starts the app with `tifatest.Start`. Its client keeps cookies and does not follow redirects, so the test sees each step:

- the POST answers 303 with `Set-Cookie: flash=saved`
- the first GET of `/items` has "Item saved." and a `Set-Cookie` that clears it, and the second has neither
- a cookie with a key that is not in the table, such as `<b>hello</b>`, shows nothing
