import type { ReactNode } from 'react'
import { prerenderToString } from 'react-dom/server.edge'
import { RouterProvider, type AnyRoute } from '@tanstack/react-router'
import { attachRouterServerSsrUtils } from '@tanstack/react-router/ssr/server'
import { askedByLoad, createAppRouter } from './router'
import { initialSnapshot, SessionContext, type SessionEnvelope } from './session'
import { LanguageContext } from './language'
import { AlternatesContext } from './alternates'
import { injected, rootElementId, RouteError, type Injected, type InjectedResult, type Source } from './data'
import {
  attributes,
  defaultHTMLAttributes,
  headEnd,
  headHTML,
  headStart,
  alternateTags,
  headTags,
  htmlAttributesOf,
  type HTMLAttributes,
} from './head'

// The tags that load the browser bundle, its stylesheets and scripts, written into the head as
// they are. tifa build defines it when it builds the SSR bundle.
declare const __TIFA_ASSETS__: string

declare global {
  var startRender: (json: string, ...texts: string[]) => void
  var renderResult: string | undefined
  var stopRender: (() => void) | undefined
}

// prerenderToString is what the tifa Vite plugin adds to React DOM's string renderer: it renders
// what renderToString renders, but waits for the components that suspend on work the jobs of the
// VM settle, such as a React.lazy component of a module in the bundle, and writes them in place.
// Its signal ends the render as renderToString ends it. Each call reports to its own onError.
declare module 'react-dom/server.edge' {
  function prerenderToString(
    node: ReactNode,
    options: { onError: (error: unknown) => void; signal: AbortSignal },
  ): Promise<string>
}

// clientRenderedBoundary is what React writes for a Suspense boundary that failed on the
// server and is left to the browser.
const clientRenderedBoundary = '<!--$!-->'
const incompleteRender =
  'a component threw during the server render and React left its Suspense boundary to the browser, ' +
  'set SSR = false on the page to see the error in the browser console'
const suspendedRender =
  'a component suspended during the server render: it threw a promise, or it is a React.lazy component, that ' +
  'never settled. The server has no timers and does no I/O, so the render waits only for what its jobs settle, ' +
  'and React left its Suspense boundary to the browser. Give the page its data with Load and import the component ' +
  'statically, or set SSR = false on the page'

// suspended reports whether error is how renderToString reports a boundary that suspended: the
// reason it aborts the boundary with, which advises a stream renderer tifa does not use.
// prerenderToString gives the same reason when it stops.
function suspended(error: unknown) {
  return typeof error === 'string' && error.startsWith('The server used "renderTo')
}

// RenderResult is an InjectedResult as the Go VM pool gives it to a server render: the JSON text of
// the data of a 200 is not in it. It is the argument of startRender that text numbers, counting
// from the first one after the input, so the text is neither escaped into the JSON of the input
// nor parsed out of it. A page that skips SSR carries the text in json instead. page is true when
// the page carries another text than the render reads: the text with a marker in place of each
// tifa.HTML value, whose markup the page holds already (see HTML in html.tsx). Such a text always
// goes into the page as a slot, which Go fills with that other text.
export type RenderResult = Omit<InjectedResult, 'json' | 'slot'> & { text?: number; page?: boolean }

// RenderInput is what the Go VM pool renders a page from: the results of the Loads, and the module
// preloads of the matched Layouts and route, which follow the tags of the browser entry.
export type RenderInput = Omit<Injected, 'data'> & {
  data: Record<string, RenderResult>
  preload?: string
  // The nonce of the render, which Go draws for it. The page carries a slot, a ~, the nonce and the number
  // of the text, where the JSON text of a Load's data goes, and Go fills it (internal/ssr/slots.go).
  nonce?: string
  // Set when the page goes out under a Content-Security-Policy: the scripts and styles the render
  // writes carry the nonce as their first attribute, the router's through its ssr option, and Go
  // takes it out again and hashes their content (internal/ssr/csp.go).
  csp?: boolean
  // The length from which a text goes into the page as a slot, slotMin below unless a test sets it.
  slotMin?: number
  // For an app with sessions: where the browser keeps its copy, the session of the request (none
  // for a page the server answers for every visitor alike), and the script and the style that go
  // at the start of the head, written by Go (sessionHead in server/sessionwire.go).
  sessionKey?: string
  session?: SessionEnvelope
  sessionHead?: string
  // An error the page shows in place of what failed at a level of its matches, set when a render of
  // the page failed (see placed).
  error?: PlacedError
}

