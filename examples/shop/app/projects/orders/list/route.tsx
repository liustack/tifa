import { useState } from 'react'
import { Link } from '@tanstack/react-router'
import { route } from './+route'
import { Icon, Status } from '../../../icons'

export default function OrdersPage() {
  const list = route.useLoaderData()
  const all = list.orders ?? []
  const [filter, setFilter] = useState('全部订单')
  const [search, setSearch] = useState('')
  const orders = all.filter(
    (order) =>
      (filter === '全部订单' || order.status === filter) &&
      `${order.id} ${order.customer}`
        .toLowerCase()
        .includes(search.trim().toLowerCase()),
  )

  return (
    <section aria-labelledby="orders-title">
      <div className="section-heading">
        <div className="title-group">
          <h2 id="orders-title">订单</h2>
          <span className="count-pill">{all.length}</span>
        </div>
        <span className="subtle">每一份信任，都有回响。</span>
      </div>
      <div className="metrics">
        <div>
          <span className="metric-label">订单总额</span>
          <strong className="revenue">{list.revenue}</strong>
          <small>所有订单累计</small>
        </div>
        <div>
          <span className="metric-label">
            待发货
            <span className="metric-dot amber" />
          </span>
          <strong>{list.pending.toString().padStart(2, '0')}</strong>
          <small>等待打包的心意</small>
        </div>
        <div>
          <span className="metric-label">
            已发货
            <span className="metric-dot green" />
          </span>
          <strong>{list.shipped.toString().padStart(2, '0')}</strong>
          <small>正在奔赴下一站</small>
        </div>
        <div className="metric-illustration" aria-hidden="true">
          <Icon name="box" size={62} />
          <span>
            GOOD THINGS
            <br />
            ON THEIR WAY.
          </span>
        </div>
      </div>
      <div className="orders-panel">
        <div className="table-toolbar">
          <div className="filter-tabs" aria-label="订单状态">
            {['全部订单', '待发货', '已发货'].map((label) => (
              <button
                key={label}
                aria-pressed={filter === label}
                className={filter === label ? 'active' : ''}
                onClick={() => setFilter(label)}
              >
                {label}
              </button>
            ))}
          </div>
          <label className="search-field">
            <Icon name="search" size={17} />
            <input
              aria-label="搜索订单或客户"
              placeholder="搜索订单或客户"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
            />
          </label>
        </div>
        <div className="table-scroll">
          <table>
            <thead>
              <tr>
                <th>订单编号</th>
                <th>客户</th>
                <th>下单日期</th>
                <th>状态</th>
                <th className="amount-column">金额</th>
                <th>
                  <span className="sr-only">查看</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {orders.map((order) => (
                <tr key={order.id}>
                  <td>
                    <Link
                      className="order-link"
                      to="/projects/$projectId/orders/$orderId"
                      params={{ projectId: order.projectId, orderId: order.id }}
                    >
                      {order.id}
                    </Link>
                  </td>
                  <td>
                    <span className="customer-cell">
                      <span className="avatar">{order.initials}</span>
                      {order.customer}
                    </span>
                  </td>
                  <td className="date-cell">{order.date}</td>
                  <td>
                    <Status status={order.status} />
                  </td>
                  <td className="amount-column order-amount">{order.total}</td>
                  <td>
                    <Link
                      className="row-arrow"
                      aria-label={`查看订单 ${order.id}`}
                      to="/projects/$projectId/orders/$orderId"
                      params={{ projectId: order.projectId, orderId: order.id }}
                    >
                      <Icon name="arrow" size={18} />
                    </Link>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          {orders.length === 0 && (
            <div className="empty-state">
              <Icon name="search" size={26} />
              <h3>没有找到匹配的订单</h3>
              <p>试试其他关键词，或切换订单状态。</p>
              <button
                className="quiet-button"
                onClick={() => {
                  setSearch('')
                  setFilter('全部订单')
                }}
              >
                查看全部订单
              </button>
            </div>
          )}
        </div>
        <div className="table-footer">
          <span>共 {orders.length} 笔订单</span>
          <span>
            每一单，都认真对待。
            <Icon name="check" size={14} />
          </span>
        </div>
      </div>
    </section>
  )
}
