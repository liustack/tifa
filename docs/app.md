---
summary: 'How to write a tifa app: folder layout, how folders become URLs, route.go and route.tsx, Layouts, hooks.go, 404s, the SSR and cache switches, how Load may use Ctx, method handlers with typed input and result, and the JSON rules for data, inputs and results'
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
| `path++` | the rest of the path `{path...}`, only as the last folder. `c.Param("path")` reads it in Go, and its key in TypeScript `Params` is `_splat` |

- A literal folder name holds only ASCII letters, digits and `- . _ ~`. A parameter name starts with a letter or `_`, followed by letters, digits or `_`, and appears once per URL.
- The go command takes the package name from the `package` line, not from the folder. Name the package after the parameter: `project+` holds `package project`, and other packages import it as `project`.
- The go command skips folders that start with `.` or `_`, and folders named `testdata`. A `route.go`, `route.tsx`, `layout.go` or `layout.tsx` inside one fails the build. Other files there, such as a `_components` folder, are fine.
- A folder name must also be valid in a Go import path, so names such as `con` or `x~1` (Windows reserved and short names) fail the build.
- A folder holds at most one parameter folder. `items/id+` next to `items/key+` or `items/path++` fails the build, because both would read the same URL segment.
- Two routes (pages or API routes) whose URLs overlap without one being more specific fail the build, as in Go's `http.ServeMux`, such as `a/b+` next to `a+/b`. A literal is more specific than a parameter, so `items/new` next to `items/id+` is fine.
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

type ShipInput struct {
	Carrier string `json:"carrier"`
}

func POST(c *tifa.Ctx, in ShipInput) (shop.Shipment, error) {
	return shop.ShipOrder(c.Param("project"), c.Param("order"), in.Carrier)
}
```

- `Load` returns the page data. Its return type must convert to TypeScript (see "JSON rules" below).
- `GET`, `POST`, `PUT`, `PATCH` and `DELETE` handle the HTTP method they are named after.
- A folder with a `route.tsx` is a page. The framework renders its GET, so you cannot also write `GET`. A page that needs no data and no other method can be `route.tsx` alone, without `route.go`.
- A folder with `route.go` and no `route.tsx` is an API route: every method handler answers as written, and `Load` is not allowed, because `Load` loads the data of a page.
- Everything is package-level functions and constants. Use the global variables the app has set up for dependencies. The framework does no dependency injection.

When an error is returned:

| Returned | Result |
| --- | --- |
| `tifa.ErrNotFound` | 404 |
| `c.Redirect(url, code)` | Redirect |
| `tifa.Error{Status, Message}` | Answered with its status code and message, does not go to `OnError`. A status outside 400 to 599 is handled as an unexpected error |
| `tifa.Fields{"carrier": "pick a carrier"}` | 422 with a message per input field by its json name, does not go to `OnError` |
| Any other error, a panic | Goes to `OnError`. Without one, it is logged and answered with 500 |

A method handler that fails answers JSON: `{"error": "message"}`, and for `tifa.Fields` also `"fields": {"carrier": "pick a carrier"}`.

In `Load` you can only set response headers and cookies. Calling `c.JSON`, `c.Text` or `c.NoContent` returns an error.

## Method handlers with input and result

A method handler may take an input after the Ctx and return a result before the error. The framework decodes the input and writes the result:

```go
func POST(c *tifa.Ctx) error                       // writes its own response
func POST(c *tifa.Ctx, in ShipInput) error         // answers 204 on success
func POST(c *tifa.Ctx) (Shipment, error)           // answers 200 with the result as JSON
func POST(c *tifa.Ctx, in ShipInput) (Shipment, error)
```

- The input is a struct, not a pointer to one. POST, PUT and PATCH decode it from the request body: JSON, or a URL-encoded or multipart form. A form fills only fields that are strings, numbers, bools or slices of them, and a form value for any other field is answered with 400. GET and DELETE decode it from the query string, so its fields may only be strings, numbers, bools or slices of them, with no embedded fields and no types with `MarshalText` or `UnmarshalText`.
- Fields are named by their `json` tags, in a form body and the query string too. A `form` tag fails the build. Path parameters, headers and cookies still come from `c`.
- The input and the result follow the JSON rules below, the same as the result of `Load`. The input must also read back from the JSON its TypeScript type describes: a type that is a string because of `MarshalText` needs `UnmarshalText([]byte) error` too, a type with `UnmarshalText` must be a string in TypeScript, and `UnmarshalJSON` is not accepted (except on `time.Time`).
- When the input type has a method `Validate` (it must be `func() error`), it runs after decoding and before the handler. `nil` goes on. A `tifa.Error` or `tifa.Fields` is answered as is, so return `tifa.Fields` to mark single fields. Any other error is answered with 400 and its `err.Error()` as the message, which the user sees.
- A body or query string that does not decode is answered with 400 (413 for a body over the limit, 415 for another content type or none), and the handler does not run.
- A handler that writes its own response decodes input with `c.Bind(&in)` for the body and `c.BindQuery(&in)` for the query string. These name a field by its `form` tag first, then its `json` tag, then its Go name.

## Ctx

`*tifa.Ctx` is the request and its response. `Load`, `Layout`, the method handlers, `OnRequest` and `OnError` get it.

| Method | What it does |
| --- | --- |
| `c.Param(name)` | A path parameter, decoded, such as `c.Param("order")` for the folder `order+` |
| `c.Query(name)` | The first value of a query parameter, `""` when absent |
| `c.Header(name)`, `c.Cookie(name)` | A request header, a request cookie (`http.ErrNoCookie` when absent) |
| `c.Context()`, `c.SetContext(ctx)` | The request context, and replacing it to store request-scoped values |
| `c.Pattern()` | The path of the matched route, such as `/projects/{project}`, `""` when no route matched |
| `c.IsData()` | Whether this is a navigation data request of the browser router rather than a page load |
| `c.SetHeader(key, value)`, `c.SetCookie(cookie)` | A response header, a response cookie. Allowed in `Load` too |
| `c.JSON(code, v)`, `c.Text(code, s)`, `c.NoContent(code)` | Writes the response. Not allowed in `Load` |
| `c.Redirect(url, code)` | Returns an error value. Return it to redirect |
| `c.Bind(&v)`, `c.BindQuery(&v)` | Decodes the body or the query string into a struct, see above |
| `c.SetBodyLimit(n)` | Changes the body limit of this request (`server.body_limit` by default). Call it before anything reads the body, usually in `OnRequest`. Once reading has started it returns an error |
| `c.Next()` | Runs the matched route from `OnRequest`, once |
| `c.Request()`, `c.Writer()` | The raw `*http.Request` and `http.ResponseWriter`, for method handlers and `OnRequest` only |

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

const OrderActions = clientOnly(() => import('./actions.client'), <button disabled>Loading…</button>)
```

