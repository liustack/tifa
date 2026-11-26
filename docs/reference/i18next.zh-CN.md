---
summary: 'Tifa 里的 i18next：`tifa/i18n` 适配器，以及 Vite 插件在 SSR 包里给 i18next 加的缓存'
audience: app
order: 15
translatedFrom: 570d5bfecca0
read_when:
  - '用 `tifa/i18n` 使用 i18next，或者关掉 `build.i18next_cache`'
---

# Tifa 里的 i18next {#i18next-in-tifa}

应用怎样使用 i18next，以及 Tifa 怎样让它在服务器上更快。

## i18next 适配器 {#the-i18next-adapter}

`tifa/i18n` 是官方的 i18next 适配器。它把应用的翻译文件打包进来，为所有语言做一个 i18next 实例，并给页面提供常用的 react-i18next hook。应用自己安装 `i18next` 和 `react-i18next`（端到端测试用 i18next 27.1.2 和 react-i18next 18.0.1 运行它），并且它的 tsconfig 在 `paths` 里有 `"tifa/*": ["./.tifa/tifa/*"]`（`tifa new` 就是这样写的）。Tifa 只为带 `_locales` 文件夹的应用写出 `tifa/i18n.ts` 和 `runtime/i18n.tsx`，因为 TypeScript 会检查 `.tifa/` 的全部内容，而没有翻译的应用没有 i18next 可导入。

字符串放在 `app/` 的各个文件夹里的 `_locales/<language>.json` 中。`<language>.draft.json` 不会发布，文件夹里的其他文件也都不算。一个文件夹的命名空间就是它相对于 `app/` 的路径：`app/store/_locales` 是 `store`，`app/admin/store/_locales` 是 `admin/store`，`app/_locales` 是默认命名空间。Vite 插件把这些文件变成一个模块，所以服务端渲染和浏览器都有每一个字符串，不用获取任何东西。

```tsx
// app/store/cart/page.tsx
import { useTranslation } from 'tifa/i18n'

export default function Page() {
  const { t } = useTranslation('store')
  return <h1>{t('cart.title')}</h1>
}
```

```tsx
// app/layout.tsx: the provider reads the language of the page from useLanguage() of tifa/client
import { I18nProvider } from 'tifa/i18n'

export default function Layout({ children }: { children: React.ReactNode }) {
  return <I18nProvider>{children}</I18nProvider>
}
```

