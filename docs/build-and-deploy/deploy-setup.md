---
summary: 'Set up the server, Cloudflare and GitHub with `tifa deploy setup`: the plan, what each `front` changes, running setup without gh'
audience: app
order: 11
read_when:
  - Running tifa deploy setup or remove on a machine without gh, with a fine-grained token in TIFA_DEPLOY_GITHUB_TOKEN
  - A deployed app with `front: none` does not answer from outside, setup stopped because port 80 or 443 is taken, you want to know which ports to open, or what setup and remove change in ufw
---

# Set up the server

`tifa deploy setup` makes the server, Cloudflare and GitHub match `tifa.yaml`. It prints a plan first and changes nothing until you agree.

## Set up with a plan

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

Setup removes only what Tifa made and marked (it cannot delete the Cloudflare token you made: the plan says to delete it in the dashboard): the lines in `authorized_keys` with the comment `tifa-github <owner>/<repo>`, the nginx site `/etc/nginx/conf.d/tifa-<app>.conf` and the certificate `tifa-<app>` of certbot, the ufw rules with the comment `tifa <app>` or `tifa nginx front`, Cloudflare objects whose names start with `tifa` (and the Access application `tifa deploy ssh <host>`), the block that starts with `# tifa <app>:` in your `~/.ssh/config`, the route and DNS record of a domain it added, the repository variables `TIFA_DEPLOY_FINGERPRINT`, `TIFA_DEPLOY_TAILSCALE_ID` and `TIFA_DEPLOY_TAILSCALE_AUDIENCE` and the secrets set with it (`TIFA_DEPLOY_HOST`, `TIFA_DEPLOY_KNOWN_HOSTS` and the others), and the workflow file that starts with Tifa's line. Set `front: none`, or take the block `auto` out, and the plan lists those removals before anything goes.

The layers of the plan:

