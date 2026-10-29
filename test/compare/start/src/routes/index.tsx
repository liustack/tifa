import { createFileRoute, redirect } from '@tanstack/react-router'

// GET / goes to the first shop, as app/route.go of the shop does.
export const Route = createFileRoute('/')({
  beforeLoad: () => {
    throw redirect({ to: '/projects/$project', params: { project: 'studio' }, statusCode: 302 })
  },
})
