import { Link } from '@tanstack/react-router'
import { useLanguage } from 'tifa/client'
import { page } from './route.gen'

export default function Cart() {
  const data = page.useLoaderData()
  return (
    <main id="cart">
      cart go={data.language} react={useLanguage()}
      <Link to="/store" id="back">store</Link>
    </main>
  )
}
