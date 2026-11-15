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
        <li>
          <Link to="/shadcn">shadcn/ui</Link>: a project dashboard made of shadcn/ui components on Radix
        </li>
        <li>
          <Link to="/query">Query</Link>: orders loaded into TanStack Query, dehydrated and shown in TanStack Table
        </li>
        <li>
          <Link to="/forms">Forms</Link>: a checkout checked by zod and shown by react-hook-form
        </li>
        <li>
          <Link to="/intl">Intl</Link>: hotel results in three languages with react-intl
        </li>
        <li>
          <Link to="/motion">Motion</Link>: a landing page animated with Motion
        </li>
      </ul>
    </main>
  )
}
