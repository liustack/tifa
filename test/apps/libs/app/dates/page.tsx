import {
  startOfMonth,
  endOfMonth,
  startOfWeek,
  endOfWeek,
  eachDayOfInterval,
  format,
  isSameMonth,
  isSameDay,
  addMonths,
  parseISO,
  differenceInMinutes,
  formatDistance,
  isWeekend,
  getISOWeek,
  addDays,
  compareAsc,
  formatDuration,
  intervalToDuration,
  isWithinInterval,
} from 'date-fns'
import dayjs from 'dayjs'
import utc from 'dayjs/plugin/utc'
import customParseFormat from 'dayjs/plugin/customParseFormat'
import relativeTime from 'dayjs/plugin/relativeTime'
import isBetween from 'dayjs/plugin/isBetween'
import weekOfYear from 'dayjs/plugin/weekOfYear'
import advancedFormat from 'dayjs/plugin/advancedFormat'
import duration from 'dayjs/plugin/duration'
import { page, type PageData } from './route.gen'

dayjs.extend(utc)
dayjs.extend(customParseFormat)
dayjs.extend(relativeTime)
dayjs.extend(isBetween)
dayjs.extend(weekOfYear)
dayjs.extend(advancedFormat)
dayjs.extend(duration)

type Raw = PageData['events'][number]
type Event = Raw & { date: Date }

export const head = () => ({ meta: [{ title: 'Dates · Library pages' }] })

function Month({ month, events, now }: { month: Date; events: Event[]; now: Date }) {
  const byDay = new Map<string, Event[]>()
  for (const e of events)
    if (isSameMonth(e.date, month)) {
      const k = format(e.date, 'yyyy-MM-dd')
      const a = byDay.get(k)
      if (a) a.push(e)
      else byDay.set(k, [e])
    }
  const days = eachDayOfInterval({ start: startOfWeek(startOfMonth(month), { weekStartsOn: 1 }), end: endOfWeek(endOfMonth(month), { weekStartsOn: 1 }) })
  const weeks: Date[][] = []
  for (let i = 0; i < days.length; i += 7) weeks.push(days.slice(i, i + 7))
  return (
    <section className="month">
      <h2>{format(month, 'MMMM yyyy')}</h2>
      <table>
        <thead>
          <tr>
            <th>Wk</th>
            {weeks[0].map((d) => (
              <th key={d.getTime()}>{format(d, 'EEE')}</th>
            ))}
          </tr>
        </thead>
        <tbody>
          {weeks.map((w) => (
            <tr key={w[0].getTime()}>
              <td className="wk">{getISOWeek(w[0])}</td>
              {w.map((d) => {
                const evs = byDay.get(format(d, 'yyyy-MM-dd')) || []
                return (
                  <td key={d.getTime()} className={[isSameMonth(d, month) ? 'in' : 'out', isWeekend(d) ? 'we' : '', isSameDay(d, now) ? 'today' : ''].join(' ')}>
                    <time dateTime={format(d, 'yyyy-MM-dd')}>{format(d, 'd')}</time>
                    {evs.slice(0, 3).map((e) => (
                      <div key={e.id} className="ev">
                        {format(e.date, 'HH:mm')} {e.title}
                      </div>
                    ))}
                    {evs.length > 3 && <div className="more">+{evs.length - 3} more</div>}
                  </td>
                )
              })}
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  )
}

function Agenda({ events, now }: { events: Event[]; now: Date }) {
  const sorted = [...events].sort((a, b) => compareAsc(a.date, b.date))
  const soon = { start: now, end: addDays(now, 21) }
  return (
    <ol className="agenda">
      {sorted.map((e) => {
        const end = new Date(e.date.getTime() + e.minutes * 60000)
        const dj = dayjs.utc(e.start)
        return (
          <li key={e.id} className={isWithinInterval(e.date, soon) ? 'soon' : ''}>
            <b>{format(e.date, 'EEEE, do MMMM')}</b> {format(e.date, 'h:mm a')} to {format(end, 'h:mm a')} ({formatDuration(intervalToDuration({ start: e.date, end }))}) {e.title}
            <span className="rel">{formatDistance(e.date, now, { addSuffix: true })}</span>
            <span className="dj">
              {dj.format('ddd D MMM YYYY [wk] w, Do')} {dj.from(dayjs.utc(now))}
            </span>
          </li>
        )
      })}
    </ol>
  )
}

type Row = { who: string; week: string; minutes: number; first: dayjs.Dayjs; last: dayjs.Dayjs; n: number }

function Timesheet({ events, now }: { events: Event[]; now: Date }) {
  const rows = new Map<string, Row>()
  for (const e of events) {
    const t = dayjs.utc(e.logged, 'DD/MM/YYYY HH:mm', true)
    if (!t.isValid()) continue
    const key = e.who + ' ' + t.format('YYYY-[W]ww')
    const r = rows.get(key) || { who: e.who, week: t.format('YYYY-[W]ww'), minutes: 0, first: t, last: t, n: 0 }
    r.minutes += differenceInMinutes(new Date(t.valueOf() + e.minutes * 60000), t.toDate())
    if (t.isBefore(r.first)) r.first = t
    if (t.isAfter(r.last)) r.last = t
    r.n++
    rows.set(key, r)
  }
  const list = [...rows.values()].sort((a, b) => (a.week < b.week ? -1 : a.week > b.week ? 1 : a.who.localeCompare(b.who)))
  return (
    <table className="timesheet">
      <tbody>
        {list.map((r) => (
          <tr key={r.who + r.week}>
            <td>{r.who}</td>
            <td>{r.week}</td>
            <td>{r.n}</td>
            <td>{dayjs.duration(r.minutes, 'minutes').format('H[h] mm[m]')}</td>
            <td>
              {r.first.format('ddd HH:mm')} to {r.last.format('ddd HH:mm')}
            </td>
            <td>{r.last.isBetween(dayjs.utc(now).subtract(14, 'day'), dayjs.utc(now)) ? 'recent' : ''}</td>
          </tr>
        ))}
      </tbody>
    </table>
  )
}

export default function Dates() {
  const data = page.useLoaderData()
  const now = new Date(data.now)
  const events: Event[] = data.events.map((e) => ({ ...e, date: parseISO(e.start + 'Z') }))
  const months = [0, 1, 2].map((k) => addMonths(startOfMonth(now), k - 1))
  return (
    <main>
      <h1>Team calendar</h1>
      {months.map((mo) => (
        <Month key={mo.getTime()} month={mo} events={events} now={now} />
      ))}
      <Agenda events={events} now={now} />
      <Timesheet events={events} now={now} />
    </main>
  )
}
