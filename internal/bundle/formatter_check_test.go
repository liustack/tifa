package bundle

import (
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liustack/tifa/js"
)

// buildFails builds page as the SSR bundle of an app and returns the error of the build.
func buildFails(t *testing.T, page string) error {
	t.Helper()
	root := copyTestdata(t, "ssr", true)
	writeTree(t, root, map[string]string{
		"package.json":         `{ "type": "module" }` + "\n",
		"vite.config.ts":       "import tifa from './.tifa/vite.js'\nexport default { plugins: [tifa()] }\n",
		".tifa/vite/plugin.js": js.VitePlugin,
		".tifa/vite.js":        "import { tifa } from './vite/plugin.js'\nexport default () => tifa({ target: ['es2022'], i18nextCache: true })\n",
		".tifa/client.ts":      "export const client = 1\n",
		".tifa/server.ts":      page,
	})
	n := NewNode(root, io.Discard)
	defer n.Close()
	_, err := App(n, root, filepath.Join(root, "public"), true, nil)
	return err
}

// A formatter that reads the time or randomness gives another text for the same call, which the
// server keeps for later requests and the browser writes differently, so the build stops it and
// says where it is.
func TestBuildRefusesAFormatterThatReadsTheClock(t *testing.T) {
	for _, c := range []struct{ name, body, read string }{
		{"Date.now", "(lng) => (value: number) => String(Date.now() - value)", "Date.now()"},
		{"new Date", "(lng) => (value: number) => new Date().toISOString() + value", "new Date()"},
		{"Date", "(value: string) => Date() + value", "Date()"},
		{"Math.random", "(value: string) => value + Math.random()", "Math.random()"},
		{"performance.now", "(lng, options) => { const at = performance.now(); return (value: string) => value + at }", "performance.now()"},
		{"crypto.randomUUID", "(value: string) => value + crypto.randomUUID()", "crypto.randomUUID()"},
	} {
		t.Run(c.name, func(t *testing.T) {
			add := "addCached"
			if !strings.HasPrefix(c.body, "(lng") {
				add = "add"
			}
			page := "import i18next from 'i18next'\n\nconst i18n = i18next.createInstance()\n" +
				"i18n.services.formatter?." + add + "('ago', " + c.body + ")\n"
			err := buildFails(t, page)
			if err == nil {
				t.Fatal("the build passed")
			}
			for _, want := range []string{".tifa/server.ts:4", `"ago"`, c.read} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("the error lacks %q:\n%v", want, err)
				}
			}
		})
	}
}

// The check never stops a formatter it cannot be sure about: a date made from the value, a Date
// of the app's own, and code outside formatters.
func TestBuildLetsPureFormattersThrough(t *testing.T) {
	page := `import i18next from 'i18next'

const i18n = i18next.createInstance()
i18n.services.formatter?.addCached('day', (lng) => (value: string) => new Date(value).toISOString())
const started = Date.now()
;(globalThis as any).result = started > 0 && Math.random() < 2
`
	if err := buildFails(t, page); err != nil {
		t.Fatalf("the build failed: %v", err)
	}
	shadowed := `import i18next from 'i18next'

const Date = { now: () => 1 }
const i18n = i18next.createInstance()
i18n.services.formatter?.add('fixed', (value: number) => String(Date.now() + value))
`
	if err := buildFails(t, shadowed); err != nil {
		t.Fatalf("a Date of the app's own failed the build: %v", err)
	}
}
