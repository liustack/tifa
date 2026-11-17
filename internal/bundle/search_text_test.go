package bundle

import (
	"encoding/json/v2"
	"io/fs"
	"testing"

	"github.com/liustack/tifa/js"
)

// searchTextPage writes query values with stringifySearch of the runtime and with a copy of the
// way it wrote them before it left alone the text that has nothing to escape.
const searchTextPage = `import { stringifySearch } from './runtime/router'

let escapes = 0
const nativeEncode = encodeURIComponent
;(globalThis as any).encodeURIComponent = (text: string) => (escapes++, nativeEncode(text))

const keptInQuery = /%(24|2C|2F|3A|3F|40|5B|5C|5D|5E|60|7B|7C|7D)/g
function before(text: string) {
  return nativeEncode(text)
    .replace(keptInQuery, (escape) => decodeURIComponent(escape))
    .replace(/'/g, '%27')
    .replace(/%20/g, '+')
}

const texts: string[] = ['', 'tea', 'green-tea_1.5~x', 'Zz09', 'a b', "it's", 'a+b', 'a&b=c', 'café', '中文', '😀', '100%', '!*()', '$,/:?@[\\]^` + "`" + `{|}', ';#']
for (let c = 0; c < 0x250; c++) texts.push('a' + String.fromCharCode(c) + 'b', String.fromCharCode(c))

;(async () => {
  const wrong: string[] = []
  let plain = 0
  let plainEscapes = 0
  for (const text of texts) {
    escapes = 0
    const got = stringifySearch({ [text]: text })
    const want = '?' + before(text) + '=' + before(text)
    if (got !== want) wrong.push(JSON.stringify(text) + ' ' + got + ' ' + want)
    if (/^[A-Za-z0-9\-._~]*$/.test(text)) {
      plain++
      plainEscapes += escapes
    }
  }
  ;(globalThis as any).result = JSON.stringify({ wrong, plain, plainEscapes, texts: texts.length })
})().catch((e) => {
  ;(globalThis as any).result = JSON.stringify({ wrong: [String(e) + ' ' + (e && e.stack)] })
})
`

// The query a link writes is the same text as before, and a value with nothing to escape (letters,
// digits and -._~) does not go through encodeURIComponent and the three replacements.
func TestStringifySearchLeavesPlainTextAlone(t *testing.T) {
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
	code, printed := i18nextApp(t, root, searchTextPage, `{ target: ["es2022"] }`)
	var result struct {
		Wrong        []string `json:"wrong"`
		Plain        int      `json:"plain"`
		PlainEscapes int      `json:"plainEscapes"`
	}
	raw := evalWithIntl(t, code)
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("%v\n%s\n%s", err, raw, printed)
	}
	if len(result.Wrong) > 0 {
		t.Errorf("%d texts are written another way, the first: %s", len(result.Wrong), result.Wrong[0])
	}
	if result.Plain == 0 {
		t.Error("no text without anything to escape")
	}
	if result.PlainEscapes != 0 {
		t.Errorf("%d calls of encodeURIComponent for %d texts with nothing to escape, want none", result.PlainEscapes, result.Plain)
	}
}
