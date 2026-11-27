---
summary: 'Where the settings of an app come from: tifa.yaml, flags, environment, .env, PORT, the options of the app, value formats, config errors'
audience: app
order: 17
read_when:
  - Defining an option of the app with tifa.String and the like, or reading one in OnStart
  - The browser needs a value that is known only when the program runs, such as a public key or an API base URL, and `import.meta.env.VITE_*` is fixed at build
  - Writing a .env file, or deploying on a platform that sets PORT
  - The program refuses to start with a config error
  - Changing internal/config
---

# Configuration

A Tifa app runs with sensible defaults for a small machine. Change a setting only when you need to.

## Where settings come from

From highest to lowest priority:

1. Command-line flags, such as `--server-addr 0.0.0.0:9527`.
2. Environment variables, such as `SERVER_ADDR=0.0.0.0:9527`.
3. The `.env` file in the folder the program runs in.
4. The deployment file given with `-c path.yaml` (or `--config path.yaml`).
5. The project file `tifa.yaml` in the app root. `tifa build` reads it and builds its values into the program.
6. The defaults: those in the [table of settings](../reference/settings.md#settings) for the settings of Tifa, those in `options.go` for the [options of the app](#options-of-the-app).

A source sets only the keys it names, so a deployment file holds just what it changes. The program reads only its own keys: other keys of the file of `-c` and other environment variables are left alone. `tifa.yaml` is stricter, see below. A flag that is neither a setting of Tifa nor one of the app, such as `--addr` for `--server-addr`, stops the program with `unknown flag`.

`tifa.yaml` holds values of the settings of Tifa. `build.*`, `i18n.*`, `site.*` and `deploy.*` settings can only be set here, and `tifa build` fails on a value of the wrong type or format. The program does not read the file: change it and build again.

`tifa.yaml` holds only the settings of Tifa, and no key of your app. A key in it that is not a setting of Tifa, such as `deploy.ssh` or a misspelt `server.adr`, fails `tifa check`, `tifa build`, `tifa dev` and the deploy commands at its line, with the closest name when one is near. The options of your app, even one that sits in a section of Tifa such as `server.name`, are set in a flag, an environment variable, `.env` or the file of `-c`, and never in `tifa.yaml`.

```yaml
# tifa.yaml
build:
  target: es2020,safari14
server:
  addr: 0.0.0.0:9527
  body_limit: 4MiB
ssr:
  timeout: 2s
  memo_size: 128MiB
```

Run `./bin/app config` to print every setting, of Tifa and of the app, with its value and where the value comes from (`default`, `tifa.yaml`, `file prod.yaml`, `.env SSR_VMS`, `env PORT`, `flag --ssr-vms` and so on). The output is a commented YAML file, a good starting point for a deployment file. It leaves out the values of the app's secrets ([`tifa.Secret`](#options-of-the-app)).

### Names

Each setting has a key, a flag and an environment variable, all from one rule:

| Key | Flag | Environment variable |
| --- | --- | --- |
| `db.url` | `--db-url` | `DB_URL` |
| `server.body_limit` | `--server-body-limit` | `SERVER_BODY_LIMIT` |

- A key is lower case letters, digits and underscores, in at least two parts joined with dots.
- The flag replaces dots and underscores with hyphens. The environment variable replaces dots with underscores and is in upper case. There is no prefix.
- Kubernetes puts an environment variable into every pod for each Service in the namespace, such as `DB_PORT=tcp://10.0.0.5:5432` for a Service called `db`. An option or setting whose key ends in `.port` after a Service's name picks that value up. Set `enableServiceLinks: false` in the pod spec, or do not name option keys `<service>.port`.
- In a YAML file each part of the key is one level: `db.url` is `url` under `db:`.
- A flag takes its value after a space or after `=`: `--ssr-vms 4` or `--ssr-vms=4`. A word that starts with `-` is always a flag and never a value, so a value that starts with `-`, such as a negative number, goes after `=`: `--ssr-vms=-1`. A flag without its value fails. The program reads no flag after `--`.

### .env

The program reads the file `.env` in the folder it runs in, with lines of `NAME=value`. A real environment variable wins over the same name in `.env`. Without the file nothing happens. `tifa build` does not put it into the program, and there is one `.env` per machine, with no `.env.local` or `.env.production`: keep it out of git.

```sh
# .env
DB_URL=/var/lib/shop/shop.db
SERVER_ADDR=0.0.0.0:9527
export SESSION_MAX_AGE=24h   # a line may start with export
GREETING="Hello # not a comment\n"
```

- Lines starting with `#` and blank lines are skipped. An unquoted value ends at a `#` after a space or a tab and loses the spaces around it, so `TLS_CERT= # none` is empty and `COLOR=#fff` is `#fff`.
- A value in double quotes may hold `\n`, `\t`, `\"` and `\\`. A value in single quotes is taken as it is. A quoted value stays on one line.
- A line it cannot read stops the program with its line number.
- `GOMEMLIMIT`, `GOGC` and `GOMAXPROCS` work in `.env` as in the environment. Go reads them before the program reads `.env`, so the program sets them itself when it starts, before anything else, and `ssr.vms` follows a `GOMAXPROCS` of `.env`. A value Go would not take stops the program. Other names Go reads, such as `GODEBUG`, work only in the real environment.
- `tifa deploy` reads its inputs from this file too, `TIFA_DEPLOY_HOST` to know the server and the other names that start with `TIFA_DEPLOY_`. The program ignores them. Without `TIFA_DEPLOY_HOST`, `tifa deploy` uses the server it remembered in `~/.tifa/config.yaml`, and `TIFA_HOME` moves that folder (see [Deploy with tifa deploy](deploy.md#deploy-with-tifa-deploy)).

`tifa dev` runs the app in the app root, so the app reads the `.env` there, and a change of `.env` starts the app again. Vite does not read `.env` (`envDir: false`): a page gets the values it needs from its `Load`.

### PORT and HOST

Hosting platforms such as Cloud Run, Heroku, Render, Railway and Fly tell the program its port in `PORT`. The program reads `PORT` and `HOST` for `server.addr` (not `HOSTNAME`, which shells and Docker set to the machine name):

- Their place is below `SERVER_ADDR`: `--server-addr` > `SERVER_ADDR` > `PORT` and `HOST` > the file of `-c` > `tifa.yaml` > the default. `.env` may hold them too, below the real variables.
- `PORT` alone listens on every interface: `PORT=8080` is `:8080`, as those platforms need.
- `HOST` alone changes the host and keeps the port of `server.addr` from the sources below.
- Empty ones count as not set.

`tifa dev` gives the app its address in `SERVER_ADDR`, so a `PORT` in your shell does not move it. `tifa dev` itself listens on `--addr` and does not read `PORT`.

## Options of the app

An app defines its own options in `options.go` at its root, as package-level variables:

```go
// options.go
package shop

import (
	"time"

	"github.com/liustack/tifa"
)

var (
	DBURL   = tifa.String("db.url", "app.db", "Database file")
	Workers = tifa.Int("jobs.workers", 4, "Background workers")
	Hosts   = tifa.Strings("feed.hosts", nil, "Hosts the feed is read from")
	Open    = tifa.Bool("shop.open", false, "Let visitors in without signing in")
	Idle    = tifa.Duration("db.idle", time.Minute, "How long an idle connection stays open")
	Upload  = tifa.Size("upload.limit", 8<<20, "Largest upload")
	APIKey  = tifa.Secret("payments.api_key", "Key of the payment provider")
)
```

- The arguments are the key, the default and a line of help. Each returns a plain value: read `shop.DBURL`, and a test sets `shop.DBURL = "test.db"`.
- The value comes from the same sources as the settings of Tifa: the flag `--db-url`, the variable `DB_URL`, `.env`, the file of `-c`, then the default. `tifa.yaml` holds only settings of Tifa: an app key written there fails the build.
- The types: `String` (`string`), `Strings` (`[]string`), `Int` (`int`), `Bool` (`bool`), `Duration` (`time.Duration`) and `Size` (`int64` bytes). Their values are written as for the settings of Tifa, see [Value formats](#value-formats). `Strings` is comma-separated text, such as `FEED_HOSTS=a.example,b.example`.
- `Secret` (`string`) is for a password, a token or a key. It has no default and is empty when no source sets it. It comes from the environment variable, `.env` or the file of `-c`, and not from a flag: a flag shows in the process list to every user of the machine, so the program refuses one and says where to put the value. `./bin/app config` writes where it came from and leaves the value out, as the comment `# api_key: (hidden)`. An error about a secret, such as an unquoted number in the file, names the key and the source but not the value. Put a secret that starts with `*`, `!`, `&` or another YAML sign in quotes.
- Values are single values. Structured data goes into a file of the app, and an option holds its path.
- An invalid value gives the default, and the program lists it with the other invalid settings and options and stops before `OnStart`.
- In `go test` the options keep their defaults, whatever the flags, variables and `.env` are.
- The packages that the root package imports cannot read these variables (it would be an import cycle): `OnStart` passes the values to them, such as `store.Open(DBURL)`.
- An app that needs more can run its own configuration in `OnStart`.

`tifa build` fails, naming the file and line, when `tifa.String` and the like are called anywhere but as the value of a package-level `var` in `options.go` at the root, when a key is not a string literal or does not follow the rule of [Names](#names), and when two settings, of the app or of Tifa, would have the same flag or environment variable, such as `a.b_c` and `a_b.c`, and when one key is a section of another, such as `db.url` next to `db.url.timeout`, or `server.addr.port` next to `server.addr` of Tifa. The program panics on the same mistakes if they get past the build.

### A value for the browser that is known at run time

`import.meta.env.VITE_*` is fixed when `tifa build` runs, so a new value needs a new build. For a value that is known only when the program runs, such as a public key or the base URL of an API, define it as an option and return it from `Load` (or from a `Layout`, for every page below it). Everything a `Load` returns is in the page, so never return a `tifa.Secret`. See [Values from the environment](../guides/environment.md).

## Value formats

- Sizes are whole numbers with an optional unit: `B`, `KiB`, `MiB` or `GiB`, such as `512KiB`. A plain number is bytes. `MB`, `KB`, `GB` and `4 MiB` are refused, and so is a decimal such as `1.5MiB` (write `1536KiB`). Only `ssr.memo_size` may be 0.
- Durations use Go's format and must be positive: `500ms`, `3s`, `1m30s`. A `tifa.Duration` of the app may also be 0 or negative.
- In a YAML file a whole number setting is a number (`vms: 2`, not `"2"`), a `true` or `false` setting a bool, and every other setting text. A size may be a number of bytes or text with a unit.
- `ssr.vms` is a whole number of at least 1.
- `ssr.queue_limit` is a whole number of at least 0. `ssr.wait_timeout` must be positive even with a queue limit of 0.
- `ssr.vm_stack_depth` is a whole number from 1 to 10000, the deepest the engine lets calls nest.
- `build.skip_tanstack_check`, `build.debug_info`, `build.i18next_cache`, `server.compress` and `server.hsts` are `true` or `false`, without quotes in a file. The flag alone is `true` and takes another value only after `=`: `--server-compress=false`. It never takes the next word.
- `server.trusted_origins` and `server.cross_site_paths` are comma-separated lists, without brackets or quotes inside, and may be empty. A higher source can empty what a lower one set: `SERVER_TRUSTED_ORIGINS=` clears the origins of the project file. An origin needs `http` or `https`, a host and no path, query or user name, and a bad one fails with what is wrong. A pattern that `http.ServeMux` rejects, or that conflicts with another of the list, fails too.
- `server.tls_domains` is a comma-separated list of host names, without scheme, port or path. Each needs a dot. `server.tls_ca` is an `https` URL. `server.http_addr` is an address with a port, such as `:80` or `0.0.0.0:80`, or `off` in lower case. Words that read as a switch, such as `OFF`, `false` or `none`, fail with what to write.
- `server.csp` is `off`, `on` or `report-only`, in lower case. `true`, `false`, `yes` and the like fail with what to write. `server.csp_policy` may not be empty: to send no policy, set `server.csp` to `off`.
- `server.tls_domains`, `server.tls_email`, `server.tls_cache`, `server.tls_cert` and `server.tls_key` may be empty, which turns off what a lower source set: `SERVER_TLS_DOMAINS=` with `--server-tls-cert` serves your own certificate where the project file asks for Let's Encrypt.

## Errors

`tifa build` fails on a value of the wrong type or format in `tifa.yaml`, naming its line. A setting written as a section or a list, such as `ssr: {vms: {count: 2}}`, is a value of the wrong type too. The program refuses to start and lists every problem, each with its source and key, when:

- a value of a setting of Tifa or of the app has the wrong type or format, such as `ssr.timeout: 3`, `SHOP_OPEN=maybe` or a section under `ssr.vms` in the file of `-c`,
- a secret of the app (`tifa.Secret`) is given as a flag,
- the file of `-c` cannot be read or is not YAML, or a line of `.env` cannot be read.

A flag that is no setting stops the program with `unknown flag`. A key or variable that is no setting is not an error, so a misspelt one such as `SERVER_PORT` changes nothing: `./bin/app config` shows what each setting ended up as. `build.*` settings are read only from `tifa.yaml`: a deployment file, a `BUILD_*` variable or a `--build-*` flag does not change them.

It also refuses to start, before `OnStart`, and names the settings when:

- HTTPS is on and `server.addr` is the default `127.0.0.1:9527`, left out or written out, and systemd passed no socket for it,
- the HTTPS settings do not go together once all sources are merged: `tls_domains` with `tls_cert`, `tls_cert` without `tls_key` or the other way round, `tls_domains` without `tls_cache`, or `tls_email` or `tls_cache` without `tls_domains`. The project file alone may leave out what the deployment adds, so `tifa build` does not check this,
- the files of `tls_cert` and `tls_key` do not load, or `tls_cache` cannot be created.

When `server.addr` or `server.http_addr` cannot be listened on, the error names the setting, and for a port only root may take it says what to do.

`serve --dist <folder>` and `serve --lifeline-fd <fd>` are flags for [`tifa dev`](dev.md) only. They are not settings and have no environment variable. The program `tifa build` makes has no `--lifeline-fd`.