// PlacedError is an error a render shows at a level of the matches: level 0 is the match that
// shows what the URL matched, the first that did not load or the last one, each level above it one
// match further out. The match there renders its errorComponent with a RouteError of the status
// and message.
export type PlacedError = { status: number; message: string; level: number }

// shownAt is the index of the match that shows what the URL matched, as headTags reads it.
function shownAt(matches: ReadonlyArray<{ status: string; _notFound?: boolean }>) {
  const i = matches.findIndex((match) => match.status !== 'success' || match._notFound)
  return i < 0 ? matches.length - 1 : i
}

// slotMin is the length, in code units, from which the JSON text of a Load's data goes into the page
// as a slot. Go fills a slot by copying the page once more, which a shorter text does not pay back:
// writing a text in the VM takes about 30 ns for each code unit, and the copy of a page of 130 KB
// about 60 µs.
const slotMin = 4096

// renderSource answers from the data of a server render. Each result that numbers a text gets
// that text of texts, the arguments of startRender after the input, as its json. A number with no
// text fails the render. The text of a result marked page goes into the page as a slot however
// short it is.
function renderSource(input: RenderInput, texts: string[]): Source {
  const data: Record<string, InjectedResult> = {}
  for (const [id, { text, page, ...result }] of Object.entries(input.data)) {
    if (text === undefined) {
      data[id] = result
      continue
    }
    const json = texts[text]
    if (json === undefined) throw new Error(`render input has no text ${text} for route ${id}`)
    const slotted = input.nonce !== undefined && (page === true || json.length >= (input.slotMin ?? slotMin))
    data[id] = slotted ? { ...result, json, slot: `~${input.nonce}.${text}.` } : { ...result, json }
  }
  return injected({ url: input.url, buildId: input.buildId, data })
}

// carried is what the page hands to the browser of the versions: the site and the default language,
// and the list only when a Load of the page set it. The browser works the rest out.
function carried(a: RenderInput['alternates']): RenderInput['alternates'] {
  if (!a) return undefined
  return { site: a.site, default: a.default, ...(a.custom ? { list: a.list } : {}) }
}

// pageHead is the tags of the head: those of the matched routes, then the links of the versions of
// the page in other languages.
function pageHead(matches: Parameters<typeof headTags>[0], input: RenderInput) {
  const own = headTags(matches)
  return own.concat(alternateTags(own, input.alternates, input.alternates?.site ?? ''))
}

// canonicalStatus is the status of a redirect the router threw. The router throws one for a URL it
// spells differently (/%61 for /a, /a/ for /a) with the temporary status 307. That URL is only another
// spelling of the page, so it answers 308: the permanent redirect that keeps the method and that
// search engines take as the sign of the canonical URL. A redirect a Load asked for keeps its status.
function canonicalStatus(redirect: Response) {
  const asked = (redirect as { options?: Record<string, unknown> }).options?.[askedByLoad]
  return asked ? redirect.status : 308
}

