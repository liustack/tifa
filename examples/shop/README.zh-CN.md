[English](README.md) · 简体中文

# 山海工作台：tifa 示例应用

一个小店主的订单工作台：两家店铺，可以排序、筛选和搜索的订单列表，订单详情，发货表单，修改收货信息的弹窗，以及带图表的销售统计。页面在服务器上渲染成完整 HTML，浏览器接管后在页面之间跳转，发货后列表和统计跟着更新。数据放在进程内存里，重启恢复初始值。

界面用 Tailwind CSS 和 shadcn/ui，列表用 TanStack Table，表单用 react-hook-form 和 zod，图表用 recharts，数字补间用 react-spring。

它用来演示一个 tifa 应用长什么样：怎么按业务分目录，Go 怎么给页面准备数据，React 组件怎么读这些数据。

## 运行

在 `examples/shop` 目录执行，需要 Go 1.27、Node 20.19 或 22.12 及以上，以及 pnpm。构建时用 Vite 在 Node 上打包前端，构建出的 `bin/shop` 运行时不需要 Node：

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
components/ui/  lib/  hooks/   shadcn/ui 组件和它们的辅助代码，用 @/components/ui/... 引用
app/
  layout.go  layout.tsx        根 Layout：侧栏、顶栏、文档外壳和全站 404
  styles.css                   Tailwind CSS 和店铺的主题
  format.ts                    中文的金额和时间，按店铺时区
  animated-number.tsx          数据变化时补间的数字
  route.go                     访问 / 时跳到第一家店铺
  projects/
    project.go                 店铺领域：类型、待发货统计、存储
    project+/                  /projects/{project}
      layout.go  layout.tsx    店铺 Layout：店名、待发货数、店铺备忘、订单和统计的入口
      route.go  route.tsx      订单列表
      columns.tsx              订单列表的列
      orders/
        order.go  shipping.go  订单领域：列表、详情、发货和收货信息的规则、存储
        stats.go               近 30 天的销售
        order+/                /projects/{project}/orders/{order}：订单详情，POST 发货，PATCH 改收货信息
          ship-form.tsx  recipient-dialog.tsx  items.tsx
      stats/                   /projects/{project}/stats：销售统计
```

每个领域放在 `app/` 下自己的目录里，和展示它的页面、它的测试在一起：`app/projects` 是店铺领域，`app/projects/project+/orders` 是订单领域，放在单个订单目录的上一层，而不是放进单个订单的目录里。根包只放钩子。存储放在内存里，自带演示数据，代替数据库表，重启后恢复初始值。`app/` 下每层目录是 URL 的一段，`route.go` 写调用领域包的 `Load`，订单页的 `route.go` 还有 `POST` 和 `PATCH`，它们的入参用 `Validate` 自己检查。`route.tsx` 写界面，用 `./+route` 读 `Load` 的结果。订单页和统计页通过店铺 Layout 读店铺：`tifa.Use(c, project.Layout)`。`app/route.go` 是 API 路由，它的 `GET` 把 `/` 重定向到店铺 `studio`。店铺领域要统计待发货的订单，所以 `projects` import `orders`，而 `orders` 既不 import `projects`，也不 import 页面。为什么这样组织见[哲学](../../docs/philosophy.zh-CN.md)，包之间按什么方向 import 见[应用的代码结构](../../docs/structure.zh-CN.md)。

Go 把时间作为时刻发给页面，页面用 `Intl` 按店铺时区格式化，时区由店铺 Layout 一起给出。浏览器在别的时区，看到的时间也和服务端渲染的一样。

## 可以试试

- 查看页面源代码，前十个订单、合计和客户信息已经在 HTML 里。
- 给订单列表排序、筛选、搜索，都在浏览器里完成，不发请求。
- 打开「店铺备忘」，再进入订单详情，备忘保持打开。切换到另一家店铺，店铺备忘收起。
- 收起侧栏后刷新，服务端渲染出的就是收起的，因为状态存在 cookie 里。
- 在开发者工具的 Network 里看：首屏接管没有额外的数据请求，每次跳转最多一个数据请求。
- 不填表单，或者填一个太短的运单号，点「确认发货」，浏览器马上提示错误。Go 会按同样的规则再查一遍，请求绕过浏览器时按字段返回错误。
- 发一单货，订单状态、店铺的待发货数和返回后的列表合计一起更新。
- 用「修改收货信息」改一个待发货订单的收货人。订单发出后按钮变灰，Go 也拒绝修改。
- 打开统计页，在近 7 天和近 30 天之间切换。关键数字已经在 HTML 里，图表只在浏览器里画。
- 打开 `/projects/studio/orders/SH-0000`、`/projects/nope`、`/nowhere`，三种 404 分别显示在订单、根 Layout 和整页的位置。

更多约定见 [构建与生成代码](../../docs/build.zh-CN.md) 和 [应用怎么写](../../docs/app.zh-CN.md)。
