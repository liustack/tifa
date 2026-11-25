---
summary: 'layout.tsx and Layout, nesting, tifa.Use, layout_.tsx, instead of route groups'
audience: app
order: 2
read_when:
  - Writing a layout.tsx or the `Layout` function of a route.go, or sharing its data with `tifa.Use`
  - Giving a part of the site its own frame with `layout_.tsx`
  - Looking for route groups, a `(group)` folder or a Layout that adds nothing to the URL
  - Giving some pages of a site a frame that the other pages do not have, such as a landing page beside a console
  - Wondering why the page at `/` always shows inside the root Layout
---

# Layouts

## The Layout of a folder

A Layout lives in its folder next to the page of that folder, if there is one: the `Layout` function in `route.go` and `layout.tsx` wrap the page in the same folder and every page below it. `layout.tsx` places child routes with `<Outlet />`. Either may be left out: without `layout.tsx` the child routes render directly, and without a `Layout` function the Layout has no data. A Layout with no page in its folder or below it fails the build.

```go
package project

func Layout(c *tifa.Ctx) (projects.Project, error) {
	return projects.GetProject(c.Param("project"))
}
```

- `Layout` goes in `route.go`, next to `Load`, `SSR`, `Memo`, `Cache` and the method handlers of the same folder. Declaring it in any other file of the package, a `layout.go` for example, fails the build. A `route.go` with only a `Layout` is no API route.
- A Layout that reads the query string takes the parameters as its second parameter, as a `Load` does: `func Layout(c *tifa.Ctx, q Filter) (T, error)` (see [Query parameters](loading-data.md#query-parameters)). A name that a Layout and a page below it declare, or two Layouts of one chain, must have the same type in each, or the build fails and names the two places. A Layout that takes none takes only the Ctx.
- A Layout's `c.Param` may only read parameters of its own folder path. `Layout` in `app/projects/project+/route.go` may read `project`, not `order`. A page's `Load` is held to its own path the same way.
- When you switch between pages under the same Layout, the Layout component is not remounted and keeps its state. When a path parameter the Layout function reads changes (for example, you move to another project), the Layout remounts and its state resets. When a query parameter changes, even one it reads, the Layout loads its data again and renders it, but is not remounted and keeps its state, as TanStack Router does by default. So a filter keeps the view the visitor chose. State a component set up from the data of `Load`, such as with `useState(data.x)`, is not reset by a new query either. Where it should follow the new data, give the component a `key` from the value, or update the state in an effect. Pages work the same way.
- Pages and Layouts can call `tifa.Use(c, project.Layout)` to get the result of a Layout that wraps them, for the current request: the Layout of the same folder or of an ancestor folder, unless a `layout_.tsx` between drops it. The same `Layout` runs only once per request. Any other Layout fails the build, and so does an argument that is not a package-level `Layout` function, such as a closure. A `Layout` function cannot use its own Layout, directly or through a helper. A Layout with query parameters cannot be passed to `tifa.Use` yet, and the build says so: when other `Load` functions need the result of a Layout, give that Layout no query parameters, and read them in the `Load` of the page.
- On navigation in the browser, the navigation data request runs only the `Load` and `Layout` functions of the listed routes. If an ancestor Layout's data is still in the cache, that Layout is not in the list and its `Layout` does not run this time. So do not put permission checks only in a Layout. Put them in `OnRequest`, or have the page's `Load` depend on the checking Layout through `tifa.Use`, so the check runs on every request.
- The root Layout also holds the `documentHead` of the site (see [The document shell and documentHead](head.md#the-document-shell-and-documenthead)).

## A Layout without the Layouts above

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

## Instead of route groups

A route group is a folder that gives the pages in it a Layout and adds nothing to the URL: `(shop)` in Next.js, SvelteKit and Nuxt, a `_name` route in TanStack Router, a layout route without a path in React Router. Tifa has no route groups and will not add them. This section says what to write for each thing a route group is used for, and why Tifa leaves them out.

In Tifa every folder below `app/` is one URL segment, and a Layout wraps the pages of its folder and below. The words below use "frame" for what a Layout draws around its pages: a header, a sidebar, a footer.

### One part of the site has a frame of its own

Name the Layout file of that part `layout_.tsx`. Its pages show without any Layout above it, the root Layout too.

The tree and the rules are in [A Layout without the Layouts above](#a-layout-without-the-layouts-above).

### Several pages share a frame and can share a URL prefix

Put them in one folder. The folder is the prefix, and its Layout is the frame.

```text
app/
  layout.tsx                    the frame of the public pages
  page.tsx                      /
  pricing/
    page.tsx                    /pricing
  console/
    route.go  layout_.tsx       the frame of the console and its data
    page.tsx                    /console
    settings/
      route.go  page.tsx        /console/settings
```

Moving between `/console` and `/console/settings` keeps the frame mounted, with its state, and does not load its data again. Use `layout.tsx` in `console/` when the console should show inside the root Layout, and `layout_.tsx` when it should not.

### Several pages share a frame and their URLs stay at the same level

Say `/dashboard` and `/settings` share the frame of the console, next to `/` and `/pricing`, which do not have it, and no prefix is wanted. Give each folder its own Layout that renders the same component with the same data:

```text
app/
  layout.tsx                    the frame of the public pages
  page.tsx                      /
  pricing/
    page.tsx                    /pricing
  dashboard/
    route.go  layout_.tsx  page.tsx
  settings/
    route.go  layout_.tsx  page.tsx
components/
  console-frame.tsx             the frame, an ordinary component
console/
  frame.go                      package console: type Frame, func LoadFrame
```

```go
// app/dashboard/route.go, and the same function in app/settings/route.go
package dashboard

func Layout(c *tifa.Ctx) (console.Frame, error) {
	return console.LoadFrame(c.Context())
}
```

```tsx
// app/dashboard/layout_.tsx, and the same file in app/settings/
import { Outlet } from '@tanstack/react-router'
import { ConsoleFrame } from '@/components/console-frame'
import { layout } from './route.gen'

export default function Layout() {
  return (
    <ConsoleFrame frame={layout.useLoaderData()}>
      <Outlet />
    </ConsoleFrame>
  )
}
```

What this costs: `/dashboard` and `/settings` are two Layouts that look the same. When the visitor moves from one to the other, the frame is mounted again, so state it holds, such as an open menu or how far a list is scrolled, starts over, and its data is loaded again, in the same request as the data of the page. When that matters, give the pages a prefix as above.

### The page at `/`

`app/` cannot hold a `layout_.tsx`, so the page at `/` always shows inside the root Layout. Make the root Layout the frame of the page at `/`, and let the parts with another frame leave it with `layout_.tsx`, as in the trees above. An app whose pages share no frame at all can leave the root `layout.tsx` out.

### Why Tifa has no route groups

- **The path is the URL.** Every folder below `app/` is one URL segment and one Go package, so you read the URL off the path of a file and find the file from the URL. With route groups a folder may or may not count, and two folders can give the same URL.
- **The name would be Tifa's own.** A folder under `app/` is a Go package, and `(shop)` is not a valid Go import path. Tifa would need a spelling that no other framework has.
- **`layout_.tsx` already does the common case.** A part of the site that leaves the frame above it needs no group. Route groups would be a second way to do the same thing.

This is settled, and route groups are not planned. A folder named `(name)` fails `tifa check` and `tifa build` with a line that says so.
