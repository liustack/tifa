---
summary: '给 Tifa 应用加上注册和邮箱确认：只能用一次、以哈希保存的令牌，不泄露哪些邮箱已有账号的回答，链接打开页面、按钮才用掉令牌'
audience: app
order: 13
translatedFrom: 963facf5967f
read_when:
  - '加注册页，或者邮件里一个用来确认邮箱地址的链接'
  - '做一个只能用一次、会过期的链接，比如确认链接或邀请链接'
  - '注册表单会让陌生人知道哪些邮箱已经有账号'
  - '确认链接在本人打开前就已经被用掉了，因为邮件扫描程序先打开了它'
---

# 注册并确认邮箱地址 {#sign-up-and-confirm-an-e-mail-address}

[登录与退出](sign-in.zh-CN.md) 从一个已经存在的用户开始。这篇做出这个用户，并证明这个邮箱地址确实是访客的。[重置密码](password-reset.zh-CN.md) 用的是同样的令牌。Tifa 自己没有用户，也没有密码，所以这些都是你的代码和你的表，只有几个函数。

它建立在这几篇之上：[登录与退出](sign-in.zh-CN.md#passwords) 里的 `HashPassword` 和 `CheckHash`，[发送邮件](email.zh-CN.md) 里的 `QueueMail`，以及 [在 SQLite 里保存数据](database.zh-CN.md) 里的数据库。代码照原样跑过，用的是一个 `tifa new` 建的临时应用（模块 `example.com/notes`）：跑了 `go tool tifa check`，跑了一个走完整个流程的 Go 测试，也在构建出的程序里跑过。

## 两张表 {#two-tables}

```sql
-- schema.sql
CREATE TABLE IF NOT EXISTS users (
  id          INTEGER PRIMARY KEY,
  email       TEXT    NOT NULL UNIQUE,  -- lower case
  name        TEXT    NOT NULL,
  hash        TEXT    NOT NULL,
  verified_at INTEGER,                  -- unix seconds, NULL until the address is confirmed
  epoch       INTEGER NOT NULL DEFAULT 0 -- goes up when the password changes
);
CREATE TABLE IF NOT EXISTS email_tokens (
  hash    TEXT    PRIMARY KEY,          -- SHA-256 of the token, hex
  user_id INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  email   TEXT    NOT NULL,             -- the address the mail went to
  purpose TEXT    NOT NULL CHECK (purpose IN ('verify', 'reset')),
  created INTEGER NOT NULL,
  expires INTEGER NOT NULL
);

```

确认和重置是同一类东西：放在链接里的一个秘密，发到一个地址，只用一次。它们共用 `email_tokens`，用 `purpose` 分开，所以重置链接不能拿来确认地址，反过来也一样。这一行记着邮件发去的地址，所以发给旧地址的链接确认不了新地址。`epoch` 给 [重置](password-reset.zh-CN.md#ending-the-other-sessions) 用。

## 令牌 {#the-tokens}

`tokens.go`：

```go
package notes

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"time"
)

// ErrBadToken is the one answer for a token that is unknown, used or expired.
var ErrBadToken = errors.New("this link has expired or was already used")

// now is the clock of the tokens. Tests move it.
var now = time.Now

// newToken makes a token for a link. The database keeps only the hash of it, so a copy of the
// table cannot be turned into working links.
func newToken() (token, stored string) {
	b := make([]byte, 32)
	rand.Read(b) // never fails: Go stops the program if the system has no randomness
	token = base64.RawURLEncoding.EncodeToString(b)
	return token, storedToken(token)
}

// storedToken is what the table keeps for a token. SHA-256 is enough here: the token has 256 bits
// of randomness, so there is nothing to guess and nothing for a slow hash to slow down.
func storedToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// issueToken writes a token for a user and returns the text for the link.
func issueToken(ctx context.Context, tx *sql.Tx, userID int64, email, purpose string, ttl time.Duration) (string, error) {
	token, stored := newToken()
	t := now()
	_, err := tx.ExecContext(ctx, `INSERT INTO email_tokens (hash, user_id, email, purpose, created, expires) VALUES (?, ?, ?, ?, ?, ?)`,
		stored, userID, email, purpose, t.Unix(), t.Add(ttl).Unix())
	return token, err
}

// useToken spends a token in one statement. Two requests with the same token cannot both get a
// row back, so a token works once even when the link is opened twice at the same moment.
func useToken(ctx context.Context, tx *sql.Tx, token, purpose string) (userID int64, email string, err error) {
	err = tx.QueryRowContext(ctx, `DELETE FROM email_tokens WHERE hash = ? AND purpose = ? AND expires > ? RETURNING user_id, email`,
		storedToken(token), purpose, now().Unix()).Scan(&userID, &email)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, "", ErrBadToken
	}
	return userID, email, err
}

// TokenValid says whether a token would work, without spending it. A page uses it to show "this
// link has expired" before the visitor types anything.
func TokenValid(ctx context.Context, token, purpose string) (bool, error) {
	var n int
	err := DB.QueryRowContext(ctx, `SELECT count(*) FROM email_tokens WHERE hash = ? AND purpose = ? AND expires > ?`,
		storedToken(token), purpose, now().Unix()).Scan(&n)
	return n == 1, err
}

```

- **随机，并且只存哈希。** 32 个随机字节是 256 位，没人猜得到。表里存的是令牌的 SHA-256，所以数据库的副本或备份里没有能用的链接。argon2 这类慢哈希是给人自己选的密码用的，在这里没有任何好处。
- **一条语句用掉。** `DELETE ... RETURNING` 只会把那一行给其中一个请求。两个请求拿同一个令牌，不可能都成功，检查和删除之间也没有代码可以输掉竞争。
- **过期时间在同一个查询里。** 确认 24 小时，重置 30 分钟，在做令牌的地方设定。

## 注册 {#sign-up}

`signup.go`：

```go
package notes

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

const verifyTTL = 24 * time.Hour

// hashing limits how many argon2 runs happen at once, as in the sign-in guide.
var hashing = make(chan struct{}, 4)

// dummyHash is checked when an e-mail is unknown, as in the sign-in guide.
var dummyHash string

// hashPassword hashes under the same limit as a sign-in.
func hashPassword(ctx context.Context, password string) (string, error) {
	select {
	case hashing <- struct{}{}:
		defer func() { <-hashing }()
	case <-ctx.Done():
		return "", ctx.Err()
	}
	return HashPassword(password)
}

func normalizeEmail(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// SignUp makes an account that cannot sign in until the address is confirmed. It returns nil
// whether the address was new or not: the visitor always sees "check your mail", and only the
// owner of the mailbox learns which case it was.
func SignUp(ctx context.Context, email, name, password string) error {
	email = normalizeEmail(email)
	// Hash first, so a taken address costs the same time as a free one, and before the
	// transaction, which holds the write lock of the database.
	hash, err := hashPassword(ctx, password)
	if err != nil {
		return err
	}
	tx, err := DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var id int64
	err = tx.QueryRowContext(ctx, `INSERT INTO users (email, name, hash) VALUES (?, ?, ?) ON CONFLICT (email) DO NOTHING RETURNING id`,
		email, strings.TrimSpace(name), hash).Scan(&id)
	switch {
	case err == nil:
		err = queueVerification(ctx, tx, id, email)
	case errors.Is(err, sql.ErrNoRows):
		// The address has an account. If it never confirmed, a mail may have been lost: send a
		// new link. If it did, tell the owner, who may have forgotten.
		var verified sql.NullInt64
		if err = tx.QueryRowContext(ctx, `SELECT id, verified_at FROM users WHERE email = ?`, email).Scan(&id, &verified); err != nil {
			return err
		}
		if verified.Valid {
			err = QueueMail(ctx, tx, Mail{To: email, Subject: "You already have an account",
				Text: "Someone used this address to sign up, and it already has an account.\n\n" +
					"Sign in: " + AppURL + "/login\nForgot the password: " + AppURL + "/forgot\n\n" +
					"If it was not you, ignore this mail.\n"})
		} else {
			err = queueVerification(ctx, tx, id, email)
		}
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

func queueVerification(ctx context.Context, tx *sql.Tx, id int64, email string) error {
	token, err := issueToken(ctx, tx, id, email, "verify", verifyTTL)
	if err != nil {
		return err
	}
	return QueueMail(ctx, tx, Mail{To: email, Subject: "Confirm your address",
		Text: "Confirm your address to finish signing up:\n\n" + AppURL + "/verify?token=" + token + "\n\n" +
			"The link works once, for 24 hours. If you did not sign up, ignore this mail.\n"})
}

// VerifyEmail spends a verify token and marks the address as confirmed.
func VerifyEmail(ctx context.Context, token string) error {
	tx, err := DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	id, email, err := useToken(ctx, tx, token, "verify")
	if err != nil {
		return err
	}
	// Only if the account still has the address the mail went to.
	res, err := tx.ExecContext(ctx, `UPDATE users SET verified_at = ? WHERE id = ? AND email = ?`, now().Unix(), id, email)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return ErrBadToken
	}
	return tx.Commit()
}

```

- **回答从不说这个地址是不是已经有人用了。** 「这个地址已经注册」会让任何人拿一份地址清单来试。`SignUp` 在三种情况下都返回 nil，路由都重定向到同一个「请查收邮件」页。只有邮箱的主人能从邮件里看出区别：新地址收到确认链接，已有账号的地址收到「你已经有账号了」，从没确认过的地址会收到新的链接，这也顺便解决了邮件丢失的问题。
- **各种情况做同样的事。** 密码先哈希再查询，所以已被占用的地址不会回答得更快。哈希在事务打开之前做，因为事务占着数据库的写锁，不该为 19 MiB 的哈希计算一直占着。`hashPassword` 和登录共用同时最多四个的限制。
- **地址已有账号时，第二次注册的密码被丢掉。** 否则任何人都能通过注册别人的地址，来设定一个不属于自己的账号的密码。
- **邮件在事务里排队**，所以账号和它的邮件要么都有，要么都没有（[发送邮件](email.zh-CN.md#queue-a-mail-from-a-handler)）。

路由（`app/signup/route.go`）像 [表单](forms.zh-CN.md) 里那样校验表单：邮箱能被 `net/mail` 解析，密码 8 到 256 个字符。然后调用 `SignUp`，不管 `SignUp` 发现了什么，都回答 `tifa.Redirect("/signup/sent", 303)`。表单和登录页一样。

## 确认地址 {#confirm-the-address}

邮件里是一个指向页面的链接 `/verify?token=...`，页面上有一个按钮，把令牌 POST 出去。路由是 `app/verify/route.go`：

```go
package verify

import (
	"errors"

	"github.com/liustack/tifa"

	"example.com/notes"
)

type Query struct {
	Token string `json:"token"`
}

type View struct {
	Valid bool `json:"valid"`
}

func Load(c *tifa.Ctx, q Query) (View, error) {
	ok, err := notes.TokenValid(c.Context(), q.Token, "verify")
	return View{Valid: ok}, err
}

type Form struct {
	Token string `json:"token"`
}

func POST(c *tifa.Ctx, in Form) error {
	err := notes.VerifyEmail(c.Context(), in.Token)
	if errors.Is(err, notes.ErrBadToken) {
		return tifa.Error{Status: 400, Message: "This link has expired or was already used."}
	}
	if err != nil {
		return err
	}
	return tifa.Redirect("/login", 303)
}

```

**为什么用按钮，而不是链接本身。** 有些邮件系统和安全扫描程序会在本人之前打开邮件里的链接。在 GET 上确认的链接会被扫描程序用掉，本人看到的是「已过期」。GET 本来也不该改任何东西。所以 `Load` 只看一眼（`TokenValid`），由 POST 来用掉令牌。

`app/verify/page.tsx` 用 `page.useSearch()` 读出令牌，`valid` 为 false 时显示「这个链接已过期」，另有一个 `Confirm` 按钮，调用 `POST({ input: { token } })`。

## 谁可以登录 {#who-may-sign-in}

没确认的账号不该能登录。这个临时应用的登录先读用户那一行并检查密码，然后再看 `verified_at`：

```go
	ok, err := CheckHash(password, hash)
	if err != nil {
		return Session{}, err
	}
	if !ok || !found {
		return Session{}, ErrWrongLogin
	}
	if !u.verified.Valid {
		return Session{}, ErrNotVerified
	}
	return Session{UserID: u.id, Name: u.name, Epoch: u.epoch}, nil
}

```

「请先确认你的地址」这条消息排在密码检查之后，所以只有知道密码的人才会看到。登录的其他部分和 [登录与退出](sign-in.zh-CN.md#checking-the-sign-in) 里一样。

从没确认过的账号什么也不拥有：不能用它发邮件，不能用它发东西，也不能被 Google 或 GitHub 登录关联上（[用 Google 或 GitHub 登录](oauth.zh-CN.md#linking-to-an-account-that-exists)）。

## 陷阱 {#traps}

- **日志里的令牌。** `smtp.host` 为空时，邮件连同链接会写进日志（[发送邮件](email.zh-CN.md#settings)）。在你自己的机器上没问题，在生产环境不行。代理、CDN 或统计脚本也可能记下页面的地址，所以日志里记路由，不要记完整地址（见 [日志](logs.zh-CN.md)）。只能用一次、有效期又短，泄露出去的链接也没多大用处。
- **用 `Host` 头拼出来的链接。** 链接里的地址要取自应用的一个选项（上面的 `app.url`），不要取自请求。被伪造的 `Host` 会把攻击者的地址放进你发出的邮件里。
- **对已被占用的地址给不一样的回答。** 这是注册表单最常见的泄露。「忘记密码」表单和登录也一样（[重置密码](password-reset.zh-CN.md)）。
- **旧的行。** 用掉的令牌会被删除，过期的还留着。用一个任务定期清理：`DELETE FROM email_tokens WHERE expires < ?`。

## 测试 {#test-it}

临时应用的 `app/account/accounts_test.go` 用 `tifatest.Start` 启动应用，把 `notes.SendMail` 换成一个把每封邮件送进 channel 的函数，让真正的 worker 跑起来（`jobs.PollEvery` 设得很短），再从邮件里读出令牌。它检查：

- 新地址和已被占用的地址得到同样的重定向，收到的邮件不同
- 地址确认前，正确的密码也登录不了
- 确认链接可以打开两次而不被用掉，按钮只能用一次
- 表里存的是哈希，不是令牌

根包里还有一个测试，移动 `now`（`tokens.go` 里的一个变量），证明令牌会过期，而重置令牌确认不了地址。用真实邮件服务器的测试要你自己手动跑一次（[发送邮件](email.zh-CN.md#test-without-sending)）。

## 试一试 {#check-it}

```sh
go tool tifa check
go tool tifa build
./bin/notes --server-addr 127.0.0.1:9527
```

注册，从日志里复制链接，打开它，按下按钮，然后登录。
