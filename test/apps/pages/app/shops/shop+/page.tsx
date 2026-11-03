import { Link, useMatch } from '@tanstack/react-router'
import { page, type PageData, type Params } from './route.gen'

export default function Overview() {
  const overview: PageData = page.useLoaderData()
  const params: Params = page.useParams()
  return (
    <>
      <ul>
        {overview.items?.map((id) => (
          <li key={id}>
            <Link to="/shops/$shop/items/$item" params={{ shop: params.shop, item: id }}>
              {id}
            </Link>
          </li>
        ))}
      </ul>
      <Loaded />
    </>
  )
}

// Loaded shows the time the router says the data of the page loaded at, which the server writes
// as 0. The hydration must render what the server did.
function Loaded() {
  const at = useMatch({ strict: false, select: (match) => match.updatedAt })
  return <p id="loaded">{at}</p>
}

export const head = ({ loaderData }: { loaderData?: PageData }) => ({
  meta: [{ title: loaderData ? loaderData.shop : 'Shop' }],
})
