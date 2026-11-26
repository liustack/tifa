---
summary: 'Reset a forgotten password in a Tifa app: a mailed single-use link, a limit on mails, ending every old session with a number the session carries'
audience: app
order: 14
read_when:
  - Adding "forgot your password" to an app
  - A password reset or change must sign the user out on every other device
  - A reset form tells strangers which addresses have an account, or can be used to fill an inbox with mail
  - Checking a reset flow, or code a coding agent wrote for one, for holes
---

# Reset a password

A visitor who forgot the password asks for a link, gets it by mail, and chooses a new password. The link is the proof that they own the mailbox. This guide uses the tokens, the tables and the mail of [Sign up and confirm an e-mail address](sign-up.md), so read it first. Tifa has no password reset of its own: it is two functions and two routes.

## Ask for a link

`reset.go`:

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

- **Same answer for every address.** `RequestReset` returns nil for an address with no account, and the route redirects to the same "check your mail" page. A form that says "no such address" tells anyone which addresses are customers. The unknown case skips a few database writes, a small difference, and the mail is sent from a job, so a request never waits for the mail server.
- **At most three a hour for one account.** Without it, the form can fill someone's inbox with mail from you. A limit per account does not stop someone who asks for many accounts, so add a limit by address in `OnRequest` for a public app.
- **A new token each time, valid for 30 minutes.** The link holds the token, and the table keeps its hash.
- **A check before the hash.** `ResetPassword` looks at the token before it hashes the new password, so guessing tokens does not cost an argon2 run each.
- **One transaction.** Spending the token, setting the password, raising `epoch` and deleting the other reset links of the account all happen or none do.
- **The address is confirmed too.** A reset mail reached the mailbox, so `verified_at` is set when it was empty.

## The page

A link in a mail opens a page, and the page posts the token. `app/reset/route.go`:

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

The `Load` only looks, so a scanner that opens the link does not spend it (see [why](sign-up.md#confirm-the-address)). `app/reset/page.tsx` reads the token with `page.useSearch()`, shows "this link has expired" when `valid` is false, and posts `{ token, password }` from a form with `autoComplete="new-password"`. On success the route redirects to `/login`. `app/forgot` is a form with an e-mail field whose handler calls `RequestReset` and redirects to a "check your mail" page.

The reset does not sign the visitor in. A mail that reached an inbox is enough to change the password, but signing in as well gives a leaked link more power. Redirect to the sign-in page.

## Ending the other sessions

Someone who has the old password may hold a session. A new password should end it. A Tifa session is found by its cookie token and holds only what you put in it, so to end every session of one user without a list of them, use a number the session carries:

- `users.epoch` goes up by one in `ResetPassword`.
- The session carries the `Epoch` it was made with: `Session{UserID, Name, Epoch}`, filled in by the sign-in.
- `Current` reads the row and counts a session with another epoch as signed out.

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

Use `Current` where [Sign in and sign out](sign-in.md#the-guard) calls `tifa.Session[Session](c)` to decide who may do something: in `RequireUser` and in the `OnRequest` guard. It costs one read of a row by its key for each request that has a session. The old sessions stay in the store until they expire, and are of no use. The same one-line bump ends the other sessions when a signed-in user changes their password: raise `epoch`, then call `SetSession` for this browser with the new value.

## Traps

- **The link in the log.** With `smtp.host` empty, the reset link is in the log ([Sending email](email.md#settings)). Do not leave that on in production.
- **The `Host` header in the link.** Build it from `app.url`, not from the request.
- **A reset page with scripts of other sites.** The token is in its address. Keep it free of analytics and widgets. Tifa already sends `Referrer-Policy: strict-origin-when-cross-origin`, so a link on that page does not carry the address to other sites ([Security headers](../build-and-deploy/headers-and-origins.md#security-headers)).
- **A reset that leaves sessions alive.** The `epoch` above. Without it a stolen session survives the new password.
- **A password check with no limit.** The reset sets the password through the same `hashPassword` and its limit of four at once.

## Test it

The test of the whole flow, built with the same helpers as in [Sign up](sign-up.md#test-it), checks that:

- an unknown address gets the same redirect and no mail
- the new password works, the old one and the old session do not, and the token works once
- a form with a password that is too short is refused with the token unspent
- the fourth request in an hour sends no mail, and a reset removes the other open links

A second test that moves the clock checks that an old token is refused.
