---
summary: 'Add sign-up and e-mail confirmation to a Tifa app: single-use tokens kept as hashes, an answer that does not tell which addresses have an account, a link that opens a page and a button that spends it'
audience: app
order: 13
read_when:
  - Adding a registration page, or a link in a mail that confirms an e-mail address
  - Making a link that works once and expires, such as a confirmation or an invitation
  - A sign-up form tells strangers which addresses have an account
  - A confirmation link is already used when the person opens it, because a mail scanner opened it first
---

# Sign up and confirm an e-mail address

[Sign in and sign out](sign-in.md) starts with a user who already exists. This guide makes the user and proves that the address is the visitor's. [Reset a password](password-reset.md) uses the same tokens. Tifa has no users and no passwords of its own, so this is your code and your table, a few functions.

It builds on `HashPassword` and `CheckHash` from [Sign in and sign out](sign-in.md#passwords), `QueueMail` from [Sending email](email.md) and the database of [Keeping data in SQLite](database.md).

## Two tables

```sql
-- migrations/002_users.sql
CREATE TABLE users (
  id          INTEGER PRIMARY KEY,
  email       TEXT    NOT NULL UNIQUE,  -- lower case
  name        TEXT    NOT NULL,
  hash        TEXT    NOT NULL,
  verified_at INTEGER,                  -- unix seconds, NULL until the address is confirmed
  epoch       INTEGER NOT NULL DEFAULT 0 -- goes up when the password changes
);
CREATE TABLE email_tokens (
  hash    TEXT    PRIMARY KEY,          -- SHA-256 of the token, hex
  user_id INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  email   TEXT    NOT NULL,             -- the address the mail went to
  purpose TEXT    NOT NULL CHECK (purpose IN ('verify', 'reset')),
  created INTEGER NOT NULL,
  expires INTEGER NOT NULL
);

```

A confirmation and a reset are the same kind of thing: a secret in a link, sent to an address, spent once. They share `email_tokens`, and `purpose` keeps them apart, so a reset link cannot confirm an address and the other way round. The row names the address the mail went to, so a link sent to an old address does not confirm a new one. `epoch` is for [the reset](password-reset.md#ending-the-other-sessions).

## The tokens

`tokens.go`:

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

- **Random, and kept as a hash.** 32 random bytes are 256 bits, which nobody guesses. The table keeps the SHA-256 of the token, so a copy of the database or a backup holds no working link. A slow hash such as argon2 is for passwords that people choose, and adds nothing here.
- **Spent in one statement.** `DELETE ... RETURNING` gives the row to one request only. Two requests with the same token cannot both win, and no code between a check and a delete can lose a race.
- **An expiry in the same query**, 24 hours for a confirmation and 30 minutes for a reset, set where the token is made.

## Sign up

`signup.go`:

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

- **The answer never says whether the address was taken.** "This address is already registered" lets anyone test a list of addresses. `SignUp` returns nil in all three cases and the route redirects to the same "check your mail" page. Only the owner of the mailbox learns the difference: a new address gets a confirmation link, an address with an account gets "you already have an account", and an address that never confirmed gets a new link, which also fixes a lost mail.
- **The same work in all cases.** The password is hashed before the lookup, so a taken address does not answer faster. It is hashed before the transaction opens, since the transaction holds the write lock of the database and should not hold it for 19 MiB of hashing. `hashPassword` shares the limit of four at once with the sign-in.
- **The password of a second sign-up is dropped** when the address already has an account. Otherwise anyone could set the password of an account that is not theirs by signing up for it.
- **The mail is queued in the transaction**, so the account and its mail exist together or not at all ([Sending email](email.md#queue-a-mail-from-a-handler)).

The route (`app/signup/route.go`) validates the form as in [Forms](forms.md), with an e-mail that `net/mail` parses and a password of 8 to 256 characters, calls `SignUp` and answers `tifa.Redirect("/signup/sent", 303)` whatever `SignUp` found. The form is like the sign-in page.

## Confirm the address

The mail holds a link to a page, `/verify?token=...`, and the page has a button that posts the token. The route is `app/verify/route.go`:

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

**Why a button and not the link itself.** Some mail systems and security scanners open the links in a mail before the person does. A link that confirms on GET is spent by the scanner, and the person sees "expired". A GET must not change anything anyway. So `Load` only looks (`TokenValid`) and the POST spends the token.

`app/verify/page.tsx` reads the token with `page.useSearch()`, shows "this link has expired" when `valid` is false, and has a `Confirm` button that calls `POST({ input: { token } })`.

## Who may sign in

An account that is not confirmed should not sign in. The sign-in reads the user row and checks the password first, then `verified_at`:

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

The message "confirm your address first" comes after the password check, so only someone who knows the password sees it. Everything else about the sign-in is as in [Sign in and sign out](sign-in.md#checking-the-sign-in).

An account that never confirmed holds nothing: no mail sent from it, nothing posted, and no link to a Google or GitHub sign-in ([Sign in with Google or GitHub](oauth.md#linking-to-an-account-that-exists)).

## Traps

- **The token in a log.** With `smtp.host` empty the mail is written to the log with its link ([Sending email](email.md#settings)). Fine on your machine, not in production. A proxy, a CDN or an analytics script may keep the address of the page too, so log the route and not the whole address (see [Logs](logs.md)). A single use and a short life make a leaked link of little use.
- **A link built from the `Host` header.** Take the address in links from an option of the app (`app.url` above), never from the request. A forged `Host` would put the attacker's address in a mail that comes from you.
- **A different answer for a taken address.** It is the most common leak of a sign-up form. The same goes for the "forgot your password" form and for the sign-in ([Reset a password](password-reset.md)).
- **Old rows.** Spent tokens are deleted, and expired ones stay. Delete them now and then with a job: `DELETE FROM email_tokens WHERE expires < ?`.

## Test it

A test of the whole flow starts the app with `tifatest.Start`, replaces `notes.SendMail` with a function that sends each mail to a channel, lets the real worker run (`jobs.PollEvery` set short) and reads the token out of the mail. It checks that:

- a new address and a taken one get the same redirect and different mails
- the right password does not sign in before the address is confirmed
- the confirm link can be opened twice without spending it, and the button works once
- the table holds the hash and not the token

A second test moves `now` (a variable in `tokens.go`) to check that a token expires and that a reset token does not confirm an address. A test with a real mail server is yours to run once by hand ([Sending email](email.md#test-without-sending)).

## Check it

```sh
go tool tifa check
go tool tifa build
./bin/notes --server-addr 127.0.0.1:9527
```

Sign up, copy the link from the log, open it, press the button, and sign in.
