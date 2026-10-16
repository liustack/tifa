import { differenceInMinutes, formatDuration } from 'date-fns'
import { zhCN } from 'date-fns/locale'

// Money and times as the workbench shows them, in Chinese. Every formatter names its locale, and
// times name the time zone of the shop, so the server render and every browser write the same
// text whatever the language and zone of the machine.

const yuanFormat = new Intl.NumberFormat('zh-CN', { style: 'currency', currency: 'CNY' })

// yuan writes an amount in cents as ¥1,234.50.
export const yuan = (cents: number) => yuanFormat.format(cents / 100)

const wholeYuanFormat = new Intl.NumberFormat('zh-CN', {
  style: 'currency',
  currency: 'CNY',
  maximumFractionDigits: 0,
})

// wholeYuan writes an amount in cents as ¥1,235, for chart axes.
export const wholeYuan = (cents: number) => wholeYuanFormat.format(cents / 100)

const percentFormat = new Intl.NumberFormat('zh-CN', { style: 'percent', maximumFractionDigits: 1 })

// percent writes a share as 95.2%.
export const percent = (share: number) => percentFormat.format(share)

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

// waited writes the time between two instants as 1 天 3 小时, or in minutes under an hour. It
// counts elapsed time, not calendar days, so it reads the same in every time zone.
export function waited(from: string, to: string) {
  const minutes = differenceInMinutes(new Date(to), new Date(from))
  const days = Math.floor(minutes / 1440)
  const hours = Math.floor((minutes % 1440) / 60)
  return formatDuration(days || hours ? { days, hours } : { minutes }, { locale: zhCN, delimiter: ' ' })
}

// day writes a calendar date such as 2026-10-13 as 10/13. The date is already a day of the shop,
// so it is read and written in UTC, where it does not move.
export const day = (date: string) =>
  dateFormat('UTC', { month: 'numeric', day: 'numeric' }).format(new Date(`${date}T00:00:00Z`))
