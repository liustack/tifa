---
summary: 'Put a Tifa app on a server: `tifa deploy`, the `deploy` block of tifa.yaml, naming the server, deploy, roll back, more developers'
audience: app
order: 10
read_when:
  - Putting a Tifa app on a VPS or in a container for the first time
  - Running tifa deploy, tifa deploy setup or tifa deploy rollback, or a deploy failed its health check and went back
  - Naming the server of tifa deploy with TIFA_DEPLOY_HOST, or finding out which server it uses when you named none (~/.tifa/config.yaml, TIFA_HOME), or choosing deploy.front, deploy.connect or deploy.auto in tifa.yaml
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
3. Points `/srv/tifa/<app>/current` at it and restarts the service. The socket stays open, so requests that come meanwhile wait and get answered (see [Restart without dropping requests](deploy-by-hand.md#restart-without-dropping-requests)).
4. Checks the new program on the server: `GET /healthz` once a second, for up to 30 seconds, until it answers with a status below 500. It asks on the loopback, and an app with its own HTTPS over HTTPS for its domain, as a visitor would. An app without a `/healthz` route passes with its 404 as soon as it answers. Write [a health check](deploy-by-hand.md#health-check) that looks at the database to make the check mean more. `OnStart`, migrations included, must finish within those 30 seconds (see [How long a migration may take](../guides/database.md#how-long-a-migration-may-take)).
5. When the check passes, writes the deploy down and keeps the last 5 releases. When it fails, it prints the last lines of the log of the app, starts the release that ran before again, and exits with 1. If there is no release that ever passed, it stops the app.
6. With `front: cloudflare`, clears the CDN cache of that host name (see [Clearing the CDN cache](deploy-cloudflare.md#clearing-the-cdn-cache)).
7. Says where visitors reach the app: `visitors: https://shop.example.com`, or `http://<address of the server>` without a domain. With `front: none` or `front: nginx` it then asks the app on its ports from your machine, and when one does not answer it says that the firewall or a security group of your provider may block it, which Tifa does not change.

Every deploy and rollback is a line of `/srv/tifa/<app>/deploys.log`: the time, the release, the git commit (with `-dirty` when the app had changes not committed), `deploy` or `rollback`, and `ok` or `failed`. The upload never includes `.env`. One deploy of an app runs at a time.

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

The database stays where it is. A rollback starts the older release on the tables the newer one left, with everything visitors wrote since, and the older release logs a warning that names the migrations it does not have. That works when each release runs on the tables of the next, as [Keep a rollback working](../guides/database.md#keep-a-rollback-working) describes. To undo data too, go back to an earlier time with the backup: [Undo a bad deploy](../guides/database.md#undo-a-bad-deploy).

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
