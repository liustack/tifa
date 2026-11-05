# Contributing to Tifa

First, the policy: **Tifa does not accept pull requests.** A framework like this touches the compiler, the bundler, server rendering and developer experience at once, and a change in one pulls on the others. Getting that right takes a lot of context, so one maintainer writes, reviews and owns every line.

Two ways to help that make a real difference:

- **[Open an issue](https://github.com/liustack/tifa/issues/new/choose).** Bugs, ideas, confusing errors, unclear docs. A clear issue with steps to reproduce helps more than a patch, and issues shape what gets built next. The templates tell you what to include.
- **Fork it.** Under the MIT license your copy is yours to change, rename and publish. No permission needed.

Everything below is for people working on a fork.

## Scope

Tifa runs a whole app as one Go program on a small VPS or container: Go handles routing, data, caching and background jobs, and React pages are rendered to complete HTML inside the process by a pure Go QuickJS. These stay out:

- cgo, V8, or a Node process next to the app. The program builds with `CGO_ENABLED=0` and needs no Node to run. `tifa build` and `tifa dev` use the app's own Vite on Node to bundle the front end.
- streaming SSR and React Server Components. Go loads all data before the render, and the render does no I/O of the app.
- a dependency injection container, or the build running the app's Go code. The build only reads it.

[AGENTS.md](AGENTS.md) has the full list of constraints.

## Setup

You need Go 1.27, and Node 20.19 or 22.12 and later with pnpm for the front end of the example app.

```sh
go test ./...
go vet ./...
CGO_ENABLED=0 go build ./...
```

The example app is [examples/shop](examples/shop), a separate Go module that points at the repository root.

## Tests

- New behavior comes with a test that fails first. A bug fix comes with a test that reproduces it, in the same commit.
- Packages with concurrency also run under `go test -race`.
- End-to-end tests, the long SSR tests and the comparison with other frameworks are off by default. [docs/testing.md](docs/testing.md) says how to turn them on and when they are needed.

## Commits

- `type(scope): imperative summary`, at most 72 characters, no period at the end.
- One commit does one thing, and the tree builds and passes its tests after each. Keep a refactor or a reformat apart from a change of behavior.
- Docs are written in English, and the user-facing ones have a `*.zh-CN.md` twin that changes with them.

The full rules are in [docs/commit.md](docs/commit.md).
