// The head of a page: the tags the head functions of the matched routes return, written as an
// HTML string on the server and kept up to date in document.head by the browser. React renders
// only the application root inside <body>, so neither side renders the head through React.
// Both sides turn a tag's attributes into HTML attributes with the same function, attribute,
// which follows what React DOM writes on the server.

// Tag is one element of the head.
export interface Tag {
  tag: 'title' | 'meta' | 'link' | 'style' | 'script'
  attrs: Record<string, unknown>
  children?: string
}

type Meta = Record<string, unknown> & { title?: string; name?: string; property?: string }

type Item = Record<string, unknown> & { children?: unknown }

// HeadMatch is the part of a TanStack match the head is built from. The head functions of the
// routes fill meta, links, styles and headScripts.
interface HeadMatch {
  status: string
  _notFound?: unknown
  meta?: ReadonlyArray<object | undefined>
  links?: ReadonlyArray<object | undefined>
  styles?: ReadonlyArray<object | undefined>
  headScripts?: ReadonlyArray<object | undefined>
}

// The meta every page starts from. A route's meta with the same name replaces it.
const baseMeta: Meta[] = [{ name: 'viewport', content: 'width=device-width, initial-scale=1' }]

// HTMLAttributes are attributes of the <html> element: the htmlAttributes of the root Layout,
// and those a head function returns. undefined leaves an attribute to the levels above, null
// takes it away.
export type HTMLAttributes = Record<string, unknown>

// The attributes of <html> of an application whose root Layout exports no htmlAttributes.
export const defaultHTMLAttributes: HTMLAttributes = { lang: 'en' }

// TanStack Router keeps only meta, links, scripts and styles of what head returns. routeHead
// carries the htmlAttributes of a head along as one more entry of meta, under this key, which
// headTags leaves out and htmlAttributesOf reads.
const htmlKey = Symbol('tifa.htmlAttributes')

// HeadResult is what a head function may return: what TanStack Router reads, and htmlAttributes.
type HeadResult =
  | {
      htmlAttributes?: HTMLAttributes
      meta?: ReadonlyArray<unknown>
      links?: ReadonlyArray<unknown>
      scripts?: ReadonlyArray<unknown>
      styles?: ReadonlyArray<unknown>
    }
  | undefined
  | void

// WithoutHTMLAttributes is the result of a head as TanStack Router types it, which knows no
// htmlAttributes.
type WithoutHTMLAttributes<R> = R extends Promise<infer T> ? Promise<Omit<T, 'htmlAttributes'>> : Omit<R, 'htmlAttributes'>

// routeHead wraps the head function of a route module so that the htmlAttributes it returns
// reach the matches. A head without them returns what it returned before, the same object.
export function routeHead<C, R extends HeadResult | Promise<HeadResult>>(head: (context: C) => R) {
  return ((context: C) => {
    const result: HeadResult | Promise<HeadResult> = head(context)
    return result instanceof Promise ? result.then(withHTMLAttributes) : withHTMLAttributes(result)
  }) as (context: C) => WithoutHTMLAttributes<R>
}

function withHTMLAttributes(head: HeadResult): unknown {
  if (head?.htmlAttributes === undefined) return head
  const { htmlAttributes, ...rest } = head
  if (typeof htmlAttributes !== 'object' || htmlAttributes === null || Array.isArray(htmlAttributes)) {
    const kind = htmlAttributes === null ? 'null' : Array.isArray(htmlAttributes) ? 'an array' : typeof htmlAttributes
    throw new Error(`head: htmlAttributes is ${kind}, not an object of attributes`)
  }
  // A new array: the meta of a head may be a constant that other renders read too.
  return { ...rest, meta: [...(rest.meta ?? []), { [htmlKey]: htmlAttributes }] }
}

