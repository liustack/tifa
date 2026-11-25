---
summary: '怎样写路由的 React 部分：page.tsx、route.gen、Link 类型、head、404、ErrorBoundary'
audience: app
order: 2
translatedFrom: b4de6c5a72f8
read_when:
  - '编写 page.tsx 或 layout.tsx，或者使用 `./route.gen` 导出的东西'
  - '给 `Link`、`navigate` 或 `useSearch` 传页面读取的查询参数，或者遇到关于 `search` 的类型错误'
  - '用 `tifa.HTML` 和 `<HTML>` 显示应用自己生成或信任的 HTML（比如文章），或者处理其中的链接'
  - '在页面里调用方法处理函数，或者页面没有显示一次写操作带来的变化'
  - '设置页面的标题、meta 标签或 `<html>` 属性，或者 404 页面和错误页面的 head'
  - '用 `layout_.tsx` 给站点的一部分单独的外框'
  - '在页面里显示 404 或错误，或者想知道哪个 `ErrorBoundary` 会显示'
---

# 页面与布局 {#pages-and-layouts}

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

- 只管 UI，不加载数据。`./route.gen` 是生成的模块，每个文件夹一个，导出 `page` 和 `layout`（页面和布局的 TanStack 路由，只在文件夹里有它们时才有）、`PageData` 和 `LayoutData`（`Load` 和 `Layout` 的返回类型）、`PageSearch` 和 `LayoutSearch`（它们读取的查询参数，见 [Link 和 navigate 里的查询参数](#query-parameters-in-link-and-navigate)）、`Params`、它们所含 Go 结构体的类型（见下文），以及每个方法处理函数对应的一个函数。同一文件夹里的子组件也从 `./route.gen` 导入。布局的数据来自它所在文件夹的 `route.gen`：`layout.tsx` 从 `'./route.gen'` 导入 `layout`，页面则用 `'../../route.gen'` 里的 `layout` 读取它的项目布局，举例来说。有人导入某个文件夹的 `route.gen`，而该文件夹没有生成的模块时，构建会失败。
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
- 默认导出是页面组件。可选的导出有 `head` 和 `ErrorBoundary`。404 也显示在 `ErrorBoundary` 里（见 [404](#404)），导出 `NotFound` 会让构建失败。
- 在浏览器里，每个 `page.tsx`，以及除根布局外的每个 `layout.tsx` 或 `layout_.tsx`，都是自成一体的分块，带着它导入的所有东西，包括 `head`。它在第一次需要它的路由时加载，路由器会等它加载完，才显示页面或运行 `head`。页面的 HTML 会预加载它的布局和路由的分块，所以它们和入口脚本一起到达。文件顶层的代码（包括只为副作用的导入）在那时运行，而不是在应用启动时，所以每个页面都需要的初始化，要放进 [`setup.ts`](hooks.zh-CN.md#setupts)，它在任何路由模块之前运行。见[浏览器里的路由模块](../build-and-deploy/build.zh-CN.md#route-modules-in-the-browser)。
- Go 的切片和 map 永远不会是 `null`：nil 切片到达时是 `[]`，nil map 是 `{}`，它们的 TypeScript 类型里没有 `| null`（见 [JSON 规则](routes.zh-CN.md#json-rules)）。
- 需要在模块顶层使用浏览器功能的组件，放进 `.client.tsx` 文件，用 `tifa/client` 的 `clientOnly` 懒加载它们。它们不会进入 SSR 包，首次绘制时显示占位内容。会渲染、但在浏览器绘制之前什么也不显示的组件也属于这里，见[只在浏览器里绘制的组件](server-render.zh-CN.md#components-that-draw-only-in-the-browser)：

```tsx
import { clientOnly } from 'tifa/client'

const OrderActions = clientOnly(() => import('./actions.client'), <button disabled>Loading…</button>)
```

- Tifa 从不替你重新加载页面数据。在一次写入（处理函数或你自己的 `fetch`）之后，页面显示的还是原来的内容，直到你提出要求：调用 TanStack 的 `router.invalidate()`，重新加载当前页面及其所有布局的数据，合并成一个请求。`await router.invalidate({ sync: true })` 在页面显示新数据之后才完成。普通的 `await router.invalidate()` 会立刻完成，页面在后台更新。见[从浏览器调用方法处理函数](#calling-method-handlers-from-the-browser)。
- 路由器已经持有的数据遵循 TanStack 的默认值。它立刻算作过期，所以回到你访问过的页面时，会先显示缓存的数据，同时在后台重新加载（`GET <url>?_tifa=…`），悬停或聚焦时预加载的链接会被复用 30 秒。

### Link 和 navigate 里的查询参数 {#query-parameters-in-link-and-navigate}

`Load` 或 `Layout` 函数读取的查询参数，在浏览器里有类型。`route.gen` 把它们导出为 `PageSearch` 和 `LayoutSearch`，路由也拿到它们，所以 TanStack 的 `Link`、`navigate` 和 `useSearch` 都会检查它们：

```go
func Load(c *tifa.Ctx) (Products, error) {
	return products.List(c.Query("sort"), c.QueryValues("color"))
}
```

```ts
// in route.gen.ts
export type PageSearch = {
  "color"?: string | string[]
  "sort"?: string | string[]
  [key: string]: string | string[] | undefined
}
```

```tsx
<Link to="/products" search={{ sort: 'price', color: ['red', 'blue'] }}>…</Link>
<Link to="/products" search={{ sort: 2 }}>…</Link> // type error: the value is a string
navigate({ to: '/products', search: (prev) => ({ ...prev, sort: 'name' }) })
const search = page.useSearch() // PageSearch, with the parameters of the Layouts above
const sort = [search.sort].flat()[0] // the first value, as c.Query reads it
```

- 同一个键在 URL 里可以出现多次，所以读取时，值是 `string` 或 `string[]`，不管 Go 用的是 `c.Query` 还是 `c.QueryValues`。Go 里的 `c.Query` 取第一个值。写入时，一个字符串就够了。每个键都可以省略。
- 其他键是开放的：页面可以在 URL 里保留自己的参数，Go 不读它们，比如面板当前打开的标签。它们同样是字符串或字符串数组。所以拼错键名不是类型错误，只有 Go 读取的键的类型不对才是。服务端渲染看不到它们，见[服务端渲染里的查询参数](ssr-and-cache.zh-CN.md#query-parameters-in-the-server-render)。
- 只能放字符串。路由器把每个值写成文本，Go 读的也是文本，所以是 `'2'`，不是 `2`，是 `'1'` 还是 `'true'`，看你的 Go 代码怎么比较。
- 类型来自 Go 读取的名字，经过辅助函数和其他包读取的也算，每个路由有它自己的 `Load` 或 `Layout` 的那些。TanStack 会加上上层布局的那些。一个什么都不读的 `Load` 没有类型，它的路由接受任何键，和以前一样。
- 浏览器里不会运行任何东西来检查这些值：路由只拿到类型（`searchTypes` 返回 `undefined`，TanStack 就按原样读取查询）。

### 显示应用信任的 HTML {#showing-html-the-app-trusts}

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

### 从浏览器调用方法处理函数 {#calling-method-handlers-from-the-browser}

有方法处理函数的文件夹，它的 `route.gen` 模块会为每个处理函数导出一个函数，以它的方法命名。API 路由也有 `route.gen` 模块，所以页面可以导入任何文件夹的处理函数：

```tsx
import { POST } from './route.gen'
import { TifaError } from 'tifa/client'

async function ship() {
  try {
    const shipment = await POST({ params: { project, order }, input: { carrier: 'SF' } })
  } catch (e) {
    if (e instanceof TifaError) setErrors(e.fields) // e.status, e.message, e.fields
  }
}
```

- 函数接受一个对象。`params` 放路径参数，文件夹没有路径参数时可以省略。`name++` 文件夹的键是 `_splat`。会让请求发到别的路径的值，在发出任何请求之前就会被拒绝，并让 promise 被 reject：`name+` 参数的空值、`.` 或 `..`，以及 `_splat` 的 `.` 或 `..` 段，或者结尾之前的空段。空的 `_splat` 没问题。`input` 是处理函数的输入，处理函数没有输入时可以省略。`signal` 用来中止请求。
- GET 和 DELETE 把输入放在查询字符串里，其他方法放在 JSON 请求体里，输入里有文件时用 multipart 表单（见[输入里的文件](routes.zh-CN.md#files-in-the-input)）。结果的类型来自 Go 的结果，遇到重定向（见下）时是 `| undefined`：`func(c, in) (Out, error)` 得到 `Promise<POSTOutput | undefined>`。`func(c, in) error` 得到 `void`。`func(c) error` 自己写响应，所以它的函数原样返回 `Response`，错误状态也一样。
- 一次调用只发送它的请求，如果响应带着会话，就应用它（见下）。无论成功还是失败、无论哪种方法，它都从不重新加载页面的数据。页面需要显示这次写入的改动时，在它之后调用 `router.invalidate()`（见下面的例子）。
- 错误响应会抛出 `TifaError`，带 `status`、`message` 和 `fields`（`tifa.Fields` 的消息，否则为空）。网络故障的抛法和 `fetch` 一样。来自比服务器更旧的构建的页面发起的调用，会以状态 409 失败，见[部署期间打开的页面](../build-and-deploy/deploy.zh-CN.md#pages-open-while-you-deploy)。
- 当处理函数或 `OnRequest` 返回 `tifa.Redirect(url, status)` 时，浏览器会前往那个 URL：本站的 URL 通过路由器，不重新加载页面，其他站点则整页加载。新页面显示路由器已有的内容，并加载缺少的部分，和任何导航之后一样。重定向到正在显示的页面时，停留在原处，什么也不加载。路径、hash 和查询值都一致时，就算同一个页面，不管查询的顺序和编码。promise 在导航结束后以 `undefined` resolve，有结果的处理函数也一样。一次调用有没有被重定向，只有看了响应才知道，因为 `OnRequest` 可以重定向任何请求，所以每个带结果的处理函数的函数，其结果类型都包含 `undefined`。自己写响应的处理函数，它的函数拿到的是重定向的 `Response`（`ok`，状态在 `X-Tifa-Redirect` 里）。相对 URL 按处理函数的 URL 解析，而不是按页面的 URL。处理函数自己通过 `c.Writer()` 写的 3xx，或者代理写的 3xx，在浏览器里读不到，会让 promise 以一个 `Error` reject。
- 让某人登录或退出的调用，会在响应里带回新的会话，浏览器里的会话副本随之更新（见[浏览器里的会话](sessions.zh-CN.md#in-the-browser)）。
- 请在事件处理函数或 effect 里调用这些函数。在页面于服务器上渲染时调用会抛错。
- `route.gen` 还为每个有这些类型的处理函数导出 `POSTInput`、`POSTOutput` 等类型。

`tifa.Redirect` 返回的错误是 `*tifa.RedirectError`，带字段 `URL` 和 `Status`。在 `OnRequest` 或测试里，用 `errors.As` 来识别它。

要前往处理函数创建出来的内容的页面，可以让处理函数返回重定向，或者返回结果后自己导航。一次调用不会重新加载任何东西，所以你离开的页面不会在刚好之前被再加载一次。页面自己显示的写入，调用之后仍留在屏幕上。用 `router.invalidate()` 来要求更新：

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
const router = useRouter() // from @tanstack/react-router
// in the event handler, where POST returns (Todo, error):
const todo = await POST({ input: { title } })
if (todo) await router.navigate({ to: '/todos/$todo', params: { todo: todo.id } }) // undefined: redirected
```

## 布局 {#layout}

布局放在它自己的文件夹里，在该文件夹的页面旁边（如果有页面的话）：`route.go` 里的 `Layout` 函数和 `layout.tsx` 包住同一文件夹里的页面和它下面的每个页面。`layout.tsx` 用 `<Outlet />` 放置子路由。两者都可以省略：没有 `layout.tsx` 时子路由直接渲染，没有 `Layout` 函数时这个布局没有数据。一个在它的文件夹里和它下面都没有页面的布局，会让构建失败。

```go
package project

func Layout(c *tifa.Ctx) (projects.Project, error) {
	return projects.GetProject(c.Param("project"))
}
```

- `Layout` 放在 `route.go` 里，与同一文件夹的 `Load`、`SSR`、`Memo`、`Cache` 和方法处理函数放在一起。在包里的任何其他文件里（比如 `layout.go`）声明它，构建都会失败。只有 `Layout` 的 `route.go` 不是 API 路由。
- 布局的 `c.Param` 只能读取它自己文件夹路径里的参数。`app/projects/project+/route.go` 里的 `Layout` 可以读 `project`，不能读 `order`。页面的 `Load` 同样被限制在它自己的路径上。
- 在同一个布局下的页面之间切换时，布局组件不会重新挂载，保持它的状态。当布局函数读取的路径参数变了（比如你转到另一个项目），布局会重新挂载，它的状态重置。查询参数变了时，即使是它读取的，布局也会重新加载它的数据并渲染，但不会重新挂载，保持它的状态，这是 TanStack Router 的默认行为。所以过滤条件会保持访客选的视图。组件根据 `Load` 的数据设置的状态，比如用 `useState(data.x)`，也不会因为新的查询而重置。需要它跟随新数据的地方，给组件一个来自这个值的 `key`，或者在 effect 里更新状态。页面的工作方式相同。
- 页面和布局可以调用 `tifa.Use(c, project.Layout)`，为当前请求拿到包住它们的某个布局的结果：同一文件夹或上层文件夹的布局，除非中间的 `layout_.tsx` 把它去掉了。同一个 `Layout` 在每个请求里只运行一次。任何其他布局都会让构建失败，参数不是包级的 `Layout` 函数（比如闭包）也一样。`Layout` 函数不能使用它自己的布局，不管是直接还是通过辅助函数。
- 在浏览器里导航时，导航数据请求只运行所列路由的 `Load` 和 `Layout` 函数。如果某个上层布局的数据还在缓存里，那个布局就不在列表里，它的 `Layout` 这次不会运行。所以不要只在布局里做权限检查。把它们放进 `OnRequest`，或者让页面的 `Load` 通过 `tifa.Use` 依赖做检查的布局，这样检查在每个请求上都会运行。
- 文档外壳（`<html>`、`<head>`、资源和注入数据的脚本）由框架当作字符串写出。React 只渲染 `<body>` 里的应用根节点，所以不管哪一层失败或返回 404，页面仍然是完整的文档。根布局（`app/layout.tsx`）可以导出 `documentHead` 来定制外壳，格式与 `head` 的返回值相同。每个页面都带它的标签（没有路由匹配或根 `Layout` 失败时也带），它的 `htmlAttributes` 是每个页面起步用的 `<html>` 属性（没有时是 `{ lang: 'en' }`，路由的 `head` 可以为它的页面改动它们，见下）。把网站默认的 `title` 放在这里，这样没匹配到的 URL 的 404 页面也有标题。页面自己的 `head` 返回了 `title` 时，会覆盖它。导出 `Document` 组件会让构建失败。

```tsx
export const documentHead = {
  htmlAttributes: { lang: 'zh-CN' },
  meta: [{ title: 'Shop' }, { name: 'theme-color', content: '#f2f4f5' }],
  links: [{ rel: 'icon', href: '/favicon.svg' }],
}
```

- 页面的 head 来自匹配到的各路由的 `head` 返回值：`title` 和同名的 meta 标签取自拥有它们的最深一层路由，它写出的每一个都保留（每个 `media` 一个 `theme-color`，多个 `og:image`），link、style 和 script 标签按路由顺序去重。框架总是加上 `<meta charset="utf-8">` 和一个 viewport meta，路由的 `name: 'viewport'` meta 会替换后者。标签属性按 React DOM 在服务器上写出它们的方式来写：`className`、`htmlFor`、`httpEquiv`、`crossOrigin` 等变成它们的 HTML 名字。以 `on` 开头的事件属性、无效的属性名、`null`、`undefined` 和函数都不写出。`async` 和 `defer` 这样的布尔属性在为 true 时写成空值。对于其他属性，只有 `data-` 和 `aria-` 属性才会把布尔值写成字符串。`javascript:` 链接会被替换。head 标签不支持 `style` 属性，用了会让渲染失败。`htmlAttributes` 遵循同样的规则。style 和 script 标签的内容原样写出。只有会结束该元素的 `<script` 和 `</script`（在 style 里是 `<style` 和 `</style`）会被转义，做法和 React DOM 一样。浏览器导航之后和 `router.invalidate()` 之后，head 会更新以匹配新的路由。
- `head` 也可以返回 `htmlAttributes`，也就是它的页面的 `<html>` 属性，比如主题 class 或 `data-` 属性。它们逐个属性地合并到根布局 `documentHead` 里的 `htmlAttributes` 之上：给出某个属性的最深一层路由胜出，`undefined` 把它留给上层路由，`null` 把它去掉。属性按 React DOM 写出的名字来匹配，所以一个路由里的 `className` 和另一个里的 `class` 是同一个属性。`class` 整个被替换，从不和别处的值合并。`style` 不支持，和 head 的标签一样。它们按与 head 标签相同的规则写出。浏览器导航之后和 `router.invalidate()` 之后，它们也会跟随新的路由。在浏览器里，只有当 Tifa 想要的值变了，它才写 `<html>` 的属性，也只去掉它自己写的属性。所以页面上脚本设置的东西，比如深色模式脚本加的 `dark` class，或者翻译工具改的 `lang`，在 hydration 和导航期间会保留，只要各路由想要的是这个属性的同一个值。当这个值变了，比如某个路由设置了另一个 `class`，或者一次导航离开它，根布局的 `class` 回来了，Tifa 会写入它，脚本加的 class 就没了。请把这样的主题放在没有路由设置的属性里，比如 `data-theme`，或者让脚本再加一次。没有 SSR 的页面在浏览器里得到这些属性。

```tsx
// app/guide/layout.tsx. Layout in route.go returns the theme of the guide.
export const head = ({ loaderData }: { loaderData?: LayoutData }) => ({
  htmlAttributes: { 'data-theme': loaderData?.theme },
})
```

- 起作用的是那些 head 在页面上的路由。布局下面的 404 或错误保留布局的属性，布局下面没有任何页面的 URL 也一样，因为它的 404 显示在布局里面（见 [404](#404)）。布局本身失败时，它的 `head` 在没有数据的情况下运行，所以上面的例子会保留根布局的 `data-theme`。
- 在有多种语言的网站上，Tifa 自己写 `lang`，也就是页面的语言。在那里，`htmlAttributes` 里有 `lang` 会让构建失败（见[多语言网站](../start/app.zh-CN.md#a-site-in-several-languages)）。
- `htmlAttributes` 必须是对象，其他值 `pnpm typecheck` 会报告。运行时，这样的值就像 `head` 抛出的错误一样失败：服务端渲染作为意外错误失败（`OnError`，或 500），消息在日志里，在 `tifa dev` 下还在页面上。在浏览器里，导航之后或在没有 SSR 的页面上，路由器会把错误写到控制台，并丢掉那个路由的整个 head。

### 标题和 meta 标签 {#title-and-meta-tags}

页面的标题和 meta 标签写在它的 `head` 导出里（或根布局的 `documentHead` 里），不要在组件里写成 `<title>` 或 `<meta>`。React 19 允许在组件里写它们，并在浏览器里把它们移进 head，但 Tifa 把文档的 head 当字符串写出，React 只渲染 `<body>` 里的应用根节点。所以服务器 HTML 里这个标签落在 `<body>` 里，爬虫和分享卡片不读那里，页面还可能出现两份描述。

```tsx
// not this
export default function Page() {
  return <><title>Pour-over kettle</title><p>...</p></>
}

// this
export const head = () => ({ meta: [{ title: 'Pour-over kettle' }] })
```

`tifa check`、`tifa build` 和 `tifa dev` 会对用 JSX 写的 `<title>` 或 `<meta>` 给出警告，带文件和行号。`<svg>` 里的 `<title>` 是图形的提示文字，没有问题，`<meta itemProp>` 也一样。

### 不带上层布局的布局 {#a-layout-without-the-layouts-above}

把组件文件命名为 `layout_.tsx` 而不是 `layout.tsx`，这个文件夹的布局就会显示它的页面，而不带它上面的任何布局，根布局也包括在内。只有框架写出的文档外壳留在它们周围。[examples/shop](../../examples/shop) 的店面就是这样工作的：它自己画页眉和页脚，根布局给其他页面画的工作台，它一点也没有。

```text
app/
  route.go  layout.tsx                the workbench: sidebar, top bar, the list of shops
  projects/
    project+/
      route.go  layout.tsx  page.tsx  inside the workbench
  store/
    layout_.tsx                       the storefront starts here, without the workbench
    _locales/                         zh-CN.json and en-US.json
    route.go  page.tsx                the storefront
    cart/
      route.go  page.tsx              inside the storefront
```

- 它下面的页面，更深层的也一样，不会渲染它上面的布局，上面那些布局的 `Layout` 函数也不会为它们运行：不为页面，不为浏览器的导航数据请求，也不为它下面没有页面的 URL 的 404。所以它们的数据永远到不了这些页面。
- 同一文件夹 `route.go` 里的 `Layout` 照常给它数据。它下面的文件夹照常嵌套：更深一层的 `layout.tsx` 显示在它里面，它的 `Layout` 会运行。更深一层的 `layout_.tsx` 则依次把它连同它上面的一切都去掉。
- 属于文档的东西仍然适用：根布局的 `documentHead`（连同它的 `htmlAttributes`），以及 `OnRequest`。属于上层布局的则不适用：它们的 `head` 和它们的 `ErrorBoundary`。当 `layout_.tsx` 的 `Layout` 失败，或返回 `tifa.ErrNotFound` 时，它上面没有布局来显示错误，所以显示框架的默认页面。它自己的 `ErrorBoundary` 显示它下面失败的东西，和任何布局一样。要在那个 `Layout` 失败时显示你自己的页面，把 `layout_.tsx` 放高一层文件夹，带上 `<Outlet />` 和 `ErrorBoundary`。
- 在浏览器里，从它下面的页面移到它外面的页面，会换掉整个框架，新页面的布局在路由器还没有它们的数据时加载数据。
- 构建会在这些情况下失败：`app/` 里有 `layout_.tsx`（它上面没有布局），同一文件夹里同时有 `layout.tsx` 和 `layout_.tsx`，以及它下面的页面或布局对被某个 `layout_.tsx` 去掉的布局使用 `tifa.Use`。它也会在一个布局的页面全都在某个 `layout_.tsx` 下面时失败，因为它一个也没有包住。
- React Router 和 TanStack Router 也用 `_` 结尾的名字来让路由不进入某个布局，但只是不进入它前一段的布局：根路由和更上层的布局仍然包着它。Tifa 的 `layout_.tsx` 会去掉它上面的每个布局，根布局也不例外。

## 404 {#404}

404 显示在路由的 `ErrorBoundary` 里（见[页面上的错误](#errors-on-a-page)），它收到的是 `status` 为 404 的 `TifaError`：

```tsx
import { TifaError } from 'tifa/client'

export function ErrorBoundary({ error }: { error: unknown }) {
  if (error instanceof TifaError && error.status === 404) return <p>No such order</p>
  return <p>Something went wrong</p>
}
```

| 情形 | 显示什么 |
| --- | --- |
| 页面的 `Load` 返回 `tifa.ErrNotFound` | 从该页面往上数的第一个 `ErrorBoundary`。外层的布局照常显示 |
| `Layout` 函数返回 `tifa.ErrNotFound` | 从该布局的上一层布局往上数的第一个 `ErrorBoundary` |
| URL 没有匹配任何页面 | 从 URL 匹配到的最深布局往上数的第一个 `ErrorBoundary`，显示在那个布局里面。从最外层到它的各个布局照常运行和显示：从根布局开始，或者从它上面的 `layout_.tsx` 开始 |
| 没有任何路由有 `ErrorBoundary` | 框架默认的 404 页面 |

在所有这些情形里，状态码都是 404，在浏览器里导航之后也一样，那时它的数据请求用各布局的数据和状态 404 来回答。

最后一段带文件扩展名（结尾是一个点加 1 到 8 个字母或数字，比如 `/wp-login.php`）的、没匹配上的 URL，或者有以点开头的段（`/.env`、`/.git/config`、`/.well-known/missing`）的 URL，不会得到 404 页面。它得到的是和 `/assets/` 下缺失文件一样的纯文本 404，没有 `Load`，也不渲染，所以请求这类路径的扫描器几乎不花什么代价。`OnRequest` 仍然运行，并从 `next` 看到 `tifa.ErrNotFound`。路由匹配的 URL 不受影响，包括捕获所有路径的路由和带点的参数（`/users/{name}` 的 `/users/john.doe`），导航数据请求也不受影响。`/.well-known/` 下的路由或 public 文件仍然正常响应。

要找到没有匹配任何页面的 URL 所属的布局，Tifa 把 URL 和每个布局与页面的路径从左到右逐段比较。字面段胜过参数，参数胜过路径的结尾。匹配得最多的路径胜出，页面算作它所在的那个布局。有这些文件夹时：

```text
app/layout.tsx
app/teams/team+/layout.tsx
app/teams/team+/members/page.tsx
app/teams/new/page.tsx
```

| URL | 404 显示在哪个布局里 |
| --- | --- |
| `/nowhere` | 根布局 |
| `/teams/blue/missing` | team 布局，它的 `Layout` 读到的 `c.Param("team")` 是 `blue` |
| `/teams/blue/members/x` | team 布局 |
| `/teams/new/x` | 根布局：`new` 是字面段，所以 `teams/new` 胜过 `teams/team+`，而那个页面在根布局里 |

- `name++` 文件夹里的页面接管它下面的每个 URL，所以这样的 URL 匹配的就是那个页面，由它的 `Load` 决定怎么回答。
- 对于没有匹配任何页面的 URL，`OnRequest` 运行时 `c.Pattern()` 为空。`next(c)` 运行各布局并返回 `tifa.ErrNotFound`。当 `OnRequest` 自己返回 `tifa.ErrNotFound`、没有调用 `next` 时，不会运行任何布局，页面的显示和那个 URL 最外层的 `Layout` 返回 `tifa.ErrNotFound` 时一样：根布局，或者它上面的 `layout_.tsx` 的布局。
- 布局的 `ErrorBoundary` 只会显示在它下面某个路由的位置上，也就是布局的 `Outlet` 里，所以它能读到布局的数据和布局组件提供的上下文。布局失败时，显示的是上层布局的 `ErrorBoundary`，根布局或 `layout_.tsx` 里的布局失败时，显示框架的默认页面。
- 在 `page.tsx` 或 `layout.tsx` 里导出 `NotFound` 会让构建失败。把它显示的内容移到 `ErrorBoundary` 里，放在 `error.status === 404` 下面。导出 `Error` 也会让构建失败：把它改名为 `ErrorBoundary`。

## 页面上的错误 {#errors-on-a-page}

`page.tsx` 和 `layout.tsx` 可以导出 `ErrorBoundary`，当路由的 `Load` 或 `Layout` 失败、当 404 显示在那里（见 [404](#404))，或者当它下面的某个组件抛错时，显示在该路由的位置上：

```tsx
import { TifaError } from 'tifa/client'

export function ErrorBoundary({ error }: { error: unknown }) {
  if (error instanceof TifaError && error.status === 403) return <p>No access</p>
  return <p>Something went wrong</p>
}
```

- 失败的 `Load` 或 `Layout` 以 `TifaError` 的形式到达 `ErrorBoundary`，带 `status`、公开消息和空的 `fields`（`tifa.Error` 的消息，或 `OnError` 返回的内容）。在服务器上和 hydration 之后都一样。
- 没有自己 `ErrorBoundary` 的路由，用最近的、导出了 `ErrorBoundary` 的祖先的那个，再往上是框架的默认值。祖先到 `layout_.tsx` 为止。
- 页面的状态码是最外层失败路由的状态码。
- 页面在服务器上渲染时抛错的组件，是意外错误：`OnError` 看到它一次，并选定状态（没有 `OnError` 时是 500）和公开消息。然后页面带着这个错误重新渲染，错误作为 `TifaError` 放在页面的位置上，所以从页面往上数的第一个 `ErrorBoundary` 把它显示在各布局里面。如果这次渲染也失败了，因为 `ErrorBoundary` 或它外面的某个布局抛了错，错误就上移一个路由，放在页面所在布局的位置上，依此类推，直到最外层的布局，也就是根布局或某个 `layout_.tsx`。只有放在那个布局位置上的错误也失败时，浏览器才会得到 Go 返回的、带状态和公开消息的纯错误页面。浏览器按服务器放置错误的位置来 hydrate 页面，不会再运行抛错的那个路由。浏览器或 CDN 都不保存这样的响应（`Cache-Control: no-store`）。构建出来的应用会改为把它记住 5 秒：这段时间内，对同一页面、同样的数据和会话的请求，得到同样的响应，不再渲染，`X-Tifa-Render` 是 `memo`。同时在等这个页面的请求共用放置错误的那些渲染。`OnError` 仍然会看到每个请求，第一个看到完整的错误，其他的看到它的第一行，以及一条说明没有再次渲染的注释。`tifa dev` 什么也不记，所以修复在下一次重新加载时就能看到。堆栈只在 `tifa dev` 下显示，盖在带 `ErrorBoundary` 的页面上，你保存修复后页面会重新加载（见 [`tifa dev`](../build-and-deploy/dev.zh-CN.md#when-a-page-fails-to-render)）。
- 用光 `ssr.vm_memory` 或超过 `ssr.timeout` 的服务端渲染（见 [config.md](../build-and-deploy/config.zh-CN.md)）会当场停止。你代码里或库里的 `catch`、`finally` 或 promise 处理函数都不会为它运行，所以页面没法捕获它去显示别的东西。VM 会被换成一个全新的。用光 `ssr.vm_memory` 的渲染，会在全新的 VM 上带着这个错误重新渲染一次，放在最外层布局的位置上，因为同样的数据在任何一层都会用光内存。它的响应像上面那样被记住。超过 `ssr.timeout` 或发现每个 VM 都忙的渲染则不会：Go 立刻返回它的纯错误页面。一个请求的所有渲染共用一个 `ssr.timeout`，从第一次算起，带着错误的渲染如果超出它，尝试就到此为止。无限递归的代码会得到 `RangeError: Maximum call stack size exceeded`，和在 Node 里一样，`catch` 可以接住它（见[栈深度](../build-and-deploy/config.zh-CN.md#stack-depth)）。没有人接住时，它和其他任何错误一样让渲染失败。
- 显示错误的页面得到的是 `Load` 失败的页面的 head。被错误替换的那个路由在没有数据的情况下运行它的 `head`，它下面的路由不加标签，它上面每个路由的 `head` 会在现在的匹配结果上再运行一次，所以 `status` 会告诉它有某个路由失败了（见[多语言网站](../start/app.zh-CN.md#a-site-in-several-languages)）。某个路由为没渲染成功的那个页面给出的标签和 `htmlAttributes`，不会出现在错误页面上。浏览器在 hydrate 时算出同样的 head。
- 浏览器接管之后，抛错的组件会显示最近的 `ErrorBoundary`，和失败的 `Load` 一样。
- 不管什么失败，文档外壳都保持完整，所以页面仍然会加载它的脚本和样式。
