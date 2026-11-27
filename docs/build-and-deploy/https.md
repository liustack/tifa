---
summary: 'Serve HTTPS from the program itself: automatic certificates, your own certificate, port 80 and HSTS, ports below 1024, HTTP/2'
audience: app
order: 19
read_when:
  - Serving HTTPS from the program itself, with Let's Encrypt or your own certificate, or deciding whether to
  - The program cannot listen on port 80 or 443, or gets no certificate
---

# HTTPS

HTTPS is off by default, and the program serves plain HTTP on `server.addr` as it always did. Leave it off behind a CDN or a proxy that ends TLS, such as Cloudflare, nginx or Caddy: they already speak HTTPS and HTTP/2 to browsers, and they would be the only ones to see a certificate of the program. With `tifa deploy`, `front: none` turns HTTPS on for you, and `cloudflare`, `external` and `nginx` leave it off.

Turn it on when the program faces the internet by itself. Browsers then also get HTTP/2, which they speak only over TLS: every file of a page shares one connection, where over HTTP/1.1 a browser opens at most 6 to a host, see [Preloading the chunks of a page](../reference/route-modules.md#preloading-the-chunks-of-a-page). There are two ways, and setting both fails. With either, set `server.addr`: with HTTPS on, the default `127.0.0.1:9527` refuses to start, since only this machine reaches it. Writing it out in the config is the same. When systemd passes the socket of `server.addr` ([socket activation](deploy-by-hand.md#restart-without-dropping-requests)), the program listens on that socket and not on the address, so the address may stay the default. Another loopback address, such as `127.0.0.1:8443`, starts with a warning in the log, for a test on this machine.

## Certificates from Let's Encrypt

```yaml
server:
  addr: :443
  tls_domains: example.com,www.example.com
  tls_email: ops@example.com     # optional
  tls_cache: /var/lib/myapp/certs
```

- At start the program asks Let's Encrypt for a certificate of each domain, or takes it from `tls_cache`, and logs until when it is valid. It renews each one on its own before it runs out (30 days before, or with a third of its life left for shorter certificates), without a restart. Setting `tls_domains` means agreeing to the terms of the ACME server, for Let's Encrypt its [subscriber agreement](https://letsencrypt.org/repository/).
- DNS must point each domain to this machine, and the internet must reach it on port 443 or 80. Let's Encrypt checks that you control the domain over TLS on port 443 (`server.addr`) or over plain HTTP on port 80 (`server.http_addr`). The program tries the first, then the second.
- `tls_cache` must survive restarts and new deployments: in a container, mount a volume on it. It holds the account key and the certificates with their private keys. A folder the program creates gets mode 0700 and each file 0600. A folder that already exists keeps its mode, so give it to the program's user alone. Without it every start would ask for new certificates, and Let's Encrypt gives the same set of names at most 5 a week: the sixth start in a week would get none.
- Wildcards (`*.example.com`) and IP addresses are not supported, since they need another kind of check. Names are kept in lower case and ASCII, so `bücher.example` becomes `xn--bcher-kva.example`, and a final dot is dropped. A name that is not a valid host name, such as `a_b.example.com` or `-x.example.com`, fails.
- A name not in `tls_domains` gets no certificate: its TLS handshake fails, and Let's Encrypt is not asked.
- When getting a certificate fails, for example because DNS points elsewhere, the log says why, and a connection a minute later tries again. Let's Encrypt allows 5 failed checks an hour for a name.
- To try it without these limits, set `tls_ca` to `https://acme-staging-v02.api.letsencrypt.org/directory`, the staging server of Let's Encrypt. Browsers do not trust its certificates. Empty `tls_cache` when you switch back.

## Your own certificate

```yaml
server:
  addr: :443
  tls_cert: /etc/letsencrypt/live/example.com/fullchain.pem
  tls_key: /etc/letsencrypt/live/example.com/privkey.pem
```

- Both files are PEM. The certificate file holds the certificate followed by its intermediate certificates, as certbot's `fullchain.pem` does.
- Before each new connection the program looks at both files and loads them again when one changed, so a renewed certificate takes effect without a restart or a signal. Links are followed, so the `live/` folder of certbot and a Kubernetes secret mounted as a volume work. While the new files do not load, for example when the certificate is written but its key not yet, the program keeps the certificate it had and logs why once.
- Files that do not load at start stop the program before `OnStart`.
- When certbot renews with the http-01 challenge, it needs port 80, which the program holds for `server.http_addr`. Renew with a DNS challenge instead, or set `http_addr: off` and let certbot listen on port 80 itself in standalone mode.

## Port 80 and HSTS

- With HTTPS on, `server.http_addr` (`:80` by default) serves plain HTTP. It answers the checks of Let's Encrypt, and redirects every other request to the same URL over HTTPS with a 308, which keeps the method. It redirects only names HTTPS has a certificate for: those of `tls_domains`, or those in your own certificate as last loaded. Other hosts, such as the bare IP address of the machine, get 421. The redirect has no port, so it goes to 443 also when `server.addr` listens on another port that 443 is mapped to. Set it to `off` for none: with `tls_domains`, Let's Encrypt can then check only over TLS on port 443.
- `server.hsts: true` adds `Strict-Transport-Security: max-age=31536000` to every answer of `server.addr`. A browser that saw it goes over HTTPS for a year, even when a link says `http://`, and turning it off again does not reach that browser. Turn it on once HTTPS works. It leaves out `includeSubDomains` and `preload`. Browsers ignore it over plain HTTP, so the redirect does not carry it. Behind a proxy that ends TLS it works too, since the proxy passes it on.

## Ports below 1024

On Linux only root may listen on ports 80 and 443. Give the program that one right instead:

```sh
sudo setcap cap_net_bind_service=+ep ./bin/myapp
```

Copying or building the file again drops it, so do it after each deployment. Under systemd, `AmbientCapabilities=CAP_NET_BIND_SERVICE` in the unit does the same. In a container that does not run as root, listen on high ports, such as `server.addr: :8443` and `server.http_addr: :8080`, and publish 443 and 80 on them (`-p 443:8443 -p 80:8080`). Without the right the program does not start, and the error says what to do. On macOS any user may listen on them. With socket activation systemd opens the ports, and the program needs no right: [`tifa deploy`](deploy-setup.md#the-app-serves-visitors-itself) with `front: none` works that way.

## What changes over HTTP/2

- Compression, the `.gz` files of the build, the render memo, ETags, redirects and server-sent events work as over HTTP/1.1, and a flush sends what was written at once.
- Deadlines run out over HTTP/2 even while the handler is not reading or writing: a write deadline that passes ends the answer, and a read deadline that passes makes the rest of the body fail to read. Moving either afterwards does not help. Over HTTP/1.1 a deadline that passed can still be moved before the next read or write. Move deadlines before they pass, see [Long requests](tuning.md#long-requests).
- `Hijack` fails with `http.ErrNotSupported` on an HTTP/2 connection, since that connection carries other requests too. Unless `GODEBUG=http2xconnect=1` is set, the server does not offer WebSockets over HTTP/2, so browsers open them over a separate HTTP/1.1 connection, where `Hijack` works.
- Over HTTPS, read what compression may leak in [Compression](../reference/server-program.md#compression).

`tifa dev` always serves plain HTTP and ignores these settings, also when the project file sets them.
