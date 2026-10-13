import { useState } from 'react'
import { useRouter } from '@tanstack/react-router'

export default function Basket({ itemId }: { itemId: string }) {
  const router = useRouter()
  const [count, setCount] = useState(Number(window.localStorage.getItem(itemId) ?? 0))
  const add = async () => {
    await fetch(window.location.pathname, { method: 'POST' })
    setCount(count + 1)
    await router.invalidate()
  }
  return <button onClick={add}>Add ({count})</button>
}
