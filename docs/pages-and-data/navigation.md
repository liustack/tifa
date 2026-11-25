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

The query parameters a `Load` or `Layout` function takes as its second parameter (see [Query parameters](loading-data.md#query-parameters)) have the types of their fields in the browser. `route.gen` exports them as `PageSearch` and `LayoutSearch`, and the route gets them, so TanStack's `Link`, `navigate` and `useSearch` check them and give them to the page as they are:

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

- A value has the type of its field in Go: a `number` for a whole number or a float, a `boolean` for a `bool`, a `string`, and an array of those for a slice. Write them with their types (`page: 2`, `stock: true`), not as text. Every key may be left out, and a parameter that the address does not pass is `undefined`, not the zero value of Go's field. That holds for an empty value too: `?page=` has no `page`.
- The router converts what it reads from the address by the type of the field and by nothing else, so a `string` field keeps `?q=1.0` as `"1.0"` and `?q=007` as `"007"`. A scalar field has one value, and a name that comes twice is not valid. A list has one element for each parameter of that name, even for one.
- A value that is not valid is refused by the same rules as in Go (see [Query parameters](loading-data.md#query-parameters)), so `?page=02`, `?stock=1` and `?price=10.50` fail the route. The `ErrorBoundary` of the page gets a `TifaError` with status 400 and the message Go answers with, `invalid query parameter "page"`. The same holds when a `navigate` writes a value that is not valid, which the types stop unless they are cast away.
- A function for `search` that builds on `prev` needs `from` on the `Link` or `useNavigate({ from })`, as above. Without it `prev` holds the parameters of every route in the app, and `prev.page` has no useful type.
- Other keys are open and have the type `unknown`: a page may keep parameters of its own in the URL that Go does not read, such as the open tab of a panel. The router leaves them as they come from the address, strings or arrays of strings. So a misspelled key is not a type error, only a wrong type for a key Go reads is. The server render does not see them, see [Query parameters in the server render](ssr-and-cache.md#query-parameters-in-the-server-render).
- Each route has the types of its own `Load` or `Layout`, and TanStack adds those of the Layouts above. A name that a Layout and a page below it both declare has one type, which the build checks. A `Load` that reads none gets no type, and its route takes any keys.
- The route gets a `validateSearch` made from the types of the fields, which the router runs on every address it reads. It also gives the page the value as it is, so a component needs no `Number(search.page)` or `search.stock === 'true'`.
