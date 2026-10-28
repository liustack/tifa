import { createContext, use, type Context, type ReactNode } from 'react'
import { Frame } from '../../frame'
import { route } from './+route'

export const head = () => ({ meta: [{ title: 'Provider 层数' }] })

type Setting = { id: number; label: string }

// The contexts are made once and kept, as modules of an app make theirs.
const contexts: Context<Setting | null>[] = []
function contextAt(i: number) {
  while (contexts.length <= i) {
    const c = createContext<Setting | null>(null)
    c.displayName = `Setting${contexts.length}`
    contexts.push(c)
  }
  return contexts[i]
}

// SettingProvider is a provider component as libraries write them: a component around the
// context's provider.
function SettingProvider({ at, children }: { at: number; children: ReactNode }) {
  const Ctx = contextAt(at)
  return <Ctx value={{ id: at + 1, label: `setting-${at + 1}` }}>{children}</Ctx>
}

function Reader({ count }: { count: number }) {
  let sum = 0
  let last = ''
  for (let i = 0; i < count; i++) {
    const s = use(contextAt(i))
    if (!s) throw new Error(`no provider for context ${i}`)
    sum += s.id
    last = s.label
  }
  return (
    <p className="font-mono text-sm">
      读到 <span id="sum">{sum}</span>，最里一层 <span id="last">{last}</span>
    </p>
  )
}

export default function ProvidersPage() {
  const { count } = route.useLoaderData()
  let tree: ReactNode = <Reader count={count} />
  for (let i = count - 1; i >= 0; i--) tree = <SettingProvider at={i}>{tree}</SettingProvider>
  return (
    <Frame title="Provider 层数" size={count} unit="层 Provider">
      {tree}
    </Frame>
  )
}