| Layer | What | Needs |
| --- | --- | --- |
| server | The user `tifa`, which owns and runs every app of the server and may log in with the keys of root (home `/srv/tifa`), and linger, so its systemd runs at boot | `ssh root@<TIFA_DEPLOY_HOST>`, the first time only |
| app | `/srv/tifa/<app>`, its empty `shared/.env`, the data folder `shared`, its port in `port` (80, or 443 with HTTPS, for `front: none`, else the first free one from 9527 on the loopback), and the user units `tifa-<app>.socket` and `.service`, with HTTPS also `tifa-<app>-http.socket`. For `front: none`, once per server as root, the setting that lets the systemd of `tifa` open ports 80 and 443 | ssh as `tifa`, and root once |
| front: cloudflare | The Cloudflare Tunnel, cloudflared on the server, the route, the proxied DNS record and the cache rules of the host of `site.url`, and on the server the token that clears its cache | cf |
| front: nginx | As root: nginx, and for an app with a domain certbot with its nginx plugin, installed with apt, `/etc/tifa/nginx` to mark that nginx as Tifa's, the site of the app in `/etc/nginx/conf.d/tifa-<app>.conf`, its certificate, and ports 80 and 443 in ufw (see [Behind nginx](deploy-proxy.md#behind-nginx)) | `ssh root@<TIFA_DEPLOY_HOST>` |
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
6. Adds rules that hold for that host name only, as [CDN settings](deploy-proxy.md#cdn-settings) says: a cache rule that caches what the `Cache-Control` of the app allows and nothing without one, a cache rule that never caches `/__tifa/` or navigation data (`?_tifa=`), and a configuration rule that turns off Rocket Loader, Email Address Obfuscation, Cloudflare Fonts, Zaraz and the Web Analytics script, which write into the HTML. Settings of the whole zone are not changed.
7. Asks for a token, which is optional, that can only clear the cache of the zone, and keeps it with the ID of the zone on the server, where every deploy finds it (see [Clearing the CDN cache](deploy-cloudflare.md#clearing-the-cdn-cache)). A login of cf cannot make API tokens, so you make it in the Cloudflare dashboard, and on a terminal setup walks you through it: it prints a link to the Cloudflare form with the permission Cache Purge filled in and the name `tifa cache purge <zone>`. Set Zone Resources to that zone only, create the token and paste it at the hidden prompt. Setup checks it by clearing a URL that does not exist, tries again up to 3 times, and writes it to the server over ssh as `tifa`, on the input of ssh. An empty line skips it, and setup goes on and succeeds. Setup asks on the first setup of the domain, and on the first setup of CI. Once the server has a token for the zone, setup checks it the same way on each run and asks nothing while Cloudflare takes it: a second app on the same server and zone needs nothing. A token Cloudflare refuses, such as one that expired or was deleted, is asked for again. Without a token, later runs with `auto`, and a first run without a terminal, list it as optional. To add the token later, or to replace it, run `go tool tifa deploy setup --cache-purge-token`: it asks even when the server has one, on a terminal at the hidden prompt and without one on a page in your browser, with `--yes` too (see [Secrets without a terminal](deploy-operate.md#secrets-without-a-terminal)).

`tifa deploy remove` of the last app on a server removes the tunnel and the cache token too (see [Logs, status and remove](deploy-operate.md#logs-status-and-remove)). To remove them by hand, run `systemctl disable --now tifa-tunnel tifa-tunnel-upgrade.path` and delete `/etc/tifa`, `/etc/systemd/system/tifa-tunnel*` and `/srv/tifa/.config/tifa` on the server, then delete the tunnel and the token `tifa cache purge <zone>` in Cloudflare. cloudflared stays installed: `apt-get purge cloudflared` and deleting `/etc/apt/sources.list.d/cloudflared.list`, `/usr/share/keyrings/cloudflare-main.gpg` and `/etc/apt/apt.conf.d/52tifa-cloudflared` remove it too.

### The app serves visitors itself

With `front: none` the program meets visitors directly, with no nginx, Caddy or certbot in front:

- **With a domain**, the host of `site.url` such as `https://shop.example.com`: systemd opens ports 443 and 80 for the app, and the app runs its [built-in HTTPS](https.md#https). Setup writes `SERVER_TLS_DOMAINS=<domain>` and `SERVER_TLS_CACHE=%h/<app>/certs` into the environment of the service, so the program gets its certificate from Let's Encrypt and keeps it in `/srv/tifa/<app>/certs`, which every release shares. Port 80 answers the checks of Let's Encrypt and sends everything else to HTTPS. DNS must point the domain at the server before the first deploy: until the program has its certificate it answers no HTTPS, and the health check of the deploy fails and says so.
- **Without a domain**: no `site.url`, or its host is an IP address or `localhost`. The app serves plain HTTP on port 80.

systemd holds the ports, so the app runs as `tifa` with no root and no capability, and restarts without refusing a connection. A user's systemd may only open ports from 1024 on Linux, so setup, once per server and as root, writes `net.ipv4.ip_unprivileged_port_start = 80` to `/etc/sysctl.d/60-tifa-ports.conf`. Ports from 80 to 1023 are then open to every user of the server, not only root. `tifa deploy remove` of the app deletes the file and puts back the value from before.

**One app per server.** Only one program can listen on a port, so only one app on a server can serve visitors itself. Before it changes anything, setup looks at what listens on the ports the app needs (80, and 443 with a domain): `ss -ltnp`, and the sockets the systemd of `tifa` holds. When another app of Tifa holds them, or a program such as nginx, Caddy or Apache, setup stops, names it and the port, and changes nothing. Stop that program and keep it from starting at boot, remove the other app, or use `front: cloudflare` or `front: nginx` for this app and the other, where several apps share a server (see [Behind nginx](deploy-proxy.md#behind-nginx)).

**ufw.** When ufw is on and does not let visitors reach the ports the app needs, 80 and 443, or 80 without a domain, the plan of setup allows them:

```
  change  firewall: allow ports 80 and 443 (tcp) in ufw, so visitors reach shop, in rules with the comment 'tifa shop', as root
```

Once you agree, or with `--yes`, setup runs `ufw allow 80/tcp comment 'tifa <app>'` as root for each of those ports. Nothing changes in ufw when it is off or not installed, when a rule of yours already allows the port, or when its default lets everything in. A rule of yours that denies the port stays: setup says which and how to delete it. The comment `tifa <app>` marks the rules as Tifa's: `tifa deploy remove` deletes them, and so does setup once the app no longer serves visitors on a port, such as after a move to `front: cloudflare`. No other rule is ever touched, the one of ssh among them. Only root can read the rules of ufw: when root cannot log in, setup says what to check on the server instead.

**The firewall of your provider is yours.** Tifa does not change the firewall or the security groups of your provider, nor a firewall on the server that is not ufw (firewalld, or rules of nftables or iptables that ufw did not write). The plan says which ports visitors need, and so does the end of setup: open them there if they block them. After a deploy, Tifa asks the app on those ports from your machine, and says when one did not answer.

A server set up with an earlier version has the app on the loopback. The next `tifa deploy setup` plans the move: the setting of the ports as root, then the new sockets, after which the app restarts on them and must pass the health check of a deploy.

Set up the CDN, if any, as [CDN settings](deploy-proxy.md#cdn-settings) says, and clear its cache on each deploy as [After a deploy](deploy-by-hand.md#after-a-deploy) says.
