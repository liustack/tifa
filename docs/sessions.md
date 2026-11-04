---
summary: 'How sessions work in a tifa app: signing in and out in Go, the session cookie, the memory store and how to keep sessions in Redis or SQL with a store of your own, where each call may be made, the checks at build time, how the browser shows who is signed in, how to test it, and what each error means'
read_when:
  - Signing users in and out, or reading who is signed in
  - Keeping sessions in Redis, SQL or a file instead of the memory of the process, or writing a store for tifa.SetSessionStore
  - Sizing the memory store with tifa.NewMemoryStore or session.memory_limit
  - Showing the name of the signed-in user in a header, `useSession`, `SignedIn`, `data-session`, or a CSP that blocks the script at the start of the head
  - Changing the session cookie, how long a session lasts, or the cookie name
  - A SetSession or ClearSession call returns an error, or the build fails with a message about the session type
  - Writing Go tests for code that needs someone signed in
  - Changing internal/core (session.go, sessionstore.go) or js/runtime/session.tsx
---

English · [简体中文](sessions.zh-CN.md)

# Sessions

A session is the record of who is signed in, kept on the server and found by a cookie. tifa keeps the record, the token, the cookie and the expiry. Your code checks the password and says when someone is signed in or out.

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

- `tifa.SetSession(c, &s)` signs the request in. It makes a new random token (32 bytes, 43 characters), stores `s` as JSON under the SHA-256 of the token, deletes the session the request came with, and sets the cookie. Every call makes a new token, so a sign-in never keeps a token an attacker planted. If the old session cannot be deleted, the sign-in fails with an error and no cookie goes out.
- `tifa.Session[S](c)` returns the session of the request, or nil. The server reads it before `OnRequest`, so every hook, `Load` and method handler sees it. It decodes into `S` on the first call, and `Load` functions that run at once share the one pointer, so treat it as read-only. Use one type `S` for the whole app: a call with another type panics. A stored record that does not fit `S` is deleted, the cookie is dropped, and the request is signed out.
- `tifa.ClearSession(c)` deletes the record, drops the cookie and makes `Session` return nil. It does nothing when nobody is signed in. If the store cannot delete the record, the cookie is still dropped and the error says the record stays valid until it expires.
- The record, with the envelope tifa adds around it, may not pass 4 KiB. Keep an ID and a few display fields in it, and the rest in your database. Whoever can write to the store can sign anyone in, so protect it like the database.

## The cookie

