---
summary: 'The files `tifa build` writes to .tifa/, and the tsconfig.json and vite.config.ts an app needs'
audience: app
order: 8
read_when:
  - You cannot tell where a file under .tifa/ comes from
  - 'Setting up `vite.config.ts` or the `paths` of `tsconfig.json`, or asking why Tifa bundles with Vite'
---

# Generated files and app setup

What `tifa build` and `tifa dev` write into `.tifa/`, which names an app may rely on, and the Vite and TypeScript files the app keeps in its own root.

## Files in .tifa/

| File | Contents |
| --- | --- |
| `main.go` | Embeds the frontend output `dist/` with `go:embed`, sets it as the `Dist` of `tifaapp.App` and calls `server.Main` (`server.DevMain` under [`tifa dev`](../build-and-deploy/dev.md)) |
| `tifaapp/app.go` | The package `tifaapp` with one variable, `App`: the application as `tifa build` found it. It fills in only the hooks that actually exist in `hooks.go`, holds the runtime settings of `tifa.yaml` as Go code (`Settings`), imports the root package when its `options.go` defines settings or its `content.go` declares collections and there is no `hooks.go`, and registers the session type. Each `Load` and `Layout` function is wrapped as `func(c *tifa.Ctx) (any, error) { return tifa.Use(c, pkg.Load) }`, and `pkg.Layout` for a Layout, and is also given as `Func`, by which the server knows its runs and gives each the part of the request it may see. A method handler with an input or a result goes through `server.Input`, `server.Output` or `server.InputOutput`, which decode the input with `(*tifa.Ctx).Bind` or, for GET and DELETE, `(*tifa.Ctx).BindQuery`, run its `Validate` and write the result. `Fingerprint` is a hash of the file, which `tifatest.Start` checks against the code. Tests of the app import it as `<module>/.tifa/tifaapp` (see [Testing](../pages-and-data/testing-apps.md#testing)) |
| `routes.tsx` | The TanStack route tree and the `Register` type registration |
| `session.gen.ts` | `SessionData`, the JSON that the `Exposed` method of the session type returns (it is an alias of the Go struct that `Exposed` returns, which the file declares under its Go name), and `SessionEnabled` (`true` when the app calls `tifa.SetSession`). `SessionData` is `never` when the app has no session or its type has no `Exposed`. Pages import the type from `tifa/client` (see [Sessions](../pages-and-data/sessions.md)) |
| `i18n.gen.ts` | `i18n`, the languages of the app for the browser runtime, `undefined` when no folder has a `_locales` folder: the default language, whether `i18n.prefix` is `always`, the languages of the site, and `routes`, the languages of each route of the route tree that is in an area, by its TanStack route ID (a Layout, a page, and the route that shows the 404 of a Layout). A route outside every area is not in `routes`: it has no language and no prefix |
| `app/<folder>/route.gen.ts` | One module for each folder with a page, a Layout or a method handler. It exports `page` for the page and `layout` for the Layout (the TanStack route objects, only when the folder has them), `PageData` and `LayoutData` (the result types of `Load` and `Layout`), `Params`, and a function per method handler with its `<METHOD>Input` and `<METHOD>Output` types. It also declares each Go struct these types hold once, under its Go name (see [page.tsx](../pages-and-data/loading-data.md#pagetsx)), and the types above are aliases of those. The functions call `runtime/call.ts` |
| `tifa/` | The public entries of the app's `tifa/*` path: `client.ts` and, for an app with the i18next adapter, `i18n.ts`. Each re-exports a module of `runtime/`. Nothing else is in this folder |
| `server.ts`, `client.ts` | The SSR entry and the browser entry. Under `tifa dev`, the browser entry also imports `runtime/dev-client.js` |
| `tsconfig.json` | See below |
| `vite.js`, `vite.d.ts`, `vite/plugin.js` | The Vite plugins of Tifa for `vite.config.ts`, see below. `vite/plugin.js` is a copy of the repository's `js/vite/plugin.js` |
| `runtime/` | A copy of the repository's `js/runtime/`. `i18n.tsx` and `locales.d.ts`, the [i18next adapter](i18next.md#the-i18next-adapter), are there only for an app with a `_locales` folder |
| `content/` | For an app with content collections: `entries.json`, the entries `tifa build` hands the Vite plugin to turn into HTML (the Markdown with the front matter left as empty lines, where the body goes, and the folder of the version in the default language), and `headings.json`, the headings the plugin found in each body, which go into `dist/content.json`. See [Content](../pages-and-data/content.md#markdown-to-html) |
| `dist/` | Frontend output, see [Frontend output](build-output.md#frontend-output) |

An app without pages (no `page.tsx` at all) generates only Go files, and `dist/` holds only `build.json` and the public files. With content collections it also gets `.tifa/vite.js` for its `vite.config.ts`, and `dist/` the bodies and the files they refer to.

The names an app may refer to are these, and they stay: `.tifa/vite.js` (imported by `vite.config.ts`), `.tifa/tsconfig.json` (extended by `tsconfig.json`), the folder `.tifa/tifa/` with `client.ts` and, for an app with the i18next adapter, `i18n.ts` (the one `tifa/*` path of `tsconfig.json`, which makes `tifa/client` and `tifa/i18n`, and holds only these public entries), `./route.gen` imports, and the Go package `<module>/.tifa/tifaapp` (for tests). Everything else in `.tifa/` can change in any release.

## The app's tsconfig and Vite config

An app with pages has these two files in its root. `tifa new` writes them:

```json
{
  "extends": "./.tifa/tsconfig.json",
  "compilerOptions": {
    "paths": {
      "@/*": ["./*"],
      "tifa/*": ["./.tifa/tifa/*"]
    }
  },
  "include": ["**/*", ".tifa/**/*"]
}
```

```ts
// vite.config.ts
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tifa from './.tifa/vite.js'

export default defineConfig({
  plugins: [tifa(), react()],
})
```

- The generated tsconfig sets `rootDirs: ["..", "."]`: TypeScript treats `.tifa/app/x/route.gen.ts` as `app/x/route.gen.ts`, so `import { page } from './route.gen'` in your source resolves, and so does the module of a folder above, such as `'../../route.gen'` for the Layout data of that folder. The app's `include` names `.tifa/**/*` on its own, because TypeScript globs skip folders whose names start with a dot. The generated tsconfig also loads the types of `vite/client`, so imports of images and stylesheets and `import.meta.env` type-check.
- `paths` lives in the app's own tsconfig, so tools that read it from there, such as the shadcn CLI, see the app root. `@/` is the app root: `@/components/ui/button` is `components/ui/button.tsx`. `tifa/client` is the fixed module name page code uses to import `clientOnly`, `TifaError`, the session hooks and the `SessionData` type, and `tifa/i18n` the one of the i18next adapter. These two names, `tifa/client` and `tifa/i18n`, and `@tanstack/react-router` do not change. A module added to Tifa later gets its own entry under `.tifa/tifa/`, so `paths` never needs another line. Both work before the first build.
- Vite resolves imports by the same `paths` (its `resolve.tsconfigPaths`), so a path you add or change, such as `"@/components/*": ["./src/components/*"]`, applies to type checking and bundling alike. Vite applies `paths` only to the files that `include` matches, so keep `**/*` in it.
- `tifa()` from `.tifa/vite.js` returns the Vite plugins of Tifa, with `build.target` of the project config passed in. Its option `markdown` (`{ remarkPlugins, rehypePlugins }`) adds plugins of the app to the conversion of content collections, see [Content](../pages-and-data/content.md#markdown-to-html). They resolve `./route.gen` the same way TypeScript does: any relative import whose last segment is `route.gen`, with or without `.js` or `.ts`, and that points to a folder under `app/`, however many levels it goes through (`'../../page/route.gen'` works too), resolves to the matching file in `.tifa/`. If the folder has no generated module, the build fails. They also set the entries and outputs below, and build the SSR bundle.
- `@vitejs/plugin-react` compiles JSX and adds React Fast Refresh under `tifa dev`. Add other Vite plugins after these two, such as `@tailwindcss/vite` (see [styles.md](../pages-and-data/styles.md#tailwind-css)). Leave the entries, `base`, outputs and the `ssr` environment to Tifa.
- `vite.config.ts` imports `./.tifa/vite.js`, so it type-checks once Tifa has generated `.tifa/`. `tifa build` and `tifa dev` generate it before Vite reads the config.

The app's `package.json` needs React, React DOM and TanStack Router, and as dev dependencies `vite` and `@vitejs/plugin-react`. Type checking also needs `typescript`, `@types/react` and `@types/react-dom`. `tifa new` writes a `package.json` with these. For Tailwind CSS, see [Styles](../pages-and-data/styles.md).

TanStack Router must be the version this Tifa is tested with: the `@tanstack/react-router` that `tifa new` writes into `package.json`, with the `@tanstack/router-core` it installs. Tifa hands page data to the browser through an option of TanStack Router that is not documented outside TanStack Start (see [Data protocol](data-protocol.md#data-protocol)), and another version may stop reading it in the browser. Then every page with object data stays blank. So `tifa build` and `tifa dev` read the installed versions of both packages, the way Node resolves them, and stop with an error that names the installed and the tested versions. To move to a newer TanStack Router, upgrade Tifa. To build with another version anyway, at your own risk, set `build.skip_tanstack_check: true` in `tifa.yaml` (see [Settings](settings.md)). An app without pages is not checked.

React and React DOM must be 19.3.0 or later. The Vite plugin rewrites the string renderer of React DOM as 19.3 writes it (see [Frontend output](build-output.md#frontend-output)), so `tifa build` and `tifa dev` read the installed versions of `react` and `react-dom` the same way and stop with an error that names the version to install. A prerelease of 19.3.0, such as a canary, counts as older. An app without pages is not checked.

### Why Vite

Tifa bundles the front end with the app's own Vite because an app with pages is a React app, and a React app has Node for npm anyway. Most of the front end world, such as Tailwind CSS and the shadcn CLI, expects Node at build time. What Tifa keeps from Node is the running program: it never needs Node.

What the app gets:

- **Edits show at once.** Under `tifa dev`, React Fast Refresh swaps the edited components in place and keeps their state, and new CSS replaces the old, without a reload (see [dev.md](../build-and-deploy/dev.md#vite)).
- **Plugins it already knows.** Tailwind CSS is `@tailwindcss/vite`, and any other Vite plugin goes into `vite.config.ts` the way its own docs say. Tifa does not wrap them.
- **The setup of any Vite app.** `vite.config.ts`, the `paths` of `tsconfig.json` and the shadcn CLI work as their docs describe, with no Tifa-only form.
- **Faster renders without changing the packages.** The Tifa plugin rewrites React DOM, i18next and other packages as they are bundled, so the SSR bundle renders faster in beastjs while the app installs the packages as they are (see [Frontend output](build-output.md#frontend-output)).
- **Bundling Tifa does not maintain.** Splitting by route, the manifest, hashed file names and source maps come from Vite and Rolldown, which the React world keeps up to date.

What it costs:

- Building an app with pages needs Node, `vite` and `@vitejs/plugin-react`. Node must be 20.19, 22.12 or later, what Vite 8 needs: on an older one `tifa build`, `tifa dev`, `tifa gen` and `tifa check` stop with a line that names the version found. An app without pages builds without Node, unless its content collections have entries.
- `tifa dev` runs one Node process next to the app, with Vite's dev server behind the same address and the SSR bundle built on a worker thread of its own, since Vite picks the build of React once per thread.
- Some features read what Vite and Rolldown do inside, such as the module list behind `dist/inputs.json`, so a new Vite can need a new Tifa.
