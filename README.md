<h1 align="center">tifa</h1>

<p align="center"><b>The full-stack framework for Gophers.</b></p>

<p align="center">
  <a href="./README.zh-CN.md">简体中文</a> ·
  <a href="docs/app.md">Writing an app</a> ·
  <a href="docs/build.md">Build and run</a> ·
  <a href="docs/dev.md">Dev server</a> ·
  <a href="examples/shop">Example app</a>
</p>

<p align="center">
  <a href="https://x.com/liustack"><img src="https://img.shields.io/badge/follow-%40liustack-black?style=flat-square&logo=x&logoColor=white" alt="Follow @liustack on X"></a>
  <a href="https://pkg.go.dev/github.com/liustack/tifa"><img src="https://img.shields.io/badge/go.dev-reference-007d9c?style=flat-square&logo=go&logoColor=white" alt="Go reference"></a>
  <a href="go.mod"><img src="https://img.shields.io/github/go-mod/go-version/liustack/tifa?style=flat-square" alt="Go version"></a>
  <img src="https://img.shields.io/badge/CGO-off-success?style=flat-square" alt="CGO off">
  <img src="https://img.shields.io/badge/Not%20backed%20by-Y%20Combinator-FF6600?style=flat-square&logo=ycombinator&logoColor=white" alt="Not backed by Y Combinator">
  <img src="https://img.shields.io/badge/users-unknown-lightgrey?style=flat-square" alt="Users unknown">
</p>

Write the backend in Go and the pages in React. tifa compiles the whole site into one executable that you copy to a small VPS and run. No Node in production, no process manager, no reverse proxy, no Redis.

It is for Go developers whose business logic and data already live in Go, who want to write pages with the React ecosystem, and who do not want to keep a Node service running in production just for server-side rendering.

> tifa is early. APIs will change.

## What it looks like

One folder per page. Go loads the data, React shows it:

```go
// app/projects/orders/detail/route.go
const Path = "/projects/{projectId}/orders/{orderId}"

func Load(c *tifa.Ctx) (orders.Order, error) {
	return orders.Get(c.Param("projectId"), c.Param("orderId"))
}
```

```tsx
// app/projects/orders/detail/route.tsx
import { route, type Data } from './+route'

export default function OrderPage() {
  const order = route.useLoaderData() // typed from the Go return value
  return <h2>{order.id}</h2>
}

// title and meta tags are written into the HTML on the server
export const head = ({ loaderData }: { loaderData?: Data }) => ({
  meta: [{ title: `Order ${loaderData?.id}` }],
})
```

```sh
go get -tool github.com/liustack/tifa/cmd/tifa
go tool tifa dev     # rebuilds and reloads the browser as you edit
go tool tifa build   # dist/<app>, with the frontend embedded
```

## Highlights

- **One file to deploy.** Pure Go, `CGO_ENABLED=0`, cross-compiles like any Go program. The browser bundle, styles and server renderer are embedded in the executable.
- **Real HTML on the first request.** Go runs every `Load` for the page and its Layouts first, then renders React to complete HTML inside the process with a pure Go QuickJS. Search engines and link previews see the content, title and meta tags.
- **No Node to build.** `tifa build` and `tifa dev` bundle with esbuild's Go API. Node is only there to install npm packages and, if you like, run the type checker.
- **Types from Go to React.** The TypeScript type of each page's data is generated from what its Go `Load` returns.
- **Mistakes caught at build time.** Duplicate paths, a Layout reading a parameter it does not own, a `Load` result that cannot become TypeScript: the build stops and names the file and line.
- **Fast after the first page.** TanStack Router takes over in the browser. Each navigation is one request for data, and Layouts that are already loaded are not fetched again.

## What it costs

A single render is slower than Node. An order detail page takes about 5 ms to render on an Apple M4 once warm. tifa makes up for it with a pool of prewarmed VMs, an HTML cache keyed by the data, and ETags. There is no streaming and there are no React Server Components.

## Try the example

[examples/shop](examples/shop) is a small order workbench: two shops, an order list, order details, shipping, and three kinds of 404. You need Go 1.27 and pnpm:

```sh
cd examples/shop
pnpm install --frozen-lockfile
go tool tifa build
./dist/shop          # http://127.0.0.1:8080
```

## Documentation

| Doc | Read it when |
| :-- | :-- |
| [Writing an app](docs/app.md) | Laying out folders, routes, Layouts, hooks, 404 pages, head tags and caching |
| [Build and run](docs/build.md) | You want to know what `tifa build` generates and how the program answers requests |
| [Dev server](docs/dev.md) | Running `tifa dev` locally, or a change did not reload |
| [Testing](docs/testing.md) | Contributing to tifa itself |

## Talk to us

Issues are welcome any time: [open one](https://github.com/liustack/tifa/issues/new). Tell us what you built, where it hurt, and what should come next. Come find me on X: **[@liustack](https://x.com/liustack)**.

## Contributing

tifa does not accept pull requests for now. One author reviews every line, which keeps the design small and consistent. Two ways to help:

- **[Open an issue](https://github.com/liustack/tifa/issues).** Bugs, ideas, confusing errors, unclear docs. Issues shape what gets built next.
- **Fork it.** Your copy is yours to change.

## Star History

<a href="https://www.star-history.com/#liustack/tifa&Date">
 <picture>
   <source media="(prefers-color-scheme: dark)" srcset="https://api.star-history.com/svg?repos=liustack/tifa&type=Date&theme=dark" />
   <source media="(prefers-color-scheme: light)" srcset="https://api.star-history.com/svg?repos=liustack/tifa&type=Date" />
   <img alt="Star History Chart" src="https://api.star-history.com/svg?repos=liustack/tifa&type=Date" />
 </picture>
</a>
