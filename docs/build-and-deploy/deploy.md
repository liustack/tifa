---
summary: 'Put a Tifa app on a server: `tifa deploy`, the `deploy` block of tifa.yaml, naming the server, deploy, roll back, more developers'
audience: app
order: 10
read_when:
  - Putting a Tifa app on a VPS or in a container for the first time
  - Running tifa deploy, tifa deploy setup or tifa deploy rollback, or a deploy failed its health check and went back
  - Naming the server of tifa deploy with TIFA_DEPLOY_HOST, or finding out which server it uses when you named none (~/.tifa/config.yaml, TIFA_HOME), or choosing deploy.front, deploy.connect or deploy.auto in tifa.yaml
  - Giving a slow start more time with deploy.start_timeout, or running tests before a deploy and a message after it
  - Running migrations or another command of the app with the new release before it starts, with deploy.before_start
  - Giving a second developer machine access to deploy
---

# Deploy with tifa deploy

A Tifa app is one executable. The server needs that file, the settings in its environment, and a folder it may write data to. It needs no Node, no process manager and no Redis. The default session store lives in memory, so for more than one copy of the program give it a [store of your own](../pages-and-data/sessions.md#where-to-store-sessions).

`tifa deploy` puts a Tifa app on your own server, and one command sets up the whole CI/CD path from push to live.

`go tool tifa deploy` puts the app on a Linux server with systemd over ssh: it builds on your machine for the server, uploads the program, restarts the app without refusing a request, checks it on the server and goes back to the release before when the check fails. The app serves visitors itself, with its own HTTPS, or behind a Cloudflare Tunnel or an nginx that Tifa installs, where several apps can share one server. The server needs no Docker, no Node and no deploy program of its own.

`tifa.yaml` says what you want. `go tool tifa deploy setup` makes the server, Cloudflare and GitHub match it. `tifa.yaml` is committed, so neither a secret nor the place of the server goes in it.

```yaml
site:
  url: https://shop.example.com   # the domain is the host of this URL
deploy:
  front: none      # what visitors come through before the server: none (the app serves them itself on ports 80 and 443) | cloudflare | nginx
  connect: direct  # how deploys reach the server over ssh: direct | cloudflare | tailscale
  before_start: migrate  # a command of the app that each deploy runs once with the new release before it starts it. Left out: none
  start_timeout: 30s  # how long a new release may take to start and answer /healthz: 1s to 10m
  auto:            # deploy by itself from GitHub Actions. No block: you deploy by hand only
    branch: main   # the branch that deploys. Left out: the default branch of the repository on GitHub
    needs: ci.yml  # the workflow file in .github/workflows that must pass first. Left out: ci.yml. none: do not wait
```

- Where the server is does not go in `tifa.yaml`, because a public repository would show it. Name it on your machine, first match wins:
  1. the flag `--host`
  2. the environment variable `TIFA_DEPLOY_HOST`
  3. `TIFA_DEPLOY_HOST` in the file `.env` at the root of the app, which git ignores (see [.env](config.md#env). The program itself takes no notice of that name)
  4. the servers `tifa deploy setup` and `tifa deploy` remembered in `~/.tifa/config.yaml` (see [The servers Tifa remembers](#the-servers-tifa-remembers))

  ```sh
  # .env
  TIFA_DEPLOY_HOST=ssh://203.0.113.7:2222
  ```

  The value is what `ssh` takes as a destination: a Host of `~/.ssh/config` (an alias such as `vps`, so `~/.ssh/config` supplies the address, the port, the keys and a `ProxyJump`), a host name, or an `ssh://host[:port]` URL. Tifa picks the user itself, root for the steps that change the system and `tifa` for the rest, so leave the user out unless root cannot log in (see [When root cannot log in](#when-root-cannot-log-in)). A path, a query or another scheme is refused. With none of the four set, every `tifa deploy` command stops and says so.
- `front` is what visitors come through before they reach the server. With `cloudflare`, that is a Cloudflare Tunnel at the host of `site.url`, with its DNS record and rules, and `deploy` and `rollback` clear the CDN cache of that host name. `site.url` must be set and have no port. With `none`, the app serves visitors itself on port 80, and with a domain in `site.url` also on 443 with its own HTTPS, and no cache is cleared (see [The app serves visitors itself](deploy-setup.md#the-app-serves-visitors-itself)). With `nginx`, an nginx that setup installs sends visitors to the app by the host of `site.url`, and gets the certificate for it with certbot, so several apps can share the server without Cloudflare (see [Behind nginx](deploy-proxy.md#behind-nginx)).
- `connect` is the one path ssh takes to the server, from your machine and from the GitHub Actions runner. With `direct`, ssh uses the address you gave. With `cloudflare`, both go through the Cloudflare Tunnel, so the server needs no open ssh port (see [`connect: cloudflare`](deploy-cloudflare.md#connect-cloudflare)). It needs `front: cloudflare`, because the ssh host name is made from the domain, and works with or without `auto`. With `tailscale`, the runner joins your tailnet, and it needs the block `auto` (see [`connect: tailscale`](deploy-github-actions.md#connect-tailscale)). Your own machine reaches the server however it already does, with the address in `TIFA_DEPLOY_HOST`, and the tailnet and the server are yours to set up.
- `before_start` is a command of the app with its arguments, such as `migrate` or `migrate --verbose`, that each deploy runs once on the server with the program of the new release, before it starts that release, while the old one still serves. The words are split at spaces and each goes to the program as it is: no quotes, no shell. Left out, nothing runs. See [A command before the start](#a-command-before-the-start).
- `start_timeout` is how long a new release may take to start and pass its health check, written as a Go duration such as `45s` or `2m`. It is whole seconds from `1s` to `10m`, and `30s` when left out. The check of the release that a failed deploy goes back to, and of the release `deploy rollback` starts, gets the same time. See [A slow start](#a-slow-start).
- `auto` turns the deploys from GitHub Actions on, by being there. Without the block you deploy by hand only. `auto: {}` takes the defaults, and `auto:` with nothing after it is an error. It has two keys, and no others:
  - `branch` is the branch that deploys: a push to it, once the workflow in `needs` passed. Left out, it is the default branch of the repository on GitHub.
  - `needs` is the file in `.github/workflows` that must pass first, written as a file name such as `ci.yml` (no folder). Left out, it is `ci.yml`, which `tifa new` writes. `none` waits for nothing: the deploy runs on every push to the branch. See [Deploy from GitHub Actions](deploy-github-actions.md#deploy-from-github-actions).
- Only GitHub is supported.

**Inputs.** The values that belong to you and not to the repository are read the same way every time: the flag when the input has one, else the environment variable, else the line in the file `.env` at the root of the app. Tifa reads that file. Setup never writes a token into it and never prints one. The only line it ever writes there is `TIFA_DEPLOY_HOST`, which `connect: cloudflare` moves to the ssh host name once ssh through the tunnel works (see [`connect: cloudflare`](deploy-cloudflare.md#connect-cloudflare)), and only when the value is already in `.env`.

| Input | Flag | What it is | Comes from |
| --- | --- | --- | --- |
| `TIFA_DEPLOY_HOST` | `--host` | Where the server is | flag, environment, `.env`, then `~/.tifa` |
| `TIFA_DEPLOY_CF_PURGE_TOKEN` | none | A Cloudflare token that can only clear the cache of one zone. Overrides the one setup keeps on the server (see [Clearing the CDN cache](deploy-cloudflare.md#clearing-the-cdn-cache)) | environment, `.env` |
| `TIFA_DEPLOY_CF_ZONE_ID` | none | The ID of that zone, needed with the token above | environment, `.env` |
| `TIFA_DEPLOY_GITHUB_TOKEN` | none | A fine-grained GitHub token, for setup and remove when gh is not there | environment, `.env` |

A `TIFA_DEPLOY_` name in `.env` that tifa deploy does not read stops the command too, and the error names the closest one.

What you need: a Linux server with systemd, on x86_64 or aarch64, that you can reach with `ssh root@<TIFA_DEPLOY_HOST>` once, or as a user with passwordless sudo (tested on Ubuntu 24.04). On your machine, what `tifa build` needs (Go, and Node for an app with pages) and `ssh`. For `front: cloudflare` and `connect: cloudflare`, [cf](https://developers.cloudflare.com/cf/get-started/), the Cloudflare CLI (in beta), installed (`pnpm add --global cf`) and logged in (`cf auth login`). Setup and remove tell the two apart: without cf on the PATH they say to install it, with cf installed they ask for the login. For `connect: cloudflare`, also [cloudflared](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/downloads/) on your machine: setup installs it with `brew install cloudflared` on macOS or `winget install --id Cloudflare.cloudflared -e` on Windows, and on Linux, or without brew or winget, it tells you where to get it. For `front: nginx`, a server with apt (Debian or Ubuntu), which setup installs nginx and certbot with as root. For `auto`, [gh](https://cli.github.com/) logged in (`gh auth login`), or without gh a fine-grained token in `TIFA_DEPLOY_GITHUB_TOKEN` (see [Deploy from GitHub Actions](deploy-github-actions.md#deploy-from-github-actions)). For `connect: tailscale`, a server that is on your tailnet (Tailscale installed and logged in there), and a Tailscale credential that you make in the Tailscale console (see [`connect: tailscale`](deploy-github-actions.md#connect-tailscale)). Your machine needs nothing of Tailscale. On a terminal, setup installs and logs in cf and gh for you (see [Set up with a plan](deploy-setup.md#set-up-with-a-plan)).

The first time, run `go tool tifa deploy setup`, see [Set up the server](deploy-setup.md). After that you only deploy and, when a release is bad, roll back.

Without a terminal, as when an agent or a script runs the command, ssh asks nothing: a server whose host key is not in your `known_hosts` yet stops the command, and the error gives the command that accepts the key. A server that ssh reaches directly gets 15 seconds to answer, unless your ssh configuration sets its own `ConnectTimeout`. One reached through a `ProxyCommand` or `ProxyJump`, such as the login of Cloudflare Access in the browser, waits as long as it takes.

## When root cannot log in

Many cloud images, such as those of AWS, Google Cloud and Azure, let no one log in as root. Put the user you log in with in front of the host: `ubuntu@203.0.113.7`, or `ssh://ubuntu@203.0.113.7:2222`. That user needs passwordless sudo. Tifa logs in as it for the steps that need root (the first setup, the ports, ufw, nginx, the tunnel) and runs them with `sudo -n`. Deploys, logs and the other steps still log in as `tifa`. Without passwordless sudo, setup stops at its first look at the server, before it changes anything there, and says what to add. With no user, or with `root@`, Tifa logs in as root.

Setup gives `tifa` the keys in the `~/.ssh/authorized_keys` of that user, so the key you log in with works for deploys too. The user stays with the host: `~/.tifa/config.yaml` keeps it, and so does `TIFA_DEPLOY_HOST` when [`connect: cloudflare`](deploy-cloudflare.md#connect-cloudflare) moves it to the ssh host name.

When the provider gave you a key file, such as the `.pem` of an AWS key pair, name it in `~/.ssh/config`, since Tifa runs the system `ssh`:

```
Host myserver
    HostName 203.0.113.7
    IdentityFile ~/.ssh/aws-key.pem
```

Then set `TIFA_DEPLOY_HOST=ubuntu@myserver` in `.env`, or pass `--host ubuntu@myserver`. On AWS the user is `ubuntu` on Ubuntu and `ec2-user` on Amazon Linux. ssh refuses a key file that others can read, so run `chmod 600 ~/.ssh/aws-key.pem` first.

## Deploy

```sh
go tool tifa deploy
```

1. Asks the server for its system and builds the app for it on your machine, as [Building for another system](build.md#building-for-another-system) does. Nothing is built on the server. On a server that setup prepared but that has no folder for this app yet, it makes the folder, port and units first, as `tifa` and without root. An app that serves visitors itself needs setup there first when `tifa` may not open port 80 yet, and the deploy says so. On a server that is not prepared, it stops and says to run `go tool tifa deploy setup`.
2. Uploads the program, gzipped, into `/srv/tifa/<app>/releases/<id>/`, where `<id>` is the time of the deploy in UTC, and checks its SHA-256 there.
3. With `before_start`, runs that command with the new program while the old release keeps serving (see [A command before the start](#a-command-before-the-start)). When it fails, the deploy stops here.
4. Points `/srv/tifa/<app>/current` at it and restarts the service. The socket stays open, so requests that come meanwhile wait and get answered (see [Restart without dropping requests](deploy-by-hand.md#restart-without-dropping-requests)).
5. Checks the new program on the server: `GET /healthz` once a second, for up to `start_timeout` (30 seconds unless you set it), until it answers with a status below 500. It asks on the loopback, and an app with its own HTTPS over HTTPS for its domain, as a visitor would. An app without a `/healthz` route passes with its 404 as soon as it answers. Write [a health check](deploy-by-hand.md#health-check) that looks at the database to make the check mean more. `OnStart`, migrations included, must finish within that time (see [A slow start](#a-slow-start) and [How long a migration may take](../guides/database.md#how-long-a-migration-may-take)).
6. When the check passes, writes the deploy down and keeps the last 5 releases. When it fails, it prints the last lines of the log of the app, starts the release that ran before again, and exits with 1. If there is no release that ever passed, it stops the app.
7. With `front: cloudflare`, clears the CDN cache of that host name (see [Clearing the CDN cache](deploy-cloudflare.md#clearing-the-cdn-cache)).
8. Says where visitors reach the app: `visitors: https://shop.example.com`, or `http://<address of the server>` without a domain. With `front: none` or `front: nginx` it then asks the app on its ports from your machine, and when one does not answer it says that the firewall or a security group of your provider may block it, which Tifa does not change.

Every deploy and rollback is a line of `/srv/tifa/<app>/deploys.log`: the time, the release, the git commit (with `-dirty` when the app had changes not committed), `deploy` or `rollback`, and `ok`, `failed` (the health check) or `failed-before-start`. The upload never includes `.env`. One deploy of an app runs at a time.

## A slow start

`tifa deploy` stops the old program and starts the new one behind the systemd socket, so the site is never refused. But while the new release starts, `OnStart` and its migrations included, the requests of visitors wait on the socket. A longer `start_timeout` allows a slower start, and a slow start is a stall of the whole site for that long: with `start_timeout: 5m`, a migration that takes four minutes holds every visitor for four minutes.

A release that does not answer `/healthz` within `start_timeout` counts as failed. The old release then runs again. When the work that did not finish was a migration, the file that was running is undone (each file runs in a transaction of its own), and the files before it stay. Either raise the setting to fit the work, or make the slow work smaller: split a migration, or run a long index build by hand at a quiet time (see [How long a migration may take](../guides/database.md#how-long-a-migration-may-take)). Or run the migrations before the start, while the old release serves: [A command before the start](#a-command-before-the-start).

```yaml
deploy:
  start_timeout: 2m
```

## A command before the start

By default the migrations run in `OnStart`, so the site pauses while they run (see [A slow start](#a-slow-start)). For a migration that takes longer than a pause you want, such as an index on a large table, run it before the start instead:

```yaml
deploy:
  before_start: migrate
```

`migrate` is the command that `tifa new` writes into `hooks.go`. It runs the same migrations as `OpenDB`, and does nothing when the database file is not there yet, since the service makes it, or restores it with `db.backup`, at its first start. Any [command of the app](../guides/commands.md) works, with its arguments.

Once per deploy, after the upload and before `current` points at the new release:

- The command runs with the program of the new release, as `tifa deploy run` runs one: as the user `tifa`, in `/srv/tifa/<app>/shared`, with the `.env` and settings of the service. Its output and error output are part of the output of the deploy.
- The old release keeps serving visitors meanwhile.
- It has no time limit of its own. `start_timeout` covers only the start that follows. Ctrl-C, or a lost connection, stops it, and the deploy with it.
- When it exits with a status other than 0, the deploy stops: `current` and the running program stay as they were, the release is written down as `failed-before-start`, a rollback cannot go to it, and `tifa deploy` exits with 1. A migration file that failed is undone, and the files before it stay, as at a start.
- When it passes and the start then fails its check, the deploy goes back to the release before, as always. The database keeps what the command did.
- A rollback, by hand or after a failed check, never runs it: the release it starts ran its command when it was deployed.

After the command, `OnStart` of the new release finds no migration left to run, so the start is as quick as any other. On your machine, in tests and with `tifa dev`, `OnStart` still migrates as before.

What it does not change: both ways need each release to run on the tables of the next (see [Keep a rollback working](../guides/database.md#keep-a-rollback-working)), since the old release serves on the new tables while the command runs, and on SQLite a long migration holds the write lock, so writes of the old release wait and then fail. The trade-offs are in [At start or before the switch](../guides/database.md#at-start-or-before-the-switch).

## Before and after a deploy

`tifa.yaml` has no place for commands to run before or after a deploy. Chain them where you already run commands.

On your machine, in a shell. `tifa deploy` exits with 1 when the release failed its check, so the step after it can tell. In `sh`:

```sh
go test ./... &&
  go tool tifa deploy &&
  curl -fsS -X POST -d 'shop deployed' "$NOTIFY_URL"
```

In PowerShell, check the exit status after each command, since Windows PowerShell 5.1 has no `&&` (PowerShell 7 has it, so the `sh` form works there):

```powershell
go test ./...
if ($LASTEXITCODE -ne 0) { exit 1 }
go tool tifa deploy
if ($LASTEXITCODE -ne 0) { exit 1 }
Invoke-RestMethod -Method Post -Uri $env:NOTIFY_URL -Body 'shop deployed'
```

In GitHub Actions, the two workflows that `tifa new` and `tifa deploy setup` write (see [Deploy from GitHub Actions](deploy-github-actions.md#deploy-from-github-actions)) are the two places. What must pass before a deploy goes in `.github/workflows/ci.yml`: the deploy waits for the workflow named by `auto.needs`, so a step that fails there stops the deploy.

```yaml
      - run: go tool tifa check
      - run: go test ./...
```

What runs after goes in `.github/workflows/tifa-deploy.yml`, after the step `Deploy`. Delete the first line of that file (the comment that starts `# Written by go tool tifa deploy setup.`) before you edit it, or setup writes the file again. A failed `Deploy` step skips the steps after it unless they say `if: always()`:

```yaml
      - name: Deploy
        # ... as setup wrote it
      - name: Tell the team
        if: always()
        env:
          NOTIFY_URL: ${{ secrets.NOTIFY_URL }}
          RESULT: ${{ job.status }}
        run: curl -fsS -X POST -d "shop deploy $RESULT" "$NOTIFY_URL"
```

Add `NOTIFY_URL` under Settings, Secrets and variables, Actions of the repository. The secrets of your machine and of CI are separate, so each one gets its own.

## Roll back

```sh
go tool tifa deploy rollback
```

It starts the newest earlier release that passed its check, checks it the same way, and clears the CDN cache. Run it again to go back further. The next `tifa deploy` makes a new release from your code.

To go to a release you pick, give its ID as `tifa deploy status` lists it:

```sh
go tool tifa deploy rollback 20261120T110000Z
```

That release can be older or newer than the one that runs. It must still be on the server and have passed its health check when it was deployed. If it is not, nothing changes and the error lists the releases it can go to. If it is the one that runs already, nothing changes and the command says so.

The database stays where it is. A rollback starts the older release on the tables the newer one left, with everything visitors wrote since, and the older release logs a warning with the newest migration the database ran. That works when each release runs on the tables of the next, as [Keep a rollback working](../guides/database.md#keep-a-rollback-working) describes. To undo data too, go back to an earlier time with the backup: [Undo a bad deploy](../guides/database.md#undo-a-bad-deploy).

## The servers Tifa remembers

After a setup or a deploy that worked, Tifa writes the server in `~/.tifa/config.yaml` (`%USERPROFILE%\.tifa\config.yaml` on Windows), so the next app on this machine needs no `TIFA_DEPLOY_HOST`. A server set up from another machine, or before the file was there, gets its entry on the first deploy from this machine. A deploy in GitHub Actions writes nothing. The file holds no secret, only the servers this machine has set up or deployed to:

```yaml
# Servers this machine has set up or deployed to with tifa deploy. tifa deploy uses the only one when an app names none.
# No secrets are kept here. To forget a server, delete its entry.
servers:
  - id: vultr-1a2b3c4d
    host: ssh://203.0.113.7:22
    apps:
      - {name: shop, path: /Users/me/shop, host: 'ssh://203.0.113.7:22'}
      - {name: blog, path: /Users/me/blog, host: 'ssh://203.0.113.7:22'}
```

- `id` is the host name of the server and the start of its machine ID, which setup and deploy read from it. A server keeps its entry when the way to reach it changes. `host` is the one a new app uses: the one that worked last.
- An app is the folder of its checkout on this machine (`path`), and `name` is only for messages. Another checkout of an app with the same name counts as a new app. Each app keeps the host that worked for it: with `connect: cloudflare` that is the ssh host name of the app's domain, once the check through the tunnel passed, else the address you gave. `remove` moves the `host` of the server to an app that stays when it was the host of the app that went.
- When an app names no server (flag, environment and `.env` come first), Tifa uses the only server in the file and prints one line that says which one, and what to put in `.env` to name another. An app whose folder is listed under one server goes to that server, even when there are several. With several servers and an app that is on none, the command stops, lists them and says to set `TIFA_DEPLOY_HOST=<host>` in `.env` or pass `--host`. The plan of `setup` and `remove` shows the file as the source of the server.
- A host taken from the file is checked in the first thing the command asks the server: if the machine there is not the one in the entry (an alias or an address that now points elsewhere), the command stops and names both IDs and the host. `tifa deploy logs` asks nothing first, so it does not check.
- `tifa deploy remove` takes the app off its entry, and the entry goes with its last app. It does so only when remove finished with nothing left for you, so a retry finds the same server.
- `TIFA_HOME` moves the folder. The file is mode 0600 in a folder of mode 0700, written through a temporary file and a rename, and a lock file next to it keeps two Tifa commands from losing each other's entry. A comment you add stays. A server listed twice (same `id`) is an error that names the file.
- To forget a server by hand, delete its entry, or the whole file. Nothing else depends on it. CI never reads it: in GitHub Actions a missing `TIFA_DEPLOY_HOST` is an error about the secret, and the workflow gets its server from that secret.

## More developers

Every machine that deploys needs its own access to the server, and setup does not carry it over. From a machine that can log in, add the public key of the new machine to the user `tifa` (with `connect: cloudflare`, use the ssh host name, which goes through the tunnel):

```sh
ssh-copy-id -i ~/.ssh/id_ed25519.pub tifa@vps
```

With `connect: cloudflare`, the new person also runs `go tool tifa deploy setup` with their own cf login, with `TIFA_DEPLOY_HOST=<ssh host>` in their `.env`: setup adds their email to the Access policy, installs cloudflared and writes the block of their `~/.ssh/config`, and the first ssh opens the Access login in their browser. The ssh host name is the one in the `.env` of the first person, or the one the plan of their setup printed. The new machine also needs to know where the server is: put `TIFA_DEPLOY_HOST=` in the `.env` of its checkout (or pass `--host` once, since a deploy that works remembers the server in `~/.tifa`), and `TIFA_DEPLOY_GITHUB_TOKEN=` there too when it runs setup without gh, and when the value is an alias, the same alias in its `~/.ssh/config`. Nothing else is stored on a machine: `tifa.yaml` is in the repository and the rest lives on the server, in Cloudflare and in GitHub.
