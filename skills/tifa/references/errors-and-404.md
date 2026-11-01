# Errors, 404 and redirects

Contents: what to return, what the user sees, writing `Error`, how a 404 is placed, render failures, `OnError`.

Full docs: [app.md](https://github.com/liustack/tifa/blob/main/docs/app.md#404) and [build.md](https://github.com/liustack/tifa/blob/main/docs/build.md#page-requests).

## What to return from Load, Layout and handlers

| Return | Result |
| --- | --- |
| `tifa.ErrNotFound` | 404 |
| `c.Redirect(url, code)` | A redirect. Page loads, browser navigation and handler calls from the browser all follow it |
| `tifa.Error{Status: 403, Message: "No access"}` | That status and message. Does not go to `OnError`. Status must be 400 to 599 |
| `tifa.Fields{"carrier": "pick a carrier"}` | 422 with a message per input field, by json name. Does not go to `OnError` |
| Any other error, or a panic | Goes to `OnError`. Without one it is logged and answered 500 |

A failed method handler answers JSON: `{"error": "message"}`, plus `"fields"` for `tifa.Fields`.

The messages of `tifa.Error` and `OnError` reach the user. Never put internal details in them.

In `Load`, `c.JSON`, `c.Text` and `c.NoContent` return an error. You can set headers and cookies there.

## Writing the Error component

`page.tsx`, `layout.tsx` and `layout_.tsx` can export `Error`. It shows in place of that route when its `Load` or `Layout` fails, when a 404 lands there, or when a component below it throws.

```tsx
import { RouteError } from 'tifa/client'

export function Error({ error }: { error: unknown }) {
  if (error instanceof RouteError && error.status === 404) return <p>No such order</p>
  if (error instanceof RouteError && error.status === 403) return <p>No access</p>
  return <p>Something went wrong</p>
}
```

- A failed `Load` or `Layout` arrives as a `RouteError` with `status` and the public message. It is the same on the server and after hydration.
- A route with no `Error` of its own uses the nearest ancestor's, then the framework's default page. The ancestors stop at a `layout_.tsx`.
- The status code of the page is that of the outermost failed route.
- Exporting `NotFound` fails the build. Show the 404 inside `Error`, under `error.status === 404`.
- Put an `Error` in the root `layout.tsx`. It also shows the 404 of URLs that match no page, and `tifa new` writes one.

## Which Error shows for a 404

| Case | Shown |
| --- | --- |
| A page's `Load` returns `tifa.ErrNotFound` | The first `Error` going up from that page. Outer Layouts show as usual |
| A `Layout` returns `tifa.ErrNotFound` | The first `Error` going up from the Layout one level up |
| The URL matches no page | The first `Error` going up from the deepest Layout the URL matches, inside that Layout |
| No route has an `Error` | The framework's default 404 page |

The status is 404 in every case, also after browser navigation. For a URL that matches no page, tifa compares the URL with the Layout and page paths segment by segment. A literal wins over a parameter. A page in a `name++` folder takes every URL below it, so its `Load` decides.

A language or tenant segment is checked in the `Layout` of its folder. Returning `tifa.ErrNotFound` there shows the 404 of the Layout above, so unknown languages never look like valid pages (see [i18n.md](i18n.md)).

## When a component throws on the server

A component that throws while rendering is an unexpected error. `OnError` sees it once and picks the status and public message. The page renders again with that error in place of the page, so the first `Error` from the page upward shows it inside the Layouts. If that fails too, the error moves one route up, and finally Go answers with a plain error page. The browser hydrates the page with the error where the server put it. Such an answer is never cached.

A render that runs past `ssr.timeout`, or that uses up `ssr.vm_memory`, stops at once. No `catch` or `finally` in your code runs for it. A render that finds all VMs busy answers 503 with `Retry-After: 1` and does not call `OnError`.

Under `tifa dev` the stack trace shows over the page, and the page reloads when you save a fix.

## OnError

```go
func OnError(c *tifa.Ctx, err error) tifa.Error {
	slog.ErrorContext(c.Context(), "request failed", "path", c.Request().URL.Path, "err", err)
	return tifa.Error{Status: 500, Message: "Something went wrong, try again later"}
}
```

It gets every unexpected error and panic once. Report it there. The returned `tifa.Error` is what users may see.

## Redirects

Return `c.Redirect("/login", http.StatusFound)` from `Load`, `Layout`, a handler or `OnRequest`. From a handler called in the browser, the router follows it without a page reload for URLs of this site. A redirect that depends on the request (cookie, `Accept-Language`) uses 302 and should add `Vary` (`c.Writer().Header().Add("Vary", "Cookie, Accept-Language")` in a method handler).
