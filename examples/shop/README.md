English · [简体中文](README.zh-CN.md)

# 山海工作台: a tifa example app

An order workbench for a small shop owner: two shops, an order list, order details and shipping. Pages are rendered to complete HTML on the server. After the browser takes over, it moves between pages on its own, and after you ship an order the list and the stats update with it. Data lives in process memory and resets on restart.

It shows what a tifa app looks like: how folders are grouped by business area, how Go prepares data for pages, and how React components read that data.

## Run it

Run in the `examples/shop` folder. You need Go 1.27 and pnpm (Node is only used to install packages and type-check, the build does not need it):

```sh
pnpm install --frozen-lockfile
go tool tifa build
./dist/shop
```

Open <http://127.0.0.1:8080>. It redirects to the order list of the first shop. Use `./dist/shop --server-addr 127.0.0.1:8081` to change the port. `dist/shop` is a single executable with the frontend output embedded, so you can copy it to another machine and run it there.

Type checking and tests:

```sh
pnpm typecheck
go vet ./...
go test ./...
```

## Folders

```text
hooks.go                     opens the data at startup, blocks cross-site write requests, logs unexpected errors
db/                          global data store (in memory), shared by business packages
app/
  layout.go  layout.tsx      root Layout: sidebar, top bar, document shell and site-wide 404
  icons.tsx  styles.css      icons and styles shared across pages
  home/route.go              redirects / to the first shop
  projects/
    project.go               shop domain: types and the count of orders waiting to ship
    layout.go  layout.tsx    shop Layout: shop name, orders waiting to ship, shop notes
    orders/
      order.go               order domain: list, details, shipping rules
      list/                  /projects/{projectId}
      detail/                /projects/{projectId}/orders/{orderId}, POST ships the order
```

In each page folder, `route.go` holds the path and `Load`, and `route.tsx` holds the UI and reads the result of `Load` through `./+route`. A business folder can be taken out or deleted as a whole.

## Things to try

- View the page source. The orders and customer details are already in the HTML.
- Open "店铺备忘" (shop notes), then go to an order's details. The notes stay open. Collapsing the sidebar works the same way. Switch to the other shop: the shop notes close, and the sidebar keeps its state.
- Watch the Network tab in the developer tools: taking over the first page makes no extra data request, and each navigation makes exactly one data request.
- Click "标记为已发货" (mark as shipped). The button, the shop's count of orders waiting to ship, and the list stats when you go back all update together.
- Open `/projects/studio/orders/SH-0000`, `/projects/nope` and `/nowhere`. The three 404s show at the order level, the root Layout level and the whole page.

For more conventions, see [Build and generated code](../../docs/build.md) and [Writing an app](../../docs/app.md).
