// Plays visit plans in Chromium against one server and writes what the browser sent to it, one JSON
// line per visit. Dynamic traffic only: documents and fetches, no scripts, styles, images or fonts.
// The plans are of the whole site: the storefront (/store and /zh-CN/store) and the back office
// (/projects). A request that carries a body is kept with the body the browser sent.
//
//   node record.mjs --base http://127.0.0.1:3000 --plans plans.json --out trace.jsonl [--workers 4] [--limit 100] [--only 3,17]
//   node record.mjs --base http://127.0.0.1:3000 --crawl pages.json --out links.json
//
// A plan that searches for a word of the list has variants, the same visit with other words: each is
// recorded as a visit of its own, and its line says which it is (variant, 0 for the visit of the
// plan). --only records the plans with those ids alone.
//
// --crawl opens each page of the list in pages.json (an array of URL paths) and writes, for each, the
// links on it that lead to another page of the list, as the href attribute reads, so that the plans
// of the store click links that exist.
//
// Each visit gets a fresh browser context (no cookies, no cache), a 1280x800 viewport, no hover (the
// pointer rests in the margin of the page but for its clicks, see REST) and real actions on the page:
// a step of a plan says what the visitor does (act), and the recorder does it with the control of the
// page that does it, so the browser sends what the page makes of it, a document load or a client
// navigation. Requests are timed from the start of their step, so that the replay keeps the order and
// the parallelism the browser had. A request the browser cancels is kept with when and why (cancel),
// so that the replay cancels it at the same offset instead of reading it to the end. Anything that
// goes wrong while recording a visit makes that visit's trace invalid: it is written with an error
// and left out.
import { chromium } from 'playwright'
import fs from 'node:fs'

const arg = (name, dflt) => {
  const i = process.argv.indexOf('--' + name)
  return i > 0 ? process.argv[i + 1] : dflt
}
const base = arg('base')
const crawl = arg('crawl')
const only = arg('only') ? new Set(arg('only').split(',').map(Number)) : null
// The visits to record: every plan, and after it each of its variants.
const visits = (crawl ? [] : JSON.parse(fs.readFileSync(arg('plans'), 'utf8')).plans)
  .filter((p) => !only || only.has(p.id))
  .flatMap((p) => [p.steps, ...(p.variants ?? [])].map((steps, variant) => ({ id: p.id, variant, steps })))
const workers = Number(arg('workers', '4'))
const limit = Math.min(Number(arg('limit', '0')) || visits.length, visits.length)
if (!base) throw new Error('--base is required')

// The sorts of the product list, in the order of the sort box (sorts in the shop's filter-schema.ts).
const SORTS = ['featured', 'newest', 'price-asc', 'price-desc', 'rating']

// sameURL says whether two URLs of the site are the same page as a server reads them: the same path,
// and the same query parameters with the same values, each name's values in the same order.
const sameURL = (a, b) => {
  const [ua, ub] = [new URL(a, base), new URL(b, base)]
  if (ua.pathname !== ub.pathname) return false
  const names = (u) => [...new Set(u.searchParams.keys())].sort()
  const [na, nb] = [names(ua), names(ub)]
  return na.length === nb.length && na.every((n, i) => n === nb[i] && JSON.stringify(ua.searchParams.getAll(n)) === JSON.stringify(ub.searchParams.getAll(n)))
}

// REST is where the pointer rests between the acts of a visit: the left edge of the window at half
// its height, in the margin of the page, where no page of the shop has a link or a control. Every
// page view checks that (resting). A click moves the pointer onto its target, as a hand does, and the
// pointer goes back to REST at once, before the page answers: left where it clicked, it would lie on
// whatever the new page draws there, and a router that preloads the link under a pointer would fetch
// a page that no act of the plan asked for.
const REST = { x: 1, y: 400 }
const rest = (page) => page.mouse.move(REST.x, REST.y)

// resting is what the pointer at REST lies on when that is a link or a control, and '' when, as it
// should, it is not.
const resting = (page) =>
  page.evaluate(({ x, y }) => {
    const on = 'a,button,input,select,textarea,label,summary,[onclick],[tabindex]:not([tabindex="-1"]),' + ['button', 'link', 'option', 'menuitem', 'tab', 'checkbox', 'radio', 'switch', 'combobox'].map((r) => `[role="${r}"]`).join(',')
    const hit = document.elementFromPoint(x, y)?.closest(on)
    return hit ? hit.outerHTML.slice(0, 120) : ''
  }, REST)