// htmlAttributesOf merges the htmlAttributes of the matches over base, attribute by attribute,
// the deepest route that gives an attribute winning, as the title does. Only the matches the
// page shows count, the first one that did not load included. Attributes are one when React DOM
// writes them under the same HTML name, so className and class are one, and Lang and lang. Only
// own keys count, and __proto__ is none.
export function htmlAttributesOf(all: ReadonlyArray<HeadMatch>, base: HTMLAttributes): HTMLAttributes {
  // The key and the value of each attribute, by its HTML name in lower case.
  const merged = new Map<string, [string, unknown]>()
  const add = (attrs: HTMLAttributes) => {
    for (const key of Object.keys(attrs)) {
      const value = attrs[key]
      if (value === undefined || key === '__proto__') continue
      merged.set((aliases[key] ?? key).toLowerCase(), [key, value])
    }
  }
  add(base)
  for (const match of presented(all))
    for (const m of (match.meta ?? []) as ReadonlyArray<Record<symbol, HTMLAttributes> | undefined>) {
      const attrs = m?.[htmlKey]
      if (attrs) add(attrs)
    }
  const out: HTMLAttributes = Object.create(null)
  for (const [key, value] of merged.values()) out[key] = value
  return out
}

// headTags collects the head of the matches the way TanStack's HeadContent does: the matches
// up to the first one that did not load, the title and each named meta of the deepest route
// that has it, then links, styles and head scripts in route order. Writing them leaves out
// repeated tags, see headHTML and syncHead.
export function headTags(all: ReadonlyArray<HeadMatch>): Tag[] {
  const matches = presented(all)
  const metas: Array<ReadonlyArray<Meta | undefined>> = [baseMeta]
  for (const match of matches) if (match.meta) metas.push(match.meta as ReadonlyArray<Meta | undefined>)
  const meta: Tag[] = []
  const named = new Set<string>()
  let title: Tag | undefined
  for (let i = metas.length - 1; i >= 0; i--) {
    const list = metas[i]!
    for (let j = list.length - 1; j >= 0; j--) {
      const m = list[j]
      if (!m || htmlKey in m) continue
      if (m.title) {
        title ??= { tag: 'title', attrs: {}, children: String(m.title) }
      } else if ('script:ld+json' in m) {
        let json: string
        try {
          json = JSON.stringify(m['script:ld+json'])
        } catch {
          continue
        }
        meta.push({ tag: 'script', attrs: { type: 'application/ld+json' }, children: escapeScriptJSON(json) })
      } else {
        const name = m.name ?? m.property
        if (name) {
          if (named.has(name)) continue
          named.add(name)
        }
        meta.push({ tag: 'meta', attrs: m })
      }
    }
  }
  if (title) meta.push(title)
  meta.reverse()
  const tags = meta
  for (const match of matches)
    for (const link of (match.links ?? []) as ReadonlyArray<Item | undefined>) if (link) tags.push({ tag: 'link', attrs: link })
  for (const kind of ['styles', 'headScripts'] as const)
    for (const match of matches)
      for (const item of (match[kind] ?? []) as ReadonlyArray<Item | undefined>) {
        if (!item) continue
        const { children, ...attrs } = item
        tags.push({ tag: kind === 'styles' ? 'style' : 'script', attrs, children: children as string | undefined })
      }
  return tags
}

// presented keeps the matches up to and including the first one that did not load: the tags
// of the routes below it are not on the page.
function presented<T extends HeadMatch>(matches: ReadonlyArray<T>): ReadonlyArray<T> {
  for (let i = 0; i < matches.length; i++) {
    const match = matches[i]!
    if (match.status !== 'success' || match._notFound) return matches.slice(0, i + 1)
  }
  return matches
}

// The React prop names React DOM writes under another HTML name.
const aliases: Record<string, string> = {
  className: 'class',
  htmlFor: 'for',
  httpEquiv: 'http-equiv',
  acceptCharset: 'accept-charset',
  crossOrigin: 'crossorigin',
  tabIndex: 'tabindex',
  autoFocus: 'autofocus',
  multiple: 'multiple',
  muted: 'muted',
}

// Props React DOM never writes as attributes.
const ignored = new Set([
  'children',
  'dangerouslySetInnerHTML',
  'innerHTML',
  'defaultValue',
  'defaultChecked',
  'suppressContentEditableWarning',
  'suppressHydrationWarning',
  'ref',
  'key',
])

