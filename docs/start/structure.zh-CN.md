---
summary: 'Tifa 应用的文件放在哪里：每个领域一个文件夹，放在 app/ 下紧挨着它的页面，根目录只放 hooks 和共享资源，包之间谁可以导入谁，共享资源怎样设置和使用'
audience: app
order: 4
translatedFrom: 2bbb8b5a977b
read_when:
  - '新建 Tifa 应用'
  - '决定一个类型、一条查询或一个共享资源放在哪里'
  - '一个领域变大了，或者两个领域需要互相调用'
  - 'go 命令报告应用的包之间有导入循环'
---

# 组织一个应用 {#structuring-an-app}

这一页说明应用里每个文件该放在哪里。为什么这样组织，见 [理念](philosophy.zh-CN.md)。Tifa 自己内部是怎么工作的，见 [架构](../internals/architecture.md)。

## 1. 域是 app/ 下的一个文件夹 {#1-a-domain-is-a-folder-under-app}

一个域住在 `app/` 下它自己的文件夹里，和显示它的页面放在一起。根目录只放 hooks 和整个应用共享的东西：

```text
myapp/
  go.mod
  hooks.go                  OnStart, OnShutdown, OnRequest, OnError
  setup.ts                  top-level code of the JavaScript side: i18next formatters and other setup
  options.go                the options of the app: var DBPath = tifa.String(...)
  db.go                     var DB, opened in OnStart and closed in OnShutdown
  schema.sql                the tables of every domain
  content.go                the content collections: var Blog = content.Define[Post](...)
  content/                  the Markdown files of the collections
  app/
    route.go  layout.tsx
    projects/
      project.go            Project, GetProject, ListProjects
      project_test.go
      route.go  page.tsx    the project list, if there is one
      project+/
        route.go  layout.tsx  page.tsx
        orders/
          order.go          Order, ListOrders, GetOrder, ShipOrder and their queries
          order_test.go
          route.go  page.tsx
          order+/
            route.go  page.tsx
```

