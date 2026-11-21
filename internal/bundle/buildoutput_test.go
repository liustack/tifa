package bundle

import (
	"path/filepath"
	"strings"
	"testing"
)

// A build prints what an app can act on and not the notes of Vite and Rolldown that Tifa's own
// design makes useless: the time the plugins took (the slow part is Tifa's or a plugin's, not
// something to change in the app), and the 500 kB limit for the entry, which Tifa makes one file
// on purpose. An entry past a size that is a problem anywhere still gets Vite's warning.
func TestAppBuildPrintsNoPluginTimingsAndASaneChunkLimit(t *testing.T) {
	build := func(t *testing.T, size int, app string) string {
		root := devApp(t)
		writeTree(t, root, map[string]string{
			"vite.config.ts": "import react from '@vitejs/plugin-react'\nimport tifa from './.tifa/vite.js'\n" +
				"const slow = { name: 'slow', async generateBundle() { await new Promise((r) => setTimeout(r, 3000)) } }\n" +
				"export default { " + app + "plugins: [tifa(), react(), slow] }\n",
			"app/page/big.ts": "export default '" + strings.Repeat("x", size) + "'\n",
			".tifa/client.ts": "import View from '../app/page/view'\nimport big from '../app/page/big'\nconsole.log(View, big)\n",
		})
		var output strings.Builder
		n := NewNode(root, &output)
		defer n.Close()
		if _, err := App(n, root, filepath.Join(root, "public"), true, nil); err != nil {
			t.Fatalf("App: %v", err)
		}
		return output.String()
	}
	small := build(t, 600_000, "")
	if strings.Contains(small, "PLUGIN_TIMINGS") || strings.Contains(small, "larger than 500 kB") {
		t.Errorf("an entry of 600 kB prints:\n%s", small)
	}
	large := build(t, 1_100_000, "")
	if strings.Contains(large, "PLUGIN_TIMINGS") || !strings.Contains(large, "Some chunks are larger than") {
		t.Errorf("an entry of 1.1 MB prints:\n%s", large)
	}
}

// What the app sets for the limit and the checks in its own Vite config wins over Tifa's.
func TestAppBuildKeepsTheChunkLimitAndChecksOfTheApp(t *testing.T) {
	root := devApp(t)
	writeTree(t, root, map[string]string{
		// Whether Rolldown prints its timings depends on how long the build took next to the plugins,
		// so the test reads the config Vite resolved for each environment instead.
		"vite.config.ts": "import react from '@vitejs/plugin-react'\nimport tifa from './.tifa/vite.js'\n" +
			"const probe = { name: 'probe', configResolved(c) { for (const e of ['client', 'ssr']) { const b = c.environments[e].build; console.warn(`probe ` + e + ` limit=` + b.chunkSizeWarningLimit + ` timings=` + b.rolldownOptions.checks?.pluginTimings) } } }\n" +
			"export default { build: { chunkSizeWarningLimit: 100, rolldownOptions: { checks: { pluginTimings: true } } }, plugins: [tifa(), react(), probe] }\n",
		"app/page/big.ts": "export default '" + strings.Repeat("x", 200_000) + "'\n",
		".tifa/client.ts": "import View from '../app/page/view'\nimport big from '../app/page/big'\nconsole.log(View, big)\n",
	})
	var output strings.Builder
	n := NewNode(root, &output)
	defer n.Close()
	if _, err := App(n, root, filepath.Join(root, "public"), true, nil); err != nil {
		t.Fatalf("App: %v", err)
	}
	if !strings.Contains(output.String(), "probe client limit=100 timings=true") || !strings.Contains(output.String(), "probe ssr limit=100 timings=true") || !strings.Contains(output.String(), "Some chunks are larger than") {
		t.Errorf("the app's own settings do not apply:\n%s", output.String())
	}
}
