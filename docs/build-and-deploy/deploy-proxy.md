---
summary: 'Put nginx, a load balancer, a CDN or a reverse proxy in front of a Tifa app: `deploy.front: nginx` and `external`, the CDN settings and cross-origin 403s behind a proxy'
audience: app
order: 14
read_when:
  - Putting several Tifa apps on one server without Cloudflare, with `deploy.front: nginx`, or setup stopped because the server has an nginx that Tifa did not install
  - Putting a load balancer of your provider or a proxy of your own in front of a Tifa app, with `deploy.front: external`, and entering the targets and the health check in it
  - Putting a CDN or a reverse proxy such as nginx, Cloudflare or CloudFront in front of a Tifa app
  - A POST gets 403 with `cross-origin request detected` only behind the proxy
---

# Put nginx or a CDN in front

Visitors can reach the program through nginx that `tifa deploy setup` installs, through a load balancer you set up at your provider, or through a proxy or CDN of your own. This page covers all three.

## Behind nginx

With `front: nginx`, an nginx that Tifa installs serves the visitors of every app of the server that has this front, so they share ports 80 and 443 without Cloudflare. Each app listens on a loopback port, as it does behind the Cloudflare Tunnel, and nginx sends a request to the app whose host it names, the host of its `site.url`.

- **The nginx is Tifa's.** Setup installs it with apt, as a line of the plan you agree to, and writes `/etc/tifa/nginx` to mark it. The file lists the packages apt added that were not there before, so remove deletes those and nothing else: a package that apt only upgraded, or a certbot that was already on the server, stays. From then on Tifa writes all the configuration of this nginx, one file for each app (`/etc/nginx/conf.d/tifa-<app>.conf`), and nothing else uses it. Do not edit those files, setup writes them again when they differ. Keep `/etc/tifa/nginx`: without it, setup takes the nginx for one of yours and stops. It does the same for an nginx package the file never recorded, such as one you installed after a setup that stopped halfway.
- **An nginx of yours is never touched.** When the server has an nginx that Tifa did not install, setup stops, names it and the port it listens on, and changes nothing. It stops the same way when another program, or an app of Tifa that serves visitors itself, holds port 80 or 443. Remove that nginx, stop that program, or use `front: cloudflare`.
- **With a domain**, the host of `site.url` such as `https://shop.example.com`, setup also installs certbot and its nginx plugin with apt, and gets a certificate from Let's Encrypt, named `tifa-shop`. Port 80 answers the checks of Let's Encrypt and sends everything else to HTTPS with a 308. The timer of certbot renews the certificate, and a hook reloads nginx. DNS must point the domain at the server before setup: until then certbot is refused, setup says so, and the site stays on plain HTTP. Run setup again when DNS is right.
- **Without a domain**: no `site.url`, or its host is an IP address or `localhost`. The app answers plain HTTP on port 80, and gets every request that names no other app. Only one app of a server can have that: when another has it already, setup stops and says so. Give one of the two a domain.
- **HSTS** is as with `front: none`: off, unless you set `server.hsts: true`. The program sends the header and nginx passes it on.
- **Each change is checked first.** Setup writes a site, runs `nginx -t`, and only then reloads nginx. A reload keeps the connections of the other apps. When `nginx -t` or the reload fails, the file from before is put back, and the error says what nginx said. A setup run again also starts nginx when it is not running. Changes of the site files and of the packages take one lock on the server, `/etc/tifa/nginx.lock`, so two setups or removes of different apps never write at the same time.

The site of an app with a domain, once it has its certificate:

```nginx
map $http_upgrade $tifa_connection_8d9001d32c6a {
	default upgrade;
	'' '';
}
upstream tifa-shop {
	server 127.0.0.1:9527;
	keepalive 16;
	keepalive_timeout 50s;
}
server {
	listen 80;
	listen [::]:80;
	server_name shop.example.com;
	location / {
		return 308 https://shop.example.com$request_uri;
	}
}
server {
	listen 443 ssl http2;
	listen [::]:443 ssl http2;
	server_name shop.example.com;
	ssl_certificate /etc/letsencrypt/live/tifa-shop/fullchain.pem;
	ssl_certificate_key /etc/letsencrypt/live/tifa-shop/privkey.pem;
	ssl_protocols TLSv1.2 TLSv1.3;
	location / {
		proxy_pass http://tifa-shop;
		proxy_http_version 1.1;
		proxy_set_header Upgrade $http_upgrade;
		proxy_set_header Connection $tifa_connection_8d9001d32c6a;
		proxy_set_header Host $http_host;
		proxy_set_header X-Forwarded-For $remote_addr;
		proxy_read_timeout 1d;
		proxy_send_timeout 1d;
		proxy_request_buffering off;
		client_max_body_size 0;
		gzip off;
	}
}
```

