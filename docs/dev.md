---
summary: 'How to use tifa dev: how pages update after each kind of change, which steps run, how Vite fits in, how to read the log, what the page and the app do when a build fails, how errors of the browser show, how `tifa errors` prints the errors since the last build, where dev output goes, and how large apps are watched'
read_when:
  - Running a tifa app locally for the first time
  - Changing internal/devserver, the dev command in cmd/tifa, or the dev build in internal/bundle
  - tifa dev did not update the page after a file change, or reports an error you do not understand
  - Checking from a script or a coding agent whether a change broke the build, a request or a page in the browser, or changing `tifa errors`
---

English · [简体中文](dev.zh-CN.md)

# tifa dev

```sh
tifa dev [--dir app-root] [--addr 127.0.0.1:9527]
```

On start it prints `tifa dev: serving http://<address>`, then runs a full build (the same steps as [tifa build](build.md), and the first one of an app takes several seconds to a minute, see [Log](#log)) and starts the app. The browser connects to `--addr` only. tifa dev forwards page and data requests to the app, and requests under `/__tifa/vite/`, the browser modules and Vite's HMR websocket, to Vite's dev server. While the app restarts or loads a new SSR bundle, page requests wait for it. Ctrl-C, SIGTERM and SIGHUP, which a terminal sends when it closes, stop the app, Vite and tifa dev. A tifa dev started with `nohup` ignores SIGHUP. A Vite build or plugin that does not end gets 5 seconds, then Vite is stopped. If tifa dev itself is killed, the app and Vite stop on their own: the app shuts down as it does on SIGTERM, and Vite within the same 5 seconds. tifa dev hands the app and Vite only the files it names for them, not the ones it inherited, such as the lock a script holds with `flock`.

An app with a `package.json` needs its npm packages first. Right after a clone, `node_modules` is missing, and tifa dev (like `tifa build`) stops before it starts anything and prints the command to run: `pnpm install` when pnpm is on PATH, `npm install` otherwise. An app without pages has no `package.json` and needs no Node.

tifa dev does not run on Windows yet. Use WSL.

A second `tifa dev` in the same app does not start. It prints `tifa: tifa dev is already running in this app at http://<address> (pid <N>)` and exits with an error, before it starts Vite. `tifa build` stops the same way while a tifa dev runs, since both write `.tifa/dist`. The lock is `.tifa/dev.lock`, and it goes away with the process that holds it. Without `--addr`, a taken port 9527 moves tifa dev on to 9528 and up, ten ports in all, and the `serving` line names the one it took. With `--addr`, tifa dev uses exactly that address and fails when it is taken.

tifa dev serves plain HTTP. The [HTTPS settings](config.md#https) of the project file apply to the built program only: the dev program ignores them.

## Vite

tifa dev runs the app's own Vite on Node, with the app's `vite.config.ts` (see [build.md](build.md#the-apps-tsconfig-and-vite-config)):

- Vite's dev server serves the browser modules. It starts with the first build of an app with pages and listens on a free port of 127.0.0.1 that only tifa dev talks to. Pages load the Vite client, the React Fast Refresh runtime of `@vitejs/plugin-react` and `.tifa/client.ts` from it.
- The SSR bundle is still a file, `.tifa/dist/ssr.js`, which the app loads into its QuickJS VMs. It is built on the same Node process as the dev server, on a worker thread of its own: the browser gets the development build of React and QuickJS runs the production one, and Vite picks that build once per thread. tifa sets `NODE_ENV` itself (development for the dev server, production for the SSR bundle), so a `NODE_ENV` in the shell changes neither.

## What each change triggers

tifa dev watches the app root and skips `.git`, `.tifa`, `node_modules`, and `bin/`, where the dev executable lives. Changes are handled in batches. A file joins a batch once it stays unchanged for 100 ms, so a build does not start in the middle of a save, and a batch starts once all its files have. A file that keeps changing, such as one a tool writes over and over, joins after one second anyway, and the files that are ready then go without waiting for the rest, so nothing is held back for much longer than a second.

| What changed | Steps that run | Open pages |
| --- | --- | --- |
| `.ts`, `.tsx`, `.js`, `.css`, `.json` outside `content/` | bundle (the SSR bundle) | Update in place through Vite |
| A file added to the public folder or removed from it | bundle (the code is generated again first) | Through Vite, as for any front end change |
| `.env` in the app root | none: the app starts again and reads it | Reload |
| `.go`, `go.mod`, `go.sum` | static analysis, go build | Reload |
| A file the Go code embeds with `go:embed`, of any kind, such as a Markdown post: edited | go build (and the bundle too, for a `.ts`, `.tsx`, `.js`, `.css` or `.json` file) | Reload |
| A file the Go code embeds, added or removed | static analysis, go build | Reload |
| As above, and the route manifest changed (routes, the result types of Load and Layout, the input and result types of handlers, hooks, etc.) | static analysis, code generation, bundle, go build | Reload |
| A `page.tsx`, `layout.tsx` or `layout_.tsx` added or removed, or `tifa.yaml` changed | all steps | Reload |
| A Markdown file of a collection under `content/`, its body or its front matter, also one added or removed, or with a new `slug` or `translationKey` | content: the scan of `content/` in Go and the conversion of that entry | Reload |
| Another file under `content/`, of any kind, such as an image, a `.json` or a `.css` of a page bundle | content: the entries in its folder, and the translations that fall back to that folder | Reload |
| A folder added to `content/` or removed from it, also an empty one | content: the scan of `content/`, which reports a folder no collection declares | Reload |
| `content.go` or a schema type | static analysis, go build (code generation and bundle when the types of the pages change), content of every entry | Reload |
| `vite.config.ts` or a plugin file it imports | bundle, then content of every entry, once Vite's dev server restarted. In an app without pages, which runs no dev server, any front end file, `package.json` included, converts every entry | Reload |

- A front end change reaches open pages through Vite: React Fast Refresh swaps the edited components and keeps their state, and new CSS replaces the old. Vite reloads the page itself when a module cannot be swapped, such as one that exports more than components. A change to `vite.config.ts` restarts Vite's dev server, and one to `tsconfig.json` reloads the page. Vite's dev server may keep importing the old target of a changed `paths` entry until tifa dev restarts.
- At the same time tifa dev rebuilds the SSR bundle, without go build, and the running app loads it from `.tifa/dist` in place of the old one. The app does not restart: `OnStart` does not run again, and what the app keeps in memory stays. A page request that arrives during a rebuild that makes a new SSR bundle waits for it, up to 30 seconds, so a reload shows the new server render. An SSR bundle that builds while Vite's dev server restarts waits for the restart, and builds again on the new server if the restart made it fail. This holds from the start of the rebuild, also when it redoes a bundle that a failed rebuild left undone, or for a Go change, from the moment static analysis finds a new route manifest, before code generation.
- The dev program calls `server.DevMain` in place of `server.Main`. Only it answers tifa dev's `POST /__tifa/dev/reload`, and it loads the bundle with the same code that loads it at start. Only it stops when tifa dev ends, by reading the pipe that tifa dev passes to it with `--lifeline-fd`. The program `tifa build` makes calls `server.Main` and has no such path or flag.
- Whether the route manifest changed is decided by the `.tifa/` content generated after static analysis. If only function bodies changed, the generated content stays the same, and generation and bundling are skipped.
- A file outside the app root that Vite's dev server loads, such as the source of another package in a workspace or a text file imported with `?raw`, is watched by Vite rather than tifa dev. When it changes, tifa dev rebuilds the SSR bundle too, whatever the kind of file, as it does for a front end file of the app, and the log names it by its path from the app root. Such a file can change while Vite's dev server restarts, when neither the old watcher nor the new one sees it. So each time restarts of the dev server end, whatever started them and whether they worked, tifa dev rebuilds the SSR bundle once (restarts that overlap count as one), and the log says `Vite's dev server restarted`. The new watcher goes on watching the files outside the root that the old one watched.
- Vite restarts its dev server without waiting for the new file watcher to read the app root, and a file the watcher has not reached has no starting point yet: an edit before that is never seen, so a module the dev server already served stays old until the file changes again. So after a restart the dev server answers requests once its watcher has read the files, which takes longer in a large app.
- A file that `go:embed` takes into the program is part of it, so a change builds it again, though the file is not Go. tifa dev learns which files these are from the static analysis, which asks the go command for the embedded files and patterns of every package in the module, and it watches them from then on, also a new file that a pattern such as `//go:embed posts` or `posts/*.md` takes. Folders named without `all:` leave out names that start with `.` or `_`, as the go command does, and so does a folder with its own `go.mod`, which is another module. The analysis runs again for a file that appears or disappears, since it can change which files are embedded, and an edit only needs go build. A file that is both embedded and a front end file, such as a `.json` the page imports, runs both steps. An embedded file in a folder that tifa dev skips (see above) is not watched. Until the first analysis works, embedded files are not known.
- Changing the content of an image, font or other file in the public folder triggers no rebuild. It is copied on the next bundle. A file added to the public folder or removed from it runs the bundle step, which generates the code again, since the generated route code lists the public files (see [Route tree](build.md#route-tree)). The log names the file as it does any change. A change of `build.public` in `tifa.yaml` moves the folder that is watched, with no restart.
- Content does not need go build: the dev program reads `content.json` and the bodies from `.tifa/dist` (see [Content](content.md)). The content step rescans `content/` in Go, converts the entries that changed in the Node process of Vite's dev server, and writes `.tifa/dist/content/` and `content.json`. Each conversion writes its bodies into a folder of its own under `.tifa/dist/content/`, so the bodies the app shows never change under it. The running app then loads the content again, runs `Validate` on every entry and takes the new data and bodies, and a page you load after that shows the new content. The app does not restart and `OnStart` does not run again. Every page of the whole-page cache expires. The first start converts every entry once. After a Go change in an app with collections the content step runs too, since the collections or their schema types may have changed, and converts every entry when they did.
- Under tifa dev the content holds drafts, with `Draft` true, and entries whose date is still to come, with their `Date`, so you see a post in its lists and on its page while you write it. A page can mark them.
- A front matter that does not fit the schema, a folder under `content/` that no collection declares, or an error of a plugin shows the overlay with the message of `tifa build`, and the app keeps the content it had. An entry that fails `Validate` shows the overlay titled `The app did not load the content`, and the app keeps the content it had too, with its old bodies.
- Stylesheets go through Vite's dev server, so `@tailwindcss/vite` compiles Tailwind CSS the way it does in any Vite app.
- The head of a page links the stylesheets that the server render imports, from Vite's dev server, so the page is styled before its scripts run, as a built page is. Vite updates these links when a stylesheet changes. A stylesheet that only `.client` modules import arrives with their scripts.
- An image or other file a module imports by URL gets the URL of Vite's dev server in the server render too, the same one the browser modules use, so hydration matches. `tifa build` gives it a hashed file under `/assets/`.
- `build.target` only applies to `tifa build`. Vite's dev server sends modern JavaScript.

## Log

Each step that runs prints one line with its duration. The last line is the total time from the start of the rebuild until the app is ready:

```text
tifa dev: 07:35:36 app/home/page.tsx modified
tifa dev: 07:35:36 bundle done in 90ms
tifa dev: 07:35:36 ready in 260ms
```

A step that runs longer than 5 seconds says so, and again every 15 seconds. The first go build of an app compiles QuickJS, SQLite and the other dependencies, which takes about 7 seconds on an Apple M4 with an empty build cache and longer on a slower machine, and later builds use Go's build cache. The first ready line also names the address to open:

```text
tifa dev: 07:35:41 go build still running (5s). The first build compiles QuickJS and the other dependencies, later builds use Go's build cache
tifa dev: 07:35:56 go build still running (20s)
tifa dev: 07:36:30 go build done in 54.1s
tifa dev: 07:36:30 ready in 58.3s, open http://127.0.0.1:9527
```

A page the browser opens during the first build (a GET or HEAD that accepts `text/html`) waits 2 seconds for it. If the build is still running, the page answers 503 and says tifa dev is building the app, and reloads on its own once the app runs. Other requests, such as a script calling an API, wait for the app as they do while it restarts.

The step names are `static analysis`, `code generation`, `bundle` and `go build`. After static analysis there is also a `route manifest changed` or `route manifest unchanged` line. Vite's warnings and errors are printed as `tifa dev:` lines too. The app's own output is printed as usual, without the `tifa: listening on` line of a built program, since the app listens on a port only tifa dev talks to. Its stderr passes through tifa dev, which keeps the last lines for the overlay (see below), so a logger that colors its output only on a terminal writes plain text there.

When a render calls Intl or a `toLocale*` method without a locale, or for a date without a `timeZone`, the app logs a `tifa: ssr:` warning that names the place in your sources, once per place. The page renders as usual. See [Warnings in tifa dev](app.md#warnings-in-tifa-dev).

tifa dev renders each page twice, the second time with the clock 36 hours on. When the two renders of the same data differ, the page read the time or a random number while rendering, and the render cache would freeze that value. The terminal and an overlay on the page say so, with the HTML around the first place where the renders part and the places in your code that read the time (`Date.now()`, `new Date()`, `Date()`) or a random number (`Math.random()`, `crypto`). Reads inside packages are named by the line of your code that called them, and those of React, TanStack Router and tifa are left out. Change the component, or write `RenderCache = false` in the `route.go` of the page (see [SSR and cache](app.md#ssr-and-cache)). Such a page is not cached until it is fixed. A built program does not render twice.

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

## Errors in the browser

Errors that happen in the browser show in a small panel in the bottom right corner. It leaves the page usable, and the overlay of a build error covers it and stays on top.

- `Hydration mismatch`: React could not reuse the server HTML and rendered the page again in the browser. The panel shows React's message with the difference, and the component stack. Other errors React recovers from by itself are titled `Recoverable error`.
- `Uncaught error`: a component threw with no error boundary around it, or any other error reached `window`.
- `Unhandled rejection`: a promise failed and nothing handled it.

The same error again adds to a counter (`×3`) instead of adding an entry. The panel keeps the last 20 different errors. Each can be dismissed, and a reload clears them. Errors that an `Error` component caught are not shown, as before they only reach the console. tifa dev keeps the same errors for [tifa errors](#tifa-errors). A built app has none of this: the code is only in the dev build.

## When a page fails to render

When a component throws while the page renders on the server, the app renders the page again with the error shown by an `Error` component (see [Errors on a page](app.md#errors-on-a-page)). Under tifa dev an overlay over that page names the request, shows the public message when it differs from the status text, and shows the whole error with its stack trace in your source files. When no `Error` renders, or a page request fails before the app can render a page, such as when `OnRequest` returns an unexpected error, the app answers with a plain page from Go, which under tifa dev shows the same. Either page has the status code that `OnError` picked, so `curl -i` sees it too, and reloads once you save a fix, also a fix in TSX that does not reload other pages. A built app shows only the status and the public message.

These pages show internal errors. tifa dev listens on `127.0.0.1` by default. With `--addr 0.0.0.0:<port>`, other machines on the network can see them.

Reloads and the overlay come over tifa dev's own connection, `/__tifa/dev/events`, not Vite's HMR websocket. Vite's dev server only starts in the first bundle step, after static analysis and code generation, and an app without pages never starts it. The page that answers while no app runs, after a first build that failed or a crash, must still show the error and reload once things are fixed, and that works only over a connection that tifa dev itself serves from the start. Every tab of a browser shares this connection through a SharedWorker.

## tifa errors

```sh
tifa errors [--dir app-root] [--json]
```

While tifa dev runs, `go tool tifa errors` prints the errors since the last build started. A coding agent that sees neither the browser nor the terminal of tifa dev learns from it whether a change works. Run it after you change code and after you open or reload the pages you changed: errors of the browser come only from pages that are open.

It lists:

- What the overlay shows: `Build failed` with the original message of the Go compiler or of Vite, `The app did not start`, `The app stopped` or `The app did not load the SSR bundle`. It stays until the next build ends.
- Unexpected errors of requests, the ones that go to `OnError` (or are logged as a 500 without it): the request, the status and the message users see, and the error with its stack trace in your source files, as the error page shows them under tifa dev. Requests of API routes count too, also in an app without pages.
- Errors of the browser that pages reported: `Hydration mismatch`, `Recoverable error`, `Uncaught error` and `Unhandled rejection`, as the panel names them, with the page that reported each first.
- Pages whose two renders differ (see [Log](#log)).

Each kind keeps the last 20 different errors, and the same error again raises its count, as in the panel. When a build starts, the errors from before it are dropped. Asked during a build, the command waits for the build to end.

```text
Request failed: GET /orders?page=2, 503 Service Unavailable (×2)
  Users see: Come back later
  tifa: render page: ...

Hydration mismatch in the browser: http://127.0.0.1:9527/orders (×3)
  ...
```

Without errors it prints `No errors since the last build.` It exits with status 0 without errors, 1 with errors, and 2 when no tifa dev runs in the app: the process that `.tifa/dev.lock` names is gone, it does not listen yet, or nothing at its address answers for it. `--json` prints the same as JSON, with every field also when it is empty:

```json
{
  "failure": {"title": "Build failed", "error": "..."},
  "requests": [{"method": "GET", "url": "/orders?page=2", "status": 503, "message": "Come back later", "error": "...", "count": 2}],
  "browser": [{"kind": "recoverable", "title": "Hydration mismatch", "url": "http://127.0.0.1:9527/orders", "text": "...", "count": 3}],
  "renders": [{"method": "GET", "url": "/stats", "text": "...", "count": 1}]
}
```

`failure` is `null` when everything builds and runs. `kind` is `recoverable`, `uncaught` or `rejection`.

The command reads the process ID and the address that tifa dev wrote into `.tifa/dev.lock`, without taking the lock, so a tifa dev or tifa build can start at the same time. It asks that address for `/__tifa/dev/errors` and takes the answer only when it names the same process in `X-Tifa-Dev-Pid`. tifa dev answers there only under `localhost` or an IP address, and takes the reports of the browser only from its own pages, up to 128 KiB each. The app answers tifa dev for its own errors. A built app has none of this.

## Where files go

| Path | Contents |
| --- | --- |
| `.tifa/` | Generated code, the same as `tifa build` |
| `.tifa/dist/` | The SSR bundle, `build.json`, the public files, and `content.json` and the bodies in `content/` of an app with collections. The browser modules come from Vite's dev server instead |
| `bin/tifa-dev` | The dev executable |

The app runs in the app root, also when tifa dev starts elsewhere with `--dir`, the same as the program `tifa build` makes when you run it from the app root. Files the app opens by a relative path, such as the `app.db` of the [tifa new](build.md#tifa-new) template, land there. So does the `.env` it reads (see [.env](config.md#env)). tifa dev passes the address the app listens on in `SERVER_ADDR`, which wins over a `PORT` of your shell and over `.env`.

When stopping the app, tifa dev waits at most 3 times the configured `server.shutdown_timeout` plus 1 second, then kills the whole process group.

## Large apps

tifa dev watches folders with the system's file events. When an app has more folders or files than the system lets one program watch comfortably, tifa dev polls the rest: it looks at them about once a second and rebuilds as usual. Nothing needs to be set up. The log says so:

```text
tifa dev: 07:35:36 watching app/assets would use up the system's file watches, so it and the directories found after it are polled for changes
```

- On macOS, FreeBSD, OpenBSD, NetBSD and DragonFly, tifa dev watches with kqueue. A watched folder takes one file descriptor, and so does each file in it of a kind in the table above, each embedded file and each file under `content/`, also one that appears later. Other files, such as images outside `content/`, take none. tifa dev counts these against half of what one process may open (the smaller of the open file limit and `kern.maxfilesperproc`), and polls the folders beyond that, also a watched folder whose files grow past it.
- On Linux, watching a folder takes one of the user's inotify watches, which editors and other tools share. tifa dev uses at most half of `fs.inotify.max_user_watches`.
- If the system runs out first, because other programs hold many watches or files, tifa dev polls the folders it could not watch, gives back a few watches it holds, and keeps working. If it runs out while tifa dev runs, such as when a file cannot be opened, tifa dev stops watching with system events, polls every folder and compares every file again.
- A poll lists the polled folders and hashes only the files whose size, modification time or identity changed. Between two polls tifa dev waits at least a second, and at least ten times as long as the last poll took. A watched folder whose entries change is listed again under the same rule, without the one second minimum, so a folder that keeps growing costs no more. Polling and listing thus each take at most about a tenth of one CPU core, on top of handling file events and rebuilding.
- How long a poll takes grows with the number of files and depends on the machine and its disk. On an Apple M4, with the files in the system's cache, a poll of 20,000 files took between 45 and 65 ms in our runs. While a poll takes less than a tenth of a second, a change in a polled folder is found within about a second, and then built as usual.
- Polled folders stay polled until tifa dev restarts.
- Vite's dev server watches the app root too. Once tifa dev polls because the system is short, it tells Vite's dev server to poll as well, unless the dev server watches with FSEvents, as it usually does on macOS. FSEvents takes no file descriptor per file, so it stays.
- To poll, Vite's dev server restarts, and from then on looks at every file once a second, about 8% of one CPU core for 10,000 files on an Apple M4. That is a single restart when the restart succeeds and no other restart of the dev server, such as one after a change to `vite.config.ts`, runs at the same time. A restart that is running is waited for first, and if it worked and its new server already polls, nothing else restarts.
- A restart can fail. One that cannot create the new server, such as one with a broken `vite.config.ts`, keeps the old server and its watcher. One whose new server cannot listen, because another program took the port meanwhile, has already closed the old server, so there is no Vite dev server until the next restart. Either way the log says so, and tifa dev and the app keep running. The dev server tries again at the next error of its watcher, and the next restart that succeeds, such as one after a change to `vite.config.ts`, polls.
- When Vite's own watcher runs out of watches or file descriptors first (ENOSPC, EMFILE or ENFILE), the dev server switches to polling the same way and says so once. This holds for a watcher that uses FSEvents too.
