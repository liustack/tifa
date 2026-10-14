---
summary: 'Every setting of a tifa app: where each one can be set, which source wins, the default, the flag and environment variable names, the value formats and the errors a bad value gives'
read_when:
  - Deploying a tifa app, or changing its port, body limit, VM count, render timeout or page cache
  - Setting the esbuild target or the static files folder of a build
  - The program refuses to start with a config error
  - Changing internal/config
---

English · [简体中文](config.zh-CN.md)

# Configuration

A tifa app runs with sensible defaults for a small machine. Change a setting only when you need to.

## Where settings come from

From highest to lowest priority:

1. Command-line flags of `serve`, such as `--server-addr 0.0.0.0:8080`.
2. `TIFA_*` environment variables, such as `TIFA_SERVER_ADDR`.
3. The deployment file given with `--config path.yaml` (`.yaml`, `.yml` or `.json`).
4. The project file `tifa.yaml` or `tifa.json` in the app root, embedded into the program by `tifa build`.
5. The defaults below.

The project file is read at build time and is the only place for `build.*` settings. Keep one project file: having both `tifa.yaml` and `tifa.json` fails the build.

```yaml
# tifa.yaml
build:
  target: es2020,safari14
server:
  addr: 0.0.0.0:8080
  body_limit: 4MiB
ssr:
  timeout: 2s
cache:
  size: 128MiB
```

Run `./bin/app config` to print the merged settings of all sources as a commented file. It is a good starting point for a deployment file.

## Settings

| Key | Default | Flag and environment variable | What it does |
| --- | --- | --- | --- |
| `build.target` | `es2020` | project file only | JavaScript target of the browser bundle, written like esbuild's `--target`: one ECMAScript version plus browsers with versions, from chrome, edge, firefox, ios, opera and safari. The SSR bundle always targets ES2022 |
| `build.public` | `public` | project file only | Folder of static files copied into the program as they are and served from `/` |
| `server.addr` | `127.0.0.1:8080` | `--server-addr`, `TIFA_SERVER_ADDR` | Address to listen on. Use `0.0.0.0:8080` in a container |
| `server.body_limit` | `1MiB` | `--server-body-limit`, `TIFA_SERVER_BODY_LIMIT` | Largest request body. A bigger one is answered with 413 |
| `server.shutdown_timeout` | `10s` | `--server-shutdown-timeout`, `TIFA_SERVER_SHUTDOWN_TIMEOUT` | After a stop signal, the time in-flight requests get to finish, and then the time `OnShutdown` gets |
| `ssr.vms` | the CPU count (`GOMAXPROCS`) | `--ssr-vms`, `TIFA_SSR_VMS` | Prewarmed JavaScript VMs, which is how many pages render at once. In a container it follows the CPU limit |
| `ssr.vm_memory` | `128MiB` | `--ssr-vm-memory`, `TIFA_SSR_VM_MEMORY` | Memory limit of each VM. A render that runs out of memory fails, and the VM is replaced with a fresh one |
| `ssr.timeout` | `3s` | `--ssr-timeout`, `TIFA_SSR_TIMEOUT` | Time budget of one render. A render that runs longer is stopped and handled as an unexpected error, which goes to `OnError` |
| `cache.size` | `64MiB` | `--cache-size`, `TIFA_CACHE_SIZE` | Size of the HTML cache of pages with `Cache = true`. `0` turns it off |

## Value formats

- Sizes are whole numbers with an optional unit: `B`, `KiB`, `MiB` or `GiB`, such as `512KiB`. A plain number is bytes. Only `cache.size` may be 0.
- Durations use Go's format and must be positive: `500ms`, `3s`, `1m30s`.
- `ssr.vms` is a whole number of at least 1.

## Errors

`tifa build` fails on a bad project file. The program refuses to start and names the source and the key when:

- a file or variable names an unknown key, such as `TIFA_SERVER_PORT`,
- a value has the wrong type or format, such as `ssr.timeout: 3`,
- a deployment file sets a `build.*` key. There are no `build.*` flags or `TIFA_BUILD_*` variables.

`serve --dist <folder>` is a flag for [`tifa dev`](dev.md) only. It is not a setting and has no environment variable.
