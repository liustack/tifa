---
summary: 'tifa 应用怎么组织代码：按领域分组，先平铺在根包里，领域做大了再拆包，依赖手动组装'
read_when:
  - 新建一个 tifa 应用
  - 不确定一个类型、一条查询或一个共享资源该放在哪
  - 应用做大了，想知道某个领域该不该拆成单独的包
---

[English](structure.md) · 简体中文

# 应用的代码结构

tifa 应用就是一个 Go 程序，沿用 Go 的习惯。三条规则：

1. 按领域分组，不按代码种类分组。
2. 先平铺在根包里。领域做大了，再拆成单独的包。
3. 依赖手动组装。

## 1. 按领域分组

代码按它讲的是什么来分（订单、店铺、客户），不按它是哪类代码来分。一个领域的类型、存储、查询和规则放在一起。不设 `models/`、`services/`、`repositories/`、`handlers/`、`utils/` 或 `db/` 这种从每个领域收集同一类代码的包。

检验标准：一个领域能整体拿走、评审或删除。

页面本来就是这样组织的。`app/` 下的目录按业务分，一个页面的 `route.go` 和 `route.tsx` 放在同一个目录。

## 2. 先平铺，做大了再拆

新应用把领域代码放在根包里，也就是模块根目录、和 `hooks.go` 并列的那个包，一个领域一个文件：

```text
myapp/
  go.mod
  hooks.go              OnStart、OnShutdown、OnRequest、OnError
  db.go                 var DB，在 OnStart 里打开，在 OnShutdown 里关闭
  project.go            Project、GetProject、ListProjects
  order.go              Order、ListOrders、GetOrder、ShipOrder 和它们的查询
  order_test.go
  app/
    layout.go  layout.tsx
    projects/
      layout.go  layout.tsx
      orders/
        list/    route.go  route.tsx
        detail/  route.go  route.tsx
```

`app/` 下的目录保持轻薄。它们只放页面，`route.go` 读请求，然后调用根包：

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

只有一个包，领域之间直接互相调用，不必为了让隔壁包用到而导出，也不会有要解开的循环引用。[examples/shop](../examples/shop/README.zh-CN.md) 就是这样写的。

出现下面这些情况时，把一个领域挪到单独的包里：

- 名字开始重复同一个前缀：`OrderList`、`OrderStatus`、`orderRecord`、`ordersMu`。拆成包以后读作 `orders.List`、`orders.Status`。
- 根包大到不好找东西，或者某个领域归一个人、一个团队负责。
- 想用一个小的公开接口把细节藏起来。

新包放在用它的页面旁边，比如 `app/projects/orders/order.go`，写作 `package orders`。好几处页面都用它时，放在模块根目录下，比如 `orders/`。只导出别的包会调用的东西。

注意 import 的方向。`app/` 下的包和拆出去的包可以 import 根包，比如用 `DB`。根包永远不反过来 import 它们，因为 Go 不允许循环引用。所以先拆调用方，再拆被调用的领域：如果 `project.go` 要统计一个店铺的订单，先把 projects 拆出去（或者和 orders 一起拆），在那之前 orders 留在根包里。

## 3. 依赖手动组装

不用依赖注入容器，不用框架，不靠反射或生成代码来组装。依赖就是普通的 Go 值。

- **共享资源**，比如数据库连接、日志、邮件客户端，放在根目录、和 `hooks.go` 并列的文件里：`db.go`、`mail.go`、`middleware.go`。每个文件只构建一个包级变量或一个函数。在 `OnStart` 里初始化，在 `OnShutdown` 里释放，其他地方直接用：

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

- **请求里的值**，比如当前登录的用户，放在标准 context 上：`OnRequest` 调用 `c.SetContext(context.WithValue(...))`，领域提供一个带类型的读取函数。
- **参数**：只有部分调用方要传不同的值时，用参数传就好。需要时钟、上限或某个客户端的函数，可以把它们作为参数。
- **测试**替换变量（`DB = openTestDB(t)`），或者把假实现作为参数传进去。接口声明在用它的包里，而且只在真的有两个实现时才声明，比如真实的和假的。
- 不要在 `init()` 里初始化，也不要在包级变量初始化时读资源。资源在 `OnStart` 运行之后才存在。