// act does what a step of a plan says on the page on screen (the acts of plan.go), and takes the
// pointer off the page as soon as the last click of the act is done.
//
//   link    a click on the first link to the page that the page shows
//   search  the word typed into the search box of the header, and Enter: a plain form
//   filter  a click on the plain link of the list that adds the condition (the category bar, the
//           quick filters, the price bands), which is the first link to the page in the list itself
//   page    a click on the link to the next page of the list, a plain link too
//   form    the conditions set in the form of the sidebar of the list, then its apply button
//   sort    the sort picked in the sort box of the list
async function act(page, step, i) {
  const none = (what) => new Error(`step ${i}: no ${what} on ${new URL(page.url()).pathname}${new URL(page.url()).search}`)
  const click = async (selector, what) => {
    const el = page.locator(selector).first()
    if ((await el.count()) === 0) throw none(what)
    await el.click()
  }
  // The form and the sort box work once the page has hydrated, which enables the apply button.
  const hydrated = () => page.locator('form:has(#filter-query) button[type="submit"]:enabled').first().waitFor({ timeout: 10000 })
  switch (step.act ?? 'link') {
    case 'link':
      await click(`a[href="${step.page}"]:visible`, `link to ${step.page}`)
      await rest(page)
      break
    case 'filter':
    case 'page':
      await click(`main a[href="${step.page}"]:visible`, `plain link to ${step.page}`)
      await rest(page)
      break
    case 'search': {
      const box = page.locator('form[role="search"] input[name="q"]:visible').first()
      if ((await box.count()) === 0) throw none('search box')
      await box.fill(step.term)
      await box.press('Enter')
      break
    }
    case 'form':
      await hydrated()
      for (const cond of step.set.split(',')) {
        const at = cond.indexOf(':')
        const [kind, value] = [cond.slice(0, at), cond.slice(at + 1)]
        const group = { category: 'category', rating: 'rating', colour: 'colors', material: 'materials', badge: 'badges' }[kind]
        if (kind === 'price') {
          const [lo, hi] = value.split('-')
          await page.locator('#filter-min').fill(lo)
          await page.locator('#filter-max').fill(hi)
        } else if (kind === 'stock') {
          await click('#filter-stock:enabled', 'in stock box')
        } else if (group) {
          await click(`#filter-${group}-${value}:enabled`, `${kind} ${value} in the form`)
        } else {
          throw new Error(`step ${i}: no control for the condition ${cond}`)
        }
      }
      await click('form:has(#filter-query) button[type="submit"]:enabled', 'apply button')
      await rest(page)
      break
    case 'sort': {
      if (!SORTS.includes(step.sort)) throw new Error(`step ${i}: no sort ${step.sort}`)
      await hydrated()
      await click('#sort-by', 'sort box')
      const option = page.locator('[role="option"]').nth(SORTS.indexOf(step.sort))
      await option.waitFor({ timeout: 5000 })
      await option.click()
      await rest(page)
      break
    }
    default:
      throw new Error(`step ${i}: no act ${step.act}`)
  }
  await page.waitForURL((u) => sameURL(u.pathname + u.search, step.page), { timeout: 10000 })
}

const keepHeader = /^(accept|rsc|next-router-.*|next-url|x-tsr-.*|x-react-router.*|purpose|sec-purpose|content-type)$/i
// within is p, or null when p takes longer than ms. The timer is dropped when p settles: a timer left
// to run keeps the process alive after the last visit, for as long as the longest limit.
const within = (ms, p) => {
  let timer
  const late = new Promise((r) => {
    timer = setTimeout(() => r(null), ms)
  })
  return Promise.race([p, late]).finally(() => clearTimeout(timer))
}
const QUIET_MS = 450
const CAP_MS = 6000
const NAV_WINDOW_MS = 200

const wanted = (req) => {
  if (!['document', 'fetch', 'xhr'].includes(req.resourceType())) return false
  const path = new URL(req.url()).pathname
  return !(path.startsWith('/_next/') || path.startsWith('/assets/') || path.includes('favicon'))
}

