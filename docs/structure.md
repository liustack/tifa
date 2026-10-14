---
summary: 'How to structure a tifa app: group code by domain, keep each domain in its folder under app/ next to its pages, keep only hooks and shared resources at the root, which way packages may import each other, wire dependencies by hand'
read_when:
  - Starting a new tifa app
  - Deciding where a type, a query or a shared resource goes
  - A domain has grown, or two domains need to call each other
  - The go command reports an import cycle between packages of an app
---

English · [简体中文](structure.zh-CN.md)

# Structuring an app

A tifa app is a Go program and follows Go's habits. Three rules:

1. Group code by domain, not by kind.
2. A domain is a folder under `app/`. Its code sits next to its pages, and the root keeps only the hooks and what the whole app shares.
3. Wire dependencies by hand.

## 1. Group code by domain

Code is grouped by what it is about (orders, projects, customers), not by what kind of code it is. A domain holds everything it needs: its types, storage, queries, rules and, in tifa, its pages. There is no `models/`, `services/`, `repositories/`, `handlers/`, `utils/` or `db/` package that collects one kind of code from every domain.

The test: you can take a domain out, review it or delete it as one unit.

## 2. A domain is a folder under app/

Folders under `app/` already follow the business, since they are the URLs. So a domain lives in its folder, next to the pages that show it:

```text
myapp/
  go.mod
  hooks.go                  OnStart, OnShutdown, OnRequest, OnError
  db.go                     var DB, opened in OnStart and closed in OnShutdown
  schema.sql                the tables of every domain
  app/
    layout.go  layout.tsx  route.go
    projects/
      project.go            Project, GetProject, ListProjects
      project_test.go
      route.go  route.tsx   the project list, if there is one
      project+/
        layout.go  layout.tsx  route.go  route.tsx
        orders/
          order.go          Order, ListOrders, GetOrder, ShipOrder and their queries
          order_test.go
          route.go  route.tsx
          order+/
            route.go  route.tsx
```

- **Where the code goes.** A domain gets a file named after it in its folder: `app/projects/project.go` holds `package projects`. When a domain has a list page and a page per item, its code goes in the list folder (`orders/`), not in the folder of one item (`order+/`). The list folder does not need a page: in [examples/shop](../examples/shop) the orders are listed on the project page, and `orders/` holds only the domain.
- **Package names.** A literal folder takes its own name (`projects`, `orders`). A parameter folder takes the parameter name (`project+` holds `package project`), as in [app.md](app.md#urls-come-from-folders).
- **Pages use the domain.** A page in the same folder is the same package and calls the domain directly. Pages below import it:

  ```go
  // app/projects/project+/orders/order+/route.go
  package order

  import (
  	"github.com/liustack/tifa"
  	"github.com/liustack/tifa/examples/shop/app/projects/project+"
  	"github.com/liustack/tifa/examples/shop/app/projects/project+/orders"
  )

  func Load(c *tifa.Ctx) (orders.Order, error) {
  	p, err := tifa.Use(c, project.Layout)
  	if err != nil {
  		return orders.Order{}, err
  	}
  	return orders.GetOrder(p.ID, c.Param("order"))
  }
  ```

- **Tests sit next to the code** they test: `order_test.go` next to `order.go`.
- **The root package** holds only the hooks and the resources the whole app shares, such as `db.go` and `mail.go`. No domain code. The database schema stays here too: the tables of all domains make up one database, `OnStart` creates them before the app listens, and the root package may not import the domains (see below). Each domain keeps its own queries.
- Code that belongs to no domain and that several domains use, such as formatting money, goes in its own package named after what it does, such as `money/` at the module root. Any number of domains may import it. It cannot import a domain that imports it. It may import the root package, but then the root package, `hooks.go` included, cannot import it. When it imports neither domains nor the root package, every package of the app may import it, the root package too.

### Which way imports go

Go refuses import cycles: if package A imports B, directly or through other packages, B cannot import A. The go command stops with `import cycle not allowed`. In a tifa app this means:

- **Domains import the root package** by its own name, for `DB` for example: `import "example.com/myapp"`, then `myapp.DB`. The root package, `hooks.go` included, does not import packages under `app/`: a domain imports the root package, so importing that domain, or a page that imports it, is a cycle. The same holds for the root package's tests in `package myapp`. A test in `package myapp_test` may import them.
- **Pages import domains**: the order page in `order+/` imports `orders` from the folder above, and the project Layout imports `projects`. A page may also import the package of a Layout above it, for `tifa.Use(c, project.Layout)`.
- **A domain may import another domain, one way only.** In examples/shop, `projects` counts the orders waiting to ship with `orders.CountPending`, so `projects` imports `orders`, and `orders` cannot import `projects`. Where the folders sit does not matter: a folder may import one below it, above it or beside it, as long as nothing imports back.
- **A domain does not import its own pages.** `order+/` imports `orders`, so `orders` cannot import `order+/`. Keep what both need in the domain.
- **Tests in the package count as the package.** A test in `package orders` cannot import `projects`, because `projects` imports `orders` (`import cycle not allowed in test`). Put such a test in `package orders_test`.

When two domains need each other, move the part both need into one of them, or into a new domain that both import.

### When a domain grows

- Split its code into more files in the same folder, one per topic: `order.go`, `shipping.go`, `refund.go`. It stays one package.
- A part with pages of its own becomes a folder below, holding its code and its pages, such as `order+/refunds/`. Once it imports the domain above it, the domain above cannot import it back.
- When a new call would close a cycle, the code is usually in the wrong domain.

## 3. Wire dependencies by hand

No dependency injection container, no framework, no reflection or generated code for wiring. Dependencies are plain Go values.

- **Shared resources** such as a database connection, a logger or a mail client live in files at the root, next to `hooks.go`: `db.go`, `mail.go`, `middleware.go`. Each one builds a package-level variable or a function. Set it up in `OnStart`, release it in `OnShutdown`, and use it directly anywhere:

  ```go
  // db.go
  package myapp

  var DB *sql.DB

  func openDB() (err error) {
  	DB, err = sql.Open("sqlite", "app.db")
  	return err
  }
  ```

  ```go
  // hooks.go
  func OnStart() error                       { return openDB() }
  func OnShutdown(ctx context.Context) error { return DB.Close() }
  ```

  ```go
  // app/todos/todo.go
  package todos

  import "example.com/myapp"

  func GetTodo(ctx context.Context, id int64) (Todo, error) {
  	t := Todo{ID: id}
  	err := myapp.DB.QueryRowContext(ctx, `SELECT title, done FROM todos WHERE id = ?`, id).Scan(&t.Title, &t.Done)
  	return t, err
  }
  ```

- **Request values**, such as the signed-in user, ride on the standard context: `OnRequest` calls `c.SetContext(context.WithValue(...))`, and the domain provides a typed getter.
- **Arguments** are fine when only some callers pass a different value. A function that needs a clock, a limit or a client can take it as a parameter.
- **Tests** replace the variable (`myapp.DB = openTestDB(t)`) or pass a fake as an argument. Declare an interface in the package that uses it, and only once there really are two implementations, such as the real one and a fake. Route code is tested with a Ctx from `tifatest.NewCtx`, see [Testing](app.md#testing).
- Do not set things up in `init()`, and do not read a resource while package-level variables are initialized. Resources exist only after `OnStart` has run.
