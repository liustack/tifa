---
summary: 'Tifa 应用里的会话怎样工作：cookie、内存存储、Redis 或 SQL 存储、`tifa.SetSessionStore`'
audience: app
order: 10
translatedFrom: 316e234cc50b
read_when:
  - '让用户登录和登出，或者读取谁已登录'
  - '把会话放在 Redis、SQL 或文件里，而不是进程的内存里，或者为 tifa.SetSessionStore 写一个存储'
  - '用 session.memory_limit 给内存存储定大小'
  - '在页眉里显示已登录用户的名字、`useSession`、`SignedIn`、`data-session`，或者 CSP 拦下了 head 开头的脚本'
  - '修改会话 Cookie、会话的有效时长或 Cookie 名'
  - 'SetSession 或 ClearSession 调用返回错误，或者构建因会话类型报错'
  - '为需要有人登录的代码写 Go 测试'
  - '修改 internal/core（session.go、sessionstore.go）或 js/runtime/session.tsx'
---

# 会话 {#sessions}

会话是记录「谁已登录」的一条记录，保存在服务器上，通过 cookie 找到它。记录、令牌、cookie 和过期时间由 Tifa 负责。你的代码负责检查密码，并说明什么时候某人登录或退出。

```go
// auth/session.go
type Session struct {
	UserID int64  `json:"userId"`
	Name   string `json:"name"`
}

// app/login/route.go
func POST(c *tifa.Ctx, in Login) error {
	u, err := users.CheckPassword(c.Context(), in.Email, in.Password)
	if err != nil {
		return tifa.Error{Status: 401, Message: "Wrong email or password"}
	}
	if err := tifa.SetSession(c, &auth.Session{UserID: u.ID, Name: u.Name}); err != nil {
		return err
	}
	return tifa.Redirect("/", 303)
}

// anywhere that has the Ctx: OnRequest, a method handler, a Load
s := tifa.Session[auth.Session](c) // nil when nobody is signed in

// app/logout/route.go
err := tifa.ClearSession(c)
```

- `tifa.SetSession(c, &s)` 让这个请求登录。它生成一个新的随机令牌（32 字节，43 个字符），把 `s` 以 JSON 存在令牌的 SHA-256 之下，删除请求原本带的会话，并设置 cookie。每次调用都会生成新令牌，所以登录绝不会沿用攻击者预先埋下的令牌。如果旧会话删不掉，登录会以错误失败，并且不会发出 cookie。
- `tifa.Session[S](c)` 返回这个请求的会话，或 nil。服务器在 `OnRequest` 之前读取它，所以每个 hook、`Load` 和方法处理函数都能看到它。它在第一次调用时解码成 `S`，同时运行的 `Load` 函数共用同一个指针，所以把它当成只读的。整个应用用同一个类型 `S`：用别的类型调用会 panic。存着的记录和 `S` 对不上时，会被删除，cookie 会被丢弃，这个请求按未登录处理。
- `tifa.ClearSession(c)` 删除记录，丢弃 cookie，并让 `Session` 返回 nil。没人登录时它什么也不做。如果存储删不掉记录，cookie 仍然会被丢弃，错误会说明这条记录在过期之前仍然有效。
- 记录加上 Tifa 在外面包的那层信封，不能超过 4 KiB。里面放一个 ID 和几个用来显示的字段，其余的放在你的数据库里。谁能写这个存储，谁就能让任何人登录，所以要像保护数据库一样保护它。

## Cookie {#the-cookie}

