---
summary: '怎么写一个 tifa 应用：目录结构、目录怎么变成 URL、route.go 与 route.tsx、Layout、hooks.go、404、SSR 与缓存开关、服务端渲染时有什么、哪些库在那里渲染得不一样、哪些组件该只在浏览器里加载、怎么用 Intl 格式化才能水合一致、用 Tailwind CSS 和 shadcn/ui 写样式，怎么测 Go 路由代码，Load 里 Ctx 的用法、带类型入参和结果的方法函数，以及数据、入参和结果的 JSON 规则'
read_when:
  - 新建一个 tifa 应用，或给应用加页面、Layout、API 路由
  - 构建报 Ctx 用法或 Load 返回类型的错误，想知道怎么改
  - 改 internal/analyze 的规则，需要知道对用户承诺了什么
  - 改 examples/shop 或 test/apps 里的应用
  - 给 Load、Layout 函数、方法函数或 OnRequest 写 Go 测试
  - 给应用加 CSS、Tailwind CSS 或 shadcn/ui
  - 在页面里格式化数字或日期，或者 tifa dev 对某个 Intl 调用报警
  - 页面在服务端渲染失败或水合不一致，想知道服务端有哪些浏览器 API 和库的能力
  - 某个组件要等浏览器画出来才有内容（比如图表、地图），想知道要不要在服务端渲染它
---

[English](app.md) · 简体中文

# 应用怎么写

完整的例子是 [examples/shop](../examples/shop)。构建、生成文件和 go.mod 的配法见 [build.zh-CN.md](build.zh-CN.md)。

## 目录

```text
myapp/
  go.mod
  hooks.go                          进程与请求钩子（可选）
  db.go                             整个应用共享的资源：包级变量和函数（可选）
  app/
    layout.go  layout.tsx           根 Layout，套到所有页面
    route.go                        /
    projects/
      project.go  project_test.go   店铺领域：Project、GetProject、ListProjects
      project+/
        layout.go  layout.tsx       /projects/{project} 及下面所有页面的 Layout
        route.go  route.tsx         页面 /projects/{project}
        orders/
          order.go  order_test.go   订单领域：Order、ListOrders、GetOrder、ShipOrder
          order+/
            route.go  route.tsx     页面 /projects/{project}/orders/{order}
            items.tsx  ship-form.tsx
        stats/
          route.go  route.tsx       页面 /projects/{project}/stats
```

- 代码按领域分组，一个领域就是 `app/` 下的一个目录：它的类型、存储和规则和页面放在一起，既有列表又有单条页面时放在列表那一层。页面 import 它要展示的领域包。根包只放钩子和整个应用共享的资源。不设一个装着所有表的 `models/` 或 `db/` 包。见 [structure.zh-CN.md](structure.zh-CN.md)。
- 跨页面共用的组件和样式放在 `app/` 根目录。
- 不写 main。`tifa build` 把 main 和路由注册代码生成在 `.tifa/`，`go vet ./...`、`go test ./...` 不会扫到它。

## 目录就是 URL

`app/` 下每一层目录是 URL 的一段，`app/` 本身是 `/`。

| 目录名 | URL 段 |
| --- | --- |
| `orders` | 字面量 `orders` |
| `order+` | 参数 `{order}`，用 `c.Param("order")` 读 |
| `path++` | 剩余路径 `{path...}`，只能是最后一层。可以为空：`files/path++` 也接 `/files`。Go 里用 `c.Param("path")` 读，TypeScript 的 `Params` 里键是 `_splat` |

- 字面量目录名只能用 ASCII 字母、数字和 `- . _ ~`。参数名以字母或 `_` 开头，后面跟字母、数字或 `_`，同一个 URL 里不能重复。
- Go 的包名取自 `package` 那一行，不取目录名。包名按参数起：`project+` 里写 `package project`，别的包 import 它时就叫 `project`。
- go 命令会跳过以 `.` 或 `_` 开头的目录和名为 `testdata` 的目录。这些目录里有 `route.go`、`route.tsx`、`layout.go` 或 `layout.tsx` 时构建报错，放别的文件没问题，比如 `_components` 目录。
- 目录名还得是合法的 Go import 路径，所以 `con`、`x~1` 这类名字（Windows 保留名和短文件名）构建报错。
- 一个目录下最多一个参数目录。`items/id+` 和 `items/key+` 或 `items/path++` 并列会构建报错，因为它们读的是同一段 URL。
- 两个路由（页面或 API 路由）的 URL 有交集、又分不出谁更具体时构建报错，规则同 Go 的 `http.ServeMux`，比如 `a/b+` 和 `a+/b`。字面量比参数具体，所以 `items/new` 和 `items/id+` 并列没问题。
- 只有 Layout、没有页面的目录，它的 URL 显示 404 页面，除非它下面紧挨着的 `name++` 目录里有页面，这时由那个页面以空的剩余路径接这个 URL。别的页面不能匹配这个 URL，比 Layout 路径更具体的除外：有 `settings/layout.tsx`、没有 `settings/route.tsx`，又有 `user+/route.tsx` 时构建报错，因为 `/settings` 也匹配 `/{user}`，而 `items/new` 和 `items/id+` 里的 Layout 并列没问题。
- 声明 `const Path` 会构建报错，目录就是 URL。

## route.go

```go
package order

func Load(c *tifa.Ctx) (orders.Order, error) {
	p, err := tifa.Use(c, project.Layout)
	if err != nil {
		return orders.Order{}, err
	}
	return orders.GetOrder(p.ID, c.Param("order"))
}

type ShipInput struct {
	Carrier string `json:"carrier"`
}

func POST(c *tifa.Ctx, in ShipInput) (orders.Shipment, error) {
	return orders.ShipOrder(c.Param("project"), c.Param("order"), in.Carrier)
}
```

- `Load` 返回页面数据，返回类型要能转成 TypeScript（见下文「JSON 规则」）。
- `GET`、`POST`、`PUT`、`PATCH`、`DELETE` 处理和函数同名的 HTTP 方法。
- 目录里有 `route.tsx` 就是页面，GET 由框架渲染，这时不能再写 `GET`。不需要数据、也没有别的方法的页面可以只有 `route.tsx`，不写 `route.go`。
- 有 `route.go`、没有 `route.tsx` 的目录是 API 路由：方法函数照写的那样回答，不能写 `Load`，因为 `Load` 是给页面取数的。
- 全部是包级函数和常量。依赖直接用应用初始化好的全局变量，框架不做依赖注入。

