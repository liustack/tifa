---
summary: 'How to use tifa dev: how pages update after each kind of change, which steps run, how Vite fits in, how to read the log, what the page and the app do when a build fails, and where dev output goes'
read_when:
  - Running a tifa app locally for the first time
  - Changing internal/devserver, the dev command in cmd/tifa, or the dev build in internal/bundle
  - tifa dev did not update the page after a file change, or reports an error you do not understand
---

English · [简体中文](dev.zh-CN.md)

# tifa dev

```sh
tifa dev [--dir app-root] [--addr 127.0.0.1:9527]
```

On start it prints `tifa dev: serving http://<address>`, then runs a full build (the same steps as [tifa build](build.md)) and starts the app. The browser connects to `--addr` only. tifa dev forwards page and data requests to the app, and requests under `/__tifa/vite/`, the browser modules and Vite's HMR websocket, to Vite's dev server. While the app restarts, requests wait for it to come back. Ctrl-C or SIGTERM stops the app, Vite and tifa dev.

## Vite

tifa dev runs the app's own Vite on Node, with the app's `vite.config.ts` (see [build.md](build.md#the-apps-tsconfig-and-vite-config)):

- Vite's dev server serves the browser modules. It starts with the first build of an app with pages and listens on a free port of 127.0.0.1 that only tifa dev talks to. Pages load the Vite client, the React Fast Refresh runtime of `@vitejs/plugin-react` and `.tifa/client.ts` from it.
- The SSR bundle is still a file, `.tifa/dist/ssr.js`, which the app loads into its QuickJS VMs. It is built on the same Node process as the dev server, on a worker thread of its own: the browser gets the development build of React and QuickJS runs the production one, and Vite picks that build once per thread. tifa sets `NODE_ENV` itself (development for the dev server, production for the SSR bundle), so a `NODE_ENV` in the shell changes neither.

## What each change triggers

tifa dev watches the app root and skips `.git`, `.tifa`, `node_modules`, and `bin/`, where the dev executable lives. Changes that arrive together are handled as one batch once the files go quiet.

| What changed | Steps that run | Open pages |
| --- | --- | --- |
| `.ts`, `.tsx`, `.js`, `.css`, `.json` | bundle (the SSR bundle) | Update in place through Vite |
| `.go`, `go.mod`, `go.sum` | static analysis, go build | Reload |
| As above, and the route manifest changed (routes, the result types of Load and Layout, the input and result types of handlers, hooks, etc.) | static analysis, code generation, bundle, go build | Reload |
| A `route.tsx` or `layout.tsx` added or removed, or `tifa.yaml` or `tifa.json` changed | all steps | Reload |

- A front end change reaches open pages through Vite: React Fast Refresh swaps the edited components and keeps their state, and new CSS replaces the old. Vite reloads the page itself when a module cannot be swapped, such as one that exports more than components.
- At the same time tifa dev rebuilds the SSR bundle and restarts the app, without go build: the dev program starts with `serve --dist .tifa/dist` and reads the new bundle after a restart. A page request that arrives during this waits for the restarted app, so a reload always shows the new server render.
- Whether the route manifest changed is decided by the `.tifa/` content generated after static analysis. If only function bodies changed, the generated content stays the same, and generation and bundling are skipped.
- Images, fonts and other files in the public folder do not trigger a rebuild. They are copied on the next bundle.
- Stylesheets go through Vite's dev server, so `@tailwindcss/vite` compiles Tailwind CSS the way it does in any Vite app.
- The head of a page links the stylesheets that the server render imports, from Vite's dev server, so the page is styled before its scripts run, as a built page is. Vite updates these links when a stylesheet changes. A stylesheet that only `.client` modules import arrives with their scripts.
- An image or other file a module imports by URL gets the URL of Vite's dev server in the server render too, the same one the browser modules use, so hydration matches. `tifa build` gives it a hashed file under `/assets/`.
- `build.target` only applies to `tifa build`. Vite's dev server sends modern JavaScript.

## Log

Each step that runs prints one line with its duration. The last line is the total time from the start of the rebuild until the app is ready:

```text
tifa dev: 07:35:36 app/home/route.tsx modified
tifa dev: 07:35:36 bundle done in 90ms
tifa dev: 07:35:36 ready in 260ms
```

The step names are `static analysis`, `code generation`, `bundle` and `go build`. After static analysis there is also a `route manifest changed` or `route manifest unchanged` line. Vite's warnings and errors are printed as `tifa dev:` lines too. The app's own output is printed as usual.

## When a build fails

- The old version keeps running. The log prints `build failed: <reason>`, and open pages get an error overlay showing the original message of the Go compiler or of Vite.
- Once fixed, the overlay goes away. Pages reload when the fix needed a go build, and stay otherwise. Steps left unfinished by the failed build are folded into the next rebuild.
- A module that fails to compile in the browser shows Vite's own overlay.
- If the very first build fails, there is no old version to serve. Every request gets 502 with the original error, and things recover on their own once fixed.
- If the app exits right after starting, the overlay title is `The app did not start` and the body shows why the app exited.

You can close the overlay. Closing it does not affect later reloads.

## Where files go

| Path | Contents |
| --- | --- |
| `.tifa/` | Generated code, the same as `tifa build` |
| `.tifa/dist/` | The SSR bundle, `build.json` and the public files. The browser modules come from Vite's dev server instead |
| `bin/tifa-dev` | The dev executable |

When stopping the app, tifa dev waits at most 3 times the configured `server.shutdown_timeout` plus 1 second, then kills the whole process group.

## Too many watched files

On macOS, watching a folder takes one file descriptor per file inside it. If the error says there are not enough descriptors, raise the limit with `ulimit -n`. On Linux, if it reports the watch limit was reached, raise `fs.inotify.max_user_watches`.