async function visit(browser, plan) {
  const context = await browser.newContext({ viewport: { width: 1280, height: 800 } })
  const page = await context.newPage()
  const steps = []
  const recs = new Map() // request -> record
  let cur = null
  let over = false // the visit is over: events of the closing context mean nothing
  let inflight = 0
  let lastActivity = Date.now()
  const navAt = []
  const headerJobs = []
  page.on('framenavigated', () => navAt.push(Date.now()))
  page.on('request', (req) => {
    if (over || !wanted(req) || !cur) return
    const step = cur
    // Prefetches do not hold the page back: the browser may keep one open for a long time.
    const ahead = Boolean(req.headers()['next-router-prefetch'])
    const rec = {
      method: req.method(),
      url: req.url().replace(new URL(req.url()).origin, ''),
      headers: Object.fromEntries(Object.entries(req.headers()).filter(([k]) => keepHeader.test(k))),
      inm: false,
      s: Date.now() - step.t0,
      status: 0,
      class: req.resourceType() === 'document' ? 'doc' : ahead ? 'prefetch' : 'nav',
      step,
      ahead,
      open: true,
    }
    const body = req.postData()
    if (body) rec.body = body
    if (!ahead) inflight++
    lastActivity = Date.now()
    recs.set(req, rec)
    step.reqs.push(rec)
  })
  const close = (rec) => {
    if (!rec.open) return false
    rec.open = false
    if (!rec.ahead) inflight--
    lastActivity = Date.now()
    rec.e = Date.now() - rec.step.t0
    return true
  }
  const headers = (req, rec) =>
    headerJobs.push(
      within(3000, req.allHeaders().catch(() => ({}))).then((all) => {
        if (!all) return
        // Add to the headers seen when the request was made: when the browser's own list cannot be
        // had, the router headers must not be lost.
        Object.assign(rec.headers, Object.fromEntries(Object.entries(all).filter(([k]) => keepHeader.test(k))))
        rec.inm = 'if-none-match' in all
        if (all['next-router-prefetch'] || /prefetch/i.test(all['sec-purpose'] ?? '')) rec.class = 'prefetch'
      }),
    )
  page.on('response', (res) => {
    const rec = recs.get(res.request())
    if (rec && rec.responded === undefined) {
      rec.responded = Date.now() - rec.step.t0
      rec.status = res.status()
    }
  })
  page.on('requestfinished', (req) => {
    const rec = recs.get(req)
    if (rec && close(rec)) headers(req, rec)
  })
  page.on('requestfailed', (req) => {
    const rec = recs.get(req)
    if (!rec || over || !close(rec)) return
    rec.cancel = req.failure()?.errorText || 'failed'
    const t = Date.now()
    rec.afterNav = navAt.some((n) => t - n >= 0 && t - n <= NAV_WINDOW_MS)
    headers(req, rec)
  })
  const settle = async () => {
    const start = Date.now()
    while (Date.now() - start < CAP_MS) {
      await page.waitForTimeout(100)
      // The quiet is counted within this page view: a click whose requests come a little later (a
      // client router that renders first) must not settle on the quiet of the page view before.
      if (inflight === 0 && Date.now() - Math.max(lastActivity, cur.t0) >= QUIET_MS) break
    }
    // A request the browser has not finished by now (a prefetch it left open) is cancelled with the
    // page view, as far as this recording goes.
    for (const rec of cur.reqs) {
      if (rec.open) {
        close(rec)
        rec.cancel = 'open-at-end'
      }
    }
    await within(3000, Promise.allSettled(headerJobs.splice(0)))
  }
  const begin = (kind, target) => {
    cur = { kind, page: target, t0: Date.now(), reqs: [] }
    steps.push(cur)
  }
  try {
    for (const [i, step] of plan.steps.entries()) {
      begin(step.kind, step.page)
      if (step.kind === 'load') {
        await page.goto(base + step.page)
        await rest(page)
      } else {
        await act(page, step, i)
      }
      await settle()
      const on = await resting(page)
      if (on) throw new Error(`step ${i}: the pointer rests on ${on}`)
      // The page shown must be the one of the plan, as a server reads its URL. A URL that is the same
      // page and is not written the same (another order of the parameters, another escape) is kept with
      // the step, so that the difference can be counted.
      const shown = ((u) => u.pathname + u.search)(new URL(page.url()))
      if (!sameURL(shown, step.page)) throw new Error(`step ${i}: shows ${shown}, want ${step.page}`)
      if (shown !== step.page) cur.shown = shown
    }
    over = true
    // A request the page started after its page view settled (while settle waited for headers,
    // such as the storefront's POST of the product looked at, once Start has hydrated) and that is
    // still open now is cancelled with the visit, as settle does with the requests it finds open.
    for (const s of steps) {
      for (const rec of s.reqs) {
        if (rec.open) {
          close(rec)
          rec.cancel = 'open-at-end'
        }
      }
    }
    await within(3000, Promise.allSettled(headerJobs.splice(0)))
    const done = steps.map((s) => ({
      kind: s.kind,
      page: s.page,
      ...(s.shown ? { shown: s.shown } : {}),
      reqs: s.reqs
        .map(({ step, ahead, open, ...r }) => r)
        .sort((a, b) => a.s - b.s),
    }))
    for (const s of done) for (const r of s.reqs) if (r.e === undefined) throw new Error(`a request of ${s.page} never ended`)
    return { plan: plan.id, variant: plan.variant, steps: done }
  } catch (e) {
    over = true
    return { plan: plan.id, variant: plan.variant, error: String(e).slice(0, 300) }
  } finally {
    await context.close()
  }
}

