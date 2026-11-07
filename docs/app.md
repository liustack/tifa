---
summary: 'How to write a Tifa app: folder layout, how folders become URLs, the three files of a folder (route.go, page.tsx, layout.tsx), Layouts, a Layout without the Layouts above (layout_.tsx), hooks.go, hooks.ts and the setup it runs before the first render, 404s and errors, a site in several languages, the SSR and cache switches and the whole-page cache (a page that is the same for everyone, kept for a set time) and making such a page again after a write, what the server render has, which libraries render differently there and which components to load only in the browser, using Intl and Temporal so hydration matches, styles with Tailwind CSS and shadcn/ui, how to test Go route code and whole requests with tifatest.Start, how Load may use Ctx, query parameter types for Link and navigate, method handlers with typed input and result, files in the input and the body limit and read time of a route, showing HTML the app trusts with tifa.HTML, what a page open in a browser does when you deploy a new version, and the JSON rules for data, inputs and results'
read_when:
  - Creating a Tifa app, or adding pages, Layouts or API routes to one
  - The build reports an error about Ctx usage or a Load return type and you want to fix it
  - Changing the rules in internal/analyze and you need to know what users were promised
  - Changing the apps in examples/shop or test/apps
  - Writing Go tests for a Load, a Layout function, a method handler or OnRequest
  - Testing a whole request in go test, such as signing in and following the redirect, with tifatest.Start or tifatest.StartPages
  - Taking files in a method handler, or giving a route a body limit or read time of its own with `BodyLimit` and `ReadTimeout`
  - Giving `Link`, `navigate` or `useSearch` the query parameters a page reads, or a type error about `search`
  - Keeping a public page in memory for a few minutes with `const Cache`, what becomes anonymous on it, or the build error about reading a cookie or the session on such a page
  - A write changes what a page with `Cache` shows, and the page should show it at once with `tifa.ExpirePages`
  - Showing HTML the app made or trusts, such as an article, with `tifa.HTML` and `<HTML>`, or links inside it
  - Adding CSS, Tailwind CSS or shadcn/ui to an app
  - Formatting numbers or dates in a page, or `tifa dev` warns about an Intl call
  - A visitor keeps a page open while you deploy, and you want to know what the browser does, or a 409 with `X-Tifa-Build` shows up
  - A page fails to render on the server or does not hydrate, and you want to know which browser APIs and library features the server has
  - A component shows nothing until the browser draws it, such as a chart or a map, and you wonder whether to render it on the server
  - Setting up something once for the whole app on the JavaScript side, such as i18next formatters or dayjs plugins, in hooks.ts
---

English · [简体中文](app.zh-CN.md)

# Writing an app

For a complete example, see [examples/shop](../examples/shop). For the build, the generated files and how to set up go.mod, see [build.md](build.md).

## Folders

```text
myapp/
  go.mod
  hooks.go                          process and request hooks (optional)
  hooks.ts                          onStart, run before the first render on the server and in the browser (optional)
  options.go                        options of the app, see Configuration (optional)
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
      _locales/                     zh-CN.json and en-US.json: the storefront is in these two languages, at /store and /en-US/store
      route.go  page.tsx            page /store
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
| `route.go` | Every Go name of the folder: `Layout`, `LayoutSSR`, `Load`, `SSR`, `RenderCache` and the method handlers |
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
| `tifa.Redirect(url, status)` | A redirect. Page loads and navigation follow it, and so does a handler function called from the browser (see below). Panics when `status` is not 3xx |
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

- The input is a struct, not a pointer to one. POST, PUT and PATCH decode it from the request body: JSON, or a URL-encoded or multipart form. A form fills only fields that are strings, numbers, bools or slices of them, and the files (see [Files in the input](#files-in-the-input)), and a form value for any other field is answered with 400. A form decodes each value by its kind and never calls `MarshalText` or `UnmarshalText`, so a type with one of them, or a slice of such types, is such another field, and so is a slice type with one of its own, such as `type Tags []string`, which TypeScript takes for one string. GET and DELETE decode it from the query string, so its fields may only be strings, numbers, bools or slices of them, by the same rule, with no embedded fields, and no `MarshalText` or `UnmarshalText` on the input type itself. The build checks this, and `c.BindQuery` returns an error for any other field whatever the query holds.
- Fields are named by their `json` tags, in a form body and the query string too. A `form` tag fails the build. Path parameters, headers and cookies still come from `c`.
- The input and the result follow the JSON rules below, the same as the result of `Load`. The input must also read back from the JSON its TypeScript type describes: a type that is a string because of `MarshalText` needs `UnmarshalText([]byte) error` too, a type with `UnmarshalText` must be a string in TypeScript unless it is a map key (a key is always a JSON string), and `UnmarshalJSON` is not accepted (except on `time.Time`).
- When the input type has a method `Validate` (it must be `func() error`), it runs after decoding and before the handler. `nil` goes on. A `tifa.Error` or `tifa.Fields` is answered as is, so return `tifa.Fields` to mark single fields. Collect them in a `tifa.Fields{}` and end with `return fields.Err()`, which is nil when nothing was added (an empty `tifa.Fields` is still an error). Any other error is answered with 400 and its `err.Error()` as the message, which the user sees.
- A POST, PUT, PATCH or DELETE that a browser sends from another site, a form or multipart included, is answered with 403 before the handler runs, so a handler needs no check of its own for it. `server.trusted_origins` and `server.cross_site_paths` let chosen origins and paths through (see [Cross-site writes](config.md#cross-site-writes)).
- A body or query string that does not decode is answered with 400 (413 for a body over the limit, 415 for another content type or none, or for JSON to an input that holds files, 408 for a body that does not arrive in time), and the handler does not run.
- A handler that writes its own response decodes input with `c.Bind(&in)` for the body and `c.BindQuery(&in)` for the query string. These name a field by its `form` tag first, then its `json` tag, then its Go name. `c.Bind` fills `*multipart.FileHeader` and `[]*multipart.FileHeader` fields from a multipart form too.

### Files in the input

A field of the input of a POST, PUT or PATCH handler of type `*multipart.FileHeader` or `[]*multipart.FileHeader` holds files of an upload, and so does a named type whose underlying type is one of them, such as `type Photos []*multipart.FileHeader`. The generated function then sends the input as a multipart form, so a page uploads a file the way it sends any other input:

```go
package photos

// BodyLimit and ReadTimeout hold for the requests the method handlers of this route.go answer.
const BodyLimit = 20 << 20          // 20 MiB, in place of server.body_limit
const ReadTimeout = 5 * time.Minute // in place of server.read_timeout

type UploadInput struct {
	Title  string                  `json:"title"`
	Photo  *multipart.FileHeader   `json:"photo"`
	Extras []*multipart.FileHeader `json:"extras"`
}

func (in UploadInput) Validate() error {
	if in.Photo == nil {
		return tifa.Fields{"photo": "Choose a picture."}
	}
	return nil
}

