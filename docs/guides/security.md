---
summary: 'What Tifa guards by default (cross-site writes, session cookie, limits, headers) and what you must do'
audience: app
order: 12
read_when:
  - Going live and asking what Tifa already protects and what is left to you
  - Checking an app, or code a coding agent wrote, for security holes
  - Choosing who may see what, or where a secret may live
  - A request is answered with 403, 413 or 408 and you want to know which check did it
---

# Security

Tifa turns on the checks it can make without knowing your business, and leaves the rest to you. This page lists both halves, each with the setting that changes it and the doc with the details.

## What Tifa does for you

### Writes from other sites are refused

A POST, PUT, PATCH or DELETE that a browser sends from another site gets a 403 before `OnRequest` and before any handler runs. A page on another site cannot make a browser post a form to your app with your visitor's cookies. It is always on. A request that has no `Sec-Fetch-Site` and no `Origin` header, such as curl or a webhook, is not a browser and passes. GET, HEAD and OPTIONS are never checked, so a GET must not change anything: sign out with a POST.

To let a site you control write, list it in `server.trusted_origins`. For a provider's callback, `server.cross_site_paths` skips the check for a path, and then your handler must prove the request is real. See [Cross-site writes](../build-and-deploy/config.md#cross-site-writes).

### The session cookie is hard to steal and to forge