// Boolean attributes: written bare when the value is truthy, left out otherwise.
const booleans = new Set(
  (
    'allowFullScreen async autoFocus autoPlay controls credentialless default defer disabled ' +
    'disablePictureInPicture disableRemotePlayback formNoValidate hidden inert itemScope loop multiple ' +
    'muted noModule noValidate open playsInline readOnly required reversed scoped seamless'
  ).split(' '),
)

// Attributes whose boolean values React DOM writes as the strings "true" and "false".
const booleanStrings = new Set(
  'contentEditable spellCheck draggable value autoReverse externalResourcesRequired focusable preserveAlpha'.split(' '),
)

// React DOM's rule for a valid attribute name.
const validName =
  /^[:A-Z_a-zÀ-ÖØ-öø-˿Ͱ-ͽͿ-῿‌-‍⁰-↏Ⰰ-⿯、-퟿豈-﷏ﷰ-�][:A-Z_a-zÀ-ÖØ-öø-˿Ͱ-ͽͿ-῿‌-‍⁰-↏Ⰰ-⿯、-퟿豈-﷏ﷰ-�\-.0-9·̀-ͯ‿-⁀]*$/

// React DOM replaces a javascript: URL with one that throws.
const javascriptURL =
  /^[\u0000-\u001F ]*j[\r\n\t]*a[\r\n\t]*v[\r\n\t]*a[\r\n\t]*s[\r\n\t]*c[\r\n\t]*r[\r\n\t]*i[\r\n\t]*p[\r\n\t]*t[\r\n\t]*:/i
const blockedURL = "javascript:throw new Error('React has blocked a javascript: URL as a security precaution.')"

// attribute is the HTML attribute React DOM writes for the prop key with value: its name and
// its value, "" for a bare boolean attribute, or undefined when React DOM writes nothing. Event
// handlers (on...), null, undefined, functions, symbols and invalid names are left out. A style
// prop is rejected: head tags take no inline style.
export function attribute(key: string, value: unknown): [string, string] | undefined {
  if (value === null || value === undefined || ignored.has(key)) return undefined
  if (typeof value === 'function' || typeof value === 'symbol') return undefined
  if (key === 'style') throw new Error('head: the head and <html> take no style attribute, set styles in a stylesheet')
  if (booleans.has(key)) return value ? [aliases[key] ?? key, ''] : undefined
  if (booleanStrings.has(key)) return [key, String(value)]
  if (key === 'href' || key === 'src') {
    if (value === '' || typeof value === 'boolean') return undefined
    const url = String(value)
    return [key, javascriptURL.test(url) ? blockedURL : url]
  }
  if (key.length > 2 && (key[0] === 'o' || key[0] === 'O') && (key[1] === 'n' || key[1] === 'N')) return undefined
  const name = aliases[key] ?? key
  if (!validName.test(name)) return undefined
  if (typeof value === 'boolean') {
    const prefix = name.slice(0, 5).toLowerCase()
    if (prefix !== 'data-' && prefix !== 'aria-') return undefined
  }
  return [name, String(value)]
}

// attributes writes the attributes of a tag into HTML.
export function attributes(attrs: Record<string, unknown>): string {
  let out = ''
  for (const key in attrs) {
    const written = attribute(key, attrs[key])
    if (written) out += ' ' + written[0] + '="' + escapeAttribute(written[1]) + '"'
  }
  return out
}

// setAttributes sets the attributes of a tag on a DOM element, the same ones attributes writes.
export function setAttributes(element: Element, attrs: Record<string, unknown>) {
  for (const key in attrs) {
    const written = attribute(key, attrs[key])
    if (written) element.setAttribute(written[0], written[1])
  }
}

// text is the content of the element of a tag as the browser holds it. The content of style
// and script is written as it is, with the sequences that would end the element escaped the way
// React DOM escapes the text of a script or style.
function text(tag: Tag): string {
  const children = tag.children ?? ''
  if (tag.tag === 'script') return children.replace(/(<\/|<)(s)(cript)/gi, scriptEscape)
  if (tag.tag === 'style') return children.replace(/(<\/|<)(s)(tyle)/gi, styleEscape)
  return children
}

const scriptEscape = (_: string, prefix: string, s: string, suffix: string) =>
  prefix + (s === 's' ? '\\u0073' : '\\u0053') + suffix
