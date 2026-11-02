---
summary: 'How to write a tifa app: folder layout, how folders become URLs, the three files of a folder (route.go, page.tsx, layout.tsx), Layouts, a Layout without the Layouts above (layout_.tsx), hooks.go, 404s and errors, a site in several languages, the SSR and cache switches, what the server render has, which libraries render differently there and which components to load only in the browser, using Intl and Temporal so hydration matches, styles with Tailwind CSS and shadcn/ui, how to test Go route code, how Load may use Ctx, method handlers with typed input and result, what a page open in a browser does when you deploy a new version, and the JSON rules for data, inputs and results'
read_when:
  - Creating a tifa app, or adding pages, Layouts or API routes to one
  - The build reports an error about Ctx usage or a Load return type and you want to fix it
  - Changing the rules in internal/analyze and you need to know what users were promised
  - Changing the apps in examples/shop or test/apps
  - Writing Go tests for a Load, a Layout function, a method handler or OnRequest
  - Adding CSS, Tailwind CSS or shadcn/ui to an app
  - Formatting numbers or dates in a page, or tifa dev warns about an Intl call
  - A visitor keeps a page open while you deploy, and you want to know what the browser does, or a 409 with `X-Tifa-Build` shows up
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
    route.go  layout.tsx            root Layout (Layout in route.go), wraps every page, and / (GET in route.go)
    projects/
      project.go  project_test.go   the project domain: Project, GetProject, ListProjects
      project+/
        route.go  layout.tsx  page.tsx   Layout of /projects/{project} and every page below it, and the page /projects/{project}
        orders/
          order.go  order_test.go   the order domain: Order, ListOrders, GetOrder, ShipOrder
          order+/
            route.go  page.tsx      page /projects/{project}/orders/{order}
            items.tsx  ship-form.tsx
        stats/
          route.go  page.tsx        page /projects/{project}/stats
    store/
      layout_.tsx                   wraps /store and every page below it without the root Layout around them, and shows their errors
      lang+/
        route.go  layout.tsx  page.tsx   Layout of /store/{lang} and every page below it, and the page /store/{lang}
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
- The go command skips folders that start with `.` or `_`, and folders named `testdata`. A `route.go`, `page.tsx`, `layout.tsx` or `layout_.tsx` inside one fails the build. Other files there, such as a `_components` folder, are fine.
- A folder name must also be valid in a Go import path, so names such as `con` or `x~1` (Windows reserved and short names) fail the build.
- A folder holds at most one parameter folder. `items/id+` next to `items/key+` or `items/path++` fails the build, because both would read the same URL segment.
- Two routes (pages or API routes) whose URLs overlap without one being more specific fail the build, as in Go's `http.ServeMux`, such as `a/b+` next to `a+/b`. A literal is more specific than a parameter, so `items/new` next to `items/id+` is fine.
- The URL of a folder with a Layout but no page shows a 404 inside that Layout (see [404](#404)), unless a `name++` folder right below it has a page, which answers that URL with an empty rest. No other page may match that URL unless it is more specific than the Layout's path: `settings/layout.tsx` without a `settings/page.tsx`, next to `user+/page.tsx`, fails the build, because `/settings` would also match `/{user}`, but `items/new` next to a Layout in `items/id+` is fine.
- Declaring `const Path` fails the build. The folder is the URL.

## The files of a folder

A folder has at most three convention files:

| File | What it holds |
| --- | --- |
| `route.go` | Every Go name of the folder: `Layout`, `LayoutSSR`, `Load`, `SSR`, `Cache` and the method handlers |
| `page.tsx` | The page component. A folder with `page.tsx` is a page |
| `layout.tsx` | The Layout component |
| `layout_.tsx` | The Layout component in place of `layout.tsx`, shown without the Layouts above it (see [A Layout without the Layouts above](#a-layout-without-the-layouts-above)) |

- The names of `route.go` are read from that file only. Declaring one of them in another file of the package, `layout.go` included, fails the build. Helpers and types may go in any file.
- A folder with only `route.go` is an API route when it declares a method handler, and gives the pages below it their Layout data when it declares `Layout`. It can do both. `Load` is only allowed next to a `page.tsx`.
- `route.tsx` is not a convention file. A `route.tsx` fails the build with the message to rename it to `page.tsx`, so a page is never taken for a plain component.
- `tifa build` also writes a `route.gen.ts` module for each folder with a page, a Layout or a method handler, which `page.tsx`, `layout.tsx` and `layout_.tsx` import (see [page.tsx](#pagetsx)).

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
- `Layout` returns the data of the Layout of the folder (see [Layout](#layout)).
- A folder with a `page.tsx` is a page. The framework renders its GET, so you cannot also write `GET`. A page that needs no data and no other method can be `page.tsx` alone, without `route.go`.
- A folder with `route.go` and no `page.tsx` is an API route when it declares a method handler: every method handler answers as written, and `Load` is not allowed, because `Load` loads the data of a page. A `route.go` with only a `Layout` is no API route. It gives the pages below it their Layout data.
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
- A POST, PUT, PATCH or DELETE that a browser sends from another site, a form or multipart included, is answered with 403 before the handler runs, so a handler needs no check of its own for it. `server.trusted_origins` and `server.cross_site_paths` let chosen origins and paths through (see [Cross-site writes](config.md#cross-site-writes)).
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
| `c.Context()`, `c.WithContext(ctx)` | The request context, and a copy of `c` that carries another one, to store request-scoped values. `c` itself does not change |
| `c.Pattern()` | The path of the matched route, such as `/projects/{project}`, `""` when no route matched |
| `c.IsData()` | Whether this is a navigation data request of the browser router rather than a page load |
| `c.SetHeader(key, value)`, `c.SetCookie(cookie)` | A response header, a response cookie. Allowed in `Load` too |
| `c.JSON(code, v)`, `c.Text(code, s)`, `c.NoContent(code)` | Writes the response. Not allowed in `Load` |
| `c.Redirect(url, code)` | Returns an error value. Return it to redirect |
| `c.Bind(&v)`, `c.BindQuery(&v)` | Decodes the body or the query string into a struct, see above. Not allowed in `Load` |
| `c.SetBodyLimit(n)` | Changes the body limit of this request (`server.body_limit` by default). Call it before anything reads the body, usually in `OnRequest`. Once reading has started it returns an error |
| `c.Request()`, `c.Writer()` | The raw `*http.Request` and `http.ResponseWriter`, for method handlers and `OnRequest` only |

## page.tsx

```tsx
import { page, type PageData } from './route.gen'

export default function OrderPage() {
  const order = page.useLoaderData()
  return <OrderItems />
}

export const head = ({ loaderData }: { loaderData?: PageData }) => ({
  meta: loaderData ? [{ title: loaderData.id }] : [],
})
```

- UI only, no data loading. `./route.gen` is a generated module, one per folder, that exports `page` and `layout` (the TanStack routes of the page and the Layout, only when the folder has them), `PageData` and `LayoutData` (the return types of `Load` and `Layout`), `Params`, and a function per method handler. Child components in the same folder also import `./route.gen`. A Layout's data comes from the `route.gen` of its folder: `layout.tsx` imports `layout` from `'./route.gen'`, and a page reads its project Layout with `layout` from `'../../route.gen'`, for example. A folder with no generated module fails the build when something imports its `route.gen`.
- The default export is the page component. Optional exports: `head` and `Error`. A 404 shows in `Error` too (see [404](#404)), and exporting `NotFound` fails the build.
- In the browser, each `page.tsx`, and each `layout.tsx` or `layout_.tsx` but the root Layout's, is a chunk of its own with everything it imports, `head` included. It loads the first time its route is needed, and the router waits for it before it shows the page or runs `head`. The HTML of a page preloads the chunks of its Layouts and route, so they arrive with the entry script. Code at the top level of the file, side-effect imports included, runs then and not when the app starts, so put setup that every page needs in the root Layout or a module it imports. See [Route modules in the browser](build.md#route-modules-in-the-browser).
- Go slices and maps carry `| null` in TypeScript (Go's nil is encoded as `null`). Write `?? []` before using them.
- Put components that need browser features at the module top level into a `.client.tsx` file, and load them lazily with `clientOnly` from `tifa/client`. They are left out of the SSR bundle, and the first paint shows a placeholder. Components that render but show nothing until the browser draws them belong there too, see [Components that draw only in the browser](#components-that-draw-only-in-the-browser):

```tsx
import { clientOnly } from 'tifa/client'

const OrderActions = clientOnly(() => import('./actions.client'), <button disabled>Loading…</button>)
```

- After a write that does not go through a handler function (below), call TanStack's `router.invalidate()`. The data of the current page and all its Layouts is loaded again, merged into one request, and the promise it returns resolves once the page shows the new data.

### Calling method handlers from the browser

The `route.gen` module of a folder with method handlers exports one function per handler, named after its method. API routes get a `route.gen` module too, so a page can import the handlers of any folder:

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

- The function takes one object. `params` holds the path parameters and is left out when the folder has none. A `name++` folder's key is `_splat`. A value that would send the request to another path is refused before any request and rejects the promise: an empty value, `.` or `..` for a `name+` parameter, and a `.` or `..` segment or an empty segment before the end for `_splat`. An empty `_splat` is fine. `input` is the handler's input and is left out when it takes none. `signal` aborts the request.
- GET and DELETE send the input in the query string, the other methods as a JSON body. The result is typed from the Go result, with `| undefined` for a redirect (below): `func(c, in) (Out, error)` gives `Promise<POSTOutput | undefined>`. `func(c, in) error` gives `void`. `func(c) error` writes its own response, so its function gives back the `Response` as it is, also for an error status.
- After a POST, PUT, PATCH or DELETE succeeds, the data of the current page and its Layouts is loaded again before the promise resolves. Pass `invalidate: false` to keep it. A failure and a GET never reload.
- An error answer throws `TifaError` with `status`, `message` and `fields` (the messages of a `tifa.Fields`, empty otherwise). A network failure throws as `fetch` does. A call from a page of an older build than the server fails with status 409, see [Pages open while you deploy](#pages-open-while-you-deploy).
- When the handler or `OnRequest` returns `c.Redirect(url, code)`, the browser goes to that URL: through the router for a URL of this site, without reloading the page, and as a whole page load for another site. After a POST, PUT, PATCH or DELETE (unless `invalidate: false`) the data the router holds goes stale first, so the new page and its Layouts show fresh data. A redirect to the page that is showing loads its data again, as a call without a redirect does. It counts as the same page when the path, the hash and the query values match, whatever the order and encoding of the query. The promise resolves once the new page shows, to `undefined`, also for a handler with a result. Whether a call is redirected is only known from its answer, since `OnRequest` may redirect any request, so the result type of every handler function with a result includes `undefined`. The function of a handler that writes its own response gets the `Response` of the redirect (`ok`, with the status in `X-Tifa-Redirect`). A relative URL is resolved against the URL of the handler, not of the page. A 3xx the handler wrote itself through `c.Writer()`, or one a proxy wrote, cannot be read in the browser and rejects the promise with an `Error`.
- Call these functions from event handlers or effects. Calling one while the page renders on the server throws.
- `route.gen` also exports `POSTInput`, `POSTOutput` and so on for the types of each handler that has them.

The error that `c.Redirect` returns is a `*tifa.RedirectError` with the fields `URL` and `Code`. In `OnRequest` or a test, recognize it with `errors.As`.

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

A Layout lives in its folder next to the page of that folder, if there is one: the `Layout` function in `route.go` and `layout.tsx` wrap the page in the same folder and every page below it. `layout.tsx` places child routes with `<Outlet />`. Either may be left out: without `layout.tsx` the child routes render directly, and without a `Layout` function the Layout has no data. A Layout with no page in its folder or below it fails the build.

```go
package project

func Layout(c *tifa.Ctx) (projects.Project, error) {
	return projects.GetProject(c.Param("project"))
}

const LayoutSSR = false // optional, default true
```

- `Layout` and `LayoutSSR` go in `route.go`, next to `Load`, `SSR`, `Cache` and the method handlers of the same folder. Declaring one of them in any other file of the package, a `layout.go` for example, fails the build. A `route.go` with only a `Layout` is no API route.
- A Layout's `c.Param` may only read parameters of its own folder path. `Layout` in `app/projects/project+/route.go` may read `project`, not `order`. A page's `Load` is held to its own path the same way.
- When you switch between pages under the same Layout, the Layout component is not remounted and keeps its state. When a parameter the Layout function reads changes (for example, you move to another project), the Layout remounts and its state resets. When a parameter it does not read changes, the state is kept. Pages work the same way.
- Pages and Layouts can call `tifa.Use(c, project.Layout)` to get the result of a Layout that wraps them, for the current request: the Layout of the same folder or of an ancestor folder, unless a `layout_.tsx` between drops it. The same `Layout` runs only once per request. Any other Layout fails the build, and so does an argument that is not a package-level `Layout` function, such as a closure. A `Layout` function cannot use its own Layout, directly or through a helper.
- On navigation in the browser, the navigation data request runs only the `Load` and `Layout` functions of the listed routes. If an ancestor Layout's data is still in the cache, that Layout is not in the list and its `Layout` does not run this time. So do not put permission checks only in a Layout. Put them in `OnRequest`, or have the page's `Load` depend on the checking Layout through `tifa.Use`, so the check runs on every request.
- The document shell (`<html>`, `<head>`, assets and the script that injects data) is written as a string by the framework. React only renders the app root inside `<body>`, so the page is still a complete document when any level fails or returns 404. The root Layout (`app/layout.tsx`) can export two constants to customize the shell: `htmlAttributes` sets the attributes of `<html>` every page starts from (`{ lang: 'en' }` when not exported, and a route's `head` can change them for its pages, see below), and `documentHead` holds head tags that every page carries (also when no route matches or the root `Layout` fails), in the same format as the return value of `head`. Put the site's default `title` here so that the 404 page for an unmatched URL also has a title. A page's own `head` that returns a `title` overrides it. Exporting a `Document` component fails the build.

```tsx
export const htmlAttributes = { lang: 'zh-CN' }
export const documentHead = {
  meta: [{ title: 'Shop' }, { name: 'theme-color', content: '#f2f4f5' }],
  links: [{ rel: 'icon', href: '/favicon.svg' }],
}
```

- A page's head comes from the `head` return values of the matched routes: `title` and meta tags with the same name come from the deepest route, and link, style and script tags are deduplicated in route order. The framework always adds `<meta charset="utf-8">` and a viewport meta, and a route's `name: 'viewport'` meta replaces the latter. Tag attributes are written the way React DOM writes them on the server: `className`, `htmlFor`, `httpEquiv`, `crossOrigin` and so on become their HTML names. Event attributes starting with `on`, invalid attribute names, `null`, `undefined` and functions are not written. Boolean attributes such as `async` and `defer` are written with an empty value when true. For other attributes, boolean values are written as strings only on `data-` and `aria-` attributes. `javascript:` links are replaced. Head tags do not support the `style` attribute, and using it makes rendering fail. `htmlAttributes` follows the same rules. The content of style and script tags is written as is. Only the `<script` and `</script` (in style, `<style` and `</style`) that would end the element are escaped, the way React DOM does it. After browser navigation and after `router.invalidate()`, the head updates to match the new routes.
- A `head` can also return `htmlAttributes`, the attributes of `<html>` for its pages, such as the language of a site in several languages. Search engines, screen readers and the translation of browsers read the language from `lang`. They are merged over the root Layout's `htmlAttributes` one attribute at a time: the deepest route that gives an attribute wins, `undefined` leaves it to the routes above, and `null` takes it away. Attributes are matched by the name React DOM writes, so `className` in one route and `class` in another are the same attribute. `class` is replaced as a whole, never merged with a value from elsewhere. `style` is not supported, as on the tags of the head. They are written by the same rules as the tags of the head. After browser navigation and after `router.invalidate()` they follow the new routes too. In the browser tifa writes an attribute of `<html>` only when the value it wants for it changes, and takes away only attributes it wrote. So what a script of the page sets, such as the `dark` class of a dark mode script or the `lang` a translation tool changes, stays through hydration and navigation while the routes want the same value of that attribute. When that value changes, because a route sets another `class` or a navigation leaves it and the root Layout's `class` comes back, tifa writes it and the class the script added is gone. Keep such a theme in an attribute no route sets, such as `data-theme`, or have the script add it again. A page without SSR gets these attributes in the browser.

```tsx
// app/guide/lang+/layout.tsx. Layout in route.go checks the language of the URL and returns it,
// or tifa.ErrNotFound for a language the site does not have.
export const head = ({ loaderData }: { loaderData?: LayoutData }) => ({
  htmlAttributes: { lang: loaderData?.lang },
})
```

- The routes that count are those whose head is on the page. A 404 or an error below the Layout keeps the Layout's language, also for a URL below the Layout that no page has, since its 404 shows inside the Layout (see [404](#404)). When the Layout itself fails, its `head` runs without data, so the example above leaves the root Layout's `lang`. See [A site in several languages](#a-site-in-several-languages) for the rest of such a site.
- `htmlAttributes` must be an object, and `pnpm typecheck` reports any other value. At run time such a value fails like an error the `head` throws: the server render fails as an unexpected error (`OnError`, or a 500), with the message in the log, and on the page under `tifa dev`. In the browser, after a navigation or on a page without SSR, the router writes the error to the console and drops the whole head of that route.
- Take the language from the Layout's data, as above, rather than from `params`: the Go function checks it, so whatever someone types into the URL does not end up as the language of the page.

### A Layout without the Layouts above

Name the component file `layout_.tsx` instead of `layout.tsx`, and the Layout of the folder shows its pages without any Layout above it, the root Layout included. Only the document shell the framework writes stays around them. The storefront of [examples/shop](../examples/shop) works this way: it draws its own header and footer, and none of the workbench the root Layout draws around the other pages.

```text
app/
  route.go  layout.tsx                the workbench: sidebar, top bar, the list of shops
  projects/
    project+/
      route.go  layout.tsx  page.tsx  inside the workbench
  store/
    layout_.tsx                       the storefront starts here, without the workbench
    lang+/
      route.go  layout.tsx  page.tsx  the storefront in one language
      cart/
        route.go  page.tsx            inside the storefront
```

- The pages below it, also those further down, do not render the Layouts above it, and their `Layout` functions do not run for them: not for a page, not for the navigation data request of the browser, not for the 404 of a URL below it that no page has. So their data never reaches these pages.
- The `Layout` in the `route.go` of the same folder gives it data as usual. The folders below it nest as usual: a `layout.tsx` further down shows inside it and its `Layout` runs. A `layout_.tsx` further down drops it in turn, with everything above it.
- What belongs to the document still applies: the root Layout's `htmlAttributes` and `documentHead`, and `OnRequest`. What belongs to the Layouts above does not: their `head`, their `Error` and their `LayoutSSR`. When the `Layout` of a `layout_.tsx` fails, or returns `tifa.ErrNotFound`, no Layout is above it to show the error, so the framework's default page shows. Its own `Error` shows for what fails below it, as with any Layout. To show your own page when that `Layout` fails, put the `layout_.tsx` one folder higher, with an `<Outlet />` and an `Error`, as the shop does in `app/store`.
- In the browser, a move between a page below it and a page outside it swaps the whole frame, and the Layouts of the new page load their data when the router has none of it yet.
- The build fails for a `layout_.tsx` in `app/`, which has no Layout above it, for `layout.tsx` and `layout_.tsx` in the same folder, and for `tifa.Use` of a Layout that a `layout_.tsx` drops, from a page or Layout below it. It also fails for a Layout whose pages are all below a `layout_.tsx`, since it wraps none of them.
- React Router and TanStack Router also end a name with `_` to keep a route out of a Layout, but only out of the Layout of the segment before it: the root route and the Layouts further up still wrap it. tifa's `layout_.tsx` drops every Layout above it, the root Layout too.

## 404

A 404 shows in the `Error` of a route (see [Errors on a page](#errors-on-a-page)), which gets it as a `RouteError` with `status` 404:

```tsx
import { RouteError } from 'tifa/client'

export function Error({ error }: { error: unknown }) {
  if (error instanceof RouteError && error.status === 404) return <p>No such order</p>
  return <p>Something went wrong</p>
}
```

| Case | What is shown |
| --- | --- |
| A page's `Load` returns `tifa.ErrNotFound` | The first `Error` going up from that page. Outer Layouts are shown as usual |
| A `Layout` function returns `tifa.ErrNotFound` | The first `Error` going up from the Layout one level up |
| The URL matches no page | The first `Error` going up from the deepest Layout the URL matches, inside that Layout. The Layouts from the outermost one down to it run and show as usual: from the root, or from the `layout_.tsx` above it |
| No route has an `Error` | The framework's default 404 page |

The status code is 404 in all these cases, also after a navigation in the browser, whose data request answers with the data of the Layouts and status 404.

To find the Layout of a URL that matches no page, tifa compares the URL with the path of each Layout and page segment by segment, from the left. A literal segment beats a parameter, and a parameter beats the end of the path. The path that matches most wins, and a page counts as the Layout it is in. With these folders:

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
- The `Error` of a Layout only ever shows in place of a route below it, in the Layout's `Outlet`, so it can read the Layout's data and the context its component provides. When a Layout fails, the `Error` of the Layout above shows, and when the root Layout or a Layout in `layout_.tsx` fails, the framework's default page.
- A `NotFound` export in `page.tsx` or `layout.tsx` fails the build. Move what it shows into `Error`, under `error.status === 404`.

## Errors on a page

`page.tsx` and `layout.tsx` can export `Error`, shown in place of that route when its `Load` or `Layout` fails, when a 404 shows there (see [404](#404)), or when a component below it throws:

```tsx
import { RouteError } from 'tifa/client'

export function Error({ error }: { error: unknown }) {
  if (error instanceof RouteError && error.status === 403) return <p>No access</p>
  return <p>Something went wrong</p>
}
```

- A failed `Load` or `Layout` reaches `Error` as a `RouteError` with `status` and the public message (the `tifa.Error` message, or what `OnError` returned). It is the same on the server and after hydration.
- A route without its own `Error` uses the one of the closest ancestor that exports one, then the framework's default. The ancestors end at a `layout_.tsx`.
- The status code of the page is that of the outermost failed route.
- A component that throws while the page renders on the server is an unexpected error: `OnError` sees it once and picks the status (500 without `OnError`) and the public message. The page then renders again with that error in place of the page, as a `RouteError`, so the first `Error` from the page up shows it inside the Layouts. If that render fails too, because the `Error` or a Layout around it throws, the error moves one route up, in place of the Layout the page is in, and so on up to the outermost Layout, the root one or a `layout_.tsx`. Only when the error in place of that Layout fails too does the browser get a plain error page from Go with the status and the public message. The browser hydrates the page with the error where the server put it, and does not run the route that threw again. Such an answer is never cached. The stack trace shows only under `tifa dev`, over the page with the `Error`, and the page reloads once you save a fix (see [tifa dev](dev.md#when-a-page-fails-to-render)).
- A server render that uses up `ssr.vm_memory` or runs past `ssr.timeout` (see [config.md](config.md)) stops right there. No `catch`, `finally` or promise handler in your code or in a library runs for it, so a page cannot catch it to show something else. The VM is replaced with a fresh one. A render that used up `ssr.vm_memory` renders again with the error, as above, on a fresh VM. A render that ran past `ssr.timeout`, or found every VM busy, does not: Go answers with its plain error page at once. All the renders of one request share one `ssr.timeout`, counted from the first, and a render with the error that runs out of it ends the tries. Code that recurses without end gets `RangeError: Maximum call stack size exceeded`, as in Node, which a `catch` can take (see [stack depth](config.md#stack-depth)). When nothing catches it, it fails the render like any other error.
- The page that shows the error gets the head of a page whose `Load` failed. The route the error replaces runs its `head` without data, the routes below it add no tags, and the `head` of every route above it runs again over the matches as they now are, so `status` tells it that a route failed (see [A site in several languages](#a-site-in-several-languages)). Tags and `htmlAttributes` that a route gave for the page that did not render are not on the error page. The browser works out the same head when it hydrates.
- Once the browser has taken over, a component that throws shows the nearest `Error`, the same as a failed `Load`.
- The document shell stays complete whatever fails, so the page still loads its scripts and styles.

## A site in several languages

tifa has no language settings: the language is a part of the URL, and the app checks it in Go. The storefront of [examples/shop](../examples/shop) under `/store` works this way.

- Put the language in a parameter folder at the start of the URLs, such as `app/store/lang+`. Every URL names its language, the default one too: `/store/zh-CN/products` and `/store/en-US/products`.
- The `Layout` of that folder checks the language and returns it with its data. A language the site does not have returns `tifa.ErrNotFound`, which shows the 404 of the Layout above, in its language:

```go
// app/store/lang+/route.go
func Layout(c *tifa.Ctx) (Shell, error) {
	l, err := store.FindLocale(c.Param("lang")) // tifa.ErrNotFound for a language the store does not have
	if err != nil {
		return Shell{}, err
	}
	return Shell{Locale: l, Menu: products.Menu(l)}, nil
}
```

- The shop puts a `layout_.tsx` in `app/store`, the folder above, so the storefront shows without the workbench around it (see [A Layout without the Layouts above](#a-layout-without-the-layouts-above)). It renders only an `<Outlet />`, and its `Error` shows the 404 of a language the store does not have in both languages, with a link to the storefront in each, since the visitor's language is not known.
- Pages and components take the language from that data, through `layout.useLoaderData()` or a context the Layout component provides, not from `params`: the Go function checked it, so whatever someone types into the URL does not end up as the language of the page. A `Load` below the Layout checks it the same way, or reads the Layout's result with `tifa.Use`.
- The `head` of the Layout writes `lang` on `<html>` and links to the same page in each language, which search engines read: one per language, the page's own included, and `x-default` for a browser that asks for none of them, here the default language. Search engines want these URLs in full. `matches` holds the routes of the page, the page last. When any of them has a `status` other than `success` (a `Load` failed, or a component threw while the page rendered on the server), or is marked `_notFound` (no page has the URL, and the deepest Layout it matches shows the 404), the page shows an error and the head leaves the links out: a URL that answers 404 is no page to point search engines to, in any language. The `lang` stays. The head:

```tsx
// app/store/lang+/layout.tsx
const site = 'https://shop.example.com' // where the store is served
const languages = ['zh-CN', 'en-US'] // the languages of the store, the default one first

export const head = ({ loaderData, matches }: { loaderData?: LayoutData; matches: Array<{ pathname: string; status: string; _notFound?: unknown }> }) => {
  if (!loaderData) return {}
  const { code } = loaderData.locale
  const common = { htmlAttributes: { lang: code } }
  if (matches.some((match) => match.status !== 'success' || match._notFound)) return common
  const page = matches[matches.length - 1].pathname.replace(/\/$/, '').slice(`/store/${code}`.length)
  const url = (lang: string) => `${site}/store/${lang}${page}`
  return {
    ...common,
    links: [
      ...languages.map((lang) => ({ rel: 'alternate', hrefLang: lang, href: url(lang) })),
      { rel: 'alternate', hrefLang: 'x-default', href: url(languages[0]) },
    ],
  }
}
```

- A URL below the language folder that no page has shows its 404 inside the language Layout, in the language of the URL (see [404](#404)). No page for the rest of the path is needed.
- The URL without a language, such as `/store`, sends the visitor on with a method handler: to the language they picked before, kept in a cookie the app sets when they pick one, or else to the best match for `Accept-Language`. Match with `Matcher` of `golang.org/x/text/language`, which knows that `en-GB` is closer to `en-US` than to `zh-CN`. The answer depends on the request, so redirect with 302 and add `Vary`:

```go
// app/store/route.go
var codes = []string{"zh-CN", "en-US"} // the default language first

var matcher = language.NewMatcher([]language.Tag{language.MustParse(codes[0]), language.MustParse(codes[1])})

func GET(c *tifa.Ctx) error {
	c.Writer().Header().Add("Vary", "Cookie, Accept-Language")
	if picked, err := c.Cookie("lang"); err == nil && slices.Contains(codes, picked.Value) {
		return c.Redirect("/store/"+picked.Value, http.StatusFound) // the language the visitor picked
	}
	tags, _, _ := language.ParseAcceptLanguage(c.Header("Accept-Language"))
	_, i, confidence := matcher.Match(tags...)
	if confidence == language.No {
		i = 0 // none of the languages is close: the default one
	}
	return c.Redirect("/store/"+codes[i], http.StatusFound)
}
```

- Load the translations synchronously: bundle the messages of every language with the app and set up the library without waiting, such as i18next with `initAsync: false` and its `resources`. The server render waits for nothing and reads no files, so messages that arrive later are missing from the HTML, and the browser would hydrate with other text. Both sides take the language from the Layout's data, so they show the same text. Format numbers and dates with the locale and time zone from the data as well (see [Formatting so hydration matches](#formatting-so-hydration-matches)).

## SSR and cache

```go
const SSR = false       // the page, default true
const Cache = true      // the page, default false
const LayoutSSR = false // the Layout of the folder, default true
```

`SSR` and `Cache` apply to the page of `page.tsx`. In a `route.go` without one they fail the build.

- `SSR = false`: middleware and every `Load` run as usual, but QuickJS is skipped. The response is a minimal document with the data, and the browser renders it. `LayoutSSR = false` applies to every page the Layout wraps.
- `Cache = true`: the rendered result is cached by build version, URL and data, in a cache of `cache.size` (see [config.md](config.md)). When the data has not changed, the HTML is returned directly. Components must not read values that change on every call, such as `Date.now()` or `Math.random()`, while rendering. Hydration requires this too.
- When requests for a `Cache = true` page miss the cache at the same time with the same build version, URL and data, such as a burst of visitors right after a deploy, they share one render. Each request still runs its middleware and `Load`s, keeps the headers they set, such as `Set-Cookie` and `Vary`, and gets the encoding it accepts. This also holds with `cache.size` 0 and for a page too large for the cache, and under `tifa dev`. Requests with other data render on their own, and pages without `Cache` render for every request, since their HTML may differ by user.
- When that shared render fails, every request that waited for it gets the same error and goes to `OnError`, a panic included. Nothing is cached, so the next request renders again. A request whose client goes away stops waiting at once and its connection is cut, with no answer sent: `next(c)` in `OnRequest` returns an error that matches `context.Canceled`, but `OnError` is not called and nothing is logged. The render goes on for the others. A request whose own deadline passes while it waits fails like any other error. Once no request waits, a render still waiting for a VM is dropped, and one that already runs finishes and goes into the cache. If it fails then, no request reports it to `OnError`, so the error is logged.
- Every page response carries an ETag. A second request with `If-None-Match` gets 304 when the data has not changed. This has nothing to do with `Cache`.
- Every answer of the app (pages, 304s, 404 pages, navigation data, method handlers, redirects) is sent with `Cache-Control: private, no-cache` unless you set `Cache-Control` yourself. A CDN or proxy does not store it, since the page may be meant for one visitor. The browser may keep it and asks again with the ETag, so an unchanged page ends in a 304. To change it, set the header in `OnRequest`, a `Load`, a `Layout` or a method handler with `c.SetHeader("Cache-Control", "...")`. Your value is sent as it is. JSON errors (including 405 and the 503 while the server shuts down), the plain error page from Go, the page with an error that the server renders again, and the busy answer are `no-store`, unless you set `Cache-Control` before the error. A 404 page that the app renders keeps the default. Files under `/assets/` keep their long cache, and files from `public/` keep `no-cache` (see [Page requests](build.md#page-requests)). `Cache = true` only caches the render inside the process and does not change the header.

## Pages open while you deploy

A visitor can keep a page open while you deploy a new version, so the page is from the old build and the server is the new one. The page sends the build it was rendered with (`X-Tifa-Build`) along with its data requests and its handler calls, and the server answers 409 for another build. What the visitor sees:

- Clicking a link: the browser reloads the current page, and the new build shows it. One reload, whatever number of requests got the 409.
- Calling a handler function: the call fails with a `TifaError` of status 409 and the browser reloads the page. The call is refused before the handler runs and never sent again, because the new page may mean another input by it. The visitor does it again on the new page. A 409 that your own handler returns has no `X-Tifa-Build` header and reloads nothing. A call without the header, from curl or another program, is not checked.
- Loading code: a lazy route or an `import()` asks for a chunk that the new build no longer has, and the browser reloads the page (this is Vite's `vite:preloadError`).

Each reload is made once for a build. When the page it brings back still disagrees with the server, which happens when a cache hands out the old page again, it does not reload a second time. The data request fails with the 409, which the `Error` of the route shows, the call throws it, and a failed import goes to the `Error` of the route or to the code that imported. The tab remembers the reloads in `sessionStorage`, and a page reloads only after it has written the mark and read it back. When the browser blocks the storage or does not keep what is written, the page does not reload: the failures above show as they are, with the message "the application was updated, reload the page", and the visitor reloads by hand. A reload that cannot be remembered could repeat for as long as the build disagrees.

A page whose HTML is old and whose entry script is gone cannot be fixed by the page: no code runs. tifa sends pages with `Cache-Control: private, no-cache`, so a CDN does not keep them, and this can only happen when your `OnRequest` lets a CDN cache pages. Then clear the pages in the CDN when you deploy.

## What the server render has

Pages render on the server in a QuickJS VM, not in Node or a browser. The VM has the JavaScript language itself, `Proxy`, `WeakRef`, `FinalizationRegistry` and `new Function` included, and these:

- `URL`, `URLSearchParams`, `TextEncoder`, `TextDecoder`, `AbortController`, `Headers`, `Response` and `queueMicrotask`. `TextDecoder` knows UTF-8 only: another encoding, such as `new TextDecoder('latin1')`, throws a `RangeError`.
- `atob` and `btoa` for Base64 binary strings, and `DOMException` for Web API errors. `btoa` rejects characters above U+00FF.
- `crypto.getRandomValues` fills integer typed arrays in place, up to 65,536 bytes per call. Views on shared or resizable buffers throw `TypeError` before element type or quota checks. `crypto.randomUUID` makes version 4 UUIDs. Both read fresh bytes from Go's `crypto/rand`, including during module loading. There is no random byte pool. This host operation can read the OS entropy source. It does not expose file or network access to application code. Keep random values out of hydrated HTML, just as with `Math.random`.
- `structuredClone` preserves cycles and shared references in primitives, objects, arrays, Date, RegExp, Map, Set, errors (including cause and AggregateError), ArrayBuffer, typed arrays, DataView and boxed primitives. Buffers are copied, including resizable buffers. Views on resizable ArrayBuffers throw `DataCloneError`, whether their length is fixed or tracks the buffer. Functions, symbols and weak collections throw `DataCloneError`. Promise, FinalizationRegistry, native iterators and unsupported host objects are rejected by their prototype chain. Removing or replacing that chain can make these values look like ordinary objects, so only their enumerable own properties are copied. Unlike supported built-ins identified by intrinsic brand checks, these types cannot always be recognized in pure JS. Unlike browsers, transparent proxies of ordinary objects or arrays are cloned through their traps as ordinary objects or arrays. Objects that only imitate a supported built-in prototype are rejected. SharedArrayBuffer and nonempty transfer lists are unsupported and throw `DataCloneError`. An empty transfer list is allowed. Custom prototypes, accessors and property attributes are not preserved, and only enumerable own string keys of ordinary objects and arrays are copied.
- `AsyncLocalStorage`, as a global and as an export of `node:async_hooks` with `AsyncResource`, as Cloudflare Workers has them: `run`, `exit`, `getStore`, `AsyncLocalStorage.bind` and `AsyncLocalStorage.snapshot`, without `enterWith` and `disable`. A store follows `await` and promise callbacks, so a package that keeps the state of a request in one works. There are no timers or events for it to follow.
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

### Packages that import Node modules

A package may import a Node module, such as `node:fs` or `node:path`, for code that runs only on Node. The server has no Node modules, but the import does not stop the app: each name it imports is a stand-in that throws when a render uses it, by calling it, constructing it or reading one of its properties. `tifa build` lists the Node modules left in the SSR bundle with the packages that import them. The list is a hint, as a page that never reaches that code renders. A module that only an `import()` reaches runs only when that import runs.

When a render uses a stand-in, the error names the export, the module, the package and the place in its source. One used at the top level of a module stops the VM from loading. Then import the package from a `.client.tsx` file, or use a build of it that does not need Node, as many packages have for the browser or for workers. A `require` of a Node module stays a `require`, which the server does not have, and the build warns of it the same way. `AsyncLocalStorage` and `AsyncResource` of `node:async_hooks` are not stand-ins, see above.

### Components that wait

A component may suspend on work that promises settle by themselves: a `React.lazy` component whose module is in the bundle, as `lazy(() => import('./panel'))` is, or `use` of a promise made from what the page already has. The server waits for it and writes the page whole, with no fallback in the HTML, as the browser shows it once everything has loaded.

The server has no timers and does no I/O, so it waits only while promise jobs are left. A component that waits for a timer or a request never gets there: the render fails with the error that a component suspended and never settled. Give the page that data with `Load`, or render the component in the browser only. A page that does not suspend renders in one pass, as it did before.

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
import { page } from './route.gen'

// sales-chart.client.tsx default-exports the chart.
const SalesChart = clientOnly(() => import('./sales-chart.client'), <div className="h-64 w-full" />)

export default function StatsPage() {
  const { days } = page.useLoaderData()
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
import { page } from './route.gen'

export default function ReceiptPage() {
  const { amount, paidAt, locale, timeZone } = page.useLoaderData()
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
const { day, locale } = page.useLoaderData()
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

Then point the aliases of `components.json` into `components/`, so the app root keeps only `hooks.go`, `app/` and `components/`, and the folder of React hooks is not taken for `hooks.go`:

```json
"aliases": {
  "components": "@/components",
  "ui": "@/components/ui",
  "hooks": "@/components/hooks",
  "lib": "@/components/lib",
  "utils": "@/components/lib/utils"
}
```

`add` places files and rewrites their imports by these aliases: `add sidebar` writes `components/hooks/use-mobile.ts`. When `lib/utils.ts` only re-exports `cn` from the `cn` package, as with shadcn 4, delete it and import `cn` from `cn` in page code too, as the components do. Otherwise move it to `components/lib/utils.ts`.

`@/` is the app root, set by `paths` in the app's tsconfig (see [build.md](build.md#the-apps-tsconfig-and-vite-config)). Components render on the server like any other React code.

## hooks.go

`hooks.go` in the project root. All four functions are optional:

```go
func OnStart() error                            // before listening: open the database, logs, etc.
func OnShutdown(ctx context.Context) error      // after an exit signal, once in-flight requests finish
func OnRequest(c *tifa.Ctx, next func(*tifa.Ctx) error) error // the only middleware, call next(c) to continue
func OnError(c *tifa.Ctx, err error) tifa.Error // unexpected errors: report, return an error users may see
```

- `OnRequest` runs for pages, navigation data requests, API routes and unmatched URLs. It does not run for static files, and it does not see a write that a browser sends from another site: the program refuses that with 403 first (see [Cross-site writes](config.md#cross-site-writes)). For an unmatched URL, `c.Pattern()` is empty, and `next(c)` runs the Layouts its 404 shows inside (see [404](#404)).
- Resources the whole app shares, such as a database connection or a logger, go in files at the app root next to `hooks.go`, for example `db.go` or `middleware.go`. Each one builds a package-level variable or a function: set it up in `OnStart`, release it in `OnShutdown`, and use it directly anywhere, for example `myapp.DB` after importing `example.com/myapp`. Domains under `app/` import the root package for this. By convention the root package imports nothing under `app/`, so this never closes an import cycle. Models and queries stay in their domains. Route code uses these resources only inside functions, never while package-level variables are initialized. Do not use `init()` for setup. See [which way imports go](structure.md#which-way-imports-go).
- `next` runs the matched route, once. A second call returns an error. Pass it the `*tifa.Ctx` the route should see: `next(c)`, or `next(c.WithContext(ctx))` to store request-scoped values in the standard context, as `r.WithContext(ctx)` does for a request. `c.WithContext` returns a copy and leaves `c` alone. Business packages provide typed getters.
- The error `next` returns can be recognized with `errors.Is` and `errors.As`, a redirect as `*tifa.RedirectError`.

### Changing the log handler

tifa logs its own messages with `log/slog`, at a level and with fields: a failed request is `ERROR` with `method`, `path` and `err`, a refused cross-site write is `WARN`, the address it listens on is `INFO`. The lines about startup, such as the address, are written after `OnStart` returns, so a handler set there already gets them. To change where they go, set the default handler in `OnStart`:

```go
func OnStart() error {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	return nil
}
```

tifa looks up `slog.Default()` for each message, so a later `slog.SetDefault` also works.

### Setting up in OnStart

`OnStart` is where the app wires what the whole process shares: the log handler and plain `net/http` middleware.

```go
import (
	"log/slog"
	"net/http"
	"net/http/pprof"
	"os"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

func OnStart() error {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	tifa.SetHTTPHandler(func(app http.Handler) http.Handler {
		mux := http.NewServeMux()
		mux.HandleFunc("/debug/pprof/", pprof.Index)
		mux.Handle("/", otelhttp.NewHandler(app, "tifa"))
		return mux
	})
	return nil
}
```

`tifa.SetHTTPHandler(wrap)` gets tifa's handler and returns the one to serve. The wrapper is outside everything else: static files, the cross-site check and the routes. It sees every request with the original `http.ResponseWriter`, including the 403 of a refused cross-site write. It is plain `net/http`, so `OnRequest`, `OnError` and the cross-site check do not apply to the paths it serves itself, such as `/debug/pprof/` above. Put such paths behind your own check, or keep them on another listener. A shutdown still waits for the requests it wraps. Call it in `OnStart` only: it panics once the server listens. `tifa.SetHTTPHandler(nil)` removes the wrapper.

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
- Call `OnRequest` directly with a closure for `next`: `OnRequest(c, func(c *tifa.Ctx) error { got = c.Context(); return nil })`. The closure stands in for the route, so it neither recovers a panic nor writes a result.
- The body limit is the default `server.body_limit`, 1 MiB, unless `c.SetBodyLimit` changes it. The test removes the multipart files of the request when it ends.
- `tifa build` reads no `_test.go` files, so tests may sit in any folder under `app/`.

### Render clock

Set the clock through the context in `OnRequest`, or give an `httptest` request the same context in a test:

```go
at := time.Date(2026, 10, 22, 12, 0, 0, 123456789, time.UTC)
ctx := tifa.WithRenderClock(c.Context(), func() time.Time { return at })
return next(c.WithContext(ctx))
```

Date and Intl round down to milliseconds. Temporal keeps nanoseconds. The clock covers the whole render, including promises and queueMicrotask, and is cleared before VM reuse or replacement. Pass nil to restore the live clock. The callback must not block and must be safe for concurrent calls when renders share it. It does not change Go's time.Now, timeouts or the browser clock.

Without `WithRenderClock` the clock stays live. A fixed clock does not make a render repeatable when it also uses randomness or outside data.

Module top-level code runs during VM prewarming, before a request, on the live clock. Time values captured there are not recomputed. A Date.now function, Temporal.Now method or Intl formatter saved at module load reads the current render's clock when called during that render.

The clock is not a cache key. A cache hit skips rendering, and requests with the same key may share a render. Return displayed times from `Load` so SSR, caching and browser hydration use the same data. `tifatest.NewCtx` preserves the request context but does not run SSR itself. Call `OnRequest` with a closure for `next` to test middleware passing this context, and use server requests to check full HTML.

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

Any other use fails the build and names the file and line. For example: converting it to another type or an interface, assigning it to an existing variable, assigning several values at once, `return`ing it, putting it in a slice, map, array or channel, `range`, package-level variables, `defer`, `go` statements, or passing it to a generic function, an interface method, a function variable or a function outside the module (the standard library and third-party libraries). `return c.Redirect(...)` is fine, because they return the result of a method call, not the Ctx itself.

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
