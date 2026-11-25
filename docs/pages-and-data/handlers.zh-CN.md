---
summary: 'POST 等方法处理函数：带类型的输入、文件、请求体上限、从页面调用'
audience: app
order: 4
translatedFrom: 3e1bf808cc9c
read_when:
  - '在 route.go 里编写 API 路由或方法处理函数'
  - '在方法处理函数里接收文件，或者用 `c.SetBodyLimit` 和 `c.SetReadTimeout` 给某条路由单独设置请求体上限或读取时间'
  - '在页面里调用方法处理函数，或者页面没有显示一次写操作带来的变化'
  - '修改 internal/analyze 里的规则，需要知道曾向用户承诺了什么'
---

# 方法处理函数 {#method-handlers}

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
- 输入和结果遵循 [JSON 规则](../reference/json.zh-CN.md)，和 `Load` 的结果一样。输入还必须能从它的 TypeScript 类型所描述的 JSON 读回来：因 `MarshalText` 而成为字符串的类型，还需要 `UnmarshalText([]byte) error`，有 `UnmarshalText` 的类型在 TypeScript 里必须是字符串，除非它是 map 的键（键永远是 JSON 字符串），而 `UnmarshalJSON` 不被接受（`time.Time` 除外）。
- 当输入类型有 `Validate` 方法时（必须是 `func() error` 或 `func(c *tifa.Ctx) error`），它在解码之后、处理函数之前运行。返回 `nil` 就继续。有了 Ctx，`Validate` 可以读取请求：用 `c.Language()` 给出访问者所用语言的消息（见[校验消息](../guides/i18n.zh-CN.md#validation-messages)），还有 `c.Param`、`c.Query`、`c.QueryValues`、`c.Header`、`c.Cookie`、`c.Path()`、`c.Context()`、`c.Request()` 和 `tifa.Session`。它只做检查，只用返回的错误来回答，所以当 `Validate`，或它把 Ctx 传进去的函数，写响应（`c.JSON`、`c.Text`、`c.NoContent`、`c.SetHeader`、`c.SetCookie`、`c.Writer`）、再次读取请求体（`c.Bind`、`c.BindQuery`、`c.SetBodyLimit`、`c.SetReadTimeout`）、调用 `tifa.SetSession` 或 `tifa.ClearSession`，或调用 `tifa.Use` 时，构建会失败。Ctx 可以去 `Load` 里允许它去的地方（见 [Load 如何使用 Ctx](loading-data.zh-CN.md#how-load-may-use-ctx)）。`tifa.Error` 或 `tifa.Fields` 会原样作为响应，所以要标记单个字段，就返回 `tifa.Fields`。把它们收集到一个 `tifa.Fields{}` 里，最后 `return fields.Err()`，没有添加任何内容时它是 nil（空的 `tifa.Fields` 仍然是一个错误）。其他任何错误和处理函数返回的一样，算应用自己出的错：交给 `OnError`，没有 `OnError` 时以 500 响应，用户看不到它的文字（见[你返回的错误会变成什么](errors.zh-CN.md#what-an-error-you-return-becomes)）。
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

## 一次写入放在哪 {#where-a-write-goes}

一个文件夹里每种方法只有一个处理函数，所以一次写入就是一个方法加一个文件夹。应用需要的写入有三种形状：

| 这次写入 | 放在哪 | 比如 |
| --- | --- | --- |
| 改某个东西的字段 | 这个东西所在文件夹的 `PATCH` | `PATCH /orders/42`，带上要改的字段 |
| 在某个东西下面新建一项 | 放这类子项的文件夹的 `POST` | `POST /orders/42/notes` |
| 有自己名字的操作 | 以这个名字命名的文件夹，和它的 `POST` | `POST /orders/42/ship` |

- **一个 `PATCH` 可以改任意几个字段。** 输入里带 `omitempty` 或 `omitzero` 的字段在 TypeScript 里是可选的，指针字段能分清「没传」（`nil`）和「传了空值」。
- **一个操作有自己的规则时，就给它一个文件夹：** 谁能做，什么时候能做，或者它还会引起什么。给订单发货只能做一次，会改订单的状态，还可能发一封邮件，所以它是 `ship/`，不是 `PATCH` 里多出来的一个字段。
- **这个文件夹很小。** 它是一个 Go 包，里面一个几行的 `route.go`，调用业务域的代码：

  ```go
  // app/orders/order+/ship/route.go
  package ship

  type Input struct {
  	Carrier    string `json:"carrier"`
  	TrackingNo string `json:"trackingNo"`
  }

  func POST(c *tifa.Ctx, in Input) (orders.Order, error) {
  	return orders.Ship(c.Context(), c.Param("order"), in.Carrier, in.TrackingNo)
  }
  ```

  页面导入它的函数时，起一个说明用途的名字：`import { POST as shipOrder } from './ship/route.gen'`。

这样每次写入都有自己的地址、输入类型和结果类型。`OnRequest` 可以根据方法和 `c.Pattern()` 决定谁能做，代理、限流和日志只看方法和路径就能把各种写入分开。

这些 Tifa 都不检查。处理函数也可以读 `c.Query("action")`，然后在几件事里做一件，但那几件事就共用一个地址、一个输入类型和一个结果类型。Tifa 为什么没有让浏览器按名字调用的函数，见[设计理念](../start/philosophy.zh-CN.md#one-address-for-each-write)。

## 从浏览器调用方法处理函数 {#calling-method-handlers-from-the-browser}

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
- GET 和 DELETE 把输入放在查询字符串里，其他方法放在 JSON 请求体里，输入里有文件时用 multipart 表单（见[输入里的文件](handlers.zh-CN.md#files-in-the-input)）。结果的类型来自 Go 的结果，遇到重定向（见下）时是 `| undefined`：`func(c, in) (Out, error)` 得到 `Promise<POSTOutput | undefined>`。`func(c, in) error` 得到 `void`。`func(c) error` 自己写响应，所以它的函数原样返回 `Response`，错误状态也一样。
- 一次调用只发送它的请求，如果响应带着会话，就应用它（见下）。无论成功还是失败、无论哪种方法，它都从不重新加载页面的数据。页面需要显示这次写入的改动时，在它之后调用 `router.invalidate()`（见下面的例子）。
- Tifa 从不替你重新加载页面数据。在一次写入（处理函数或你自己的 `fetch`）之后，页面显示的还是原来的内容，直到你提出要求：调用 TanStack 的 `router.invalidate()`，重新加载当前页面及其所有布局的数据，合并成一个请求。`await router.invalidate({ sync: true })` 在页面显示新数据之后才完成。普通的 `await router.invalidate()` 会立刻完成，页面在后台更新。
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
