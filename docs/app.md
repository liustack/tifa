---
summary: 'How to write a tifa app: folder layout, how folders become URLs, route.go and route.tsx, Layouts, hooks.go, 404s, the SSR and cache switches, what the server render has, which libraries render differently there and which components to load only in the browser, using Intl and Temporal so hydration matches, styles with Tailwind CSS and shadcn/ui, how to test Go route code, how Load may use Ctx, method handlers with typed input and result, and the JSON rules for data, inputs and results'
read_when:
  - Creating a tifa app, or adding pages, Layouts or API routes to one
  - The build reports an error about Ctx usage or a Load return type and you want to fix it
  - Changing the rules in internal/analyze and you need to know what users were promised
  - Changing the apps in examples/shop or test/apps
  - Writing Go tests for a Load, a Layout function, a method handler or OnRequest
  - Adding CSS, Tailwind CSS or shadcn/ui to an app
  - Formatting numbers or dates in a page, or tifa dev warns about an Intl call
  - A page fails to render on the server or does not hydrate, and you want to know which browser APIs and library features the server has
  - A component shows nothing until the browser draws it, such as a chart or a map, and you wonder whether to render it on the server
---

English · [简体中文](app.zh-CN.md)

# Writing an app

For a complete example, see [examples/shop](../examples/shop). For the build, the generated files and how to set up go.mod, see [build.md](build.md).

## Folders

```text
myapp/
  go.mod
  hooks.go                          process and request hooks (optional)
  db.go                             resources the whole app shares: package-level variables and functions (optional)
  app/
    layout.go  layout.tsx           root Layout, wraps every page
    route.go                        /
    projects/
      project.go  project_test.go   the project domain: Project, GetProject, ListProjects
      project+/
        layout.go  layout.tsx       Layout of /projects/{project} and every page below it
        route.go  route.tsx         page /projects/{project}
        orders/
          order.go  order_test.go   the order domain: Order, ListOrders, GetOrder, ShipOrder
          order+/
            route.go  route.tsx     page /projects/{project}/orders/{order}
            items.tsx  ship-form.tsx
        stats/
          route.go  route.tsx       page /projects/{project}/stats
```

- Code is grouped by domain, and a domain is a folder under `app/`: its types, storage and rules sit next to its pages, in the list folder when it has a list and a page per item. Pages import the domain packages they show. The root package holds only the hooks and the resources the whole app shares. There is no shared `models/` or `db/` package holding every table. See [structure.md](structure.md).
- Components and styles shared across pages go in the `app/` root.
- Do not write a main. `tifa build` generates main and the route registration code in `.tifa/`, which `go vet ./...` and `go test ./...` do not scan.

## URLs come from folders

Every folder below `app/` is one segment of the URL. `app/` itself is `/`.

| Folder name | URL segment |
| --- | --- |
| `orders` | the literal `orders` |
| `order+` | the parameter `{order}`, read with `c.Param("order")` |
| `path++` | the rest of the path `{path...}`, only as the last folder. It may be empty: `files/path++` also answers `/files`. `c.Param("path")` reads it in Go, and its key in TypeScript `Params` is `_splat` |

- A literal folder name holds only ASCII letters, digits and `- . _ ~`. A parameter name starts with a letter or `_`, followed by letters, digits or `_`, and appears once per URL.
- The go command takes the package name from the `package` line, not from the folder. Name the package after the parameter: `project+` holds `package project`, and other packages import it as `project`.
- The go command skips folders that start with `.` or `_`, and folders named `testdata`. A `route.go`, `route.tsx`, `layout.go` or `layout.tsx` inside one fails the build. Other files there, such as a `_components` folder, are fine.
- A folder name must also be valid in a Go import path, so names such as `con` or `x~1` (Windows reserved and short names) fail the build.
- A folder holds at most one parameter folder. `items/id+` next to `items/key+` or `items/path++` fails the build, because both would read the same URL segment.
- Two routes (pages or API routes) whose URLs overlap without one being more specific fail the build, as in Go's `http.ServeMux`, such as `a/b+` next to `a+/b`. A literal is more specific than a parameter, so `items/new` next to `items/id+` is fine.
- The URL of a folder with a Layout but no page shows the 404 page, unless a `name++` folder right below it has a page, which answers that URL with an empty rest. No other page may match that URL unless it is more specific than the Layout's path: `settings/layout.tsx` without a `settings/route.tsx`, next to `user+/route.tsx`, fails the build, because `/settings` would also match `/{user}`, but `items/new` next to a Layout in `items/id+` is fine.
- Declaring `const Path` fails the build. The folder is the URL.

## route.go

