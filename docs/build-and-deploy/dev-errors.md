---
summary: 'Find out why `tifa dev` shows an error: the log, a failed build, errors in the browser and a page that fails to render'
audience: app
order: 4
read_when:
  - 'Reading the log of `tifa dev`, or finding out why a build, a request or a render failed under it'
---

# Find out why `tifa dev` shows an error

`tifa dev` tells you what went wrong in its log, in the terminal that runs it and in the browser. This page says where to look.

## Log

Each step that runs prints one line with its duration. The last line is the total time from the start of the rebuild until the app is ready:

```text
tifa dev: 07:35:36 app/home/page.tsx modified
tifa dev: 07:35:36 bundle done in 90ms
tifa dev: 07:35:36 ready in 260ms
```

A step that runs longer than 5 seconds says so, and again every 15 seconds. The first go build of an app compiles beastjs, SQLite and the other dependencies, which takes about 7 seconds on an Apple M4 with an empty build cache and longer on a slower machine, and later builds use Go's build cache. The first ready line also names the address to open:

```text
tifa dev: 07:35:41 go build still running (5s). The first build compiles beastjs and the other dependencies, later builds use Go's build cache
tifa dev: 07:35:56 go build still running (20s)
tifa dev: 07:36:30 go build done in 54.1s
tifa dev: 07:36:30 ready in 58.3s, open http://127.0.0.1:9527
```

A page the browser opens during the first build (a GET or HEAD that accepts `text/html`) waits 2 seconds for it. If the build is still running, the page answers 503 and says `tifa dev` is building the app, and reloads on its own once the app runs. Other requests, such as a script calling an API, wait for the app as they do while it restarts.

The step names are `static analysis`, `code generation`, `bundle`, `styles`, `content` and `go build`. After static analysis there is also a `route manifest changed` or `route manifest unchanged` line. Vite's warnings and errors are printed as `tifa dev:` lines too. The app's own output is printed as usual, without the `tifa: listening on` line of a built program, since the app listens on a port only `tifa dev` talks to. Its stderr passes through `tifa dev`, which keeps the last lines for the overlay (see below), so a logger that colors its output only on a terminal writes plain text there.

