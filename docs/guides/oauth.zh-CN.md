---
summary: '在 Tifa 应用里用 golang.org/x/oauth2 做 Google 或 GitHub 登录：放在短 cookie 里的 state 和 PKCE，调用 SetSession 的回调，按已验证邮箱关联账号，以及用 POST 回调的服务商'
audience: app
order: 15
translatedFrom: da787be15cd2
read_when:
  - '加「用 Google 登录」「用 GitHub 登录」，或其他 OAuth 2 或 OpenID Connect 登录'
  - '服务商用 POST 把访客送回来（用 Apple 登录），回调得到 403 `cross-origin request detected`'
  - '决定服务商登录能不能并入一个已经存在的账号'
  - '为 OAuth 回调写不需要真实服务商的 Go 测试'
---

# 用 Google 或 GitHub 登录 {#sign-in-with-google-or-github}

访客点一个按钮，在服务商那里同意，然后回来时已经登录了。Tifa 保存会话（[会话](../pages-and-data/sessions.zh-CN.md)），访客是谁由服务商说了算。你的代码把访客送出去，检查他回来的路，再调用 `tifa.SetSession`，就像 [登录与退出](sign-in.zh-CN.md) 在密码通过之后做的那样。Tifa 自己没有 OAuth。用的库是 `golang.org/x/oauth2`。

端点来自 `oauth2` 的 `google` 和 `github` 包。每个服务商请你用真实账号亲手试一次。

## 设置 {#set-it-up}

