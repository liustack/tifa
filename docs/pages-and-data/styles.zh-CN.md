---
summary: '怎样给应用加 CSS、Tailwind CSS 和 shadcn/ui'
audience: app
order: 8
translatedFrom: cec95094451b
read_when:
  - '给应用加 CSS、Tailwind CSS 或 shadcn/ui'
  - '在样式表里放字体或图片，以及它要不要放进 `public/`'
  - '样式表里写的字体或图片找不到，或者想知道 Vite 把它放在哪里'
---

# 样式 {#styles}

在布局或页面里 import CSS，例如在 `app/layout.tsx` 里写 `import './site.css'`。CSS 由 Vite 构建：每个页面都链接同一个样式表，里面是整个应用的 CSS。CSS 引用的字体和图片，名字里会带上内容哈希，放在 `/assets/` 下（小文件会内联）。以 `/` 开头的 URL（如 `/logo.svg`）保持原样，指向 public 文件夹里的文件。页面自己用的资源，请用相对 URL，如 `url(./font.woff2)`。public 文件夹留给名字固定的文件（见[静态文件](../start/app.zh-CN.md#static-files)）。在 `tifa dev` 下，CSS 经过 Vite 的开发服务器，原地更新，见 [dev.md](../build-and-deploy/dev.zh-CN.md)。

## Tailwind CSS {#tailwind-css}

通过 Vite 插件使用 Tailwind CSS v4，和任何 Vite 应用一样。先安装：

```sh
pnpm add -D tailwindcss @tailwindcss/vite
```

把它加进 `vite.config.ts` 的 plugins：

```ts
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import tifa from './.tifa/vite.js'

export default defineConfig({
  plugins: [tifa(), react(), tailwindcss()],
})
```

然后在某个布局 import 的样式表里引入 Tailwind：

```css
/* app/site.css */
@import "tailwindcss";
```

Tailwind 通过扫描应用根目录来找类名，并跳过 `.gitignore` 列出的文件，新应用里这些文件包括 `.tifa/` 和 `bin/`。其他文件夹用 `@source` 加进去。可运行的例子见 `test/apps/tailwind`。

## shadcn/ui {#shadcnui}

按上面的方式配好 Tailwind 后，shadcn CLI 会把这个应用识别为 Vite 应用：

```sh
pnpm dlx shadcn@latest init
pnpm dlx shadcn@latest add button card
```

`init` 会安装组件需要的包（`radix-ui`、`class-variance-authority`、`lucide-react` 等），写入 `components.json`、`lib/utils.ts`，并把主题写进引入 Tailwind 的样式表。`add button` 会在应用根目录写入 `components/ui/button.tsx`，页面代码从 `@/components/ui/button` import 它。这两步在第一次构建之前就能用。

接着把 `components.json` 的别名指到 `components/` 里，这样应用根目录只留 `hooks.go`、`app/` 和 `components/`，React hooks 的文件夹也不会被当成 `hooks.go`：

```json
"aliases": {
  "components": "@/components",
  "ui": "@/components/ui",
  "hooks": "@/components/hooks",
  "lib": "@/components/lib",
  "utils": "@/components/lib/utils"
}
```

`add` 按这些别名放置文件并改写其中的 import：`add sidebar` 会写入 `components/hooks/use-mobile.ts`。如果 `lib/utils.ts` 只是从 `cn` 包重新导出 `cn`（shadcn 4 就是这样），就删掉它，页面代码里也像组件那样直接从 `cn` import `cn`。否则把它移到 `components/lib/utils.ts`。

`@/` 是应用根目录，由应用 tsconfig 里的 `paths` 设置（见 [build.md](../build-and-deploy/build.zh-CN.md#the-apps-tsconfig-and-vite-config)）。组件和其他 React 代码一样在服务器上渲染。
