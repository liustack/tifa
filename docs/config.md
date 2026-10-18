---
summary: 'Every setting of a tifa app: where each one can be set, which source wins, the default, the flag and environment variable names, the value formats and the errors a bad value gives'
read_when:
  - Deploying a tifa app, or changing its port, body limit, VM count, VM stack depth, render timeout or page cache
  - Setting the browser target or the static files folder of a build
  - The program refuses to start with a config error
  - Changing internal/config
---

English · [简体中文](config.zh-CN.md)

# Configuration

A tifa app runs with sensible defaults for a small machine. Change a setting only when you need to.

## Where settings come from

From highest to lowest priority:

1. Command-line flags of `serve`, such as `--server-addr 0.0.0.0:9527`.
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
  addr: 0.0.0.0:9527
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
| `build.target` | `es2020` | project file only | JavaScript target of the browser bundle that Vite builds, a comma-separated list: one ECMAScript version (`es2015` to `es2029`, `esnext`) plus browsers with versions, from chrome, edge, firefox, ios, opera and safari, such as `es2019,chrome90,safari14`. The SSR bundle always targets ES2022 |
| `build.public` | `public` | project file only | Folder of static files copied into the program as they are and served from `/` |
| `build.skip_tanstack_check` | `false` | project file only | `true` builds an app whose TanStack Router is not the version tifa is tested with, which may leave pages blank in the browser. See [build.md](build.md#the-apps-tsconfig-and-vite-config) |
| `server.addr` | `127.0.0.1:9527` | `--server-addr`, `TIFA_SERVER_ADDR` | Address to listen on. Use `0.0.0.0:9527` in a container |
| `server.body_limit` | `1MiB` | `--server-body-limit`, `TIFA_SERVER_BODY_LIMIT` | Largest request body. A bigger one is answered with 413 |
| `server.shutdown_timeout` | `10s` | `--server-shutdown-timeout`, `TIFA_SERVER_SHUTDOWN_TIMEOUT` | After a stop signal, the time in-flight requests get to finish, and then the time `OnShutdown` gets. Requests still running when it ends are canceled and get one more `shutdown_timeout` to return. `OnShutdown` runs once they have returned and is skipped if they have not. Either way the program exits with status 1 |
| `ssr.vms` | the CPU count (`GOMAXPROCS`) | `--ssr-vms`, `TIFA_SSR_VMS` | Prewarmed JavaScript VMs, which is how many pages render at once. In a container it follows the CPU limit |
| `ssr.vm_memory` | `128MiB` | `--ssr-vm-memory`, `TIFA_SSR_VM_MEMORY` | Memory limit of each VM. A render that runs out of memory fails, and the VM is replaced with a fresh one. Page code cannot catch it |
| `ssr.vm_stack_depth` | `10000` | `--ssr-vm-stack-depth`, `TIFA_SSR_VM_STACK_DEPTH` | How deep JavaScript calls may nest in each VM, about one level per call. Deeper calls throw `RangeError: Maximum call stack size exceeded`, at about the depth Node does. From 1 to 10000, see [Stack depth](#stack-depth) |
| `ssr.timeout` | `3s` | `--ssr-timeout`, `TIFA_SSR_TIMEOUT` | Time budget of one render. A render that runs longer is stopped and handled as an unexpected error, which goes to `OnError`. Page code cannot catch it |
| `cache.size` | `64MiB` | `--cache-size`, `TIFA_CACHE_SIZE` | Size of the HTML cache of pages with `Cache = true`. `0` turns it off |

## Stack depth

With the default `ssr.vm_stack_depth` of 10000, plain recursion goes as deep in a render as in Node, which stops at about 10,400 calls. Each level takes room on the stack of the Go goroutine that renders, not in the memory `ssr.vm_memory` limits. A normal page needs a few hundred levels and a few MB of stack. Code that recurses without end goes all the way down before it throws, and the stack of that render then grows to about 16 MB. Go takes it back at the next garbage collection. The stack grows by doubling, and when a garbage collection is running as it grows, the old stack is freed only once the collection ends, so for a moment the render can hold about 24 MB.

Every VM can do this at the same moment, so the worst case is `ssr.vms` times 24 MB on top of the memory of the VMs. On a small machine, lower the depth. Measured on darwin/arm64 and linux/amd64, where a level takes about 1 KB:

| `ssr.vm_stack_depth` | Stack of a render that recurses without end |
| --- | --- |
| `10000` | about 16 MB, up to about 24 MB while it grows |
| `5000` | about 8 MB |
| `2000` | about 2 MB |
| `1000` | about 1 MB |

On Linux, each level also keeps up to about 2 KB of memory the engine uses for its own local variables, so a VM that once recursed 10000 levels deep holds about 20 MB more. This memory is not in `ssr.vm_memory` either, and the VM keeps it until it closes. A render that fails closes the VM and starts a new one, so the memory comes back then. When the page catches the `RangeError` and the render succeeds, the VM stays and keeps it.

What a lower depth breaks:

- Plain recursive code that goes deeper than the limit, such as a function walking a tree of comments or categories, in your code or in a library. It throws the `RangeError`, and the render fails unless the code catches it.
- `JSON.parse` of data nested deeper than the limit, the data Go hands to the render included. Node has no such limit.
- Not React: when a deep component tree hits the limit, React renders the rest of that tree from a fresh stack. Only far below 1000 do pages fail, with odd errors from libraries that ran out of stack halfway through their work, and with a few dozen the bundle fails to load when the program starts. Every page of the example shop still renders at 200.

`2000` keeps such pages working and holds a render that recurses without end to about 2 MB.

## Value formats

- Sizes are whole numbers with an optional unit: `B`, `KiB`, `MiB` or `GiB`, such as `512KiB`. A plain number is bytes. Only `cache.size` may be 0.
- Durations use Go's format and must be positive: `500ms`, `3s`, `1m30s`.
- `ssr.vms` is a whole number of at least 1.
- `ssr.vm_stack_depth` is a whole number from 1 to 10000. A deeper stack would let code that recurses without end grow the goroutine stack past Go's limit, and the whole process would exit.
- `build.skip_tanstack_check` is `true` or `false`, without quotes.

## Errors

`tifa build` fails on a bad project file. The program refuses to start and names the source and the key when:

- a file or variable names an unknown key, such as `TIFA_SERVER_PORT`,
- a value has the wrong type or format, such as `ssr.timeout: 3`,
- a deployment file sets a `build.*` key. There are no `build.*` flags or `TIFA_BUILD_*` variables.

`serve --dist <folder>` is a flag for [`tifa dev`](dev.md) only. It is not a setting and has no environment variable.
