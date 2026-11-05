---
summary: '会话在 Tifa 应用里怎么工作：在 Go 里登录和登出、会话 cookie、内存存储，以及用自己写的存储把会话放到 Redis 或 SQL、每个调用能在哪里调、构建时的检查、浏览器怎么显示谁登录了、怎么测试、每个错误是什么意思'
read_when:
  - 让用户登录登出，或读谁登录了
  - 把会话放到 Redis、SQL 或文件里而不是进程内存，或者给 tifa.SetSessionStore 写存储
  - 用 tifa.NewMemoryStore 或 session.memory_limit 定内存存储的大小
  - 在页头显示登录用户的名字，`useSession`、`SignedIn`、`data-session`，或者 CSP 拦住了 head 最前面的脚本
  - 改会话 cookie、会话有效期或 cookie 名
  - SetSession 或 ClearSession 返回错误，或者构建报会话类型的错
  - 给需要有人登录的代码写 Go 测试
  - 改 internal/core（session.go、sessionstore.go）或 js/runtime/session.tsx
---

[English](sessions.md) · 简体中文

# 会话

会话是「谁登录了」的记录，存在服务器上，靠一个 cookie 找到。Tifa 管记录、token、cookie 和过期。验证密码、决定什么时候登录或登出，是你的代码的事。

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
		return tifa.Error{Status: 401, Message: "邮箱或密码不对"}
	}
	if err := tifa.SetSession(c, &auth.Session{UserID: u.ID, Name: u.Name}); err != nil {
		return err
	}
	return tifa.Redirect("/", 303)
}

// 任何拿得到 Ctx 的地方：OnRequest、方法处理函数、Load
s := tifa.Session[auth.Session](c) // 没登录是 nil

