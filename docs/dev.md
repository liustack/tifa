---
summary: 'How to use tifa dev: which steps each kind of change rebuilds, how to read the log, what the page and the app do when a build fails, and where dev output goes'
read_when:
  - Running a tifa app locally for the first time
  - Changing internal/devserver, the dev command in cmd/tifa, or the incremental build in internal/bundle
  - tifa dev did not reload after a file change, or reports an error you do not understand
---

English · [简体中文](dev.zh-CN.md)

# tifa dev

```sh
tifa dev [--dir app-root] [--addr 127.0.0.1:9527]
```

On start it prints `tifa dev: serving http://<address>`, then runs a full build (the same steps as [tifa build](build.md)) and starts the app. The browser connects to `--addr` and tifa dev forwards requests to the app. While the app restarts, requests wait for it to come back. Ctrl-C or SIGTERM stops both the app and tifa dev.

Bundling uses the esbuild built into tifa.

## What each change triggers

tifa dev watches the app root and skips `.git`, `.tifa`, `node_modules`, and `bin/`, where the dev executable lives. Changes that arrive together are handled as one batch once the files go quiet.

| What changed | Steps that run |
| --- | --- |
| `.ts`, `.tsx`, `.js`, `.css`, `.json` | bundle |
| `.go`, `go.mod`, `go.sum` | static analysis, go build |
| As above, and the route manifest changed (routes, the result types of Load and Layout, the input and result types of handlers, hooks, etc.) | static analysis, code generation, bundle, go build |
| A `route.tsx` or `layout.tsx` added or removed, or `tifa.yaml` or `tifa.json` changed | all steps |

Whether the route manifest changed is decided by the `.tifa/` content generated after static analysis. If only function bodies changed, the generated content stays the same, and generation and bundling are skipped.

After every rebuild the app restarts and open pages reload. Frontend-only changes need no go build: the dev program starts with `serve --dist .tifa/dist` and reads the new frontend output directly after a restart.

Images, fonts and other files in the public folder do not trigger a rebuild. They are copied on the next bundle.

## Log

Each step that runs prints one line with its duration. The last line is the total time from the start of the rebuild until the app is ready:

```text
tifa dev: 07:35:36 app/home/route.tsx modified
tifa dev: 07:35:36 bundle done in 51ms
tifa dev: 07:35:37 ready in 201ms
```

The step names are `static analysis`, `code generation`, `bundle` and `go build`. After static analysis there is also a `route manifest changed` or `route manifest unchanged` line. The app's own output is printed as usual.

## When a build fails

- The old version keeps running. The log prints `build failed: <reason>`, and open pages get an error overlay showing the compiler's or esbuild's original message.
- Once fixed, the overlay goes away and the page reloads. Steps left unfinished by the failed build are folded into the next rebuild.
- If the very first build fails, there is no old version to serve. Every request gets 502 with the original error, and things recover on their own once fixed.
- If the app exits right after starting, the overlay title is `The app did not start` and the body shows why the app exited.

You can close the overlay. Closing it does not affect later reloads.

## Where files go

| Path | Contents |
| --- | --- |
| `.tifa/` | Generated code, the same as `tifa build` |
| `.tifa/dist/` | Frontend output, updated by incremental builds |
| `bin/tifa-dev` | The dev executable |

When stopping the app, tifa dev waits at most 3 times the configured `server.shutdown_timeout` plus 1 second, then kills the whole process group.

## Too many watched files

On macOS, watching a folder takes one file descriptor per file inside it. If the error says there are not enough descriptors, raise the limit with `ulimit -n`. On Linux, if it reports the watch limit was reached, raise `fs.inotify.max_user_watches`.
