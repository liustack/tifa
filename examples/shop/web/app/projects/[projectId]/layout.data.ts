import { defineData } from '@go-quick/data'

export default defineData({
  project: {
    source: 'projects.detail',
    params: { projectId: { routeParam: 'projectId' } },
  },
})
