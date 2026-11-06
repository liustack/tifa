import { useState } from 'react'
import { Link, useLocation, useRouter } from '@tanstack/react-router'
import { useLanguage } from 'tifa/client'
import { page } from './route.gen'
import { GET } from './mine/route.gen'

export default function Store() {
  const data = page.useLoaderData()
  const language = useLanguage()
  const location = useLocation()
  const router = useRouter()
  const [mine, setMine] = useState('')
  return (
    <main id="store">
      store go={data.language} react={language} path={location.pathname} q={data.q}
      <nav>
        <Link to="/store/cart" id="to-cart">cart</Link>
        <Link to="/projects" id="to-projects">projects</Link>
        <Link to="/store/leave" id="to-leave">leave</Link>
        <Link to="/store/stay" id="to-stay">stay</Link>
        <button id="nav-default" onClick={() => void router.navigate({ href: '/store/cart' })}>
          default
        </button>
        <button id="nav-english" onClick={() => void router.navigate({ href: '/en-US/store/cart' })}>
          english
        </button>
        <button id="call" onClick={() => void GET().then((out) => setMine(out?.language ?? ''))}>
          call
        </button>
      </nav>
      <p id="mine">{mine}</p>
    </main>
  )
}
