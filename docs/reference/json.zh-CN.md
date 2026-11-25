---
summary: 'Load 和处理函数可以接收和返回的 Go 类型，以及对应的 TypeScript 类型'
audience: app
order: 5
translatedFrom: bbd6942d107d
read_when:
  - '决定 Load 或处理函数可以接收和返回哪些 Go 类型，或者构建拒绝了某个类型'
  - '构建报出关于 Load 返回类型的错误，想把它修好'
  - '修改 internal/analyze 里的规则，需要知道曾向用户承诺了什么'
---

# JSON 规则 {#json-rules}

`Load` 或 `Layout` 函数返回的值，以及方法处理函数的输入和结果，都以 JSON 传输，Tifa 会根据它们生成 TypeScript 类型。Tifa 用 `encoding/json/v2` 及其默认设置来写入和读取这些 JSON，所以构建只接受下面这些类型，它们是 json v2 能无错写出、并且形式能被 TypeScript 类型描述的类型。`c.JSON`、`c.Bind` 和会话用的是同样的规则。

| Go | TypeScript |
| --- | --- |
| `bool` | `boolean` |
| 整数、浮点数、`json.Number` | `number` |
| `string`、`time.Time` | `string` |
| `tifa.HTML` | `tifa/client` 的 `HTML`，见[显示应用信任的 HTML](loading-data.zh-CN.md#showing-html-the-app-trusts)。不能作为 map 的键、处理函数的输入、会话，也不能作为整个结果 |
| `*multipart.FileHeader`、`[]*multipart.FileHeader` | `Blob \| null`、`Blob[]`。只能出现在 POST、PUT 或 PATCH 处理函数输入的字段里，见[输入里的文件](../pages-and-data/handlers.zh-CN.md#files-in-the-input) |
| 指针 | `T \| null` |
| 切片 | `T[]`。nil 切片写成 `[]` |
| `[]byte`、`[N]byte`（元素恰好是 `byte` 或 `uint8`） | `string`，以 base64 写出。nil 的 `[]byte` 是 `""`。命名的字节类型的切片或数组，是普通的数字数组 |
| 数组 | `T[]`，长度为 0 时是 `[]` |
| map | `Record<string, V>`。nil 的 map 写成 `{}` |
| 结构体 | 由它的导出字段组成的对象。具名结构体还会在 `route.gen` 里按它的 Go 名字声明，见 [page.tsx](loading-data.zh-CN.md#pagetsx) |

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