```go
package order

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

- `Load` returns the page data. Its return type must convert to TypeScript (see "JSON rules" below).
- `GET`, `POST`, `PUT`, `PATCH` and `DELETE` handle the HTTP method they are named after.
- A folder with a `route.tsx` is a page. The framework renders its GET, so you cannot also write `GET`. A page that needs no data and no other method can be `route.tsx` alone, without `route.go`.
- A folder with `route.go` and no `route.tsx` is an API route: every method handler answers as written, and `Load` is not allowed, because `Load` loads the data of a page.
- Everything is package-level functions and constants. Use the global variables the app has set up for dependencies. The framework does no dependency injection.

When an error is returned:

| Returned | Result |
| --- | --- |
| `tifa.ErrNotFound` | 404 |
| `c.Redirect(url, code)` | A redirect. Page loads and navigation follow it, and so does a handler function called from the browser (see below) |
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

- The input is a struct, not a pointer to one. POST, PUT and PATCH decode it from the request body: JSON, or a URL-encoded or multipart form. A form fills only fields that are strings, numbers, bools or slices of them, and a form value for any other field is answered with 400. GET and DELETE decode it from the query string, so its fields may only be strings, numbers, bools or slices of them, with no embedded fields and no types with `MarshalText` or `UnmarshalText`, also on the input type itself.
- Fields are named by their `json` tags, in a form body and the query string too. A `form` tag fails the build. Path parameters, headers and cookies still come from `c`.
- The input and the result follow the JSON rules below, the same as the result of `Load`. The input must also read back from the JSON its TypeScript type describes: a type that is a string because of `MarshalText` needs `UnmarshalText([]byte) error` too, a type with `UnmarshalText` must be a string in TypeScript unless it is a map key (a key is always a JSON string), and `UnmarshalJSON` is not accepted (except on `time.Time`).
- When the input type has a method `Validate` (it must be `func() error`), it runs after decoding and before the handler. `nil` goes on. A `tifa.Error` or `tifa.Fields` is answered as is, so return `tifa.Fields` to mark single fields. Any other error is answered with 400 and its `err.Error()` as the message, which the user sees.
- A body or query string that does not decode is answered with 400 (413 for a body over the limit, 415 for another content type or none), and the handler does not run.
- A handler that writes its own response decodes input with `c.Bind(&in)` for the body and `c.BindQuery(&in)` for the query string. These name a field by its `form` tag first, then its `json` tag, then its Go name.

## Ctx

`*tifa.Ctx` is the request and its response. `Load`, `Layout`, the method handlers, `OnRequest` and `OnError` get it.

| Method | What it does |
| --- | --- |
| `c.Param(name)` | A path parameter, decoded, such as `c.Param("order")` for the folder `order+` |
| `c.Query(name)` | The first value of a query parameter, `""` when absent |
| `c.QueryAll(name)` | Every value of a query parameter, such as `["a", "b"]` for `?tag=a&tag=b`, `nil` when absent |
| `c.Header(name)`, `c.Cookie(name)` | A request header, a request cookie (`http.ErrNoCookie` when absent) |
| `c.Context()`, `c.SetContext(ctx)` | The request context, and replacing it to store request-scoped values |
| `c.Pattern()` | The path of the matched route, such as `/projects/{project}`, `""` when no route matched |
| `c.IsData()` | Whether this is a navigation data request of the browser router rather than a page load |
| `c.SetHeader(key, value)`, `c.SetCookie(cookie)` | A response header, a response cookie. Allowed in `Load` too |
| `c.JSON(code, v)`, `c.Text(code, s)`, `c.NoContent(code)` | Writes the response. Not allowed in `Load` |
| `c.Redirect(url, code)` | Returns an error value. Return it to redirect |
| `c.Bind(&v)`, `c.BindQuery(&v)` | Decodes the body or the query string into a struct, see above. Not allowed in `Load` |
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
- In the browser, each `route.tsx`, and each `layout.tsx` but the root Layout's, is a chunk of its own with everything it imports, `head` included. It loads the first time its route is needed, and the router waits for it before it shows the page or runs `head`. The HTML of a page preloads the chunks of its Layouts and route, so they arrive with the entry script. Code at the top level of the file, side-effect imports included, runs then and not when the app starts, so put setup that every page needs in the root Layout or a module it imports. See [Route modules in the browser](build.md#route-modules-in-the-browser).
- Go slices and maps carry `| null` in TypeScript (Go's nil is encoded as `null`). Write `?? []` before using them.
- Put components that need browser features at the module top level into a `.client.tsx` file, and load them lazily with `clientOnly` from `tifa/client`. They are left out of the SSR bundle, and the first paint shows a placeholder. Components that render but show nothing until the browser draws them belong there too, see [Components that draw only in the browser](#components-that-draw-only-in-the-browser):

```tsx
import { clientOnly } from 'tifa/client'

const OrderActions = clientOnly(() => import('./actions.client'), <button disabled>Loading…</button>)
```

- After a write that does not go through a handler function (below), call TanStack's `router.invalidate()`. The data of the current page and all its Layouts is loaded again, merged into one request, and the promise it returns resolves once the page shows the new data.

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

- The function takes one object. `params` holds the path parameters and is left out when the folder has none. A `name++` folder's key is `_splat`. A value that would send the request to another path is refused before any request and rejects the promise: an empty value, `.` or `..` for a `name+` parameter, and a `.` or `..` segment or an empty segment before the end for `_splat`. An empty `_splat` is fine. `input` is the handler's input and is left out when it takes none. `signal` aborts the request.
- GET and DELETE send the input in the query string, the other methods as a JSON body. The result is typed from the Go result, with `| undefined` for a redirect (below): `func(c, in) (Out, error)` gives `Promise<POSTOutput | undefined>`. `func(c, in) error` gives `void`. `func(c) error` writes its own response, so its function gives back the `Response` as it is, also for an error status.
- After a POST, PUT, PATCH or DELETE succeeds, the data of the current page and its Layouts is loaded again before the promise resolves. Pass `invalidate: false` to keep it. A failure and a GET never reload.
- An error answer throws `TifaError` with `status`, `message` and `fields` (the messages of a `tifa.Fields`, empty otherwise). A network failure throws as `fetch` does.
- When the handler or `OnRequest` returns `c.Redirect(url, code)`, the browser goes to that URL: through the router for a URL of this site, without reloading the page, and as a whole page load for another site. After a POST, PUT, PATCH or DELETE (unless `invalidate: false`) the data the router holds goes stale first, so the new page and its Layouts show fresh data. A redirect to the page that is showing loads its data again, as a call without a redirect does. It counts as the same page when the path, the hash and the query values match, whatever the order and encoding of the query. The promise resolves once the new page shows, to `undefined`, also for a handler with a result. Whether a call is redirected is only known from its answer, since `OnRequest` may redirect any request, so the result type of every handler function with a result includes `undefined`. The function of a handler that writes its own response gets the `Response` of the redirect (`ok`, with the status in `X-Tifa-Redirect`). A relative URL is resolved against the URL of the handler, not of the page. A 3xx the handler wrote itself through `c.Writer()`, or one a proxy wrote, cannot be read in the browser and rejects the promise with an `Error`.
- Call these functions from event handlers or effects. Calling one while the page renders on the server throws.
- `+route` also exports `POSTInput`, `POSTOutput` and so on for the types of each handler that has them.

To go to the page of what a handler created, return a redirect from the handler, or return the result and navigate yourself. Pass `invalidate: false` in the second case, so the current page does not load its data again just before you leave it. Data the router already holds, such as that of a Layout both pages share, then stays as it was:

```go
func POST(c *tifa.Ctx, in AddInput) error {
	todo, err := AddTodo(in.Title)
	if err != nil {
		return err
	}
	return c.Redirect("/todos/"+todo.ID, http.StatusSeeOther)
}
```

```tsx
const router = useRouter() // from @tanstack/react-router
// in the event handler, where POST returns (Todo, error):
const todo = await POST({ input: { title }, invalidate: false })
if (todo) await router.navigate({ to: '/todos/$todo', params: { todo: todo.id } }) // undefined: redirected
```

## Layout

A Layout lives in its folder next to the page of that folder, if there is one: `layout.go` and `layout.tsx` wrap the page in the same folder and every page below it. `layout.tsx` places child routes with `<Outlet />`. Either file may be left out: without `layout.tsx` the child routes render directly, and without `layout.go` the Layout has no data. A Layout with no page in its folder or below it fails the build.

```go
package project

