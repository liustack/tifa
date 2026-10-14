---
summary: 'How to write a tifa app: folder layout, how folders become URLs, route.go and route.tsx, Layouts, hooks.go, 404s, the SSR and cache switches, how Load may use Ctx, and the JSON rules for what Load returns'
read_when:
  - Creating a tifa app, or adding pages, Layouts or API routes to one
  - The build reports an error about Ctx usage or a Load return type and you want to fix it
  - Changing the rules in internal/analyze and you need to know what users were promised
  - Changing the apps in examples/shop or test/apps
---

English · [简体中文](app.zh-CN.md)

# Writing an app

For a complete example, see [examples/shop](../examples/shop). For the build, the generated files and how to set up go.mod, see [build.md](build.md).

## Folders

```text
myapp/
  go.mod
  hooks.go                          process and request hooks (optional)
  db.go                             shared resources: package-level variables and functions (optional)
  project.go  order.go              domain code, one file per domain, flat in the root package
  app/
    layout.go  layout.tsx           root Layout, wraps every page
    route.go                        /
    projects/
      project+/
        layout.go  layout.tsx       Layout of /projects/{project} and every page below it
        route.go  route.tsx         page /projects/{project}
        orders/
          order+/
            route.go  route.tsx     page /projects/{project}/orders/{order}
            items.tsx  actions.client.tsx
```

- Code is grouped by domain. A new app keeps its domain code (types, storage, rules) flat in the root package, one file per domain, and folders under `app/` hold pages that call it. A domain moves into its own package when it grows. There is no shared `models/` or `db/` package holding every table. See [structure.md](structure.md).
- Components and styles shared across pages go in the `app/` root.
- Do not write a main. `tifa build` generates main and the route registration code in `.tifa/`, which `go vet ./...` and `go test ./...` do not scan.

## URLs come from folders

Every folder below `app/` is one segment of the URL. `app/` itself is `/`.

| Folder name | URL segment |
| --- | --- |
| `orders` | the literal `orders` |
| `order+` | the parameter `{order}`, read with `c.Param("order")` |
| `path++` | the rest of the path `{path...}`, only as the last folder |

- A literal folder name holds only ASCII letters, digits and `- . _ ~`. A parameter name starts with a letter or `_`, followed by letters, digits or `_`, and appears once per URL.
- The go command takes the package name from the `package` line, not from the folder. Name the package after the parameter: `project+` holds `package project`, and other packages import it as `project`.
- The go command skips folders that start with `.` or `_`, and folders named `testdata`. A `route.go`, `route.tsx`, `layout.go` or `layout.tsx` inside one fails the build. Other files there, such as a `_components` folder, are fine.
- A folder name must also be valid in a Go import path, so names such as `con` or `x~1` (Windows reserved and short names) fail the build.
- Two pages whose URLs match the same requests fail the build, such as `items/id+` next to `items/key+`. So do two whose URLs overlap without one being more specific, as in Go's `http.ServeMux`. A literal is more specific than a parameter, so `items/new` next to `items/id+` is fine.
- Declaring `const Path` fails the build. The folder is the URL.

## route.go

```go
package order

func Load(c *tifa.Ctx) (shop.Order, error) {
	p, err := tifa.Use(c, project.Layout)
	if err != nil {
		return shop.Order{}, err
	}
	return shop.GetOrder(p.ID, c.Param("order"))
}

func POST(c *tifa.Ctx) error {
	if err := shop.ShipOrder(c.Param("project"), c.Param("order")); err != nil {
		return err
	}
	return c.NoContent(204)
}
```

- `Load` returns the page data. Its return type must convert to TypeScript (see "JSON rules for what Load returns" below).
- `GET`, `POST`, `PUT`, `PATCH` and `DELETE` handle the HTTP method they are named after.
- A folder with a `route.tsx` is a page. The framework renders its GET, so you cannot also write `GET`. A page that needs no data and no other method can be `route.tsx` alone, without `route.go`.
- A folder with `route.go` and no `route.tsx` is an API route: every method handler answers as written, and `Load` is not allowed, because `Load` loads the data of a page.
- Everything is package-level functions and constants. Use the global variables the app has set up for dependencies. The framework does no dependency injection.

