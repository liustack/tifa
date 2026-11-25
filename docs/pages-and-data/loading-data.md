---
summary: 'Load in route.go, page.tsx data, route.gen, tifa.HTML, how Load may use Ctx'
audience: app
order: 3
read_when:
  - Writing the `Load` of a page, or the route.go of a page
  - Writing a page.tsx, or using what `./route.gen` exports
  - Showing HTML the app made or trusts, such as an article, with `tifa.HTML` and `<HTML>`, or links inside it
  - The build reports an error about Ctx usage and you want to fix it
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

- Another spelling of a number is the same number everywhere: the server renders the page for the shortest text (`?page=7`), so `?page=007` and `?page=7` are one page with one render, one cache entry and one canonical link, and after the page loads the address bar quietly becomes `?page=7`, with no request for data, no reload and no hydration warning. `-0` is `0`. A float that has more digits than a float64 holds reads as the nearest float64 and is written as that one.

- A value that is none of the above answers 400 with `invalid query parameter "page"`, before `Load` runs. The data request of a navigation gets the 400 for that page. A query string that does not parse answers 400 with `invalid query string`. A name that comes twice for a field that is not a slice is not valid either. When two values are not valid, the message names the first by name.
- The browser reads the address by the same rules, so a value that Go refuses, the browser refuses too, and the `ErrorBoundary` of the page gets the same `TifaError` with status 400 and the same message, whether the full page, the data request of a navigation or the router in the browser found it. A table of cases tests Go and the browser alike.
- The page gets the values with the types of the fields: `useSearch()` and `PageSearch` give a `number` for a whole number or a float, a `boolean` for a `bool`, a `string`, and an array of them for a slice (see [Navigation](navigation.md#query-parameters-in-link-and-navigate)). The browser converts by the declared type and by nothing else, so `?q=1.0` and `?q=007` stay the texts `1.0` and `007` for a `string` field.
- An empty value (`?page=`) of a field that is not a string counts as not passed, and the field keeps its zero value, so a form that sends its empty fields works. An empty element of a list of numbers or bools is skipped. For a string, empty is a value. The server renders the page as if the empty value were not there and answers 200 with no redirect. After the page loads, the address bar drops it (`?page=&q=x` becomes `?q=x`) with no request for data, no reload and no hydration warning. In the browser the value is `undefined`, and the zero value is only what Go's field holds.
- A parameter that no field names is ignored.
- A visitor who edits the address bar, or fills a plain `<form method="get">` with no JavaScript, can write `007`, `1.0` or `.5` and the page takes it. These rules are for the query string of a page only: the input of a `GET` or `DELETE` handler is decoded as before, by `c.BindQuery`.
- The struct has no `Validate`: the build fails for a struct that has one. Check the values in `Load` and return `tifa.Error{Status: 400, Message: "page must be at least 1"}` or `tifa.ErrNotFound`.
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

This section covers `Load`, `Layout` and the functions they call. At build time, Tifa starts from each `Load` and `Layout` function and finds the route parameters (`c.Param`) and Layouts (`tifa.Use`) it reads. The query parameters come from the second parameter (see [Query parameters](#query-parameters)). When `Load` passes `*tifa.Ctx` to a function in this module, Tifa follows the call level by level, across packages too, and counts every parameter it finds toward that `Load`. A helper called by several `Load` functions counts toward each of them. Tifa does not follow into its own functions.

So that Tifa finds everything, Ctx may only be used where Tifa can see it. Pass `c.Param` a constant string, such as `c.Param("order")`, and call it and `tifa.Use` directly: storing one in a variable or passing it as a value fails the build.

The only types that may hold a Ctx are: `*tifa.Ctx`, `tifa.Ctx`, non-generic structs declared in this module (whose fields hold Ctx only through these types, directly, embedded or indirectly), and pointers to such structs.

A value that holds a Ctx may only appear in these five places:

1. As the receiver of a direct method call, such as `c.Param("order")` or `req.Page()`.
2. As an argument to a non-generic function from this module or from Tifa, where the parameter has exactly the same type. A function literal called in place also counts.
3. In a composite literal of a struct that holds Ctx, for a field of exactly the same type.
4. Assigned with `:=` or `var x T = ...` to a new local variable of exactly the same type. That variable follows the same rules.
5. With its address taken, where the resulting pointer is still a type that may hold Ctx.

Any other use fails the build and names the file and line. For example: converting it to another type or an interface, assigning it to an existing variable, assigning several values at once, `return`ing it, putting it in a slice, map, array or channel, `range`, package-level variables, `defer`, `go` statements, or passing it to a generic function, an interface method, a function variable or a function outside the module (the standard library and third-party libraries).

`Load`, `Layout` and the functions they call must not call `c.Request()` or `c.Writer()`. Tifa cannot see parameters read through the raw request. Use `c.Param`, `c.Header`, `c.Cookie` or `c.Context()` instead, and take query parameters as the second parameter. `c.Request()` and `c.Writer()` are only for method handlers and `OnRequest`.

`c.Path()` is the way to get the path of the page in `Load`, such as for a `next` address in a redirect to a sign-in page. It is the path the visitor asked for, with the language prefix and without the query, for a page load and for the data request of a navigation alike, so it is no reason to call `c.Request()`. A `Load` that needs a query parameter in that address takes it from its second parameter. A `Layout` and the functions it calls must not call `c.Path()`: the data of a Layout is shared by every page below it and is not loaded again when the visitor goes from one of them to another, so it would keep the path of the page that loaded it first. Call it in the `Load` of the page, in `OnRequest`, or in a method handler.

They must not call `c.Query`, `c.QueryValues`, `c.Bind` or `c.BindQuery` either: a query parameter is a field of the second parameter, so that Tifa and the browser know it without reading the code, and a page load has no body to bind. The build fails on a call it can see. For `c.Query`, `c.QueryValues` and `c.BindQuery`, one it cannot see, such as a Ctx kept in a global variable, panics when it runs, and the error goes to `OnError` like any other error of the `Load`. These methods stay for method handlers (`c.Query` and `c.QueryValues` also for `Validate` and `OnRequest`).

When you hit one of these errors, call Ctx methods directly, or pass it to a plain function. If you only need the request context, pass `c.Context()`. A helper that only calls `c.Context()` (such as `auth.User(c)`) collects nothing when Tifa follows it.

Tifa only checks code that `Load` and `Layout` can reach. If you store a Ctx in a global variable or another shared place from `OnRequest`, a method handler or anywhere else, and then take it out in `Load` or `Layout`, Tifa cannot catch it. Do not do this.
