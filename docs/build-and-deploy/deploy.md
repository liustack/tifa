---
summary: 'Put a Tifa app on a server: `tifa deploy`, ssh, systemd, GitHub Actions, Cloudflare, health check'
audience: app
order: 3
read_when:
  - Putting a Tifa app on a VPS or in a container for the first time
  - Running tifa deploy, tifa deploy setup or tifa deploy rollback, or a deploy failed its health check and went back
  - Naming the server of tifa deploy with TIFA_DEPLOY_HOST, or finding out which server it uses when you named none (~/.tifa/config.yaml, TIFA_HOME), or choosing deploy.front, deploy.connect or deploy.auto in tifa.yaml
  - Deploying with GitHub Actions after CI, choosing the branch that deploys or the workflow it waits for (`auto: branch` and `needs`), or taking away the key CI deploys with
  - Putting an app behind a Cloudflare Tunnel with cf, or finding what tifa deploy setup changed in Cloudflare and how to remove it
  - A deploy said the CDN cache was not cleared, or you want to add or replace the token that clears it, which setup keeps on the server
  - Running tifa deploy setup or remove on a machine without gh, with a fine-grained token in TIFA_DEPLOY_GITHUB_TOKEN
  - Giving a second developer machine access to deploy
  - Deploying through the Cloudflare Tunnel with `connect: cloudflare` from your own machine, so the server needs no open ssh port, or a deploy waits on a Cloudflare Access login in the browser
  - Deploying from CI with `connect: tailscale`, making the Tailscale identity the runner joins your tailnet with, or a CI run that cannot join the tailnet
  - Setting a secret or a setting in the .env of a deployed app, with tifa deploy env
  - An agent runs tifa deploy env set, or tifa deploy setup with --cache-purge-token, and a page opened in the browser to type the secret
  - Reading the log of a deployed app, checking which release runs and whether it is healthy, or taking an app off its server and out of Cloudflare and GitHub, with tifa deploy logs, status or remove
  - Putting several apps on one server, or finding the log, the .env or the data of a deployed app
  - A deployed app with `front: none` does not answer from outside, setup stopped because port 80 or 443 is taken, you want to know which ports to open, or what setup and remove change in ufw
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

