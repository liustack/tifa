package bundle

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

// muiClassNamePage asks the ClassNameGenerator of @mui/utils, as an ES module and as a CommonJS
// file, for names while it has the default generator, after configure gave it others (one that
// records its calls, one that gives the name back as the default does, one that throws, and a value
// that is not a function), and after reset. Each step also renders a MUI Button, which names its
// classes through generateUtilityClass.
const muiClassNamePage = `import './host'
import { createElement as h } from 'react'
import { renderToString } from 'react-dom/server.edge'
import { unstable_ClassNameGenerator as esm } from '@mui/material/className'
import generateUtilityClass from '@mui/material/generateUtilityClass'
import Button from '@mui/material/Button'
import cjs from './generator.cjs'

const out: string[] = []
let calls: string[] = []
const record = function (this: unknown, ...args: unknown[]) {
  calls.push(String(this) + ' ' + args.length + ' ' + args.map(String).join())
  return 'x-' + String(args[0])
}
const step = (name: string, f: () => unknown) => {
  calls = []
  let v: string
  try {
    v = JSON.stringify(f())
  } catch (e) {
    v = 'throws ' + (e as Error).name + ': ' + (e as Error).message
  }
  out.push(name + ' = ' + v + ', calls: ' + calls.join(' | '))
}
const names = (g: typeof esm) => [
  g.generate('MuiButton'),
  g.generate(''),
  g.generate(undefined as any),
  g.generate(5 as any),
  (g.generate as any)('MuiChip', 'extra'),
]
const page = () => [
  generateUtilityClass('MuiButton', 'root'),
  generateUtilityClass('MuiButton', 'disabled'),
  renderToString(h(Button, { variant: 'contained', disabled: true }, 'Go')),
]

for (const [form, g] of [['esm', esm], ['cjs', cjs]] as const) {
  step(form + ' default', () => names(g))
  step(form + ' default page', page)
  g.configure(record)
  step(form + ' recording', () => names(g))
  step(form + ' recording page', page)
  g.configure((name: string) => name)
  step(form + ' same as the default', () => names(g))
  g.configure(() => {
    throw new Error('generator')
  })
  step(form + ' throwing', () => names(g))
  g.configure(undefined as any)
  step(form + ' not a function', () => names(g))
  g.reset()
  step(form + ' reset', () => names(g))
  step(form + ' reset page', page)
}
;(globalThis as any).result = JSON.stringify(out)
`

// muiClassNameWrapper is the generate method of ClassNameGenerator as it is without the plugin,
// minified: a call of the generator, also of the default one, which gives the name back.
var muiClassNameWrapper = regexp.MustCompile(`generate\((\w+)\)\{return [\w$]+\((\w+)\)\}`)

func muiClassNameRender(t *testing.T, root string) ([]string, string, string) {
	t.Helper()
	writeTree(t, root, map[string]string{
		"class-name.tsx": muiClassNamePage,
		"generator.cjs":  "module.exports = require('./node_modules/@mui/utils/ClassNameGenerator/ClassNameGenerator.js').default\n",
	})
	code, printed := buildPrinting(t, root, "class-name.tsx")
	var out []string
	if err := json.Unmarshal([]byte(evalResult(t, code)), &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 2*9 {
		t.Fatalf("%d steps: %s", len(out), strings.Join(out, "\n"))
	}
	return out, code, printed
}

// generateUtilityClass of MUI asks ClassNameGenerator for the name of each class, and its generate
// calls the generator, which by default gives the name back. The tifa plugin has generate give the
// name back itself while the generator is the default. A generator that configure gave is called as
// before, and reset brings the default back. The names, the HTML, the calls of the generator and
// the errors are the same, in the ES module and in the CommonJS file, and a ClassNameGenerator of
// another text is left as it is, with a warning.
func TestSSRClassNameGeneratorGivesTheNameBackWhileItIsTheDefault(t *testing.T) {
	files := []string{"ClassNameGenerator/ClassNameGenerator.mjs", "ClassNameGenerator/ClassNameGenerator.js"}
	changed, code, printed := muiClassNameRender(t, withMUIFiles(t, "utils", files, nil))
	if strings.Contains(printed, notRecognized) {
		t.Errorf("the build prints:\n%s", printed)
	}
	if n := len(muiClassNameWrapper.FindAllString(code, -1)); n != 0 {
		t.Errorf("generate calls the default generator in %d places: %v", n, muiClassNameWrapper.FindAllString(code, -1))
	}
	plain, code, printed := muiClassNameRender(t, withMUIFiles(t, "utils", files, func(source string) string {
		return strings.Replace(source, "      return generate(componentName);", "      return generate(componentName); /* changed */", 1)
	}))
	for _, file := range []string{"ClassNameGenerator.mjs", "ClassNameGenerator.js"} {
		if !strings.Contains(printed, notRecognized+"@mui/utils 9.5.0: ") || !strings.Contains(printed, file+" has no ClassNameGenerator the tifa plugin knows") {
			t.Errorf("an unknown %s: the build prints:\n%s", file, printed)
		}
	}
	if n := len(muiClassNameWrapper.FindAllString(code, -1)); n != 2 {
		t.Errorf("an unknown ClassNameGenerator: generate calls the generator in %d places, want 2", n)
	}
	for i, p := range plain {
		if changed[i] != p {
			t.Errorf("step %d gives\n%s\nwant\n%s", i, changed[i], p)
		}
	}
	// configure still changes the names, and reset brings the default back.
	for _, want := range []string{
		`esm default = ["MuiButton","",null,5,"MuiChip"], calls: `,
		`esm recording = ["x-MuiButton","x-","x-undefined","x-5","x-MuiChip"], calls: undefined 1 MuiButton | undefined 1  | undefined 1 undefined | undefined 1 5 | undefined 1 MuiChip`,
		`cjs reset = ["MuiButton","",null,5,"MuiChip"], calls: `,
	} {
		if !strings.Contains(strings.Join(changed, "\n"), want) {
			t.Errorf("no step %s in\n%s", want, strings.Join(changed, "\n"))
		}
	}
	for _, step := range changed {
		if strings.HasPrefix(step, "esm recording page") && !strings.Contains(step, "x-MuiButton-root") {
			t.Errorf("configure does not change the classes of the page: %s", step)
		}
	}
}
