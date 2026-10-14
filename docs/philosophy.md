---
summary: 'The ideas behind how a tifa app is organized and what each one gives you: group code by domain, wire dependencies by hand, start small, do things the Go way'
read_when:
  - Starting a new tifa app and wondering why it is laid out the way it is
  - Weighing a new layer, package or helper against how tifa apps are meant to look
  - Changing the conventions of tifa, its docs, examples/shop or the tifa new template
---

English · [简体中文](philosophy.zh-CN.md)

# Philosophy

A tifa app is a Go program. Four ideas decide how its code is organized. This page gives the ideas and what you get from each. [Structuring an app](structure.md) says where each file goes.

## Group code by domain

Code is grouped by what it is about (orders, projects, customers), not by what kind of code it is. A domain is a folder under `app/`. This follows [organizing by component](https://developer20.com/how-to-structure-go-code/): a component holds everything it needs, its storage, its HTTP handling and its business model. In tifa the folders under `app/` are already the URLs, and the URLs already follow the business, so the folder of a domain holds its types, queries, rules and tests next to the pages that show it.

There is no `models/`, `services/`, `repositories/`, `handlers/` or `utils/` package that collects one kind of code from every domain.

What you get:

- You can take a domain out, review it or delete it as one unit. A change to one feature touches one folder.
- The URL in the browser tells you where the code is.
- Go refuses import cycles, so a call that would close a cycle points at code that sits in the wrong domain.

## Wire dependencies by hand

Dependencies are package-level variables and functions. A database connection is `var DB` in the root package, opened in `OnStart` and closed in `OnShutdown`, and every domain uses it directly. There is no dependency injection container, no reflection and no generated wiring. A function that needs a different value from some callers takes it as an argument, and tests replace the variable or pass a fake.

What you get:

- Go to definition always lands on the code that runs.
- What starts when, and in what order, is the code of `OnStart`, top to bottom.
- Nothing is looked up by name or type at startup, so a dependency that does not exist is a compile error.
- There is nothing to learn beyond Go.

## Start small

A domain starts as one file next to its pages. When it grows, it splits into more files in the same folder, one per topic. A part with pages of its own becomes a folder below. An interface appears once there really are two implementations, such as the real one and a fake for tests.

What you get:

- No empty layers to keep up before there is code to put in them.
- Each step of growth is a move inside one folder, so the structure follows the code instead of a plan made on day one.

## Do things the Go way

- **Little magic.** The conventions are folder names and a few names of functions and constants: `Load`, `Layout`, the method handlers, the hooks. Everything else is plain Go. You write no main, and the code tifa generates sits in `.tifa/`, where you can read it.
- **The build only reads your code.** `tifa build` analyzes the source and never runs it: an `init()` of the app does not run during the build.
- **Platform differences use build constraints**: files named like `limit_windows.go`, or a `//go:build` line. The build picks files for the target platform the way the go command does.
- **Pure Go.** The app builds with `CGO_ENABLED=0` into one executable.

What you get:

- `go test ./...`, `go vet ./...` and your editor work on the app as on any Go module.
- A build is safe to run anywhere, CI included, without the database or the secrets the app needs at run time.
- An app builds for the platforms Go supports, as one file to copy. See [Building for another system](build.md#building-for-another-system).
