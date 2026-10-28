import { Frame } from '../../frame'
import { route, type Data } from './+route'

export const head = () => ({ meta: [{ title: 'DOM 数量' }] })

type File = NonNullable<Data['files']>[number]

export default function DomPage() {
  const diff = route.useLoaderData()
  const files = diff.files ?? []
  return (
    <Frame title={diff.title} size={diff.elements} unit="个元素">
      <p className="text-sm text-muted-foreground">
        <span id="files">{files.length}</span> 个文件
      </p>
      {files.map((f) => (
        <FileDiff key={f.path} file={f} />
      ))}
    </Frame>
  )
}

function FileDiff({ file }: { file: File }) {
  return (
    <section className="border bg-card">
      <div className="flex items-center gap-3 border-b px-3 py-2">
        <h3 className="font-mono text-xs">{file.path}</h3>
        <span className="font-mono text-xs text-[#2f6b3f]">+{file.additions}</span>
        <span className="font-mono text-xs text-destructive">−{file.deletions}</span>
      </div>
      <table className="w-full font-mono text-xs leading-5">
        <tbody>
          {(file.lines ?? []).map((l, i) => (
            <tr key={i} className={l.k === '+' ? 'bg-[#e6f1e8]' : l.k === '-' ? 'bg-[#f6e3e0]' : undefined}>
              <td className="w-12 pr-2 text-right text-muted-foreground select-none">{l.o || ''}</td>
              <td className="w-12 pr-2 text-right text-muted-foreground select-none">{l.n || ''}</td>
              <td className="whitespace-pre">
                <span className="inline-block w-4 text-center select-none">{l.k}</span>
                <span>{l.t}</span>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  )
}
