---
summary: 'Load in route.go, page.tsx data, route.gen, tifa.HTML, how Load may use Ctx and what c.Request() holds there'
audience: app
order: 3
read_when:
  - Writing the `Load` of a page, or the route.go of a page
  - Writing a page.tsx, or using what `./route.gen` exports
  - Showing HTML the app made or trusts, such as an article, with `tifa.HTML` and `<HTML>`, or links inside it
  - The build reports an error about Ctx usage and you want to fix it
  - Reading `c.Request()` or using `c.Writer()` in a `Load` or `Layout`, or passing the Ctx to a library or a goroutine
  - Changing the rules in internal/analyze and you need to know what users were promised
---

# Loading data

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

- `Load` returns the page data. Its return type must convert to TypeScript (see [JSON rules](../reference/json.md)).
- `GET`, `POST`, `PUT`, `PATCH` and `DELETE` handle the HTTP method they are named after.
- `Layout` returns the data of the Layout of the folder (see [Layout](layouts.md#the-layout-of-a-folder)).
- A folder with a `page.tsx` is a page. The framework renders its GET, so you cannot also write `GET`. A page that needs no data and no other method can be `page.tsx` alone, without `route.go`.
- A folder with `route.go` and no `page.tsx` is an API route when it declares a method handler: every method handler answers as written, and `Load` is not allowed, because `Load` loads the data of a page. A `route.go` with only a `Layout` is no API route. It gives the pages below it their Layout data.
- Everything is package-level functions and constants. Use the global variables the app has set up for dependencies. The framework does no dependency injection.
- The names of the switches (`SSR`, `Cache`, `Memo`) are read as written, so a misspelled one would be ignored. The build fails on a bool constant in `route.go` named `cache`, `memo` or `ssr` in lower case, and on an exported constant that is one or two letters off a switch (`Cahce`, `Memoo`) and has a type that switch takes: a bool for `SSR` and `Memo`, a bool or a `time.Duration` for `Cache`. The message names the switch it probably meant. Constants of other types, such as `const Demo = "demo"`, short names such as `ID` or `Max`, and names such as `Mode` that are two edits from a four-letter switch, are left alone.

In `Load` you can only set response headers and cookies. Calling `c.JSON`, `c.Text` or `c.NoContent` returns an error.

What `Load` returns when it fails is in [What an error you return becomes](errors.md#what-an-error-you-return-becomes).

## Query parameters

A `Load` or `Layout` that reads the query string takes a struct as its second parameter, with a field for each query parameter:

```go
type Filter struct {
	Sort  string   `json:"sort"`
	Color []string `json:"color"`
	Page  int      `json:"page"`
}

func Load(c *tifa.Ctx, q Filter) (products.List, error) {
	return products.Find(q.Sort, q.Color, q.Page)
}
```

A `Load` that reads none takes only the Ctx. The second parameter is a struct value, not a pointer, and the type may be named or written in place.

- A field is named by its `json` tag, then by its Go name. A field with `json:"-"` and an unexported field are left out. No field may be named `_tifa`, which Tifa keeps for navigation.
- Tifa reads the names from the struct when it builds. The page loads again when one of them changes, the server renders the page with these parameters and no others (a `?utm_source=` is left out), and `Link`, `navigate` and `useSearch` know the names (see [Navigation](navigation.md#query-parameters-in-link-and-navigate)).
- A field is a string, a `bool`, a whole number, a `float64`, or a slice of one of them. Embedded fields, pointers, maps, types with `MarshalText` or `UnmarshalText` and `float32` fail the build, and the message names the field. See the types below for `float32`. Pages that share parameters write the fields again in each struct, and a name that a Layout and a page below it both declare has the same type in both.
- What the page gets for each type of field, in TypeScript:

| Go field | TypeScript (in `useSearch()` and `PageSearch`) |
| --- | --- |
| `string` | `string` |
| `bool` | `boolean` |
| `int`, `int8`, `int16`, `int32`, `int64`, `uint`, `uint8`, `uint16`, `uint32`, `uint64` | `number` |
| `float64` | `number` |
| a slice of one of them | an array of that type |

- A field may not be a `float32`, and the build fails if it is. A query value must be the same number in Go and in the browser, and the browser has only 64-bit numbers. A `float32` would round the value: `?price=1234567.89` is 1234567.9 in a float32, while the page in the browser would show 1234567.89. Declare the field as `float64`. This is a rule for the query struct of a `Load` or `Layout` only: the input of a method handler still takes `float32`.
- A value is read as the field's type, and a number may be written the way a person types it. `?page=007` is page 7, `?rating=1.0` is 1, `?min=%2B10.50` is 10.5 and a number input that sends `1e3` gives 1000:

| Type | Accepted | Answers 400 |
| --- | --- | --- |
| `string` | any text, empty too, never changed (`?q=007` stays `007`) | |
| whole numbers | digits with an optional `+` or `-` and, after the digits, an optional point with only zeros (`7`, `007`, `+7`, `-0`, `7.`, `7.00`), in the range of the type and at most 2^53-1 | `7.5`, `1e3`, `0x10`, `abc`, a space, a value out of range |
| `float64` | digits with an optional `+` or `-`, an optional fraction, and an optional exponent (`10.50`, `.5`, `5.`, `1e3`, `-0`), and `0` or between 1e-6 and 1e21 in size | `1e400`, `NaN`, `Infinity`, `0x1p3`, `1,5`, `abc`, a space, a value out of range |
| `bool` | `true` and `false`, nothing else | `1`, `TRUE`, `on`, `yes` |
| a slice | one element for each parameter of that name, in order, each as the element type | |

- Another spelling of a number is the same number everywhere: the server renders the page for the shortest text (`?page=7`), so `?page=007` and `?page=7` are one page with one render, one cache entry and one canonical link, and after the page loads the address bar quietly becomes `?page=7`, with no request for data, no reload and no hydration warning. `-0` is `0`. A float that has more digits than a float64 holds reads as the nearest float64 and is written as that one. The text of a number is at most 64 bytes long, whole numbers and floats alike, and a longer one answers 400: Go and a browser round a very long decimal differently, and no number a person types or a form sends is that long.

- A value that is none of the above answers 400 with `invalid query parameter "page"`, before `Load` runs. The data request of a navigation gets the 400 for that page. A query string that does not parse answers 400 with `invalid query string`. A name that comes twice for a field that is not a slice is not valid either. When two values are not valid, the message names the first by name.
- The browser reads the address by the same rules, so a value that Go refuses, the browser refuses too, and the `ErrorBoundary` of the page gets the same `TifaError` with status 400 and the same message, whether the full page, the data request of a navigation or the router in the browser found it. A table of cases tests Go and the browser alike.
- A value that is not valid is a wrong address, as a path with no page is. The `ErrorBoundary` of the page decides what the visitor sees, such as a short message and a link to the page without the filter. Do not declare the field a `string` to get around the 400: the page then loses the types, the shortest address and the active state of its links, and has to check each value by hand.
- The page gets the values with the types of the fields: `useSearch()` and `PageSearch` give a `number` for a whole number or a float, a `boolean` for a `bool`, a `string`, and an array of them for a slice (see [Navigation](navigation.md#query-parameters-in-link-and-navigate)). The browser converts by the declared type and by nothing else, so `?q=1.0` and `?q=007` stay the texts `1.0` and `007` for a `string` field.
- An empty value (`?page=`) of a field that is not a string counts as not passed, and the field keeps its zero value, so a form that sends its empty fields works. An empty element of a list of numbers or bools is skipped. For a string, empty is a value. The server renders the page as if the empty value were not there and answers 200 with no redirect. After the page loads, the address bar drops it (`?page=&q=x` becomes `?q=x`) with no request for data, no reload and no hydration warning. In the browser the value is `undefined`, and the zero value is only what Go's field holds.
- So Go cannot tell a parameter that was not passed from one passed as the zero value: `?page=0` and no `page` both give 0. Let the zero value be the default (page 0 is the first page, a `max` of 0 is no limit). A choice that needs its own on and off, such as free books only, is a `bool` field of its own. Fields cannot be pointers.
- A parameter that no field names is ignored.
- A visitor who edits the address bar, or fills a plain `<form method="get">` with no JavaScript, can write `007`, `1.0` or `.5` and the page takes it. These rules are for the query string of a page only: the input of a `GET` or `DELETE` handler is decoded as before, by `c.BindQuery`.
- A `Validate() error` or `Validate(c *tifa.Ctx) error` method of the struct runs once the values are decoded and before `Load`, as for the input of a method handler, so a page and a handler can share one filter type. What it returns is the error of `Load`: `tifa.Error{Status: 400, Message: "page must be at least 1"}` or `tifa.ErrNotFound` reaches the visitor as it is, and any other error goes to `OnError` and answers 500. The browser does not run it: on a navigation, a value it refuses comes back as the error of the data request, and the `ErrorBoundary` of the page shows it.
- A test calls the function with the struct: `Load(c, Filter{Sort: "price"})` (see [Testing the app](testing-apps.md)).

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

- UI only, no data loading. `./route.gen` is a generated module, one per folder, that exports `page` and `layout` (the TanStack routes of the page and the Layout, only when the folder has them), `PageData` and `LayoutData` (the return types of `Load` and `Layout`), `PageSearch` and `LayoutSearch` (the query parameters they read, see [Query parameters in Link and navigate](navigation.md#query-parameters-in-link-and-navigate)), `Params`, the types of the Go structs they hold (see below), and a function per method handler. Child components in the same folder also import `./route.gen`. A Layout's data comes from the `route.gen` of its folder: `layout.tsx` imports `layout` from `'./route.gen'`, and a page reads its project Layout with `layout` from `'../../route.gen'`, for example. A folder with no generated module fails the build when something imports its `route.gen`.
- `./route.gen` also exports each Go struct that the data of the folder uses, under its Go name, so a component can name a part of the data. `PageData`, `LayoutData` and the `<METHOD>Input` and `<METHOD>Output` of the handlers are the same types under their usual names. With `type Order struct` and `type Item struct` in the Go package of the folder:

  ```tsx
  import { page, type Item } from './route.gen'

  function Row({ item }: { item: Item }) {
    return <li>{item.sku}</li>
  }
  ```

  - A struct of the package of the folder keeps its name. When two packages have a struct of one name, the other package's gets its package name in front (`cart.Order` is `CartOrder`), and if that is still taken the struct stays written in place, with no name.
  - A struct named like something `route.gen` exports or uses itself, such as `Params`, `PageData`, `Record`, `Blob` or `HTML`, gets `Go` in front (`GoParams`). A Go struct named `PageData` that `Load` returns is the one `PageData`.
  - Only structs get names. A type such as `type Status string`, a slice or map type, a generic type such as `Page[Order]`, a type declared inside a function and an anonymous struct are written in place.
  - Each folder has its own copy of the types, and `route.gen` files do not import each other. Import a type from the `route.gen` of the folder you are in.
  - Writing `PageData['orders'][number]` still works.
- `route.gen` is written from `route.go`, `page.tsx` and `layout.tsx`, and its first lines name the files it comes from. After a change to a return type or input type in Go, or after you add or remove a `page.tsx` or `layout.tsx`, run `go tool tifa gen` once, then write the TypeScript that uses the new types: one run for the whole batch of Go changes is enough. `go tool tifa check` and `pnpm typecheck` run it for you, and while `tifa dev` runs it rewrites the types after each saved change (a Go build that fails keeps the old ones).
- The default export is the page component. Optional exports: `head` and `ErrorBoundary`. A 404 shows in `ErrorBoundary` too (see [404](errors.md#404)), and exporting `NotFound` fails the build.
- In the browser, each `page.tsx`, and each `layout.tsx` or `layout_.tsx` but the root Layout's, is a chunk of its own with everything it imports, `head` included. It loads the first time its route is needed, and the router waits for it before it shows the page or runs `head`. The HTML of a page preloads the chunks of its Layouts and route, so they arrive with the entry script. Code at the top level of the file, side-effect imports included, runs then and not when the app starts, so put setup that every page needs in [`setup.ts`](hooks.md#setupts), which runs before any route module. See [Route modules in the browser](../build-and-deploy/build.md#route-modules-in-the-browser).
- Go slices and maps are never `null`: a nil slice arrives as `[]` and a nil map as `{}`, and their TypeScript types have no `| null` (see [JSON rules](../reference/json.md)).
- Put components that need browser features at the module top level into a `.client.tsx` file, and load them lazily with `clientOnly` from `tifa/client`. They are left out of the SSR bundle, and the first paint shows a placeholder. Components that render but show nothing until the browser draws them belong there too, see [Components that draw only in the browser](server-render.md#components-that-draw-only-in-the-browser):

```tsx
import { clientOnly } from 'tifa/client'

const OrderActions = clientOnly(() => import('./actions.client'), <button disabled>Loading…</button>)
```

## Showing HTML the app trusts

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

## How Load may use Ctx

This section covers `Load`, `Layout` and the functions they call. They get the Ctx as a `net/http` handler gets its request: use it while the function runs, and pass it wherever Go lets you, to a helper of this module or a library, into an interface, a struct or a goroutine.

The build reads `Load`, `Layout` and the functions of this module they pass the Ctx to, and fails on these mistakes where it sees them, with the file and line:

- `c.Query`, `c.QueryValues`, `c.Bind` and `c.BindQuery`. A query parameter is a field of the second parameter (see [Query parameters](#query-parameters)), so that Tifa and the browser know it without reading the code, and a page load has no body.
- `c.JSON`, `c.Text` and `c.NoContent`. Tifa writes the answer of a page from what `Load` and `Layout` return: return the data, an error such as `tifa.Error{Status: 404, Message: "no such order"}`, or `tifa.Redirect(url, 302)`.
- `tifa.SetSession` and `tifa.ClearSession`. A page load must not sign anyone in or out: call them in `OnRequest` before `next`, or in a method handler.
- A constant name in `c.Param` that the path of the folder does not have. A `Layout` reads the parameters of its own folder: `Layout` in `app/projects/project+/route.go` may read `project`, not `order`.
- `c.Path()` in a `Layout`. Read the address in `layout.tsx` instead (see [Breadcrumbs and the current page](layouts.md#breadcrumbs-and-the-current-page)).
- `tifa.Use` of a Layout that does not wrap the page, or of anything but a package-level `Layout` function (see [Layouts](layouts.md)).

Where the build cannot see which function runs, such as a function value, an interface method or a library, it stops without a word. That code is yours to get right, as in an `http.Handler`:

- Do not keep the Ctx, or a value that holds it, after the function returns: not in a variable of the package, a cache, or a goroutine that outlives the function.
- Wait for every goroutine you start before the function returns. `errgroup.Group.Wait` does it.
- What the build refuses does not work when it runs either: `c.Query` sees only the declared parameters, `c.Param` in a `Layout` is `""` for a parameter of a folder below it, and `c.JSON`, `c.Text` and `c.NoContent` write nothing and return an error.

`c.Path()` gives the path of the page in a `Load`, such as for a `next` address in a redirect to a sign-in page. It is the path the visitor asked for, with the language prefix and without the query, for a page load and for the data request of a navigation alike. A `Load` that needs a query parameter in that address takes it from its second parameter.

### The raw request in Load

**`c.Request()` in a `Load` or `Layout` is not the request as it came.** It is a copy that holds only what the function may depend on, so that the memo, the data the browser keeps and the pages of other visitors stay right:

- Its URL has only the query parameters the function declares in its second parameter, each number spelled the shortest way (`?page=007` reads as `page=7`). Any other parameter, such as `utm_source`, is not there.
- It has no body.
- In a `Layout` it has only the path parameters of the Layout's own folder. The others are `""`.
- `URL.Path` is the path of the page. In a `Layout` it goes stale, as `c.Path()` would: the Layout's data stays when the visitor moves to another page below it.
- Each run of a function gets its own copy. Changing it changes nothing for the other functions.

**`c.Writer()` is the writer of the response**, as everywhere. Writing to it from a `Load` or `Layout` breaks the page, which Tifa writes once the functions return. Set a header with `c.SetHeader`.

Logging, counting requests and anything else that needs the whole request belong in `OnRequest`, which sees the request as it came:

```go
// hooks.go
func OnRequest(c *tifa.Ctx, next func(*tifa.Ctx) error) error {
	slog.InfoContext(c.Context(), "request", "uri", c.Request().RequestURI)
	return next(c)
}
```

For a log of every request with its status and time, see [Logging each request](../guides/logs.md#logging-each-request).