The cookie is `__Host-tifa_session` with `HttpOnly`, `Secure`, `SameSite=Lax`, `Path=/`, and no `Domain`. A session lasts 30 days from `SetSession`. It does not renew, so a request never writes to the store. Change the name and the lifetime with `session.cookie` and `session.max_age` (see [Configuration](config.md#settings)). Under `tifa dev` the cookie is `tifa_session` without `Secure`, since dev serves plain HTTP on localhost. A cookie that is not 43 characters of `A-Z a-z 0-9 - _` is dropped without asking the store. A cookie whose record the store no longer has, because it expired or the process restarted with the memory store, is dropped too, and the request is signed out.

An answer that sets or drops the cookie goes out as `Cache-Control: no-store` without `ETag` or `Last-Modified`, whatever the app set, and it keeps its status: it is never turned into a 304.

## Where to store sessions

Without a store, sessions live in the memory of the process, up to `session.memory_limit` (64 MiB). A full store drops the sessions that expire first, and a restart signs everyone out. The server logs one line about this at start. `tifa dev` restarts when your Go code changes, so you are signed out then. If your app lets visitors who are not signed in start a session, limit that at the entry, since flooding it pushes real sessions out.

To keep sessions somewhere else, such as Redis or SQL, give `OnStart` a store:

```go
func OnStart(ctx context.Context) error {
	tifa.SetSessionStore(redisStore{client})
	return nil
}
```

`tifa.SetSessionStore` panics once the server listens, like `tifa.SetHTTPHandler`. A store implements `tifa.SessionStore`:

```go
type SessionStore interface {
	Get(ctx context.Context, token string) (data []byte, found bool, err error)
	Put(ctx context.Context, token string, data []byte, expiry time.Time) error
	Delete(ctx context.Context, token string) error
}
```

- `token` is the SHA-256 of the cookie's token in base64url, never the cookie itself.
- `Get` returns `found == false` with a nil error for a key that is missing, expired or not valid. Return an error only when the store itself fails. The request then fails through `OnError` as an unexpected error: carrying on as signed out would hide the fault.
- `Put` creates or replaces the record, and `Get` must not return it after `expiry`. `Delete` returns nil for a key that does not exist.
- The three methods run on many requests at once. `Put` must not keep the `data` slice, and `Get` must not return a slice the store still uses. They return an error when `ctx` is done.

### The memory store

The store tifa uses when `OnStart` sets none is a `*tifa.MemoryStore` of `session.memory_limit` bytes. To make one yourself, for example to give sessions less memory, call `tifa.NewMemoryStore(maxBytes)` and pass it on:

```go
func OnStart(ctx context.Context) error {
	tifa.SetSessionStore(tifa.NewMemoryStore(8 << 20))
	return nil
}
```

- A store you set replaces the default one, so `session.memory_limit` no longer applies.
- `maxBytes` counts, for each record, its key, its data and a fixed cost. When the store is full, a `Put` drops the records that expire first. A record that alone costs more than `maxBytes` makes `Put` return an error, so `SetSession` fails. `maxBytes` must be positive, or `NewMemoryStore` panics.
- It runs a goroutine that removes expired records once a minute. The server stops the store it made. Call `Close` yourself on one you made (in a test, `defer m.Close()`). The store keeps working after `Close`, as `Get` never returns an expired record.

### A store of your own

Write the three methods of `tifa.SessionStore` on top of Redis, SQL or a file, and give it to `tifa.SetSessionStore` in `OnStart`. The session test app (`test/apps/session`) keeps sessions in a file, so that a second process finds the sessions of the first. It rewrites the whole file on every call, which is fine for a test and wrong for traffic:

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
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.load()[key]
	if !ok || !time.Now().Before(r.Expires) {
		return nil, false, nil
	}
	return r.Data, true, nil
}

func (s *fileStore) Put(ctx context.Context, key string, data []byte, expiry time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	records := s.load()
	records[key] = fileRecord{Data: append([]byte(nil), data...), Expires: expiry}
	return s.save(records)
}

func (s *fileStore) Delete(ctx context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	records := s.load()
	delete(records, key)
	return s.save(records)
}
```

`load` reads the file into a map and `save` writes the map back. Note what the methods do: a missing or expired key is `found == false` with a nil error, `Put` copies `data`, and a key that is not there is not an error for `Delete`.

With Redis or SQL, let the database remove expired records (`EXPIRE` or `SET ... EX` in Redis, a `DELETE` of old rows or an index on the expiry column in SQL), and still check `expiry` in `Get`.

### Testing a store

`tifatest.TestSessionStore(t, store)` runs the rules above against your store: a missing key, a replaced record, delete of a missing key, expiry, a copy of the slices, a done `ctx` and many calls at once. Give it an empty store that nothing else uses. It takes about half a second, since it waits for a record to expire, and it deletes what it writes. The memory store passes it in tifa's own tests:

```go
func TestMemoryStoreIsAValidStore(t *testing.T) {
	m := tifa.NewMemoryStore(1 << 20)
	defer m.Close()
	tifatest.TestSessionStore(t, m)
}
```

Call it the same way with your store in place of `m`.

## Where to call what

| Where | `Session` | `SetSession`, `ClearSession` |
| --- | --- | --- |
| `OnRequest` before `next` | works | works |
| `OnRequest` after `next` returned | works | returns an error |
| A method handler | works | works |
| `Load`, `Layout`, `OnError` | works | returns an error |
| After the response is sent | works | returns an error |

A `Load` runs for a GET, which must not change anything, so signing in belongs in a method handler or in `OnRequest`.

On a page with `Cache` the GET and HEAD requests are anonymous: `Session` is nil there whoever asks, and `SetSession` and `ClearSession` return an error. The promise is about the request headers tifa hands to your code. A value that a `net/http` middleware from `tifa.SetHTTPHandler` put into the context still arrives, so recognize visitors in `OnRequest` and not in that middleware (see [The whole-page cache](app.md#the-whole-page-cache)).

## Sign-in libraries

tifa stores the session. Your app or a library checks who the user is: password hashes (bcrypt, argon2), the OAuth code flow and its `state` check (`golang.org/x/oauth2`), two-step verification. When that succeeds, call `tifa.SetSession`. The temporary state of such a library, an OAuth `state` for instance, goes in a short cookie of the library's own. Check access in Go by reading `tifa.Session`.

Writes from other sites are refused before any of this runs (see [Cross-site writes](config.md#cross-site-writes)), and the cookie is `SameSite=Lax`, so a form on another site cannot sign a user out or act for them. For that to hold, make sure GET, HEAD and OPTIONS never change anything: sign out with a POST.

## Checks at build time

`tifa build` and `tifa check` read every call of `tifa.SetSession`, `tifa.Session` and `tifa.ClearSession` in your module and fail when:

- the calls do not all use one session type (the error shows both places), or one is made with a type parameter in a generic function
- `Session` or `ClearSession` is called and nothing calls `SetSession`
- the type is not a struct, or cannot be read back from JSON
- `Load` or `Layout`, or a function they pass the Ctx to, calls `SetSession` or `ClearSession`
- a page or API route path starts with `/__tifa/`, or `public/__tifa/` holds a file (`/__tifa/session` is the path of the session answer)

The session type may have a method `Exposed() M` (a pointer receiver works too). It returns the part of the session that may go to the browser. It is written into the page HTML and kept in the browser's `localStorage`, so every field in it must be fine to stay on the visitor's device for a long time. Put no credentials and no secrets in it, and always check access with the server-side session. `M` follows the rules of a `Load` result, and the build writes its TypeScript type to `.tifa/session.gen.ts`:

```tsx
import type { SessionData } from '@/.tifa/session.gen'
```

The same file exports `SessionEnabled`, which is `true` when the app calls `tifa.SetSession` and `false` otherwise. `SessionData` is `never` when the type has no `Exposed` method or the app has no session. A `Exposed` with parameters, with more than one result, or whose result cannot be JSON fails the build.

## In the browser

The browser keeps a copy of who is signed in and shows it from the first paint, before any script of the page has run. Pages read it with `useSession`, and `SignedIn` and `SignedOut` show or hide an element by it.

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

- `useSession()` returns `{ signedIn, data, ready }`. `data` is what the `Exposed` method returned (its type is `SessionData`), and `undefined` when the type has no `Exposed` or nobody is signed in. Calling it in an app that never calls `tifa.SetSession` is a type error, and throws.
- `SignedIn` and `SignedOut` show their child only to someone signed in or signed out. Each takes exactly one element as its child and renders no element of its own. The child must pass its props on to its DOM element, as `<a>`, `<div>` and components that spread `...props` do. In development a child that does not fails with a message when it mounts. A style tifa puts at the start of the head hides the wrong one from the first paint, so you write no CSS.
- `ready` is `true` once the browser knows which state it shows. For a page rendered for the visitor it is `true` from the first render. See below for the pages that come without a session.
- The copy decides what to show, never what someone may do. Check access in Go.

The session reaches the browser with every answer that is meant for one visitor, in the shape `{ "signedIn": true, "data": ... }` (`data` only when the type has `Exposed`, and `{ "signedIn": false }` when nobody is signed in):

| Answer | What it carries |
| --- | --- |
| A page | The session in the page, and `<html data-session="in">` or `<html data-session="out">` written by the server. With `SSR = false` the session is in the boot data |
| Navigation data | `{ "routes": { ... }, "session": ... }`, so a link click brings the current session |
| A call of a handler function that signed someone in or out | `{ "result": ..., "session": ... }`. Without a result, `{ "session": ... }` instead of 204. A call that left the session alone has no `session` and keeps its 204. A redirect carries it too, an error does not |
| `GET /__tifa/session` | The session of the request, `Cache-Control: private, no-store`. It runs inside `OnRequest`, so a middleware that signs visitors in must let it through |

A request without the header `X-Tifa-Call` (curl, a form, another program) gets the answer it always got.

The browser keeps the session in memory and in `localStorage` under the cookie name and `:v1`, such as `__Host-tifa_session:v1`. Whatever answer arrives with a session writes both copies: signed in writes, signed out clears them, an answer without a session leaves them alone, and `<html data-session>` follows. The session a page carries wins over `localStorage`. If `localStorage` fails in any step, as in a private window, the copy in memory is used and nothing stops. When requests cross, the answer to the one sent last wins.

A page the server answers for every visitor alike, such as one with `Cache` (see [The whole-page cache](app.md#the-whole-page-cache)), carries no session and no `data-session`. Only there, a script at the start of the head sets `data-session="in"` from `localStorage` before the first paint, `useSession` renders signed out until the page has hydrated, and then switches to the copy in `localStorage` and asks `/__tifa/session` once to check it. With no copy it asks nothing. If `localStorage` cannot be read it asks once too.

Calling a handler function from the browser updates the copy when the call changed the session. The data requests of later navigations bring the session too. Use an input or a result type for sign-in and sign-out handlers (`func POST(c *tifa.Ctx, in Login) error`). A handler that writes its own response (`func POST(c *tifa.Ctx) error`) cannot carry the session in its answer.

An answer that carries the session must say `Cache-Control: no-store`, or `private` and `no-cache` together, both without a list of fields (`private="Set-Cookie"` does not count), and no `must-understand`. Leave the header alone and it is `private, no-cache`. If `OnRequest`, a `Load` or a handler sets anything else on a page, a page's boot data or the envelope of a call, the request fails as an unexpected error through `OnError`, and the error answer is `no-store` without `ETag` or `Last-Modified`. A navigation redirect, and a navigation answer that is already reporting an error, are not raised again: the server logs it, clears the validators and sends `no-store`. `/__tifa/session` is always `private, no-store` without validators, also when a middleware answers it itself. Handlers that write their own response are not checked.

`data-session` is an attribute tifa keeps on `<html>`. A root Layout whose `htmlAttributes` sets it stops the server when it starts (the VMs fail to prewarm, and `tifa dev` shows the error), and a route whose `htmlAttributes` sets it fails the render.

**Content Security Policy.** The script and the style are inline and the same on every page. With a CSP in a `<meta>` tag they are not affected, since they come before the tag. With a CSP in a header, allow their hashes:

```text
script-src 'sha256-NWjzg1BGj/5CJBw80xzDV4Gmw6vQUG2ExZLUcX4mWkE='
style-src 'sha256-dutb67YDz+meUugqDFx3RsopE/hCQ31NRHukbTNY+OQ='
```

These are for the default cookie `__Host-tifa_session`. The script holds the name of the `localStorage` key, so it has another hash when you set `session.cookie`, and under `tifa dev`, where the cookie is `tifa_session` (the script's hash is `sha256-RXrgEYZIzBu7H8LtSsupWFiD5XE/6dvi74b6rHR+nVg=`). Read the script out of a page of your build and hash it.

**What it cannot do.** Another tab shows the old state until its next answer with a session, and a form submitted there already uses the new cookie. After you sign out on another device, a page that comes from `localStorage` shows the old name until the check comes back. Going back to a page of the browser's back-forward cache after signing in as someone else shows the old page as it was. A preload that was running when you switched accounts can bring the old account's data. A `fetch` you write yourself that changes the session is unknown to tifa: call `router.invalidate()` after it, and `router.clearCache()` when the pages the router kept must not be shown to the next person. Keep `Exposed()` to what the browser may keep on the device, such as a name and an avatar, and no credentials, e-mail address or phone number.

## Testing sessions

`tifatest.NewCtx(t, r)` gives the `Ctx` a memory store that belongs to the test, so `SetSession` and `ClearSession` work and the recorder holds the `Set-Cookie`. All the `Ctx` of one test share the store, so the cookie from one can sign in the next: add it to the request with `r.AddCookie`. To test a function that needs someone signed in, skip the cookie:

```go
c, _ := tifatest.NewCtx(t, httptest.NewRequest("GET", "/", nil))
tifatest.WithSession(c, &auth.Session{UserID: 1})
```

`WithSession` only sets the state of the request. It does not check the cookie and store flow, which tifa's own tests cover.

## Errors

| Message starts with | When | What to do |
| --- | --- | --- |
| `tifa: SetSession cannot be called while a page with Cache is served` | The call ran on a GET or HEAD of a page with `Cache` | Put sign-in and OAuth callbacks on a route without `Cache` |
| `tifa: SetSession cannot be called in a Load or a Layout` | The call ran in `Load` or `Layout` | Sign in in a method handler, or in `OnRequest` before `next` |
| `tifa: SetSession cannot be called in OnRequest after next returned` | `OnRequest` called it after `next(c)` | Call it before `next` |
| `tifa: SetSession cannot be called in OnError` or `once the response is sent` | Too late, the response is decided or written | Move the call earlier. Same for `ClearSession` |
| `tifa: SetSession has no session store` | The `Ctx` did not come from the server | In a test, make the `Ctx` with `tifatest.NewCtx` |
| `tifa: SetSession needs a session, got nil` or `the session encodes as null` | The pointer is nil, or the value is JSON `null` | Pass a pointer to a struct |
| `tifa: the session takes N bytes, over the limit of 4 KiB` | The record is too big | Keep an ID and a few display fields, and the rest in your database |
| `tifa: storing the session` | `Put` of the store failed | The error of the store is wrapped, look there |
| `tifa: the previous session was not revoked, so the sign-in did not complete` | `Delete` of the old record failed | The sign-in is not done: no cookie goes out, the new record is removed again, and the old session stays. Look at the store |
| `tifa: deleting the session failed, so its record stays valid until it expires` | `ClearSession` could not delete the record | The cookie is dropped, but the record lives until it expires. Look at the store |
| `tifa: reading the session` | `Get` of the store failed | The request fails through `OnError` as an unexpected error |
| A panic that names two session types | `SetSession` and `Session` use different types | Use one type for the whole app. The build reports it first |
