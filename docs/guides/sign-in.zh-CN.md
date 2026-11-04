---
summary: '在 tifa 应用里怎么用 tifa 的会话让用户登录和登出：登录表单、用 argon2id 保存和检查密码、SetSession、在 Load 和方法函数里读用户、保护页面、登出、在浏览器里显示谁登录了，以及一种减慢猜密码的办法'
read_when:
  - 给应用加登录和登出，或把一些页面放到登录之后
  - 决定怎么保存和检查密码
  - 没登录的访客看到了应该私有的页面，或者页头显示的登录状态不对
---

[English](sign-in.md) · 简体中文

# 登录和登出

访客用邮箱和密码登录，看到只有自己能打开的账户页，再登出。tifa 管会话：随机 token、cookie 和过期。你的代码做三件事：保存和检查密码，说明什么时候有人登录或登出，决定谁能看什么。

代码照原样跑过（用 `go tool tifa check`，也在浏览器里走过）。每个会话调用做什么、cookie 是什么，见[会话](../sessions.zh-CN.md)，这篇不重复。

## 谁登录了

根包里的 `session.go`：

```go
package signin

// Session 是谁登录了。tifa 把它存在服务器上，靠会话 cookie 找到它。
// 里面只放一个 ID 和几个显示用的字段，其余的放在你的数据库里。
type Session struct {
	UserID int64  `json:"userId"`
	Name   string `json:"name"`
}

// Me 是会话中浏览器可以保存的部分。
type Me struct {
	Name string `json:"name"`
}

func (s Session) Exposed() Me { return Me{Name: s.Name} }
```