- After a write that does not go through a handler function (below), call TanStack's `router.invalidate()`. The data of the current page and all its Layouts is loaded again, merged into one request.

### Calling method handlers from the browser

The `+route` module of a folder with method handlers exports one function per handler, named after its method. API routes get a `+route` module too, so a page can import the handlers of any folder:

```tsx
import { POST } from './+route'
import { TifaError } from 'tifa/client'

async function ship() {
  try {
    const shipment = await POST({ params: { project, order }, input: { carrier: 'SF' } })
  } catch (e) {
    if (e instanceof TifaError) setErrors(e.fields) // e.status, e.message, e.fields
  }
}
```

- The function takes one object. `params` holds the path parameters and is left out when the folder has none. A `name++` folder's key is `_splat`. A value that would send the request to another path is refused before any request and rejects the promise: an empty value, `.` or `..`, and for `_splat` a `.` or `..` segment or an empty one before the end. `input` is the handler's input and is left out when it takes none. `signal` aborts the request.
- GET and DELETE send the input in the query string, the other methods as a JSON body. The result is typed from the Go result. `func(c, in) error` gives `void`. `func(c) error` writes its own response, so its function gives back the `Response` as it is, also for an error status.
- After a POST, PUT, PATCH or DELETE succeeds, the data of the current page and its Layouts is loaded again before the promise resolves. Pass `invalidate: false` to keep it. A failure and a GET never reload.
- An error answer throws `TifaError` with `status`, `message` and `fields` (the messages of a `tifa.Fields`, empty otherwise). A network failure throws as `fetch` does.
- Call these functions from event handlers or effects. Calling one while the page renders on the server throws.
- `+route` also exports `POSTInput`, `POSTOutput` and so on for the types of each handler that has them.

## Layout

A Layout lives in its folder next to the page of that folder, if there is one: `layout.go` and `layout.tsx` wrap the page in the same folder and every page below it. `layout.tsx` places child routes with `<Outlet />`. Either file may be left out: without `layout.tsx` the child routes render directly, and without `layout.go` the Layout has no data. A Layout with no page in its folder or below it fails the build.

