---
summary: 'How to structure a tifa app: group code by domain, start flat in the root package, split a domain out when it grows, wire dependencies by hand'
read_when:
  - Starting a new tifa app
  - Deciding where a type, a query or a shared resource goes
  - An app has grown and you wonder whether a domain should get its own package
---

English · [简体中文](structure.zh-CN.md)

# Structuring an app

A tifa app is a Go program and follows Go's habits. Three rules:

1. Group code by domain, not by kind.
2. Start flat in the root package. Split a domain into its own package only when it grows.
3. Wire dependencies by hand.

## 1. Group code by domain

Code is grouped by what it is about (orders, projects, customers), not by what kind of code it is. A domain keeps its types, storage, queries and rules together. There is no `models/`, `services/`, `repositories/`, `handlers/`, `utils/` or `db/` package that collects one kind of code from every domain.

The test: you can take a domain out, review it or delete it as one unit.

Pages already work this way. Under `app/`, folders follow the business, and a page's `route.go` and `route.tsx` sit in the same folder.

## 2. Start flat, split when it grows

A new app puts its domain code in the root package, the one at the module root next to `hooks.go`, with one file per domain:

```text
myapp/
  go.mod
  hooks.go              OnStart, OnShutdown, OnRequest, OnError
  db.go                 var DB, opened in OnStart and closed in OnShutdown
  project.go            Project, GetProject, ListProjects
  order.go              Order, ListOrders, GetOrder, ShipOrder and their queries
  order_test.go
  app/
    layout.go  layout.tsx
    projects/
      layout.go  layout.tsx
      orders/
        list/    route.go  route.tsx
        detail/  route.go  route.tsx
```

Folders under `app/` stay thin. They hold pages, and `route.go` reads the request and calls the root package:

```go
// app/projects/orders/detail/route.go
package detail

import (
	"github.com/liustack/tifa"
	shop "github.com/liustack/tifa/examples/shop"
)

const Path = "/projects/{projectId}/orders/{orderId}"

func Load(c *tifa.Ctx) (shop.Order, error) {
	return shop.GetOrder(c.Param("projectId"), c.Param("orderId"))
}
```

One package means domains call each other directly, nothing is exported only so a sibling package can reach it, and there are no import cycles to untangle. [examples/shop](../examples/shop) is written this way.

Move a domain into its own package when one of these happens:

- Names start repeating a prefix: `OrderList`, `OrderStatus`, `orderRecord`, `ordersMu`. As a package they read `orders.List`, `orders.Status`.
- The root package gets too big to find your way around, or one person or team owns a domain.
- You want a small public API with the details hidden behind it.

Put the new package next to the pages that use it, for example `app/projects/orders/order.go` as `package orders`, or at the module root such as `orders/` when pages in several places use it. Export only what other packages call.

Mind the import direction. Packages under `app/` and packages you split out may import the root package, for `DB` for example. The root package never imports them back, because Go does not allow import cycles. So split callers before the domains they call: if `project.go` counts the orders of a project, move projects out first (or together with orders), and keep orders in the root until then.

## 3. Wire dependencies by hand

No dependency injection container, no framework, no reflection or generated code for wiring. Dependencies are plain Go values.

- **Shared resources** such as a database connection, a logger or a mail client live in files at the root, next to `hooks.go`: `db.go`, `mail.go`, `middleware.go`. Each one builds a package-level variable or a function. Set it up in `OnStart`, release it in `OnShutdown`, and use it directly anywhere:

  ```go
  // db.go
  package shop

  var DB *sql.DB

  func openDB() (err error) {
  	DB, err = sql.Open("sqlite", "shop.db")
  	return err
  }
  ```

  ```go
  // hooks.go
  func OnStart() error                       { return openDB() }
  func OnShutdown(ctx context.Context) error { return DB.Close() }
  ```

- **Request values**, such as the signed-in user, ride on the standard context: `OnRequest` calls `c.SetContext(context.WithValue(...))`, and the domain provides a typed getter.
- **Arguments** are fine when only some callers pass a different value. A function that needs a clock, a limit or a client can take it as a parameter.
- **Tests** replace the variable (`DB = openTestDB(t)`) or pass a fake as an argument. Declare an interface in the package that uses it, and only once there really are two implementations, such as the real one and a fake.
- Do not set things up in `init()`, and do not read a resource while package-level variables are initialized. Resources exist only after `OnStart` has run.
