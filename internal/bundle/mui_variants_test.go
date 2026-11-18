package bundle

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

// muiVariantsPage renders styled components of createStyled of @mui/system, as its ES module and as
// its CommonJS file, whose styles and whose theme have variants that match by props, by ownerState,
// by a function, by nothing, by a value that is undefined, null or NaN, and through an inherited key,
// and gives the HTML of each case. The class names are hashes of the styles, so the HTML is the same
// only when the styles are.
const muiVariantsPage = `import './host'
import { createElement as h } from 'react'
import { renderToString } from 'react-dom/server.edge'
import { createStyled } from '@mui/system'
import cjs from './create-styled.cjs'

const inherited = Object.create({ tone: 'warm' })
const hidden = Object.defineProperty({}, 'tone', { value: 'cold', enumerable: false })
const variants = [
  { props: { tone: 'warm' }, style: { color: 'orange' } },
  { props: { tone: 'cold', shape: 'round' }, style: { borderRadius: 9 } },
  { props: { shape: 'round' }, style: { borderRadius: 4 } },
  { props: {}, style: { margin: 1 } },
  { props: undefined, style: { margin: 2 } },
  { props: null, style: { margin: 3 } },
  { props: (p: any) => p.tone === 'warm' && p.size === 3, style: { padding: 5 } },
  { props: (p: any) => p.ownerState?.tone === 'cold', style: (p: any) => ({ padding: p.tone === 'cold' ? 6 : 7 }) },
  { props: { tone: 'warm', size: 3 }, style: (p: any) => ({ width: p.size * 10 }) },
  { props: { missing: undefined }, style: { outline: 'none' } },
  { props: { nothing: null }, style: { zIndex: 1 } },
  { props: { nan: NaN }, style: { zIndex: 2 } },
  { props: { 0: 'zero', tone: 'warm' }, style: { top: 0 } },
  { props: inherited, style: { left: 1 } },
  { props: hidden, style: { right: 1 } },
  { props: { tone: 'warm', shape: 'round', size: 3 }, style: [{ gap: 1 }, { gap: 2 }] },
]
const themeVariants = [
  { props: { tone: 'warm' }, style: { background: 'beige' } },
  { props: { tone: 'cold', size: 3 }, style: { background: 'navy' } },
  { props: (p: any) => p.size === 3, style: { fontSize: 30 } },
  { props: {}, style: { lineHeight: 1 } },
]
const theme = { components: { MuiThing: { variants: themeVariants } } }
const cases: Record<string, any>[] = [
  {},
  { tone: 'warm' },
  { tone: 'cold' },
  { tone: 'cold', shape: 'round' },
  { shape: 'round' },
  { tone: 'warm', size: 3 },
  { tone: 'warm', shape: 'round', size: 3 },
  { ownerState: { tone: 'warm' } },
  { ownerState: { tone: 'cold', shape: 'round' } },
  { ownerState: { tone: 'cold' }, shape: 'round' },
  { ownerState: { tone: 'warm', size: 3 }, tone: 'cold' },
  { ownerState: null, tone: 'warm' },
  { ownerState: undefined, tone: 'warm' },
  { ownerState: { nothing: null } },
  { ownerState: { missing: undefined } },
  { ownerState: { 0: 'zero', tone: 'warm' } },
  { 0: 'zero', tone: 'warm' },
  { ownerState: { size: 3 }, tone: 'warm', shape: 'round' },
]

// ownerState that throws when it is read, beside a prop that matches directly: the old code never
// reads it, so neither may the new.
const throwing = () => ({ theme: {}, tone: 'warm', get ownerState() { throw new Error('unused ownerState') } })
const lazy = () => ({ theme: {}, shape: 'round', tone: 'cold', get ownerState() { return { tone: 'cold', shape: 'round' } } })

const out: string[] = []
for (const [form, createStyled2] of [['esm', createStyled], ['cjs', cjs]] as [string, any][]) {
  const styled = createStyled2({})
  const Plain = styled('div', { shouldForwardProp: () => false })({ color: 'red', variants })
  const Themed = styled('div', { name: 'MuiThing', slot: 'Root', shouldForwardProp: () => false })({ color: 'red' })
  const direct = (C: any, make: () => any) => {
    const element = h(C, null)
    ;(element as any).props = make()
    try {
      return renderToString(element)
    } catch (e) {
      return 'throws ' + (e as Error).message
    }
  }
  const Direct = styled('div', { shouldForwardProp: () => false })(() => ({ variants: [{ props: { tone: 'warm' }, style: { color: 'red' } }] }))
  out.push(form + ' throwing ' + direct(Direct, throwing))
  out.push(form + ' lazy ' + direct(Plain, lazy))
  const Both = styled('div', { name: 'MuiThing', shouldForwardProp: (p: string) => p === 'children' })(({ ownerState }: any) => ({ margin: ownerState?.size ?? 0, variants }))
  for (const props of cases) {
    for (const [name, C, extra] of [['plain', Plain, {}], ['themed', Themed, { theme }], ['both', Both, { theme }]] as [string, any, any][]) {
      let html: string
      try {
        html = renderToString(h(C, { ...props, ...extra }, 'x'))
      } catch (e) {
        html = 'throws ' + (e as Error).message
      }
      out.push(form + ' ' + name + ' ' + JSON.stringify(props) + ' ' + html)
    }
  }
}
;(globalThis as any).result = JSON.stringify(out)
`

