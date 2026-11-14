import { structuredPatch, diffWordsWithSpace, diffJson, diffChars } from 'diff'
import { page } from './route.gen'

type Hunk = ReturnType<typeof structuredPatch>['hunks'][number]

export const head = () => ({ meta: [{ title: 'Diff · Library pages' }] })

function Words({ a, b }: { a: string; b: string }) {
  const parts = diffWordsWithSpace(a, b)
  return (
    <>
      <td className="old">
        {parts
          .filter((p) => !p.added)
          .map((p, i) => (p.removed ? <del key={i}>{p.value}</del> : <span key={i}>{p.value}</span>))}
      </td>
      <td className="new">
        {parts
          .filter((p) => !p.removed)
          .map((p, i) => (p.added ? <ins key={i}>{p.value}</ins> : <span key={i}>{p.value}</span>))}
      </td>
    </>
  )
}

function HunkRows({ h }: { h: Hunk }) {
  const rows = []
  let oldNo = h.oldStart
  let newNo = h.newStart
  for (let i = 0; i < h.lines.length; i++) {
    const l = h.lines[i]
    const t = l[0]
    const text = l.slice(1)
    if (t === '-' && h.lines[i + 1] && h.lines[i + 1][0] === '+') {
      rows.push(
        <tr key={i} className="chg">
          <td>{oldNo++}</td>
          <td>{newNo++}</td>
          <Words a={text} b={h.lines[i + 1].slice(1)} />
        </tr>,
      )
      i++
    } else if (t === '-')
      rows.push(
        <tr key={i} className="del">
          <td>{oldNo++}</td>
          <td></td>
          <td className="old">
            <del>{text}</del>
          </td>
          <td></td>
        </tr>,
      )
    else if (t === '+')
      rows.push(
        <tr key={i} className="add">
          <td></td>
          <td>{newNo++}</td>
          <td></td>
          <td className="new">
            <ins>{text}</ins>
          </td>
        </tr>,
      )
    else
      rows.push(
        <tr key={i}>
          <td>{oldNo++}</td>
          <td>{newNo++}</td>
          <td>{text}</td>
          <td>{text}</td>
        </tr>,
      )
  }
  return (
    <tbody>
      <tr className="hunk">
        <th colSpan={4}>
          @@ -{h.oldStart},{h.oldLines} +{h.newStart},{h.newLines} @@
        </th>
      </tr>
      {rows}
    </tbody>
  )
}

export default function Diff() {
  const data = page.useLoaderData()
  const A = data.before.join('\n') + '\n'
  const B = data.after.join('\n') + '\n'
  const patch = structuredPatch('handbook.md', 'handbook.md', A, B, 'v3', 'v4', { context: 2 })
  const json = diffJson(data.settingsBefore, data.settingsAfter)
  const title = diffChars('Shop handbook, third edition', 'Shop handbook: fourth edition (draft)')
  return (
    <main>
      <h1>
        {title.map((p, i) => (p.added ? <ins key={i}>{p.value}</ins> : p.removed ? <del key={i}>{p.value}</del> : p.value))}
      </h1>
      <p>{patch.hunks.length} hunks</p>
      <table className="diff">
        {patch.hunks.map((h, i) => (
          <HunkRows key={i} h={h} />
        ))}
      </table>
      <pre className="json">
        {json.map((p, i) => (
          <span key={i} className={p.added ? 'add' : p.removed ? 'del' : ''}>
            {p.value}
          </span>
        ))}
      </pre>
    </main>
  )
}
