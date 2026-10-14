---
summary: '怎么写一个 tifa 应用：目录结构、目录怎么变成 URL、route.go 与 route.tsx、Layout、hooks.go、404、SSR 与缓存开关，Load 里 Ctx 的用法、带类型入参和结果的方法函数，以及数据、入参和结果的 JSON 规则'
read_when:
  - 新建一个 tifa 应用，或给应用加页面、Layout、接口
  - 构建报 Ctx 用法或 Load 返回类型的错误，想知道怎么改
  - 改 internal/analyze 的规则，需要知道对用户承诺了什么
  - 改 examples/shop 或 test/apps 里的应用
---

[English](app.md) · 简体中文

# 应用怎么写

完整的例子是 [examples/shop](../examples/shop)。构建、生成文件和 go.mod 的配法见 [build.zh-CN.md](build.zh-CN.md)。

## 目录

```text
myapp/
  go.mod
  hooks.go                          进程与请求钩子（可选）
  db.go                             共享资源：包级变量和函数（可选）
  project.go  order.go              领域代码，一个领域一个文件，平铺在根包里
  app/
    layout.go  layout.tsx           根 Layout，套到所有页面
    route.go                        /
    projects/
      project+/
        layout.go  layout.tsx       /projects/{project} 及下面所有页面的 Layout
        route.go  route.tsx         页面 /projects/{project}
        orders/
          order+/
            route.go  route.tsx     页面 /projects/{project}/orders/{order}
            items.tsx  actions.client.tsx
```

- 代码按领域分组。新应用把领域代码（类型、存储、规则）平铺在根包里，一个领域一个文件，`app/` 下的目录放调用它的页面。领域做大了再拆成单独的包。不设一个装着所有表的 `models/` 或 `db/` 包。见 [structure.zh-CN.md](structure.zh-CN.md)。
- 跨页面共用的组件和样式放在 `app/` 根目录。
- 不写 main。`tifa build` 把 main 和路由注册代码生成在 `.tifa/`，`go vet ./...`、`go test ./...` 不会扫到它。

## 目录就是 URL

`app/` 下每一层目录是 URL 的一段，`app/` 本身是 `/`。

| 目录名 | URL 段 |
| --- | --- |
| `orders` | 字面量 `orders` |
| `order+` | 参数 `{order}`，用 `c.Param("order")` 读 |
| `path++` | 剩余路径 `{path...}`，只能是最后一层 |

- 字面量目录名只能用 ASCII 字母、数字和 `- . _ ~`。参数名以字母或 `_` 开头，后面跟字母、数字或 `_`，同一个 URL 里不能重复。
- Go 的包名取自 `package` 那一行，不取目录名。包名按参数起：`project+` 里写 `package project`，别的包 import 它时就叫 `project`。
- go 命令会跳过以 `.` 或 `_` 开头的目录和名为 `testdata` 的目录。这些目录里有 `route.go`、`route.tsx`、`layout.go` 或 `layout.tsx` 时构建报错，放别的文件没问题，比如 `_components` 目录。
- 目录名还得是合法的 Go import 路径，所以 `con`、`x~1` 这类名字（Windows 保留名和短文件名）构建报错。
- 两个页面的 URL 匹配同样的请求时构建报错，比如 `items/id+` 和 `items/key+` 并列。两个 URL 有交集、又分不出谁更具体时也报错，规则同 Go 的 `http.ServeMux`。字面量比参数具体，所以 `items/new` 和 `items/id+` 并列没问题。
- 声明 `const Path` 会构建报错，目录就是 URL。

## route.go

```go
package order

func Load(c *tifa.Ctx) (shop.Order, error) {
	p, err := tifa.Use(c, project.Layout)
	if err != nil {
		return shop.Order{}, err
	}
	return shop.GetOrder(p.ID, c.Param("order"))
}

func POST(c *tifa.Ctx) error {
	if err := shop.ShipOrder(c.Param("project"), c.Param("order")); err != nil {
		return err
	}
	return c.NoContent(204)
}
```

