import { route } from './+route'

// The App, Table and Entry of eknkc/ssr-benchmark (modules/react/src/App.tsx). The data is ready
// before the render, so there is no Suspense or use().
export default function App() {
  // A Go slice may be nil, so its TypeScript type includes null.
  return <Table data={route.useLoaderData() ?? []} />
}

function Table({ data }: { data: { id: string; name: string }[] }) {
  return (
    <table>
      <tbody>
        {data.map((entry) => (
          <Entry key={entry.id} entry={entry} />
        ))}
      </tbody>
    </table>
  )
}

function Entry(props: { entry: { id: string; name: string } }) {
  return (
    <tr>
      <td>{props.entry.id}</td>
      <td>{props.entry.name}</td>
    </tr>
  )
}
