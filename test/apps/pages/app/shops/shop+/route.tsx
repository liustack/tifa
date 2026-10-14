import { Link } from '@tanstack/react-router'
import { route, type Data, type Params } from './+route'

export default function Overview() {
  const overview: Data = route.useLoaderData()
  const params: Params = route.useParams()
  return (
    <ul>
      {overview.items?.map((id) => (
        <li key={id}>
          <Link to="/shops/$shop/items/$item" params={{ shop: params.shop, item: id }}>
            {id}
          </Link>
        </li>
      ))}
    </ul>
  )
}

export const head = ({ loaderData }: { loaderData?: Data }) => ({
  meta: [{ title: loaderData ? loaderData.shop : 'Shop' }],
})
