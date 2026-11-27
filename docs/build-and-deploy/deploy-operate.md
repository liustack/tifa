---
summary: 'Run a deployed app: `tifa deploy env`, secrets without a terminal, `logs`, `status`, `run`, `remove`, and where an app keeps its files on the server'
audience: app
order: 15
read_when:
  - Setting a secret or a setting in the .env of a deployed app, with tifa deploy env
  - An agent runs tifa deploy env set, or tifa deploy setup with --cache-purge-token, and a page opened in the browser to type the secret
  - Reading the log of a deployed app, checking which release runs and whether it is healthy, or taking an app off its server and out of Cloudflare and GitHub, with tifa deploy logs, status or remove
  - Running a command of the app on the server, such as one that changes data once, with tifa deploy run
  - Putting several apps on one server, or finding the log, the .env or the data of a deployed app
---

# Run a deployed app

Set a secret, read the log, see which release runs, run a command of the app, take an app off a server, and find the files of an app on it.

An app on several servers gets each command on every server, except `run`, which goes to the first one, and `logs -f`, which follows the one you name with `--host`: see [The other commands](deploy-several-servers.md#the-other-commands).

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
go tool tifa deploy status --all   # every server of ~/.tifa/config.yaml, once
go tool tifa deploy status --json  # this app, as JSON
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

`status --all` needs no app checkout. It looks once at every server in `~/.tifa/config.yaml` and prints, for each: the system, CPUs, memory, disk, uptime and load, and then every app of the user `tifa` on it, also the apps set up from another machine. For each app it shows the release and its commit, whether it passed its health check, the service and how many times systemd restarted it by itself, what `GET /healthz` answers now, and the memory of the program. An app with no release says it is not deployed yet.

```
vps
  system    Ubuntu 24.04.1 LTS, x86_64, 2 CPUs
  memory    2.1 GB total, 1.0 GB available
  disk      12.3 GB used of 25.0 GB (49%)
  uptime    up 12d 6h, load 0.10 0.20 0.30

  shop
    release   20261120T120000Z  commit ccc333  passed its health check
    service   active since 2026-11-20 12:00:05 UTC, 2h3m ago, 0 restarts
    health    GET /healthz answered 200
    memory    38.7 MB

Only the servers this machine has set up or deployed to, the ones in ~/.tifa/config.yaml, are shown.
```

- Servers are asked in parallel and printed in the order of the file, the apps of a server by name. A server that does not answer prints its error, and the others still print. A host that now answers as another machine is reported as that, and its apps are not shown.
- The exit status is 0 when every server answers and every deployed app is active with a passing health check. Otherwise everything is printed and the command exits with 1, and its message says how many servers could not be checked and how many apps are not well. A script or an agent can rely on that.
- `--json` prints the same as one JSON document on stdout: a list of servers. A server has `name`, `id`, `host`, `reached`, `error` (when it was not reached) and `apps`, and `facts` when it was reached: `os`, `arch`, `cpus`, `memoryTotalKiB`, `memoryAvailableKiB`, `diskTotalKiB`, `diskUsedKiB` (the disk of the home of `tifa`), `uptimeSeconds`, `load1`, `load5` and `load15`. A number the server did not say is 0. An app has `name`, `host`, `deployed`, `release`, `commit`, `checkPassed`, `service`, `activeSince`, `restarts`, `health` (the status of `GET /healthz`, empty while the service does not run, `000` for no answer), `memoryKiB`, `ok` and `error`.
- It is a look at one moment, not monitoring: it does not refresh, alert or keep a history. It covers only the servers in that file, which are the ones this machine has set up or deployed to. For alerts, use a probe outside the server, such as UptimeRobot or the health checks of Cloudflare, since a probe on the server dies with the server.
- `--all` takes no `--host` and no `--dir`.

`status --json` without `--all` prints one JSON object for the app of the folder, for a script or an agent. It has the fields of an app above (`name`, `host`, `deployed`, `release`, `commit`, `checkPassed`, `service`, `activeSince`, `restarts`, `health`, `memoryKiB` and `ok`), the server it asked as `server` (`host`, and `name` and `id` when the host comes from `~/.tifa/config.yaml`), and `releases`: the releases kept, the current one first and then the newest first, each with `id`, `commit` and `checkPassed`. It exits as the text does: with an error when the app is not on the server or the server cannot be reached, else 0, also when the service is down, so read `ok`.

```json
{
  "name": "shop",
  "host": "vps",
  "deployed": true,
  "release": "20261120T120000Z",
  "commit": "ccc333",
  "checkPassed": true,
  "service": "active",
  "activeSince": "Fri 2026-11-20 12:00:05 UTC",
  "restarts": 0,
  "health": "200",
  "memoryKiB": 39630,
  "ok": true,
  "server": {"name": "vps", "id": "vps-1a2b3c4d", "host": "vps"},
  "releases": [
    {"id": "20261120T120000Z", "commit": "ccc333", "checkPassed": true},
    {"id": "20261120T110000Z", "commit": "bbb222-dirty", "checkPassed": false}
  ]
}
```

`remove` takes the app off the server and takes back what Tifa made for it, and nothing else:

- With `front: nginx`: as root, the site of the app in nginx, with `nginx -t` before the reload, its certificate in certbot, and, with the last app of the nginx, nginx, certbot (when setup installed it), their configuration, `/etc/tifa/nginx` and the ufw rules with the comment `tifa nginx front` (see [Behind nginx](deploy-proxy.md#behind-nginx)). The certificate and nginx stay while the site cannot be taken out. The other apps keep answering.
- On the server, as `tifa`: it stops and disables `tifa-<app>.socket`, `tifa-<app>-http.socket` and `.service`, and deletes their unit files and `/srv/tifa/<app>` with its releases, `shared/.env`, the data in `shared/`, the certificates in `certs/` and `deploys.log`. Copy the data first if you want to keep it. For an app that served visitors itself, as root, `/etc/sysctl.d/60-tifa-ports.conf`, and the first port users may open goes back to what it was, and with ufw the rules with the comment `tifa <app>` that setup added, which the plan lists. Other apps, the user `tifa`, the other rules of the firewall and sshd stay as they are.
- With `front: cloudflare`: the route, the DNS record and the rules of the domain, and with `connect: cloudflare`, with or without CI, the Access application of the ssh host name with its people policy, its route and its DNS record, and the service token and policy of the repository. Everything that needs ssh runs first (the app, the cache tokens), then the stop of the tunnel, and only then the ssh host name goes: the route and record before the application and its policies, so a partial failure never leaves a route without Access, and only when the steps before it worked. A run again finds the ssh host name from the route of the tunnel when the application is already gone. When your ssh goes through the tunnel, the last app of a server stops the tunnel a few seconds after ssh left, so the command does not cut its own connection. The tunnel and cloudflared serve every app of the server, so they go only with the last app of Tifa there: then remove stops cloudflared as root, deletes `/etc/tifa`, uninstalls cloudflared when setup installed it, and deletes the tunnel.
- The cache tokens on the server, `/srv/tifa/.config/tifa`, serve every app of the server too, and go with the last app of Tifa there. Remove looks for apps again under the lock of the file when it deletes, so an app set up while remove waited for your answer keeps them. Delete the token `tifa cache purge <zone>` in the Cloudflare dashboard yourself: Tifa cannot.
- On your machine: the block of the app in `~/.ssh/config`, last, so a run that stopped halfway can still reach the server. Without a login of cf, when you reach the server by the ssh host name, the block stays and remove says so. cloudflared stays installed, and `TIFA_DEPLOY_HOST` in `.env` stays as it is.
- With the block `auto`: the key of the repository in `authorized_keys`, and the secrets and the variables that setup set (`TIFA_DEPLOY_FINGERPRINT` and the two of the Tailscale identity). The identity itself is in your Tailscale console: delete it there. `.github/workflows/tifa-deploy.yml` is a file of your repository, so it stays: until you delete it, each push fails to deploy.

Like setup, remove prints a plan first. On a terminal it then asks you to type the name of the app, also with `--yes`. Without a terminal it removes nothing unless you pass `--yes`. What it cannot remove, without a login of cf, or gh or a token, is listed last with the login it needs. It removes the rest and exits with an error: log in and run it again. A step that fails does not stop the others, but the tunnel then stays until a run again has removed the rest. A run that finds nothing left says so in one line.

## Run a command of the app

```sh
go tool tifa deploy run promote ann@example.com   # a command the app adds in OnCommand
go tool tifa deploy run config                    # the settings of the service, and where each comes from
go tool tifa deploy run --host vps2 seed --count 5
```

`run` runs the program of the current release with the arguments, over ssh as `tifa`, after the same look at the server as `status`: it finds the server the way `tifa deploy` does and stops when the host now answers as another machine than the one `~/.tifa/config.yaml` remembers. The program runs in `/srv/tifa/<app>/shared`, the folder of the service, with the variables the service has, so it reads the same `.env`, settings and data. It does not start or stop the service.

- Its flags, `--dir` and `--host`, go before the command. Everything from the command on goes to the program as it was given, flags, quotes, spaces and `$` included. `--` ends the flags of `run` too: `go tool tifa deploy run -- --help` prints the help of the program.
- The output, the error output and the exit status are those of the program, and `run` adds no message of its own after it.
- With a terminal on both its input and its output, ssh gets a terminal on the server, so the command can ask a question and Ctrl-C reaches it. Otherwise, as a coding agent runs it, ssh runs in BatchMode with no terminal, and the input of `run` is the input of the program (`go tool tifa deploy run import < rows.csv`).
- An app with no release yet stops with `run go tool tifa deploy first`.

### After a deploy

`tifa deploy` exits with 1 when the new release fails its health check, so work that must follow a good deploy chains with `&&` and runs on the release that just went live:

```sh
go tool tifa deploy && go tool tifa deploy run notify-team
```

In [GitHub Actions](deploy-github-actions.md), add a step after the step `Deploy` of `.github/workflows/tifa-deploy.yml`. Delete Tifa's first line of the file first, so setup leaves your edit alone. A step runs only when the steps before it passed, and it gets no terminal, as an agent does:

```yaml
      - name: Notify the team
        if: inputs.action != 'rollback'
        env:
          TIFA_DEPLOY_HOST: ${{ secrets.TIFA_DEPLOY_HOST }}
        run: go tool tifa deploy run --host "$TIFA_DEPLOY_HOST" notify-team
```

With `connect: cloudflare`, copy the two `TUNNEL_SERVICE_TOKEN_` variables of the step `Deploy` into its `env` too.

The commands an app adds, and what they can rely on while the service runs, are in [Commands of the app](../guides/commands.md).

## On the server

- The log of an app: `go tool tifa deploy logs`, or `journalctl --user -u tifa-<app>` as `tifa` (`sudo -iu tifa`).
- Start, stop or look at it: `systemctl --user status tifa-<app>.service` as `tifa`. Restart the service, never the socket.
- The apps start at boot, before the first visitor.
- Several apps: each has its own folder, port and units. Run `tifa deploy` in each, after one `tifa deploy setup`. Only one of them can serve visitors itself on ports 80 and 443: the others go behind the Cloudflare Tunnel or nginx.

[Deploy by hand](deploy-by-hand.md) is the way to do it by hand, and what `tifa deploy` does underneath.
