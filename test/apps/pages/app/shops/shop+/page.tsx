import { Link } from '@tanstack/react-router'
import { page, type PageData, type Params } from './route.gen'

export default function Overview() {
  const overview: PageData = page.useLoaderData()
  const params: Params = page.useParams()
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

export const head = ({ loaderData }: { loaderData?: PageData }) => ({
  meta: [{ title: loaderData ? loaderData.shop : 'Shop' }],
})
