// Plays visit plans in Chromium against one server and writes what the browser sent to it, one JSON
// line per visit. Dynamic traffic only: documents and fetches, no scripts, styles, images or fonts,
// and nothing outside the area being recorded: the workbench (/projects) or the store (/store and
// /zh-CN/store). A request that carries a body is kept with the body the browser sent.
//
//   node record.mjs --base http://127.0.0.1:3000 --plans plans.json --out trace.jsonl [--workers 4] [--limit 100] [--area workbench|store]
//   node record.mjs --base http://127.0.0.1:3000 --crawl pages.json --out links.json
//
// --crawl opens each page of the list in pages.json (an array of URL paths) and writes, for each, the
// links on it that lead to another page of the list, as the href attribute reads, so that the plans
// of the store click links that exist.
//
// Each visit gets a fresh browser context (no cookies, no cache), a 1280x800 viewport, no hover (the
// mouse rests in a corner between steps) and real clicks on the links of the page. Requests are
// timed from the start of their step, so that the replay keeps the order and the parallelism the
// browser had. A request the browser cancels is kept with when and why (cancel), so that the replay
// cancels it at the same offset instead of reading it to the end. Anything that goes wrong while
// recording a visit makes that visit's trace invalid: it is written with an error and left out.
import { chromium } from 'playwright'
import fs from 'node:fs'

const arg = (name, dflt) => {
  const i = process.argv.indexOf('--' + name)
  return i > 0 ? process.argv[i + 1] : dflt
}
const base = arg('base')
const area = arg('area', 'workbench')
if (!['workbench', 'store'].includes(area)) throw new Error('--area is workbench or store')
const crawl = arg('crawl')
const plans = crawl ? [] : JSON.parse(fs.readFileSync(arg('plans'), 'utf8')).plans
const workers = Number(arg('workers', '4'))
const limit = Number(arg('limit', String(plans.length)))
if (!base) throw new Error('--base is required')

const keepHeader = /^(accept|rsc|next-router-.*|next-url|x-tsr-.*|x-react-router.*|purpose|sec-purpose|content-type)$/i
const within = (ms, p) => Promise.race([p, new Promise((r) => setTimeout(() => r(null), ms))])
const QUIET_MS = 450
const CAP_MS = 6000
const NAV_WINDOW_MS = 200

const inStore = (path) => /^(\/zh-CN)?\/store(\/|$)/.test(path)
const wanted = (req) => {
  if (!['document', 'fetch', 'xhr'].includes(req.resourceType())) return false
  const path = new URL(req.url()).pathname
  if (path.startsWith('/_next/') || path.startsWith('/assets/') || path.includes('favicon')) return false
  return area === 'store' ? inStore(path) : !inStore(path)
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
      } else {
        const link = page.locator(`a[href="${step.page}"]:visible`).first()
        if ((await link.count()) === 0) throw new Error(`step ${i}: no link to ${step.page} on ${new URL(page.url()).pathname}`)
        await link.click()
        await page.waitForURL((u) => u.pathname + u.search === step.page, { timeout: 10000 })
      }
      await settle()
      await page.mouse.move(1, 1)
      const shown = ((u) => u.pathname + u.search)(new URL(page.url()))
      if (shown !== step.page) throw new Error(`step ${i}: shows ${shown}, want ${step.page}`)
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
      reqs: s.reqs
        .map(({ step, ahead, open, ...r }) => r)
        .sort((a, b) => a.s - b.s),
    }))
    for (const s of done) for (const r of s.reqs) if (r.e === undefined) throw new Error(`a request of ${s.page} never ended`)
    return { plan: plan.id, steps: done }
  } catch (e) {
    over = true
    return { plan: plan.id, error: String(e).slice(0, 300) }
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
      const v = (await within(120000, visit(browser, plans[i]))) ?? { plan: plans[i].id, error: 'timeout' }
      if (v.error) {
        bad++
        console.error(`plan ${v.plan}: ${v.error}`)
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