```go
package project

func Layout(c *tifa.Ctx) (shop.Project, error) {
	return shop.GetProject(c.Param("project"))
}

const LayoutSSR = false // optional, default true
```

- `layout.go` and `route.go` of one folder are one Go package, but each name has its own file: `Layout` and `LayoutSSR` go in `layout.go`, and `Load`, `SSR`, `Cache` and the method handlers go in `route.go`. Declaring one of them in any other file of the package fails the build. Helpers and types may go in any file.
- A Layout's `c.Param` may only read parameters of its own folder path. `app/projects/project+/layout.go` may read `project`, not `order`. A page's `Load` is held to its own path the same way.
- When you switch between pages under the same Layout, the Layout component is not remounted and keeps its state. When a parameter the Layout function reads changes (for example, you move to another project), the Layout remounts and its state resets. When a parameter it does not read changes, the state is kept. Pages work the same way.
- Pages and Layouts can call `tifa.Use(c, project.Layout)` to get the result of a Layout that wraps them, for the current request: the Layout of the same folder or of an ancestor folder. The same `Layout` runs only once per request. Any other Layout fails the build, and so does an argument that is not a package-level `Layout` function, such as a closure.
- On navigation in the browser, the navigation data request runs only the `Load` and `Layout` functions of the listed routes. If an ancestor Layout's data is still in the cache, that Layout is not in the list and its `Layout` does not run this time. So do not put permission checks only in a Layout. Put them in `OnRequest`, or have the page's `Load` depend on the checking Layout through `tifa.Use`, so the check runs on every request.
- The document shell (`<html>`, `<head>`, assets and the script that injects data) is written as a string by the framework. React only renders the app root inside `<body>`, so the page is still a complete document when any level fails or returns 404. The root Layout (`app/layout.tsx`) can export two constants to customize the shell: `htmlAttributes` sets the attributes of `<html>` (`{ lang: 'en' }` when not exported), and `documentHead` holds head tags that every page carries (also when no route matches or the root `Layout` fails), in the same format as the return value of `head`. Put the site's default `title` here so that the 404 page for an unmatched URL also has a title. A page's own `head` that returns a `title` overrides it. Exporting a `Document` component fails the build.

```tsx
export const htmlAttributes = { lang: 'zh-CN' }
export const documentHead = {
  meta: [{ title: 'Shop' }, { name: 'theme-color', content: '#f7f8f5' }],
  links: [{ rel: 'icon', href: '/favicon.svg' }],
}
```

- A page's head comes from the `head` return values of the matched routes: `title` and meta tags with the same name come from the deepest route, and link, style and script tags are deduplicated in route order. The framework always adds `<meta charset="utf-8">` and a viewport meta, and a route's `name: 'viewport'` meta replaces the latter. Tag attributes are written the way React DOM writes them on the server: `className`, `htmlFor`, `httpEquiv`, `crossOrigin` and so on become their HTML names. Event attributes starting with `on`, invalid attribute names, `null`, `undefined` and functions are not written. Boolean attributes such as `async` and `defer` are written with an empty value when true. For other attributes, boolean values are written as strings only on `data-` and `aria-` attributes. `javascript:` links are replaced. Head tags do not support the `style` attribute, and using it makes rendering fail. `htmlAttributes` follows the same rules. The content of style and script tags is written as is. Only the `<script` and `</script` (in style, `<style` and `</style`) that would end the element are escaped, the way React DOM does it. After browser navigation and after `router.invalidate()`, the head updates to match the new routes.

## 404

`route.tsx` and `layout.tsx` can export `NotFound`:

| Case | What is shown |
| --- | --- |
| The URL matches no page, such as the URL of a folder with a Layout but no page | The root Layout's `NotFound`, without the root Layout's UI, and no `Load` or `Layout` runs |
| A page's `Load` returns `tifa.ErrNotFound` | The first `NotFound` going up from that page. Outer Layouts are shown as usual |
| A `Layout` function returns `tifa.ErrNotFound` | The search starts at the Layout one level up |
| Nothing exports one | The framework's default page |

The status code is 404 in all these cases. The root Layout's `NotFound` is sometimes shown on its own, so it cannot read loader data.

## Errors on a page

`route.tsx` and `layout.tsx` can export `Error`, shown in place of that route when its `Load` or `Layout` fails or a component below it throws:

