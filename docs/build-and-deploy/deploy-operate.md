---
summary: 'Run a deployed app: `tifa deploy env`, secrets without a terminal, `logs`, `status`, `remove`, and where an app keeps its files on the server'
audience: app
order: 15
read_when:
  - Setting a secret or a setting in the .env of a deployed app, with tifa deploy env
  - An agent runs tifa deploy env set, or tifa deploy setup with --cache-purge-token, and a page opened in the browser to type the secret
  - Reading the log of a deployed app, checking which release runs and whether it is healthy, or taking an app off its server and out of Cloudflare and GitHub, with tifa deploy logs, status or remove
  - Putting several apps on one server, or finding the log, the .env or the data of a deployed app
---

# Run a deployed app

Set a secret, read the log, see which release runs, take an app off a server, and find the files of an app on it.

## Settings and secrets on the server

```sh
printf %s "$VALUE" | go tool tifa deploy env set API_KEY   # or on a terminal for a hidden prompt, or else a page
go tool tifa deploy env unset API_KEY
go tool tifa deploy env list                               # names only
```

These edit `/srv/tifa/<app>/shared/.env` over ssh as `tifa`, the file the app reads from the folder it runs in (see [.env](config.md#env)). The value is read from a hidden prompt on a terminal, else from a pipe or a file on the standard input (an empty one sets an empty value), never from an argument, so it stays out of your shell history and the process list. Nothing prints a value. The file keeps mode 600, and your other lines and comments stay as they are. After a change the app restarts through its socket and must pass the health check of a deploy. If it does not, the old file comes back, the app restarts again, and the command says so and exits with 1. The file is read by the app as configuration only: it is not put into the environment of the process or of systemd, so each app on a server keeps its own.

Without a terminal, a pipe or a file, such as the `/dev/null` a coding agent usually gives, `env set` asks on a page (see below).

## Secrets without a terminal

A coding agent runs commands without a terminal, and a secret must not pass through it: not its chat, its input or its output. So Tifa asks for the secret on a page in your browser:

- `tifa deploy env set NAME`, when its standard input is not a terminal, a pipe or a file.
- `tifa deploy setup --cache-purge-token`, with `--yes` too. The flag is the ask: without it, setup without a terminal lists the token among what you need to do, with the command, and never waits.

Tifa opens the page and prints its URL, `http://127.0.0.1:<port>/<code>`, so the agent can pass it on when no browser opened. The page says what is needed and links to where you make it. The page listens on this machine only, answers only at the random code in its URL, takes one value and closes. When nobody sends it within 10 minutes, the command stops with an error: run it again for a new page.

## Logs, status and remove

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

- With `front: nginx`: as root, the site of the app in nginx, with `nginx -t` before the reload, its certificate in certbot, and, with the last app of the nginx, nginx, certbot (when setup installed it), their configuration, `/etc/tifa/nginx` and the ufw rules with the comment `tifa nginx front` (see [Behind nginx](deploy-proxy.md#behind-nginx)). The certificate and nginx stay while the site cannot be taken out. The other apps keep answering.
- On the server, as `tifa`: it stops and disables `tifa-<app>.socket`, `tifa-<app>-http.socket` and `.service`, and deletes their unit files and `/srv/tifa/<app>` with its releases, `shared/.env`, the data in `shared/`, the certificates in `certs/` and `deploys.log`. Copy the data first if you want to keep it. For an app that served visitors itself, as root, `/etc/sysctl.d/60-tifa-ports.conf`, and the first port users may open goes back to what it was, and with ufw the rules with the comment `tifa <app>` that setup added, which the plan lists. Other apps, the user `tifa`, the other rules of the firewall and sshd stay as they are.
- With `front: cloudflare`: the route, the DNS record and the rules of the domain, and with `connect: cloudflare`, with or without CI, the Access application of the ssh host name with its people policy, its route and its DNS record, and the service token and policy of the repository. Everything that needs ssh runs first (the app, the cache tokens), then the stop of the tunnel, and only then the ssh host name goes: the route and record before the application and its policies, so a partial failure never leaves a route without Access, and only when the steps before it worked. A run again finds the ssh host name from the route of the tunnel when the application is already gone. When your ssh goes through the tunnel, the last app of a server stops the tunnel a few seconds after ssh left, so the command does not cut its own connection. The tunnel and cloudflared serve every app of the server, so they go only with the last app of Tifa there: then remove stops cloudflared as root, deletes `/etc/tifa`, uninstalls cloudflared when setup installed it, and deletes the tunnel.
- The cache tokens on the server, `/srv/tifa/.config/tifa`, serve every app of the server too, and go with the last app of Tifa there. Remove looks for apps again under the lock of the file when it deletes, so an app set up while remove waited for your answer keeps them. Delete the token `tifa cache purge <zone>` in the Cloudflare dashboard yourself: Tifa cannot.
- On your machine: the block of the app in `~/.ssh/config`, last, so a run that stopped halfway can still reach the server. Without a login of cf, when you reach the server by the ssh host name, the block stays and remove says so. cloudflared stays installed, and `TIFA_DEPLOY_HOST` in `.env` stays as it is.
- With the block `auto`: the key of the repository in `authorized_keys`, and the secrets and the variables that setup set (`TIFA_DEPLOY_FINGERPRINT` and the two of the Tailscale identity). The identity itself is in your Tailscale console: delete it there. `.github/workflows/tifa-deploy.yml` is a file of your repository, so it stays: until you delete it, each push fails to deploy.

Like setup, remove prints a plan first. On a terminal it then asks you to type the name of the app, also with `--yes`. Without a terminal it removes nothing unless you pass `--yes`. What it cannot remove, without a login of cf, or gh or a token, is listed last with the login it needs. It removes the rest and exits with an error: log in and run it again. A step that fails does not stop the others, but the tunnel then stays until a run again has removed the rest. A run that finds nothing left says so in one line.

## On the server

- The log of an app: `go tool tifa deploy logs`, or `journalctl --user -u tifa-<app>` as `tifa` (`sudo -iu tifa`).
- Start, stop or look at it: `systemctl --user status tifa-<app>.service` as `tifa`. Restart the service, never the socket.
- The apps start at boot, before the first visitor.
- Several apps: each has its own folder, port and units. Run `tifa deploy` in each, after one `tifa deploy setup`. Only one of them can serve visitors itself on ports 80 and 443: the others go behind the Cloudflare Tunnel or nginx.

[Deploy by hand](deploy-by-hand.md) is the way to do it by hand, and what `tifa deploy` does underneath.
