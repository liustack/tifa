[English](README.md) · 简体中文

# 山海工作台：tifa 示例应用

一个小店主的订单工作台：两家店铺、订单列表、订单详情和发货。页面在服务器上渲染成完整 HTML，浏览器接管后在页面之间跳转，发货后列表和统计跟着更新。数据放在进程内存里，重启恢复初始值。

它用来演示一个 tifa 应用长什么样：怎么按业务分目录，Go 怎么给页面准备数据，React 组件怎么读这些数据。

## 运行

在 `examples/shop` 目录执行，需要 Go 1.27 和 pnpm（Node 只用来装依赖和类型检查，构建不需要它）：

```sh
pnpm install --frozen-lockfile
go tool tifa build
./bin/shop
```

打开 <http://127.0.0.1:9527>，会跳到第一家店铺的订单列表。`./bin/shop --server-addr 127.0.0.1:9528` 换端口。`bin/shop` 是单个可执行文件，前端产物已经嵌在里面，拷到别的机器上直接运行。

类型检查与测试：

```sh
pnpm typecheck
go vet ./...
go test ./...
```

## 目录

```text
hooks.go                       拦截跨站的写请求，记录意外错误
app/
  layout.go  layout.tsx        根 Layout：侧栏、顶栏、文档外壳和全站 404
  icons.tsx  styles.css        跨页面共用的图标和样式
  route.go                     访问 / 时跳到第一家店铺
  projects/
    project.go                 店铺领域：类型、待发货统计、存储
    project+/                  /projects/{project}
      layout.go  layout.tsx    店铺 Layout：店名、待发货数、店铺备忘
      route.go  route.tsx      订单列表
      orders/
        order.go               订单领域：列表、详情、发货规则、存储
        order+/                /projects/{project}/orders/{order}：订单详情，POST 即发货
```

每个领域放在 `app/` 下自己的目录里，和展示它的页面、它的测试在一起：`app/projects` 是店铺领域，`app/projects/project+/orders` 是订单领域，放在单个订单目录的上一层，而不是放进单个订单的目录里。根包只放钩子。存储放在内存里，自带演示数据，代替数据库表，重启后恢复初始值。`app/` 下每层目录是 URL 的一段，`route.go` 写调用领域包的 `Load`，订单页的 `route.go` 还有发货用的 `POST`，`route.tsx` 写界面，用 `./+route` 读 `Load` 的结果。订单页通过店铺 Layout 读店铺：`tifa.Use(c, project.Layout)`。`app/route.go` 是 API 路由，它的 `GET` 把 `/` 重定向到店铺 `studio`。店铺领域要统计待发货的订单，所以 `projects` import `orders`，而 `orders` 既不 import `projects`，也不 import 页面。为什么这样组织、包之间按什么方向 import，见[应用的代码结构](../../docs/structure.zh-CN.md)。

## 可以试试

- 查看页面源代码，订单和客户信息已经在 HTML 里。
- 打开「店铺备忘」，再进入订单详情，备忘保持打开。收起侧栏也一样。切换到另一家店铺，店铺备忘收起，侧栏状态保留。
- 在开发者工具的 Network 里看：首屏接管没有额外的数据请求，每次跳转只有一个数据请求。
- 点「标记为已发货」，按钮、店铺的待发货数和返回后的列表统计一起更新。
- 打开 `/projects/studio/orders/SH-0000`、`/projects/nope`、`/nowhere`，三种 404 分别显示在订单、根 Layout 和整页的位置。

更多约定见 [构建与生成代码](../../docs/build.zh-CN.md) 和 [应用怎么写](../../docs/app.zh-CN.md)。
