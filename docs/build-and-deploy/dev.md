---
summary: 'Run `tifa dev`: what it starts, how Vite fits in, the port, the files it writes, big apps and Windows'
audience: app
order: 2
read_when:
  - Running a Tifa app locally for the first time
  - Changing internal/devserver, the dev command in cmd/tifa, or the dev build in internal/bundle
  - Running `tifa dev` on Windows, or changing how it starts, stops or ends its children there
---

# `tifa dev`

```sh
tifa dev [--dir app-root] [--addr 127.0.0.1:9527] [--background]
```

On start it prints `tifa dev: serving http://<address>`, then runs a full build (the same steps as [`tifa build`](build.md), and the first one of an app takes several seconds to a minute, see [Log](dev-errors.md#log)) and starts the app. The browser connects to `--addr` only. `tifa dev` forwards page and data requests to the app, and requests under `/__tifa/vite/`, the browser modules and Vite's HMR websocket, to Vite's dev server. While the app restarts or loads a new SSR bundle, page requests wait for it. Ctrl-C, SIGTERM and SIGHUP, which a terminal sends when it closes, stop the app, Vite and `tifa dev`. A `tifa dev` started with `nohup` ignores SIGHUP. A Vite build or plugin that does not end gets 5 seconds, then Vite is stopped. If `tifa dev` itself is killed, the app and Vite stop on their own: the app shuts down as it does on SIGTERM, and Vite within the same 5 seconds (on Windows both end at once, see [On Windows](#on-windows)). `tifa dev` hands the app and Vite only the files it names for them, not the ones it inherited, such as the lock a script holds with `flock`.

An app with a `package.json` needs its npm packages first. Right after a clone, `node_modules` is missing, and `tifa dev` (like `tifa build`) stops before it starts anything and prints the command to run: `pnpm install` when pnpm is on PATH, `npm install` otherwise. An app without pages has no `package.json` and needs no Node.

`tifa dev` runs on Windows 10 1809 and later as it does elsewhere, `--background` and the commands of [Agents and scripts](dev-scripts.md#agents-and-scripts) included. [On Windows](#on-windows) has what differs there.

A second `tifa dev` in the same app does not start. It prints `tifa: tifa dev is already running in this app at http://<address> (pid <N>)` and exits with an error, before it starts Vite. `tifa build` stops the same way while a `tifa dev` runs, since both write `.tifa/dist`. The lock is `.tifa/dev.lock`, and it goes away with the process that holds it. Without `--addr`, a taken port 9527 moves `tifa dev` on to 9528 and up, ten ports in all, and the `serving` line names the one it took. With `--addr`, `tifa dev` uses exactly that address and fails when it is taken.

`tifa dev` serves plain HTTP. The [HTTPS settings](https.md#https) of the project file apply to the built program only: the dev program ignores them.

## Vite

`tifa dev` runs the app's own Vite on Node, with the app's `vite.config.ts` (see [Generated files and app setup](../reference/generated-files.md#the-apps-tsconfig-and-vite-config)):

- Vite's dev server serves the browser modules. It starts with the first build of an app with pages and listens on a free port of 127.0.0.1 that only `tifa dev` talks to. Pages load the Vite client, the React Fast Refresh runtime of `@vitejs/plugin-react` and `.tifa/client.ts` from it.
- The SSR bundle is still a file, `.tifa/dist/ssr.js`, which the app loads into its beastjs VMs. It is built on the same Node process as the dev server, on a worker thread of its own: the browser gets the development build of React and beastjs runs the production one, and Vite picks that build once per thread. Tifa sets `NODE_ENV` itself (development for the dev server, production for the SSR bundle), so a `NODE_ENV` in the shell changes neither. The SSR bundle moves to a new worker thread after every 10 builds, failed ones included, because Node never frees everything a Vite build leaves behind in a thread, and after a build in which the thread ended, such as by an error a plugin did not catch. A plugin that keeps something on `globalThis` of that thread starts over then.

## Where files go

| Path | Contents |
| --- | --- |
| `.tifa/` | Generated code, the same as `tifa build` |
| `.tifa/dev.lock` | The lock of `tifa dev` and `tifa build`, with the process ID, address and start time of its holder |
| `.tifa/dev.log` | Everything a `tifa dev --background` prints, see [Agents and scripts](dev-scripts.md#agents-and-scripts) |
| `.tifa/dist/` | The SSR bundle, `build.json`, the public files, and `content.json` and the bodies in `content/` of an app with collections. The browser modules come from Vite's dev server instead |
| `bin/tifa-dev` | The dev executable |

The app runs in the app root, also when `tifa dev` starts elsewhere with `--dir`, the same as the program `tifa build` makes when you run it from the app root. Files the app opens by a relative path, such as the `app.db` of the [`tifa new`](create-app.md#tifa-new) template, land there. So does the `.env` it reads (see [.env](config.md#env)). `tifa dev` passes the address the app listens on in `SERVER_ADDR`, which wins over a `PORT` of your shell and over `.env`.

When stopping the app, `tifa dev` waits at most 3 times the configured `server.shutdown_timeout` plus 1 second, then kills the whole process group.

## Large apps

`tifa dev` watches folders with the system's file events. When an app has more folders or files than the system lets one program watch comfortably, `tifa dev` polls the rest: it looks at them about once a second and rebuilds as usual. Nothing needs to be set up. The log says so:

```text
tifa dev: 07:35:36 watching app/assets would use up the system's file watches, so it and the directories found after it are polled for changes
```

- On macOS, FreeBSD, OpenBSD, NetBSD and DragonFly, `tifa dev` watches with kqueue. A watched folder takes one file descriptor, and so does each file in it of a kind in the [table of what each change triggers](dev-reloads.md#what-each-change-triggers), each embedded file and each file under `content/`, also one that appears later. Other files, such as images outside `content/`, take none. `tifa dev` counts these against half of what one process may open (the smaller of the open file limit and `kern.maxfilesperproc`), and polls the folders beyond that, also a watched folder whose files grow past it.
- On Linux, watching a folder takes one of the user's inotify watches, which editors and other tools share. `tifa dev` uses at most half of `fs.inotify.max_user_watches`.
- If the system runs out first, because other programs hold many watches or files, `tifa dev` polls the folders it could not watch, gives back a few watches it holds, and keeps working. If it runs out while `tifa dev` runs, such as when a file cannot be opened, `tifa dev` stops watching with system events, polls every folder and compares every file again.
- A poll lists the polled folders and hashes only the files whose size, modification time or identity changed. Between two polls `tifa dev` waits at least a second, and at least ten times as long as the last poll took. A watched folder whose entries change is listed again under the same rule, without the one second minimum, so a folder that keeps growing costs no more. Polling and listing thus each take at most about a tenth of one CPU core, on top of handling file events and rebuilding.
- How long a poll takes grows with the number of files and depends on the machine and its disk. On an Apple M4, with the files in the system's cache, a poll of 20,000 files took between 45 and 65 ms in our runs. While a poll takes less than a tenth of a second, a change in a polled folder is found within about a second, and then built as usual.
- Polled folders stay polled until `tifa dev` restarts.
- A page requested right after a save in a polled folder, before the poll finds the change, gets the old server render, and a reload that Vite starts then fails to hydrate. Reload once more after the `ready` line.
- Vite's dev server watches the app root too. Once `tifa dev` polls because the system is short, it tells Vite's dev server to poll as well, unless the dev server watches with FSEvents, as it usually does on macOS. FSEvents takes no file descriptor per file, so it stays.
- To poll, Vite's dev server restarts, and from then on looks at every file once a second, about 8% of one CPU core for 10,000 files on an Apple M4. That is a single restart when the restart succeeds and no other restart of the dev server, such as one after a change to `vite.config.ts`, runs at the same time. A restart that is running is waited for first, and if it worked and its new server already polls, nothing else restarts.
- A restart can fail. One that cannot create the new server, such as one with a broken `vite.config.ts`, keeps the old server and its watcher. One whose new server cannot listen, because another program took the port meanwhile, has already closed the old server, so there is no Vite dev server until the next restart. Either way the log says so, and `tifa dev` and the app keep running. The dev server tries again at the next error of its watcher, and the next restart that succeeds, such as one after a change to `vite.config.ts`, polls.
- When Vite's own watcher runs out of watches or file descriptors first (ENOSPC, EMFILE or ENFILE), the dev server switches to polling the same way and says so once. This holds for a watcher that uses FSEvents too.

## On Windows

`tifa dev` works the same on Windows, with these differences:

- Ctrl-C, Ctrl-Break and closing the console window stop it. Windows has no signal that one program can send another, so `tifa dev stop` sets an event that `tifa dev` waits on, `Local\tifa-dev-stop-<pid>`. The event belongs to the Windows login session, so `stop` reaches only a `tifa dev` started in the same session.
- To restart the app, `tifa dev` closes the app's standard input, which the app reads to its end, and then shuts down as it does on SIGTERM: `OnShutdown` runs. The app's code needs nothing for it.
- `tifa dev` runs in a job object (how Windows treats a group of processes as one), and the app in one of its own inside it. When `tifa dev` is killed, from the Task Manager or `taskkill /F`, Windows ends Node, Vite, `go build`, the app and what the app started, at once. The app gets no `OnShutdown` then. If Windows refuses to set up the job object, `tifa dev` stops at once with an error that names the call that failed, and starts nothing.
- `tifa dev` sees a saved file once the program that wrote it closes it. An editor that keeps a file open after writing it may leave the next page request without the change.
- Right after a save `tifa dev` reads the file to see whether it changed. Windows does not let another program rename a file over one that is open, so a save that comes in that moment can fail with "Access is denied". Editors try again, and a script that saves by renaming should too.
- Windows does not let a running program be written over. Before each Go build `tifa dev` renames the program the app runs from, `bin\tifa-dev.exe`, to `bin\tifa-dev.old-<number>.exe`, and deletes the ones earlier builds left once they no longer run.
- `--background` runs `tifa dev` without a console window. Node, `go` and the app share its hidden console, so no window opens.
- Without `--addr`, a port that Windows reserves for Hyper-V, WSL or Docker (`netsh interface ipv4 show excludedportrange protocol=tcp`) counts as taken, and `tifa dev` moves on to the next one.