Without a domain there is one server, with `listen 80 default_server;`. A server without IPv6 gets no `[::]` lines. What the site does, as [Behind a CDN or proxy](#behind-a-cdn-or-proxy) asks:

- `Host` and `Origin` reach the app as the browser sent them, so the check of [cross-site writes](headers-and-origins.md#cross-site-writes) works. `X-Forwarded-For` is the address of the visitor. nginx sets it and never appends to what the visitor sent, so it cannot be made up.
- nginx waits up to a day for the app, so it never ends a request the app allows to run: the timeouts of the program decide ([Long requests](tuning.md#long-requests)).
- A request body goes to the app as it arrives, and nginx limits nothing and writes nothing to its disk: `server.body_limit`, or the limit a route sets, answers with 413.
- An answer with `X-Accel-Buffering: no`, such as server-sent events, is passed on at once, as nginx does when it is not told to ignore that header.
- A WebSocket reaches the app: `Upgrade` passes, and `Connection` is `upgrade` when the browser asks for one and empty otherwise, which keeps the connections to the app open for the next request. The `map` that picks it has a name of its own in each site, since nginx takes each name once. A site written before Tifa did this gets it when you run setup again.
- nginx does not compress (`gzip off`), so what the program decides about compression holds, `No-Gzip-Compression` included. Leave `server.compress` on.
- The connections to the app are kept open for 50 seconds when idle, shorter than the 60 seconds after which the program closes an idle connection.

**ufw.** When ufw is on and does not let visitors reach ports 80 and 443, the plan allows both, in rules with the comment `tifa nginx front`, before nginx asks for a certificate. A server set up with an earlier Tifa has them with the comment `tifa nginx`, and setup takes them over without closing the ports. Remove deletes them with the last app of the nginx. Everything under **ufw** and **The firewall of your provider is yours** above holds here too.

**Moves.** Setup plans a move between `none`, `cloudflare` and `nginx` like any other change. An app that moves to nginx first moves to a loopback port, which frees port 80 for nginx, so visitors wait for a moment. An app that moves away takes its site and certificate out of nginx, and nginx and certbot with it when no other app has a site. An app cannot move to `front: none` while other apps are behind nginx: nginx holds the ports, and setup stops and says so.

`tifa deploy remove` takes the site and the certificate of the app out of nginx, and with the last app uninstalls nginx, and certbot when setup installed it, with their configuration, and deletes `/etc/tifa/nginx`. When setup only added the nginx plugin to a certbot that was there, remove takes the plugin and leaves certbot, its timer and your certificates alone. A site that nginx will not let go stays, and so do its certificate and nginx: the error says what nginx said, and the next remove goes on. The other apps keep answering. To take nginx off Tifa by hand, run `apt-get purge` on the packages named in `/etc/tifa/nginx`, delete `/etc/nginx`, `/etc/letsencrypt` and that file, and delete the ufw rules with the comment `tifa nginx front`.

## Behind your load balancer

With `front: external`, something of yours in front of the servers sends visitors to the app: most often a load balancer you set up at your provider, such as an AWS Application Load Balancer, or else a proxy of your own. It ends HTTPS with its own certificate and sends plain HTTP to the port of the app on each server. Tifa only deploys: it sets up no balancer, calls no API of your provider, and installs no nginx or certificate. The app answers plain HTTP and does not redirect to HTTPS. It works with one server and is the front for [several servers](deploy-several-servers.md).

```yaml
site:
  url: https://shop.example.com
deploy:
  front: external
```

**The port.** The app listens on its own port, from 9527 up, on every address of the server, not only on the loopback as behind the tunnel or nginx. With several servers it has the same port on all of them, which setup picks free on each. The plan names it: `port 9527 on every address of the server, for the load balancer or proxy in front`.

To choose the port, set `deploy.port`, from 80 to 65535. Some proxies reach only certain ports of the server, Cloudflare among them ([its ports](https://developers.cloudflare.com/fundamentals/reference/network-ports/)):

```yaml
deploy:
  front: external
  port: 8080
```

Setup stops before it changes anything when another app or program listens on that port on a server, and names it. On port 80 or 443 the app still serves plain HTTP, with no certificate. A port below 1024 needs root once per server: the plan lets the user `tifa` open ports from 80, as it does for an app that serves visitors itself, and `tifa deploy remove` of the last app on such a port puts the setting back. Changing `deploy.port` moves the app at the next setup: the plan says so, and the app restarts on the new port and passes its health check. A deploy alone never moves it, and stops with a line that says to run setup.

**What to enter in the balancer.** Setup and each deploy end with the targets:

```text
load balancer or proxy: send plain HTTP to these targets, and check each with GET /healthz, which a status from 200 to 499 passes (a balancer that takes one status wants 200, so give the app a /healthz route):
  203.0.113.7:9527         vps
  203.0.113.8:9527         vps2
```

- **Targets**: the address of each server and the port of the app. When the balancer and the servers share a private network, use the private addresses instead.
- **Protocol to the targets**: HTTP. The balancer ends HTTPS.
- **Health check**: `GET /healthz` on the same port. Write [a health check](deploy-by-hand.md#health-check) that answers 204 while the app can serve. An app without that route answers 404, which `tifa deploy` counts as up, but a balancer that wants 200 marks every server down. Leave the balancer's PROXY protocol off: the app reads plain HTTP.

**Headers.** `tifa deploy` sets none: the balancer does, and the app reads them.

- Keep `Host` as the browser sent it. The program compares `Host` and `Origin` to refuse writes from other sites, so a balancer that rewrites `Host` makes your own forms fail with 403 (see [Cross-site writes](headers-and-origins.md#cross-site-writes)).
- `X-Forwarded-For` and `X-Forwarded-Proto` are what the balancer says about the visitor. Tifa does not read the address, and reads `X-Forwarded-Proto` only to not warn about a sign-in over plain HTTP. `c.Request().RemoteAddr` is the address of the balancer. To log or limit by visitor, read the header in `OnRequest`, and only from the last entry the balancer added: what is before it came from the visitor.
- The rest of [Behind a CDN or proxy](#behind-a-cdn-or-proxy) holds too: compression, caching, `No-Gzip-Compression`.

**The firewall.** The port answers anyone who can reach it, over plain HTTP, past the balancer. Setup opens it nowhere, neither in ufw nor at your provider, and the plan says so. Let only the balancer reach the port: in the security group or firewall of your provider, or with `sudo ufw allow from <addresses of the balancer> to any port 9527 proto tcp` when ufw is on. When the port answers from your own machine, setup and deploy say so after the targets.

**During a deploy** the socket of the app keeps the connections while the new program starts, so a health check of the balancer waits instead of failing. It fails only when the start takes longer than the timeout of the check, and the balancer takes a server out only after several failed checks in a row. Keep the start under the timeout of the check, 5 seconds at AWS and Google Cloud unless you change it.

### Balancer settings

Checked against the vendors' documentation on 2026-11-27. Names change, so open the linked page if a field is missing.

**AWS Application Load Balancer**

- A target group with target type `instance` or `ip`, protocol HTTP, and the port of the app. A listener on HTTPS 443 with your certificate forwards to it.
- Health check: protocol HTTP, path `/healthz`, port "traffic port". Success codes ("Matcher") take a range, so `200-499` matches `tifa deploy`, and the default is `200`. Timeout 5 seconds, two failures in a row take a target out, by default ([target group health checks](https://docs.aws.amazon.com/elasticloadbalancing/latest/application/target-group-health-checks.html)).
- In the security group of the servers, allow the port of the app from the security group of the balancer only.

**Google Cloud**

- An external Application Load Balancer with a backend service on an instance group (or a zonal NEG) of the servers, protocol HTTP, and the named port set to the port of the app.
- Health check: HTTP, request path `/healthz`, the serving port. It passes only on 200, so the app needs a `/healthz` route that answers 200: a redirect or a 204 counts as down. Interval and timeout are 5 seconds, two failures in a row take a server out ([health checks](https://docs.cloud.google.com/load-balancing/docs/health-check-concepts)).
- Allow the port of the app in the VPC firewall from the ranges of the load balancer and its health checks, which that page lists, and from nothing else.

**DigitalOcean**

- A forwarding rule from HTTPS 443, with your certificate, to HTTP on the port of the app.
- Health check: protocol HTTP, the port of the app (the default is 80), path `/healthz`. A status from 200 to 399 passes ([manage load balancers](https://docs.digitalocean.com/products/networking/load-balancers/how-to/manage/)).
- The balancer reaches Droplets in its VPC on their private addresses. Allow the port of the app in the Cloud Firewall of the Droplets from the balancer only.

**Hetzner Cloud**

- A service with protocol HTTPS and a certificate, source port 443, and the port of the app as destination port. Its health check: HTTP, the port of the app, path `/healthz`.
- Put the balancer and the servers in one Network to add the servers by their private addresses ([creating a load balancer](https://docs.hetzner.com/cloud/load-balancers/getting-started/creating-a-load-balancer/)). Allow the port of the app in the firewall of the servers from that Network only.

## Behind a CDN or proxy

- **With `front: nginx`**, Tifa writes these settings for you (see [Behind nginx](#behind-nginx)). The rest of this section is for a proxy or CDN of your own.
- **Host and Origin.** Pass `Host` and `Origin` through as the browser sent them. The program refuses a write that a browser sends from another site by comparing them, and a proxy that rewrites `Host` makes your own forms look foreign (403 with `cross-origin request detected`). See [Cross-site writes](headers-and-origins.md#cross-site-writes).
- **The client address.** Tifa does not read `Forwarded` or `X-Forwarded-For`. `c.Request().RemoteAddr` is the address of the proxy. If `OnRequest` needs the visitor's address, read the header your own proxy sets, and only when the request really comes from that proxy. The nginx of `front: nginx` sets `X-Forwarded-For` to the address of the visitor.
- **Compression.** The program compresses text answers itself. When the CDN or proxy compresses (brotli, for example), set `server.compress` to `false`. CloudFront, Cloudflare and Google Cloud CDN all leave an answer that already has a `Content-Encoding` alone. See [Compression](../reference/server-program.md#compression).
- **Caching.** Pages, navigation data and handler answers are `private, no-cache` by default, so a CDN does not store them. Keep it that way. Let the CDN cache these:
  - `/assets/`: content-hashed, `public, max-age=31536000, immutable`.
  - The files in `public/`: `no-cache` with an ETag, so the CDN asks and gets a 304. They keep fixed names, so only files that need one belong there. Import the rest to get `/assets/` (see [Static files](../start/app.md#static-files)).
  - Pages with `Cache`: they are anonymous and carry a `public` `Cache-Control` with an `s-maxage` (the exact value is in [Cache-Control of an answer](../pages-and-data/ssr-and-cache.md#cache-control-of-an-answer)). A CDN that follows the origin's `Cache-Control` keeps a page for the time of `Cache` and no longer. See [Cache for CDNs](../pages-and-data/ssr-and-cache.md#cache-for-cdns).
- **Do not make the CDN ignore the origin's cache headers.** Use a cache setting that follows them, and never one that forces a time or caches everything. The settings for each CDN are in [CDN settings](#cdn-settings).
- **The navigation data of a page with `Cache` is not shared.** It is the page URL plus `?_tifa=...`, it carries the session of the browser, and it is always `private, no-cache`. The cache key must include the query string (at least `_tifa`), or the CDN must skip the cache for requests with `_tifa`. Drop cookies by path only for the HTML of the page, never for the same path with `_tifa`.
- **Do not cache `/__tifa/session`.** It answers per visitor and is `private, no-store`.
- **Do not set `Cache-Control: public` yourself** on other pages, or on a handler that answers the URL of a page. A page with per-user data is safe by default.
- **Over HTTPS, compression can leak a secret** through the length of an answer (BREACH) when the answer repeats something from the request and also carries a secret such as a CSRF token. Set the header `No-Gzip-Compression` on such an answer, or turn `server.compress` off.
- **With `server.csp`, a script the CDN writes into a page does not run.** Its hash is not in the policy Tifa sent with the page. Turn off the features that change the HTML, such as Rocket Loader and Email Address Obfuscation of Cloudflare. See [Content-Security-Policy](headers-and-origins.md#content-security-policy).

### CDN settings

Checked against the vendors' own documentation on 2026-11-04. Names and defaults change, so open the linked page if a setting is missing.

**CloudFront**

- **Host and Origin.** CloudFront sends the domain name of the origin as `Host`, not the one the browser used ([request headers](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/RequestAndResponseBehaviorCustomOrigin.html)). Attach an origin request policy that forwards the viewer's headers, such as the managed `AllViewer`. Not `AllViewerExceptHostHeader`, which drops `Host` ([managed origin request policies](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/using-managed-origin-request-policies.html)).
- **Cache policy for pages.** Not the managed `CachingOptimized`: its minimum TTL is 1 second, and CloudFront then caches the answer even when it says `private`, `no-cache` or `no-store` ([managed cache policies](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/using-managed-cache-policies.html)). With a minimum TTL of 0, CloudFront respects those directives and takes `s-maxage` as the time to keep a page ([expiration](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/Expiration.html)). The managed `UseOriginCacheControlHeaders` has a minimum TTL of 0, but it puts all cookies in the cache key, so a page with `Cache` would not be shared. Make your own cache policy with a minimum TTL of 0, no cookies, and all query strings (or at least `_tifa`) in the key.
- **Client address.** CloudFront appends the address of the connection to `X-Forwarded-For`. What is before it came from the visitor and cannot be trusted. `CloudFront-Viewer-Address` arrives only with an origin request policy that includes it, such as `AllViewerAndCloudFrontHeaders-2022-06` ([client IP addresses](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/RequestAndResponseBehaviorCustomOrigin.html#RequestCustomIPAddresses)).
- **Compression.** Off until "Compress objects automatically" is on. CloudFront compresses only listed content types, between 1,000 and 10,000,000 bytes, only when the origin answer has a `Content-Length`, and never when it has a `Content-Encoding` ([serve compressed files](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/ServingCompressedFiles.html)).
- `stale-if-error` is supported, and capped by the maximum TTL ([expiration](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/Expiration.html#stale-content)).

**Cloudflare**

`tifa deploy setup` with cf adds these rules for its domain itself (see [Set up with a plan](deploy-setup.md#set-up-with-a-plan)).

- **Caching.** HTML and JSON are not cached by default, because Cloudflare decides by file extension ([default cache behavior](https://developers.cloudflare.com/cache/concepts/default-cache-behavior/)). To cache pages with `Cache`, add a Cache Rule for their paths with "Eligible for cache" and set Edge TTL to "Use cache-control header if present, bypass cache if not". Never "Ignore cache-control header and use this TTL" ([cache rule settings](https://developers.cloudflare.com/cache/how-to/cache-rules/settings/)). In the same rule set Browser TTL to "Respect origin TTL": otherwise the zone's Browser Cache TTL, 4 hours by default, replaces the `max-age=0` of a page with `Cache` and the `no-cache` of a public file in what browsers get, and browsers keep them that long, where no purge reaches.
- **`private` and `no-store`** are never cached. `s-maxage` is honored. The documentation does not say how `s-maxage=N` works next to `max-age=0`, which is what a page with `Cache` sends, so check that the second request for such a page is a cache hit ([Cache-Control directives](https://developers.cloudflare.com/cache/concepts/cache-control/)). `stale-if-error` is honored for 5xx answers.
- **Cache key.** The default key holds the scheme, the host and the URI with its query string, so the navigation data (`?_tifa=...`) is a different entry from the page ([cache keys](https://developers.cloudflare.com/cache/how-to/cache-keys/)).
- **Host.** Do not rewrite it with an Origin Rule "Override Host header" (Enterprise plans only) ([Origin Rules](https://developers.cloudflare.com/rules/origin-rules/)).
- **Client address.** `CF-Connecting-IP` and `X-Forwarded-For`. `True-Client-IP` is for Enterprise plans and off by default ([HTTP headers](https://developers.cloudflare.com/fundamentals/reference/http-headers/)).
- **Compression.** Cloudflare compresses with gzip, Brotli and Zstandard, and keeps the encoding of the origin when the origin sends a `Content-Encoding` the visitor accepts ([compression](https://developers.cloudflare.com/speed/optimization/content/compression/)).

**Google Cloud CDN**

- **Cache mode.** Use `USE_ORIGIN_HEADERS`. The default `CACHE_ALL_STATIC` also keeps `private` and `no-store` answers out. Not `FORCE_CACHE_ALL`: it ignores `private` and `no-store` and may cache the pages of single users ([cache modes](https://docs.cloud.google.com/cdn/docs/using-cache-modes)).
- **Headers.** `private` and `no-store` are not cached, `s-maxage` beats `max-age`, an answer with `Set-Cookie` is not cached, and `no-cache` is cached but checked with the origin each time. `stale-if-error` has no effect as a response header ([caching](https://docs.cloud.google.com/cdn/docs/caching)).
- **Cache key.** For a backend service the default key is the whole URI with its query string. Do not set a query string include or exclude list that leaves `_tifa` out ([caching](https://docs.cloud.google.com/cdn/docs/caching)).
- **Host and client address.** The load balancer keeps the `Host` of the request. It appends `<client address>,<load balancer address>` to `X-Forwarded-For`, and does not check what was there before ([external Application Load Balancer](https://docs.cloud.google.com/load-balancing/docs/https)).
- **Compression.** Dynamic compression is off by default, needs a `Content-Length`, and skips answers that have a `Content-Encoding` ([dynamic compression](https://docs.cloud.google.com/cdn/docs/dynamic-compression)).