在服务商那里建一个 OAuth 应用，把访客回来的地址告诉它：`https://notes.example.com/auth/google/callback`，`github` 也一样。客户端 ID 和密钥放在 [应用的选项](../build-and-deploy/config.zh-CN.md#options-of-the-app) 里，密钥用 `tifa.Secret`：

```go
// options.go, and the same two for GitHub
var (
	GoogleClientID     = tifa.String("google.client_id", "", "Client ID of the Google OAuth app, empty turns Google sign-in off")
	GoogleClientSecret = tifa.Secret("google.client_secret", "Client secret of the Google OAuth app")
)

```

回调地址要用 `AppURL`（访客用的地址）拼，不要用请求里的 `Host`。没有客户端 ID 的服务商就是关着的。用户和会话用的是 [注册](sign-up.zh-CN.md) 里的，再加一张身份表：

```sql
CREATE TABLE IF NOT EXISTS identities (
  provider TEXT    NOT NULL,
  subject  TEXT    NOT NULL,  -- the provider's own id for the person, which never changes
  user_id  INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  PRIMARY KEY (provider, subject)
) WITHOUT ROWID;

```

## 服务商 {#the-providers}

`providers.go`：

```go
package notes

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/github"
	"golang.org/x/oauth2/google"
)

// Profile is what the app needs to know about the person who signed in at a provider.
type Profile struct {
	Subject       string // the provider's id for the person
	Email         string
	EmailVerified bool // the provider says the person owns Email
	Name          string
}

// Provider is one way to sign in.
type Provider struct {
	Config oauth2.Config
	// Profile asks the provider who the token belongs to. client sends the token.
	Profile func(ctx context.Context, client *http.Client) (Profile, error)
	// FormPost is true for a provider that sends the visitor back with a POST of a form.
	FormPost bool
}

// Providers is the set of providers that are on, by the name in the address: /auth/google.
// OnStart fills it from the options.
var Providers = map[string]*Provider{}

func registerProviders() {
	redirect := func(name string) string { return AppURL + "/auth/" + name + "/callback" }
	if GoogleClientID != "" {
		Providers["google"] = &Provider{
			Config: oauth2.Config{ClientID: GoogleClientID, ClientSecret: GoogleClientSecret, Endpoint: google.Endpoint,
				RedirectURL: redirect("google"), Scopes: []string{"openid", "email", "profile"}},
			Profile: OIDCProfile("https://openidconnect.googleapis.com/v1/userinfo"),
		}
	}
	if GitHubClientID != "" {
		Providers["github"] = &Provider{
			Config: oauth2.Config{ClientID: GitHubClientID, ClientSecret: GitHubClientSecret, Endpoint: github.Endpoint,
				RedirectURL: redirect("github"), Scopes: []string{"read:user", "user:email"}},
			Profile: GitHubProfile("https://api.github.com"),
		}
	}
}

// OIDCProfile reads the userinfo endpoint of an OpenID Connect provider such as Google.
func OIDCProfile(userinfo string) func(context.Context, *http.Client) (Profile, error) {
	return func(ctx context.Context, client *http.Client) (Profile, error) {
		var u struct {
			Sub           string `json:"sub"`
			Email         string `json:"email"`
			EmailVerified bool   `json:"email_verified"`
			Name          string `json:"name"`
		}
		if err := getJSON(ctx, client, userinfo, &u); err != nil {
			return Profile{}, err
		}
		if u.Sub == "" {
			return Profile{}, fmt.Errorf("%s gave no sub", userinfo)
		}
		return Profile{Subject: u.Sub, Email: u.Email, EmailVerified: u.EmailVerified, Name: u.Name}, nil
	}
}

// GitHubProfile reads the user and the e-mail addresses of a GitHub account. The user object
// has an address only when the person made it public, so the list of addresses is the one to
// trust: take the primary address, and only if GitHub has verified it.
func GitHubProfile(api string) func(context.Context, *http.Client) (Profile, error) {
	return func(ctx context.Context, client *http.Client) (Profile, error) {
		var u struct {
			ID    int64  `json:"id"`
			Login string `json:"login"`
			Name  string `json:"name"`
		}
		if err := getJSON(ctx, client, api+"/user", &u); err != nil {
			return Profile{}, err
		}
		var emails []struct {
			Email    string `json:"email"`
			Primary  bool   `json:"primary"`
			Verified bool   `json:"verified"`
		}
		if err := getJSON(ctx, client, api+"/user/emails", &emails); err != nil {
			return Profile{}, err
		}
		p := Profile{Subject: strconv.FormatInt(u.ID, 10), Name: u.Name}
		if p.Name == "" {
			p.Name = u.Login
		}
		for _, e := range emails {
			if e.Primary && e.Verified {
				p.Email, p.EmailVerified = e.Email, true
			}
		}
		return p, nil
	}
}

```

- `getJSON`（没有列出）是一个 GET，状态码不是 200 就失败。
- **Google** 是 OpenID Connect。`email` scope 会给出 `email` 和 `email_verified`，`sub` 永远不变（[文档](https://developers.google.com/identity/openid-connect/openid-connect)）。`email_verified` 按标准当作布尔值读。
- **GitHub** 没有 OpenID Connect，用户对象里只有在本人公开了邮箱时才有地址。`GET /user/emails` 需要 `user:email` scope，列出每个地址的 `email`、`primary` 和 `verified`（[文档](https://docs.github.com/en/rest/users/emails)）。代码取主地址，前提是 GitHub 验证过它。

## 把访客送出去，再接回来 {#send-the-visitor-out-and-take-them-back}

`oauth.go`：

```go
package notes

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/liustack/tifa"
	"golang.org/x/oauth2"
)

// flowCookie holds what the callback needs to check: the provider, the state and the PKCE
// verifier. It lives for ten minutes and is gone after the callback.
const flowCookie = "oauth_flow"

func setFlowCookie(c *tifa.Ctx, p *Provider, value string, maxAge int) {
	secure := strings.HasPrefix(AppURL, "https://")
	sameSite := http.SameSiteLaxMode // sent when the provider sends the visitor back with a GET
	if p != nil && p.FormPost && secure {
		sameSite = http.SameSiteNoneMode // a POST from another site carries no Lax cookie
	}
	c.SetCookie(&http.Cookie{Name: flowCookie, Value: value, Path: "/auth", MaxAge: maxAge,
		HttpOnly: true, Secure: secure, SameSite: sameSite})
}

// StartSignIn returns the address of the provider to send the visitor to, and sets the cookie.
func StartSignIn(c *tifa.Ctx, name, next string) (string, error) {
	p := Providers[name]
	if p == nil {
		return "", tifa.ErrNotFound
	}
	state := oauth2.GenerateVerifier() // 32 random bytes, any random string does for state
	verifier := oauth2.GenerateVerifier()
	flow := url.Values{"provider": {name}, "state": {state}, "verifier": {verifier}, "next": {next}}
	setFlowCookie(c, p, flow.Encode(), 600)
	return p.Config.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier)), nil
}

var (
	errFlow = tifa.Error{Status: 400, Message: "The sign-in did not start here or took too long. Try again."}
	// ErrEmailNotVerified: the provider does not vouch for the address.
	errEmail = tifa.Error{Status: 403, Message: "The provider has not confirmed your e-mail address."}
	// ErrUnconfirmed: an account with this address exists and its address was never confirmed.
	errUnconfirmed = tifa.Error{Status: 409, Message: "This address has an account that was never confirmed. Confirm it with the link we sent, or reset the password."}
)

// FinishSignIn checks the callback, asks the provider who the visitor is and signs them in. It
// returns where to go next.
func FinishSignIn(c *tifa.Ctx, name, code, state, providerError string) (string, error) {
	p := Providers[name]
	if p == nil {
		return "", tifa.ErrNotFound
	}
	ck, err := c.Cookie(flowCookie)
	setFlowCookie(c, p, "", -1) // one try for each cookie, whatever happens next
	if err != nil {
		return "", errFlow
	}
	flow, err := url.ParseQuery(ck.Value)
	if err != nil || flow.Get("provider") != name || state == "" ||
		subtle.ConstantTimeCompare([]byte(flow.Get("state")), []byte(state)) != 1 {
		return "", errFlow
	}
	if providerError != "" { // the visitor said no, or the provider refused
		return "", tifa.Error{Status: 403, Message: "Sign-in was cancelled."}
	}

	ctx, cancel := context.WithTimeout(c.Context(), 10*time.Second)
	defer cancel()
	tok, err := p.Config.Exchange(ctx, code, oauth2.VerifierOption(flow.Get("verifier")))
	if err != nil {
		slog.WarnContext(c.Context(), "oauth: code exchange failed", "provider", name, "err", err)
		return "", tifa.Error{Status: 400, Message: "Sign-in failed. Try again."}
	}
	profile, err := p.Profile(ctx, p.Config.Client(ctx, tok))
	if err != nil {
		return "", err // the provider is down or changed: an unexpected error
	}
	s, err := userFor(ctx, name, profile)
	if err != nil {
		return "", err
	}
	if err := tifa.SetSession(c, &s); err != nil {
		return "", err
	}
	return safeNext(flow.Get("next")), nil
}

// userFor finds or makes the user of a provider's profile.
func userFor(ctx context.Context, provider string, p Profile) (Session, error) {
	tx, err := DB.BeginTx(ctx, nil)
	if err != nil {
		return Session{}, err
	}
	defer tx.Rollback()

	// 1. Someone who signed in this way before. The provider's id is the key, not the address:
	// addresses change hands and change.
	var s Session
	err = tx.QueryRowContext(ctx, `SELECT u.id, u.name, u.epoch FROM identities i JOIN users u ON u.id = i.user_id
		WHERE i.provider = ? AND i.subject = ?`, provider, p.Subject).Scan(&s.UserID, &s.Name, &s.Epoch)
	if err == nil {
		return s, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Session{}, err
	}

	// 2. A new way in. It needs an address that the provider vouches for.
	email := normalizeEmail(p.Email)
	if !p.EmailVerified || email == "" {
		return Session{}, errEmail
	}
	var verified sql.NullInt64
	err = tx.QueryRowContext(ctx, `SELECT id, name, epoch, verified_at FROM users WHERE email = ?`, email).
		Scan(&s.UserID, &s.Name, &s.Epoch, &verified)
	switch {
	case err == nil && !verified.Valid:
		// Anyone can sign up with an address that is not theirs. Joining such an account would
		// give the person who made it, and knows its password, a way into this one.
		return Session{}, errUnconfirmed
	case errors.Is(err, sql.ErrNoRows):
		// 3. A new person.
		s = Session{Name: p.Name}
		if s.Name == "" {
			s.Name = email
		}
		// An empty hash is no password: CheckHash refuses it, so only the provider signs in.
		err = tx.QueryRowContext(ctx, `INSERT INTO users (email, name, hash, verified_at) VALUES (?, ?, '', ?) RETURNING id`,
			email, s.Name, now().Unix()).Scan(&s.UserID)
	}
	if err != nil {
		return Session{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO identities (provider, subject, user_id) VALUES (?, ?, ?)`,
		provider, p.Subject, s.UserID); err != nil {
		return Session{}, err
	}
	return s, tx.Commit()
}


```

`normalizeEmail`、`now` 和 `Session` 是 [注册](sign-up.zh-CN.md) 和 [重置密码](password-reset.zh-CN.md) 里的，`safeNext` 是 [登录那一篇](sign-in.zh-CN.md#the-sign-in-route) 里的。

- **`state`** 放在地址里发出去，在回调里带回来，必须和 cookie 里的相等，而 cookie 只有这个浏览器有。它能防止攻击者把受害者登录成攻击者。
- **PKCE**（`code_challenge` 配 `S256`，换令牌时再带上 `code_verifier`）把授权码绑定到发起流程的那个浏览器。GitHub 只支持 `S256`（[文档](https://docs.github.com/en/apps/oauth-apps/building-oauth-apps/authorizing-oauth-apps)），Google 在它的 discovery 文档里列了它。verifier 只留在 cookie 里。
- **这个 cookie 又短又是它自己的**，不是会话 cookie：带 `HttpOnly`，`AppURL` 是 HTTPS 时带 `Secure`，`Path=/auth` 所以别的页面收不到，存活十分钟。回调一开始就清掉它，所以它只能用一次。

路由是一个带参数的文件夹，所以一对路由服务所有服务商。`app/auth/provider+/route.go`：

```go
package provider

import (
	"github.com/liustack/tifa"

	"example.com/notes"
)

// Query is the query string of the address that starts a sign-in.
type Query struct {
	Next string `json:"next"`
}

// GET sends the visitor to the provider. It changes nothing except the cookie of the flow.
func GET(c *tifa.Ctx, q Query) error {
	to, err := notes.StartSignIn(c, c.Param("provider"), q.Next)
	if err != nil {
		return err
	}
	return tifa.Redirect(to, 302)
}

```

还有 `app/auth/provider+/callback/route.go`：

```go
package callback

import (
	"github.com/liustack/tifa"

	"example.com/notes"
)

// Query is what a provider adds to the address when it sends the visitor back.
type Query struct {
	Code  string `json:"code"`
	State string `json:"state"`
	Error string `json:"error"`
}

// GET is the callback of Google and GitHub.
func GET(c *tifa.Ctx, q Query) error {
	next, err := notes.FinishSignIn(c, c.Param("provider"), q.Code, q.State, q.Error)
	if err != nil {
		return err
	}
	return tifa.Redirect(next, 303)
}

// POST is the callback of a provider that sends a form, such as Apple. The form has the same
// fields, and `server.cross_site_paths` lets the request in.
func POST(c *tifa.Ctx, f Query) error {
	next, err := notes.FinishSignIn(c, c.Param("provider"), f.Code, f.State, f.Error)
	if err != nil {
		return err
	}
	return tifa.Redirect(next, 303)
}

```

起点只设一个 cookie，所以放一个链接就够了：`<a href="/auth/google?next=%2Faccount">Sign in</a>`。回调也是 GET，因为服务商是用重定向把访客送回来的，它可以调用 `SetSession`（[什么地方调什么](../pages-and-data/sessions.zh-CN.md#where-to-call-what)）。

## 关联到已有的账号 {#linking-to-an-account-that-exists}

`userFor` 按这个顺序决定一份资料是哪个用户：

1. **身份已知。** 键是（服务商，`Subject`）这一对，不是地址。
2. **新身份，而且有一个同地址的账号。** 服务商必须说这个地址已验证，账号的地址也必须已确认。然后这个身份并入这个账号。地址从没确认过的账号会被拒绝，返回 409：任何人都可以用不属于自己的地址注册，并且知道那个密码，并入它就会让他们进入真正主人的账号（[注册](sign-up.zh-CN.md)）。
3. **新的人。** 用户得到已确认的地址和空的密码哈希，`CheckHash` 会拒绝空哈希，所以只有服务商能让他们登录。

## 用 POST 回调的服务商 {#a-provider-that-posts-the-callback}

用 `response_mode=form_post` 时，用 Apple 登录会把结果以 POST 发到重定向地址，正文是表单，带姓名和地址的 `user` 字段也在其中，只有第一次才有（[文档](https://developer.apple.com/documentation/signinwithapplerestapi/request-an-authorization-to-the-sign-in-with-apple-server)）。这是来自另一个网站的写操作，Tifa 会用 403 拒绝它（[跨站写入](../build-and-deploy/headers-and-origins.zh-CN.md#cross-site-writes)）。把这个路径列出来：

```yaml
# tifa.yaml
server:
  cross_site_paths: "POST /auth/apple/callback"
```

这个路径不再检查请求来自哪个网站，所以处理函数要自己用 `state` 和 cookie 证明请求是真的，`FinishSignIn` 对这个 POST 也是这样检查的。只列这一个路径。

`SameSite=Lax` 的 cookie 不会随另一个网站发起的 POST 发出去，所以带 `FormPost` 的服务商用的是 `SameSite=None` 的 cookie，它需要 HTTPS。

Apple 还需要一个要你自己签出来的客户端密钥令牌，以及一个读它的 `id_token` 的 `Profile`。

## 测试 {#test-it}

测试不需要真实的服务商。用 `tifatest.Start` 启动应用，在一个 `httptest` 服务器里搭一个服务商，有 `/token`、`/userinfo`、`/user` 和 `/user/emails`。让它的令牌端点对每个授权码只给一次令牌，而且只在 `code_verifier` 的 SHA-256 和收到的 `code_challenge` 对得上时才给。从重定向里读出 `state` 和 challenge，再用一个保存 cookie 的客户端去调回调。检查上面的关联规则，以及回调在第二次、换了 `state`、没有 cookie、带 `error=` 和授权码未知时都会失败。对 Apple 的路径，跨站 POST 在 `tifa.yaml` 里没有那一行时应得到 403，有了就通过。