func Layout(c *tifa.Ctx) (projects.Project, error) {
	return projects.GetProject(c.Param("project"))
}

const LayoutSSR = false // optional, default true
```

- `layout.go` and `route.go` of one folder are one Go package, but each name has its own file: `Layout` and `LayoutSSR` go in `layout.go`, and `Load`, `SSR`, `Cache` and the method handlers go in `route.go`. Declaring one of them in any other file of the package fails the build. Helpers and types may go in any file.
- A Layout's `c.Param` may only read parameters of its own folder path. `app/projects/project+/layout.go` may read `project`, not `order`. A page's `Load` is held to its own path the same way.
- When you switch between pages under the same Layout, the Layout component is not remounted and keeps its state. When a parameter the Layout function reads changes (for example, you move to another project), the Layout remounts and its state resets. When a parameter it does not read changes, the state is kept. Pages work the same way.
- Pages and Layouts can call `tifa.Use(c, project.Layout)` to get the result of a Layout that wraps them, for the current request: the Layout of the same folder or of an ancestor folder. The same `Layout` runs only once per request. Any other Layout fails the build, and so does an argument that is not a package-level `Layout` function, such as a closure. A `Layout` function cannot use its own Layout, directly or through a helper.
- On navigation in the browser, the navigation data request runs only the `Load` and `Layout` functions of the listed routes. If an ancestor Layout's data is still in the cache, that Layout is not in the list and its `Layout` does not run this time. So do not put permission checks only in a Layout. Put them in `OnRequest`, or have the page's `Load` depend on the checking Layout through `tifa.Use`, so the check runs on every request.
- The document shell (`<html>`, `<head>`, assets and the script that injects data) is written as a string by the framework. React only renders the app root inside `<body>`, so the page is still a complete document when any level fails or returns 404. The root Layout (`app/layout.tsx`) can export two constants to customize the shell: `htmlAttributes` sets the attributes of `<html>` every page starts from (`{ lang: 'en' }` when not exported, and a route's `head` can change them for its pages, see below), and `documentHead` holds head tags that every page carries (also when no route matches or the root `Layout` fails), in the same format as the return value of `head`. Put the site's default `title` here so that the 404 page for an unmatched URL also has a title. A page's own `head` that returns a `title` overrides it. Exporting a `Document` component fails the build.

```tsx
export const htmlAttributes = { lang: 'zh-CN' }
export const documentHead = {
  meta: [{ title: 'Shop' }, { name: 'theme-color', content: '#f7f8f5' }],
  links: [{ rel: 'icon', href: '/favicon.svg' }],
}
```

- A page's head comes from the `head` return values of the matched routes: `title` and meta tags with the same name come from the deepest route, and link, style and script tags are deduplicated in route order. The framework always adds `<meta charset="utf-8">` and a viewport meta, and a route's `name: 'viewport'` meta replaces the latter. Tag attributes are written the way React DOM writes them on the server: `className`, `htmlFor`, `httpEquiv`, `crossOrigin` and so on become their HTML names. Event attributes starting with `on`, invalid attribute names, `null`, `undefined` and functions are not written. Boolean attributes such as `async` and `defer` are written with an empty value when true. For other attributes, boolean values are written as strings only on `data-` and `aria-` attributes. `javascript:` links are replaced. Head tags do not support the `style` attribute, and using it makes rendering fail. `htmlAttributes` follows the same rules. The content of style and script tags is written as is. Only the `<script` and `</script` (in style, `<style` and `</style`) that would end the element are escaped, the way React DOM does it. After browser navigation and after `router.invalidate()`, the head updates to match the new routes.
- A `head` can also return `htmlAttributes`, the attributes of `<html>` for its pages, such as the language of a site in several languages. Search engines, screen readers and the translation of browsers read the language from `lang`. They are merged over the root Layout's `htmlAttributes` one attribute at a time: the deepest route that gives an attribute wins, `undefined` leaves it to the routes above, and `null` takes it away. Attributes are matched by the name React DOM writes, so `className` in one route and `class` in another are the same attribute. `class` is replaced as a whole, never merged with a value from elsewhere. `style` is not supported, as on the tags of the head. They are written by the same rules as the tags of the head. After browser navigation and after `router.invalidate()` they follow the new routes too. In the browser tifa writes an attribute of `<html>` only when the value it wants for it changes, and takes away only attributes it wrote. So what a script of the page sets, such as the `dark` class of a dark mode script or the `lang` a translation tool changes, stays through hydration and navigation while the routes want the same value of that attribute. When that value changes, because a route sets another `class` or a navigation leaves it and the root Layout's `class` comes back, tifa writes it and the class the script added is gone. Keep such a theme in an attribute no route sets, such as `data-theme`, or have the script add it again. A page without SSR gets these attributes in the browser.

```tsx
// app/guide/lang+/layout.tsx. layout.go checks the language of the URL and returns it,
// or tifa.ErrNotFound for a language the site does not have.
export const head = ({ loaderData }: { loaderData?: Data }) => ({
  htmlAttributes: { lang: loaderData?.lang },
})
```

- The routes that count are those whose head is on the page. A 404 or an error below the Layout keeps the Layout's language. When the Layout itself fails, its `head` runs without data, so the example above leaves the root Layout's `lang`. A URL no route matches shows the root Layout's `NotFound` alone, with the root Layout's attributes. To answer such URLs in the language of the URL, add a page that takes the rest of the path, such as `app/guide/lang+/rest++/route.go`, whose `Load` returns `tifa.ErrNotFound`: the 404 then shows inside the Layout.
- `htmlAttributes` must be an object, and `pnpm typecheck` reports any other value. At run time such a value fails like an error the `head` throws: the server render fails as an unexpected error (`OnError`, or a 500), with the message in the log, and on the page under `tifa dev`. In the browser, after a navigation or on a page without SSR, the router writes the error to the console and drops the whole head of that route.
- Take the language from the Layout's data, as above, rather than from `params`: the Go function checks it, so whatever someone types into the URL does not end up as the language of the page.

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

`route.tsx` and `layout.tsx` can export `Error`, shown in place of that route when its `Load` or `Layout` fails, or when a component below it throws in the browser:

```tsx
import { RouteError } from 'tifa/client'