// app/logout/route.go
err := tifa.ClearSession(c)
```

- `tifa.SetSession(c, &s)` 让这个请求登录。它生成一个新的随机 token（32 字节，43 个字符），把 `s` 按 JSON 存在 token 的 SHA-256 下面，删掉请求原来带的会话，再设 cookie。每次调用都换新 token，所以登录之后不会沿用攻击者事先塞进来的 token。旧会话删不掉时，登录失败并返回错误，不发 cookie。
- `tifa.Session[S](c)` 返回本请求的会话，没有就是 nil。服务器在 `OnRequest` 之前就读好了，所以每个钩子、`Load` 和方法处理函数都读得到。第一次调用时解码进 `S`，同时在跑的 `Load` 共用同一个指针，当成只读用。整个应用用同一个类型 `S`，换别的类型调用会 panic。存着的记录对不上 `S` 时，记录被删掉，cookie 被清掉，这个请求按没登录处理。
- `tifa.ClearSession(c)` 删记录，清 cookie，之后 `Session` 返回 nil。没登录时什么都不做。存储删不掉记录时，cookie 仍然会清，返回的错误会说明记录在过期前仍然有效。
- 记录加上 Tifa 包在外面的信封不能超过 4 KiB。里面放一个 ID 和几个展示用的字段，其余放在你自己的数据库里。能写这个存储的人可以让任何人登录，所以要像保护数据库一样保护它。

## cookie

cookie 叫 `__Host-tifa_session`，带 `HttpOnly`、`Secure`、`SameSite=Lax`、`Path=/`，没有 `Domain`。会话从 `SetSession` 起算 30 天，不续期，所以请求从不写存储。名字和时长用 `session.cookie` 和 `session.max_age` 改（见[配置](config.zh-CN.md#配置项)）。`tifa dev` 下 cookie 叫 `tifa_session`，不带 `Secure`，因为 dev 在 localhost 上跑普通 HTTP。不是 43 个 `A-Z a-z 0-9 - _` 字符的 cookie 会被直接丢掉，不去问存储。存储里已经没有记录的 cookie（过期了，或者用内存存储时进程重启过）也会被丢掉，这个请求按没登录处理。

设置或清除 cookie 的回答，不管应用设了什么，都以 `Cache-Control: no-store` 发出，不带 `ETag` 和 `Last-Modified`，状态码保持原样，永远不会变成 304。

## 会话存在哪

不设存储时，会话放在进程的内存里，上限是 `session.memory_limit`（64 MiB）。满了就先挤掉最早过期的，重启后所有人都要重新登录。服务器启动时会打一行日志说明这一点。`tifa dev` 在你改 Go 代码后会重启，那时你会被登出。如果应用允许没登录的访客开会话，要在入口限流，否则灌满它会把真正的会话挤出去。

`tifa new` 生成的应用在 `OnStart` 里设了一个存储，把会话存进它的 SQLite 数据库（`sessions.go`，测试用的是 `tifatest.TestSessionStore`），所以重启或部署不会让任何人登出，`tifa dev` 里也不会被登出。

想把会话放到别处，比如 Redis 或 SQL，在 `OnStart` 里给它一个存储：

```go
func OnStart(ctx context.Context) error {
	tifa.SetSessionStore(redisStore{client})
	return nil
}
```

`tifa.SetSessionStore` 和 `tifa.SetHTTPHandler` 一样，服务开始监听后再调会 panic。存储实现 `tifa.SessionStore`：

```go
type SessionStore interface {
	Get(ctx context.Context, token string) (data []byte, found bool, err error)
	Put(ctx context.Context, token string, data []byte, expiry time.Time) error
	Delete(ctx context.Context, token string) error
}
```

- `token` 是 cookie 里 token 的 SHA-256（base64url），不是 cookie 本身。
- 键不存在、已过期或无效时，`Get` 返回 `found == false` 和 nil 错误。只有存储自己出了故障才返回错误。这时请求按意外错误走 `OnError`，不会当成没登录继续，那样会把故障藏起来。
- `Put` 新建或覆盖记录，`expiry` 之后 `Get` 不能再返回它。键不存在时 `Delete` 返回 nil。
- 三个方法会被很多请求同时调用。`Put` 不能留着 `data` 切片，`Get` 不能返回存储还在用的切片。`ctx` 结束时都要返回错误。

### 内存存储

`OnStart` 没设存储时，Tifa 用的是容量为 `session.memory_limit` 字节的 `*tifa.MemoryStore`。想自己建一个，比如给会话少留点内存，调 `tifa.NewMemoryStore(maxBytes)` 再交出去：

```go
func OnStart(ctx context.Context) error {
	tifa.SetSessionStore(tifa.NewMemoryStore(8 << 20))
	return nil
}
```

- 你设的存储会取代默认的，所以 `session.memory_limit` 不再起作用。
- `maxBytes` 对每条记录计入它的键、数据和一笔固定开销。存满时，`Put` 先挤掉最早过期的记录。单条记录自己就超过 `maxBytes` 时，`Put` 返回错误，`SetSession` 也就失败。`maxBytes` 必须是正数，否则 `NewMemoryStore` 会 panic。
- 它起了一个 goroutine，每分钟清一次过期记录。服务器会自己停掉它建的那个。你自己建的要自己调 `Close`（测试里写 `defer m.Close()`）。`Close` 之后存储照常工作，因为 `Get` 从不返回过期的记录。

### 自己写存储

在 Redis、SQL 或文件上实现 `tifa.SessionStore` 的三个方法，再在 `OnStart` 里交给 `tifa.SetSessionStore`。会话测试应用（`test/apps/session`）把会话存在一个文件里，这样第二个进程能找到第一个进程的会话。它每次调用都重写整个文件，测试够用，跑真实流量不行：

```go
type fileStore struct {
	mu   sync.Mutex
	path string
}

type fileRecord struct {
	Data    []byte    `json:"data"`
	Expires time.Time `json:"expires"`
}

func (s *fileStore) Get(ctx context.Context, key string) ([]byte, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	records, err := s.load()
	if err != nil {
		return nil, false, err
	}
	r, ok := records[key]
	if !ok || !time.Now().Before(r.Expires) {
		return nil, false, nil
	}
	return r.Data, true, nil
}

func (s *fileStore) Put(ctx context.Context, key string, data []byte, expiry time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	records, err := s.load()
	if err != nil {
		return err
	}
	records[key] = fileRecord{Data: append([]byte(nil), data...), Expires: expiry}
	return s.save(records)
}

