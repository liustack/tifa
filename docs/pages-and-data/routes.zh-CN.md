---
summary: '怎样在 route.go 里写路由的 Go 部分：Load、方法处理函数、带类型的输入、错误、Ctx、JSON 规则'
audience: app
order: 1
translatedFrom: c9912192c2c2
read_when:
  - '编写页面或 API 路由的 route.go：Load、方法处理函数，以及它们返回的 `tifa.Fields`、`tifa.Redirect` 之类的错误'
  - '构建报出关于 Ctx 用法或 Load 返回类型的错误，想把它修好'
  - '修改 internal/analyze 里的规则，需要知道曾向用户承诺了什么'
  - '在方法处理函数里接收文件，或者用 `c.SetBodyLimit` 和 `c.SetReadTimeout` 给某条路由单独设置请求体上限或读取时间'
  - '查 `*tifa.Ctx` 的某个方法做什么'
  - '决定 Load 或处理函数可以接收和返回哪些 Go 类型，或者构建拒绝了某个类型'
---

# 路由和 Ctx {#routes-and-ctx}

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

- `Load` 返回页面数据。它的返回类型必须能转换成 TypeScript（见下面的「JSON 规则」）。
- `GET`、`POST`、`PUT`、`PATCH` 和 `DELETE` 处理与它们同名的 HTTP 方法。
- `Layout` 返回这个文件夹的布局的数据（见[布局](pages.zh-CN.md#layout)）。
- 有 `page.tsx` 的文件夹是一个页面。框架负责渲染它的 GET，所以你不能再写 `GET`。一个不需要数据、也不需要其他方法的页面，可以只有 `page.tsx`，不要 `route.go`。
- 有 `route.go` 但没有 `page.tsx` 的文件夹，只要声明了方法处理函数，就是一个 API 路由：每个方法处理函数都按你写的方式响应，并且不允许有 `Load`，因为 `Load` 是用来加载页面数据的。只有 `Layout` 的 `route.go` 不是 API 路由，它只是给下面的页面提供布局数据。
- 一切都是包级函数和常量。依赖请用应用已经设置好的全局变量。框架不做依赖注入。
- 开关的名字（`SSR`、`Cache`、`Memo`）按字面读取，所以拼错的会被忽略。`route.go` 里一个小写的 bool 常量，名叫 `cache`、`memo` 或 `ssr`，构建会失败。一个导出的常量，与某个开关只差一两个字母（`Cahce`、`Memoo`），并且类型是那个开关接受的，构建也会失败：`SSR` 和 `Memo` 是 bool，`Cache` 是 bool 或 `time.Duration`。错误消息会指出它大概想写哪个开关。其他类型的常量，比如 `const Demo = "demo"`，像 `ID` 或 `Max` 这样的短名字，以及像 `Mode` 这样与四个字母的开关差两处编辑的名字，都不管。

返回错误时：

| 返回的内容 | 结果 |
| --- | --- |
| `tifa.ErrNotFound` | 404 |
| `tifa.Redirect(url, status)` | 重定向。页面加载和导航都会跟随它，从浏览器调用的处理函数也会（见[在浏览器里调用方法处理函数](pages.zh-CN.md#calling-method-handlers-from-the-browser)）。`status` 不是 3xx 时会 panic |
| `tifa.Error{Status, Message}` | 以它的状态码和消息作为响应，不会交给 `OnError`。状态码在 400 到 599 之外时，按意外错误处理 |
| `tifa.Fields{"carrier": "pick a carrier"}` | 422，按 json 名给每个输入字段一条消息，不会交给 `OnError` |
| 其他任何错误，或 panic | 交给 `OnError`。没有 `OnError` 时，记入日志并以 500 响应 |

失败的方法处理函数会响应 JSON：`{"error": "message"}`，对于 `tifa.Fields`，还会有 `"fields": {"carrier": "pick a carrier"}`。

在 `Load` 里只能设置响应头和 cookie。调用 `c.JSON`、`c.Text` 或 `c.NoContent` 会返回错误。

## 带输入和结果的方法处理函数 {#method-handlers-with-input-and-result}

方法处理函数可以在 Ctx 之后接收一个输入，并在 error 之前返回一个结果。框架负责解码输入、写出结果：

```go
func POST(c *tifa.Ctx) error                       // writes its own response
func POST(c *tifa.Ctx, in ShipInput) error         // answers 204 on success
func POST(c *tifa.Ctx) (Shipment, error)           // answers 200 with the result as JSON
func POST(c *tifa.Ctx, in ShipInput) (Shipment, error)
```

- 输入是一个结构体，不是结构体指针。POST、PUT 和 PATCH 从请求体解码它：JSON，或 URL 编码的表单，或 multipart 表单。表单只会填充字符串、数字、布尔值或它们的切片这几类字段，以及文件（见[输入里的文件](#files-in-the-input)），表单里给任何其他字段的值，都会得到 400 响应。表单按每个值的种类解码，从不调用 `MarshalText` 或 `UnmarshalText`，所以带有其中之一的类型，或这类类型的切片，都属于那种「其他字段」，自带其中之一的切片类型也是，比如 `type Tags []string`，TypeScript 会把它当成一个字符串。GET 和 DELETE 从查询字符串解码，所以它的字段按同样的规则，也只能是字符串、数字、布尔值或它们的切片，不能有嵌入字段，输入类型本身也不能有 `MarshalText` 或 `UnmarshalText`。构建会检查这一点，而 `c.BindQuery` 对任何其他字段都会返回错误，不管查询里有什么。
- 字段按它们的 `json` 标签命名，表单体和查询字符串里也一样。`form` 标签会让构建失败，传给 `c.Bind` 或 `c.BindQuery` 的结构体里也一样。路径参数、请求头和 cookie 仍然从 `c` 里取。
- 输入和结果遵循下面的 JSON 规则，和 `Load` 的结果一样。输入还必须能从它的 TypeScript 类型所描述的 JSON 读回来：因 `MarshalText` 而成为字符串的类型，还需要 `UnmarshalText([]byte) error`，有 `UnmarshalText` 的类型在 TypeScript 里必须是字符串，除非它是 map 的键（键永远是 JSON 字符串），而 `UnmarshalJSON` 不被接受（`time.Time` 除外）。
- 当输入类型有 `Validate` 方法时（必须是 `func() error` 或 `func(c *tifa.Ctx) error`），它在解码之后、处理函数之前运行。返回 `nil` 就继续。有了 Ctx，`Validate` 可以读取请求：用 `c.Language()` 给出访问者所用语言的消息（见[校验消息](../guides/i18n.zh-CN.md#validation-messages)），还有 `c.Param`、`c.Query`、`c.QueryValues`、`c.Header`、`c.Cookie`、`c.Path()`、`c.Context()`、`c.Request()` 和 `tifa.Session`。它只做检查，只用返回的错误来回答，所以当 `Validate`，或它把 Ctx 传进去的函数，写响应（`c.JSON`、`c.Text`、`c.NoContent`、`c.SetHeader`、`c.SetCookie`、`c.Writer`）、再次读取请求体（`c.Bind`、`c.BindQuery`、`c.SetBodyLimit`、`c.SetReadTimeout`）、调用 `tifa.SetSession` 或 `tifa.ClearSession`，或调用 `tifa.Use` 时，构建会失败。Ctx 可以去 `Load` 里允许它去的地方（见 [Load 如何使用 Ctx](#how-load-may-use-ctx)）。`tifa.Error` 或 `tifa.Fields` 会原样作为响应，所以要标记单个字段，就返回 `tifa.Fields`。把它们收集到一个 `tifa.Fields{}` 里，最后 `return fields.Err()`，没有添加任何内容时它是 nil（空的 `tifa.Fields` 仍然是一个错误）。其他任何错误都以 400 响应，消息是它的 `err.Error()`，用户会看到这条消息。
- 浏览器从另一个站点发来的 POST、PUT、PATCH 或 DELETE（包括表单和 multipart），会在处理函数运行之前被以 403 响应，所以处理函数不需要为此做自己的检查。`server.trusted_origins` 和 `server.cross_site_paths` 可以放行指定的来源和路径（见[跨站写入](../build-and-deploy/config.zh-CN.md#cross-site-writes)）。
- 无法解码的请求体或查询字符串，会以 400 响应（请求体超过限制是 413，内容类型不对或没有、或把 JSON 发给含文件的输入是 415，请求体没有按时到达是 408），并且处理函数不会运行。
- 自己写响应的处理函数，用 `c.Bind(&in)` 解码请求体，用 `c.BindQuery(&in)` 解码查询字符串。它们按字段的 `json` 标签命名，再按 Go 名，和有类型的输入一样。传给它们的结构体里有 `form` 标签，会让构建失败，和有类型的输入一样（构建看不到类型的值，比如在接口后面的，不会检查，标签被忽略）。`c.Bind` 也会从 multipart 表单填充 `*multipart.FileHeader` 和 `[]*multipart.FileHeader` 字段。

### 输入里的文件 {#files-in-the-input}

POST、PUT 或 PATCH 处理函数的输入里，类型为 `*multipart.FileHeader` 或 `[]*multipart.FileHeader` 的字段，装的是上传的文件，底层类型是它们之一的命名类型也一样，比如 `type Photos []*multipart.FileHeader`。这样生成的函数会把输入作为 multipart 表单发送，所以页面上传文件的方式，和发送其他任何输入一样：

```go
package photos

type UploadInput struct {
	Title  string                  `json:"title"`
	Photo  *multipart.FileHeader   `json:"photo"`
	Extras []*multipart.FileHeader `json:"extras"`
}

func (in UploadInput) Validate() error {
	if in.Photo == nil {
		return tifa.Fields{"photo": "Choose a picture."}
	}
	return nil
}

func POST(c *tifa.Ctx, in UploadInput) (Photo, error) {
	f, err := in.Photo.Open()
	if err != nil {
		return Photo{}, err
	}
	defer f.Close()
	// check the bytes, copy the file where it is kept
}
```

```tsx
const file = form.get('photo') // a File from <input type="file">
await POST({ input: { title, photo: file instanceof File ? file : null, extras: [] } })
```

- 在 TypeScript 里，`*multipart.FileHeader` 是 `Blob | null`，`[]*multipart.FileHeader` 是 `Blob[]`。`File` 是一种 `Blob`，它的文件名也会跟着。带 `omitempty` 或 `omitzero` 的字段是可选的。
- `*multipart.FileHeader` 取同名的第一个文件，没有时保持 `nil`。切片取同名的每个文件，没有时是 `nil`。这个名字下的空文本值，也就是文件输入框没选文件时浏览器发来的，算作没有文件。有内容的文本值会得到 400 响应。
- 这样的输入只接受 multipart 表单或 URL 编码表单（后者不能带文件）：JSON 请求体会得到 415 响应。它的其他字段以文本形式到达，所以遵循 GET 输入的规则：字符串、数字、布尔值或它们的切片，没有嵌入字段，输入类型、字段或字段的元素上都没有 `MarshalText` 或 `UnmarshalText`。
- 文件只能是输入本身的字段。GET 或 DELETE 的输入里、嵌套结构体里、map 或数组里、结果里、`Load` 或 `Layout` 的数据里、会话里出现文件，都会让构建失败。带有 `MarshalText` 或 `UnmarshalText` 方法的文件类型也一样，TypeScript 会把它当成字符串。
- 文件的名字、大小和 `Content-Type`（`in.Photo.Header.Get("Content-Type")`）都是客户端发来的。要根据文件的字节来判断它是什么，保存时用的文件名也要自己生成，见[上传](../guides/uploads.zh-CN.md)。
- 表单按 Go 的 `mime/multipart` 的预算来解析：最多 32 MiB 的文件内容留在内存里，和文本字段以及各部分的名字和头一起共用约 42 MiB，放不下的文件会进入临时文件，Tifa 在请求结束时把它删掉。峰值可能高于预算：最终落到磁盘上的文件，会先被读进一个最大 32 MiB 的不断增长的缓冲区，所以一个大文件可能会在短时间内占用约 64 MiB，而同时进行的每个上传都各占一份。在内存很小的机器上，请在 `OnRequest` 里限制同一时间只解析少量上传，见[上传](../guides/uploads.zh-CN.md#how-much-memory-an-upload-takes)。要保留的内容，请在处理函数返回之前复制出来，也不要把 `*multipart.FileHeader` 交给比请求活得更久的 goroutine。

### 路由的请求体上限和读取时间 {#body-limit-and-read-time-of-a-route}

每个请求体都受 `server.body_limit` 限制，每个请求也只有 `server.read_timeout` 的时间把它发完。接收上传的路由这两样都要更大。在 `OnRequest` 里为需要的请求设置它们，要在任何东西读取请求体之前设置。也要匹配方法：页面的 GET 没有请求体可等。

```go
// hooks.go
func OnRequest(c *tifa.Ctx, next func(*tifa.Ctx) error) error {
	if c.Pattern() == "/photos" && c.Request().Method == http.MethodPost {
		if err := c.SetBodyLimit(100 << 20); err != nil {
			return err
		}
		if err := c.SetReadTimeout(5 * time.Minute); err != nil {
			return err
		}
	}
	return next(c)
}
```

- `c.SetBodyLimit(n)` 的 `n` 是字节数。超过它的请求体，会在处理函数运行之前以 413 响应。这个上限统计整个请求体：文件、其他字段和 multipart 的分隔结构。如果想不重新构建就能改，数字可以取自[应用的选项](../build-and-deploy/config.zh-CN.md#options-of-the-app)：`c.SetBodyLimit(int64(UploadLimit))`，其中 `UploadLimit = tifa.Size("upload.limit", 4<<20, "Largest upload")`。
- `c.SetReadTimeout(d)` 让请求体从现在起有 `d` 的时间到达，之后响应再得到 `server.write_timeout`，因为响应是在请求体收完之后才写的。时间到了请求体还在传，就以 408 响应。没有请求体的请求，比如 GET，保持它的期限，这个调用什么都不做。
- 读取请求体开始之后，两者都会返回错误，`SetReadTimeout` 在 `d` 不大于 0 时，或响应到不了连接时（比如在一个不会解开包装的 `tifa.SetHTTPHandler` 中间件后面）也会返回错误。检查这个错误并返回它。

## Ctx {#ctx}

`*tifa.Ctx` 是请求和它的响应。`Load`、`Layout`、方法处理函数、`OnRequest` 和 `OnError` 都会拿到它。

| 方法 | 作用 |
| --- | --- |
| `c.Param(name)` | 一个路径参数，已解码，比如文件夹 `order+` 对应 `c.Param("order")` |
| `c.Query(name)` | 查询参数的第一个值，不存在时为 `""` |
| `c.QueryValues(name)` | 查询参数的每个值，比如 `?tag=a&tag=b` 得到 `["a", "b"]`，不存在时为 `nil` |
| `c.Header(name)`、`c.Cookie(name)` | 一个请求头，一个请求 cookie（不存在时返回 `http.ErrNoCookie`） |
| `c.Context()`、`c.WithContext(ctx)` | 请求的 context，以及携带另一个 context 的 `c` 的副本，用来保存请求范围内的值。`c` 本身不会变 |
| `c.Pattern()` | 匹配到的路由的路径，比如 `/projects/{project}`，没有匹配到路由时为 `""` |
| `c.Path()` | 访问者在地址栏里看到的页面路径：已转义，带语言前缀，不带查询，比如 `/en-US/store/items/a%20b`。页面加载和导航的数据请求得到的相同。在方法处理函数里，它是处理函数的路径。在 `Layout` 里不允许使用，见 [Load 如何使用 Ctx](#how-load-may-use-ctx) |
| `c.Language()` | 正在回答的路由的语言：URL 里的语言，URL 没有语言时是默认语言，没有多语言的站点上为 `""`。见[代码里的语言](../guides/i18n.zh-CN.md#the-language-in-code) |
| `c.SetAlternates(list)` | 替换页面在其他语言里的版本，用于 `hreflang` 和站点地图。在 `Load` 里调用。见[搜索引擎得到什么](../guides/i18n.zh-CN.md#what-search-engines-get) |
| `c.IsDataRequest()` | 这是不是浏览器路由的导航数据请求，而不是页面加载 |
| `c.SetHeader(key, value)`、`c.SetCookie(cookie)` | 一个响应头，一个响应 cookie。`Load` 里也允许 |
| `c.JSON(code, v)`、`c.Text(code, s)`、`c.NoContent(code)` | 写出响应。`Load` 里不允许 |
| `c.Bind(&v)`、`c.BindQuery(&v)` | 把请求体或查询字符串解码进结构体，见上文。`Load` 里不允许 |
| `c.SetBodyLimit(n)` | 改变这个请求的请求体上限（默认是 `server.body_limit`，见[路由的请求体上限和读取时间](#body-limit-and-read-time-of-a-route)）。要在任何东西读取请求体之前调用，通常在 `OnRequest` 里。读取一旦开始，它会返回错误 |
| `c.SetReadTimeout(d)` | 给这个请求的请求体 `d` 的时间到达，代替 `server.read_timeout`，之后响应再得到 `server.write_timeout`。要在任何东西读取请求体之前调用，通常在 `OnRequest` 里。读取一旦开始，它会返回错误 |
| `c.Request()`、`c.Writer()` | 原始的 `*http.Request` 和 `http.ResponseWriter`，只给方法处理函数和 `OnRequest` 用 |

## Load 如何使用 Ctx {#how-load-may-use-ctx}

这一节讲 `Load`、`Layout` 和它们调用的函数。构建时，Tifa 从每个 `Load` 和 `Layout` 函数出发，找出它读取的路由参数（`c.Param`）、查询参数（`c.Query` 和 `c.QueryValues`）和布局（`tifa.Use`）。当 `Load` 把 `*tifa.Ctx` 传给本模块里的函数时，Tifa 会一层一层地跟进这个调用，跨包也一样，并把找到的每个参数都算到这个 `Load` 头上。被多个 `Load` 函数调用的辅助函数，会算到它们每一个头上。Tifa 不会跟进它自己的函数。

为了让 Tifa 能找全，Ctx 只能用在 Tifa 看得见的地方。给 `c.Param`、`c.Query` 和 `c.QueryValues` 传一个常量字符串，比如 `c.Param("order")`，并且直接调用它们和 `tifa.Use`：把其中一个存进变量，或当成值传递，都会让构建失败。

能持有 Ctx 的类型只有这些：`*tifa.Ctx`、`tifa.Ctx`、本模块里声明的非泛型结构体（它的字段只通过这些类型持有 Ctx，直接、嵌入或间接都可以），以及指向这类结构体的指针。

持有 Ctx 的值，只能出现在这五个地方：

1. 作为直接方法调用的接收者，比如 `c.Query("tab")` 或 `req.Page()`。
2. 作为参数传给本模块或 Tifa 里的非泛型函数，且该参数的类型完全相同。就地调用的函数字面量也算。
3. 在持有 Ctx 的结构体的复合字面量里，赋给完全相同类型的字段。
4. 用 `:=` 或 `var x T = ...` 赋给一个完全相同类型的新局部变量。这个变量遵循同样的规则。
5. 取它的地址，并且得到的指针仍然是可以持有 Ctx 的类型。

任何其他用法都会让构建失败，并指出文件和行号。比如：把它转换成另一个类型或接口，赋给已有的变量，一次赋多个值，`return` 它，放进切片、map、数组或 channel，`range`，包级变量，`defer`，`go` 语句，或把它传给泛型函数、接口方法、函数变量，或模块之外的函数（标准库和第三方库）。

`Load`、`Layout` 和它们调用的函数，不能调用 `c.Request()` 或 `c.Writer()`。Tifa 看不见通过原始请求读取的参数。请改用 `c.Param`、`c.Query`、`c.QueryValues`、`c.Header`、`c.Cookie` 或 `c.Context()`。`c.Request()` 和 `c.Writer()` 只给方法处理函数和 `OnRequest` 用。

在 `Load` 里要取页面的路径，用 `c.Path()`，比如在重定向到登录页的 `next` 地址里。它是访问者请求的路径，带语言前缀，不带查询，页面加载和导航的数据请求得到的都一样，所以不必为此去调用 `c.Request()`。需要把某个查询参数也放进这个地址的 `Load`，用 `c.Query("name")` 读取它。`Layout` 和它调用的函数不能调用 `c.Path()`：布局的数据由它下面的每个页面共享，访问者在这些页面之间跳转时不会重新加载，所以它会一直保留第一次加载它的那个页面的路径。请在页面的 `Load`、`OnRequest` 或方法处理函数里调用它。

它们也不能调用 `c.BindQuery` 或 `c.Bind`。Tifa 说不出一个结构体解码了哪些查询参数，所以这些参数变化时页面不会重新加载，而且页面加载或导航的数据请求没有请求体。请用 `c.Query("name")` 读取每个查询参数，重复出现的参数用 `c.QueryValues("name")`。

遇到这些错误时，请直接调用 Ctx 的方法，或把它传给一个普通函数。如果只需要请求的 context，就传 `c.Context()`。只调用 `c.Context()` 的辅助函数（比如 `auth.User(c)`），Tifa 跟进它时什么也收集不到。

Tifa 只检查 `Load` 和 `Layout` 能到达的代码。如果你在 `OnRequest`、方法处理函数或别的任何地方，把 Ctx 存进全局变量或其他共享的地方，然后在 `Load` 或 `Layout` 里把它取出来，Tifa 就抓不住。不要这样做。

## JSON 规则 {#json-rules}

`Load` 或 `Layout` 函数返回的值，以及方法处理函数的输入和结果，都以 JSON 传输，Tifa 会根据它们生成 TypeScript 类型。Tifa 用 `encoding/json/v2` 及其默认设置来写入和读取这些 JSON，所以构建只接受下面这些类型，它们是 json v2 能无错写出、并且形式能被 TypeScript 类型描述的类型。`c.JSON`、`c.Bind` 和会话用的是同样的规则。

| Go | TypeScript |
| --- | --- |
| `bool` | `boolean` |
| 整数、浮点数、`json.Number` | `number` |
| `string`、`time.Time` | `string` |
| `tifa.HTML` | `tifa/client` 的 `HTML`，见[显示应用信任的 HTML](pages.zh-CN.md#showing-html-the-app-trusts)。不能作为 map 的键、处理函数的输入、会话，也不能作为整个结果 |
| `*multipart.FileHeader`、`[]*multipart.FileHeader` | `Blob \| null`、`Blob[]`。只能出现在 POST、PUT 或 PATCH 处理函数输入的字段里，见[输入里的文件](#files-in-the-input) |
| 指针 | `T \| null` |
| 切片 | `T[]`。nil 切片写成 `[]` |
| `[]byte`、`[N]byte`（元素恰好是 `byte` 或 `uint8`） | `string`，以 base64 写出。nil 的 `[]byte` 是 `""`。命名的字节类型的切片或数组，是普通的数字数组 |
| 数组 | `T[]`，长度为 0 时是 `[]` |
| map | `Record<string, V>`。nil 的 map 写成 `{}` |
| 结构体 | 由它的导出字段组成的对象。具名结构体还会在 `route.gen` 里按它的 Go 名字声明，见 [page.tsx](pages.zh-CN.md#pagetsx) |

- map 的键按排序后的顺序写出，所以同一个值总是得到同样的文本。键必须是字符串或整数类型，或者按下面的 `MarshalText` 规则输出字符串的类型。接口和指针不能做键。
- 没有 json 名的导出嵌入结构体（指针也可以），它的字段会提升到外层对象里。通过嵌入指针到达的字段是可选的。不同结构体的字段同名时，层级最浅的胜出，然后是带 json 标签的，如果这样还剩下多个，就全部丢弃。嵌入字段的类型不是结构体时，需要一个 json 名。嵌入的结构体不能带 json 标签选项，也不能有名为 `MarshalJSON`、`MarshalJSONTo`、`MarshalText`、`AppendText`、`UnmarshalJSON`、`UnmarshalJSONFrom` 或 `UnmarshalText` 的方法。未导出的嵌入字段不被接受，除非标成 `json:"-"`。
- 同一个结构体的两个字段不能有相同的 JSON 名。未导出的字段除了 `json:"-"` 外不能有 json 标签，有字段的结构体需要至少一个导出字段或 json 标签。
- json 标签的名字只能用字母、数字、空格和 `` !#$%&()*+-./:;<=>?@[]^_{|}~ ``。唯一允许的选项是 `omitempty`、`omitzero` 和 `string`。带 `omitempty` 或 `omitzero` 的字段是可选的。`omitzero` 会省略 Go 类型的零值。`omitempty` 会省略 JSON 为空的值：`null`、`""`、`[]` 或 `{}`，所以它从不省略 `false` 或 `0`。`string` 选项只允许用在预声明数字类型的字段上，或指向它们的未命名指针上，并把数字写成字符串。在 `int64` 或 `uint64` 字段上用 `string` 选项，就是发送大整数的办法：它的类型是 `string`，以 JSON 字符串写出。见下一条。
- 浏览器和服务端渲染都把每个 JSON 数字读成双精度浮点数，所以整数只有到 2^53 - 1（9007199254740991）为止才是安全的。整数的类型是 `number`，而 Go 会写出每一位数字，所以雪花 ID、哈希或 `time.UnixNano()` 这样的值会被悄悄四舍五入，不报任何错误。Tifa 干脆拒绝发送它：请求会以意外错误失败（`OnError`、500、开发页面和 `tifa errors`，开发和生产环境都一样），消息里会写出 `Load`、`Layout`、处理函数或会话的名字，这个数字的路径（`rows[3].id`），以及它原本的写法和浏览器读到的样子。它检查的是写出的 JSON，所以 Go 类型怎么拼写都没关系，而 `float64` 永远不会被拒绝。两条出路：给字段加上 `json:",string"`（类型为 `string`），或者让值保持在 ±9007199254740991 之内（对时间来说，发送毫秒数）。只检查页面会读取的内容：`Load` 和 `Layout` 的结果，`Exposed` 的会话数据，以及当调用来自页面的生成函数时处理函数的结果。普通的 HTTP 客户端（curl、移动应用）从处理函数拿到的是原始数字，不带 `X-Tifa-Call: 1` 头的 `tifatest.Start` 也一样，所以要向处理函数 post 并看到这个拒绝的测试，必须设置那个头。`Load` 和 `Layout` 在 `tifatest.Start` 下也会被检查。处理函数用 `c.JSON` 写出的内容，以及对不发送 `X-Tifa-Call` 的客户端（比如移动应用）的响应，都按原样发送。`tifa build`、`tifa check` 和 `tifa dev` 会警告，并给出文件和行号，指出代码把超过 2^53 的数字直接放进这样的字段的地方：常量、`time.Time.UnixNano`、`math/rand` 的 `Int63`、`Int64`、`Uint64`、`Sum64` 哈希，或 `crypto/rand` 字节的 `binary.Uint64`，出现在复合字面量或对字段的赋值里，经过类型转换或一个局部变量也一样。其他情况不做猜测。
- `time.Duration` 不被接受，作为 map 的键也一样，因为 json v2 没有它的形式。请返回你想要的单位的整数，比如毫秒，或者返回字符串。
- 如果一个类型的值或指针有名为 `MarshalJSON`、`MarshalJSONTo`、`AppendText` 或 `AppendJSON` 的方法或字段，不管签名如何都会被拒绝（`time.Time` 和 `json.Number` 除外）。请改名，或返回另一个没有这些方法的类型。
- 如果一个类型自己声明了签名恰好是 `MarshalText() ([]byte, error)` 的方法，无论在值接收者还是指针接收者上，它就写成字符串。签名不对、方法是从嵌入字段提升来的、或方法被同名字段遮住时，一律被拒绝。
- 接口在任何地方都不被接受，包括字段、元素和 map 的键。chan、func、复数、`unsafe.Pointer` 和类型参数也不接受。
- `<`、`>` 和 `&` 按原样写出，页面数据里也一样。Tifa 在把数据放进页面的地方会转义它们，所以应用里的任何文本都无法结束一个 script 元素。
- 含有非 UTF-8 字节的字符串，每个这样的字节会被写成 U+FFFD，和浏览器显示的一样，不会让页面失败。
- 输入按严格方式读取：名字必须和 json 名完全一致，大小写也要一致，一个名字在对象里只能出现一次，字符串必须是 UTF-8，`time.Time` 必须是 RFC 3339，base64 里不能有换行，数组的长度必须和 Go 数组一致。每种错误都以 400 响应。类型里没有的名字会被跳过。无法按这些规则读回的会话记录，会让访问者退出登录。
