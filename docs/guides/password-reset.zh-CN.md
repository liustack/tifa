---
summary: '在 Tifa 应用里重置忘记的密码：邮件里的一次性链接，限制发信数量，用会话里带的一个数字让所有旧会话失效'
audience: app
order: 14
translatedFrom: 6879ab1c3333
read_when:
  - '给应用加「忘记密码」'
  - '重置或修改密码后，要让用户在所有其他设备上都退出登录'
  - '重置表单会让陌生人知道哪些邮箱已有账号，或者能被用来往别人邮箱里灌邮件'
  - '检查一个重置流程，或者编程 agent 写的重置代码有没有漏洞'
---

# 重置密码 {#reset-a-password}

忘了密码的访客要一个链接，通过邮件收到它，再选一个新密码。这个链接就是他拥有这个邮箱的证明。这篇用的是 [注册并确认邮箱地址](sign-up.zh-CN.md) 里的令牌、表和邮件，所以请先读那一篇。Tifa 自己没有重置密码的功能：它是两个函数加两个路由。

代码照原样在同一个临时应用里跑过：`go tool tifa check`、一个走完整个流程的 Go 测试，还有构建出的程序。

## 要一个链接 {#ask-for-a-link}

`reset.go`：

```go
package notes

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// A reset mail is sent at most this many times an hour for one account, so the form cannot
// be used to fill someone's inbox.
const (
	resetTTL      = 30 * time.Minute
	resetsPerHour = 3
)

// RequestReset mails a reset link when the address has an account, and does nothing when it has
// none. The caller answers the same in both cases.
func RequestReset(ctx context.Context, email string) error {
	email = normalizeEmail(email)
	tx, err := DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var id int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM users WHERE email = ?`, email).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var recent int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM email_tokens WHERE user_id = ? AND purpose = 'reset' AND created > ?`,
		id, now().Add(-time.Hour).Unix()).Scan(&recent); err != nil {
		return err
	}
	if recent >= resetsPerHour {
		return nil
	}
	token, err := issueToken(ctx, tx, id, email, "reset", resetTTL)
	if err != nil {
		return err
	}
	err = QueueMail(ctx, tx, Mail{To: email, Subject: "Reset your password",
		Text: "Choose a new password:\n\n" + AppURL + "/reset?token=" + token + "\n\n" +
			"The link works once, for 30 minutes. If you did not ask for it, ignore this mail: your password is unchanged.\n"})
	if err != nil {
		return err
	}
	return tx.Commit()
}

// ResetPassword spends a reset token and sets the new password. It ends every session of the
// user by raising epoch, see Current.
func ResetPassword(ctx context.Context, token, password string) error {
	// A guess at a token must not cost a hash.
	if ok, err := TokenValid(ctx, token, "reset"); err != nil || !ok {
		if err == nil {
			err = ErrBadToken
		}
		return err
	}
	hash, err := hashPassword(ctx, password)
	if err != nil {
		return err
	}
	tx, err := DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	id, email, err := useToken(ctx, tx, token, "reset")
	if err != nil {
		return err
	}
	// The mail reached the owner of the address, so the address is confirmed too.
	res, err := tx.ExecContext(ctx, `UPDATE users SET hash = ?, epoch = epoch + 1, verified_at = coalesce(verified_at, ?) WHERE id = ? AND email = ?`,
		hash, now().Unix(), id, email)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return ErrBadToken
	}
	// Other reset links of this account are no good now.
	if _, err := tx.ExecContext(ctx, `DELETE FROM email_tokens WHERE user_id = ? AND purpose = 'reset'`, id); err != nil {
		return err
	}
	return tx.Commit()
}

```

- **每个地址都是同一个回答。** 对没有账号的地址，`RequestReset` 返回 nil，路由重定向到同一个「请查收邮件」页。一个说「没有这个地址」的表单，会让任何人知道哪些地址是你的客户。地址不存在的情况少做几次数据库写入，差别很小，而且邮件由任务来发，所以请求从不等邮件服务器。
- **同一个账号每小时最多三封。** 没有这个限制，表单就能被用来往别人的邮箱里灌你发的邮件。按账号的限制拦不住一个对很多账号都发请求的人，所以公开的应用要在 `OnRequest` 里再加一个按地址的限制。
- **每次一个新令牌，有效 30 分钟。** 链接里是令牌，表里存的是它的哈希。
- **先检查再哈希。** `ResetPassword` 先看令牌，再对新密码做哈希，所以乱猜令牌不用每次都付一次 argon2 的代价。
- **一个事务。** 用掉令牌、设新密码、`epoch` 加一、删掉这个账号其他的重置链接，要么全做，要么都不做。
- **地址也确认了。** 重置邮件到达了邮箱，所以 `verified_at` 为空时会被设上。

## 页面 {#the-page}

邮件里的链接打开一个页面，页面把令牌 POST 出去。`app/reset/route.go`：