func (s *fileStore) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	records, err := s.load()
	if err != nil {
		return err
	}
	delete(records, key)
	return s.save(records)
}
```

`load` 把文件读进一个 map，文件还不存在时就是没有记录，文件读不出或解不开时返回错误。`save` 把 map 写回去。注意三个方法做了什么：每个方法先返回 `ctx.Err()`，键不存在或已过期是 `found == false` 加 nil 错误，文件读不出是错误而不是空存储，`Put` 复制了 `data`，`Delete` 遇到不存在的键不报错。

用 Redis 或 SQL 时，让数据库自己清过期记录（Redis 用 `EXPIRE` 或 `SET ... EX`，SQL 定期 `DELETE` 旧行，或在过期列上建索引），`Get` 里仍然要检查 `expiry`。

### 测试存储

你写的每个存储都要配一个测试。`tifatest.TestSessionStore(t, store)` 就是现成的这个测试：它按上面的规则检查你的存储，毛病在 `go test` 里就暴露，不会变成用户莫名其妙被登出。

```go
func TestRedisStoreIsAValidStore(t *testing.T) {
	store := newRedisStore(testClient(t)) // 只有这个测试在用的空数据库
	tifatest.TestSessionStore(t, store)
}
```

（`newRedisStore` 和 `testClient` 代表你自己的代码。）Tifa 自己的测试对内存存储也是这样调用的：

```go
func TestMemoryStoreIsAValidStore(t *testing.T) {
	m := tifa.NewMemoryStore(1 << 20)
	defer m.Close()
	tifatest.TestSessionStore(t, m)
}
```

它按顺序检查：

- 从没存过的键是 `found == false` 加 nil 错误。
- `Put` 之后 `Get` 返回记录，第二次 `Put` 替换它。
- `Delete` 不存在的键返回 nil，`Delete` 会删掉记录。
- 过期时间已过的记录找不到，写入时就已过期的也一样。它要等 400 毫秒让一条短命记录过期，所以整个测试要跑半秒左右。
- 存储不保留交给 `Put` 的切片，`Get` 返回的切片可以放心修改。
- `ctx` 已结束时，`Get`、`Put`、`Delete` 返回错误，而且结束了 `ctx` 的 `Put` 或 `Delete` 不改变任何东西。
- 八个 goroutine 同时对同一个键调 `Put`、`Get`、`Delete`。加 `-race` 运行可以抓数据竞争。

测试本身的规则：

- 给它一个空的、没有别人在用的存储。它会写几个 43 个字符的键（Tifa 用的形式），结束时删掉。别的键它不会清理。
- `t` 可以是 `*testing.T` 或 `*testing.B`。每项失败的检查它都用 `t.Errorf` 报告，所以一次运行能列出所有问题。第一个 `Put` 失败时用 `t.Fatalf` 停下。
- 它只测一个进程。两台服务器是否看到同样的记录、数据库挂了会怎样，要你自己测。

每条失败消息都点明了规则。上面的文件存储如果去掉每个方法开头的 `ctx.Err()` 检查，会这样失败：

```
--- FAIL: TestFileStoreIsAValidStore (0.45s)
    Get with a canceled context returned no error
    Put with a canceled context returned no error
    Delete with a canceled context returned no error
    Put with a canceled context stored the record
    Delete with a canceled context deleted the record