```tsx
import { RouteError } from 'tifa/client'

export function Error({ error }: { error: unknown }) {
  if (error instanceof RouteError && error.status === 403) return <p>No access</p>
  return <p>Something went wrong</p>
}
```

- A failed `Load` or `Layout` reaches `Error` as a `RouteError` with `status` and the public message (the `tifa.Error` message, or what `OnError` returned). It is the same on the server and after hydration.
- A route without its own `Error` uses the one of the closest ancestor that exports one, then the framework's default.
- The status code of the page is that of the outermost failed route. A component that throws during the server render is an unexpected error: it goes to `OnError`, which picks the status (500 without `OnError`), and that HTML is never cached.
- The document shell stays complete whatever fails, so the page still loads its scripts and styles.

## SSR and cache

```go
const SSR = false       // the page, default true
const Cache = true      // the page, default false
const LayoutSSR = false // the Layout of the folder, default true
```

- `SSR = false`: middleware and every `Load` run as usual, but QuickJS is skipped. The response is a minimal document with the data, and the browser renders it. `LayoutSSR = false` applies to every page the Layout wraps.
- `Cache = true`: the rendered result is cached by build version, URL and data, in a cache of `cache.size` (see [config.md](config.md)). When the data has not changed, the HTML is returned directly. Components must not read values that change on every call, such as `Date.now()` or `Math.random()`, while rendering. Hydration requires this too.
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

This section covers `Load`, `Layout` and the functions they call. At build time, tifa starts from each `Load` and `Layout` function and finds the route parameters (`c.Param`), query parameters (`c.Query`) and Layouts (`tifa.Use`) it reads. When `Load` passes `*tifa.Ctx` to a function in this module, tifa follows the call level by level, across packages too, and counts every parameter it finds toward that `Load`. A helper called by several `Load` functions counts toward each of them. tifa does not follow into its own functions.

So that tifa finds everything, Ctx may only be used where tifa can see it. Pass `c.Param` and `c.Query` a constant string, such as `c.Param("order")`, and call `c.Param`, `c.Query` and `tifa.Use` directly: storing one in a variable or passing it as a value fails the build.

The only types that may hold a Ctx are: `*tifa.Ctx`, `tifa.Ctx`, non-generic structs declared in this module (whose fields hold Ctx only through these types, directly, embedded or indirectly), and pointers to such structs.

A value that holds a Ctx may only appear in these five places:

1. As the receiver of a direct method call, such as `c.Query("tab")` or `req.Page()`.
2. As an argument to a non-generic function from this module or from tifa, where the parameter has exactly the same type. A function literal called in place also counts.
3. In a composite literal of a struct that holds Ctx, for a field of exactly the same type.
4. Assigned with `:=` or `var x T = ...` to a new local variable of exactly the same type. That variable follows the same rules.
5. With its address taken, where the resulting pointer is still a type that may hold Ctx.

Any other use fails the build and names the file and line. For example: converting it to another type or an interface, assigning it to an existing variable, assigning several values at once, `return`ing it, putting it in a slice, map, array or channel, `range`, package-level variables, `defer`, `go` statements, or passing it to a generic function, an interface method, a function variable or a function outside the module (the standard library and third-party libraries). `return c.Redirect(...)` and `return c.Next()` are fine, because they return the result of a method call, not the Ctx itself.

`Load`, `Layout` and the functions they call must not call `c.Request()` or `c.Writer()`. tifa cannot see parameters read through the raw request. Use `c.Param`, `c.Query`, `c.Header`, `c.Cookie` or `c.Context()` instead. `c.Request()` and `c.Writer()` are only for method handlers and `OnRequest`.

When you hit one of these errors, call Ctx methods directly, or pass it to a plain function. If you only need the request context, pass `c.Context()`. A helper that only calls `c.Context()` (such as `auth.User(c)`) collects nothing when tifa follows it.

tifa only checks code that `Load` and `Layout` can reach. If you store a Ctx in a global variable or another shared place from `OnRequest`, a method handler or anywhere else, and then take it out in `Load` or `Layout`, tifa cannot catch it. Do not do this.

## JSON rules

The value `Load` or a `Layout` function returns, and the input and result of a method handler, travel as JSON, and tifa generates TypeScript types from them. So that the output is the same under both `encoding/json` implementations in Go 1.27 (the default one and `GOEXPERIMENT=nojsonv2`), only the types below are accepted.

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
