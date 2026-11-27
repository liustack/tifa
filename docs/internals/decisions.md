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

Safety here is what Tifa itself promises: the render memo gives the right page, its own cookie and session are right, and visitors of a live app cannot get in where nobody decided. A check, a refusal or a guard costs code, a rule to learn and often an exception in the API, and it is worth that only for those promises. A mistake in the app that Tifa cannot stop for good, such as a goroutine of its own that panics, an object it shares between goroutines and changes, or a response it writes over, is the app's to avoid, as it is in Go: the docs say it, and nothing is refused for it (2026-11-26). A check in the build costs far less than one at run time, so the build reports a mistake it can see by default. A guard at run time is only for those promises (2026-11-26).

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

### When a page or a Layout remounts

**Settled (2026-11-26).** A page or a Layout remounts when a path parameter of its folder or of a folder above it changes, whatever its `Load` or `Layout` function reads. Another value of a query parameter loads the data again and keeps the component. It used to remount only for the path parameters its function read, found by reading the code (2026-10-14).

**Why.**
- The path names the thing a page shows, and the query says how to look at it. State kept across another order is state about the wrong thing: a form whose inputs took their first values from order 1 (`defaultValue`, `useState(data.x)`) still shows them on order 2, and a save writes them there. Keeping it is a silent wrong write. Remounting when it was not needed loses a scroll position or an open panel, which shows at once and which the app fixes by keeping that state in a Layout above the folder.
- What a function reads does not tell what the page shows: a page without a `Load` that fetches in the browser shows another thing for another parameter too. And the reads were found by reading the code, which needed [rules on where a Ctx may go](#rules-on-where-a-ctx-may-go), since taken out.
- Next.js keys each segment in the browser by its parameters, "so navigation preserves or resets component state according to the segment's actual identity" (`create-segment-key.browser.ts`). TanStack Router, React Router, SvelteKit and Vue Router keep the component and leave a `key` or `remountDeps` to the app, which an app forgets.
- The 2026-10-14 reason, a root Layout whose route path was the common prefix of its pages and so held `projectId`, is gone: the route path of a Layout is the path of its folder.

**Instead of a way to opt out.** State that should outlive another value of the parameter, such as the scroll of a list next to the details of an item, goes in a Layout above the folder of the parameter, as in Next.js.

**Reopen when** apps need a page that keeps its state across its own parameter, such as an animation between items, and a Layout above cannot hold it.

## Requests, handlers and errors

### The methods route.go serves

**Settled (2026-11-27).** `route.go` takes a handler for eight standard methods of RFC 9110: `GET`, `HEAD`, `POST`, `PUT`, `PATCH`, `DELETE`, `OPTIONS` and `TRACE`. `HEAD`, `OPTIONS` and `TRACE` take the Ctx alone and write their own answer. Without `HEAD`, `GET` answers HEAD, and with `HEAD` and no `GET` a GET gets 405. Without `OPTIONS`, Tifa answers 204 with `Allow`, after `OnRequest`. A function that takes the Ctx first, is named after one of the eight in another case (`Post`, `get`) and is called by nothing fails the build, since it would be skipped without a word. CONNECT goes to the wrapper of `tifa.SetHTTPHandler`, because it is about the connection, not a route: a function `CONNECT` in `route.go` that takes the Ctx first fails the build and points there, and `Allow` never lists CONNECT. A first version the same day failed the build on `HEAD` and `OPTIONS`, and a second one served CONNECT in `route.go` too: the user turned down both.

**Why.**
- A path may answer HEAD alone, and OPTIONS has uses beyond CORS. Refusing a standard method breaks plain HTTP.
- What a method risks is the app's choice, as with any of the many things an app can write. TRACE sends back what its handler writes, cookies and `Authorization` included when it writes the headers, and the docs say so in one line, without a refusal. Go's `ServeMux` takes any method in a pattern, Express routes `trace`, Fastify has `TRACE` in its default list, Hono takes any method with `app.on`, ASP.NET Core has `HttpMethods.Trace` and `Connect`, and Spring has `RequestMethod.TRACE` (dispatched when the app turns it on). Next.js serves seven, without CONNECT and TRACE. Node never routes CONNECT: it goes to the `connect` event of the server, so Express and Next.js cannot see one.
- CONNECT asks for a tunnel, and what it opens is the connection, not an answer of a route. One for a host and port, as a client asks a proxy, has no path, so no route could match it. `tifa.SetHTTPHandler` sees every request before routing with the original `ResponseWriter`, so a wrapper checks `r.Method == http.MethodConnect` and takes the connection with `Hijack`, the one for a host and port and the one with a path alike (tested over HTTP/1.1). Over HTTP/2 a CONNECT is a stream and cannot be hijacked, and Go turns extended CONNECT (RFC 8441) off by default (`GODEBUG=http2xconnect=1`, golang/go#71128). Without a wrapper, a CONNECT for a host and port gets 404 and one with the path of a route 405.
- The automatic answer to OPTIONS sends `Allow`, as RFC 9110 9.3.7 asks, with 204 and no body, as Next.js does. Every route lists `OPTIONS` in its `Allow`, the one of a 405 too.
- No input and no result for HEAD, OPTIONS and TRACE: HEAD has no body, OPTIONS answers with headers and TRACE sends the request back. `route.gen` has no function for them: a page has no use for them, and `fetch` refuses TRACE.
- A folder with `page.tsx` takes no `HEAD`, as it takes no `GET`: Tifa answers HEAD of a page as its GET, and a HEAD of its own would send other headers than the page. It may take `OPTIONS`.
- The cross-site check is Go's `CrossOriginProtection`, which lets GET, HEAD and OPTIONS through and checks every other method, TRACE included. A CONNECT taken in the wrapper is outside it, like any path the wrapper answers itself.

**Instead, for CORS.** The preflight stays in `OnRequest`, since its headers go on every answer of the routes it opens: [Calls from another site](../guides/cors.md).

### A query string on the handler functions of POST, PUT and PATCH

**Not done (2026-11-25).** The functions `route.gen` writes for `POST`, `PUT` and `PATCH` send the input as the body and have no place for a query string, though the handler can read one with `c.Query`.

**Why not.** A write seldom needs one: a switch such as `?dryRun=1` can be a field of the body. Giving it types would take a second struct in the signature of a handler.

**Reopen when** an app needs it. An option without types, `search`, is the first step then.

### Route constants for what a request can set

**Turned down (2026-11-23).** `BodyLimit`, `ReadTimeout` and `LayoutSSR` as constants in `route.go`. All three existed and were removed.

**Why not.** What a request can set needs no constant, and no app used `LayoutSSR`.

**Instead.** `c.SetBodyLimit` and `c.SetReadTimeout` in `OnRequest` ([Body limit and read time of a route](../pages-and-data/handlers.md#body-limit-and-read-time-of-a-route)), and `const SSR = false` on each page. `SSR`, `Cache` and `Memo` stay, because the build needs them.

### Loading page data again after a write

**Turned down (2026-11-03).** After a method handler answers, loading the data of the page and its Layouts again without being asked, as Remix and React Router do.

**Why not.** Tifa does not know what a write changed, and every request sent on a guess is load on the server. TanStack Router, which Tifa follows in the browser, has no such step.

**Instead.** The page calls `router.invalidate()` when it should show what the write changed: [Calling method handlers from the browser](../pages-and-data/handlers.md#calling-method-handlers-from-the-browser).

A redirect from the handler to the address the page already has does load it again (2026-11-27): the server asked for that page, a form without script would load it, and TanStack Router loads a navigation to the same address too. Tifa still loads nothing it was not asked for.

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

**Instead.** The `json` tag names a field everywhere: in JSON, in a form, in the query string and in TypeScript. A `form` tag that names a field otherwise than its `json` tag fails the build (2026-11-26: one with the same name is let through, since a type shared with gin or gorilla/schema is right and Tifa reads it the same).

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

**Instead.** `Load(c, q Filter)` and `Layout(c, q Filter)`: the parameters are the fields of the struct in the signature, which the build reads without following anything, and the runtime gives `Load` the declared ones only. The types, the loading again on a change, the render URL, the memo key and the canonical link all come from the same fields. `c.Query`, `c.QueryValues` and `c.BindQuery` are for method handlers (and `Validate`, `OnRequest` for the first two), and a call in `Load` or `Layout` that the build sees fails it. One it does not see reads the copy of the request the function gets, which holds the declared parameters only (see [The raw request in a Load](#the-raw-request-in-a-load)).

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

### Reading a query value that is not valid as not passed

**Turned down (2026-11-26).** Reading `?page=abc` for a whole number as if `page` were not there, and dropping it from the address, as TanStack suggests with `.catch()` or `fallback()` on each field. It came up when an agent that had not seen Tifa built a filtered list, read that such a value answers 400, and declared every field a `string` to check by hand.

**Why not.** A typed field is there to be checked: a value of the wrong type is a wrong address, as a path with no page is, and the page should say so rather than show something the visitor did not ask for. Read as not passed, a typo in a link or a form goes unseen.

**Instead.** It stays a 400 before `Load` runs, in Go and in the browser alike, and the `ErrorBoundary` of the page decides what the visitor sees. The docs on query parameters say not to declare the field a `string` to get around it.

**Reopen when** apps are seen working around the 400 even with those docs, or a field needs a default for values that are not valid.

### A float32 field in a query struct

**Turned down (2026-11-26).** Accepting a `float32` field, or a slice of them, in the struct a `Load` or `Layout` takes as its second parameter, as other Go frameworks do. gin, echo, gorilla/schema, go-playground/form and grpc-gateway parse such a field with `strconv.ParseFloat(value, 32)` and keep the rounded value, huma marks it `format: float` in OpenAPI, and tools that write TypeScript from Go map `float32` and `float64` both to `number`, so with them Go and the browser can hold different numbers for the same address.

**Why not.** The typed query exists so that Go and the browser read every query value as the same number, and `float32` would be the one silent exception. A browser has only 64 bit numbers and no way to write the shortest text of a 32 bit float: `?price=1234567.89` is 1234567.9 in a float32 and the page would show 1234567.89, and `String(Math.fround(0.1))` is `0.10000000149011612`, where Go writes `0.1`. Handler inputs are not part of this, since they have no address to keep.

**Instead.** `float64`, and the build says so.

**Reopen when** apps are seen to need `float32` in the query of a page for a reason `float64` does not serve.

### A pointer field in a query struct

**Turned down (2026-11-26).** Accepting `*int` and other pointer fields in the struct a `Load` or `Layout` takes, so that a parameter not passed is `nil` and `?page=0` can be told from no `page`.

**Why not.** It adds a second way to write every field for a need that is rare: in Go the zero value is the default, and a page number or a price limit of 0 reads well as "the first page" or "no limit". A choice that needs its own on and off is clearer as a `bool` field of its own.

**Instead.** Value fields only, and the docs on query parameters say to let the zero value be the default.

**Reopen when** apps are seen needing to tell 0 from no value in a way a `bool` field does not serve.

### A build warning for a link built on prev

**Turned down (2026-11-26).** `tifa build` warning of `search={(prev) => ({ ...prev, ... })}` or `search` alone on a `Link`, which reads the whole query while rendering.

**Why not.** Spreading `prev` is how TanStack teaches links that change one parameter, and the navigation docs show it. The link differs from the server's only on an address with a parameter no `Load` reads, such as `utm_source`, and a production build keeps the server's `href` and reports nothing. A warning on every such link would be noise for a rare case that does no harm.

**Instead.** The router writes every address with its parameters in the order of their names, so the order never makes a difference, and the docs on the memo say how to name the keys a link keeps when the `href` must match: [Query parameters](../reference/memo.md#query-parameters).

**Reopen when** the differing `href` causes real problems in apps, such as a crawler or a click that should not carry a tracking parameter.

### The raw request in a Load

**Settled (2026-11-26).** `c.Request()` and `c.Writer()` work in a `Load` and a `Layout`. There `c.Request()` gives a copy of the request whose URL holds only the query parameters that function declares, spelled as the memo key spells them, with no body, and a `Layout` sees only the path parameters of its own folder: the others are `""`, in the copy and from `c.Param`. `URL.Path` stays the path of the page, which goes stale in a `Layout` as `c.Path()` does. Each run of a function gets its own copy, made on its first call. `c.Writer()` is the writer of the response, as everywhere.

**Why.** They were refused there (2026-10-14) because the build found what a `Load` read by reading its code, and a read through the raw request hid it. The query parameters now come from the declared struct and the path parameters from the folder, so that reason is gone. What is left is a part of the address the browser does not load the function again for, which would show old data. The copy leaves it out, so such a read is always empty and shows the first time it runs. A method that the package exports and then refuses was a rule to learn that kept nothing safe, and a library that takes an `*http.Request` works. Changing the request, which the `Load`s of a page share while they run at once, or writing the response from a `Load`, breaks the page. `net/http` only documents the first for its handlers ("Except for reading the body, handlers should not modify the provided Request"), and Tifa does the same for both.

**Instead.** The docs on loading data say it plainly, and that logging, counting and anything that needs the whole request belong in `OnRequest`, which sees it as it came. `c.JSON`, `c.Text` and `c.NoContent`, which write the response too, fail the build where it sees them in a `Load` or `Layout`, and return an error without writing where it does not. The generated app gives the server each `Load` and `Layout` function (`Func`), and the server keeps what each sees of the request by the code address of the function: one map lookup when a function starts, and the copy only when it is asked for.

### Reading the visitor on a page with Cache

**Settled (2026-11-26).** `c.Cookie`, `c.Header` and `tifa.Session` are not refused at run time in a `Load` or `Layout` of a page with `Cache`: they give what an anonymous visitor has. The build reports such a read only in code that runs for pages with `Cache` alone, such as the `Load` of the page, and no longer in a `Layout` shared with pages without it.

**Why.** The request is made anonymous before `OnRequest`, so a read cannot leak, and the anonymous answer is the right one for a page that is the same for everyone: a `Layout` that shows a sign-in link to a visitor it does not know shows it there. Next.js refuses `cookies()` in `use cache` because it does not make the request anonymous, and the error is what keeps one visitor's data out of the shared cache. Tifa does not have that reason. A 500 would take down every page with `Cache` under such a `Layout` for a read whose answer was right. In code that only pages with `Cache` run, the read is always empty, which is surely a mistake, so the build reports it there.

### Validate on the query struct of a Load

**Settled (2026-11-26).** A `Validate` method on the struct a `Load` or `Layout` takes runs after the query is decoded and before the function, and its error is the error of that function: a `tifa.Error` or `tifa.Fields` as it is, any other error through `OnError` as a 500. It used to fail the build. It has the signature of the `Validate` of a handler input, and a `Validate(c *tifa.Ctx)` runs with the Ctx of the function, so the build checks it as a function that `Load` or `Layout` calls.

**Why.** `Validate` says when a value of the type is valid, which does not depend on where it is decoded, so a page and a handler can share a filter type. The browser does not run it: a value it refuses reaches the `ErrorBoundary` as the 400 of the data request, the same as a `Load` that returns one.

### Rules on where a Ctx may go

**Turned down (2026-11-26), and taken out.** A list of the places a value that holds a Ctx may stand in a `Load`, a `Layout`, a `Validate` and the functions they call (the receiver of a method, an argument of a function of the module, a field of a struct literal, a new local variable), with a build error for a `go` statement, an interface method, a function value and a function outside the module that gets it, for a name of `c.Param` that is not a constant, and for `c.Param` or `tifa.Use` taken as a value. They were in place from 2026-10-14.

**Why not.** They were there so that the build could find every path parameter a function read and every `tifa.Use` by reading the code. The path parameters that remount a page now come from its folder and the query parameters from the struct in the signature, so nothing the build writes depends on finding them all. What was left refused code that is right in any Go program: `errgroup`, a Ctx in an interface or a struct, a generic function, a library that takes the Ctx. gin, echo, Next.js and TanStack Router have no such rule.

**Instead.** The build follows the Ctx where it is passed to a function of the module, as an argument or as the receiver of a method, and reports the mistakes it sees there. Where it cannot see which function runs, it stops without a word. A name of `c.Param` that is a constant is still checked against the path of the folder, in a method handler as well (2026-11-26).

### Runtime guards for Ctx in Load and Layout

**Turned down (2026-11-26).** Checks at run time for what the build cannot see once the rules above are gone: a table of every `Load` and `Layout` by code address to check each call against, the identity of the Ctx kept for those checks, and a panic or an error for `c.Path()` in a `Layout`, a path parameter below its folder, a `tifa.Use` of a Layout that does not wrap the page, a call in `Validate` that changes the response, and a write after the function returned.

**Why not.** The build reports what it can see, and a guard at run time does not catch the rest either: a Ctx handed to a library, or the raw request, goes past it. A panic in a goroutine the app started has no `recover` above it and ends the process. The table and the field are a cost every request pays for mistakes of the app, not for a promise of Tifa ([How ideas are weighed](#how-ideas-are-weighed)).

**Instead.** The build reports what it sees, and the rest is the app's to get right, as in an `http.Handler`: the docs on loading data say not to keep the Ctx after the function returns, and to wait for a goroutine of its own before returning. At run time a `Load` that passes itself to `tifa.Use` gets an error, since it would wait for ever, and `c.JSON`, `c.Text` and `c.NoContent` in a `Load` return one. What a function sees of the request is the copy of [The raw request in a Load](#the-raw-request-in-a-load), which checks nothing: it only holds less.

### The path of the page in a Layout

**Settled (2026-11-26).** `c.Path()` in a `Layout` function, or in a function it calls, fails the build, and the message says to read the address in `layout.tsx` with `useRouterState({ select: (s) => s.location.pathname })`. Nothing checks it at run time.

**Why.**
- The data of a Layout is not loaded again when the visitor moves between the pages below it, so a path it read would stay the one of the first page. Next.js gives a layout no pathname for the same reason, and its breadcrumbs use a hook in the browser. TanStack Router and React Router read the address in components, with `useRouterState` and `useMatches`.
- Not as SvelteKit does it: there a `load` that reads `url.pathname` runs again on every move, a switch nobody sees in the code, and a Layout that keeps its data across its pages is lost.
- No `layoutPath`, the URL of the folder of the Layout: the folder and `c.Param` give it already, and it does not help breadcrumbs.

**Instead.** [Breadcrumbs and the current page](../pages-and-data/layouts.md#breadcrumbs-and-the-current-page).

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

### Commands of the app

**Settled (2026-11-27).** `func OnCommand(root *cobra.Command)` in `hooks.go`. The app adds plain Cobra commands to the root command of the program before the program reads its command line. A command gets the flags of the settings, the settings loaded as `serve` loads them, a context that ends on SIGINT and SIGTERM, and its error printed once with exit status 1. It runs no `OnStart` and no `OnShutdown`, and starts no server and no VMs: it opens what it needs, such as `OpenDB(DBPath)`. A name of the program (`serve`, `config`, `version`, `help`, `completion`) or a flag of the settings stops the program at start. `tifa deploy run` runs a command on the server as the service runs, and `tifatest.Command` runs one in a test.

**Why.** Not every command needs the database or the setup of serving, so a command comes before `OnStart` and not after it. Cobra is already a dependency of Tifa, and agents and Go developers know it, so subcommands, flags, help, required arguments and completion need no API of Tifa's.

**Turned down.**
- `commands.go` with typed functions: a new convention file, for what Cobra already does.
- `OnCommand(ctx, args []string)`: raw arguments, so every app writes its own parsing, help and errors.
- Running `OnStart` before a command: it brings the setup of serving and the background loops, such as jobs and the backup copy, that a command does not want. A second backup copy next to the one of the service breaks it.
- A wrapper of Tifa over Cobra: an API only Tifa has, which agents do not know.
- A separate migrate step in `tifa deploy`: migrations run in `OnStart` (see [Tifa has no database layer](#tifa-has-no-database-layer-migrations-are-app-code-that-uses-goose)).

**Instead.** [Commands of the app](../guides/commands.md).

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

**Instead.** The check is always on: [Cross-site writes](../build-and-deploy/headers-and-origins.md#cross-site-writes).

### A setting that turns Secure off

**Turned down (2026-11-18).** `session.secure`.

**Instead.** The cookie stays as it is, and the log gets one warning when `tifa.SetSession` answers a request that is neither local nor HTTPS. That is Tifa checking its own cookie, not the app's.

## Database

### Tifa has no database layer (migrations are app code that uses goose)

**Settled (2026-11-27).** Tifa ships no database package: no migration runner, no schema tool, no ORM. `tifa new` writes the database code into the app, as it writes `db.go`: `migrations/001_init.sql` in goose's format, `migrate` in `db.go`, which runs the files with github.com/pressly/goose/v3 (the Provider API, dialect `sqlite3`) at `OpenDB` and in the command `migrate` (see [When migrations run](#when-migrations-run-at-start-or-before-the-switch)), `migrate_test.go` (`TestSchema`, `TestDropsSayWhy`) and `schema.sql`, which `TestSchema` writes. Tifa keeps only what depends on how it deploys, and the docs say it: what `tifa deploy` needs from any migration tool ([Another migration library](../guides/database.md#another-migration-library)), the rules that keep a rollback working, and the SQLite table rebuild.

What `migrate` adds around goose, each a few lines: goose gets a `*sql.DB` of its own with one connection, closed before the app opens its pool, so a `-- +goose NO TRANSACTION` file that turns foreign keys off never hands that connection to the app. goose runs every statement of such a file on one `*sql.Conn` (the Provider API, v3.30.2), but returns that connection to the pool it was given. One `slog.Warn` when the database is at a version above the newest file, since goose skips such versions without a word. Without `db.backup`, a `VACUUM INTO` copy before files run on a database that ran some.

**Why goose.** Applying migrations is a library's job, not Tifa's. goose is MIT licensed, widely used (about 11.8k stars and 8.8k dependent modules), released every few weeks, works with any `database/sql` driver, links three small packages besides the standard library, builds with `CGO_ENABLED=0`, and starts on a database a newer release migrated. A Postgres app keeps the same code with another dialect.

**Turned down.** A runner of Tifa's own, as `tifa new` wrote it for a day (`migrate.go`, with SHA-256 records of each file): it was SQLite only, coding agents do not know it, and a bug in it lives on in the copy of every app, which only an "Upgrading:" note can reach. A declarative schema that a tool diffs into `ALTER` statements: it cannot tell a rename from a drop and an add, SQLite table rebuilds are where such tools lose rows, and an older release after a rollback would "fix" the newer tables. Go migrations: data work in Go goes in an app command or a job. A drop check in `tifa check`, and a CI step that fails when a merged migration changes: the app's `TestDropsSayWhy` covers the first.

**Instead.** [Change the tables](../guides/database.md#change-the-tables).

**Reopen when** goose stops being kept up, or a deploy need comes up that goose cannot meet.

### Rolling back the database with a release

**Turned down (2026-11-27).** Down files, or `tifa deploy rollback` (or the automatic rollback of a failed deploy) undoing the migrations of the release it leaves, or restoring the database from before them.

**Why not.** It would throw away what visitors wrote after the migration: a down step that drops a column drops what was written into it, and a restore loses every write since, on every table. A down file is also known only to the newer release, which is the one being thrown away.

**Instead.** The database stays where it is and older releases run on it. Each release must run on the tables of the next ones: add, then stop using, then drop in a later release. To undo data, restore from `db.backup` or from the copy `OpenDB` keeps: [Roll back](../build-and-deploy/deploy.md#roll-back).

### A program that finds migrations it does not have

**Settled (2026-11-27).** When the database is at a version above the newest file of the program (a newer release ran more files and was rolled back), goose skips those versions and `migrate` logs one `slog.Warn` line with the version of the database and the newest file, then starts. Flyway does the same by default.

**Why.** Refusing to start turns the automatic rollback of a failed deploy into an outage: the older release would refuse the database the newer one left. golang-migrate does refuse (`no migration found for version N`), which is why the docs name it. The rules above make the older release work on those tables.

### Checking that a migration did not change after it ran

**Turned down (2026-11-27).** A check at start that a file which ran is the same as when it ran, as the runner of Tifa's own did with SHA-256 records. goose keeps versions only.

**Why not.** It would mean code of Tifa's own around goose again, kept in every app's copy. The edit of a file that ran never reaches the databases that ran it, so the rule "never edit a file that ran anywhere but on your machine" stays, in the docs, the notes of the app and the skill.

**Reopen when** apps lose data or time to edited migrations, or goose gets such a check.

## Rendering, memo and Cache

### React Server Components

**Turned down (a hard constraint from the start, written down 2026-11-26).** Components that run only on the server, fetch their own data while they render and reach the browser as the RSC payload, as in the App Router of Next.js and the RSC mode of React Router.

**Why not.**
- Go loads all the data of a page before the render and hands it in, and the render does no application I/O. A component that waits on a database or an API while it renders would put that I/O into the JavaScript engine, where the pool of VMs and the render time limit assume there is none.
- The render memo keys a page by the data its `Load` functions returned. With data fetched inside components, the key could not tell when the HTML changes.
- RSC needs a second module graph for the server, the serializer of its payload with references to client components, and a streamed answer. Tifa renders complete HTML in one pass without streaming, so it would be a second way to render and a second protocol to keep safe. The security advisories of React in 2025 and 2026 are in RSC and Server Functions: decoding what a request sends to them is attack surface that a page render without them does not have.
- Data has one place: `Load` and `Layout` in `route.go`, with their types written to `route.gen.ts`.

**Instead.** `Load` and `Layout` in Go, and `Suspense` with a fetch in the browser for what only the browser can load. Code that must not reach the browser stays in Go. [Philosophy](../start/philosophy.md) gives the short version to app authors.

**Reopen when** apps show a measured cost that only server components remove, such as large browser bundles from components that only format data, and Go cannot take that work.

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

**Instead.** Keep `GOMAXPROCS` above `ssr.vms`. The measured cost of not doing so is in [Settings](../reference/settings.md) under `ssr.vms`.

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

### How the memo chooses what to drop

**Settled (2026-11-26).** The memo is a segmented LRU. A new page enters a probation list, and a second hit moves it to a protected list that holds up to 80% of `ssr.memo_size`. When protected is full, its least recently used page goes back to the head of probation, and pages are dropped only from the tail of probation. A page larger than the protected share stays in probation.

**Why.** With a plain LRU, a run of addresses that come once (a search term, a page number nobody asked before) pushes the home page and the popular products out, and every normal visitor then misses. A page seen once is the first to go now, and one that is asked for again and again stays.

**Not chosen: W-TinyLFU** (Caffeine, Ristretto). It needs one more structure for counting how often each key is seen. For a memo of pages, a segmented LRU is enough.

### A priority for the memo in route.go

**Turned down (2026-11-26).** A priority, or any value besides `false`, for `Memo` in `route.go`, to say which pages the memo should keep when it is full.

**Why not.** The pattern of requests already shows which pages are hot. A priority written by hand is guessed, and everyone writes high for their own pages. It is also one more switch to learn. Redis, Varnish, CDNs and Next.js `cacheLife` have no priority for each entry either.

**Instead.** `Memo` stays `false` or nothing, and the memo keeps what is asked for again and again (see the entry above).

**Reopen when** there is data that pages which cost a lot to render but are asked for rarely are pushed out again and again. Look first at weighing by render time and size, done by Tifa itself, and not at a setting the app has to fill in.

### Fewer page switches

**Kept as they are (2026-11-23).** Dropping one of `const SSR`, `const Cache` and `const Memo`, or moving them out of `route.go`.

**Why not.** The build needs each of them: `SSR` for what goes into the SSR bundle, `Cache` for the check that no `Load` of the page reads a cookie or the session, and `Memo` for the check in `tifa dev`. `LayoutSSR`, `BodyLimit` and `ReadTimeout` were removed in the same round, see [Route constants for what a request can set](#route-constants-for-what-a-request-can-set).

### A list of query parameters written by hand

**Turned down (2026-11-09).** A setting that lists the query parameters that count for the render memo, and making the first render in the browser use the reduced URL too.

**Why not.** The list exists already: the parameters that the `Load` and the Layouts of the page take. A second list would drift from the code. The second idea was built and dropped: nothing tells when an app's own Suspense boundaries are done, and it raced with early navigation and with history entries. It was built again on 2026-11-26, so that links built from the query of the page hydrate as the server wrote them: the router hydrated at the render URL and then moved to the address bar. It was dropped for the same reason, since it relied on React hydrating the dehydrated Suspense boundaries before the move, and for two more: an effect that ran on mount could not read a parameter of the address that no `Load` reads, and a write built on `prev` then wiped it, and a landing page with `utm_source` got a second router event, which counted the visit twice in analytics.

**Instead.** The memo key and the URL given to the render hold the path and those parameters, sorted by name, so `?x=random` makes no new entry. A 404 and a page whose `Load` failed are not kept: [Query parameters](../reference/memo.md#query-parameters). The router writes every query parameter in the order of its name, those no `Load` reads too, and the browser puts the address bar in that order before it takes the page over, so a link built from the query of the page has the same href on the server and in the browser whatever the order of the address. A parameter no `Load` reads still makes such a link differ, which the docs on that rule say how to avoid.

**Reopen when** that difference, on landing pages with a parameter no `Load` reads, causes problems in real apps.

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

### The flags of go build

**Settled (2026-11-27).** `tifa build` passes `-trimpath` and `-ldflags=-w` by default, and never `-s`. `build.debug_info: true` leaves out both, for Delve. The flags of `GOFLAGS` stay: a `-trimpath` there in any form, or a `-w` or `-s` in its `-ldflags`, decides instead. `tifa dev` passes neither, so its panics give full paths an editor can open.

**Why.** A program with no folders of the build machine shows no user name or home folder in its panics, and the same code builds the same program on another machine. `-s` would take the symbol table that `go tool nm` reads, for little size.

**Reopen when.** A tool an app needs reads the full paths of a built program.

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

### What the page prints on the server

**Settled (2026-11-26).** Under `tifa dev`, `console.log`, `info`, `debug` and `warn` of a server render go to the log of the app, with the URL of the page and the place of the call in the sources. A built program prints nothing. `error` and a failed `assert` throw in both, and `table`, `dir`, `group`, `groupCollapsed`, `groupEnd`, `time`, `timeEnd`, `timeLog`, `count`, `countReset` and `trace` exist in both and do nothing, except that `table`, `dir` and `group` print as `log` under `tifa dev`.

**Why.** A developer had no way to see what a component printed on the server, and a library that called `console.time` failed the render with a `TypeError`. Printing only changes the log, never whether a render works, so it does not break [Checks that only tifa dev runs](#checks-that-only-tifa-dev-runs): `tifa dev` and the built program succeed and fail together. The VM keeps the calls in a list and Go logs it after the render, so a render still does no I/O. It is not [Browser errors printed in the terminal](#browser-errors-printed-in-the-terminal): that was the browser console, this is the server's.

### A line for each request in `tifa dev`

**Settled (2026-11-26).** The app under `tifa dev` logs one line per request of a route: method, URL, status, time, route and what the render memo did (`memo hit`, `memo miss`, `memo shared`, `memo off`) or `data` for navigation data. Files of the bundle and `public/`, and the requests of `tifa dev` and Vite, get none. A built program has no request log, as before: the recipe in [Logs](../guides/logs.md#logging-each-request) stays the way to get one.

**Why.** A developer who asks "was that a hit?" or "what did the click ask for?" had to read response headers. The app writes the line itself, in the `serve` of a route, so `tifa dev` needs no second place to know the route, and no header is added: the memo word comes from `X-Tifa-Render`, which pages carry anyway.

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

**Instead.** A key in `tifa.yaml` that is no setting of Tifa fails `tifa check` and `tifa build`, and a flag nobody defined stops the program. A setting Tifa retires is deleted at once, with no warning for it (2026-11-26): what is left of it in a file is an unknown key like any other, and it fails. Other environment variables, and other keys in the file of `-c`, are left alone: [Configuration](../build-and-deploy/config.md).

### Copying every dot file of public/

**Turned down (2026-11-25).** A switch that copies every dot file of `public/`.

**Why not.** A `.env` or a `.git` that was left there by mistake would be published with it.

**Instead.** `build.public_dotfiles` names the dot files and folders to copy, and `tifa build` says which ones it left out: [Settings](../reference/settings.md).

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
- A page of an older build reloads once when it meets the new one: [Pages open while you deploy](../build-and-deploy/deploy-by-hand.md#pages-open-while-you-deploy). Next.js self-hosted, SvelteKit and Nuxt do the same.
- Old HTML that a CDN still holds is taken away by clearing the CDN cache on deploy, when the app gives a token: [Clearing the CDN cache](../build-and-deploy/deploy-cloudflare.md#clearing-the-cdn-cache). Without one, the old copy stays until it runs out, which is the accepted cost of a CDN.

**Reopen when** old HTML in a CDN still breaks pages in practice although the cache is cleared on deploy, or an app needs open pages to keep working through a deploy without a reload.

### A reload that skips the CDN

**Turned down (2026-11-21).** When a page meets a new build, it would reload to its own URL plus a build ID, such as `?_tb=<build>`, so the CDN misses and the new page comes from the program. Clearing the cache would then only make new pages show up sooner.

**Why not.** The reload is code in the page's scripts, and the pages that need it most cannot run it. A visitor who opens an old page from the CDN gets HTML whose scripts the new build no longer has: no script runs, so nothing reloads. The idea only helps a tab that was already open and working, which the plain reload already handles once the CDN copy runs out. No framework or CDN removes this gap entirely: Vercel, Netlify and Cloudflare Workers route old pages to old builds only on their own platforms and only for a limited time.

**Instead.** Clear the CDN cache on deploy when the app gives a token. Without one, a page with `Cache` can be up to its `Cache` time old after a deploy, an accepted cost: [Clearing the CDN cache](../build-and-deploy/deploy-cloudflare.md#clearing-the-cdn-cache).

**Reopen when** a page can recover even when its entry script is gone.

### Clearing the CDN cache from the running program

**Turned down (2026-11-23).** The new build clearing the CDN cache when it starts.

**Why not.** Clearing the cache is a step of a deploy. The program that serves visitors does not do it.

**Instead.** `tifa deploy` clears it after a deploy. The token stays on the server and is read over ssh at that moment: [Clearing the CDN cache](../build-and-deploy/deploy-cloudflare.md#clearing-the-cdn-cache).

### Hints that tell the user to run gh

**Turned down (2026-11-21).** A message of `tifa deploy` that says to run `gh secret set` or another tool by hand.

**Why not.** `tifa deploy` is there to make deploying simple, so what it needs it asks for with its own command, also when that command runs `gh` underneath.

**Instead.** A `tifa deploy setup` flag for each thing to add or replace, such as `--cache-purge-token`. Only what a vendor's own page must do, such as making an API token, points there.

### The server in tifa.yaml

**Turned down (2026-11-21).** `deploy.host`, also with a warning when it is there.

**Why not.** `tifa.yaml` is in git and the address of a server should not be. Nobody reads a warning, and a commit does not pass through setup.

**Instead.** `--host`, or `TIFA_DEPLOY_HOST` in the environment or `.env`, or the one server that `~/.tifa/config.yaml` knows, a file that holds no secret: [Deploy](../build-and-deploy/deploy.md).

### The user tifa deploy logs in as

**Settled (2026-11-27).** The host may name an admin user, as `ubuntu@203.0.113.7` or `ssh://ubuntu@203.0.113.7:2222`. The steps that need root log in as that user and run through `sudo -n`, which needs passwordless sudo. Setup checks it at its first look at the server and stops before it changes anything when it is missing. The steps of the apps always log in as `tifa`, whatever user the host names, and setup gives `tifa` the keys of the admin user. With no user, or `root@`, root logs in as before.

**Why.** The images of AWS, Google Cloud and Azure do not let root log in, and their default user has passwordless sudo. Telling people to turn the root login on would make their servers less safe to use Tifa. Keeping `tifa` for the apps means a deploy never needs more than the user that owns them.

**Turned down.** Asking for a sudo password: a deploy runs without a terminal, and a coding agent must never get the password.

### Who writes the servers of ~/.tifa

**Settled (2026-11-27).** A `tifa deploy` whose release went live and passed its health check remembers the server in `~/.tifa/config.yaml`, as setup does: the machine ID the deploy read from the server, the host as named (with its admin user), and the app. It prints one line when the file changed, and a file it cannot write is a warning, since the release is live. In GitHub Actions it writes nothing, because CI never reads the file.

**Why.** Only setup wrote the file, so a server set up before the file existed, or from another machine or CI, never got an entry, and `status --all` and the default server did not know it. A separate command to add it would be one more thing to learn.

### Several targets, and the options of GitHub Actions in tifa.yaml

**Turned down (2026-11-20 to 2026-11-23).** Environments such as staging and production in `tifa.yaml`, and keys for the runner, the Node version, approvals, timeouts or path filters.

**Why not.** `tifa.yaml` holds only what Tifa needs to decide something.

**Instead.** One target, described by `deploy.front`, `deploy.connect` and `deploy.auto`. Anything else about the workflow is an edit of `tifa-deploy.yml`.

### How long a new release may take to pass its check

**Settled (2026-11-27).** `deploy.start_timeout` in `tifa.yaml`: a Go duration in whole seconds from `1s` to `10m`, `30s` when left out. It is how long the server asks for `/healthz`, about once a second, before a release counts as failed: a deadline, and each request waits at most the time that is left, so a socket that holds the connection cannot stretch it. The check of the release a failed deploy goes back to, and of a `deploy rollback`, takes the same time, since all three run the same check function on the server. It replaced the fixed 30 tries. The same day the idea had been turned down in the Database area (a start that takes longer keeps visitors waiting): the user settled it the other way, with the cost written in the docs.

**Why.** An app whose `OnStart` runs a long migration cannot deploy in 30 seconds, and the only way out was a hand-run statement on the server. Kamal has `deploy_timeout` for the same reason. The name follows `server.read_timeout` and `ssr.wait_timeout`: what is waited for, then `_timeout`. `deploy.deploy_timeout` would say deploy twice, and `health_timeout` names the check, while the person waits for the start. The upper bound of 10 minutes is there because the wait is a stall of the whole site: visitors' requests sit on the systemd socket while the new release starts, so a longer wait is a longer outage, not a slower deploy.

**Cost, as the docs say it.** `tifa deploy` stops the old program and starts the new one behind the socket. While the release starts, `OnStart` and its migrations included, requests wait. A migration that cannot finish in the time fails the deploy: nothing changes and the old release keeps running. The answer is to raise the setting or make the slow work smaller: [A slow start](../build-and-deploy/deploy.md#a-slow-start).

### Commands before and after a deploy in tifa.yaml

**Turned down (2026-11-27).** `deploy.before` and `deploy.after`, shell lines that `tifa deploy` runs around a deploy, as Kamal's `.kamal/hooks` do.

**Why not.**
- The same line must run in `sh` and in PowerShell, since `tifa deploy` runs natively on Windows. One string cannot be both.
- It is a second place for build and test scripts, next to CI and the `Makefile` or `package.json`.
- A local deploy and CI have different secrets, so a hook that works on one machine fails on the other.
- An `after` hook that fails after the site went live has no clear meaning: the deploy worked, and the command exits with an error.
- Chaining in the shell, or steps in CI before and after the deploy step, already does it.

**Instead.** [Before and after a deploy](../build-and-deploy/deploy.md#before-and-after-a-deploy).

### When migrations run: at start, or before the switch

**Settled (2026-11-27).** Migrations run in `OnStart` by default (A): `OpenDB` runs them in the new release, after `tifa deploy` stopped the old one, while requests wait on the socket. `deploy.release_command` in `tifa.yaml` is the other way (B): a command of the app, with its arguments, that `tifa deploy` runs once per deploy with the program of the new release, after the upload and before any switch, while the old release serves. `tifa new` writes the command `migrate` in `OnCommand`, which runs the same `migrate` of `db.go` as `OpenDB`, so B is one line, `release_command: migrate`, commented out in the `tifa.yaml` of a new app next to `start_timeout`.

- The command runs as `tifa deploy run` runs one: as `tifa`, in the folder of the service, with its `.env` and the HTTPS variables, with the program of the new release in place of `current`. Its output is part of the deploy's.
- A status other than 0 stops the deploy: no switch, the old release untouched, and the release is written down as `failed-release-command` in `deploys.log`, so a rollback cannot go to it, as with `failed`. The command takes the lock of the deploys of the app.
- It has no time limit of its own. `start_timeout` covers only the start after the switch.
- A rollback, by hand or after a failed check, never runs it.
- It is a step of its own, apart from the steps of the server (upload, switch, health check, rollback), and is defined as "once per deploy, before any switch", not "on each server": a deploy to several servers would run it once.
- The words of the setting are split at spaces and each goes to the program as written. Quotes, a first word that is a flag, and `serve` are refused at load.

**Why both.** A pauses the site for the migration, and no request is lost: right for most migrations, and it needs nothing set. B keeps the old release serving during a long migration, such as an index on a large table, and keeps a failed migration from ever touching the running release. On SQLite B keeps reads going but not writes: a migration holds the write lock, so the old release's writes wait `busy_timeout` and then fail. Both need each release to run on the tables of the next, and neither rolls the database back. B also fits apps that later run several copies on Postgres, where the migration runs once before the copies roll out.

**What Tifa promises.** A short pause per deploy and no lost requests. No pause at all would need the old and the new program serving at the same time, which `tifa deploy` does not do.

**The name.** `release_command` is the name Fly.io (`release_command`) and Heroku (the release phase) use for it, so people and agents who know them read it right away. The command runs with the new release, and a release is what Tifa calls a folder of the server, so the name says what runs and with what. `before_start` was the first choice and was turned down the same day as ambiguous: "start" could mean `OnStart`, the start of the service, or the start of the deploy. `pre_deploy` (Render, Railway) would say deploy twice under `deploy`, as `deploy_timeout` would have.

**Turned down.** Running B by default: A needs no setting and has no write gap on SQLite. Shell lines: see [Commands before and after a deploy in tifa.yaml](#commands-before-and-after-a-deploy-in-tifayaml).

### Tifa downloading programs, wrangler, and a front door of its own

**Turned down (2026-11-20 to 2026-11-22).**

- Downloading `gh` or `cf` by itself. Setup installs them only with a package manager the machine already has, only when `tifa.yaml` needs them, and as a line of the plan the user agrees to.
- wrangler, which `cf` replaces.
- A reverse proxy of Tifa's own for several apps on one server, or in front of several servers. Tifa ships none. A Cloudflare Tunnel or `front: nginx` shares a server: [An nginx of Tifa for several apps on one server](#an-nginx-of-tifa-for-several-apps-on-one-server).
- A local `tifa deploy` that hands over to GitHub Actions by itself, which would change what a command does without a word.
- A GitHub App in place of the `gh` sign-in. Put off as too early.

### An nginx of Tifa for several apps on one server

**Settled (2026-11-26).** `deploy.front: nginx` is a third front, next to `none` and `cloudflare`. Several apps on one server share ports 80 and 443 without Cloudflare.

- The nginx is Tifa's. Setup installs it with apt, as a line of the plan the user agrees to, and marks it with `/etc/tifa/nginx`. From then on Tifa writes all of its configuration, one site for each app, and nothing else uses it.
- An nginx that Tifa did not install is never touched. Setup stops, names it and its port, and changes nothing, as it does for any other program on port 80 or 443.
- Certificates come from certbot with its nginx plugin, installed with apt as a line of the plan and renewed by its own timer. An app without a domain is served over plain HTTP on port 80, and only one app of a server can be that.
- Remove takes the site and certificate of an app, and nginx and certbot with the last app, as it does for the tunnel.
- The site sets `X-Forwarded-For` to `$remote_addr`, the address of the visitor, and does not append to what the visitor sent, so an app that reads it behind this nginx gets an address nobody can make up. Tifa itself still does not read the header.

**Why.** Tifa cannot manage a configuration it does not own. A file it writes into an nginx of the user can clash with what is already there, and it cannot know what else changes that nginx later. In an nginx of its own it knows every line, can check each change with `nginx -t` before the reload, and can put the last good file back.

**Turned down.** Plugging into an nginx the user already has (2026-11-26): writing sites into it and reloading it. The same reason, and an app of Tifa would then depend on a server that other people also edit.

**Reopen when** many users ask to run Tifa apps behind an nginx they cannot give up, and a way is found that leaves its configuration to the user.

### Several servers for one app

**Settled (2026-11-27).** One app can run on several servers, behind a load balancer of the user: `deploy.front: external` (named `loadbalancer` until the same day, see [Something of the user's in front](#something-of-the-users-in-front)). The servers are a list in `TIFA_DEPLOY_HOST` (or `--host` given again, or with commas), and one server is the same as before. One process per machine.

- Setup sets up each server in turn, the first first, gives the app one port that is free on all of them, and lets it listen on every address of the server, not only the loopback. GitHub Actions gets every server in one round after them: one key on each, the host keys of each, and the list in the secret `TIFA_DEPLOY_HOST`. `~/.tifa/config.yaml` keeps the list with each server.
- A deploy builds once, uploads to every server, runs `deploy.release_command` once on the first, then switches one server at a time, each once the one before passed its health check. When one fails, it goes back by itself, and the servers before it are moved back, so all of them run one release. `rollback`, `status`, `logs`, `env` and `remove` work on every server, `run` on the first unless `--host` names another.
- Setup prints the targets, the port, `GET /healthz` and the statuses that pass, for the user to enter in the balancer. It opens the port in no firewall, and says to let only the balancer reach it.
- The app's storage is the app's choice. Tifa checks nothing about it, and the docs say what works: a database all servers reach, a session store of their own, and no SQLite, which is one file on one machine.

**Why.** One VPS is where an app starts, not a limit. A load balancer is what every provider sells for this, and the user knows the network between its servers.

**Turned down (2026-11-27).**
- `front: none` on several servers: each server would serve its own HTTPS with its own certificate, with nothing to spread the load.
- Tifa setting up the load balancer, or calling any cloud API: Tifa calls no cloud API, and the user sets up the balancer. Every provider has its own, and they change.
- A balancer node of Tifa's own in front of the servers: a reverse proxy of Tifa's own, turned down above in [Tifa downloading programs, wrangler, and a front door of its own](#tifa-downloading-programs-wrangler-and-a-front-door-of-its-own).
- An nginx entry server for several servers, with `front: nginx`: the network between the servers (their addresses, private networks, firewalls) is the user's and varies too much for deploy to set up safely. **Reopen when** deploy manages a private network between servers.
- Several servers behind a Cloudflare Tunnel, with `front: cloudflare`: a tunnel run on several machines has a replica on each, and Cloudflare sends each request to the replica closest to it, trying another only when that one fails, so the servers do not share the load ([Tunnel availability](https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/configure-tunnels/tunnel-availability/)). `none`, `nginx` and `cloudflare` with several servers are refused when the command reads its settings, and the error names `external`.

### Something of the user's in front

**Settled (2026-11-27).** `deploy.front: external` says that something of the user's stands in front of the servers: a load balancer at the provider, the user's own Cloudflare, or a proxy of their own. Tifa only deploys. The app serves plain HTTP on its port on every address of the server, with no certificate and no redirect to HTTPS, on one server or several. It replaced `loadbalancer` before any release, with no alias.

`deploy.port` (settled the same day) sets the port of the app with `front: external` only, from 80 to 65535, and any other front with it fails with the reason. Without it setup picks the first port free from 9527, as before. Setup fails when another app or program holds the port, and a port below 1024 reuses the sysctl of `front: none` (`net.ipv4.ip_unprivileged_port_start = 80`, removed with the last app that has a port below 1024). A port below 80 is refused: the sysctl opens ports from 80. Changing the port is a move in the setup plan: new socket, restart, health check. A deploy alone refuses to run on the old port.

**Why.** The same setting fits every front the user runs, and the old name said load balancer only, so a user with their own Cloudflare in Flexible mode, or their own proxy, would not have found it. Cloudflare proxies only some ports of the origin (HTTP 80, 8080, 8880, 2052, 2082, 2086, 2095, [network ports](https://developers.cloudflare.com/fundamentals/reference/network-ports/)), and none of them is in the range setup picks from.

### Your own Cloudflare in front

**Settled (2026-11-27).**
- The redirect from plain HTTP to HTTPS of an app that serves HTTPS itself stays on, with no setting to turn it off. Caddy, Kamal (kamal-proxy with `ssl: true`) and Rails (`config.force_ssl`, on in production by default) redirect the same way. A user who wants plain HTTP to reach the app, as Cloudflare in Flexible mode sends it, uses `front: external`.
- Flexible mode is `front: external` with `deploy.port` set to a port Cloudflare proxies for HTTP, such as 80 or 8080. Full (strict) is `front: none`, with Let's Encrypt or the user's own certificate. Flexible in front of `front: none` loops on the redirect, and the docs say so.
- `front: cloudflare` keeps the Cloudflare Tunnel.

**Turned down (2026-11-27).**
- A Cloudflare Load Balancer set up by Tifa for several servers. **Reopen when** users ask for balancing that Cloudflare manages.
- Tifa managing the DNS of the user's Cloudflare without the Tunnel, with proxied A records: more to manage (A records, certificates, the firewall of the origin) and no one asked. **Reopen when** users ask for it.

### Your own certificate with front none

**Settled (2026-11-27).** An app with `front: none` and a domain can serve HTTPS with a certificate of the user's, such as a Cloudflare Origin CA certificate. The files are named on the developer's machine by `TIFA_DEPLOY_TLS_CERT` and `TIFA_DEPLOY_TLS_KEY`, read like `TIFA_DEPLOY_HOST` (the environment, else the `.env` of the app), never in `tifa.yaml`. Both or neither, and only with `front: none` and a domain. Setup checks them on the machine first (PEM, the key matches, the certificate covers the host of `site.url`, not expired), uploads them to `/srv/tifa/<app>/shared/tls/` (owned by `tifa`, folder `0700`, files `0600`, each written beside its place and renamed), and the service gets `SERVER_TLS_CERT` and `SERVER_TLS_KEY` in place of `SERVER_TLS_DOMAINS` and `SERVER_TLS_CACHE`. The local files are the truth: other files on the server are replaced in the plan, which shows both, with no restart since the program reloads them. Without the inputs, setup goes back to Let's Encrypt and deletes `shared/tls`. Only setup uploads, never deploy. `server.tls_cert` in `tifa.yaml` stays a runtime setting, for the place the program runs, and setup says when the service overrides it.

**Why.** The certificate belongs to the developer's machine like the host, and a path on the server that Tifa owns is one setup can check, replace and delete. CI runners do not have the files, so a deploy must not need them. Writing the paths into `tifa.yaml` would commit a path of one machine, and reading the server's `.env` would leave setup unable to see or check the certificate.

### A secret that passes through the agent

**Turned down (2026-11-23).** Asking for a token in the chat, on stdin of a command the agent runs, or printing one.

**Why not.** Most of the time a coding agent runs `tifa deploy`, so a secret in the chat, on stdin or in the output would be in what the agent reads.

**Instead.** Tifa says what it will do and why, opens the exact page in the browser, the person approves there, and Tifa fetches the result itself. A token that must be typed goes into a local page that works once. Every command also runs without a terminal.

### Watching the servers

**Settled (2026-11-27).** `tifa deploy status --all` is a one-shot look at the servers of `~/.tifa/config.yaml`, with `--json` for scripts and agents: the system, memory, disk and load of each server, and each app's release, service, restarts, health and memory. It exits with an error when a server cannot be reached or a deployed app is not well.

**Turned down (2026-11-27).** A live dashboard, continuous uptime checks and alerts.

**Why not.** They need a service that runs all the time, on a machine that is up all the time, and a single program per app has neither. A probe on the server also dies with the server, so it cannot say the server is down.

**Instead.** An outside probe, such as UptimeRobot or the health checks of Cloudflare, for alerts. `status --all` for a look when you want one.

**Reopen when** apps on several servers need alerts that no outside probe gives, such as the restart count of a service.

### Hardening the server

**Settled (2026-11-27).** `tifa deploy harden` is a command of its own, per server and not per app, that is run once and touches ssh. It makes ssh take keys only, turns on ufw when it is off or not installed, and makes sure security updates install by themselves. It changes Debian and Ubuntu (apt) only, and only after the plan is agreed to.

- ssh: a file `/etc/ssh/sshd_config.d/00-tifa-harden.conf` with `PasswordAuthentication no` and `KbdInteractiveAuthentication no`, and `PermitRootLogin prohibit-password` only when `sshd -T` says `yes`. The name starts with `00` because sshd keeps the first value it reads for a keyword and reads the files of that folder in the order of their names: cloud-init's `50-cloud-init.conf` (`PasswordAuthentication yes` on many VPS images) would win over a later name. After `sshd -t` and the reload, a login from the machine of the developer with a new connection (no `ControlMaster`, no password prompt) must work. The session of the change stays open until that login answers, and undoes the change when it fails or when `tifa` dies.
- ufw: only when it is off or not installed, with the ports of ssh and of the apps of Tifa that serve visitors. A listener on a non-loopback address that is not sshd, a socket of Tifa or the nginx of Tifa stops it, and harden names the program and its port. It does not guess. A ufw that is on is reported and not changed. The ssh port is never closed or limited.
- Updates: `unattended-upgrades` installed and on. A reboot that waits is reported and never done.
- Record: `/etc/tifa/harden` holds what harden changed and the state from before. `--undo` puts back exactly that. `tifa deploy remove` does not undo it, since hardening belongs to the server.
- `tifa deploy setup` prints one line when the server accepts ssh passwords, and changes nothing else.

**Turned down.** fail2ban: harden only advises it, in one line, when it is absent. With key-only ssh a guessed password gets nobody in, so it only cuts the log noise. Kernel settings (sysctl, BBR), closing the ssh port and the firewall of the provider are not harden's either.

**Why.** Passwords and open ports are what a new VPS is attacked through, and a lockout is the one mistake that cannot be fixed from the server. So the change that could cause it has a way back that works without the developer.

**Reopen when** a way to reach the server that is not ssh with a key (a provider API, say) makes the new-login check wrong.

## Coding agents

### Changing behavior when an agent is detected

**Turned down (2026-11-17).** `tifa dev` noticing that a coding agent runs it and going to the background by itself.

**Why not.** A command that acts differently depending on who runs it is hard to predict, for agents and people alike.

**Instead.** An explicit flag: `tifa dev --background`, with `status`, `logs` and `stop` ([Run tifa dev from a script or a coding agent](../build-and-deploy/dev-scripts.md)).

### Agent hooks written by tifa new

**Turned down (2026-11-25).** `tifa new` writing a hook into the settings of a coding agent, such as `.claude/settings.json`, that runs `go tool tifa gen` after every edit of a Go file, so the types of `route.gen` are never old.

**Why not.** How an agent is set up is its user's business. Tifa gives an agent notes and a skill to read, and changes nothing about what the agent runs.

**Instead.** The notes and the skill say to run `go tool tifa gen` once after a batch of Go changes. The `typecheck` script of an app runs it before `tsc`, `go tool tifa check` writes `.tifa/` again before it runs `tsc`, and each `route.gen.ts` names the files it comes from: [`tifa gen`](../build-and-deploy/check.md#tifa-gen).

### Summaries of the docs in the AGENTS.md of an app

**Turned down (2026-11-25).** The docs index of the `AGENTS.md` that `tifa new` writes holding a line per doc with its whole summary, as it did for a day.

**Why not.** An agent reads the whole file at the start of every session, and the file was 3,780 tokens, of which the index was 1,150. The budget for the whole file is 4,000 tokens, and most sessions need two or three docs.

**Instead.** The index names the files, one line per section folder, and `go tool tifa docs` prints the summary of each doc. An agent runs that command anyway to learn where the folder is. A test keeps the whole file at 12,000 bytes, about 3,250 tokens: when a new note does not fit, another one is shortened or moved to the docs.

**Reopen when** agents that start from `tifa new` are seen to skip the docs they needed because the file names did not tell them.
