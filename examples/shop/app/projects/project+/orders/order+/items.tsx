import { route } from './+route'
import { Icon } from '../../../../icons'
import { yuan } from '../../../../format'

// A nested business component consumes the page's injected data directly.
// It neither receives a chain of props nor initiates a new server request.
export default function OrderItems() {
  const order = route.useLoaderData()
  return (
    <div className="detail-card">
      <div className="card-heading">
        <Icon name="box" size={19} />
        <h3>订单商品</h3>
      </div>
      <div className="line-items">
        {(order.items ?? []).map((item) => (
          <div className="line-item" key={item.name}>
            <div className={`product-placeholder ${item.color}`}>
              <Icon name="bag" size={31} />
            </div>
            <div className="item-description">
              <strong>{item.name}</strong>
              <span>{item.description}</span>
              <small>数量 {item.quantity}</small>
            </div>
            <strong className="item-price">
              {yuan(item.priceCents)}
              <small> / 件</small>
            </strong>
          </div>
        ))}
      </div>
      <div className="order-totals">
        <div>
          <span>商品小计</span>
          <span>{yuan(order.totalCents)}</span>
        </div>
        <div>
          <span>运费</span>
          <span>包邮</span>
        </div>
        <div className="total-row">
          <strong>实付金额</strong>
          <strong>{yuan(order.totalCents)}</strong>
        </div>
      </div>
    </div>
  )
}
