// Counts how often a write of the storefront encodes JSON, for TestStoreWritesEncodeOnce: Tifa
// encodes the answer of a handler once, and so must the shared layer that stands in for it. It
// prints, for each request, the number of calls of JSON.stringify and Response.json.
import { callCheckout, callCoupon, callMine, callProduct, callQuantity, callSeen } from './calls.ts'
import { parseBody, stats } from './json.ts'

let stringify = 0
let responseJSON = 0
const realStringify = JSON.stringify
JSON.stringify = ((...args: Parameters<typeof JSON.stringify>) => {
  stringify++
  return realStringify(...args)
}) as typeof JSON.stringify
const realResponseJSON = Response.json
Response.json = ((...args: Parameters<typeof Response.json>) => {
  responseJSON++
  return realResponseJSON.apply(Response, args)
}) as typeof Response.json

const order = '{"email":"lin@example.com","address":{"name":"Lin Xia","phone":"138 0013 8000","country":"CN","region":"Shanghai","city":"Shanghai","line1":"99 Wukang Road","line2":"","postalCode":"200031"},"delivery":"standard","payment":"card","cardNumber":"4242 4242 4242 4242","cardExpiry":"12/30","cardCvc":"123","billingSame":true,"notes":"","newsletter":false,"terms":true}'
const out: Record<string, { stringify: number; json: number; status: number }> = {}
let cookie = ''

async function count(name: string, run: () => Promise<Response>) {
  stringify = 0
  responseJSON = 0
  const res = await run()
  const set = res.headers.getSetCookie().map((c) => c.split(';')[0]).find((c) => c.startsWith('store_cart='))
  if (set) cookie = set
  out[name] = { stringify, json: responseJSON, status: res.status }
  await res.text()
}

const post = (body: string, call: boolean) =>
  new Request('http://shop.test/x', { method: 'POST', body, headers: { 'content-type': 'application/json', cookie, ...(call ? { 'x-tifa-call': '1' } : {}) } })
const get = (call: boolean) => new Request('http://shop.test/x', { headers: { cookie, ...(call ? { 'x-tifa-call': '1' } : {}) } })

for (const call of [false, true]) {
  const k = call ? ' call' : ' plain'
  await count('add' + k, () => callProduct(post('{"color":"mist","size":"","quantity":1}', call), { lang: 'zh-CN', product: 'pour-over-kettle' }))
  await count('quantity' + k, () => callQuantity(post('{"line":"pour-over-kettle.mist","quantity":2}', call), { lang: 'zh-CN' }))
  await count('coupon' + k, () => callCoupon(post('{"code":"SHANHAI10"}', call), { lang: 'zh-CN' }))
  await count('seen' + k, () => callSeen(post('{"slug":"oak-tray"}', call), { lang: 'zh-CN' }))
  await count('mine' + k, () => callMine(get(call), { lang: 'zh-CN' }))
  await count('refused' + k, () => callQuantity(post('{"line":"x.y","quantity":99}', call), { lang: 'zh-CN' }))
  await count('checkout' + k, () => callCheckout(post(order, call), { lang: 'zh-CN' }))
}
// A body with a member the write does not know: the reader keeps only the object itself and the members it needs.
stats.containers = 0
parseBody(new TextEncoder().encode('{"code":"x","unknown":{"a":[1,2,{"b":[[]]}],"c":{}},"more":[[[]]]}'), { code: 'string' })
out.unknown = { stringify: stats.containers, json: 0, status: 0 }
process.stdout.write(realStringify(out))