- **代码放哪里。** 一个域在它的文件夹里有一个以它命名的文件：`app/projects/project.go` 里是 `package projects`。当一个域有列表页和每项一个页面时，它的代码放在列表文件夹（`orders/`）里，不放在单个条目的文件夹（`order+/`）里。列表文件夹不需要有页面：在 [examples/shop](../../examples/shop) 里，订单列在项目页面上，`orders/` 里有这个域，以及创建订单的 `POST`。
- **包名。** 字面的文件夹用它自己的名字（`projects`、`orders`）。参数文件夹用参数名（`project+` 里是 `package project`），和 [app.md](app.zh-CN.md#urls-come-from-folders) 里一样。
- **页面使用域。** 同一个文件夹里的页面是同一个包，直接调用域。下面的页面则导入它：

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

- **测试和它们测的代码放在一起：** `order_test.go` 在 `order.go` 旁边。
- **组件和其他前端文件** 也可以放在 `app/` 下。对 Tifa 有意义的只有 `page.tsx`、`layout.tsx` 和 `layout_.tsx`，所以其他任何 `.tsx` 或 `.ts` 文件，比如共享组件，都是普通模块。把它放在用它的页面旁边，用相对路径导入：examples/shop 把 `product-card.tsx` 放在 `app/store/` 里。几个区域共用的组件可以放在 `app/` 旁边的文件夹里，比如 examples/shop 里的 `components/`。
- **根包** 只放 hooks 和整个应用共享的资源，比如 `db.go`、`mail.go` 和 `content.go`（内容集合，见 [内容](../pages-and-data/content.zh-CN.md)）。不放域的代码。数据库 schema 也留在这里：所有域的表构成一个数据库，`OnStart` 会在应用开始监听之前创建它们。每个域保留自己的查询。
- 不属于任何域、又被几个域使用的代码，比如格式化金额，放进以它做的事命名的单独的包里，比如模块根目录的 `money/`。它能导入什么，见下文。

### 导入的方向 {#which-way-imports-go}

Go 的规则：包不能依赖它自己。如果包 A 直接或通过别的包导入了 B，那么 B 就不能直接或通过别的包导入 A。go 命令会以 `import cycle not allowed` 停下。包放在哪里无所谓，要看的只是某个导入是否会绕回来。在 Tifa 应用里，这意味着：

- **页面导入域**：`order+/` 里的订单页面导入上一级文件夹的 `orders`，项目布局导入 `projects`。页面也可以导入上层布局所在的包，用于 `tifa.Use(c, project.Layout)`。
- **一个域可以导入另一个不依赖它的域。** 在 examples/shop 里，`projects` 用 `orders.CountPending` 统计等待发货的订单，所以 `projects` 导入 `orders`，而 `orders` 不能导入 `projects`。文件夹可以导入它下面的、上面的或旁边的，只要没有东西绕回来。
- **域不能导入一个导入了它的页面。** `order+/` 导入 `orders`，所以 `orders` 不能导入 `order+/`。把两边都需要的东西留在域里。
- **包里的测试算作这个包。** `package orders` 里的测试不能导入 `projects`，因为 `projects` 导入了 `orders`（`import cycle not allowed in test`）。`package orders_test` 里的测试是一个独立的包，可以导入 `projects`。

- **根包只有在域不使用它的时候才能导入这个域。** 域为了 `DB` 和其他共享资源会导入根包（`import "example.com/myapp"`，然后用 `myapp.DB`），一旦某个域这样做了，根包就不能导入那个域，也不能导入任何导入了它的页面，`hooks.go` 和 `package myapp` 里的测试都包括在内。`package myapp_test` 里的测试是一个独立的包，可以导入域。当 `hooks.go` 需要这种域的某个函数时，把函数需要的东西作为参数传给它，比如 `*sql.DB`，函数放在一个不导入根包的包里。
- **像 `money/` 这样不导入任何域或页面的共享包，每个域都可以导入。** 完全不导入应用里任何包的共享包，应用里的每个包都可以导入，根包也可以。它每多一个导入，就关上一扇门：一旦它导入了某个域，那个域和这个域依赖的每个包，比如根包，就不能导入它了。一旦它导入了根包，根包就不能导入它。一旦它导入了另一个共享包，那个包就不能反过来导入它。

当两个域互相需要时，把一个需要的东西挪进另一个，或者挪进一个新包，这个新包被两者导入、自己却不导入任何一个，这样导入就只有一个方向。

### 域变大的时候 {#when-a-domain-grows}

- 把它的代码拆成同一文件夹里的更多文件，每个主题一个：`order.go`、`shipping.go`、`refund.go`。它仍然是一个包。
- 有自己页面的部分变成下面的一个文件夹，里面放它的代码和页面，比如 `order+/refunds/`。一旦它导入了上面的域，上面的域就不能反过来导入它。

## 2. 共享资源和其他依赖 {#2-shared-resources-and-other-dependencies}

- **共享资源**，比如数据库连接、日志器或邮件客户端，放在根目录里 `hooks.go` 旁边的文件里：`db.go`、`mail.go`、`middleware.go`。每个文件构建一个包级变量或函数。在 `OnStart` 里设置，在 `OnShutdown` 里释放，在任何地方直接使用：

  ```go
  // db.go
  package myapp

  var DB *sql.DB

  func openDB() (err error) {
  	DB, err = sql.Open("sqlite", "file:app.db?_txlock=immediate") // see the guide on SQLite
  	return err
  }
  ```

  ```go
  // hooks.go
  func OnStart(ctx context.Context) error    { return openDB() }
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

- **请求里的值**，比如已登录的用户，搭在标准 context 上：`OnRequest` 把 `c.WithContext(context.WithValue(...))` 传给 `next`，域提供一个带类型的 getter。
- **参数** 在只有部分调用者要传不同的值时就可以用。需要时钟、限制或客户端的函数，可以把它作为参数接收。
- **测试** 替换变量（`myapp.DB = openTestDB(t)`），或者把假的实现作为参数传入。接口要声明在使用它的包里，而且只有真的有两个实现时才声明，比如真实的和假的。路由代码用 `tifatest.NewCtx` 造的 Ctx 来测试，见 [测试](../pages-and-data/testing-apps.zh-CN.md#testing)。
- 不要在 `init()` 里做设置，也不要在包级变量初始化时读取资源。资源只有在 `OnStart` 运行之后才存在。把函数放进注册表，比如 `jobs.Handle`，不读取任何东西，可以在 `init` 里运行（见 [需要某个域的代码的任务](../guides/jobs.zh-CN.md#a-job-that-needs-code-of-a-domain)）。
