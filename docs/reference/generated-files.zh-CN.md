---
summary: '`tifa build` 写到 .tifa/ 里的文件，以及应用需要的 tsconfig.json 和 vite.config.ts'
audience: app
order: 7
translatedFrom: 70dd900192bb
read_when:
  - '说不清 .tifa/ 下某个文件是从哪里来的'
  - '设置 `vite.config.ts` 或 `tsconfig.json` 的 `paths`，或者想知道 Tifa 为什么用 Vite 打包'
---

# 生成的文件和应用的配置 {#generated-files-and-app-setup}

`tifa build` 和 `tifa dev` 往 `.tifa/` 里写什么，应用可以依赖哪些名字，以及应用在自己根目录里保存的 Vite 和 TypeScript 文件。

## .tifa/ 里的文件 {#files-in-tifa}

| 文件 | 内容 |
| --- | --- |
| `main.go` | 用 `go:embed` 嵌入前端输出 `dist/`，把它设为 `tifaapp.App` 的 `Dist`，并调用 `server.Main`（在 [`tifa dev`](../build-and-deploy/dev.zh-CN.md) 下是 `server.DevMain`） |
| `tifaapp/app.go` | 包 `tifaapp`，里面只有一个变量 `App`：`tifa build` 找到的那个应用。它只填入 `hooks.go` 里真正存在的 hook，把 `tifa.yaml` 的运行时设置作为 Go 代码保存（`Settings`），当根包的 `options.go` 定义了设置、或它的 `content.go` 声明了集合并且没有 `hooks.go` 时，会导入根包，并注册会话类型。每个 `Load` 和 `Layout` 函数会被包成 `func(c *tifa.Ctx) (any, error) { return tifa.Use(c, pkg.Load) }`，布局则是 `pkg.Layout`，同时还作为 `Func` 给出，服务器靠它认出这个函数的每次运行，并给每次运行它能看到的那部分请求。带输入或结果的方法处理函数会经过 `server.Input`、`server.Output` 或 `server.InputOutput`，它们用 `(*tifa.Ctx).Bind` 解码输入（GET 和 DELETE 用 `(*tifa.Ctx).BindQuery`），运行它的 `Validate`，并写出结果。`Fingerprint` 是这个文件的哈希，`tifatest.Start` 会拿它和代码核对。应用的测试以 `<module>/.tifa/tifaapp` 导入它（见[测试](../pages-and-data/testing-apps.zh-CN.md#testing)） |
| `routes.tsx` | TanStack 路由树和 `Register` 类型注册 |
| `session.gen.ts` | `SessionData`，即会话类型的 `Exposed` 方法返回的 JSON（它是 `Exposed` 返回的那个 Go 结构体的别名，文件里按 Go 名字声明了这个结构体），以及 `SessionEnabled`（应用调用了 `tifa.SetSession` 时为 `true`）。当应用没有会话，或它的类型没有 `Exposed` 时，`SessionData` 是 `never`。页面从 `tifa/client` 导入这个类型（见[会话](../pages-and-data/sessions.zh-CN.md)） |
| `i18n.gen.ts` | `i18n`，给浏览器运行时用的应用语言信息，当没有文件夹带 `_locales` 文件夹时是 `undefined`。其中有：默认语言，`i18n.prefix` 是否为 `always`，站点的语言，以及 `routes`，即路由树里属于某个区域的每个路由的语言，按它的 TanStack 路由 ID 索引（布局、页面，以及显示某个布局的 404 的路由）。不属于任何区域的路由不在 `routes` 里：它没有语言，也没有前缀 |
| `app/<folder>/route.gen.ts` | 每个带页面、布局或方法处理函数的文件夹各有一个模块。它为页面导出 `page`，为布局导出 `layout`（TanStack 的路由对象，只在文件夹里有它们时才有），还导出 `PageData` 和 `LayoutData`（`Load` 和 `Layout` 的结果类型）、`Params`，以及每个方法处理函数一个函数，连同它的 `<METHOD>Input` 和 `<METHOD>Output` 类型。这些类型里的每个 Go 结构体，模块也按它的 Go 名字各声明一次（见 [page.tsx](../pages-and-data/loading-data.zh-CN.md#pagetsx)），上面那些类型是它们的别名。这些函数调用 `runtime/call.ts` |
| `tifa/` | 应用的 `tifa/*` 路径的公开入口：`client.ts`，以及带 i18next 适配器的应用还有 `i18n.ts`。每个都重新导出 `runtime/` 里的一个模块。这个文件夹里没有别的东西 |
| `server.ts`, `client.ts` | SSR 入口和浏览器入口。在 `tifa dev` 下，浏览器入口还会导入 `runtime/dev-client.js` |
| `tsconfig.json` | 见下文 |
| `vite.js`, `vite.d.ts`, `vite/plugin.js` | 给 `vite.config.ts` 用的 Tifa 的 Vite 插件，见下文。`vite/plugin.js` 是仓库里 `js/vite/plugin.js` 的副本 |
| `runtime/` | 仓库里 `js/runtime/` 的副本。`i18n.tsx` 和 `locales.d.ts`，即 [i18next 适配器](i18next.zh-CN.md#the-i18next-adapter)，只有带 `_locales` 文件夹的应用才有 |
| `content/` | 给有内容集合的应用：`entries.json`，是 `tifa build` 交给 Vite 插件转成 HTML 的条目（Markdown 里的 front matter 被留成空行，正文放在那里，以及默认语言版本所在的文件夹），还有 `headings.json`，是插件在每篇正文里找到的标题，它们会进入 `dist/content.json`。见[内容](../pages-and-data/content.zh-CN.md#markdown-to-html) |
| `dist/` | 前端输出，见[前端输出](build-output.zh-CN.md#frontend-output) |

没有页面的应用（根本没有 `page.tsx`）只生成 Go 文件，`dist/` 里只有 `build.json` 和公开文件。有内容集合时，它还会得到给它的 `vite.config.ts` 用的 `.tifa/vite.js`，而 `dist/` 里会有正文和它们引用的文件。

应用可以引用的名字有下面这些，它们会保持不变：`.tifa/vite.js`（由 `vite.config.ts` 导入）、`.tifa/tsconfig.json`（由 `tsconfig.json` 继承）、文件夹 `.tifa/tifa/`，里面有 `client.ts`，带 i18next 适配器的应用还有 `i18n.ts`（即 `tsconfig.json` 里唯一的 `tifa/*` 路径，它提供 `tifa/client` 和 `tifa/i18n`，里面只放这些公开入口）、`./route.gen` 的导入，以及 Go 包 `<module>/.tifa/tifaapp`（用于测试）。`.tifa/` 里的其他一切在任何版本里都可能改变。

## 应用的 tsconfig 和 Vite 配置 {#the-apps-tsconfig-and-vite-config}

有页面的应用在它的根目录里有这两个文件。`tifa new` 会写出它们：

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

- 生成的 tsconfig 设置了 `rootDirs: ["..", "."]`：TypeScript 把 `.tifa/app/x/route.gen.ts` 当作 `app/x/route.gen.ts`，所以你源码里的 `import { page } from './route.gen'` 能解析，上面某个文件夹的模块也能，比如为那个文件夹的布局数据用的 `'../../route.gen'`。应用的 `include` 单独写出 `.tifa/**/*`，因为 TypeScript 的通配符会跳过名字以点开头的文件夹。生成的 tsconfig 还会加载 `vite/client` 的类型，所以图片和样式表的导入以及 `import.meta.env` 都能通过类型检查。
- `paths` 在应用自己的 tsconfig 里，所以从那里读取它的工具，比如 shadcn CLI，能看到应用根目录。`@/` 是应用根目录：`@/components/ui/button` 是 `components/ui/button.tsx`。`tifa/client` 是页面代码导入 `clientOnly`、`TifaError`、会话 hook 和 `SessionData` 类型所用的固定模块名，`tifa/i18n` 是 i18next 适配器的模块名。这两个名字，`tifa/client` 和 `tifa/i18n`，以及 `@tanstack/react-router`，不会改变。以后加进 Tifa 的模块会在 `.tifa/tifa/` 下得到它自己的入口，所以 `paths` 永远不需要再加一行。两者在第一次构建之前就能用。
- Vite 通过同样的 `paths` 解析导入（它的 `resolve.tsconfigPaths`），所以你加的或改的路径，比如 `"@/components/*": ["./src/components/*"]`，对类型检查和打包同样生效。Vite 只对 `include` 匹配的文件应用 `paths`，所以请在里面保留 `**/*`。
- `.tifa/vite.js` 里的 `tifa()` 返回 Tifa 的 Vite 插件，并传入项目配置的 `build.target`。它的选项 `markdown`（`{ remarkPlugins, rehypePlugins }`）把应用的插件加进内容集合的转换里，见[内容](../pages-and-data/content.zh-CN.md#markdown-to-html)。它们解析 `./route.gen` 的方式和 TypeScript 相同：任何最后一段是 `route.gen`（带不带 `.js` 或 `.ts` 都行），并且指向 `app/` 之下某个文件夹的相对导入，不管经过多少层（`'../../page/route.gen'` 也行），都会解析到 `.tifa/` 里匹配的文件。如果那个文件夹没有生成的模块，构建会失败。它们还设置下面的入口和输出，并构建 SSR 包。
- `@vitejs/plugin-react` 编译 JSX，并在 `tifa dev` 下加上 React Fast Refresh。把其他 Vite 插件加在这两个之后，比如 `@tailwindcss/vite`（见 [styles.md](../pages-and-data/styles.zh-CN.md#tailwind-css)）。入口、`base`、输出和 `ssr` 环境请留给 Tifa。
- `vite.config.ts` 导入 `./.tifa/vite.js`，所以在 Tifa 生成了 `.tifa/` 之后它就能通过类型检查。`tifa build` 和 `tifa dev` 在 Vite 读取配置之前就会生成它。

应用的 `package.json` 需要 React、React DOM 和 TanStack Router，并且开发依赖里要有 `vite` 和 `@vitejs/plugin-react`。类型检查还需要 `typescript`、`@types/react` 和 `@types/react-dom`。`tifa new` 写出的 `package.json` 里就有这些。Tailwind CSS 见[样式](../pages-and-data/styles.zh-CN.md)。

TanStack Router 必须是这个 Tifa 测试过的版本：`tifa new` 写进 `package.json` 的 `@tanstack/react-router`，以及它装上的 `@tanstack/router-core`。Tifa 通过 TanStack Router 的一个在 TanStack Start 之外没有文档的选项把页面数据交给浏览器（见[数据协议](data-protocol.zh-CN.md#data-protocol)），别的版本可能不再在浏览器里读它。那样，每个带对象数据的页面都会保持空白。所以 `tifa build` 和 `tifa dev` 会按 Node 解析它们的方式读取这两个包的已安装版本，并以一个点出已安装版本和测试过的版本的错误停止。要换到更新的 TanStack Router，请升级 Tifa。想要无论如何都用另一个版本来构建，风险自负，请在 `tifa.yaml` 里设置 `build.skip_tanstack_check: true`（见[设置](settings.zh-CN.md)）。没有页面的应用不检查。

React 和 React DOM 必须是 19.3.0 或更高。Vite 插件按 19.3 写出的样子改写 React DOM 的字符串渲染器（见[前端输出](build-output.zh-CN.md#frontend-output)），所以 `tifa build` 和 `tifa dev` 用同样的方式读取 `react` 和 `react-dom` 的已安装版本，并以一个点出要安装哪个版本的错误停止。19.3.0 的预发布版本，比如 canary，算作更旧。没有页面的应用不检查。

### 为什么用 Vite {#why-vite}

Tifa 用应用自己的 Vite 打包前端，因为有页面的应用是一个 React 应用，而 React 应用为了 npm 本来就有 Node。前端世界的大部分东西，比如 Tailwind CSS 和 shadcn CLI，都期望构建时有 Node。Tifa 从 Node 那里留下的是运行中的程序：它从不需要 Node。

应用得到的：

- **修改立刻可见。** 在 `tifa dev` 下，React Fast Refresh 会就地替换被改动的组件并保留它们的状态，新的 CSS 会替换旧的，都不用重新加载（见 [dev.md](../build-and-deploy/dev.zh-CN.md#vite)）。
- **它已经认识的插件。** Tailwind CSS 就是 `@tailwindcss/vite`，其他任何 Vite 插件，按它自己的文档说的方式放进 `vite.config.ts`。Tifa 不去包装它们。
- **任何 Vite 应用的设置。** `vite.config.ts`、`tsconfig.json` 的 `paths` 和 shadcn CLI 都按它们的文档所说的方式工作，没有只属于 Tifa 的形式。
- **不用改包就能渲染得更快。** Tifa 插件在打包时改写 React DOM、i18next 和其他包，所以 SSR 包在 beastjs 里渲染得更快，而应用安装的是原样的包（见[前端输出](build-output.zh-CN.md#frontend-output)）。
- **Tifa 不用维护的打包功能。** 按路由拆分、清单、带哈希的文件名和源映射都来自 Vite 和 Rolldown，React 世界会让它们保持最新。

它的代价：

- 构建有页面的应用需要 Node、`vite` 和 `@vitejs/plugin-react`。Node 必须是 20.19、22.12 或更高，这是 Vite 8 需要的：在更旧的版本上，`tifa build`、`tifa dev`、`tifa gen` 和 `tifa check` 会停下来，并给出一行点出所发现版本的信息。没有页面的应用不需要 Node 也能构建，除非它的内容集合里有条目。
- `tifa dev` 在应用旁边运行一个 Node 进程，Vite 的开发服务器在同一个地址后面，SSR 包则在它自己的一个 worker 线程上构建，因为 Vite 对每个线程只选一次 React 的构建。
- 有些功能会读取 Vite 和 Rolldown 内部做的事，比如 `dist/inputs.json` 背后的模块列表，所以新的 Vite 可能需要新的 Tifa。
