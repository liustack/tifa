import { createContext, useContext } from 'react'
import { useTranslation } from 'react-i18next'
import { Star } from 'lucide-react'
import { cn } from 'cn'

export type Market = { lang: string; currency: string; timeZone: string }

export const MarketContext = createContext<Market>({ lang: 'zh-CN', currency: 'CNY', timeZone: 'Asia/Shanghai' })

const formats = new Map<string, { money: Intl.NumberFormat; day: Intl.DateTimeFormat; long: Intl.DateTimeFormat; count: Intl.NumberFormat }>()

function formatsOf(m: Market) {
  const key = `${m.lang} ${m.currency} ${m.timeZone}`
  let f = formats.get(key)
  if (!f) {
    f = {
      money: new Intl.NumberFormat(m.lang, { style: 'currency', currency: m.currency }),
      day: new Intl.DateTimeFormat(m.lang, { timeZone: m.timeZone, weekday: 'short', month: 'long', day: 'numeric' }),
      long: new Intl.DateTimeFormat(m.lang, { timeZone: m.timeZone, dateStyle: 'long' }),
      count: new Intl.NumberFormat(m.lang),
    }
    formats.set(key, f)
  }
  return f
}

// useMarket gives the page's components their messages and the formats of its language.
export function useMarket() {
  const market = useContext(MarketContext)
  const { t } = useTranslation()
  const f = formatsOf(market)
  return {
    t,
    lang: market.lang,
    money: (fen: number) => f.money.format(fen / 100),
    day: (date: string) => f.day.format(new Date(`${date}T12:00:00Z`)),
    date: (instant: string) => f.long.format(new Date(instant)),
    count: (n: number) => f.count.format(n),
  }
}

export function Stars({ rating, className }: { rating: number; className?: string }) {
  const { t } = useMarket()
  return (
    <span className={cn('inline-flex items-center gap-px', className)} role="img" aria-label={t('card.stars', { rating })}>
      {[1, 2, 3, 4, 5].map((n) => (
        <Star
          key={n}
          aria-hidden
          className={cn('size-3', rating >= n - 0.25 ? 'fill-[#b3843a] text-[#b3843a]' : rating >= n - 0.75 ? 'fill-[#b3843a]/40 text-[#b3843a]' : 'text-muted-foreground/40')}
        />
      ))}
    </span>
  )
}