When an error is returned:

| Returned | Result |
| --- | --- |
| `tifa.ErrNotFound` | 404 |
| `c.Redirect(url, code)` | Redirect |
| `tifa.Error{Status, Message}` | Answered with its status code and message, does not go to `OnError` |
| Any other error, a panic | Goes to `OnError`. Without one, it is logged and answered with 500 |

In `Load` you can only set response headers and cookies. Calling `c.JSON`, `c.Text` or `c.NoContent` returns an error.

## route.tsx

```tsx
import { route, type Data } from './+route'

export default function OrderPage() {
  const order = route.useLoaderData()
  return <OrderItems />
}

export const head = ({ loaderData }: { loaderData?: Data }) => ({
  meta: loaderData ? [{ title: loaderData.id }] : [],
})
```

- UI only, no data loading. `./+route` is a generated module that exports `route`, `Data` (the return type of `Load`) and `Params`. Child components in the same folder also import `./+route`. A Layout's data comes from the `+layout` module of its folder: `layout.tsx` imports `'./+layout'`, and a page reads its project Layout with `'../../+layout'`, for example.
- The default export is the page component. Optional exports: `head`, `Error`, `NotFound`.
- Go slices and maps carry `| null` in TypeScript (Go's nil is encoded as `null`). Write `?? []` before using them.
- Put components that need browser features at the module top level into a `.client.tsx` file, and load them lazily with `clientOnly` from `tifa/client`. They are left out of the SSR bundle, and the first paint shows a placeholder:

```tsx
import { clientOnly } from 'tifa/client'

const OrderActions = clientOnly(() => import('./actions.client'), <button disabled>正在准备操作…</button>)
```

- After a write, call TanStack's `router.invalidate()`. The data of the current page and all its Layouts is loaded again, merged into one request.

## Layout

A Layout lives in its folder next to the page of that folder, if there is one: `layout.go` and `layout.tsx` wrap the page in the same folder and every page below it. `layout.tsx` places child routes with `<Outlet />`. A Layout with no page in its folder or below it fails the build.

```go
package project

func Layout(c *tifa.Ctx) (shop.Project, error) {
	return shop.GetProject(c.Param("project"))
}

const LayoutSSR = false // optional, default true
```

- `layout.go` and `route.go` of one folder are one Go package, so `Layout`, `LayoutSSR` and the page's `Load`, `SSR`, `Cache` and method handlers may sit in either file. In a folder with no page, the page names fail the build.
- A Layout's `c.Param` may only read parameters of its own folder path. `app/projects/project+/layout.go` may read `project`, not `order`.
- When you switch between pages under the same Layout, the Layout component is not remounted and keeps its state. When a parameter the Layout function reads changes (for example, you move to another project), the Layout remounts and its state resets. When a parameter it does not read changes, the state is kept. Pages work the same way.
- Pages and Layouts can call `tifa.Use(c, project.Layout)` to get the result of a Layout that wraps them, for the current request: the Layout of the same folder or of an ancestor folder. The same `Layout` runs only once per request. Any other Layout fails the build.
- On navigation in the browser, the navigation data request runs only the `Load` and `Layout` functions of the listed routes. If an ancestor Layout's data is still in the cache, that Layout is not in the list and its `Layout` does not run this time. So do not put permission checks only in a Layout. Put them in `OnRequest`, or have the page's `Load` depend on the checking Layout through `tifa.Use`, so the check runs on every request.
- The document shell (`<html>`, `<head>`, assets and the script that injects data) is written as a string by the framework. React only renders the app root inside `<body>`, so the page is still a complete document when any level fails or returns 404. The root Layout (`app/layout.tsx`) can export two constants to customize the shell: `htmlAttributes` sets the attributes of `<html>` (`{ lang: 'en' }` when not exported), and `documentHead` holds head tags that every page carries (also when no route matches or the root `Layout` fails), in the same format as the return value of `head`. Put the site's default `title` here so that the 404 page for an unmatched URL also has a title. A page's own `head` that returns a `title` overrides it. Exporting a `Document` component fails the build.

```tsx
export const htmlAttributes = { lang: 'zh-CN' }
export const documentHead = {
  meta: [{ title: '山海工作台' }, { name: 'theme-color', content: '#f7f8f5' }],
  links: [{ rel: 'icon', href: '/favicon.svg' }],
}
```

- A page's head comes from the `head` return values of the matched routes: `title` and meta tags with the same name come from the deepest route, and link, style and script tags are deduplicated in route order. The framework always adds `<meta charset="utf-8">` and a viewport meta, and a route's `name: 'viewport'` meta replaces the latter. Tag attributes are written the way React DOM writes them on the server: `className`, `htmlFor`, `httpEquiv`, `crossOrigin` and so on become their HTML names. Event attributes starting with `on`, invalid attribute names, `null`, `undefined` and functions are not written. Boolean attributes such as `async` and `defer` are written with an empty value when true. For other attributes, boolean values are written as strings only on `data-` and `aria-` attributes. `javascript:` links are replaced. Head tags do not support the `style` attribute, and using it makes rendering fail. `htmlAttributes` follows the same rules. The content of style and script tags is written as is. Only the `<script` and `</script` (in style, `<style` and `</style`) that would end the element are escaped, the way React DOM does it. After browser navigation and after `router.invalidate()`, the head updates to match the new routes.

## 404

`route.tsx` and `layout.tsx` can export `NotFound`:

| Case | What is shown |
| --- | --- |
| The URL matches no page | The root Layout's `NotFound`, without the root Layout's UI, and no `Load` or `Layout` runs |
| A page's `Load` returns `tifa.ErrNotFound` | The first `NotFound` going up from that page. Outer Layouts are shown as usual |
| A `Layout` function returns `tifa.ErrNotFound` | The search starts at the Layout one level up |
| Nothing exports one | The framework's default page |

The status code is 404 in all these cases. The root Layout's `NotFound` is sometimes shown on its own, so it cannot read loader data.

## SSR and cache

```go
const SSR = false       // the page, default true
const Cache = true      // the page, default false
const LayoutSSR = false // the Layout of the folder, default true
```

- `SSR = false`: middleware and every `Load` run as usual, but QuickJS is skipped. The response is a minimal document with the data, and the browser renders it. `LayoutSSR = false` applies to every page the Layout wraps.
- `Cache = true`: the rendered result is cached by build version, URL and data. When the data has not changed, the HTML is returned directly. Components must not read values that change on every call, such as `Date.now()` or `Math.random()`, while rendering. Hydration requires this too.
- Every page response carries an ETag. A second request with `If-None-Match` gets 304 when the data has not changed. This has nothing to do with `Cache`.

## hooks.go

`hooks.go` in the project root. All four functions are optional:

```go
func OnStart() error                            // before listening: open the database, logs, etc.
func OnShutdown(ctx context.Context) error      // after an exit signal, once in-flight requests finish
func OnRequest(c *tifa.Ctx) error              // the only middleware, call c.Next() to continue
func OnError(c *tifa.Ctx, err error) tifa.Error // unexpected errors: report, return an error users may see
```

- `OnRequest` runs for pages, navigation data requests, API routes and unmatched URLs. It does not run for static files. For an unmatched URL, `c.Pattern()` is empty.
- Resources the whole app shares, such as a database connection or a logger, go in files at the app root next to `hooks.go`, for example `db.go` or `middleware.go`. Each one builds a package-level variable or a function: set it up in `OnStart`, release it in `OnShutdown`, and use it directly anywhere, for example `myapp.DB`. Packages under `app/` import the root package for this, so the root package does not import them back (Go does not allow import cycles). Tables, models and queries stay in their domains. Route code uses these resources only inside functions, never while package-level variables are initialized. Do not use `init()` for setup.
- Store request-scoped values with the standard context: `c.SetContext(context.WithValue(...))`. Business packages provide typed getters.

## How Load may use Ctx

At build time, tifa starts from each `Load` and `Layout` function and finds the route parameters (`c.Param`), query parameters (`c.Query`) and Layouts (`tifa.Use`) it reads. When `Load` passes `*tifa.Ctx` to a function in this module, tifa follows the call level by level, across packages too, and counts every parameter it finds toward that `Load`. A helper called by several `Load` functions counts toward each of them. tifa does not follow into its own functions.

So that tifa finds everything, Ctx may only be used where tifa can see it.

The only types that may hold a Ctx are: `*tifa.Ctx`, `tifa.Ctx`, non-generic structs declared in this module (whose fields hold Ctx only through these types, directly, embedded or indirectly), and pointers to such structs.

A value that holds a Ctx may only appear in these five places:

1. As the receiver of a direct method call, such as `c.Query("tab")` or `req.Page()`.
2. As an argument to a non-generic function from this module or from tifa, where the parameter has exactly the same type. A function literal called in place also counts.
3. In a composite literal of a struct that holds Ctx, for a field of exactly the same type.
4. Assigned with `:=` or `var x T = ...` to a new local variable of exactly the same type. That variable follows the same rules.
5. With its address taken, where the resulting pointer is still a type that may hold Ctx.

Any other use fails the build and names the file and line. For example: converting it to another type or an interface, assigning it to an existing variable, assigning several values at once, `return`ing it, putting it in a slice, map, array or channel, `range`, package-level variables, `defer`, `go` statements, or passing it to a generic function, an interface method, a function variable or a function outside the module (the standard library and third-party libraries). `return c.Redirect(...)` and `return c.Next()` are fine, because they return the result of a method call, not the Ctx itself.

`Load` and the functions it calls must not call `c.Request()` or `c.Writer()`. tifa cannot see parameters read through the raw request. Use `c.Param`, `c.Query`, `c.Header`, `c.Cookie` or `c.Context()` instead. `c.Request()` and `c.Writer()` are only for method handlers and `OnRequest`.

When you hit one of these errors, call Ctx methods directly, or pass it to a plain function. If you only need the request context, pass `c.Context()`. A helper that only calls `c.Context()` (such as `auth.User(c)`) collects nothing when tifa follows it.

tifa only checks code that `Load` can reach. If you store a Ctx in a global variable or another shared place from `OnRequest`, a method handler or anywhere else, and then take it out in `Load`, tifa cannot catch it. Do not do this.

## JSON rules for what Load returns

The value `Load` returns is encoded as JSON and sent to the page, and tifa generates TypeScript types from it. So that the output is the same under both `encoding/json` implementations in Go 1.27 (the default one and `GOEXPERIMENT=nojsonv2`), only the types below are accepted.

| Go | TypeScript |
| --- | --- |
| `bool` | `boolean` |
| integers, floats, `json.Number` | `number` |
| `string`, `time.Time` | `string` |
| pointer | `T \| null` |
| slice | `T[] \| null`. A `[]byte` whose element is exactly `byte` or `uint8` is written as base64 and becomes `string \| null`. Slices of a named byte type are not accepted |
| array | `T[]`, or `[]` when the length is 0 |
| map | `Record<string, V> \| null` |
| struct | an object of its exported fields |

- Map keys must be string or integer types without `MarshalText`, or types that output a string under the rules below. Interfaces and pointers cannot be keys.
- An exported embedded struct (a pointer works too) without a json name has its fields promoted to the outer object. Fields reached through an embedded pointer are optional. Unexported embedded fields are not accepted unless tagged `json:"-"`.
- A json tag name may only use letters, digits, spaces and `` !#$%&()*+-./:;<=>?@[]^_{|}~ ``. The only allowed options are `omitempty`, `omitzero` and `string`. Fields with `omitempty` or `omitzero` are optional. The `string` option is only allowed on bool, predeclared number types or string fields, or on unnamed pointers to them.
- If a type's value or pointer has a method or field named `MarshalJSON`, `MarshalJSONTo`, `AppendText` or `AppendJSON`, it is rejected whatever the signature (except `time.Time` and `json.Number`). Rename it, or return a different type without these methods.
- If a type itself declares a method on a value receiver with exactly the signature `MarshalText() ([]byte, error)`, it is written as a string. If the method is on a pointer receiver, it is written as a string only when held through a pointer. A wrong signature, a method promoted from an embedded field, or a method hidden by a field of the same name is always rejected.
- Interfaces are not accepted anywhere, including fields, elements and map keys. Neither are chan, func, complex numbers, `unsafe.Pointer` or type parameters.
