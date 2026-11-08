// Prints the data of the order list for each URL of a list of URLs with a query string, as JSON, for
// the check that the filters of the port are the ones of the Go code:
//   node test/compare/domain/dumpfilters.ts '["/projects/studio?status=shipped", ...]'
// Node 22.18 and later run TypeScript as it is, by removing its types, so this needs no build.
import { loadOrderList, loadProject, loadShell } from './loaders.ts'

const out: Record<string, Record<string, unknown>> = {}
for (const url of JSON.parse(process.argv[2] ?? '[]') as string[]) {
  const [path, query = ''] = url.split('?')
  const p = loadProject(path.split('/')[2])
  out[url] = {
    'app/layout': loadShell(undefined),
    'app/projects/project+/layout': p,
    'app/projects/project+/route': loadOrderList(p, new URLSearchParams(query)),
  }
}
process.stdout.write(JSON.stringify(out))
