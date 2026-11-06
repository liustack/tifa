// Plays visit plans in Chromium against one server and writes what the browser sent to it, one JSON
// line per visit. Dynamic traffic only: documents and fetches, no scripts, styles, images or fonts,
// and nothing outside the workbench (the store links 404 in the ports).
//
//   node record.mjs --base http://127.0.0.1:3000 --plans plans.json --out trace.jsonl [--workers 4] [--limit 100]
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
const plans = JSON.parse(fs.readFileSync(arg('plans'), 'utf8')).plans
const out = fs.createWriteStream(arg('out'))
const workers = Number(arg('workers', '4'))
const limit = Number(arg('limit', String(plans.length)))
if (!base) throw new Error('--base is required')

const keepHeader = /^(accept|rsc|next-router-.*|next-url|x-tsr-.*|x-react-router.*|purpose|sec-purpose|content-type)$/i
const within = (ms, p) => Promise.race([p, new Promise((r) => setTimeout(() => r(null), ms))])
const QUIET_MS = 450
const CAP_MS = 6000
const NAV_WINDOW_MS = 200

const wanted = (req) => {
  if (!['document', 'fetch', 'xhr'].includes(req.resourceType())) return false
  const path = new URL(req.url()).pathname
  return !(path.startsWith('/store') || path.startsWith('/_next/') || path.startsWith('/assets/') || path.includes('favicon'))
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
      if (inflight === 0 && Date.now() - lastActivity >= QUIET_MS) break
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
        const link = page.locator(`a[href="${step.page}"]`).first()
        if ((await link.count()) === 0) throw new Error(`step ${i}: no link to ${step.page} on ${new URL(page.url()).pathname}`)
        await link.click()
        await page.waitForURL((u) => u.pathname === step.page, { timeout: 10000 })
      }
      await settle()
      await page.mouse.move(1, 1)
      const shown = new URL(page.url()).pathname
      if (shown !== step.page) throw new Error(`step ${i}: shows ${shown}, want ${step.page}`)
    }
    over = true
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
