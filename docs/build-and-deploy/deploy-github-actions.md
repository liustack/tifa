---
summary: 'Deploy from GitHub Actions after CI: `deploy.auto`, the branch and workflow, and how the runner reaches the server with `connect: direct` or `tailscale`'
audience: app
order: 12
read_when:
  - Deploying with GitHub Actions after CI, choosing the branch that deploys or the workflow it waits for (`auto: branch` and `needs`), or taking away the key CI deploys with
  - Deploying from CI with `connect: tailscale`, making the Tailscale identity the runner joins your tailnet with, or a CI run that cannot join the tailnet
---

# Deploy from GitHub Actions

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

## connect: direct

The default. CI logs in to the address and port of `TIFA_DEPLOY_HOST`. The port must then be open to GitHub's runners, whose addresses change, so you cannot allow only some. Allow key-only login (`PasswordAuthentication no`): the key of the repository is the only way in.

## connect: tailscale

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