export function Error({ error }: { error: unknown }) {
  if (error instanceof RouteError && error.status === 403) return <p>No access</p>
  return <p>Something went wrong</p>
}
```

- A failed `Load` or `Layout` reaches `Error` as a `RouteError` with `status` and the public message (the `tifa.Error` message, or what `OnError` returned). It is the same on the server and after hydration.
- A route without its own `Error` uses the one of the closest ancestor that exports one, then the framework's default.
- The status code of the page is that of the outermost failed route.
- A component that throws while the page renders on the server fails the whole render. It is an unexpected error: `OnError` sees it and picks the status (500 without `OnError`), and the browser gets a plain error page from Go with that status and the public message, not your `Error` component. That answer is never cached. Under `tifa dev` the page also shows the error with its stack trace in your source files, and reloads once you save a fix (see [tifa dev](dev.md#when-a-page-fails-to-render)).
- A server render that uses up `ssr.vm_memory` or runs past `ssr.timeout` (see [config.md](config.md)) stops right there. No `catch`, `finally` or promise handler in your code or in a library runs for it, so a page cannot catch it to show something else. The render fails as above, and the VM is replaced with a fresh one. Code that recurses without end gets `RangeError: Maximum call stack size exceeded`, as in Node, which a `catch` can take (see [stack depth](config.md#stack-depth)). When nothing catches it, it fails the render like any other error.
- Once the browser has taken over, a component that throws shows the nearest `Error`, the same as a failed `Load`.
- The document shell stays complete whatever fails, so the page still loads its scripts and styles.

## SSR and cache

```go
const SSR = false       // the page, default true
const Cache = true      // the page, default false
const LayoutSSR = false // the Layout of the folder, default true
```

- `SSR = false`: middleware and every `Load` run as usual, but QuickJS is skipped. The response is a minimal document with the data, and the browser renders it. `LayoutSSR = false` applies to every page the Layout wraps.
- `Cache = true`: the rendered result is cached by build version, URL and data, in a cache of `cache.size` (see [config.md](config.md)). When the data has not changed, the HTML is returned directly. Components must not read values that change on every call, such as `Date.now()` or `Math.random()`, while rendering. Hydration requires this too.
- When requests for a `Cache = true` page miss the cache at the same time with the same build version, URL and data, such as a burst of visitors right after a deploy, they share one render. Each request still runs its middleware and `Load`s, keeps the headers they set, such as `Set-Cookie` and `Vary`, and gets the encoding it accepts. This also holds with `cache.size` 0 and for a page too large for the cache, and under `tifa dev`. Requests with other data render on their own, and pages without `Cache` render for every request, since their HTML may differ by user.
- When that shared render fails, every request that waited for it gets the same error and goes to `OnError`, a panic included. Nothing is cached, so the next request renders again. A request whose client goes away stops waiting at once and its connection is cut, with no answer sent: `c.Next()` in `OnRequest` returns an error that matches `context.Canceled`, but `OnError` is not called and nothing is logged. The render goes on for the others. A request whose own deadline passes while it waits fails like any other error. Once no request waits, a render still waiting for a VM is dropped, and one that already runs finishes and goes into the cache. If it fails then, no request reports it to `OnError`, so the error is logged.
- Every page response carries an ETag. A second request with `If-None-Match` gets 304 when the data has not changed. This has nothing to do with `Cache`.

## What the server render has

Pages render on the server in a QuickJS VM, not in Node or a browser. The VM has the JavaScript language itself, `Proxy`, `WeakRef`, `FinalizationRegistry` and `new Function` included, and these:

- `URL`, `URLSearchParams`, `TextEncoder`, `TextDecoder`, `AbortController`, `Headers`, `Response` and `queueMicrotask`. `TextDecoder` knows UTF-8 only: another encoding, such as `new TextDecoder('latin1')`, throws a `RangeError`.
- `atob` and `btoa` for Base64 binary strings, and `DOMException` for Web API errors. `btoa` rejects characters above U+00FF.
- `crypto.getRandomValues` fills integer typed arrays in place, up to 65,536 bytes per call. Views on shared or resizable buffers throw `TypeError` before element type or quota checks. `crypto.randomUUID` makes version 4 UUIDs. Both read fresh bytes from Go's `crypto/rand`, including during module loading. There is no random byte pool. This host operation can read the OS entropy source. It does not expose file or network access to application code. Keep random values out of hydrated HTML, just as with `Math.random`.
- `structuredClone` preserves cycles and shared references in primitives, objects, arrays, Date, RegExp, Map, Set, errors (including cause and AggregateError), ArrayBuffer, typed arrays, DataView and boxed primitives. Buffers are copied, including resizable buffers. Views on resizable ArrayBuffers throw `DataCloneError`, whether their length is fixed or tracks the buffer. Functions, symbols and weak collections throw `DataCloneError`. Promise, FinalizationRegistry, native iterators and unsupported host objects are rejected by their prototype chain. Removing or replacing that chain can make these values look like ordinary objects, so only their enumerable own properties are copied. Unlike supported built-ins identified by intrinsic brand checks, these types cannot always be recognized in pure JS. Unlike browsers, transparent proxies of ordinary objects or arrays are cloned through their traps as ordinary objects or arrays. Objects that only imitate a supported built-in prototype are rejected. SharedArrayBuffer and nonempty transfer lists are unsupported and throw `DataCloneError`. An empty transfer list is allowed. Custom prototypes, accessors and property attributes are not preserved, and only enumerable own string keys of ordinary objects and arrays are copied.
- `Intl` and the `toLocale*` methods, see [Intl in server rendering](#intl-in-server-rendering).
- `Temporal` and `Date.prototype.toTemporalInstant`, see [Temporal in server rendering](#temporal-in-server-rendering).
- `console` with `log`, `info`, `debug`, `warn`, `error` and `assert`. The first four print nothing. `error` and a failed `assert` throw, so the render fails with their message. Other methods, such as `console.time` or `console.table`, are missing, and calling one throws a `TypeError`.
- `Date.now()` and `Math.random()`. They work, but their values must not reach the HTML, see [SSR and cache](#ssr-and-cache).

It does not have:

- Timers: `setTimeout`, `setInterval`, `requestAnimationFrame`, `requestIdleCallback`. A render ends when its microtasks run out, nothing waits for time.
- `performance` and `crypto.subtle`.
- The browser: `window`, `document`, `navigator`, `self`, `location`, `localStorage`, `matchMedia`, `ResizeObserver`, `Event`, `Blob`, `FormData` and the like.
- I/O: `fetch`, `XMLHttpRequest`, `WebSocket`, `require`, `process`, `Buffer`. The build replaces `process.env.NODE_ENV` in the bundle, so code that reads only that works.

React does not run effects on the server, so code that uses these in `useEffect`, `useLayoutEffect` or event handlers is fine. Code that uses them while rendering fails the render, and code that uses them at the top level of a module stops the VM from loading. Check with `typeof` first, use them in an effect, or move the component into a `.client.tsx` file.

### Top-level await and strict mode

The SSR bundle is one ES module. Each VM runs it once when it loads, and a module in it, of the app or of an npm package, may use top-level `await`: the VM waits for it before its first render. The VM has no timers and does no I/O, so such an `await` must settle with promises alone, as `await import('./mod')` of a module in the bundle does. One that waits for a timer or a request never settles, and the server does not start: the VM fails to load with the error that the module's top-level await is still pending.

As a module, the whole bundle runs in strict mode, as the browser bundle does. A package that works only outside strict mode fails on the server too. Syntax that only sloppy mode allows, such as a `with` statement, two parameters with the same name or an assignment to `eval`, fails `tifa build` with the place in the source file: the build compiles the SSR bundle as the server does, without running it. Under `tifa dev` the same error shows when the app loads the rebuilt bundle. What strict mode stops only as the code runs throws there: assigning to a variable that was never declared throws a `ReferenceError`, and reading `arguments.callee` a `TypeError`.

### What one render leaves for the next

A VM is loaded once and then renders one page after another, one at a time. What a module keeps at its top level stays between renders: variables, caches, closures and the state of libraries, such as a store created outside a component. The next render in that VM sees it, whichever user it is for. Each of the `ssr.vms` VMs has its own copy, and a VM that is replaced (after an error, a timeout, running out of memory or growing too much) starts again from a fresh load. A Node server keeps module state for the life of the process in the same way.

So keep data of a request or a user out of module scope: create stores, query clients and the like inside a component or a provider, for example with `useState(() => createStore())`. A module-level cache is fine when what it holds is the same for every request, such as a formatter or a parsed constant.

### Libraries that render differently on the server

Some libraries do part of their work in effects, so the server renders less than the browser shows later. The page still hydrates without errors when it is written like this:

- react-spring: do not pass `from` to `useSpring`, and give `useTransition` `initial: null`. The first render, on the server and in the browser, then shows the final value, and only later changes animate.
- react-hook-form: write the fields with `Controller`. The server writes the value of a controlled field into the HTML, a field from `register` gets its value only after hydration. Keep the submit button disabled until the page has hydrated, for example with `useSyncExternalStore(() => () => {}, () => true, () => false)`, so that Enter before hydration does not send the form as a plain HTML form.
- Radix Select (the shadcn Select): the label of the chosen item moves into the trigger only after hydration. When the field has a value, pass the label as the children of `SelectValue`, or the server renders an empty button. Without a value the placeholder renders as usual.
- recharts 3 renders only its empty wrapper on the server and draws after hydration. Give the chart box a fixed height so the page does not jump, and show the key numbers as text too. To keep the chart out of the server render altogether, see [Components that draw only in the browser](#components-that-draw-only-in-the-browser).

### Components that draw only in the browser

Some components show nothing on the server: they measure their box, draw on a canvas, or wait for an effect before they render their content. The server writes an empty box or a loading state, and the browser replaces it after hydration. Such a component still costs its full price on the server: its code is in the SSR bundle, every VM loads it, and every render of the page runs it.

Load a component like that with `clientOnly`, with a placeholder of the same size. To tell whether a component qualifies, look at the HTML the server writes for it (view the page source). If it is an empty box, a loading state or markup the browser throws away, the server gains nothing from rendering it. Text that people should see before the scripts run, or that search engines should read, such as the key numbers behind a chart, belongs in the server render: write it next to the component as plain text.

```tsx
import { clientOnly } from 'tifa/client'
import { route } from './+route'

