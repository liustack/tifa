---
summary: 'How to sign users in and out in a Tifa app with Tifa sessions: a sign-in form, keeping and checking passwords with argon2id, SetSession, reading the user in Load and handlers, a guard that sends a signed-out visitor to the sign-in page and back with c.Path(), guarding pages, signing out, showing who is signed in in the browser, and one way to slow down password guessing'
audience: app
order: 3
read_when:
  - Adding sign-in and sign-out to an app, or putting some pages behind a login
  - Choosing how to store and check passwords
  - A signed-out visitor sees a page that should be private, or a signed-in header shows the wrong state
  - Redirecting a signed-out visitor to the sign-in page and bringing them back to the page they asked for
---

# Sign in and sign out

Visitors sign in with an e-mail and a password, see an account page that only they can open, and sign out. Tifa keeps the session: the random token, the cookie and the expiry. Your code does three things: keep and check the passwords, say when someone is signed in or out, and decide who may see what.

The code was run as written (with `go tool tifa check` and in a browser). What each session call does, and what the cookie is, is in [Sessions](../pages-and-data/sessions.md). This guide does not repeat it.

## Who is signed in

`session.go` in the root package:

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

One session type for the whole app. `Exposed` is the part that goes to the browser, where it is kept on the visitor's device. Put nothing secret in it (see [the browser side](../pages-and-data/sessions.md#in-the-browser)).

## Passwords