const styleEscape = (_: string, prefix: string, s: string, suffix: string) =>
  prefix + (s === 's' ? '\\73 ' : '\\53 ') + suffix

export function tagHTML(tag: Tag): string {
  const open = '<' + tag.tag + attributes(tag.attrs) + '>'
  if (tag.tag === 'meta' || tag.tag === 'link') return open
  return open + (tag.tag === 'title' ? escapeText(text(tag)) : text(tag)) + '</' + tag.tag + '>'
}

// headHTML writes the tags, each distinct tag once.
export function headHTML(tags: Tag[]): string {
  let out = ''
  const seen = new Set<string>()
  for (const tag of tags) {
    const html = tagHTML(tag)
    if (seen.has(html)) continue
    seen.add(html)
    out += html
  }
  return out
}

// tagKey identifies a tag by its element, attributes in order and content, the same way for a
// tag and for the element the browser parsed from the HTML of it, see elementKey. Two tags with
// the same key are written as the same HTML. HTML attribute names are case-insensitive, and
// the browser holds them in lower case.
function tagKey(tag: Tag): string {
  let key = tag.tag
  for (const name in tag.attrs) {
    const written = attribute(name, tag.attrs[name])
    if (written) key += '\n' + written[0].toLowerCase() + '=' + written[1]
  }
  return key + '\n\n' + text(tag)
}

// A page with a Content-Security-Policy hides the nonce of an element in the document: the
// attribute reads empty and the nonce property keeps the value, so the key reads the property.
function elementKey(element: Element): string {
  let key = element.localName
  for (const attr of Array.from(element.attributes))
    key += '\n' + attr.name + '=' + (attr.name === 'nonce' ? (element as HTMLElement).nonce : attr.value)
  return key + '\n\n' + (element.textContent ?? '')
}

// createTagElement builds the element of a tag with the DOM, never by parsing HTML, so no
// value of a tag can add an element of its own.
function createTagElement(tag: Tag): Element {
  const element = document.createElement(tag.tag)
  setAttributes(element, tag.attrs)
  if (tag.children !== undefined) element.textContent = text(tag)
  return element
}

// escapeScriptJSON makes JSON safe inside a script element, as TanStack's escapeHtml does: no
// character of it can end the element, and U+2028 and U+2029 stay escaped.
const scriptJSONEscapes: Record<string, string> = {
  '&': '\\u0026',
  '>': '\\u003e',
  '<': '\\u003c',
  '\u2028': '\\u2028',
  '\u2029': '\\u2029',
}

export function escapeScriptJSON(json: string) {
  if (!/[&><\u2028\u2029]/.test(json)) return json
  return json.replace(/[&><\u2028\u2029]/g, (c) => scriptJSONEscapes[c]!)
}

export function escapeText(text: string) {
  if (!/[&<>]/.test(text)) return text
  return text.replace(/[&<>]/g, (c) => (c === '&' ? '&amp;' : c === '<' ? '&lt;' : '&gt;'))
}

