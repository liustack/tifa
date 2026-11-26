---
summary: 'Deploy through a Cloudflare Tunnel with `connect: cloudflare`, and keep the CDN cache clear after a deploy'
audience: app
order: 13
read_when:
  - Putting an app behind a Cloudflare Tunnel with cf, or finding what tifa deploy setup changed in Cloudflare and how to remove it
  - A deploy said the CDN cache was not cleared, or you want to add or replace the token that clears it, which setup keeps on the server
  - Deploying through the Cloudflare Tunnel with `connect: cloudflare` from your own machine, so the server needs no open ssh port, or a deploy waits on a Cloudflare Access login in the browser
---

# Deploy through Cloudflare

With Cloudflare the server needs no open ssh port, and a deploy can clear the cache of your host name. This page covers both.

## connect: cloudflare

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
3. Checks that `ssh tifa@<ssh host> true` works through the block. **The first time, cloudflared opens the Cloudflare Access login in your browser and waits for you**, up to 4 minutes. Setup says so before it starts. cloudflared also prints the login address, which a coding agent can show you when no browser opened. Logging in is the only step that is yours. Your public key must also be on the server (the login of Access lets you reach sshd, and sshd still checks your key: see [More developers](deploy.md#more-developers)).
4. Only then points `TIFA_DEPLOY_HOST` at the ssh host. The first setup of a fresh server still needs the address you gave, to log in as root before the tunnel exists, so the move comes last. When the value is a line of the `.env` of the app, setup changes that line (the plan shows `TIFA_DEPLOY_HOST in .env, vps -> <ssh host>`, and every other line of the file stays) and from then on `tifa deploy`, `logs`, `status` and `env` go through the tunnel. Only the value changes: an `export` in front, the quotes, a comment after it, the line endings and a missing final newline stay. The check runs whatever the source of `TIFA_DEPLOY_HOST` is. When the value comes from the environment or the flag `--host`, setup changes nothing, and after a check that passed it tells you what to set. If the check fails, nothing changes, setup says why, and the next run tries again. When it comes from `~/.tifa`, setup records the ssh host there itself, once the check passed. Setup says your key must be in `/srv/tifa/.ssh/authorized_keys` only when ssh got to authentication (its output has `Permission denied`). Any other failure shows what ssh and cloudflared printed and says the check failed, without blaming the key.

A server you reach by its ssh host name, with this machine not set up yet (another person, a new checkout), needs the block before setup can read anything. Setup plans that first: the Access policy, cloudflared and the block, then asks again for the rest. Without a login of cf it says to log in first (on a terminal it logs cf in for you), and does not try to read the server.

Switching `connect` back to `direct` while `TIFA_DEPLOY_HOST` is the ssh host name stops with an item that says to set it to the address of the server first (its ssh port must be open to you). The next setup then removes the route, the record, the Access application, its policies and the block of your ssh config.

## Clearing the CDN cache

With `front: cloudflare`, a deploy and a rollback clear the cache of that host name in Cloudflare, by host name and nothing wider: the rest of the zone keeps its cache. A page with `Cache` would otherwise show the old HTML, whose scripts are gone, until its `s-maxage` runs out.

Clearing is optional. No CDN, and no framework, makes every visitor see a new build the moment it is up: a cache that is not cleared keeps its copy until it runs out. Without a way to clear it, a page with `Cache` can be up to its `Cache` time old after a deploy, and that old copy may ask for scripts the new build no longer has. That is the price of a CDN, and Tifa says so on each deploy without calling it an error.

The deploy clears it with the first of these it finds, run on your machine or in CI alike:

1. The inputs `TIFA_DEPLOY_CF_PURGE_TOKEN`, a token that can only clear the cache of that one zone, and `TIFA_DEPLOY_CF_ZONE_ID`, the ID of the zone: the environment, else the `.env` at the root of the app. They override the server. Give the zone ID as well, because a token that only purges usually cannot read the zone to find it. When the token is set and the zone ID is not, the deploy says so in its warning. Setup does not write them to `.env`: put them there yourself.
2. The token that setup keeps on the server (see [Set up with a plan](deploy-setup.md#set-up-with-a-plan)): `/srv/tifa/.config/tifa/cloudflare-purge`, one line per zone with the name of the zone, its ID and the token. Tifa keeps the token on your server, readable only by `tifa`: the file is owned by `tifa` with mode 600, in a folder with mode 700. The deploy reads it over the ssh it deploys with, as `tifa`, once the new release passed its check. It holds the token in memory only and never prints it, logs it or puts it on a command line. So a deploy from CI, and from any machine that may log in as `tifa`, clears the cache, and one token serves every app of the server on that zone. Names of hosts and zones match in any case. Writes to the file hold a lock next to it (`cloudflare-purge.lock`, with `flock`), so two setups on one server keep each other's line. When the file cannot be read, the deploy says so in one line and goes on to the next way. The apps run as `tifa` too, so their code could read the file: the token can only clear the cache of the zone.
3. On your machine, cf with your login.

With none of them the deploy prints `cache: not cleared for <host>` with what that costs and says to run `go tool tifa deploy setup --cache-purge-token`, and is no warning.

A clearing that fails does not fail the deploy: it prints a warning and exits with 0, and the summary of the run shows it. Clear the cache by hand in Cloudflare, Caching, Configuration, Custom Purge, by host name.

Purging by host name works on every Cloudflare plan.
