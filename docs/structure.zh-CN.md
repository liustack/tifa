---
summary: 'tifa 应用的文件放在哪：每个领域放在 app/ 下自己的目录里、和页面在一起，根目录只放钩子和共享资源，包之间按什么方向 import，共享资源怎么初始化和使用'
read_when:
  - 新建一个 tifa 应用
  - 不确定一个类型、一条查询或一个共享资源该放在哪
  - 一个领域做大了，或者两个领域要互相调用
  - go 命令报应用里的包之间有循环引用
---

[English](structure.md) · 简体中文

# 应用的代码结构

这篇讲应用里每个文件放在哪。为什么这样组织，见[哲学](philosophy.zh-CN.md)。

## 1. 领域就是 app/ 下的目录

领域放在 `app/` 下自己的目录里，和展示它的页面在一起。根目录只放钩子和整个应用共享的东西：

```text
myapp/
  go.mod
  hooks.go                  OnStart、OnShutdown、OnRequest、OnError
  db.go                     var DB，在 OnStart 里打开，在 OnShutdown 里关闭
  schema.sql                所有领域的表
  app/
    layout.go  layout.tsx  route.go
    projects/
      project.go            Project、GetProject、ListProjects
      project_test.go
      route.go  route.tsx   店铺列表（如果有）
      project+/
        layout.go  layout.tsx  route.go  route.tsx
        orders/
          order.go          Order、ListOrders、GetOrder、ShipOrder 和它们的查询
          order_test.go
          route.go  route.tsx
          order+/
            route.go  route.tsx
```

- **代码放哪。** 领域在自己的目录里有一个以它命名的文件：`app/projects/project.go` 写作 `package projects`。一个领域既有列表页又有单条详情页时，代码放在列表那一层（`orders/`），不放进单条的目录（`order+/`）。列表那层不一定要有页面：[examples/shop](../examples/shop/README.zh-CN.md) 的订单列表显示在店铺页上，`orders/` 里只有领域代码。
- **包名。** 普通目录用目录名（`projects`、`orders`）。参数目录用参数名（`project+` 里写 `package project`），见 [app.zh-CN.md](app.zh-CN.md#目录就是-url)。
- **页面调用领域。** 同一目录下的页面和领域是同一个包，直接调用。下面的页面 import 它：

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

- **测试和被测代码放在一起**：`order_test.go` 挨着 `order.go`。
- **根包**只放钩子和整个应用共享的资源，比如 `db.go`、`mail.go`，不放领域代码。数据库的表结构也留在这里：所有领域的表组成同一个数据库，`OnStart` 在应用开始监听之前建好它们，而根包不能 import 领域（见下文）。查询留在各自的领域里。
- 不属于任何领域、又被好几个领域用到的代码，比如金额格式化，单独放一个按用途命名的包，比如模块根目录下的 `money/`。多少个领域 import 它都可以。它不能 import 一个 import 了它的领域。它可以 import 根包，但这样根包（包括 `hooks.go`）就不能 import 它。它既不 import 领域也不 import 根包时，应用里的任何包都能 import 它，根包也可以。

### import 的方向

Go 不允许循环引用：A 包直接或经过别的包 import 了 B，B 就不能再 import A，go 命令会报 `import cycle not allowed` 并停下。放到 tifa 应用里就是：

- **领域 import 根包**，比如用 `DB`，直接用它自己的包名：`import "example.com/myapp"`，然后写 `myapp.DB`。根包（包括 `hooks.go`）不 import `app/` 下的包：领域 import 了根包，所以根包再 import 这个领域，或者 import 了这个领域的页面，就成了循环。根包自己 `package myapp` 的测试也一样。写成 `package myapp_test` 的测试可以 import 它们。
- **页面 import 领域**：`order+/` 里的订单页 import 上一层的 `orders`，店铺的 Layout import `projects`。页面也可以 import 上层 Layout 的包，用来写 `tifa.Use(c, project.Layout)`。
- **领域之间可以 import，但只能单向。** 在 examples/shop 里，`projects` 用 `orders.CountPending` 统计待发货的订单，所以 `projects` import `orders`，`orders` 就不能 import `projects`。目录在哪一层不要紧：可以 import 下层、上层或旁边的目录，只要没有反向的 import。
- **领域不 import 自己的页面。** `order+/` import 了 `orders`，`orders` 就不能 import `order+/`。两边都要用的东西放进领域。
- **包内测试算作这个包。** `package orders` 里的测试不能 import `projects`，因为 `projects` import 了 `orders`（报 `import cycle not allowed in test`）。这样的测试写成 `package orders_test`。

两个领域需要互相调用时，把两边都要的那部分挪进其中一个，或者挪进一个两边都 import 的新领域。

### 领域做大了

- 在同一个目录里按主题拆成多个文件：`order.go`、`shipping.go`、`refund.go`。还是同一个包。
- 有自己页面的一块，变成下面的一个目录，里面放它的代码和页面，比如 `order+/refunds/`。它一旦 import 了上层的领域，上层的领域就不能反过来 import 它。

## 2. 共享资源和其他依赖

- **共享资源**，比如数据库连接、日志、邮件客户端，放在根目录、和 `hooks.go` 并列的文件里：`db.go`、`mail.go`、`middleware.go`。每个文件只构建一个包级变量或一个函数。在 `OnStart` 里初始化，在 `OnShutdown` 里释放，其他地方直接用：

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

- **请求里的值**，比如当前登录的用户，放在标准 context 上：`OnRequest` 调用 `c.SetContext(context.WithValue(...))`，领域提供一个带类型的读取函数。
- **参数**：只有部分调用方要传不同的值时，用参数传就好。需要时钟、上限或某个客户端的函数，可以把它们作为参数。
- **测试**替换变量（`myapp.DB = openTestDB(t)`），或者把假实现作为参数传进去。接口声明在用它的包里，而且只在真的有两个实现时才声明，比如真实的和假的。路由代码用 `tifatest.NewCtx` 造出的 Ctx 来测，见[测试](app.zh-CN.md#测试)。
- 不要在 `init()` 里初始化，也不要在包级变量初始化时读资源。资源在 `OnStart` 运行之后才存在。
