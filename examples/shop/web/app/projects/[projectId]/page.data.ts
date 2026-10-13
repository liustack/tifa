import { defineData } from '@go-quick/data'

export default defineData({
  list: {
    source: 'orders.list',
    params: { projectId: { routeParam: 'projectId' } },
  },
})