// sales-chart.client.tsx default-exports the chart.
const SalesChart = clientOnly(() => import('./sales-chart.client'), <div className="h-64 w-full" />)

export default function StatsPage() {
  const { days } = route.useLoaderData()
  return <SalesChart days={days ?? []} />
}
```

- The `.client.tsx` file default-exports the component, and props pass through as usual.
- The server and the hydration both render the placeholder, so the page hydrates without errors. Then the browser loads the module and shows the component. Give the placeholder the size the component will take, so the page does not move.
- Nothing the `.client.tsx` file imports reaches the SSR bundle. In the browser it is a chunk of its own, fetched the first time the component renders, also when the page itself is part of the entry script (the root Layout is).
- The component shows a moment after the rest of the page, since its code is fetched after hydration. If the fetch fails, the component throws and the nearest `Error` shows.

What this saved on the two charts of the example shop's stats page, with the two builds measured in turns on one machine: the SSR bundle went from 1.15 MB to 0.77 MB (a third less), compiling it when the program starts took 40% less time, loading each VM 24% less, each VM held 3.8 MB (a third) less memory after rendering, and the stats page rendered 21% faster. The browser entry script went from 1.08 MB to 0.70 MB, and the 0.38 MB of chart code is fetched only when a stats page opens.

## Intl in server rendering

Pages can use `Intl` while they render on the server, in every locale Chromium has. `Intl.DateTimeFormat.format()` and `formatToParts()` without a date read the same render clock as Date and Temporal.Now, including a clock supplied with `tifa.WithRenderClock`. All of it works as in Chromium:

- `Intl.NumberFormat`, `Intl.DateTimeFormat` with `formatRange`, `Intl.PluralRules`, `Intl.Collator`, `Intl.Segmenter`, `Intl.RelativeTimeFormat`, `Intl.ListFormat`, `Intl.DisplayNames`, `Intl.DurationFormat`, `Intl.Locale`, `Intl.supportedValuesOf`, `Intl.getCanonicalLocales` and the `supportedLocalesOf` of each constructor.
- `toLocaleString`, `toLocaleDateString` and `toLocaleTimeString` of `Date`, `toLocaleString` of `Number` and `BigInt`, `localeCompare`, which sorts by the locale's rules, and `toLocaleUpperCase` and `toLocaleLowerCase`.
- Every calendar Chromium has, asked for with `-u-ca-` or the `calendar` option.

Without a locale the server formats in `en-US`, without a `timeZone` in `UTC`, whatever the language and zone of the machine. The local time of `Date` on the server is always UTC too: `getTimezoneOffset()` is 0, and `new Date(2026, 0, 1)`, `getHours` and `toString` work in UTC. The Go code of the app is not affected: the process keeps the zone it was started with, so `time.Local`, the log and SQLite use that zone as in any Go program.

Time zones come from tz 2026e, built into the program: the machine's zoneinfo is never read. The output is compared with Chromium 156 on about 75,000 expressions. Numbers, dates and plurals match in full. The rest differs in a few places: `Intl.Segmenter` breaks words in some languages differently, `Intl.DisplayNames` has more names than Chromium, and Winnipeg is on permanent daylight time, which tz 2026e has and Chromium 156 does not yet. Error messages are worded differently. Firefox and Safari ship their own copy of the data. Recent versions are close, but Safari before macOS 26.4 has older data (CLDR 46) and may differ in a few formats.

### Formatting so hydration matches

The same call gives the same text on the server and in the browser only with the same locale and time zone. A browser fills in what a call leaves out with the language and zone of its user, the server with `en-US` and `UTC`, and then React reports a hydration mismatch. So:

- Pass the locale and, for dates, the `timeZone` to every call, from the data of `Load` or a Layout. Where they come from is up to the app: a setting the user saved, a cookie in which the browser stored `Intl.DateTimeFormat().resolvedOptions().timeZone`, or `c.Header("Accept-Language")`. Check a value that comes from the request before it reaches the page: an unknown time zone throws a `RangeError` and the render fails.
- Do not render with the local time methods of `Date` (`getHours`, `getDate`, `toString`, `new Date(2026, 0, 1)`): they give UTC on the server and the user's zone in the browser. Use the UTC methods, or Intl with a `timeZone`.
- Text that depends on the current time, such as "3 minutes ago", differs between the render and hydration whatever you pass. Compute it in `Load`, or render it in a `useEffect`.

```go
type Receipt struct {
	Amount   float64 `json:"amount"`
	PaidAt   string  `json:"paidAt"`
	Locale   string  `json:"locale"`
	TimeZone string  `json:"timeZone"`
}
```

```tsx
import { route } from './+route'

