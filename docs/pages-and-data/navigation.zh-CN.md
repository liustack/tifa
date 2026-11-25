---
summary: 'Link、navigate、useSearch 和带类型的查询参数'
audience: app
order: 5
translatedFrom: 8937593f2ff0
read_when:
  - '给 `Link`、`navigate` 或 `useSearch` 传页面读取的查询参数，或者遇到关于 `search` 的类型错误'
  - '想知道为什么回到一个页面时先显示旧数据'
---

# 链接和导航 {#links-and-navigation}

## 路由器保留的数据 {#data-the-router-keeps}

路由器已经持有的数据遵循 TanStack 的默认值。它立刻算作过期，所以回到你访问过的页面时，会先显示缓存的数据，同时在后台重新加载（`GET <url>?_tifa=…`），悬停或聚焦时预加载的链接会被复用 30 秒。

## Link 和 navigate 里的查询参数 {#query-parameters-in-link-and-navigate}

`Load` 或 `Layout` 函数作为第二个参数接收的查询参数（见[查询参数](loading-data.zh-CN.md#query-parameters)），在浏览器里有类型。`route.gen` 把它们导出为 `PageSearch` 和 `LayoutSearch`，路由也拿到它们，所以 TanStack 的 `Link`、`navigate` 和 `useSearch` 都会检查它们：

```go
type Filter struct {
	Sort  string   `json:"sort"`
	Color []string `json:"color"`
}

func Load(c *tifa.Ctx, q Filter) (Products, error) {
	return products.List(q.Sort, q.Color)
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
const sort = [search.sort].flat()[0] // the first value
```

- 同一个键在 URL 里可以出现多次，所以读取时，值是 `string` 或 `string[]`，不管字段在 Go 里是什么类型。字段不是切片而同名参数出现两次，Go 会返回 400。写入时，一个字符串就够了。每个键都可以省略。
- 其他键是开放的：页面可以在 URL 里保留自己的参数，Go 不读它们，比如面板当前打开的标签。它们同样是字符串或字符串数组。所以拼错键名不是类型错误，只有 Go 读取的键的类型不对才是。服务端渲染看不到它们，见[服务端渲染里的查询参数](ssr-and-cache.zh-CN.md#query-parameters-in-the-server-render)。
- 只能放字符串。路由器把每个值写成文本，Go 按[查询参数](loading-data.zh-CN.md#query-parameters)的规则读文本，所以是 `'2'`，不是 `2`，`bool` 字段用 `'true'` 或 `'false'`。不是唯一合法写法的文本，比如 `'02'`，会得到 400。
- 类型来自结构体的字段，每个路由有它自己的 `Load` 或 `Layout` 的那些。TanStack 会加上上层布局的那些。一个什么都不读的 `Load` 没有类型，它的路由接受任何键，和以前一样。
- 浏览器里不会运行任何东西来检查这些值：路由只拿到类型（`searchTypes` 返回 `undefined`，TanStack 就按原样读取查询）。
