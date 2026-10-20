import type { TFunction } from 'i18next'

const day = { month: 'short', day: 'numeric' } as const

// arrival writes when a delivery arrives: its one day, or the first and the last day.
export function arrival(t: TFunction, d: { earliest: string; latest: string }) {
  return d.earliest === d.latest
    ? t('cart.arrivesOn', { date: d.latest, formatParams: { date: { weekday: 'short', ...day } } })
    : t('cart.arrives', { from: d.earliest, to: d.latest, formatParams: { from: day, to: day } })
}