export function escapeAttribute(text: string) {
  if (!/[&"<>]/.test(text)) return text
  return text.replace(/[&"<>]/g, (c) => (c === '&' ? '&amp;' : c === '"' ? '&quot;' : c === '<' ? '&lt;' : '&gt;'))
}

// The comments around the tags the router manages in document.head.
export const headStart = '<!--tifa-head-->'
export const headEnd = '<!--/tifa-head-->'

// syncHead makes the managed tags of document.head equal to headTags of the matches, in the
// same order. An element whose tag stays is kept, so the head the server wrote stays as it is
// after hydration, and a script both pages share does not run again. When the order changed,
// kept elements move the way React moves the children of a keyed list, as TanStack's
// HeadContent does for a style: walking the new order, an element stays where it is unless it
// came before one that already stayed. Only the elements out of place move, since moving one is
// not free: a style element builds its style sheet again from its text, dropping rules added
// with insertRule, and a stylesheet link is processed again. New elements go in in document
// order, so new inline scripts run in the order of the tags.
export function syncHead(matches: ReadonlyArray<HeadMatch>) {
  const [start, end] = markers()
  const tags: Tag[] = []
  const keys: string[] = []
  for (const tag of headTags(matches)) {
    const key = tagKey(tag)
    if (keys.includes(key)) continue
    tags.push(tag)
    keys.push(key)
  }
  const elements: Element[] = []
  for (let node = start.nextSibling; node && node !== end; node = node.nextSibling)
    if (node.nodeType === 1) elements.push(node as Element)
  const existing = elements.map(elementKey)
  if (existing.length === keys.length && existing.every((key, i) => key === keys[i])) return
  const unused = new Map<string, Element[]>()
  existing.forEach((key, i) => {
    const list = unused.get(key)
    if (list) list.push(elements[i]!)
    else unused.set(key, [elements[i]!])
  })
  const kept: Array<Element | undefined> = keys.map((key) => unused.get(key)?.shift())
  for (const list of unused.values()) for (const element of list) element.remove()
  // stays[i]: kept[i] is already in place among the elements that stay.
  const stays: boolean[] = []
  let last = -1
  for (const element of kept) {
    const at = element ? elements.indexOf(element) : -1
    stays.push(at > last)
    if (at > last) last = at
  }
  // before[i]: the element that stays right after position i, or the end marker.
  const before: Node[] = []
  let anchor: Node = end
  for (let i = kept.length - 1; i >= 0; i--) {
    before[i] = anchor
    if (stays[i]) anchor = kept[i]!
  }
  const parent = start.parentNode!
  for (let i = 0; i < tags.length; i++)
    if (!stays[i]) parent.insertBefore(kept[i] ?? createTagElement(tags[i]!), before[i]!)
}

// markers finds the comments around the managed tags, or adds them at the end of the head of a
// page the server did not render.
function markers(): [Comment, Comment] {
  const head = document.head
  let start: Comment | undefined
  let end: Comment | undefined
  for (const node of Array.from(head.childNodes)) {
    if (node.nodeType !== 8) continue
    if (node.nodeValue === 'tifa-head') start = node as Comment
    else if (node.nodeValue === '/tifa-head') end = node as Comment
  }
  if (start && end) return [start, end]
  start = document.createComment('tifa-head')
  end = document.createComment('/tifa-head')
  head.append(start, end)
  return [start, end]
}

// The attributes of <html> the last syncHTMLAttributes wrote, by their names in lower case, and
// the element it wrote them on. Attributes of other names, such as a theme a script of the page
// sets, are left as they are.
let managed: { element: Element; names: Set<string> } | undefined

// syncHTMLAttributes makes the attributes of <html> those htmlAttributesOf gives for the
// matches, written by the rules of attribute. It takes away the ones it wrote before and no
// longer gives, and sets only those whose value changed.
export function syncHTMLAttributes(matches: ReadonlyArray<HeadMatch>, base: HTMLAttributes) {
  const element = document.documentElement
  const before = managed?.element === element ? managed.names : new Set<string>()
  const names = new Set<string>()
  const attrs = htmlAttributesOf(matches, base)
  for (const key in attrs) {
    const written = attribute(key, attrs[key])
    if (!written) continue
    const name = written[0].toLowerCase()
    names.add(name)
    if (element.getAttribute(name) !== written[1]) element.setAttribute(written[0], written[1])
  }
  for (const name of before) if (!names.has(name)) element.removeAttribute(name)
  managed = { element, names }
}

// followHead keeps document.head and the attributes of <html> in step with the router's
// matches: after a navigation, and whenever the data of a match changes, such as when
// router.invalidate() refetches in the background. This is what TanStack's HeadContent reads
// too. In the browser build TanStack makes the matches a store one can subscribe to, on the
// server a plain value. base is the htmlAttributes of the root Layout.
export function followHead(router: { stores: { matches: { get: () => ReadonlyArray<HeadMatch> } } }, base: HTMLAttributes) {
  const matches = router.stores.matches as { get: () => ReadonlyArray<HeadMatch>; subscribe?: (listener: () => void) => unknown }
  if (typeof matches.subscribe !== 'function') throw new Error('head: the router has no store of matches to follow, it is not a browser router')
  matches.subscribe(() => {
    const current = matches.get()
    syncHead(current)
    syncHTMLAttributes(current, base)
  })
}
