---
summary: 'What an error you return becomes, who sees its text, 404 and ErrorBoundary'
audience: app
order: 7
read_when:
  - Finding out whether a visitor can see the error you return, and how to give them a message
  - Returning an error from `Load` or a handler, such as `tifa.Fields`, `tifa.Redirect` or `tifa.ErrNotFound`
  - Showing a 404 or an error in a page, or finding out which `ErrorBoundary` shows
---

# Errors and 404

## What an error you return becomes

Every function Tifa calls for a request returns an `error`: `Load`, `Layout`, a method handler, a `Validate` method and `OnRequest`. All of them go through the same place. An error reaches the visitor only when its type says it may be public. Any other error is unexpected: its text goes to the log, and the visitor gets a 500 with the plain status text. So `return err` is always safe.

| Returned | Status | The visitor sees | Goes to `OnError` |
| --- | --- | --- | --- |
| `tifa.ErrNotFound` | 404 | `Not Found` | No |
| `tifa.Redirect(url, status)` | A redirect | The redirect. Page loads and navigation follow it, and so does a handler function called from the browser (see [Calling method handlers from the browser](handlers.md#calling-method-handlers-from-the-browser)). It panics when `status` is not 3xx | No |
| `tifa.Error{Status, Message}` | Its `Status` | Its `Message`, or the status text when `Message` is empty. A status outside 400 to 599 is handled as an unexpected error | No |
| `tifa.Fields{"carrier": "pick a carrier"}` | 422 | A message per input field, by its json name | No |
| Any other error, a panic | 500 | The status text, or what `OnError` returns | Yes |

Errors from a database or another service often hold an address, a user name or a query. Because only the types above are public, none of that text reaches a visitor by accident.

### Giving the visitor a sentence

Return `tifa.Error` with a status and a message you wrote. For a message that belongs to one input, return `tifa.Fields`. Do not put `err.Error()` into `Message`, since that makes the text of any error public.

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

- Wrapping does not change this. Tifa looks for these types with `errors.Is` and `errors.As`, so `fmt.Errorf("ship: %w", tifa.Fields{...})` is still a 422. A wrapped `tifa.Error` shows only its `Message`, not the text around it.
- `tifa.Fields` is a map with an `Error` method, so an empty `tifa.Fields{}` returned as an `error` is not nil. End a validator with `return fields.Err()` (see [Method handlers](handlers.md#method-handlers-with-input-and-result)).
- A plain error from a `Validate` method is unexpected, like one from the handler. Only `tifa.Error` and `tifa.Fields` reach the visitor.
- When the request body or the query string cannot be decoded, the handler does not run. Tifa answers with a fixed short message (400, 413, 415 or 408), with no Go type names in it (see [Method handlers](handlers.md#method-handlers-with-input-and-result)).

### OnError

`OnError` in `hooks.go` gets every unexpected error once, a panic in `Load`, a handler or `OnRequest` included. It returns the `tifa.Error` the visitor sees. Report to your error tracker and write the log there (see [Hooks](hooks.md#hooksgo)). Without it, Tifa logs the error and answers 500. When `OnError` returns a status outside 400 to 599, the answer is 500, and an empty `Message` becomes the status text.

### What the browser gets

A method handler that fails answers JSON: `{"error": "message"}`, and for `tifa.Fields` also `"fields": {"carrier": "pick a carrier"}`. A call from a page throws a `TifaError` with `status`, `message` and `fields`. A failed `Load` or `Layout` reaches `ErrorBoundary` as the same `TifaError` (see [Errors on a page](#errors-on-a-page)). `tifa dev` shows the original error on the page. A built program never does.

## 404

A 404 shows in the `ErrorBoundary` of a route (see [Errors on a page](#errors-on-a-page)), which gets it as a `TifaError` with `status` 404:

```tsx
import { TifaError } from 'tifa/client'

export function ErrorBoundary({ error }: { error: unknown }) {
  if (error instanceof TifaError && error.status === 404) return <p>No such order</p>
  return <p>Something went wrong</p>
}
```

| Case | What is shown |
| --- | --- |
| A page's `Load` returns `tifa.ErrNotFound` | The first `ErrorBoundary` going up from that page. Outer Layouts are shown as usual |
| A `Layout` function returns `tifa.ErrNotFound` | The first `ErrorBoundary` going up from the Layout one level up |
| The URL matches no page | The first `ErrorBoundary` going up from the deepest Layout the URL matches, inside that Layout. The Layouts from the outermost one down to it run and show as usual: from the root, or from the `layout_.tsx` above it |
| No route has an `ErrorBoundary` | The framework's default 404 page |

The status code is 404 in all these cases, also after a navigation in the browser, whose data request answers with the data of the Layouts and status 404.

An unmatched URL whose last segment has a file extension (a dot and 1 to 8 letters or digits at the end, such as `/wp-login.php`) or that has a segment starting with a dot (`/.env`, `/.git/config`, `/.well-known/missing`) gets no 404 page. It gets the plain text 404 that a missing file under `/assets/` gets, with no `Load` and no render, so the scanners that ask for such paths cost almost nothing. `OnRequest` still runs and sees `tifa.ErrNotFound` from `next`. A URL that a route matches is not affected, a catch-all route and a parameter that holds a dot (`/users/john.doe` for `/users/{name}`) included, and neither is a navigation data request. A route or a public file under `/.well-known/` keeps answering.

To find the Layout of a URL that matches no page, Tifa compares the URL with the path of each Layout and page segment by segment, from the left. A literal segment beats a parameter, and a parameter beats the end of the path. The path that matches most wins, and a page counts as the Layout it is in. With these folders:

```text
app/layout.tsx
app/teams/team+/layout.tsx
app/teams/team+/members/page.tsx
app/teams/new/page.tsx
```

| URL | The 404 shows inside |
| --- | --- |
| `/nowhere` | the root Layout |
| `/teams/blue/missing` | the team Layout, whose `Layout` reads `c.Param("team")` as `blue` |
| `/teams/blue/members/x` | the team Layout |
| `/teams/new/x` | the root Layout: `new` is a literal, so `teams/new` wins over `teams/team+`, and that page is in the root Layout |

- A page in a `name++` folder takes every URL below it, so such a URL matches that page, and its `Load` decides what to answer.
- `OnRequest` runs for a URL that matches no page with an empty `c.Pattern()`. `next(c)` runs the Layouts and returns `tifa.ErrNotFound`. When `OnRequest` returns `tifa.ErrNotFound` itself, without calling `next`, no Layout runs, and the page shows as when the outermost `Layout` of that URL returns `tifa.ErrNotFound`: the root one, or that of the `layout_.tsx` above it.
- The `ErrorBoundary` of a Layout only ever shows in place of a route below it, in the Layout's `Outlet`, so it can read the Layout's data and the context its component provides. When a Layout fails, the `ErrorBoundary` of the Layout above shows, and when the root Layout or a Layout in `layout_.tsx` fails, the framework's default page.
- Tifa reads `default`, `head` and `ErrorBoundary` from `page.tsx` and `layout.tsx` (and `documentHead` from the root `layout.tsx`), and leaves other exports alone. A `NotFound` or `Error` export shows nothing: put what a 404 shows into `ErrorBoundary`, under `error.status === 404`.

## Errors on a page

`page.tsx` and `layout.tsx` can export `ErrorBoundary`, shown in place of that route when its `Load` or `Layout` fails, when a 404 shows there (see [404](#404)), or when a component below it throws:

```tsx
import { TifaError } from 'tifa/client'

export function ErrorBoundary({ error }: { error: unknown }) {
  if (error instanceof TifaError && error.status === 403) return <p>No access</p>
  return <p>Something went wrong</p>
}
```

- A failed `Load` or `Layout` reaches `ErrorBoundary` as a `TifaError` with `status` and the public message and an empty `fields` (the `tifa.Error` message, or what `OnError` returned). It is the same on the server and after hydration.
- A route without its own `ErrorBoundary` uses the one of the closest ancestor that exports one, then the framework's default. The ancestors end at a `layout_.tsx`.
- The status code of the page is that of the outermost failed route.
- A component that throws while the page renders on the server is an unexpected error: `OnError` sees it once and picks the status (500 without `OnError`) and the public message. The page then renders again with that error in place of the page, as a `TifaError`, so the first `ErrorBoundary` from the page up shows it inside the Layouts. If that render fails too, because the `ErrorBoundary` or a Layout around it throws, the error moves one route up, in place of the Layout the page is in, and so on up to the outermost Layout, the root one or a `layout_.tsx`. Only when the error in place of that Layout fails too does the browser get a plain error page from Go with the status and the public message. The browser hydrates the page with the error where the server put it, and does not run the route that threw again. No browser or CDN keeps such an answer (`Cache-Control: no-store`). The built app remembers it for 5 seconds instead: a request for the same page with the same data and session in that time gets the same answer without a render, and `X-Tifa-Render` says `memo`. Requests that wait for the page at the same time share the renders that place the error. `OnError` still sees every request, the first with the whole error and the others with its first line and a note that it was not rendered again. `tifa dev` remembers nothing, so a fix shows on the next reload. The stack trace shows only under `tifa dev`, over the page with the `ErrorBoundary`, and the page reloads once you save a fix (see [`tifa dev`](../build-and-deploy/dev-errors.md#when-a-page-fails-to-render)).
- A server render that uses up `ssr.vm_memory` or runs past `ssr.timeout` (see [Settings](../reference/settings.md)) stops right there. No `catch`, `finally` or promise handler in your code or in a library runs for it, so a page cannot catch it to show something else. The VM is replaced with a fresh one. A render that used up `ssr.vm_memory` renders again with the error once, on a fresh VM, in place of the outermost Layout, since the same data would use up the memory at any level. Its answer is remembered as above. A render that ran past `ssr.timeout`, or found every VM busy, does not: Go answers with its plain error page at once. All the renders of one request share one `ssr.timeout`, counted from the first, and a render with the error that runs out of it ends the tries. Code that recurses without end gets `RangeError: Maximum call stack size exceeded`, as in Node, which a `catch` can take (see [stack depth](../build-and-deploy/tuning.md#stack-depth)). When nothing catches it, it fails the render like any other error.
- Once the browser has taken over, a component that throws shows the nearest `ErrorBoundary`, the same as a failed `Load`.
- The document shell stays complete whatever fails, so the page still loads its scripts and styles.
- The page that shows the error gets the head of a page whose `Load` failed, see [The head of a 404 or an error page](head.md#the-head-of-a-404-or-an-error-page).
