import { useState } from 'react'
import { TifaError } from 'tifa/client'
import { POST } from './+route'
import { Icon } from '../../../../icons'

// Shipping works only once the page runs in the browser, so the page loads this component
// through clientOnly and the server renders a disabled button in its place.
export default function OrderActions({
  project,
  order,
  status,
}: {
  project: string
  order: string
  status: string
}) {
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')
  async function ship() {
    setPending(true)
    setError('')
    try {
      // The order page answers POST by shipping the order. The page data reloads afterwards.
      await POST({ params: { project, order } })
    } catch (cause) {
      setError(cause instanceof TifaError ? cause.message : '操作未完成，请重试')
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
