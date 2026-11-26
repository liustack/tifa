---
summary: 'Link、navigate、useSearch 和带类型的查询参数'
audience: app
order: 5
translatedFrom: 73075cd05b99
read_when:
  - '给 `Link`、`navigate` 或 `useSearch` 传页面读取的查询参数，或者遇到关于 `search` 的类型错误'
  - '想知道为什么回到一个页面时先显示旧数据'
---

# 链接和导航 {#links-and-navigation}

## 路由器保留的数据 {#data-the-router-keeps}

路由器已经持有的数据遵循 TanStack 的默认值。它立刻算作过期，所以回到你访问过的页面时，会先显示缓存的数据，同时在后台重新加载（`GET <url>?_tifa=…`），悬停或聚焦时预加载的链接会被复用 30 秒。

## Link 和 navigate 里的查询参数 {#query-parameters-in-link-and-navigate}

`Load` 或 `Layout` 函数作为第二个参数接收的查询参数（见[查询参数](loading-data.zh-CN.md#query-parameters)），在浏览器里有字段的类型。`route.gen` 把它们导出为 `PageSearch` 和 `LayoutSearch`，路由也拿到它们，所以 TanStack 的 `Link`、`navigate` 和 `useSearch` 都会检查它们，并把它们原样交给页面：

```go
type Filter struct {
	Sort  string   `json:"sort"`
	Color []string `json:"color"`
	Page  int      `json:"page"`
}

func Load(c *tifa.Ctx, q Filter) (Products, error) {
	return products.List(q.Sort, q.Color)
}
```

```ts
// in route.gen.ts
export type PageSearch = {
  "color"?: string[]
  "page"?: number
  "sort"?: string
  [key: string]: unknown
}
```

```tsx
<Link to="/products" search={{ sort: 'price', color: ['red', 'blue'], page: 2 }}>…</Link>
<Link to="/products" search={{ page: '2' }}>…</Link> // type error: page is a number
<Link from="/products" to="/products" search={(prev) => ({ ...prev, page: (prev.page ?? 1) + 1 })}>Next</Link>
const search = page.useSearch() // PageSearch, with the parameters of the Layouts above
const number = search.page // a number, or undefined when the address has none
```

- 值的类型就是字段在 Go 里的类型：整数和浮点数是 `number`，`bool` 是 `boolean`，字符串是 `string`，切片是它们的数组。写入时也用这些类型（`page: 2`、`stock: true`），不要写成文本。每个键都可以省略，地址里没传的参数是 `undefined`，不是 Go 字段的零值。空值也一样：`?page=` 里没有 `page`。
- 路由器按字段的类型读地址里的值，不按别的，所以 `string` 字段会把 `?q=1.0` 保持为 `"1.0"`，把 `?q=007` 保持为 `"007"`。标量字段只有一个值，同名参数出现两次不合法。列表里每个同名参数是一个元素，只有一个也是。
- 不合法的值按和 Go 一样的规则拒绝（见[查询参数](loading-data.zh-CN.md#query-parameters)），所以 `?page=2.5`、`?stock=1` 和 `?page=abc` 都会让路由失败。数字的另一种写法（`?page=007`）是同一个数：路由把它读成 7，把地址写成 `?page=7`。页面的 `ErrorBoundary` 拿到一个 `TifaError`，状态是 400，消息和 Go 返回的一样，`invalid query parameter "page"`。`navigate` 写入不合法的值时也一样，类型会拦住这种写法，除非用类型转换绕开。
- 给 `search` 传的函数如果基于 `prev` 来写，要在 `Link` 上写 `from`，或者用 `useNavigate({ from })`，如上。不写的话，`prev` 里是应用里所有路由的参数，`prev.page` 没有有用的类型。
- 基于 `prev` 构建的链接，或者只写 `search` 的链接，会保留地址里的每个参数，包括没有 `Load` 读的参数，比如 `utm_source`。服务器渲染页面时没有这些参数，所以在这样的地址上，链接在浏览器里的 `href` 和 HTML 里的不同，见 [查询参数](../reference/memo.zh-CN.md#query-parameters)。要让两边一样，就写出链接保留的键：`search={(prev) => ({ sort: prev.sort, color: prev.color, page: 2 })}`。地址里参数的顺序没有影响：路由写每个地址时都按名字的顺序写参数，和服务器渲染时一样，并且在接管页面之前把地址栏也排成这个顺序。
- 一个 `Link` 的参数只要包含在地址的参数里，TanStack 就把它标成当前页（`active` 状态和 `aria-current="page"`）。在 `?page=2` 上，去第一页的链接没有 `page`，也会被标上。页码、排序选项这类只有参数不同的链接，加上 `activeOptions={{ exact: true }}`。
- 其他键是开放的，类型是 `unknown`：页面可以在 URL 里保留自己的参数，Go 不读它们，比如面板当前打开的标签。路由器让它们保持地址里的样子，字符串或字符串数组。所以拼错键名不是类型错误，只有 Go 读取的键的类型不对才是。服务端渲染看不到它们，见[服务端渲染里的查询参数](ssr-and-cache.zh-CN.md#query-parameters-in-the-server-render)。
- 每个路由有它自己的 `Load` 或 `Layout` 的类型，TanStack 会加上上层布局的那些。布局和它下面的页面都声明的同名参数只有一个类型，构建会检查。一个什么都不读的 `Load` 没有类型，它的路由接受任何键。
- 路由会得到一个由字段类型生成的 `validateSearch`，路由器读每个地址时都会运行它。它也把值原样交给页面，所以组件里不需要 `Number(search.page)` 或 `search.stock === 'true'`。
