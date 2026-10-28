import i18next, { type i18n } from 'i18next'
import { I18nextProvider, useTranslation } from 'react-i18next'
import { Frame } from '../../frame'
import { route, type Data } from './+route'

export const head = () => ({ meta: [{ title: 'i18n 和 Intl' }] })

// One instance for each version of the messages, kept for the next renders as an app keeps the
// instance its backend filled.
const instances = new Map<string, i18n>()

function instanceFor(data: Data) {
  let instance = instances.get(data.version)
  if (!instance) {
    instance = i18next.createInstance()
    const resources: Record<string, { translation: Record<string, Record<string, string> | null> }> = {}
    for (const [lng, groups] of Object.entries(data.messages ?? {})) resources[lng] = { translation: groups ?? {} }
    instance.init({ lng: 'zh-CN', fallbackLng: 'en-US', resources, initAsync: false, interpolation: { escapeValue: false } })
    instances.set(data.version, instance)
  }
  return instance
}

export default function I18nPage() {
  const data = route.useLoaderData()
  return (
    <I18nextProvider i18n={instanceFor(data)}>
      <Frame title="i18n 和 Intl" size={data.keys} unit="个文案键">
        <Messages />
        <Formats />
      </Frame>
    </I18nextProvider>
  )
}

function Messages() {
  const data = route.useLoaderData()
  const { t } = useTranslation()
  const users = data.users ?? []
  const items = []
  for (let i = 0; i < data.keys; i++) {
    const key = `g${String(Math.floor(i / 100)).padStart(3, '0')}.k${i}`
    items.push(
      <li key={i} className="border-b py-1">
        {t(key, { name: users[i % users.length], count: (i % 7) + 1, val: i * 37, amount: i * 12.5 })}
      </li>,
    )
  }
  return (
    <section className="flex flex-col gap-2">
      <h2 className="font-heading text-xl">文案</h2>
      <ol id="messages" className="columns-1 text-xs md:columns-2">
        {items}
      </ol>
    </section>
  )
}

const locales = ['zh-CN', 'en-US', 'ja-JP', 'de-DE', 'fr-FR', 'ar-EG', 'hi-IN', 'pt-BR']
const zones = ['Asia/Shanghai', 'America/New_York', 'Europe/Berlin', 'Asia/Tokyo', 'Asia/Kolkata', 'America/Sao_Paulo']
const currencies = ['CNY', 'USD', 'JPY', 'EUR', 'EUR', 'EGP', 'INR', 'BRL']
const day = 24 * 60 * 60 * 1000

// Formats makes the page's Intl calls the ways apps make them: some build a formatter for each
// call, some keep one, some go through toLocaleString.
function Formats() {
  const data = route.useLoaderData()
  const now = Date.parse(data.now)
  const kept = locales.map((l, i) => ({
    date: new Intl.DateTimeFormat(l, { timeZone: zones[i % zones.length], dateStyle: 'medium', timeStyle: 'short' }),
    plural: new Intl.PluralRules(l),
    relative: new Intl.RelativeTimeFormat(l, { numeric: 'auto' }),
    list: new Intl.ListFormat(l, { type: 'conjunction' }),
    compact: new Intl.NumberFormat(l, { notation: 'compact' }),
  }))
  const users = data.users ?? []
  const items = []
  for (let i = 0; i < data.intlCalls; i++) {
    const at = i % locales.length
    const l = locales[at]
    const v = (i * 7919) % 100000
    const when = new Date(now - (i % 400) * day - i * 60000)
    let text: string
    switch (i % 8) {
      case 0:
        text = new Intl.NumberFormat(l, { style: 'currency', currency: currencies[at] }).format(v / 7)
        break
      case 1:
        text = v.toLocaleString(l)
        break
      case 2:
        text = kept[at].date.format(when)
        break
      case 3:
        text = when.toLocaleDateString(l, { timeZone: zones[i % zones.length], weekday: 'long', month: 'long', day: 'numeric' })
        break
      case 4:
        text = kept[at].plural.select(i % 12)
        break
      case 5:
        text = kept[at].relative.format(-(i % 30), 'day')
        break
      case 6:
        text = kept[at].list.format([users[i % users.length], users[(i + 1) % users.length], users[(i + 2) % users.length]])
        break
      default:
        text = kept[at].compact.format(v * 113)
    }
    items.push(
      <li key={i} lang={l} className="truncate border-b py-0.5">
        {text}
      </li>,
    )
  }
  return (
    <section className="flex flex-col gap-2">
      <h2 className="font-heading text-xl">格式化</h2>
      <ol id="formats" className="grid grid-cols-2 gap-x-4 font-mono text-[11px] md:grid-cols-4">
        {items}
      </ol>
    </section>
  )
}
