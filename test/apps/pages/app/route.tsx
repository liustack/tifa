import { Link } from '@tanstack/react-router'

export default function Home() {
  return (
    <main>
      <Link to="/shops/$shop" params={{ shop: 'north' }}>
        North shop
      </Link>{' '}
      <Link to="/shops/$shop" params={{ shop: 'south' }}>
        South shop
      </Link>
    </main>
  )
}

export const head = () => ({ meta: [{ title: 'Home' }] })
