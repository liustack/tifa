---
summary: 'How to add CSS, Tailwind CSS and shadcn/ui to an app'
audience: app
order: 3
read_when:
  - Adding CSS, Tailwind CSS or shadcn/ui to an app
  - Putting a font or an image in a stylesheet, and whether it goes in `public/`
  - A font or an image that a stylesheet names is not found, or you want to know where Vite puts it
---

# Styles

Import CSS from a Layout or a page, such as `import './site.css'` in `app/layout.tsx`. Vite builds it: every page links one stylesheet with all the CSS of the app, and fonts and images that CSS references get a content hash in their names under `/assets/` (small ones go inline). A URL that starts with `/`, such as `/logo.svg`, is left as it is and names a file of the public folder. Use a relative URL such as `url(./font.woff2)` for what the page uses, and keep the public folder for files with a fixed name (see [Static files](../start/app.md#static-files)). Under `tifa dev` the CSS goes through Vite's dev server and updates in place, see [dev.md](../build-and-deploy/dev.md).

## Tailwind CSS

Use Tailwind CSS v4 through its Vite plugin, the same as in any Vite app. Install it:

```sh
pnpm add -D tailwindcss @tailwindcss/vite
```

add it to the plugins in `vite.config.ts`:

```ts
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import tifa from './.tifa/vite.js'

export default defineConfig({
  plugins: [tifa(), react(), tailwindcss()],
})
```

and import Tailwind in a stylesheet that a Layout imports:

```css
/* app/site.css */
@import "tailwindcss";
```

Tailwind finds class names by scanning the app root and skips the files `.gitignore` lists, which in a new app include `.tifa/` and `bin/`. Add other folders with `@source`. See `test/apps/tailwind` for a working app.

## shadcn/ui

With Tailwind set up as above, the shadcn CLI recognizes the app as a Vite app:

```sh
pnpm dlx shadcn@latest init
pnpm dlx shadcn@latest add button card
```

`init` installs the packages the components need (`radix-ui`, `class-variance-authority`, `lucide-react` and others), writes `components.json`, `lib/utils.ts` and the theme into the stylesheet that imports Tailwind. `add button` writes `components/ui/button.tsx` in the app root, and page code imports it from `@/components/ui/button`. Both work before the first build.

Then point the aliases of `components.json` into `components/`, so the app root keeps only `hooks.go`, `app/` and `components/`, and the folder of React hooks is not taken for `hooks.go`:

```json
"aliases": {
  "components": "@/components",
  "ui": "@/components/ui",
  "hooks": "@/components/hooks",
  "lib": "@/components/lib",
  "utils": "@/components/lib/utils"
}
```

`add` places files and rewrites their imports by these aliases: `add sidebar` writes `components/hooks/use-mobile.ts`. When `lib/utils.ts` only re-exports `cn` from the `cn` package, as with shadcn 4, delete it and import `cn` from `cn` in page code too, as the components do. Otherwise move it to `components/lib/utils.ts`.

`@/` is the app root, set by `paths` in the app's tsconfig (see [build.md](../build-and-deploy/build.md#the-apps-tsconfig-and-vite-config)). Components render on the server like any other React code.