cookie 是 `__Host-tifa_session`，带 `HttpOnly`、`Secure`、`SameSite=Lax`、`Path=/`，没有 `Domain`。会话从 `SetSession` 起持续 30 天。它不会续期，所以请求从不写存储。名字和有效期可以用 `session.cookie` 和 `session.max_age` 修改（见[配置](../reference/settings.zh-CN.md#settings)）。在 `tifa dev` 下，cookie 是 `tifa_session`，没有 `Secure`，因为 dev 在 localhost 上提供的是普通 HTTP。浏览器会丢弃通过普通 HTTP 从 localhost 以外的地方来的 `Secure` cookie，所以在 `http://<IP>:9527` 登录看起来成功了，用户在下一个请求就被退出登录。第一次响应这类请求的 `SetSession` 会记录一条警告，每个进程只记一次。它在请求通过 TLS 到达，或带有 `X-Forwarded-Proto: https` 或 `Forwarded: proto=https` 时，把请求算作 HTTPS，在它的主机是 `localhost`、某个 `.localhost` 名字或回环地址时，算作本地。请通过 HTTPS 测试，或通过设置了该头的代理测试，或在 `http://localhost` 测试。cookie 不是由 43 个 `A-Z a-z 0-9 - _` 字符组成时，会被直接丢弃，不去问存储。cookie 对应的记录存储里已经没有了（因为过期了，或者进程用内存存储重启过），也会被丢弃，这个请求按未登录处理。

设置或丢弃 cookie 的响应，不管应用设置了什么，都会以 `Cache-Control: no-store` 发出，不带 `ETag` 或 `Last-Modified`，并保持它的状态码：它永远不会变成 304。

## 会话存在哪里 {#where-to-store-sessions}

没有存储时，会话存在进程的内存里，上限是 `session.memory_limit`（64 MiB）。存满后，会先丢弃最早过期的会话，重启会让所有人退出登录。服务器在启动时会为此记录一行日志。`tifa dev` 在你的 Go 代码变化时会重启，所以那时你会退出登录。如果你的应用允许未登录的访问者开启会话，请在入口处限制它，因为被灌满会把真正的会话挤出去。

`tifa new` 生成的应用，会在 `OnStart` 里设置一个把会话存在它的 SQLite 数据库里的存储（`sessions.go`，它的测试用 `tifatest.TestSessionStore`），所以重启或部署不会让任何人退出登录，`tifa dev` 也会让你保持登录。

要把会话存到别的地方，比如 Redis 或 SQL，就给 `OnStart` 一个存储：

```go
func OnStart(ctx context.Context) error {
	tifa.SetSessionStore(redisStore{client})
	return nil
}
```

服务器一开始监听，`tifa.SetSessionStore` 就会 panic，和 `tifa.SetHTTPHandler` 一样。存储要实现 `tifa.SessionStore`：

```go
type SessionStore interface {
	Get(ctx context.Context, token string) (data []byte, found bool, err error)
	Put(ctx context.Context, token string, data []byte, expiry time.Time) error
	Delete(ctx context.Context, token string) error
}
```

- `token` 是 cookie 里令牌的 SHA-256 的 base64url，绝不是 cookie 本身。
- 对缺失、已过期或无效的键，`Get` 返回 `found == false` 和 nil 错误。只有存储自己出故障时才返回错误。这时请求会通过 `OnError` 以意外错误失败：当作未登录继续往下走，会掩盖故障。
- `Put` 创建或替换记录，`expiry` 之后 `Get` 一定不能再返回它。对不存在的键，`Delete` 返回 nil。
- 这三个方法会在许多请求上同时运行。`Put` 不能保留 `data` 切片，`Get` 不能返回存储仍在使用的切片。`ctx` 结束时，它们要返回错误。

### 内存存储 {#the-memory-store}

`OnStart` 没有设置存储时，Tifa 用的存储把记录放在进程的内存里，上限是 `session.memory_limit` 字节（见[配置](../reference/settings.zh-CN.md#settings)）。设置它可以让会话占用更多或更少的内存。设置过存储之后，`tifa.SetSessionStore(nil)` 可以换回内存存储。

- `session.memory_limit` 对每条记录统计它的键、数据和一笔固定开销。存储满了的时候，`Put` 会丢弃最早过期的记录。单条记录的开销就超过上限时，`Put` 会返回错误，于是 `SetSession` 失败。
- 它会运行一个 goroutine，每分钟清理一次过期的记录，服务器会让它停下。停下之后存储照常工作，因为 `Get` 从不返回已过期的记录。

### 测试存储 {#testing-a-store}

你写的每个存储都要写测试。`tifatest.TestSessionStore(t, store)` 就是这个现成的测试：它按上面的规则检查你的存储，所以错误会出现在 `go test` 里，而不是变成随机被退出登录的用户。

```go
func TestRedisStoreIsAValidStore(t *testing.T) {
	store := newRedisStore(testClient(t)) // an empty database that only this test uses
	tifatest.TestSessionStore(t, store)
}
```

（`newRedisStore` 和 `testClient` 代表你自己的代码。）Tifa 自己的测试就是对它的内存存储调用它。

它按顺序检查：

- 从未存过的键是 `found == false`，错误为 nil。
- `Put` 后再 `Get` 能返回记录，再次 `Put` 会替换它。
- 对不存在的键 `Delete` 返回 nil，`Delete` 会移除记录。
- 过期时间已过的记录找不到，写入时就已过期的也一样。它会等 400 毫秒让一条短命的记录过期，所以这个测试要花大约半秒。
- 存储不保留对传给 `Put` 的切片的引用，`Get` 返回的切片可以放心修改。
- `ctx` 结束时，`Get`、`Put` 和 `Delete` 返回错误，并且 `ctx` 已结束的 `Put` 或 `Delete` 不会改变任何东西。
- 八个 goroutine 同时对同一个键调用 `Put`、`Get` 和 `Delete`。用 `-race` 运行测试来发现数据竞争。

测试本身的规则：

- 给它一个没有别的东西在用的空存储。它会写入几个 43 个字符的键（Tifa 用的形式），并在结束时把它们删掉。它不会清理别的键。
- `t` 可以是 `*testing.T` 或 `*testing.B`。它用 `t.Errorf` 报告每一项失败的检查，所以一次运行会列出所有问题。第一次 `Put` 失败时，它用 `t.Fatalf` 停下。
- 它测试的是一个进程。两台服务器能不能看到相同的记录，或数据库挂了会怎样，要你自己测试。

每条失败消息都会点出规则。如果存储的每个方法开头没有 `ctx.Err()` 检查（比如把记录放在文件里的存储），会这样失败：

```
--- FAIL: TestFileStoreIsAValidStore (0.45s)
    Get with a canceled context returned no error
    Put with a canceled context returned no error
    Delete with a canceled context returned no error
    Put with a canceled context stored the record
    Delete with a canceled context deleted the record
```

基于接受 `ctx` 的数据库客户端的存储，会自己返回这些错误。测试应用 [test/apps/session](../../test/apps/session/hooks_test.go) 对一个把记录放在文件里的存储运行了这个测试。

## 在哪里调用什么 {#where-to-call-what}

| 位置 | `Session` | `SetSession`、`ClearSession` |
| --- | --- | --- |
| `next` 之前的 `OnRequest` | 可用 | 可用 |
| `next` 返回之后的 `OnRequest` | 可用 | 返回错误 |
| 方法处理函数 | 可用 | 可用 |
| `Load`、`Layout`、`OnError` | 可用 | 返回错误 |
| 响应发出之后 | 可用 | 返回错误 |

`Load` 为 GET 运行，而 GET 不能改变任何东西，所以登录应该放在方法处理函数或 `OnRequest` 里。

在带 `Cache` 的页面上，GET 和 HEAD 请求是匿名的：不管是谁在请求，那里的 `Session` 都是 nil，`SetSession` 和 `ClearSession` 会返回错误。Tifa 在那里承诺什么，以及通过 `tifa.SetHTTPHandler` 加的 `net/http` 中间件放在哪里，见 [给 CDN 用的缓存](ssr-and-cache.zh-CN.md#cache-for-cdns)。

## 登录库 {#sign-in-libraries}

会话由 Tifa 存储。用户是谁，由你的应用或某个库来检查：密码哈希（bcrypt、argon2）、OAuth 授权码流程及其 `state` 检查（`golang.org/x/oauth2`）、两步验证。检查成功后，调用 `tifa.SetSession`。这类库的临时状态，比如 OAuth 的 `state`，放在库自己的一个短期 cookie 里。要在 Go 里检查访问权限，就读 `tifa.Session`。

来自其他站点的写入，会在这一切运行之前就被拒绝（见[跨站写入](../build-and-deploy/headers-and-origins.zh-CN.md#cross-site-writes)），而且 cookie 是 `SameSite=Lax`，所以另一个站点上的表单无法让用户退出登录，也无法代表用户行事。要让这一点成立，请确保 GET、HEAD 和 OPTIONS 永远不改变任何东西：退出登录要用 POST。 Tifa 还防了什么、还有什么留给你，见[安全](../guides/security.zh-CN.md)。

## 构建时的检查 {#checks-at-build-time}

`tifa build` 和 `tifa check` 会读取你的模块里每一处 `tifa.SetSession`、`tifa.Session` 和 `tifa.ClearSession` 的调用，在下列情况下失败：

- 这些调用没有全部使用同一个会话类型（错误会显示两处位置），或者有调用在泛型函数里使用了类型参数
- 调用了 `Session` 或 `ClearSession`，却没有任何地方调用 `SetSession`
- 这个类型不是结构体，或者无法从 JSON 读回来
- `Load` 或 `Layout`，或它们把 Ctx 传进去的函数，调用了 `SetSession` 或 `ClearSession`
- 某个页面或 API 路由的路径以 `/__tifa/` 开头，或者 `public/__tifa/` 里有文件（`/__tifa/session` 是会话响应的路径）

会话类型可以有一个方法 `Exposed() M`（指针接收者也可以）。它返回会话中可以交给浏览器的那部分。这部分会被写进页面 HTML，并保存在浏览器的 `localStorage` 里，所以其中的每个字段，都必须可以放心地在访问者的设备上长期保留。不要放凭据和密钥，并且始终用服务器端的会话检查访问权限。`M` 遵循 `Load` 结果的规则，构建会写出它的 TypeScript 类型，由 `tifa/client` 导出为 `SessionData`：

```tsx
import type { SessionData } from 'tifa/client'
```

当类型没有 `Exposed` 方法，或应用没有会话时，`SessionData` 是 `never`。这个类型写在 `.tifa/session.gen.ts` 里，那里也有给运行时用的 `SessionEnabled`，不是给应用代码用的。带参数、有多个结果、或结果无法成为 JSON 的 `Exposed`，会让构建失败。

## 在浏览器里 {#in-the-browser}

浏览器保存一份「谁已登录」的副本，并在首次绘制时就显示它，这时页面的任何脚本都还没运行。页面用 `useSession` 读取它，`SignedIn` 和 `SignedOut` 按它来显示或隐藏一个元素。

```tsx
import { SignedIn, SignedOut, useSession } from 'tifa/client'

function Header() {
	const { signedIn, data, ready } = useSession()
	return (
		<header>
			<SignedOut><a href="/login">Sign in</a></SignedOut>
			<SignedIn><span>Hello, {data?.name}</span></SignedIn>
		</header>
	)
}
```

- `useSession()` 返回 `{ signedIn, data, ready }`。`data` 是 `Exposed` 方法返回的内容（它的类型是 `SessionData`），当类型没有 `Exposed` 或没人登录时是 `undefined`。在从不调用 `tifa.SetSession` 的应用里调用它，是类型错误，并且会抛出异常。
- `SignedIn` 和 `SignedOut` 只对已登录或未登录的人显示它们的子元素。每个都只接受恰好一个元素作为子元素，自己不渲染任何元素。子元素必须把它的 props 传给它的 DOM 元素，`<a>`、`<div>` 和展开 `...props` 的组件都是这样。在开发环境里，不这样做的子元素会在挂载时以一条消息失败。Tifa 放在 head 开头的一段样式，会从首次绘制起就隐藏不对的那个，所以你不用写任何 CSS。
- 当浏览器知道要显示哪种状态时，`ready` 是 `true`。对于为访问者渲染的页面，它从第一次渲染起就是 `true`。不带会话的页面见下文。
- 这份副本只决定显示什么，绝不决定某人能做什么。访问权限要在 Go 里检查。

会话随每个面向某一位访问者的响应到达浏览器，形式是 `{ "signedIn": true, "data": ... }`（只有类型有 `Exposed` 时才有 `data`，没人登录时是 `{ "signedIn": false }`）：

| 响应 | 它带什么 |
| --- | --- |
| 页面 | 页面里的会话，以及服务器写下的 `<html data-session="in">` 或 `<html data-session="out">`。`SSR = false` 时，会话在启动数据里 |
| 导航数据 | `{ "routes": { ... }, "session": ... }`，所以点击链接会带来当前的会话 |
| 一次让某人登录或退出的处理函数调用 | `{ "result": ..., "session": ... }`。没有结果时，是 `{ "session": ... }`，而不是 204。没有动过会话的调用没有 `session`，仍是 204。重定向也会带上它，错误则不带 |
| `GET /__tifa/session` | 这个请求的会话，`Cache-Control: private, no-store`。它在 `OnRequest` 里运行，所以让访问者登录的中间件必须放行它 |

不带 `X-Tifa-Call` 头的请求（curl、表单、别的程序）得到的响应和以前一样。

浏览器把会话保存在内存里，也保存在 `localStorage` 里，键名是 cookie 名加 `:v1`，比如 `__Host-tifa_session:v1`。凡是带着会话到达的响应，都会写这两份副本：已登录就写入，已退出就清除，不带会话的响应则不动它们，`<html data-session>` 也跟着变。页面带的会话优先于 `localStorage`。如果 `localStorage` 在任何一步失败，比如在隐私窗口里，就使用内存里的副本，什么也不会因此停下。请求交叉到达时，最后发出的那个请求的响应胜出。

服务器对每个访问者都一样地响应的页面，比如带 `Cache` 的（见[给 CDN 的缓存](ssr-and-cache.zh-CN.md#cache-for-cdns)），不带会话，也没有 `data-session`。只在那里，head 开头的一段脚本会在首次绘制之前，根据 `localStorage` 设置 `data-session="in"`，`useSession` 在页面水合完成之前渲染为未登录，然后切换到 `localStorage` 里的副本，并向 `/__tifa/session` 请求一次来核对。没有副本时它什么也不请求。如果读不了 `localStorage`，也会请求一次。

从浏览器调用处理函数，当这次调用改变了会话时，会更新这份副本。之后导航的数据请求也会带来会话。这对每种形式的处理函数都成立，没有输入也没有结果的也一样（`func POST(c *tifa.Ctx) error { return tifa.ClearSession(c) }`）：当它什么也没写时，响应就只有会话。自己用 `c.JSON` 或 `c.Text` 写响应的处理函数，无法在响应里带上会话，所以要在不这样做的处理函数里登录和退出。

带会话的响应必须声明 `Cache-Control: no-store`，或者同时声明 `private` 和 `no-cache`，两者都不能带字段列表（`private="Set-Cookie"` 不算），也不能有 `must-understand`。不去动这个头，它就是 `private, no-cache`。它与渲染记忆 memo 和 CDN 如何配合，见 [渲染记忆 memo](../reference/memo.zh-CN.md#signed-in-and-private-pages)。如果 `OnRequest`、某个 `Load` 或某个处理函数在页面、页面的启动数据或调用的信封上设置了别的值，请求会通过 `OnError` 以意外错误失败，错误响应是 `no-store`，不带 `ETag` 或 `Last-Modified`。导航重定向，以及已经在报告错误的导航响应，不会再次抛出：服务器会记录它，清除验证头，并发送 `no-store`。`/__tifa/session` 永远是 `private, no-store`，没有验证头，中间件自己响应它时也一样。自己写响应的处理函数不检查。

`data-session` 是 Tifa 在 `<html>` 上维护的属性。根布局的 `documentHead.htmlAttributes` 如果设置了它，服务器启动时就会停下（VM 预热失败，`tifa dev` 会显示错误），路由的 `htmlAttributes` 如果设置了它，渲染就会失败。

**Content Security Policy。** 这段脚本和样式是内联的，每个页面都一样。设置了 `server.csp` 时，Tifa 会自己把它们的哈希放进每个页面的策略里（见 [Content-Security-Policy](../build-and-deploy/headers-and-origins.zh-CN.md#content-security-policy)）。用 `<meta>` 标签里的 CSP 时它们不受影响，因为它们出现在这个标签之前。如果你在响应头里放了自己的 CSP，请允许它们的哈希：

```text
script-src 'sha256-NWjzg1BGj/5CJBw80xzDV4Gmw6vQUG2ExZLUcX4mWkE='
style-src 'sha256-dutb67YDz+meUugqDFx3RsopE/hCQ31NRHukbTNY+OQ='
```

这些适用于默认的 cookie `__Host-tifa_session`。脚本里有 `localStorage` 键的名字，所以当你设置了 `session.cookie` 时，它会有另一个哈希，在 `tifa dev` 下也一样，那里的 cookie 是 `tifa_session`（脚本的哈希是 `sha256-RXrgEYZIzBu7H8LtSsupWFiD5XE/6dvi74b6rHR+nVg=`）。请从你构建出的某个页面里取出这段脚本，再计算它的哈希。

**它做不到的事。** 另一个标签页会一直显示旧状态，直到它下一次收到带会话的响应，而在那里提交的表单已经在用新 cookie 了。在另一台设备上退出登录后，来自 `localStorage` 的页面会一直显示旧名字，直到核对结果返回。登录成为另一个人之后，回到浏览器前进后退缓存里的页面，显示的是原样的旧页面。切换账号时仍在进行的预加载，可能带来旧账号的数据。你自己写的、会改变会话的 `fetch`，Tifa 并不知道：之后要调用 `router.invalidate()`，当路由器保留的页面不能展示给下一个人时，还要调用 `router.clearCache()`。让 `Exposed()` 只包含浏览器可以留在设备上的内容，比如名字和头像，不要有凭据、邮箱地址或电话号码。

## 测试会话 {#testing-sessions}

`tifatest.NewCtx(t, r)` 给 `Ctx` 一个属于这个测试的内存存储，所以 `SetSession` 和 `ClearSession` 可以工作，记录器里会有 `Set-Cookie`。同一个测试里的所有 `Ctx` 共用这个存储，所以一个 `Ctx` 的 cookie 可以让下一个登录：用 `r.AddCookie` 把它加到请求上。要测试需要有人登录的函数，可以跳过 cookie：

```go
c, _ := tifatest.NewCtx(t, httptest.NewRequest("GET", "/", nil))
tifatest.SignIn(t, c, &auth.Session{UserID: 1})
```

`SignIn` 只设置请求的状态。它不检查 cookie 和存储的流程，那部分由 Tifa 自己的测试覆盖。要测试你自己的存储，见[测试存储](#testing-a-store)。

要像浏览器那样测试登录，从表单、经过 cookie 到下一个页面，请用 `tifatest.Start` 启动应用：它的客户端会保存 cookie，存储则是应用在 `OnStart` 里设置的那个，或者测试服务器的内存存储（见[测试整个请求](testing-apps.zh-CN.md#testing-a-whole-request)）。

## 错误 {#errors}

| 消息开头是 | 何时出现 | 怎么办 |
| --- | --- | --- |
| `tifa: SetSession cannot be called while a page with Cache is served` | 调用发生在带 `Cache` 的页面的 GET 或 HEAD 上 | 把登录和 OAuth 回调放在没有 `Cache` 的路由上 |
| `tifa: SetSession cannot be called in a Load or a Layout` | 调用发生在 `Load` 或 `Layout` 里 | 在方法处理函数里登录，或在 `next` 之前的 `OnRequest` 里登录 |
| `tifa: SetSession cannot be called in OnRequest after next returned` | `OnRequest` 在 `next(c)` 之后调用了它 | 在 `next` 之前调用 |
| `tifa: SetSession cannot be called in OnError` 或 `once the response is sent` | 太晚了，响应已经决定或已经写出 | 把调用提前。`ClearSession` 也一样 |
| `tifa: SetSession has no session store` | 这个 `Ctx` 不是来自服务器 | 在测试里，用 `tifatest.NewCtx` 生成 `Ctx` |
| `tifa: SetSession needs a session, got nil` 或 `the session encodes as null` | 指针是 nil，或者值是 JSON `null` | 传一个指向结构体的指针 |
| `tifa: the session takes N bytes, over the limit of 4 KiB` | 记录太大 | 保留一个 ID 和几个用来显示的字段，其余放在你的数据库里 |
| `tifa: storing the session` | 存储的 `Put` 失败了 | 存储的错误被包在里面，去那里看 |
| `tifa: the previous session was not revoked, so the sign-in did not complete` | 旧记录的 `Delete` 失败了 | 登录没有完成：不会发出 cookie，新记录会被再次删除，旧会话保持不变。去看存储 |
| `tifa: deleting the session failed, so its record stays valid until it expires` | `ClearSession` 没能删除记录 | cookie 被丢弃了，但记录会一直有效到过期。去看存储 |
| `tifa: reading the session` | 存储的 `Get` 失败了 | 请求通过 `OnError` 以意外错误失败 |
| 点出两个会话类型的 panic | `SetSession` 和 `Session` 用了不同的类型 | 整个应用用同一个类型。构建会先报告它 |