`tifa.SetSession` makes a random token of 32 bytes. The cookie holds only that token, and the store keeps the record under its SHA-256. The cookie is `__Host-tifa_session` with `HttpOnly`, `Secure`, `SameSite=Lax`, `Path=/` and no `Domain`, and it lasts 30 days. Every sign-in makes a new token and deletes the old session, so a token an attacker planted is never kept. Change the name and the lifetime with `session.cookie` and `session.max_age`. See [The cookie](../pages-and-data/sessions.md#the-cookie).

Only what the session type's `Exposed()` method returns goes to the browser. It is written into the page and kept in `localStorage`, so it must hold nothing secret. Without an `Exposed` method nothing of the session goes there. See [Checks at build time](../pages-and-data/sessions.md#checks-at-build-time).

### Requests have a size and a time limit

A request body may be 1 MiB (`server.body_limit`), and a bigger one gets 413. A whole request has 10 seconds to arrive (`server.read_timeout`), and a slower one fails with 408. The request header must arrive within 5 seconds, which is no setting. A route that takes uploads raises its own limit with `c.SetBodyLimit` and `c.SetReadTimeout` in `OnRequest`. See [Body limit and read time of a route](../pages-and-data/handlers.md#body-limit-and-read-time-of-a-route).

### Every answer carries three headers

| Header | Value |
| --- | --- |
| `X-Content-Type-Options` | `nosniff` |
| `Referrer-Policy` | `strict-origin-when-cross-origin` |
| `Content-Security-Policy` | `frame-ancestors 'self'`, so other sites cannot show a page in a frame |

Pages, JSON, errors and static files all carry them. They are set before `OnRequest`, so `OnRequest` can change them for a path, such as pages a partner shows in a frame. See [Security headers](../build-and-deploy/config.md#security-headers).

### Data in a page cannot close its script

What `Load` returns is written into the page's HTML. Tifa escapes `<`, `>`, `&` and two line separators there, so no text of your data can end the script element that carries it. See [JSON rules](../reference/json.md).

### Pages with `Cache` are anonymous

A page with `const Cache` may be kept by a CDN and shown to everyone. Tifa removes `Cookie`, `Authorization`, `Accept-Language`, `User-Agent` and every other request header but a short list before `OnRequest` sees the request, `tifa.Session` is nil, and a `Set-Cookie` on the HTML is removed. The build fails when a `Load` or `Layout` of such a page reads a cookie, a header or the session. So one visitor's data cannot leak into a shared page by accident. See [Cache for CDNs](../pages-and-data/ssr-and-cache.md#cache-for-cdns).

### Error details stay in the log

An error that `Load`, a handler or a `Validate` method returns, other than `tifa.Error`, `tifa.Fields` and `tifa.ErrNotFound`, is logged and answered with a 500 and the plain status text. The visitor never sees its text. What `OnError` returns is what visitors see. The text of a `tifa.Error` is public, so put nothing internal in it. A built app does not show the error behind a failed page either, and only `tifa dev` does. See [Errors and 404](../pages-and-data/errors.md#what-an-error-you-return-becomes).

### Every way in is a folder

Code runs for a request only as the `Load`, `Layout` or method handler of a `route.go`. There is no function the browser calls by name. `tifa routes` prints every route and every address Tifa answers on its own, such as `/sitemap.xml`, and `OnRequest` is the one place every request to a route passes: pages, navigation data, API routes and unmatched URLs. It does not run for static files or for a write refused as cross-site. See [Philosophy](../start/philosophy.md#one-address-for-each-write) and [Hooks](../pages-and-data/hooks.md#hooksgo).

### Building needs no secrets

`tifa build` reads your code and never runs it, so building, in CI too, needs no database and no secret. `.env` is not put into the program, and neither is any file or folder of `public/` whose name starts with a dot, such as `.DS_Store`. The exception is `.well-known`. To copy one anyway, list it in `build.public_dotfiles`. A `tifa.Secret` option cannot be set with a flag, since a flag shows in the process list, and `./bin/app config` leaves its value out. See [Options of the app](../build-and-deploy/config.md#options-of-the-app).

## What you do

### Decide who may do what

Tifa stores the session. It does not know your users, roles or owners. Check access in `OnRequest`, or at the top of a handler or `Load`, by reading `tifa.Session`. A guard in `OnRequest` that covers an area asks which route matched, with `c.Pattern()`, and never compares the text of the URL, which has many spellings for one route (`/%61ccount` is `/account`). Check it on the server for every handler: what `useSession` and `SignedIn` show decides only what the browser displays. A check on the owner of a thing (is this order theirs?) belongs in the handler that reads or changes it. The `Layout` function of the root Layout runs for every URL, the 404 page and the sign-in page included, so do not let it return data that only signed-in visitors may see, or read the session in it first. [Sign in and sign out](sign-in.md#the-guard) shows a guard.

Keep passwords as a slow hash, such as argon2id, and say the same thing for an unknown e-mail and a wrong password. The same guide shows how.

### Turn on HTTPS and HSTS

The program serves HTTPS itself with a certificate from Let's Encrypt: set `server.tls_domains`. `tifa deploy` with `front: none` does this for you, `front: cloudflare` puts a Cloudflare Tunnel in front, and `front: nginx` an nginx that Tifa installs, with a certificate from certbot. Once HTTPS works, set `server.hsts: true`, which is off by default, so browsers keep to HTTPS for a year. See [HTTPS](../build-and-deploy/config.md#https), [Port 80 and HSTS](../build-and-deploy/config.md#port-80-and-hsts) and [The app serves visitors itself](../build-and-deploy/deploy.md#the-app-serves-visitors-itself).

### Turn on a Content-Security-Policy

With `server.csp: on`, scripts load only from your site, and a script that gets into a page through data does not run. It is off by default. Try it first with `server.csp: report-only`. Tifa adds the hashes of its own inline scripts to the policy of each page. Leave `'strict-dynamic'` out of it, or pages stop hydrating. See [Content-Security-Policy](../build-and-deploy/config.md#content-security-policy).

### Keep secrets out of what `Load` returns

Everything a `Load` or `Layout` returns is in the page, and every visitor can read it. Never return a `tifa.Secret`, a token or a password. Keep secrets in the environment or `.env` on the server, and `.env` out of git. Vite does not read `.env`, but a variable that starts with `VITE_` in the shell that runs `tifa build` goes into the browser bundle. See [Values from the environment](environment.md).

### Clean HTML that visitors write

`tifa.HTML` is shown without escaping. Use it for HTML you wrote or made yourself, such as Markdown from your own `content/` folder. HTML from visitors must go through a sanitizer first. See [Showing HTML the app trusts](../pages-and-data/loading-data.md#showing-html-the-app-trusts).

### Check uploads

The body limit caps one request. The name, the size and the `Content-Type` of an uploaded file come from the client. Decide what a file is from its bytes and accept a short list. Make the file name yourself and never join a client's name into a path. Serve files with `X-Content-Type-Options: nosniff`, and check `tifa.Session` before serving a file that only its owner may see. See [Uploads](uploads.md).

### Keep redirects on your site

A sign-in page that sends the visitor on to `?next=...` is an open door to any site an attacker names, unless it checks the address. Tifa does not check it for you. Keep only a path of your own site, as `safeNext` does in [Sign in and sign out](sign-in.md#the-sign-in-route).

### Slow down guessing and floods

Tifa has no rate limiter. `OnRequest` sees every request with its method and path, so it is the place to limit by address. With `deploy.front: none`, the address is the host part of `c.Request().RemoteAddr`. Behind a proxy, `RemoteAddr` is the proxy, so read the one header your own proxy sets, and never the leftmost value of `X-Forwarded-For`, which the visitor writes. See [The client address](../build-and-deploy/deploy.md#behind-a-cdn-or-proxy). Slow down wrong passwords too, as the sign-in guide shows. If visitors who are not signed in may start a session, limit that at the entry, since a flood pushes real sessions out of the memory store. See [Sessions](../pages-and-data/sessions.md#where-to-store-sessions).

Limit public pages the same way. A query parameter that no `Load` or Layout declares is dropped before the memo key, so any number of values for it hit the same entry and cost nothing. A declared parameter whose values have no bound, such as a search word `q` or a page number `page`, is another matter: each new value runs the `Load` and takes a render on a VM, and it may push other pages out of the memo. Put the limit in `OnRequest`. See [Query parameters](../reference/memo.md#query-parameters).

### Write queries with parameters

Tifa has no database layer. Your app uses `database/sql`, so pass values as arguments and never build SQL by joining text. See [Keeping data in SQLite](database.md).

### Guard what Tifa does not reach

- `tifa.SetHTTPHandler` wraps everything, the cross-site check too. A path your wrapper answers itself, such as `/debug/pprof/`, has no `OnRequest` and no check. Put it behind your own check or on another listener. See [Setting up in OnStart](../pages-and-data/hooks.md#setting-up-in-onstart).
- Cookies you set with `c.SetCookie` go out as you write them. Set `HttpOnly`, `Secure` and `SameSite` yourself.
- Compression can leak a secret over HTTPS when an answer repeats something from the request and also holds a secret. See [Compression](../build-and-deploy/build.md#compression).
- `tifa dev` shows internal errors and listens on `127.0.0.1`. Do not open it to a network you do not trust. See [When a page fails to render](../build-and-deploy/dev.md#when-a-page-fails-to-render).

### Keep the program and its packages current

Fixes for Tifa land in the latest release, and `go tool tifa upgrade` moves your app to it (see [Upgrading Tifa](../build-and-deploy/build.md#upgrading-tifa)). Tifa does not check your Go modules or npm packages for known holes, so update them on your own schedule.

`net/http` is compiled into your program, so a Go security release reaches the app only when you build and deploy it again. The CI that `tifa new` writes installs the newest patch of the Go minor in `go.mod` and runs `govulncheck`. The workflow of `tifa deploy` installs the newest patch too, so a fix is in the next deploy.

To report a hole in Tifa itself, open a [private security advisory](https://github.com/liustack/tifa/security/advisories/new) and not a public issue.
