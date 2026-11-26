---
summary: 'Sign in with Google or GitHub in a Tifa app with golang.org/x/oauth2: state and PKCE in a short cookie, a callback that calls SetSession, linking by verified e-mail, providers that post the callback'
audience: app
order: 15
read_when:
  - Adding "Sign in with Google", "Sign in with GitHub" or another OAuth 2 or OpenID Connect sign-in
  - A provider sends the visitor back with a POST (Sign in with Apple) and the callback gets 403 `cross-origin request detected`
  - Deciding whether a sign-in by a provider may join an account that already exists
  - Writing a Go test for an OAuth callback without the provider
---

# Sign in with Google or GitHub

A visitor presses a button, agrees at the provider, and comes back signed in. Tifa keeps the session ([Sessions](../pages-and-data/sessions.md)) and the provider says who the visitor is. Your code sends the visitor out, checks the way back, and calls `tifa.SetSession`, as [Sign in and sign out](sign-in.md) does after a password. Tifa has no OAuth of its own. The library is `golang.org/x/oauth2`.

The endpoints come from the `google` and `github` packages of `oauth2`. Try each provider once with a real account.

## Set it up

At the provider, make an OAuth app with the address the visitor comes back to: `https://notes.example.com/auth/google/callback`, and the same with `github`. Keep the client ID and the secret in [options of the app](../build-and-deploy/config.md#options-of-the-app), the secret as a `tifa.Secret`:

```go
// options.go, and the same two for GitHub
var (
	GoogleClientID     = tifa.String("google.client_id", "", "Client ID of the Google OAuth app, empty turns Google sign-in off")
	GoogleClientSecret = tifa.Secret("google.client_secret", "Client secret of the Google OAuth app")
)

```

Build the callback address from `AppURL`, the address visitors use, and never from the `Host` of the request. A provider without a client ID is off. Users and sessions are those of [Sign up](sign-up.md), plus a table of identities:

```sql
CREATE TABLE IF NOT EXISTS identities (
  provider TEXT    NOT NULL,
  subject  TEXT    NOT NULL,  -- the provider's own id for the person, which never changes
  user_id  INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  PRIMARY KEY (provider, subject)
) WITHOUT ROWID;

```

## The providers

`providers.go`:

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

- `getJSON` (not shown) is a GET that fails unless the status is 200.
- **Google** is OpenID Connect. The `email` scope gives `email` and `email_verified`, and `sub` never changes ([documentation](https://developers.google.com/identity/openid-connect/openid-connect)). `email_verified` is read as a boolean, as the standard has it.
- **GitHub** has no OpenID Connect, and its user object has an address only if the person made it public. `GET /user/emails` needs the `user:email` scope and lists `email`, `primary` and `verified` ([documentation](https://docs.github.com/en/rest/users/emails)). The code takes the primary address, if GitHub verified it.

## Send the visitor out and take them back

`oauth.go`:

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

`normalizeEmail`, `now` and `Session` are those of [Sign up](sign-up.md) and [Reset a password](password-reset.md), and `safeNext` is the one of [the sign-in guide](sign-in.md#the-sign-in-route).

- **`state`** goes out in the address and comes back in the callback, and must equal the one in the cookie, which only this browser holds. It stops an attacker from signing a victim in as the attacker.
- **PKCE** (`code_challenge` with `S256`, then the `code_verifier` at the token request) ties the code to the browser that started the flow,. GitHub accepts only `S256` ([documentation](https://docs.github.com/en/apps/oauth-apps/building-oauth-apps/authorizing-oauth-apps)), and Google lists it in its discovery document. The verifier stays in the cookie.
- **The cookie is short and its own**, not the session cookie: `HttpOnly`, `Secure` when `AppURL` is HTTPS, `Path=/auth` so no other page gets it, ten minutes. The callback clears it first, so it works once.

The routes are a folder with a parameter, so one pair serves every provider. `app/auth/provider+/route.go`:

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

and `app/auth/provider+/callback/route.go`:

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

The start only changes its own cookie, so a link is enough: `<a href="/auth/google?next=%2Faccount">Sign in with Google</a>`. The callback is a GET too, since the provider redirects the visitor back, and it may call `SetSession` ([where](../pages-and-data/sessions.md#where-to-call-what)).

## Linking to an account that exists

`userFor` decides which user a profile is, in this order:

1. **The identity is known.** The key is (provider, `Subject`), not the address.
2. **A new identity, and an account with the same address.** The provider must say the address is verified and the account's address must be confirmed too. Then the identity joins the account. An account whose address was never confirmed is refused with a 409: anyone can sign up with an address that is not theirs and knows that password, so joining it would let them into the real owner's account ([Sign up](sign-up.md)).
3. **A new person.** The user gets a confirmed address and an empty password hash, which `CheckHash` refuses, so only the provider signs them in.

## A provider that posts the callback

With `response_mode=form_post`, Sign in with Apple sends the results as a POST with a form body to the redirect address, the `user` field with the name and address in it the first time only ([documentation](https://developer.apple.com/documentation/signinwithapplerestapi/request-an-authorization-to-the-sign-in-with-apple-server)). That is a write from another site, which Tifa refuses with 403 ([Cross-site writes](../build-and-deploy/headers-and-origins.md#cross-site-writes)). List the path:

```yaml
# tifa.yaml
server:
  cross_site_paths: "POST /auth/apple/callback"
```

That path is no longer checked, so the handler proves the request is real with the `state` and the cookie, which `FinishSignIn` checks for the POST too. List only that path.

A `SameSite=Lax` cookie is not sent with a POST from another site, so the cookie of a provider with `FormPost` is `SameSite=None`, which needs HTTPS.

Apple also needs a client secret that is a signed token you make yourself and a `Profile` that reads its `id_token`.

## Test it

A test needs no real provider. Start the app with `tifatest.Start` and stand a provider up in an `httptest` server with `/token`, `/userinfo`, `/user` and `/user/emails`. Let its token endpoint give a token once for each code, only when the SHA-256 of the `code_verifier` matches the `code_challenge` it was sent. Read `state` and the challenge from the redirect, then call the callback with a client that keeps cookies. Check the linking rules above, and that a callback fails the second time, with another `state`, with no cookie, with `error=` and with an unknown code. A cross-site POST to the Apple path should get 403 without the line in `tifa.yaml` and pass with it.
