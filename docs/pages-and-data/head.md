---
summary: 'Title, meta tags, documentHead and htmlAttributes of a page'
audience: app
order: 6
read_when:
  - Setting the title, meta tags or `<html>` attributes of a page, or the head of a 404 or error page
---

# Title and meta tags

Write the title and the meta tags of a page in its `head` export (or in `documentHead` of the root Layout), never as `<title>` or `<meta>` in a component. React 19 allows them in a component and moves them into the head in the browser, but Tifa writes the head of the document as a string and React renders only the app root inside `<body>`. So the server HTML has the tag in `<body>`, where crawlers and share cards do not read it, and the page can end up with two descriptions.

```tsx
// not this
export default function Page() {
  return <><title>Pour-over kettle</title><p>...</p></>
}

// this
export const head = () => ({ meta: [{ title: 'Pour-over kettle' }] })
```

`tifa check`, `tifa build` and `tifa dev` warn about a `<title>` or `<meta>` written as JSX, with its file and line. A `<title>` inside an `<svg>` is the tooltip of a graphic and is fine, and so is a `<meta itemProp>`.

## The document shell and documentHead

- The document shell (`<html>`, `<head>`, assets and the script that injects data) is written as a string by the framework. React only renders the app root inside `<body>`, so the page is still a complete document when any level fails or returns 404. The root Layout (`app/layout.tsx`) can export `documentHead` to customize the shell, in the same format as the return value of `head`. Its tags are carried by every page, also when no route matches or the root `Layout` fails, and its `htmlAttributes` are the attributes of `<html>` every page starts from (`{ lang: 'en' }` when it has none, and a route's `head` can change them for its pages, see below). Put the site's default `title` here so that the 404 page for an unmatched URL also has a title. A page's own `head` that returns a `title` overrides it.

```tsx
export const documentHead = {
  htmlAttributes: { lang: 'zh-CN' },
  meta: [{ title: 'Shop' }, { name: 'theme-color', content: '#f2f4f5' }],
  links: [{ rel: 'icon', href: '/favicon.svg' }],
}
```

## What the head of a page is made of

- A page's head comes from the `head` return values of the matched routes: `title` and meta tags with the same name come from the deepest route that has them, which keeps every one of them it writes (a `theme-color` for each `media`, several `og:image`), and link, style and script tags are deduplicated in route order. The framework always adds `<meta charset="utf-8">` and a viewport meta, and a route's `name: 'viewport'` meta replaces the latter. Tag attributes are written the way React DOM writes them on the server: `className`, `htmlFor`, `httpEquiv`, `crossOrigin` and so on become their HTML names. Event attributes starting with `on`, invalid attribute names, `null`, `undefined` and functions are not written. Boolean attributes such as `async` and `defer` are written with an empty value when true. For other attributes, boolean values are written as strings only on `data-` and `aria-` attributes. `javascript:` links are replaced. Head tags do not support the `style` attribute, and using it makes rendering fail. `htmlAttributes` follows the same rules. The content of style and script tags is written as is. Only the `<script` and `</script` (in style, `<style` and `</style`) that would end the element are escaped, the way React DOM does it. After browser navigation and after `router.invalidate()`, the head updates to match the new routes.
- A `head` can also return `htmlAttributes`, the attributes of `<html>` for its pages, such as a theme class or a `data-` attribute. They are merged over the `htmlAttributes` of the root Layout's `documentHead` one attribute at a time: the deepest route that gives an attribute wins, `undefined` leaves it to the routes above, and `null` takes it away. Attributes are matched by the name React DOM writes, so `className` in one route and `class` in another are the same attribute. `class` is replaced as a whole, never merged with a value from elsewhere. `style` is not supported, as on the tags of the head. They are written by the same rules as the tags of the head. After browser navigation and after `router.invalidate()` they follow the new routes too. In the browser Tifa writes an attribute of `<html>` only when the value it wants for it changes, and takes away only attributes it wrote. So what a script of the page sets, such as the `dark` class of a dark mode script or the `lang` a translation tool changes, stays through hydration and navigation while the routes want the same value of that attribute. When that value changes, because a route sets another `class` or a navigation leaves it and the root Layout's `class` comes back, Tifa writes it and the class the script added is gone. Keep such a theme in an attribute no route sets, such as `data-theme`, or have the script add it again. A page without SSR gets these attributes in the browser.

```tsx
// app/guide/layout.tsx. Layout in route.go returns the theme of the guide.
export const head = ({ loaderData }: { loaderData?: LayoutData }) => ({
  htmlAttributes: { 'data-theme': loaderData?.theme },
})
```

- The routes that count are those whose head is on the page. A 404 or an error below the Layout keeps the Layout's attributes, also for a URL below the Layout that no page has, since its 404 shows inside the Layout (see [404](errors.md#404)). When the Layout itself fails, its `head` runs without data, so the example above leaves the root Layout's `data-theme`.
- On a site with languages Tifa writes `lang` itself, the language of the page. A `lang` in `htmlAttributes` fails the build there (see [A site in several languages](../start/app.md#a-site-in-several-languages)).
- `htmlAttributes` must be an object, and `pnpm typecheck` reports any other value. At run time such a value fails like an error the `head` throws: the server render fails as an unexpected error (`OnError`, or a 500), with the message in the log, and on the page under `tifa dev`. In the browser, after a navigation or on a page without SSR, the router writes the error to the console and drops the whole head of that route.

## The head of a 404 or an error page

The page that shows the error gets the head of a page whose `Load` failed. The route the error replaces runs its `head` without data, the routes below it add no tags, and the `head` of every route above it runs again over the matches as they now are, so `status` tells it that a route failed (see [A site in several languages](../start/app.md#a-site-in-several-languages)). Tags and `htmlAttributes` that a route gave for the page that did not render are not on the error page. The browser works out the same head when it hydrates.
