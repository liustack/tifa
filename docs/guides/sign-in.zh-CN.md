---
summary: '怎样用 Tifa 会话让用户登录和登出：argon2id、SetSession、保护页面、登出'
audience: app
order: 3
translatedFrom: cf75c5f2349d
read_when:
  - '给应用加登录和登出，或者把一些页面放在登录之后'
  - '选择怎样保存和检查密码'
  - '未登录的访客看到了本该私有的页面，或者已登录的页眉显示了错误的状态'
  - '把未登录的访客重定向到登录页，之后再带回他们要访问的页面'
---

# 登录和退出 {#sign-in-and-sign-out}

访问者用邮箱和密码登录，看到只有自己能打开的账号页，然后退出。会话由 Tifa 负责保管：随机令牌、cookie 和过期时间。你的代码做三件事：保存并检查密码，说明什么时候某人登录或退出，决定谁能看到什么。

这里的代码都是按所写的样子运行过的（用 `go tool tifa check`，并在浏览器里试过）。每个会话调用做什么、cookie 是什么，见[会话](../pages-and-data/sessions.zh-CN.md)。本指南不再重复。

## 谁已登录 {#who-is-signed-in}

根包里的 `session.go`：

```go
package signin

// Session is who is signed in. tifa keeps it on the server and finds it by the session cookie.
// Keep an ID and a few display fields in it, and the rest in your database.
type Session struct {
	UserID int64  `json:"userId"`
	Name   string `json:"name"`
}

// Me is the part of the session the browser may keep.
type Me struct {
	Name string `json:"name"`
}

func (s Session) Exposed() Me { return Me{Name: s.Name} }
```

