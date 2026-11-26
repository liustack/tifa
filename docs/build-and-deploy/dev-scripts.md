---
summary: 'Run `tifa dev` from a script or a coding agent: `--background`, `status`, `logs`, `stop` and `tifa errors`'
audience: app
order: 5
read_when:
  - Checking from a script or a coding agent whether a change broke the build, a request or a page in the browser, or changing `tifa errors`
  - Starting, watching or stopping `tifa dev` from a script or a coding agent, or changing `tifa dev --background`, `status`, `logs` or `stop`
---

# Run `tifa dev` from a script or a coding agent

A script or a coding agent has no terminal to watch. These commands start `tifa dev` in the background and tell you whether a change worked.

## Agents and scripts

```sh
go tool tifa dev --background    # start, and return once the app answers
go tool tifa dev status [--json] # is it running, where, and did the last build work
go tool tifa dev logs [-n 100] [-f]
go tool tifa dev stop
```

`status`, `logs` and `stop` take `--dir app-root` too.

A coding agent or a script cannot keep `tifa dev` open in a terminal. `tifa dev --background` starts it detached from the terminal and returns once the build ended and `GET /` answers. It prints the address and the process ID, then how to read the log and how to stop it:

```text
tifa dev: ready at http://127.0.0.1:9527 (pid 41231)
  log:    go tool tifa dev logs
  status: go tool tifa dev status
  stop:   go tool tifa dev stop
```

Closing the terminal does not stop it. `--background` is the only switch: Tifa does not look for an agent, and a plain `tifa dev` stays in the foreground.

