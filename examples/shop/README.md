English · [简体中文](README.zh-CN.md)

# Shanhai Workbench (山海工作台): a tifa example app

Two sides of one small shop, in one program.

- **The workbench**, for the shop owner: two shops, an order list to sort, filter and search, order details, a shipping form, a dialog to change where an order goes, and a sales stats page with charts. After you ship an order the list and the stats update with it.
- **The storefront**, for customers, in Chinese and English: a home page, a product list with search and filters, product pages with reviews, a cart kept in a cookie, a checkout with a long form, and a journal of articles in Markdown with highlighted code. Prices are in yuan or dollars, and dates and numbers follow the language.

Pages are rendered to complete HTML on the server. After the browser takes over, it moves between pages on its own. Data lives in process memory and resets on restart.

The interface is built with Tailwind CSS and shadcn/ui, the order list with TanStack Table, the forms with react-hook-form and zod, the charts with recharts, and the numbers tween with react-spring. The storefront is translated with i18next, and the journal is rendered with react-markdown, with code highlighted by highlight.js.

It shows what a tifa app looks like: how folders are grouped by business area, how Go prepares data for pages, and how React components read that data.

## Run it

Run in the `examples/shop` folder. You need Go 1.27, Node 20.19 or 22.12 and later, and pnpm. The build bundles the front end with Vite on Node, and the built `bin/shop` runs without Node:

```sh
pnpm install --frozen-lockfile
go tool tifa build
./bin/shop
```

Open <http://127.0.0.1:9527>. It redirects to the order list of the first shop. The storefront is at <http://127.0.0.1:9527/store/zh-CN> and <http://127.0.0.1:9527/store/en-US>. Use `./bin/shop --server-addr 127.0.0.1:9528` to change the port. `bin/shop` is a single executable with the frontend output embedded, so you can copy it to another machine and run it there.

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
        revenue-chart.client.tsx  products-chart.client.tsx    the charts, loaded in the browser only
  store/
    store.go                   what the storefront shares: its two languages, their currency and time zone, prices
    i18n.ts  locales/          i18next with the texts of both languages, money and dates formatted per language
    product-card.tsx           the product card of every list
    lang+/                     /store/{lang}
      layout.go  layout.tsx    storefront Layout: categories, search, language switch, cart count, footer
      route.go  route.tsx      home page
      products/
        product.go  catalogue.go  review.go    catalogue domain: products, search, filters and their counts, reviews
        route.go  route.tsx    product list, filters in the query string
        filter-schema.ts       the zod schema of the filter form
        product+/              /store/{lang}/products/{product}: product page, POST adds to the cart
      cart/
        cart.go  checkout.go   cart domain: lines, coupons, delivery, the checks of an order, placing it
        route.go  route.tsx    /store/{lang}/cart: PATCH changes a quantity, POST applies a coupon
      checkout/                /store/{lang}/checkout: the order form, POST places the order
        checkout-schema.ts     the zod schema of the order, with the same rules as Order.Validate
      journal/
        post.go  posts/        journal domain: articles in Markdown, one file per language
        markdown.tsx           Markdown with GitHub tables and highlighted code
        post+/                 /store/{lang}/journal/{post}
```

Each domain sits in its folder under `app/`, next to the pages that show it, with its tests: `app/projects` holds the shop domain, and `app/projects/project+/orders` the order domain, one level above the folder of one order rather than inside it. The root package holds only the hooks. Storage is in memory with demo data, standing in for database tables, and changes are lost on restart. Each folder under `app/` is a segment of the URL. `route.go` holds `Load`, which calls the domain packages, and the order page's `POST` and `PATCH`, whose inputs check themselves with `Validate`. `route.tsx` holds the UI and reads the result of `Load` through `./+route`. The order and stats pages read their shop through the shop Layout with `tifa.Use(c, project.Layout)`. `app/route.go` is an API route whose `GET` redirects `/` to the shop `studio`. The shop domain counts the orders waiting to ship, so `projects` imports `orders`, and `orders` imports neither `projects` nor the pages. See [Philosophy](../../docs/philosophy.md) for why, and [Structuring an app](../../docs/structure.md) for which way imports may go.

Go sends times as instants, and pages format them with `Intl` in the shop's time zone, which the shop Layout passes along. A browser in another zone shows the same times as the server render.

The storefront takes its language from the first segment of the URL. Go loads the texts in that language and the prices in its currency, and the storefront Layout passes the currency and time zone to i18next, which formats money, dates and plurals with `Intl`. The checks of a form live twice with the same rules: a zod schema in the browser and `Validate` in Go. Both give the same message keys, which the page translates. The catalogue domain is `products`, and `cart` imports it to price the lines. Pages under `lang+` find their language with `store.FindLocale`.

## Things to try

- View the page source. The first ten orders, the totals and the customer details are already in the HTML.
- Sort, filter and search the order list. It all happens in the browser, with no request.
- Open "店铺备忘" (shop notes), then go to an order's details. The notes stay open. Switch to the other shop: the shop notes close.
- Collapse the sidebar and reload. The server renders it collapsed, because the state is kept in a cookie.
- Watch the Network tab in the developer tools: taking over the first page makes no extra data request, and each navigation makes at most one data request.
- Press "确认发货" (ship) with the form empty, or with a tracking number that is too short. The browser shows the errors at once. Go checks the same rules again and returns its errors field by field when a request gets past the browser.
- Ship an order. Its status, the shop's count of orders waiting to ship and the list totals when you go back all update together.
- Change the recipient of an order waiting to ship with "修改收货信息". Once the order has shipped, the button is disabled and Go refuses the change.
- Open the stats page and switch between 7 and 30 days. The key numbers come in the HTML. The charts load and draw only in the browser, and until then the HTML holds a placeholder of the same height. Their code is fetched only when a stats page opens.
- Open `/projects/studio/orders/SH-0000`, `/projects/nope` and `/nowhere`. The three 404s show at the order level, the root Layout level and the whole page.
- Switch the storefront between 中文 and English. Prices change between yuan and dollars, and dates, review counts and delivery days follow the language.
- Search and filter the product list. The filters go into the URL, so a filtered list can be shared and the server renders it as it is. Set a minimum price above the maximum: the form refuses before sending anything.
- Add a product to the cart in a colour, change quantities in the cart and try the codes `SHANHAI10` and `WELCOME30`. The first change gives you a cart of your own in a cookie.
- Check out with an empty form, a postal code of the wrong shape, or a card number that fails the check digit. The errors show field by field, and Go returns the same ones when a request gets past the browser.
- Open a journal article. The code blocks are highlighted in the HTML the server sends.

For more conventions, see [Build and generated code](../../docs/build.md) and [Writing an app](../../docs/app.md).
