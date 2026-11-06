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
//
// Links in the markup go through the router: a click on a link to a page of the app navigates, and
// a hover or a focus preloads it, as a Link does. HTML handles the events on its own element only.
import { createElement, useId, useRef, type FocusEvent, type HTMLAttributes, type MouseEvent, type SyntheticEvent, type TouchEvent } from 'react'
import { useRouter, type AnyRouter } from '@tanstack/react-router'
import { reaches } from './prefix'
import { isPage } from './router'

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

export function HTML({ value, as = 'div', onClick, onMouseOver, onMouseOut, onFocus, onBlur, onTouchStart, ...rest }: HTMLProps) {
  if (!(holders as readonly string[]).includes(as)) throw new Error(`tifa: <HTML as="${as}"> cannot hold markup: as takes one of ${holders.join(', ')}`)
  const id = useId()
  const router = useRouter()
  // The preload a hover or a focus waits to start.
  const waiting = useRef<ReturnType<typeof setTimeout>>(undefined)
  const preload = (event: SyntheticEvent<HTMLElement>, delay: number) => {
    clearTimeout(waiting.current)
    const href = routerHref(router, event.currentTarget, event.target)
    if (href === null) return
    // As a Link of TanStack does, a preload that fails only warns: the click loads again. The type
    // of preloadRoute wants a to, which href takes the place of at run time as in navigate.
    const start = () => void router.preloadRoute({ href } as Parameters<typeof router.preloadRoute>[0]).catch((error: unknown) => console.warn(error))
    if (delay === 0) start()
    else waiting.current = setTimeout(start, delay)
  }
  const cancel = () => clearTimeout(waiting.current)
  // The type keeps both out, but a spread of an object brings them in, and React throws for children
  // next to dangerouslySetInnerHTML.
  const { children: _children, dangerouslySetInnerHTML: _inner, ...props } = rest as typeof rest & { children?: unknown; dangerouslySetInnerHTML?: unknown }
  // The handlers of the app run first. One that calls preventDefault keeps the router out.
  return createElement(as, {
    ...props,
    'data-tifa-html': id,
    dangerouslySetInnerHTML: { __html: markup(value, id) },
    onClick: (event: MouseEvent<HTMLElement>) => {
      onClick?.(event)
      const href = routerHref(router, event.currentTarget, event.target, event.nativeEvent)
      if (href === null) return
      event.preventDefault()
      cancel()
      void router.navigate({ href })
    },
    onMouseOver: (event: MouseEvent<HTMLElement>) => {
      onMouseOver?.(event)
      preload(event, intentDelay)
    },
    onMouseOut: (event: MouseEvent<HTMLElement>) => {
      onMouseOut?.(event)
      cancel()
    },
    onFocus: (event: FocusEvent<HTMLElement>) => {
      onFocus?.(event)
      preload(event, intentDelay)
    },
    onBlur: (event: FocusEvent<HTMLElement>) => {
      onBlur?.(event)
      cancel()
    },
    onTouchStart: (event: TouchEvent<HTMLElement>) => {
      onTouchStart?.(event)
      preload(event, 0)
    },
  })
}

// intentDelay is how long a hover or a focus stays on a link before its page preloads, the
// defaultPreloadDelay of TanStack Router.
const intentDelay = 50

// Click is what routerHref reads of a click.
export type Click = Pick<globalThis.MouseEvent, 'button' | 'defaultPrevented' | 'ctrlKey' | 'metaKey' | 'shiftKey' | 'altKey'>

// routerHref returns the URL the router goes to for an event on target inside container, an
// element of <HTML>, or null when the browser follows the link itself: a click that the app took
// or that is not a plain left click, no link with an href inside container, a link with a target
// other than _self, download or rel="external", another origin or scheme, a hash on the page that
// shows, a file Go serves and a path without a page, such as an API route. A hover or a focus
// passes no click.
export function routerHref(router: AnyRouter, container: Element, target: EventTarget | null, click?: Click): string | null {
  if (click && (click.defaultPrevented || click.button !== 0 || click.ctrlKey || click.metaKey || click.shiftKey || click.altKey)) return null
  if (!(target instanceof Element)) return null
  const link = target.closest('a[href], area[href]')
  if (link === null || !container.contains(link)) return null
  const linkTarget = link.getAttribute('target')
  if ((linkTarget && linkTarget !== '_self') || link.hasAttribute('download')) return null
  if ((link.getAttribute('rel') ?? '').split(/\s+/).includes('external')) return null
  // The markup is the app's, and an href that is no URL is the browser's to handle.
  let url: URL
  try {
    url = new URL(link.getAttribute('href')!, document.baseURI)
  } catch {
    return null
  }
  if (url.origin !== location.origin) return null
  if (url.hash !== '' && url.pathname === location.pathname && url.search === location.search) return null
  // isPage matches the plain path the routes see, which the router's input rewrite gives.
  const plain = router.parseLocation({ href: url.pathname, pathname: url.pathname, search: '', hash: '', state: { __TSR_index: 0 } }).pathname
  if (!isPage(router, plain)) return null
  // One document holds one language: the browser follows a link into another one.
  if (!reaches(router, url.pathname)) return null
  return url.pathname + url.search + url.hash
}
