import { useState } from 'react'
import { configureStore, createSlice, createEntityAdapter, createSelector, type PayloadAction } from '@reduxjs/toolkit'
import { Provider, useSelector } from 'react-redux'
import { create } from 'zustand'
import { QueryClient, QueryClientProvider, useQuery } from '@tanstack/react-query'
import { produce } from 'immer'
import _ from 'lodash'
import { page, type PageData } from './route.gen'

type Ticket = PageData['tickets'][number]
type Event = PageData['events'][number]
type User = { id: string; name: string; team: string }
type Stats = { solved: number; median: number }

export const head = () => ({ meta: [{ title: 'State · Library pages' }] })

const adapter = createEntityAdapter<Ticket>({ sortComparer: (a, b) => a.opened - b.opened || a.id.localeCompare(b.id) })
const tickets = createSlice({
  name: 'tickets',
  initialState: adapter.getInitialState({ filter: 'all' }),
  reducers: {
    loaded: adapter.setAll,
    event(state, { payload: e }: PayloadAction<Event>) {
      const t = state.entities[e.id]
      if (!t) return
      if (e.type === 'reply') t.replies++
      else if (e.type === 'assign') t.agent = e.who
      else if (e.type === 'close') t.status = 'closed'
      else if (e.type === 'reopen') t.status = 'open'
      else if (!t.tags.includes(e.tag)) t.tags.push(e.tag)
    },
    filter(state, { payload }: PayloadAction<string>) {
      state.filter = payload
    },
  },
})
type State = { tickets: ReturnType<typeof tickets.reducer> }
const sel = adapter.getSelectors((s: State) => s.tickets)
const selectOpen = createSelector([sel.selectAll], (all) => all.filter((t) => t.status === 'open' || t.status === 'pending'))
const selectByAgent = createSelector([selectOpen], (open) =>
  _.mapValues(_.groupBy(open, 'agent'), (ts) => ({ n: ts.length, urgent: _.filter(ts, { prio: 'urgent' }).length, replies: _.sumBy(ts, 'replies') })),
)
const selectTags = createSelector([sel.selectAll], (all) => _.orderBy(_.toPairs(_.countBy(_.flatMap(all, 'tags'))), [1, 0], ['desc', 'asc']))

type Ui = { density: string; collapsed: Record<string, boolean>; toggle: (k: string) => void }
const useUi = create<Ui>((set) => ({
  density: 'compact',
  collapsed: {},
  toggle: (k) => set((s) => ({ collapsed: { ...s.collapsed, [k]: !s.collapsed[k] } })),
}))

function AgentTable() {
  const by = useSelector(selectByAgent)
  const density = useUi((s) => s.density)
  return (
    <table className={density}>
      <tbody>
        {_.sortBy(Object.keys(by)).map((a) => (
          <tr key={a}>
            <td>{_.capitalize(a)}</td>
            <td>{by[a].n}</td>
            <td>{by[a].urgent}</td>
            <td>{by[a].replies}</td>
          </tr>
        ))}
      </tbody>
    </table>
  )
}

function TicketList() {
  const open = useSelector(selectOpen)
  const collapsed = useUi((s) => s.collapsed)
  const { data: users } = useQuery({ queryKey: ['users'], queryFn: (): Record<string, User> => ({}), staleTime: Infinity })
  return (
    <ul>
      {open.map((t) => (
        <li key={t.id} className={'prio-' + t.prio + (collapsed[t.agent] ? ' collapsed' : '')}>
          <b>{t.id}</b> {t.title} · {users![t.agent].name} ({users![t.agent].team}) · {t.status} · {t.replies} replies · SLA {_.get(t, 'meta.sla.hours')}h
          {t.tags.length > 0 && <span className="tags">{_.join(_.map(t.tags, _.upperFirst), ', ')}</span>}
        </li>
      ))}
    </ul>
  )
}

function TagCloud() {
  const tags = useSelector(selectTags)
  const { data: stats } = useQuery({ queryKey: ['stats', { range: '30d' }], queryFn: (): Stats => ({ solved: 0, median: 0 }), staleTime: Infinity })
  return (
    <section>
      <p>
        {stats!.solved} solved, median {stats!.median}h
      </p>
      {tags.map(([t, n]) => (
        <span key={t} style={{ fontSize: 10 + n / 10 + 'px' }}>
          {t}{' '}
        </span>
      ))}
    </section>
  )
}

// Replays every event through the store, then fills the query cache and the zustand store.
function setup(data: PageData) {
  const store = configureStore({
    reducer: { tickets: tickets.reducer },
    middleware: (g) => g({ serializableCheck: false, immutableCheck: false }),
  })
  store.dispatch(tickets.actions.loaded(_.cloneDeep(data.tickets)))
  for (const e of data.events) store.dispatch(tickets.actions.event(e))
  const qc = new QueryClient()
  qc.setQueryData(
    ['users'],
    _.keyBy(
      data.agents.map((a, i) => ({ id: a, name: _.startCase(a + ' ' + data.agents[(i + 3) % 8]), team: i % 2 ? 'EU' : 'US' })),
      'id',
    ),
  )
  const solved = sel.selectAll(store.getState()).filter((t) => t.status === 'solved' || t.status === 'closed')
  qc.setQueryData(['stats', { range: '30d' }], { solved: solved.length, median: _.sortBy(solved.map((t) => t.meta.sla.hours))[solved.length >> 1] })
  useUi.setState(
    produce(useUi.getState(), (d) => {
      for (const a of data.agents.slice(0, 3)) d.collapsed[a] = true
    }),
  )
  return { store, qc }
}

export default function State() {
  const data = page.useLoaderData()
  const [{ store, qc }] = useState(() => setup(data))
  return (
    <Provider store={store}>
      <QueryClientProvider client={qc}>
        <main>
          <h1>Support desk</h1>
          <AgentTable />
          <TicketList />
          <TagCloud />
        </main>
      </QueryClientProvider>
    </Provider>
  )
}
