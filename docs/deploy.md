---
summary: 'How to put a Tifa app on a server: where the server is named (DEPLOY_HOST in .env, never in tifa.yaml) and the deploy settings of tifa.yaml (front, ssh, ci), tifa deploy setup with its plan of add, change and remove lines, tifa deploy and tifa deploy rollback over ssh to a Linux server with systemd, clearing the Cloudflare CDN cache of the domain on each deploy, deploys from GitHub Actions after the CI workflow (key, host keys, secrets, directly or through Cloudflare or Tailscale), a second developer machine, tifa deploy env for the .env of the app on the server, tifa deploy logs, status and remove to read the log of the app, see which release runs and take the app off the server, several apps on one server, a Cloudflare Tunnel or Caddy in front, and by hand: building for Linux, running it with its settings and data folder, stopping it cleanly, restarting it under systemd without refusing a connection and without signing users out, containers, ports and HTTPS, what to set when a CDN or proxy stands in front (Host and Origin, compression, which answers to cache), a health check route, metrics for Prometheus from tifa.Stats and what to read in them, and what happens to open pages when you deploy'
audience: app
read_when:
  - Putting a Tifa app on a VPS or in a container for the first time
  - Running tifa deploy, tifa deploy setup or tifa deploy rollback, or a deploy failed its health check and went back
  - Naming the server of tifa deploy with DEPLOY_HOST, or choosing deploy.front, deploy.ssh or deploy.ci in tifa.yaml
  - Deploying on every push with GitHub Actions after CI, or taking away the key CI deploys with
  - Putting an app behind a Cloudflare Tunnel with cf, or finding what tifa deploy setup changed in Cloudflare and how to remove it
  - A deploy said the CDN cache was not cleared, or you want to add the token CF_CACHE_PURGE_TOKEN
  - Giving a second developer machine access to deploy
  - Setting a secret or a setting in the .env of a deployed app, with tifa deploy env
  - Reading the log of a deployed app, checking which release runs and whether it is healthy, or taking an app off its server and out of Cloudflare and GitHub, with tifa deploy logs, status or remove
  - Putting several apps on one server, or finding the log, the .env or the data of a deployed app
  - Putting a CDN or a reverse proxy such as nginx, Cloudflare or CloudFront in front of a Tifa app
  - A POST gets 403 with `cross-origin request detected` only behind the proxy
  - Pages show old content or a missing script after a deploy
  - Deciding what a systemd unit or a Dockerfile for a Tifa app needs
  - Visitors get refused connections, or are signed out, each time you deploy
  - A visitor keeps a page open while you deploy, and you want to know what the browser does, or a 409 with `X-Tifa-Build` shows up
  - Adding a health check for a load balancer, a container or Kubernetes
  - Watching the VM pool and the render memo with Prometheus or another monitoring system, or deciding from them whether the machine needs more CPU or a larger `ssr.memo_size`
---

# Deploying

