// The head of a page: the tags the head functions of the matched routes return, written as an
// HTML string on the server and kept up to date in document.head by the browser. React renders
// only the application root inside <body>, so neither side renders the head through React.

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
      if (!m) continue
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

// HTML names of the React-style attribute names head functions use. Other names are written
// in lower case, names with a dash as they are.
const attributeNames: Record<string, string> = {
  httpEquiv: 'http-equiv',
  className: 'class',
  htmlFor: 'for',
  acceptCharset: 'accept-charset',
}

const validName = /^[A-Za-z_:][-A-Za-z0-9_:.]*$/
const checkedNames = new Map<string, string>()

// attributeName is the HTML name of an attribute. It throws for a name that is not one.
export function attributeName(key: string) {
  let name = checkedNames.get(key)
  if (name !== undefined) return name
  name = attributeNames[key] ?? (key.includes('-') ? key : key.toLowerCase())
  if (!validName.test(name)) throw new Error(`head: invalid attribute name ${JSON.stringify(key)}`)
  checkedNames.set(key, name)
  return name
}

// attributes writes the attributes the way React would: true as a bare attribute, false, null
// and undefined left out, everything else as an escaped string. Event handlers and other
// functions have no place in a server-written head and are left out too.
export function attributes(attrs: Record<string, unknown>): string {
  let out = ''
  for (const key in attrs) {
    const value = attrs[key]
    if (value === undefined || value === null || value === false || typeof value === 'function') continue
    const name = attributeName(key)
    out += value === true ? ' ' + name : ' ' + name + '="' + escapeAttribute(String(value)) + '"'
  }
  return out
}

export function tagHTML(tag: Tag): string {
  const open = '<' + tag.tag + attributes(tag.attrs) + '>'
  if (tag.tag === 'meta' || tag.tag === 'link') return open
  // Like React's dangerouslySetInnerHTML for style and script, text for title.
  const children = tag.children ?? ''
  return open + (tag.tag === 'title' ? escapeText(children) : children) + '</' + tag.tag + '>'
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
// tag and for the element the browser parsed from it, see elementKey. Two tags with the same key
// are written as the same HTML.
function tagKey(tag: Tag): string {
  let key = tag.tag
  for (const name in tag.attrs) {
    const value = tag.attrs[name]
    if (value === undefined || value === null || value === false || typeof value === 'function') continue
    key += '\n' + attributeName(name) + '=' + (value === true ? '' : String(value))
  }
  return key + '\n\n' + (tag.children ?? '')
}

function elementKey(element: Element): string {
  let key = element.localName
  for (const attr of Array.from(element.attributes)) key += '\n' + attr.name + '=' + attr.value
  return key + '\n\n' + (element.textContent ?? '')
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

// syncHead keeps the managed tags of document.head equal to headTags of the current matches.
// It compares the elements in place with the tags, so the head the server wrote stays as it is
// after hydration, and a tag both pages share, a stylesheet say, is never removed and added
// again on navigation.
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
  // A new element goes before the next kept one, so kept elements never move.
  let next: Node = end
  for (let i = tags.length - 1; i >= 0; i--) {
    const element = kept[i]
    if (element) {
      next = element
      continue
    }
    const created = document.createRange().createContextualFragment(tagHTML(tags[i]!))
    const first = created.firstChild!
    start.parentNode!.insertBefore(created, next)
    next = first
  }
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
