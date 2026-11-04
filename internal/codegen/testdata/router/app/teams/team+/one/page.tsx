import { page } from './route.gen'

export const head = () => ({
  meta: [{ title: 'ONE_TITLE' }, { name: 'description', content: 'one' }],
  links: [{ rel: 'icon', href: '/icon.svg' }],
  scripts: [{ children: 'window.ONE = 1' }, { type: 'application/ld+json', children: '{"@type":"Thing"}' }],
  styles: [{ children: '.one{color:teal}' }],
})

export default function One() {
  return <p>TEAM_ONE {page.useLoaderData()}</p>
}