```

用接受 `ctx` 的数据库客户端写的存储会自己返回这些错误。测试应用在 `hooks_test.go` 里对它的文件存储跑了这个测试。

## 哪里能调什么

| 位置 | `Session` | `SetSession`、`ClearSession` |
| --- | --- | --- |
| `OnRequest` 里 `next` 之前 | 可以 | 可以 |
| `OnRequest` 里 `next` 返回之后 | 可以 | 返回错误 |
| 方法处理函数 | 可以 | 可以 |
| `Load`、`Layout`、`OnError` | 可以 | 返回错误 |
| 响应发出之后 | 可以 | 返回错误 |

`Load` 跑在 GET 上，GET 不该改任何东西，所以登录放在方法处理函数或 `OnRequest` 里。

开了 `Cache` 的页面，GET 和 HEAD 请求是匿名的：不管谁来，`Session` 都是 nil，`SetSession` 和 `ClearSession` 返回错误。这个承诺说的是 Tifa 交给你的代码的请求头。用 `tifa.SetHTTPHandler` 挂的 `net/http` 中间件放进 context 的值仍会到达，所以认人放在 `OnRequest` 里，不要放在那个中间件里（见[整页缓存](app.zh-CN.md#整页缓存)）。

## 和登录库的分工

Tifa 存会话。用户是谁由你的应用或登录库来认：密码哈希（bcrypt、argon2）、OAuth 授权码流程和它的 `state` 校验（`golang.org/x/oauth2`）、两步验证。认证成功后调 `tifa.SetSession`。这类库自带的临时状态，比如 OAuth 的 `state`，放在库自己的短命 cookie 里。鉴权在 Go 里读 `tifa.Session` 来做。

从别的网站发来的写请求会在这一切之前被拒绝（见[跨站写请求](config.zh-CN.md#跨站写请求)），cookie 又是 `SameSite=Lax`，所以别的网站上的表单不能替用户登出或操作。要让这一点成立，GET、HEAD 和 OPTIONS 都不能改任何东西，登出用 POST。

## 构建时的检查

`tifa build` 和 `tifa check` 会读模块里每一处 `tifa.SetSession`、`tifa.Session` 和 `tifa.ClearSession` 的调用，遇到下面的情况就失败：

- 调用用的不是同一个会话类型（错误会写出两处位置），或者在泛型函数里用类型参数调用
- 有 `Session` 或 `ClearSession` 的调用，却没有任何地方调用 `SetSession`
- 类型不是 struct，或者不能从 JSON 读回来
- `Load`、`Layout`，或者它们传下 Ctx 的函数调用了 `SetSession` 或 `ClearSession`
- 页面或 API 路由的路径以 `/__tifa/` 开头，或者 `public/__tifa/` 下有文件（`/__tifa/session` 是会话回答的路径）

会话类型可以有方法 `Exposed() M`（指针接收者也行）。`Exposed` 返回允许交给浏览器的那部分 session。它会写进页面 HTML，并保存在浏览器的 localStorage 里。放进这里的字段要能接受长期留在访客的设备上。不要放凭据和需要保密的信息。权限判断永远用服务端的 session。`M` 遵守 `Load` 结果的规则，构建把它的 TypeScript 类型写进 `.tifa/session.gen.ts`：

```tsx
import type { SessionData } from '@/.tifa/session.gen'
```

同一个文件还导出 `SessionEnabled`：应用调用了 `tifa.SetSession` 时是 `true`，否则是 `false`。类型没有 `Exposed` 方法，或应用没有会话时，`SessionData` 是 `never`。`Exposed` 带参数、有多个返回值，或者返回值不能是 JSON，构建会失败。

## 在浏览器里

浏览器里存着一份「谁登录了」的副本，第一次绘制时就按它显示，那时页面上的脚本还没跑。页面用 `useSession` 读它，用 `SignedIn` 和 `SignedOut` 按它显示或隐藏元素。

```tsx
import { SignedIn, SignedOut, useSession } from 'tifa/client'

