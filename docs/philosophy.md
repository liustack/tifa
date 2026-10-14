---
summary: 'The ideas behind how a tifa app is organized and what each one gives you: group code by domain, wire dependencies by hand, start small, do things the Go way'
read_when:
  - Starting a new tifa app and wondering why it is laid out the way it is
  - Weighing a new layer, package or helper against how tifa apps are meant to look
  - Changing the conventions of tifa, its docs, examples/shop or the tifa new template
---

English · [简体中文](philosophy.zh-CN.md)

# Philosophy

A tifa app is a Go program. Four ideas decide how its code is organized. This page gives the ideas, why, and what you get from each. Where each file goes and how to set things up is in [Structuring an app](structure.md).

## Group code by domain

Code is grouped by what it is about (orders, projects, customers), not by what kind of code it is. This follows [organizing by component](https://developer20.com/how-to-structure-go-code/): a component holds everything it needs, its storage, its HTTP handling and its business model. In tifa the folders under `app/` are already the URLs, and the URLs already follow the business, so a domain is a folder under `app/`, with its code next to the pages that show it. No package collects one kind of code from every domain.

What you get:

- You can take a domain out, review it or delete it as one unit. A change to a feature starts in its folder and mostly stays there. What reaches outside shows up as an import or a table in the shared schema, so you can see it.
- The URL in the browser tells you where the code is.
- Go refuses import cycles, so two domains cannot depend on each other: the imports between domains always go one way.

## Wire dependencies by hand

Dependencies are plain Go values: package-level variables and functions, set up in `OnStart` and released in `OnShutdown`. There is no dependency injection container, no reflection, and no generated code that wires your dependencies together.

What you get:

- Go to definition always lands on the code that runs.
- What starts when, and in what order, is the code of `OnStart`, top to bottom.
- Nothing is looked up by name or type at startup, so a dependency that does not exist is a compile error.
- There is nothing to learn beyond Go.

## Start small

A domain starts as one file. It gets more files, or more packages, when there is code to put in them, and an abstraction such as an interface when there is a second implementation to hide behind it. [Structuring an app](structure.md#when-a-domain-grows) says how.

What you get:

- No empty layers to keep up before there is code to put in them.
- The structure grows with the code instead of following a plan made on day one.

## Do things the Go way

- **Little magic.** The conventions are folder names and a few function and constant names. Everything else is plain Go, and the code tifa generates is Go and TypeScript you can read.
- **The build only reads your code.** `tifa build` analyzes the source and never runs it: an `init()` of the app does not run during the build.
- **Platform differences use Go's build constraints.** The build picks files for the target platform the way the go command does.
- **Pure Go.** The app builds with `CGO_ENABLED=0` into one executable.

What you get:

- `go test ./...`, `go vet ./...` and your editor work on the app as on any Go module.
- A build is safe to run anywhere, CI included, without the database or the secrets the app needs at run time.
- An app builds for the platforms Go supports, as one file to copy.
