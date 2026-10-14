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
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue?style=flat-square" alt="License"></a>
  <img src="https://img.shields.io/badge/Not%20backed%20by-Y%20Combinator-FF6600?style=flat-square&logo=ycombinator&logoColor=white" alt="Not backed by Y Combinator">
  <img src="https://img.shields.io/badge/users-unknown-lightgrey?style=flat-square" alt="Users unknown">
</p>

Write the backend in Go and the pages in React. tifa compiles the whole site into one executable that you copy to a small VPS and run. No Node in production, no process manager, no reverse proxy, no Redis.

It is for Go developers whose business logic and data already live in Go, who want to write pages with the React ecosystem, and who do not want to keep a Node service running in production just for server-side rendering.

> tifa is very early. APIs will change, and many compiler optimizations are not done yet.

## Why it exists

I wanted to ship a small product on TanStack Start. My other VPSes were full, Black Friday was still weeks away and I did not feel like buying another one, so I dug out an old box with 1 CPU and 1 GB of RAM. I had Codex set up the usual stack in one go: nginx, Node.js, PM2, TanStack Start, Redis, BullMQ, cron and SQLite.

It did not really run. It crawled.

So I started hacking on tifa. Once it is done, deploying should mean copying one Go binary to the VPS, and that is it.

| On a 1 CPU / 1 GB VPS | The usual stack | With tifa |
| :-- | :-- | :-- |
| Reverse proxy | nginx | The Go HTTP server in the binary |
| Runtime and process manager | Node.js + PM2 | One Go executable |
| React SSR | TanStack Start | TanStack Router, rendered by a pure Go QuickJS inside the Go process |
| Cache | Redis | An in-process HTML cache, plus whatever you keep in Go memory |
| Queues and scheduled jobs | BullMQ + cron | Goroutines you start in `OnStart` |
| Database | SQLite | SQLite, with a pure Go driver so CGO stays off |

I still do not know what to call it. A React SSR framework with no Node runtime? A React SSR framework with a Go backend? Both sound a little unhinged. Under the hood it is Go, a pure Go build of QuickJS, and TanStack Router.

## What it looks like

One folder per page. Go loads the data, React shows it:

```go
// app/projects/project+/orders/order+/route.go  →  /projects/{project}/orders/{order}
func Load(c *tifa.Ctx) (orders.Order, error) {
	return orders.Get(c.Param("project"), c.Param("order"))
}
```

```tsx
// app/projects/project+/orders/order+/route.tsx
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
go tool tifa build   # bin/<app>, with the frontend embedded
```

## Highlights

- **One file to deploy.** Pure Go, `CGO_ENABLED=0`, cross-compiles like any Go program. The browser bundle, styles and server renderer are embedded in the executable.
- **Real HTML on the first request.** Go runs the page's `Load` and every `Layout` function first, then renders React to complete HTML inside the process with a pure Go QuickJS. Search engines and link previews see the content, title and meta tags. Each page renders whole in one pass: there is no streaming and there are no React Server Components.
- **No Node to build.** `tifa build` and `tifa dev` bundle with esbuild's Go API. Node is only there to install npm packages and, if you like, run the type checker.
- **Types from Go to React.** The TypeScript type of each page's data is generated from what its Go `Load` returns.
- **Mistakes caught at build time.** Duplicate paths, a Layout reading a parameter it does not own, a `Load` result that cannot become TypeScript: the build stops and names the file and line.
- **Fast after the first page.** TanStack Router takes over in the browser. Each navigation is one request for data, and Layouts that are already loaded are not fetched again.

## Start an app

You need Go 1.27 and pnpm (npm works too):

```sh
go run github.com/liustack/tifa/cmd/tifa@latest new myapp
cd myapp
go mod tidy
pnpm install
go tool tifa dev     # http://127.0.0.1:9527
```

`tifa new` writes a home page with a Go `Load`, a `POST` the page calls from the browser, and a test of both. The module path is the folder name unless you pass `--module example.com/myapp`.

## Try the example

[examples/shop](examples/shop) is a small order workbench: two shops, an order list, order details, shipping, and three kinds of 404. You need Go 1.27 and pnpm:

```sh
cd examples/shop
pnpm install --frozen-lockfile
go tool tifa build
./bin/shop           # http://127.0.0.1:9527
```

## Documentation

| Doc | Read it when |
| :-- | :-- |
| [Structuring an app](docs/structure.md) | Deciding where domain code, storage and shared resources go |
| [Writing an app](docs/app.md) | Laying out folders, routes, Layouts, hooks, 404 pages, head tags and caching |
| [Build and run](docs/build.md) | You want to know what `tifa build` generates and how the program answers requests |
| [Dev server](docs/dev.md) | Running `tifa dev` locally, or a change did not reload |
| [Configuration](docs/config.md) | Deploying: the port, body limit, VMs, render timeout and page cache |
| [Testing](docs/testing.md) | Contributing to tifa itself |

## Talk to us

Issues are welcome any time: [open one](https://github.com/liustack/tifa/issues/new). Tell us what you built, where it hurt, and what should come next. Come find me on X: **[@liustack](https://x.com/liustack)**.

## Contributing

tifa does not accept pull requests. A framework like this touches the compiler, the bundler, server rendering and developer experience at once, and a change in one pulls on the others. Getting that right takes a lot of context, so every line is written and reviewed by one author. Two ways to help:

- **[Open an issue](https://github.com/liustack/tifa/issues).** Bugs, ideas, confusing errors, unclear docs. A clear issue with steps to reproduce helps more than a patch, and issues shape what gets built next.
- **Fork it.** Under MIT your copy is yours to change and publish.

## Star History

<a href="https://www.star-history.com/#liustack/tifa&Date">
 <picture>
   <source media="(prefers-color-scheme: dark)" srcset="https://api.star-history.com/svg?repos=liustack/tifa&type=Date&theme=dark" />
   <source media="(prefers-color-scheme: light)" srcset="https://api.star-history.com/svg?repos=liustack/tifa&type=Date" />
   <img alt="Star History Chart" src="https://api.star-history.com/svg?repos=liustack/tifa&type=Date" />
 </picture>
</a>

## License

[MIT](LICENSE)
