---
summary: '*tifa.Ctx 的方法'
audience: app
order: 4
translatedFrom: 9a3efbafba96
read_when:
  - '查 `*tifa.Ctx` 的某个方法做什么'
---

# Ctx {#ctx}

`*tifa.Ctx` 是请求和它的响应。`Load`、`Layout`、方法处理函数、`OnRequest` 和 `OnError` 都会拿到它。

| 方法 | 作用 |
| --- | --- |
| `c.Param(name)` | 一个路径参数，已解码，比如文件夹 `order+` 对应 `c.Param("order")` |
| `c.Query(name)` | 查询参数的第一个值，不存在时为 `""`。给方法处理函数、`Validate` 和 `OnRequest` 用。`Load` 和 `Layout` 把查询参数作为第二个参数接收，见[查询参数](../pages-and-data/loading-data.zh-CN.md#query-parameters)，在那里调用它会让构建失败 |
| `c.QueryValues(name)` | 查询参数的每个值，比如 `?tag=a&tag=b` 得到 `["a", "b"]`，不存在时为 `nil`。和 `c.Query` 一样，`Load` 和 `Layout` 里不允许 |
| `c.Header(name)`、`c.Cookie(name)` | 一个请求头，一个请求 cookie（不存在时返回 `http.ErrNoCookie`） |
| `c.Context()`、`c.WithContext(ctx)` | 请求的 context，以及携带另一个 context 的 `c` 的副本，用来保存请求范围内的值。`c` 本身不会变 |
| `c.Pattern()` | 匹配到的路由的路径，比如 `/projects/{project}`，没有匹配到路由时为 `""` |
| `c.Path()` | 访问者在地址栏里看到的页面路径：已转义，带语言前缀，不带查询，比如 `/en-US/store/items/a%20b`。页面加载和导航的数据请求得到的相同。在方法处理函数里，它是处理函数的路径。在 `Layout` 里不允许使用，见 [Load 如何使用 Ctx](../pages-and-data/loading-data.zh-CN.md#how-load-may-use-ctx) |
| `c.Language()` | 正在回答的路由的语言：URL 里的语言，URL 没有语言时是默认语言，没有多语言的站点上为 `""`。见[代码里的语言](../guides/i18n.zh-CN.md#the-language-in-code) |
| `c.SetAlternates(list)` | 替换页面在其他语言里的版本，用于 `hreflang` 和站点地图。在 `Load` 里调用。见[搜索引擎得到什么](../guides/i18n.zh-CN.md#what-search-engines-get) |
| `c.IsDataRequest()` | 这是不是浏览器路由的导航数据请求，而不是页面加载 |
| `c.SetHeader(key, value)`、`c.SetCookie(cookie)` | 一个响应头，一个响应 cookie。`Load` 里也允许 |
| `c.JSON(code, v)`、`c.Text(code, s)`、`c.NoContent(code)` | 写出响应。`Load` 里不允许 |
| `c.Bind(&v)`、`c.BindQuery(&v)` | 把请求体或查询字符串解码进结构体，见[方法处理函数](../pages-and-data/handlers.zh-CN.md)。`Load` 和 `Layout` 里不允许 |
| `c.SetBodyLimit(n)` | 改变这个请求的请求体上限（默认是 `server.body_limit`，见[路由的请求体上限和读取时间](../pages-and-data/handlers.zh-CN.md#body-limit-and-read-time-of-a-route)）。要在任何东西读取请求体之前调用，通常在 `OnRequest` 里。读取一旦开始，它会返回错误 |
| `c.SetReadTimeout(d)` | 给这个请求的请求体 `d` 的时间到达，代替 `server.read_timeout`，之后响应再得到 `server.write_timeout`。要在任何东西读取请求体之前调用，通常在 `OnRequest` 里。读取一旦开始，它会返回错误 |
| `c.Request()`、`c.Writer()` | 原始的 `*http.Request` 和 `http.ResponseWriter`，只给方法处理函数和 `OnRequest` 用 |