```go
package reset

import (
	"errors"

	"github.com/liustack/tifa"

	"example.com/notes"
)

type Query struct {
	Token string `json:"token"`
}

// View says whether the link can still be used.
type View struct {
	Valid bool `json:"valid"`
}

// Load only looks: a GET never spends the token, so a mail scanner that opens the link does not
// use it up.
func Load(c *tifa.Ctx, q Query) (View, error) {
	ok, err := notes.TokenValid(c.Context(), q.Token, "reset")
	return View{Valid: ok}, err
}

type Form struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

func (in Form) Validate() error {
	if n := len(in.Password); n < 8 || n > 256 {
		return tifa.Fields{"password": "Use 8 to 256 characters."}
	}
	return nil
}

func POST(c *tifa.Ctx, in Form) error {
	err := notes.ResetPassword(c.Context(), in.Token, in.Password)
	if errors.Is(err, notes.ErrBadToken) {
		return tifa.Error{Status: 400, Message: "This link has expired or was already used."}
	}
	if err != nil {
		return err
	}
	return tifa.Redirect("/login", 303)
}

```

`Load` 只看一眼，所以扫描程序打开链接不会把它用掉（[原因](sign-up.zh-CN.md#confirm-the-address)）。`app/reset/page.tsx` 用 `page.useSearch()` 读出令牌，`valid` 为 false 时显示「这个链接已过期」，并用一个带 `autoComplete="new-password"` 的表单 POST `{ token, password }`。成功后路由重定向到 `/login`。`app/forgot` 是一个只有邮箱字段的表单，它的处理函数调用 `RequestReset`，再重定向到一个「请查收邮件」页。

重置不会让访客登录。一封到了收件箱的邮件，足够用来改密码，但再让它顺带登录，会让泄露的链接权力更大。重定向到登录页就好。

## 让其他会话失效 {#ending-the-other-sessions}

知道旧密码的人可能手里有一个会话。新密码应该让它失效。Tifa 的会话靠 cookie 里的令牌找到，里面只有你放进去的东西，所以想不靠一份清单就让某个用户的所有会话失效，可以用会话里带的一个数字：

- `users.epoch` 在 `ResetPassword` 里加一。
- 会话带着创建时的 `Epoch`：`Session{UserID, Name, Epoch}`，由登录填上。
- `Current` 读取那一行，把 epoch 不同的会话当作没有登录。

```go
package notes

import (
	"database/sql"
	"errors"

	"github.com/liustack/tifa"
)

// Current returns who is signed in, or nil. The session must also match the user row: a
// session made before the last password change has an older epoch and counts as signed out.
func Current(c *tifa.Ctx) (*Session, error) {
	s := tifa.Session[Session](c)
	if s == nil {
		return nil, nil
	}
	var epoch int64
	err := DB.QueryRowContext(c.Context(), `SELECT epoch FROM users WHERE id = ?`, s.UserID).Scan(&epoch)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && epoch != s.Epoch) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return s, nil
}

```

在 [登录与退出](sign-in.zh-CN.md#the-guard) 里用 `tifa.Session[Session](c)` 来决定谁能做什么的地方，改用 `Current`：`RequireUser` 里和 `OnRequest` 的守卫里。每个带会话的请求要多按主键读一行。旧会话留在存储里直到过期，已经没用了。已登录的用户改密码时，同样的一行加一也能让其他会话失效：把 `epoch` 加一，再用新的值为当前浏览器调用 `SetSession`。

## 陷阱 {#traps}

- **日志里的链接。** `smtp.host` 为空时，重置链接就在日志里（[发送邮件](email.zh-CN.md#settings)）。生产环境不要这样留着。
- **链接里的 `Host` 头。** 用 `app.url` 拼，不要用请求里的。
- **放了其他网站脚本的重置页。** 令牌就在它的地址里。别放统计和小部件。Tifa 已经发 `Referrer-Policy: strict-origin-when-cross-origin`，所以这个页面上的链接不会把地址带给别的网站（[安全头](../build-and-deploy/headers-and-origins.zh-CN.md#security-headers)）。
- **重置后还留着活的会话。** 就是上面的 `epoch`。没有它，被偷走的会话在新密码之后还活着。
- **没有限制的密码检查。** 重置用同一个 `hashPassword` 设密码，也受同时最多四个的限制。

## 测试 {#test-it}

临时应用的 `app/account/accounts_test.go`，用和 [注册](sign-up.zh-CN.md#test-it) 里一样的辅助函数，检查：

- 不存在的地址得到同样的重定向，也收不到邮件
- 新密码能用，旧密码和旧会话不能用，令牌只能用一次
- 密码太短的表单被拒绝，令牌没被用掉
- 一小时内第四次请求不会发邮件，重置成功会删掉其他还开着的链接

过期的旧令牌，由根包里一个移动时钟的测试来拒绝。