export default function ReceiptPage() {
  const { amount, paidAt, locale, timeZone } = route.useLoaderData()
  return (
    <p>
      {new Intl.NumberFormat(locale, { style: 'currency', currency: 'CNY' }).format(amount)}
      {new Date(paidAt).toLocaleDateString(locale, { timeZone })}
    </p>
  )
}
```

With `locale: 'zh-CN'`, `timeZone: 'Asia/Shanghai'` and `paidAt: '2025-08-16T20:05:09Z'` this renders `¥1,234.50` and `2025/8/17` on the server and in the browser.

### Warnings in tifa dev

Under [`tifa dev`](dev.md), a render that calls an Intl constructor or a `toLocale*` method without a locale, or a date one without a `timeZone`, logs a warning with the place of the call in your sources and its stack:

```text
2026/10/16 08:00:00 tifa: ssr: Number.prototype.toLocaleString without a locale at app/orders/order+/items.tsx:12:7. The server formats in en-US and UTC, a browser in the language and time zone of its user, so the text can differ from the HTML and React reports a hydration mismatch. Pass the locale and, for a date, the timeZone, from data Load returns, as in toLocaleDateString('zh-CN', {timeZone: 'Asia/Shanghai'}), or format in a useEffect.
    at Price (app/orders/order+/items.tsx:12:7)
    at ...
