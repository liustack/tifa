import { createContext, useContext, useEffect, useId, useState } from 'react'
import { renderToString } from 'react-dom/server.edge'

const Tenant = createContext('unset')
const attributes = { title: '' }
let evaluations = 0

export const staticElement = () => <svg viewBox="0 0 8 8"><path d="M0 0h8v8H0z" /></svg>
export const mutableElement = () => <div style={{ opacity: 1 }} />
export const refElement = () => <input ref={() => {}} />

function ContextValue() {
  return <span data-tenant={useContext(Tenant)}>scope</span>
}

const contextElement = () => <ContextValue />
const spreadElement = () => <span {...attributes}>spread</span>

function Page({ name }: { name: string }) {
  const id = useId()
  const [initial] = useState(() => name)
  useEffect(() => {}, [++evaluations])
  return (
    <main>
      {staticElement()}
      {contextElement()}
      {spreadElement()}
      {mutableElement()}
      {refElement()}
      <label htmlFor={id}>{name}</label>
      <input id={id} defaultValue={initial} />
      <ul>{[name, '<script>订单 & 🙂</script>'].map(value => <li key={value}>{value}</li>)}</ul>
    </main>
  )
}

export function renderCase(name: string) {
  attributes.title = name
  return renderToString(<Tenant.Provider value={name}><Page name={name} /></Tenant.Provider>)
}

export const effectEvaluations = () => evaluations
