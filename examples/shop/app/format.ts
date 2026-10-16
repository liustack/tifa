// Money and times as the workbench shows them, in Chinese. Every formatter names its locale, and
// times name the time zone of the shop, so the server render and every browser write the same
// text whatever the language and zone of the machine.

const yuanFormat = new Intl.NumberFormat('zh-CN', { style: 'currency', currency: 'CNY' })

// yuan writes an amount in cents as ¥1,234.50.
export const yuan = (cents: number) => yuanFormat.format(cents / 100)

const formats = new Map<string, Intl.DateTimeFormat>()

function dateFormat(timeZone: string, options: Intl.DateTimeFormatOptions) {
  const key = `${timeZone} ${JSON.stringify(options)}`
  let f = formats.get(key)
  if (!f) {
    f = new Intl.DateTimeFormat('zh-CN', { ...options, timeZone })
    formats.set(key, f)
  }
  return f
}

// orderTime writes an instant as 10月13日 14:32 in timeZone.
export const orderTime = (instant: string, timeZone: string) =>
  dateFormat(timeZone, { month: 'long', day: 'numeric', hour: '2-digit', minute: '2-digit', hourCycle: 'h23' }).format(
    new Date(instant),
  )

// orderDate writes an instant as 2026年10月13日 in timeZone.
export const orderDate = (instant: string, timeZone: string) =>
  dateFormat(timeZone, { dateStyle: 'long' }).format(new Date(instant))
