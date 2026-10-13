import { Link } from '@tanstack/react-router'
import { clientOnly } from 'tifa/client'
import { route, type Data } from './+route'
import { Icon, Status } from '../../../icons'
import OrderItems from './items'

const OrderActions = clientOnly(
  () => import('./actions.client'),
  <button className="primary-button" disabled>
    正在准备操作…
  </button>,
)

export default function OrderPage() {
  const order = route.useLoaderData()
  return (
    <section aria-labelledby="order-title">
      <Link
        className="back-link"
        to="/projects/$projectId"
        params={{ projectId: order.projectId }}
      >
        <Icon name="back" size={17} />
        全部订单
      </Link>
      <div className="detail-heading">
        <div>
          <div className="title-group">
            <h2 id="order-title">{order.id}</h2>
            <Status status={order.status} />
          </div>
          <p className="subtle">
            创建于 {order.date}
            <span className="inline-dot">·</span>共{' '}
            {(order.items ?? []).reduce((n, item) => n + item.quantity, 0)} 件商品
          </p>
        </div>
        <OrderActions
          projectId={order.projectId}
          orderId={order.id}
          status={order.status}
        />
      </div>
      <div className="detail-grid">
        <OrderItems />
        <aside className="detail-card customer-detail">
          <div className="card-heading">
            <h3>客户信息</h3>
          </div>
          <div className="customer-profile">
            <span className="avatar">{order.initials}</span>
            <div>
              <strong>{order.customer}</strong>
              <span>{order.email}</span>
            </div>
          </div>
          <div className="shipping-address">
            <span className="eyebrow">收货地址</span>
            <p>{order.address}</p>
          </div>
          <div className="customer-message">
            <Icon name="note" size={18} />
            <p>每一个细节，都让拆开包裹的时刻更美好。</p>
          </div>
        </aside>
      </div>
    </section>
  )
}

export const head = ({ loaderData }: { loaderData?: Data }) => ({
  meta: loaderData ? [{ title: `${loaderData.id} · 山海工作台` }] : [],
})

export function NotFound() {
  return (
    <div className="empty-state">
      <Icon name="search" size={26} />
      <h3>没有找到这个订单</h3>
      <p>订单号可能有误，或者它属于另一家店铺。</p>
    </div>
  )
}