格式化器放在 [setup.ts](../pages-and-data/hooks.zh-CN.md#setupts) 的顶层，它在服务器和浏览器的第一次渲染之前运行。格式化器会拿到语言作为 `lng`，所以一个就够所有的语言用：

```ts
// setup.ts
import { i18n } from 'tifa/i18n'

i18n.services.formatter?.addCached('currency', (lng, options) => {
  const f = new Intl.NumberFormat(lng, { currency: 'USD', ...options, style: 'currency' })
  return (value) => f.format(value)
})
```

| 导出 | 作用 |
| --- | --- |
| `I18nProvider` | 给它的子元素提供这个实例，使用 `useLanguage()` 的语言 |
| `useTranslation`, `Trans` | react-i18next 的那两个，原样不变 |
| `i18n` | 这个实例。用于 `setup.ts` 里的格式化器，以及组件之外的代码，比如页面标题：`i18n.getFixedT(language, 'store')('cart.title')` |

这个实例有每种语言的字符串，`fallbackLng: false`，`load: 'currentOnly'`（en-US 从不取 en 的字符串），`initAsync: false`（`init` 返回时它就准备好了，所以第一次渲染就有每个字符串）和 `interpolation.escapeValue: false`（React 会转义）。没有字符串的键给出键本身，和 i18next 一样。某个区域在某种语言里没有字符串，就是没有。`I18nProvider` 在服务端渲染之前把实例切换到页面的语言。在浏览器里，一个文档只有一种语言，因为另一种语言的页面是另一个文档，所以它只切换一次。

这个适配器可以配合 [`build.i18next_cache`](#i18next-cache) 工作，它也会保存使用应用的格式化器的翻译。所以格式化器必须对相同的语言、选项和值给出相同的文本。

## i18next 缓存 {#i18next-cache}

用 i18next 翻译一次，在 beastjs 里要花几十微秒，商店的一个店面页面每次渲染要做约 126 次，大多数是每次都相同的文本和相同的值。所以 Tifa 插件在 SSR 包里给 i18next 加了一个缓存。`getFixedT` 返回的 `t`（react-i18next 的 `useTranslation` 把它交出来）会保存它给出的每个字符串，之后用同样的键和选项调用，不用再翻译一次就能得到那个字符串。每个 VM 有它自己的缓存。它默认是开启的。在项目文件里设置 `build.i18next_cache: false` 可以关闭它，包就和没有这个缓存时一模一样，一个字节都不差。

一次调用按实例、语言、命名空间、键和选项来查找，只有值都是普通值的调用才会被保存，其余的照旧翻译。应用通过 i18next 的 API 改变翻译时（添加资源、再次 `init`、加载命名空间或语言、`use` 等），缓存会清空，每个 VM 里它最多保存约 1 MB。调用很少被缓存答中的键，会不再使用缓存。完整的规则见 [i18next 缓存](../internals/ssr-bundle-rewrites.md#the-i18next-cache)。

运行应用的格式化器（用 `services.formatter.add` 或 `addCached` 添加的），或后处理器（比如 i18next-sprintf-postprocessor）的调用，和其他的调用一样被保存，i18next 自己的格式化器的调用也一样。这样的代码对相同的语言、选项和值必须给出相同的文本，就像 React 组件对相同的 props 必须给出相同的输出一样：它不读时间，不读随机数，也不读任何随请求而变的东西。请求的值，比如用户的时区或当前时间，放进调用的选项里，选项是每个条目的一部分。当在 `add` 或 `addCached` 里就地写出的格式化器自己调用了 `Date.now()`、`Date()`、不带参数的 `new Date()`、`Math.random()`、`performance.now()`、`crypto.randomUUID()` 或 `crypto.getRandomValues()` 时，构建会失败，并点出文件和行。格式化器所调用的函数里的这类调用，它看不到。

留给应用的是：在第一次渲染之后，不要去碰 i18next 不通过它的 API 读取的东西。当应用做下面这些事之一时，请用 `build.i18next_cache: false` 关闭缓存：

- 不通过 i18next 的 API 修改资源：它交给 `init` 的资源对象（i18next 保存并读取它），或 `getResourceBundle` 和 `getDataByLanguage` 返回的对象
- 不通过 i18next 的 API 修改 `i18n.options`（`interpolation.defaultVariables` 除外）或 `services.formatter.formats` 里的格式化器，或就地修改 `i18nFormat` 模块的 `formats` 对象。i18next-icu 的 `addUserDefinedFormats` 会给模块一个新的对象，缓存能看到这一点。
- 给 i18next 提供对相同输入结果会变化的其他函数，比如因为它们读取了请求或时间的某些东西：`overloadTranslationOptionHandler`、`interpolation.escape`、`i18nFormat` 模块，或调用选项里的 getter。这样的格式化器或后处理器，即使关了缓存也是错的，因为浏览器写出的文本会和服务器的不同，所以这不是关闭缓存的理由。

这样的应用的页面，可能会显示改动之前的文本，或另一个请求得到的文本，直到缓存因为别的原因清空。

在商店上，它使用适配器的那一个实例，并在 `setup.ts` 里给它自己的钱和日期的格式化器，十个店面页面的渲染耗时是原来的 0.778，HTML 相同。用 i18next 的其他方式收益更小，而每个请求用 `cloneInstance` 做一个实例的应用没有收益。测量结果见 [i18next 缓存](../internals/ssr-bundle-rewrites.md#the-i18next-cache)。

插件认识 i18next 25.5 到 27.0 的代码。对于其他版本，或某个版本改动了缓存所需要的代码时，构建会打印警告，包就是没有缓存的那个。用 `build.i18next_cache: false` 构建这样的应用，就没有这条警告。
