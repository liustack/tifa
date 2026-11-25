---
summary: 'What is settled in the design of Tifa and why: names, conventions, defaults and ideas that were turned down, each with what would reopen it, reading query parameters with a call in Load, the browser guessing the type of a query value, a Layout with query parameters passed to tifa.Use'
audience: dev
order: 3
read_when:
  - Before proposing, questioning or listing for discussion any API, convention, file name, setting or default of Tifa
  - Someone asks why Tifa does not do something another framework does
  - Reopening one of these, to see what would have to change first
---

# Decisions

What is settled, by area. An entry is an idea that was weighed, with the verdict and its date, the reason, what Tifa does instead, and what would reopen it. "Turned down" is an idea that was never built or was taken out. "Kept as it is" is a name, a file or a rule that was questioned and stays.

Read the area you are about to touch before you propose a change or list something as open. An entry is reopened only when its "Reopen when" has happened or the user asks. An entry without one is not waiting for anything. Add an entry on the day something is settled, in the commit that makes the change when there is one. Speed experiments that were measured and dropped are in [What was tried and not done](../studies/optimizations.md#what-was-tried-and-not-done).

## How ideas are weighed

In this order (2026-11-25):

1. Safety and stability. No way in that nobody decided on, and no behavior that rests on something Go, the browser or a library does not promise.
2. Simplicity. One way to write a thing, and the way Go does it when Go has one. No second spelling for convenience.
3. Speed.
4. Convenience.

A lower one never buys out a higher one: routes are folders, and each write has its own folder, at the price of more small packages. When an idea is turned down, the docs for apps say what to write instead and what the idea would have risked.

## Routing and the files of a folder

### Folders that group pages without a URL segment

**Turned down (2026-11-01), and for good (2026-11-25).** A folder such as `(shop)` that gives the pages in it a Layout and adds nothing to the URL, as Next.js, SvelteKit, Nuxt, TanStack Router and React Router have.

**Why not.**
- A path under `app/` is the URL, and it is the Go import path too. A folder that is in one and not in the other ends that.
- No app needed it. The one case in `examples/shop`, a storefront that must not show the workbench around it, is one part of the site leaving the Layouts above it, which `layout_.tsx` does.
- The shape it alone can write, pages at the same URL level that share a Layout other pages at that level do not have, can be written today with a remount of the frame as its cost.

**Instead.**
- [`layout_.tsx`](../pages-and-data/layouts.md#a-layout-without-the-layouts-above) for a part of the site with a frame of its own.
- For folders at the same URL level that share a frame: a URL prefix (`/app/settings`), or a Layout in each folder that renders the same component. The second remounts the frame and loads its data again when the visitor moves between the folders.
- Each case is written out in [Instead of route groups](../pages-and-data/layouts.md#instead-of-route-groups).

**Not to be reopened.** The docs for apps say that route groups are not planned, so apps are written on that.

### Dropping a chosen number of Layouts

**Turned down (2026-11-01).** A file name that says how many of the Layouts above to drop, or which one to keep, as SvelteKit's `+page@item.svelte` does.

**Why not.** More rules to learn, the file name has to carry the name of a level, and adding or removing a Layout in between changes what the file does without a word.

**Instead.** `layout_.tsx` has one meaning: every Layout above is dropped, the root Layout too.

### More convention files in a folder

**Turned down (2026-10-31, 2026-11-01).** `layout.go` for the `Layout` function, `not-found.tsx` and `error.tsx`, a `NotFound` export next to the error export, and two generated modules, `./page.gen` and `./layout.gen`.

**Why not.**
- A folder is easier to remember with at most three convention files.
- A 404 is one status among others, so the export that shows errors shows it too, as in SvelteKit and React Router.
- With two generated modules, a folder that only has method handlers would need a third name.

**Instead.** `route.go` holds every Go name of the folder, `ErrorBoundary` shows every status, and each folder has one `./route.gen`: [The files of a folder](../pages-and-data/routing.md#the-files-of-a-folder).

### Another mark for a parameter folder

**Turned down (2026-10-31).** `$order`, `[order]`, `@order` or `_order` in place of `order+`.

**Why not.** A folder under `app/` is a Go package. An element of a Go import path may only hold letters, digits and `- . _ ~ +`, and the go command skips folders that start with `_` or `.`.

**Instead.** `order+` for a parameter and `path++` for the rest of the path: [URLs come from folders](../pages-and-data/routing.md#urls-come-from-folders).

### Generated types by a module name or a path string

**Turned down (2026-10-31).** Importing the types of a route from a module without a path, such as `'tifa:page'`, or looking them up by the path of the route.

**Why not.** TypeScript cannot resolve a module without a path by the file that imports it, so the type of the data would be lost. A path string writes the URL a second time, and it no longer matches once a folder is renamed.

**Instead.** A relative import of `./route.gen`, the way SvelteKit's `./$types` and React Router's `./+types` work.

### Functions the browser calls by name

**Turned down (2026-11-25).** Three ways to reach server code that has no folder of its own:

- Server functions: a function anywhere in the app that the build gives an address, as the Server Actions of Next.js and the server functions of TanStack Start.
- Named handlers in a `route.go`, such as `POSTShip` for `POST /orders/42/ship`.
- Every exported function of a `route.go` with the signature of a handler, called with a query parameter such as `?_action=Ship`, as the named actions of SvelteKit (`?/ship`).

**Why not.**
- A write needs an address that `OnRequest`, a proxy, a rate limit and the log can see. A generated address is in no folder, and writes that differ only in the query string share one path.
- An exported name in Go says that other packages may use it. If it also said that the network may call it, a helper with the signature of a handler would become a way in without anyone deciding so.
- A URL segment that no folder gives ends "the path is the URL", as a route group would.
- A second way to write a handler is a choice to make in every app.
- What they save is a folder and a file of a few lines for each operation.

**Instead.** A folder for each operation: [Where a write goes](../pages-and-data/handlers.md#where-a-write-goes).

**Reopen when** apps suffer from the number of small packages. The third way is the closest to workable, with a prefix or a mark in place of "every exported function".

### Leaving the `.client.tsx` suffix out

**Kept as it is (2026-11-25).** Loading a component in the browser only with `clientOnly` alone, whatever its file is called.

**Why not.** The suffix is how the build knows the module: the SSR bundle replaces every `*.client` module with a stub, so the package it imports never loads on the server. `clientOnly` alone would still put the module into the SSR bundle.

**Instead.** Both together: [Components that draw only in the browser](../pages-and-data/server-render.md#components-that-draw-only-in-the-browser).

### Another name for layout_.tsx

**Turned down (2026-11-01).** `layout.root.tsx`, `root.tsx`, `layout.reset.tsx`, `layout.alone.tsx`, `layout-.tsx`, `layout@.tsx`, `layout~.tsx`, `layout+.tsx` and `layout!.tsx`.

**Why not.** React Router and TanStack Router end a name with `_` for a route that leaves what is above it, so the mark is known. `reset` is the word of SvelteKit, where it keeps the root. `-` looks like a typing mistake, files that end in `~` are often ignored by git, `+` already marks a parameter, and `!` is special in a shell.

**Instead.** `layout_.tsx`. Unlike in those two routers it drops the root Layout too, and the docs say so: [A Layout without the Layouts above](../pages-and-data/layouts.md#a-layout-without-the-layouts-above).

### Calling the error export Error

**Turned down (2026-11-18).** `export function Error` in `page.tsx` and `layout.tsx`, and two error classes in the browser, `RouteError` and `TifaError`.

**Why not.** A component named `Error` hides the global `Error` in its module: `e instanceof Error` then compares with the component and is always false, and `tsc` says nothing. React Router and Remix say `ErrorBoundary`, the name an agent that never saw Tifa tries first.

**Instead.** `ErrorBoundary`, and one class, `TifaError`, with `status`, `message` and `fields`: [Errors and 404](../pages-and-data/errors.md).

## Requests, handlers and errors

### A query string on the handler functions of POST, PUT and PATCH

**Not done (2026-11-25).** The functions `route.gen` writes for `POST`, `PUT` and `PATCH` send the input as the body and have no place for a query string, though the handler can read one with `c.Query`.

**Why not.** A write seldom needs one: a switch such as `?dryRun=1` can be a field of the body. Giving it types would take following Ctx through handlers as for `Load`, or a second struct in the signature of a handler.

**Reopen when** an app needs it. An option without types, `search`, is the first step then.

### Route constants for what a request can set

**Turned down (2026-11-23).** `BodyLimit`, `ReadTimeout` and `LayoutSSR` as constants in `route.go`. All three existed and were removed.

**Why not.** What a request can set needs no constant, and no app used `LayoutSSR`.

**Instead.** `c.SetBodyLimit` and `c.SetReadTimeout` in `OnRequest` ([Body limit and read time of a route](../pages-and-data/handlers.md#body-limit-and-read-time-of-a-route)), and `const SSR = false` on each page. `SSR`, `Cache` and `Memo` stay, because the build needs them.

### Loading page data again after a write

**Turned down (2026-11-03).** After a method handler answers, loading the data of the page and its Layouts again without being asked, as Remix and React Router do.

**Why not.** Tifa does not know what a write changed, and every request sent on a guess is load on the server. TanStack Router, which Tifa follows in the browser, has no such step.

**Instead.** The page calls `router.invalidate()` when it should show what the write changed: [Calling method handlers from the browser](../pages-and-data/handlers.md#calling-method-handlers-from-the-browser).

### Renaming c.Param

**Turned down (2026-11-25).** `c.PathParam`, or `c.PathValue` after the standard library, in place of `c.Param`.

**Why not.** The page calls the same thing params: the generated `Params` type and the `params` of a link. TanStack Router, React Router, Next.js, SvelteKit, Express, gin and echo all say params for the parameters of the path. A new name on the Go side alone would give one thing two names.

### Path parameters in the input struct

**Turned down (2026-11-25).** Decoding the parameters of the path into the second argument of `Load`, `Layout` or a handler, next to the query string or the body.

**Why not.** The folders already declare them. A second declaration in a struct could disagree with the folder, and one struct would be filled from two places.

**Instead.** `c.Param("name")`.

### Dropping Validate, or giving it another return type

**Turned down (2026-11-25).** Two ideas: removing the `Validate` method of an input struct so every handler calls its own checks, and `Validate() tifa.Fields` in place of `Validate() error`.

**Why not.**
- `Validate` runs every time the framework decodes the input, so a handler cannot forget it.
- With `tifa.Fields` as the return type, the version that takes a Ctx would have another shape than the one that does not, and a check that needs to answer 409 would have to move into the handler.
- The one real fault was that a plain error from `Validate` reached the caller with its text. That is fixed: it is now answered like a plain error of a handler.

**Instead.** `Validate() error` and `Validate(c *tifa.Ctx) error` as they are, under the one rule for errors: [What an error you return becomes](../pages-and-data/errors.md#what-an-error-you-return-becomes).

### A form tag on the fields of an input

**Turned down (2026-11-18).** Taking the name of a field from a `form` tag first, then from `json`, then from the Go name.

**Why not.** Two tags are two names for one field, and a struct could decode one way from JSON and another way from a form.

**Instead.** The `json` tag names a field everywhere: in JSON, in a form, in the query string and in TypeScript. A `form` tag fails the build.

### Renaming tifa.Use

**Kept as it is (2026-11-18).** `tifa.LayoutData`, because `Use` attaches middleware in gin and echo.

**Why not.** `Use` is short and close to `use()` in React. Someone who takes it for middleware gets a compile error about the signature, so the mistake costs little.

### An API for private data that is big or written often

**Turned down (2026-11-03).** `useClientData`, `usePrivate` or `definePrivateData` for a cart, recommendations or an unread count, and adapters for state libraries.

**Why not.** Such data is big and written often, which a session is not. When to load it, and whether to load it ahead, is for the app to choose.

**Instead.** An API route and its generated `GET` function, which works as the `queryFn` of TanStack Query. The cart of a visitor who is not signed in is kept on the server under a cookie, by the app.

**Reopen when** real apps need more than that.

### A refresh option on a write

**Turned down (2026-11-03, confirmed 2026-11-25).** `refresh: true` on a call of a handler, so one answer carries the result and the new data of the page and its Layouts.

**Why not.** It was built and taken out. Loading the routes inside the request of the write means doing again everything a navigation request does: the middleware, the paths of cookies, the session, the order of answers. Each fix showed a new corner. It saves one round trip, and the `Load` functions do the same work either way.

**Instead.** The page calls `router.invalidate()`, which is an ordinary navigation request and right for that reason.

### Removing c.Bind and c.BindQuery

**Turned down (2026-11-25).** Leaving the second parameter of a handler as the only way to decode an input, and reading the raw body with `c.Request()` otherwise.

**Why not.**
- They are for a handler that writes its own response: a stream, a download, server-sent events. A handler with a second parameter has its response written by the framework, so such a handler cannot take one. The ones that read the query string are common, since `EventSource` only sends GET.
- `c.Bind` does more than parse JSON: it reads JSON, a URL-encoded form or a multipart form with files by the content type, refuses data after the JSON value, and answers 413, 408, 415 and 400 for a body that is too big, too slow, of another type or broken. A handler that reads `c.Request().Body` itself would get most of these wrong.
- They are no second decoder: the second parameter is decoded by the same code.

**Instead.** Both stay, for handlers only. In `Load`, `Layout` and `Validate` they fail the build.

### Reading query parameters with a call in Load

**Turned down (2026-11-25).** `Load(c)` reads each query parameter with `c.Query("name")`, and Tifa follows the Ctx through the functions `Load` calls to find the names. The other way, `c.BindQuery(&q)` in `Load`, was refused before for the same reason.

**Why not.**
- The names come from reading the code, so a Ctx the build cannot follow (a helper outside the module, an interface, a stored Ctx) hides a parameter. The page does not load again when it changes, and the server render and the memo key do not see it. The runtime also gave `Load` the whole query string, whatever the build found.
- A parameter had no type, so every page parsed its own numbers, and a value that did not parse was left out without a word.

**Instead.** `Load(c, q Filter)` and `Layout(c, q Filter)`: the parameters are the fields of the struct in the signature, which the build reads without following anything, and the runtime gives `Load` the declared ones only. The types, the loading again on a change, the render URL, the memo key and the canonical link all come from the same fields. `c.Query`, `c.QueryValues` and `c.BindQuery` are for method handlers (and `Validate`, `OnRequest` for the first two), and a call in `Load` or `Layout` fails the build, or panics when the build could not see it.

**Reopen when** an app needs query parameters whose names are not known when it is built, such as a free list of filters. A field that holds the rest of them would be the first thing to try.

### The browser guessing the type of a query value

**Turned down (2026-11-25).** The router parses `?page=2` as the number 2 and `?stock=true` as a bool, as TanStack Router does by default, and the page uses what it finds.

**Why not.** A guess by the look of the value gives a different type for the same parameter on different URLs: `?q=1.0` became the number 1 and `?q=007` became 7, so a search for those words changed (commit 665a754d turned it off for this reason). Go and the browser would have to guess alike for every text, floats included, or the server render and the hydration would differ.

**Instead.** The Go struct of the `Load` says the type of each field, and the browser converts by that declaration and by nothing else. Go and the browser read a text as the same value by the same rules, and one table of cases tests both (`internal/core/testdata/query-values.txt`). This is for the query string of a page: a handler has no address to keep and no memo key, so `c.BindQuery` and the input of a `GET` or `DELETE` handler decode as they did. The browser converts so since 2026-11-26 (`searchCodec` in `js/runtime/searchcodec.ts`, and the table is read in Go, in Node and in beastjs).

**Reopen when** a type that the rules cannot say is needed in a query string, such as a date or an id with its own text form.

### Answering 400 for a number written another way

**Turned down (2026-11-26).** Reading a number only in the one way Go writes it, and answering 400 for `?page=007`, `?rating=1.0`, `?price=.5` or `?price=%2B1e3`. This was the rule of the first typed query, so that Go and the browser would read the same text the same way.

**Why not.** A visitor who edits the address bar writes `007` or `1.0`, and a number input sends `1e3` or `.5`. The page can take them without the two sides disagreeing, since both read the same number, and the address ends up as the shortest text anyway: the server renders for it (one render, one memo entry, one ETag and one canonical link for `?page=007` and `?page=7`, and the render URL is what the router would write, so it does not redirect), and the browser rewrites the address bar before it takes the page over, with no request for data. The only things the strict rule protected were the memo key and the canonical link, and the shortest text does that too. A 400 for a number the visitor meant, and a workaround that declared the field a `string`, were a worse answer.

**Instead.** The set of spellings is small and fixed. A whole number is digits with an optional `+` or `-` and an optional point with only zeros after it, a `float64` is digits with an optional sign, fraction and exponent, `-0` is 0, and both are read as the same number in Go and in the browser (`internal/core/testdata/query-values.txt` has the cases, with the shortest text of each, and is read in Go, in Node and in beastjs, next to 120,000 random spellings that Go and the browser must read alike). A bool is `true` and `false` only: the one other thing a plain form sends for a checkbox is `on`, and a field that should take it can be a string. A string is never changed (`?q=007` stays `007`). Still 400: a text that is no number of its type (`10.50` or `1e3` for a whole number, `abc`, `0x10`, `NaN`, a space), a number out of the range of its type or of 2^53-1, a float outside 1e-6 and 1e21 (a browser writes those with an exponent), a text of more than 64 bytes (Go and a browser round a decimal of hundreds of digits differently, and no number a person types is near that), and a name given twice for a field that is not a list. The input of a method handler is not part of this.

**Reopen when** a spelling that people really write (a space around the number, a thousands separator, `on` for a bool) shows up in the logs of an app.

### A float32 field in a query struct

**Turned down (2026-11-26).** Accepting a `float32` field, or a slice of them, in the struct a `Load` or `Layout` takes as its second parameter, as other Go frameworks do. gin, echo, gorilla/schema, go-playground/form and grpc-gateway parse such a field with `strconv.ParseFloat(value, 32)` and keep the rounded value, huma marks it `format: float` in OpenAPI, and tools that write TypeScript from Go map `float32` and `float64` both to `number`, so with them Go and the browser can hold different numbers for the same address.

**Why not.** The typed query exists so that Go and the browser read every query value as the same number, and `float32` would be the one silent exception. A browser has only 64 bit numbers and no way to write the shortest text of a 32 bit float: `?price=1234567.89` is 1234567.9 in a float32 and the page would show 1234567.89, and `String(Math.fround(0.1))` is `0.10000000149011612`, where Go writes `0.1`. Handler inputs are not part of this, since they have no address to keep.

**Instead.** `float64`, and the build says so.

**Reopen when** apps are seen to need `float32` in the query of a page for a reason `float64` does not serve.

### An empty number or bool in a query string

**Turned down (2026-11-26).** Answering `?page=` with a redirect to the address without it, or keeping the empty text as a value in the browser.

**Why not.** The router reads an empty number as not passed and writes the address back without it, so the server render found the address spelled differently and answered 308. A browser keeps a 308 for good, and a plain GET form that is sent with its fields left empty (`?q=&min=&max=`) would cost a round trip each time. Keeping the empty text as a value cannot work for a number.

**Instead.** An empty value of a field that is not a string counts as not passed, in Go and in the browser. The server leaves it out of the URL it renders the page at (and so out of the memo key and the canonical link) and answers 200. A number is written the shortest way there too (see the entry above). Before the browser takes the page over, it writes the address as the router reads it, without the empty value, so the router has nothing to load again: no request for data, no reload, no hydration warning. A page without SSR carries its boot URL without the empty values for the same reason. For a string, empty is a value and stays.

**Reopen when** the router stops comparing the address it reads with the one it writes, or a browser shows a flash or a warning for the address change before the page is taken over.

### A Layout with query parameters passed to tifa.Use

**Not done (2026-11-25).** `tifa.Use(c, Layout)` for a Layout that takes a second parameter, `Layout(c, q Filter)`.

**Why not.** `tifa.Use` takes `func(*Ctx) (T, error)`, and Go cannot infer a generic function from two signatures. A second function name, such as `tifa.UseWith(c, Layout, q)`, is a new name to learn for a case no app has yet: every `tifa.Use` in the apps and tests of the repository passes a Layout that reads only path parameters. The build explains the type error of the compiler when a call does not compile.

**Instead.** A Layout whose result other `Load` functions need takes no query parameters, and the `Load` of the page reads them.

**Reopen when** an app needs the result of a Layout that depends on query parameters from another `Load`. Then add the second function name.

## Head, setup and page exports

### The title and meta tags as elements in a component

**Turned down (2026-11-23).** Dropping the `head` export for `<title>` and `<meta>` written in components, which React 19 moves into the head.

**Why not.** Tifa writes the head of the document as a string, the heads of nested routes are merged, and a 404 or error page needs a head when its component did not render.

**Instead.** The `head` export and `documentHead` of the root Layout. The build warns about a `<title>` or `<meta>` written as JSX: [Title and meta tags](../pages-and-data/head.md#title-and-meta-tags).

### Renaming the head export to metadata

**Turned down (2026-11-23, again 2026-11-25).** Calling the `head` export of `page.tsx` and `layout.tsx` `metadata`, as Next.js does.

**Why not.**
- The exports of a `page.tsx` are the options of its route. `head` is passed on as the route option `head` of TanStack Router, with the same name and the same shape.
- `metadata` in Next.js is an object of meanings (`title`, `description`, `openGraph`) that Next.js turns into tags. `head` returns the tags themselves (`meta`, `links`, `styles`, `scripts`). Under the name `metadata`, people and agents would write the shape of Next.js.
- The build warns about an exported `metadata` object and says to write `head`. With the same name for another shape it could not.

**Instead.** [`head`](../pages-and-data/head.md#title-and-meta-tags).

**Reopen when** Tifa stops passing `head` to TanStack Router and takes an object of meanings.

### Setup code at the top of the root Layout

**Turned down (2026-11-23).** Dropping `setup.ts` and putting what every page needs at the top of `app/layout.tsx`.

**Why not.** A `layout_.tsx` drops the root Layout, the root Layout may be left out, and a reader could not tell the code of the Layout from the code of the app.

**Instead.** [`setup.ts`](../pages-and-data/hooks.md#setupts).

### One head export in the root Layout

**Turned down (2026-11-23).** Dropping `documentHead` and letting the `head` export of the root Layout carry the tags of every document.

**Why not.** `head` of the root Layout gets the data of the `Layout` function. A 404 page, an error page and a page under a `layout_.tsx` have a document but not that data, so they would get none of these tags.

**Instead.** `documentHead` is a plain object for what every document carries, with `htmlAttributes` inside it in the shape `head` returns: [The document shell and documentHead](../pages-and-data/head.md#the-document-shell-and-documenthead).

### hooks.ts for setup code

**Turned down (2026-11-23).** A `hooks.ts` at the root that exports `onStart()`.

**Why not.** Next to `hooks.go` it reads as the same thing, and `hooks` in a `.ts` file reads as React hooks.

**Instead.** `setup.ts`: plain top-level code with nothing to export.

## Hooks and wiring

### A scheduled job API

**Turned down (2026-11, again 2026-11-18).** A function such as `tifa.Every(interval, fn)`.

**Why not.** Go already has goroutines and `time.Ticker`, and a job needs nothing that only the framework knows.

**Instead.** Start the goroutine in `OnStart` and stop it in `OnShutdown`: [Background work](../guides/jobs.md).

**Reopen when** a job needs something only Tifa has, as sitemaps need the routes.

### A rule on what the root package imports

**Turned down.** A convention that the root package of an app imports nothing from `app/`.

**Why not.** How an app arranges its own imports is the app's business, not the framework's.

### Another name for hooks.go, or hooks in any file

**Turned down (2026-11-01).** `middleware.go`, `server.go` or `app.go` in place of `hooks.go`.

**Why not.** `middleware.go` fits only `OnRequest`, `server.go` says too little, and `app.go` reads as `app/`. `hooks` is the one name that holds start, shutdown, every request and every error, as `hooks.server.ts` does in SvelteKit. A fixed file name also tells a reader, and the build, where to look.

**Instead.** `hooks.go`, `options.go`, `content.go` and `setup.ts` at the root of the app, each with one job: [Hooks](../pages-and-data/hooks.md).

### Names in hooks.go that are not events

**Turned down (2026-11-02).** A `Wrap` function in `hooks.go` for `net/http` middleware, a `Session` function there, `tifa.SetLogger` and `tifa.UseSessions`.

**Why not.**
- `hooks.go` holds what happens: `OnStart`, `OnShutdown`, `OnRequest`, `OnError`. Wiring is not an event.
- Tifa logs with `log/slog`, so `slog.SetDefault` already sets its logger.
- `UseSessions` reads like the `useSession` of the page.

**Instead.** Wiring is a call in `OnStart`: `tifa.SetSessionStore`, `tifa.SetHTTPHandler`, `slog.SetDefault`. `OnRequest` is the middleware of the routes. `tifa.SetHTTPHandler` wraps the whole server, for `net/http` middleware and for handlers outside the routes, such as pprof: [Setting up in OnStart](../pages-and-data/hooks.md#setting-up-in-onstart).

### Checks on how an app uses cookies and headers

**Turned down (2026-11-03).** A warning in `tifa dev` when an app sets a cookie without a `Path`, and a note in the docs about what that breaks.

**Why not.** How an app uses a standard part of the web is the app's business. Tifa sets its own cookie right and checks nothing about the others.

## Sessions

### Stronger promises about the session in the browser

**Turned down (2026-11-02).** A full page load after sign-in, comparing a hash of who is signed in, keeping tabs in step with each other, a promise about pages the browser restores from its back and forward cache, a session that lasts longer each time it is used, and a second cookie that only says "signed in".

**Why not.** Next.js, Nuxt and React Router promise none of these, and each one adds a mechanism that can go wrong. A session that grows with use needs a conditional update in every store and can bring back a session that was just deleted.

**Instead.** The page loads its data again after sign-in and sign-out, a session ends at a fixed time, and the browser keeps one copy of what `Exposed()` returns. The app implements the store, and a memory store is the default: [Sessions](../pages-and-data/sessions.md).

### Session.In, Session.Out and Browser()

**Turned down (2026-11-03).** `Session.In` and `Session.Out` components that put a `span` around their children, a public `data-session` attribute as a second way to do the same, and `Browser()` as the name of the method that says what the browser gets.

**Why not.** A `span` is wrong around a `div`. Two ways to do one thing mean nobody decided. A misspelled component does not compile, and a misspelled attribute fails without a word.

**Instead.** `SignedIn` and `SignedOut` from `tifa/client`, which add no element, and `Exposed()`: [In the browser](../pages-and-data/sessions.md#in-the-browser).

### A built-in Redis or SQL store, and sign-in itself

**Turned down (2026-11-02).** Stores for Redis and SQL in Tifa, a file store for `tifa dev`, and passwords, OAuth or users as part of the framework.

**Why not.** A session has one job, keeping the page and the person apart. Where it is kept and who a user is belong to the app.

**Instead.** The app implements `Get`, `Put` and `Delete` and sets the store in `OnStart`: [Where to store sessions](../pages-and-data/sessions.md#where-to-store-sessions). Signing in is a guide: [Sign in and sign out](../guides/sign-in.md).

### A CSRF token

**Turned down (2026-11-02).** A token in every form.

**Why not.** Every write passes `http.CrossOriginProtection` of the standard library, which reads `Sec-Fetch-Site` and otherwise compares `Origin` with `Host`, and the session cookie is `SameSite=Lax`.

**Instead.** The check is always on: [Cross-site writes](../build-and-deploy/config.md#cross-site-writes).

### A setting that turns Secure off

**Turned down (2026-11-18).** `session.secure`.

**Instead.** The cookie stays as it is, and the log gets one warning when `tifa.SetSession` answers a request that is neither local nor HTTPS. That is Tifa checking its own cookie, not the app's.

## Rendering, memo and Cache

### Keeping the site files for a short time

**Turned down (2026-11-26).** Keeping `/sitemap.xml`, `/llms.txt`, `/llms-full.txt` and the `.md` versions for a short time, the first idea being 10 seconds, so that a crawler that loops on them does not cost a build each.

**Why not.** A file would be old for that time, and a post would not be in the sitemap from the moment it comes out, which [the SEO guide](../guides/seo.md) promises. The [render memo](../reference/memo.md#site-files) gives the same saving with nothing old.

**Instead.** The function of `SetSitemap` and `OnRequest` run on every request, and the files are keyed by what the function returned, in the one memo with the pages and its one size. The list is the same, so the file is not built again, and a changed list shows at once. Only a 200 is kept.

### Markdown to HTML in Go

**Turned down (2026-10-29).** A `Load` that turns Markdown or other content into HTML in Go, with goldmark or a template.

**Why not.** Tifa is not a template engine. Rendering in Go would lose the React ecosystem: remark and rehype plugins, and components inside the content.

**Instead.** `Load` returns the content and React renders it: [Content](../pages-and-data/content.md).

### A react-markdown parse cache

**Turned down (2026-11-16).** Keeping the parsed Markdown between renders.

**Why not.** When the render memo hits, nothing is parsed anyway, so the cache would only help misses, for its memory.

**Instead.** The [render memo](../reference/memo.md). An optimization that overlaps with the memo is first measured on what is left after memo hits.

### A cache version set by the app

**Turned down (2026-11-18).** Letting an app declare a version so the render memo hits more often, or changing the data of `examples/shop` to raise its hit rate.

**Why not.** It moves the framework's job to every app, and tuning the example for a score makes the measurement say nothing about real apps.

**Instead.** Better memo hits come from the framework side only.

### A switch that lets renders yield the CPU

**Turned down (2026-11-08).** A setting that makes a long render give the CPU to short requests.

**Why not.** It would rest on behavior of the Go scheduler that Go does not promise.

**Instead.** Keep `GOMAXPROCS` above `ssr.vms`. The measured cost of not doing so is in [config.md](../build-and-deploy/config.md) under `ssr.vms`.

### A place for styles collected after the render

**Turned down (2026-11-25).** A way for an app to put text into the head once the render is over, which styled-components (`ServerStyleSheet`) and the cssinjs of Ant Design need: they keep the styles of a render in a sheet and expect the server to write the sheet into the head afterwards.

**Why not.** Both libraries are on their way out, and Tifa does not add API for them. Libraries that write a style next to each component as they render (Emotion, so MUI and Chakra UI), that ship a CSS file (Mantine, daisyUI) or that have no styles of their own (Base UI, React Aria, Radix) need no such place.

**Instead.** Nothing in Tifa. `test/apps/uilibs` shows what an app can do by itself: write the sheet out at the end of the page for styled-components, and the static `antd/dist/antd.css` for Ant Design. Those two pages are there because their code once made the build run without end, not to keep the libraries working: when an upgrade breaks what the test says about their styles, that part of the test goes.

**Reopen when** a library that many new apps choose needs it.

### Keeping whole pages in the program

**Removed (2026-11-09).** A store of finished pages in memory, with the settings `cache.whole_page` and `cache.ignore_query`.

**Why not.** The render memo is always right and covers every page. Keeping a page for a while, at the price of showing an old one, is the job of a CDN, and the two should not share a name or a setting.

**Instead.** The [render memo](../reference/memo.md), and `const Cache`, which only makes the request anonymous and sets `Cache-Control` for a CDN: [Cache for CDNs](../pages-and-data/ssr-and-cache.md#cache-for-cdns).

### A memo of pieces smaller than a Layout

**Turned down (2026-11-08, 2026-11-09).** Keeping the HTML of a part of a page that does not change with the request, and keeping the page while rendering its Layout again.

**Why not.** Tifa cannot know how an app writes its pages, so it cannot tell which part stays the same. A Layout whose data changes while the data of the page does not is rare.

**Instead.** One memo entry for each page, whose key is also the ETag.

**Reopen when** there is a design that keeps the rendered Layout and renders only the page into it, with nothing for the app to write. Nothing smaller than a Layout.

### Fewer page switches

**Kept as they are (2026-11-23).** Dropping one of `const SSR`, `const Cache` and `const Memo`, or moving them out of `route.go`.

**Why not.** The build needs each of them: `SSR` for what goes into the SSR bundle, `Cache` for the check that no `Load` of the page reads a cookie or the session, and `Memo` for the check in `tifa dev`. `LayoutSSR`, `BodyLimit` and `ReadTimeout` were removed in the same round, see [Route constants for what a request can set](#route-constants-for-what-a-request-can-set).

### A list of query parameters written by hand

**Turned down (2026-11-09).** A setting that lists the query parameters that count for the render memo, and making the first render in the browser use the reduced URL too.

**Why not.** The list exists already: the parameters that the `Load` and the Layouts of the page take. A second list would drift from the code. The second idea was built and dropped: nothing tells when an app's own Suspense boundaries are done, and it raced with early navigation and with history entries.

**Instead.** The memo key and the URL given to the render hold the path and those parameters, sorted by name, so `?x=random` makes no new entry. A 404 and a page whose `Load` failed are not kept: [Query parameters](../reference/memo.md#query-parameters).

### A cache switch on a Layout

**Turned down (2026-11-02).** HTTP caching of the data of one Layout, and navigation data that a CDN may keep.

**Why not.** No framework caches by Layout at the HTTP level, and Cloudflare does not look at `Vary`.

**Instead.** Navigation data is always `private, no-cache`, under a URL that differs from the page's (`?_tifa=`), so a CDN never answers it with the HTML. To keep the data of a Layout, cache it in Go.

### A long cache time for public/

**Turned down (2026-11-02, again 2026-11-20).**

**Why not.** A file in `public/` keeps its name, so any cache time shows the old file for that long after a deploy. Next.js, Vercel, Netlify and SvelteKit do not add one either.

**Instead.** Files under `/assets/` have a hash in the name and are kept a year, `immutable`. `public/` is `no-cache` with an ETag: [Start an app](../start/app.md).

### Renaming `_tifa` to `__tifa`

**Kept as it is (2026-11-18).** The query parameter of navigation data, next to the path prefix `/__tifa/`.

**Why not.** It would only look tidier, and docs and CDN rules are written with `_tifa`.

**Instead.** The names an app or a CDN can meet are fixed: [Compatibility](../reference/compatibility.md).

## Build, types and tools

### Other ways to keep the generated types fresh

**Turned down (2026-11-25).** Three ways to make sure `route.gen.ts` is never older than its `route.go` when `tifa dev` is not running: a TypeScript language service plugin that writes the types again, `tifa gen --watch`, and keeping `.tifa/` in git.

**Why not.**
- A language service plugin runs in an editor only. `tsc` does not load it, and an agent runs commands, not an editor.
- `tifa gen --watch` is a second process to start and to forget. `tifa dev` already writes `.tifa/` again on every save.
- Files kept in git are as old after a change to `route.go` as files that are not, until a command writes them again. They would also add a diff, and conflicts, to every change of a route.

**Instead.** The same as for the entry above.

### Checks that only tifa dev runs

**Turned down.** Relying on `tifa dev` to catch a mistake.

**Why not.** An agent may build, test and deploy without ever starting `tifa dev`.

**Instead.** Checks go into `tifa build` and `tifa check`, which every path runs.

### A build without Node

**Put off (2026-11-19).** Bundling with esbuild inside `tifa`, or growing the JavaScript engine into a runtime that runs npm and build tools.

**Why not.** An app would only save one install, and it still needs npm for its packages. It would lose what comes with Vite: Fast Refresh, Vite plugins, remark and rehype, the rewrites that make renders faster, and the way of writing an agent already knows. The engine cannot load native modules such as Rolldown.

**Instead.** `tifa build` and `tifa dev` run the app's own Vite on Node. The running app needs no Node.

**Reopen when** Node or Vite is the first thing that fails when new apps are made, upgrades of Vite or Rolldown keep breaking what Tifa reads inside them, or the memory or time of a build on a small machine becomes a real complaint.

### A looser check of the TanStack Router version

**Turned down (2026-11-16).** Accepting the version before the pinned one too, and asking TanStack for public API in place of the internal parts Tifa uses.

**Why not.** Server rendering stands on parts of TanStack Router that are marked for frameworks only. With one exact version, what was tested is what runs.

**Instead.** A test names every internal member Tifa uses, and another keeps a fingerprint of each piece of TanStack source Tifa copied. When an upgrade breaks one, it names the function to look at.

### Telling Windows users to use WSL

**Turned down (2026-11-22).** Docs or commands that need WSL on a developer's machine.

**Why not.** Many developers work on Windows, and "use WSL" turns them away.

**Instead.** `tifa dev`, `tifa build` and `tifa deploy` run on Windows itself, and CI tests them on a Windows runner. Nothing on the developer's machine may need `sh`, Unix sockets or Unix signals without a Windows path. The server that `tifa deploy` sets up is Linux.

### Static export, and the app as wasm on a Worker

**Turned down (2026-11-04).** Writing the site out as files, and compiling the app to wasm to run on Cloudflare Workers.

**Why not.** Tifa is for a Go program on a VPS. A static export would need a second set of rules for everything that expects a server, and people choose Next.js for its server rendering and Astro for static sites. wasm on a Worker would be a second Tifa, not a way to deploy this one.

**Instead.** `const Cache` and a CDN in front of the program.

**Reopen when**, for wasm only, someone clearly needs a dynamic Tifa app on Workers.

### An npm package

**Put off (2026-11-07).** `@liustack/tifa`, holding the JavaScript of Tifa and TanStack Router.

**Why not.** It is a second place to release from and a new mistake to make: a CLI and a package of different versions. It would save a few lines of `package.json` and the version check.

**Instead.** The JavaScript ships inside the Go module, and one `go get` upgrades all of it.

**Reopen when** apps keep meeting version problems with TanStack or Vite, or having no types before `.tifa/` is written becomes a real problem.

### A build check that may fail code that is right

**Turned down (2026-11-07).** Checks that guess.

**Why not.** A build that fails code that is right is worse than one that misses a mistake.

**Instead.** A build check fails only what is surely wrong, and catching part of the mistakes is enough. What it cannot be sure of is a warning or nothing.

### Browser errors printed in the terminal

**Turned down (2026-11-03).** `tifa dev` writing the errors of the browser into its own output.

**Why not.** An agent cannot see the browser console, and often not the terminal of `tifa dev` either.

**Instead.** `tifa errors` asks the running `tifa dev` for the errors since the last build, with `--json` and exit code 1 when there are any.

### More in tifatest

**Turned down (2026-11-05).** Assertions of its own, browser automation, a mock of `fetch`, and a parameter that overrides settings.

**Instead.** `tifatest.Start` and `tifatest.StartPages` return a `*httptest.Server`, and a test uses `net/http` and `testing` from there: [Testing apps](../pages-and-data/testing-apps.md).

## Configuration

### Variants of .env and names for environments

**Turned down (2026-11-03).** `.env.production` and `.env.local`, and a built-in name such as `NODE_ENV` or `production`.

**Why not.** `.env` is not in git, so each machine has its own, and that file is the environment.

**Instead.** One `.env`, read from the working directory, below the real environment. Vite does not read it: [Values from the environment](../guides/environment.md).

### A prefix for environment variables

**Turned down (2026-11-03, again 2026-11-18).** `TIFA_SERVER_ADDR`, or a prefix an app may turn on.

**Why not.** One `.env` with three kinds of names is two too many, and a prefix that may be on or off gives one setting two names.

**Instead.** One rule for the settings of Tifa and the options of the app: `db.url` is `--db-url` and `DB_URL`. A key has at least two parts, since a key of one part would read `LANG` or `PATH`: [Why not a name prefix](../guides/environment.md#why-not-a-name-prefix). The inputs of `tifa deploy` are not settings, and they all start with `TIFA_DEPLOY_`.

**Reopen when** two programs in one environment want the same name.

### HOSTNAME next to PORT and HOST

**Turned down (2026-11-03).**

**Why not.** Shells and Docker set `HOSTNAME` to the name of the machine or the ID of the container.

**Instead.** `PORT` and `HOST`, which platforms such as Cloud Run set, below `SERVER_ADDR`.

### Options of the app through a getter, init or a struct

**Turned down (2026-11-03, the file name 2026-11-07).** A function such as `dbURL()`, registering in `init`, a struct with a call to parse it, and `config.go` or `settings.go` as the name of the file.

**Why not.** A getter is how a signal works in JavaScript, not how Go reads a value. `init` and a struct are more to write and more to explain. "Settings" is the word for what `tifa.yaml` holds.

**Instead.** A package-level `var` in `options.go` that is a plain value: `var DBURL = tifa.String("db.url", "app.db", "Database file")`. See [Options of the app](../build-and-deploy/config.md#options-of-the-app).

### Lists and nested values in settings

**Turned down (2026-11-03).**

**Why not.** Every setting is one value that a flag and an environment variable can carry.

**Instead.** Data with a structure goes in a file of the app, and an option holds its path.

**Reopen when** a real app has a list that can only be written in settings.

### Leaving unknown keys of tifa.yaml alone

**Turned down (2026-11-25).** Reading only the keys Tifa knows from `tifa.yaml` and saying nothing about the others.

**Why not.** `tifa.yaml` holds the settings of the framework and nothing else, so a key Tifa does not know there is a spelling mistake. The app has its own place, `options.go`, and its own ways.

**Instead.** A key in `tifa.yaml` that is no setting of Tifa fails `tifa check` and `tifa build`, and a flag nobody defined stops the program. A setting Tifa retired is a warning. Other environment variables, and other keys in the file of `-c`, are left alone: [Configuration](../build-and-deploy/config.md).

### Copying every dot file of public/

**Turned down (2026-11-25).** A switch that copies every dot file of `public/`.

**Why not.** A `.env` or a `.git` that was left there by mistake would be published with it.

**Instead.** `build.public_dotfiles` names the dot files and folders to copy, and `tifa build` says which ones it left out: [Configuration](../build-and-deploy/config.md).

### camelCase or hyphens in the keys of tifa.yaml

**Kept as it is (2026-11-25).** `server.bodyLimit` or `server.body-limit` in place of `server.body_limit`.

**Why not.** Each key is also an environment variable and a flag, and with underscores the three match word for word: `server.body_limit`, `SERVER_BODY_LIMIT`, `--server-body-limit`. camelCase would need a guess at where a word ends, as in `tlsCA`. Hyphens would map as well and bring nothing new. Docker Compose, Prometheus and fly.toml use underscores, Kubernetes and Hugo camelCase, GitHub Actions hyphens, so there is no standard to follow.

## Languages

### Choosing the language from the request

**Turned down (2026-11-07).** Redirecting by `Accept-Language`, a cookie or the address of the visitor. Showing another language when a translation or a content entry is missing. A prefix rule for each part of the site. An optional route parameter for the language.

**Why not.** A URL shows one language, and nothing is chosen behind the visitor's back. An optional parameter cannot be written for Go's `ServeMux`: `/{lang}/cart` next to `/projects/{project}` panics.

**Instead.** A `_locales/` folder says which languages the pages below it have. The default language has no prefix, as in Astro, Nuxt i18n and the pages router of Next.js, and the others come first in the path (`/en-US/store`). A content entry that is missing in a language is a 404 there: [What Tifa does not do](../guides/i18n.md#what-tifa-does-not-do).

### A translation library in the core

**Turned down (2026-11-07).**

**Instead.** The core has none. `tifa/i18n` is an adapter for i18next, the most used one, and `tifa new` sets it up: [Translate the interface](../guides/i18n.md#translate-the-interface).

## Content

### MDX and components inside content

**Turned down (2026-11-04).** MDX, and Markdown compiled to React components or to a JSON tree. Also not done: Markdown at run time, references between collections, collections of JSON or YAML, generated pages for tags, number prefixes for the order, `expiryDate`, `aliases`, a `url` in the front matter, resizing local images, and a search index.

**Why not.** HTML made at build time costs a render nothing: with 1000 entries (10 MB) the bundles for the server and the browser are the same byte for byte as with none, and turning them into HTML takes about 2 seconds.

**Instead.** The build turns Markdown into HTML with unified, `Load` returns it as `tifa.HTML` and the page shows it with `<HTML value={...} />`: [Content](../pages-and-data/content.md). Content that lives in a database is read by the app's own `Load`.

### Collections for many thousands of entries

**Not a goal (2026-11-23, 2026-11-25).** Saying that collections are for sites of any size.

**Why not.** They were measured at 1,000 entries of about 10 KB: the program grows by about 10 MB and the build takes about 2 seconds more. 2,000 or more work too, with a slower build and a bigger program, but 1,000 is already a lot: a personal blog does not get there in ten years, and it covers the articles of a small product site.

**Instead.** The docs say "up to about 1,000 entries". Beyond that, or for writing that people edit while the site runs, the content goes in a database and the app is its CMS.

## Search engines and language models

### Leaving sitemap.xml, robots.txt and llms.txt to the app

**Turned down (2026-11-19).** An app writing these itself as handlers, since the standard library can serve a file.

**Why not.** They need what only the framework has: the table of routes, the content collections, the languages of each page and the Markdown source.

**Instead.** Tifa answers `/sitemap.xml`, `/robots.txt`, `/llms.txt`, `/llms-full.txt` and the `.md` version of a page: [SEO](../guides/seo.md).

### A name in route.go for the sitemap

**Turned down (2026-11-20).** A constant such as `Index`, and a function in each `route.go` that lists its URLs.

**Why not.** Next.js, Nuxt, Astro and SvelteKit all set the sitemap up in one place, and `route.go` has names enough.

**Instead.** `site.url` is the switch that says the site is public, and `tifa.SetSitemap` in `OnStart` lists the pages with parameters. A page leaves the index with the `robots` meta tag of its `head`.

## Deploying

### Keeping the assets of older builds

**Turned down (2026-11-03, again 2026-11-20).** A new build would keep serving the `/assets/` files of the builds before it, so a page of an older build still finds its scripts. Vercel's Skew Protection works this way.

**Why not.**
- Nobody knows which build a visitor has open: a tab can stay open for days, so no number of kept builds is enough.
- Each kept build adds about 6 to 7 MB to the program.
- The build would need the assets of the build before it, from CI or a folder kept somewhere.

**Instead.**
- A page of an older build reloads once when it meets the new one: [Pages open while you deploy](../build-and-deploy/deploy.md#pages-open-while-you-deploy). Next.js self-hosted, SvelteKit and Nuxt do the same.
- Old HTML that a CDN still holds is taken away by clearing the CDN cache on deploy, when the app gives a token: [Clearing the CDN cache](../build-and-deploy/deploy.md#clearing-the-cdn-cache). Without one, the old copy stays until it runs out, which is the accepted cost of a CDN.

**Reopen when** old HTML in a CDN still breaks pages in practice although the cache is cleared on deploy, or an app needs open pages to keep working through a deploy without a reload.

### A reload that skips the CDN

**Turned down (2026-11-21).** When a page meets a new build, it would reload to its own URL plus a build ID, such as `?_tb=<build>`, so the CDN misses and the new page comes from the program. Clearing the cache would then only make new pages show up sooner.

**Why not.** The reload is code in the page's scripts, and the pages that need it most cannot run it. A visitor who opens an old page from the CDN gets HTML whose scripts the new build no longer has: no script runs, so nothing reloads. The idea only helps a tab that was already open and working, which the plain reload already handles once the CDN copy runs out. No framework or CDN removes this gap entirely: Vercel, Netlify and Cloudflare Workers route old pages to old builds only on their own platforms and only for a limited time.

**Instead.** Clear the CDN cache on deploy when the app gives a token. Without one, a page with `Cache` can be up to its `Cache` time old after a deploy, an accepted cost: [Clearing the CDN cache](../build-and-deploy/deploy.md#clearing-the-cdn-cache).

**Reopen when** a page can recover even when its entry script is gone.

### Clearing the CDN cache from the running program

**Turned down (2026-11-23).** The new build clearing the CDN cache when it starts.

**Why not.** Clearing the cache is a step of a deploy. The program that serves visitors does not do it.

**Instead.** `tifa deploy` clears it after a deploy. The token stays on the server and is read over ssh at that moment: [Clearing the CDN cache](../build-and-deploy/deploy.md#clearing-the-cdn-cache).

### Hints that tell the user to run gh

**Turned down (2026-11-21).** A message of `tifa deploy` that says to run `gh secret set` or another tool by hand.

**Why not.** `tifa deploy` is there to make deploying simple, so what it needs it asks for with its own command, also when that command runs `gh` underneath.

**Instead.** A `tifa deploy setup` flag for each thing to add or replace, such as `--cache-purge-token`. Only what a vendor's own page must do, such as making an API token, points there.

### The server in tifa.yaml

**Turned down (2026-11-21).** `deploy.host`, also with a warning when it is there.

**Why not.** `tifa.yaml` is in git and the address of a server should not be. Nobody reads a warning, and a commit does not pass through setup.

**Instead.** `--host`, or `TIFA_DEPLOY_HOST` in the environment or `.env`, or the one server that `~/.tifa/config.yaml` knows, a file that holds no secret: [Deploy](../build-and-deploy/deploy.md).

### Several targets, and the options of GitHub Actions in tifa.yaml

**Turned down (2026-11-20 to 2026-11-23).** Environments such as staging and production in `tifa.yaml`, and keys for the runner, the Node version, approvals, timeouts or path filters.

**Why not.** `tifa.yaml` holds only what Tifa needs to decide something.

**Instead.** One target, described by `deploy.front`, `deploy.connect` and `deploy.auto`. Anything else about the workflow is an edit of `tifa-deploy.yml`.

### Tifa downloading programs, wrangler, and a front door of its own

**Turned down (2026-11-20 to 2026-11-22).**

- Downloading `gh` or `cf` by itself. Setup installs them only with a package manager the machine already has, only when `tifa.yaml` needs them, and as a line of the plan the user agrees to.
- wrangler, which `cf` replaces.
- A reverse proxy in Tifa for several apps on one server. A Cloudflare Tunnel is the way, and the docs show Caddy as the other.
- A local `tifa deploy` that hands over to GitHub Actions by itself, which would change what a command does without a word.
- A GitHub App in place of the `gh` sign-in. Put off as too early.

### A secret that passes through the agent

**Turned down (2026-11-23).** Asking for a token in the chat, on stdin of a command the agent runs, or printing one.

**Why not.** Most of the time a coding agent runs `tifa deploy`, so a secret in the chat, on stdin or in the output would be in what the agent reads.

**Instead.** Tifa says what it will do and why, opens the exact page in the browser, the person approves there, and Tifa fetches the result itself. A token that must be typed goes into a local page that works once. Every command also runs without a terminal.

## Coding agents

### Changing behavior when an agent is detected

**Turned down (2026-11-17).** `tifa dev` noticing that a coding agent runs it and going to the background by itself.

**Why not.** A command that acts differently depending on who runs it is hard to predict, for agents and people alike.

**Instead.** An explicit flag: `tifa dev --background`, with `status`, `logs` and `stop` ([Development](../build-and-deploy/dev.md)).

### Agent hooks written by tifa new

**Turned down (2026-11-25).** `tifa new` writing a hook into the settings of a coding agent, such as `.claude/settings.json`, that runs `go tool tifa gen` after every edit of a Go file, so the types of `route.gen` are never old.

**Why not.** How an agent is set up is its user's business. Tifa gives an agent notes and a skill to read, and changes nothing about what the agent runs.

**Instead.** The notes and the skill say to run `go tool tifa gen` once after a batch of Go changes. The `typecheck` script of an app runs it before `tsc`, `go tool tifa check` writes `.tifa/` again before it runs `tsc`, and each `route.gen.ts` names the files it comes from: [`tifa gen`](../build-and-deploy/build.md#tifa-gen).

### Summaries of the docs in the AGENTS.md of an app

**Turned down (2026-11-25).** The docs index of the `AGENTS.md` that `tifa new` writes holding a line per doc with its whole summary, as it did for a day.

**Why not.** An agent reads the whole file at the start of every session, and the file was 3,780 tokens, of which the index was 1,150. The budget for the whole file is 4,000 tokens, and most sessions need two or three docs.

**Instead.** The index names the files, one line per section folder, and `go tool tifa docs` prints the summary of each doc. An agent runs that command anyway to learn where the folder is. A test keeps the whole file at 12,000 bytes, about 3,250 tokens: when a new note does not fit, another one is shortened or moved to the docs.

**Reopen when** agents that start from `tifa new` are seen to skip the docs they needed because the file names did not tell them.