func POST(c *tifa.Ctx, in UploadInput) (Photo, error) {
	f, err := in.Photo.Open()
	if err != nil {
		return Photo{}, err
	}
	defer f.Close()
	// check the bytes, copy the file where it is kept
}
```

```tsx
const file = form.get('photo') // a File from <input type="file">
await POST({ input: { title, photo: file instanceof File ? file : null, extras: [] } })
```

- In TypeScript a `*multipart.FileHeader` is `Blob | null` and a `[]*multipart.FileHeader` is `Blob[]`. A `File` is a `Blob`, and its name goes with it. A field with `omitempty` or `omitzero` is optional.
- `*multipart.FileHeader` gets the first file of its name and stays `nil` when there is none. The slice gets every file of the name, `nil` for none. An empty text value of the name, which a browser sends for a file input left empty, counts as no file. A text value with something in it is answered with 400.
- Such an input comes only as a multipart form or a URL-encoded one (which can hold no file): a JSON body is answered with 415. Its other fields come as text, so they follow the rules of a GET input: strings, numbers, bools or slices of them, no embedded fields, and no `MarshalText` or `UnmarshalText` on the input type, on a field or on the element of a field.
- A file can only be a field of the input itself. A file in a GET or DELETE input, in a nested struct, a map or an array, in a result, in the data of `Load` or a `Layout`, or in the session fails the build. So does a file type with a `MarshalText` or `UnmarshalText` method, which TypeScript would take for a string.
- The name, the size and the `Content-Type` of a file (`in.Photo.Header.Get("Content-Type")`) are what the client sent. Decide what a file is from its bytes, and make the name you keep it under yourself, see [Uploads](guides/uploads.md).
- The form is parsed with the budget of Go's `mime/multipart`: up to 32 MiB of file content stays in memory, sharing about 42 MiB with the text fields and the names and headers of the parts, and a file that does not fit goes to a temporary file, which Tifa removes when the request ends. The peak can be higher than the budget: a file that ends on disk is first read into a growing buffer of up to 32 MiB, so one large file can take about 64 MiB for a moment, and each upload at the same time takes its own. On a machine with little memory, let only a few uploads parse at once in `OnRequest`, see [Uploads](guides/uploads.md#how-much-memory-an-upload-takes). Copy what you keep before the handler returns, and do not hand the `*multipart.FileHeader` to a goroutine that outlives the request.

### Body limit and read time of a route

`BodyLimit` and `ReadTimeout` in a `route.go` replace `server.body_limit` and `server.read_timeout` for the requests its method handlers answer, any method. The GET of a page and its navigation data keep the settings, since they read no body.

- `BodyLimit` is a number of bytes, of any integer type. A body over it is answered with 413 before the handler runs. The limit counts the whole body: the files, the other fields and the multipart framing.
- `ReadTimeout` is a `time.Duration`. The time counts from the moment Tifa matches the route, before `OnRequest`, and holds for this request. The answer then gets `server.write_timeout` after it, since it is written once the body is in. A body still coming when the time is over is answered with 408.
- Each can be a constant, which must be over 0, or a variable, such as a [option of the app](config.md#options-of-the-app): `var BodyLimit = uploads.UploadLimit` with `UploadLimit = tifa.Size("upload.limit", 4<<20, "Largest upload")`. The variable is read once when the app starts. A variable that is 0 keeps the setting, and a negative one stops the app at start.
- A `route.go` with no method handler that declares either fails the build. A page's `route.go` with a POST may declare them, and they hold for the POST.
- `c.SetBodyLimit` in `OnRequest` or a handler still changes the limit of one request, as before.

## Ctx

`*tifa.Ctx` is the request and its response. `Load`, `Layout`, the method handlers, `OnRequest` and `OnError` get it.

| Method | What it does |
| --- | --- |
| `c.Param(name)` | A path parameter, decoded, such as `c.Param("order")` for the folder `order+` |
| `c.Query(name)` | The first value of a query parameter, `""` when absent |
| `c.QueryValues(name)` | Every value of a query parameter, such as `["a", "b"]` for `?tag=a&tag=b`, `nil` when absent |
| `c.Header(name)`, `c.Cookie(name)` | A request header, a request cookie (`http.ErrNoCookie` when absent) |
| `c.Context()`, `c.WithContext(ctx)` | The request context, and a copy of `c` that carries another one, to store request-scoped values. `c` itself does not change |
| `c.Pattern()` | The path of the matched route, such as `/projects/{project}`, `""` when no route matched |
| `c.IsData()` | Whether this is a navigation data request of the browser router rather than a page load |
| `c.SetHeader(key, value)`, `c.SetCookie(cookie)` | A response header, a response cookie. Allowed in `Load` too |
| `c.JSON(code, v)`, `c.Text(code, s)`, `c.NoContent(code)` | Writes the response. Not allowed in `Load` |
| `c.Bind(&v)`, `c.BindQuery(&v)` | Decodes the body or the query string into a struct, see above. Not allowed in `Load` |
| `c.SetBodyLimit(n)` | Changes the body limit of this request (`server.body_limit` by default, or the `BodyLimit` of the route, see [Body limit and read time of a route](#body-limit-and-read-time-of-a-route)). Call it before anything reads the body, usually in `OnRequest`. Once reading has started it returns an error |
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

- UI only, no data loading. `./route.gen` is a generated module, one per folder, that exports `page` and `layout` (the TanStack routes of the page and the Layout, only when the folder has them), `PageData` and `LayoutData` (the return types of `Load` and `Layout`), `PageSearch` and `LayoutSearch` (the query parameters they read, see [Query parameters in Link and navigate](#query-parameters-in-link-and-navigate)), `Params`, and a function per method handler. Child components in the same folder also import `./route.gen`. A Layout's data comes from the `route.gen` of its folder: `layout.tsx` imports `layout` from `'./route.gen'`, and a page reads its project Layout with `layout` from `'../../route.gen'`, for example. A folder with no generated module fails the build when something imports its `route.gen`.
- The default export is the page component. Optional exports: `head` and `Error`. A 404 shows in `Error` too (see [404](#404)), and exporting `NotFound` fails the build.
- In the browser, each `page.tsx`, and each `layout.tsx` or `layout_.tsx` but the root Layout's, is a chunk of its own with everything it imports, `head` included. It loads the first time its route is needed, and the router waits for it before it shows the page or runs `head`. The HTML of a page preloads the chunks of its Layouts and route, so they arrive with the entry script. Code at the top level of the file, side-effect imports included, runs then and not when the app starts, so put setup that every page needs in the root Layout or a module it imports. See [Route modules in the browser](build.md#route-modules-in-the-browser).
- Go slices and maps are never `null`: a nil slice arrives as `[]` and a nil map as `{}`, and their TypeScript types have no `| null` (see [JSON rules](#json-rules)).
- Put components that need browser features at the module top level into a `.client.tsx` file, and load them lazily with `clientOnly` from `tifa/client`. They are left out of the SSR bundle, and the first paint shows a placeholder. Components that render but show nothing until the browser draws them belong there too, see [Components that draw only in the browser](#components-that-draw-only-in-the-browser):

```tsx
import { clientOnly } from 'tifa/client'

const OrderActions = clientOnly(() => import('./actions.client'), <button disabled>Loading…</button>)
```

- Tifa never reloads page data for you. After a write, a handler function or a `fetch` of your own, the page shows what it showed before until you ask: call TanStack's `router.invalidate()` to load the data of the current page and all its Layouts again, merged into one request. `await router.invalidate({ sync: true })` resolves once the page shows the new data. A plain `await router.invalidate()` resolves at once and the page updates in the background. See [Calling method handlers from the browser](#calling-method-handlers-from-the-browser).
- Data the router already holds follows TanStack's defaults. It counts as stale at once, so going back to a page you visited shows the cached data and loads it again in the background (`GET <url>?_tifa=…`), and a link preloaded on hover or focus is reused for 30 seconds.

### Query parameters in Link and navigate

The query parameters a `Load` or `Layout` function reads have types in the browser. `route.gen` exports them as `PageSearch` and `LayoutSearch`, and the route gets them, so TanStack's `Link`, `navigate` and `useSearch` check them:

```go
func Load(c *tifa.Ctx) (Products, error) {
	return products.List(c.Query("sort"), c.QueryValues("color"))
}
```

```ts
// in route.gen.ts
export type PageSearch = {
  "color"?: string | string[]
  "sort"?: string | string[]
  [key: string]: string | string[] | undefined
}
```

```tsx
<Link to="/products" search={{ sort: 'price', color: ['red', 'blue'] }}>…</Link>
<Link to="/products" search={{ sort: 2 }}>…</Link> // type error: the value is a string
navigate({ to: '/products', search: (prev) => ({ ...prev, sort: 'name' }) })
const search = page.useSearch() // PageSearch, with the parameters of the Layouts above
const sort = [search.sort].flat()[0] // the first value, as c.Query reads it
```

- A key can come more than once in a URL, so a value is a `string` or a `string[]` when you read it, whether Go reads it with `c.Query` or `c.QueryValues`. `c.Query` in Go takes the first value. Writing one, a single string is enough. Every key may be left out.
- Other keys are open: a page may keep parameters of its own in the URL that Go does not read, such as the open tab of a panel. They are strings or arrays of strings too. So a misspelled key is not a type error, only a wrong type for a key Go reads is.
- Only strings go in. The router writes every value as text, and Go reads text, so `'2'`, not `2`, and `'1'` or `'true'` as your Go code compares it.
- The types come from the names Go reads, also through helpers and other packages, and each route has those of its own `Load` or `Layout`. TanStack adds those of the Layouts above. A `Load` that reads none gets no type, and its route takes any keys, as before.
- Nothing runs in the browser to check the values: the route gets the type only (`searchTypes` returns `undefined`, and TanStack then reads the query as it is).

### Showing HTML the app trusts

A field of type `tifa.HTML` holds markup that the page shows as it is, such as an article your build turned from Markdown into HTML. Show it with the `HTML` component of `tifa/client`:

```go
type Post struct {
	Title string    `json:"title"`
	Body  tifa.HTML `json:"body"`
}
```

```tsx
import { HTML } from 'tifa/client'
import { page } from './route.gen'

