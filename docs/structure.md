---
summary: 'Where the files of a tifa app go: each domain in its folder under app/ next to its pages, only hooks and shared resources at the root, which way packages may import each other, how shared resources are set up and used'
read_when:
  - Starting a new tifa app
  - Deciding where a type, a query or a shared resource goes
  - A domain has grown, or two domains need to call each other
  - The go command reports an import cycle between packages of an app
---

English · [简体中文](structure.zh-CN.md)

# Structuring an app

This page says where each file of an app goes. Why it is organized this way is in [Philosophy](philosophy.md).

## 1. A domain is a folder under app/

A domain lives in its folder under `app/`, next to the pages that show it. The root keeps only the hooks and what the whole app shares:

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
- **The root package** holds only the hooks and the resources the whole app shares, such as `db.go` and `mail.go`. No domain code. The database schema stays here too: the tables of all domains make up one database, `OnStart` creates them before the app listens, and by the convention below the root package imports nothing under `app/`. Each domain keeps its own queries.
- Code that belongs to no domain and that several domains use, such as formatting money, goes in its own package named after what it does, such as `money/` at the module root. See below for what it may import.

### Which way imports go

Go's rule: a package cannot depend on itself. If package A imports B, directly or through other packages, then B cannot import A, directly or through other packages. The go command stops with `import cycle not allowed`. Where the packages sit does not matter, only whether an import leads back. In a tifa app this means:

- **Pages import domains**: the order page in `order+/` imports `orders` from the folder above, and the project Layout imports `projects`. A page may also import the package of a Layout above it, for `tifa.Use(c, project.Layout)`.
- **A domain may import another domain that does not depend on it.** In examples/shop, `projects` counts the orders waiting to ship with `orders.CountPending`, so `projects` imports `orders`, and `orders` cannot import `projects`. A folder may import one below it, above it or beside it, as long as nothing leads back.
- **A domain cannot import a page that imports it.** `order+/` imports `orders`, so `orders` cannot import `order+/`. Keep what both need in the domain.
- **Tests in a package count as the package.** A test in `package orders` cannot import `projects`, because `projects` imports `orders` (`import cycle not allowed in test`). A test in `package orders_test` is a package of its own and may import `projects`.

Two more rules are conventions of tifa apps, not limits of Go:

- **The root package, `hooks.go` and its tests in `package myapp` included, imports nothing under `app/`.** Go would let it import a package under `app/` that does not depend on it. But domains import the root package by its own name for `DB` and the other shared resources (`import "example.com/myapp"`, then `myapp.DB`), and once a domain does, the root package can no longer import that domain or any page that imports it. Keeping the root free of `app/` imports means any domain can start using a shared resource without breaking the build. A test in `package myapp_test` is a package of its own and may import domains.
- **A shared package such as `money/` imports no domain or page, directly or through other packages.** Then every domain can import it. One that imports no package of the app at all can be imported by every package of the app, the root package too. Each import it adds closes a door: once it imports a domain, that domain and every package the domain depends on, such as the root package, cannot import it. Once it imports the root package, the root package cannot import it. Once it imports another shared package, that one cannot import it back.

When two domains need each other, move what one needs into the other, or into a new package that both import and that imports neither, so the imports go one way.

### When a domain grows

- Split its code into more files in the same folder, one per topic: `order.go`, `shipping.go`, `refund.go`. It stays one package.
- A part with pages of its own becomes a folder below, holding its code and its pages, such as `order+/refunds/`. Once it imports the domain above it, the domain above cannot import it back.

## 2. Shared resources and other dependencies

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
