---
summary: 'What Tifa v0.1 keeps stable for apps (packages, file conventions, generated paths, commands, settings, HTTP names) and what may change in any release'
audience: app
read_when:
  - Deciding whether a Tifa upgrade can break your app
  - Putting a CDN, a proxy or monitoring in front of a Tifa program
  - Wondering whether you may import the `server` package or parse the JSON of `tifa routes` or `tifa errors`
  - Changing a public name of Tifa and needing to know if it needs a minor version
---

# Compatibility

From v0.1, a change that breaks anything on the stable list below ships in a minor version (`v0.1.x` to `v0.2.0`) and not in a patch. The changelog says what to change, and `go tool tifa upgrade` prints it. A name that is not on the list may change in any release.

## Stable

**Go packages.** The exported names of `github.com/liustack/tifa`, `github.com/liustack/tifa/content` and `github.com/liustack/tifa/tifatest`. That includes the names of the hooks in `hooks.go`, the signatures of `Load`, `Layout` and the method handlers in `route.go`, the constants `route.go` reads (`SSR`, `Memo`, `Cache`), the `Validate` and `Exposed` methods, the fields of `tifa.Stats()`, and the JSON rules in [routes.md](routes.md).

**File conventions.** The root files (`go.mod`, `tifa.yaml`, `hooks.go`, `setup.ts`, `options.go`, `content.go`, `content/`, `public/`, `app/`), the meaning of folder names in `app/` (`name`, `name+`, `name++`), the convention files in a folder (`route.go`, `page.tsx`, `layout.tsx`, `layout_.tsx`), what `page.tsx` and `layout.tsx` export, `_locales/<language>.json`, and `*.client.tsx`.

**Generated paths an app imports.** `.tifa/vite.js`, `.tifa/tsconfig.json`, `tifa/client` and `tifa/i18n` (mapped by the app's `tsconfig.json`), `./route.gen` and its exports, the `SessionData` type of `tifa/client`, and the Go package `<module>/.tifa/tifaapp` with its variable `App`.

**Commands and flags.** The commands of `go tool tifa` (`new`, `build`, `dev`, `gen`, `check`, `routes`, `errors`, `docs`, `agents`, `upgrade`) with their flags. The built program's `serve`, `config` and `version`, `-c/--config`, and one flag for every setting.

**`tifa.yaml` keys.** Every key in [config.md](config.md), with its flag and environment variable, its type and its default. A key Tifa drops is first listed as retired, and a retired key only prints a warning. The next minor version may remove it.

**HTTP names that apps and CDNs touch.**

- The query parameter `_tifa` of navigation data requests. A cache in front of the program must include it in the cache key.
- The path prefix `/__tifa/`, including `/__tifa/session`, which must not be cached.
- The request header `X-Tifa-Call` (`1` for a call from the browser runtime).
- The response header `X-Tifa-Build`, which comes back with a 409 when the browser and the program are of different builds.
- The response header `X-Tifa-Render` and its four values `vm`, `shared`, `memo` and `none`.
- The default of `session.cookie`, `__Host-tifa_session` (`tifa_session` under `tifa dev`).

## May change in any release

- `tifa deploy`, `tifa deploy setup`, `tifa deploy rollback` and `tifa deploy env` with their flags, the plan setup prints, the `deploy.*` keys of `tifa.yaml`, the files, units and records they keep on the server, the workflow `tifa deploy setup` writes (`.github/workflows/tifa-deploy.yml`), and what it sets in GitHub and Cloudflare. They are new, and may change while the deploys from CI and from a Cloudflare account settle.
- The `server` package. It is public only because generated code in your module imports it. Do not import it. Use `tifa` and `tifatest`.
- The JSON of `tifa routes --json` and `tifa errors --json`. Read them in scripts at your own risk, or use the text output.
- The other `X-Tifa-*` headers (`X-Tifa-Redirect`, `X-Tifa-Session`, `X-Tifa-Dev`, `X-Tifa-Reload`), the JSON envelopes between the browser runtime and the program, `Server-Timing` details, the browser's `localStorage` and `sessionStorage` keys, and the element ids of the page. A browser and a program of one build agree on them, and a program of another build answers 409 so the page loads again.
- Everything under `internal/`, the contents of `.tifa/` apart from the paths listed above, the bundled JavaScript, and the text of logs and error messages.
- Speed, memory use and the exact HTML bytes of a page.
