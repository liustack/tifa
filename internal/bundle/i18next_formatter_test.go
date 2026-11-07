package bundle

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/liustack/quickjs"
	"github.com/liustack/tifa/internal/ssr"
)

// i18nextFormatterPage formats a new value on each call, with a format of i18next's own and one
// the app adds with addCached. i18next keeps a formatter for each language and options of a call,
// the value included, so without the plugin's change each new value stays in the VM for good.
const i18nextFormatterPage = `import './host'
import i18next from 'i18next'

const i18n = i18next.createInstance()
i18n.init({
  lng: 'en',
  initAsync: false,
  resources: { en: { translation: { price: 'Costs {{val, number(minimumFractionDigits: 2)}}', mine: 'Mine {{val, shout}}' } } },
})
i18n.services.formatter.addCached('shout', () => (value) => String(value).toUpperCase())
const t = i18n.getFixedT('en')
let next = 0
globalThis.run = (n) => {
  for (let i = 0; i < n; i++, next++) t('price', { val: next + 0.5 }), t('mine', { val: 'v' + next })
}
globalThis.result = [t('price', { val: 1234.5 }), t('mine', { val: 'ab' })].join('|')
`

// The formatters i18next keeps for the calls of an app stay a bounded number, however many values
// the app formats, and they format as before. i18next 23.12.7 to 25.1 write the function that
// keeps them with longer names, which the second case makes of the installed one.
func TestSSRBoundsTheFormattersI18nextKeeps(t *testing.T) {
	names := strings.NewReplacer("(v, l, o)", "(val, lng, options)", "= o;", "= options;", "o &&", "options &&",
		"o.", "options.", "o[", "options[", "l +", "lng +", "(l)", "(lng)", ", o)", ", options)", "frm", "formatter", "(v)", "(val)")
	for _, c := range []struct {
		name string
		root func() string
	}{
		{"i18next 26.5", func() string { return copyTestdata(t, "ssr", true) }},
		{"longer names", func() string {
			return withChangedPackage(t, "i18next", filepath.Join("dist", "esm", "i18next.js"), func(source string) string {
				start := strings.Index(source, "const createCachedFormatter = fn => {")
				end := start + strings.Index(source[start:], "\n};\n")
				return source[:start] + names.Replace(source[start:end]) + source[end:]
			})
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			root := c.root()
			writeTree(t, root, map[string]string{"i18n.tsx": i18nextFormatterPage})
			code, printed := buildPrinting(t, root, "i18n.tsx")
			if strings.Contains(printed, "createCachedFormatter") {
				t.Fatalf("the build prints:\n%s", printed)
			}
			bytecode, err := ssr.Compile(code, 128<<20, 30*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer bytecode.Close()
			vm, err := ssr.Load(bytecode, 128<<20, 0, 30*time.Second, false)
			if err != nil {
				t.Fatal(err)
			}
			defer vm.Close()
			if _, err := vm.ExecutePendingJobs(); err != nil {
				t.Fatal(err)
			}
			if got, err := vm.Eval("globalThis.result", quickjs.EvalGlobal); err != nil || got != "Costs 1,234.50|Mine AB" {
				t.Fatalf("result = %v, %v", got, err)
			}
			run := func(n int) int64 {
				if _, err := vm.Eval("run("+strconv.Itoa(n)+")", quickjs.EvalGlobal); err != nil {
					t.Fatal(err)
				}
				return ssr.VMMemory(vm.VM, true).MemoryUsedSize
			}
			// The first run fills the caches that have a bound anyway, the plugin's cache of strings among them.
			run(20000)
			before := run(5000)
			after := run(20000)
			// Each format keeps up to 1,024 formatters, so the two may differ by up to about 1.5 MB, against
			// about 28 MB for 20,000 values without the bound.
			t.Logf("20,000 more values: %+d KB", (after-before)>>10)
			if grown := after - before; grown > 4<<20 {
				t.Errorf("20,000 more values took %d KB more in the VM", grown>>10)
			}
		})
	}
}
