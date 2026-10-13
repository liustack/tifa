import { defineData } from '@go-quick/data'

export default defineData({
  order: {
    source: 'orders.detail',
    params: {
      projectId: { routeParam: 'projectId' },
      orderId: { routeParam: 'orderId' },
    },
  },
})