整个应用只用一个会话类型。`Exposed` 是交给浏览器的那部分，会保存在访问者的设备上。不要在里面放任何机密（见[浏览器这一侧](../pages-and-data/sessions.zh-CN.md#in-the-browser)）。

## 密码 {#passwords}

只保存每个密码的哈希，用一种故意做得很慢的哈希算法生成。本指南用的是 argon2id，来自 `golang.org/x/crypto/argon2`。[OWASP 的密码存储指南](https://cheatsheetseries.owasp.org/cheatsheets/Password_Storage_Cheat_Sheet.html)把它列在第一位，并说只有在用不了 argon2 的地方才用 bcrypt。下面的参数是它给出的最低值：19 MiB 内存，2 轮，1 个并行通道。`examples/shop` 用的是 bcrypt，这是很好的第二选择：每个方向只要一次调用（`bcrypt.GenerateFromPassword`、`bcrypt.CompareHashAndPassword`），盐和成本都在哈希里面，每个密码最多 72 字节（[包文档](https://pkg.go.dev/golang.org/x/crypto/bcrypt)）。argon2 需要多几行代码，因为这个包只给你密钥，盐和参数要你自己记下来。

`passwords.go`：

```go
package signin

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// argon2id with the parameters OWASP lists first: 19 MiB of memory, 2 passes, 1 lane.
const (
	argonMemory  = 19 * 1024 // KiB
	argonTime    = 2
	argonThreads = 1
	argonKeyLen  = 32
	argonSaltLen = 16
)

// HashPassword returns the text to keep in the database: the parameters, the salt and the key.
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

// CheckHash reports whether password is the one that made hash.
func CheckHash(password, hash string) (bool, error) {
	parts := strings.Split(hash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, errors.New("signin: not an argon2id hash")
	}
	var version, memory, passes, threads int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, errors.New("signin: unknown argon2 version")
	}
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &passes, &threads); err != nil {
		return false, errors.New("signin: bad argon2 parameters")
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, err
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, err
	}
	got := argon2.IDKey([]byte(password), salt, uint32(passes), uint32(memory), uint8(threads), uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}
```

它生成的文本形如 `$argon2id$v=19$m=19456,t=2,p=1$<salt>$<key>`，这是书写 argon2 哈希的通用方式。参数跟着哈希一起保存，所以以后你可以调高参数，而旧的哈希仍然能校验。有人注册或修改密码时运行 `HashPassword`，把结果保存在你的用户表里。本指南没有注册页面。

## 检查登录 {#checking-the-sign-in}

`users.go`：

```go
package signin

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
)

// user is a row of your users table.
type user struct {
	id    int64
	email string
	name  string
	hash  string
}

// users stands in for the database. OnStart fills it.
var users []user

// dummyHash is checked when the e-mail is unknown, so that a wrong e-mail takes as long as a wrong
// password and the answer does not tell which one was wrong.
var dummyHash string

var (
	// ErrWrongLogin is the one answer for an unknown e-mail and a wrong password.
	ErrWrongLogin = errors.New("wrong e-mail or password")
	// ErrTooMany is returned while an e-mail has had too many wrong passwords.
	ErrTooMany = errors.New("too many attempts")
)

// hashing limits how many hashes run at once. Each takes 19 MiB, and a small server should not
// spend hundreds of MiB because someone sent many sign-ins together.
var hashing = make(chan struct{}, 4)

// CheckLogin returns who the e-mail and password belong to.
func CheckLogin(ctx context.Context, email, password string) (Session, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if blocked(email) {
		return Session{}, ErrTooMany
	}
	select {
	case hashing <- struct{}{}:
		defer func() { <-hashing }()
	case <-ctx.Done():
		return Session{}, ctx.Err()
	}
	var found *user
	hash := dummyHash
	usersMu.RLock()
	for _, u := range users {
		if u.email == email {
			found, hash = &u, u.hash
		}
	}
	usersMu.RUnlock()
	ok, err := CheckHash(password, hash)
	if err != nil {
		return Session{}, err
	}
	if !ok || found == nil {
		failed(email)
		return Session{}, ErrWrongLogin
	}
	forgive(email)
	return Session{UserID: found.id, Name: found.name}, nil
}

// Wrong passwords are counted per e-mail: five in a quarter of an hour block that e-mail for the
// rest of it. Whoever knows an e-mail can block it, which is the price of a limit this short.
const (
	maxFailures = 5
	window      = 15 * time.Minute
)

var (
	failMu sync.Mutex
	fails  = map[string][]time.Time{}
)

func recent(email string, now time.Time) []time.Time {
	kept := fails[email][:0]
	for _, t := range fails[email] {
		if now.Sub(t) < window {
			kept = append(kept, t)
		}
	}
	if len(kept) == 0 {
		delete(fails, email)
	} else {
		fails[email] = kept
	}
	return kept
}

func blocked(email string) bool {
	failMu.Lock()
	defer failMu.Unlock()
	return len(recent(email, time.Now())) >= maxFailures
}

func failed(email string) {
	failMu.Lock()
	defer failMu.Unlock()
	// Someone sending endless made-up e-mails must not grow the map without end.
	if len(fails) >= 10000 {
		clear(fails)
	}
	now := time.Now()
	fails[email] = append(recent(email, now), now)
}

func forgive(email string) {
	failMu.Lock()
	defer failMu.Unlock()
	delete(fails, email)
}

var usersMu sync.RWMutex

// Profile is what the account page shows.
type Profile struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// ProfileOf reads the account of a signed-in user from the database, not from the session.
func ProfileOf(id int64) Profile {
	usersMu.RLock()
	defer usersMu.RUnlock()
	for _, u := range users {
		if u.id == id {
			return Profile{Name: u.name, Email: u.email}
		}
	}
	return Profile{}
}

// Rename changes the display name.
func Rename(id int64, name string) {
	usersMu.Lock()
	defer usersMu.Unlock()
	for i := range users {
		if users[i].id == id {
			users[i].name = name
		}
	}
}
```

把 `users` 切片换成对你的数据库的查询。要保留的有：邮箱错和密码错给出同样的回答，即使邮箱未知也要做一次哈希检查，以及这两个限制。

- **同样的回答，同样的耗时。** 「没有这个用户」和「密码错误」会告诉攻击者哪些邮箱有账号。那个假哈希让这两种情况耗时差不多。
- **拖慢猜密码。** 这里展示的一种办法：按邮箱统计错误的密码，错五次之后，这个邮箱在这 15 分钟的剩余时间里不再被检查。它很便宜，也不需要应用之外的任何东西。代价是：知道你的用户邮箱的人，可以让这个用户几分钟登不上。如果你的应用跑在不止一台机器上，请把计数放在共享的存储里。
- **哈希队列。** 每次 argon2 检查在运行期间占用 19 MiB。同时最多四个，这样当有人狂刷表单时，哈希占用的内存上限约为 76 MiB。

## 守卫 {#the-guard}

`auth.go`：

```go
package signin

import (
	"net/url"

	"github.com/liustack/tifa"
)

// RequireUser returns who is signed in. For a visitor who is not, it returns a redirect to the
// sign-in page with the page they asked for in `next`, so the sign-in page can send them back.
func RequireUser(c *tifa.Ctx) (*Session, error) {
	s := tifa.Session[Session](c)
	if s == nil {
		return nil, SignInRedirect(c.Path())
	}
	return s, nil
}

// SignInRedirect sends the visitor to the sign-in page, which sends them on to next afterwards.
func SignInRedirect(next string) error {
	return tifa.Redirect("/login?next="+url.QueryEscape(next), 302)
}
```

`c.Path()` 是访问者在地址栏里看到的页面路径：里面有语言前缀，没有查询。页面整体加载时和点击链接请求它的数据时，它都一样，对处理函数来说，它是处理函数的路径。这里不该用 `c.Request()`：`Load` 不能调用它，而点击链接时，请求是数据请求，不是页面本身。`RequireUser` 可以在 `OnRequest`、`Load` 和它们调用的函数里工作。

`RequireUser` 生成的 `next` 里不带查询。把状态放在查询参数里的页面，要在自己的 `Load` 里把它们补回去，比如 `signin.SignInRedirect(c.Path()+"?tab="+url.QueryEscape(q.Tab))`。被许多页面共用的守卫没办法为所有页面都这样做，因为 `Load` 只有它自己的结构体点名的查询参数（见[查询参数](../pages-and-data/loading-data.zh-CN.md#query-parameters)）。`OnRequest` 不一样：hook 可以读取请求，所以下面的守卫保留了整个查询。服务器会在 `OnRequest` 运行之前，把数据请求的参数 `_tifa` 从 URL 里去掉，所以 `c.Request().URL.RawQuery` 对页面加载和点击链接都是访问者写下的那个查询。

根 Layout 的 `Layout` 函数对每个 URL 都会运行，包括 404 页和登录页，而在 `OnRequest` 里按路径写的守卫管不到它们。别让它返回没登录的访客不该看的数据，或者先在里面读会话。见[决定谁能做什么](security.zh-CN.md#decide-who-may-do-what)。

## hook {#the-hooks}

`hooks.go`：

```go
package signin

import (
	"context"
	"strings"

	"github.com/liustack/tifa"
)

// A demo account for this example. A real app has a registration page and a database.
const (
	DemoEmail    = "ada@example.com"
	DemoPassword = "correct horse battery"
)

func OnStart(context.Context) error {
	hash, err := HashPassword(DemoPassword)
	if err != nil {
		return err
	}
	users = []user{{id: 1, email: DemoEmail, name: "Ada", hash: hash}}
	dummyHash, err = HashPassword("not a password anyone has")
	return err
}

// OnRequest keeps visitors who are not signed in out of /account and sends them to the sign-in
// page, which sends them back afterwards. It runs for page loads and for the data requests of
// link clicks alike.
func OnRequest(c *tifa.Ctx, next func(*tifa.Ctx) error) error {
	if path := c.Path(); (path == "/account" || strings.HasPrefix(path, "/account/")) && tifa.Session[Session](c) == nil {
		if query := c.Request().URL.RawQuery; query != "" {
			path += "?" + query
		}
		return SignInRedirect(path)
	}
	return next(c)
}
```

没有人登录时，`tifa.Session[Session](c)` 是 nil。服务器在 `OnRequest` 之前就读取了会话，所以每个 hook、`Load` 和处理函数都可以去问。

## 登录路由 {#the-sign-in-route}

`app/login/route.go`：

```go
package login

import (
	"errors"
	"net/url"
	"strings"

	"github.com/liustack/tifa"

	"signin"
)

// View is what the form needs: where to go after signing in.
type View struct {
	Next string `json:"next"`
}

// Query is the query string of the page.
type Query struct {
	Next string `json:"next"`
}

func Load(c *tifa.Ctx, q Query) (View, error) {
	return View{Next: safeNext(q.Next)}, nil
}

// Credentials is the form.
type Credentials struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Next     string `json:"next"`
}

func (in Credentials) Validate() error {
	fields := tifa.Fields{}
	if strings.TrimSpace(in.Email) == "" {
		fields["email"] = "Enter your e-mail."
	}
	if in.Password == "" {
		fields["password"] = "Enter your password."
	}
	return fields.Err()
}

// POST signs the visitor in and sends them back to the page they came from.
func POST(c *tifa.Ctx, in Credentials) error {
	s, err := signin.CheckLogin(c.Context(), in.Email, in.Password)
	switch {
	case errors.Is(err, signin.ErrWrongLogin):
		return tifa.Error{Status: 401, Message: "Wrong e-mail or password."}
	case errors.Is(err, signin.ErrTooMany):
		return tifa.Error{Status: 429, Message: "Too many attempts. Try again in a few minutes."}
	case err != nil:
		return err
	}
	if err := tifa.SetSession(c, &s); err != nil {
		return err
	}
	return tifa.Redirect(safeNext(in.Next), 303)
}

const home = "/account"

// safeNext keeps the address on this site and sends everything else home. A browser drops tabs
// and line breaks from a URL and reads a backslash as a slash, so "/\n/host" and "/\host" would
// leave the site.
func safeNext(next string) string {
	if next == "" || strings.ContainsFunc(next, func(r rune) bool { return r < 0x20 || r == 0x7f || r == '\\' }) {
		return home
	}
	u, err := url.Parse(next)
	if err != nil || u.Scheme != "" || u.Host != "" || u.User != nil || u.Opaque != "" {
		return home
	}
	if !strings.HasPrefix(u.Path, "/") || strings.HasPrefix(u.Path, "//") || u.Path == "/login" {
		return home
	}
	return next
}
```

- 要在方法处理函数里登录，不要在 `Load` 里。`Load` 为 GET 运行，而 GET 不能改变任何东西，`SetSession` 在那里会返回错误（见[在哪里调用什么](../pages-and-data/sessions.zh-CN.md#where-to-call-what)）。
- `SetSession` 每次调用都会生成新的随机令牌，并丢掉请求原本带的会话，所以登录绝不会沿用别人选定的令牌。
- 意外错误（比如数据库挂了）会交给 `OnError`。密码错误是带状态码的 `tifa.Error`，不会交给它。
- `next` 来自 URL，所以任何人都可以往里放任何东西。`safeNext` 把它限制成本站的一个路径。没有它，你的登录页会把访问者送到攻击者指定的任何网站。不要去掉它。
- 处理函数返回 `tifa.Redirect(..., 303)`。页面的调用通过路由器跟随它，浏览器里的会话副本由同一个响应更新（见[浏览器这一侧](../pages-and-data/sessions.zh-CN.md#in-the-browser)）。

## 登录页 {#the-sign-in-page}

`app/login/page.tsx`：

```tsx
import { useState, useSyncExternalStore, type FormEvent } from 'react'
import { TifaError } from 'tifa/client'
import { page, POST } from './route.gen'

const useHydrated = () =>
  useSyncExternalStore(
    () => () => {},
    () => true,
    () => false,
  )

export default function SignIn() {
  const { next } = page.useLoaderData()
  const hydrated = useHydrated()
  const [fields, setFields] = useState<Record<string, string>>({})
  const [message, setMessage] = useState('')
  const [pending, setPending] = useState(false)

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const data = new FormData(event.currentTarget)
    setPending(true)
    try {
      // On success the handler answers with a redirect and the call goes to `next` through the
      // router, so there is nothing to do after it.
      await POST({ input: { email: String(data.get('email')), password: String(data.get('password')), next } })
    } catch (e) {
      if (!(e instanceof TifaError)) {
        setFields({})
        setMessage('Could not reach the server.')
        return
      }
      setFields(e.fields)
      setMessage(Object.keys(e.fields).length === 0 ? e.message : '')
    } finally {
      setPending(false)
    }
  }

  return (
    <>
      <h1>Sign in</h1>
      <form onSubmit={submit} noValidate>
        <p>
          <label htmlFor="email">E-mail</label>
          <input id="email" name="email" type="email" autoComplete="username" />
          {fields.email && <span className="error">{fields.email}</span>}
        </p>
        <p>
          <label htmlFor="password">Password</label>
          <input id="password" name="password" type="password" autoComplete="current-password" />
          {fields.password && <span className="error">{fields.password}</span>}
        </p>
        {message && <p role="alert">{message}</p>}
        <button type="submit" disabled={pending || !hydrated}>
          {pending ? 'Signing in…' : 'Sign in'}
        </button>
      </form>
    </>
  )
}
```

`autoComplete="username"` 和 `"current-password"` 让密码管理器可以填充表单。字段错误的工作方式和[表单](forms.zh-CN.md)里一样。这里也可以用表单库，就像 `examples/shop/app/login/page.tsx` 里那样。

## 使用用户 {#using-the-user}

`app/account/route.go`：

```go
package account

import (
	"strings"

	"github.com/liustack/tifa"

	"signin"
)

// Load reads who is signed in. OnRequest has already turned visitors away, and this check is for
// the day someone changes the guard there.
func Load(c *tifa.Ctx) (signin.Profile, error) {
	s, err := signin.RequireUser(c)
	if err != nil {
		return signin.Profile{}, err
	}
	return signin.ProfileOf(s.UserID), nil
}

type RenameInput struct {
	Name string `json:"name"`
}

func (in RenameInput) Validate() error {
	if strings.TrimSpace(in.Name) == "" {
		return tifa.Fields{"name": "Enter a name."}
	}
	return nil
}

// POST changes the display name. A handler is reachable without its page, so it checks the
// session itself.
func POST(c *tifa.Ctx, in RenameInput) error {
	s := tifa.Session[signin.Session](c)
	if s == nil {
		return tifa.Error{Status: 401, Message: "Sign in first."}
	}
	name := strings.TrimSpace(in.Name)
	signin.Rename(s.UserID, name)
	// The session holds a copy of the name. Sign in again with the new one: this also makes a new token.
	return tifa.SetSession(c, &signin.Session{UserID: s.UserID, Name: name})
}
```

- **先读会话，再读数据库。** 从会话里取 ID，再加载其余的信息。会话是登录时做的一份副本。之后改的名字、被收回的角色或被停用的账号，都不在里面。
- **两处设防。** `OnRequest` 在任何 `Load` 运行之前，就把访问者挡在整个区域之外，页面加载和点击链接都一样。处理函数是一个独立的 URL，可以不经过它的页面直接被调用，所以每个需要用户的处理函数都要自己检查会话。返回某个用户数据的 `Load` 也应该这样做，用 `RequireUser`。
- **让副本保持同步。** 会话里有东西变化时，像 `POST` 那样再次调用 `SetSession`。这次调用会把新会话带给浏览器，所以页头会立即跟着变。
- **带 `Cache` 的页面。** 在带 `Cache` 的页面上，对所有人来说 `Session` 都是 nil，所以这样的页面不能是私有的（见[给 CDN 的缓存](../pages-and-data/ssr-and-cache.zh-CN.md#cache-for-cdns)）。

`app/account/page.tsx`：

```tsx
import { useRouter } from '@tanstack/react-router'
import { useState, type FormEvent } from 'react'
import { TifaError } from 'tifa/client'
import { page, POST } from './route.gen'

export default function Account() {
  const profile = page.useLoaderData()
  const router = useRouter()
  const [error, setError] = useState('')

  async function rename(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const name = String(new FormData(event.currentTarget).get('name'))
    try {
      // The handler signed in again with the new name, so the header follows. The page data
      // shows the old name until it is loaded again.
      await POST({ input: { name } })
      setError('')
      await router.invalidate({ sync: true })
    } catch (e) {
      setError(e instanceof TifaError ? (e.fields.name ?? e.message) : 'Could not reach the server.')
    }
  }

  return (
    <>
      <h1>Account</h1>
      <p>
        {profile.name} ({profile.email})
      </p>
      <form onSubmit={rename} noValidate>
        <label htmlFor="name">New name</label>
        <input id="name" name="name" />
        <button type="submit">Save</button>
        {error && <p role="alert">{error}</p>}
      </form>
    </>
  )
}
```

## 退出登录 {#signing-out}

`app/logout/route.go`：

```go
package logout

import "github.com/liustack/tifa"

// POST ends the session and goes to the sign-in page. It is a POST, so another site cannot sign a
// visitor out with a link or an image.
func POST(c *tifa.Ctx) error {
	if err := tifa.ClearSession(c); err != nil {
		return err
	}
	return tifa.Redirect("/login", 303)
}
```

`ClearSession` 会删除服务器上的记录并丢弃 cookie，所以即使有人留着一份旧 cookie，它也没有用了。没人登录时它什么也不做。退出登录要用 POST，绝不能用 GET：否则别的站点上的一个链接或一张图片就能做到这件事（见[登录库](../pages-and-data/sessions.zh-CN.md#sign-in-libraries)）。

页面需要知道会话已经结束：浏览器里的那份副本驱动着 `SignedIn`、`SignedOut` 和 `useSession`。Tifa 把新的会话放进这次调用的响应里，所以页头会立即跟着变。这对像上面那样重定向的处理函数成立，对以 `return tifa.ClearSession(c)` 结尾、没有输入也没有结果的处理函数也成立。只有自己用 `c.JSON` 或 `c.Text` 写响应的处理函数，没办法带上它（见[浏览器这一侧](../pages-and-data/sessions.zh-CN.md#in-the-browser)）。

## 浏览器这一侧 {#the-browser-side}

`app/layout.tsx`（要紧的部分，`app/styles.css` 可以是你喜欢的任何样式表）：

```tsx
import { Link, Outlet, useRouter } from '@tanstack/react-router'
import { SignedIn, SignedOut, useSession } from 'tifa/client'
import { POST as signOut } from './logout/route.gen'
import './styles.css'

export const documentHead = {
  htmlAttributes: { lang: 'en' },
  meta: [{ title: 'Sign-in example' }],
  links: [{ rel: 'icon', href: 'data:,' }],
}

export default function Root() {
  return (
    <>
      <header>
        <nav>
          <Link to="/account">Account</Link>
          <Account />
        </nav>
      </header>
      <main>
        <Outlet />
      </main>
    </>
  )
}

// The server writes which of the two shows on <html>, so the first paint is right before any
// script runs.
function Account() {
  const { data } = useSession()
  const router = useRouter()
  return (
    <>
      <SignedOut>
        <a href="/login">Sign in</a>
      </SignedOut>
      <SignedIn>
        <span>Hello, {data?.name}</span>
      </SignedIn>
      <SignedIn>
        {/* The handler redirects to /login. Then drop what the router kept of the last person's pages. */}
        <button onClick={() => void signOut().then(() => router.clearCache())}>Sign out</button>
      </SignedIn>
    </>
  )
}
```

- `SignedIn` 和 `SignedOut` 按会话显示它们唯一的子元素。服务器会标记页面，所以首次绘制就是对的，不会闪烁。
- `data` 是会话里 `Exposed` 的那部分，类型来自 Go。它用来显示，不用来决定某人能做什么。那由 Go 来决定。
- 退出登录之后的 `router.clearCache()` 会丢掉路由器放在内存里的页面，这样这个浏览器上的下一个人，不会看到上一个人的数据。
- 你自己写的、会让人登录或退出的 `fetch`，Tifa 并不知道。请使用生成的函数，或者在它之后调用 `router.invalidate()`（见[浏览器这一侧](../pages-and-data/sessions.zh-CN.md#in-the-browser)的末尾）。

## 测试 {#test-it}

`app/login/route_test.go`：

```go
package login

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/tifatest"

	"signin"
)

func TestSignIn(t *testing.T) {
	if err := signin.OnStart(context.Background()); err != nil {
		t.Fatal(err)
	}
	c, rec := tifatest.NewCtx(t, httptest.NewRequest("POST", "/login", nil))
	err := POST(c, Credentials{Email: signin.DemoEmail, Password: "wrong", Next: "/account"})
	var e tifa.Error
	if !errors.As(err, &e) || e.Status != 401 {
		t.Fatalf("a wrong password gave %v", err)
	}
	if len(rec.Result().Cookies()) != 0 {
		t.Fatal("a wrong password set a cookie")
	}

	// A new request gets a new Ctx and recorder.
	c, rec = tifatest.NewCtx(t, httptest.NewRequest("POST", "/login", nil))
	err = POST(c, Credentials{Email: signin.DemoEmail, Password: signin.DemoPassword, Next: "https://evil.example/"})
	var r *tifa.RedirectError
	if !errors.As(err, &r) || r.URL != "/account" || r.Status != 303 {
		t.Fatalf("signing in gave %v", err)
	}
	if s := tifa.Session[signin.Session](c); s == nil || s.Name != "Ada" {
		t.Fatalf("session = %+v", s)
	}
	if cookies := rec.Result().Cookies(); len(cookies) != 1 {
		t.Fatalf("cookies %v, want the session cookie", cookies)
	}
}
```

`tifatest.NewCtx` 给测试一个它自己的会话存储，所以 `SetSession` 可以工作。`SetSession` 一返回，cookie 就在记录器里了。每个请求建一个 `NewCtx`，在函数返回之后再读 `rec.Result()`：记录器会保留它第一次 `Result()` 时的头，所以用于两个请求的记录器，永远看不到第二个请求的 cookie。同一个测试里的 `Ctx` 共用存储，所以一个的 cookie 可以让下一个登录（见[测试会话](../pages-and-data/sessions.zh-CN.md#testing-sessions)）。要测试一个需要用户、但不想真的登录的函数，用 `tifatest.SignIn`（见[测试会话](../pages-and-data/sessions.zh-CN.md#testing-sessions)）。要像浏览器那样走完整个登录过程，从受保护的页面开始，经过重定向和表单，再回到页面，请用 `tifatest.Start` 启动应用（见[测试整个请求](../pages-and-data/testing-apps.zh-CN.md#testing-a-whole-request)）。

`app/account/request_test.go` 用 `tifatest.Start`，像浏览器那样走完整条路径：守卫、登录、页面、退出，以及再次触发守卫。它放在自己的包里，因为 `tifaapp` 会 import 每个路由：

```go
package account_test

import (
	"net/http"
	"strings"
	"testing"

	"signin"
	"signin/.tifa/tifaapp"

	"github.com/liustack/tifa/tifatest"
)

func TestSignInAndOut(t *testing.T) {
	srv := tifatest.Start(t, tifaapp.App)
	client := srv.Client() // keeps cookies, does not follow redirects

	expect := func(res *http.Response, status int, location string) {
		t.Helper()
		if res.StatusCode != status || res.Header.Get("Location") != location {
			t.Fatalf("%s %s: %d %q, want %d %q", res.Request.Method, res.Request.URL.Path, res.StatusCode, res.Header.Get("Location"), status, location)
		}
	}
	get := func(path string) *http.Response {
		res, err := client.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res
	}
	post := func(path, body string) *http.Response {
		res, err := client.Post(srv.URL+path, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res
	}

	expect(get("/account"), 302, "/login?next=%2Faccount")
	// The query comes back too, also when the request is a data request (`_tifa` is not in it).
	expect(get("/account?tab=a"), 302, "/login?next=%2Faccount%3Ftab%3Da")
	expect(get("/account?tab=a&_tifa=b.01"), 302, "/login?next=%2Faccount%3Ftab%3Da")
	expect(post("/login", `{"email":"`+signin.DemoEmail+`","password":"`+signin.DemoPassword+`","next":"/account"}`), 303, "/account")
	expect(get("/account"), 200, "")
	expect(post("/logout", ""), 303, "/login")
	expect(get("/account"), 302, "/login?next=%2Faccount")
}
```

`Start` 会运行 `OnStart`，所以演示账号是存在的。`client` 保存登录时拿到的会话 cookie，并在下一个请求里带上它，退出登录则会把它丢掉。当 `.tifa/tifaapp` 过期时，测试会失败，并提示运行 `go tool tifa check`（见[测试整个请求](../pages-and-data/testing-apps.zh-CN.md#testing-a-whole-request)）。

## 需要决定的事 {#decisions}

**会话放在内存还是放在存储里。** 用 `tifa new` 做的应用已经把会话存在它的 SQLite 数据库里（`sessions.go`）。没有存储时，会话存在进程的内存里，重启会让所有人退出登录。刚开始这样就行。想把会话放到别处，就给 `OnStart` 设置一个存储（见[会话存在哪里](../pages-and-data/sessions.zh-CN.md#where-to-store-sessions)）。

**会话持续多久。** 从登录起 30 天，由 `session.max_age` 设置。它不会续期。对涉及钱或私人数据的应用，请把它缩短。

**OAuth、魔法链接、两步验证码。** 它们替换的是密码检查，不是其余部分。检查成功后，像上面那样调用 `tifa.SetSession`。这些流程的临时状态放在库自己的一个短期 cookie 里（见[登录库](../pages-and-data/sessions.zh-CN.md#sign-in-libraries)）。

**注册和重置密码。** 这里没有展示。两者最后都落在 `HashPassword` 和对你的用户表的一次写入上。

## 检查一下 {#check-it}

```sh
go tool tifa check
go tool tifa dev
```

在未登录时打开 `/account`，它会把你送到 `/login?next=%2Faccount`。提交空表单，两条消息都会显示。同一个邮箱输错五次密码之后，第六次尝试，即使密码正确，也会得到 "Too many attempts"。用 `correct horse battery` 以 `ada@example.com` 登录，你会落在 `/account`，页头显示 "Hello, Ada"。给自己改名，页头会变。退出登录，`/account` 会再次把你送到登录页。
