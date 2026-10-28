[English](README.md) · 简体中文

# 山海工作台：tifa 示例应用

一家小店的两面，放在同一个程序里。

- **工作台**，给店主用：两家店铺，每页 25 行、带商品图的订单列表，可以排序、搜索，按状态、日期、快递和金额筛选，也可以勾选订单批量导出，带物流轨迹的订单详情，发货表单，修改收货信息的弹窗，以及带图表和每日明细的销售统计。发货后列表和统计跟着更新。
- **店面**，给顾客用，有中文和英文：每页都有分类菜单和购物车抽屉，首页有轮播和按分类切换的热销，带搜索和筛选的商品列表，商品详情有图库、带图评价、问答和搭配购买，存在程序内存里、cookie 只放编号的购物车，表单很长的结算页，以及用 Markdown 写、代码带高亮的手记文章。价格按语言用人民币或美元，日期和数字也按语言格式化。

页面在服务器上渲染成完整 HTML，浏览器接管后在页面之间跳转。数据放在进程内存里，重启恢复初始值。

界面照着印刷的商品目录来做：细线，商品编号，规格摆在明处。每件商品有三张图：商品本身、细节和使用场景，存成 AVIF，各有 160、480 和 1200 像素宽三份。英文标题用 Newsreader，随应用打包，中文标题和正文用系统字体。界面用 Tailwind CSS 和 shadcn/ui，订单列表用 TanStack Table，表单用 react-hook-form 和 zod，图表用 recharts，数字补间用 react-spring。店面的翻译用 i18next，手记用 react-markdown 渲染，代码高亮用 highlight.js。

它用来演示一个 tifa 应用长什么样：怎么按业务分目录，Go 怎么给页面准备数据，React 组件怎么读这些数据。

## 运行

在 `examples/shop` 目录执行，需要 Go 1.27、Node 20.19 或 22.12 及以上，以及 pnpm。构建时用 Vite 在 Node 上打包前端，构建出的 `bin/shop` 运行时不需要 Node：

```sh
pnpm install --frozen-lockfile
go tool tifa build
./bin/shop
```

打开 <http://127.0.0.1:9527>，会跳到第一家店铺的订单列表。店面在 <http://127.0.0.1:9527/store/zh-CN> 和 <http://127.0.0.1:9527/store/en-US>。`./bin/shop --server-addr 127.0.0.1:9528` 换端口。`bin/shop` 是单个可执行文件，前端产物已经嵌在里面，拷到别的机器上直接运行。

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
        revenue-chart.client.tsx  products-chart.client.tsx    两张图表，只在浏览器里加载
  store/
    store.go                   店面共用的部分：两种语言、各自的币种和时区、价格
    i18n.ts  locales/          i18next 和两种语言的文案，金额和日期按语言格式化
    product-card.tsx           各个列表共用的商品卡片、商品图和价格
    images.ts  images/         商品、首页和手记的配图，AVIF 格式
    lang+/                     /store/{lang}
      layout.go  layout.tsx    店面 Layout：分类菜单、搜索、语言切换、购物车抽屉、页脚
      route.go  route.tsx      首页
      products/
        product.go  catalogue.go  review.go    商品领域：商品、搜索、筛选和各项计数、评价
        route.go  route.tsx    商品列表，筛选条件放在查询字符串里
        filter-schema.ts       筛选表单的 zod schema
        product+/              /store/{lang}/products/{product}：商品详情，POST 加入购物车
      cart/
        cart.go  checkout.go   购物车领域：商品行、优惠码、配送、订单的检查和下单
        recent.go              放在内存里的购物车和已下订单，有数量上限，过期就丢掉
        route.go  route.tsx    /store/{lang}/cart：PATCH 改数量，POST 用优惠码
      checkout/                /store/{lang}/checkout：订单表单，POST 下单
        checkout-schema.ts     订单的 zod schema，规则和 Order.Validate 一样
      journal/
        post.go  posts/        手记领域：Markdown 文章，每种语言一个文件
        markdown.tsx           带 GitHub 表格和代码高亮的 Markdown
        post+/                 /store/{lang}/journal/{post}
      rest++/                  店面其他所有路径：在店面 Layout 里显示 404
