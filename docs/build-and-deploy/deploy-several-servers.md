---
summary: 'Run one app on several servers behind your load balancer: the list of servers, setup, rolling deploys, the other commands, and what the app must keep in a shared place'
audience: app
order: 22
read_when:
  - One server is no longer enough for an app, or one server going down must not take the site down
  - Naming several servers in TIFA_DEPLOY_HOST or --host, or a command refused several servers with deploy.front none, nginx or cloudflare
  - A deploy to several servers failed on one of them and moved the others back, or the servers run different releases
  - Deciding where sessions, data, uploads and background work go when the app runs on more than one server
---

# Run an app on several servers

One server is where an app starts. When it needs more, `tifa deploy` puts the same app on several servers behind a load balancer you set up at your provider. Each server runs one copy of the program, the deploy rolls from one server to the next, and the site stays up while one server is down.

The app must keep what it shares somewhere all servers reach: see [What the app must do](#what-the-app-must-do) before you add a second server.

## The settings

Several servers need `front: external`, a load balancer of yours in front of them ([Behind your load balancer](deploy-proxy.md#behind-your-load-balancer)):

```yaml
deploy:
  front: external
```

Name the servers as you name one, with commas between them, first match wins as in [Deploy with tifa deploy](deploy.md#deploy-with-tifa-deploy):

```sh
# .env
TIFA_DEPLOY_HOST=vps1,ssh://203.0.113.8:2222,ubuntu@vps3
```

Or give `--host` more than once, or with commas: `go tool tifa deploy setup --host vps1 --host vps2`. Each entry is written as for one server. A server named twice is refused. The first server in the list has a role of its own: `deploy.release_command` runs there, and `deploy run` goes there.

With one server, everything is as before.

**The other fronts work with one server only.** With several servers, `none`, `nginx` and `cloudflare` are refused before anything is changed, and the error says to use `external`:

- `none`: each server would serve visitors itself, with its own certificate, and nothing would spread the load.
- `nginx`: the nginx of Tifa serves the apps of one server. Sending visitors from it to other servers needs a network between the servers, which is yours and differs at every provider.
- `cloudflare`: a tunnel that runs on several servers sends each request to the nearest server, and to another only when that one fails ([Tunnel availability](https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/configure-tunnels/tunnel-availability/)). The servers would not share the load.

## Set up

`go tool tifa deploy setup` sets up each server in turn, the first first, with a plan for each: `Plan for shop on vps2 (2 of 3)`. It gives the app one port that is free on every server, so the balancer has one port to send to. When the port the app has on one server is taken on another, the app moves to a port free on all of them.

Setup stops at the first server that fails or needs you. Fix what it says and run it again: the servers that are right show one line each, and setup goes on from there.

With `deploy.auto`, GitHub Actions comes last, in a plan of its own for all the servers: one key of the repository that may log in to each, the host keys of each, and the list in the secret `TIFA_DEPLOY_HOST` (see [Deploy from GitHub Actions](deploy-github-actions.md#deploy-from-github-actions)).

Setup ends with the targets and the health check to enter in the balancer, and `~/.tifa/config.yaml` keeps the list of servers, so later commands without `--host` reach all of them ([The servers Tifa remembers](deploy.md#the-servers-tifa-remembers)). To add a server, add it to the list, run setup, and add the target to the balancer. To take one away, take it out of the balancer first, then run `go tool tifa deploy remove --host <that server>`: it takes the app off that server only, and `~/.tifa/config.yaml` keeps the others as the servers of the app. Then take the server out of `TIFA_DEPLOY_HOST`. With `deploy.auto`, run setup too, so GitHub Actions gets the servers that are left. A deploy that names fewer servers than `~/.tifa/config.yaml` keeps for the app stops before it changes anything, and names the server to remove.

## Deploy

`go tool tifa deploy`:

1. Reads every server. They must have the same system, since one build runs on all of them, and the app on the same port. A server without the app, or with it on another port, stops the deploy before anything changes: run setup.
2. Builds once, and uploads the release to every server.
3. Runs `deploy.release_command`, when you set one, once on the first server, before any server switches (see [The release command](deploy.md#the-release-command)). When it fails, the deploy stops, the new release is deleted from every server, and the `deploys.log` of the first one says why.
4. Switches one server at a time: `switching vps2 (2 of 3)`. Each server starts the new release and passes its health check before the next one starts. The balancer keeps sending visitors to the servers that are not switching.

When a server fails its check, it goes back to its release by itself, as with one server, and the deploy stops. The servers before it, which passed, are moved back to the release they ran, the last one first, so all servers run one release again. A server that ran no release before, as at the first deploy, stops the app instead, and so does one whose old release fails its check now: no server keeps running a release that failed on another. The error says which release runs where when the servers differ. Then deploy again, or run `go tool tifa deploy rollback <release>` to bring them to one release.

The release that failed is written down as failed in the `deploys.log` of every server that has it, also of those that passed with it or never switched, so `status` shows it as failed and no rollback goes to it.

## The other commands

- `rollback`, and `rollback <release>`: the release is chosen on the first server, every server must have it, and they move one at a time, with the same health check and the same moving back as a deploy.
- `status`: each server, one after the other. `status --json` is a list, one entry per server.
- `logs`: the log of each server, under a line that names it. `logs -f` follows one server: name it with `--host`.
- `env set` and `env unset`: every server, one after the other. An error says which servers have the change already. `env list` shows the names once, or each server's names when they differ.
- `run`: the first server. Name another with `--host`.
- `remove`: a plan for each server, one question, then the others first and the first last. Only the plan of the first server takes back what is in GitHub. With `--host`, it takes the app off that one server, as [Set up](#set-up) says.

## What the app must do

Each server runs its own copy of the program, with its own memory and its own disk. Tifa checks none of this. What one copy writes, the others must find:

- **Sessions.** The default store keeps sessions in the memory of the program, so a visitor signed in on one server is signed out on the next. Give the app a [store of your own](../pages-and-data/sessions.md#where-to-store-sessions) that all servers reach, such as Postgres or Redis.
- **Data.** SQLite is one file on one machine. Each server would have its own, with its own orders and users. Use a database that every server reaches over the network, such as Postgres ([Postgres](../guides/database.md#postgres)).
- **Uploads.** A file written to the disk of one server is missing on the others. Keep files in object storage, such as S3 or R2 ([Uploads](../guides/uploads.md#uploads)).
- **Background work.** A loop started in `OnStart` runs on every server. Work that must run once, such as a daily mail, needs a lock or a jobs table in the shared database ([Background work](../guides/jobs.md#background-work)).
- **Migrations.** With migrations in `OnStart`, the first server to start migrates, and the others find the tables done. Running them once before the switch with `release_command: migrate` is clearer with several servers.
- **Settings in `.env`.** `tifa deploy env` writes the same value to every server. A value set by hand on one server stays on that server.
