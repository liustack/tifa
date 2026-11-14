import { Link } from '@tanstack/react-router'

export const head = () => ({ meta: [{ title: 'Library pages' }] })

export default function Home() {
  return (
    <main>
      <h1>Library pages</h1>
      <ul>
        <li>
          <Link to="/state">State</Link>: a support desk replayed through Redux Toolkit and immer
        </li>
        <li>
          <Link to="/chart">Chart</Link>: a sales dashboard drawn with recharts and d3
        </li>
        <li>
          <Link to="/dates">Dates</Link>: a team calendar made with date-fns and dayjs
        </li>
        <li>
          <Link to="/diff">Diff</Link>: a document review made with jsdiff
        </li>
      </ul>
    </main>
  )
}