```

每个领域放在 `app/` 下自己的目录里，和展示它的页面、它的测试在一起：`app/projects` 是店铺领域，`app/projects/project+/orders` 是订单领域，放在单个订单目录的上一层，而不是放进单个订单的目录里。根包只放钩子。存储放在内存里，自带演示数据，代替数据库表，重启后恢复初始值。`app/` 下每层目录是 URL 的一段，`route.go` 写调用领域包的 `Load`，订单页的 `route.go` 还有 `POST` 和 `PATCH`，它们的入参用 `Validate` 自己检查。`route.tsx` 写界面，用 `./+route` 读 `Load` 的结果。订单页和统计页通过店铺 Layout 读店铺：`tifa.Use(c, project.Layout)`。`app/route.go` 是 API 路由，它的 `GET` 把 `/` 重定向到店铺 `studio`。店铺领域要统计待发货的订单，所以 `projects` import `orders`，而 `orders` 既不 import `projects`，也不 import 页面。为什么这样组织见[哲学](../../docs/philosophy.zh-CN.md)，包之间按什么方向 import 见[应用的代码结构](../../docs/structure.zh-CN.md)。

Go 把时间作为时刻发给页面，页面用 `Intl` 按店铺时区格式化，时区由店铺 Layout 一起给出。浏览器在别的时区，看到的时间也和服务端渲染的一样。

店面从 URL 的第一段取语言。Go 按这种语言准备文案，按它的币种给出价格，店面 Layout 把币种和时区交给 i18next，由它用 `Intl` 格式化金额、日期和复数。表单的检查按同样的规则写两份：浏览器里是 zod schema，Go 里是 `Validate`。两边给出同样的消息键，由页面翻译。商品领域是 `products`，`cart` 要给商品行定价，所以 import 它。`lang+` 下的页面用 `store.FindLocale` 找到自己的语言。店面 Layout 的 `head` 用 `htmlAttributes` 把这种语言设到 `<html>` 上，盖过根 Layout 的 `zh-CN`，切到另一种语言时浏览器跟着改。店面没有的路径经 `rest++` 回答 404，用的是 URL 里的语言。

## 可以试试

- 查看页面源代码，前 25 个订单、合计和客户信息已经在 HTML 里。
- 给订单列表排序、筛选、搜索，勾选几单导出，都在浏览器里完成，不发请求。
- 打开「店铺备忘」，再进入订单详情，备忘保持打开。切换到另一家店铺，店铺备忘收起。
- 收起侧栏后刷新，服务端渲染出的就是收起的，因为状态存在 cookie 里。
- 在开发者工具的 Network 里看：首屏接管没有额外的数据请求，每次跳转最多一个数据请求。
- 不填表单，或者填一个太短的运单号，点「确认发货」，浏览器马上提示错误。Go 会按同样的规则再查一遍，请求绕过浏览器时按字段返回错误。
- 发一单货，订单状态、店铺的待发货数和返回后的列表合计一起更新。
- 用「修改收货信息」改一个待发货订单的收货人。订单发出后按钮变灰，Go 也拒绝修改。
- 打开统计页，在近 7 天和近 30 天之间切换。关键数字已经在 HTML 里。图表只在浏览器里加载和绘制，在此之前 HTML 里是同样高度的占位。图表的代码只在打开统计页时才下载。
- 打开 `/projects/studio/orders/SH-0000`、`/projects/nope`、`/nowhere`，三种 404 分别显示在订单、根 Layout 和整页的位置。
- 把店面在中文和 English 之间切换。价格在人民币和美元之间换算，日期、评价数和配送日期跟着语言变。
- 在商品列表里搜索和筛选。筛选条件写进 URL，筛好的列表可以直接分享，服务端也按它渲染。把最低价填得比最高价高，表单在发请求之前就拒绝。
- 选一种颜色把商品加入购物车，在购物车里改数量，试试优惠码 `SHANHAI10` 和 `WELCOME30`。第一次改动时，你会拿到自己的购物车。它存在程序内存里，cookie 里只放它的编号。改动失败时，比如断网了，那一行下面会给出提示。
- 下一单，再在无痕窗口里打开它的确认页。会返回 404：确认页上有邮箱地址，所以只有下单的那个购物车能看到。
- 不填表单、填一个格式不对的邮编，或者填一个校验位不对的卡号，再去结算。错误按字段显示，请求绕过浏览器时 Go 也返回同样的错误。
- 打开一篇手记，代码块的高亮已经在服务器发来的 HTML 里。

更多约定见 [构建与生成代码](../../docs/build.zh-CN.md) 和 [应用怎么写](../../docs/app.zh-CN.md)。