A Tifa app is one executable. The server needs that file, the settings in its environment, and a folder it may write data to. It needs no Node, no process manager and no Redis. The default session store lives in memory, so for more than one copy of the program give it a [store of your own](sessions.md#where-to-store-sessions).

## Deploy with tifa deploy

`tifa deploy` puts a Tifa app on your own server, and one command sets up the whole CI/CD path from push to live.

`go tool tifa deploy` puts the app on a Linux server with systemd over ssh: it builds on your machine for the server, uploads the program, restarts the app without refusing a request, checks it on the server and goes back to the release before when the check fails. Several apps can share one server. The server needs no Docker, no Node and no deploy program of its own.

`tifa.yaml` says what you want. `go tool tifa deploy setup` makes the server, Cloudflare and GitHub match it. `tifa.yaml` is committed, so neither a secret nor the place of the server goes in it.

```yaml
site:
  url: https://shop.example.com   # the domain is the host of this URL
deploy:
  front: none      # what visitors come through before the server: none | cloudflare
  ssh: direct      # how the GitHub Actions runner reaches the server's ssh: direct | cloudflare | tailscale
  ci: none         # who deploys automatically: none | github
```

- Where the server is does not go in `tifa.yaml`, because a public repository would show it. Name it on your machine, first match wins:
  1. the flag `--host`
  2. the environment variable `DEPLOY_HOST`
  3. `DEPLOY_HOST` in the file `.env` at the root of the app, which git ignores (see [.env](config.md#env). The program itself takes no notice of that name)

  ```sh
  # .env
  DEPLOY_HOST=ssh://203.0.113.7:2222
  ```

  The value is what `ssh` takes as a destination, without a user: a Host of `~/.ssh/config` (an alias such as `vps`, so `~/.ssh/config` supplies the address, the port, the keys and a `ProxyJump`), a host name, or an `ssh://host[:port]` URL. Tifa picks the user itself, root for the first setup and `tifa` after, so a user, a path, a query or another scheme is refused. With none of the three set, every `tifa deploy` command stops and says so. A `deploy.host` still in `tifa.yaml` is a warning at its line, and every `tifa deploy` command stops on it.
- `front` is what visitors come through before they reach the server. With `cloudflare`, that is a Cloudflare Tunnel at the host of `site.url`, with its DNS record and rules, and `deploy` and `rollback` clear the CDN cache of that host name. `site.url` must be set and have no port. With `none`, the server takes visitors another way, such as Caddy, and no cache is cleared.
- `ssh` is how the GitHub Actions runner reaches the ssh of the server. Anything but `direct` needs `ci: github`, because deploys from your machine use `~/.ssh/config`. `ssh: cloudflare` also needs `front: cloudflare`, because the ssh host name is made from the domain.
- `ci: github` turns the deploys from GitHub Actions on.

What you need: a Linux server with systemd, on x86_64 or aarch64, that you can reach with `ssh root@<DEPLOY_HOST>` once (tested on Ubuntu 24.04). On your machine, what `tifa build` needs (Go, and Node for an app with pages) and `ssh`. For `front: cloudflare` and `ssh: cloudflare`, [cf](https://developers.cloudflare.com/cf/get-started/), the Cloudflare CLI (in beta), logged in (`cf auth login`). For `ci: github`, [gh](https://cli.github.com/) logged in (`gh auth login`).

### Set up with a plan

```sh
go tool tifa deploy setup
```

Setup compares `tifa.yaml` with the server, Cloudflare and GitHub and prints a plan. The plan lists only what will `add`, `change` or `remove`. What is already right is counted in one line (`--all` lists it). Last comes what setup cannot do for you, as a numbered list with the reason under each item:

```
Plan for shop on vps

  add     app: /srv/tifa/shop with an empty shared/.env, port 9527, the systemd socket and service tifa-shop
  add     cloudflare: the tunnel of the server, tifa-vps-1234
  add     server: install cloudflared when missing and run the tunnel as the service tifa-tunnel, as root
  add     cloudflare: route shop.example.com -> http://127.0.0.1:9527
  add     cloudflare: DNS shop.example.com CNAME the tunnel, proxied
  add     cloudflare: cache rules for shop.example.com
  add     cloudflare: HTML rewriting off for shop.example.com
  add     github: a key for acme/shop that may log in as tifa, kept as the secret DEPLOY_SSH_KEY
  add     github: where CI logs in (direct) and the host keys it trusts, the secrets DEPLOY_HOST, DEPLOY_KNOWN_HOSTS and CF_ZONE_ID
  add     github: .github/workflows/tifa-deploy.yml, which you commit and push

Already right: server 1 (--all lists them)

You need to do 1 thing that setup cannot:
  1. cloudflare: a token that can only clear the cache of shop.example.com, pasted by you and kept as CF_CACHE_PURGE_TOKEN (optional)
     There is no terminal to paste on.
     Without it a deploy still works, but ... (the rest says what going without costs, and to add it later with tifa deploy setup --cache-purge-token)

Apply this plan? [y/N]
```

On a terminal it asks, and only when there is something to change. Without one, setup prints the plan and stops with an error unless you pass `--yes`. After the answer it applies the plan and prints one line for each step. When everything is right and nothing is left for you, setup prints one line and asks nothing:

```
shop on vps matches tifa.yaml: 9 things checked (--all lists them).
```

Setup exits with an error when an item is left for you or a step that may fail failed (a cache purge token that Cloudflare refused three times), after it did all the rest. An item marked `(optional)`, such as the cache purge token, does not make it fail. A script can rely on the exit code: run setup again once you did what the list says. Run it again whenever `tifa.yaml` changes: it only does what differs.

Setup removes only what Tifa made and marked (it cannot delete the Cloudflare token you made: the plan says to delete it in the dashboard): the lines in `authorized_keys` with the comment `tifa-github <owner>/<repo>`, Cloudflare objects whose names start with `tifa`, the route and DNS record of a domain it added, the repository variable `TIFA_DEPLOY` and the secrets set with it (`DEPLOY_HOST`, `DEPLOY_KNOWN_HOSTS`, `CF_ZONE_ID` and the others), and the workflow file that starts with Tifa's line. Set `front: none` or `ci: none`, and the plan lists those removals before anything goes.

The layers of the plan:

| Layer | What | Needs |
| --- | --- | --- |
| server | The user `tifa`, which owns and runs every app of the server and may log in with the keys of root (home `/srv/tifa`), and linger, so its systemd runs at boot | `ssh root@<DEPLOY_HOST>`, the first time only |
| app | `/srv/tifa/<app>`, its empty `shared/.env`, the data folder `shared`, its port (the first free one from 9527) in `port`, and the user units `tifa-<app>.socket` and `.service` | ssh as `tifa` |
| front | The Cloudflare Tunnel, cloudflared on the server, the route, the proxied DNS record and the cache rules of the host of `site.url` | cf |
| ci | A key for the repository, the secrets (where CI logs in, the host keys, the zone ID), the token that clears the cache, and the workflow | gh, and cf for the zone |
| ssh: cloudflare | An ssh host name on the tunnel, an Access application and a service token | cf |

`<app>` is the last part of the module path in `go.mod`, the name `tifa build` gives the program. Fill `.env` on the server, as `tifa` or root: the app reads it at each start.

The app listens on the loopback address only, so something on the server must take the visitors to it: the Tunnel of `front: cloudflare`, or a proxy of yours.

**Cloudflare Tunnel** (`front: cloudflare`). The server opens no port: cloudflared on the server connects out to Cloudflare, and one tunnel serves every app of the server. Setup uses your login of cf. Tifa keeps no Cloudflare token: the token of the tunnel goes from cf to the server only.

1. Finds the zone of the domain in your account.
2. Creates the tunnel of the server, `tifa-<host name>-<start of its machine ID>`, when the server has none, and keeps its ID in `/etc/tifa/tunnel`.
3. Installs cloudflared on the server when it has none, with apt from [Cloudflare's package repository](https://pkg.cloudflare.com/): apt checks the package against Cloudflare's signing key, and the setting `/etc/apt/apt.conf.d/52tifa-cloudflared` lets unattended-upgrades install new versions. Tifa downloads no program itself: on a server without apt, install cloudflared from your system's packages first. Runs cloudflared as the system service `tifa-tunnel`, with the token in `/etc/tifa/tunnel-token` (root only) handed to it as a systemd credential, and restarts it when cloudflared changes.
4. Adds the route `<domain> -> http://127.0.0.1:<port>` to the tunnel, keeping the routes of the other apps.
5. Adds a proxied CNAME record of the domain to `<tunnel ID>.cfargotunnel.com`. A record of the domain that points elsewhere is left alone, and setup stops.
6. Adds rules that hold for that host name only, as [CDN settings](#cdn-settings) says: a cache rule that caches what the `Cache-Control` of the app allows and nothing without one, a cache rule that never caches `/__tifa/` or navigation data (`?_tifa=`), and a configuration rule that turns off Rocket Loader, Email Address Obfuscation, Cloudflare Fonts, Zaraz and the Web Analytics script, which write into the HTML. Settings of the whole zone are not changed.

`tifa deploy remove` of the last app on a server removes the tunnel too (see [Logs, status and remove](#logs-status-and-remove)). To remove it by hand, run `systemctl disable --now tifa-tunnel tifa-tunnel-upgrade.path` and delete `/etc/tifa` and `/etc/systemd/system/tifa-tunnel*` on the server, then delete the tunnel in Cloudflare. cloudflared stays installed: `apt-get purge cloudflared` and deleting `/etc/apt/sources.list.d/cloudflared.list`, `/usr/share/keyrings/cloudflare-main.gpg` and `/etc/apt/apt.conf.d/52tifa-cloudflared` remove it too.

**Caddy**, or another proxy that gets certificates, when the server takes visitors itself: leave `front` at `none`. A Caddyfile:

```
shop.example.com {
    reverse_proxy 127.0.0.1:9527
}
```

Set up the CDN, if any, as [CDN settings](#cdn-settings) says, and clear its cache on each deploy as [After a deploy](#after-a-deploy) says.

### Deploy

```sh
go tool tifa deploy
```

1. Asks the server for its system and builds the app for it on your machine, as [Building for another system](build.md#building-for-another-system) does. Nothing is built on the server. On a server that setup prepared but that has no folder for this app yet, it makes the folder, port and units first, as `tifa` and without root. On a server that is not prepared, it stops and says to run `go tool tifa deploy setup`.
2. Uploads the program, gzipped, into `/srv/tifa/<app>/releases/<id>/`, where `<id>` is the time of the deploy in UTC, and checks its SHA-256 there.
3. Points `/srv/tifa/<app>/current` at it and restarts the service. The socket stays open, so requests that come meanwhile wait and get answered (see [Restart without dropping requests](#restart-without-dropping-requests)).
4. Checks the new program on the server: `GET /healthz` once a second, for up to 30 seconds, until it answers with a status below 500. An app without a `/healthz` route passes with its 404 as soon as it answers. Write [a health check](#health-check) that looks at the database to make the check mean more.
5. When the check passes, writes the deploy down and keeps the last 5 releases. When it fails, it prints the last lines of the log of the app, starts the release that ran before again, and exits with 1. If there is no release that ever passed, it stops the app.
6. With `front: cloudflare`, clears the CDN cache of that host name (see below).

Every deploy and rollback is a line of `/srv/tifa/<app>/deploys.log`: the time, the release, the git commit (with `-dirty` when the app had changes not committed), `deploy` or `rollback`, and `ok` or `failed`. The upload never includes `.env`. One deploy of an app runs at a time.

### Roll back

```sh
go tool tifa deploy rollback
```

It starts the newest earlier release that passed its check, checks it the same way, and clears the CDN cache. Run it again to go back further. The next `tifa deploy` makes a new release from your code.

### Clearing the CDN cache

With `front: cloudflare`, a deploy and a rollback clear the cache of that host name in Cloudflare, by host name and nothing wider: the rest of the zone keeps its cache. A page with `Cache` would otherwise show the old HTML, whose scripts are gone, until its `s-maxage` runs out.

Clearing is optional. No CDN, and no framework, makes every visitor see a new build the moment it is up: a cache that is not cleared keeps its copy until it runs out. Without a way to clear it, a page with `Cache` can be up to its `Cache` time old after a deploy, and that old copy may ask for scripts the new build no longer has. That is the price of a CDN, and Tifa says so on each deploy without calling it an error.

- On your machine, cf does it with your login.
- In CI there is no cf. The workflow gives Tifa a token that can only clear the cache of that one zone, `CF_CACHE_PURGE_TOKEN`, and the ID of the zone, `CF_ZONE_ID`. Both are secrets of the repository: the zone ID is no password, but a public repository must not show which zone the app is in. Tifa calls the Cloudflare API with them. The token needs no Zone Read, because the ID is already known. Tifa keeps neither.
- With neither, the deploy prints `cache: not cleared for <host>` with what that costs and how to add a token (`tifa deploy setup --cache-purge-token`), and is no warning.
- A clearing that fails does not fail the deploy: it prints a warning and exits with 0, and the summary of the run shows it. Clear the cache by hand in Cloudflare, Caching, Configuration, Custom Purge, by host name.

Purging by host name works on every Cloudflare plan.

### Deploy from GitHub Actions

Set `ci: github`, run `go tool tifa deploy setup`, then commit and push the workflow it wrote. Setup needs gh logged in and the app pushed to a GitHub repository. Actions then builds the app and runs `go tool tifa deploy`.

There are two workflows. `.github/workflows/ci.yml` is written by `tifa new` (or `tifa agents`) and runs `go tool tifa check` on pull requests and pushes. `.github/workflows/tifa-deploy.yml` is written by setup:

- It runs after the `CI` workflow passed on a push to the default branch, and deploys the commit that was checked. Without a `ci.yml` it runs on a push to the default branch instead. GitHub starts a `workflow_run` workflow only from the file on the default branch, so merge the file there before expecting it to run. It runs only for a push of this repository, never for a pull request from a fork.
- In the Actions tab, Run workflow offers `deploy` or `rollback`, on the ref you run it on.
- Only one deploy runs at a time, and a deploy that is running is never cancelled. The workflow has the permission `contents: read` only.
- A file that does not start with Tifa's line is yours: setup leaves it alone. Delete that line before you edit the file.
- An app with pages needs `pnpm-lock.yaml` in the repository, which CI installs from. The workflow uses the pnpm of `packageManager` in `package.json`, or pnpm 10 without it.

For the repository, setup:

1. Makes a key for this repository alone, and adds its public key to `/srv/tifa/.ssh/authorized_keys` on the server with the comment `tifa-github <owner>/<repo>`. The key cannot open a terminal or forward ports. Keep the secret `DEPLOY_SSH_KEY` as it is made: to rotate the key, set `ci: none`, run setup, set it back and run setup again.
2. Reads the host keys of the server over your own ssh and keeps them as the secret `DEPLOY_KNOWN_HOSTS`, so CI trusts that server and no other. CI never runs `ssh-keyscan`.
3. Sets `DEPLOY_HOST`, the whole target CI logs in to, as a secret, so the workflow file shows neither the address nor the port. With `ssh: direct` it is `ssh://<address>:<port>`, from what `ssh -G <your DEPLOY_HOST>` gives, so an alias from `~/.ssh/config` works, and the port is always written, even 22. A host that ssh reaches through `ProxyJump` or `ProxyCommand` cannot be copied into CI: use one of the two ways below. The host keys go the same way, with `[host]:port` in front of a port other than 22.
4. With `front: cloudflare`, keeps the ID of the zone as the secret `CF_ZONE_ID`, with the other secrets above, so it needs cf for the zone. The workflow hands it to Tifa to clear the CDN cache.
5. With `front: cloudflare`, asks for a token, which is optional, that can only clear the cache of the zone, kept as `CF_CACHE_PURGE_TOKEN`. A login of cf cannot make API tokens, so you make it in the Cloudflare dashboard, and on a terminal setup walks you through it: it prints a link to the Cloudflare form with the permission Cache Purge filled in and the name `tifa cache purge <owner>/<repo>`. Set Zone Resources to the zone of the domain only, create the token and paste it at the hidden prompt. Setup checks it by clearing a URL that does not exist, tries again up to 3 times, and gives it to gh on its input. An empty line skips it, and setup goes on and succeeds. Setup asks only on the first CI setup. Later runs, and a first run without a terminal, list it as optional. To add the token later, or to replace it, run `tifa deploy setup --cache-purge-token` on a terminal: it asks even when the secret is there. GitHub secrets are write-only, so a secret that is there is taken as right. Setup also removes a secret named `CF_PURGE_TOKEN`.
6. Keeps `TIFA_DEPLOY`, a repository variable that marks the secrets as Tifa's and holds a fingerprint of the connection and the zone, so a second run sees what is current.

Secret values go to gh on its input: they are never printed, never written to a file and never on a command line. In the log of a run, `tifa deploy` and `rollback` first print `::add-mask::<host name or address>` so GitHub hides it, because an ssh error prints the address it resolved, which is not the exact text of the secret.

**`ssh: direct`** (the default). CI logs in to the address and port of `DEPLOY_HOST`. The port must then be open to GitHub's runners, whose addresses change, so you cannot allow only some. Allow key-only login (`PasswordAuthentication no`): the key of the repository is the only way in.

**`ssh: cloudflare`**. For a server that opens no port for ssh. It needs `front: cloudflare` and the tunnel. With your login of cf, setup:

1. Adds the route `<ssh host> -> ssh://localhost:22` to the tunnel, keeping the other routes, and a proxied DNS record for it. The ssh host is the domain with `-ssh-` and 12 random characters after its first label, because Cloudflare's free certificates cover one level of subdomain: `deploy-a.example.com` becomes `deploy-a-ssh-k3j9x2m5q7wt.example.com`, and the apex `example.com` becomes `ssh-k3j9x2m5q7wt.example.com`. The random part keeps the name from being guessed from the domain. Setup makes it once and reads it back on later runs from the route of the tunnel and the Access application it made, so it stays the same. A name from an earlier version, without the random part, shows in the plan as a replacement: the new route, record and Access application are added, `DEPLOY_HOST` changes, and the old ones are removed.
2. Makes a service token called `tifa-github-<owner>-<repo>` that does not expire, a policy that lets in only that token and asks for no login, and a self-hosted Access application for the ssh host with that policy. Another repository for the same server adds its own token and policy to the same application.
3. Sets `CF_ACCESS_CLIENT_ID` and `CF_ACCESS_CLIENT_SECRET`, and `DEPLOY_HOST` is the ssh host.

The workflow installs one fixed version of cloudflared from its release page and checks the SHA-256 of the package (the apt repository keeps only the newest, so it cannot pin a version, and a newer cloudflared once stopped reading service tokens). Tifa raises the version only after a deploy through the tunnel passed with it. It connects with `ProxyCommand cloudflared access ssh`, handing it the token in `TUNNEL_SERVICE_TOKEN_ID` and `TUNNEL_SERVICE_TOKEN_SECRET`.

**`ssh: tailscale`**. The workflow joins your tailnet with `tailscale/github-action`, and `DEPLOY_HOST` is `ssh://` with the tailnet name of the server and its port. Only the Tailscale admin console can make the OAuth client: on Trust credentials, make one with the `auth_keys` scope for the tag `tag:ci`. On a terminal setup says so and asks for its client ID and secret, hidden. It checks that the client can make keys for `tag:ci` by asking the Tailscale API for a token, which makes no key and no device, tries again up to 3 times, and keeps the two values as the secrets `TS_OAUTH_CLIENT_ID` and `TS_OAUTH_SECRET`. CI cannot reach the server without them, so skipping, or running without a terminal, leaves an item for you and setup exits with an error. To add or replace them later, run `tifa deploy setup --tailscale-oauth` on a terminal. The tag `tag:ci` must exist in your tailnet policy (`tagOwners`) and be allowed to reach the server on port 22.

In Actions, `tifa deploy` and `rollback` write a short summary to the run page when the variable `GITHUB_STEP_SUMMARY` is set: the app, release, commit, time, the health check and whether the cache was cleared.

### More developers

Every machine that deploys needs its own access to the server, and setup does not carry it over. From a machine that can log in, add the public key of the new machine to the user `tifa`:

```sh
ssh-copy-id -i ~/.ssh/id_ed25519.pub tifa@vps
```

The new machine also needs to know where the server is: put `DEPLOY_HOST=` in the `.env` of its checkout (or pass `--host`), and when the value is an alias, the same alias in its `~/.ssh/config`. Nothing else is stored on a machine: `tifa.yaml` is in the repository and the rest lives on the server, in Cloudflare and in GitHub.

### Settings and secrets on the server

```sh
printf %s "$VALUE" | go tool tifa deploy env set API_KEY   # or run it on a terminal for a hidden prompt
go tool tifa deploy env unset API_KEY
go tool tifa deploy env list                               # names only
```

These edit `/srv/tifa/<app>/shared/.env` over ssh as `tifa`, the file the app reads from the folder it runs in (see [.env](config.md#env)). The value is read from the standard input or a hidden prompt, never from an argument, so it stays out of your shell history and the process list. Nothing prints a value. The file keeps mode 600, and your other lines and comments stay as they are. After a change the app restarts through its socket and must pass the health check of a deploy. If it does not, the old file comes back, the app restarts again, and the command says so and exits with 1. The file is read by the app as configuration only: it is not put into the environment of the process or of systemd, so each app on a server keeps its own.

### Logs, status and remove

```sh
go tool tifa deploy logs        # the last 100 lines of the log, -n 500 for more
go tool tifa deploy logs -f     # and then the new ones, until Ctrl-C
go tool tifa deploy status
go tool tifa deploy remove
```

They log in over ssh as `tifa` and find the server the way `tifa deploy` does.

`logs` prints the journal of `tifa-<app>.service` and `tifa-<app>.socket`: what the app printed, and what systemd said about it, such as a start, an exit or a failure. journald keeps these lines in a journal file of the user `tifa` when it keeps the journal on disk, the default of Ubuntu and Debian. A server that keeps its journal in memory only shows `tifa` no lines.

`status` prints the release that runs and its commit, the other releases kept and whether each passed its health check, since when the service runs, what `GET /healthz` answers now, and the memory of the program:

```
shop on vps, port 9527
  release  20261120T120000Z  commit ccc333        passed its health check
  kept     20261120T110000Z  commit bbb222-dirty  failed its health check
           20261120T100000Z  commit aaa111        passed its health check
  service  active since 2026-11-20 12:00:05 UTC, 2h3m ago
  health   GET /healthz answered 200
  memory   38.7 MB
```

It asks for `/healthz` only while the service runs, because a request to the socket would start it.

`remove` takes the app off the server and takes back what Tifa made for it, and nothing else:

- On the server, as `tifa`: it stops and disables `tifa-<app>.socket` and `.service`, and deletes their unit files and `/srv/tifa/<app>` with its releases, `shared/.env`, the data in `shared/` and `deploys.log`. Copy the data first if you want to keep it. Other apps, the user `tifa`, the firewall and sshd stay as they are.
- With `front: cloudflare`: the route, the DNS record and the rules of the domain, and with `ssh: cloudflare` the Access application, policy and service token of the repository. The tunnel and cloudflared serve every app of the server, so they go only with the last app of Tifa there: then remove stops cloudflared as root, deletes `/etc/tifa`, uninstalls cloudflared when setup installed it, and deletes the tunnel.
- With `ci: github`: the key of the repository in `authorized_keys`, and the secrets and the variable `TIFA_DEPLOY` that setup set. `.github/workflows/tifa-deploy.yml` is a file of your repository, so it stays: until you delete it, each push fails to deploy.

Like setup, remove prints a plan first. On a terminal it then asks you to type the name of the app, also with `--yes`. Without a terminal it removes nothing unless you pass `--yes`. What it cannot remove, without a login of cf or gh, is listed last with the login it needs. It removes the rest and exits with an error: log in and run it again. A step that fails does not stop the others, but the tunnel then stays until a run again has removed the rest. A run that finds nothing left says so in one line.

### On the server

- The log of an app: `go tool tifa deploy logs`, or `journalctl --user -u tifa-<app>` as `tifa` (`sudo -iu tifa`).
- Start, stop or look at it: `systemctl --user status tifa-<app>.service` as `tifa`. Restart the service, never the socket.
- The apps start at boot, before the first visitor.
- Several apps: each has its own folder, port and units. Run `tifa deploy` in each, after one `tifa deploy setup`.

The rest of this page is the way to do it by hand, and what `tifa deploy` does underneath.

## Build for the server

The server render engine runs on linux and darwin, on amd64 and arm64, and windows/amd64. Build on your machine or in CI with the target set, and copy the file over. Do not build on a small server such as a 1 GB VPS: the build needs more memory than the running app, and it can run out.

```sh
GOOS=linux GOARCH=amd64 $(go tool -n tifa) build -o bin/myapp-linux
scp bin/myapp-linux server:/srv/myapp/myapp
```

`GOOS=linux go tool tifa build` does not work, and [Building for another system](build.md#building-for-another-system) says why. The program holds code under several licenses, so when you hand it to others, ship the notices with it (see [Notices to ship with the program](build.md#notices-to-ship-with-the-program)).

## Run it

```sh
SERVER_ADDR=0.0.0.0:9527 ./myapp
```

- Every setting has a flag and an environment variable, and the program also reads a `.env` file of its working folder and a file given with `-c`. [Configuration](config.md) has the list and which source wins. `./myapp config` prints every setting with its value and where it came from, and `./myapp version` prints the versions.
- Platforms that tell the program its port in `PORT`, such as Cloud Run or Fly, work without more settings (see [PORT and HOST](config.md#port-and-host)).
- Run it from the folder it keeps data in. A relative path such as the `app.db` of the `tifa new` template (the setting `db.path`) is resolved against the working directory. Back that folder up, or mount it in a container.
- SIGINT and SIGTERM stop it gracefully: new connections stop, requests in flight get `server.shutdown_timeout` to finish, then `OnShutdown` runs. A stream with no end of its own, such as server-sent events, returns at once when it watches `tifa.ShuttingDown` (see [Streaming](guides/streaming.md#when-the-program-stops)). A systemd unit or a container needs nothing more than the executable, the environment and a writable data folder.
- A `FROM scratch` container works. The program holds the time zone data its renders need.
- Size the machine with [Performance](performance.md#sizing-a-machine). `ssr.vms` follows the CPU limit of a container, up to 4 VMs. On more CPUs, set it.

## Restart without dropping requests

A restart stops the program, and the next one prewarms its VMs before it listens. A connection that arrives in between is refused, unless systemd holds the socket: with socket activation, systemd listens on the port and hands the socket to the program, and keeps it open while the program restarts. The system takes the connections that arrive meanwhile, and the next program answers them. A visitor only waits a little longer: on an M4 Mac, the program of the example shop answers its first page 0.42 seconds after it starts with 10 VMs, and 0.27 seconds with `ssr.vms: 1`. The first start after a build, with the file not yet in the cache of the system, took 0.79 seconds. On a Linux machine with systemd 261, the 300 requests a small app got every 10 ms while `systemctl restart` ran all answered 200, the slowest in 0.14 seconds.

Users stay signed in across the restart only when sessions are kept outside the program. The default store lives in memory, so every restart signs everyone out (see [Where to store sessions](sessions.md#where-to-store-sessions)). An app from `tifa new` keeps them in its SQLite database (`sessions.go`).

Two units, for an app behind a proxy, or served over plain HTTP:

```ini
# /etc/systemd/system/shop.socket
[Unit]
Description=Shop socket

[Socket]
ListenStream=0.0.0.0:9527

[Install]
WantedBy=sockets.target
```

```ini
# /etc/systemd/system/shop.service
[Unit]
Description=Shop
Requires=shop.socket
After=shop.socket

[Service]
User=shop
WorkingDirectory=/var/lib/shop
ExecStart=/srv/shop/shop
Restart=on-failure

[Install]
WantedBy=multi-user.target
```

```sh
sudo systemctl daemon-reload
sudo systemctl enable --now shop.socket shop.service
# each deploy: copy the new program, then
sudo systemctl restart shop.service
```

- Restart the service, not the socket. Restarting or stopping `shop.socket` closes the port.
- `shop.service` is enabled too, so the program starts and prewarms at boot instead of on the first visitor.
- The program reads the socket from `LISTEN_PID`, `LISTEN_FDS` and `LISTEN_FDNAMES`, which systemd sets, and removes them from its environment, so the programs it starts do not see them. `server.addr` is not listened on then. The log line says `socket=systemd`. Without these variables the program listens itself, as before.
- systemd opens the port as root, so a port below 1024 needs no `setcap` or `AmbientCapabilities`.
- With the default `server.shutdown_timeout`, a stop takes at most about 30 seconds: 10 for the requests, 10 more for those still running after they are canceled, and 10 for `OnShutdown`. systemd waits 90 seconds (`TimeoutStopSec`) before it kills the program.

With HTTPS on, the program serves two addresses, and each socket says which by its name. One socket unit gives the same name to all its sockets, so use a unit for each:

```ini
# /etc/systemd/system/shop-https.socket
[Socket]
ListenStream=443
FileDescriptorName=server.addr
Service=shop.service

[Install]
WantedBy=sockets.target
```

```ini
# /etc/systemd/system/shop-http.socket
[Socket]
ListenStream=80
FileDescriptorName=server.http_addr
Service=shop.service

[Install]
WantedBy=sockets.target
```

In `shop.service`, `Requires=` and `After=` name both units, and `[Service]` adds `Sockets=shop-https.socket shop-http.socket`. `server.addr` may stay unset: [HTTPS](config.md#https) asks for it only when the program listens on the address itself. A socket named `server.addr` or `server.http_addr` serves that setting, and a single socket with any other name, or none, serves `server.addr`. Several must be named `server.addr` and `server.http_addr`, and a socket for `server.http_addr` without HTTPS on stops the program with an error. The program listens itself on an address systemd passed no socket for.

## Ports and HTTPS

- The program can get its own certificates from Let's Encrypt, or use yours: [HTTPS](config.md#https).
- Ports 80 and 443 need a right on Linux, or a container that publishes them from high ports: [Ports below 1024](config.md#ports-below-1024).
- Behind a CDN or a proxy that ends TLS, leave HTTPS off and listen on `0.0.0.0:<port>`.

## Behind a CDN or proxy

- **Host and Origin.** Pass `Host` and `Origin` through as the browser sent them. The program refuses a write that a browser sends from another site by comparing them, and a proxy that rewrites `Host` makes your own forms look foreign (403 with `cross-origin request detected`). See [Cross-site writes](config.md#cross-site-writes).
- **The client address.** Tifa does not read `Forwarded` or `X-Forwarded-For`. `c.Request().RemoteAddr` is the address of the proxy. If `OnRequest` needs the visitor's address, read the header your own proxy sets, and only when the request really comes from that proxy.
- **Compression.** The program compresses text answers itself. When the CDN or proxy compresses (brotli, for example), set `server.compress` to `false`. CloudFront, Cloudflare and Google Cloud CDN all leave an answer that already has a `Content-Encoding` alone. See [Compression](build.md#compression).
- **Caching.** Pages, navigation data and handler answers are `private, no-cache` by default, so a CDN does not store them. Keep it that way. Let the CDN cache these:
  - `/assets/`: content-hashed, `public, max-age=31536000, immutable`.
  - The files in `public/`: `no-cache` with an ETag, so the CDN asks and gets a 304. They keep fixed names, so only files that need one belong there. Import the rest to get `/assets/` (see [Static files](app.md#static-files)).
  - Pages with `Cache`: they are anonymous and carry `Cache-Control: public, max-age=0, s-maxage=<seconds>, stale-if-error=86400`. A CDN that follows the origin's `Cache-Control` keeps a page for the time of `Cache` and no longer. See [Cache for CDNs](ssr-and-cache.md#cache-for-cdns).
- **Do not make the CDN ignore the origin's cache headers.** Use a cache setting that follows them, and never one that forces a time or caches everything. The settings for each CDN are in [CDN settings](#cdn-settings).
- **The navigation data of a page with `Cache` is not shared.** It is the page URL plus `?_tifa=...`, it carries the session of the browser, and it is always `private, no-cache`. The cache key must include the query string (at least `_tifa`), or the CDN must skip the cache for requests with `_tifa`. Drop cookies by path only for the HTML of the page, never for the same path with `_tifa`.
- **Do not cache `/__tifa/session`.** It answers per visitor and is `private, no-store`.
- **Do not set `Cache-Control: public` yourself** on other pages, or on a handler that answers the URL of a page. A page with per-user data is safe by default.
- **Over HTTPS, compression can leak a secret** through the length of an answer (BREACH) when the answer repeats something from the request and also carries a secret such as a CSRF token. Set the header `No-Gzip-Compression` on such an answer, or turn `server.compress` off.
- **With `server.csp`, a script the CDN writes into a page does not run.** Its hash is not in the policy Tifa sent with the page. Turn off the features that change the HTML, such as Rocket Loader and Email Address Obfuscation of Cloudflare. See [Content-Security-Policy](config.md#content-security-policy).

### CDN settings

Checked against the vendors' own documentation on 2026-11-04. Names and defaults change, so open the linked page if a setting is missing.

**CloudFront**

- **Host and Origin.** CloudFront sends the domain name of the origin as `Host`, not the one the browser used ([request headers](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/RequestAndResponseBehaviorCustomOrigin.html)). Attach an origin request policy that forwards the viewer's headers, such as the managed `AllViewer`. Not `AllViewerExceptHostHeader`, which drops `Host` ([managed origin request policies](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/using-managed-origin-request-policies.html)).
- **Cache policy for pages.** Not the managed `CachingOptimized`: its minimum TTL is 1 second, and CloudFront then caches the answer even when it says `private`, `no-cache` or `no-store` ([managed cache policies](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/using-managed-cache-policies.html)). With a minimum TTL of 0, CloudFront respects those directives and takes `s-maxage` as the time to keep a page ([expiration](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/Expiration.html)). The managed `UseOriginCacheControlHeaders` has a minimum TTL of 0, but it puts all cookies in the cache key, so a page with `Cache` would not be shared. Make your own cache policy with a minimum TTL of 0, no cookies, and all query strings (or at least `_tifa`) in the key.
- **Client address.** CloudFront appends the address of the connection to `X-Forwarded-For`. What is before it came from the visitor and cannot be trusted. `CloudFront-Viewer-Address` arrives only with an origin request policy that includes it, such as `AllViewerAndCloudFrontHeaders-2022-06` ([client IP addresses](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/RequestAndResponseBehaviorCustomOrigin.html#RequestCustomIPAddresses)).
- **Compression.** Off until "Compress objects automatically" is on. CloudFront compresses only listed content types, between 1,000 and 10,000,000 bytes, only when the origin answer has a `Content-Length`, and never when it has a `Content-Encoding` ([serve compressed files](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/ServingCompressedFiles.html)).
- `stale-if-error` is supported, and capped by the maximum TTL ([expiration](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/Expiration.html#stale-content)).

**Cloudflare**

`tifa deploy setup` with cf adds these rules for its domain itself (see [Set up the server](#set-up-the-server)). On 2026-11-20 they gave, through a tunnel to the example shop: an `/assets/` file `HIT` from the second or third request, pages `BYPASS`, `/__tifa/session` `DYNAMIC`.

- **Caching.** HTML and JSON are not cached by default, because Cloudflare decides by file extension ([default cache behavior](https://developers.cloudflare.com/cache/concepts/default-cache-behavior/)). To cache pages with `Cache`, add a Cache Rule for their paths with "Eligible for cache" and set Edge TTL to "Use cache-control header if present, bypass cache if not". Never "Ignore cache-control header and use this TTL" ([cache rule settings](https://developers.cloudflare.com/cache/how-to/cache-rules/settings/)). In the same rule set Browser TTL to "Respect origin TTL": otherwise the zone's Browser Cache TTL, 4 hours by default, replaces the `max-age=0` of a page with `Cache` and the `no-cache` of a public file in what browsers get, and browsers keep them that long, where no purge reaches (seen on 2026-11-20: `max-age=14400`).
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

## Health checks and metrics

Tifa has no health route or metrics route of its own. Add the ones you need as API routes.

### Health check

A `GET` that answers 204 while the program can serve. Check what it cannot work without, such as the database of [the database guide](guides/database.md), and answer 503 when that is down:

```go
// app/healthz/route.go
package healthz

func GET(c *tifa.Ctx) error {
	if err := notes.DB.PingContext(c.Context()); err != nil {
		return tifa.Error{Status: 503, Message: "database unreachable"}
	}
	return c.NoContent(204)
}
```

- `tifa deploy` asks for `/healthz` after each deploy and goes back when it does not answer below 500 within 30 seconds (see [Deploy](#deploy)).
- Point the load balancer, the `HEALTHCHECK` of a container or a Kubernetes probe at `/healthz`. The program listens only once `OnStart` is done and the VMs are loaded, so the first 204 means it can render. When it is told to stop, new connections fail and the check goes red.
- The route renders nothing, so a full render queue does not fail it, and a busy program is not restarted for being busy. Watch `tifa_renders_rejected_total` in the metrics below for that.

### Metrics

`tifa.Stats()` returns the state of the VM pool, the render memo, and what it did since the program started. The counts are atomic, and reading the number of entries and VMs takes a short lock, so reading it every few seconds costs nothing you can measure. Under `tifa dev` the counts of the VM pool start again from zero each time the front end is rebuilt. This route writes it in the text format Prometheus reads, with no library:

```go
// app/metrics/route.go
package metrics

import (
	"fmt"
	"io"
	"strings"

	"github.com/liustack/tifa"
)

func GET(c *tifa.Ctx) error {
	s := tifa.Stats()
	var b strings.Builder
	metric := func(name, kind, help string, value float64) {
		fmt.Fprintf(&b, "# HELP %s %s\n# TYPE %s %s\n%s %g\n", name, help, name, kind, name, value)
	}
	metric("tifa_vms", "gauge", "VMs loaded.", float64(s.SSR.VMs))
	metric("tifa_vms_busy", "gauge", "VMs rendering.", float64(s.SSR.Busy))
	metric("tifa_renders_queued", "gauge", "Renders waiting for a VM.", float64(s.SSR.Queued))
	metric("tifa_renders_total", "counter", "Renders that ran in a VM.", float64(s.SSR.Renders))
	metric("tifa_renders_failed_total", "counter", "Renders that failed.", float64(s.SSR.Failed))
	metric("tifa_render_seconds_total", "counter", "Time the renders took.", s.SSR.RenderTime.Seconds())
	metric("tifa_renders_rejected_total", "counter", "Renders answered 503 as busy.", float64(s.SSR.Rejected))
	metric("tifa_vms_replaced_total", "counter", "VMs replaced because they grew.", float64(s.SSR.Replaced))
	metric("tifa_vms_memory_bytes", "gauge", "Bytes the VMs hold after their last GC.", float64(s.SSR.Memory))
	metric("tifa_memo_entries", "gauge", "Entries kept.", float64(s.Memo.Entries))
	metric("tifa_memo_hits_total", "counter", "Answered from an entry.", float64(s.Memo.Hits))
	metric("tifa_memo_misses_total", "counter", "Rendered by the request.", float64(s.Memo.Misses))
	metric("tifa_memo_shared_total", "counter", "Waited for another request.", float64(s.Memo.Shared))
	metric("tifa_memo_evictions_total", "counter", "Dropped to make room for another.", float64(s.Memo.Evictions))
	metric("tifa_memo_bytes", "gauge", "Bytes the render memo holds.", float64(s.MemoBytes))
	metric("tifa_memo_size_bytes", "gauge", "Bytes the render memo may hold.", float64(s.MemoSize))
	c.SetHeader("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, err := io.WriteString(c.Writer(), b.String())
	return err
}
```

- An app that already uses `client_golang` can write a `prometheus.Collector` instead, whose `Collect` reads `tifa.Stats()` once and sends each value with `prometheus.MustNewConstMetric`.
- The numbers say nothing about visitors, but they say how loaded the program is. Keep the route away from the public: let the proxy in front refuse `/metrics` from outside, or check a token in the handler.
- Before the server starts and after it stops, `tifa.Stats()` returns zeros. An app without pages has no VMs. The counts start at zero with each start of the program, which Prometheus reads as a restart.

What to read, and what to change:

| What you see | What it means | What to do |
| --- | --- | --- |
| `tifa_renders_rejected_total` grows | Renders found the queue full or waited past `ssr.wait_timeout`, and got 503 | More CPU and `ssr.vms`, or fewer renders (see [Sizing a machine](performance.md#sizing-a-machine) and [Render queue](config.md#render-queue)) |
| `tifa_vms_busy` is `tifa_vms` most of the time, `tifa_renders_queued` above 0 | Every VM renders and others wait | The same: the program is at its limit |
| `rate(tifa_render_seconds_total[5m]) / rate(tifa_renders_total[5m])` | The mean time of a render | When it grows, find the page that got slower |
| `tifa_renders_failed_total` grows | Renders throw, run past `ssr.timeout` or use up `ssr.vm_memory` | The log has each failure |
| `tifa_vms_memory_bytes` keeps climbing, or `tifa_vms_replaced_total` grows | The VMs hold more after each collection. A VM that holds more than half of `ssr.vm_memory` is replaced | Find what the pages keep, or the leak in the app. If VMs are replaced again within minutes, raise `ssr.vm_memory` (see [Memory](performance.md#memory)) |
| Hits low next to misses, `tifa_memo_evictions_total` grows, `tifa_memo_bytes` near `tifa_memo_size_bytes` | The render memo drops pages before they are asked for again | A larger `ssr.memo_size` |

## After a deploy

- A visitor who had a page open keeps working: the next click or handler call sees the new build, and the browser reloads the page once. See [Pages open while you deploy](deploy.md#pages-open-while-you-deploy).
- If your `OnRequest` or a CDN rule lets a CDN keep pages other than the ones with `Cache`, clear them on every deploy. Otherwise a visitor can get an old page whose scripts are gone.
- A CDN that keeps pages with `Cache` hands out the old HTML until `s-maxage` runs out, and the scripts that HTML names may be gone. With `front: cloudflare`, `tifa deploy` clears the host name for you when it has the token or a cf login (see [Clearing the CDN cache](#clearing-the-cdn-cache)). Without them the old page stays until `s-maxage` runs out, the accepted cost of a CDN. On another CDN, clear those pages when you deploy. A browser that asks the program itself gets the new page at once.
- The render memo of the program lives in memory and starts empty after a restart.

## Pages open while you deploy

A visitor can keep a page open while you deploy a new version, so the page is from the old build and the server is the new one. The page sends the build it was rendered with along with its data requests (in the `_tifa` query parameter) and its handler calls (`X-Tifa-Build`), and the server answers 409 for another build. What the visitor sees:

- Clicking a link: the browser reloads the current page, and the new build shows it. One reload, whatever number of requests got the 409.
- Calling a handler function: the call fails with a `TifaError` of status 409 and the browser reloads the page. The call is refused before the handler runs and never sent again, because the new page may mean another input by it. The visitor does it again on the new page. A 409 that your own handler returns has no `X-Tifa-Build` header and reloads nothing. A call without the header, from curl or another program, is not checked.
- Loading code: a lazy route or an `import()` asks for a chunk that the new build no longer has, and the browser reloads the page (this is Vite's `vite:preloadError`).

Each reload is made once for a build. When the page it brings back still disagrees with the server, which happens when a cache hands out the old page again, it does not reload a second time. The data request fails with the 409, which the `ErrorBoundary` of the route shows, the call throws it, and a failed import goes to the `ErrorBoundary` of the route or to the code that imported. The tab remembers the reloads in `sessionStorage`, and a page reloads only after it has written the mark and read it back. When the browser blocks the storage or does not keep what is written, the page does not reload: the failures above show as they are, with the message "the application was updated, reload the page", and the visitor reloads by hand. A reload that cannot be remembered could repeat for as long as the build disagrees.

A page whose HTML is old and whose entry script is gone cannot be fixed by the page: no code runs. Tifa sends pages with `Cache-Control: private, no-cache`, so a CDN does not keep them, and this can only happen when your `OnRequest` lets a CDN cache pages. Then clear the pages in the CDN when you deploy.

