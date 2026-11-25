---
summary: 'How to write the React side of a route: page.tsx, route.gen, Link types, head, 404s, ErrorBoundary'
audience: app
order: 2
read_when:
  - Writing a page.tsx or layout.tsx, or using what `./route.gen` exports
  - Giving `Link`, `navigate` or `useSearch` the query parameters a page reads, or a type error about `search`
  - Showing HTML the app made or trusts, such as an article, with `tifa.HTML` and `<HTML>`, or links inside it
  - Calling a method handler from a page, or the page does not show what a write changed
  - Setting the title, meta tags or `<html>` attributes of a page, or the head of a 404 or error page
  - Giving a part of the site its own frame with `layout_.tsx`
  - Showing a 404 or an error in a page, or finding out which `ErrorBoundary` shows
---

# Pages and Layouts

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

- UI only, no data loading. `./route.gen` is a generated module, one per folder, that exports `page` and `layout` (the TanStack routes of the page and the Layout, only when the folder has them), `PageData` and `LayoutData` (the return types of `Load` and `Layout`), `PageSearch` and `LayoutSearch` (the query parameters they read, see [Query parameters in Link and navigate](#query-parameters-in-link-and-navigate)), `Params`, the types of the Go structs they hold (see below), and a function per method handler. Child components in the same folder also import `./route.gen`. A Layout's data comes from the `route.gen` of its folder: `layout.tsx` imports `layout` from `'./route.gen'`, and a page reads its project Layout with `layout` from `'../../route.gen'`, for example. A folder with no generated module fails the build when something imports its `route.gen`.
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
- The default export is the page component. Optional exports: `head` and `ErrorBoundary`. A 404 shows in `ErrorBoundary` too (see [404](#404)), and exporting `NotFound` fails the build.
- In the browser, each `page.tsx`, and each `layout.tsx` or `layout_.tsx` but the root Layout's, is a chunk of its own with everything it imports, `head` included. It loads the first time its route is needed, and the router waits for it before it shows the page or runs `head`. The HTML of a page preloads the chunks of its Layouts and route, so they arrive with the entry script. Code at the top level of the file, side-effect imports included, runs then and not when the app starts, so put setup that every page needs in [`setup.ts`](hooks.md#setupts), which runs before any route module. See [Route modules in the browser](../build-and-deploy/build.md#route-modules-in-the-browser).
- Go slices and maps are never `null`: a nil slice arrives as `[]` and a nil map as `{}`, and their TypeScript types have no `| null` (see [JSON rules](routes.md#json-rules)).
- Put components that need browser features at the module top level into a `.client.tsx` file, and load them lazily with `clientOnly` from `tifa/client`. They are left out of the SSR bundle, and the first paint shows a placeholder. Components that render but show nothing until the browser draws them belong there too, see [Components that draw only in the browser](server-render.md#components-that-draw-only-in-the-browser):

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
- Other keys are open: a page may keep parameters of its own in the URL that Go does not read, such as the open tab of a panel. They are strings or arrays of strings too. So a misspelled key is not a type error, only a wrong type for a key Go reads is. The server render does not see them, see [Query parameters in the server render](ssr-and-cache.md#query-parameters-in-the-server-render).
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
- GET and DELETE send the input in the query string, the other methods as a JSON body, or as a multipart form when the input holds files (see [Files in the input](routes.md#files-in-the-input)). The result is typed from the Go result, with `| undefined` for a redirect (below): `func(c, in) (Out, error)` gives `Promise<POSTOutput | undefined>`. `func(c, in) error` gives `void`. `func(c) error` writes its own response, so its function gives back the `Response` as it is, also for an error status.
- A call only sends its request and, when the answer carries a session, applies it (see below). It never reloads the data of a page, on success or on failure, for any method. When the page should show what the write changed, call `router.invalidate()` after it (see the example below).
- An error answer throws `TifaError` with `status`, `message` and `fields` (the messages of a `tifa.Fields`, empty otherwise). A network failure throws as `fetch` does. A call from a page of an older build than the server fails with status 409, see [Pages open while you deploy](../build-and-deploy/deploy.md#pages-open-while-you-deploy).
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
```

- `Layout` goes in `route.go`, next to `Load`, `SSR`, `Memo`, `Cache` and the method handlers of the same folder. Declaring it in any other file of the package, a `layout.go` for example, fails the build. A `route.go` with only a `Layout` is no API route.
- A Layout's `c.Param` may only read parameters of its own folder path. `Layout` in `app/projects/project+/route.go` may read `project`, not `order`. A page's `Load` is held to its own path the same way.
- When you switch between pages under the same Layout, the Layout component is not remounted and keeps its state. When a path parameter the Layout function reads changes (for example, you move to another project), the Layout remounts and its state resets. When a query parameter changes, even one it reads, the Layout loads its data again and renders it, but is not remounted and keeps its state, as TanStack Router does by default. So a filter keeps the view the visitor chose. State a component set up from the data of `Load`, such as with `useState(data.x)`, is not reset by a new query either. Where it should follow the new data, give the component a `key` from the value, or update the state in an effect. Pages work the same way.
- Pages and Layouts can call `tifa.Use(c, project.Layout)` to get the result of a Layout that wraps them, for the current request: the Layout of the same folder or of an ancestor folder, unless a `layout_.tsx` between drops it. The same `Layout` runs only once per request. Any other Layout fails the build, and so does an argument that is not a package-level `Layout` function, such as a closure. A `Layout` function cannot use its own Layout, directly or through a helper.
- On navigation in the browser, the navigation data request runs only the `Load` and `Layout` functions of the listed routes. If an ancestor Layout's data is still in the cache, that Layout is not in the list and its `Layout` does not run this time. So do not put permission checks only in a Layout. Put them in `OnRequest`, or have the page's `Load` depend on the checking Layout through `tifa.Use`, so the check runs on every request.
- The document shell (`<html>`, `<head>`, assets and the script that injects data) is written as a string by the framework. React only renders the app root inside `<body>`, so the page is still a complete document when any level fails or returns 404. The root Layout (`app/layout.tsx`) can export `documentHead` to customize the shell, in the same format as the return value of `head`. Its tags are carried by every page, also when no route matches or the root `Layout` fails, and its `htmlAttributes` are the attributes of `<html>` every page starts from (`{ lang: 'en' }` when it has none, and a route's `head` can change them for its pages, see below). Put the site's default `title` here so that the 404 page for an unmatched URL also has a title. A page's own `head` that returns a `title` overrides it. Exporting a `Document` component fails the build.

```tsx
export const documentHead = {
  htmlAttributes: { lang: 'zh-CN' },
  meta: [{ title: 'Shop' }, { name: 'theme-color', content: '#f2f4f5' }],
  links: [{ rel: 'icon', href: '/favicon.svg' }],
}
```

- A page's head comes from the `head` return values of the matched routes: `title` and meta tags with the same name come from the deepest route that has them, which keeps every one of them it writes (a `theme-color` for each `media`, several `og:image`), and link, style and script tags are deduplicated in route order. The framework always adds `<meta charset="utf-8">` and a viewport meta, and a route's `name: 'viewport'` meta replaces the latter. Tag attributes are written the way React DOM writes them on the server: `className`, `htmlFor`, `httpEquiv`, `crossOrigin` and so on become their HTML names. Event attributes starting with `on`, invalid attribute names, `null`, `undefined` and functions are not written. Boolean attributes such as `async` and `defer` are written with an empty value when true. For other attributes, boolean values are written as strings only on `data-` and `aria-` attributes. `javascript:` links are replaced. Head tags do not support the `style` attribute, and using it makes rendering fail. `htmlAttributes` follows the same rules. The content of style and script tags is written as is. Only the `<script` and `</script` (in style, `<style` and `</style`) that would end the element are escaped, the way React DOM does it. After browser navigation and after `router.invalidate()`, the head updates to match the new routes.
- A `head` can also return `htmlAttributes`, the attributes of `<html>` for its pages, such as a theme class or a `data-` attribute. They are merged over the `htmlAttributes` of the root Layout's `documentHead` one attribute at a time: the deepest route that gives an attribute wins, `undefined` leaves it to the routes above, and `null` takes it away. Attributes are matched by the name React DOM writes, so `className` in one route and `class` in another are the same attribute. `class` is replaced as a whole, never merged with a value from elsewhere. `style` is not supported, as on the tags of the head. They are written by the same rules as the tags of the head. After browser navigation and after `router.invalidate()` they follow the new routes too. In the browser Tifa writes an attribute of `<html>` only when the value it wants for it changes, and takes away only attributes it wrote. So what a script of the page sets, such as the `dark` class of a dark mode script or the `lang` a translation tool changes, stays through hydration and navigation while the routes want the same value of that attribute. When that value changes, because a route sets another `class` or a navigation leaves it and the root Layout's `class` comes back, Tifa writes it and the class the script added is gone. Keep such a theme in an attribute no route sets, such as `data-theme`, or have the script add it again. A page without SSR gets these attributes in the browser.

```tsx
// app/guide/layout.tsx. Layout in route.go returns the theme of the guide.
export const head = ({ loaderData }: { loaderData?: LayoutData }) => ({
  htmlAttributes: { 'data-theme': loaderData?.theme },
})
```

- The routes that count are those whose head is on the page. A 404 or an error below the Layout keeps the Layout's attributes, also for a URL below the Layout that no page has, since its 404 shows inside the Layout (see [404](#404)). When the Layout itself fails, its `head` runs without data, so the example above leaves the root Layout's `data-theme`.
- On a site with languages Tifa writes `lang` itself, the language of the page. A `lang` in `htmlAttributes` fails the build there (see [A site in several languages](../start/app.md#a-site-in-several-languages)).
- `htmlAttributes` must be an object, and `pnpm typecheck` reports any other value. At run time such a value fails like an error the `head` throws: the server render fails as an unexpected error (`OnError`, or a 500), with the message in the log, and on the page under `tifa dev`. In the browser, after a navigation or on a page without SSR, the router writes the error to the console and drops the whole head of that route.

### Title and meta tags

Write the title and the meta tags of a page in its `head` export (or in `documentHead` of the root Layout), never as `<title>` or `<meta>` in a component. React 19 allows them in a component and moves them into the head in the browser, but Tifa writes the head of the document as a string and React renders only the app root inside `<body>`. So the server HTML has the tag in `<body>`, where crawlers and share cards do not read it, and the page can end up with two descriptions.

```tsx
// not this
export default function Page() {
  return <><title>Pour-over kettle</title><p>...</p></>
}

// this
export const head = () => ({ meta: [{ title: 'Pour-over kettle' }] })
```

`tifa check`, `tifa build` and `tifa dev` warn about a `<title>` or `<meta>` written as JSX, with its file and line. A `<title>` inside an `<svg>` is the tooltip of a graphic and is fine, and so is a `<meta itemProp>`.

### A Layout without the Layouts above

Name the component file `layout_.tsx` instead of `layout.tsx`, and the Layout of the folder shows its pages without any Layout above it, the root Layout included. Only the document shell the framework writes stays around them. The storefront of [examples/shop](../../examples/shop) works this way: it draws its own header and footer, and none of the workbench the root Layout draws around the other pages.

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
- What belongs to the document still applies: the root Layout's `documentHead` with its `htmlAttributes`, and `OnRequest`. What belongs to the Layouts above does not: their `head`, and their `ErrorBoundary`. When the `Layout` of a `layout_.tsx` fails, or returns `tifa.ErrNotFound`, no Layout is above it to show the error, so the framework's default page shows. Its own `ErrorBoundary` shows for what fails below it, as with any Layout. To show your own page when that `Layout` fails, put the `layout_.tsx` one folder higher, with an `<Outlet />` and an `ErrorBoundary`.
- In the browser, a move between a page below it and a page outside it swaps the whole frame, and the Layouts of the new page load their data when the router has none of it yet.
- The build fails for a `layout_.tsx` in `app/`, which has no Layout above it, for `layout.tsx` and `layout_.tsx` in the same folder, and for `tifa.Use` of a Layout that a `layout_.tsx` drops, from a page or Layout below it. It also fails for a Layout whose pages are all below a `layout_.tsx`, since it wraps none of them.
- React Router and TanStack Router also end a name with `_` to keep a route out of a Layout, but only out of the Layout of the segment before it: the root route and the Layouts further up still wrap it. Tifa's `layout_.tsx` drops every Layout above it, the root Layout too.

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
- A `NotFound` export in `page.tsx` or `layout.tsx` fails the build. Move what it shows into `ErrorBoundary`, under `error.status === 404`. An `Error` export fails the build too: rename it to `ErrorBoundary`.

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
- A component that throws while the page renders on the server is an unexpected error: `OnError` sees it once and picks the status (500 without `OnError`) and the public message. The page then renders again with that error in place of the page, as a `TifaError`, so the first `ErrorBoundary` from the page up shows it inside the Layouts. If that render fails too, because the `ErrorBoundary` or a Layout around it throws, the error moves one route up, in place of the Layout the page is in, and so on up to the outermost Layout, the root one or a `layout_.tsx`. Only when the error in place of that Layout fails too does the browser get a plain error page from Go with the status and the public message. The browser hydrates the page with the error where the server put it, and does not run the route that threw again. No browser or CDN keeps such an answer (`Cache-Control: no-store`). The built app remembers it for 5 seconds instead: a request for the same page with the same data and session in that time gets the same answer without a render, and `X-Tifa-Render` says `memo`. Requests that wait for the page at the same time share the renders that place the error. `OnError` still sees every request, the first with the whole error and the others with its first line and a note that it was not rendered again. `tifa dev` remembers nothing, so a fix shows on the next reload. The stack trace shows only under `tifa dev`, over the page with the `ErrorBoundary`, and the page reloads once you save a fix (see [`tifa dev`](../build-and-deploy/dev.md#when-a-page-fails-to-render)).
- A server render that uses up `ssr.vm_memory` or runs past `ssr.timeout` (see [config.md](../build-and-deploy/config.md)) stops right there. No `catch`, `finally` or promise handler in your code or in a library runs for it, so a page cannot catch it to show something else. The VM is replaced with a fresh one. A render that used up `ssr.vm_memory` renders again with the error once, on a fresh VM, in place of the outermost Layout, since the same data would use up the memory at any level. Its answer is remembered as above. A render that ran past `ssr.timeout`, or found every VM busy, does not: Go answers with its plain error page at once. All the renders of one request share one `ssr.timeout`, counted from the first, and a render with the error that runs out of it ends the tries. Code that recurses without end gets `RangeError: Maximum call stack size exceeded`, as in Node, which a `catch` can take (see [stack depth](../build-and-deploy/config.md#stack-depth)). When nothing catches it, it fails the render like any other error.
- The page that shows the error gets the head of a page whose `Load` failed. The route the error replaces runs its `head` without data, the routes below it add no tags, and the `head` of every route above it runs again over the matches as they now are, so `status` tells it that a route failed (see [A site in several languages](../start/app.md#a-site-in-several-languages)). Tags and `htmlAttributes` that a route gave for the page that did not render are not on the error page. The browser works out the same head when it hydrates.
- Once the browser has taken over, a component that throws shows the nearest `ErrorBoundary`, the same as a failed `Load`.
- The document shell stays complete whatever fails, so the page still loads its scripts and styles.

