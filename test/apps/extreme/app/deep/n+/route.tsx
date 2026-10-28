import type { ReactNode } from 'react'
import { Frame } from '../../frame'
import { route } from './+route'

export const head = () => ({ meta: [{ title: 'DOM 嵌套深度' }] })

export default function DeepPage() {
  const { depth } = route.useLoaderData()
  let tree: ReactNode = (
    <span id="bottom" className="font-mono text-xs">
      {depth}
    </span>
  )
  for (let i = depth; i > 0; i--) tree = <div className="pl-px">{tree}</div>
  return (
    <Frame title="DOM 嵌套深度" size={depth} unit="层 div">
      {tree}
    </Frame>
  )
}