const browser = await chromium.launch()
if (crawl) {
  const pages = JSON.parse(fs.readFileSync(crawl, 'utf8'))
  const known = new Set(pages)
  // The links of a page that every visitor sees, whatever they did before: those of a visitor with
  // nothing behind them and those of two who looked at other products and changed the cart (the "seen"
  // products, the products suggested for the first line of the cart). A link in only one of them
  // depends on the visitor and is left out, like a link to the other orders of a customer on the
  // workbench. Only links in the document the server sends count: a list that adds products as the
  // page runs has links a visit that arrives by a click does not see.
  // Two visitors with other history: what they looked at and the one line they leave in the cart.
  const histories = [
    { seen: ['wax-cloth', 'copper-cups', 'hinoki-spray'], first: { slug: 'camp-lantern', color: 'brass', size: '' } },
    { seen: ['oak-tray', 'tea-towels', 'glass-carafe'], first: { slug: 'walnut-pen', color: 'charcoal', size: '' } },
  ]
  const linksOf = async (url, history) => {
    const context = await browser.newContext({ viewport: { width: 1280, height: 800 } })
    try {
      if (history) {
        const store = url.startsWith('/zh-CN') ? '/zh-CN/store' : '/store'
        const page = await context.newPage()
        for (const slug of history.seen) {
          await page.goto(`${base}${store}/products/${slug}`, { waitUntil: 'networkidle' })
        }
        const send = async (method, path, data) => {
          const res = await context.request.fetch(base + path, { method, data, headers: { 'content-type': 'application/json' } })
          if (!res.ok()) throw new Error(`${method} ${path}: ${res.status()}`)
        }
        for (const line of ['pour-over-kettle.mist', 'ceramic-cup.moss', 'linen-apron.oat.M', 'reading-candle.charcoal']) {
          await send('PATCH', `${store}/cart`, { line, quantity: 0 })
        }
        await send('POST', `${store}/products/${history.first.slug}`, { color: history.first.color, size: history.first.size, quantity: 1 })
        await page.close()
      }
      const page = await context.newPage()
      await page.goto(base + url, { waitUntil: 'networkidle' })
      const hrefs = await page.$$eval('a[href]', (as) => as.map((a) => a.getAttribute('href')))
      const html = await (await context.request.get(base + url)).text()
      const found = new Set()
      for (const h of new Set(hrefs)) {
        if (known.has(h) && h !== url && html.includes(`href="${h.replaceAll('&', '&amp;')}"`) && (await page.locator(`a[href="${h}"]:visible`).count()) > 0) found.add(h)
      }
      return found
    } finally {
      await context.close()
    }
  }
  const links = {}
  let dropped = 0
  for (const url of pages) {
    const fresh = await linksOf(url, null)
    const others = []
    for (const h of histories) others.push(await linksOf(url, h))
    links[url] = [...fresh].filter((h) => others.every((o) => o.has(h)))
    dropped += fresh.size - links[url].length
  }
  await browser.close()
  fs.writeFileSync(arg('out'), JSON.stringify(links, null, 1) + '\n')
  console.error(`${pages.length} pages crawled, ${dropped} links left out because a visitor's history changes them`)
  process.exit(0)
}
const out = fs.createWriteStream(arg('out'))
let next = 0
let bad = 0
let done = 0
await Promise.all(
  Array.from({ length: workers }, async () => {
    for (;;) {
      const i = next++
      if (i >= limit) return
      const v = (await within(120000, visit(browser, visits[i]))) ?? { plan: visits[i].id, variant: visits[i].variant, error: 'timeout' }
      if (v.error) {
        bad++
        console.error(`plan ${v.plan} variant ${v.variant}: ${v.error}`)
      }
      out.write(JSON.stringify(v) + '\n')
      if (++done % 25 === 0) console.error(`${done}/${limit} visits, ${bad} failed`)
    }
  }),
)
await browser.close()
await new Promise((r) => out.end(r))
console.error(`${done} visits, ${bad} failed`)
if (bad > done * 0.02) process.exit(1)
