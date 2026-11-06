// Plays visit plans in Chromium against one server and writes what the browser sent to it, one JSON
// line per visit. Dynamic traffic only: documents and fetches, no scripts, styles, images or fonts,
// and nothing outside the workbench (the store links 404 in the ports).
//
//   node record.mjs --base http://127.0.0.1:3000 --plans plans.json --out trace.jsonl [--workers 4]
//
// Each visit gets a fresh browser context (no cookies, no cache), a 1280x800 viewport, no hover (the
// mouse rests in a corner between steps), real clicks on the links of the page and the browser's own
// reload. Requests are timed from the start of their step, so that the replay can keep the
// order and the parallelism the browser had.
import { chromium } from 'playwright'
import fs from 'node:fs'

const arg = (name, dflt) => {
  const i = process.argv.indexOf('--' + name)
  return i > 0 ? process.argv[i + 1] : dflt
}
const base = arg('base')
const plans = JSON.parse(fs.readFileSync(arg('plans'), 'utf8'))
const out = fs.createWriteStream(arg('out'))
const workers = Number(arg('workers', '4'))
const limit = Number(arg('limit', String(plans.length)))
if (!base) throw new Error('--base is required')

const keepHeader = /^(accept|rsc|next-router-.*|next-url|x-tsr-.*|x-react-router.*|purpose|sec-purpose|content-type)$/i
const within = (ms, p) => Promise.race([p, new Promise((r) => setTimeout(() => r(null), ms))])
const QUIET_MS = 450
const CAP_MS = 6000

const wanted = (req) => {
  if (!['document', 'fetch', 'xhr'].includes(req.resourceType())) return false
  const path = new URL(req.url()).pathname
  return !(path.startsWith('/store') || path.startsWith('/_next/') || path.startsWith('/assets/') || path.includes('favicon'))
}

async function visit(browser, plan) {
  const context = await browser.newContext({ viewport: { width: 1280, height: 800 } })
  const page = await context.newPage()
  const steps = []
  let cur = null
  let inflight = 0
  let lastActivity = Date.now()
  const pending = []
  page.on('request', (req) => {
    if (!wanted(req) || !cur) return
    // Prefetches do not hold the page back: the browser may keep one open for a long time.
    const ahead = Boolean(req.headers()['next-router-prefetch'])
    if (!ahead) inflight++
    lastActivity = Date.now()
    const rec = { method: req.method(), url: req.url().replace(new URL(req.url()).origin, ''), s: Date.now() - cur.t0, type: req.resourceType() }
    const early = req.headers()
    rec.headers = Object.fromEntries(Object.entries(early).filter(([k]) => keepHeader.test(k)))
    rec.inm = false
    rec.class = rec.type === 'document' ? 'doc' : ahead ? 'prefetch' : 'nav'
    cur.reqs.push(rec)
    const done = (status) => {
      if (!ahead) inflight--
      lastActivity = Date.now()
      rec.e = Date.now() - cur.t0
      rec.status = status
    }
    const p = (async () => {
      const finish = within(
        8000,
        req.response().then(
          async (res) => {
            await within(8000, res?.finished().catch(() => {}))
            done(res ? res.status() : 0)
            return res
          },
          () => (done(0), null),
        ),
      )
      const res = await finish
      if (rec.e === undefined) done(0) // the browser never finished it (cancelled)
      const all = (await within(3000, req.allHeaders().catch(() => ({})))) ?? {}
      rec.headers = Object.fromEntries(Object.entries(all).filter(([k]) => keepHeader.test(k)))
      rec.inm = 'if-none-match' in all
      if (res && res.status() === 0) rec.failed = true
      if (all['next-router-prefetch'] || /prefetch/i.test(all['sec-purpose'] ?? '')) rec.class = 'prefetch'
    })()
    pending.push(p)
  })
  page.on('requestfailed', (req) => {
    // A request the browser cancels (Next.js does it to its prefetches once it has what it wants).
  })
  const settle = async () => {
    const start = Date.now()
    while (Date.now() - start < CAP_MS) {
      await page.waitForTimeout(100)
      if (inflight === 0 && Date.now() - lastActivity >= QUIET_MS) break
    }
    await within(2000, Promise.allSettled(pending.splice(0)))
    // A request the browser has not finished by now (a prefetch it left open) ends with the step.
    for (const r of cur.reqs) if (r.e === undefined) ((r.e = Date.now() - cur.t0), (r.status = r.status ?? 0))
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
      } else if (step.kind === 'reload') {
        await page.reload()
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
    return { plan: plan.id, steps: steps.map((s) => ({ kind: s.kind, page: s.page, reqs: s.reqs.filter((r) => r.class).sort((a, b) => a.s - b.s) })) }
  } catch (e) {
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
