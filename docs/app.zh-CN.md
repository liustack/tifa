---
summary: '怎么写一个 tifa 应用：目录结构、目录怎么变成 URL、一个目录的三个文件（route.go、page.tsx、layout.tsx）、Layout、不套外层的 Layout（layout_.tsx）、hooks.go、会话（存在服务器上的登录状态、它的 cookie、存放位置，以及浏览器怎么显示谁登录了）、404 和错误、多语言网站、SSR 与缓存开关、服务端渲染时有什么、哪些库在那里渲染得不一样、哪些组件该只在浏览器里加载、怎么用 Intl 和 Temporal 才能水合一致、用 Tailwind CSS 和 shadcn/ui 写样式，怎么测 Go 路由代码，Load 里 Ctx 的用法、带类型入参和结果的方法函数、用 tifa.HTML 显示应用信任的 HTML，访客开着旧页面时你部署新版本，浏览器会怎么做，以及数据、入参和结果的 JSON 规则'
read_when:
  - 新建一个 tifa 应用，或给应用加页面、Layout、API 路由
  - 构建报 Ctx 用法或 Load 返回类型的错误，想知道怎么改
  - 改 internal/analyze 的规则，需要知道对用户承诺了什么
  - 改 examples/shop 或 test/apps 里的应用
  - 给 Load、Layout 函数、方法函数或 OnRequest 写 Go 测试
  - 让用户登录登出、读谁登录了，或把会话放到 Redis、SQL 里
  - 在页头显示登录用户的名字，`useSession`、`SignedIn`、`data-session`，或者 CSP 拦住了 head 最前面的脚本
  - 在页面里显示应用自己做出来或信任的 HTML（比如一篇文章），用 `tifa.HTML` 和 `<HTML>`，或者处理其中的链接
  - 给应用加 CSS、Tailwind CSS 或 shadcn/ui
  - 在页面里格式化数字或日期，或者 tifa dev 对某个 Intl 调用报警
  - 访客开着一个页面时你部署了新版本，想知道浏览器会怎么做，或者看到带 `X-Tifa-Build` 的 409
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
  config.go                         应用的配置，见配置文档（可选）
  db.go                             整个应用共享的资源：包级变量和函数（可选）
  app/
    route.go  layout.tsx            根 Layout（route.go 里的 Layout），套到所有页面，以及 /（route.go 里的 GET）
    projects/
      project.go  project_test.go   店铺领域：Project、GetProject、ListProjects
      project+/
        route.go  layout.tsx  page.tsx   /projects/{project} 及下面所有页面的 Layout，以及页面 /projects/{project}
        orders/
          order.go  order_test.go   订单领域：Order、ListOrders、GetOrder、ShipOrder
          order+/
            route.go  page.tsx      页面 /projects/{project}/orders/{order}
            items.tsx  ship-form.tsx
        stats/
          route.go  page.tsx        页面 /projects/{project}/stats
    store/
      layout_.tsx                   套住 /store 及下面所有页面，外面不套根 Layout，并显示它们的错误
      lang+/
        route.go  layout.tsx  page.tsx   /store/{lang} 及下面所有页面的 Layout，以及页面 /store/{lang}
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
- go 命令会跳过以 `.` 或 `_` 开头的目录和名为 `testdata` 的目录。这些目录里有 `route.go`、`page.tsx`、`layout.tsx` 或 `layout_.tsx` 时构建报错，放别的文件没问题，比如 `_components` 目录。
- 目录名还得是合法的 Go import 路径，所以 `con`、`x~1` 这类名字（Windows 保留名和短文件名）构建报错。
- 一个目录下最多一个参数目录。`items/id+` 和 `items/key+` 或 `items/path++` 并列会构建报错，因为它们读的是同一段 URL。
- 两个路由（页面或 API 路由）的 URL 有交集、又分不出谁更具体时构建报错，规则同 Go 的 `http.ServeMux`，比如 `a/b+` 和 `a+/b`。字面量比参数具体，所以 `items/new` 和 `items/id+` 并列没问题。
- 只有 Layout、没有页面的目录，它的 URL 在这个 Layout 里显示 404（见 [404](#404)），除非它下面紧挨着的 `name++` 目录里有页面，这时由那个页面以空的剩余路径接这个 URL。别的页面不能匹配这个 URL，比 Layout 路径更具体的除外：有 `settings/layout.tsx`、没有 `settings/page.tsx`，又有 `user+/page.tsx` 时构建报错，因为 `/settings` 也匹配 `/{user}`，而 `items/new` 和 `items/id+` 里的 Layout 并列没问题。
- 声明 `const Path` 会构建报错，目录就是 URL。

## 一个目录的文件

一个目录最多有三个约定文件：

| 文件 | 内容 |
| --- | --- |
| `route.go` | 这个目录的全部 Go 名字：`Layout`、`LayoutSSR`、`Load`、`SSR`、`RenderCache` 和方法函数 |
| `page.tsx` | 页面组件。有 `page.tsx` 的目录是页面 |
| `layout.tsx` | Layout 组件 |
| `layout_.tsx` | 代替 `layout.tsx` 的 Layout 组件，外面不套上层的 Layout（见[不套外层的 Layout](#不套外层的-layout)） |

- `route.go` 的这些名字只从这个文件读。写在包里别的文件，包括 `layout.go`，会构建报错。辅助函数和类型写在哪个文件都行。
- 只有 `route.go`、没有 `page.tsx` 的目录：写了方法函数就是 API 路由，写了 `Layout` 就给下面的页面提供 Layout 数据，两者可以同时有。`Load` 只能和 `page.tsx` 一起出现。
- `route.tsx` 不是约定文件。出现 `route.tsx` 时构建报错，提示改名 `page.tsx`，免得页面被悄悄当成普通组件。
- `tifa build` 还会为每个有页面、Layout 或方法函数的目录写一个 `route.gen.ts` 模块，`page.tsx`、`layout.tsx` 和 `layout_.tsx` 从它 import（见 [page.tsx](#pagetsx)）。

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
- `Layout` 返回这个目录的 Layout 的数据（见 [Layout](#layout)）。
- 目录里有 `page.tsx` 就是页面，GET 由框架渲染，这时不能再写 `GET`。不需要数据、也没有别的方法的页面可以只有 `page.tsx`，不写 `route.go`。
- 有 `route.go`、没有 `page.tsx` 的目录，写了方法函数就是 API 路由：方法函数照写的那样回答，不能写 `Load`，因为 `Load` 是给页面取数的。只写了 `Layout` 的 `route.go` 不是 API 路由，它只给下面的页面提供 Layout 数据。
- 全部是包级函数和常量。依赖直接用应用初始化好的全局变量，框架不做依赖注入。

返回错误时：

| 返回 | 结果 |
| --- | --- |
| `tifa.ErrNotFound` | 404 |
| `tifa.Redirect(url, status)` | 重定向。整页加载和导航跟着跳，浏览器里调用的方法函数也会把浏览器带过去（见下文）。`status` 不是 3xx 时 panic |
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
- 入参类型有 `Validate` 方法（必须是 `func() error`）时，它在解码之后、处理函数之前执行。返回 `nil` 就继续。返回 `tifa.Error` 或 `tifa.Fields` 时原样回答，想标出具体字段就返回 `tifa.Fields`。先往 `tifa.Fields{}` 里收集，最后写 `return fields.Err()`，没有收集到任何字段时它是 nil（空的 `tifa.Fields` 仍然是一个错误）。其他错误一律答 400，消息是 `err.Error()`，会原样给用户看到。
- 浏览器从别的网站发来的 POST、PUT、PATCH 或 DELETE，表单和 multipart 也一样，在处理函数运行之前就答 403，处理函数不用自己再检查。`server.trusted_origins` 和 `server.cross_site_paths` 可以放行指定的来源和路径（见[跨站写请求](config.zh-CN.md#跨站写请求)）。
- 请求体或查询字符串解码失败时答 400（请求体超过上限答 413，其他内容类型或没有内容类型答 415），处理函数不执行。
- 自己写响应的方法函数用 `c.Bind(&in)` 解码请求体，用 `c.BindQuery(&in)` 解码查询字符串。它们给字段取名时先看 `form` 标签，再看 `json` 标签，最后用 Go 字段名。

## Ctx

`*tifa.Ctx` 是请求和它的响应。`Load`、`Layout`、方法函数、`OnRequest` 和 `OnError` 都拿到它。

| 方法 | 作用 |
| --- | --- |
| `c.Param(name)` | 路径参数，已解码，比如目录 `order+` 用 `c.Param("order")` |
| `c.Query(name)` | 查询参数的第一个值，没有时为 `""` |
| `c.QueryValues(name)` | 查询参数的全部值，比如 `?tag=a&tag=b` 得到 `["a", "b"]`，没有时为 `nil` |
| `c.Header(name)`、`c.Cookie(name)` | 请求头、请求 cookie（没有时返回 `http.ErrNoCookie`） |
| `c.Context()`、`c.WithContext(ctx)` | 请求 context，以及带着另一个 context 的 `c` 的副本，用来存请求级的值。`c` 本身不变 |
| `c.Pattern()` | 匹配到的路由路径，比如 `/projects/{project}`，没匹配时为 `""` |
| `c.IsData()` | 是不是浏览器路由的导航数据请求，而不是整页加载 |
| `c.SetHeader(key, value)`、`c.SetCookie(cookie)` | 响应头、响应 cookie。`Load` 里也能用 |
| `c.JSON(code, v)`、`c.Text(code, s)`、`c.NoContent(code)` | 写响应。`Load` 里不能用 |
| `c.Bind(&v)`、`c.BindQuery(&v)` | 把请求体或查询字符串解码进结构体，见上文。`Load` 里不能用 |
| `c.SetBodyLimit(n)` | 改这次请求的请求体上限（默认 `server.body_limit`）。要在读请求体之前调用，通常在 `OnRequest` 里。开始读以后调用会返回错误 |
| `c.Request()`、`c.Writer()` | 原始的 `*http.Request` 和 `http.ResponseWriter`，只给方法函数和 `OnRequest` 用 |

## page.tsx

```tsx
import { page, type PageData } from './route.gen'

export default function OrderPage() {
  const order = page.useLoaderData()
  return <OrderItems />
}

export const head = ({ loaderData }: { loaderData?: PageData }) => ({
  meta: loaderData ? [{ title: loaderData.id }] : [],
})
```

- 只放界面，不取数。`./route.gen` 是生成的模块，每个目录一个，导出 `page` 和 `layout`（页面和 Layout 的 TanStack 路由，目录里有才导出）、`PageData` 和 `LayoutData`（`Load` 和 `Layout` 的返回类型）、`Params`，以及每个方法函数一个调用函数。同目录的子组件同样 import `./route.gen`。Layout 的数据来自它所在目录的 `route.gen`：`layout.tsx` 从 `'./route.gen'` import `layout`，页面读项目 Layout 的数据时从比如 `'../../route.gen'` import `layout`。有谁 import 了一个没有生成模块的目录的 `route.gen`，构建就会报错。
- 默认导出是页面组件。可选导出 `head` 和 `Error`。404 也显示在 `Error` 里（见 [404](#404)），导出 `NotFound` 构建报错。
- 在浏览器里，每个 `page.tsx`，以及根 Layout 以外的每个 `layout.tsx` 或 `layout_.tsx`，连同它 import 的一切（含 `head`）各自是一个 chunk。路由第一次用到时才加载，路由器等它加载完才显示页面、运行 `head`。页面的 HTML 预加载它的 Layout 和路由的 chunk，所以它们和入口脚本一起到。文件顶层的代码（含只为副作用的 import）在这时运行，而不是应用启动时，所以每个页面都需要的初始化放在根 Layout 里，或放在它 import 的模块里。见[浏览器里的路由模块](build.zh-CN.md#浏览器里的路由模块)。
- Go 的切片和 map 不会是 `null`：nil 切片到浏览器是 `[]`，nil map 是 `{}`，它们的 TypeScript 类型不带 `| null`（见 [JSON 规则](#json-规则)）。
- 模块顶层就要用浏览器能力的组件放进 `.client.tsx`，用 `tifa/client` 的 `clientOnly` 延迟加载。它不进 SSR 包，首屏显示占位内容。能渲染、但要等浏览器画出来才有内容的组件也适合这样放，见[只在浏览器里绘制的组件](#只在浏览器里绘制的组件)：

```tsx
import { clientOnly } from 'tifa/client'

const OrderActions = clientOnly(() => import('./actions.client'), <button disabled>正在准备操作…</button>)
```

- tifa 不会替你重新取页面数据。写操作之后，不管是处理函数还是你自己的 `fetch`，页面在你开口之前都显示原来的内容：调用 TanStack 的 `router.invalidate()`，当前页面和所有 Layout 的数据重新取一次，合并成一个请求。`await router.invalidate({ sync: true })` 在页面显示出新数据后兑现。只写 `await router.invalidate()` 会立刻兑现，页面在后台更新。见[在浏览器里调用方法处理函数](#在浏览器里调用方法函数)。
- 路由器已经持有的数据遵循 TanStack 的默认值：它立刻算作过期，所以回到访问过的页面时先显示缓存的数据，再在后台重新取一次（`GET <url>?_tifa=…`），鼠标悬停或聚焦时预加载的链接，30 秒内直接使用。

### 显示应用信任的 HTML

`tifa.HTML` 类型的字段装的是页面原样显示的标记，比如构建时从 Markdown 转成 HTML 的文章。用 `tifa/client` 的 `HTML` 组件显示它：

```go
type Post struct {
	Title string    `json:"title"`
	Body  tifa.HTML `json:"body"`
}
```

```tsx
import { HTML } from 'tifa/client'
import { page } from './route.gen'

export default function PostPage() {
  const post = page.useLoaderData()
  return <HTML value={post.body} as="article" className="prose" />
}
```

- 标记不经转义直接进页面。`tifa.HTML` 只用来装你自己写的或生成的 HTML。用户提交的 HTML 要先用清理库过滤，再转成 `tifa.HTML`。
- 在 TypeScript 里它的类型是 `HTML`，不是 `string`：要字符串的地方（比如当文本用、读 `.length`）TypeScript 会报错。只用 `<HTML>` 显示它。放进 `dangerouslySetInnerHTML` 时 TypeScript 拦不住，而服务器渲染的页面里它在那里只是一个标记。`as` 指定装标记的元素，只能是 `div`、`article`、`section`、`main`、`aside`、`header`、`footer`、`nav`、`figure`、`blockquote`、`li`、`td`、`span` 或 `p`（默认 `div`）。这些元素装的是普通内容。内容会被执行或不当作标记的元素，比如 `script`、`style`、`iframe`、`textarea`，会让过滤 HTML 失去作用，所以别的名字 TypeScript 会报错，`<HTML>` 也会抛错。其他属性都给这个元素，`children` 和 `dangerouslySetInnerHTML` 除外，展开对象带进来的也会被去掉。
- 服务器渲染的页面里每个值只带一份：在标记里。水合用的数据在它的位置放一个小标记，`<HTML>` 从页面读回标记。所以一篇文章不会发两遍。导航数据和 `SSR = false` 的页面带的是字符串。
- 所以显示页面里的值时，`<HTML value={...}>` 每次都要在服务器上渲染出来：不要等 effect、点击或只有浏览器才有的状态之后才渲染，也不要放在 `.client.tsx` 组件里。否则浏览器会抛出 `tifa: this tifa.HTML value came with the page, which carries it only in the markup the server rendered, and the server rendered no <HTML> for it here`。
- 标记里的链接和 `Link` 一样：点击指向应用页面的链接，路由直接显示那个页面，不重新加载整个文档。鼠标悬停或获得焦点 50 ms 后，或者手指一碰，就预加载它。以下情况由浏览器自己跳转：按着修饰键或用别的鼠标键点击，链接的 `target` 不是 `_self`，带 `download` 或 `rel="external"`，指向别的网站或 `mailto:` 这类协议，只是当前页面的 `#锚点`，public 文件，`/assets/` 或 `/__tifa/` 下的路径，以及没有页面的路径（比如 API 路由）。站内某个链接要整页跳转（比如同一域名下不是 tifa 的服务），就写 `rel="external"`。`<HTML>` 只管它自己元素里的链接。写在 `<HTML>` 上的 `onClick` 等处理函数先执行，其中调用了 `event.preventDefault()` 就不走路由。
- `tifa.HTML` 不能做 map 的键，不能放在方法函数的入参或会话里。那里用 `string`，过滤之后再转换。`Load` 和 `Layout` 不能直接返回一个 `tifa.HTML`，要放进结构体的字段里。

### 在浏览器里调用方法函数

有方法函数的目录，它的 `route.gen` 模块按方法各导出一个同名函数。API 路由也有 `route.gen` 模块，页面可以 import 任何目录的方法函数：

```tsx
import { POST } from './route.gen'
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
- 调用只发出请求，回答带着会话时应用会话（见下）。它不重新取任何页面的数据，成功失败都一样，任何方法都一样。想让页面显示写操作的结果，在调用之后调用 `router.invalidate()`（见下面的例子）。
- 错误回答抛出 `TifaError`，带 `status`、`message` 和 `fields`（`tifa.Fields` 的消息，其他情况为空）。网络错误照 `fetch` 的样子抛出。页面的构建比服务器旧时，调用以状态 409 失败，见[部署时还开着的页面](#部署时还开着的页面)。
- 处理函数或 `OnRequest` 返回 `tifa.Redirect(url, status)` 时，浏览器去那个 URL：本站的 URL 经路由器跳转，页面不重新加载，别的站点整页加载。新页面显示路由器已有的数据，缺的再去取，和任何一次导航一样。重定向到正在显示的页面时，页面留在原处，什么都不取。路径、hash 和查询参数的值都相同就算同一页，查询参数的顺序和编码不论。promise 在导航结束后兑现为 `undefined`，有返回值的处理函数也一样。一次调用是否被重定向，要等回答来了才知道（`OnRequest` 可以重定向任何请求），所以有返回值的处理函数，返回类型都带 `undefined`。自己写响应的处理函数，调用函数拿到这次重定向的 `Response`（`ok` 为真，状态码在 `X-Tifa-Redirect` 里）。相对 URL 按处理函数的 URL 解析，不按页面的 URL。处理函数经 `c.Writer()` 自己写的 3xx，或者代理写的 3xx，浏览器读不到，promise 以 `Error` 失败。
- 让某人登录或登出的调用，回答里带着新的会话，浏览器里的会话副本随之更新（见[会话在浏览器里](#在浏览器里)）。
- 在事件处理函数或 effect 里调用。服务器渲染页面期间调用会抛错。
- `route.gen` 还为有入参或返回值的处理函数导出 `POSTInput`、`POSTOutput` 等类型。

`tifa.Redirect` 返回的错误是 `*tifa.RedirectError`，字段有 `URL` 和 `Status`。在 `OnRequest` 或测试里用 `errors.As` 认出它。

要在创建之后去新建内容的页面，可以让处理函数返回重定向，也可以返回结果再自己导航。调用不重新取数据，所以离开前当前页面不会再取一次。写操作的结果要在当前页面上看到的话，用 `router.invalidate()` 来要：

```go
func POST(c *tifa.Ctx, in AddInput) error {
	todo, err := AddTodo(in.Title)
	if err != nil {
		return err
	}
	return tifa.Redirect("/todos/"+todo.ID, http.StatusSeeOther)
}
```

```tsx
const router = useRouter() // 来自 @tanstack/react-router
// 在事件处理函数里，这里的 POST 返回 (Todo, error)：
const todo = await POST({ input: { title } })
if (todo) await router.navigate({ to: '/todos/$todo', params: { todo: todo.id } }) // undefined 表示已被重定向
```

## Layout

Layout 放在它的目录里，和这个目录的页面（如果有）并列：`route.go` 里的 `Layout` 函数和 `layout.tsx` 套到同目录的页面和下面所有页面。`layout.tsx` 用 `<Outlet />` 放子路由。两者都可以省掉一个：没有 `layout.tsx` 时子路由直接渲染，没有 `Layout` 函数时 Layout 没有数据。所在目录和下面都没有页面的 Layout 构建报错。

```go
package project

func Layout(c *tifa.Ctx) (projects.Project, error) {
	return projects.GetProject(c.Param("project"))
}

const LayoutSSR = false // 可选，默认 true
```

- `Layout` 和 `LayoutSSR` 写在 `route.go`，和同一个目录的 `Load`、`SSR`、`RenderCache` 和方法函数放在一起。写在包里其他文件，比如 `layout.go`，会构建报错。只写了 `Layout` 的 `route.go` 不是 API 路由。
- Layout 用 `c.Param` 只能读自己目录路径里的参数。`app/projects/project+/route.go` 里的 `Layout` 能读 `project`，不能读 `order`。页面的 `Load` 同样只能读自己路径里的参数。
- 同一个 Layout 下切换页面时，Layout 组件不重新挂载，它的状态保留。`Layout` 函数读的参数变了（比如换了一个项目），Layout 重新挂载，状态重置。它不读的参数变了，状态保留，页面同理。
- 页面和 Layout 都可以用 `tifa.Use(c, project.Layout)` 拿到套着自己的 Layout（同目录或祖先目录的，中间的 `layout_.tsx` 去掉的除外）本次请求的结果，同一个 `Layout` 每个请求只跑一次。引用别的 Layout 构建报错，传入的不是包级 `Layout` 函数（比如闭包）也报错。`Layout` 函数不能引用自己所在目录的 Layout，直接引用或经辅助函数引用都不行。
- 浏览器里跳转时，导航数据请求只执行所列路由的 `Load` 和 `Layout` 函数。祖先 Layout 的数据还在缓存里时，它不在列表中，它的 `Layout` 这次不会再跑。所以权限检查不要只写在 Layout 里：放进 `OnRequest`，或者让页面的 `Load` 用 `tifa.Use` 依赖做检查的那个 Layout，这样每次请求都会执行它。
- 文档外壳（`<html>`、`<head>`、资源与注入数据的脚本）由框架写成字符串，React 只渲染 `<body>` 里的应用根节点，所以任何一层出错或 404 时页面仍是完整文档。根 Layout（`app/layout.tsx`）可以导出两个常量定制外壳：`htmlAttributes` 是每个页面 `<html>` 的初始属性（不导出时为 `{ lang: 'en' }`，路由的 `head` 可以为自己的页面改它们，见下文），`documentHead` 是每个页面都带的 head 标签（没有路由匹配、根 `Layout` 失败时也带），格式同 `head` 的返回值。在这里放网站的默认 `title`，没有路由匹配的 404 页面也有标题，页面自己的 `head` 返回 `title` 时会覆盖它。导出 `Document` 组件会构建报错。

```tsx
export const htmlAttributes = { lang: 'zh-CN' }
export const documentHead = {
  meta: [{ title: '山海工作台' }, { name: 'theme-color', content: '#f2f4f5' }],
  links: [{ rel: 'icon', href: '/favicon.svg' }],
}
```

- 页面的 head 由匹配到的路由的 `head` 返回值决定：`title` 和同名的 meta 取最深的路由，link、style、script 按路由顺序去重。框架默认带 `<meta charset="utf-8">` 和 viewport meta，路由的 `name: 'viewport'` meta 会替换后者。标签的属性按 React DOM 在服务器上的规则写：`className`、`htmlFor`、`httpEquiv`、`crossOrigin` 等改成 HTML 的名字，`on` 开头的事件属性、非法的属性名、`null`、`undefined`、函数不写，`async`、`defer` 这类布尔属性为真时写成空值，其他属性的布尔值只在 `data-`、`aria-` 属性上写成字符串，`javascript:` 链接被替换。head 标签不支持 `style` 属性，写了会让渲染出错。`htmlAttributes` 按同样的规则写。style、script 的内容原样写入，只有其中会结束元素的 `<script`、`</script`（style 里是 `<style`、`</style`）按 React DOM 的方式转义。浏览器里跳转、`router.invalidate()` 之后 head 随新的匹配更新。
- `head` 还可以返回 `htmlAttributes`，即这些页面 `<html>` 的属性，比如多语言网站每个页面的语言。搜索引擎、读屏软件和浏览器的翻译都从 `lang` 读语言。它们逐个属性合并到根 Layout 的 `htmlAttributes` 上：同一个属性取给出它的最深的路由，`undefined` 交给上层决定，`null` 去掉这个属性。属性按 React DOM 写出的名字对应，所以一个路由的 `className` 和另一个路由的 `class` 是同一个属性。`class` 按整个属性替换，不和别处的值合并。`style` 和 head 标签一样不支持。写法规则和 head 标签相同。浏览器里跳转、`router.invalidate()` 之后它们也随新的匹配更新。在浏览器里，tifa 只在它要给 `<html>` 某个属性的值变了时才写这个属性，也只去掉自己写过的属性。所以页面脚本设的值（比如暗色模式脚本加的 `dark` class，翻译插件改的 `lang`）在 hydration 和跳转之后都还在，只要路由要的这个属性的值没变。这个值变了时，比如某个路由设了另一个 `class`，或者跳出这个路由后根 Layout 的 `class` 又回来，tifa 就会写上新值，脚本加的 class 也就没了。这类主题放在没有路由设的属性上（比如 `data-theme`），或者让脚本再加一次。不走 SSR 的页面在浏览器里得到这些属性。

```tsx
// app/guide/lang+/layout.tsx。route.go 里的 Layout 检查 URL 里的语言并返回它，
// 网站没有的语言返回 tifa.ErrNotFound。
export const head = ({ loaderData }: { loaderData?: LayoutData }) => ({
  htmlAttributes: { lang: loaderData?.lang },
})
```

- 算数的路由就是 head 出现在页面上的那些。Layout 下面的 404 或出错保留 Layout 的语言，Layout 下面没有页面的 URL 也一样，因为它的 404 显示在这个 Layout 里（见 [404](#404)）。Layout 自己失败时，它的 `head` 拿不到数据，上面的例子就留下根 Layout 的 `lang`。这类网站的其他部分见[多语言网站](#多语言网站)。
- `htmlAttributes` 必须是对象，别的值 `pnpm typecheck` 会报出来。运行时这样的值和 `head` 抛错一样处理：服务器渲染按意外错误失败（交给 `OnError`，没有就是 500），消息写进日志，`tifa dev` 下也显示在页面上。浏览器里跳转后、或者不走 SSR 的页面上，路由器把错误打到控制台，并丢掉这个路由的整个 head。
- 语言像上面那样从 Layout 的数据取，不要直接取 `params`：Go 函数会检查它，别人在 URL 里随便写的字不会变成页面的语言。

### 不套外层的 Layout

组件文件起名 `layout_.tsx` 代替 `layout.tsx`，这个目录的 Layout 显示它的页面时外面不套任何上层 Layout，根 Layout 也不套，只留框架写的文档外壳。[examples/shop](../examples/shop) 的店面就是这样：它画自己的页头页脚，根 Layout 给其他页面画的工作台一点都不带。

```text
app/
  route.go  layout.tsx                工作台：侧栏、顶栏、店铺列表
  projects/
    project+/
      route.go  layout.tsx  page.tsx  在工作台里
  store/
    layout_.tsx                       店面从这里开始，不套工作台
    lang+/
      route.go  layout.tsx  page.tsx  某一种语言的店面
      cart/
        route.go  page.tsx            在店面里
```

- 它下面的页面，包括更深的，都不渲染它上面的 Layout，那些 Layout 的 `Layout` 函数也不为它们执行：整页渲染、浏览器导航的数据请求、它下面没有页面的 URL 的 404，都一样。所以那些数据不会出现在这些页面里。
- 同一个目录 `route.go` 里的 `Layout` 照常给它数据。它下面的目录照常一层套一层：更深处的 `layout.tsx` 套在它里面，它的 `Layout` 照常执行。更深处再有 `layout_.tsx`，就连它和它上面的一起去掉。
- 属于文档的照常生效：根 Layout 的 `htmlAttributes` 和 `documentHead`，还有 `OnRequest`。属于上层 Layout 的不生效：它们的 `head`、`Error` 和 `LayoutSSR`。`layout_.tsx` 这一层的 `Layout` 失败或返回 `tifa.ErrNotFound` 时，上面没有 Layout 能显示这个错误，于是显示框架默认页面。它自己的 `Error` 照常显示它下面的失败，和别的 Layout 一样。想在这个 `Layout` 失败时显示自己的页面，就把 `layout_.tsx` 往上放一层，只渲染 `<Outlet />` 并导出 `Error`，shop 的 `app/store` 就是这样。
- 浏览器里在它下面的页面和外面的页面之间跳转，整个外框跟着换。新页面的 Layout 数据路由器里还没有时才去取。
- 这些情况构建报错：`app/` 根目录放 `layout_.tsx`（根上面本来就没有 Layout），同一个目录同时有 `layout.tsx` 和 `layout_.tsx`，它下面的页面或 Layout 用 `tifa.Use` 引用被它去掉的 Layout。一个 Layout 的页面全在某个 `layout_.tsx` 下面时也报错，因为它一个页面都没套上。
- React Router 和 TanStack Router 也用名字结尾的 `_` 让路由不套进某个 Layout，但只跳过前一段的那个 Layout，根路由和更上层的 Layout 照样套着它。tifa 的 `layout_.tsx` 去掉它上面所有的 Layout，根 Layout 也去掉。

## 404

404 显示在路由的 `Error` 里（见[页面上的错误](#页面上的错误)），它收到 `status` 为 404 的 `RouteError`：

```tsx
import { RouteError } from 'tifa/client'

export function Error({ error }: { error: unknown }) {
  if (error instanceof RouteError && error.status === 404) return <p>没有这个订单</p>
  return <p>出了点问题</p>
}
```

| 情况 | 显示 |
| --- | --- |
| 页面 `Load` 返回 `tifa.ErrNotFound` | 从这个页面往上第一个 `Error`，外层 Layout 照常显示 |
| `Layout` 函数返回 `tifa.ErrNotFound` | 从上一层 Layout 往上第一个 `Error` |
| URL 没有匹配任何页面 | 从 URL 能匹配上的最深一层 Layout 往上第一个 `Error`，显示在这个 Layout 里面。从最外层到这一层的 Layout 照常执行、照常显示：从根开始，或者从它上面的 `layout_.tsx` 开始 |
| 没有路由导出 `Error` | 框架默认的 404 页面 |

这几种情况的状态码都是 404，浏览器里跳转后也一样，这时的数据请求回答各层 Layout 的数据和状态码 404。

找没有匹配页面的 URL 落在哪个 Layout 时，tifa 从左往右逐段拿 URL 和每个 Layout、每个页面的路径比：字面段胜过参数，参数胜过路径结束。匹配得最多的路径胜出，页面算作它所在的 Layout。比如有这些目录：

```text
app/layout.tsx
app/teams/team+/layout.tsx
app/teams/team+/members/page.tsx
app/teams/new/page.tsx
```

| URL | 404 显示在 |
| --- | --- |
| `/nowhere` | 根 Layout 里 |
| `/teams/blue/missing` | team Layout 里，它的 `Layout` 读到的 `c.Param("team")` 是 `blue` |
| `/teams/blue/members/x` | team Layout 里 |
| `/teams/new/x` | 根 Layout 里：`new` 是字面段，`teams/new` 胜过 `teams/team+`，而这个页面在根 Layout 里 |

- `name++` 目录里的页面接住它下面的所有 URL，所以这类 URL 匹配的是那个页面，回答什么由它的 `Load` 决定。
- 没有匹配页面的 URL 也会经过 `OnRequest`，这时 `c.Pattern()` 为空。`next(c)` 执行各层 Layout，返回 `tifa.ErrNotFound`。`OnRequest` 不调用 `next`、自己返回 `tifa.ErrNotFound` 时，任何 Layout 都不执行，页面显示得和这个 URL 最外层的 `Layout` 返回 `tifa.ErrNotFound` 时一样：根 Layout，或者它上面那个 `layout_.tsx` 的 Layout。
- Layout 的 `Error` 只会显示在它下面某个路由的位置，也就是这个 Layout 的 `Outlet` 里，所以它能读这个 Layout 的数据和它的组件提供的 context。Layout 失败时显示上一层 Layout 的 `Error`，根 Layout 或 `layout_.tsx` 的 Layout 失败时显示框架默认页面。
- `page.tsx` 或 `layout.tsx` 导出 `NotFound` 时构建报错。把它显示的内容挪进 `Error`，放在 `error.status === 404` 的分支里。

## 页面上的错误

`page.tsx` 和 `layout.tsx` 可以导出 `Error`。这一层的 `Load` 或 `Layout` 失败、404 显示在这一层（见 [404](#404)），或者它下面的组件抛错时，在这一层的位置显示它：

```tsx
import { RouteError } from 'tifa/client'

export function Error({ error }: { error: unknown }) {
  if (error instanceof RouteError && error.status === 403) return <p>没有权限</p>
  return <p>出了点问题</p>
}
```

- `Load` 或 `Layout` 失败时，`Error` 收到 `RouteError`，带 `status` 和公开的消息（`tifa.Error` 的消息，或 `OnError` 返回的）。服务器上和 hydration 之后都一样。
- 自己没导出 `Error` 的路由，用最近的导出了 `Error` 的祖先的，都没有就用框架默认的。祖先找到 `layout_.tsx` 为止。
- 页面的状态码取最外层失败的那一层。
- 服务器渲染页面时组件抛错，算意外错误：`OnError` 只看到它一次，由它决定状态码（没定义 `OnError` 时是 500）和公开的消息。接着页面带着这个错误再渲染一次，错误以 `RouteError` 放在页面的位置，于是从页面往上第一个 `Error` 在各层 Layout 里面显示它。这次渲染如果也失败（`Error` 或包着它的 Layout 抛错），错误就往上挪一层，放到页面所在 Layout 的位置，这样一直挪到最外层的 Layout，即根 Layout 或 `layout_.tsx`。错误放在那一层的位置还失败时，浏览器才拿到 Go 写的简单错误页，只有状态码和公开的消息。浏览器按服务器放错误的位置 hydrate，不再运行抛错的那个路由。这种回答不进缓存。错误栈只在 `tifa dev` 下显示，盖在显示 `Error` 的页面上，保存修复后页面自动刷新（见 [tifa dev](dev.zh-CN.md#页面渲染失败)）。
- 服务器渲染用完 `ssr.vm_memory` 或超过 `ssr.timeout`（见 [config.zh-CN.md](config.zh-CN.md)）时当场停下。你的代码和库里的 `catch`、`finally`、promise 处理函数都不会为它运行，所以页面接不住它，也没法改成显示别的内容。VM 换成新的。用完 `ssr.vm_memory` 的渲染照上面那样在新的 VM 上带着错误再渲染。超过 `ssr.timeout` 或者遇上所有 VM 都忙的渲染不再重来，Go 立刻回答简单错误页。同一个请求的几次渲染共用一个 `ssr.timeout`，从第一次开始算，带着错误的渲染用完了它就不再往上试。无限递归的代码得到 `RangeError: Maximum call stack size exceeded`，和 Node 一样，`catch` 能接住（见[栈深度](config.zh-CN.md#栈深度)）。没有代码接住时，它和其他错误一样让渲染失败。
- 显示错误的页面，head 和 `Load` 失败的页面一样。被错误顶替的那个路由在没有数据的情况下运行它的 `head`，它下面的路由不再添加标签，它上面每个路由的 `head` 会在现在的 matches 上重新运行一遍，所以 `status` 能告诉它有路由失败了（见[多语言网站](#多语言网站)）。路由为没渲染出来的页面给出的标签和 `htmlAttributes` 不会出现在错误页上。浏览器 hydrate 时算出的 head 也是同一份。
- 浏览器接管之后组件抛错，显示最近的 `Error`，和 `Load` 失败一样。
- 无论哪里失败，文档外壳都是完整的，页面照样加载脚本和样式。

## 多语言网站

tifa 没有语言设置：语言是 URL 的一部分，由应用在 Go 里检查。[examples/shop](../examples/shop) 在 `/store` 下的店面就是这样写的。

- 把语言放进 URL 开头的参数目录，比如 `app/store/lang+`。每个 URL 都写明语言，默认语言也写：`/store/zh-CN/products` 和 `/store/en-US/products`。
- 这个目录的 `Layout` 检查语言，把它放进返回的数据。网站没有的语言返回 `tifa.ErrNotFound`，显示上一层 Layout 的 404，用上一层的语言：

```go
// app/store/lang+/route.go
func Layout(c *tifa.Ctx) (Shell, error) {
	l, err := store.FindLocale(c.Param("lang")) // 店面没有的语言返回 tifa.ErrNotFound
	if err != nil {
		return Shell{}, err
	}
	return Shell{Locale: l, Menu: products.Menu(l)}, nil
}
```

- shop 在上一层的 `app/store` 放了 `layout_.tsx`，店面外面就不套工作台（见[不套外层的 Layout](#不套外层的-layout)）。它只渲染 `<Outlet />`，它的 `Error` 用中英两种语言显示店面没有的语言的 404，并分别链接到两种语言的店面，因为这时不知道访客要哪种语言。
- 页面和组件从这份数据取语言，经 `layout.useLoaderData()` 或 Layout 组件提供的 context，不从 `params` 取：Go 函数检查过它，别人在 URL 里随便写的字不会变成页面的语言。Layout 下面的 `Load` 也这样检查，或者用 `tifa.Use` 读 Layout 的结果。
- Layout 的 `head` 在 `<html>` 上写 `lang`，再链接到同一页的各语言版本，供搜索引擎读：每种语言一个，包括页面自己的，再加一个 `x-default`，给哪种语言都不要的浏览器，这里用默认语言。搜索引擎要完整的 URL。`matches` 是页面的各层路由，页面在最后。其中任何一层的 `status` 不是 `success`（`Load` 失败，或组件在服务器渲染页面时抛错），或带 `_notFound` 标记（这个 URL 没有页面，由匹配到的最深 Layout 显示 404）时，页面显示的是错误，head 就不写这些链接：回 404 的 URL 不该让搜索引擎指向它，不论哪种语言。`lang` 照旧写。head 是这样写的：

```tsx
// app/store/lang+/layout.tsx
const site = 'https://shop.example.com' // 店面的网址
const languages = ['zh-CN', 'en-US'] // 店面的语言，默认语言在前

export const head = ({ loaderData, matches }: { loaderData?: LayoutData; matches: Array<{ pathname: string; status: string; _notFound?: unknown }> }) => {
  if (!loaderData) return {}
  const { code } = loaderData.locale
  const common = { htmlAttributes: { lang: code } }
  if (matches.some((match) => match.status !== 'success' || match._notFound)) return common
  const page = matches[matches.length - 1].pathname.replace(/\/$/, '').slice(`/store/${code}`.length)
  const url = (lang: string) => `${site}/store/${lang}${page}`
  return {
    ...common,
    links: [
      ...languages.map((lang) => ({ rel: 'alternate', hrefLang: lang, href: url(lang) })),
      { rel: 'alternate', hrefLang: 'x-default', href: url(languages[0]) },
    ],
  }
}
```

- 语言目录下没有页面的 URL，在语言 Layout 里显示 404，用 URL 里的语言（见 [404](#404)），不需要接住剩余路径的页面。
- 不带语言的 URL（比如 `/store`）用一个方法函数把访客送走：送到他以前选过的语言（他选的时候应用写进 cookie），没有就送到和 `Accept-Language` 最接近的语言。匹配用 `golang.org/x/text/language` 的 `Matcher`，它知道 `en-GB` 离 `en-US` 比离 `zh-CN` 近。回答随请求而变，所以用 302 跳转并加上 `Vary`：

```go
// app/store/route.go
var codes = []string{"zh-CN", "en-US"} // 默认语言在前

var matcher = language.NewMatcher([]language.Tag{language.MustParse(codes[0]), language.MustParse(codes[1])})

func GET(c *tifa.Ctx) error {
	c.Writer().Header().Add("Vary", "Cookie, Accept-Language")
	if picked, err := c.Cookie("lang"); err == nil && slices.Contains(codes, picked.Value) {
		return tifa.Redirect("/store/"+picked.Value, http.StatusFound) // 访客自己选过的语言
	}
	tags, _, _ := language.ParseAcceptLanguage(c.Header("Accept-Language"))
	_, i, confidence := matcher.Match(tags...)
	if confidence == language.No {
		i = 0 // 没有接近的语言：用默认语言
	}
	return tifa.Redirect("/store/"+codes[i], http.StatusFound)
}
```

- 翻译同步加载：把每种语言的文案和应用打包在一起，库的初始化不等待，比如 i18next 用 `initAsync: false` 加 `resources`。服务端渲染什么都不等，也不读文件，后到的文案不会出现在 HTML 里，浏览器 hydrate 时就会是另一套文字。两边都从 Layout 的数据取语言，所以显示同样的文字。数字和日期也用数据里的语言和时区格式化（见[怎么写才能水合一致](#怎么写才能水合一致)）。

## SSR 与缓存

```go
const SSR = false              // 页面，默认 true
const RenderCache = false      // 页面，关掉渲染缓存，默认开
const Cache = 10 * time.Minute // 页面，10 分钟内所有人拿到同一份回答，默认关
const LayoutSSR = false        // 本目录的 Layout，默认 true
```

`SSR`、`RenderCache` 和 `Cache` 只作用于 `page.tsx` 的页面。没有 `page.tsx` 的 `route.go` 里写了它们，构建会报错。`RenderCache` 只能写 `false`：渲染缓存默认就是开的，不用写 `true`。`Cache` 是至少一秒的 `time.Duration` 常量，见[整页缓存](#整页缓存)。名字要照上面的大小写写，包级常量叫 `ssr`、`cache`、`rendercache` 或 `layoutssr` 会构建报错，因为它们会被悄悄跳过。`Cache = true` 是 `Cache` 还是渲染缓存开关时的写法，现在也会构建报错，删掉即可。

- `SSR = false`：中间件和所有 `Load` 照常执行，但跳过 QuickJS，响应是带数据的最小文档，由浏览器渲染。`LayoutSSR = false` 作用于该 Layout 包着的所有页面。
- 所有页面都开着渲染缓存。每个请求照样跑自己的中间件、`Load` 和 Layout，然后把渲染出的 HTML 按构建版本、SSR 包、URL、每个 `Load` 和 Layout 的数据、浏览器拿到的会话存起来，缓存大小是 `cache.size`（见 [config.zh-CN.md](config.zh-CN.md)）。这些都没变时，不渲染直接返回 HTML。响应头 `X-Tifa-Render` 说明是哪种：`vm` 是这个请求自己渲染，`cache` 是命中，`shared` 是共用了别的请求发起的渲染（见[页面请求](build.zh-CN.md#页面请求)）。`cache.size` 为 0 时缓存关闭，共用渲染照旧。
- 组件不能把每次渲染都不同的值写进 HTML，比如 `Date.now()` 或 `Math.random()`。hydration 也要求这样，而且缓存会把这样的值固定下来给之后的访客。`tifa dev` 下每个页面再渲染一次，第二次把时钟往后拨 36 小时，两次不同时终端和 dev 浮层会提示，给出第一处不同的位置，并列出你的代码里读时间或随机数的位置，这个页面在改好之前不进缓存。如果页面就是要显示这样的值，比如服务器时间，就在它的 `route.go` 里写 `const RenderCache = false`，这个页面每个请求都渲染。这个检查在构建出的程序里不做。
- 带渲染时钟（`tifa.WithRenderClock`）的请求跳过缓存：不读也不存，因为它的 HTML 取决于键里没有的时间。
- 多个请求同时没命中缓存，并且构建版本、URL 和数据都相同时（比如刚部署完涌进来一批访客），它们共用一次渲染。每个请求照样跑自己的中间件和 `Load`，保留它们设的响应头（比如 `Set-Cookie` 和 `Vary`），拿到自己接受的编码。`cache.size` 为 0、页面大到缓存放不下、在 `tifa dev` 下也都这样。数据或会话不同的请求各自渲染，写了 `RenderCache = false` 的页面每个请求都渲染。
- 共用的那次渲染失败时，等它的每个请求都拿到同一个错误并交给 `OnError`，panic 也一样。失败的结果不缓存，下一个请求重新渲染。客户端断开的请求立刻停止等待，连接被中断，不发响应：`OnRequest` 里 `next(c)` 返回一个匹配 `context.Canceled` 的错误，但不调 `OnError`，也不打日志。渲染继续给其他请求用。请求自己的 deadline 在等待时到了，和别的错误一样处理。没有请求再等时，还在等 VM 的渲染就此放弃，已经在跑的跑完放进缓存。这时如果渲染失败，没有请求把它交给 `OnError`，所以错误写进日志。
- 页面响应都带 ETag，它是和缓存键同样内容的哈希。这些都没变的第二次请求带 `If-None-Match` 时答 304，不渲染，也不查缓存。ETag 和缓存是两件事：ETag 省下已经有这个页面的浏览器的传输，缓存省下其他人的渲染。写了 `RenderCache = false` 的页面和带渲染时钟的请求没有 ETag，因为它们的 HTML 不由这些内容决定，总是重新渲染。
- 页面里不带数据加载的时间，同样的数据总是得到同样的 HTML。页面的响应（渲染、304、应用渲染的 404 页面）改在 `Server-Timing: tifa-age;dur=<毫秒>` 里说明数据可能有多旧，从第一个 `Load` 开始时算起。只有任何缓存都不能不问服务器就重用的响应才带它：`Cache-Control` 里有 `no-cache` 或 `no-store`，默认的 `private, no-cache` 就是。整页缓存给的页面，以及你把 `Cache-Control` 设成允许缓存存下来的页面（比如 `public, max-age=60`），都不带它，因为缓存过一阵会把同一个时长原样发出去。浏览器的路由器把浏览器发出请求的时间减去这个时长，当作数据加载的时间，`staleTime` 和 `gcTime` 从这里算。它在第一次加载或预加载之前才把这个时间写进页面的各个 match，在那之前 `updatedAt` 是 0，和服务器渲染的一样，显示它的组件水合时不会对不上。响应不带它或浏览器读不到它时（Firefox 走普通 HTTP，或者从前进后退历史打开的页面），数据按旧数据处理，回到这个页面时重新加载。导航数据不带它：路由器自己取的数据由它自己记时间。
- 应用的每个回答（页面、304、404 页面、导航数据、方法处理函数、重定向）默认带 `Cache-Control: private, no-cache`，你自己设过 `Cache-Control` 的除外。开了 `Cache` 的页面里被存下来的回答是例外，见[整页缓存](#整页缓存)。CDN 和代理不会存它，因为页面可能只属于某一位访客。浏览器可以存下来，下次带着 ETag 回来问，页面没变就答 304。要改的话，在 `OnRequest`、`Load`、`Layout` 或方法处理函数里用 `c.SetHeader("Cache-Control", "...")`，你设的值原样发出，只有导航数据（带 `_tifa` 的请求）例外，它永远不是 public：不是 `no-store`、也不是同时有 `private` 和 `no-cache` 的值会换成 `private, no-cache`。JSON 错误（包括 405 和关机时的 503）、Go 写的纯错误页、渲染失败后带错误重新渲染的页面和过载回答都是 `no-store`，你在出错之前设过 `Cache-Control` 的除外。应用渲染的 404 页面仍用默认值。`/assets/` 下的文件保持长期缓存，`public/` 里的文件保持 `no-cache`（见[页面请求](build.zh-CN.md#页面请求)）。渲染缓存只在进程内存渲染结果，不改这个头。

### 整页缓存

```go
// app/blog/slug+/route.go
const Cache = 10 * time.Minute
```

开了 `Cache` 的页面，在这段时间里所有人拿到同一份回答，直接从内存给出，不跑 `Load`，也不渲染。适合公开、很多人看的页面：文章、文档、商品页和落地页。访客看到改动最多晚 `Cache` 这么久。

- **所有人都是匿名的。** 页面的 GET 和 HEAD，以及浏览器导航到它时为取数据发的请求，在进 `OnRequest` 之前就被去掉除一份短名单以外的所有请求头：`Host`、`Accept-Encoding`、`If-None-Match`、`If-Modified-Since`、`Cache-Control`、`Pragma`、`Content-Length`、`Connection`、`Origin` 和 `Sec-Fetch-*`。`Cookie` 和 `Authorization` 会去掉，`Accept-Language`、`User-Agent`、`Referer`、`Forwarded`、`X-Forwarded-*` 以及代理或登录网关设的 `X-User-ID` 这类头也一样，所以它们没法经 `OnRequest` 和 context 传到 `Load`。`tifa.Session` 返回 nil，`c.Cookie` 找不到东西，`c.Header` 和 `c.Request().Header` 里只有名单上的头，`c.Request()` 拿到的也是这份请求。`tifa.SetSession` 和 `tifa.ClearSession` 返回错误，信息说登录和 OAuth 回调要放在不开 `Cache` 的路由上。页面要共用，说的内容就不能取决于是谁在问。这类页面 HTML 上的 `Set-Cookie` 会被去掉，因为共享回答里的 cookie 会发给每一位访客。导航请求（带 `_tifa` 的）的回答只给一个浏览器，保留 `Set-Cookie`，并带上这个浏览器的会话（见[会话](#在浏览器里)）。同一个 URL 上的方法处理函数，比如 POST，不匿名，也不缓存。
- **匿名的边界。** tifa 保证的是请求头：在开了 `Cache` 的页面上，`OnRequest`、`Load` 和 `Layout` 看到的请求只有名单上的头，`tifa.Session` 是 nil。用 `tifa.SetHTTPHandler` 挂的 `net/http` 中间件在 tifa 外面运行，拿到的是原来的请求，它放进请求 context 的值会原样到达 `OnRequest` 和 `Load`。不要在那里认人，认人放在不开 `Cache` 的路由的 `OnRequest` 里。连接地址和 TLS 信息也仍然保留，外层中间件放进 context 的值和白名单里的头（例如 `Origin`）也能区分请求，这些都不得用来生成个性化的共享内容。
- **构建会检查。** 这类页面的 `Load`、外层 `Layout` 的 `Layout` 函数，或它们调用的函数读了 `c.Cookie`、`c.Header` 或 `tifa.Session`，构建就失败并指出那一行，因为这样的读取只会看到匿名请求。`c.Header` 的报错会写出有哪几个头。需要知道谁登录了的页面不要用 `Cache`。取决于访客的内容放到浏览器里，页面加载后由浏览器去读（见[在浏览器里](#在浏览器里)）。开了 `Cache` 的页面外面那几层 Layout，只放对所有人都一样的数据。
- **`OnRequest` 能做什么。** 每个请求都跑，命中的也跑，看到的是匿名请求。适合按路径做的事：重定向、安全头、日志、计数、按地址限流。它看不到谁登录了，所以按登录状态重定向的 `OnRequest` 要用在不开 `Cache` 的路由上，在没看到 cookie 时就发一个 cookie 的中间件，会在每次导航时都发。它在 HTML 上设的 cookie 会被去掉。
- **多语言。** 多语言页面按 URL 缓存，每个语言目录一份条目。按 cookie 或 `Accept-Language` 选语言再跳转的入口，是不开 `Cache` 的路由上的 GET 处理函数（开了 `Cache` 的页面两样都读不到），它跳去的页面把语言写在 URL 里（见[多语言网站](#多语言网站)）。
- **键**是协议、`Host` 请求头（不信 `X-Forwarded-Host`）、路径，以及去掉跟踪参数后的查询串，其余参数按名字排序。`cache.ignore_query` 列出跟踪参数（`utm_*`、`gclid`、`fbclid` 等，见 [config.zh-CN.md](config.zh-CN.md)）。页面链上有 `Load` 读的参数一律保留。页面用同样处理过的 URL 渲染，所以访客看到的内容不会取决于键忽略掉的参数。
- **存什么。** GET 和 HEAD 共用一份。只存状态码 200、301、302 和 308 的回答。以下情况这次不存，并且这个 URL 的请求两分钟内跳过缓存，免得排队：`Cache-Control` 里有 `private`、`no-store` 或 `no-cache`（由 `OnRequest` 或 `Load` 设的），`Vary` 里有 `Accept-Encoding` 以外的值，以及其他状态码，404 也算。请求里的强制刷新（`Cache-Control: no-cache` 或 `Pragma`）绕不过缓存。
- **时间到了**以后谁都拿不到旧页。下一个请求重新生成，同时到的请求等它。数据没变时渲染缓存直接命中，不用重新渲染。
- **内容条目到点发布时**，这一刻之前做好的页面全部算作到时，所以文章列表马上就有新的一篇（见[草稿和日期](content.zh-CN.md#草稿和日期)）。
- **重新生成失败时**，比如 `Load` 返回错误、渲染失败或超时、所有 VM 都忙，就给旧页，最多到过期后一天。再往后照实返回错误。`Load` 返回 `tifa.ErrNotFound`、重定向或 500 以下的其他 `tifa.Error`，是业务上的回答而不是失败，会取代旧页。
- **响应头。** `Load` 和 `Layout` 设的响应头（除了 tifa 自己写的 `Set-Cookie`、`Cache-Control`、`Vary`、`Age`、`ETag` 和 `Content-Length`）随条目存下，用这个条目答的每个回答都带上。`Load` 或 `Layout` 设了 `private`、`no-store` 或 `no-cache` 的 `Cache-Control`，或 `Accept-Encoding` 以外的 `Vary`，这个页面就不进缓存，这些头会随回答发出，盖过 `OnRequest` 设的 `public`。存下来的页面带 `Cache-Control: public, max-age=0, s-maxage=<Cache 的秒数>, stale-if-error=86400` 和 `Age`。浏览器每次都来问，ETag 匹配时从条目回 304，所以部署后马上就能看到新版。`Age` 是页面生成后过了几秒，前面的 CDN 会用 `s-maxage` 减去它，两层加起来不会超过 `Cache`。`Cache-Status: tifa; hit`、`tifa; fwd=miss` 或 `tifa; fwd=stale` 说明回答从哪来，`X-Tifa-Render` 照旧。导航数据仍是 `private, no-cache`。CDN 里要怎么设，见 tifa skill 的部署说明。
- 条目和渲染缓存共用 `cache.size`。`cache.size` 为 0 时什么都不存，同时没命中的请求仍然共用一次生成。

### 用户无关性页面与私有数据

一个完整的页面 = 用户无关性页面 + 私有数据。tifa 按数据的种类来分，不按组件分：

- **对所有人相同的数据。** `Load` 和 Layout 看不到用户，所以页面可以放进整页缓存和 CDN。
- **会话。** 谁登录了，以及会话里很小的 `Exposed` 部分，由会话系统携带（见[会话](#会话)）。它会写进页面，浏览器也保存一份副本。
- **其他私有数据。** 购物车、推荐、未读数。量可以很大，浏览器还可能写它。把它放在应用自己的 API 路由后面，由浏览器去取。

别的系统也是这样分的：HTTP 缓存叫 `public` 和 `private`，Magento 叫 private content，donut caching 则是共用的页面保持完整，再把空洞填上。

第三种数据的做法：写一个 API 路由（有 `GET` 处理函数、没有 `page.tsx` 的 `route.go`）。它生成的 `GET` 函数有类型，可以直接在浏览器里调用。什么时候调由页面决定：页面加载后、面板打开时，或者鼠标悬停时提前取。如果应用想要缓存，生成的函数可以直接接进 TanStack Query。`examples/shop/app/store/mine.ts` 是一个完整的例子：店面页面有缓存、对所有人相同，这个模块在页面加载后调用 `/store/{lang}/mine` 的 `GET`，填上购物车和访客最近看过的商品，写入之后再调用一次。


## 部署时还开着的页面

访客可能开着一个页面，你却部署了新版本，页面是旧构建的，服务器是新的。页面在数据请求（查询参数 `_tifa`）和处理函数调用（`X-Tifa-Build`）里带上它渲染时的构建，构建不同时服务器答 409。访客会看到：

- 点链接：浏览器刷新当前页面，由新构建显示。不管有多少个请求收到 409，都只刷新一次。
- 调用处理函数：调用以状态 409 的 `TifaError` 失败，浏览器刷新页面。调用在处理函数运行前就被拒绝，也不会再发一次，因为新页面对这个入参的含义可能已经变了。访客在新页面上重做一次。你自己的处理函数返回的 409 没有 `X-Tifa-Build` 头，不会刷新任何东西。不带这个头的调用（curl 或别的程序）不检查。
- 加载代码：懒加载路由或 `import()` 请求新构建已经没有的代码块时，浏览器刷新页面（这是 Vite 的 `vite:preloadError`）。

每个构建只刷新一次。刷新后回来的页面仍然和服务器不一致时（缓存又给了旧页面），它不会再刷新第二次。数据请求带着 409 失败，由路由的 `Error` 显示，调用抛出这个 409，失败的 import 交给路由的 `Error` 或发起 import 的代码。标签页用 `sessionStorage` 记住刷新过的构建。页面写下标记并读回来之后才会刷新。浏览器禁用存储，或者存进去的东西没有留下时，页面不刷新：上面的失败照常显示，消息是「the application was updated, reload the page」，由访客手动刷新。记不住的刷新，会在构建持续不一致时一直重复。

HTML 是旧的、入口脚本已经没有的页面，页面自己救不了：没有任何代码在运行。tifa 发出的页面带 `Cache-Control: private, no-cache`，CDN 不会留着，所以只有你在 `OnRequest` 里让 CDN 缓存页面时才会碰到。那就在部署时清掉 CDN 里的页面。

## 服务端渲染时有什么

页面在服务端由 QuickJS 虚拟机渲染，不是 Node，也不是浏览器。虚拟机里有 JavaScript 语言本身（包括 `Proxy`、`WeakRef`、`FinalizationRegistry` 和 `new Function`），另外还有：

- `URL`、`URLSearchParams`、`TextEncoder`、`TextDecoder`、`AbortController`、`Headers`、`Response` 和 `queueMicrotask`。`TextDecoder` 只认 UTF-8，别的编码（比如 `new TextDecoder('latin1')`）会抛 `RangeError`。
- 二进制字符串的 Base64 编解码 `atob` 和 `btoa`，以及 Web API 错误使用的 `DOMException`。`btoa` 拒绝 U+00FF 以上的字符。
- `crypto.getRandomValues` 原地填充整数类型数组，每次最多 65,536 字节。共享或可调整大小缓冲区的视图会在元素类型和配额检查之前抛 `TypeError`。`crypto.randomUUID` 生成 v4 UUID。两者每次都从 Go 的 `crypto/rand` 读取新字节，模块加载期间也能调用，没有随机字节池。这项宿主操作可能读取操作系统熵源，不向应用代码开放文件或网络访问。和 `Math.random` 一样，不要把随机值放进需要 hydration 的 HTML。
- `structuredClone` 支持原始值、对象、数组、Date、RegExp、Map、Set、错误对象（含 cause 和 AggregateError）、ArrayBuffer、类型数组、DataView 和装箱原始值，保留循环引用与共享引用。缓冲区会复制，也支持可调整大小的缓冲区。这类 ArrayBuffer 的所有视图都抛 `DataCloneError`，无论长度固定还是随缓冲区变化。函数、symbol 和弱引用集合抛 `DataCloneError`。Promise、FinalizationRegistry、内建迭代器和不支持的宿主对象按原型链识别并拒绝。移除或替换原型链后，它们可能被当作普通对象，只复制可枚举的自有属性。这些类型无法像受支持的内建类型那样，通过纯 JS 的内部品牌检查始终识别。与浏览器不同，普通对象或数组的透明 Proxy 会通过 traps 克隆为普通对象或数组。仅模仿受支持内建类型原型的对象会被拒绝。SharedArrayBuffer 和非空 transfer 列表暂不支持，也抛 `DataCloneError`，空 transfer 列表可以传。自定义原型、访问器和属性描述符不保留，普通对象和数组只复制可枚举的自有字符串键。
- `AsyncLocalStorage`，既是全局变量，也和 `AsyncResource` 一起由 `node:async_hooks` 导出，和 Cloudflare Workers 一样：有 `run`、`exit`、`getStore`、`AsyncLocalStorage.bind` 和 `AsyncLocalStorage.snapshot`，没有 `enterWith` 和 `disable`。store 跟着 `await` 和 promise 回调走，所以把一次请求的状态放在里面的包能用。这里没有定时器和事件，也就谈不上跟着它们走。
- `Intl` 和各个 `toLocale*` 方法，见[服务端渲染里的 Intl](#服务端渲染里的-intl)。
- `Temporal` 和 `Date.prototype.toTemporalInstant`，见[服务端渲染里的 Temporal](#服务端渲染里的-temporal)。
- `console`，有 `log`、`info`、`debug`、`warn`、`error` 和 `assert`。前四个什么也不输出。`error` 和不成立的 `assert` 会抛错，这次渲染失败，错误信息就是它们的参数。其他方法（比如 `console.time`、`console.table`）没有，调用会抛 `TypeError`。
- `Date.now()` 和 `Math.random()`。能用，但它们的值不能进 HTML，见 [SSR 与缓存](#ssr-与缓存)。

没有的：

- 定时器：`setTimeout`、`setInterval`、`requestAnimationFrame`、`requestIdleCallback`。微任务跑完渲染就结束，不会等时间。
- `performance` 和 `crypto.subtle`。
- 浏览器的东西：`window`、`document`、`navigator`、`self`、`location`、`localStorage`、`matchMedia`、`ResizeObserver`、`Event`、`Blob`、`FormData` 等。
- I/O：`fetch`、`XMLHttpRequest`、`WebSocket`、`require`、`process`、`Buffer`。构建时会替换包里的 `process.env.NODE_ENV`，只读它的代码没问题。

React 在服务端不执行 effect，所以只在 `useEffect`、`useLayoutEffect` 和事件处理里用到这些的代码没问题。渲染过程中用到会让这次渲染失败，模块顶层用到会让虚拟机加载不起来。先用 `typeof` 判断，或者挪进 effect，或者把组件放进 `.client.tsx`。

### 顶层 await 和严格模式

SSR 包是一个 ES 模块。每个虚拟机加载时运行它一次，包里的模块（应用自己的或 npm 包的）可以用顶层 `await`：虚拟机等它完成，再做第一次渲染。虚拟机没有定时器，也不做 I/O，所以这样的 `await` 只能靠 promise 完成，比如对包内模块的 `await import('./mod')`。等定时器或请求的 `await` 永远等不到，服务端起不来：虚拟机加载失败，错误说模块的顶层 await 还没完成。

既然是模块，整个包都在严格模式下运行，和浏览器包一样。只能在非严格模式下运行的包，在服务端同样会失败。只有非严格模式允许的语法，比如 `with` 语句、两个同名参数或给 `eval` 赋值，会让 `tifa build` 失败，错误里有源文件中的位置：构建时像服务端那样把 SSR 包编译一遍，但不运行它。在 `tifa dev` 下，应用加载重建的包时报同样的错。严格模式要等代码运行才拦的，在运行到那里时抛错：给从没声明过的变量赋值抛 `ReferenceError`，读 `arguments.callee` 抛 `TypeError`。

### 引入 Node 模块的包

有的包为只在 Node 上跑的代码引入 Node 模块，比如 `node:fs` 或 `node:path`。服务端没有 Node 模块，但这样的 import 不会让应用起不来：它引入的每个名字都是一个替身，渲染用到它（调用、构造或读它的属性）时才抛错。`tifa build` 列出留在 SSR 包里的 Node 模块和引入它们的包。这份清单只是提示，走不到那段代码的页面照样渲染。只有 `import()` 才走到的模块，要等那个 import 运行时才运行。

渲染用到替身时，错误里有用到的导出、模块、包和它源码里的位置。模块顶层就用到的，虚拟机加载不起来。这时从 `.client.tsx` 文件里引入这个包，或者换用它不需要 Node 的构建，很多包都给浏览器或 workers 准备了这样的构建。对 Node 模块的 `require` 仍是 `require`，服务端没有它，构建同样会提示。`node:async_hooks` 的 `AsyncLocalStorage` 和 `AsyncResource` 不是替身，见上文。

### 会等待的组件

组件可以挂起，等 promise 自己就能完成的事：模块在包里的 `React.lazy` 组件，比如 `lazy(() => import('./panel'))`，或者对一个用页面已有数据做出来的 promise 调 `use`。服务端会等它，写出完整的页面，HTML 里没有 fallback，就是浏览器加载完后显示的样子。

服务端没有定时器，也不做 I/O，所以只在还有 promise 任务可跑时等。等定时器或请求的组件等不到结果：这次渲染失败，错误说有组件挂起了且一直没完成。这种数据用 `Load` 交给页面，或者让组件只在浏览器里渲染。不挂起的页面和以前一样一遍渲染完。

### 一次渲染会给下一次留下什么

虚拟机加载一次，之后一页接一页地渲染，同一时间只渲染一页。模块顶层保存的东西会在渲染之间保留：变量、缓存、闭包，以及库的状态，比如在组件外创建的 store。同一个虚拟机里的下一次渲染能看到它，不管那次是给哪个用户渲染的。`ssr.vms` 个虚拟机各有一份，被替换的虚拟机（出错、超时、内存不够或增长太多之后）从全新加载重新开始。Node 服务端同样会在整个进程存活期间保留模块状态。

所以请求或用户的数据不要放在模块作用域：store、查询客户端之类在组件或 provider 里创建，比如 `useState(() => createStore())`。缓存的内容对每个请求都一样时，模块级缓存没问题，比如格式化器或解析好的常量。

### 在服务端渲染得不一样的库

有些库把一部分工作放在 effect 里，服务端渲染出的内容比浏览器稍后显示的少。照下面这样写，页面仍然能无错水合：

- react-spring：`useSpring` 不传 `from`，`useTransition` 传 `initial: null`。这样服务端和浏览器的第一次渲染都直接显示最终值，之后的变化才有动画。
- react-hook-form：字段用 `Controller` 写。受控字段的值由服务端写进 HTML，`register` 的字段要等水合后才有值。提交按钮在水合前保持禁用（比如用 `useSyncExternalStore(() => () => {}, () => true, () => false)` 判断），免得水合前按回车把表单当普通 HTML 表单提交。
- Radix Select（也就是 shadcn 的 Select）：选中项的文字要等水合后才搬进触发按钮。字段有值时，把这段文字作为 `SelectValue` 的 children 传进去，否则服务端渲染出一个空按钮。没有值时照常显示占位文字。
- recharts 3 在服务端只渲染一个空的外层容器，水合后才画图。给图表区域固定高度，页面就不会跳动，关键数字另外用文字显示。想让图表完全不进服务端渲染，见[只在浏览器里绘制的组件](#只在浏览器里绘制的组件)。

### 只在浏览器里绘制的组件

有些组件在服务端什么也显示不出来：它们要量自己的尺寸、在 canvas 上画，或者等 effect 跑完才渲染内容。服务端写出的是一个空盒子或加载状态，水合后由浏览器换掉。这样的组件在服务端照样付全价：代码在 SSR 包里，每个虚拟机都要加载它，页面每次渲染都要运行它。

这类组件用 `clientOnly` 加载，配一个同样大小的占位。判断一个组件适不适合，就看服务端为它写出的 HTML（查看网页源代码）。如果是空盒子、加载状态，或者浏览器会整个丢掉的标记，服务端渲染它什么也得不到。希望用户在脚本跑起来之前就看到、或者希望搜索引擎读到的文字（比如图表背后的关键数字），应该留在服务端渲染里：在组件旁边用普通文字写出来。

```tsx
import { clientOnly } from 'tifa/client'
import { page } from './route.gen'

// sales-chart.client.tsx 默认导出图表组件。
const SalesChart = clientOnly(() => import('./sales-chart.client'), <div className="h-64 w-full" />)

export default function StatsPage() {
  const { days } = page.useLoaderData()
  return <SalesChart days={days} />
}
```

- `.client.tsx` 文件默认导出组件，props 照常传。
- 服务端渲染和水合时都渲染占位，所以页面能无错水合。之后浏览器加载这个模块，显示组件。占位要和组件最终的尺寸一样，页面才不会跳动。
- `.client.tsx` 文件 import 的东西都不进 SSR 包。在浏览器里它是单独的 chunk，组件第一次渲染时才下载。页面本身在入口脚本里时也是这样（根 Layout 在入口脚本里）。
- 组件比页面其余部分晚一点出现，因为它的代码在水合之后才下载。下载失败时组件抛错，显示最近的 `Error`。

示例 shop 统计页的两张图表改成这样之后，在同一台机器上轮流测两个版本：SSR 包从 1.15 MB 降到 0.77 MB（少三分之一），程序启动时编译它的时间少 40%，加载每个虚拟机的时间少 24%，渲染后每个虚拟机的内存少 3.8 MB（三分之一），统计页渲染快 21%。浏览器入口脚本从 1.08 MB 降到 0.70 MB，0.38 MB 的图表代码只在打开统计页时才下载。

## 服务端渲染里的 Intl

`Intl.DateTimeFormat.format()` 和 `formatToParts()` 不传日期时，与 Date、Temporal.Now 读取同一个渲染时钟，也会使用 `tifa.WithRenderClock` 指定的时钟。

页面在服务端渲染时可以用 `Intl`，支持 Chromium 有的全部语言，用法和 Chromium 一样：

- `Intl.NumberFormat`、带 `formatRange` 的 `Intl.DateTimeFormat`、`Intl.PluralRules`、`Intl.Collator`、`Intl.Segmenter`、`Intl.RelativeTimeFormat`、`Intl.ListFormat`、`Intl.DisplayNames`、`Intl.DurationFormat`、`Intl.Locale`、`Intl.supportedValuesOf`、`Intl.getCanonicalLocales`，以及各构造器的 `supportedLocalesOf`。
- `Date` 的 `toLocaleString`、`toLocaleDateString`、`toLocaleTimeString`，`Number` 和 `BigInt` 的 `toLocaleString`，按语言规则排序的 `localeCompare`，以及 `toLocaleUpperCase` 和 `toLocaleLowerCase`。
- Chromium 有的全部历法，用 `-u-ca-` 或 `calendar` 选项指定。

不传 locale 时服务端按 `en-US` 格式化，不传 `timeZone` 时按 `UTC`，和机器的语言、时区无关。服务端 `Date` 的本地时间也恒为 UTC：`getTimezoneOffset()` 是 0，`new Date(2026, 0, 1)`、`getHours`、`toString` 都按 UTC 算。应用的 Go 代码不受影响：进程保持启动时的时区，`time.Local`、日志和 SQLite 照常用这个时区，和普通 Go 程序一样。

时区数据用程序里内嵌的 tz 2026e，从不读机器的 zoneinfo。输出和 Chromium 156 在约 75,000 个表达式上做过比对。数字、日期和复数全部一致。其余只有几处不同：`Intl.Segmenter` 在部分语言里分词不同，`Intl.DisplayNames` 的名称比 Chromium 多，Winnipeg 按 tz 2026e 改成了永久夏令时，Chromium 156 的数据还没有这一条。错误信息的措辞不同。Firefox 和 Safari 用各自的数据。新版本很接近，但 macOS 26.4 之前的 Safari 数据较旧（CLDR 46），少数格式可能不同。

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
import { page } from './route.gen'

export default function ReceiptPage() {
  const { amount, paidAt, locale, timeZone } = page.useLoaderData()
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

## 服务端渲染里的 Temporal

页面可以用 `Temporal.Now`、`Instant`、`ZonedDateTime`、`PlainDate`、`PlainTime`、`PlainDateTime`、`PlainYearMonth`、`PlainMonthDay`、`Duration` 和 `Date.prototype.toTemporalInstant`，模块加载时也能用。QuickJS 提供 V8 已有的方法、IANA 时区和 Intl 支持的历法。Temporal 和 Intl 共用内嵌的 tz 2026e 数据，不读机器的 zoneinfo。

`Date.now()`、`new Date()`、`Date()`、`Temporal.Now` 和 Intl 无参日期格式化共用 VM 时钟。默认读取实时系统时钟，不在一次渲染内冻结。Go 可以通过 `tifa.WithRenderClock` 为一次渲染指定时钟，见[渲染时钟](#渲染时钟)。`Temporal.Now.instant().epochMilliseconds` 和 `Date.now()` 分别采样，调用之间经过了时间，结果就可能不同。`Temporal.Now.timeZoneId()` 是 `UTC`，和 Intl 的默认时区一致。其他 Now 方法不传时区时也用 UTC。进程的 Go 时区保持不变。

`toLocaleString` 通过 Intl 格式化，要明确传入 locale。普通日期或时间没有时区，会忽略 `timeZone`，`Instant` 用格式化器的时区，`ZonedDateTime` 用自身的时区。构造带时区的值或格式化瞬时时间时要明确指定时区。ISO 历法的 `PlainYearMonth` 和 `PlainMonthDay` 格式化时需要 `calendar: 'iso8601'`。

要让水合一致，从 `Load` 或 Layout 返回 ISO 字符串、语言和时区，在两端构造相同的 Temporal 值。例如数据是 `day: '2026-10-22'` 和 `locale: 'en-US'`：

```tsx
const { day, locale } = page.useLoaderData()
return <time>{Temporal.PlainDate.from(day).toLocaleString(locale, { dateStyle: 'long' })}</time>
```

结果是 `October 22, 2026`。不要把 `Temporal.Now` 的结果放进要水合的 HTML，浏览器运行得更晚，默认时区也可能不同。应在 `Load` 里算好固定值，或者在水合后的 effect 里显示当前时间。浏览器的语言数据也可能不同，见 Intl 一节。tifa 只向服务端 VM 提供 Temporal，不提供浏览器 polyfill 或 TypeScript 类型声明。不支持 Temporal 的浏览器需要在页面代码运行前加载 polyfill，或者让页面在浏览器里避开它。

每个 VM 第一次读取 `Temporal` 或调用 `toTemporalInstant` 时才安装完整实现，不使用它的页面只保留占位实现。用来安装它的字节码在进程内只编译一次。程序体积、VM 内存和首次使用成本见[内存](performance.zh-CN.md#内存)。原有的[六份声明文件](build.zh-CN.md#要随程序附带的声明)也已覆盖 Temporal。

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

接着把 `components.json` 的 aliases 指到 `components/` 里，这样应用根目录只剩 `hooks.go`、`app/` 和 `components/`，放 React hook 的文件夹也不会被当成 `hooks.go`：

```json
"aliases": {
  "components": "@/components",
  "ui": "@/components/ui",
  "hooks": "@/components/hooks",
  "lib": "@/components/lib",
  "utils": "@/components/lib/utils"
}
```

`add` 按这些 alias 放文件、改写导入：`add sidebar` 会写出 `components/hooks/use-mobile.ts`。`lib/utils.ts` 只是从 `cn` 包转出 `cn` 时（shadcn 4 就是这样），删掉它，页面代码也和组件一样从 `cn` 引入 `cn`。不是的话，把它挪到 `components/lib/utils.ts`。

`@/` 指向应用根目录，由应用 tsconfig 里的 `paths` 设定（见 [build.zh-CN.md](build.zh-CN.md#应用的-tsconfig-与-vite-配置)）。这些组件和其他 React 代码一样在服务端渲染。

## hooks.go

项目根目录的 `hooks.go`，四个函数都可选：

```go
func OnStart(ctx context.Context) error         // 开始监听之前，打开数据库、日志等
func OnShutdown(ctx context.Context) error      // 收到退出信号、在途请求完成之后
func OnRequest(c *tifa.Ctx, next func(*tifa.Ctx) error) error // 唯一的中间件，调用 next(c) 继续
func OnError(c *tifa.Ctx, err error) tifa.Error // 意外错误：上报，返回能给用户看的错误
```

- `OnStart` 收到的是程序的 context：程序收到 SIGINT 或 SIGTERM 时它就结束。把它传给可能要等的调用，比如 `db.PingContext(ctx)`，这样连不上的数据库不会让 Ctrl-C 一直等下去。`OnStart` 返回错误时程序停止。`OnStart` 返回之后不要再留着 `ctx`，之后的工作用请求的 context 或 `OnShutdown` 的 context。- `OnRequest` 作用于页面、导航数据请求、API 路由和没匹配的 URL，不作用于静态文件，也看不到浏览器从别的网站发来的写请求：程序先用 403 拒绝它（见[跨站写请求](config.zh-CN.md#跨站写请求)）。没匹配时 `c.Pattern()` 为空，`next(c)` 执行它的 404 所在的各层 Layout（见 [404](#404)）。在开了 `Cache` 的页面上它看到的是匿名请求，没有 cookie、会话，也只有很少几个请求头（见[整页缓存](#整页缓存)）。
- 整个应用共享的资源，比如数据库连接、日志，放在应用根目录、和 `hooks.go` 并列的文件里，比如 `db.go`、`middleware.go`。每个文件构建一个包级变量或一个函数：在 `OnStart` 里初始化，在 `OnShutdown` 里释放，其他地方直接用，比如 import `example.com/myapp` 之后写 `myapp.DB`。`app/` 下的领域 import 根包来用它们。按约定，根包不 import `app/` 下的任何包，所以这样永远不会构成循环引用。模型和查询留在各自的领域里。路由代码只在函数里用这些资源，不在包级变量初始化时读。不用 `init()` 做初始化。见 [import 的方向](structure.zh-CN.md#import-的方向)。
- `next` 执行匹配到的路由，只能调一次，再调返回错误。传给它路由该看到的 `*tifa.Ctx`：`next(c)`，或者用 `next(c.WithContext(ctx))` 把请求级的值存进标准 context，和 `r.WithContext(ctx)` 对请求做的一样。`c.WithContext` 返回副本，`c` 不变。业务包提供带类型的读取函数。
- `next` 返回的错误可以用 `errors.Is`、`errors.As` 认，重定向是 `*tifa.RedirectError`。

### 换日志 Handler

tifa 自己的日志走 `log/slog`，带级别和字段：请求失败是 `ERROR`，带 `method`、`path` 和 `err`，拒绝跨站写请求是 `WARN`，监听的地址是 `INFO`。监听地址这类启动日志在 `OnStart` 返回之后才写，所以在 `OnStart` 里设好的 Handler 能收到它们。要换去处，在 `OnStart` 里设默认 Handler：

```go
func OnStart(ctx context.Context) error {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	return nil
}
```

tifa 每条日志现取 `slog.Default()`，所以之后再调 `slog.SetDefault` 也生效。

### 在 OnStart 里装配

`OnStart` 是应用装配整个进程共用的东西的地方：日志 Handler、普通的 `net/http` 中间件和[会话存储](#会话存在哪)。

```go
import (
	"log/slog"
	"net/http"
	"net/http/pprof"
	"os"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

func OnStart(ctx context.Context) error {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	tifa.SetHTTPHandler(func(app http.Handler) http.Handler {
		mux := http.NewServeMux()
		mux.HandleFunc("/debug/pprof/", pprof.Index)
		mux.Handle("/", otelhttp.NewHandler(app, "tifa"))
		return mux
	})
	return nil
}
```

`tifa.SetHTTPHandler(wrap)` 拿到 tifa 的 handler，返回要对外服务的 handler。这层包在最外面，静态文件、跨站检查和路由都在它里面。它看得到每个请求，拿到的是原始的 `http.ResponseWriter`，被拒绝的跨站写请求的 403 也看得到。它包着的 handler 返回之后，这个请求的 `r.Pattern` 就是路由模板，和 `c.Pattern()` 相同（如 `/projects/{project}`，没有路由匹配时为空），所以指标中间件可以按路由而不是按 URL 计数。浏览器用 `_tifa` 参数请求一次导航的数据，`.p` 标记预加载。不要用这些请求统计页面浏览量：预加载的结果可以不再发请求就用于导航，重新加载又会多一次。数浏览器完成的客户端导航，加整页请求（见[页面请求](build.zh-CN.md#页面请求)）。挂上去的是普通 `net/http`，所以它自己处理的路径（比如上面的 `/debug/pprof/`）不走 `OnRequest`、`OnError` 和跨站检查，要自己加访问控制，或者放到另一个监听端口上。关停时仍会等它包着的请求。只能在 `OnStart` 里调，服务开始监听后再调会 panic。`tifa.SetHTTPHandler(nil)` 去掉这层包装。它在开了 `Cache` 的页面的匿名请求之外运行（见[整页缓存](#整页缓存)）：它放进请求 context 的值会到达这类页面的 `OnRequest` 和 `Load`，所以不要在这里认人，认人放在 `OnRequest` 里。

## 会话

会话是「谁登录了」的记录，存在服务器上，靠一个 cookie 找到。tifa 管记录、token、cookie 和过期。验证密码、决定什么时候登录或登出，是你的代码的事。

```go
// auth/session.go
type Session struct {
	UserID int64  `json:"userId"`
	Name   string `json:"name"`
}

// app/login/route.go
func POST(c *tifa.Ctx, in Login) error {
	u, err := users.CheckPassword(c.Context(), in.Email, in.Password)
	if err != nil {
		return tifa.Error{Status: 401, Message: "邮箱或密码不对"}
	}
	if err := tifa.SetSession(c, &auth.Session{UserID: u.ID, Name: u.Name}); err != nil {
		return err
	}
	return tifa.Redirect("/", 303)
}

// 任何拿得到 Ctx 的地方：OnRequest、方法处理函数、Load
s := tifa.Session[auth.Session](c) // 没登录是 nil

// app/logout/route.go
err := tifa.ClearSession(c)
```

- `tifa.SetSession(c, &s)` 让这个请求登录。它生成一个新的随机 token（32 字节，43 个字符），把 `s` 按 JSON 存在 token 的 SHA-256 下面，删掉请求原来带的会话，再设 cookie。每次调用都换新 token，所以登录之后不会沿用攻击者事先塞进来的 token。旧会话删不掉时，登录失败并返回错误，不发 cookie。
- `tifa.Session[S](c)` 返回本请求的会话，没有就是 nil。服务器在 `OnRequest` 之前就读好了，所以每个钩子、`Load` 和方法处理函数都读得到。第一次调用时解码进 `S`，同时在跑的 `Load` 共用同一个指针，当成只读用。整个应用用同一个类型 `S`，换别的类型调用会 panic。存着的记录对不上 `S` 时，记录被删掉，cookie 被清掉，这个请求按没登录处理。
- `tifa.ClearSession(c)` 删记录，清 cookie，之后 `Session` 返回 nil。没登录时什么都不做。存储删不掉记录时，cookie 仍然会清，返回的错误会说明记录在过期前仍然有效。
- 记录加上 tifa 包在外面的信封不能超过 4 KiB。里面放一个 ID 和几个展示用的字段，其余放在你自己的数据库里。能写这个存储的人可以让任何人登录，所以要像保护数据库一样保护它。

### cookie

cookie 叫 `__Host-tifa_session`，带 `HttpOnly`、`Secure`、`SameSite=Lax`、`Path=/`，没有 `Domain`。会话从 `SetSession` 起算 30 天，不续期，所以请求从不写存储。名字和时长用 `session.cookie` 和 `session.max_age` 改（见[配置](config.zh-CN.md#配置项)）。`tifa dev` 下 cookie 叫 `tifa_session`，不带 `Secure`，因为 dev 在 localhost 上跑普通 HTTP。不是 43 个 `A-Z a-z 0-9 - _` 字符的 cookie 会被直接丢掉，不去问存储。存储里已经没有记录的 cookie（过期了，或者用内存存储时进程重启过）也会被丢掉，这个请求按没登录处理。

设置或清除 cookie 的回答，不管应用设了什么，都以 `Cache-Control: no-store` 发出，不带 `ETag` 和 `Last-Modified`，状态码保持原样，永远不会变成 304。

### 会话存在哪

不设存储时，会话放在进程的内存里，上限是 `session.memory_limit`（64 MiB）。满了就先挤掉最早过期的，重启后所有人都要重新登录。服务器启动时会打一行日志说明这一点。`tifa dev` 在你改 Go 代码后会重启，那时你会被登出。如果应用允许没登录的访客开会话，要在入口限流，否则灌满它会把真正的会话挤出去。

想把会话放到别处，比如 Redis 或 SQL，在 `OnStart` 里给它一个存储：

```go
func OnStart(ctx context.Context) error {
	tifa.SetSessionStore(redisStore{client})
	return nil
}
```

`tifa.SetSessionStore` 和 `tifa.SetHTTPHandler` 一样，服务开始监听后再调会 panic。存储实现 `tifa.SessionStore`：

```go
type SessionStore interface {
	Get(ctx context.Context, token string) (data []byte, found bool, err error)
	Put(ctx context.Context, token string, data []byte, expiry time.Time) error
	Delete(ctx context.Context, token string) error
}
```

- `token` 是 cookie 里 token 的 SHA-256（base64url），不是 cookie 本身。
- 键不存在、已过期或无效时，`Get` 返回 `found == false` 和 nil 错误。只有存储自己出了故障才返回错误。这时请求按意外错误走 `OnError`，不会当成没登录继续，那样会把故障藏起来。
- `Put` 新建或覆盖记录，`expiry` 之后 `Get` 不能再返回它。键不存在时 `Delete` 返回 nil。
- 三个方法会被很多请求同时调用。`Put` 不能留着 `data` 切片，`Get` 不能返回存储还在用的切片。`ctx` 结束时都要返回错误。

`tifatest.TestSessionStore(t, store)` 对你的存储跑这些检查。

### 哪里能调什么

| 位置 | `Session` | `SetSession`、`ClearSession` |
| --- | --- | --- |
| `OnRequest` 里 `next` 之前 | 可以 | 可以 |
| `OnRequest` 里 `next` 返回之后 | 可以 | 返回错误 |
| 方法处理函数 | 可以 | 可以 |
| `Load`、`Layout`、`OnError` | 可以 | 返回错误 |
| 响应发出之后 | 可以 | 返回错误 |

`Load` 跑在 GET 上，GET 不该改任何东西，所以登录放在方法处理函数或 `OnRequest` 里。

开了 `Cache` 的页面，GET 和 HEAD 请求是匿名的：不管谁来，`Session` 都是 nil，`SetSession` 和 `ClearSession` 返回错误。这个承诺说的是 tifa 交给你的代码的请求头。用 `tifa.SetHTTPHandler` 挂的 `net/http` 中间件放进 context 的值仍会到达，所以认人放在 `OnRequest` 里，不要放在那个中间件里（见[整页缓存](#整页缓存)）。

### 和登录库的分工

tifa 存会话。用户是谁由你的应用或登录库来认：密码哈希（bcrypt、argon2）、OAuth 授权码流程和它的 `state` 校验（`golang.org/x/oauth2`）、两步验证。认证成功后调 `tifa.SetSession`。这类库自带的临时状态，比如 OAuth 的 `state`，放在库自己的短命 cookie 里。鉴权在 Go 里读 `tifa.Session` 来做。

从别的网站发来的写请求会在这一切之前被拒绝（见[跨站写请求](config.zh-CN.md#跨站写请求)），cookie 又是 `SameSite=Lax`，所以别的网站上的表单不能替用户登出或操作。要让这一点成立，GET、HEAD 和 OPTIONS 都不能改任何东西，登出用 POST。

### 构建时的检查

`tifa build` 和 `tifa check` 会读模块里每一处 `tifa.SetSession`、`tifa.Session` 和 `tifa.ClearSession` 的调用，遇到下面的情况就失败：

- 调用用的不是同一个会话类型（错误会写出两处位置），或者在泛型函数里用类型参数调用
- 有 `Session` 或 `ClearSession` 的调用，却没有任何地方调用 `SetSession`
- 类型不是 struct，或者不能从 JSON 读回来
- `Load`、`Layout`，或者它们传下 Ctx 的函数调用了 `SetSession` 或 `ClearSession`
- 页面或 API 路由的路径以 `/__tifa/` 开头，或者 `public/__tifa/` 下有文件（`/__tifa/session` 是会话回答的路径）

会话类型可以有方法 `Exposed() M`（指针接收者也行）。`Exposed` 返回允许交给浏览器的那部分 session。它会写进页面 HTML，并保存在浏览器的 localStorage 里。放进这里的字段要能接受长期留在访客的设备上。不要放凭据和需要保密的信息。权限判断永远用服务端的 session。`M` 遵守 `Load` 结果的规则，构建把它的 TypeScript 类型写进 `.tifa/session.gen.ts`：

```tsx
import type { SessionData } from '@/.tifa/session.gen'
```

同一个文件还导出 `SessionEnabled`：应用调用了 `tifa.SetSession` 时是 `true`，否则是 `false`。类型没有 `Exposed` 方法，或应用没有会话时，`SessionData` 是 `never`。`Exposed` 带参数、有多个返回值，或者返回值不能是 JSON，构建会失败。

### 在浏览器里

浏览器里存着一份「谁登录了」的副本，第一次绘制时就按它显示，那时页面上的脚本还没跑。页面用 `useSession` 读它，用 `SignedIn` 和 `SignedOut` 按它显示或隐藏元素。

```tsx
import { SignedIn, SignedOut, useSession } from 'tifa/client'

function Header() {
	const { signedIn, data, ready } = useSession()
	return (
		<header>
			<SignedOut><a href="/login">登录</a></SignedOut>
			<SignedIn><span>你好，{data?.name}</span></SignedIn>
		</header>
	)
}
```

- `useSession()` 返回 `{ signedIn, data, ready }`。`data` 是 `Exposed` 方法返回的值（类型是 `SessionData`），类型没有 `Exposed` 或没人登录时是 `undefined`。在从不调用 `tifa.SetSession` 的应用里调用它是类型错误，运行时也会抛错。
- `SignedIn` 和 `SignedOut` 只在有人登录或没人登录时显示它们的子元素。它们必须正好有一个元素作子节点，自己不产生任何元素。子元素要把收到的 props 传给自己的 DOM 元素，`<a>`、`<div>` 和展开 `...props` 的组件都可以。开发模式下，不这样做的子元素在挂载时会报错并说明原因。tifa 放在 head 最前面的一条样式会从第一次绘制起隐藏不该显示的那个，你不用写 CSS。
- `ready` 在浏览器知道自己该显示哪个状态之后是 `true`。为访客渲染的页面从第一次渲染起就是 `true`。不带会话的页面见下文。
- 副本只决定显示什么，不决定谁能做什么。权限在 Go 里检查。

凡是只给一个访客的回答都带会话，形状是 `{ "signedIn": true, "data": ... }`（只有类型有 `Exposed` 才有 `data`，没人登录时是 `{ "signedIn": false }`）：

| 回答 | 带什么 |
| --- | --- |
| 页面 | 页面里带着会话，服务器在 `<html>` 上写 `data-session="in"` 或 `data-session="out"`。`SSR = false` 时会话在启动数据里 |
| 导航数据 | `{ "routes": { ... }, "session": ... }`，点链接就拿到当前的会话 |
| 让某人登录或登出的处理函数调用 | `{ "result": ..., "session": ... }`。没有结果时是 `{ "session": ... }`，不再是 204。没动会话的调用没有 `session`，仍然是 204。重定向也带，错误不带 |
| `GET /__tifa/session` | 本次请求的会话，`Cache-Control: private, no-store`。它在 `OnRequest` 里运行，所以让访客先登录才能通过的中间件必须放行它 |

不带 `X-Tifa-Call` 请求头的请求（curl、表单、别的程序）得到的回答和以前一样。

浏览器把会话存在内存里，也存在 `localStorage` 里，键是 cookie 名加 `:v1`，比如 `__Host-tifa_session:v1`。凡是带会话的回答到了，两处副本一起写：登录了就写，登出了就清，回答里没有会话就不动，`<html data-session>` 跟着改。页面自带的会话优先于 `localStorage`。`localStorage` 任何一步失败（比如无痕窗口），就只用内存里的副本，不会出错中断。请求交错时，以最后发出的那个请求的回答为准。

服务器对所有访客回答得一样的页面，比如开了 `Cache` 的页面（见[整页缓存](#整页缓存)），不带会话，也没有 `data-session`。只有这样的页面里，head 最前面的脚本在第一次绘制前按 `localStorage` 设好 `data-session="in"`，`useSession` 在页面水合完成前按未登录渲染，之后切到 `localStorage` 里的副本，并向 `/__tifa/session` 问一次来核对。没有副本就不问。`localStorage` 读不了时也问一次。

在浏览器里调用处理函数，如果这次调用改了会话，副本随之更新。之后导航的数据请求也会带回会话。登录、登出的处理函数要有入参类型或结果类型（`func POST(c *tifa.Ctx, in Login) error`）。自己写响应的处理函数（`func POST(c *tifa.Ctx) error`）没法把会话放进回答里。

带着会话的回答，`Cache-Control` 必须是 `no-store`，或者同时有 `private` 和 `no-cache`，两个都不带字段列表（`private="Set-Cookie"` 不算），也不能有 `must-understand`。不设这个头，它就是 `private, no-cache`。`OnRequest`、`Load` 或处理函数在页面、页面的启动数据或调用的信封上设了别的值，请求按意外错误交给 `OnError`，错误回答是 `no-store`，没有 `ETag` 和 `Last-Modified`。导航重定向和已经在报错的导航回答不再抛一次：服务器记日志、清验证器、改成 `no-store`。`/__tifa/session` 永远是不带验证器的 `private, no-store`，中间件自己回答它时也一样。自己写响应的处理函数不检查。

`data-session` 是 tifa 在 `<html>` 上管的属性。根 Layout 的 `htmlAttributes` 设了它，服务器启动时就会停下（VM 预热失败，`tifa dev` 会显示错误）。路由的 `htmlAttributes` 设了它，渲染会失败。

**内容安全策略（CSP）。** 脚本和样式都是内联的，每个页面都一样。CSP 写在 `<meta>` 标签里时它们不受影响，因为它们排在标签前面。CSP 写在响应头里时，放行它们的哈希：

```text
script-src 'sha256-NWjzg1BGj/5CJBw80xzDV4Gmw6vQUG2ExZLUcX4mWkE='
style-src 'sha256-dutb67YDz+meUugqDFx3RsopE/hCQ31NRHukbTNY+OQ='
```

这是默认 cookie `__Host-tifa_session` 的哈希。脚本里写着 `localStorage` 键的名字，所以设了 `session.cookie` 之后脚本的哈希会变，`tifa dev` 下 cookie 是 `tifa_session`，脚本的哈希是 `sha256-RXrgEYZIzBu7H8LtSsupWFiD5XE/6dvi74b6rHR+nVg=`。从你的构建出的页面里取出脚本再算哈希。

**做不到的事。** 另一个标签页在它下一次带会话的回答到来之前显示旧状态，在那里提交的表单已经用新的 cookie。在别的设备上登出后，从 `localStorage` 出来的页面会先显示旧名字，直到核对的回答回来。在同一个标签页登录成别人之后按后退，浏览器的前进后退缓存里恢复的是原样的旧页面。切换账号时正在进行的预加载，可能带回旧账号的数据。你自己写的 `fetch` 改了会话，tifa 不知道，在它之后调 `router.invalidate()`，路由器留着的页面不该给下一个人看时再调 `router.clearCache()`。`Exposed()` 只放允许留在设备上的东西，比如名字和头像，不要放凭证、邮箱或手机号。

### 测试会话

`tifatest.NewCtx(t, r)` 给 `Ctx` 配了一个只属于这个测试的内存存储，所以 `SetSession` 和 `ClearSession` 能用，recorder 里有 `Set-Cookie`。同一个测试的所有 `Ctx` 共用这个存储，所以一个 `Ctx` 拿到的 cookie 可以让下一个登录：用 `r.AddCookie` 加到请求里。要测一个需要登录状态的函数，不用走 cookie：

```go
c, _ := tifatest.NewCtx(t, httptest.NewRequest("GET", "/", nil))
tifatest.WithSession(c, &auth.Session{UserID: 1})
```

`WithSession` 只设请求的状态，不验证 cookie 和存储的流程，那部分由 tifa 自己的测试覆盖。

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
- 测 `OnRequest` 时直接调用它，`next` 传一个闭包：`OnRequest(c, func(c *tifa.Ctx) error { got = c.Context(); return nil })`。闭包代替的是路由，所以不恢复 panic，也不写出结果。
- 请求体上限是 `server.body_limit` 的默认值 1 MiB，可以用 `c.SetBodyLimit` 改。测试结束时会清理请求的 multipart 临时文件。
- `tifa build` 不读 `_test.go` 文件，所以测试可以放在 `app/` 下任何目录里。

### 渲染时钟

在 `OnRequest` 中通过 context 指定时钟，也可以在测试里给 `httptest` 请求设置同样的 context：

```go
at := time.Date(2026, 10, 22, 12, 0, 0, 123456789, time.UTC)
ctx := tifa.WithRenderClock(c.Context(), func() time.Time { return at })
return next(c.WithContext(ctx))
```

Date 和 Intl 取向下舍入的毫秒，Temporal 保留纳秒。这个时钟覆盖整个渲染，包括 promise 和 queueMicrotask，归还或替换 VM 前会清除。传 nil 恢复实时时钟。回调不能阻塞，多次渲染共用回调时必须保证并发安全。它不改变 Go 的 time.Now、超时或浏览器时钟。

不调 `WithRenderClock` 时时钟保持实时。固定时钟不会让同时用到随机数或外部数据的渲染变得可重复。

模块顶层代码在 VM 预热时执行，早于请求，用的是实时时钟。顶层保存的时间值不会重新计算。顶层保存的 Date.now 函数、Temporal.Now 方法和 Intl formatter 在渲染时调用，则会读当前渲染的时钟。

时钟不参与缓存键，所以带时钟的请求跳过渲染缓存和共用渲染，也不带 ETag，总是用自己的时钟渲染。需要显示并水合的时间应从 `Load` 返回，让服务端、缓存和浏览器使用同一份数据。`tifatest.NewCtx` 保留请求的 context，但自身不执行 SSR。可以给 `OnRequest` 传一个闭包当 `next`，测试中间件传递这个 context，完整 HTML 应通过服务端请求验证。

## Load 里 Ctx 的用法

这一节管 `Load`、`Layout` 和它们调用的函数。构建时，tifa 从每个 `Load` 和 `Layout` 函数出发，找出它读的路由参数（`c.Param`）、查询参数（`c.Query` 和 `c.QueryValues`）和它依赖的 Layout（`tifa.Use`）。`Load` 把 `*tifa.Ctx` 传给本模块里的函数时，tifa 会一层层跟进去，跨包也可以，读到的参数都算在这个 `Load` 名下。一个辅助函数被多个 `Load` 调用时，分别算给每个 `Load`。tifa 自己的函数不跟进。

为了让 tifa 找全，Ctx 只能用在它看得见的地方。`c.Param`、`c.Query` 和 `c.QueryValues` 的参数写常量字符串，比如 `c.Param("order")`。它们和 `tifa.Use` 都要直接调用，存进变量或当作值传出去都会构建报错。

能持有 Ctx 的类型只有：`*tifa.Ctx`、`tifa.Ctx`、本模块里声明的非泛型结构体（字段只通过这几种类型持有 Ctx，直接含有、嵌入或间接含有都可以），以及指向这种结构体的指针。

持有 Ctx 的值只能出现在这五个地方：

1. 直接调用方法时的接收者，比如 `c.Query("tab")`、`req.Page()`。
2. 传给本模块或 tifa 的非泛型函数，对应参数的类型完全相同。就地调用的函数字面量也算。
3. 写进持有 Ctx 的结构体字面量，字段类型完全相同。
4. 用 `:=` 或 `var x T = ...` 赋给类型完全相同的新局部变量，这个变量同样受这些规则约束。
5. 取地址，得到的指针仍是能持有 Ctx 的类型。

其他用法都会构建报错，并指出文件和行号，比如：转换成别的类型或接口，赋给已有变量，多个值一起赋值，`return` 它，放进切片、map、数组或通道，`range`，包级变量，`defer`，`go` 语句，传给泛型函数、接口方法、函数变量或模块外的函数（标准库和第三方库）。

`Load`、`Layout` 和它们调用的函数里不能调用 `c.Request()` 和 `c.Writer()`，通过原始请求读的参数 tifa 看不到。改用 `c.Param`、`c.Query`、`c.QueryValues`、`c.Header`、`c.Cookie` 或 `c.Context()`。这两个方法只在方法函数和 `OnRequest` 里用。

`c.BindQuery` 和 `c.Bind` 也不能调用。结构体解码了哪些查询参数 tifa 说不出名字，这些参数变了页面不会重新取数据，而页面加载和导航的数据请求都没有请求体。查询参数用 `c.Query("name")` 逐个读，重复的参数用 `c.QueryValues("name")`。

遇到报错时，直接调用 Ctx 的方法，或者把它传给普通函数。只需要请求上下文时传 `c.Context()`，只调用 `c.Context()` 的辅助函数（比如 `auth.User(c)`）跟进后什么也不收集。

tifa 只检查 `Load` 和 `Layout` 能到达的代码。在 `OnRequest`、方法函数或别处把 Ctx 存进全局变量或其他共享位置，再在 `Load` 或 `Layout` 里取出来用，这条路查不到，不要这样写。

## JSON 规则

`Load` 和 `Layout` 函数的返回值、方法函数的入参和结果都以 JSON 传输，tifa 按它们生成 TypeScript 类型。tifa 用 `encoding/json/v2` 及其默认设置读写这些 JSON，所以构建只接受下面这些类型：json v2 写它们不会出错，写出来的样子和 TypeScript 类型一致。`c.JSON`、`c.Bind` 和会话用同样的规则。

| Go | TypeScript |
| --- | --- |
| `bool` | `boolean` |
| 整数、浮点数、`json.Number` | `number` |
| `string`、`time.Time` | `string` |
| `tifa.HTML` | `tifa/client` 的 `HTML`，见[显示应用信任的 HTML](#显示应用信任的-html)。不能做 map 的键，不能放在入参或会话里，不能是整个结果 |
| 指针 | `T \| null` |
| 切片 | `T[]`。nil 切片写成 `[]` |
| `[]byte`、`[N]byte`（元素就是 `byte` 或 `uint8`） | `string`，写成 base64。nil 的 `[]byte` 是 `""`。元素是具名 byte 类型的切片或数组是普通的数字数组 |
| 数组 | `T[]`，长度为 0 时为 `[]` |
| map | `Record<string, V>`。nil map 写成 `{}` |
| 结构体 | 导出字段组成的对象 |

- map 的键按顺序排好再写，同一个值总是写出同样的文本。键必须是 string 或整数类型，或者是按下面 `MarshalText` 规则输出 string 的类型。接口和指针不能做键。
- 导出的嵌入结构体（也可以是指针）没写 json 名字时，字段提升到外层，经由嵌入指针到达的字段是可选的。不同结构体里的字段同名时，层级最浅的胜出，其次是带 json tag 的，还分不出来就全部丢掉。嵌入字段的类型不是结构体时要写 json 名字，嵌入的结构体不能带 json tag 选项，也不能有名为 `MarshalJSON`、`MarshalJSONTo`、`MarshalText`、`AppendText`、`UnmarshalJSON`、`UnmarshalJSONFrom` 或 `UnmarshalText` 的方法。未导出的嵌入字段不接受，除非标了 `json:"-"`。
- 同一个结构体里的两个字段不能有相同的 JSON 名字。未导出字段不能带 `json:"-"` 以外的 json tag，有字段的结构体至少要有一个导出字段或一个 json tag。
- json tag 的名字只能用字母、数字、空格和 `` !#$%&()*+-./:;<=>?@[]^_{|}~ ``。选项只能用 `omitempty`、`omitzero` 和 `string`，用了 `omitempty` 或 `omitzero` 的字段是可选的。`omitzero` 省掉 Go 类型的零值。`omitempty` 省掉 JSON 为空的值：`null`、`""`、`[]` 或 `{}`，所以不会省掉 `false` 和 `0`。`string` 选项只能用在预声明的数字类型字段上，或者指向它们的非具名指针上，把数字写成字符串。
- 不接受 `time.Duration`，做 map 的键也不行，json v2 没有它的写法。请返回你要的单位的整数（比如毫秒），或者字符串。
- 类型的值或指针上有名为 `MarshalJSON`、`MarshalJSONTo`、`AppendText` 或 `AppendJSON` 的方法或字段时，不论签名一律拒绝（`time.Time` 和 `json.Number` 除外）。请改名，或换一个不带这些方法的类型返回。
- 类型自己声明了签名正好是 `MarshalText() ([]byte, error)` 的方法时，不论在值接收者还是指针接收者上，都输出 string。签名不对、从嵌入字段提升而来、或被同名字段遮住时，一律拒绝。
- 接口出现在任何位置都不接受，包括字段、元素和 map 键。chan、func、复数、`unsafe.Pointer` 和类型参数也不接受。
- `<`、`>` 和 `&` 原样写出，页面数据里也一样。tifa 把数据放进页面的地方会转义它们，应用的文本不会提前结束 script 元素。
- 字符串里不是 UTF-8 的字节，每个写成一个 U+FFFD，和浏览器显示的一样，页面不会因此失败。
- 读入参时很严格：名字必须和 json 名字完全一致（大小写也算），一个对象里同一个名字只能出现一次，字符串必须是 UTF-8，`time.Time` 必须是 RFC 3339，base64 里不能有换行，数组长度必须等于 Go 数组的长度。不符合的回 400。类型里没有的名字会跳过。会话记录按这些规则读不回来时，访客会被登出。
