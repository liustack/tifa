English · [简体中文](README.zh-CN.md)

# Shanhai Workbench (山海工作台): a tifa example app

An order workbench for a small shop owner: two shops, an order list, order details and shipping. Pages are rendered to complete HTML on the server. After the browser takes over, it moves between pages on its own, and after you ship an order the list and the stats update with it. Data lives in process memory and resets on restart.

It shows what a tifa app looks like: how folders are grouped by business area, how Go prepares data for pages, and how React components read that data.

## Run it

Run in the `examples/shop` folder. You need Go 1.27, Node and pnpm. The build bundles the front end with Vite on Node, and the built `bin/shop` runs without Node:

```sh
pnpm install --frozen-lockfile
go tool tifa build
./bin/shop
```

Open <http://127.0.0.1:9527>. It redirects to the order list of the first shop. Use `./bin/shop --server-addr 127.0.0.1:9528` to change the port. `bin/shop` is a single executable with the frontend output embedded, so you can copy it to another machine and run it there.

Type checking and tests:

```sh
pnpm typecheck
go vet ./...
go test ./...
```

## Folders

```text
hooks.go                       blocks cross-site write requests, logs unexpected errors
app/
  layout.go  layout.tsx        root Layout: sidebar, top bar, document shell and site-wide 404
  icons.tsx  styles.css        icons and styles shared across pages
  route.go                     redirects / to the first shop
  projects/
    project.go                 shop domain: types, the count of orders waiting to ship, storage
    project+/                  /projects/{project}
      layout.go  layout.tsx    shop Layout: shop name, orders waiting to ship, shop notes
      route.go  route.tsx      order list
      orders/
        order.go               order domain: list, details, shipping rules, storage
        order+/                /projects/{project}/orders/{order}: order details, POST ships the order
```

Each domain sits in its folder under `app/`, next to the pages that show it, with its tests: `app/projects` holds the shop domain, and `app/projects/project+/orders` the order domain, one level above the folder of one order rather than inside it. The root package holds only the hooks. Storage is in memory with demo data, standing in for database tables, and changes are lost on restart. Each folder under `app/` is a segment of the URL. `route.go` holds `Load`, which calls the domain packages, and the order page's `POST`, which ships the order, and `route.tsx` holds the UI and reads the result of `Load` through `./+route`. The order page reads its shop through the shop Layout with `tifa.Use(c, project.Layout)`. `app/route.go` is an API route whose `GET` redirects `/` to the shop `studio`. The shop domain counts the orders waiting to ship, so `projects` imports `orders`, and `orders` imports neither `projects` nor the pages. See [Philosophy](../../docs/philosophy.md) for why, and [Structuring an app](../../docs/structure.md) for which way imports may go.

## Things to try

- View the page source. The orders and customer details are already in the HTML.
- Open "店铺备忘" (shop notes), then go to an order's details. The notes stay open. Collapsing the sidebar works the same way. Switch to the other shop: the shop notes close, and the sidebar keeps its state.
- Watch the Network tab in the developer tools: taking over the first page makes no extra data request, and each navigation makes exactly one data request.
- Click "标记为已发货" (mark as shipped). The button, the shop's count of orders waiting to ship, and the list stats when you go back all update together.
- Open `/projects/studio/orders/SH-0000`, `/projects/nope` and `/nowhere`. The three 404s show at the order level, the root Layout level and the whole page.

For more conventions, see [Build and generated code](../../docs/build.md) and [Writing an app](../../docs/app.md).
