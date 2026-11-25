---
summary: 'How folders under app/ become URLs: parameters, overlaps, the files of a folder'
audience: app
order: 1
read_when:
  - Deciding which folder a route goes in, or the build rejects a folder name or two routes that overlap
  - Giving a route a parameter, or reading it in Go with `c.Param` and in TypeScript with `Link` and `params`
  - Deciding which convention files a folder needs, or whether a folder with only `route.go` is an API route
---

# Routing

## URLs come from folders

Every folder below `app/` is one segment of the URL. `app/` itself is `/`.

| Folder name | URL segment |
| --- | --- |
| `orders` | the literal `orders` |
| `order+` | the parameter `{order}`, read with `c.Param("order")` |
| `path++` | the rest of the path `{path...}`, only as the last folder. It may be empty: `files/path++` also answers `/files`. `c.Param("path")` reads it in Go, and its key in TypeScript `Params` is `_splat` |

- A literal folder name holds only ASCII letters, digits and `- . _ ~`. A parameter name starts with a letter or `_`, followed by letters, digits or `_`, and appears once per URL.
- The go command takes the package name from the `package` line, not from the folder. Name the package after the parameter: `project+` holds `package project`, and other packages import it as `project`.
- The go command skips folders that start with `.` or `_`, and folders named `testdata`. A `route.go`, `page.tsx`, `layout.tsx` or `layout_.tsx` inside one fails the build. Other files there, such as a `_components` folder, are fine.
- A folder name must also be valid in a Go import path, so names such as `con` or `x~1` (Windows reserved and short names) fail the build.
- A folder holds at most one parameter folder. `items/id+` next to `items/key+` or `items/path++` fails the build, because both would read the same URL segment.
- Two routes (pages or API routes) whose URLs overlap without one being more specific fail the build, as in Go's `http.ServeMux`, such as `a/b+` next to `a+/b`. A literal is more specific than a parameter, so `items/new` next to `items/id+` is fine.
- The URL of a folder with a Layout but no page shows a 404 inside that Layout (see [404](../pages-and-data/errors.md#404)), unless a `name++` folder right below it has a page, which answers that URL with an empty rest. No other page may match that URL unless it is more specific than the Layout's path: `settings/layout.tsx` without a `settings/page.tsx`, next to `user+/page.tsx`, fails the build, because `/settings` would also match `/{user}`, but `items/new` next to a Layout in `items/id+` is fine.
- Declaring `const Path` fails the build. The folder is the URL.

### Parameter names in Go and TypeScript

A parameter has one name in the folder and a spelling in each language. For `app/projects/project+` and `app/files/path++`:

| Folder | Go: `c.Pattern()` and `tifa routes` | TypeScript: `Link to` | TypeScript: key of `params` |
| --- | --- | --- | --- |
| `project+` | `/projects/{project}` | `/projects/$project` | `project` |
| `path++` | `/files/{path...}` | `/files/$` | `_splat` |

`c.Param("project")` and `c.Param("path")` read them in Go by the name of the folder.

## The files of a folder

A folder has at most three convention files:

| File | What it holds |
| --- | --- |
| `route.go` | Every Go name of the folder: `Layout`, `Load`, `SSR`, `Memo`, `Cache` and the method handlers |
| `page.tsx` | The page component. A folder with `page.tsx` is a page |
| `layout.tsx` | The Layout component |
| `layout_.tsx` | The Layout component in place of `layout.tsx`, shown without the Layouts above it (see [A Layout without the Layouts above](../pages-and-data/layouts.md#a-layout-without-the-layouts-above)) |

- The names of `route.go` are read from that file only. Declaring one of them in another file of the package, `layout.go` included, fails the build. Helpers and types may go in any file.
- A folder with only `route.go` is an API route when it declares a method handler, and gives the pages below it their Layout data when it declares `Layout`. It can do both. `Load` is only allowed next to a `page.tsx`.
- `tifa build` also writes a `route.gen.ts` module for each folder with a page, a Layout or a method handler, which `page.tsx`, `layout.tsx` and `layout_.tsx` import (see [page.tsx](../pages-and-data/loading-data.md#pagetsx)).
