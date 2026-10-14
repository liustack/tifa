---
summary: 'How to use tifa dev: how pages update after each kind of change, which steps run, how Vite fits in, how to read the log, what the page and the app do when a build fails, where dev output goes, and how large apps are watched'
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

On start it prints `tifa dev: serving http://<address>`, then runs a full build (the same steps as [tifa build](build.md)) and starts the app. The browser connects to `--addr` only. tifa dev forwards page and data requests to the app, and requests under `/__tifa/vite/`, the browser modules and Vite's HMR websocket, to Vite's dev server. While the app restarts or loads a new SSR bundle, page requests wait for it. Ctrl-C or SIGTERM stops the app, Vite and tifa dev. A Vite build or plugin that does not end gets 5 seconds, then Vite is stopped. If tifa dev itself is killed, Vite stops on its own within the same 5 seconds.

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
- At the same time tifa dev rebuilds the SSR bundle, without go build, and the running app loads it from `.tifa/dist` in place of the old one. The app does not restart: `OnStart` does not run again, and what the app keeps in memory stays. A page request that arrives during this waits for the new bundle, so a reload always shows the new server render.
- The dev program calls `server.DevMain` in place of `server.Main`. Only it answers tifa dev's `POST /__tifa/dev/reload`, and it loads the bundle with the same code that loads it at start. The program `tifa build` makes calls `server.Main` and has no such path.
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

When a render calls Intl or a `toLocale*` method without a locale, or for a date without a `timeZone`, the app logs a `tifa: ssr:` warning that names the place in your sources, once per place. The page renders as usual. See [Warnings in tifa dev](app.md#warnings-in-tifa-dev).

## When a build fails

- The old version keeps running. The log prints `build failed: <reason>`, and open pages get an error overlay showing the original message of the Go compiler or of Vite.
- Once fixed, the overlay goes away. Pages reload when the fix needed a go build, and stay otherwise. Steps left unfinished by the failed build are folded into the next rebuild.
- A module that fails to compile in the browser shows Vite's own overlay.
- If the very first build fails, there is no old version to serve. Every request gets 502 with the original error, and things recover on their own once fixed.
- If the app exits right after starting, the overlay title is `The app did not start` and the body shows why the app exited.
- If the running app cannot load a rebuilt SSR bundle, such as one QuickJS cannot compile, or one whose top-level code throws, the overlay title is `The app did not load the SSR bundle`. The app keeps serving the bundle it had. The stack trace gives places in the source files, and a package that calls `require`, which QuickJS does not have, is named with what to do.

You can close the overlay. Closing it does not affect later reloads.

Reloads and the overlay come over tifa dev's own connection, `/__tifa/dev/events`, not Vite's HMR websocket. Vite's dev server only starts in the first bundle step, after static analysis and code generation, and an app without pages never starts it. The page that answers while no app runs, after a first build that failed or a crash, must still show the error and reload once things are fixed, and that works only over a connection that tifa dev itself serves from the start. Every tab of a browser shares this connection through a SharedWorker.

## Where files go

| Path | Contents |
| --- | --- |
| `.tifa/` | Generated code, the same as `tifa build` |
| `.tifa/dist/` | The SSR bundle, `build.json` and the public files. The browser modules come from Vite's dev server instead |
| `bin/tifa-dev` | The dev executable |

When stopping the app, tifa dev waits at most 3 times the configured `server.shutdown_timeout` plus 1 second, then kills the whole process group.

## Large apps

tifa dev watches folders with the system's file events. When an app has more folders or files than the system lets one program watch comfortably, tifa dev polls the rest: it looks at them about once a second and rebuilds as usual. Nothing needs to be set up. The log says so once:

```text
tifa dev: 07:35:36 watching app/assets would use up the system's file watches, so it and the directories found after it are polled for changes
```

- On macOS, watching a folder takes a file descriptor for the folder and one for each entry in it. tifa dev uses at most half of what one process may open (61,440 on a default macOS, so 30,720), and polls the folders beyond that.
- On Linux, watching a folder takes one of the user's inotify watches, which editors and other tools share. tifa dev uses at most half of `fs.inotify.max_user_watches`.
- If the system runs out first, because other programs hold many watches or files, tifa dev polls the folders it could not watch, gives back a few watches it holds, and keeps working.
- A poll lists the polled folders and hashes only the files whose size, modification time or identity changed. It runs at most once a second, and waits ten times as long as the last poll took, so polling uses at most a tenth of one CPU core. With tens of thousands of polled files, a change can take a few seconds to show.
- Polled folders stay polled until tifa dev restarts.
