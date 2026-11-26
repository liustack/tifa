---
summary: 'Why Tifa is this way: folders are URLs, one address per write, nothing hidden'
audience: app
order: 3
read_when:
  - Starting a new Tifa app and wondering why it is laid out the way it is
  - Weighing a new layer, package or helper against how Tifa apps are meant to look
  - Looking for route groups, server functions or another feature of a framework you know, and wondering why Tifa has none
  - Changing the conventions of Tifa, its docs, examples/shop or the `tifa new` template
---

# Philosophy

A Tifa app is a Go program, and its folder tree is its map: a folder under `app/` is a part of the business, a segment of the URL and a Go package, all three at once. The ideas on this page keep it that way: simple, organized by the business, and with nothing hidden. Each one says what Tifa does, why, and what you get from it. The last section lists what Tifa leaves out because of them, why, and what to write instead.

When two ideas pull apart, Tifa takes them in this order: safety and stability, then simplicity (one way to write a thing, and the way Go does it), then speed, then convenience. A lower one never buys out a higher one. Folders as routes, and a folder for each write, cost more small packages, and that is the price. Where each file goes and how to set things up is in [Structuring an app](structure.md).

## The path is the URL

A folder under `app/` is one segment of the URL, and nothing else decides a URL: `app/projects/project+/orders/` is `/projects/{project}/orders`. No folder is left out of the URL, and no segment comes from anywhere but a folder. So Tifa has no route groups, no file that lists routes, and no handler whose address the build makes up. A parameter is a folder too (`project+`), since a folder is also a Go package and Go decides which characters its name may have.

**Why.** A path under `app/` is also the Go import path, so a folder that counted in one and not in the other would end the match. A parameter is written `+` because Go allows only letters, digits and `- . _ ~ +` in an import path, and skips folders that start with `_` or `.`.

What you get:

