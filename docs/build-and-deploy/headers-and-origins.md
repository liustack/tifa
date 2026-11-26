---
summary: 'Set security headers, a Content-Security-Policy and allow cross-site writes: `server.csp`, framing, `cross-origin request detected`'
audience: app
order: 20
read_when:
  - A POST, PUT, PATCH or DELETE gets 403 with `cross-origin request detected`, or a page of another site or a provider's callback must be allowed to send one
  - A page must be shown in a frame on another site, or a header such as Referrer-Policy or Content-Security-Policy needs another value
  - Turning on a Content-Security-Policy, or a script or style of a page is blocked by one
---

# Headers and cross-site writes

Tifa sends security headers and refuses writes that come from another site. This page covers how to change them.

## Security headers

Every answer of the program carries three headers, pages, JSON, errors and public files alike. None of them changes what a page loads or how it works:

| Header | Value | What it does |
| --- | --- | --- |
| `X-Content-Type-Options` | `nosniff` | The browser takes the `Content-Type` as it is, so a file that is no script never runs as one |
| `Referrer-Policy` | `strict-origin-when-cross-origin` | A link to another site tells it the origin, not the path and query, which may hold a token. Current browsers do this anyway |
| `Content-Security-Policy` | `frame-ancestors 'self'` | Only pages of the same origin may show a page in a frame, so another site cannot show it under its own buttons |

They are set before `OnRequest`, so change or remove them there for the paths that need something else, such as pages a partner shows in a frame. Static files do not pass through `OnRequest` and always carry them.

```go
func OnRequest(c *tifa.Ctx, next func(*tifa.Ctx) error) error {
	if strings.HasPrefix(c.Request().URL.Path, "/embed/") {
		c.SetHeader("Content-Security-Policy", "frame-ancestors https://partner.example")
	}
	return next(c)
}
```

`Strict-Transport-Security` is not among them: it is `server.hsts`, see [Port 80 and HSTS](https.md#port-80-and-hsts). A proxy in front passes the headers on, so do not add them there again.

### Content-Security-Policy

A Content-Security-Policy tells the browser which scripts may run. With one that allows only the files of the site, a script that gets into a page through data, such as a comment that carries `<script>`, does not run. Turn it on with `server.csp: on`, or try it first with `server.csp: report-only`, which only reports what it would block in the browser console (and to the address of `report-uri` or `report-to`, when the policy has one).

The default policy, `server.csp_policy`, is `script-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'self'`:

- scripts load only from the site itself, and an inline script runs only when the policy lists its hash
- no plugins (`<object>`, `<embed>`), and no `<base>` that sends relative URLs elsewhere
- only pages of the same origin show the pages in a frame

Styles are left alone: React writes `style` attributes, and emotion and MUI add style elements while the page runs.

Every page has inline scripts of Tifa's own: the router hands the data of the Loads to the browser in one and restores the scroll position with another. Tifa hashes each inline script and style it writes into a page (SHA-256) and adds the hashes to the policy in the header of that page, so they run. These are the scripts and styles of the head of the build (Vite's preamble under `tifa dev`), of the session head, of the router, and of the `scripts` and `styles` of `head` functions, and under `tifa dev` its own error overlay. The same page always gets the same header, so the render memo and ETags work as before. Other answers, JSON and files, carry the policy as it is.

The hashes go into `script-src` and `script-src-elem`, and into `style-src` and `style-src-elem` when the policy has them. A policy with only `default-src` gets a `script-src` with the sources of `default-src` and the hashes. A directive whose `'unsafe-inline'` still lets every inline script run gets no hashes, since browsers ignore `'unsafe-inline'` once a directive lists one. With a hash, a nonce or, for scripts, `'strict-dynamic'` in the directive, browsers already ignore it, so the hashes go in.

Leave `'strict-dynamic'` out of the script directives. Browsers then ignore `'self'` and host sources, so they block the script files of the pages (the entry of the build and its preloads), and pages do not hydrate.

