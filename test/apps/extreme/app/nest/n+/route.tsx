import { createContext, forwardRef, memo, useContext, type ReactNode } from 'react'
import { Frame } from '../../frame'
import { route } from './+route'

export const head = () => ({ meta: [{ title: '组件嵌套深度' }] })

// Every 50 levels a div, the levels between them components of the kinds apps stack: plain
// functions, memo, forwardRef and ones that read a context.
const DivEvery = 50

const Level = createContext(0)

function Plain({ children }: { children: ReactNode }) {
  return children
}

const Memo = memo(function Memo({ children }: { children: ReactNode }) {
  return children
})

const Ref = forwardRef<HTMLDivElement, { children: ReactNode }>(function Ref({ children }, _ref) {
  return children
})

function Reader({ children }: { children: ReactNode }) {
  useContext(Level)
  return children
}

const kinds = [Plain, Memo, Ref, Reader]

export default function NestPage() {
  const { depth } = route.useLoaderData()
  let tree: ReactNode = (
    <p id="leaf" className="font-mono text-sm">
      第 {depth} 层
    </p>
  )
  for (let i = depth; i > 0; i--) {
    const Kind = kinds[i % kinds.length]
    tree = <Kind>{tree}</Kind>
    if (i % DivEvery === 0) tree = <div data-level={i}>{tree}</div>
  }
  return (
    <Frame title="组件嵌套深度" size={depth} unit="层组件">
      <Level value={depth}>{tree}</Level>
    </Frame>
  )
}
