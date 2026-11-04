# Testing a tifa app

Contents: the check command, testing domain code, testing route code with `tifatest`, middleware, signed-in requests, the render clock, checking the real HTML.

Full docs: [app.md](https://github.com/liustack/tifa/blob/main/docs/app.md#testing). The `docs/testing.md` of the tifa repository is about developing tifa itself, not about testing an app.

## The check command

```sh
go tool tifa check
```

Run it before you finish. It runs, and stops at the first failure: the tifa analysis, the `.tifa/` generation, `go vet ./...`, `go test ./...` and the TypeScript check (`tsc --noEmit` with the app's own `typescript`). An app without pages skips the TypeScript step. The TypeScript step needs `pnpm install` (or npm) to have been run.

For a quick Go-only loop, `go test ./...` works on its own. `tifa build` reads no `_test.go` files, so tests may sit in any folder under `app/`.

## Domain code

Domain code (queries, rules) is plain Go. Test it next to the code, `order_test.go` beside `order.go`. Shared resources are package-level variables, so a test replaces them: `myapp.DB = openTestDB(t)` with a fresh database in `t.TempDir()`. Tests that replace a global do not run in parallel. A `tifa new` app has a `useTestDB(t)` helper for this.

Declare an interface for a dependency only when there really are two implementations, the real one and a fake. Otherwise pass the value as an argument or swap the variable.

A test in `package orders` that imports a package which imports `orders` is an import cycle. Use `package orders_test` for it.

## Route code

`Load`, `Layout`, method handlers and `OnRequest` are plain functions. `tifatest.NewCtx(t, r)` turns an `httptest` request into the `*tifa.Ctx` they take.

```go
import (
	"net/http/httptest"
	"testing"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/tifatest"
)

func TestLoad(t *testing.T) {
	r := httptest.NewRequest("GET", "/projects/studio/orders/SH-1041", nil)
	r.Pattern = "/projects/{project}/orders/{order}"
	r.SetPathValue("project", "studio")
	r.SetPathValue("order", "SH-1041")
	c, _ := tifatest.NewCtx(t, r)
	o, err := tifa.Use(c, Load)
	if err != nil {
		t.Fatal(err)
	}
	if o.ID != "SH-1041" {
		t.Fatalf("got order %q", o.ID)
	}
}
```

- Path parameters are what `c.Param` reads, so set them with `r.SetPathValue`. `c.Pattern()` is `r.Pattern`.
- Run `Load` and `Layout` through `tifa.Use(c, Load)`. It applies the server's rules: once per request, writing the body fails. A `tifa.Use` of a Layout inside `Load` runs that Layout too.
- Call a method handler directly: `POST(c)`, or `POST(c, in)` with a typed input. The input is the value you pass, so test `Validate` on its own, and pass the validated input if `Validate` changes it.
- Check what the function returns, the value and the error. These tests do not check the HTTP answer. The server alone decodes input, runs `Validate`, writes JSON or 204, turns errors into 404, 422, redirects or `OnError`, and answers navigation requests. The recorder from `NewCtx` stays an empty 200 unless the function writes itself.

Testing `Validate` and the error a handler returns:

```go
func TestAddValidate(t *testing.T) {
	err := AddInput{Title: "  "}.Validate()
	var fields tifa.Fields
	if !errors.As(err, &fields) || fields["title"] == "" {
		t.Fatalf("want a message for title, got %v", err)
	}
}

func TestLoadNotFound(t *testing.T) {
	r := httptest.NewRequest("GET", "/todos/x", nil)
	r.SetPathValue("todo", "x")
	c, _ := tifatest.NewCtx(t, r)
	if _, err := tifa.Use(c, Load); !errors.Is(err, tifa.ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
}
```

An input that holds a file takes `tifatest.File(t, "sea.png", data)`, the `*multipart.FileHeader` the server would give. Its Content-Type follows the extension, and its temporary files go when the test ends.

## Middleware

Call `OnRequest` directly and pass a closure as `next`. The closure stands in for the route, so it does not recover a panic or write a result.

```go
func TestOnRequestRedirectsAnonymous(t *testing.T) {
	r := httptest.NewRequest("GET", "/admin", nil)
	r.Pattern = "/admin"
	c, _ := tifatest.NewCtx(t, r)
	called := false
	if err := OnRequest(c, func(*tifa.Ctx) error { called = true; return nil }); err == nil || called {
		t.Fatal("want a redirect and no route run")
	}
}
```

To test code that needs someone signed in, skip the cookie and call `tifatest.WithSession(c, &auth.Session{UserID: 1, Name: "Ann"})` after `NewCtx`. It sets the state of the request only, so `tifa.Session[auth.Session](c)` returns it in `OnRequest`, `Load` and `Layout`. `NewCtx` also gives each test a memory session store, so `SetSession` and `ClearSession` work and the recorder holds the `Set-Cookie`. `tifatest.TestSessionStore(t, store)` checks a store of your own.

```go
func TestLayoutShowsTheUser(t *testing.T) {
	c, _ := tifatest.NewCtx(t, httptest.NewRequest("GET", "/", nil))
	tifatest.WithSession(c, &auth.Session{UserID: 1, Name: "Ann"})
	shell, err := tifa.Use(c, Layout)
	// check shell.User and err
}
```

## Render clock

Only for tests and apps that need a fixed time in server renders. Set a clock on the request context in `OnRequest` (or in a test):

```go
at := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
return next(c.WithContext(tifa.WithRenderClock(c.Context(), func() time.Time { return at })))
```

It covers `Date`, `Temporal.Now` and Intl formatting without a date for the whole render. It does not change Go's `time.Now`. Returning displayed times from `Load` is still the right way to keep SSR, cache and hydration equal.

## Checking the real HTML and browser behavior

`tifatest` cannot render. To see the HTTP answer, hydration and navigation:

1. `go tool tifa build`, then run `./bin/<app> --server-addr 127.0.0.1:9600` in the background and note its PID. Or use `go tool tifa dev` and stop it by its PID when done. Only stop what you started.
2. `curl -si http://127.0.0.1:9600/path` shows the status, headers (`ETag`, `X-Tifa-Render`, and `Cache-Status` and `Age` on a page with `Cache`) and the HTML a crawler sees. For the navigation data a page fetches, add the query parameter `_tifa=<build ID>.<digits>` to the page URL, with a digit for each `Load` on the chain of the page from the root Layout down (`1` asks for it, `011` skips the root Layout). The build ID is in `.tifa/dist/build.json`. The answer is `{"routes": {route ID: result}}`, with a `session` next to `routes` in an app that has sessions (see the data protocol in [build.md](https://github.com/liustack/tifa/blob/main/docs/build.md#data-protocol)).
3. For hydration and clicks, open the page in a browser tool and watch the console. A hydration mismatch is reported there.
4. A POST from `curl` has no `Sec-Fetch-Site` or `Origin`, so it passes the cross-site check. A POST from a page on another origin gets 403.

On a page with `Cache`, the server hands `OnRequest` and `Load` an anonymous request, which `tifatest.NewCtx` does not do: it gives your function the request as you built it. The build already fails when such a `Load` or Layout reads a cookie, header or session. Check the cache itself with two `curl -si` requests, the first `Cache-Status: tifa; fwd=miss` and the second `tifa; hit`, and once with a `Cookie` header, which must give the same body.

Test cases worth writing for a new route: the not found path, the validation failure (422 with `fields`), the unauthorized path, and a second visit with `If-None-Match` for a page that should answer 304.
