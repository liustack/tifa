package bundle

import (
	"strings"
	"testing"
)

// The reads of import.meta.env.VITE_* in the code of the app get one warning for the build, at the
// place of the first read, with every name. The names Vite always has, and the reads of a
// dependency, are left out.
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
		`.tifa/server.ts:2:18: import.meta.env.VITE_KEY, import.meta.env.VITE_API, import.meta.env.VITE_FLAG are fixed when the build runs`,
		"known only when the program runs comes from the Load of the page",
		"docs/guides/environment.md",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the warnings lack %q:\n%s", want, out)
		}
	}
	if n := strings.Count(out, "fixed when the build runs"); n != 1 {
		t.Errorf("%d warnings, want 1 (one for the build):\n%s", n, out)
	}
	for _, not := range []string{"VITE_DEP", "import.meta.env.DEV", "import.meta.env.PROD", "import.meta.env.MODE", "import.meta.env.SSR", "BASE_URL"} {
		if strings.Contains(out, not) {
			t.Errorf("the warnings name %q:\n%s", not, out)
		}
	}
}