export default function PostPage() {
  const post = page.useLoaderData()
  return <HTML value={post.body} as="article" className="prose" />
}
```

- The markup goes into the page without escaping. Use `tifa.HTML` only for HTML you wrote or made yourself. HTML from users must be cleaned with a sanitizer first, and only then converted to `tifa.HTML`.
- In TypeScript the value has the type `HTML`, not `string`: TypeScript refuses it where a string goes, such as in text or with `.length`. Show it with `<HTML>` only. TypeScript does not stop it in `dangerouslySetInnerHTML`, and on a page the server rendered it holds only a marker there. `as` picks the element that holds the markup: `div`, `article`, `section`, `main`, `aside`, `header`, `footer`, `nav`, `figure`, `blockquote`, `li`, `td`, `span` or `p` (`div` by default). These hold ordinary content. Elements whose content runs or is not markup, such as `script`, `style`, `iframe` or `textarea`, would make cleaning the HTML useless, so TypeScript refuses any other name and `<HTML>` throws for it. The other props go to that element, except `children` and `dangerouslySetInnerHTML`, which `<HTML>` leaves out also when a spread brings them.
- A page the server renders carries each value once: in its markup. Its data for hydration holds a small marker in its place, and `<HTML>` reads the markup back from the page. So an article is not sent twice. Navigation data and pages with `SSR = false` carry the string.
- So render `<HTML value={...}>` on the server every time it shows a value of the page: not only after an effect, a click or a state only the browser has, and not in a `.client.tsx` component. Otherwise the browser throws `tifa: this tifa.HTML value came with the page, which carries it only in the markup the server rendered, and the server rendered no <HTML> for it here`.
- Links in the markup work like a `Link`: a click on a link to a page of the app shows that page through the router, without loading the whole document again, and a hover or a focus of 50 ms, or a touch, preloads it. The browser follows the link itself for a click with a modifier key or another mouse button, a link with a `target` other than `_self`, with `download` or with `rel="external"`, a link to another site or scheme such as `mailto:`, a `#hash` of the page that shows, a public file, a path under `/assets/` or `/__tifa/`, and a path with no page, such as an API route. Write `rel="external"` on a link of your site that should load the whole page, such as one to a service on the same domain that is not Tifa. `<HTML>` handles only the links inside its own element. Your `onClick` and other handlers on `<HTML>` run first, and one that calls `event.preventDefault()` keeps the router out.
- `tifa.HTML` cannot be a map key, a handler input or part of the session. Take a `string` there and convert it after you sanitize it. A `Load` or `Layout` cannot return a `tifa.HTML` on its own: put it in a struct field.

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
- GET and DELETE send the input in the query string, the other methods as a JSON body, or as a multipart form when the input holds files (see [Files in the input](#files-in-the-input)). The result is typed from the Go result, with `| undefined` for a redirect (below): `func(c, in) (Out, error)` gives `Promise<POSTOutput | undefined>`. `func(c, in) error` gives `void`. `func(c) error` writes its own response, so its function gives back the `Response` as it is, also for an error status.
- A call only sends its request and, when the answer carries a session, applies it (see below). It never reloads the data of a page, on success or on failure, for any method. When the page should show what the write changed, call `router.invalidate()` after it (see the example below).
- An error answer throws `TifaError` with `status`, `message` and `fields` (the messages of a `tifa.Fields`, empty otherwise). A network failure throws as `fetch` does. A call from a page of an older build than the server fails with status 409, see [Pages open while you deploy](#pages-open-while-you-deploy).
- When the handler or `OnRequest` returns `tifa.Redirect(url, status)`, the browser goes to that URL: through the router for a URL of this site, without reloading the page, and as a whole page load for another site. The new page shows what the router has for it and loads what is missing, as after any navigation. A redirect to the page that is showing stays where it is and loads nothing. It counts as the same page when the path, the hash and the query values match, whatever the order and encoding of the query. The promise resolves once the navigation has finished, to `undefined`, also for a handler with a result. Whether a call is redirected is only known from its answer, since `OnRequest` may redirect any request, so the result type of every handler function with a result includes `undefined`. The function of a handler that writes its own response gets the `Response` of the redirect (`ok`, with the status in `X-Tifa-Redirect`). A relative URL is resolved against the URL of the handler, not of the page. A 3xx the handler wrote itself through `c.Writer()`, or one a proxy wrote, cannot be read in the browser and rejects the promise with an `Error`.
- A call that signs someone in or out brings the new session in its answer, and the copy of the session in the browser follows (see [Sessions in the browser](sessions.md#in-the-browser)).
- Call these functions from event handlers or effects. Calling one while the page renders on the server throws.
- `route.gen` also exports `POSTInput`, `POSTOutput` and so on for the types of each handler that has them.

The error that `tifa.Redirect` returns is a `*tifa.RedirectError` with the fields `URL` and `Status`. In `OnRequest` or a test, recognize it with `errors.As`.

To go to the page of what a handler created, return a redirect from the handler, or return the result and navigate yourself. A call reloads nothing, so the page you leave is not loaded again just before. A write the page itself shows stays on screen after the call. Ask for it with `router.invalidate()`:

```go
func POST(c *tifa.Ctx, in AddInput) error {
	todo, err := AddTodo(in.Title)
	if err != nil {
		return err
	}
	return tifa.Redirect("/todos/"+todo.ID, http.StatusSeeOther)
}
```

```tsx
const router = useRouter() // from @tanstack/react-router
// in the event handler, where POST returns (Todo, error):
const todo = await POST({ input: { title } })
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

- `Layout` and `LayoutSSR` go in `route.go`, next to `Load`, `SSR`, `RenderCache` and the method handlers of the same folder. Declaring one of them in any other file of the package, a `layout.go` for example, fails the build. A `route.go` with only a `Layout` is no API route.
- A Layout's `c.Param` may only read parameters of its own folder path. `Layout` in `app/projects/project+/route.go` may read `project`, not `order`. A page's `Load` is held to its own path the same way.
- When you switch between pages under the same Layout, the Layout component is not remounted and keeps its state. When a path parameter the Layout function reads changes (for example, you move to another project), the Layout remounts and its state resets. When a query parameter changes, even one it reads, the Layout loads its data again and renders it, but is not remounted and keeps its state, as TanStack Router does by default. So a filter keeps the view the visitor chose. State a component set up from the data of `Load`, such as with `useState(data.x)`, is not reset by a new query either. Where it should follow the new data, give the component a `key` from the value, or update the state in an effect. Pages work the same way.
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

- A page's head comes from the `head` return values of the matched routes: `title` and meta tags with the same name come from the deepest route that has them, which keeps every one of them it writes (a `theme-color` for each `media`, several `og:image`), and link, style and script tags are deduplicated in route order. The framework always adds `<meta charset="utf-8">` and a viewport meta, and a route's `name: 'viewport'` meta replaces the latter. Tag attributes are written the way React DOM writes them on the server: `className`, `htmlFor`, `httpEquiv`, `crossOrigin` and so on become their HTML names. Event attributes starting with `on`, invalid attribute names, `null`, `undefined` and functions are not written. Boolean attributes such as `async` and `defer` are written with an empty value when true. For other attributes, boolean values are written as strings only on `data-` and `aria-` attributes. `javascript:` links are replaced. Head tags do not support the `style` attribute, and using it makes rendering fail. `htmlAttributes` follows the same rules. The content of style and script tags is written as is. Only the `<script` and `</script` (in style, `<style` and `</style`) that would end the element are escaped, the way React DOM does it. After browser navigation and after `router.invalidate()`, the head updates to match the new routes.
- A `head` can also return `htmlAttributes`, the attributes of `<html>` for its pages, such as a theme class or a `data-` attribute. They are merged over the root Layout's `htmlAttributes` one attribute at a time: the deepest route that gives an attribute wins, `undefined` leaves it to the routes above, and `null` takes it away. Attributes are matched by the name React DOM writes, so `className` in one route and `class` in another are the same attribute. `class` is replaced as a whole, never merged with a value from elsewhere. `style` is not supported, as on the tags of the head. They are written by the same rules as the tags of the head. After browser navigation and after `router.invalidate()` they follow the new routes too. In the browser Tifa writes an attribute of `<html>` only when the value it wants for it changes, and takes away only attributes it wrote. So what a script of the page sets, such as the `dark` class of a dark mode script or the `lang` a translation tool changes, stays through hydration and navigation while the routes want the same value of that attribute. When that value changes, because a route sets another `class` or a navigation leaves it and the root Layout's `class` comes back, Tifa writes it and the class the script added is gone. Keep such a theme in an attribute no route sets, such as `data-theme`, or have the script add it again. A page without SSR gets these attributes in the browser.

```tsx
// app/guide/layout.tsx. Layout in route.go returns the theme of the guide.
export const head = ({ loaderData }: { loaderData?: LayoutData }) => ({
  htmlAttributes: { 'data-theme': loaderData?.theme },
})
```

- The routes that count are those whose head is on the page. A 404 or an error below the Layout keeps the Layout's attributes, also for a URL below the Layout that no page has, since its 404 shows inside the Layout (see [404](#404)). When the Layout itself fails, its `head` runs without data, so the example above leaves the root Layout's `data-theme`.
- On a site with languages Tifa writes `lang` itself, the language of the page. A `lang` in `htmlAttributes` fails the build there (see [A site in several languages](#a-site-in-several-languages)).
- `htmlAttributes` must be an object, and `pnpm typecheck` reports any other value. At run time such a value fails like an error the `head` throws: the server render fails as an unexpected error (`OnError`, or a 500), with the message in the log, and on the page under `tifa dev`. In the browser, after a navigation or on a page without SSR, the router writes the error to the console and drops the whole head of that route.

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
    _locales/                         zh-CN.json and en-US.json
    route.go  page.tsx                the storefront
    cart/
      route.go  page.tsx              inside the storefront
```

- The pages below it, also those further down, do not render the Layouts above it, and their `Layout` functions do not run for them: not for a page, not for the navigation data request of the browser, not for the 404 of a URL below it that no page has. So their data never reaches these pages.
- The `Layout` in the `route.go` of the same folder gives it data as usual. The folders below it nest as usual: a `layout.tsx` further down shows inside it and its `Layout` runs. A `layout_.tsx` further down drops it in turn, with everything above it.
- What belongs to the document still applies: the root Layout's `htmlAttributes` and `documentHead`, and `OnRequest`. What belongs to the Layouts above does not: their `head`, their `Error` and their `LayoutSSR`. When the `Layout` of a `layout_.tsx` fails, or returns `tifa.ErrNotFound`, no Layout is above it to show the error, so the framework's default page shows. Its own `Error` shows for what fails below it, as with any Layout. To show your own page when that `Layout` fails, put the `layout_.tsx` one folder higher, with an `<Outlet />` and an `Error`, as the shop does in `app/store`.
- In the browser, a move between a page below it and a page outside it swaps the whole frame, and the Layouts of the new page load their data when the router has none of it yet.
- The build fails for a `layout_.tsx` in `app/`, which has no Layout above it, for `layout.tsx` and `layout_.tsx` in the same folder, and for `tifa.Use` of a Layout that a `layout_.tsx` drops, from a page or Layout below it. It also fails for a Layout whose pages are all below a `layout_.tsx`, since it wraps none of them.
- React Router and TanStack Router also end a name with `_` to keep a route out of a Layout, but only out of the Layout of the segment before it: the root route and the Layouts further up still wrap it. Tifa's `layout_.tsx` drops every Layout above it, the root Layout too.

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
- A component that throws while the page renders on the server is an unexpected error: `OnError` sees it once and picks the status (500 without `OnError`) and the public message. The page then renders again with that error in place of the page, as a `RouteError`, so the first `Error` from the page up shows it inside the Layouts. If that render fails too, because the `Error` or a Layout around it throws, the error moves one route up, in place of the Layout the page is in, and so on up to the outermost Layout, the root one or a `layout_.tsx`. Only when the error in place of that Layout fails too does the browser get a plain error page from Go with the status and the public message. The browser hydrates the page with the error where the server put it, and does not run the route that threw again. Such an answer is never cached. The stack trace shows only under `tifa dev`, over the page with the `Error`, and the page reloads once you save a fix (see [`tifa dev`](dev.md#when-a-page-fails-to-render)).
- A server render that uses up `ssr.vm_memory` or runs past `ssr.timeout` (see [config.md](config.md)) stops right there. No `catch`, `finally` or promise handler in your code or in a library runs for it, so a page cannot catch it to show something else. The VM is replaced with a fresh one. A render that used up `ssr.vm_memory` renders again with the error, as above, on a fresh VM. A render that ran past `ssr.timeout`, or found every VM busy, does not: Go answers with its plain error page at once. All the renders of one request share one `ssr.timeout`, counted from the first, and a render with the error that runs out of it ends the tries. Code that recurses without end gets `RangeError: Maximum call stack size exceeded`, as in Node, which a `catch` can take (see [stack depth](config.md#stack-depth)). When nothing catches it, it fails the render like any other error.
- The page that shows the error gets the head of a page whose `Load` failed. The route the error replaces runs its `head` without data, the routes below it add no tags, and the `head` of every route above it runs again over the matches as they now are, so `status` tells it that a route failed (see [A site in several languages](#a-site-in-several-languages)). Tags and `htmlAttributes` that a route gave for the page that did not render are not on the error page. The browser works out the same head when it hydrates.
- Once the browser has taken over, a component that throws shows the nearest `Error`, the same as a failed `Load`.
- The document shell stays complete whatever fails, so the page still loads its scripts and styles.

## A site in several languages

Put a `_locales` folder in a folder of `app/`, with one `<language>.json` file per language, and the pages and API routes in that folder and below it are offered in those languages. The storefront of [examples/shop](../examples/shop) is `app/store/` with `_locales/zh-CN.json` and `_locales/en-US.json`.

```yaml
# tifa.yaml
i18n:
  default: zh-CN
site:
  url: https://shop.example.com
```

- The default language has no prefix, any other is the first segment of the URL: `/store/cart` is in `zh-CN` and `/en-US/store/cart` in `en-US`. `/zh-CN/store/cart` answers 308 to `/store/cart`. A route outside every folder with `_locales`, such as `/projects`, has one URL, and a language in front of it is a 404. `i18n.prefix: always` puts the default language in the URL too.
- A route does not name its language in its folder: no `lang+` folder, no list of languages and no Layout that checks one. `c.Language()` gives the language in Go, in a `Load`, a `Layout` or a handler, and `useLanguage()` from `tifa/client` gives it in React.
- `<html lang>`, the `hreflang` links, `x-default` and a canonical link are written by Tifa. A site with several languages needs `site.url` for them.
- Links stay as you write them: `<Link to="/store/cart">` becomes `/en-US/store/cart` in an `en-US` page. A link to another language, or to a route outside the folder, loads a new page, since one page holds one language.
- Translate with the i18next adapter `tifa/i18n`, or any library that takes its language from `useLanguage()`. Load the translations synchronously: bundle them with the app and set the library up without waiting. The server render waits for nothing and reads no files, so messages that arrive later are missing from the HTML, and the browser would hydrate with other text. Format numbers and dates with the language too (see [Formatting so hydration matches](#formatting-so-hydration-matches)).
- A whole-page cache keeps each language under its own URL.
- Tifa does not pick a language for a visitor and has no fallback language.

The [guide](guides/i18n.md) goes through all of it: translating, the language switch, translated slugs, Markdown in each language and what fails the build.

## SSR and cache

```go
const SSR = false              // the page, default true
const RenderCache = false      // the page, turns the render cache off, default on
const Cache = 10 * time.Minute // the page, the same answer for everyone for 10 minutes, default off
const LayoutSSR = false        // the Layout of the folder, default true
```

`SSR`, `RenderCache` and `Cache` apply to the page of `page.tsx`. In a `route.go` without one they fail the build. `RenderCache` can only be `false`: the render cache is on by default, so you never write `true`. `Cache` is a `time.Duration` constant of at least a second, see [The whole-page cache](#the-whole-page-cache). Write the names with the capitals shown. A constant named `ssr`, `cache`, `rendercache` or `layoutssr` fails the build, since it would be skipped. `Cache = true`, from when `Cache` was the switch of the render cache, fails the build too: delete it.

- `SSR = false`: middleware and every `Load` run as usual, but QuickJS is skipped. The response is a minimal document with the data, and the browser renders it. `LayoutSSR = false` applies to every page the Layout wraps.
- The render cache is on for every page (see [The render cache](render-cache.md) for why it is safe for signed-in pages and what it does not cover). Each request runs its middleware, its `Load`s and its Layouts. The rendered HTML is then kept by build version, SSR bundle, URL, the data of every `Load` and Layout, and the session the browser gets, in a cache of `cache.size` (see [config.md](config.md)). When none of these changed, the HTML is returned without a render. The answer says which: `X-Tifa-Render` is `vm` for a render of this request, `cache` for a hit and `shared` for a render another request started (see [Page requests](build.md#page-requests)). `cache.size` 0 turns the cache off, and shared renders stay.
- Components must not put a value into the HTML that changes on every render, such as `Date.now()` or `Math.random()`. Hydration needs this too, and the cache would freeze such a value for later visitors. Under `tifa dev` every page is rendered a second time with the clock 36 hours on, and when the two differ the terminal and the dev overlay say so, show the first place where they part and list the places in your code that read the time or a random number. The page is then not cached until it is fixed. If a page has to show such a value on purpose, such as the time of the server, write `const RenderCache = false` in its `route.go`. That page renders for every request. The check does not run in a built app.
- A request that carries a render clock (`tifa.WithRenderClock`) skips the cache: it neither reads nor stores a page, because its HTML depends on a time the key does not hold.
- When requests miss the cache at the same time with the same build version, URL and data, such as a burst of visitors right after a deploy, they share one render. Each request still runs its middleware and `Load`s, keeps the headers they set, such as `Set-Cookie` and `Vary`, and gets the encoding it accepts. This also holds with `cache.size` 0, for a page too large for the cache, and under `tifa dev`. Requests with other data or another session render on their own, and a page with `RenderCache = false` renders for every request.
- When that shared render fails, every request that waited for it gets the same error and goes to `OnError`, a panic included. Nothing is cached, so the next request renders again. A request whose client goes away stops waiting at once and its connection is cut, with no answer sent: `next(c)` in `OnRequest` returns an error that matches `context.Canceled`, but `OnError` is not called and nothing is logged. The render goes on for the others. A request whose own deadline passes while it waits fails like any other error. Once no request waits, a render still waiting for a VM is dropped, and one that already runs finishes and goes into the cache. If it fails then, no request reports it to `OnError`, so the error is logged.
- Every page response carries an ETag, a hash of the same things as the cache key. A second request with `If-None-Match` gets 304 when none of them changed, without a render and before the cache is asked. The ETag and the cache are separate: the ETag saves the transfer to a browser that has the page, the cache saves the render for everyone else. A page with `RenderCache = false` and a request with a render clock get no ETag, since their HTML is not a function of those things, and always render.
- A page does not carry the time its data was loaded, so the same data always gives the same HTML. Instead the answer of a page (a render, a 304, a 404 page of the app) carries `Server-Timing: tifa-age;dur=<ms>`, how old its data may be, from the start of its first `Load`. Only an answer that no cache may reuse without asking the server again carries it: its `Cache-Control` has `no-cache` or `no-store`, as the default `private, no-cache` does. A page of the whole-page cache, or one whose `Cache-Control` you set to let caches keep it (such as `public, max-age=60`), does not carry it, since a cache would hand out the same age later. The browser router takes the time the browser sent the request, less that age, as the time the data was loaded, which `staleTime` and `gcTime` count from. It sets that time on the matches of the page just before it first loads or preloads, so until then their `updatedAt` is 0, as the server rendered it, and a component that shows it hydrates without a mismatch. Where the answer has no age or the browser cannot read it (Firefox over plain HTTP, a page from the back and forward history), the data counts as old and loads again when you come back to the page. Navigation data does not carry it: the router dates what it fetches itself.
- Every answer of the app (pages, 304s, 404 pages, navigation data, method handlers, redirects) is sent with `Cache-Control: private, no-cache` unless you set `Cache-Control` yourself. The kept pages of a page with `Cache` are the exception, see [The whole-page cache](#the-whole-page-cache). A CDN or proxy does not store it, since the page may be meant for one visitor. The browser may keep it and asks again with the ETag, so an unchanged page ends in a 304. To change it, set the header in `OnRequest`, a `Load`, a `Layout` or a method handler with `c.SetHeader("Cache-Control", "...")`. Your value is sent as it is, except on navigation data (the requests with `_tifa`), which is never public: a value that is not `no-store`, or `private` with `no-cache`, is replaced by `private, no-cache`. JSON errors (including 405 and the 503 while the server shuts down), the plain error page from Go, the page with an error that the server renders again, and the busy answer are `no-store`, unless you set `Cache-Control` before the error. A 404 page that the app renders keeps the default. Files under `/assets/` keep their long cache, and files from `public/` keep `no-cache` (see [Page requests](build.md#page-requests)). The render cache only keeps the render inside the process and does not change the header.

### The whole-page cache

```go
// app/blog/slug+/route.go
const Cache = 10 * time.Minute
```

A page with `Cache` is answered to everyone alike for that long, from memory, without running its `Load` or rendering it. Use it for pages that are public and that many visitors read: articles, docs, product and landing pages. A visitor sees a change up to `Cache` late, unless the app expires the page after the write (see [Making a page again after a write](#making-a-page-again-after-a-write)).

- **Everyone is anonymous.** The GET and HEAD of the page, and the requests the browser sends for its data when you navigate to it, lose all request headers but a short list before `OnRequest`: `Host`, `Accept-Encoding`, `If-None-Match`, `If-Modified-Since`, `Cache-Control`, `Pragma`, `Content-Length`, `Connection`, `Origin` and `Sec-Fetch-*`. `Cookie` and `Authorization` go, and so do `Accept-Language`, `User-Agent`, `Referer`, `Forwarded`, `X-Forwarded-*` and a header that a proxy or login gateway sets such as `X-User-ID`, so none of them can reach a `Load` through `OnRequest` and the context. `tifa.Session` returns nil, `c.Cookie` finds nothing, `c.Header` and `c.Request().Header` have only the list, and the request is the same for `c.Request()`. `tifa.SetSession` and `tifa.ClearSession` return an error that says sign-in and OAuth callbacks belong on routes without `Cache`. A page can only be shared if what it says does not depend on who asks. A `Set-Cookie` on the HTML of such a page is removed, since a cookie in a shared answer would reach every visitor. The answer to a navigation (the request with `_tifa`) is for one browser and keeps its `Set-Cookie`, and it carries the session of that browser (see [Sessions](sessions.md#in-the-browser)). Method handlers of the same URL, a POST say, are not anonymous and not cached.
- **The edge of anonymous.** What Tifa guarantees is the request headers: on a page with `Cache`, `OnRequest`, `Load` and `Layout` see a request with only the headers of the list, and `tifa.Session` is nil. A `net/http` middleware set with `tifa.SetHTTPHandler` runs outside Tifa and gets the request as it came, and what it puts into the context of the request reaches `OnRequest` and the `Load` unchanged. Do not recognize visitors there: do it in `OnRequest` on routes without `Cache`. The address of the connection and the TLS state also remain, and values an outer middleware puts into the context and headers on the list (such as `Origin`) can still tell requests apart. None of them may be used to build personalized shared content.
- **The build checks it.** When the `Load` of such a page, the `Layout` function of a Layout around it, or a function they call reads `c.Cookie`, `c.Header` or `tifa.Session`, the build fails with the line, since the read can only see an anonymous request. For `c.Header` the message says which few headers are there. A page that needs to know who is signed in does not use `Cache`. Put what depends on the visitor into the browser, which can read it after the page has loaded (see [In the browser](sessions.md#in-the-browser)). Keep the Layouts around a page with `Cache` to data that is the same for everyone.
- **What `OnRequest` can do.** It runs for every request, hits included, and sees the anonymous request. Use it for what depends on the path: redirects, security headers, logging, counting, rate limits by address. It cannot see who is signed in, so an `OnRequest` that redirects by login state belongs to routes without `Cache`, and one that sets a cookie when it does not see one would set it on every navigation. Cookies it sets on the HTML are removed.
- **Languages.** A page in several languages is cached by URL, so each language has its own entry. An entry point that picks the language from a cookie or from `Accept-Language` and redirects is a GET handler on a route without `Cache` (a page with `Cache` cannot read either), and the pages it leads to carry the language in their URL (see [A site in several languages](#a-site-in-several-languages)).
- **The key** is the scheme, the `Host` header (`X-Forwarded-Host` is not trusted), the path and the query without tracking parameters, the rest sorted by name. `cache.ignore_query` lists the tracking parameters (`utm_*`, `gclid`, `fbclid` and the like, see [config.md](config.md)). A parameter that a `Load` on the page's chain reads is always kept. The page is rendered for the same cleaned URL, so what the visitor sees does not depend on a parameter the key ignored.
- **What is kept.** GET and HEAD share one entry. Only answers with status 200, 301, 302 and 308 are kept. An answer is not kept, and the requests for that URL skip the cache for two minutes so they do not queue, when `Cache-Control` has `private`, `no-store` or `no-cache` (set by `OnRequest` or a `Load`), when `Vary` has anything but `Accept-Encoding`, and for any other status, a 404 included. A forced refresh (`Cache-Control: no-cache` or `Pragma` in the request) does not bypass the cache.
- **When the time is over** nobody gets the old page. The next request makes a new one and the others wait for it. If the data did not change, the render cache answers and nothing is rendered again.
- **When a content entry comes out at its date**, every page made before that moment counts as over its time, so a list of posts shows the new one at once (see [Drafts and dates](content.md#drafts-and-dates)).
- **When making the new page fails**, because a `Load` returns an error, the render fails or runs out of time, or all VMs are busy, the old page is served for up to a day after its time ended. After that the error is answered. A `Load` that returns `tifa.ErrNotFound`, a redirect or another `tifa.Error` below 500 is an answer, not a failure, and it replaces the old page.
- **Headers.** The headers a `Load` or `Layout` sets (not `Set-Cookie`, `Cache-Control`, `Vary`, `Age`, `ETag` or `Content-Length`, which Tifa writes) are kept with the entry and sent with every answer made from it. A `Cache-Control` of `private`, `no-store` or `no-cache`, or a `Vary` beyond `Accept-Encoding`, from a `Load` or `Layout` keeps the page out of the cache and goes out with the answer, over a `public` that `OnRequest` set. A kept page carries `Cache-Control: public, max-age=0, s-maxage=<Cache in seconds>, stale-if-error=86400` and `Age`. The browser asks every time and gets a 304 from the entry when the ETag matches, so a deploy shows at once. `Age` counts the seconds since the page was made and a CDN in front subtracts it from `s-maxage`, so the two together never keep a page longer than `Cache`. An old page sent because making the new one failed has an `s-maxage` no larger than its `Age`, so a CDN does not count it as fresh. `Cache-Status: tifa; hit`, `tifa; fwd=miss` or `tifa; fwd=stale` says where the answer came from, and `X-Tifa-Render` says as before. Navigation data is still `private, no-cache`. For what to set in a CDN, see the deployment notes of the Tifa skill.
- Entries share `cache.size` with the render cache, and share the page with it too. A page is kept in one form, gzip with `server.compress` on, and goes out in the encoding each visitor takes (see [Compression](build.md#compression)). `cache.size` 0 keeps nothing, and requests that miss at the same time still share one make.
- **`cache.whole_page: false`** in `tifa.yaml` (or `--cache-whole-page=false`) turns the whole-page cache off for the whole app and leaves the render cache on. A page with `Cache` is then answered like any other page: each request runs its `Load`s with its own headers, and the answer has the default `Cache-Control`. The build checks above still hold, so the page shows the same. Use it to measure or debug the pages without the cache.

#### Making a page again after a write

When a write changes what pages with `Cache` show, call `tifa.ExpirePages` with their paths once the write succeeded. The next request for each runs its `Load`s and renders it, as when its time is over:

```go
// app/admin/posts/post+/route.go
func PUT(c *tifa.Ctx, in PostInput) (posts.Post, error) {
	p, err := posts.Update(c.Context(), c.Param("post"), in)
	if err != nil {
		return posts.Post{}, err
	}
	tifa.ExpirePages("/blog/"+p.Slug, "/blog", "/")
	return p, nil
}
```

- Tifa does not guess what a write changed. Name every page that shows it: here the post, the list of posts and the home page that shows the latest ones, and in a site in several languages, the page of each language.
- A path is the path of the URL, decoded as `c.Request().URL.Path` has it: `/blog/café`, not `/blog/caf%C3%A9`. It expires the page for every query string, host and scheme, so `/blog` also expires `/blog?page=2`. Paths match exactly: `/blog` and `/blog/` are two pages, and neither expires the pages below it.
- A request that started before the call may still get the old page. A page made from data read before the call is not kept, and requests after the call do not wait for it.
- When the data did not change, the new page is the old one: the render cache answers without a render, and a browser that has the page gets a 304 for its ETag.
- When making the new page fails, the old page stands in as when its time is over, up to a day after its `Cache` time ends.
- Call it from anywhere in the program: a method handler, a background job, `OnStart`. Before the server starts and after it stops it does nothing. A path that does not start with `/` makes it panic.
- It expires the pages in the memory of this program only. A CDN in front keeps its copy until its `s-maxage` runs out. With several copies of the program, each has its own cache, and the call reaches only the copy that makes it.

### User-independent pages and private data

A full page is a user-independent page plus private data. Tifa splits by the kind of data, not by component:

- **Data that is the same for everyone.** `Load` and Layout see no user, so the page can be kept in the whole-page cache and in a CDN.
- **The session.** Who is signed in and a small `Exposed` part of the session, carried by the session system (see [Sessions](sessions.md)). It is written into the page, and the browser keeps a copy.
- **Other private data.** A cart, recommendations, an unread count. It can be large, and the browser may write to it. Put it behind an API route of your app and fetch it from the browser.

Other systems draw the same line: HTTP caches call it `public` against `private`, Magento calls it private content, and donut caching keeps the shared page whole and fills in the holes.

For the third kind, make an API route (a `route.go` with a `GET` handler and no `page.tsx`). Its generated `GET` function is typed and works from the browser. Call it when the page needs the data: after the page has loaded, when a panel opens, or on hover to fetch ahead. The generated functions plug straight into TanStack Query if the app wants a cache. `examples/shop/app/store/mine.ts` is a worked example: the storefront pages are cached and the same for everyone, and this module calls the `GET` of `/store/{lang}/mine` after a page has loaded to fill in the cart and the products the visitor looked at last, and calls it again after a write.


## Pages open while you deploy

A visitor can keep a page open while you deploy a new version, so the page is from the old build and the server is the new one. The page sends the build it was rendered with along with its data requests (in the `_tifa` query parameter) and its handler calls (`X-Tifa-Build`), and the server answers 409 for another build. What the visitor sees:

- Clicking a link: the browser reloads the current page, and the new build shows it. One reload, whatever number of requests got the 409.
- Calling a handler function: the call fails with a `TifaError` of status 409 and the browser reloads the page. The call is refused before the handler runs and never sent again, because the new page may mean another input by it. The visitor does it again on the new page. A 409 that your own handler returns has no `X-Tifa-Build` header and reloads nothing. A call without the header, from curl or another program, is not checked.
- Loading code: a lazy route or an `import()` asks for a chunk that the new build no longer has, and the browser reloads the page (this is Vite's `vite:preloadError`).

Each reload is made once for a build. When the page it brings back still disagrees with the server, which happens when a cache hands out the old page again, it does not reload a second time. The data request fails with the 409, which the `Error` of the route shows, the call throws it, and a failed import goes to the `Error` of the route or to the code that imported. The tab remembers the reloads in `sessionStorage`, and a page reloads only after it has written the mark and read it back. When the browser blocks the storage or does not keep what is written, the page does not reload: the failures above show as they are, with the message "the application was updated, reload the page", and the visitor reloads by hand. A reload that cannot be remembered could repeat for as long as the build disagrees.

A page whose HTML is old and whose entry script is gone cannot be fixed by the page: no code runs. Tifa sends pages with `Cache-Control: private, no-cache`, so a CDN does not keep them, and this can only happen when your `OnRequest` lets a CDN cache pages. Then clear the pages in the CDN when you deploy.

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
  return <SalesChart days={days} />
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

### Warnings in `tifa dev`

Under [`tifa dev`](dev.md), a render that calls an Intl constructor or a `toLocale*` method without a locale, or a date one without a `timeZone`, logs a warning with the place of the call in your sources and its stack:

```text
2026/10/16 08:00:00 tifa: ssr: Number.prototype.toLocaleString without a locale at app/orders/order+/items.tsx:12:7. The server formats in en-US and UTC, a browser in the language and time zone of its user, so the text can differ from the HTML and React reports a hydration mismatch. Pass the locale and, for a date, the timeZone, from data Load returns, as in toLocaleDateString('zh-CN', {timeZone: 'Asia/Shanghai'}), or format in a useEffect.
    at Price (app/orders/order+/items.tsx:12:7)
    at ...
```

Each place is logged once, and again after `tifa dev` reloads the SSR bundle. The warning does not change the render. A built program does not check. A call inside a package names the package's file first, and your code further down the stack.

### Cost

A VM carries only stand-ins for Intl, about 10 KB, until one of its renders uses it, which adds about 30 to 45 KB to that VM. A VM of the example shop takes about 11 MB, so that is under 1% of it. Under `tifa dev` every VM pays this at once, because the warnings need Intl installed. Each language is read into memory once per process, 100 to 180 KB, plus about 1 MB shared by all languages the first time any is used. The locale data makes the program about 1.7 MB larger, and the time zone database the generated `main.go` embeds about 450 KB more. The locale data comes with a license notice to ship with the program, see [Notices to ship with the program](build.md#notices-to-ship-with-the-program).

## Temporal in server rendering

Pages can use `Temporal.Now`, `Instant`, `ZonedDateTime`, `PlainDate`, `PlainTime`, `PlainDateTime`, `PlainYearMonth`, `PlainMonthDay`, `Duration` and `Date.prototype.toTemporalInstant`, including at module load. QuickJS provides the methods V8 ships, IANA time zones and the calendars supported by Intl. Temporal uses the same embedded tz 2026e data as Intl, without reading the machine's zoneinfo.

`Date.now()`, `new Date()`, `Date()`, `Temporal.Now` and Intl date formatting without a date share the VM clock. By default it stays live during a render. Go can supply a clock for one render with `tifa.WithRenderClock`, see [Render clock](#render-clock). `Temporal.Now.instant().epochMilliseconds` and `Date.now()` sample that clock separately, so they can differ if time passes between calls. `Temporal.Now.timeZoneId()` is `UTC`, the default zone of Intl, and the other Now methods use UTC when no zone is passed. The process's Go time zone stays unchanged.

`toLocaleString` uses Intl. Pass an explicit locale. A plain date or time has no zone and ignores `timeZone`, an `Instant` uses the formatter's zone, and a `ZonedDateTime` uses its own zone. Use an explicit zone when constructing a zoned value or formatting an instant. ISO `PlainYearMonth` and `PlainMonthDay` need `calendar: 'iso8601'` when formatting.

For hydration, return ISO strings and the locale and zone from `Load` or a Layout, then reconstruct the same Temporal values on both sides. For example, with `day: '2026-10-22'` and `locale: 'en-US'`:

```tsx
const { day, locale } = page.useLoaderData()
return <time>{Temporal.PlainDate.from(day).toLocaleString(locale, { dateStyle: 'long' })}</time>
```

This renders `October 22, 2026`. Do not put `Temporal.Now` results into hydrated HTML: the browser runs later and its default zone can differ. Compute a fixed value in `Load`, or show the current time after hydration in an effect. Browser locale data can differ, as described under Intl. Tifa provides Temporal only to the server VM, not a browser polyfill or TypeScript declarations. Browsers that lack Temporal need a polyfill loaded before the page code, or the page must avoid it there.

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
func OnStart(ctx context.Context) error         // before listening: open the database, logs, etc.
func OnShutdown(ctx context.Context) error      // after an exit signal, once in-flight requests finish
func OnRequest(c *tifa.Ctx, next func(*tifa.Ctx) error) error // the only middleware, call next(c) to continue
func OnError(c *tifa.Ctx, err error) tifa.Error // unexpected errors: report, return an error users may see
```

- `OnStart` gets the context of the program: it is done when the program gets SIGINT or SIGTERM. Pass it to the calls that can wait, such as `db.PingContext(ctx)`, so Ctrl-C does not wait for a database that never answers. If `OnStart` returns an error, the program stops. `ctx` stays valid after `OnStart` returns, so a goroutine may watch it, but it is done as soon as the stop signal comes: before the requests in flight finish and before `OnShutdown` runs. Work that requests feed, such as a job queue, runs on a context of its own that `OnShutdown` cancels, so the jobs the last requests queue still run (see [Background work](guides/jobs.md)).
- `OnShutdown` runs once the requests in flight have returned. A handler that streams with no end of its own, such as server-sent events, would hold that for `server.shutdown_timeout`: it returns when `tifa.ShuttingDown(c.Context())` closes, as the stop starts (see [Streaming](guides/streaming.md#when-the-program-stops)).
- `OnRequest` runs for pages, navigation data requests, API routes and unmatched URLs. It does not run for static files, and it does not see a write that a browser sends from another site: the program refuses that with 403 first (see [Cross-site writes](config.md#cross-site-writes)). It sees the [security headers](config.md#security-headers) already set and can change them. For an unmatched URL, `c.Pattern()` is empty, and `next(c)` runs the Layouts its 404 shows inside (see [404](#404)). On a page with `Cache` it sees an anonymous request, with no cookie, session or most headers (see [The whole-page cache](#the-whole-page-cache)).
- Resources the whole app shares, such as a database connection or a logger, go in files at the app root next to `hooks.go`, for example `db.go` or `middleware.go`. Each one builds a package-level variable or a function: set it up in `OnStart`, release it in `OnShutdown`, and use it directly anywhere, for example `myapp.DB` after importing `example.com/myapp`. Domains under `app/` import the root package for this. By convention the root package imports nothing under `app/`, so this never closes an import cycle. Models and queries stay in their domains. Route code uses these resources only inside functions, never while package-level variables are initialized. Do not use `init()` for setup. See [which way imports go](structure.md#which-way-imports-go).
- `next` runs the matched route, once. A second call returns an error. Pass it the `*tifa.Ctx` the route should see: `next(c)`, or `next(c.WithContext(ctx))` to store request-scoped values in the standard context, as `r.WithContext(ctx)` does for a request. `c.WithContext` returns a copy and leaves `c` alone. Business packages provide typed getters.
- The error `next` returns can be recognized with `errors.Is` and `errors.As`, a redirect as `*tifa.RedirectError`, and a render turned away because every VM is busy as `tifa.ErrSSRBusy` (see [Handling a rejected render](config.md#handling-a-rejected-render)).

### Changing the log handler

Tifa logs its own messages with `log/slog`, at a level and with fields: a failed request is `ERROR` with `method`, `path` and `err`, a refused cross-site write is `WARN`, the address it listens on is `INFO`. The lines about startup, such as the address, are written after `OnStart` returns, so a handler set there already gets them. To change where they go, set the default handler in `OnStart`:

```go
func OnStart(ctx context.Context) error {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	return nil
}
```

Tifa looks up `slog.Default()` for each message, so a later `slog.SetDefault` also works.

### Setting up in OnStart

`OnStart` is where the app wires what the whole process shares: the log handler, plain `net/http` middleware and the [session store](sessions.md#where-to-store-sessions).

```go
import (
	"log/slog"
	"net/http"
	"net/http/pprof"
	"os"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

func OnStart(ctx context.Context) error {
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

`tifa.SetHTTPHandler(wrap)` gets Tifa's handler and returns the one to serve. The wrapper is outside everything else: static files, the cross-site check and the routes. It sees every request with the original `http.ResponseWriter`, including the 403 of a refused cross-site write. Once the handler it wraps has returned, `r.Pattern` of that request is the route template, the same as `c.Pattern()` (such as `/projects/{project}`, and empty when no route matched), so a metrics middleware can count by route and not by URL. The browser asks for the data of a navigation with the `_tifa` parameter, and `.p` marks a preload. Do not count page views from these requests: a preload can serve a navigation without another request, and a reload adds one. Count the client navigations the browser finishes, plus the full page requests (see [Page requests](build.md#page-requests)). It is plain `net/http`, so `OnRequest`, `OnError` and the cross-site check do not apply to the paths it serves itself, such as `/debug/pprof/` above. Put such paths behind your own check, or keep them on another listener. A shutdown still waits for the requests it wraps. Call it in `OnStart` only: it panics once the server listens. `tifa.SetHTTPHandler(nil)` removes the wrapper. It runs outside the anonymous request of a page with `Cache` (see [The whole-page cache](#the-whole-page-cache)): the values it puts into the context of the request reach `OnRequest` and `Load` of such a page, so do not recognize visitors in it, do it in `OnRequest`.

## hooks.ts

`hooks.ts` in the project root is the JavaScript side of `hooks.go`. It exports one optional function:

```ts
// hooks.ts
import { i18n } from 'tifa/i18n'
import dayjs from 'dayjs'
import relativeTime from 'dayjs/plugin/relativeTime'

export function onStart() {
  dayjs.extend(relativeTime)
  i18n.services.formatter?.addCached('currency', (lng, options) => {
    const f = new Intl.NumberFormat(lng, { currency: 'USD', ...options, style: 'currency' })
    return (value) => f.format(value)
  })
}
```

- `onStart` runs once in each JavaScript runtime before any page renders, whatever the page: in each VM of the server when the SSR bundle loads, and in the browser before hydration. It is for what the whole app sets up once, such as i18next formatters, dayjs plugins, chart types or an error reporter.
- It runs on both sides, so the server and the browser render with the same setup. Tifa has no JavaScript that runs only on the server: data and I/O are in Go.
- It is synchronous. Work it starts and does not wait for, such as a dynamic `import()`, may finish after the first render.
- If it throws on the server, the program stops before it serves. In the browser the page is not hydrated: it stays the HTML the server sent, links load whole pages, and the error is in the console.
- `hooks.ts` exports nothing else: another export fails the build, naming it, since Tifa would never run it. An app without pages has no JavaScript, and a `hooks.ts` there fails the build too.
- `hooks.ts` is in the main bundle of the browser, with what it imports. Keep it small.

## Sessions

A session is the record of who is signed in, kept on the server and found by a cookie. Sign in with `tifa.SetSession(c, &s)` in a method handler, read it with `tifa.Session[S](c)` anywhere that has the Ctx, and sign out with `tifa.ClearSession(c)`. [Sessions](sessions.md) has the rest: the cookie, a store of your own on Redis or SQL, where each call may be made, `useSession` and `SignedIn` in the browser, testing and the errors.

## Testing

Tests run with `go test`, next to the code, in two ways. A test of one function calls `Load`, a method handler or `OnRequest` with a `Ctx` from `tifatest.NewCtx`. A test of a whole request starts the app with `tifatest.Start` and sends it HTTP requests, so it can sign in, post a form, follow the redirect and read the page.

### Testing a function

`Load`, `Layout`, the method handlers and `OnRequest` are plain Go functions. `tifatest.NewCtx(t, r)` turns an `httptest` request into the `*tifa.Ctx` the function takes, and returns the recorder that holds what the function writes:

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

This tests the function, not the HTTP answer. Check what the function returns: the value and the error. Only the server decodes the input, runs `Validate`, turns a result into JSON or 204 and an error into 404, 422, a redirect or a call to `OnError`, and answers navigation data requests. A returned value or error is not written into the recorder: when the function writes nothing itself, the recorder stays an empty 200. To check the HTTP answer, [test the whole request](#testing-a-whole-request).

- Path parameters are what `c.Param` reads: set the decoded values with `r.SetPathValue`. `c.Pattern()` is `r.Pattern` as it is when `NewCtx` is called.
- Run `Load` and `Layout` through `tifa.Use(c, Load)`. It runs them under the rules of the server: once per request, and writing the response body fails. A `tifa.Use` of a Layout inside `Load` runs that Layout for the same request. Only the Layouts the function asks for with `tifa.Use` run.
- Call a method handler directly: `POST(c)`, or `POST(c, in)` with a typed input. The input is the value you pass, so test `Validate` on its own, and if `Validate` changes the input, pass the changed input.
- Call `OnRequest` directly with a closure for `next`: `OnRequest(c, func(c *tifa.Ctx) error { got = c.Context(); return nil })`. The closure stands in for the route, so it neither recovers a panic nor writes a result.
- The body limit is the default `server.body_limit`, 1 MiB, unless `c.SetBodyLimit` changes it. The `BodyLimit` and `ReadTimeout` of `route.go` are not applied. The test removes the multipart files of the request when it ends.
- `tifatest.File(t, "sea.png", data)` gives the `*multipart.FileHeader` of an uploaded file, for an input you pass to a handler or check with `Validate`. Its `Content-Type` follows the extension of the name. Its temporary files are removed when the test ends.
- Read the recorder once the function has returned, and make a new `NewCtx` for each request: `rec.Result()` keeps the headers it saw the first time it was called.
- `tifa build` reads no `_test.go` files, so tests may sit in any folder under `app/`.

### Testing a whole request

`tifatest.Start(t, tifaapp.App)` runs the app inside the test and returns an `*httptest.Server`. `tifaapp` is the package `.tifa/tifaapp` that `tifa check`, `tifa build` and `tifa dev` generate: the routes, Layouts, hooks and options of the app, as the program serves them. It imports every route package, so import it from a test in a package of its own, named after the folder with `_test` (`package login_test`). From the shop, `app/login/request_test.go`:

```go
package login_test

import (
	"strings"
	"testing"

	"github.com/liustack/tifa/examples/shop"
	"github.com/liustack/tifa/examples/shop/.tifa/tifaapp"
	"github.com/liustack/tifa/tifatest"
)

func TestSignInLeadsBackToTheWorkbench(t *testing.T) {
	srv := tifatest.Start(t, tifaapp.App)
	client := srv.Client()

	res, _ := client.Get(srv.URL + "/projects/studio/stats")
	// 302 to /login?next=%2Fprojects%2Fstudio%2Fstats

	body := `{"email":"` + shop.DemoEmail + `","password":"` + shop.DemoPassword + `","next":"/projects/studio/stats"}`
	res, _ = client.Post(srv.URL+"/login", "application/json", strings.NewReader(body))
	// 303 to /projects/studio/stats, and the client keeps the session cookie

	res, _ = client.Get(srv.URL + "/projects/studio/stats")
	// 200, with the data of the page
}
```

- Everything the program runs takes part: `OnStart` with the context of the test, `OnRequest`, decoding the input, `Validate` and its 422, sessions, redirects and `OnError`. When the test ends, the server waits for the requests in flight and runs `OnShutdown`.
- `srv.Client()` trusts the certificate of the server, keeps the cookies it sets and does not follow redirects, so the test sees each 3xx with its `Location`.
- The server listens on HTTPS on this machine. The settings of Tifa are those of the program, from the defaults and `tifa.yaml`, apart from the address and HTTPS. Options of the app keep their defaults, as flags and environment variables do not reach a test, so set the variable before `Start`: `myapp.DBPath = filepath.Join(t.TempDir(), "test.db")`.
- Without a front end, a page answers the results of its `Load` and `Layout` functions as JSON, by route ID: `{"app/layout": …, "app/projects/project+/stats/route": …}`. Method handlers answer as in the program.
- Each `Start` makes a new server, with sessions of its own unless `OnStart` sets a store. The app is one per test binary at a time, as the session store, `SetHTTPHandler` and the content belong to the process, so a test that calls `Start` cannot call `t.Parallel`.
- `Start` fails the test when `.tifa/tifaapp` was made from other routes than the code has now. Run `go tool tifa check` again, or keep `tifa dev` running. In a fresh checkout `.tifa` is missing, and `go vet`, `go test` and `go mod tidy` fail on the import until `tifa check` writes it. `tifa check` generates before it runs `go vet` and `go test`, so it needs nothing first.

`tifatest.StartPages(t, tifaapp.App)` also renders the pages to HTML, with the front end that `tifa build` or `tifa dev` wrote to `.tifa/dist`, and serves its scripts, stylesheets and public files. `tifa check` does not bundle, so run `tifa build` before such tests, or keep `tifa dev` running. `StartPages` fails the test when `.tifa/dist` is missing, and when a file the front end was built from has changed since: it names the files, such as a page, a component, a stylesheet, `package.json`, the lock file or the Vite config. Pages render on two VMs whatever `ssr.vms` says, and the test binary never holds more. The first `StartPages` starts them, which takes a moment. Later ones with the same build use them, and one with another build closes them and starts two new ones.

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

The clock is not a cache key, so a request with a clock skips the render cache and shared renders and gets no ETag: it always renders with its own clock. Return displayed times from `Load` so SSR, caching and browser hydration use the same data. `tifatest.NewCtx` preserves the request context but does not run SSR itself. Call `OnRequest` with a closure for `next` to test middleware passing this context, and use server requests to check full HTML.

## How Load may use Ctx

This section covers `Load`, `Layout` and the functions they call. At build time, Tifa starts from each `Load` and `Layout` function and finds the route parameters (`c.Param`), query parameters (`c.Query` and `c.QueryValues`) and Layouts (`tifa.Use`) it reads. When `Load` passes `*tifa.Ctx` to a function in this module, Tifa follows the call level by level, across packages too, and counts every parameter it finds toward that `Load`. A helper called by several `Load` functions counts toward each of them. Tifa does not follow into its own functions.

So that Tifa finds everything, Ctx may only be used where Tifa can see it. Pass `c.Param`, `c.Query` and `c.QueryValues` a constant string, such as `c.Param("order")`, and call them and `tifa.Use` directly: storing one in a variable or passing it as a value fails the build.

The only types that may hold a Ctx are: `*tifa.Ctx`, `tifa.Ctx`, non-generic structs declared in this module (whose fields hold Ctx only through these types, directly, embedded or indirectly), and pointers to such structs.

A value that holds a Ctx may only appear in these five places:

1. As the receiver of a direct method call, such as `c.Query("tab")` or `req.Page()`.
2. As an argument to a non-generic function from this module or from Tifa, where the parameter has exactly the same type. A function literal called in place also counts.
3. In a composite literal of a struct that holds Ctx, for a field of exactly the same type.
4. Assigned with `:=` or `var x T = ...` to a new local variable of exactly the same type. That variable follows the same rules.
5. With its address taken, where the resulting pointer is still a type that may hold Ctx.

Any other use fails the build and names the file and line. For example: converting it to another type or an interface, assigning it to an existing variable, assigning several values at once, `return`ing it, putting it in a slice, map, array or channel, `range`, package-level variables, `defer`, `go` statements, or passing it to a generic function, an interface method, a function variable or a function outside the module (the standard library and third-party libraries).

`Load`, `Layout` and the functions they call must not call `c.Request()` or `c.Writer()`. Tifa cannot see parameters read through the raw request. Use `c.Param`, `c.Query`, `c.QueryValues`, `c.Header`, `c.Cookie` or `c.Context()` instead. `c.Request()` and `c.Writer()` are only for method handlers and `OnRequest`.

They must not call `c.BindQuery` or `c.Bind` either. Tifa cannot name the query parameters a struct decodes, so the page would not load again when they change, and a page load or the data request of a navigation has no body. Read each query parameter with `c.Query("name")`, or `c.QueryValues("name")` for a repeated one.

When you hit one of these errors, call Ctx methods directly, or pass it to a plain function. If you only need the request context, pass `c.Context()`. A helper that only calls `c.Context()` (such as `auth.User(c)`) collects nothing when Tifa follows it.

Tifa only checks code that `Load` and `Layout` can reach. If you store a Ctx in a global variable or another shared place from `OnRequest`, a method handler or anywhere else, and then take it out in `Load` or `Layout`, Tifa cannot catch it. Do not do this.

## JSON rules

The value `Load` or a `Layout` function returns, and the input and result of a method handler, travel as JSON, and Tifa generates TypeScript types from them. Tifa writes and reads this JSON with `encoding/json/v2` and its defaults, so the build accepts only the types below, which json v2 writes without an error and in a form the TypeScript type describes. `c.JSON`, `c.Bind` and the session use the same rules.

| Go | TypeScript |
| --- | --- |
| `bool` | `boolean` |
| integers, floats, `json.Number` | `number` |
| `string`, `time.Time` | `string` |
| `tifa.HTML` | `HTML` of `tifa/client`, see [Showing HTML the app trusts](#showing-html-the-app-trusts). Not a map key, a handler input, in the session or a whole result |
| `*multipart.FileHeader`, `[]*multipart.FileHeader` | `Blob \| null`, `Blob[]`. Only in a field of the input of a POST, PUT or PATCH handler, see [Files in the input](#files-in-the-input) |
| pointer | `T \| null` |
| slice | `T[]`. A nil slice is written as `[]` |
| `[]byte`, `[N]byte` (the element is exactly `byte` or `uint8`) | `string`, written as base64. A nil `[]byte` is `""`. A slice or array of a named byte type is an ordinary array of numbers |
| array | `T[]`, or `[]` when the length is 0 |
| map | `Record<string, V>`. A nil map is written as `{}` |
| struct | an object of its exported fields |

- Map keys are written in sorted order, so the same value always gives the same text. They must be string or integer types, or types that output a string under the `MarshalText` rule below. Interfaces and pointers cannot be keys.
- An exported embedded struct (a pointer works too) without a json name has its fields promoted to the outer object. Fields reached through an embedded pointer are optional. When fields of different structs have the same name, the shallowest wins, then the one with a json tag, and if that still leaves several, all of them are dropped. An embedded field needs a json name when its type is not a struct, and an embedded struct may not have json tag options or a method named `MarshalJSON`, `MarshalJSONTo`, `MarshalText`, `AppendText`, `UnmarshalJSON`, `UnmarshalJSONFrom` or `UnmarshalText`. Unexported embedded fields are not accepted unless tagged `json:"-"`.
- Two fields of one struct may not have the same JSON name. An unexported field may not have a json tag other than `json:"-"`, and a struct that has fields needs an exported one or a json tag.
- A json tag name may only use letters, digits, spaces and `` !#$%&()*+-./:;<=>?@[]^_{|}~ ``. The only allowed options are `omitempty`, `omitzero` and `string`. Fields with `omitempty` or `omitzero` are optional. `omitzero` leaves out the zero value of the Go type. `omitempty` leaves out a value whose JSON is empty: `null`, `""`, `[]` or `{}`, so it never leaves out `false` or `0`. The `string` option is only allowed on fields of a predeclared number type, or on unnamed pointers to them, and writes the number as a string.
- `time.Duration` is not accepted, also as a map key, since json v2 has no form for it. Return an integer of the unit you mean, such as milliseconds, or a string.
- If a type's value or pointer has a method or field named `MarshalJSON`, `MarshalJSONTo`, `AppendText` or `AppendJSON`, it is rejected whatever the signature (except `time.Time` and `json.Number`). Rename it, or return a different type without these methods.
- If a type itself declares a method with exactly the signature `MarshalText() ([]byte, error)`, on the value or the pointer receiver, it is written as a string. A wrong signature, a method promoted from an embedded field, or a method hidden by a field of the same name is always rejected.
- Interfaces are not accepted anywhere, including fields, elements and map keys. Neither are chan, func, complex numbers, `unsafe.Pointer` or type parameters.
- `<`, `>` and `&` are written as they are, also in the data of a page. Tifa escapes them where it puts the data into a page, so no text of the app can end a script element.
- A string with bytes that are not UTF-8 is written with U+FFFD for each of them, as the browser would show it, and does not fail the page.
- Input is read strictly: names must match the json name exactly, also in case, a name may appear once in an object, strings must be UTF-8, `time.Time` must be RFC 3339, base64 may not hold line breaks, and an array must have the length of the Go array. Each mistake answers 400. Names the type does not have are skipped. A session record that does not read back this way signs the visitor out.
