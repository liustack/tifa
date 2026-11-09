// Times of the shop. Go keeps an order's time as an instant and writes it as RFC 3339 in the zone
// it was made in, which is China Standard Time for the seed data. Here an instant is the number of
// milliseconds since the epoch, and the data handed out carries it as text, as Go's JSON does.

export const SECOND = 1000
export const MINUTE = 60 * SECOND
export const HOUR = 60 * MINUTE
export const DAY = 24 * HOUR

// china is the offset of China Standard Time, which has no daylight saving time.
const china = 8 * HOUR

// at is the instant of a wall clock time in China in 2026, where both shops trade.
export function at(month: number, day: number, hour: number, minute: number): number {
  return Date.UTC(2026, month - 1, day, hour, minute) - china
}

const pad = (n: number, width = 2) => String(n).padStart(width, '0')

// rfc3339 writes an instant as Go's time.Time does for a time made in China: 2026-10-13T14:32:00+08:00,
// with the milliseconds as a fraction without its trailing zeros when there are any
// (2026-11-09T07:20:42.308+08:00), as RFC3339Nano writes them.
export function rfc3339(ms: number): string {
  const d = new Date(ms + china)
  const milli = d.getUTCMilliseconds()
  const fraction = milli === 0 ? '' : '.' + pad(milli, 3).replace(/0+$/, '')
  return `${d.getUTCFullYear()}-${pad(d.getUTCMonth() + 1)}-${pad(d.getUTCDate())}T${pad(d.getUTCHours())}:${pad(d.getUTCMinutes())}:${pad(d.getUTCSeconds())}${fraction}+08:00`
}

// hours is Go's Duration.Hours: whole hours plus the rest as a fraction, so the float is the same.
export function hours(ms: number): number {
  const whole = Math.trunc(ms / HOUR)
  return whole + (ms % HOUR) / HOUR
}

export interface Local {
  date: string // 2026-10-13
  year: number
  month: number // 1 to 12
  day: number
  weekday: number // Sunday is 0
  hour: number
}

const formats = new Map<string, Intl.DateTimeFormat>()

// local reads an instant on the calendar of a time zone, as Go's t.In(zone) does.
export function local(ms: number, zone: string): Local {
  let format = formats.get(zone)
  if (!format) {
    format = new Intl.DateTimeFormat('en-US', {
      timeZone: zone, hourCycle: 'h23', year: 'numeric', month: 'numeric', day: 'numeric', weekday: 'short', hour: 'numeric',
    })
    formats.set(zone, format)
  }
  const parts: Record<string, string> = {}
  for (const p of format.formatToParts(ms)) parts[p.type] = p.value
  return {
    date: `${parts.year}-${parts.month.padStart(2, '0')}-${parts.day.padStart(2, '0')}`,
    year: Number(parts.year), month: Number(parts.month), day: Number(parts.day), hour: Number(parts.hour),
    weekday: ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'].indexOf(parts.weekday),
  }
}

const clocks = new Map<string, Intl.DateTimeFormat>()

// zoneOffset is how far the wall clock of a time zone is ahead of UTC at an instant, in milliseconds.
function zoneOffset(ms: number, zone: string): number {
  let format = clocks.get(zone)
  if (!format) {
    format = new Intl.DateTimeFormat('en-US', {
      timeZone: zone, hourCycle: 'h23', year: 'numeric', month: 'numeric', day: 'numeric', hour: 'numeric', minute: 'numeric', second: 'numeric',
    })
    clocks.set(zone, format)
  }
  const p: Record<string, number> = {}
  for (const part of format.formatToParts(ms)) if (part.type !== 'literal') p[part.type] = Number(part.value)
  return Date.UTC(p.year, p.month - 1, p.day, p.hour, p.minute, p.second) - Math.floor(ms / SECOND) * SECOND
}

// noon is 12:00 of the day that is days after the day of an instant, in a time zone, as Go's
// Deliveries computes a date: time.Date(y, m, d+days, 12, 0, 0, 0, zone). A delivery date is a date,
// carried as noon in the store zone: noon is never skipped or repeated by a change of the clock, so
// a page can add whole 24 hour steps to it and still land on the right calendar day.
export function noon(ms: number, zone: string, days: number): number {
  const l = local(ms, zone)
  const wall = Date.UTC(l.year, l.month - 1, l.day + days, 12)
  // The offset of the zone at that noon, found from a first guess, since it can change (daylight
  // saving time) between the instant and the day it is counted to.
  let at = wall - zoneOffset(wall, zone)
  at = wall - zoneOffset(at, zone)
  return at
}