整个应用只有一个会话类型。`Exposed` 是送到浏览器的部分，会保存在访客的设备上。里面不要放任何秘密（见[浏览器这一侧](../sessions.zh-CN.md#在浏览器里)）。

## 密码

每个密码只保存它的哈希，用故意做得很慢的哈希算法。这篇用 argon2id，来自 `golang.org/x/crypto/argon2`。[OWASP 的密码存储指南](https://cheatsheetseries.owasp.org/cheatsheets/Password_Storage_Cheat_Sheet.html)把它排在第一位，并说 bcrypt 只用于没有 argon2 的地方。下面的参数是它给的最低值：19 MiB 内存、2 轮、1 条并行线路。`examples/shop` 用的是 bcrypt，这是不错的第二选择：两个方向各一个调用（`bcrypt.GenerateFromPassword`、`bcrypt.CompareHashAndPassword`），盐和成本都在哈希里面，每个密码最多 72 字节（[包文档](https://pkg.go.dev/golang.org/x/crypto/bcrypt)）。argon2 多几行代码，因为这个包只给你密钥，盐和参数要自己记下来。

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

// argon2id，参数是 OWASP 排在第一位的那组：19 MiB 内存、2 轮、1 条并行线路。
const (
	argonMemory  = 19 * 1024 // KiB
	argonTime    = 2
	argonThreads = 1
	argonKeyLen  = 32
	argonSaltLen = 16
)

// HashPassword 返回要存进数据库的文本：参数、盐和密钥。
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

// CheckHash 判断 password 是不是做出 hash 的那个密码。
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

做出来的文本形如 `$argon2id$v=19$m=19456,t=2,p=1$<盐>$<密钥>`，这是写 argon2 哈希的通用格式。参数跟着哈希走，所以以后可以调高参数，旧的哈希照样能检查。有人注册或改密码时运行 `HashPassword`，把结果存进用户表。这篇没有注册页。

## 检查登录

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

// user 是用户表里的一行。
type user struct {
	id    int64
	email string
	name  string
	hash  string
}

// users 代替数据库。OnStart 往里填数据。
var users []user

// 邮箱不存在时检查的是 dummyHash，这样邮箱错和密码错花的时间一样，
// 回答也不会泄露是哪一个错了。
var dummyHash string

var (
	// ErrWrongLogin 是邮箱不存在和密码错共用的那一个回答。
	ErrWrongLogin = errors.New("wrong e-mail or password")
	// ErrTooMany 在一个邮箱输错太多次密码之后返回。
	ErrTooMany = errors.New("too many attempts")
)

// hashing 限制同时运行的哈希个数。每个要 19 MiB，小服务器不该因为有人同时发来很多次登录
// 就用掉几百 MiB。
var hashing = make(chan struct{}, 4)

// CheckLogin 返回这个邮箱和密码属于谁。
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

// 输错的密码按邮箱计数：一刻钟内错五次，这个邮箱在这一刻钟剩下的时间里被挡住。
// 知道邮箱的人就能挡住它，这是这么短的限制要付的代价。
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
	// 有人不停发编出来的邮箱，也不能让这个 map 无限变大。
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

// Profile 是账户页显示的内容。
type Profile struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// ProfileOf 从数据库读已登录用户的账户，不从会话里读。
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

// Rename 修改显示名。
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

把 `users` 切片换成对你数据库的查询。要保留的是：邮箱错和密码错给同一个回答，邮箱不存在时也做一次哈希检查，以及两个限制。

- **同样的回答，同样的时间。** 「没有这个用户」和「密码错」会告诉攻击者哪些邮箱有账户。dummyHash 让两种情况花的时间差不多。
- **减慢猜密码。** 这里给的一种办法：按邮箱数输错的密码，错五次之后，这个邮箱在 15 分钟里不再检查。它便宜，也不需要应用之外的东西。代价是：知道你的用户邮箱的人，可以把他挡在外面几分钟。应用跑在不止一台机器上时，把计数放到共享的存储里。
- **哈希排队。** 每次 argon2 检查运行时占 19 MiB。同时最多四个，有人刷表单时哈希占的内存最多约 76 MiB。

## 钩子

`hooks.go`：

```go
package signin

import (
	"context"
	"net/url"
	"strings"

	"github.com/liustack/tifa"
)

// 这个例子用的演示账户。真实的应用有注册页和数据库。
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

// OnRequest 不让没登录的访客进入 /account，把他们送到登录页，登录后再送回来。
// 页面加载和点链接时的数据请求都会经过它。
func OnRequest(c *tifa.Ctx, next func(*tifa.Ctx) error) error {
	path := c.Request().URL.Path
	if (path == "/account" || strings.HasPrefix(path, "/account/")) && tifa.Session[Session](c) == nil {
		return tifa.Redirect("/login?next="+url.QueryEscape(c.Request().URL.RequestURI()), 302)
	}
	return next(c)
}
```

没有人登录时 `tifa.Session[Session](c)` 是 nil。服务器在 `OnRequest` 之前读取会话，所以每个钩子、`Load` 和方法函数都能查。

## 登录路由

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

// View 是表单需要的东西：登录后去哪里。
type View struct {
	Next string `json:"next"`
}

func Load(c *tifa.Ctx) (View, error) {
	return View{Next: safeNext(c.Query("next"))}, nil
}

// Credentials 就是表单。
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

// POST 让访客登录，并把他送回来时的那个页面。
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

// safeNext 只留下本站的地址，其他一律送回首页。浏览器会丢掉 URL 里的制表符和换行，
// 还把反斜杠当斜杠读，所以 "/\n/host" 和 "/\host" 都会离开本站。
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

- 在方法函数里登录，不要在 `Load` 里。`Load` 为 GET 运行，GET 不该改任何东西，在那里 `SetSession` 会返回错误（见[哪里能调什么](../sessions.zh-CN.md#哪里能调什么)）。
- `SetSession` 每次调用都生成新的随机 token，并丢掉这次请求带来的会话，所以登录永远不会沿用别人指定的 token。
- 意外的错误（比如数据库挂了）进 `OnError`。密码错是带状态码的 `tifa.Error`，不会进。
- `next` 来自 URL，谁都能往里放任何东西。`safeNext` 把它限制在本站的路径上。没有它，你的登录页会把访客送到攻击者指定的任何网站。不要去掉它。
- 方法函数返回 `tifa.Redirect(..., 303)`。页面的调用通过路由器跟着走，浏览器里的会话副本由同一个回答更新（见[浏览器这一侧](../sessions.zh-CN.md#在浏览器里)）。

## 登录页

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
      // 成功时方法函数回答一个重定向，调用通过路由器去 `next`，之后没有什么要做的。
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

`autoComplete="username"` 和 `"current-password"` 让密码管理器能自动填。字段错误的做法和[表单](forms.zh-CN.md)里一样。这里也可以用表单库，见 `examples/shop/app/login/page.tsx`。

## 用上这个用户

`app/account/route.go`：

```go
package account

import (
	"strings"

	"github.com/liustack/tifa"

	"signin"
)

// Load 读谁登录了。OnRequest 已经把访客挡在外面，这里的检查是为了防哪天有人改了那里的守卫。
func Load(c *tifa.Ctx) (signin.Profile, error) {
	s := tifa.Session[signin.Session](c)
	if s == nil {
		return signin.Profile{}, tifa.Redirect("/login", 302)
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

// POST 修改显示名。方法函数不经过它的页面也能被调用，所以它自己检查会话。
func POST(c *tifa.Ctx, in RenameInput) error {
	s := tifa.Session[signin.Session](c)
	if s == nil {
		return tifa.Error{Status: 401, Message: "Sign in first."}
	}
	name := strings.TrimSpace(in.Name)
	signin.Rename(s.UserID, name)
	// 会话里有一份名字的副本。用新名字重新登录一次，这也会换一个新 token。
	return tifa.SetSession(c, &signin.Session{UserID: s.UserID, Name: name})
}
```

- **先读会话，再读数据库。** 从会话里拿 ID，其余的去数据库加载。会话是登录时做的一份副本。之后改的名字、被收回的角色、被停用的账户，它里面都没有。
- **两个地方都要守。** `OnRequest` 在任何 `Load` 运行之前，就把访客挡在整个区域外面，页面加载和点链接都一样。方法函数有自己的 URL，不经过它的页面也能被调用，所以每个需要用户的方法函数都要自己检查会话。返回用户数据的 `Load` 也一样。
- **让副本跟上。** 会话里的内容变了，就像 `POST` 那样再调一次 `SetSession`。这个调用把新会话带到浏览器，页头马上跟着变。
- **有 `Cache` 的页面。** 在整页缓存里的页面上，`Session` 对所有人都是 nil，所以这样的页面不能是私有的（见[整页缓存](../app.zh-CN.md#整页缓存)）。

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
      // 方法函数用新名字重新登录了，所以页头会跟着变。页面数据要重新加载之后才显示新名字。
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

## 登出

`app/logout/route.go`：

```go
package logout

import "github.com/liustack/tifa"

// POST 结束会话并去登录页。它是 POST，所以别的网站不能用一个链接或一张图片让访客登出。
func POST(c *tifa.Ctx) error {
	if err := tifa.ClearSession(c); err != nil {
		return err
	}
	return tifa.Redirect("/login", 303)
}
```

`ClearSession` 在服务器上删掉记录，并丢掉 cookie，所以就算有人留了一份旧 cookie，它也没用了。没有人登录时它什么也不做。登出要用 POST，不要用 GET，否则别的网站上的一个链接或一张图片就能做到（见[和登录库的分工](../sessions.zh-CN.md#和登录库的分工)）。

## 浏览器这一侧

`app/layout.tsx`（重要的部分）：

```tsx
import { Link, Outlet, useRouter } from '@tanstack/react-router'
import { SignedIn, SignedOut, useSession } from 'tifa/client'
import { POST as signOut } from './logout/route.gen'
import './styles.css'

export const htmlAttributes = { lang: 'en' }

export const documentHead = {
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

// 服务器在 <html> 上写明两个里显示哪一个，所以首次绘制就是对的，不用等任何脚本运行。
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
        {/* 方法函数重定向到 /login。然后丢掉路由器为上一个人保存的页面。 */}
        <button onClick={() => void signOut().then(() => router.clearCache())}>Sign out</button>
      </SignedIn>
    </>
  )
}
```

- `SignedIn` 和 `SignedOut` 按会话显示它们唯一的子元素。服务器给页面做了标记，所以首次绘制是对的，不会闪。
- `data` 是会话的 `Exposed` 部分，类型来自 Go。它用来显示，不用来决定谁能做什么。这由 Go 决定。
- 登出之后 `router.clearCache()` 会丢掉路由器放在内存里的页面，这样用这个浏览器的下一个人看不到上一个人的数据。
- 你自己写的、会让人登录或登出的 `fetch`，tifa 并不知道。用生成的函数，或者在它之后调 `router.invalidate()`（见[浏览器这一侧](../sessions.zh-CN.md#在浏览器里)的结尾）。

## 测试

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

	// 新的请求用新的 Ctx 和 recorder。
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

`tifatest.NewCtx` 给测试一个它自己的会话存储，所以 `SetSession` 能用。`SetSession` 一返回，cookie 就在 recorder 里。每个请求用一个新的 `NewCtx`，函数返回后再读 `rec.Result()`：recorder 记下的是第一次调用 `Result()` 时的头，一个 recorder 用于两次请求，第二次的 cookie 永远看不到。同一个测试的 `Ctx` 共用存储，所以前一个请求的 cookie 能让下一个登录（见[测试会话](../sessions.zh-CN.md#测试会话)）。要测试需要用户的函数、又不想真的登录，用 `tifatest.WithSession`（见[测试会话](../sessions.zh-CN.md#测试会话)）。要像浏览器那样走完整个登录，从受保护的页面经过重定向和表单再回到页面，就用 `tifatest.Start` 启动应用（见[测整个请求](../app.zh-CN.md#测整个请求)）。

## 要做的选择

**会话放内存还是存储里。** 没有存储时会话放在进程的内存里，重启就让所有人登出。刚开始这样就行。到它成了问题时，在 `OnStart` 里给它一个存储（见[会话存在哪](../sessions.zh-CN.md#会话存在哪)）。

**会话持续多久。** 从登录算起 30 天，由 `session.max_age` 设置，不会续期。涉及钱或私人数据的应用，把它缩短。

**OAuth、魔法链接、两步验证码。** 它们替代的是密码检查，不是其余部分。检查通过后，像上面那样调 `tifa.SetSession`。这些流程的临时状态放在那个库自己的短期 cookie 里（见[和登录库的分工](../sessions.zh-CN.md#和登录库的分工)）。

**注册和重置密码。** 这里没有。两者最后都是 `HashPassword` 加一次对用户表的写入。

## 检查一下

```sh
go tool tifa check
go tool tifa dev
```

没登录时打开 `/account`，它把你送到 `/login?next=/account`。提交空表单，两条消息都显示。一个邮箱输错五次密码，第六次即使密码对也得到 "Too many attempts"。用 `correct horse battery` 以 `ada@example.com` 登录，你来到 `/account`，页头显示 "Hello, Ada"。改名后页头跟着变。登出后再打开 `/account`，又被送到登录页。