// variantsLoop is the loop over the keys of a variant's props as it is without the plugin, minified.
var variantsLoop = regexp.MustCompile("for\\((?:let|const|var) (\\w+) in \\w+\\.props\\)if\\(\\w+\\[\\w+\\]!==\\w+\\.props\\[\\w+\\]&&\\w+\\.ownerState\\?\\.\\[\\w+\\]!==\\w+\\.props\\[\\w+\\]\\)continue \\w+")

func muiVariantsRender(t *testing.T, root string) ([]string, string, string) {
	t.Helper()
	writeTree(t, root, map[string]string{
		"variants.tsx":      muiVariantsPage,
		"create-styled.cjs": "module.exports = require('./node_modules/@mui/system/createStyled/createStyled.js').default\n",
	})
	code, printed := buildPrinting(t, root, "variants.tsx")
	var out []string
	if err := json.Unmarshal([]byte(evalResult(t, code)), &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 2*(3*18+2) {
		t.Fatalf("%d pages", len(out))
	}
	return out, code, printed
}

// processStyleVariants of @mui/system reads variant.props and its values again and again for each of
// the variants of a component. The tifa plugin reads each once, and props.ownerState once for the
// call, with the same loop over the same keys. The styles of components with variants that match in
// every way the loop can, in the ES module and in the CommonJS file, are the same as without the
// change. A createStyled of another text is left as it is, with a warning.
func TestSSRMUIProcessStyleVariantsReadsEachOnce(t *testing.T) {
	changed, code, printed := muiVariantsRender(t, withMUIFiles(t, nil, nil))
	if strings.Contains(printed, notRecognized) {
		t.Errorf("the build prints:\n%s", printed)
	}
	// The cases reach the ways a variant matches, or the check proves nothing.
	all := strings.Join(changed, "\n")
	for _, want := range []string{"color:orange", "border-radius:9px", "padding:5px", "padding:6px", "width:30px", "color:red", "left:1px", "top:0", "background:navy", "font-size:30px"} {
		if !strings.Contains(all, want) {
			t.Errorf("no page has %s", want)
		}
	}
	if n := len(variantsLoop.FindAllString(code, -1)); n != 0 {
		t.Errorf("%d loops over variant.props read it again and again", n)
	}
	if strings.Contains(all, "unused ownerState") {
		t.Error("a page reads the ownerState that its props match without")
	}
	// A file with a change anywhere in processStyleVariants, in the lines the plugin changes or in
	// any other, stays as it is.
	for _, file := range []string{"createStyled/createStyled.mjs", "createStyled/createStyled.js"} {
		for _, edit := range [][2]string{
			{"  let mergedState; // We might not need it, initialized lazily\n", "  let mergedState; // We might not need it, initialized lazily\n  /* changed */\n"},
			{"  return results;\n}\nexport default function createStyled", "  return results; /* changed */\n}\nexport default function createStyled"},
			{"  return results;\n}\nfunction createStyled", "  return results; /* changed */\n}\nfunction createStyled"},
		} {
			applied := false
			root := withMUIFiles(t, []string{file}, func(source string) string {
				if strings.Count(source, edit[0]) != 1 {
					return source
				}
				applied = true
				return strings.Replace(source, edit[0], edit[1], 1)
			})
			if !applied {
				continue
			}
			plain, code, printed := muiVariantsRender(t, root)
			if !strings.Contains(printed, notRecognized+"@mui/system 9.5.0: ") || !strings.Contains(printed, file+" has no processStyleVariants the tifa plugin knows") {
				t.Errorf("an unknown %s (%q): the build prints:\n%s", file, edit[1], printed)
			}
			if n := len(variantsLoop.FindAllString(code, -1)); n != 1 {
				t.Errorf("an unknown %s (%q): %d loops read variant.props again and again, want 1", file, edit[1], n)
			}
			for i, p := range plain {
				if changed[i] != p {
					t.Errorf("an unknown %s (%q): page %d is\n%s\nwant\n%s", file, edit[1], i, p, changed[i])
				}
			}
		}
	}
}
