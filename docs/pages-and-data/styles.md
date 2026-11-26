---
summary: 'Add CSS, Tailwind, shadcn/ui or another UI library'
audience: app
order: 8
read_when:
  - Adding CSS, Tailwind CSS or shadcn/ui to an app
  - Choosing a UI library, or one such as daisyUI, Base UI, React Aria, Mantine, Chakra UI or MUI shows no styles until the page has loaded
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

`@/` is the app root, set by `paths` in the app's tsconfig (see [Generated files and app setup](../reference/generated-files.md#the-apps-tsconfig-and-vite-config)). Components render on the server like any other React code.

## Other UI libraries

Tifa has no list of UI libraries it works with. A component library is React code that your Vite config bundles, the server renders and the browser hydrates. What decides whether the first paint has its styles is where the library gets them from:

| Where the styles come from | Libraries | What to do |
| --- | --- | --- |
| A CSS file or Tailwind classes | Tailwind CSS, daisyUI, shadcn/ui | Import the stylesheet as above |
| Your own CSS: the library has none | Radix, Base UI, React Aria Components | Nothing |
| A style tag written next to each component as it renders | MUI and Chakra UI, both on Emotion | Nothing: the tags are in the HTML of the server |
| A CSS file whose colors hang on an attribute of `<html>` | Mantine | Set the attribute, see [Mantine](#mantine) |
| Styles gathered during the render and put in the head afterwards | styled-components, Ant Design | Not supported, see [below](#styled-components-and-ant-design) |

daisyUI is a plugin of Tailwind, added in the stylesheet that imports Tailwind:

```css
@import "tailwindcss";
@plugin "daisyui";
```

A component that needs the browser while it renders goes in a `.client.tsx` file: [Components that draw only in the browser](server-render.md#components-that-draw-only-in-the-browser).

### Mantine

The colors in the stylesheet of Mantine are under `data-mantine-color-scheme` on `<html>`. Mantine sets it with a script in the head, and Tifa writes the head, so name it in `documentHead` of the root Layout:

```tsx
// app/layout.tsx
import '@mantine/core/styles.css'
import { MantineProvider } from '@mantine/core'
import { Outlet } from '@tanstack/react-router'

export const documentHead = { htmlAttributes: { 'data-mantine-color-scheme': 'light' } }

export default function Layout() {
  return (
    <MantineProvider defaultColorScheme="light">
      <Outlet />
    </MantineProvider>
  )
}
```

Without the attribute the components have no colors until the browser has run the scripts of the page.

### styled-components and Ant Design

Both keep the styles of a render in a sheet and expect the server to write the sheet into the head when the render is over. Tifa has no place for that and plans none, so with their usual setup the HTML of the server has no styles for their components: the page looks unstyled until the browser has run its scripts. For a new app, pick a library from the rows above.
