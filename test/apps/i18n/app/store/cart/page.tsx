import { Link } from '@tanstack/react-router'
import { useLanguage } from 'tifa/client'
import { i18n } from 'tifa/i18n'
import { page } from './route.gen'

// The head translates with the instance of the app, in the language of the page it heads.
export const head = () => ({ meta: [{ title: i18n.t('store:cart') }] })

export default function Cart() {
  const data = page.useLoaderData()
  return (
    <main id="cart">
      cart go={data.language} react={useLanguage()}
      <Link to="/store" id="back">store</Link>
    </main>
  )
}
