// HTML shows a tifa.HTML value of the data of a page: markup the app trusts, such as an article the
// build turned from Markdown into HTML.
//
// A page the server renders carries such a value once. Go writes a marker ({"$tifa":"html"}) in its
// place into the hydration data, since the markup holds it already (pageText in server/htmltext.go),
// and HTML reads the markup back from the element the server rendered when the browser hydrates
// the page. useId names that element: React gives the same ID on the server and in the browser
// that hydrates the same tree. React compares __html with the markup only in its development build
// (diffHydratedGenericElement of react-dom-bindings), and sets innerHTML on an update only when
// __html changes (setProp), so reading the markup back keeps both quiet and leaves the DOM as the
// server wrote it. Navigation data and a page that skips SSR carry the value as a string.
import { createElement, useId, type HTMLAttributes } from 'react'

declare const htmlBrand: unique symbol

// HTML is the type of tifa.HTML in the data of a page. It is no string, so TypeScript refuses it
// where a string goes. The component HTML shows it.
export type HTML = { readonly [htmlBrand]: true }

const notHTML = 'tifa: <HTML> got a value that is not tifa.HTML'
const notRendered =
  'tifa: this tifa.HTML value came with the page, which carries it only in the markup the server rendered, and the server rendered no <HTML> for it here. ' +
  'Render <HTML value={...}> on the server every time: not behind an effect, a click or a state only the browser has, and not in a .client component'

// fromPage holds the markup read back for each marker, so that the data the router keeps for the
// page shows it again after a move away and back, and with the same string, which React does not
// write into the DOM again.
const fromPage = new WeakMap<object, string>()

function markup(value: unknown, id: string): string {
  if (typeof value === 'string') return value
  if (typeof value !== 'object' || value === null || (value as { $tifa?: unknown }).$tifa !== 'html') throw new Error(notHTML)
  const known = fromPage.get(value)
  if (known !== undefined) return known
  const element = rendered(id)
  if (element === undefined) throw new Error(notRendered)
  const text = element.innerHTML
  fromPage.set(value, text)
  return text
}

// rendered finds the element the server rendered for the HTML with id, which only the page the
// browser hydrates has.
function rendered(id: string): Element | undefined {
  if (typeof document === 'undefined') return undefined
  for (const element of document.querySelectorAll('[data-tifa-html]')) if (element.getAttribute('data-tifa-html') === id) return element
  return undefined
}

export type HTMLProps = Omit<HTMLAttributes<HTMLElement>, 'children' | 'dangerouslySetInnerHTML'> & {
  // The tifa.HTML value of the data of the page.
  value: HTML
  // The element that holds the markup, div unless it says another.
  as?: HTMLHolder
}

// holders are the elements as may name: those that hold flow or phrasing content. An element whose
// content runs or is not markup (script, style, iframe, template, textarea and the like) would make
// the cleaning of the markup useless or show it as text.
const holders = ['div', 'article', 'section', 'main', 'aside', 'header', 'footer', 'nav', 'figure', 'blockquote', 'li', 'td', 'span', 'p'] as const

export type HTMLHolder = (typeof holders)[number]

export function HTML({ value, as = 'div', ...rest }: HTMLProps) {
  if (!(holders as readonly string[]).includes(as)) throw new Error(`tifa: <HTML as="${as}"> cannot hold markup: as takes one of ${holders.join(', ')}`)
  const id = useId()
  // The type keeps both out, but a spread of an object brings them in, and React throws for children
  // next to dangerouslySetInnerHTML.
  const { children: _children, dangerouslySetInnerHTML: _inner, ...props } = rest as typeof rest & { children?: unknown; dangerouslySetInnerHTML?: unknown }
  return createElement(as, { ...props, 'data-tifa-html': id, dangerouslySetInnerHTML: { __html: markup(value, id) } })
}