A policy of your own, with an analytics host and a report address:

```yaml
server:
  csp: on
  csp_policy: "script-src 'self' https://plausible.io; object-src 'none'; base-uri 'none'; frame-ancestors 'self'; report-uri /csp-reports"
```

A policy that `OnRequest`, a `Load` or a `Layout` sets on an answer goes out as they set it, without hashes. The 304 of a page carries no policy: the browser keeps the one that came with its copy, which has the hashes of that copy.

An inline script of `head` that first appears after a navigation in the browser, and one of a page with `SSR = false`, is not in the policy of the document that is open, so the browser blocks it.

## Cross-site writes

A program refuses a POST, PUT, PATCH or DELETE that a browser sends from another site. A page of `evil.test` can make a browser send a form or a multipart body to your app with the user's cookies, and a handler that trusts the cookie would act for the user. The check runs first, before `OnRequest` and before any route, for pages, API routes and URLs that match nothing. It is on for every app, and no setting turns it off. It is Go's `http.CrossOriginProtection`:

- A request with `Sec-Fetch-Site: same-origin` passes, and so does `none` (the user typed the address). `same-site`, which is another subdomain of the same domain, and `cross-site` are refused.
- A browser that sends no `Sec-Fetch-Site` (all current ones do) is judged by its `Origin`, which must name the `Host` of the request.
- A request with neither header does not come from a browser, such as curl, a webhook or another server, and passes.
- GET, HEAD and OPTIONS are never checked, so a GET must not change anything.

A refused request gets 403 and `{"error":"cross-origin request detected from Sec-Fetch-Site header"}` (or the same with `Sec-Fetch-Site is missing, and Origin does not match Host`), with `Cache-Control: no-store`. `OnRequest` does not run for it, so a log or a counter there does not see it. The server logs one line for it with the method, the path, the reason and the `Origin`, `Sec-Fetch-Site` and `Host` it read, which tells a proxy that rewrites `Host` from a real attack. Calls from your own pages, with the functions of `route.gen` or `fetch`, and `router.invalidate()` are same-origin and unaffected. So is `tifa dev`, where the browser talks to one address.

Behind a proxy, let `Host` and `Origin` through as the browser sent them. A modern browser is judged by `Sec-Fetch-Site`, which proxies pass on, but an old one is judged by `Origin` against `Host`, so a proxy that rewrites `Host` makes its writes look foreign.

To allow some:

- `server.trusted_origins` lists origins whose pages may write, such as a separate admin site or a front end on `http://localhost:3000` during development. An origin is a scheme, a host and, if it is not the default of the scheme, a port, with no path: `https://admin.example.com`. It is compared as text with the `Origin` header, so `http://` and `https://` are different origins. Write it as a browser sends it: the program lowers ASCII letters and drops `:80` and `:443`, and refuses any other way of writing the host, naming what to write. That is a domain name in ASCII, its letters outside ASCII written as `xn--` (`https://xn--bcher-kva.example` for `bücher.example`), an IPv4 address as four decimal numbers, an IPv6 address in its shortest form, and a port without leading zeros. Trust only sites you control, since a page of a trusted origin can act for your users.
- `server.cross_site_paths` lists patterns, in the syntax of `http.ServeMux`, such as `POST /auth/apple/callback` or `/hooks/{name}`, whose requests are not checked at all. Use it for a provider's browser callback, such as a sign-in that posts a form from the provider's page. A pattern without a method takes every method. A request that ServeMux would only redirect to the pattern, such as one with a final slash, is not let through. A handler on such a path has to prove the request is real itself, for example with the `state` it put in the cookie or the signature of the provider. See [Sign in with Google or GitHub](../guides/oauth.md#a-provider-that-posts-the-callback).

A page of another origin that calls your API from the browser needs CORS headers as well, which Tifa does not send: [Calls from another site](../guides/cors.md) shows the `OnRequest` that answers the preflight and how it goes with `server.trusted_origins`.
