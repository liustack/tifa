---
summary: '页面的标题、meta 标签、documentHead 和 htmlAttributes'
audience: app
order: 6
translatedFrom: 16c5a62c68cd
read_when:
  - '设置页面的标题、meta 标签或 `<html>` 属性，或者 404 页面和错误页面的 head'
---

# 标题和 meta 标签 {#title-and-meta-tags}

页面的标题和 meta 标签写在它的 `head` 导出里（或根布局的 `documentHead` 里），不要在组件里写成 `<title>` 或 `<meta>`。React 19 允许在组件里写它们，并在浏览器里把它们移进 head，但 Tifa 把文档的 head 当字符串写出，React 只渲染 `<body>` 里的应用根节点。所以服务器 HTML 里这个标签落在 `<body>` 里，爬虫和分享卡片不读那里，页面还可能出现两份描述。

```tsx
// not this
export default function Page() {
  return <><title>Pour-over kettle</title><p>...</p></>
}

// this
export const head = () => ({ meta: [{ title: 'Pour-over kettle' }] })
```

`tifa check`、`tifa build` 和 `tifa dev` 会对用 JSX 写的 `<title>` 或 `<meta>` 给出警告，带文件和行号。`<svg>` 里的 `<title>` 是图形的提示文字，没有问题，`<meta itemProp>` 也一样。

## 文档外壳和 documentHead {#the-document-shell-and-documenthead}

- 文档外壳（`<html>`、`<head>`、资源和注入数据的脚本）由框架当作字符串写出。React 只渲染 `<body>` 里的应用根节点，所以不管哪一层失败或返回 404，页面仍然是完整的文档。根布局（`app/layout.tsx`）可以导出 `documentHead` 来定制外壳，格式与 `head` 的返回值相同。每个页面都带它的标签（没有路由匹配或根 `Layout` 失败时也带），它的 `htmlAttributes` 是每个页面起步用的 `<html>` 属性（没有时是 `{ lang: 'en' }`，路由的 `head` 可以为它的页面改动它们，见下）。把网站默认的 `title` 放在这里，这样没匹配到的 URL 的 404 页面也有标题。页面自己的 `head` 返回了 `title` 时，会覆盖它。导出 `Document` 组件会让构建失败。

```tsx
export const documentHead = {
  htmlAttributes: { lang: 'zh-CN' },
  meta: [{ title: 'Shop' }, { name: 'theme-color', content: '#f2f4f5' }],
  links: [{ rel: 'icon', href: '/favicon.svg' }],
}
```

## 页面的 head 由什么合成 {#what-the-head-of-a-page-is-made-of}

- 页面的 head 来自匹配到的各路由的 `head` 返回值：`title` 和同名的 meta 标签取自拥有它们的最深一层路由，它写出的每一个都保留（每个 `media` 一个 `theme-color`，多个 `og:image`），link、style 和 script 标签按路由顺序去重。框架总是加上 `<meta charset="utf-8">` 和一个 viewport meta，路由的 `name: 'viewport'` meta 会替换后者。标签属性按 React DOM 在服务器上写出它们的方式来写：`className`、`htmlFor`、`httpEquiv`、`crossOrigin` 等变成它们的 HTML 名字。以 `on` 开头的事件属性、无效的属性名、`null`、`undefined` 和函数都不写出。`async` 和 `defer` 这样的布尔属性在为 true 时写成空值。对于其他属性，只有 `data-` 和 `aria-` 属性才会把布尔值写成字符串。`javascript:` 链接会被替换。head 标签不支持 `style` 属性，用了会让渲染失败。`htmlAttributes` 遵循同样的规则。style 和 script 标签的内容原样写出。只有会结束该元素的 `<script` 和 `</script`（在 style 里是 `<style` 和 `</style`）会被转义，做法和 React DOM 一样。浏览器导航之后和 `router.invalidate()` 之后，head 会更新以匹配新的路由。
- `head` 也可以返回 `htmlAttributes`，也就是它的页面的 `<html>` 属性，比如主题 class 或 `data-` 属性。它们逐个属性地合并到根布局 `documentHead` 里的 `htmlAttributes` 之上：给出某个属性的最深一层路由胜出，`undefined` 把它留给上层路由，`null` 把它去掉。属性按 React DOM 写出的名字来匹配，所以一个路由里的 `className` 和另一个里的 `class` 是同一个属性。`class` 整个被替换，从不和别处的值合并。`style` 不支持，和 head 的标签一样。它们按与 head 标签相同的规则写出。浏览器导航之后和 `router.invalidate()` 之后，它们也会跟随新的路由。在浏览器里，只有当 Tifa 想要的值变了，它才写 `<html>` 的属性，也只去掉它自己写的属性。所以页面上脚本设置的东西，比如深色模式脚本加的 `dark` class，或者翻译工具改的 `lang`，在 hydration 和导航期间会保留，只要各路由想要的是这个属性的同一个值。当这个值变了，比如某个路由设置了另一个 `class`，或者一次导航离开它，根布局的 `class` 回来了，Tifa 会写入它，脚本加的 class 就没了。请把这样的主题放在没有路由设置的属性里，比如 `data-theme`，或者让脚本再加一次。没有 SSR 的页面在浏览器里得到这些属性。

```tsx
// app/guide/layout.tsx. Layout in route.go returns the theme of the guide.
export const head = ({ loaderData }: { loaderData?: LayoutData }) => ({
  htmlAttributes: { 'data-theme': loaderData?.theme },
})
```

- 起作用的是那些 head 在页面上的路由。布局下面的 404 或错误保留布局的属性，布局下面没有任何页面的 URL 也一样，因为它的 404 显示在布局里面（见 [404](errors.zh-CN.md#404)）。布局本身失败时，它的 `head` 在没有数据的情况下运行，所以上面的例子会保留根布局的 `data-theme`。
- 在有多种语言的网站上，Tifa 自己写 `lang`，也就是页面的语言。在那里，`htmlAttributes` 里有 `lang` 会让构建失败（见[多语言网站](../start/app.zh-CN.md#a-site-in-several-languages)）。
- `htmlAttributes` 必须是对象，其他值 `pnpm typecheck` 会报告。运行时，这样的值就像 `head` 抛出的错误一样失败：服务端渲染作为意外错误失败（`OnError`，或 500），消息在日志里，在 `tifa dev` 下还在页面上。在浏览器里，导航之后或在没有 SSR 的页面上，路由器会把错误写到控制台，并丢掉那个路由的整个 head。

## 404 页面和错误页面的 head {#the-head-of-a-404-or-an-error-page}

显示错误的页面得到的是 `Load` 失败的页面的 head。被错误替换的那个路由在没有数据的情况下运行它的 `head`，它下面的路由不加标签，它上面每个路由的 `head` 会在现在的匹配结果上再运行一次，所以 `status` 会告诉它有某个路由失败了（见[多语言网站](../start/app.zh-CN.md#a-site-in-several-languages)）。某个路由为没渲染成功的那个页面给出的标签和 `htmlAttributes`，不会出现在错误页面上。浏览器在 hydrate 时算出同样的 head。
