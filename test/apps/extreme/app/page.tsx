import { scenarios } from './scenarios'

export const head = () => ({ meta: [{ title: '极限场景' }] })

export default function Index() {
  return (
    <main className="mx-auto flex max-w-4xl flex-col gap-8 px-6 py-12">
      <header className="flex flex-col gap-2 border-b border-foreground pb-4">
        <p className="font-mono text-[11px] tracking-[0.14em] text-muted-foreground">TIFA · EXTREME</p>
        <h1 className="font-heading text-4xl font-medium tracking-[0.04em]">极限场景</h1>
      </header>
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b text-left text-muted-foreground">
            <th className="py-2 font-normal">场景</th>
            <th className="py-2 font-normal">压什么</th>
            <th className="py-2 font-normal">档位</th>
          </tr>
        </thead>
        <tbody>
          {scenarios.map((s) => (
            <tr key={s.path} className="border-b align-top">
              <th scope="row" className="py-3 pr-4 text-left font-heading text-base font-normal">
                {s.title}
              </th>
              <td className="py-3 pr-4 text-muted-foreground">{s.note}</td>
              <td className="py-3">
                <ul className="flex flex-wrap gap-2 font-mono text-xs">
                  {s.sizes.map((size) => (
                    <li key={size}>
                      <a href={`${s.path}/${size}`} className="border px-2 py-0.5 hover:border-foreground">
                        {size}
                      </a>
                    </li>
                  ))}
                </ul>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </main>
  )
}
