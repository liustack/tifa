package bundle

import (
	"strings"
	"testing"
)

// A read of import.meta.env.VITE_* in the code of the app gets a warning once per name, at the
// place of the first read. The names Vite always has, and the reads of a dependency, get none.
func TestBuildWarnsOfViteEnvReads(t *testing.T) {
	server := `import '../vendor/node_modules/dep/index.js'
export const a = import.meta.env.VITE_KEY
export const b = import.meta.env.VITE_KEY
export const c = import.meta.env['VITE_API']
const { VITE_FLAG, VITE_KEY: k, DEV: d } = import.meta.env
export const e = [VITE_FLAG, k, d]
export const f = [import.meta.env.DEV, import.meta.env.PROD, import.meta.env.MODE, import.meta.env.SSR, import.meta.env.BASE_URL]
`
	out := buildWarnings(t, map[string]string{
		".tifa/server.ts":                      server,
		"vendor/node_modules/dep/package.json": `{ "name": "dep", "type": "module" }`,
		"vendor/node_modules/dep/index.js":     "export const x = import.meta.env.VITE_DEP\n",
	}, "{}")
	for _, want := range []string{
		`.tifa/server.ts:2:18: import.meta.env.VITE_KEY is fixed when the build runs`,
		`.tifa/server.ts:4:18: import.meta.env.VITE_API is fixed when the build runs`,
		`.tifa/server.ts:5:9: import.meta.env.VITE_FLAG is fixed when the build runs`,
		"known only when the program runs comes from the Load of the page",
		"docs/guides/environment.md",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the warnings lack %q:\n%s", want, out)
		}
	}
	if n := strings.Count(out, "is fixed when the build runs"); n != 3 {
		t.Errorf("%d warnings, want 3 (one per name):\n%s", n, out)
	}
	for _, not := range []string{"VITE_DEP", "import.meta.env.DEV", "import.meta.env.PROD", "import.meta.env.MODE", "import.meta.env.SSR", "BASE_URL"} {
		if strings.Contains(out, not) {
			t.Errorf("the warnings name %q:\n%s", not, out)
		}
	}
}
