English · [简体中文](README.zh-CN.md)

# Shanhai Workbench (山海工作台): a tifa example app

An order workbench for a small shop owner: two shops, an order list to sort, filter and search, order details, a shipping form, a dialog to change where an order goes, and a sales stats page with charts. Pages are rendered to complete HTML on the server. After the browser takes over, it moves between pages on its own, and after you ship an order the list and the stats update with it. Data lives in process memory and resets on restart.

The interface is built with Tailwind CSS and shadcn/ui, the list with TanStack Table, the forms with react-hook-form and zod, the charts with recharts, and the numbers tween with react-spring.

It shows what a tifa app looks like: how folders are grouped by business area, how Go prepares data for pages, and how React components read that data.

## Run it

Run in the `examples/shop` folder. You need Go 1.27, Node 20.19 or 22.12 and later, and pnpm. The build bundles the front end with Vite on Node, and the built `bin/shop` runs without Node:

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
components/ui/  lib/  hooks/   shadcn/ui components and their helpers, imported as @/components/ui/...
app/
  layout.go  layout.tsx        root Layout: sidebar, top bar, document shell and site-wide 404
  styles.css                   Tailwind CSS and the shop's theme
  format.ts                    money and times in Chinese, in the shop's time zone
  animated-number.tsx          numbers that tween when the data changes
  route.go                     redirects / to the first shop
  projects/
    project.go                 shop domain: types, the count of orders waiting to ship, storage
    project+/                  /projects/{project}
      layout.go  layout.tsx    shop Layout: shop name, orders waiting to ship, shop notes, links to orders and stats
      route.go  route.tsx      order list
      columns.tsx              the columns of the order list
      orders/
        order.go  shipping.go  order domain: list, details, shipping and recipient rules, storage
        stats.go               sales of the last thirty days
        order+/                /projects/{project}/orders/{order}: order details, POST ships, PATCH changes the recipient
          ship-form.tsx  recipient-dialog.tsx  items.tsx
      stats/                   /projects/{project}/stats: sales stats
```

Each domain sits in its folder under `app/`, next to the pages that show it, with its tests: `app/projects` holds the shop domain, and `app/projects/project+/orders` the order domain, one level above the folder of one order rather than inside it. The root package holds only the hooks. Storage is in memory with demo data, standing in for database tables, and changes are lost on restart. Each folder under `app/` is a segment of the URL. `route.go` holds `Load`, which calls the domain packages, and the order page's `POST` and `PATCH`, whose inputs check themselves with `Validate`. `route.tsx` holds the UI and reads the result of `Load` through `./+route`. The order and stats pages read their shop through the shop Layout with `tifa.Use(c, project.Layout)`. `app/route.go` is an API route whose `GET` redirects `/` to the shop `studio`. The shop domain counts the orders waiting to ship, so `projects` imports `orders`, and `orders` imports neither `projects` nor the pages. See [Philosophy](../../docs/philosophy.md) for why, and [Structuring an app](../../docs/structure.md) for which way imports may go.

Go sends times as instants, and pages format them with `Intl` in the shop's time zone, which the shop Layout passes along. A browser in another zone shows the same times as the server render.

## Things to try

- View the page source. The first ten orders, the totals and the customer details are already in the HTML.
- Sort, filter and search the order list. It all happens in the browser, with no request.
- Open "店铺备忘" (shop notes), then go to an order's details. The notes stay open. Switch to the other shop: the shop notes close.
- Collapse the sidebar and reload. The server renders it collapsed, because the state is kept in a cookie.
- Watch the Network tab in the developer tools: taking over the first page makes no extra data request, and each navigation makes at most one data request.
- Press "确认发货" (ship) with the form empty, or with a tracking number that is too short. The browser shows the errors at once. Go checks the same rules again and returns its errors field by field when a request gets past the browser.
- Ship an order. Its status, the shop's count of orders waiting to ship and the list totals when you go back all update together.
- Change the recipient of an order waiting to ship with "修改收货信息". Once the order has shipped, the button is disabled and Go refuses the change.
- Open the stats page and switch between 7 and 30 days. The key numbers come in the HTML, the charts are drawn only in the browser.
- Open `/projects/studio/orders/SH-0000`, `/projects/nope` and `/nowhere`. The three 404s show at the order level, the root Layout level and the whole page.

For more conventions, see [Build and generated code](../../docs/build.md) and [Writing an app](../../docs/app.md).