The skill and the notes that tell an agent about these commands are written by [`tifa agents`](upgrade.md#tifa-agents).

- Every other flag and every environment variable of the command reaches the server as given, `--dir` and `--addr` included.
- All output of the server goes to `.tifa/dev.log`: the `tifa dev:` lines, the output of the app and Vite's messages. A new `tifa dev --background` starts the file again. `tifa gen` and `tifa build` leave it alone.
- The command waits up to 5 minutes. It exits with status 1 and says why when the server ended first (with the end of its log), when the first build failed, or when `GET /` answered a 5xx status. After a failed first build the server keeps running and builds again when you save a file, so fix the code, then run `tifa errors` or `tifa dev status`. `tifa dev stop` stops it.
- A second `tifa dev`, in the foreground or with `--background`, fails as before. Its message names the address and the process ID of the one that runs, and says to use `tifa dev status` and `tifa dev stop`.

### Status

`tifa dev status` says whether a `tifa dev` runs in the app, its address, process ID, start time and the state of its last build. It exits with status 0 when one runs and 1 when none does.

```text
tifa dev is running in this app
  url:     http://127.0.0.1:9527
  pid:     41231
  started: 2026-11-16 22:22:50 (2m3s ago)
  app pid: 41290
  build:   failed: go build: app/store.go:3:2: undefined: x
  log:     /work/shop/.tifa/dev.log
```

`app pid` is the process ID of the app that `tifa dev` runs, which a debugger attaches to (see [Debugging](debugging.md)). It changes each time a Go change starts the app again, and it is left out while no app runs. `--json` prints one object. `build.state` is `starting` (the first build runs), `building`, `ok`, `failed` (then `build.error` has the first line of the error) or `unknown` (the server did not answer, `build.error` says why). `log` is empty for a `tifa dev` that prints to its terminal. When none runs it prints `{"running": false}`.

```json
{
  "running": true,
  "pid": 41231,
  "url": "http://127.0.0.1:9527",
  "started": "2026-11-16T14:22:50Z",
  "build": {"state": "ok"},
  "appPid": 41290,
  "log": "/work/shop/.tifa/dev.log"
}
```

### Logs

`tifa dev logs` prints the last 100 lines of `.tifa/dev.log`, `-n <lines>` as many as you say. With `-f` it then prints new lines as they come, and ends when the `tifa dev` ends. The log stays after the server ended, so `tifa dev logs` also tells why it stopped. A `tifa dev` that was not started with `--background` prints to its terminal and keeps no log: `logs` says so. What the app writes there, and how to set its format and level, is in [Logs](../guides/logs.md).

### Stop

`tifa dev stop` asks the server to stop as Ctrl-C does, and returns once it is gone and the lock is free (the app gets up to 3 times `server.shutdown_timeout` plus 1 second). It exits with status 0 also when no `tifa dev` runs, so a script can use it to make sure none does. It gives up after 60 seconds and says so, without killing anything.

### How it tells what runs

A `tifa dev` runs when it holds the lock `.tifa/dev.lock`, which the system frees when the process ends, however it ends. The file also records the process ID, the address and the start time, but a record left by a server that ended proves nothing, and its process ID may belong to another program by now. So `status` and `stop` ask the lock first: a leftover file and no holder is `not running`, and nothing is signalled.

`stop` also does not trust the record's process ID by itself. It signals only after the program at the recorded address has answered at `/__tifa/dev/status` with that same process ID in `X-Tifa-Dev-Pid`. A holder that does not answer so, for instance a record that the new holder has not written yet, is left alone: the command says so and exits with status 1. It never touches a `tifa build`, which takes the same lock.

## Tifa errors

```sh
tifa errors [--dir app-root] [--json]
```

While `tifa dev` runs, `go tool tifa errors` prints the errors since the last build started. A coding agent that sees neither the browser nor the terminal of `tifa dev` learns from it whether a change works. Run it after you change code and after you open or reload the pages you changed: errors of the browser come only from pages that are open.

It lists:

- What the overlay shows: `Build failed` with the original message of the Go compiler or of Vite, `The app did not start`, `The app stopped` or `The app did not load the SSR bundle`. It stays until the next build ends.
- Unexpected errors of requests, the ones that go to `OnError` (or are logged as a 500 without it): the request, the status and the message users see, and the error with its stack trace in your source files, as the error page shows them under `tifa dev`. Requests of API routes count too, also in an app without pages.
- Errors of the browser that pages reported: `Hydration mismatch`, `Recoverable error`, `Uncaught error` and `Unhandled rejection`, as the panel names them, with the page that reported each first.
- Pages whose two renders differ (see [Log](dev-errors.md#log)).
- The warnings of the last build, after the errors under `Warnings`: those of the analysis (such as a number above 2^53 put into a field that pages read), of the check for code of other frameworks, of the content and of the plugin of Tifa in Vite (such as a read of `import.meta.env.VITE_*`). The warnings of Vite and Rolldown themselves are in the log of `tifa dev` only. They are not errors and never change the exit status. A save that fixes one removes it from the list.

Each kind keeps the last 20 different errors, and the same error again raises its count, as in the panel. When a build starts, the errors from before it are dropped. Asked during a build, the command waits for the build to end.

```text
Request failed: GET /orders?page=2, 503 Service Unavailable (×2)
  Users see: Come back later
  tifa: render page: ...

Hydration mismatch in the browser: http://127.0.0.1:9527/orders (×3)
  ...
```

Without errors it prints `No errors since the last build.` It exits with status 0 without errors, 1 with errors, and 2 when no `tifa dev` runs in the app: the process that `.tifa/dev.lock` names is gone, it does not listen yet, or nothing at its address answers for it. `--json` prints the same as JSON, with every field also when it is empty:

```json
{
  "failure": {"title": "Build failed", "error": "..."},
  "requests": [{"method": "GET", "url": "/orders?page=2", "status": 503, "message": "Come back later", "error": "...", "count": 2}],
  "browser": [{"kind": "recoverable", "title": "Hydration mismatch", "url": "http://127.0.0.1:9527/orders", "text": "...", "count": 3}],
  "renders": [{"method": "GET", "url": "/stats", "text": "...", "count": 1}],
  "warnings": ["app/api/route.go:22:55: Mine.ID gets a value from time.Time.UnixNano, a number above 2^53. ... (warning)"]
}
```

`failure` is `null` when everything builds and runs. `kind` is `recoverable`, `uncaught` or `rejection`.

The command reads the process ID and the address that `tifa dev` wrote into `.tifa/dev.lock`, without taking the lock, so a `tifa dev` or `tifa build` can start at the same time. It asks that address for `/__tifa/dev/errors` and takes the answer only when it names the same process in `X-Tifa-Dev-Pid`. `tifa dev` answers there only under `localhost` or an IP address, and takes the reports of the browser only from its own pages, up to 128 KiB each. The app answers `tifa dev` for its own errors. A built app has none of this.