```

Each place is logged once, and again after tifa dev reloads the SSR bundle. The warning does not change the render. A built program does not check. A call inside a package names the package's file first, and your code further down the stack.

### Cost

A VM carries only stand-ins for Intl, about 10 KB, until one of its renders uses it, which adds about 30 to 45 KB to that VM. A VM of the example shop takes about 11 MB, so that is under 1% of it. Under tifa dev every VM pays this at once, because the warnings need Intl installed. Each language is read into memory once per process, 100 to 180 KB, plus about 1 MB shared by all languages the first time any is used. The locale data makes the program about 1.7 MB larger, and the time zone database the generated `main.go` embeds about 450 KB more. The locale data comes with a license notice to ship with the program, see [Notices to ship with the program](build.md#notices-to-ship-with-the-program).

## Temporal in server rendering

Pages can use `Temporal.Now`, `Instant`, `ZonedDateTime`, `PlainDate`, `PlainTime`, `PlainDateTime`, `PlainYearMonth`, `PlainMonthDay`, `Duration` and `Date.prototype.toTemporalInstant`, including at module load. QuickJS provides the methods V8 ships, IANA time zones and the calendars supported by Intl. Temporal uses the same embedded tz 2026e data as Intl, without reading the machine's zoneinfo.

`Date.now()`, `new Date()`, `Date()`, `Temporal.Now` and Intl date formatting without a date share the VM clock. By default it stays live during a render. Go can supply a clock for one render with `tifa.WithRenderClock`, see [Render clock](#render-clock). `Temporal.Now.instant().epochMilliseconds` and `Date.now()` sample that clock separately, so they can differ if time passes between calls. `Temporal.Now.timeZoneId()` is `UTC`, the default zone of Intl, and the other Now methods use UTC when no zone is passed. The process's Go time zone stays unchanged.

`toLocaleString` uses Intl. Pass an explicit locale. A plain date or time has no zone and ignores `timeZone`, an `Instant` uses the formatter's zone, and a `ZonedDateTime` uses its own zone. Use an explicit zone when constructing a zoned value or formatting an instant. ISO `PlainYearMonth` and `PlainMonthDay` need `calendar: 'iso8601'` when formatting.

For hydration, return ISO strings and the locale and zone from `Load` or a Layout, then reconstruct the same Temporal values on both sides. For example, with `day: '2026-10-22'` and `locale: 'en-US'`:

```tsx
const { day, locale } = route.useLoaderData()
return <time>{Temporal.PlainDate.from(day).toLocaleString(locale, { dateStyle: 'long' })}</time>
```

This renders `October 22, 2026`. Do not put `Temporal.Now` results into hydrated HTML: the browser runs later and its default zone can differ. Compute a fixed value in `Load`, or show the current time after hydration in an effect. Browser locale data can differ, as described under Intl. tifa provides Temporal only to the server VM, not a browser polyfill or TypeScript declarations. Browsers that lack Temporal need a polyfill loaded before the page code, or the page must avoid it there.

Temporal is installed lazily per VM on the first read of `Temporal` or call to `toTemporalInstant`. Pages that do not use it keep only the stand-ins. The bytecode used to install it is compiled once per process. See [Memory](performance.md#memory) for measured size, VM memory and first-use costs. The existing [six notice files](build.md#notices-to-ship-with-the-program) cover Temporal too.

## Styles

Import CSS from a Layout or a page, such as `import './site.css'` in `app/layout.tsx`. Vite builds it: every page links one stylesheet with all the CSS of the app, and fonts and images that CSS references get a content hash in their names under `/assets/` (small ones go inline). A URL that starts with `/`, such as `/logo.svg`, is left as it is, so it can name a file of the public folder. Under `tifa dev` the CSS goes through Vite's dev server and updates in place, see [dev.md](dev.md).

### Tailwind CSS

Use Tailwind CSS v4 through its Vite plugin, the same as in any Vite app. Install it:

```sh
pnpm add -D tailwindcss @tailwindcss/vite
```

add it to the plugins in `vite.config.ts`:

```ts
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import tifa from './.tifa/vite.js'

