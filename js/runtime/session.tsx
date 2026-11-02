// The session in the browser. The server sends it as {signedIn, data?} with a whole page, with
// every navigation, with a call that changed it and from /__tifa/session (SessionEnvelope). The
// browser keeps one copy in memory and mirrors it in localStorage, so that a page the server
// answers for every visitor alike can show who is signed in from the first paint (the script
// the server puts at the start of the head reads the mirror, see sessionHead in server/sessionwire.go).
// The copy only decides what to show. It is never used to decide what someone may do.

import {
  Children,
  cloneElement,
  createContext,
  isValidElement,
  useContext,
  useSyncExternalStore,
  type ReactElement,
  type Ref,
} from 'react'
import type { SessionData, SessionEnabled } from '../session.gen'
import { buildHeader } from './data'
import { reloadForBuild } from './skew'

// SessionEnvelope is the session as the server sends it. data is what the Exposed method of the
// session type returned, left out when the type has none.
export interface SessionEnvelope {
  signedIn: boolean
  data?: Json
}

// Json is what the server sends: the router carries a value to the browser only when its type says
// it is JSON.
export type Json = string | number | boolean | null | Json[] | { [key: string]: Json }

// Snapshot is what useSession returns. ready tells that the browser has settled which session it
// shows: at once for a page with the session in it, after the hydration for a page without.
export interface Snapshot {
  signedIn: boolean
  data: Json | undefined
  ready: boolean
}

// The path of the session answer, and the attribute the server and this module keep on <html>.
export const sessionPath = '/__tifa/session'
const attributeName = 'data-session'

let key: string | undefined
let current: Snapshot = { signedIn: false, data: undefined, ready: false }
let injectedPage = false
let issued = 0
let applied = 0
let build: () => string = () => ''
const listeners = new Set<() => void>()

// isEnvelope reports whether value is {signedIn: boolean, data?}.
function isEnvelope(value: unknown): value is SessionEnvelope {
  return typeof value === 'object' && value !== null && typeof (value as SessionEnvelope).signedIn === 'boolean'
}

// initialSnapshot is what a page shows until the browser has settled, and what the server renders:
// the injected session, or someone signed out for a page that carries none.
export function initialSnapshot(envelope: SessionEnvelope | undefined): Snapshot {
  if (envelope === undefined) return { signedIn: false, data: undefined, ready: false }
  return { signedIn: envelope.signedIn, data: envelope.signedIn ? envelope.data : undefined, ready: true }
}

// startSession begins the session of a page in the browser. key is where the mirror lives, injected
// the session the page carries (none for a page the server answers for every visitor alike), and
// buildId the build the page was rendered with. It returns the snapshot the hydration renders.
export function startSession(options: { key: string; injected?: SessionEnvelope; buildId: () => string }): Snapshot {
  key = options.key
  build = options.buildId
  issued = 0
  applied = 0
  listeners.clear()
  injectedPage = options.injected !== undefined
  current = { signedIn: false, data: undefined, ready: false }
  if (isEnvelope(options.injected)) apply(options.injected, true)
  return current
}

// beginSession numbers a request that may bring a session, as it is sent.
export function beginSession(): number {
  return ++issued
}

// acceptSession takes the session of the answer to the request numbered seq. An answer that has no
// session leaves the copy alone, and so does one to a request sent before one already accepted.
export function acceptSession(seq: number, value: unknown) {
  if (!isEnvelope(value) || seq < applied) return
  applied = seq
  apply(value, current.ready)
}

function apply(envelope: SessionEnvelope, ready: boolean) {
  const data = envelope.signedIn ? envelope.data : undefined
  writeMirror(envelope.signedIn, data)
  writeAttribute(envelope.signedIn)
  if (current.signedIn === envelope.signedIn && current.ready === ready && JSON.stringify(current.data) === JSON.stringify(data)) return
  current = { signedIn: envelope.signedIn, data, ready }
  for (const listener of [...listeners]) listener()
}

function setReady() {
  if (current.ready) return
  current = { ...current, ready: true }
  for (const listener of [...listeners]) listener()
}

// settleSession switches a page that came without a session to the copy of the browser, once it has
// hydrated. A copy that says someone is signed in is checked with the server once, in the
// background. A page with the session in it, or one that has heard from the server already, is
// settled as it is. When the mirror cannot be read the copy cannot be trusted to be empty, so the
// server is asked then too.
export function settleSession() {
  if (key === undefined || current.ready) return
  if (injectedPage || applied > 0) return setReady()
  const mirror = readMirror()
  if (mirror === 'unavailable') {
    setReady()
    void refreshSession()
    return
  }
  if (mirror === undefined) return setReady()
  current = { signedIn: true, data: mirror.data, ready: true }
  writeAttribute(true)
  for (const listener of [...listeners]) listener()
  void refreshSession()
}

