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

On start it prints `tifa dev: serving http://<address>`, then runs a full build (the same steps as [tifa build](build.md), and the first one of an app takes a minute or two, see [Log](#log)) and starts the app. The browser connects to `--addr` only. tifa dev forwards page and data requests to the app, and requests under `/__tifa/vite/`, the browser modules and Vite's HMR websocket, to Vite's dev server. While the app restarts or loads a new SSR bundle, page requests wait for it. Ctrl-C or SIGTERM stops the app, Vite and tifa dev. A Vite build or plugin that does not end gets 5 seconds, then Vite is stopped. If tifa dev itself is killed, Vite stops on its own within the same 5 seconds.

tifa dev does not run on Windows yet. Use WSL.

## Vite

tifa dev runs the app's own Vite on Node, with the app's `vite.config.ts` (see [build.md](build.md#the-apps-tsconfig-and-vite-config)):

- Vite's dev server serves the browser modules. It starts with the first build of an app with pages and listens on a free port of 127.0.0.1 that only tifa dev talks to. Pages load the Vite client, the React Fast Refresh runtime of `@vitejs/plugin-react` and `.tifa/client.ts` from it.
- The SSR bundle is still a file, `.tifa/dist/ssr.js`, which the app loads into its QuickJS VMs. It is built on the same Node process as the dev server, on a worker thread of its own: the browser gets the development build of React and QuickJS runs the production one, and Vite picks that build once per thread. tifa sets `NODE_ENV` itself (development for the dev server, production for the SSR bundle), so a `NODE_ENV` in the shell changes neither.

## What each change triggers

tifa dev watches the app root and skips `.git`, `.tifa`, `node_modules`, and `bin/`, where the dev executable lives. Changes are handled in batches. A file joins a batch once it stays unchanged for 100 ms, so a build does not start in the middle of a save, and a batch starts once all its files have. A file that keeps changing, such as one a tool writes over and over, joins after one second anyway, and the files that are ready then go without waiting for the rest, so nothing is held back for much longer than a second.

| What changed | Steps that run | Open pages |
| --- | --- | --- |
| `.ts`, `.tsx`, `.js`, `.css`, `.json` | bundle (the SSR bundle) | Update in place through Vite |
| `.env` or `.env.*` in the app root | bundle | Reload, as Vite restarts its dev server |
| `.go`, `go.mod`, `go.sum` | static analysis, go build | Reload |
| As above, and the route manifest changed (routes, the result types of Load and Layout, the input and result types of handlers, hooks, etc.) | static analysis, code generation, bundle, go build | Reload |
| A `route.tsx` or `layout.tsx` added or removed, or `tifa.yaml` or `tifa.json` changed | all steps | Reload |

- A front end change reaches open pages through Vite: React Fast Refresh swaps the edited components and keeps their state, and new CSS replaces the old. Vite reloads the page itself when a module cannot be swapped, such as one that exports more than components. A change to `vite.config.ts` restarts Vite's dev server, and one to `tsconfig.json` reloads the page. Vite's dev server may keep importing the old target of a changed `paths` entry until tifa dev restarts.
- At the same time tifa dev rebuilds the SSR bundle, without go build, and the running app loads it from `.tifa/dist` in place of the old one. The app does not restart: `OnStart` does not run again, and what the app keeps in memory stays. A page request that arrives during a rebuild that makes a new SSR bundle waits for it, up to 30 seconds, so a reload shows the new server render. An SSR bundle that builds while Vite's dev server restarts waits for the restart, and builds again on the new server if the restart made it fail. This holds from the start of the rebuild, also when it redoes a bundle that a failed rebuild left undone, or for a Go change, from the moment static analysis finds a new route manifest, before code generation.
- The dev program calls `server.DevMain` in place of `server.Main`. Only it answers tifa dev's `POST /__tifa/dev/reload`, and it loads the bundle with the same code that loads it at start. The program `tifa build` makes calls `server.Main` and has no such path.
- Whether the route manifest changed is decided by the `.tifa/` content generated after static analysis. If only function bodies changed, the generated content stays the same, and generation and bundling are skipped.
- A file outside the app root that Vite's dev server loads, such as the source of another package in a workspace or a text file imported with `?raw`, is watched by Vite rather than tifa dev. When it changes, tifa dev rebuilds the SSR bundle too, whatever the kind of file, as it does for a front end file of the app, and the log names it by its path from the app root. Such a file can change while Vite's dev server restarts, when neither the old watcher nor the new one sees it. So each time restarts of the dev server end, whatever started them and whether they worked, tifa dev rebuilds the SSR bundle once (restarts that overlap count as one), and the log says `Vite's dev server restarted`. The new watcher goes on watching the files outside the root that the old one watched.
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

A step that runs longer than 5 seconds says so, and again every 15 seconds. The first go build of an app compiles QuickJS, SQLite and the other dependencies, which can take a minute or two, and later builds use Go's build cache. The first ready line also names the address to open:

```text
tifa dev: 07:35:41 go build still running (5s). The first build compiles QuickJS and the other dependencies, later builds use Go's build cache
tifa dev: 07:35:56 go build still running (20s)
tifa dev: 07:36:30 go build done in 54.1s
tifa dev: 07:36:30 ready in 58.3s, open http://127.0.0.1:9527
```

A page the browser opens during the first build (a GET or HEAD that accepts `text/html`) waits 2 seconds for it. If the build is still running, the page answers 503 and says tifa dev is building the app, and reloads on its own once the app runs. Other requests, such as a script calling an API, wait for the app as they do while it restarts.

The step names are `static analysis`, `code generation`, `bundle` and `go build`. After static analysis there is also a `route manifest changed` or `route manifest unchanged` line. Vite's warnings and errors are printed as `tifa dev:` lines too. The app's own output is printed as usual, without the `tifa: listening on` line of a built program, since the app listens on a port only tifa dev talks to. Its stderr passes through tifa dev, which keeps the last lines for the overlay (see below), so a logger that colors its output only on a terminal writes plain text there.

When a render calls Intl or a `toLocale*` method without a locale, or for a date without a `timeZone`, the app logs a `tifa: ssr:` warning that names the place in your sources, once per place. The page renders as usual. See [Warnings in tifa dev](app.md#warnings-in-tifa-dev).

## When a build fails

- The old version keeps running. The log prints `build failed: <reason>`, and open pages get an error overlay showing the original message of the Go compiler or of Vite.
- Once fixed, the overlay goes away. Pages reload when the fix needed a go build, and stay otherwise. Steps left unfinished by the failed build are folded into the next rebuild.
- A module that fails to compile in the browser shows Vite's own overlay.
- If the installed TanStack Router is not the version tifa is tested with (see [build.md](build.md#the-apps-tsconfig-and-vite-config)), the bundle step fails with that error. tifa dev does not watch `node_modules`, so after installing the tested version, restart tifa dev.
- If the installed React or React DOM is older than 19.3.0 (see [build.md](build.md#the-apps-tsconfig-and-vite-config)), the bundle step fails with that error. Restart tifa dev after installing a newer one.
- If the very first build fails, there is no old version to serve. Every request gets 502 with the original error, and things recover on their own once fixed.
- If the app exits right after starting, or does not listen within 30 seconds, the overlay title is `The app did not start`. The body shows the exit status and the last 40 lines the app wrote to stderr, where an `OnStart` error and a panic go. The line of a panic stays even when its stack trace is longer, as long as the output ends with that panic's report. A `panic: ` line the app logged and went on from does not. If the running app exits, as after a panic outside a request, the title is `The app stopped` and the body is the same. Until the next build, page requests get 502 with the same text. The log of tifa dev only says the app exited, since the app's output is printed above it already.
- If the running app cannot load a rebuilt SSR bundle, such as one QuickJS cannot compile, or one whose top-level code throws, the overlay title is `The app did not load the SSR bundle`. The app keeps serving the bundle it had. The stack trace gives places in the source files, and a package that calls `require`, which QuickJS does not have, is named with what to do.

You can close the overlay. Closing it does not affect later reloads.

## When a page fails to render

When a page request fails before the app can render a page, such as when a component throws while the page renders on the server or `OnRequest` returns an unexpected error, the app answers with a plain page from Go. It has the status code that `OnError` picked, so `curl -i` sees it too. Under tifa dev that page also names the request, shows the public message when it differs from the status text, and shows the whole error. A render error gives its stack trace in your source files. The page reloads once you save a fix, also a fix in TSX that does not reload other pages. A built app shows only the status and the public message.

The page shows internal errors. tifa dev listens on `127.0.0.1` by default. With `--addr 0.0.0.0:<port>`, other machines on the network can see them.

Reloads and the overlay come over tifa dev's own connection, `/__tifa/dev/events`, not Vite's HMR websocket. Vite's dev server only starts in the first bundle step, after static analysis and code generation, and an app without pages never starts it. The page that answers while no app runs, after a first build that failed or a crash, must still show the error and reload once things are fixed, and that works only over a connection that tifa dev itself serves from the start. Every tab of a browser shares this connection through a SharedWorker.

## Where files go

| Path | Contents |
| --- | --- |
| `.tifa/` | Generated code, the same as `tifa build` |
| `.tifa/dist/` | The SSR bundle, `build.json` and the public files. The browser modules come from Vite's dev server instead |
| `bin/tifa-dev` | The dev executable |

The app runs in the app root, also when tifa dev starts elsewhere with `--dir`, the same as the program `tifa build` makes when you run it from the app root. Files the app opens by a relative path, such as the `app.db` of the [tifa new](build.md#tifa-new) template, land there.

When stopping the app, tifa dev waits at most 3 times the configured `server.shutdown_timeout` plus 1 second, then kills the whole process group.

## Large apps

tifa dev watches folders with the system's file events. When an app has more folders or files than the system lets one program watch comfortably, tifa dev polls the rest: it looks at them about once a second and rebuilds as usual. Nothing needs to be set up. The log says so:

```text
tifa dev: 07:35:36 watching app/assets would use up the system's file watches, so it and the directories found after it are polled for changes
```

- On macOS, FreeBSD, OpenBSD, NetBSD and DragonFly, tifa dev watches with kqueue. A watched folder takes one file descriptor, and so does each file in it of a kind in the table above, also one that appears later. Other files, such as images, take none. tifa dev counts these against half of what one process may open (the smaller of the open file limit and `kern.maxfilesperproc`), and polls the folders beyond that, also a watched folder whose files grow past it.
- On Linux, watching a folder takes one of the user's inotify watches, which editors and other tools share. tifa dev uses at most half of `fs.inotify.max_user_watches`.
- If the system runs out first, because other programs hold many watches or files, tifa dev polls the folders it could not watch, gives back a few watches it holds, and keeps working. If it runs out while tifa dev runs, such as when a file cannot be opened, tifa dev stops watching with system events, polls every folder and compares every file again.
- A poll lists the polled folders and hashes only the files whose size, modification time or identity changed. Between two polls tifa dev waits at least a second, and at least ten times as long as the last poll took. A watched folder whose entries change is listed again under the same rule, without the one second minimum, so a folder that keeps growing costs no more. Polling and listing thus each take at most about a tenth of one CPU core, on top of handling file events and rebuilding.
- How long a poll takes grows with the number of files and depends on the machine and its disk. On an Apple M4, with the files in the system's cache, a poll of 20,000 files took between 45 and 65 ms in our runs. While a poll takes less than a tenth of a second, a change in a polled folder is found within about a second, and then built as usual.
- Polled folders stay polled until tifa dev restarts.
- Vite's dev server watches the app root too. Once tifa dev polls because the system is short, it tells Vite's dev server to poll as well, unless the dev server watches with FSEvents, as it usually does on macOS. FSEvents takes no file descriptor per file, so it stays.
- To poll, Vite's dev server restarts, and from then on looks at every file once a second, about 8% of one CPU core for 10,000 files on an Apple M4. That is a single restart when the restart succeeds and no other restart of the dev server, such as one after a change to `vite.config.ts`, runs at the same time. A restart that is running is waited for first, and if it worked and its new server already polls, nothing else restarts.
- A restart can fail. One that cannot create the new server, such as one with a broken `vite.config.ts`, keeps the old server and its watcher. One whose new server cannot listen, because another program took the port meanwhile, has already closed the old server, so there is no Vite dev server until the next restart. Either way the log says so, and tifa dev and the app keep running. The dev server tries again at the next error of its watcher, and the next restart that succeeds, such as one after a change to `vite.config.ts`, polls.
- When Vite's own watcher runs out of watches or file descriptors first (ENOSPC, EMFILE or ENFILE), the dev server switches to polling the same way and says so once. This holds for a watcher that uses FSEvents too.
