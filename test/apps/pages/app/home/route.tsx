import { Link } from '@tanstack/react-router'

export default function Home() {
  return (
    <main>
      <Link to="/shops/$shopId" params={{ shopId: 'north' }}>
        North shop
      </Link>{' '}
      <Link to="/shops/$shopId" params={{ shopId: 'south' }}>
        South shop
      </Link>
    </main>
  )
}

export const head = () => ({ meta: [{ title: 'Home' }] })