- The folder tree shows every URL of the app, and every way into it. There is one tree to read, for you and for a coding agent.
- Renaming a folder renames the URL and the package together, and the Go compiler and the TypeScript check find what still uses the old name.
- A Layout is a file in a folder like any other, so which pages a frame is around is read from where it sits. [Instead of route groups](../pages-and-data/layouts.md#instead-of-route-groups) shows how pages share a frame without them.

## Group code by domain

Code is grouped by what it is about (orders, projects, customers), not by what kind of code it is. This follows [organizing by component](https://developer20.com/how-to-structure-go-code/): a component holds everything it needs, its storage, its HTTP handling and its business model. In Tifa the folders under `app/` are already the URLs, and the URLs already follow the business, so a domain is a folder under `app/`, with its code next to the pages that show it. No package collects one kind of code from every domain.

**Why.** Code that changes together should sit together, and Go refuses import cycles, so a domain cannot quietly grow into another. Grouping by kind of code spreads one feature over every folder.

What you get:

- You can take a domain out, review it or delete it as one unit. A change to a feature starts in its folder and mostly stays there. What reaches outside shows up as an import or a table in the shared schema, so you can see it.
- The URL in the browser tells you where the code is.
- Go refuses import cycles, so two domains cannot depend on each other: the imports between domains always go one way.

## One address for each write

Every write an app accepts has an address of its own: a method and a path, and the path is a folder. Changing the fields of a thing is the `PATCH` of its folder. A new thing below it is the `POST` of a folder for those things. An operation with a name of its own, such as shipping an order, is a folder with that name and its `POST`. There are no functions that the browser calls by name, and no second way to reach a handler. [Where a write goes](../pages-and-data/handlers.md#where-a-write-goes) shows the three shapes.

**Why.** A write needs an address that `OnRequest`, a proxy, a rate limit and the log can see. An exported Go name says other packages may call it, and if it also said the network may, a helper shaped like a handler would be a way in that nobody decided on.

What you get:

- `OnRequest`, a proxy, a rate limit and the log see which write a request is from its method and path alone, so who may do what is decided in one place.
- `tifa routes` prints every route of the app and every address Tifa answers on its own, so you can read all the ways in.
- There is one way to write a thing. Nobody has to choose, and code by another person or by a coding agent looks like yours.
- A folder for an operation makes you say what the operation is: its name, its input, who may do it and when. That sorts out the business as the app grows.

## Nothing happens that you did not write

Tifa does what the code says and nothing on a guess. What it needs to know it reads from names you wrote, a folder, a function or a constant, and what it cannot know it leaves to you.

- **A write never loads the data of the page again by itself.** Tifa does not know what a write changed, so the page calls `router.invalidate()` when it should show it: [Calling method handlers from the browser](../pages-and-data/handlers.md#calling-method-handlers-from-the-browser).
- **Nothing can be called that is not a route.** Code is reached from outside by being the `Load`, the `Layout` or a method handler of a `route.go`, never by being exported or imported somewhere.
- **`OnRequest` is one function that every request to a route passes**, pages, navigation data requests and API routes alike: [Hooks](../pages-and-data/hooks.md).
- **A command does the same whoever runs it**, a person, a script or a coding agent. What differs is a flag you pass.
- **The build reads your code and never runs it**, see [Do things the Go way](#do-things-the-go-way).

**Why.** Tifa cannot know what a write changed, so a reload after it would be a request sent on a guess, and load on your server. It was built once as a refresh option and taken out, because loading the routes inside a write repeats everything a navigation request does.

What you get:

- You can tell what a request does by reading `OnRequest` and the folder it goes to.
- An app that is easier to keep safe, because every way in is in sight: it is a folder that `tifa routes` prints (the few addresses Tifa answers itself are in the same output), it has a method and a path that a proxy, a rate limit and the log can tell apart, and it reads JSON or a form, which any tool can send and inspect.
- No request is sent on a guess, so the load on the server is the load the pages ask for.

## Wire dependencies by hand

Dependencies are plain Go values: package-level variables and functions, set up in `OnStart` and released in `OnShutdown`. There is no dependency injection container, no reflection, and no generated code that wires your dependencies together.

**Why.** Go already has a way to share a value: a package-level variable that `OnStart` sets. A container adds a second way, and with it lookups by name or type that fail only at run time.

What you get:

- Go to definition always lands on the code that runs.
- What starts when, and in what order, is the code of `OnStart`, top to bottom.
- Nothing is looked up by name or type at startup, so using a resource that is not declared is a compile error. A declared one that `OnStart` has not set up yet is still nil at run time.
- There is nothing to learn beyond Go.

## Start small

A domain starts as one file. It gets more files, or more packages, when there is code to put in them, and an abstraction such as an interface when there is a second implementation to hide behind it. [Structuring an app](structure.md#when-a-domain-grows) says how.

**Why.** An empty layer is a guess about code that does not exist yet. A wrong guess costs more to undo than a split made when the code is there.

What you get:

- No empty layers to keep up before there is code to put in them.
- The structure grows with the code instead of following a plan made on day one.

## Do things the Go way

- **Little magic.** The conventions are folder names and a few function and constant names. Everything else is plain Go, and the code Tifa generates is Go and TypeScript you can read.
- **The build only reads your code.** `tifa build` analyzes the source and never runs it: an `init()` of the app does not run during the build.
- **Platform differences use Go's build constraints.** The build picks files for the target platform the way the go command does.
- **Pure Go.** The app builds with `CGO_ENABLED=0` into one executable.

**Why.** Nothing may rest on a behavior that Go, the browser or a library does not promise. A build that only reads the code is safe to run anywhere, and one exact version of the router is what Tifa tested, so what was tested is what runs.

What you get:

- `go test ./...`, `go vet ./...` and your editor work on the app as on any Go module.
- A build is safe to run anywhere, CI included, without the database or the secrets the app needs at run time.
- An app builds into one file to copy, for each platform its engine, [beastjs](https://github.com/liustack/beastjs), supports: linux and darwin, on amd64 and arm64, and windows on amd64 and arm64.

## What Tifa leaves out

Each of these was weighed and left out, so that there stays one way to write a thing. The table says why and what to write instead.

| You may look for | Why it is not there | Write this |
| --- | --- | --- |
| Route groups such as `(shop)` | A path under `app/` is the URL and the Go import path, and a folder in one and not the other ends that. No app needed one, since a part of the site that must not show the frame around it is `layout_.tsx` | `layout_.tsx`, a URL prefix, or one frame component in several Layouts: [Instead of route groups](../pages-and-data/layouts.md#instead-of-route-groups) |
| Server functions, server actions or named actions | A write needs an address that `OnRequest`, a proxy and the log can see. A function that the network may call by name, with no folder, is a way in that nobody decided on | A folder for each operation, with its `POST`: [Where a write goes](../pages-and-data/handlers.md#where-a-write-goes) |
| React Server Components | Go loads all the data first and the render does no I/O, so a component has nothing to wait for on the server. Components that fetch while they render would put application I/O into the render | `Load` in `route.go`, and `Suspense` for what only the browser can fetch: [Loading states](app.md#loading-states) |
| Page data that loads again after every write | Tifa cannot know what a write changed, so each reload is a request sent on a guess | `router.invalidate()` where the page needs it: [Calling method handlers from the browser](../pages-and-data/handlers.md#calling-method-handlers-from-the-browser) |
| `<title>` and `<meta>` written in a component | Tifa writes the head of the document as a string, merges the heads of nested routes, and a 404 or error page needs a head when its component did not render. In a component the tag lands in `<body>`, where crawlers do not read it | The `head` export: [Title and meta tags](../pages-and-data/head.md) |
| `error.tsx`, `not-found.tsx`, `layout.go` or more convention files | A folder is easy to remember with at most three, and a 404 is one status among others | `ErrorBoundary` shows every status, and `route.go` holds every Go name: [The files of a folder](../pages-and-data/routing.md#the-files-of-a-folder) |
| Static generation and prerender | A static export would need a second set of rules for everything that expects a server, and Tifa is for a Go program on a server | A page with `Cache`: [No static generation](app.md#no-static-generation) |
| Built-in sign-in, user accounts, a Redis or SQL session store | A session has one job, keeping the page and the person apart. Where it is kept and who a user is belong to the app | A store you write with three methods, and a guide for sign-in: [Sessions](../pages-and-data/sessions.md#where-to-store-sessions) and [Sign in](../guides/sign-in.md) |
| A CSRF token in every form | Every write already passes Go's `http.CrossOriginProtection`, and the session cookie is `SameSite=Lax`. A token would be a second mechanism that can go wrong | The check that is always on: [Cross-site writes](../build-and-deploy/config.md#cross-site-writes) |
| A dependency injection container | Package-level variables already do the job, with no lookup at run time | Package-level variables set up in `OnStart`: [Wire dependencies by hand](#wire-dependencies-by-hand) |
| An API for scheduled jobs | Go has goroutines and `time.Ticker`, and a job needs nothing that only the framework knows | A goroutine started in `OnStart`: [Background work](../guides/jobs.md) |