Under `tifa dev` the app also writes a line for each request of a route, once it has answered: the method, the URL, the status, the time, the route and what the [render memo](../pages-and-data/ssr-and-cache.md#ssr-and-cache) did:

```text
2026/11/26 08:00:00 INFO GET /projects/studio 200 38ms /projects/{project} memo miss
2026/11/26 08:00:01 INFO GET /projects/studio 200 1ms /projects/{project} memo hit
2026/11/26 08:00:03 INFO GET /projects/studio?page=2 200 9ms /projects/{project} data
2026/11/26 08:00:04 INFO POST /api/orders 201 4ms /api/orders
```

`memo miss` is a page rendered in a VM, `memo hit` one the memo had, `memo shared` one that waited for the render another request had started, and `memo off` a page the memo does not keep (`Memo = false`, a 404 or an error page). `data` is the navigation data a client move asks for, which renders nothing. The route is the template of the page or handler and is missing when no route matched. The files of the browser bundle and of `public/`, the requests of `tifa dev` and Vite's modules get no line. A built program writes none, and `tifa dev` adds no header to answers: the memo word is read from `X-Tifa-Render`, which pages always carry. For a line in a built program, see [Logging each request](../guides/logs.md#logging-each-request).

When a render calls Intl or a `toLocale*` method without a locale, or for a date without a `timeZone`, the app logs a `tifa: ssr:` warning that names the place in your sources, once per place. The page renders as usual. See [Warnings in `tifa dev`](../pages-and-data/server-render.md#warnings-in-tifa-dev).

`tifa dev` renders each page twice, the second time with the clock 36 hours on. When the two renders of the same data differ, the page read the time or a random number while rendering, and the render memo would freeze that value. The terminal and an overlay on the page say so, with the HTML around the first place where the renders part and the places in your code that read the time (`Date.now()`, `new Date()`, `Date()`) or a random number (`Math.random()`, `crypto`). Reads inside packages are named by the line of your code that called them, and those of React, TanStack Router and Tifa are left out. Change the component, or write `Memo = false` in the `route.go` of the page (see [SSR and cache](../pages-and-data/ssr-and-cache.md#ssr-and-cache)). Such a page is not cached until it is fixed. A built program does not render twice.

Under `tifa dev`, what a page prints with `console.log` and the other console methods while it renders on the server is logged too, with the page and the place in your sources. See [Console in `tifa dev`](../pages-and-data/server-render.md#console-in-tifa-dev). To step through the Go code or a component, see [Debugging](debugging.md).

## When a build fails

- The old version keeps running. The log prints `build failed: <reason>`, and open pages get an error overlay showing the original message of the Go compiler or of Vite.
- Once fixed, the overlay goes away. Pages reload when the fix needed a go build, and stay otherwise. Steps left unfinished by the failed build are folded into the next rebuild.
- A module that fails to compile in the browser shows Vite's own overlay.
- If the installed TanStack Router is not the version Tifa is tested with (see [Generated files and app setup](../reference/generated-files.md#the-apps-tsconfig-and-vite-config)), the bundle step fails with that error. `tifa dev` does not watch `node_modules`, so after installing the tested version, restart `tifa dev`.
- If the installed React or React DOM is older than 19.3.0 (see [Generated files and app setup](../reference/generated-files.md#the-apps-tsconfig-and-vite-config)), the bundle step fails with that error. Restart `tifa dev` after installing a newer one.
- If the very first build fails, there is no old version to serve. Every request gets 502 with the original error, and things recover on their own once fixed.
- If the app exits right after starting, or does not listen within 30 seconds, the overlay title is `The app did not start`. The body shows the exit status and the last 40 lines the app wrote to stderr, where an `OnStart` error and a panic go. The line of a panic stays even when its stack trace is longer, as long as the output ends with that panic's report. A `panic: ` line the app logged and went on from does not. If the running app exits, as after a panic outside a request, the title is `The app stopped` and the body is the same. Until the next build, page requests get 502 with the same text. The log of `tifa dev` only says the app exited, since the app's output is printed above it already.
- If the running app cannot load a rebuilt SSR bundle, such as one beastjs cannot compile, or one whose top-level code throws, the overlay title is `The app did not load the SSR bundle`. The app keeps serving the bundle it had. The stack trace gives places in the source files, and a package that calls `require`, which beastjs does not have, is named with what to do.

You can close the overlay. Closing it does not affect later reloads.

## Errors in the browser

Errors that happen in the browser show in a small panel in the bottom right corner. It leaves the page usable, and the overlay of a build error covers it and stays on top.

- `Hydration mismatch`: React could not reuse the server HTML and rendered the page again in the browser. The panel shows React's message with the difference, and the component stack. Other errors React recovers from by itself are titled `Recoverable error`.
- `Uncaught error`: a component threw with no error boundary around it, or any other error reached `window`.
- `Unhandled rejection`: a promise failed and nothing handled it.

The same error again adds to a counter (`×3`) instead of adding an entry. The panel keeps the last 20 different errors. Each can be dismissed, and a reload clears them. Errors that an `ErrorBoundary` component caught are not shown, as before they only reach the console. `tifa dev` keeps the same errors for [Tifa errors](dev-scripts.md#tifa-errors). A built app has none of this: the code is only in the dev build.

## When a page fails to render

When a component throws while the page renders on the server, the app renders the page again with the error shown by an `ErrorBoundary` component (see [Errors on a page](../pages-and-data/errors.md#errors-on-a-page)). Under `tifa dev` an overlay over that page names the request, shows the public message when it differs from the status text, and shows the whole error with its stack trace in your source files. When no `ErrorBoundary` renders, or a page request fails before the app can render a page, such as when `OnRequest` returns an unexpected error, the app answers with a plain page from Go, which under `tifa dev` shows the same. Either page has the status code that `OnError` picked, so `curl -i` sees it too, and reloads once you save a fix, also a fix in TSX that does not reload other pages. A built app shows only the status and the public message.

These pages show internal errors. `tifa dev` listens on `127.0.0.1` by default. With `--addr 0.0.0.0:<port>`, other machines on the network can see them.

Reloads and the overlay come over `tifa dev`'s own connection, `/__tifa/dev/events`, not Vite's HMR websocket. Vite's dev server only starts in the first bundle step, after static analysis and code generation, and an app without pages never starts it. The page that answers while no app runs, after a first build that failed or a crash, must still show the error and reload once things are fixed, and that works only over a connection that `tifa dev` itself serves from the start. Every tab of a browser shares this connection through a SharedWorker.