A Tifa app is one executable. The server needs that file, the settings in its environment, and a folder it may write data to. It needs no Node, no process manager and no Redis. The default session store lives in memory, so for more than one copy of the program give it a [store of your own](../pages-and-data/sessions.md#where-to-store-sessions).

## Deploy with tifa deploy

`tifa deploy` puts a Tifa app on your own server, and one command sets up the whole CI/CD path from push to live.

`go tool tifa deploy` puts the app on a Linux server with systemd over ssh: it builds on your machine for the server, uploads the program, restarts the app without refusing a request, checks it on the server and goes back to the release before when the check fails. The app serves visitors itself, with its own HTTPS, or behind a Cloudflare Tunnel, where several apps can share one server. The server needs no Docker, no Node and no deploy program of its own.

`tifa.yaml` says what you want. `go tool tifa deploy setup` makes the server, Cloudflare and GitHub match it. `tifa.yaml` is committed, so neither a secret nor the place of the server goes in it.

```yaml
site:
  url: https://shop.example.com   # the domain is the host of this URL
deploy:
  front: none      # what visitors come through before the server: none (the app serves them itself on ports 80 and 443) | cloudflare
  connect: direct  # how deploys reach the server over ssh: direct | cloudflare | tailscale
  auto:            # deploy by itself from GitHub Actions. No block: you deploy by hand only
    branch: main   # the branch that deploys. Left out: the default branch of the repository on GitHub
    needs: ci.yml  # the workflow file in .github/workflows that must pass first. Left out: ci.yml. none: do not wait
```

- Where the server is does not go in `tifa.yaml`, because a public repository would show it. Name it on your machine, first match wins:
  1. the flag `--host`
  2. the environment variable `TIFA_DEPLOY_HOST`
  3. `TIFA_DEPLOY_HOST` in the file `.env` at the root of the app, which git ignores (see [.env](config.md#env). The program itself takes no notice of that name)
  4. the servers `tifa deploy setup` remembered in `~/.tifa/config.yaml` (see [The servers Tifa remembers](#the-servers-tifa-remembers))

  ```sh
  # .env
  TIFA_DEPLOY_HOST=ssh://203.0.113.7:2222
  ```

  The value is what `ssh` takes as a destination, without a user: a Host of `~/.ssh/config` (an alias such as `vps`, so `~/.ssh/config` supplies the address, the port, the keys and a `ProxyJump`), a host name, or an `ssh://host[:port]` URL. Tifa picks the user itself, root for the first setup and `tifa` after, so a user, a path, a query or another scheme is refused. With none of the four set, every `tifa deploy` command stops and says so.
- `front` is what visitors come through before they reach the server. With `cloudflare`, that is a Cloudflare Tunnel at the host of `site.url`, with its DNS record and rules, and `deploy` and `rollback` clear the CDN cache of that host name. `site.url` must be set and have no port. With `none`, the app serves visitors itself on port 80, and with a domain in `site.url` also on 443 with its own HTTPS, and no cache is cleared (see [The app serves visitors itself](#the-app-serves-visitors-itself)).
- `connect` is the one path ssh takes to the server, from your machine and from the GitHub Actions runner. With `direct`, ssh uses the address you gave. With `cloudflare`, both go through the Cloudflare Tunnel, so the server needs no open ssh port (see [`connect: cloudflare`](#connect-cloudflare)). It needs `front: cloudflare`, because the ssh host name is made from the domain, and works with or without `auto`. With `tailscale`, the runner joins your tailnet, and it needs the block `auto` (see [`connect: tailscale`](#connect-tailscale)). Your own machine reaches the server however it already does, with the address in `TIFA_DEPLOY_HOST`, and the tailnet and the server are yours to set up.
- `auto` turns the deploys from GitHub Actions on, by being there. Without the block you deploy by hand only. `auto: {}` takes the defaults, and `auto:` with nothing after it is an error. It has two keys, and no others:
  - `branch` is the branch that deploys: a push to it, once the workflow in `needs` passed. Left out, it is the default branch of the repository on GitHub.
  - `needs` is the file in `.github/workflows` that must pass first, written as a file name such as `ci.yml` (no folder). Left out, it is `ci.yml`, which `tifa new` writes. `none` waits for nothing: the deploy runs on every push to the branch. See [Deploy from GitHub Actions](#deploy-from-github-actions).
- Only GitHub is supported.

**Inputs.** The values that belong to you and not to the repository are read the same way every time: the flag when the input has one, else the environment variable, else the line in the file `.env` at the root of the app. Tifa reads that file. Setup never writes a token into it and never prints one. The only line it ever writes there is `TIFA_DEPLOY_HOST`, which `connect: cloudflare` moves to the ssh host name once ssh through the tunnel works (see [`connect: cloudflare`](#connect-cloudflare)), and only when the value is already in `.env`.

| Input | Flag | What it is | Comes from |
| --- | --- | --- | --- |
| `TIFA_DEPLOY_HOST` | `--host` | Where the server is | flag, environment, `.env`, then `~/.tifa` |
| `TIFA_DEPLOY_CF_PURGE_TOKEN` | none | A Cloudflare token that can only clear the cache of one zone. Overrides the one setup keeps on the server (see [Clearing the CDN cache](#clearing-the-cdn-cache)) | environment, `.env` |
| `TIFA_DEPLOY_CF_ZONE_ID` | none | The ID of that zone, needed with the token above | environment, `.env` |
| `TIFA_DEPLOY_GITHUB_TOKEN` | none | A fine-grained GitHub token, for setup and remove when gh is not there | environment, `.env` |

A `TIFA_DEPLOY_` name in `.env` that tifa deploy does not read stops the command too, and the error names the closest one.

What you need: a Linux server with systemd, on x86_64 or aarch64, that you can reach with `ssh root@<TIFA_DEPLOY_HOST>` once (tested on Ubuntu 24.04). On your machine, what `tifa build` needs (Go, and Node for an app with pages) and `ssh`. For `front: cloudflare` and `connect: cloudflare`, [cf](https://developers.cloudflare.com/cf/get-started/), the Cloudflare CLI (in beta), installed (`pnpm add --global cf`) and logged in (`cf auth login`). Setup and remove tell the two apart: without cf on the PATH they say to install it, with cf installed they ask for the login. For `connect: cloudflare`, also [cloudflared](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/downloads/) on your machine: setup installs it with `brew install cloudflared` on macOS or `winget install --id Cloudflare.cloudflared -e` on Windows, and on Linux, or without brew or winget, it tells you where to get it. For `auto`, [gh](https://cli.github.com/) logged in (`gh auth login`), or without gh a fine-grained token in `TIFA_DEPLOY_GITHUB_TOKEN` (see [Deploy from GitHub Actions](#deploy-from-github-actions)). For `connect: tailscale`, a server that is on your tailnet (Tailscale installed and logged in there), and a Tailscale credential that you make in the Tailscale console (see [`connect: tailscale`](#connect-tailscale)). Your machine needs nothing of Tailscale. On a terminal, setup installs and logs in cf and gh for you (see below).

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
  add     github: a key for acme/shop that may log in as tifa, kept as the secret TIFA_DEPLOY_SSH_KEY
  add     github: where CI logs in (direct) and the host keys it trusts, the secrets TIFA_DEPLOY_HOST and TIFA_DEPLOY_KNOWN_HOSTS
  add     github: .github/workflows/tifa-deploy.yml, which you commit and push

Already right: server 1 (--all lists them)

You need to do 1 thing that setup cannot:
  1. server: a token that can only clear the cache of example.com, pasted by you and kept in /srv/tifa/.config/tifa/cloudflare-purge, which only tifa can read (optional)
     There is no terminal to paste on.
     go tool tifa deploy setup --yes --cache-purge-token opens a page in your browser for it.
     Without it a deploy still works, but ... (the rest says what going without costs, and to add it later with tifa deploy setup --cache-purge-token)

Apply this plan? [y/N]
```

On a terminal it asks, and only when there is something to change. Without one, setup prints the plan and stops with an error unless you pass `--yes`. After the answer it applies the plan and prints one line for each step. When everything is right and nothing is left for you, setup prints one line and asks nothing:

```
shop on vps matches tifa.yaml: 9 things checked (--all lists them).
```

**cf and gh.** When `tifa.yaml` needs cf (`front: cloudflare`) or gh (the block `auto` without `TIFA_DEPLOY_GITHUB_TOKEN`) and it is missing or not logged in, the first plan installs and logs it in, and the rest waits for it, since setup reads Cloudflare and GitHub with them:

```
Plan for shop on vps

  add     this machine: install cf with pnpm add --global cf
  add     this machine: log in to cf (opens a browser)

Setup plans the rest, with the Cloudflare part, once cf is ready, and asks again.

Apply this plan? [y/N]
```

Your typed y is the consent: setup asks nothing more about it. It installs cf with `pnpm add --global cf`, and gh with `brew install gh` on macOS or `winget install --id GitHub.cli -e` on Windows, then runs `cf auth login` and `gh auth login --web` on your terminal, and each one opens a browser. gh logs in with the git protocol https, which leaves your other repositories as they are, and asks one question: whether to log git in with GitHub too. Either answer works for Tifa. Yes lets git push with the login of gh, the workflow file included. Setup uses the program where the package manager put it, even when the PATH of your terminal does not have that folder yet. Then it plans the rest and asks again. It installs only with a package manager the machine has and never downloads a program itself. With `--yes`, even on a terminal, or without a terminal, it installs nothing and logs in nothing, and lists the commands among what you need to do: an agent that runs setup with `--yes` never installs a global package or opens a browser. It does the same without pnpm, without brew or winget, and on Linux, where gh needs root and [GitHub's package repository](https://github.com/cli/cli/blob/trunk/docs/install_linux.md). An install or login that fails is a warning that says how to do it by hand (pnpm needs `pnpm setup` once before it installs anything globally), and setup goes on with what does not need that tool.

Setup exits with an error when an item is left for you or a step that may fail failed (a cache purge token that Cloudflare refused three times), after it did all the rest. An item marked `(optional)`, such as the cache purge token, does not make it fail. A script can rely on the exit code: run setup again once you did what the list says. Run it again whenever `tifa.yaml` changes: it only does what differs.

Setup removes only what Tifa made and marked (it cannot delete the Cloudflare token you made: the plan says to delete it in the dashboard): the lines in `authorized_keys` with the comment `tifa-github <owner>/<repo>`, Cloudflare objects whose names start with `tifa` (and the Access application `tifa deploy ssh <host>`), the block that starts with `# tifa <app>:` in your `~/.ssh/config`, the route and DNS record of a domain it added, the repository variables `TIFA_DEPLOY_FINGERPRINT`, `TIFA_DEPLOY_TAILSCALE_ID` and `TIFA_DEPLOY_TAILSCALE_AUDIENCE` and the secrets set with it (`TIFA_DEPLOY_HOST`, `TIFA_DEPLOY_KNOWN_HOSTS` and the others), and the workflow file that starts with Tifa's line. Set `front: none`, or take the block `auto` out, and the plan lists those removals before anything goes.

The layers of the plan:

| Layer | What | Needs |
| --- | --- | --- |
| server | The user `tifa`, which owns and runs every app of the server and may log in with the keys of root (home `/srv/tifa`), and linger, so its systemd runs at boot | `ssh root@<TIFA_DEPLOY_HOST>`, the first time only |
| app | `/srv/tifa/<app>`, its empty `shared/.env`, the data folder `shared`, its port in `port` (80, or 443 with HTTPS, for `front: none`, else the first free one from 9527 on the loopback), and the user units `tifa-<app>.socket` and `.service`, with HTTPS also `tifa-<app>-http.socket`. For `front: none`, once per server as root, the setting that lets the systemd of `tifa` open ports 80 and 443 | ssh as `tifa`, and root once |
| front | The Cloudflare Tunnel, cloudflared on the server, the route, the proxied DNS record and the cache rules of the host of `site.url`, and on the server the token that clears its cache | cf |
| auto | A key for the repository, the secrets (where CI logs in, the host keys), and the workflow | gh or `TIFA_DEPLOY_GITHUB_TOKEN` |
| connect: cloudflare | An ssh host name on the tunnel, an Access application that admits your email (and, with CI, a service token), and on your machine cloudflared and a block in `~/.ssh/config` | cf |
| connect: tailscale | The variables of the Tailscale identity the runner joins your tailnet with | the client ID of the identity, gh |

`<app>` is the last part of the module path in `go.mod`, the name `tifa build` gives the program. Fill `.env` on the server, as `tifa` or root: the app reads it at each start.

**Cloudflare Tunnel** (`front: cloudflare`). The server opens no port: cloudflared on the server connects out to Cloudflare, and one tunnel serves every app of the server. Setup uses your login of cf. The Cloudflare tokens Tifa uses stay on your server: the token of the tunnel goes from cf to the server only, and the token that clears the cache goes from your paste to the server only.

1. Finds the zone of the domain in your account.
2. Creates the tunnel of the server, `tifa-<host name>-<start of its machine ID>`, when the server has none, and keeps its ID in `/etc/tifa/tunnel`.
3. Installs cloudflared on the server when it has none, with apt from [Cloudflare's package repository](https://pkg.cloudflare.com/): apt checks the package against Cloudflare's signing key, and the setting `/etc/apt/apt.conf.d/52tifa-cloudflared` lets unattended-upgrades install new versions. Tifa downloads no program itself: on a server without apt, install cloudflared from your system's packages first. Runs cloudflared as the system service `tifa-tunnel`, with the token in `/etc/tifa/tunnel-token` (root only) handed to it as a systemd credential, and restarts it when cloudflared changes.
4. Adds the route `<domain> -> http://127.0.0.1:<port>` to the tunnel, keeping the routes of the other apps.
5. Adds a proxied CNAME record of the domain to `<tunnel ID>.cfargotunnel.com`. A record of the domain that points elsewhere is left alone, and setup stops.
6. Adds rules that hold for that host name only, as [CDN settings](#cdn-settings) says: a cache rule that caches what the `Cache-Control` of the app allows and nothing without one, a cache rule that never caches `/__tifa/` or navigation data (`?_tifa=`), and a configuration rule that turns off Rocket Loader, Email Address Obfuscation, Cloudflare Fonts, Zaraz and the Web Analytics script, which write into the HTML. Settings of the whole zone are not changed.
7. Asks for a token, which is optional, that can only clear the cache of the zone, and keeps it with the ID of the zone on the server, where every deploy finds it (see [Clearing the CDN cache](#clearing-the-cdn-cache)). A login of cf cannot make API tokens, so you make it in the Cloudflare dashboard, and on a terminal setup walks you through it: it prints a link to the Cloudflare form with the permission Cache Purge filled in and the name `tifa cache purge <zone>`. Set Zone Resources to that zone only, create the token and paste it at the hidden prompt. Setup checks it by clearing a URL that does not exist, tries again up to 3 times, and writes it to the server over ssh as `tifa`, on the input of ssh. An empty line skips it, and setup goes on and succeeds. Setup asks on the first setup of the domain, and on the first setup of CI. Once the server has a token for the zone, setup checks it the same way on each run and asks nothing while Cloudflare takes it: a second app on the same server and zone needs nothing. A token Cloudflare refuses, such as one that expired or was deleted, is asked for again. Without a token, later runs with `auto`, and a first run without a terminal, list it as optional. To add the token later, or to replace it, run `go tool tifa deploy setup --cache-purge-token`: it asks even when the server has one, on a terminal at the hidden prompt and without one on a page in your browser, with `--yes` too (see [Secrets without a terminal](#secrets-without-a-terminal)).

`tifa deploy remove` of the last app on a server removes the tunnel and the cache token too (see [Logs, status and remove](#logs-status-and-remove)). To remove them by hand, run `systemctl disable --now tifa-tunnel tifa-tunnel-upgrade.path` and delete `/etc/tifa`, `/etc/systemd/system/tifa-tunnel*` and `/srv/tifa/.config/tifa` on the server, then delete the tunnel and the token `tifa cache purge <zone>` in Cloudflare. cloudflared stays installed: `apt-get purge cloudflared` and deleting `/etc/apt/sources.list.d/cloudflared.list`, `/usr/share/keyrings/cloudflare-main.gpg` and `/etc/apt/apt.conf.d/52tifa-cloudflared` remove it too.

#### The app serves visitors itself

With `front: none` the program meets visitors directly, with no nginx, Caddy or certbot in front:

- **With a domain**, the host of `site.url` such as `https://shop.example.com`: systemd opens ports 443 and 80 for the app, and the app runs its [built-in HTTPS](config.md#https). Setup writes `SERVER_TLS_DOMAINS=<domain>` and `SERVER_TLS_CACHE=%h/<app>/certs` into the environment of the service, so the program gets its certificate from Let's Encrypt and keeps it in `/srv/tifa/<app>/certs`, which every release shares. Port 80 answers the checks of Let's Encrypt and sends everything else to HTTPS. DNS must point the domain at the server before the first deploy: until the program has its certificate it answers no HTTPS, and the health check of the deploy fails and says so.
- **Without a domain**: no `site.url`, or its host is an IP address or `localhost`. The app serves plain HTTP on port 80.

systemd holds the ports, so the app runs as `tifa` with no root and no capability, and restarts without refusing a connection. A user's systemd may only open ports from 1024 on Linux, so setup, once per server and as root, writes `net.ipv4.ip_unprivileged_port_start = 80` to `/etc/sysctl.d/60-tifa-ports.conf`. Ports from 80 to 1023 are then open to every user of the server, not only root. `tifa deploy remove` of the app deletes the file and puts back the value from before.

**One app per server.** Only one program can listen on a port, so only one app on a server can serve visitors itself. Before it changes anything, setup looks at what listens on the ports the app needs (80, and 443 with a domain): `ss -ltnp`, and the sockets the systemd of `tifa` holds. When another app of Tifa holds them, or a program such as nginx, Caddy or Apache, setup stops, names it and the port, and changes nothing. Stop that program and keep it from starting at boot, remove the other app, or put this app behind Cloudflare with `front: cloudflare`, where several apps share a server.

**ufw.** When ufw is on and does not let visitors reach the ports the app needs, 80 and 443, or 80 without a domain, the plan of setup allows them:

```
  change  firewall: allow ports 80 and 443 (tcp) in ufw, so visitors reach shop, in rules with the comment 'tifa shop', as root
```

Once you agree, or with `--yes`, setup runs `ufw allow 80/tcp comment 'tifa <app>'` as root for each of those ports. Nothing changes in ufw when it is off or not installed, when a rule of yours already allows the port, or when its default lets everything in. A rule of yours that denies the port stays: setup says which and how to delete it. The comment `tifa <app>` marks the rules as Tifa's: `tifa deploy remove` deletes them, and so does setup once the app no longer serves visitors on a port, such as after a move to `front: cloudflare`. No other rule is ever touched, the one of ssh among them. Only root can read the rules of ufw: when root cannot log in, setup says what to check on the server instead.

**The firewall of your provider is yours.** Tifa does not change the firewall or the security groups of your provider, nor a firewall on the server that is not ufw (firewalld, or rules of nftables or iptables that ufw did not write). The plan says which ports visitors need, and so does the end of setup: open them there if they block them. After a deploy, Tifa asks the app on those ports from your machine, and says when one did not answer.

A server set up with an earlier version has the app on the loopback. The next `tifa deploy setup` plans the move: the setting of the ports as root, then the new sockets, after which the app restarts on them and must pass the health check of a deploy.

Set up the CDN, if any, as [CDN settings](#cdn-settings) says, and clear its cache on each deploy as [After a deploy](#after-a-deploy) says.

### Deploy

```sh
go tool tifa deploy
```

1. Asks the server for its system and builds the app for it on your machine, as [Building for another system](build.md#building-for-another-system) does. Nothing is built on the server. On a server that setup prepared but that has no folder for this app yet, it makes the folder, port and units first, as `tifa` and without root. An app that serves visitors itself needs setup there first when `tifa` may not open port 80 yet, and the deploy says so. On a server that is not prepared, it stops and says to run `go tool tifa deploy setup`.
2. Uploads the program, gzipped, into `/srv/tifa/<app>/releases/<id>/`, where `<id>` is the time of the deploy in UTC, and checks its SHA-256 there.
3. Points `/srv/tifa/<app>/current` at it and restarts the service. The socket stays open, so requests that come meanwhile wait and get answered (see [Restart without dropping requests](#restart-without-dropping-requests)).
4. Checks the new program on the server: `GET /healthz` once a second, for up to 30 seconds, until it answers with a status below 500. It asks on the loopback, and an app with its own HTTPS over HTTPS for its domain, as a visitor would. An app without a `/healthz` route passes with its 404 as soon as it answers. Write [a health check](#health-check) that looks at the database to make the check mean more.
5. When the check passes, writes the deploy down and keeps the last 5 releases. When it fails, it prints the last lines of the log of the app, starts the release that ran before again, and exits with 1. If there is no release that ever passed, it stops the app.
6. With `front: cloudflare`, clears the CDN cache of that host name (see below).
7. Says where visitors reach the app: `visitors: https://shop.example.com`, or `http://<address of the server>` without a domain. With `front: none` it then asks the app on its ports from your machine, and when one does not answer it says that the firewall or a security group of your provider may block it, which Tifa does not change.

Every deploy and rollback is a line of `/srv/tifa/<app>/deploys.log`: the time, the release, the git commit (with `-dirty` when the app had changes not committed), `deploy` or `rollback`, and `ok` or `failed`. The upload never includes `.env`. One deploy of an app runs at a time.

### Roll back

```sh
go tool tifa deploy rollback
```

It starts the newest earlier release that passed its check, checks it the same way, and clears the CDN cache. Run it again to go back further. The next `tifa deploy` makes a new release from your code.

### Clearing the CDN cache

With `front: cloudflare`, a deploy and a rollback clear the cache of that host name in Cloudflare, by host name and nothing wider: the rest of the zone keeps its cache. A page with `Cache` would otherwise show the old HTML, whose scripts are gone, until its `s-maxage` runs out.

Clearing is optional. No CDN, and no framework, makes every visitor see a new build the moment it is up: a cache that is not cleared keeps its copy until it runs out. Without a way to clear it, a page with `Cache` can be up to its `Cache` time old after a deploy, and that old copy may ask for scripts the new build no longer has. That is the price of a CDN, and Tifa says so on each deploy without calling it an error.

The deploy clears it with the first of these it finds, run on your machine or in CI alike:

1. The inputs `TIFA_DEPLOY_CF_PURGE_TOKEN`, a token that can only clear the cache of that one zone, and `TIFA_DEPLOY_CF_ZONE_ID`, the ID of the zone: the environment, else the `.env` at the root of the app. They override the server. Give the zone ID as well, because a token that only purges usually cannot read the zone to find it. When the token is set and the zone ID is not, the deploy says so in its warning. Setup does not write them to `.env`: put them there yourself.
2. The token that setup keeps on the server (see [Set up with a plan](#set-up-with-a-plan)): `/srv/tifa/.config/tifa/cloudflare-purge`, one line per zone with the name of the zone, its ID and the token. Tifa keeps the token on your server, readable only by `tifa`: the file is owned by `tifa` with mode 600, in a folder with mode 700. The deploy reads it over the ssh it deploys with, as `tifa`, once the new release passed its check. It holds the token in memory only and never prints it, logs it or puts it on a command line. So a deploy from CI, and from any machine that may log in as `tifa`, clears the cache, and one token serves every app of the server on that zone. Names of hosts and zones match in any case. Writes to the file hold a lock next to it (`cloudflare-purge.lock`, with `flock`), so two setups on one server keep each other's line. When the file cannot be read, the deploy says so in one line and goes on to the next way. The apps run as `tifa` too, so their code could read the file: the token can only clear the cache of the zone.
3. On your machine, cf with your login.

With none of them the deploy prints `cache: not cleared for <host>` with what that costs and says to run `go tool tifa deploy setup --cache-purge-token`, and is no warning.

A clearing that fails does not fail the deploy: it prints a warning and exits with 0, and the summary of the run shows it. Clear the cache by hand in Cloudflare, Caching, Configuration, Custom Purge, by host name.

Purging by host name works on every Cloudflare plan.

### Deploy from GitHub Actions

Add the block `auto: {}` to the `deploy` section of `tifa.yaml`, run `go tool tifa deploy setup`, then commit and push the workflow it wrote. Setup needs access to GitHub (gh logged in, or a token, see below) and the app pushed to a GitHub repository. Actions then builds the app and runs `go tool tifa deploy`.

There are two workflows. `.github/workflows/ci.yml` is written by `tifa new` (or `tifa agents`) and runs `go tool tifa check` on pull requests and pushes. `.github/workflows/tifa-deploy.yml` is written by setup:

- It deploys a push to `auto.branch` (the default branch of the repository without it) once the workflow in `auto.needs` passed on it (`ci.yml` without it), and deploys the commit that was checked. It runs only for a push of this repository, never for a pull request from a fork, and only after the file named in `needs`: two workflows can share a name, so the job also checks the path of the run that finished (`github.event.workflow_run.path`).
- Setup reads the file in `needs` and refuses to go on when it is missing, has no `name:` line (GitHub's `workflow_run` finds the workflow by its name), or does not run on a push to the branch (its `on:` has no `push`, or a `branches` or `branches-ignore` filter leaves the branch out). The plan lists that as something for you, and setup exits with an error: fix the file, change `needs`, or write `needs: none`. It never deploys without waiting for a workflow you named.
- With `needs: none` the workflow runs on a push to the branch and waits for nothing.
- GitHub starts a `workflow_run` workflow only from the file on the default branch, whatever branch deploys, so merge the file there before expecting it to run. When `branch` is not the default branch, the plan line of the workflow says so. A `push` workflow (`needs: none`) runs from the file on the branch that was pushed.
- In the Actions tab, Run workflow offers `deploy` or `rollback`, on the ref you run it on.
- Only one deploy runs at a time, and a deploy that is running is never cancelled. The workflow has the permission `contents: read` only.
- A file that does not start with Tifa's line is yours: setup leaves it alone. Delete that line before you edit the file.
- An app with pages needs `pnpm-lock.yaml` in the repository, which CI installs from. The workflow uses the pnpm of `packageManager` in `package.json`, or pnpm 10 without it.

For the repository, setup:

1. Makes a key for this repository alone, and adds its public key to `/srv/tifa/.ssh/authorized_keys` on the server with the comment `tifa-github <owner>/<repo>`. The key cannot open a terminal or forward ports. Keep the secret `TIFA_DEPLOY_SSH_KEY` as it is made: to rotate the key, take the block `auto` out, run setup, put it back and run setup again.
2. Reads the host keys of the server over your own ssh and keeps them as the secret `TIFA_DEPLOY_KNOWN_HOSTS`, so CI trusts that server and no other. CI never runs `ssh-keyscan`.
3. Sets `TIFA_DEPLOY_HOST`, the whole target CI logs in to, as a secret, so the workflow file shows neither the address nor the port. With `connect: direct` it is `ssh://<address>:<port>`, from what `ssh -G <your TIFA_DEPLOY_HOST>` gives, so an alias from `~/.ssh/config` works, and the port is always written, even 22. A host that ssh reaches through `ProxyJump` or `ProxyCommand` cannot be copied into CI: use one of the two ways below. The host keys go the same way, with `[host]:port` in front of a port other than 22.
4. Keeps `TIFA_DEPLOY_FINGERPRINT`, a repository variable that marks the secrets as Tifa's and holds a fingerprint of the connection, so a second run sees what is current.

With `front: cloudflare`, CI clears the CDN cache with the token setup keeps on the server, which the deploy reads over ssh: the repository holds no Cloudflare token and no zone ID.

**Access to GitHub.** Setup and remove read the repository and set and delete its secrets and variable. gh comes first: when it is installed and logged in (`gh auth login`), gh does it all. Without it, Tifa calls the GitHub REST API with a fine-grained personal access token from the input `TIFA_DEPLOY_GITHUB_TOKEN` (the environment, else `.env`). The repository is the one in the git remote of the app (`origin`, else another remote on github.com), and its default branch comes from the API. Secret values are sealed with the public key of the repository before they leave your machine, and the token is sent in the `Authorization` header only, never on a command line and never printed.

**Without gh: a fine-grained token.** Make a token limited to this one repository, with the repository permissions:

| Permission | Level | Why |
| --- | --- | --- |
| Secrets | Read and write | List, set and delete the secrets |
| Variables | Read and write | List, set and delete the variable `TIFA_DEPLOY_FINGERPRINT` |
| Metadata | Read | Comes with the two above |

Nothing else is needed. Put `TIFA_DEPLOY_GITHUB_TOKEN=<token>` in `.env`. When neither gh nor a token works, setup lists the GitHub part among what it cannot do and prints a link that opens the form with the name, the owner and these permissions filled in, `https://github.com/settings/personal-access-tokens/new?name=...&target_name=<owner>&secrets=write&actions_variables=write&metadata=read` (GitHub's [template URLs](https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/managing-your-personal-access-tokens)). The link cannot choose the repository: under Repository access pick Only select repositories and then the repository. A token that GitHub refuses (401) or that cannot reach the repository (404) says so and names these permissions.

Secret values go to GitHub through gh on its input, or sealed through the API: they are never printed, never written to a file and never on a command line. In the log of a run, `tifa deploy` and `rollback` first print `::add-mask::<host name or address>` so GitHub hides it, because an ssh error prints the address it resolved, which is not the exact text of the secret.

#### connect: direct

The default. CI logs in to the address and port of `TIFA_DEPLOY_HOST`. The port must then be open to GitHub's runners, whose addresses change, so you cannot allow only some. Allow key-only login (`PasswordAuthentication no`): the key of the repository is the only way in.

#### connect: cloudflare

For a server that opens no port for ssh, with or without CI. It needs `front: cloudflare` and the tunnel. Both CI and your own machine reach the server through it, so once it works you may close the ssh port of the server. Tifa never closes it: do that yourself, after a deploy through the tunnel worked. With your login of cf, setup:

1. Makes the Access application and its policies first, and only then adds the route `<ssh host> -> ssh://localhost:22` to the tunnel, keeping the other routes, and a proxied DNS record for it. When nothing can admit anyone yet (cf gives no email and there is no CI, or a policy cannot be made), setup publishes no route and no record, and says why: the ssh host name is never open to everyone. The ssh host is the domain with `-ssh-` and 12 random characters after its first label, because Cloudflare's free certificates cover one level of subdomain: `deploy-a.example.com` becomes `deploy-a-ssh-k3j9x2m5q7wt.example.com`, and the apex `example.com` becomes `ssh-k3j9x2m5q7wt.example.com`. The random part keeps the name from being guessed from the domain. Setup makes it once and reads it back on later runs from the route of the tunnel and the Access application it made, so it stays the same, with CI or without. A name from an earlier version, without the random part, shows in the plan as a replacement: the new route, record and Access application are added, `TIFA_DEPLOY_HOST` changes, and the old ones are removed.
2. Makes a self-hosted Access application for the ssh host, named `tifa deploy ssh <ssh host>`, and a policy `tifa-people-<ssh host>` that admits the email of your cf login (read with `cf auth whoami`, which only reads). Only that application and that policy are touched, nothing of the zone or the account. Run again, with the same email, it changes nothing. Another person who runs setup with their own cf login adds their email to the same policy, and the plan says so: `change  cloudflare: the Access application of <host> admits ana@example.com, and keeps dev@example.com`. A cf logged in with an API token has no email: without CI that is something for you to do, with CI it is only a note.
3. With the block `auto`, also makes a service token called `tifa-github-<owner>-<repo>` that does not expire, a policy that lets in only that token and asks for no login, and attaches it to the same application, before the policy of the people. Another repository for the same server adds its own token and policy. An app set up by an earlier version keeps its application and its token: setup only adds the policy of the people, and renews no token.
4. With CI, sets `TIFA_DEPLOY_CF_ACCESS_ID` and `TIFA_DEPLOY_CF_ACCESS_SECRET`, and `TIFA_DEPLOY_HOST` is the ssh host.

The workflow installs one fixed version of cloudflared from its release page and checks the SHA-256 of the package (the apt repository keeps only the newest, so it cannot pin a version, and a newer cloudflared once stopped reading service tokens). Tifa raises the version only after a deploy through the tunnel passed with it. It connects with `ProxyCommand cloudflared access ssh`, handing it the token in `TUNNEL_SERVICE_TOKEN_ID` and `TUNNEL_SERVICE_TOKEN_SECRET`.

**What setup does on your machine** (items of the plan, applied with your y or `--yes`, no terminal needed):

1. Installs cloudflared when it is not on the PATH: `brew install cloudflared` on macOS, `winget install --id Cloudflare.cloudflared -e` on Windows. On Linux, or without brew or winget, the item gives the command or the [download page](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/downloads/) and setup does not try. Setup never removes cloudflared.
2. Writes this block at the top of `~/.ssh/config` (`%USERPROFILE%\.ssh\config` on Windows), and makes the file with mode 600 when there is none:

   ```
   # tifa shop deploy-a-ssh-k3j9x2m5q7wt.example.com: through the Cloudflare tunnel (written by tifa deploy setup)
   Host deploy-a-ssh-k3j9x2m5q7wt.example.com
     ProxyCommand /opt/homebrew/bin/cloudflared access ssh --hostname %h
     StrictHostKeyChecking accept-new
   Match all
   # end tifa shop deploy-a-ssh-k3j9x2m5q7wt.example.com
   ```

   Every other line of the file stays as it is, with its line endings (a file with CRLF gets a block with CRLF). The block goes first, because ssh keeps the first value it finds, so a `Host *` of yours further down cannot replace the ProxyCommand. `Match all` ends the block, so an `Include` or a global option of yours at the top stays global. Setup writes the full path of cloudflared, in quotes when it has spaces, and on Windows with forward slashes (`C:/Tools/cloudflared.exe`), which both Windows OpenSSH and the ssh of Git for Windows read. The markers name the app and the ssh host and are matched whole: a comment of yours that only starts with `# tifa` is yours. A block that starts inside another, that never ends, or whose `Host` line differs from its first line is an error that names the line, and setup writes nothing. `StrictHostKeyChecking accept-new` is there because the first connection through the tunnel has no known host key and ssh cannot ask without a terminal. A key that changes later is refused. There is no `User` line: Tifa picks the user, root for the first setup and `tifa` after. A later run replaces only this block, and `tifa deploy remove` deletes it.
3. Checks that `ssh tifa@<ssh host> true` works through the block. **The first time, cloudflared opens the Cloudflare Access login in your browser and waits for you**, up to 4 minutes. Setup says so before it starts. cloudflared also prints the login address, which a coding agent can show you when no browser opened. Logging in is the only step that is yours. Your public key must also be on the server (the login of Access lets you reach sshd, and sshd still checks your key: see [More developers](#more-developers)).
4. Only then points `TIFA_DEPLOY_HOST` at the ssh host. The first setup of a fresh server still needs the address you gave, to log in as root before the tunnel exists, so the move comes last. When the value is a line of the `.env` of the app, setup changes that line (the plan shows `TIFA_DEPLOY_HOST in .env, vps -> <ssh host>`, and every other line of the file stays) and from then on `tifa deploy`, `logs`, `status` and `env` go through the tunnel. Only the value changes: an `export` in front, the quotes, a comment after it, the line endings and a missing final newline stay. The check runs whatever the source of `TIFA_DEPLOY_HOST` is. When the value comes from the environment or the flag `--host`, setup changes nothing, and after a check that passed it tells you what to set. If the check fails, nothing changes, setup says why, and the next run tries again. When it comes from `~/.tifa`, setup records the ssh host there itself, once the check passed. Setup says your key must be in `/srv/tifa/.ssh/authorized_keys` only when ssh got to authentication (its output has `Permission denied`). Any other failure shows what ssh and cloudflared printed and says the check failed, without blaming the key.

A server you reach by its ssh host name, with this machine not set up yet (another person, a new checkout), needs the block before setup can read anything. Setup plans that first: the Access policy, cloudflared and the block, then asks again for the rest. Without a login of cf it says to log in first (on a terminal it logs cf in for you), and does not try to read the server.

Switching `connect` back to `direct` while `TIFA_DEPLOY_HOST` is the ssh host name stops with an item that says to set it to the address of the server first (its ssh port must be open to you). The next setup then removes the route, the record, the Access application, its policies and the block of your ssh config.

#### connect: tailscale

For a server that is on your tailnet and reached by GitHub Actions through it. Setup does nothing about Tailscale on your machine, and only the runner joins the tailnet for CI. It needs the block `auto`, and works with either `front`: visitors may come through Cloudflare while ssh goes over the tailnet.

You set up Tailscale yourself, on the server and in your tailnet: Tifa installs and changes nothing of it. `TIFA_DEPLOY_HOST` must be an address the runner can reach on the tailnet, such as the MagicDNS name of the server (`ssh://vps.tail1234.ts.net:22`). Tifa uses ssh to it as for any other server, and setup copies the same value, with the port, into the secret `TIFA_DEPLOY_HOST` of CI.

**CI** joins the tailnet with `tailscale/github-action@v4` and workload identity federation. GitHub gives the runner a short-lived token that says which repository it is, and Tailscale trades it for a key that lets the runner join with the tag `tag:ci`. No long-lived Tailscale secret exists. The workflow has:

```yaml
permissions:
  contents: read
  id-token: write
...
      - name: Connect to Tailscale
        uses: tailscale/github-action@v4
        with:
          oauth-client-id: ${{ vars.TIFA_DEPLOY_TAILSCALE_ID }}
          audience: ${{ vars.TIFA_DEPLOY_TAILSCALE_AUDIENCE }}
          tags: tag:ci
```

The client ID and the audience are not secret, so setup keeps them as the repository variables `TIFA_DEPLOY_TAILSCALE_ID` and `TIFA_DEPLOY_TAILSCALE_AUDIENCE`. `id-token: write` is only what lets the action ask GitHub for the token. Only the Tailscale admin console can make the identity, because Tifa holds no Tailscale login that may. In the console, setup says to:

1. Open [Trust credentials](https://console.tailscale.com/admin/settings/trust-credentials), choose Credential, then OpenID Connect.
2. Pick the GitHub issuer (`https://token.actions.githubusercontent.com`) and set the subject to `repo:<owner>/<repo>:ref:refs/heads/<branch>`, with the branch the deploy job runs with. A job that starts after `needs` passed (`workflow_run`) has the default branch of the repository as its ref, whatever branch deploys, so that is the branch in the subject. With `needs: none` the job starts on a push, and has the ref of `auto.branch`. Setup prints the subject for your settings ([`GITHUB_REF` for `workflow_run`](https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows)). A manual run on another branch is refused.

   That is the form of a repository made before July 15, 2026. GitHub gives a repository made after that date, one that opted in to immutable subject claims, and one renamed or transferred after the date, a subject with the IDs of the owner and the repository: `repo:<owner>@<owner ID>/<repo>@<repository ID>:ref:refs/heads/<branch>` ([GitHub: immutable subject claims](https://docs.github.com/en/actions/reference/security/oidc)). No field tells for sure which form a repository gets, so setup prints both. To see which one GitHub sends, run [`github/actions-oidc-debugger`](https://github.com/github/actions-oidc-debugger) in a workflow and read `sub`, or read `use_immutable_subject` at `GET /repos/<owner>/<repo>/actions/oidc/customization/sub` ([REST: OIDC](https://docs.github.com/en/rest/actions/oidc)), which says whether the repository opted in. The IDs are `gh api repos/<owner>/<repo> --jq '.owner.id, .id'`. Tailscale lets a subject contain `*`, which matches any characters ([workload identity federation](https://tailscale.com/kb/1581/workload-identity-federation)), so `repo:<owner>/<repo>:*` and its ID form would cover every ref. That also admits every branch and pull request of the repository, and Tifa does not rely on it: use it only if you accept that.
3. Give it the scope `auth_keys` for the tag `tag:ci`, and generate it.
4. Copy its client ID, and run `go tool tifa deploy setup --tailscale-client-id <client ID>`, or on a terminal paste it when setup asks. The client ID is not secret, so an agent may pass it. The audience is made from it (`api.tailscale.com/<client ID>`).

Your tailnet policy must have `tag:ci` in `tagOwners` and let it reach the server on its ssh port. Setup cannot read your policy, so it cannot check that. If a run cannot join, the log of the Tailscale step says so (`requested tags [tag:ci] are invalid or not permitted` means the tag or the credential's tags are wrong).

In Actions, `tifa deploy` and `rollback` write a short summary to the run page when the variable `GITHUB_STEP_SUMMARY` is set: the app, release, commit, time, the health check and whether the cache was cleared.

### The servers Tifa remembers

After a setup that worked, Tifa writes the server in `~/.tifa/config.yaml` (`%USERPROFILE%\.tifa\config.yaml` on Windows), so the next app on this machine needs no `TIFA_DEPLOY_HOST`. The file holds no secret, only the servers this machine has set up:

```yaml
# Servers tifa deploy setup has set up from this machine. tifa deploy uses the only one when an app names none.
# No secrets are kept here. To forget a server, delete its entry.
servers:
  - id: vultr-1a2b3c4d
    host: ssh://203.0.113.7:22
    apps:
      - {name: shop, path: /Users/me/shop, host: 'ssh://203.0.113.7:22'}
      - {name: blog, path: /Users/me/blog, host: 'ssh://203.0.113.7:22'}
```

- `id` is the host name of the server and the start of its machine ID, which setup reads from it. A server keeps its entry when the way to reach it changes. `host` is the one a new app uses: the one that worked last.
- An app is the folder of its checkout on this machine (`path`), and `name` is only for messages. Another checkout of an app with the same name counts as a new app. Each app keeps the host that worked for it: with `connect: cloudflare` that is the ssh host name of the app's domain, once the check through the tunnel passed, else the address you gave. `remove` moves the `host` of the server to an app that stays when it was the host of the app that went.
- When an app names no server (flag, environment and `.env` come first), Tifa uses the only server in the file and prints one line that says which one, and what to put in `.env` to name another. An app whose folder is listed under one server goes to that server, even when there are several. With several servers and an app that is on none, the command stops, lists them and says to set `TIFA_DEPLOY_HOST=<host>` in `.env` or pass `--host`. The plan of `setup` and `remove` shows the file as the source of the server.
- A host taken from the file is checked in the first thing the command asks the server: if the machine there is not the one in the entry (an alias or an address that now points elsewhere), the command stops and names both IDs and the host. `tifa deploy logs` asks nothing first, so it does not check.
- `tifa deploy remove` takes the app off its entry, and the entry goes with its last app. It does so only when remove finished with nothing left for you, so a retry finds the same server.
- `TIFA_HOME` moves the folder. The file is mode 0600 in a folder of mode 0700, written through a temporary file and a rename, and a lock file next to it keeps two Tifa commands from losing each other's entry. A comment you add stays. A server listed twice (same `id`) is an error that names the file.
- To forget a server by hand, delete its entry, or the whole file. Nothing else depends on it. CI never reads it: in GitHub Actions a missing `TIFA_DEPLOY_HOST` is an error about the secret, and the workflow gets its server from that secret.

### More developers

Every machine that deploys needs its own access to the server, and setup does not carry it over. From a machine that can log in, add the public key of the new machine to the user `tifa` (with `connect: cloudflare`, use the ssh host name, which goes through the tunnel):

```sh
ssh-copy-id -i ~/.ssh/id_ed25519.pub tifa@vps
```

With `connect: cloudflare`, the new person also runs `go tool tifa deploy setup` with their own cf login, with `TIFA_DEPLOY_HOST=<ssh host>` in their `.env`: setup adds their email to the Access policy, installs cloudflared and writes the block of their `~/.ssh/config`, and the first ssh opens the Access login in their browser. The ssh host name is the one in the `.env` of the first person, or the one the plan of their setup printed. The new machine also needs to know where the server is: put `TIFA_DEPLOY_HOST=` in the `.env` of its checkout (or pass `--host`, or run setup once, which remembers the server in `~/.tifa`), and `TIFA_DEPLOY_GITHUB_TOKEN=` there too when it runs setup without gh, and when the value is an alias, the same alias in its `~/.ssh/config`. Nothing else is stored on a machine: `tifa.yaml` is in the repository and the rest lives on the server, in Cloudflare and in GitHub.

### Settings and secrets on the server

```sh
printf %s "$VALUE" | go tool tifa deploy env set API_KEY   # or on a terminal for a hidden prompt, or else a page
go tool tifa deploy env unset API_KEY
go tool tifa deploy env list                               # names only
```

These edit `/srv/tifa/<app>/shared/.env` over ssh as `tifa`, the file the app reads from the folder it runs in (see [.env](config.md#env)). The value is read from a hidden prompt on a terminal, else from a pipe or a file on the standard input (an empty one sets an empty value), never from an argument, so it stays out of your shell history and the process list. Nothing prints a value. The file keeps mode 600, and your other lines and comments stay as they are. After a change the app restarts through its socket and must pass the health check of a deploy. If it does not, the old file comes back, the app restarts again, and the command says so and exits with 1. The file is read by the app as configuration only: it is not put into the environment of the process or of systemd, so each app on a server keeps its own.

Without a terminal, a pipe or a file, such as the `/dev/null` a coding agent usually gives, `env set` asks on a page (see below).

### Secrets without a terminal

A coding agent runs commands without a terminal, and a secret must not pass through it: not its chat, its input or its output. So Tifa asks for the secret on a page in your browser:

- `tifa deploy env set NAME`, when its standard input is not a terminal, a pipe or a file.
- `tifa deploy setup --cache-purge-token`, with `--yes` too. The flag is the ask: without it, setup without a terminal lists the token among what you need to do, with the command, and never waits.

Tifa opens the page and prints its URL, `http://127.0.0.1:<port>/<code>`, so the agent can pass it on when no browser opened. The page says what is needed and links to where you make it. The page listens on this machine only, answers only at the random code in its URL, takes one value and closes. When nobody sends it within 10 minutes, the command stops with an error: run it again for a new page.

### Logs, status and remove

```sh
go tool tifa deploy logs        # the last 100 lines of the log, -n 500 for more
go tool tifa deploy logs -f     # and then the new ones, until Ctrl-C
go tool tifa deploy status
go tool tifa deploy remove
```

They log in over ssh as `tifa` and find the server the way `tifa deploy` does.

`logs` prints the journal of `tifa-<app>.service` and `tifa-<app>.socket`: what the app printed, and what systemd said about it, such as a start, an exit or a failure. journald keeps these lines in a journal file of the user `tifa` when it keeps the journal on disk, the default of Ubuntu and Debian. A server that keeps its journal in memory only shows `tifa` no lines. What the app logs, how to set the format and level, and how long journald keeps the lines are in [Logs](../guides/logs.md).

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

- On the server, as `tifa`: it stops and disables `tifa-<app>.socket`, `tifa-<app>-http.socket` and `.service`, and deletes their unit files and `/srv/tifa/<app>` with its releases, `shared/.env`, the data in `shared/`, the certificates in `certs/` and `deploys.log`. Copy the data first if you want to keep it. For an app that served visitors itself, as root, `/etc/sysctl.d/60-tifa-ports.conf`, and the first port users may open goes back to what it was, and with ufw the rules with the comment `tifa <app>` that setup added, which the plan lists. Other apps, the user `tifa`, the other rules of the firewall and sshd stay as they are.
- With `front: cloudflare`: the route, the DNS record and the rules of the domain, and with `connect: cloudflare`, with or without CI, the Access application of the ssh host name with its people policy, its route and its DNS record, and the service token and policy of the repository. Everything that needs ssh runs first (the app, the cache tokens), then the stop of the tunnel, and only then the ssh host name goes: the route and record before the application and its policies, so a partial failure never leaves a route without Access, and only when the steps before it worked. A run again finds the ssh host name from the route of the tunnel when the application is already gone. When your ssh goes through the tunnel, the last app of a server stops the tunnel a few seconds after ssh left, so the command does not cut its own connection. The tunnel and cloudflared serve every app of the server, so they go only with the last app of Tifa there: then remove stops cloudflared as root, deletes `/etc/tifa`, uninstalls cloudflared when setup installed it, and deletes the tunnel.
- The cache tokens on the server, `/srv/tifa/.config/tifa`, serve every app of the server too, and go with the last app of Tifa there. Remove looks for apps again under the lock of the file when it deletes, so an app set up while remove waited for your answer keeps them. Delete the token `tifa cache purge <zone>` in the Cloudflare dashboard yourself: Tifa cannot.
- On your machine: the block of the app in `~/.ssh/config`, last, so a run that stopped halfway can still reach the server. Without a login of cf, when you reach the server by the ssh host name, the block stays and remove says so. cloudflared stays installed, and `TIFA_DEPLOY_HOST` in `.env` stays as it is.
- With the block `auto`: the key of the repository in `authorized_keys`, and the secrets and the variables that setup set (`TIFA_DEPLOY_FINGERPRINT` and the two of the Tailscale identity). The identity itself is in your Tailscale console: delete it there. `.github/workflows/tifa-deploy.yml` is a file of your repository, so it stays: until you delete it, each push fails to deploy.

Like setup, remove prints a plan first. On a terminal it then asks you to type the name of the app, also with `--yes`. Without a terminal it removes nothing unless you pass `--yes`. What it cannot remove, without a login of cf, or gh or a token, is listed last with the login it needs. It removes the rest and exits with an error: log in and run it again. A step that fails does not stop the others, but the tunnel then stays until a run again has removed the rest. A run that finds nothing left says so in one line.

### On the server

- The log of an app: `go tool tifa deploy logs`, or `journalctl --user -u tifa-<app>` as `tifa` (`sudo -iu tifa`).
- Start, stop or look at it: `systemctl --user status tifa-<app>.service` as `tifa`. Restart the service, never the socket.
- The apps start at boot, before the first visitor.
- Several apps: each has its own folder, port and units. Run `tifa deploy` in each, after one `tifa deploy setup`. Only one of them can serve visitors itself on ports 80 and 443: the others go behind the Cloudflare Tunnel.

The rest of this page is the way to do it by hand, and what `tifa deploy` does underneath.

## Build for the server

The server render engine runs on linux and darwin, on amd64 and arm64, and windows on amd64 and arm64. Build on your machine or in CI with the target set, and copy the file over. Do not build on a small server such as a 1 GB VPS: the build needs more memory than the running app, and it can run out.

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
- SIGINT and SIGTERM stop it gracefully: new connections stop, requests in flight get `server.shutdown_timeout` to finish, then `OnShutdown` runs. A stream with no end of its own, such as server-sent events, returns at once when it watches `tifa.ShuttingDown` (see [Streaming](../guides/streaming.md#when-the-program-stops)). A systemd unit or a container needs nothing more than the executable, the environment and a writable data folder.
- A `FROM scratch` container works. The program holds the time zone data its renders need.
- Size the machine with [Performance](../reference/performance.md#sizing-a-machine). `ssr.vms` follows the CPU limit of a container, up to 4 VMs. On more CPUs, set it.

## Restart without dropping requests

A restart stops the program, and the next one prewarms its VMs before it listens. A connection that arrives in between is refused, unless systemd holds the socket: with socket activation, systemd listens on the port and hands the socket to the program, and keeps it open while the program restarts. The system takes the connections that arrive meanwhile, and the next program answers them. A visitor only waits a little longer: on an M4 Mac, the program of the example shop answers its first page 0.42 seconds after it starts with 10 VMs, and 0.27 seconds with `ssr.vms: 1`. The first start after a build, with the file not yet in the cache of the system, took 0.79 seconds. On a Linux machine with systemd 261, the 300 requests a small app got every 10 ms while `systemctl restart` ran all answered 200, the slowest in 0.14 seconds.

Users stay signed in across the restart only when sessions are kept outside the program. The default store lives in memory, so every restart signs everyone out (see [Where to store sessions](../pages-and-data/sessions.md#where-to-store-sessions)). An app from `tifa new` keeps them in its SQLite database (`sessions.go`).

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
  - The files in `public/`: `no-cache` with an ETag, so the CDN asks and gets a 304. They keep fixed names, so only files that need one belong there. Import the rest to get `/assets/` (see [Static files](../start/app.md#static-files)).
  - Pages with `Cache`: they are anonymous and carry a `public` `Cache-Control` with an `s-maxage` (the exact value is in [Cache-Control of an answer](../pages-and-data/ssr-and-cache.md#cache-control-of-an-answer)). A CDN that follows the origin's `Cache-Control` keeps a page for the time of `Cache` and no longer. See [Cache for CDNs](../pages-and-data/ssr-and-cache.md#cache-for-cdns).
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

`tifa deploy setup` with cf adds these rules for its domain itself (see [Set up with a plan](#set-up-with-a-plan)).

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

## Health checks and metrics

Tifa has no health route or metrics route of its own. Add the ones you need as API routes.

### Health check

A `GET` that answers 204 while the program can serve. Check what it cannot work without, such as the database of [the database guide](../guides/database.md), and answer 503 when that is down:

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
| `tifa_renders_rejected_total` grows | Renders found the queue full or waited past `ssr.wait_timeout`, and got 503 | More CPU and `ssr.vms`, or fewer renders (see [Sizing a machine](../reference/performance.md#sizing-a-machine) and [Render queue](config.md#render-queue)) |
| `tifa_vms_busy` is `tifa_vms` most of the time, `tifa_renders_queued` above 0 | Every VM renders and others wait | The same: the program is at its limit |
| `rate(tifa_render_seconds_total[5m]) / rate(tifa_renders_total[5m])` | The mean time of a render | When it grows, find the page that got slower |
| `tifa_renders_failed_total` grows | Renders throw, run past `ssr.timeout` or use up `ssr.vm_memory` | The log has each failure |
| `tifa_vms_memory_bytes` keeps climbing, or `tifa_vms_replaced_total` grows | The VMs hold more after each collection. A VM that holds more than half of `ssr.vm_memory` is replaced | Find what the pages keep, or the leak in the app. If VMs are replaced again within minutes, raise `ssr.vm_memory` (see [Memory](../reference/performance.md#memory)) |
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

