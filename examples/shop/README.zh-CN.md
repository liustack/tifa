[English](README.md) · 简体中文

# 山海工作台：tifa 示例应用

一家小店的两面，放在同一个程序里。

- **工作台**，给店主用：两家店铺，每页 25 行、带商品图的订单列表，可以排序、搜索，按状态、日期、快递和金额筛选，也可以勾选订单批量导出。订单详情有操作记录、内部备注、付款与发票、库存检查、拣货单、物流轨迹和各家快递的表现，还有发货表单和修改收货信息的弹窗。销售统计有图表、带走势线的商品销售明细、下单时段热力图、快递表现、发货时效和常客。发货后列表和统计跟着更新。
- **店面**，给顾客用，有中文和英文：每页都有分类菜单和购物车抽屉，首页有轮播和按分类切换的热销，商品列表有搜索、带数量的筛选、常用筛选、已选条件、网格和列表两种视图和每件商品的快速查看，商品详情有图库、完整规格、和同类商品的对比、分页的评价（带图，店家会回复）、问答和搭配购买，购物车存在程序内存里、cookie 只放编号，周围有优惠券和推荐，结算页表单很长，还有用 Markdown 写、代码带高亮的手记文章。价格按语言用人民币或美元，日期和数字也按语言格式化。

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

打开 <http://127.0.0.1:9527>，会先到登录页，登录后跳到第一家店铺的订单列表。店面在 <http://127.0.0.1:9527/store/zh-CN> 和 <http://127.0.0.1:9527/store/en-US>。`./bin/shop --server-addr 127.0.0.1:9528` 换端口。`bin/shop` 是单个可执行文件，前端产物已经嵌在里面，拷到别的机器上直接运行。

检查与测试，一条命令：

```sh
go tool tifa check
```

## 登录

工作台是给店主用的，`/projects` 下的每个页面都要登录，店面不用。演示账号：

| 邮箱 | 密码 |
| --- | --- |
| `owner@shanhai.example` | `shanhai-demo` |

没登录的访客会被 `hooks.go` 里的 `OnRequest` 带到 `/login?next=...`，登录后回到原来要看的页面。顶栏和侧栏显示名字（Layout 的数据对所有人一样，名字来自浏览器里的会话），顶栏有退出按钮或登录链接，都用 `tifa/client` 的 `Session.In`、`Session.Out` 和 `useSession`。应用没有调用 `tifa.SetSessionStore`，会话放在进程内存里，重启后所有人要重新登录。密码和 `session.go` 里的 bcrypt 哈希比对，真实的店铺会从数据库里读它。

想不登录就看工作台，用 `SHOP_OPEN=1` 启动程序。和 Next.js、TanStack Start 的对比以及页面压测不能登录，用的就是它。它放行整个工作台，包括发货这类写操作，只给对比和压测用，不要在服务器上设置。

## 目录

```text
hooks.go                       OnRequest 把没登录的访客挡在工作台外，OnError 记录意外错误
session.go                     会话类型、演示账号和密码校验
components/                    shadcn/ui 组件在 ui/，它们用到的 hook 在 hooks/，用 @/components/... 引用
app/
  route.go  layout.tsx         工作台的根 Layout：侧栏、顶栏、文档外壳和全站 404，还有把 / 跳到第一家店铺的 GET
  account.tsx                  顶栏里显示谁登录了，以及退出按钮
  login/                       /login：不带工作台的 layout_.tsx、登录表单，和负责登录的 POST
  logout/                      /logout：负责退出的 POST
  styles.css                   Tailwind CSS 和店铺的主题
  format.ts                    中文的金额和时间，按店铺时区
  animated-number.tsx          数据变化时补间的数字
  projects/
    project.go                 店铺领域：类型、待发货统计、存储
    project+/                  /projects/{project}
      route.go  layout.tsx  page.tsx   店铺 Layout（店名、待发货数、店铺备忘、订单和统计的入口）和订单列表
      columns.tsx              订单列表的列
      orders/
        order.go  shipping.go  订单领域：列表、详情、发货和收货信息的规则、存储
        stats.go               近 30 天的销售
        order+/                /projects/{project}/orders/{order}：订单详情，POST 发货，PATCH 改收货信息，PUT 加备注
          ship-form.tsx  recipient-dialog.tsx  items.tsx  notes.tsx  activity.tsx  side-cards.tsx
        detail.go              订单页除订单本身外要显示的东西：付款、操作记录、备注、拣货单、快递统计
      stats/                   /projects/{project}/stats：销售统计
        revenue-chart.client.tsx  products-chart.client.tsx    两张图表，只在浏览器里加载
        panels.tsx             商品明细表、下单热力图、快递、常客和最近订单
  store/
    store.go                   店面共用的部分：两种语言、各自的币种和时区、价格
    route.go                   /store：GET 跳到浏览器偏好的语言
    layout_.tsx                店面外面不套工作台，并用两种语言显示店面没有的语言的 404
    i18n.ts  locales/          i18next 和两种语言的文案，金额和日期按语言格式化
    product-card.tsx           各个列表共用的商品卡片、商品图和价格
    images.ts  images/         商品、首页和手记的配图，AVIF 格式
    lang+/                     /store/{lang}
      route.go  layout.tsx  page.tsx   店面 Layout（分类菜单、搜索、语言切换、购物车抽屉、页脚）和首页，缓存 10 分钟
      mine/                    /store/{lang}/mine：GET 给出访客的购物车和最近看过的商品，POST 记下一个商品
      products/
        product.go  catalogue.go  review.go    商品领域：商品、搜索、筛选和各项计数、评价、规格、最近看过
        route.go  page.tsx      商品列表，筛选条件放在查询字符串里，缓存 5 分钟
        filter-schema.ts       筛选表单的 zod schema
        product+/              /store/{lang}/products/{product}：商品详情，缓存 10 分钟，POST 加入购物车
      cart/
        cart.go  checkout.go   购物车领域：商品行、优惠码、配送、订单的检查和下单
        recent.go              放在内存里的购物车和已下订单，有数量上限，过期就丢掉
        route.go  page.tsx      /store/{lang}/cart：PATCH 改数量，POST 用优惠码
      checkout/                /store/{lang}/checkout：订单表单，POST 下单
        checkout-schema.ts     订单的 zod schema，规则和 Order.Validate 一样
      journal/
        post.go  posts/        手记领域：Markdown 文章，每种语言一个文件
        markdown.tsx           带 GitHub 表格和代码高亮的 Markdown
        post+/                 /store/{lang}/journal/{post}，缓存 1 小时
```