// serve installs startRender, which the Go VM pool calls with the JSON of a RenderInput and, as
// the arguments after it, the JSON text of the data of each successful Load, which the results
// of the input number. It leaves the HTML, or the JSON of {"redirect": {location, status}} or
// {"error": ..., "levels": n}, in renderResult once the queued jobs have run, n being how many
// levels of the matches could show the error (see PlacedError), 0 when the render failed before
// the router matched the URL. While React waits for a
// component that suspended, stopRender is set: the pool calls it when no job is left, as nothing
// could settle what React waits for, and the render ends with the boundaries still waiting left to
// the browser. The router redirects a URL it spells differently, such as /%61 for /a. Go answers
// redirects of Loads itself before it renders.
//
// React renders only the application root. The document around it, the attributes of <html>
// and the head from the head functions of the matched routes, the assets and the hydration
// scripts are written as strings, so a page that failed at any level still comes as a whole
// document.
export function serve(routeTree: AnyRoute, htmlAttributes: HTMLAttributes = defaultHTMLAttributes) {
  // data-session is "in" or "out" on a page that carries the session, and absent on a page that
  // carries none: only that page lets the head script read the copy in the browser.
  // htmlAttributesOf refuses a data-session of the app.
  const documentStart = (attrs: HTMLAttributes, input?: RenderInput) =>
    '<!DOCTYPE html><html' +
    (input?.session ? (input.session.signedIn ? ' data-session="in"' : ' data-session="out"') : '') +
    attributes(attrs) +
    '><head><meta charset="utf-8">' +
    (input?.sessionHead ?? '')
  // The root Layout's attributes fail here, when the bundle loads, rather than in every render.
  documentStart(htmlAttributesOf([], htmlAttributes))
  const assetTags = __TIFA_ASSETS__
  const bodyStart = headEnd + '</head><body><div id="' + rootElementId + '">'
  globalThis.startRender = (json: string, ...texts: string[]) => {
    const input: RenderInput = JSON.parse(json)
    const nonce = input.csp ? input.nonce : undefined
    const router = createAppRouter(routeTree, renderSource(input, texts), {
      buildId: input.buildId,
      serverURL: input.url,
      sessionKey: input.sessionKey,
      session: input.session,
      language: input.language,
      alternates: carried(input.alternates),
      nonce,
    })
    attachRouterServerSsrUtils({ router, manifest: undefined })
    globalThis.renderResult = undefined
    let levels = 0
    void (async () => {
      try {
        await router.load()
        const result = router._serverResult
        if (result?.type === 'redirect') {
          globalThis.renderResult = JSON.stringify({
            redirect: { location: result.redirect.headers.get('Location'), status: canonicalStatus(result.redirect) },
          })
          return
        }
        const shown = shownAt(router.stores.matches.get())
        levels = shown + 1
        if (input.error) await placed(router, shown, input.error)
        undated(router)
        await router.serverSsr!.dehydrate()
        const errors: unknown[] = []
        const stop = new AbortController()
        globalThis.stopRender = () => stop.abort()
        const rendered = input.sessionKey === undefined ? undefined : initialSnapshot(input.session)
        const root = await prerenderToString(
          <LanguageContext.Provider value={input.language ?? ''}>
            <AlternatesContext.Provider value={input.alternates?.list ?? []}>
              <SessionContext.Provider value={rendered}>
                <RouterProvider router={router} />
              </SessionContext.Provider>
            </AlternatesContext.Provider>
          </LanguageContext.Provider>,
          {
            onError: (error) => errors.push(error),
            signal: stop.signal,
          },
        )
        globalThis.stopRender = undefined
        // React swallows an error thrown inside a Suspense boundary, and the router
        // wraps the routes in one: the boundary renders its fallback and leaves the marker of a
        // boundary the browser must render itself. The page is then incomplete, never a success.
        // The first error a component threw is why, if the plugin could pass it, or else that a
        // component suspended. Only the marker tells for sure: a component that asks for the
        // browser with use(browser()) leaves its boundary without calling onError.
        if (root.includes(clientRenderedBoundary)) {
          const thrown = errors.filter((error) => !suspended(error))
          if (thrown.length > 0) throw thrown[0]
          throw new Error(errors.length > 0 ? suspendedRender : incompleteRender)
        }
        const hydration = router.serverSsr!.takeInitialHydrationScriptTags()
        if (!hydration) throw new Error('the router has no hydration scripts for this render')
        router.serverSsr!.setRenderFinished()
        const matches = router.stores.matches.get()
        const html =
          documentStart(htmlAttributesOf(matches, htmlAttributes, input.language), input) +
          assetTags +
          (input.preload ?? '') +
          headStart +
          headHTML(pageHead(matches, input), nonce) +
          bodyStart +
          root +
          '</div>' +
          scriptsHTML(hydration.before) +
          scriptsHTML([hydration.boundary]) +
          '</body></html>'
        globalThis.renderResult = html
      } catch (error) {
        globalThis.renderResult = JSON.stringify({
          error:
            error instanceof Error
              ? `${error.name}: ${error.message}\n${error.stack}`
              : String(error),
          levels,
        })
      } finally {
        globalThis.stopRender = undefined
        router.serverSsr!.cleanup()
      }
    })()
  }
}

