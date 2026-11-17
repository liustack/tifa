package bundle

import (
	"encoding/json/v2"
	"io/fs"
	"testing"

	"github.com/liustack/tifa/js"
)

// searchWritePage writes queries with stringifySearch of the runtime and with a copy of the way
// it wrote them before it stopped building a list of parts.
const searchWritePage = `import { stringifySearch } from './runtime/router'

const keptInQuery = /%(24|2C|2F|3A|3F|40|5B|5C|5D|5E|60|7B|7C|7D)/g
function part(text: string) {
  return encodeURIComponent(text)
    .replace(keptInQuery, (escape) => decodeURIComponent(escape))
    .replace(/'/g, '%27')
    .replace(/%20/g, '+')
}
function before(search: Record<string, unknown>): string {
  const parts: string[] = []
  for (const key in search) {
    const value = search[key]
    if (value === undefined) continue
    for (const item of Array.isArray(value) ? value : [value]) parts.push(part(key) + '=' + part(String(item)))
  }
  return parts.length > 0 ? '?' + parts.join('&') : ''
}

let arrays = 0
const join = Array.prototype.join
Array.prototype.join = function (this: unknown[], separator?: string) {
  arrays++
  return join.call(this, separator)
}

const cases: Array<Record<string, unknown>> = [
  {},
  { a: 'b' },
  { a: 'b', c: 'd e', f: "it's" },
  { a: undefined },
  { a: undefined, b: 1 },
  { a: 1, b: undefined },
  { a: [] },
  { a: [], b: 'x' },
  { a: ['x'] },
  { a: ['x', 'y', 'z'], b: 'w' },
  { a: 'x', b: ['y', 'z'], c: 'w' },
  { a: 0, b: false, c: true, d: null, e: '' },
  { '': '' },
  { 'a b': 'c&d=e', 'café': '中文' },
  { a: [undefined, 'x', 1] },
  Object.create(null, { a: { value: 'b', enumerable: true } }),
  Object.assign(Object.create({ inherited: 'yes' }), { own: 'no' }),
]

;(async () => {
  const wrong: string[] = []
  arrays = 0
  for (const search of [{}, { a: 'b' }, { a: 'b', c: 'd' }, { a: 1, b: undefined }]) stringifySearch(search)
  const single = arrays
  for (const search of cases) {
    const got = stringifySearch(search)
    const want = before(search)
    if (got !== want) wrong.push(JSON.stringify(got) + ' ' + JSON.stringify(want))
  }
  ;(globalThis as any).result = JSON.stringify({ wrong, n: cases.length, arrays: single })
})().catch((e) => {
  ;(globalThis as any).result = JSON.stringify({ wrong: [String(e) + ' ' + (e && e.stack)] })
})
`

// stringifySearch writes the queries it always wrote, an empty one, an undefined value, an empty
// list and a repeated name among them.
func TestStringifySearchWritesTheSameQueries(t *testing.T) {
	root := prefixLinksRoot(t)
	files := map[string]string{}
	err := fs.WalkDir(js.Runtime, "runtime", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := fs.ReadFile(js.Runtime, path)
		files[".tifa/"+path] = string(data)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	writeTree(t, root, files)
	code, printed := i18nextApp(t, root, searchWritePage, `{ target: ["es2022"] }`)
	var result struct {
		Wrong  []string `json:"wrong"`
		N      int      `json:"n"`
		Arrays int      `json:"arrays"`
	}
	raw := evalWithIntl(t, code)
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("%v\n%s\n%s", err, raw, printed)
	}
	if len(result.Wrong) > 0 {
		t.Errorf("%d of %d queries are written another way, the first: %s", len(result.Wrong), result.N, result.Wrong[0])
	}
	// An empty query and one of single values make no array: the writer builds one string.
	if result.Arrays != 0 {
		t.Errorf("stringifySearch made %d arrays for queries of single values, want none", result.Arrays)
	}
}