- `Load` 返回页面数据，返回类型要能转成 TypeScript（见下文「JSON 规则」）。
- `GET`、`POST`、`PUT`、`PATCH`、`DELETE` 处理和函数同名的 HTTP 方法。
- 目录里有 `route.tsx` 就是页面，GET 由框架渲染，这时不能再写 `GET`。不需要数据、也没有别的方法的页面可以只有 `route.tsx`，不写 `route.go`。
- 有 `route.go`、没有 `route.tsx` 的目录是接口：方法函数照写的那样回答，不能写 `Load`，因为 `Load` 是给页面取数的。
- 全部是包级函数和常量。依赖直接用应用初始化好的全局变量，框架不做依赖注入。

返回错误时：

| 返回 | 结果 |
| --- | --- |
| `tifa.ErrNotFound` | 404 |
| `c.Redirect(url, code)` | 重定向 |
| `tifa.Error{Status, Message}` | 按它的状态码和消息回答，不进 `OnError` |
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

- 入参是结构体。POST、PUT、PATCH 从请求体解码：JSON，或 URL 编码、multipart 表单。GET、DELETE 从查询字符串解码，所以字段只能是字符串、数字、布尔或它们的切片，不能有嵌入字段。
- 字段按 `json` 标签取名，表单和查询字符串也一样。写 `form` 标签会构建报错。路径参数、请求头、cookie 照旧从 `c` 读。
- 入参和结果遵守下文的 JSON 规则，和 `Load` 的返回值一样。
- 入参类型有 `Validate() error` 方法时，它在解码之后、处理函数之前执行。返回 `nil` 就继续。返回 `tifa.Error` 或 `tifa.Fields` 时原样回答，想标出具体字段就返回 `tifa.Fields`。其他错误一律答 400，消息是 `err.Error()`，会原样给用户看到。
- 请求体或查询字符串解码失败时答 400（请求体超过上限答 413，其他内容类型答 415），处理函数不执行。
- 自己写响应的方法函数用 `c.Bind(&in)` 解码请求体，用 `c.BindQuery(&in)` 解码查询字符串。

## Ctx

`*tifa.Ctx` 是请求和它的响应。`Load`、`Layout`、方法函数、`OnRequest` 和 `OnError` 都拿到它。

| 方法 | 作用 |
| --- | --- |
| `c.Param(name)` | 路径参数，已解码，比如目录 `order+` 用 `c.Param("order")` |
| `c.Query(name)` | 查询参数的第一个值，没有时为 `""` |
| `c.Header(name)`、`c.Cookie(name)` | 请求头、请求 cookie（没有时返回 `http.ErrNoCookie`） |
| `c.Context()`、`c.SetContext(ctx)` | 请求 context，替换它来存请求级的值 |
| `c.Pattern()` | 匹配到的路由路径，比如 `/projects/{project}`，没匹配时为 `""` |
| `c.IsData()` | 是不是浏览器路由的导航数据请求，而不是整页加载 |
| `c.SetHeader(key, value)`、`c.SetCookie(cookie)` | 响应头、响应 cookie。`Load` 里也能用 |
| `c.JSON(code, v)`、`c.Text(code, s)`、`c.NoContent(code)` | 写响应。`Load` 里不能用 |
| `c.Redirect(url, code)` | 返回一个错误值，把它 return 出去就会重定向 |
| `c.Bind(&v)`、`c.BindQuery(&v)` | 把请求体或查询字符串解码进结构体，见上文 |
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
- Go 的切片和 map 在 TypeScript 里带 `| null`（Go 的 nil 编码成 `null`），用之前写 `?? []`。
- 模块顶层就要用浏览器能力的组件放进 `.client.tsx`，用 `tifa/client` 的 `clientOnly` 延迟加载。它不进 SSR 包，首屏显示占位内容：

```tsx
import { clientOnly } from 'tifa/client'

const OrderActions = clientOnly(() => import('./actions.client'), <button disabled>正在准备操作…</button>)
```

- 不经过调用函数（见下）的写操作之后，调用 TanStack 的 `router.invalidate()`，当前页面和所有 Layout 的数据重新取一次，合并成一个请求。

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