// undated sets the time each match loaded at to 0 before the router writes the matches into the
// page. The router stamps a match with Date.now() and dehydrates the stamp (u in dehydrateMatch of
// ssr-server.js), so two renders of the same input would differ by the time, and the render cache
// would give the time of the first render to every later visitor. The page is then a function of
// its input. The browser reads 0 as data of unknown age, and dates the matches by the age of the
// data that Go sends with the page (datePage in browser.tsx). router-core has no API to change a
// match, so the matches go back to the store as copies, as placed does.
function undated(router: ReturnType<typeof createAppRouter>) {
  router.stores.setMatches(router.stores.matches.get().map((match) => ({ ...match, updatedAt: 0 })))
}

// placed makes the match at error.level above the shown one fail with the error, so that it renders
// its errorComponent in its place and the browser hydrates it so. The matches below it stay as they
// loaded, and nothing renders them. The head follows: the match that fails is a route whose Load
// failed, so it has no data, and the head functions run again over the matches as they are now,
// exactly as the router runs them for a page whose Load fails, and as the browser runs them when it
// hydrates the page. The tags the head functions gave before the error do not stay, and the routes
// below the match give none (see presented in head.ts).
async function placed(router: ReturnType<typeof createAppRouter>, shown: number, error: PlacedError) {
  const at = shown - error.level
  if (error.level < 0 || at < 0) throw new Error(`the matches have no level ${error.level} to show the error at`)
  const matches = router.stores.matches.get().map((match, i) =>
    i === at
      ? { ...match, status: 'error' as const, error: new RouteError(error.status, error.message), loaderData: undefined, _notFound: undefined }
      : i < at
        ? { ...match }
        : match,
  )
  // The router's own projectLane, in load-server.ts and load-client.ts: it stops after the first
  // match that did not load, the one that fails here. It sets the head on copies, so the store keeps
  // its matches until setMatches.
  for (const match of matches.slice(0, at + 1)) {
    const route = router.routesById[match.routeId]
    if (!route.options.head) continue
    try {
      const head = await route.options.head({
        ssr: router.options.ssr,
        matches,
        match,
        params: match.params,
        loaderData: match.loaderData,
      } as never)
      match.meta = head?.meta
      match.links = head?.links
      match.headScripts = head?.scripts
      match.styles = head?.styles
    } catch (cause) {
      console.error(cause)
    }
  }
  router.stores.setMatches(matches)
}

// scriptsHTML writes the router's hydration scripts as React would: their source as it is.
// The router escapes the data in them so that no value can end the script element.
function scriptsHTML(tags: Array<{ attrs?: Record<string, unknown>; children?: string }>) {
  let out = ''
  for (const tag of tags) out += '<script' + attributes(tag.attrs ?? {}) + '>' + (tag.children ?? '') + '</script>'
  return out
}