// refreshSession asks the server for the session and takes the answer.
export async function refreshSession() {
  const seq = beginSession()
  const headers: Record<string, string> = { Accept: 'application/json' }
  const id = build()
  if (id !== '') headers[buildHeader] = id
  try {
    const response = await fetch(sessionPath, { headers })
    if (response.status === 409 && response.headers.has(buildHeader)) {
      reloadForBuild(response.headers.get(buildHeader)!)
      return
    }
    if (!response.ok) return
    acceptSession(seq, await response.json())
  } catch {
    // The network or a proxy failed. The copy stays as it is until the next answer with a session.
  }
}

// readMirror reads the copy in localStorage: the envelope of someone signed in, undefined when there
// is none, 'unavailable' when the storage cannot be read or holds something else.
function readMirror(): SessionEnvelope | undefined | 'unavailable' {
  try {
    const text = window.localStorage.getItem(key!)
    if (text === null) return undefined
    const value: unknown = JSON.parse(text)
    return isEnvelope(value) && value.signedIn ? value : 'unavailable'
  } catch {
    return 'unavailable'
  }
}

function writeMirror(signedIn: boolean, data: Json | undefined) {
  if (key === undefined || typeof window === 'undefined') return
  try {
    if (signedIn) window.localStorage.setItem(key, JSON.stringify(data === undefined ? { signedIn } : { signedIn, data }))
    else window.localStorage.removeItem(key)
  } catch {
    // localStorage is blocked or full, as in a private window. The copy in memory is all there is.
  }
}

function writeAttribute(signedIn: boolean) {
  if (typeof document === 'undefined') return
  const html = document.documentElement
  html.setAttribute(attributeName, signedIn ? 'in' : 'out')
}

export function subscribeSession(listener: () => void) {
  listeners.add(listener)
  return () => void listeners.delete(listener)
}

export function sessionSnapshot(): Snapshot {
  return current
}

// SessionContext holds the snapshot the server rendered, and the hydration renders: the page
// uses it as the server snapshot of useSession. It is undefined for an app without sessions.
export const SessionContext = createContext<Snapshot | undefined>(undefined)

export interface SessionResult {
  signedIn: boolean
  // What the Exposed method of the session type returns, undefined when the type has none or
  // nobody is signed in.
  data: SessionData | undefined
  // true once the browser has settled which session it shows.
  ready: boolean
}

// useSession returns who is signed in. It is a type error in an app that never calls
// tifa.SetSession, and throws there.
export function useSession(
  ...enabled: SessionEnabled extends true ? [] : [error: 'the app never calls tifa.SetSession, so it has no session']
): SessionResult {
  void enabled
  const rendered = useContext(SessionContext)
  if (rendered === undefined) throw new Error('useSession needs a session: call tifa.SetSession somewhere in the app')
  return useSyncExternalStore(subscribeSession, sessionSnapshot, () => rendered) as SessionResult
}

type Marked = { ref?: Ref<unknown>; 'data-session'?: string }

// mark gives the only child of SignedIn or SignedOut the attribute data-session, so the style at
// the start of the head hides the wrong one from the first paint, before any script of the page
// has run. The components add no node of their own. The attribute is private to tifa.
function mark(name: string, value: 'in' | 'out', children: ReactElement): ReactElement {
  const child: unknown = Children.only(children)
  if (!isValidElement<Marked>(child)) throw new Error(`${name} needs one React element as its child`)
  if (child.props['data-session'] !== undefined) {
    throw new Error(`${name} sets data-session on its child itself: remove data-session from the child`)
  }
  const props: Marked = { 'data-session': value }
  if (import.meta.env.DEV) props.ref = checkedRef(name, value, child.props.ref)
  return cloneElement(child, props)
}

// checkedRef, in development only, looks at the DOM element once it is mounted. A child component
// that does not pass its props on to an element would lose the attribute, and the wrong block
// would show to everyone. It runs the ref the child had as well. Refs do not run while rendering
// on the server, and a production build has none of this.
function checkedRef(name: string, value: string, inner: Ref<unknown> | undefined): Ref<unknown> {
  return (node: unknown) => {
    // A class instance or a useImperativeHandle handle is not an element: nothing to check there.
    const el = node as Element | null
    if (el !== null && typeof el.getAttribute === 'function' && el.getAttribute(attributeName) !== value) {
      throw new Error(
        `${name}: the DOM element of its child has no data-session. The child component must pass its props on to the DOM element (a function component takes ...props and spreads them), or wrap it in an element such as <div>.`,
      )
    }
    if (typeof inner === 'function') return inner(node)
    if (inner) inner.current = node
  }
}

// SignedIn shows its child to someone signed in. Its child is one element, which gets the attribute.
export function SignedIn({ children }: { children: ReactElement }) {
  return mark('SignedIn', 'in', children)
}

// SignedOut shows its child to someone who is not signed in.
export function SignedOut({ children }: { children: ReactElement }) {
  return mark('SignedOut', 'out', children)
}
