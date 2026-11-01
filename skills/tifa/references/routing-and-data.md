# Routing and data

Contents: URLs from folders, `route.go`, `Load`, Layouts and `tifa.Use`, method handlers, `Ctx`, what `Load` may do with `Ctx`, the page side (`route.gen`), JSON rules, `hooks.go`.

Full docs: [app.md](https://github.com/liustack/tifa/blob/main/docs/app.md).

## URLs come from folders

Every folder under `app/` is one URL segment. `app/` itself is `/`.

| Folder | URL segment |
| --- | --- |
| `orders` | the literal `orders` |
| `order+` | the parameter `{order}`, read with `c.Param("order")` |
| `path++` | the rest of the path, last folder only, may be empty. Read with `c.Param("path")` in Go, `_splat` in TypeScript |

- A literal folder name uses only ASCII letters, digits and `- . _ ~`. Folders starting with `.` or `_` and folders named `testdata` are skipped by Go, and a convention file inside one fails the build. Other files there are fine.
- Name the Go package after the parameter: `project+` holds `package project`, and others import it as `project`.
- A folder has at most one parameter folder. Two routes whose URLs overlap fail the build, as in `http.ServeMux`. A literal beats a parameter, so `items/new` next to `items/id+` is fine.
- A folder with a Layout but no page shows a 404 inside that Layout for its own URL. Add the page, or leave it.
- Run `go tool tifa routes` to see what the build understood.

## route.go

All Go names of a folder live in its `route.go`. Declaring one in another file of the package fails the build. Helpers and types may go in any file.

```go
package order

import (
	"github.com/liustack/tifa"
	"github.com/liustack/tifa/examples/shop/app/projects/project+"
	"github.com/liustack/tifa/examples/shop/app/projects/project+/orders"
)

func Load(c *tifa.Ctx) (orders.Order, error) {
	p, err := tifa.Use(c, project.Layout)
	if err != nil {
		return orders.Order{}, err
	}
	return orders.GetOrder(p.ID, c.Param("order"))
}

type ShipInput struct {
	Carrier string `json:"carrier"`
}

func POST(c *tifa.Ctx, in ShipInput) (orders.Shipment, error) {
	return orders.ShipOrder(c.Param("project"), c.Param("order"), in.Carrier)
}
```

- `Load` returns the page data. It is allowed only next to a `page.tsx`.
- A folder with `page.tsx` is a page, and the framework answers its GET, so it cannot declare `GET`. A page with no data and no other method can be a `page.tsx` alone, without `route.go`.
- A folder with `route.go`, a method handler and no `page.tsx` is an API route. It cannot have `Load`.
- A `route.go` with only `Layout` is no API route. It gives the pages below it their Layout data.
- Everything is package-level functions and constants. Reach shared resources through package-level variables, not injection.

## Layouts

A Layout is the `Layout` function in `route.go` (the data) plus `layout.tsx` (the component), in the folder of the pages it wraps. Either may be missing. A Layout with no page in or below its folder fails the build.

```go
// app/projects/project+/route.go
func Layout(c *tifa.Ctx) (projects.Project, error) {
	return projects.GetProject(c.Param("project"))
}
```

```tsx
// app/projects/project+/layout.tsx
import { Outlet } from '@tanstack/react-router'
import { layout } from './route.gen'

export default function ProjectLayout() {
  const project = layout.useLoaderData()
  return (
    <section>
      <h1>{project.name}</h1>
      <Outlet />
    </section>
  )
}
```

- `c.Param` in a `Layout` may read only the parameters of its own folder path. `Load` is held to its own path the same way.
- Moving between pages under one Layout keeps the Layout mounted and its state. When a parameter the Layout function reads changes, it remounts.
- `tifa.Use(c, project.Layout)` gives a `Load` or `Layout` the result of a Layout above it (same folder or an ancestor), for the current request. It runs once per request however many callers ask. The argument must be a package-level `Layout` function of a Layout that wraps the caller. Anything else fails the build.
- On browser navigation only the `Load` and `Layout` of routes not already loaded run. A Layout whose data is cached does not run. Never rely on a Layout alone for a permission check. Put it in `OnRequest`, or make the page's `Load` call `tifa.Use` on the checking Layout so the check runs on every request.
- Name the file `layout_.tsx` to show a section without any Layout above it, for example a storefront inside a site that has an admin shell. Details in [app.md](https://github.com/liustack/tifa/blob/main/docs/app.md#a-layout-without-the-layouts-above).

## Method handlers with input and result

```go
func POST(c *tifa.Ctx) error                       // writes its own response
func POST(c *tifa.Ctx, in ShipInput) error         // 204 on success
func POST(c *tifa.Ctx) (Shipment, error)           // 200 with the result as JSON
func POST(c *tifa.Ctx, in ShipInput) (Shipment, error)
```

- The input is a struct (not a pointer). POST, PUT and PATCH decode it from the body: JSON, or a URL-encoded or multipart form. GET and DELETE decode it from the query string, so only strings, numbers, bools and slices of them.
- Fields are named by their `json` tags. A `form` tag fails the build.
- If the input type has `Validate() error`, it runs before the handler. Return `tifa.Fields{"carrier": "pick a carrier"}` for a message per field (422). Any other plain error answers 400 with its text, which the user sees.
- A body that does not decode answers 400 (413 over the body limit, 415 for another content type). The handler does not run.
- A handler that writes its own response reads input with `c.Bind(&in)` or `c.BindQuery(&in)`, and answers with `c.JSON`, `c.Text` or `c.NoContent`.
- A POST, PUT, PATCH or DELETE sent by a browser from another site gets 403 before your code runs. See [build-and-deploy.md](build-and-deploy.md) for letting chosen origins through.

## Ctx

| Method | What it does |
| --- | --- |
| `c.Param(name)`, `c.Query(name)`, `c.QueryAll(name)` | Path parameter, first query value (`""` when absent), every value of a repeated query key |
| `c.Header(name)`, `c.Cookie(name)` | Request header, request cookie |
| `c.Context()`, `c.SetContext(ctx)` | Request context, and replacing it to carry request-scoped values |
| `c.Pattern()` | The matched route, such as `/projects/{project}` |
| `c.IsData()` | True for the browser router's navigation data request |
| `c.SetHeader(k, v)`, `c.SetCookie(cookie)` | Response header and cookie. Allowed in `Load` too |
| `c.JSON(code, v)`, `c.Text(code, s)`, `c.NoContent(code)` | Write the response. Not allowed in `Load` or `Layout` |
| `c.Redirect(url, code)` | Returns an error value. Return it to redirect |
| `c.Bind(&v)`, `c.BindQuery(&v)` | Decode body or query into a struct. Not in `Load` |
| `c.Next()` | In `OnRequest`, run the matched route |
| `c.Request()`, `c.Writer()` | Raw request and writer. Method handlers and `OnRequest` only |

Request-scoped values (the signed-in user) ride on the standard context: `OnRequest` calls `c.SetContext(context.WithValue(...))` and a domain package offers a typed getter. Domain code takes `context.Context`, not `*tifa.Ctx`.

## What Load and Layout may do with Ctx

At build time tifa reads `Load`, `Layout` and the module functions they call, to find which path parameters, query parameters and Layouts each page depends on. That decides when the browser must fetch data again. So:

- Pass `c.Param`, `c.Query` and `c.QueryAll` a constant string: `c.Param("order")`. A variable name fails the build.
- Call them and `tifa.Use` directly. Storing the method in a variable or passing it as a value fails.
- Do not call `c.Request()`, `c.Writer()`, `c.Bind` or `c.BindQuery`. Read each query value with `c.Query("name")`. A body does not exist in a page load.
- `*tifa.Ctx` may be passed to a plain (non-generic) function of this module or of tifa, or used for a direct method call. Converting it, returning it, putting it in a slice, map or channel, assigning to an existing variable, `go` or `defer` with it, passing it to an interface, a function variable, a generic function or a library function all fail the build, and the error names the file and line.
- If a helper needs only the context, pass `c.Context()`.

```go
// fine
func Load(c *tifa.Ctx) (Page, error) { return page(c, c.Query("tab")) }
func page(c *tifa.Ctx, tab string) (Page, error) { return build(c.Context(), c.Param("order"), tab) }

// fails the build: the name is not a constant
func param(c *tifa.Ctx, name string) string { return c.Param(name) }
```

Full rules: [app.md](https://github.com/liustack/tifa/blob/main/docs/app.md#how-load-may-use-ctx).

## The page side

```tsx
// app/projects/project+/orders/order+/page.tsx
import { Link } from '@tanstack/react-router'
import { page, layout, POST, type PageData } from './route.gen'

export default function OrderPage() {
  const order = page.useLoaderData()        // typed from the Go return value of Load
  const params = page.useParams()
  return <Link to="/projects/$project" params={{ project: params.project }}>{order.id}</Link>
}

export const head = ({ loaderData }: { loaderData?: PageData }) => ({
  meta: loaderData ? [{ title: loaderData.id }] : [],
})
```

- `./route.gen` is generated for each folder with a page, Layout or method handler. It exports `page` and `layout` (only when the folder has them), `PageData`, `LayoutData`, `Params`, `POSTInput`/`POSTOutput` style types, and one function per method handler. To read a Layout above you, import `layout` from that folder's `route.gen`, such as `'../../route.gen'`. `go tool tifa build` or `tifa dev` writes them, and `tsc` needs them, so run one first.
- Links are typed from the folders: `<Link to="/todos/$todo" params={{ todo: '1' }}>` from `@tanstack/react-router`.
- Pages are UI only. Data comes from `Load`.
- Exports of `page.tsx`: the default component, `head`, `Error`. See [errors-and-404.md](errors-and-404.md) and [rendering.md](rendering.md).
- Each `page.tsx` and non-root `layout.tsx` is its own chunk in the browser. Its top-level code runs when its route first loads, not at app start. Put setup every page needs (an i18n instance, say) in the root Layout or a module it imports.

### Calling handlers from the browser

```tsx
import { POST } from './route.gen'
import { TifaError } from 'tifa/client'

async function ship() {
  try {
    const shipment = await POST({ params: { project, order }, input: { carrier: 'SF' } })
  } catch (e) {
    if (e instanceof TifaError) setErrors(e.fields) // e.status, e.message, e.fields
  }
}
```

- The function takes one object: `params` (left out when the folder has none, `_splat` for a `name++` folder), `input` (left out when the handler takes none), `signal`, and `invalidate`.
- GET and DELETE send the input in the query string, the others as a JSON body.
- After a successful POST, PUT, PATCH or DELETE the data of the current page and its Layouts loads again before the promise resolves. Pass `invalidate: false` to skip it. After a write that bypasses these functions, call `router.invalidate()`.
- An error answer throws `TifaError` with `status`, `message` and `fields` (the `tifa.Fields` messages, empty otherwise).
- When the handler (or `OnRequest`) returns `c.Redirect(...)`, the browser follows it, and the promise resolves to `undefined`. So the result type of a handler with a result includes `undefined`: check it before use.
- A handler that writes its own response (`func POST(c) error`) gives back the `Response`, also for 4xx and 5xx. Check `response.ok`.
- Call these from event handlers or effects. Calling one during the server render throws.

To go to the page of what a handler created, redirect from Go:

```go
func POST(c *tifa.Ctx, in AddInput) error {
	todo, err := AddTodo(in.Title)
	if err != nil {
		return err
	}
	return c.Redirect("/todos/"+todo.ID, http.StatusSeeOther)
}
```

## JSON rules

What `Load`, `Layout` and handlers return or take travels as JSON, and tifa writes TypeScript types from the Go types. Only these types are accepted:

| Go | TypeScript |
| --- | --- |
| `bool` | `boolean` |
| integers, floats, `json.Number` | `number` |
| `string`, `time.Time` | `string` |
| pointer | `T \| null` |
| slice | `T[] \| null` (`[]byte` becomes a base64 `string \| null`) |
| array | `T[]` |
| map | `Record<string, V> \| null` |
| struct | object of its exported fields |

- No interfaces anywhere (fields, elements, map keys), no chan, func, complex numbers or `unsafe.Pointer`.
- A type with a method or field named `MarshalJSON`, `MarshalJSONTo`, `AppendText` or `AppendJSON` is rejected (except `time.Time` and `json.Number`). Rename it or return another type.
- `MarshalText() ([]byte, error)` on the value receiver makes a type a string. A handler input with `MarshalText` also needs `UnmarshalText`.
- Embedded exported structs without a json name are flattened. Unexported embedded fields are not accepted.
- Tags: `omitempty`, `omitzero` and `string` are the only options. `omitempty` or `omitzero` make the field optional in TypeScript.
- Go's nil slice or map becomes `null`. Write `data.items ?? []` in TSX.

If the build rejects a type, the error names the file and line. Convert to a plain struct in the route package rather than adding methods.

## hooks.go

`hooks.go` sits in the root package. All four functions are optional:

```go
func OnStart() error                            // before listening: open the database, etc.
func OnShutdown(ctx context.Context) error      // after a stop signal, once requests finish
func OnRequest(c *tifa.Ctx) error               // the only middleware, call c.Next() to continue
func OnError(c *tifa.Ctx, err error) tifa.Error // report unexpected errors, pick what users see
```

- `OnRequest` runs for pages, navigation data requests, API routes and unmatched URLs. Not for static files. A cross-site write is refused before it.
- `OnError` gets errors that are not `ErrNotFound`, `tifa.Error`, `tifa.Fields` or a redirect, and panics. Writing one replaces the default (log and 500). Return a message that is safe for users.
- Auth example. `signedIn` is the app's own function:

```go
func OnRequest(c *tifa.Ctx) error {
	if strings.HasPrefix(c.Pattern(), "/admin") && !signedIn(c) {
		return c.Redirect("/login", http.StatusFound)
	}
	return c.Next()
}
```

- `c.Next()` returns the outermost `Load` error. Return it unchanged (or wrapped once with `%w`) to let the framework handle it. Returning another error makes the middleware win.
- Use `OnStart` for background goroutines too (jobs, mail queues) and stop them in `OnShutdown`. Do not use `init()`.
