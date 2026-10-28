import { Frame } from '../../frame'
import { route, type Data } from './+route'

export const head = () => ({ meta: [{ title: '数据嵌套' }] })

type Node = NonNullable<Data['root']>

export default function JsonPage() {
  const tree = route.useLoaderData()
  // Walk the chain in a loop: the page reads deep data, the engine parsed it.
  const path: Node[] = []
  for (let node = tree.root; node; node = node.child ?? null) path.push(node)
  const shown = path.length > 20 ? [...path.slice(0, 10), ...path.slice(-10)] : path
  return (
    <Frame title="数据嵌套" size={tree.depth} unit="层 JSON">
      <p className="text-sm">
        分类链共 <span id="levels">{path.length}</span> 级
      </p>
      <ol className="grid grid-cols-2 gap-x-6 font-mono text-xs md:grid-cols-4">
        {shown.map((n) => (
          <li key={n.level} className="border-b py-1">
            {n.name}
          </li>
        ))}
      </ol>
    </Frame>
  )
}
