import { route } from './+route'

export const head = () => ({
  meta: [{ title: 'ONE_TITLE' }, { name: 'description', content: 'one' }],
  links: [{ rel: 'icon', href: '/icon.svg' }],
})

export default function One() {
  return <p>TEAM_ONE {route.useLoaderData()}</p>
}