每个领域放在 `app/` 下自己的目录里，和展示它的页面、它的测试在一起：`app/projects` 是店铺领域，`app/projects/project+/orders` 是订单领域，放在单个订单目录的上一层，而不是放进单个订单的目录里。根包只放钩子和会话。存储放在内存里，自带演示数据，代替数据库表，重启后恢复初始值。`app/` 下每层目录是 URL 的一段，`route.go` 放这个目录所有的 Go 名字：调用领域包的 `Load`，店铺和店面目录的 `Layout` 函数，订单页的 `POST` 和 `PATCH`，它们的入参用 `Validate` 自己检查。`page.tsx` 写界面，用 `./route.gen` 读 `Load` 的结果。订单页和统计页通过店铺 Layout 读店铺：`tifa.Use(c, project.Layout)`。`app/route.go` 放根 Layout，它的 `GET` 把 `/` 重定向到店铺 `studio`，所以 `/` 是 API 路由。店铺领域要统计待发货的订单，所以 `projects` import `orders`，而 `orders` 既不 import `projects`，也不 import 页面。为什么这样组织见[哲学](../../docs/philosophy.zh-CN.md)，包之间按什么方向 import 见[应用的代码结构](../../docs/structure.zh-CN.md)。

Go 把时间作为时刻发给页面，页面用 `Intl` 按店铺时区格式化，时区由店铺 Layout 一起给出。浏览器在别的时区，看到的时间也和服务端渲染的一样。

店面从 URL 的第一段取语言。Go 按这种语言准备文案，按它的币种给出价格，店面 Layout 把币种和时区交给 i18next，由它用 `Intl` 格式化金额、日期和复数。表单的检查按同样的规则写两份：浏览器里是 zod schema，Go 里是 `Validate`。两边给出同样的消息键，由页面翻译。商品领域是 `products`，`cart` 要给商品行定价，所以 import 它。`lang+` 下的页面用 `store.FindLocale` 找到自己的语言。店面 Layout 的 `head` 用 `htmlAttributes` 把这种语言设到 `<html>` 上，盖过根 Layout 的 `zh-CN`，切到另一种语言时浏览器跟着改。它还给每一页链接两种语言的同一页，并把中文那页作为 `x-default`。店面没有的路径在店面 Layout 里显示 404，用的是 URL 里的语言。`app/store` 放了 `layout_.tsx`，所以店面外面不套工作台，根 Layout 的 `Layout` 也不为它执行：工作台的数据不会进店面的页面。这个 `layout_.tsx` 只渲染 `<Outlet />`，它的 `Error` 显示店面没有的语言（比如 `/store/xx`）的 404，用中英两种语言写，并分别链接到两种语言的店面，因为这时不知道访客要哪种语言。`/store` 跳到浏览器的 `Accept-Language` 要的语言，用 `golang.org/x/text/language` 匹配，两种都不要时跳到中文。

## 店面的整页缓存

店面的页面对每个访客都一样，所以它们的 `route.go` 里都写了 `const Cache`（见[整页缓存](../../docs/app.zh-CN.md#整页缓存)）：首页、手记列表和商品页缓存 10 分钟，商品列表缓存 5 分钟，手记文章缓存 1 小时。这段时间内程序直接从内存回答，不跑 `Load`，也不渲染，前面的 CDN 也可以把页面存同样久。页面语言来自 URL（`lang+`），所以每种语言各有一条缓存。`/store` 按 `Accept-Language` 选语言，它是 GET 处理函数，没有 `Cache`。

缓存的页面看不到 cookie，所以属于某个访客的东西不在页面里。店面 Layout 只有语言、菜单和包邮门槛。顶栏的购物车和「最近看过」是浏览器在页面加载后向 `/store/{lang}/mine` 要的，每次改动后再要一次。商品页用一次 POST 告诉这个路由现在打开的是哪个商品。

想看它怎么起作用，把一个页面的响应头看两次：

```sh
curl -si http://127.0.0.1:9527/store/zh-CN/journal | grep -i '^cache-status\|^age\|^x-tifa-render'
```

第一次是 `Cache-Status: tifa; fwd=miss` 和 `X-Tifa-Render: vm`，第二次是 `Cache-Status: tifa; hit`，带 `Age` 和 `X-Tifa-Render: cache`。请求里带 cookie 不改变结果，带 `utm_source` 这样的跟踪参数也一样。工作台的页面没有 `Cache`，响应头是 `Cache-Control: private, no-cache`。

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
- 打开一个商品页，再打开商品列表。「最近看过」里有刚才那个商品，它是页面加载后浏览器从 `/store/zh-CN/mine` 取来的，而页面本身来自缓存。
- 打开一篇手记，代码块的高亮已经在服务器发来的 HTML 里。

更多约定见 [构建与生成代码](../../docs/build.zh-CN.md) 和 [应用怎么写](../../docs/app.zh-CN.md)。