- 函数接收一个对象。`params` 是路径参数，目录没有参数时不写。`input` 是处理函数的入参，没有入参时不写。`signal` 用来取消请求。
- GET、DELETE 把入参放进查询字符串，其他方法发 JSON 请求体。返回值的类型来自 Go 的返回值，只返回 error 的处理函数是 `void`。自己写响应的处理函数原样交回 `Response`，错误状态码也一样。
- POST、PUT、PATCH、DELETE 成功后，先重新取当前页面和它的 Layout 的数据，再兑现 promise。传 `invalidate: false` 可以不刷新。失败和 GET 都不刷新。
- 错误回答抛出 `TifaError`，带 `status`、`message` 和 `fields`（`tifa.Fields` 的消息，其他情况为空）。网络错误照 `fetch` 的样子抛出。
- 在事件处理函数或 effect 里调用。服务器渲染页面期间调用会抛错。
- `+route` 还为有入参或返回值的处理函数导出 `POSTInput`、`POSTOutput` 等类型。

## Layout

Layout 放在它的目录里，和这个目录的页面（如果有）并列：`layout.go`、`layout.tsx` 套到同目录的页面和下面所有页面。`layout.tsx` 用 `<Outlet />` 放子路由。所在目录和下面都没有页面的 Layout 构建报错。

```go
package project

func Layout(c *tifa.Ctx) (shop.Project, error) {
	return shop.GetProject(c.Param("project"))
}

const LayoutSSR = false // 可选，默认 true
```

- 同一个目录的 `layout.go` 和 `route.go` 是同一个 Go 包，`Layout`、`LayoutSSR` 和页面的 `Load`、`SSR`、`Cache`、方法函数写在哪个文件都行。没有页面的目录里写页面的这些名字会构建报错。
- Layout 用 `c.Param` 只能读自己目录路径里的参数。`app/projects/project+/layout.go` 能读 `project`，不能读 `order`。
- 同一个 Layout 下切换页面时，Layout 组件不重新挂载，它的状态保留。`Layout` 函数读的参数变了（比如换了一个项目），Layout 重新挂载，状态重置。它不读的参数变了，状态保留，页面同理。
- 页面和 Layout 都可以用 `tifa.Use(c, project.Layout)` 拿到套着自己的 Layout（同目录或祖先目录的）本次请求的结果，同一个 `Layout` 每个请求只跑一次。引用别的 Layout 构建报错。
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
| URL 没有匹配任何页面 | 根 Layout 的 `NotFound`，不带根 Layout 的界面，不执行任何 `Load` 或 `Layout` |
| 页面 `Load` 返回 `tifa.ErrNotFound` | 从这个页面往上第一个 `NotFound`，外层 Layout 照常显示 |
| `Layout` 函数返回 `tifa.ErrNotFound` | 从上一层 Layout 开始往上找 |
| 都没有导出 | 框架默认页面 |

这几种情况的状态码都是 404。根 Layout 的 `NotFound` 有时单独显示，所以不能读 loader 数据。

## 页面上的错误

`route.tsx` 和 `layout.tsx` 可以导出 `Error`。这一层的 `Load` 或 `Layout` 失败，或者它下面的组件抛错时，在这一层的位置显示它：

```tsx
import { RouteError } from 'tifa/client'

export function Error({ error }: { error: unknown }) {
  if (error instanceof RouteError && error.status === 403) return <p>没有权限</p>
  return <p>出了点问题</p>
}
```

- `Load` 或 `Layout` 失败时，`Error` 收到 `RouteError`，带 `status` 和公开的消息（`tifa.Error` 的消息，或 `OnError` 返回的）。服务器上和 hydration 之后都一样。
- 自己没导出 `Error` 的路由，用最近的导出了 `Error` 的祖先的，都没有就用框架默认的。
- 页面的状态码取最外层失败的那一层。服务器渲染时组件抛错算意外错误：交给 `OnError` 决定状态码（没定义 `OnError` 时是 500），这次的 HTML 不进缓存。
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