export default defineConfig({
  plugins: [tifa(), react(), tailwindcss()],
})
```

and import Tailwind in a stylesheet that a Layout imports:

```css
/* app/site.css */
@import "tailwindcss";
```

Tailwind finds class names by scanning the app root and skips the files `.gitignore` lists, which in a new app include `.tifa/` and `bin/`. Add other folders with `@source`. See `test/apps/tailwind` for a working app.

### shadcn/ui

With Tailwind set up as above, the shadcn CLI recognizes the app as a Vite app:

```sh
pnpm dlx shadcn@latest init
pnpm dlx shadcn@latest add button card
```

`init` installs the packages the components need (`radix-ui`, `class-variance-authority`, `lucide-react` and others), writes `components.json`, `lib/utils.ts` and the theme into the stylesheet that imports Tailwind. `add button` writes `components/ui/button.tsx` in the app root, and page code imports it from `@/components/ui/button`. Both work before the first build.

`@/` is the app root, set by `paths` in the app's tsconfig (see [build.md](build.md#the-apps-tsconfig-and-vite-config)). Components render on the server like any other React code.

## hooks.go

`hooks.go` in the project root. All four functions are optional:

```go
func OnStart() error                            // before listening: open the database, logs, etc.
func OnShutdown(ctx context.Context) error      // after an exit signal, once in-flight requests finish
func OnRequest(c *tifa.Ctx) error              // the only middleware, call c.Next() to continue
func OnError(c *tifa.Ctx, err error) tifa.Error // unexpected errors: report, return an error users may see
```

- `OnRequest` runs for pages, navigation data requests, API routes and unmatched URLs. It does not run for static files. For an unmatched URL, `c.Pattern()` is empty.
- Resources the whole app shares, such as a database connection or a logger, go in files at the app root next to `hooks.go`, for example `db.go` or `middleware.go`. Each one builds a package-level variable or a function: set it up in `OnStart`, release it in `OnShutdown`, and use it directly anywhere, for example `myapp.DB` after importing `example.com/myapp`. Domains under `app/` import the root package for this. By convention the root package imports nothing under `app/`, so this never closes an import cycle. Models and queries stay in their domains. Route code uses these resources only inside functions, never while package-level variables are initialized. Do not use `init()` for setup. See [which way imports go](structure.md#which-way-imports-go).
- Store request-scoped values with the standard context: `c.SetContext(context.WithValue(...))`. Business packages provide typed getters.

## Testing

`Load`, `Layout`, the method handlers and `OnRequest` are plain Go functions, so test them with `go test` next to the code. `tifatest.NewCtx(t, r)` turns an `httptest` request into the `*tifa.Ctx` the function takes, and returns the recorder that holds what the function writes:

```go
// app/projects/project+/orders/order+/route_test.go
func TestLoad(t *testing.T) {
	r := httptest.NewRequest("GET", "/projects/studio/orders/SH-1041", nil)
	r.Pattern = "/projects/{project}/orders/{order}"
	r.SetPathValue("project", "studio")
	r.SetPathValue("order", "SH-1041")
	c, _ := tifatest.NewCtx(t, r)
	o, err := tifa.Use(c, Load)
	// check o and err
}
```

This tests the function, not the HTTP answer. Check what the function returns: the value and the error. Only the server decodes the input, runs `Validate`, turns a result into JSON or 204 and an error into 404, 422, a redirect or a call to `OnError`, and answers navigation data requests. A returned value or error is not written into the recorder: when the function writes nothing itself, the recorder stays an empty 200. To check the HTTP answer, send requests to the built app.

- Path parameters are what `c.Param` reads: set the decoded values with `r.SetPathValue`. `c.Pattern()` is `r.Pattern` as it is when `NewCtx` is called.
- Run `Load` and `Layout` through `tifa.Use(c, Load)`. It runs them under the rules of the server: once per request, and writing the response body fails. A `tifa.Use` of a Layout inside `Load` runs that Layout for the same request. Only the Layouts the function asks for with `tifa.Use` run.
- Call a method handler directly: `POST(c)`, or `POST(c, in)` with a typed input. The input is the value you pass, so test `Validate` on its own, and if `Validate` changes the input, pass the changed input.
- For `OnRequest`, `tifatest.SetNext(c, next)` sets what `c.Next()` calls: `next` runs once, as it is, and its error is what `c.Next()` returns. It stands in for the route, so it neither recovers a panic nor writes a result.
- The body limit is the default `server.body_limit`, 1 MiB, unless `c.SetBodyLimit` changes it. The test removes the multipart files of the request when it ends.
- `tifa build` reads no `_test.go` files, so tests may sit in any folder under `app/`.

### Render clock

Set the clock through the context in `OnRequest`, or give an `httptest` request the same context in a test:

```go
at := time.Date(2026, 10, 22, 12, 0, 0, 123456789, time.UTC)
ctx := tifa.WithRenderClock(c.Context(), func() time.Time { return at })
c.SetContext(ctx)
return c.Next()
```

Date and Intl round down to milliseconds. Temporal keeps nanoseconds. The clock covers the whole render, including promises and queueMicrotask, and is cleared before VM reuse or replacement. Pass nil to restore the live clock. The callback must not block and must be safe for concurrent calls when renders share it. It does not change Go's time.Now, timeouts or the browser clock.

Without `WithRenderClock` the clock stays live. A fixed clock does not make a render repeatable when it also uses randomness or outside data.

Module top-level code runs during VM prewarming, before a request, on the live clock. Time values captured there are not recomputed. A Date.now function, Temporal.Now method or Intl formatter saved at module load reads the current render's clock when called during that render.

The clock is not a cache key. A cache hit skips rendering, and requests with the same key may share a render. Return displayed times from `Load` so SSR, caching and browser hydration use the same data. `tifatest.NewCtx` preserves the request context but does not run SSR itself. Use `tifatest.SetNext` to test middleware passing this context, and server requests to check full HTML.

## How Load may use Ctx

This section covers `Load`, `Layout` and the functions they call. At build time, tifa starts from each `Load` and `Layout` function and finds the route parameters (`c.Param`), query parameters (`c.Query` and `c.QueryAll`) and Layouts (`tifa.Use`) it reads. When `Load` passes `*tifa.Ctx` to a function in this module, tifa follows the call level by level, across packages too, and counts every parameter it finds toward that `Load`. A helper called by several `Load` functions counts toward each of them. tifa does not follow into its own functions.

So that tifa finds everything, Ctx may only be used where tifa can see it. Pass `c.Param`, `c.Query` and `c.QueryAll` a constant string, such as `c.Param("order")`, and call them and `tifa.Use` directly: storing one in a variable or passing it as a value fails the build.

The only types that may hold a Ctx are: `*tifa.Ctx`, `tifa.Ctx`, non-generic structs declared in this module (whose fields hold Ctx only through these types, directly, embedded or indirectly), and pointers to such structs.

A value that holds a Ctx may only appear in these five places:

1. As the receiver of a direct method call, such as `c.Query("tab")` or `req.Page()`.
2. As an argument to a non-generic function from this module or from tifa, where the parameter has exactly the same type. A function literal called in place also counts.
3. In a composite literal of a struct that holds Ctx, for a field of exactly the same type.
4. Assigned with `:=` or `var x T = ...` to a new local variable of exactly the same type. That variable follows the same rules.
5. With its address taken, where the resulting pointer is still a type that may hold Ctx.

Any other use fails the build and names the file and line. For example: converting it to another type or an interface, assigning it to an existing variable, assigning several values at once, `return`ing it, putting it in a slice, map, array or channel, `range`, package-level variables, `defer`, `go` statements, or passing it to a generic function, an interface method, a function variable or a function outside the module (the standard library and third-party libraries). `return c.Redirect(...)` and `return c.Next()` are fine, because they return the result of a method call, not the Ctx itself.

`Load`, `Layout` and the functions they call must not call `c.Request()` or `c.Writer()`. tifa cannot see parameters read through the raw request. Use `c.Param`, `c.Query`, `c.QueryAll`, `c.Header`, `c.Cookie` or `c.Context()` instead. `c.Request()` and `c.Writer()` are only for method handlers and `OnRequest`.

They must not call `c.BindQuery` or `c.Bind` either. tifa cannot name the query parameters a struct decodes, so the page would not load again when they change, and a page load or the data request of a navigation has no body. Read each query parameter with `c.Query("name")`, or `c.QueryAll("name")` for a repeated one.

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