返回错误时：

| 返回 | 结果 |
| --- | --- |
| `tifa.ErrNotFound` | 404 |
| `c.Redirect(url, code)` | 重定向。整页加载和导航跟着跳，浏览器里调用的方法函数也会把浏览器带过去（见下文） |
| `tifa.Error{Status, Message}` | 按它的状态码和消息回答，不进 `OnError`。状态码不在 400 到 599 之间时按意外错误处理 |
| `tifa.Fields{"carrier": "请选择承运商"}` | 答 422，按入参字段的 json 名字各带一条消息，不进 `OnError` |
| 其他错误、panic | 交给 `OnError`，没定义时记日志并答 500 |

方法函数出错时回答 JSON：`{"error": "消息"}`，`tifa.Fields` 还会加上 `"fields": {"carrier": "请选择承运商"}`。

`Load` 里只能设置响应头和 cookie，调用 `c.JSON`、`c.Text`、`c.NoContent` 会返回错误。

## 带入参和返回值的方法函数

方法函数可以在 Ctx 后面接一个入参，在 error 前面返回一个结果。入参由框架解码，结果由框架写出：

```go
func POST(c *tifa.Ctx) error                       // 自己写响应
func POST(c *tifa.Ctx, in ShipInput) error         // 成功答 204
func POST(c *tifa.Ctx) (Shipment, error)           // 答 200，结果写成 JSON
func POST(c *tifa.Ctx, in ShipInput) (Shipment, error)
```

- 入参是结构体，不能是指向结构体的指针。POST、PUT、PATCH 从请求体解码：JSON，或 URL 编码、multipart 表单。表单只填字符串、数字、布尔或它们的切片类型的字段，表单里出现其他字段的值时答 400。GET、DELETE 从查询字符串解码，所以字段只能是字符串、数字、布尔或它们的切片，不能有嵌入字段，也不能是带 `MarshalText` 或 `UnmarshalText` 的类型，入参类型本身也不行。
- 字段按 `json` 标签取名，表单和查询字符串也一样。写 `form` 标签会构建报错。路径参数、请求头、cookie 照旧从 `c` 读。
- 入参和结果遵守下文的 JSON 规则，和 `Load` 的返回值一样。入参还要能从它的 TypeScript 类型描述的 JSON 读回来：因为 `MarshalText` 成为 string 的类型也要有 `UnmarshalText([]byte) error`，有 `UnmarshalText` 的类型在 TypeScript 里必须是 string（map 键除外，JSON 的键总是字符串），`UnmarshalJSON` 不接受（`time.Time` 除外）。
- 入参类型有 `Validate` 方法（必须是 `func() error`）时，它在解码之后、处理函数之前执行。返回 `nil` 就继续。返回 `tifa.Error` 或 `tifa.Fields` 时原样回答，想标出具体字段就返回 `tifa.Fields`。其他错误一律答 400，消息是 `err.Error()`，会原样给用户看到。
- 请求体或查询字符串解码失败时答 400（请求体超过上限答 413，其他内容类型或没有内容类型答 415），处理函数不执行。
- 自己写响应的方法函数用 `c.Bind(&in)` 解码请求体，用 `c.BindQuery(&in)` 解码查询字符串。它们给字段取名时先看 `form` 标签，再看 `json` 标签，最后用 Go 字段名。

## Ctx

`*tifa.Ctx` 是请求和它的响应。`Load`、`Layout`、方法函数、`OnRequest` 和 `OnError` 都拿到它。

| 方法 | 作用 |
| --- | --- |
| `c.Param(name)` | 路径参数，已解码，比如目录 `order+` 用 `c.Param("order")` |
| `c.Query(name)` | 查询参数的第一个值，没有时为 `""` |
| `c.QueryAll(name)` | 查询参数的全部值，比如 `?tag=a&tag=b` 得到 `["a", "b"]`，没有时为 `nil` |
| `c.Header(name)`、`c.Cookie(name)` | 请求头、请求 cookie（没有时返回 `http.ErrNoCookie`） |
| `c.Context()`、`c.SetContext(ctx)` | 请求 context，替换它来存请求级的值 |
| `c.Pattern()` | 匹配到的路由路径，比如 `/projects/{project}`，没匹配时为 `""` |
| `c.IsData()` | 是不是浏览器路由的导航数据请求，而不是整页加载 |
| `c.SetHeader(key, value)`、`c.SetCookie(cookie)` | 响应头、响应 cookie。`Load` 里也能用 |
| `c.JSON(code, v)`、`c.Text(code, s)`、`c.NoContent(code)` | 写响应。`Load` 里不能用 |
| `c.Redirect(url, code)` | 返回一个错误值，把它 return 出去就会重定向 |
| `c.Bind(&v)`、`c.BindQuery(&v)` | 把请求体或查询字符串解码进结构体，见上文。`Load` 里不能用 |
| `c.SetBodyLimit(n)` | 改这次请求的请求体上限（默认 `server.body_limit`）。要在读请求体之前调用，通常在 `OnRequest` 里。开始读以后调用会返回错误 |
| `c.Next()` | 在 `OnRequest` 里执行匹配到的路由，只能调一次 |
| `c.Request()`、`c.Writer()` | 原始的 `*http.Request` 和 `http.ResponseWriter`，只给方法函数和 `OnRequest` 用 |

## route.tsx

```tsx
import { route, type Data } from './+route'

export default function OrderPage() {
  const order = route.useLoaderData()
  return <OrderItems />
}

export const head = ({ loaderData }: { loaderData?: Data }) => ({
  meta: loaderData ? [{ title: loaderData.id }] : [],
})
```

