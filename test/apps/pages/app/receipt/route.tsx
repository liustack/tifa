import { route } from './+route'

export default function Receipt() {
  const receipt = route.useLoaderData()
  const paidAt = new Date(receipt.paidAt)
  const options = { timeZone: receipt.timeZone }
  const money = new Intl.NumberFormat(receipt.locale, { style: 'currency', currency: 'CNY' })
  return (
    <main>
      <p id="amount">{money.format(receipt.amount)}</p>
      <p id="refunded">{money.format(receipt.refunded)}</p>
      <p id="day">{paidAt.toLocaleDateString(receipt.locale, options)}</p>
      <p id="time">{new Intl.DateTimeFormat(receipt.locale, { ...options, dateStyle: 'full', timeStyle: 'short' }).format(paidAt)}</p>
    </main>
  )
}

export const head = () => ({ meta: [{ title: 'Receipt' }] })
