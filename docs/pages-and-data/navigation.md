---
summary: 'Link, navigate, useSearch and typed query parameters'
audience: app
order: 5
read_when:
  - Giving `Link`, `navigate` or `useSearch` the query parameters a page reads, or a type error about `search`
  - Wondering why going back to a page shows old data first
---

# Links and navigation

## Data the router keeps

Data the router already holds follows TanStack's defaults. It counts as stale at once, so going back to a page you visited shows the cached data and loads it again in the background (`GET <url>?_tifa=…`), and a link preloaded on hover or focus is reused for 30 seconds.

## Query parameters in Link and navigate

The query parameters a `Load` or `Layout` function reads have types in the browser. `route.gen` exports them as `PageSearch` and `LayoutSearch`, and the route gets them, so TanStack's `Link`, `navigate` and `useSearch` check them:

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

- A key can come more than once in a URL, so a value is a `string` or a `string[]` when you read it, whether Go reads it with `c.Query` or `c.QueryValues`. `c.Query` in Go takes the first value. Writing one, a single string is enough. Every key may be left out.
- Other keys are open: a page may keep parameters of its own in the URL that Go does not read, such as the open tab of a panel. They are strings or arrays of strings too. So a misspelled key is not a type error, only a wrong type for a key Go reads is. The server render does not see them, see [Query parameters in the server render](ssr-and-cache.md#query-parameters-in-the-server-render).
- Only strings go in. The router writes every value as text, and Go reads text, so `'2'`, not `2`, and `'1'` or `'true'` as your Go code compares it.
- The types come from the names Go reads, also through helpers and other packages, and each route has those of its own `Load` or `Layout`. TanStack adds those of the Layouts above. A `Load` that reads none gets no type, and its route takes any keys, as before.
- Nothing runs in the browser to check the values: the route gets the type only (`searchTypes` returns `undefined`, and TanStack then reads the query as it is).
