---
summary: 'route.go 里的 Load、page.tsx 里的数据、route.gen、tifa.HTML、Load 怎样使用 Ctx'
audience: app
order: 3
translatedFrom: f5aed0a7a739
read_when:
  - '编写页面的 `Load`，或者页面的 route.go'
  - '编写 page.tsx，或者使用 `./route.gen` 导出的东西'
  - '用 `tifa.HTML` 和 `<HTML>` 显示应用自己生成或信任的 HTML（比如文章），或者处理其中的链接'
  - '构建报出关于 Ctx 用法的错误，想把它修好'
  - '修改 internal/analyze 里的规则，需要知道曾向用户承诺了什么'
---

# 加载数据 {#loading-data}

## route.go {#routego}

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

- `Load` 返回页面数据。它的返回类型必须能转换成 TypeScript（见 [JSON 规则](../reference/json.zh-CN.md)）。
- `GET`、`POST`、`PUT`、`PATCH` 和 `DELETE` 处理与它们同名的 HTTP 方法。
- `Layout` 返回这个文件夹的布局的数据（见[布局](layouts.zh-CN.md#the-layout-of-a-folder)）。
- 有 `page.tsx` 的文件夹是一个页面。框架负责渲染它的 GET，所以你不能再写 `GET`。一个不需要数据、也不需要其他方法的页面，可以只有 `page.tsx`，不要 `route.go`。
- 有 `route.go` 但没有 `page.tsx` 的文件夹，只要声明了方法处理函数，就是一个 API 路由：每个方法处理函数都按你写的方式响应，并且不允许有 `Load`，因为 `Load` 是用来加载页面数据的。只有 `Layout` 的 `route.go` 不是 API 路由，它只是给下面的页面提供布局数据。
- 一切都是包级函数和常量。依赖请用应用已经设置好的全局变量。框架不做依赖注入。
- 开关的名字（`SSR`、`Cache`、`Memo`）按字面读取。`route.go` 里一个小写的 bool 常量，名叫 `cache`、`memo` 或 `ssr`，构建会失败，因为它会被忽略。其他常量都是应用自己的：`route.go` 是你的包，所以 `const SSL = true` 或 `const Demo = true` 不会被当成开关拼错了，而拼错的开关（`Cahce`）会被悄悄忽略。

在 `Load` 里只能设置响应头和 cookie。调用 `c.JSON`、`c.Text` 或 `c.NoContent` 会返回错误。

`Load` 失败时返回什么，见[你返回的错误会变成什么](errors.zh-CN.md#what-an-error-you-return-becomes)。

## 查询参数 {#query-parameters}

读取查询串的 `Load` 或 `Layout`，把一个结构体作为第二个参数，每个查询参数对应结构体的一个字段：

```go
type Filter struct {
	Sort  string   `json:"sort"`
	Color []string `json:"color"`
	Page  int      `json:"page"`
}

func Load(c *tifa.Ctx, q Filter) (products.List, error) {
	return products.Find(q.Sort, q.Color, q.Page)
}
```

不读查询参数的 `Load` 只接收 Ctx。第二个参数是结构体的值，不是指针，类型可以有名字，也可以直接写在参数里。

- 字段名取 `json` 标签，没有标签就取 Go 字段名。带 `json:"-"` 的字段和未导出的字段不算。任何字段都不能叫 `_tifa`，Tifa 把它留给导航使用。
- 构建时 Tifa 从这个结构体读出参数名。其中一个变化时页面会重新加载，服务端渲染页面时只带这些参数，不带别的（比如 `?utm_source=` 会被去掉），`Link`、`navigate` 和 `useSearch` 也知道这些名字（见[导航](navigation.zh-CN.md#query-parameters-in-link-and-navigate)）。
- 字段可以是字符串、`bool`、整数、`float64`，或者它们的切片。嵌入字段、指针、map、带 `MarshalText` 或 `UnmarshalText` 的类型，以及 `float32` 会让构建失败，报错里会指出是哪个字段。`float32` 见下面的类型说明。几个页面共用的参数，在每个结构体里各写一遍，布局和它下面的页面都声明的同名参数，在两处类型相同。
- 每种字段类型，页面在 TypeScript 里拿到的类型：

| Go 字段 | TypeScript（在 `useSearch()` 和 `PageSearch` 里） |
| --- | --- |
| `string` | `string` |
| `bool` | `boolean` |
| `int`、`int8`、`int16`、`int32`、`int64`、`uint`、`uint8`、`uint16`、`uint32`、`uint64` | `number` |
| `float64` | `number` |
| 以上任一种的切片 | 该类型的数组 |

- 字段不能是 `float32`，否则构建失败。查询值在 Go 和浏览器里必须是同一个数，而浏览器只有 64 位的数。`float32` 会把值四舍五入：`?price=1234567.89` 在 float32 里是 1234567.9，浏览器里的页面却会显示 1234567.89。把字段声明成 `float64`。这条规则只管 `Load` 和 `Layout` 的查询结构体：方法处理函数的输入仍然可以用 `float32`。
- 值按字段的类型来读，数字可以写成人手输的样子。`?page=007` 是第 7 页，`?rating=1.0` 是 1，`?min=%2B10.50` 是 10.5，数字输入框发出的 `1e3` 是 1000：

| 类型 | 接受 | 返回 400 |
| --- | --- | --- |
| `string` | 任何文本，空串也行，从不改动（`?q=007` 还是 `007`） | |
| 整数 | 数字，可带 `+` 或 `-`，数字之后可以有一个小数点，后面只能是零（`7`、`007`、`+7`、`-0`、`7.`、`7.00`），不超出该类型的范围，也不超过 2^53-1 | `7.5`、`1e3`、`0x10`、`abc`、空格、超出范围的值 |
| `float64` | 数字，可带 `+` 或 `-`，可带小数部分，可带指数（`10.50`、`.5`、`5.`、`1e3`、`-0`），值是 `0` 或绝对值在 1e-6 到 1e21 之间 | `1e400`、`NaN`、`Infinity`、`0x1p3`、`1,5`、`abc`、空格、超出范围的值 |
| `bool` | 只有 `true` 和 `false` | `1`、`TRUE`、`on`、`yes` |
| 切片 | 同名参数每出现一次就是一个元素，每个元素按元素类型的规则 | |

- 同一个数的另一种写法，在各处都是同一个数：服务端按最短的文本渲染页面（`?page=7`），所以 `?page=007` 和 `?page=7` 是同一个页面，一次渲染，一个缓存条目，一个 canonical 链接。页面加载之后，地址栏会悄悄变成 `?page=7`，不请求数据，不重新加载，也没有水合警告。`-0` 就是 `0`。位数比 float64 能放下的更多的浮点数，读成最接近的那个 float64，也按那个来写。数字的文本最长 64 个字节，整数和浮点数都一样，更长的返回 400：很长的小数，Go 和浏览器会舍入成不同的结果，而人手输的或表单发出的数字不会有这么长。

- 值不是以上任何一种时，在 `Load` 运行之前就返回 400，内容是 `invalid query parameter "page"`。导航的数据请求拿到的是该页面的 400。查询串本身解析不了，返回 400 和 `invalid query string`。不是切片的字段，同名参数出现两次也不合法。有两个值不合法时，消息里写的是按名字排在前面的那个。
- 浏览器按同样的规则读地址，所以 Go 拒绝的值，浏览器也拒绝，页面的 `ErrorBoundary` 拿到同一个 `TifaError`，状态是 400，消息也一样，不管是整页、导航的数据请求还是浏览器里的路由器发现的。一张用例表同时测试 Go 和浏览器。
- 不合法的值就是写错的地址，和没有页面的路径一样。访客看到什么由页面的 `ErrorBoundary` 决定，比如一句简短的提示，加一个去掉这个筛选的链接。不要为了躲开 400 把字段声明成 `string`：那样页面就丢了类型、最短的地址和链接的当前状态，每个值还得自己检查。
- 页面拿到的值带着字段的类型：`useSearch()` 和 `PageSearch` 里，整数和浮点数是 `number`，`bool` 是 `boolean`，字符串是 `string`，切片是它们的数组（见[导航](navigation.zh-CN.md#query-parameters-in-link-and-navigate)）。浏览器只按声明的类型转换，不按别的，所以 `string` 字段里的 `?q=1.0` 和 `?q=007` 还是文本 `1.0` 和 `007`。
- 不是字符串的字段，值为空（`?page=`）算没传，字段保持零值，所以会发送空字段的表单也能用。数字或 bool 列表里的空元素会被跳过。对字符串来说，空串就是一个值。服务端渲染时当作没有这个空值，返回 200，不重定向。页面加载之后，地址栏会去掉它（`?page=&q=x` 变成 `?q=x`），不请求数据，不重新加载，也没有水合警告。在浏览器里它的值是 `undefined`，零值只是 Go 字段里放的东西。
- 所以 Go 分不清一个参数是没传，还是传了零值：`?page=0` 和没有 `page` 都得到 0。让零值就是默认值（page 为 0 就是第一页，`max` 为 0 就是不限）。需要单独开关的选择，比如只看免费书，用一个单独的 `bool` 字段。字段不能是指针。
- 没有字段对应的参数会被忽略。
- 访客改地址栏，或者填一个不用 JavaScript 的普通 `<form method="get">`，可以写 `007`、`1.0` 或 `.5`，页面照收。这些规则只管页面的查询字符串：`GET` 或 `DELETE` 处理函数的输入照旧，由 `c.BindQuery` 解码。
- 这个结构体不带 `Validate`，带了构建就失败。请在 `Load` 里检查值，返回 `tifa.Error{Status: 400, Message: "page must be at least 1"}` 或 `tifa.ErrNotFound`。
- 测试直接用结构体调用函数：`Load(c, Filter{Sort: "price"})`（见[测试应用](testing-apps.zh-CN.md)）。

## page.tsx {#pagetsx}

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

- 只管 UI，不加载数据。`./route.gen` 是生成的模块，每个文件夹一个，导出 `page` 和 `layout`（页面和布局的 TanStack 路由，只在文件夹里有它们时才有）、`PageData` 和 `LayoutData`（`Load` 和 `Layout` 的返回类型）、`PageSearch` 和 `LayoutSearch`（它们读取的查询参数，见 [Link 和 navigate 里的查询参数](navigation.zh-CN.md#query-parameters-in-link-and-navigate)）、`Params`、它们所含 Go 结构体的类型（见下文），以及每个方法处理函数对应的一个函数。同一文件夹里的子组件也从 `./route.gen` 导入。布局的数据来自它所在文件夹的 `route.gen`：`layout.tsx` 从 `'./route.gen'` 导入 `layout`，页面则用 `'../../route.gen'` 里的 `layout` 读取它的项目布局，举例来说。有人导入某个文件夹的 `route.gen`，而该文件夹没有生成的模块时，构建会失败。
- `./route.gen` 还会导出文件夹的数据用到的每个 Go 结构体，名字就是它的 Go 名字，所以组件可以直接用名字指一块数据。`PageData`、`LayoutData`，以及处理函数的 `<METHOD>Input` 和 `<METHOD>Output`，是同样的类型，只是用了常见的名字。文件夹的 Go 包里有 `type Order struct` 和 `type Item struct` 时：

  ```tsx
  import { page, type Item } from './route.gen'

  function Row({ item }: { item: Item }) {
    return <li>{item.sku}</li>
  }
  ```

  - 文件夹所在包里的结构体保留原名。两个包有同名结构体时，另一个包的结构体在名字前加上它的包名（`cart.Order` 是 `CartOrder`），如果这样还是重名，这个结构体就仍然写在原处，没有名字。
  - 结构体的名字和 `route.gen` 自己导出或用到的名字相同时，比如 `Params`、`PageData`、`Record`、`Blob` 或 `HTML`，前面加 `Go`（`GoParams`）。`Load` 返回的 Go 结构体本身就叫 `PageData` 时，它就是那个 `PageData`。
  - 只有结构体有名字。`type Status string` 这样的类型、切片或 map 类型、`Page[Order]` 这样的泛型类型、在函数里声明的类型和匿名结构体，都仍然写在原处。
  - 每个文件夹有自己的一份类型，`route.gen` 之间不互相导入。要用哪个类型，就从你所在文件夹的 `route.gen` 导入。
  - 原来的写法 `PageData['orders'][number]` 仍然可用。
- `route.gen` 是根据 `route.go`、`page.tsx` 和 `layout.tsx` 写出来的，它开头几行写明了来自哪些文件。Go 里改了返回类型或输入类型，或者增删了 `page.tsx` 或 `layout.tsx` 之后，运行一次 `go tool tifa gen`，再去写用到新类型的 TypeScript：一批 Go 改动运行一次就够。`go tool tifa check` 和 `pnpm typecheck` 会替你运行它，`tifa dev` 运行期间，每次保存改动后它也会重写类型（Go 构建失败时保留旧的）。
- 默认导出是页面组件。可选的导出有 `head` 和 `ErrorBoundary`。404 也显示在 `ErrorBoundary` 里（见 [404](errors.zh-CN.md#404)）。
- 在浏览器里，每个 `page.tsx`，以及除根布局外的每个 `layout.tsx` 或 `layout_.tsx`，都是自成一体的分块，带着它导入的所有东西，包括 `head`。它在第一次需要它的路由时加载，路由器会等它加载完，才显示页面或运行 `head`。页面的 HTML 会预加载它的布局和路由的分块，所以它们和入口脚本一起到达。文件顶层的代码（包括只为副作用的导入）在那时运行，而不是在应用启动时，所以每个页面都需要的初始化，要放进 [`setup.ts`](hooks.zh-CN.md#setupts)，它在任何路由模块之前运行。见[浏览器里的路由模块](../build-and-deploy/build.zh-CN.md#route-modules-in-the-browser)。
- Go 的切片和 map 永远不会是 `null`：nil 切片到达时是 `[]`，nil map 是 `{}`，它们的 TypeScript 类型里没有 `| null`（见 [JSON 规则](../reference/json.zh-CN.md)）。
- 需要在模块顶层使用浏览器功能的组件，放进 `.client.tsx` 文件，用 `tifa/client` 的 `clientOnly` 懒加载它们。它们不会进入 SSR 包，首次绘制时显示占位内容。会渲染、但在浏览器绘制之前什么也不显示的组件也属于这里，见[只在浏览器里绘制的组件](server-render.zh-CN.md#components-that-draw-only-in-the-browser)：

```tsx
import { clientOnly } from 'tifa/client'

const OrderActions = clientOnly(() => import('./actions.client'), <button disabled>Loading…</button>)
```

## 显示应用信任的 HTML {#showing-html-the-app-trusts}

`tifa.HTML` 类型的字段保存页面原样显示的标记，比如你的构建把 Markdown 转成 HTML 得到的文章。用 `tifa/client` 的 `HTML` 组件来显示它：

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

- 标记不经转义就放进页面。`tifa.HTML` 只用于你自己写的或自己生成的 HTML。来自用户的 HTML 必须先用 sanitizer 清理，然后才能转成 `tifa.HTML`。
- 在 TypeScript 里，这个值的类型是 `HTML`，不是 `string`：用到字符串的地方，比如放进文本里或取 `.length`，TypeScript 会拒绝它。只用 `<HTML>` 来显示它。TypeScript 拦不住 `dangerouslySetInnerHTML`，而在服务端渲染的页面上，它在那里只是一个标记。`as` 选择容纳标记的元素：`div`、`article`、`section`、`main`、`aside`、`header`、`footer`、`nav`、`figure`、`blockquote`、`li`、`td`、`span` 或 `p`（默认是 `div`）。这些都是装普通内容的。内容会被执行、或者不是标记的元素，比如 `script`、`style`、`iframe` 或 `textarea`，会让清理 HTML 变得没有意义，所以 TypeScript 拒绝任何其他名字，`<HTML>` 遇到也会抛错。其他 props 交给那个元素，但 `children` 和 `dangerouslySetInnerHTML` 除外，即使它们是通过展开带进来的，`<HTML>` 也会去掉。
- 服务端渲染的页面里，每个值只带一份：在它的标记里。它用于 hydration 的数据里，那个位置只放一个小标记，`<HTML>` 从页面上把标记读回来。所以一篇文章不会发送两次。导航数据和 `SSR = false` 的页面带的是字符串。
- 所以，每次页面显示这个值时，都要在服务端渲染 `<HTML value={...}>`：不能只在某个 effect、一次点击或只有浏览器才有的状态之后渲染，也不能放在 `.client.tsx` 组件里。否则浏览器会抛出 `tifa: this tifa.HTML value came with the page, which carries it only in the markup the server rendered, and the server rendered no <HTML> for it here`。
- 标记里的链接和 `Link` 的行为一样：点击指向应用内页面的链接，会通过路由器显示那个页面，不重新加载整个文档，悬停或聚焦 50 毫秒，或者触摸，会预加载它。带修饰键或用其他鼠标键的点击、`target` 不是 `_self` 的链接、带 `download` 或 `rel="external"` 的链接、指向其他站点或其他协议（如 `mailto:`）的链接、指向当前页面 `#hash` 的链接、指向 public 文件的链接、`/assets/` 或 `/__tifa/` 下的路径，以及没有页面的路径（比如 API 路由），由浏览器自己跟随链接。你网站里应该整页加载的链接，比如指向同一个域名下不是 Tifa 的服务，请写上 `rel="external"`。`<HTML>` 只处理它自己元素内部的链接。你放在 `<HTML>` 上的 `onClick` 和其他处理函数先运行，调用了 `event.preventDefault()` 的就不让路由器介入。
- `tifa.HTML` 不能做 map 的键、处理函数的输入或会话的一部分。在那些地方用 `string`，清理之后再转换。`Load` 或 `Layout` 不能单独返回一个 `tifa.HTML`：请把它放进结构体字段里。

## Load 如何使用 Ctx {#how-load-may-use-ctx}

这一节讲 `Load`、`Layout` 和它们调用的函数。构建时，Tifa 从每个 `Load` 和 `Layout` 函数出发，找出它读取的路由参数（`c.Param`）和布局（`tifa.Use`）。查询参数来自第二个参数（见[查询参数](#query-parameters)）。当 `Load` 把 `*tifa.Ctx` 传给本模块里的函数时，Tifa 会一层一层地跟进这个调用，跨包也一样，并把找到的每个参数都算到这个 `Load` 头上。被多个 `Load` 函数调用的辅助函数，会算到它们每一个头上。Tifa 不会跟进它自己的函数。

为了让 Tifa 能找全，Ctx 只能用在 Tifa 看得见的地方。给 `c.Param` 传一个常量字符串，比如 `c.Param("order")`，并且直接调用它和 `tifa.Use`：把其中一个存进变量，或当成值传递，都会让构建失败。

能持有 Ctx 的类型只有这些：`*tifa.Ctx`、`tifa.Ctx`、本模块里声明的非泛型结构体（它的字段只通过这些类型持有 Ctx，直接、嵌入或间接都可以），以及指向这类结构体的指针。

持有 Ctx 的值，只能出现在这五个地方：

1. 作为直接方法调用的接收者，比如 `c.Param("order")` 或 `req.Page()`。
2. 作为参数传给本模块或 Tifa 里的非泛型函数，且该参数的类型完全相同。就地调用的函数字面量也算。
3. 在持有 Ctx 的结构体的复合字面量里，赋给完全相同类型的字段。
4. 用 `:=` 或 `var x T = ...` 赋给一个完全相同类型的新局部变量。这个变量遵循同样的规则。
5. 取它的地址，并且得到的指针仍然是可以持有 Ctx 的类型。

任何其他用法都会让构建失败，并指出文件和行号。比如：把它转换成另一个类型或接口，赋给已有的变量，一次赋多个值，`return` 它，放进切片、map、数组或 channel，`range`，包级变量，`defer`，`go` 语句，或把它传给泛型函数、接口方法、函数变量，或模块之外的函数（标准库和第三方库）。

`Load`、`Layout` 和它们调用的函数，不能调用 `c.Request()` 或 `c.Writer()`。Tifa 看不见通过原始请求读取的参数。请改用 `c.Param`、`c.Header`、`c.Cookie` 或 `c.Context()`，查询参数则作为第二个参数接收。`c.Request()` 和 `c.Writer()` 只给方法处理函数和 `OnRequest` 用。

在 `Load` 里要取页面的路径，用 `c.Path()`，比如在重定向到登录页的 `next` 地址里。它是访问者请求的路径，带语言前缀，不带查询，页面加载和导航的数据请求得到的都一样，所以不必为此去调用 `c.Request()`。需要把某个查询参数也放进这个地址的 `Load`，从它的第二个参数里取这个参数。`Layout` 和它调用的函数不能调用 `c.Path()`：布局的数据由它下面的每个页面共享，访问者在这些页面之间跳转时不会重新加载，所以它会一直保留第一次加载它的那个页面的路径。请在页面的 `Load`、`OnRequest` 或方法处理函数里调用它。

它们也不能调用 `c.Query`、`c.QueryValues`、`c.Bind` 或 `c.BindQuery`：查询参数是第二个参数的字段，这样 Tifa 和浏览器不用读代码就知道它，而且页面加载没有请求体可绑定。构建会让看得见的调用失败。对 `c.Query`、`c.QueryValues` 和 `c.BindQuery` 来说，看不见的调用，比如把 Ctx 存在全局变量里再取出来，会在运行时 panic，错误像 `Load` 的其他错误一样交给 `OnError`。这些方法留给方法处理函数（`c.Query` 和 `c.QueryValues` 也留给 `Validate` 和 `OnRequest`）。

遇到这些错误时，请直接调用 Ctx 的方法，或把它传给一个普通函数。如果只需要请求的 context，就传 `c.Context()`。只调用 `c.Context()` 的辅助函数（比如 `auth.User(c)`），Tifa 跟进它时什么也收集不到。

Tifa 只检查 `Load` 和 `Layout` 能到达的代码。如果你在 `OnRequest`、方法处理函数或别的任何地方，把 Ctx 存进全局变量或其他共享的地方，然后在 `Load` 或 `Layout` 里把它取出来，Tifa 就抓不住。不要这样做。
