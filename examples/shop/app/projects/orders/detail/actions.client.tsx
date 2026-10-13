import { useState } from 'react'
import { useRouter } from '@tanstack/react-router'
import { Icon } from '../../../icons'

// Reads the document at module scope, so it may only load in the browser: the page imports it
// through clientOnly, which keeps it out of the SSR bundle.
const origin = document.location.origin

export default function OrderActions({
  projectId,
  orderId,
  status,
}: {
  projectId: string
  orderId: string
  status: string
}) {
  const router = useRouter()
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')
  async function ship() {
    setPending(true)
    setError('')
    try {
      // The order page answers POST by shipping the order.
      const response = await fetch(
        `${origin}/projects/${encodeURIComponent(
          projectId,
        )}/orders/${encodeURIComponent(orderId)}`,
        { method: 'POST' },
      )
      if (!response.ok) {
        const problem = await response.json()
        throw new Error(problem.error || '操作未完成，请重试')
      }
      await router.invalidate()
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : '操作未完成，请重试')
    } finally {
      setPending(false)
    }
  }
  return (
    <div className="order-actions">
      <button
        className="primary-button"
        disabled={pending || status === '已发货'}
        onClick={ship}
      >
        <Icon name={status === '已发货' ? 'check' : 'box'} size={17} />
        {pending
          ? '正在更新…'
          : status === '已发货'
          ? '订单已发货'
          : '标记为已发货'}
      </button>
      {error && (
        <p role="alert" className="action-error">
          {error}
        </p>
      )}
    </div>
  )
}