## hooks.go

项目根目录的 `hooks.go`，四个函数都可选：

```go
func OnStart() error                            // 开始监听之前，打开数据库、日志等
func OnShutdown(ctx context.Context) error      // 收到退出信号、在途请求完成之后
func OnRequest(c *tifa.Ctx) error              // 唯一的中间件，调用 c.Next() 继续
func OnError(c *tifa.Ctx, err error) tifa.Error // 意外错误：上报，返回能给用户看的错误
```

- `OnRequest` 作用于页面、导航数据请求、接口和没匹配的 URL，不作用于静态文件。没匹配时 `c.Pattern()` 为空。
- 整个应用共享的资源，比如数据库连接、日志，放在应用根目录、和 `hooks.go` 并列的文件里，比如 `db.go`、`middleware.go`。每个文件只构建一个包级变量或一个函数：在 `OnStart` 里初始化，在 `OnShutdown` 里释放，其他地方直接用，比如 `myapp.DB`。`app/` 下的包 import 根包来用它们，所以根包不反过来 import 它们（Go 不允许循环引用）。表、模型和查询留在各自的领域里。路由代码只在函数里用这些资源，不在包级变量初始化时读。不用 `init()` 做初始化。
- 请求级的值用标准 context 写入：`c.SetContext(context.WithValue(...))`，业务包提供带类型的读取函数。

## Load 里 Ctx 的用法

构建时，tifa 从每个 `Load` 和 `Layout` 函数出发，找出它读的路由参数（`c.Param`）、查询参数（`c.Query`）和它依赖的 Layout（`tifa.Use`）。`Load` 把 `*tifa.Ctx` 传给本模块里的函数时，tifa 会一层层跟进去，跨包也可以，读到的参数都算在这个 `Load` 名下。一个辅助函数被多个 `Load` 调用时，分别算给每个 `Load`。tifa 自己的函数不跟进。

为了让 tifa 找全，Ctx 只能用在它看得见的地方。

能持有 Ctx 的类型只有：`*tifa.Ctx`、`tifa.Ctx`、本模块里声明的非泛型结构体（字段只通过这几种类型持有 Ctx，直接含有、嵌入或间接含有都可以），以及指向这种结构体的指针。

持有 Ctx 的值只能出现在这五个地方：

1. 直接调用方法时的接收者，比如 `c.Query("tab")`、`req.Page()`。
2. 传给本模块或 tifa 的非泛型函数，对应参数的类型完全相同。就地调用的函数字面量也算。
3. 写进持有 Ctx 的结构体字面量，字段类型完全相同。
4. 用 `:=` 或 `var x T = ...` 赋给类型完全相同的新局部变量，这个变量同样受这些规则约束。
5. 取地址，得到的指针仍是能持有 Ctx 的类型。

其他用法都会构建报错，并指出文件和行号，比如：转换成别的类型或接口，赋给已有变量，多个值一起赋值，`return` 它，放进切片、map、数组或通道，`range`，包级变量，`defer`，`go` 语句，传给泛型函数、接口方法、函数变量或模块外的函数（标准库和第三方库）。`return c.Redirect(...)`、`return c.Next()` 不受影响，返回的是方法调用的结果，不是 Ctx 本身。

`Load` 和它调用的函数里不能调用 `c.Request()` 和 `c.Writer()`，通过原始请求读的参数 tifa 看不到。改用 `c.Param`、`c.Query`、`c.Header`、`c.Cookie` 或 `c.Context()`。这两个方法只在方法函数和 `OnRequest` 里用。

遇到报错时，直接调用 Ctx 的方法，或者把它传给普通函数。只需要请求上下文时传 `c.Context()`，只调用 `c.Context()` 的辅助函数（比如 `auth.User(c)`）跟进后什么也不收集。

tifa 只检查 `Load` 能到达的代码。在 `OnRequest`、方法函数或别处把 Ctx 存进全局变量或其他共享位置，再在 `Load` 里取出来用，这条路查不到，不要这样写。

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
