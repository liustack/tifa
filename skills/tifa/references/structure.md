# Structuring an app

Contents: where code goes, which way packages may import, shared resources.

Full docs: [structure.md](https://github.com/liustack/tifa/blob/main/docs/structure.md) and [philosophy.md](https://github.com/liustack/tifa/blob/main/docs/philosophy.md).

## A domain is a folder under app/

Group code by business area, not by kind. A domain's types, queries and rules sit next to its pages, with their tests. There are no shared `models/`, `services/` or `handlers/` packages.

```text
myapp/
  go.mod
  hooks.go                  OnStart, OnShutdown, OnRequest, OnError
  db.go                     var DB, opened in OnStart and closed in OnShutdown
  schema.sql                the tables of every domain
  app/
    route.go  layout.tsx
    projects/
      project.go            Project, GetProject, ListProjects
      project_test.go
      project+/
        route.go  layout.tsx  page.tsx
        orders/
          order.go          Order, ListOrders, GetOrder, ShipOrder and their queries
          order_test.go
          order+/
            route.go  page.tsx
```

- A domain gets a file named after it in its folder: `app/projects/project.go` holds `package projects`.
- When a domain has a list page and a page per item, its code goes in the list folder (`orders/`), not in the folder of one item (`order+/`). The list folder does not need a page.
- A literal folder takes its own name as package name (`projects`). A parameter folder takes the parameter name (`project+` holds `package project`).
- Pages in the same folder call the domain directly. Pages below import it. The `Load` of the order page calls `orders.GetOrder`.
- Tests sit next to the code.
- The root package holds only the hooks and resources the whole app shares (`db.go`, `mail.go`). The database schema stays at the root too. Each domain keeps its own queries.
- Code that belongs to no domain and several use, such as formatting money, goes in its own package named after what it does (`money/`).
- Components and styles shared across pages go in the `app/` root, or in `components/` (shadcn/ui puts them there).

When a domain grows, split its code into more files in the same folder (`order.go`, `shipping.go`, `refund.go`). A part with pages of its own becomes a folder below.

## Which way imports go

Go refuses import cycles. In a tifa app:

- Pages import domains, and may import the package of a Layout above them for `tifa.Use(c, project.Layout)`.
- A domain may import another domain that does not depend on it. If `projects` imports `orders`, then `orders` cannot import `projects`.
- A domain cannot import a page that imports it.
- Tests in `package orders` count as `orders`. Use `package orders_test` to import something that imports `orders`.
- By convention, the root package (and `hooks.go`) imports nothing under `app/`. Domains import the root package for `DB`, so the reverse import would close a cycle sooner or later.
- A shared package such as `money/` imports no domain or page. Then every domain can use it.

When two domains need each other, move what one needs into the other, or into a new package that both import and that imports neither.

## Shared resources

A database connection, logger or mail client lives in a root file and is a package-level variable. Set it up in `OnStart`, release it in `OnShutdown`, use it anywhere.

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
func OnStart(ctx context.Context) error    { return openDB(ctx) }
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

- Request values (the signed-in user) ride on the standard context: `OnRequest` passes `c.WithContext(context.WithValue(...))` to `next`, and the domain provides a typed getter.
- Pass arguments when only some callers use another value (a clock, a limit, a client).
- Tests replace the variable or pass a fake. See [testing.md](testing.md).
- No `init()` for setup, and no reading a resource while package-level variables are initialized. Resources exist only after `OnStart`.
- The SQLite driver `github.com/liustack/sqlite` is pure Go, so `CGO_ENABLED=0` stays. Pick pure Go drivers for any dependency, because the build is `CGO_ENABLED=0`.