- 只放界面，不取数。`./+route` 是生成的模块，导出 `route`、`Data`（`Load` 的返回类型）和 `Params`。同目录的子组件同样 import `./+route`。Layout 的数据来自它所在目录的 `+layout` 模块：`layout.tsx` import `'./+layout'`，页面读项目 Layout 的数据时 import 比如 `'../../+layout'`。
- 默认导出是页面组件。可选导出 `head`、`Error`、`NotFound`。
- 在浏览器里，每个 `route.tsx`，以及根 Layout 以外的每个 `layout.tsx`，连同它 import 的一切（含 `head`）各自是一个 chunk。路由第一次用到时才加载，路由器等它加载完才显示页面、运行 `head`。页面的 HTML 预加载它的 Layout 和路由的 chunk，所以它们和入口脚本一起到。文件顶层的代码（含只为副作用的 import）在这时运行，而不是应用启动时，所以每个页面都需要的初始化放在根 Layout 里，或放在它 import 的模块里。见[浏览器里的路由模块](build.zh-CN.md#浏览器里的路由模块)。
- Go 的切片和 map 在 TypeScript 里带 `| null`（Go 的 nil 编码成 `null`），用之前写 `?? []`。
- 模块顶层就要用浏览器能力的组件放进 `.client.tsx`，用 `tifa/client` 的 `clientOnly` 延迟加载。它不进 SSR 包，首屏显示占位内容。能渲染、但要等浏览器画出来才有内容的组件也适合这样放，见[只在浏览器里绘制的组件](#只在浏览器里绘制的组件)：

```tsx
import { clientOnly } from 'tifa/client'

const OrderActions = clientOnly(() => import('./actions.client'), <button disabled>正在准备操作…</button>)
```

- 不经过调用函数（见下）的写操作之后，调用 TanStack 的 `router.invalidate()`，当前页面和所有 Layout 的数据重新取一次，合并成一个请求，它返回的 promise 在页面显示出新数据后兑现。

### 在浏览器里调用方法函数

有方法函数的目录，它的 `+route` 模块按方法各导出一个同名函数。API 路由也有 `+route` 模块，页面可以 import 任何目录的方法函数：

```tsx
import { POST } from './+route'
import { TifaError } from 'tifa/client'

async function ship() {
  try {
    const shipment = await POST({ params: { project, order }, input: { carrier: 'SF' } })
  } catch (e) {
    if (e instanceof TifaError) setErrors(e.fields) // e.status、e.message、e.fields
  }
}
```

- 函数接收一个对象。`params` 是路径参数，目录没有参数时不写。`name++` 目录的键是 `_splat`。会把请求带到别的路径的值在发请求前就被拒绝，promise 失败：`name+` 参数的空值、`.`、`..`，以及 `_splat` 里的 `.`、`..` 段和末尾以外的空段。`_splat` 为空没问题。`input` 是处理函数的入参，没有入参时不写。`signal` 用来取消请求。
- GET、DELETE 把入参放进查询字符串，其他方法发 JSON 请求体。返回值的类型来自 Go 的返回值，并为重定向（见下文）加上 `| undefined`：`func(c, in) (Out, error)` 得到 `Promise<POSTOutput | undefined>`。`func(c, in) error` 得到 `void`。`func(c) error` 自己写响应，它的调用函数原样交回 `Response`，错误状态码也一样。
- POST、PUT、PATCH、DELETE 成功后，先重新取当前页面和它的 Layout 的数据，再兑现 promise。传 `invalidate: false` 可以不刷新。失败和 GET 都不刷新。
- 错误回答抛出 `TifaError`，带 `status`、`message` 和 `fields`（`tifa.Fields` 的消息，其他情况为空）。网络错误照 `fetch` 的样子抛出。
- 处理函数或 `OnRequest` 返回 `c.Redirect(url, code)` 时，浏览器去那个 URL：本站的 URL 经路由器跳转，页面不重新加载，别的站点整页加载。POST、PUT、PATCH、DELETE 之后（除非传了 `invalidate: false`），路由器里已有的数据先标为过期，所以新页面和它的 Layout 显示的是新数据。重定向到正在显示的页面时，和没有重定向的调用一样重新取这一页的数据。路径、hash 和查询参数的值都相同就算同一页，查询参数的顺序和编码不论。promise 在新页面显示出来后兑现为 `undefined`，有返回值的处理函数也一样。一次调用是否被重定向，要等回答来了才知道（`OnRequest` 可以重定向任何请求），所以有返回值的处理函数，返回类型都带 `undefined`。自己写响应的处理函数，调用函数拿到这次重定向的 `Response`（`ok` 为真，状态码在 `X-Tifa-Redirect` 里）。相对 URL 按处理函数的 URL 解析，不按页面的 URL。处理函数经 `c.Writer()` 自己写的 3xx，或者代理写的 3xx，浏览器读不到，promise 以 `Error` 失败。
- 在事件处理函数或 effect 里调用。服务器渲染页面期间调用会抛错。
- `+route` 还为有入参或返回值的处理函数导出 `POSTInput`、`POSTOutput` 等类型。

要在创建之后去新建内容的页面，可以让处理函数返回重定向，也可以返回结果再自己导航。第二种写法要传 `invalidate: false`，免得当前页面在离开前又取一次数据。路由器里已有的数据（比如两个页面共用的 Layout 的数据）这时保持原样：

```go
func POST(c *tifa.Ctx, in AddInput) error {
	todo, err := AddTodo(in.Title)
	if err != nil {
		return err
	}
	return c.Redirect("/todos/"+todo.ID, http.StatusSeeOther)
}
```

```tsx
const router = useRouter() // 来自 @tanstack/react-router
// 在事件处理函数里，这里的 POST 返回 (Todo, error)：
const todo = await POST({ input: { title }, invalidate: false })
if (todo) await router.navigate({ to: '/todos/$todo', params: { todo: todo.id } }) // undefined 表示已被重定向
```

## Layout

Layout 放在它的目录里，和这个目录的页面（如果有）并列：`layout.go`、`layout.tsx` 套到同目录的页面和下面所有页面。`layout.tsx` 用 `<Outlet />` 放子路由。两个文件都可以省掉一个：没有 `layout.tsx` 时子路由直接渲染，没有 `layout.go` 时 Layout 没有数据。所在目录和下面都没有页面的 Layout 构建报错。

```go
package project

func Layout(c *tifa.Ctx) (projects.Project, error) {
	return projects.GetProject(c.Param("project"))
}

const LayoutSSR = false // 可选，默认 true
```

- 同一个目录的 `layout.go` 和 `route.go` 是同一个 Go 包，但每个名字有固定的文件：`Layout`、`LayoutSSR` 写在 `layout.go`，`Load`、`SSR`、`Cache` 和方法函数写在 `route.go`。写在包里其他文件会构建报错。辅助函数和类型写在哪个文件都行。
- Layout 用 `c.Param` 只能读自己目录路径里的参数。`app/projects/project+/layout.go` 能读 `project`，不能读 `order`。页面的 `Load` 同样只能读自己路径里的参数。
- 同一个 Layout 下切换页面时，Layout 组件不重新挂载，它的状态保留。`Layout` 函数读的参数变了（比如换了一个项目），Layout 重新挂载，状态重置。它不读的参数变了，状态保留，页面同理。
- 页面和 Layout 都可以用 `tifa.Use(c, project.Layout)` 拿到套着自己的 Layout（同目录或祖先目录的）本次请求的结果，同一个 `Layout` 每个请求只跑一次。引用别的 Layout 构建报错，传入的不是包级 `Layout` 函数（比如闭包）也报错。`Layout` 函数不能引用自己所在目录的 Layout，直接引用或经辅助函数引用都不行。
- 浏览器里跳转时，导航数据请求只执行所列路由的 `Load` 和 `Layout` 函数。祖先 Layout 的数据还在缓存里时，它不在列表中，它的 `Layout` 这次不会再跑。所以权限检查不要只写在 Layout 里：放进 `OnRequest`，或者让页面的 `Load` 用 `tifa.Use` 依赖做检查的那个 Layout，这样每次请求都会执行它。
- 文档外壳（`<html>`、`<head>`、资源与注入数据的脚本）由框架写成字符串，React 只渲染 `<body>` 里的应用根节点，所以任何一层出错或 404 时页面仍是完整文档。根 Layout（`app/layout.tsx`）可以导出两个常量定制外壳：`htmlAttributes` 是 `<html>` 的属性（不导出时为 `{ lang: 'en' }`），`documentHead` 是每个页面都带的 head 标签（没有路由匹配、根 `Layout` 失败时也带），格式同 `head` 的返回值。在这里放网站的默认 `title`，没有路由匹配的 404 页面也有标题，页面自己的 `head` 返回 `title` 时会覆盖它。导出 `Document` 组件会构建报错。

```tsx
export const htmlAttributes = { lang: 'zh-CN' }
export const documentHead = {
  meta: [{ title: '山海工作台' }, { name: 'theme-color', content: '#f7f8f5' }],
  links: [{ rel: 'icon', href: '/favicon.svg' }],
}
```

- 页面的 head 由匹配到的路由的 `head` 返回值决定：`title` 和同名的 meta 取最深的路由，link、style、script 按路由顺序去重。框架默认带 `<meta charset="utf-8">` 和 viewport meta，路由的 `name: 'viewport'` meta 会替换后者。标签的属性按 React DOM 在服务器上的规则写：`className`、`htmlFor`、`httpEquiv`、`crossOrigin` 等改成 HTML 的名字，`on` 开头的事件属性、非法的属性名、`null`、`undefined`、函数不写，`async`、`defer` 这类布尔属性为真时写成空值，其他属性的布尔值只在 `data-`、`aria-` 属性上写成字符串，`javascript:` 链接被替换。head 标签不支持 `style` 属性，写了会让渲染出错。`htmlAttributes` 按同样的规则写。style、script 的内容原样写入，只有其中会结束元素的 `<script`、`</script`（style 里是 `<style`、`</style`）按 React DOM 的方式转义。浏览器里跳转、`router.invalidate()` 之后 head 随新的匹配更新。

## 404

`route.tsx` 和 `layout.tsx` 可以导出 `NotFound`：

| 情况 | 显示 |
| --- | --- |
| URL 没有匹配任何页面，比如只有 Layout、没有页面的目录的 URL | 根 Layout 的 `NotFound`，不带根 Layout 的界面，不执行任何 `Load` 或 `Layout` |
| 页面 `Load` 返回 `tifa.ErrNotFound` | 从这个页面往上第一个 `NotFound`，外层 Layout 照常显示 |
| `Layout` 函数返回 `tifa.ErrNotFound` | 从上一层 Layout 开始往上找 |
| 都没有导出 | 框架默认页面 |

这几种情况的状态码都是 404。根 Layout 的 `NotFound` 有时单独显示，所以不能读 loader 数据。

## 页面上的错误

`route.tsx` 和 `layout.tsx` 可以导出 `Error`。这一层的 `Load` 或 `Layout` 失败，或者它下面的组件在浏览器里抛错时，在这一层的位置显示它：

```tsx
import { RouteError } from 'tifa/client'

export function Error({ error }: { error: unknown }) {
  if (error instanceof RouteError && error.status === 403) return <p>没有权限</p>
  return <p>出了点问题</p>
}
```

- `Load` 或 `Layout` 失败时，`Error` 收到 `RouteError`，带 `status` 和公开的消息（`tifa.Error` 的消息，或 `OnError` 返回的）。服务器上和 hydration 之后都一样。
- 自己没导出 `Error` 的路由，用最近的导出了 `Error` 的祖先的，都没有就用框架默认的。
- 页面的状态码取最外层失败的那一层。
- 服务器渲染页面时组件抛错，整次渲染失败。这算意外错误：交给 `OnError` 决定状态码（没定义 `OnError` 时是 500），浏览器拿到的是 Go 写的简单错误页，只有这个状态码和公开的消息，不是你的 `Error` 组件。这次回答不进缓存。在 `tifa dev` 下，这张页还会显示错误和它在源文件里的错误栈，保存修复后自动刷新（见 [tifa dev](dev.zh-CN.md#页面渲染失败)）。
- 服务器渲染用完 `ssr.vm_memory` 或超过 `ssr.timeout`（见 [config.zh-CN.md](config.zh-CN.md)）时当场停下。你的代码和库里的 `catch`、`finally`、promise 处理函数都不会为它运行，所以页面接不住它，也没法改成显示别的内容。这次渲染照上面那样失败，VM 换成新的。无限递归的代码得到 `RangeError: Maximum call stack size exceeded`，和 Node 一样，`catch` 能接住（见[栈深度](config.zh-CN.md#栈深度)）。没有代码接住时，它和其他错误一样让渲染失败。
- 浏览器接管之后组件抛错，显示最近的 `Error`，和 `Load` 失败一样。
- 无论哪里失败，文档外壳都是完整的，页面照样加载脚本和样式。

## SSR 与缓存

```go
const SSR = false       // 页面，默认 true
const Cache = true      // 页面，默认 false
const LayoutSSR = false // 本目录的 Layout，默认 true
```

- `SSR = false`：照常执行中间件和所有 `Load`，不进 QuickJS，返回带数据的最简文档，浏览器直接渲染。`LayoutSSR = false` 作用于这个 Layout 套到的所有页面。
- `Cache = true`：渲染结果按构建版本、URL 和数据缓存，缓存大小是 `cache.size`（见 [config.zh-CN.md](config.zh-CN.md)），数据没变时直接返回 HTML。组件渲染时不能读 `Date.now()`、`Math.random()` 这类每次不同的值，这也是 hydration 的要求。
- 页面响应都带 ETag，数据没变的第二次请求带 `If-None-Match` 时答 304，和 `Cache` 无关。

## 服务端渲染时有什么

页面在服务端由 QuickJS 虚拟机渲染，不是 Node，也不是浏览器。虚拟机里有 JavaScript 语言本身（包括 `Proxy`、`WeakRef`、`FinalizationRegistry` 和 `new Function`），另外还有：

- `URL`、`URLSearchParams`、`TextEncoder`、`TextDecoder`、`AbortController`、`Headers`、`Response` 和 `queueMicrotask`。`TextDecoder` 只认 UTF-8，别的编码（比如 `new TextDecoder('latin1')`）会抛 `RangeError`。
- `Intl` 和各个 `toLocale*` 方法，见[服务端渲染里的 Intl](#服务端渲染里的-intl)。
- `console`，有 `log`、`info`、`debug`、`warn`、`error` 和 `assert`。前四个什么也不输出。`error` 和不成立的 `assert` 会抛错，这次渲染失败，错误信息就是它们的参数。其他方法（比如 `console.time`、`console.table`）没有，调用会抛 `TypeError`。
- `Date.now()` 和 `Math.random()`。能用，但它们的值不能进 HTML，见 [SSR 与缓存](#ssr-与缓存)。

没有的：

- 定时器：`setTimeout`、`setInterval`、`requestAnimationFrame`、`requestIdleCallback`。微任务跑完渲染就结束，不会等时间。
- `crypto`、`performance`、`structuredClone`、`atob` 和 `btoa`。
- 浏览器的东西：`window`、`document`、`navigator`、`self`、`location`、`localStorage`、`matchMedia`、`ResizeObserver`、`Event`、`Blob`、`FormData` 等。
- I/O：`fetch`、`XMLHttpRequest`、`WebSocket`、`require`、`process`、`Buffer`。构建时会替换包里的 `process.env.NODE_ENV`，只读它的代码没问题。

React 在服务端不执行 effect，所以只在 `useEffect`、`useLayoutEffect` 和事件处理里用到这些的代码没问题。渲染过程中用到会让这次渲染失败，模块顶层用到会让虚拟机加载不起来。先用 `typeof` 判断，或者挪进 effect，或者把组件放进 `.client.tsx`。

### 在服务端渲染得不一样的库

有些库把一部分工作放在 effect 里，服务端渲染出的内容比浏览器稍后显示的少。照下面这样写，页面仍然能无错水合：

- react-spring：`useSpring` 不传 `from`，`useTransition` 传 `initial: null`。这样服务端和浏览器的第一次渲染都直接显示最终值，之后的变化才有动画。
- react-hook-form：字段用 `Controller` 写。受控字段的值由服务端写进 HTML，`register` 的字段要等水合后才有值。提交按钮在水合前保持禁用（比如用 `useSyncExternalStore(() => () => {}, () => true, () => false)` 判断），免得水合前按回车把表单当普通 HTML 表单提交。
- Radix Select（也就是 shadcn 的 Select）：选中项的文字要等水合后才搬进触发按钮。字段有值时，把这段文字作为 `SelectValue` 的 children 传进去，否则服务端渲染出一个空按钮。没有值时照常显示占位文字。
- recharts 3 在服务端只渲染一个空的外层容器，水合后才画图。给图表区域固定高度，页面就不会跳动，关键数字另外用文字显示。想让图表完全不进服务端渲染，见[只在浏览器里绘制的组件](#只在浏览器里绘制的组件)。
- 文本排序：服务端的 `localeCompare` 按码点比较，浏览器按语言规则，渲染时排序的文本顺序可能不同。在 Go 里排好，或者用不依赖语言的比较。

### 只在浏览器里绘制的组件

有些组件在服务端什么也显示不出来：它们要量自己的尺寸、在 canvas 上画，或者等 effect 跑完才渲染内容。服务端写出的是一个空盒子或加载状态，水合后由浏览器换掉。这样的组件在服务端照样付全价：代码在 SSR 包里，每个虚拟机都要加载它，页面每次渲染都要运行它。

这类组件用 `clientOnly` 加载，配一个同样大小的占位。判断一个组件适不适合，就看服务端为它写出的 HTML（查看网页源代码）。如果是空盒子、加载状态，或者浏览器会整个丢掉的标记，服务端渲染它什么也得不到。希望用户在脚本跑起来之前就看到、或者希望搜索引擎读到的文字（比如图表背后的关键数字），应该留在服务端渲染里：在组件旁边用普通文字写出来。

```tsx
import { clientOnly } from 'tifa/client'
import { route } from './+route'

// sales-chart.client.tsx 默认导出图表组件。
const SalesChart = clientOnly(() => import('./sales-chart.client'), <div className="h-64 w-full" />)

export default function StatsPage() {
  const { days } = route.useLoaderData()
  return <SalesChart days={days ?? []} />
}
```

- `.client.tsx` 文件默认导出组件，props 照常传。
- 服务端渲染和水合时都渲染占位，所以页面能无错水合。之后浏览器加载这个模块，显示组件。占位要和组件最终的尺寸一样，页面才不会跳动。
- `.client.tsx` 文件 import 的东西都不进 SSR 包。在浏览器里它是单独的 chunk，组件第一次渲染时才下载。页面本身在入口脚本里时也是这样（根 Layout 在入口脚本里）。
- 组件比页面其余部分晚一点出现，因为它的代码在水合之后才下载。下载失败时组件抛错，显示最近的 `Error`。

示例 shop 统计页的两张图表改成这样之后，在同一台机器上轮流测两个版本：SSR 包从 1.15 MB 降到 0.77 MB（少三分之一），程序启动时编译它的时间少 40%，加载每个虚拟机的时间少 24%，渲染后每个虚拟机的内存少 3.8 MB（三分之一），统计页渲染快 21%。浏览器入口脚本从 1.08 MB 降到 0.70 MB，0.38 MB 的图表代码只在打开统计页时才下载。

## 服务端渲染里的 Intl

页面在服务端渲染时可以格式化数字、日期和复数，支持 Chromium 有的全部语言（Unicode CLDR 48.2）。能用的有：

- `Intl.NumberFormat`、`Intl.DateTimeFormat`、`Intl.PluralRules`、`Intl.getCanonicalLocales`，以及各构造器的 `supportedLocalesOf`。
- `Date` 的 `toLocaleString`、`toLocaleDateString`、`toLocaleTimeString`，`Number` 和 `BigInt` 的 `toLocaleString`。

服务端没有 `Intl.Collator`、`Intl.Segmenter`、`Intl.RelativeTimeFormat`、`Intl.ListFormat`、`Intl.DisplayNames`、`Intl.DurationFormat`、`Intl.supportedValuesOf` 和 `formatRange`，渲染时调用它们会失败。`localeCompare` 按码点比较，不按语言。日期用公历（th 用佛历，fa 等少数语言用波斯历），用 `-u-ca-` 或 `calendar` 选项要的其他历法（比如 japanese）会退回这个语言的默认历法。`roundingIncrement` 和 `roundingPriority` 只支持默认值。

不传 locale 时服务端按 `en-US` 格式化，不传 `timeZone` 时按 `UTC`，和机器的语言、时区无关。服务端 `Date` 的本地时间也恒为 UTC：`getTimezoneOffset()` 是 0，`new Date(2026, 0, 1)`、`getHours`、`toString` 都按 UTC 算。应用的 Go 代码不受影响：进程保持启动时的时区，`time.Local`、日志和 SQLite 照常用这个时区，和普通 Go 程序一样。

输出和 Chromium 156 在约 60,000 个表达式上做过比对，除了服务端没有的历法里的日期，全部一致。少数冷门语言和选项和 Chromium 不同：sr-Cyrl-ME 的货币、单位和时区名用西里尔字母，uz-AF 和 uz-Arab-AF 在带时间的日期里把纪元写在年份前面，en-IE 和 fr-MA 加 `hourCycle: 'h11'` 时写 "am" 或 "AM"，Chromium 写 "a.m."，om-KE 的短纪元是 "BA"。错误信息的措辞不同。Firefox 和 Safari 用各自的数据。新版本很接近，但 macOS 26.4 之前的 Safari 数据较旧（CLDR 46），少数格式可能不同。

### 怎么写才能水合一致

同一个调用只有 locale 和时区都相同时，服务端和浏览器才会得到同样的文字。调用里没传的，浏览器按用户的语言和时区补上，服务端按 `en-US` 和 `UTC` 补上，这时 React 会报水合不一致。所以：

- 每次调用都传 locale，日期还要传 `timeZone`，值来自 `Load` 或 Layout 返回的数据。从哪来由应用决定：用户保存的设置、浏览器把 `Intl.DateTimeFormat().resolvedOptions().timeZone` 存进的 cookie，或者 `c.Header("Accept-Language")`。来自请求的值要先校验再交给页面：不认识的时区会抛 `RangeError`，渲染失败。
- 渲染时不要用 `Date` 的本地时间方法（`getHours`、`getDate`、`toString`、`new Date(2026, 0, 1)`）：它们在服务端给出 UTC，在浏览器给出用户的时区。改用 UTC 方法，或者带 `timeZone` 的 Intl。
- 和当前时间有关的文字（比如「3 分钟前」）不管传什么，渲染和水合时都会不同。在 `Load` 里算好，或者放到 `useEffect` 里渲染。

```go
type Receipt struct {
	Amount   float64 `json:"amount"`
	PaidAt   string  `json:"paidAt"`
	Locale   string  `json:"locale"`
	TimeZone string  `json:"timeZone"`
}
```

```tsx
import { route } from './+route'

export default function ReceiptPage() {
  const { amount, paidAt, locale, timeZone } = route.useLoaderData()
  return (
    <p>
      {new Intl.NumberFormat(locale, { style: 'currency', currency: 'CNY' }).format(amount)}
      {new Date(paidAt).toLocaleDateString(locale, { timeZone })}
    </p>
  )
}
```

数据是 `locale: 'zh-CN'`、`timeZone: 'Asia/Shanghai'`、`paidAt: '2025-08-16T20:05:09Z'` 时，服务端和浏览器渲染出的都是 `¥1,234.50` 和 `2025/8/17`。

### tifa dev 下的报警

在 [`tifa dev`](dev.zh-CN.md) 下，渲染时调用 Intl 构造器或 `toLocale*` 方法却没传 locale，或者日期没传 `timeZone`，会打一条警告，带上调用在源码里的位置和调用栈：

```text
2026/10/16 08:00:00 tifa: ssr: Number.prototype.toLocaleString without a locale at app/orders/order+/items.tsx:12:7. The server formats in en-US and UTC, a browser in the language and time zone of its user, so the text can differ from the HTML and React reports a hydration mismatch. Pass the locale and, for a date, the timeZone, from data Load returns, as in toLocaleDateString('zh-CN', {timeZone: 'Asia/Shanghai'}), or format in a useEffect.
    at Price (app/orders/order+/items.tsx:12:7)
    at ...
```

每个位置只打一次，tifa dev 重新加载 SSR 包后再打一次。警告不影响渲染结果。构建出的程序不做这个检查。调用发生在某个包里时，第一行指向包里的文件，你自己的代码在调用栈更下面。

### 代价

一个 VM 在它的渲染第一次用到 Intl 之前，只带 Intl 的占位，约 10 KB。第一次用到时这个 VM 再多约 30 到 45 KB。示例商店的一个 VM 约 11 MB，这部分不到它的 1%。tifa dev 下每个 VM 一开始就付这份代价，因为报警需要先装好 Intl。每种语言在每个进程里只读进内存一次，100 到 180 KB，另外第一次用到任何语言时还要约 1 MB 所有语言共用的数据。语言数据让程序大约增加 1.7 MB，生成的 `main.go` 嵌入的时区数据库再增加约 450 KB。语言数据带有要随程序附带的许可声明，见[要随程序附带的声明](build.zh-CN.md#要随程序附带的声明)。

## 样式

在 Layout 或页面里引入 CSS，比如在 `app/layout.tsx` 里写 `import './site.css'`。CSS 由 Vite 构建：每个页面都引用同一份装着应用全部 CSS 的样式表，CSS 引用的字体和图片放到 `/assets/` 下，文件名带内容哈希（小文件直接内联）。以 `/` 开头的 URL（如 `/logo.svg`）原样保留，可以指向公共目录里的文件。`tifa dev` 下 CSS 经过 Vite 开发服务器，原地更新，见 [dev.zh-CN.md](dev.zh-CN.md)。

### Tailwind CSS

Tailwind CSS v4 通过它的 Vite 插件使用，和在任何 Vite 应用里一样。先安装：

```sh
pnpm add -D tailwindcss @tailwindcss/vite
```

在 `vite.config.ts` 的 plugins 里加上它：

```ts
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import tifa from './.tifa/vite.js'

export default defineConfig({
  plugins: [tifa(), react(), tailwindcss()],
})
```

再在 Layout 引入的样式表里引入 Tailwind：

```css
/* app/site.css */
@import "tailwindcss";
```

Tailwind 扫描应用根目录找类名，跳过 `.gitignore` 列出的文件，新应用里已经包括 `.tifa/` 和 `bin/`。别的目录用 `@source` 加进来。能跑的例子见 `test/apps/tailwind`。

### shadcn/ui

按上文配好 Tailwind 后，shadcn CLI 会把应用认作 Vite 应用：

```sh
pnpm dlx shadcn@latest init
pnpm dlx shadcn@latest add button card
```

`init` 安装组件要用的包（`radix-ui`、`class-variance-authority`、`lucide-react` 等），写好 `components.json`、`lib/utils.ts`，并把主题写进引入 Tailwind 的样式表。`add button` 把 `components/ui/button.tsx` 写到应用根目录下，页面代码从 `@/components/ui/button` 引入。两者在第一次构建前就能用。

`@/` 指向应用根目录，由应用 tsconfig 里的 `paths` 设定（见 [build.zh-CN.md](build.zh-CN.md#应用的-tsconfig-与-vite-配置)）。这些组件和其他 React 代码一样在服务端渲染。

## hooks.go

项目根目录的 `hooks.go`，四个函数都可选：

```go
func OnStart() error                            // 开始监听之前，打开数据库、日志等
func OnShutdown(ctx context.Context) error      // 收到退出信号、在途请求完成之后
func OnRequest(c *tifa.Ctx) error              // 唯一的中间件，调用 c.Next() 继续
func OnError(c *tifa.Ctx, err error) tifa.Error // 意外错误：上报，返回能给用户看的错误
```

- `OnRequest` 作用于页面、导航数据请求、API 路由和没匹配的 URL，不作用于静态文件。没匹配时 `c.Pattern()` 为空。
- 整个应用共享的资源，比如数据库连接、日志，放在应用根目录、和 `hooks.go` 并列的文件里，比如 `db.go`、`middleware.go`。每个文件构建一个包级变量或一个函数：在 `OnStart` 里初始化，在 `OnShutdown` 里释放，其他地方直接用，比如 import `example.com/myapp` 之后写 `myapp.DB`。`app/` 下的领域 import 根包来用它们。按约定，根包不 import `app/` 下的任何包，所以这样永远不会构成循环引用。模型和查询留在各自的领域里。路由代码只在函数里用这些资源，不在包级变量初始化时读。不用 `init()` 做初始化。见 [import 的方向](structure.zh-CN.md#import-的方向)。
- 请求级的值用标准 context 写入：`c.SetContext(context.WithValue(...))`，业务包提供带类型的读取函数。

## 测试

`Load`、`Layout`、方法函数和 `OnRequest` 都是普通的 Go 函数，在代码旁边用 `go test` 测。`tifatest.NewCtx(t, r)` 把一个 `httptest` 请求变成函数要的 `*tifa.Ctx`，同时返回记录函数写出内容的 recorder：

```go
// app/projects/project+/orders/order+/route_test.go
func TestLoad(t *testing.T) {
	r := httptest.NewRequest("GET", "/projects/studio/orders/SH-1041", nil)
	r.Pattern = "/projects/{project}/orders/{order}"
	r.SetPathValue("project", "studio")
	r.SetPathValue("order", "SH-1041")
	c, _ := tifatest.NewCtx(t, r)
	o, err := tifa.Use(c, Load)
	// 检查 o 和 err
}
```

这样测的是函数，不是 HTTP 响应，要检查的是函数的返回值和错误。解码入参、跑 `Validate`、把结果变成 JSON 或 204、把错误变成 404、422、重定向或交给 `OnError`、回答导航数据请求，这些只有服务端做。返回值和错误不会写进 recorder，函数自己没写响应时，recorder 一直是空的 200。要检查 HTTP 响应，就向构建好的应用发请求。

- `c.Param` 读的就是路径参数，用 `r.SetPathValue` 设置解码后的值。`c.Pattern()` 是调用 `NewCtx` 时的 `r.Pattern`。
- `Load` 和 `Layout` 通过 `tifa.Use(c, Load)` 调用，这样跑起来的规则和服务端一样：同一个请求只跑一次，写响应体会失败。`Load` 里 `tifa.Use` 的 Layout 在同一个请求上跑。只有函数用 `tifa.Use` 要的 Layout 才会跑。
- 方法函数直接调用：`POST(c)`，有入参就是 `POST(c, in)`。入参就是你传的值，所以 `Validate` 单独测。`Validate` 会改入参时，传改过的入参。
- 测 `OnRequest` 时，`tifatest.SetNext(c, next)` 设置 `c.Next()` 调用什么：`next` 原样跑一次，它的错误就是 `c.Next()` 的返回值。它代替的是路由，所以不恢复 panic，也不写出结果。
- 请求体上限是 `server.body_limit` 的默认值 1 MiB，可以用 `c.SetBodyLimit` 改。测试结束时会清理请求的 multipart 临时文件。
- `tifa build` 不读 `_test.go` 文件，所以测试可以放在 `app/` 下任何目录里。

## Load 里 Ctx 的用法

这一节管 `Load`、`Layout` 和它们调用的函数。构建时，tifa 从每个 `Load` 和 `Layout` 函数出发，找出它读的路由参数（`c.Param`）、查询参数（`c.Query` 和 `c.QueryAll`）和它依赖的 Layout（`tifa.Use`）。`Load` 把 `*tifa.Ctx` 传给本模块里的函数时，tifa 会一层层跟进去，跨包也可以，读到的参数都算在这个 `Load` 名下。一个辅助函数被多个 `Load` 调用时，分别算给每个 `Load`。tifa 自己的函数不跟进。

为了让 tifa 找全，Ctx 只能用在它看得见的地方。`c.Param`、`c.Query` 和 `c.QueryAll` 的参数写常量字符串，比如 `c.Param("order")`。它们和 `tifa.Use` 都要直接调用，存进变量或当作值传出去都会构建报错。

能持有 Ctx 的类型只有：`*tifa.Ctx`、`tifa.Ctx`、本模块里声明的非泛型结构体（字段只通过这几种类型持有 Ctx，直接含有、嵌入或间接含有都可以），以及指向这种结构体的指针。

持有 Ctx 的值只能出现在这五个地方：

1. 直接调用方法时的接收者，比如 `c.Query("tab")`、`req.Page()`。
2. 传给本模块或 tifa 的非泛型函数，对应参数的类型完全相同。就地调用的函数字面量也算。
3. 写进持有 Ctx 的结构体字面量，字段类型完全相同。
4. 用 `:=` 或 `var x T = ...` 赋给类型完全相同的新局部变量，这个变量同样受这些规则约束。
5. 取地址，得到的指针仍是能持有 Ctx 的类型。

其他用法都会构建报错，并指出文件和行号，比如：转换成别的类型或接口，赋给已有变量，多个值一起赋值，`return` 它，放进切片、map、数组或通道，`range`，包级变量，`defer`，`go` 语句，传给泛型函数、接口方法、函数变量或模块外的函数（标准库和第三方库）。`return c.Redirect(...)`、`return c.Next()` 不受影响，返回的是方法调用的结果，不是 Ctx 本身。

`Load`、`Layout` 和它们调用的函数里不能调用 `c.Request()` 和 `c.Writer()`，通过原始请求读的参数 tifa 看不到。改用 `c.Param`、`c.Query`、`c.QueryAll`、`c.Header`、`c.Cookie` 或 `c.Context()`。这两个方法只在方法函数和 `OnRequest` 里用。

`c.BindQuery` 和 `c.Bind` 也不能调用。结构体解码了哪些查询参数 tifa 说不出名字，这些参数变了页面不会重新取数据，而页面加载和导航的数据请求都没有请求体。查询参数用 `c.Query("name")` 逐个读，重复的参数用 `c.QueryAll("name")`。

遇到报错时，直接调用 Ctx 的方法，或者把它传给普通函数。只需要请求上下文时传 `c.Context()`，只调用 `c.Context()` 的辅助函数（比如 `auth.User(c)`）跟进后什么也不收集。

tifa 只检查 `Load` 和 `Layout` 能到达的代码。在 `OnRequest`、方法函数或别处把 Ctx 存进全局变量或其他共享位置，再在 `Load` 或 `Layout` 里取出来用，这条路查不到，不要这样写。

## JSON 规则

`Load` 和 `Layout` 函数的返回值、方法函数的入参和结果都以 JSON 传输，tifa 按它们生成 TypeScript 类型。为了在 Go 1.27 的两种 `encoding/json` 实现下（默认实现和 `GOEXPERIMENT=nojsonv2`）输出一致，只接受下面这些类型。

| Go | TypeScript |
| --- | --- |
| `bool` | `boolean` |
| 整数、浮点数、`json.Number` | `number` |
| `string`、`time.Time` | `string` |
| 指针 | `T \| null` |
| 切片 | `T[] \| null`。元素就是 `byte` 或 `uint8` 的 `[]byte` 输出 base64，为 `string \| null`。元素是具名 byte 类型的切片不接受 |
| 数组 | `T[]`，长度为 0 时为 `[]` |
| map | `Record<string, V> \| null` |
| 结构体 | 导出字段组成的对象 |

- map 的键必须是不带 `MarshalText` 的 string 或整数类型，或者是按下面规则输出 string 的类型。接口和指针不能做键。
- 导出的嵌入结构体（也可以是指针）没写 json 名字时，字段提升到外层，经由嵌入指针到达的字段是可选的。未导出的嵌入字段不接受，除非标了 `json:"-"`。
- json tag 的名字只能用字母、数字、空格和 `` !#$%&()*+-./:;<=>?@[]^_{|}~ ``。选项只能用 `omitempty`、`omitzero` 和 `string`，用了 `omitempty` 或 `omitzero` 的字段是可选的。`string` 选项只能用在 bool、预声明的数字类型或 string 字段上，或者指向它们的非具名指针上。
- 类型的值或指针上有名为 `MarshalJSON`、`MarshalJSONTo`、`AppendText` 或 `AppendJSON` 的方法或字段时，不论签名一律拒绝（`time.Time` 和 `json.Number` 除外）。请改名，或换一个不带这些方法的类型返回。
- 类型自己在值接收者上声明了签名正好是 `MarshalText() ([]byte, error)` 的方法时，输出 string。声明在指针接收者上时，只有通过指针持有才输出 string。签名不对、从嵌入字段提升而来、或被同名字段遮住时，一律拒绝。
- 接口出现在任何位置都不接受，包括字段、元素和 map 键。chan、func、复数、`unsafe.Pointer` 和类型参数也不接受。