function Header() {
	const { signedIn, data, ready } = useSession()
	return (
		<header>
			<SignedOut><a href="/login">登录</a></SignedOut>
			<SignedIn><span>你好，{data?.name}</span></SignedIn>
		</header>
	)
}
```

- `useSession()` 返回 `{ signedIn, data, ready }`。`data` 是 `Exposed` 方法返回的值（类型是 `SessionData`），类型没有 `Exposed` 或没人登录时是 `undefined`。在从不调用 `tifa.SetSession` 的应用里调用它是类型错误，运行时也会抛错。
- `SignedIn` 和 `SignedOut` 只在有人登录或没人登录时显示它们的子元素。它们必须正好有一个元素作子节点，自己不产生任何元素。子元素要把收到的 props 传给自己的 DOM 元素，`<a>`、`<div>` 和展开 `...props` 的组件都可以。开发模式下，不这样做的子元素在挂载时会报错并说明原因。Tifa 放在 head 最前面的一条样式会从第一次绘制起隐藏不该显示的那个，你不用写 CSS。
- `ready` 在浏览器知道自己该显示哪个状态之后是 `true`。为访客渲染的页面从第一次渲染起就是 `true`。不带会话的页面见下文。
- 副本只决定显示什么，不决定谁能做什么。权限在 Go 里检查。

凡是只给一个访客的回答都带会话，形状是 `{ "signedIn": true, "data": ... }`（只有类型有 `Exposed` 才有 `data`，没人登录时是 `{ "signedIn": false }`）：

| 回答 | 带什么 |
| --- | --- |
| 页面 | 页面里带着会话，服务器在 `<html>` 上写 `data-session="in"` 或 `data-session="out"`。`SSR = false` 时会话在启动数据里 |
| 导航数据 | `{ "routes": { ... }, "session": ... }`，点链接就拿到当前的会话 |
| 让某人登录或登出的处理函数调用 | `{ "result": ..., "session": ... }`。没有结果时是 `{ "session": ... }`，不再是 204。没动会话的调用没有 `session`，仍然是 204。重定向也带，错误不带 |
| `GET /__tifa/session` | 本次请求的会话，`Cache-Control: private, no-store`。它在 `OnRequest` 里运行，所以让访客先登录才能通过的中间件必须放行它 |

不带 `X-Tifa-Call` 请求头的请求（curl、表单、别的程序）得到的回答和以前一样。

浏览器把会话存在内存里，也存在 `localStorage` 里，键是 cookie 名加 `:v1`，比如 `__Host-tifa_session:v1`。凡是带会话的回答到了，两处副本一起写：登录了就写，登出了就清，回答里没有会话就不动，`<html data-session>` 跟着改。页面自带的会话优先于 `localStorage`。`localStorage` 任何一步失败（比如无痕窗口），就只用内存里的副本，不会出错中断。请求交错时，以最后发出的那个请求的回答为准。

服务器对所有访客回答得一样的页面，比如开了 `Cache` 的页面（见[整页缓存](app.zh-CN.md#整页缓存)），不带会话，也没有 `data-session`。只有这样的页面里，head 最前面的脚本在第一次绘制前按 `localStorage` 设好 `data-session="in"`，`useSession` 在页面水合完成前按未登录渲染，之后切到 `localStorage` 里的副本，并向 `/__tifa/session` 问一次来核对。没有副本就不问。`localStorage` 读不了时也问一次。

在浏览器里调用处理函数，如果这次调用改了会话，副本随之更新。之后导航的数据请求也会带回会话。登录、登出的处理函数要有入参类型或结果类型（`func POST(c *tifa.Ctx, in Login) error`）。自己写响应的处理函数（`func POST(c *tifa.Ctx) error`）没法把会话放进回答里。

带着会话的回答，`Cache-Control` 必须是 `no-store`，或者同时有 `private` 和 `no-cache`，两个都不带字段列表（`private="Set-Cookie"` 不算），也不能有 `must-understand`。不设这个头，它就是 `private, no-cache`。`OnRequest`、`Load` 或处理函数在页面、页面的启动数据或调用的信封上设了别的值，请求按意外错误交给 `OnError`，错误回答是 `no-store`，没有 `ETag` 和 `Last-Modified`。导航重定向和已经在报错的导航回答不再抛一次：服务器记日志、清验证器、改成 `no-store`。`/__tifa/session` 永远是不带验证器的 `private, no-store`，中间件自己回答它时也一样。自己写响应的处理函数不检查。

`data-session` 是 Tifa 在 `<html>` 上管的属性。根 Layout 的 `htmlAttributes` 设了它，服务器启动时就会停下（VM 预热失败，`tifa dev` 会显示错误）。路由的 `htmlAttributes` 设了它，渲染会失败。

**内容安全策略（CSP）。** 脚本和样式都是内联的，每个页面都一样。开了 `server.csp` 时，Tifa 自己把它们的哈希加进每个页面的策略（见 [Content-Security-Policy](config.zh-CN.md#content-security-policy)）。CSP 写在 `<meta>` 标签里时它们不受影响，因为它们排在标签前面。自己在响应头里写 CSP 时，放行它们的哈希：

```text
script-src 'sha256-NWjzg1BGj/5CJBw80xzDV4Gmw6vQUG2ExZLUcX4mWkE='
style-src 'sha256-dutb67YDz+meUugqDFx3RsopE/hCQ31NRHukbTNY+OQ='
```

这是默认 cookie `__Host-tifa_session` 的哈希。脚本里写着 `localStorage` 键的名字，所以设了 `session.cookie` 之后脚本的哈希会变，`tifa dev` 下 cookie 是 `tifa_session`，脚本的哈希是 `sha256-RXrgEYZIzBu7H8LtSsupWFiD5XE/6dvi74b6rHR+nVg=`。从你的构建出的页面里取出脚本再算哈希。

**做不到的事。** 另一个标签页在它下一次带会话的回答到来之前显示旧状态，在那里提交的表单已经用新的 cookie。在别的设备上登出后，从 `localStorage` 出来的页面会先显示旧名字，直到核对的回答回来。在同一个标签页登录成别人之后按后退，浏览器的前进后退缓存里恢复的是原样的旧页面。切换账号时正在进行的预加载，可能带回旧账号的数据。你自己写的 `fetch` 改了会话，Tifa 不知道，在它之后调 `router.invalidate()`，路由器留着的页面不该给下一个人看时再调 `router.clearCache()`。`Exposed()` 只放允许留在设备上的东西，比如名字和头像，不要放凭证、邮箱或手机号。

## 测试会话

`tifatest.NewCtx(t, r)` 给 `Ctx` 配了一个只属于这个测试的内存存储，所以 `SetSession` 和 `ClearSession` 能用，recorder 里有 `Set-Cookie`。同一个测试的所有 `Ctx` 共用这个存储，所以一个 `Ctx` 拿到的 cookie 可以让下一个登录：用 `r.AddCookie` 加到请求里。要测一个需要登录状态的函数，不用走 cookie：

```go
c, _ := tifatest.NewCtx(t, httptest.NewRequest("GET", "/", nil))
tifatest.WithSession(c, &auth.Session{UserID: 1})
```

`WithSession` 只设请求的状态，不验证 cookie 和存储的流程，那部分由 Tifa 自己的测试覆盖。测试你自己写的存储见[测试存储](#测试存储)。

要像浏览器那样测登录，从表单到 cookie 再到下一个页面，就用 `tifatest.Start` 启动应用：它的 client 会记下 cookie，存储是应用在 `OnStart` 里设的那个，没设就是测试服务器自己的内存存储（见[测整个请求](app.zh-CN.md#测整个请求)）。

## 错误

| 错误信息开头 | 什么时候 | 怎么办 |
| --- | --- | --- |
| `tifa: SetSession cannot be called while a page with Cache is served` | 在带 `Cache` 的页面的 GET 或 HEAD 上调用 | 把登录和 OAuth 回调放到没有 `Cache` 的路由上 |
| `tifa: SetSession cannot be called in a Load or a Layout` | 在 `Load` 或 `Layout` 里调用 | 在方法处理函数里登录，或在 `OnRequest` 里 `next` 之前登录 |
| `tifa: SetSession cannot be called in OnRequest after next returned` | `OnRequest` 在 `next(c)` 之后调用 | 在 `next` 之前调用 |
| `tifa: SetSession cannot be called in OnError` 或 `once the response is sent` | 太晚了，响应已经定了或已经发出 | 把调用提前。`ClearSession` 同理 |
| `tifa: SetSession has no session store` | 这个 `Ctx` 不是服务器建的 | 测试里用 `tifatest.NewCtx` 建 `Ctx` |
| `tifa: SetSession needs a session, got nil` 或 `the session encodes as null` | 指针是 nil，或值编码成了 JSON `null` | 传一个结构体指针 |
| `tifa: the session takes N bytes, over the limit of 4 KiB` | 记录太大 | 只放一个 ID 和几个展示字段，其余放自己的数据库 |
| `tifa: storing the session` | 存储的 `Put` 失败 | 错误里包着存储的错误，去那里查 |
| `tifa: the previous session was not revoked, so the sign-in did not complete` | 删旧记录的 `Delete` 失败 | 登录没完成：不发 cookie，新记录会被删掉，旧会话还在。查存储 |
| `tifa: deleting the session failed, so its record stays valid until it expires` | `ClearSession` 删不掉记录 | cookie 已清，但记录在过期前仍然有效。查存储 |
| `tifa: reading the session` | 存储的 `Get` 失败 | 请求按意外错误走 `OnError` |
| 一个点名两个会话类型的 panic | `SetSession` 和 `Session` 用了不同的类型 | 整个应用用同一个类型。构建会先报出来 |