Keep only a hash of each password, made with a hash that is slow on purpose. This guide uses argon2id, from `golang.org/x/crypto/argon2`. The [OWASP password storage guide](https://cheatsheetseries.owasp.org/cheatsheets/Password_Storage_Cheat_Sheet.html) lists it first and says to use bcrypt only where argon2 is not available. The parameters below are its minimum: 19 MiB of memory, 2 passes, 1 lane. `examples/shop` uses bcrypt, which is a good second choice: one call each way (`bcrypt.GenerateFromPassword`, `bcrypt.CompareHashAndPassword`), a salt and a cost inside the hash, and a limit of 72 bytes per password ([package docs](https://pkg.go.dev/golang.org/x/crypto/bcrypt)). Argon2 needs a few more lines, because the package gives you the key and you write down the salt and the parameters yourself.

`passwords.go`:

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

The text it makes looks like `$argon2id$v=19$m=19456,t=2,p=1$<salt>$<key>`, which is the usual way to write an argon2 hash. The parameters travel with the hash, so you can raise them later and old hashes still check. Run `HashPassword` when someone registers or changes their password, and keep the result in your users table. This guide has no registration page.

## Checking the sign-in

`users.go`:

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

Replace the `users` slice with a query on your database. What stays: the same answer for a wrong e-mail and a wrong password, a hash check even when the e-mail is unknown, and the two limits.

- **Same answer, same time.** "No such user" and "wrong password" would tell an attacker which e-mails have accounts. The dummy hash makes the two cases take about as long.
- **Slowing down guessing.** The one way shown: count wrong passwords per e-mail and stop checking that e-mail for the rest of 15 minutes after five. It is cheap and needs nothing outside the app. The cost: someone who knows your user's e-mail can keep them out for a few minutes. If your app runs on more than one machine, keep the counts in a shared store.
- **The hashing queue.** Each argon2 check holds 19 MiB while it runs. Four at once caps the memory of hashing at about 76 MiB when someone floods the form.

## The guard

`auth.go`:

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

`c.Path()` is the path of the page as the visitor sees it in the address bar: the language prefix is in it, and the query is not. It is the same when the page is loaded whole and when a link click asks for its data, and for a handler it is the path of the handler. `c.Request()` is not for this: `Load` may not call it, and on a link click the request is a data request, not the page. `RequireUser` works in `OnRequest`, in a `Load` and in the functions they call.

The query is left out of the `next` that `RequireUser` makes. A page that keeps its state in query parameters puts them back in its own `Load`, such as `signin.SignInRedirect(c.Path()+"?tab="+url.QueryEscape(c.Query("tab")))`. A guard shared by many pages cannot do it for all of them, because `Load` may only read the query parameters it names (see [How Load may use Ctx](../pages-and-data/routes.md#how-load-may-use-ctx)). `OnRequest` is different: a hook may read the request, so the guard below keeps the whole query. The server takes `_tifa`, the parameter of a data request, off the URL before `OnRequest` runs, so `c.Request().URL.RawQuery` is the query the visitor wrote for a page load and for a link click alike.

## The hooks

`hooks.go`:

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

`tifa.Session[Session](c)` is nil when nobody is signed in. The server reads the session before `OnRequest`, so every hook, `Load` and handler can ask.

## The sign-in route

`app/login/route.go`:

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

func Load(c *tifa.Ctx) (View, error) {
	return View{Next: safeNext(c.Query("next"))}, nil
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

- Sign in in a method handler, not in a `Load`. A `Load` runs for a GET, which must not change anything, and `SetSession` returns an error there (see [where to call what](../pages-and-data/sessions.md#where-to-call-what)).
- `SetSession` makes a new random token on every call and drops the session the request came with, so a sign-in never keeps a token someone else chose.
- An unexpected error (a database that is down) goes to `OnError`. A wrong password is a `tifa.Error` with a status, which does not.
- `next` comes from the URL, so anyone can put anything in it. `safeNext` keeps it to a path on this site. Without it your sign-in page would send visitors to any site an attacker names. Do not drop it.
- The handler returns `tifa.Redirect(..., 303)`. The page's call follows it through the router, and the copy of the session in the browser is updated by the same answer (see [the browser side](../pages-and-data/sessions.md#in-the-browser)).

## The sign-in page

`app/login/page.tsx`:

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

`autoComplete="username"` and `"current-password"` let password managers fill the form. The field errors work as in [Forms](forms.md). A form library works here too, as in `examples/shop/app/login/page.tsx`.

## Using the user

`app/account/route.go`:

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

- **Read the session, then the database.** Take the ID from the session and load the rest. The session is a copy made at sign-in. A name changed later, a role taken away or a disabled account is not in it.
- **Guard in two places.** `OnRequest` turns visitors away from a whole area before any `Load` runs, for page loads and link clicks alike. A handler is a URL of its own and can be called without its page, so each handler that needs a user checks the session itself. A `Load` that returns a user's data should do the same, with `RequireUser`.
- **Keep the copy in step.** When something in the session changes, call `SetSession` again, as `POST` does. The call brings the new session to the browser, so the header follows at once.
- **Pages with `Cache`.** On a page with `Cache`, `Session` is nil for everyone, so such a page cannot be private (see [Cache for CDNs](../pages-and-data/ssr-and-cache.md#cache-for-cdns)).

`app/account/page.tsx`:

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

## Signing out

`app/logout/route.go`:

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

`ClearSession` deletes the record on the server and drops the cookie, so the old cookie is of no use even if someone kept a copy. It does nothing when nobody is signed in. Sign out with a POST and never a GET: a link or an image on another site would otherwise do it (see [Sign-in libraries](../pages-and-data/sessions.md#sign-in-libraries)).

The page needs to hear that the session ended: the copy of it in the browser drives `SignedIn`, `SignedOut` and `useSession`. Tifa puts the new session in the answer of the call, so the header follows at once. This holds for a handler that redirects, as above, and for one that ends with `return tifa.ClearSession(c)` and has no input and no result. Only a handler that writes its own response with `c.JSON` or `c.Text` cannot carry it (see [the browser side](../pages-and-data/sessions.md#in-the-browser)).

## The browser side

`app/layout.tsx` (the part that matters, with `app/styles.css` any stylesheet you like):

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

- `SignedIn` and `SignedOut` show their one child by the session. The server marks the page, so the first paint is right and nothing flashes.
- `data` is the `Exposed` part of the session, typed from Go. It is for showing, not for deciding what someone may do. Go decides that.
- `router.clearCache()` after signing out drops the pages the router kept in memory, so the next person on this browser does not see the last one's data.
- A `fetch` of your own that signs someone in or out is unknown to Tifa. Use the generated function, or call `router.invalidate()` after it (see the end of [the browser side](../pages-and-data/sessions.md#in-the-browser)).

## Test it

`app/login/route_test.go`:

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

`tifatest.NewCtx` gives the test a session store of its own, so `SetSession` works. The cookie is in the recorder as soon as `SetSession` returns. Make one `NewCtx` per request and read `rec.Result()` after the function returns: the recorder keeps the headers of its first `Result()`, so a recorder used for two requests never shows the cookie of the second. The `Ctx`s of one test share the store, so the cookie of one signs in the next (see [Testing sessions](../pages-and-data/sessions.md#testing-sessions)). To test a function that needs a user without signing in, use `tifatest.SignIn` (see [Testing sessions](../pages-and-data/sessions.md#testing-sessions)). To walk through the whole sign-in as a browser does, from the protected page through the redirect and the form to the page again, start the app with `tifatest.Start` (see [Testing a whole request](../pages-and-data/testing-apps.md#testing-a-whole-request)).

`app/account/request_test.go` walks the whole path as a browser does, with `tifatest.Start`: the guard, the sign-in, the page, the sign-out and the guard again. It is in a package of its own because `tifaapp` imports every route:

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

`Start` runs `OnStart`, so the demo account exists. `client` keeps the session cookie from the sign-in and sends it on the next request, and the sign-out drops it. The test fails with a message to run `go tool tifa check` when `.tifa/tifaapp` is out of date (see [Testing a whole request](../pages-and-data/testing-apps.md#testing-a-whole-request)).

## Decisions

**Sessions in memory or in a store.** An app made with `tifa new` already keeps sessions in its SQLite database (`sessions.go`). Without a store, sessions live in the memory of the process and a restart signs everyone out. That is fine to start with. To keep them somewhere else, give `OnStart` a store (see [Where to store sessions](../pages-and-data/sessions.md#where-to-store-sessions)).

**How long a session lasts.** 30 days from sign-in, set by `session.max_age`. It does not renew. Shorten it for an app with money or private data in it.

**OAuth, magic links, two-step codes.** They replace the password check, not the rest. When the check succeeds, call `tifa.SetSession` as above. The temporary state of those flows goes in a short cookie of the library's own (see [Sign-in libraries](../pages-and-data/sessions.md#sign-in-libraries)).

**Registration and password reset.** Not shown. Both end in `HashPassword` and a write to your users table.

## Check it

```sh
go tool tifa check
go tool tifa dev
```

Open `/account` signed out and it sends you to `/login?next=%2Faccount`. Send the form empty and both messages show. Five wrong passwords for one e-mail and the sixth try, even with the right password, gets "Too many attempts". Sign in as `ada@example.com` with `correct horse battery` and you land on `/account` with "Hello, Ada" in the header. Rename yourself and the header changes. Sign out and `/account` sends you to the sign-in page again.
