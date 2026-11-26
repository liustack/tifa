---
summary: 'Let a page on another site call the API of a Tifa app: answer the OPTIONS preflight in OnRequest, set Access-Control-Allow-*, and how that fits with server.trusted_origins'
audience: app
order: 17
read_when:
  - A separate front end or a partner site calls an API route of the app from the browser and the console says "blocked by CORS policy" or the call fails with "Failed to fetch"
  - Answering an OPTIONS request, or setting `Access-Control-Allow-Origin`
  - A cross-site POST gets 403 `cross-origin request detected` and you do not know whether you need `server.trusted_origins` or CORS headers
---

# Calls from another site (CORS)

Your own pages call your own routes, which is the same origin, and nothing here applies. This guide is for a page on **another origin**, such as `https://app.example.com` calling `https://api.example.com/api/items`, from a browser. A call from a server, from curl or from an app is not limited by CORS.

Tifa sends no CORS headers and has no setting for them. It is a few lines in `OnRequest`. The code ran in a scratch app made with `tifa new`: `go tool tifa check`, a Go test, curl against the built program, and a page on a second port in a browser.

## Two gates, two settings

A browser call from another origin passes two checks, and each has its own answer:

| Check | Who makes it | What opens it |
| --- | --- | --- |
| **CORS**: may the page send this request and read the answer? | The browser, from `Access-Control-Allow-*` headers | Your `OnRequest`, below |
| **Cross-site writes**: may a POST, PUT, PATCH or DELETE from another site run at all? | The program, before `OnRequest` ([Cross-site writes](../build-and-deploy/headers-and-origins.md#cross-site-writes)) | `server.trusted_origins`, or `server.cross_site_paths` for a path |

A read (GET) needs only CORS. A write needs both. Tried in a browser, with the page on `http://127.0.0.1:39520` and the API on port 39519, which is another origin:

| Settings | GET | POST with a JSON body |
| --- | --- | --- |
| none | blocked | blocked |
| CORS headers for the origin | 200 | blocked, the program answered 403 |
| CORS headers and `server.trusted_origins` | 200 | 200 |

A refused write is answered by the program before `OnRequest`, so it carries no CORS headers, and the page sees only "Failed to fetch" and not the 403. Read the log of the program: it prints `refused a cross-site write` with the `Origin` it saw.

A POST with a JSON body is not a simple request, so the browser first sends an OPTIONS preflight. The preflight is never checked as a write, and `OnRequest` runs for it.

## A route has no OPTIONS

`route.go` takes `GET`, `POST`, `PUT`, `PATCH` and `DELETE` ([Conventions](../reference/conventions.md)). A function named `OPTIONS` in it is not a route method: `tifa routes` lists only `GET` and `POST` for such a route, and `tifa check` does not warn about the function. Without a hook an OPTIONS request gets 405 and `Allow: GET, HEAD, POST`. So answer the preflight in `OnRequest`, which sees every request first.

## The hook

`cors.go` in the root package, and the option with the allowed origins:

```go
// CORSOrigins are the origins of other sites whose pages may call the API under /api/, such as
// https://app.example.com. Empty, the default, allows none.
var CORSOrigins = tifa.Strings("cors.origins", nil, "Origins of other sites that may call /api/ from a browser")

```

```go
package cors

import (
	"net/http"
	"slices"
	"strings"

	"github.com/liustack/tifa"
)

// corsPaths is the part of the site that other origins may call.
const corsPaths = "/api/"

// allowCORS answers the preflight of an API request, and adds the CORS headers to the answer of
// the others. It returns true when it wrote the answer. OnRequest calls it first.
func allowCORS(c *tifa.Ctx) (bool, error) {
	r := c.Request()
	origin := r.Header.Get("Origin")
	if origin == "" || !strings.HasPrefix(c.Path(), corsPaths) {
		return false, nil
	}
	// The answer depends on Origin, so a cache must keep one copy for each.
	c.Writer().Header().Add("Vary", "Origin")
	if !slices.Contains(CORSOrigins, origin) {
		return false, nil // no CORS headers: the browser does not let the page read the answer
	}
	c.SetHeader("Access-Control-Allow-Origin", origin)
	if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
		// A preflight: the browser asks before it sends a request that is not simple.
		c.SetHeader("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE")
		c.SetHeader("Access-Control-Allow-Headers", "Content-Type, Authorization")
		c.SetHeader("Access-Control-Max-Age", "600")
		return true, c.NoContent(http.StatusNoContent)
	}
	return false, nil
}

```

```go
package cors

import (
	"context"

	"github.com/liustack/tifa"
)

func OnStart(ctx context.Context) error { return nil }

func OnRequest(c *tifa.Ctx, next func(*tifa.Ctx) error) error {
	if done, err := allowCORS(c); done {
		return err
	}
	return next(c)
}

```

- **A list of origins, compared as text.** The browser sends `Origin` as scheme, host and port, and the list holds the same text. Never echo whatever `Origin` arrives: that allows every site. A page of an origin that is not in the list gets no CORS headers, so the browser does not let it read the answer.
- **`Vary: Origin`**, added for every call that has an `Origin`, so a cache never gives the answer made for one origin to another. It is added, not set, to keep the `Vary: Accept-Encoding` of Tifa.
- **Only `/api/`.** The pages and the navigation data of the app are not for other origins.
- **The preflight is answered with 204** and does not reach the route, so nothing in `Validate` or a `Load` runs for it. `Access-Control-Allow-Headers` lists the headers the page may send beyond the simple ones: `Content-Type` for JSON, `Authorization` for a token. `Access-Control-Max-Age` lets the browser keep the answer for ten minutes.
- **No credentials.** The code does not send `Access-Control-Allow-Credentials`, so a browser neither sends cookies with these calls nor lets the page read an answer made for cookies. The session cookie of Tifa is `SameSite=Lax` ([The cookie](../pages-and-data/sessions.md#the-cookie)) and is not what a page of another site should rely on. Give such a client a token in `Authorization` and check it in Go. Calls with cookies from another origin were not tried here.

For a public read-only API with no cookies and no tokens, `Access-Control-Allow-Origin: *` is the other choice, and then the list is not needed. It was not tried here either.

## Allow the writes

For the origins in `cors.origins` that may write, list the same origins in `server.trusted_origins`:

```yaml
# tifa.yaml
server:
  trusted_origins: "https://app.example.com"
```

Trust only sites you control, since a page of a trusted origin can act for your users. If the API takes a bearer token and no cookie, and any site may call it, `server.cross_site_paths` with the pattern of that path lets every origin write to it, which is safe only when nothing there relies on a cookie (not tried here). The two settings, and the exact way to write an origin, are in [Cross-site writes](../build-and-deploy/headers-and-origins.md#cross-site-writes).

## Test it

`app/api/items/cors_test.go` starts the app with `tifatest.Start` and checks that:

- a preflight from the listed origin gets 204 with the origin, the methods and `Vary: Origin`
- the same from another origin gets 405 and no CORS header, and so does a path outside `/api/`
- a GET from the listed origin carries `Access-Control-Allow-Origin`
- a cross-site POST gets 403 until the origin is in `server.trusted_origins`

With curl against the built program:

```sh
curl -i -X OPTIONS http://127.0.0.1:9527/api/items \
  -H 'Origin: https://app.example.com' -H 'Access-Control-Request-Method: POST'
```

The answer is `204` with the `Access-Control-Allow-*` headers. Leave out `Origin` and you get the 405 of any OPTIONS.
