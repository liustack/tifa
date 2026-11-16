package bundle

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/liustack/tifa/js"
)

func TestNodeReadsExports(t *testing.T) {
	root := t.TempDir()
	if err := symlinkModules(t, root); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"route.tsx": `import x from './y'
export default function Page() { return <p>Don't {x}</p> }
export const head = () => ({})
export function Error() {}
export type T = number
export interface I {}
export declare const declared: number
const NotFound = 1, z = 2
export { NotFound, z as Zed }
export * from './other'
export * as ns from './ns'
export const { a, b: [c] } = { a: 1, b: [2] }
`,
		"layout.ts": "export const htmlAttributes = { lang: 'en' }\n",
	}
	writeTree(t, root, files)
	n := NewNode(root, io.Discard)
	defer n.Close()
	route, layout := filepath.Join(root, "route.tsx"), filepath.Join(root, "layout.ts")
	got, err := n.Exports([]string{route, layout})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"Error", "NotFound", "Zed", "a", "c", "default", "head", "ns"}; !slices.Equal(got[route], want) {
		t.Errorf("route.tsx exports %q, want %q", got[route], want)
	}
	if want := []string{"htmlAttributes"}; !slices.Equal(got[layout], want) {
		t.Errorf("layout.ts exports %q, want %q", got[layout], want)
	}
}

func TestNodeReportsSyntaxErrors(t *testing.T) {
	root := t.TempDir()
	if err := symlinkModules(t, root); err != nil {
		t.Fatal(err)
	}
	writeTree(t, root, map[string]string{"route.tsx": "export default function () { return <div> }\n"})
	n := NewNode(root, io.Discard)
	defer n.Close()
	_, err := n.Exports([]string{filepath.Join(root, "route.tsx")})
	if err == nil || !strings.Contains(err.Error(), "route.tsx:1:") {
		t.Fatalf("err = %v, want the place of the syntax error", err)
	}
	// The driver keeps running after a failed request.
	writeTree(t, root, map[string]string{"ok.ts": "export const ok = 1\n"})
	if got, err := n.Exports([]string{filepath.Join(root, "ok.ts")}); err != nil || len(got) != 1 {
		t.Fatalf("after an error: %v, %v", got, err)
	}
}

func TestNodeTellsHowToInstallVite(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not installed")
	}
	n := NewNode(t.TempDir(), io.Discard)
	defer n.Close()
	_, err := n.Exports(nil)
	if err == nil || !strings.Contains(err.Error(), "pnpm add -D vite @vitejs/plugin-react") {
		t.Fatalf("err = %v, want the install command", err)
	}
	// The error stays, without starting Node again.
	if _, again := n.Exports(nil); again == nil || again.Error() != err.Error() {
		t.Fatalf("second call: %v", again)
	}
}

func TestNodeNeedsNode(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	n := NewNode(t.TempDir(), io.Discard)
	defer n.Close()
	_, err := n.Exports(nil)
	if err == nil || !strings.Contains(err.Error(), "node is not on PATH") {
		t.Fatalf("err = %v, want node missing", err)
	}
}

func symlinkModules(t *testing.T, root string) error {
	t.Helper()
	return os.Symlink(testModules(t), filepath.Join(root, "node_modules"))
}

func TestNodeFindsTheLangKeysOfHTMLAttributes(t *testing.T) {
	root := t.TempDir()
	if err := symlinkModules(t, root); err != nil {
		t.Fatal(err)
	}
	writeTree(t, root, map[string]string{
		// The exported object, with a type assertion around it and a quoted key.
		"layout.tsx": "export const htmlAttributes = {\n  dir: 'ltr',\n  'lang': 'en',\n} as const\nexport default function L() { return null }\n",
		// A head that returns htmlAttributes, in an arrow function and in a function.
		"page.tsx": "export const head = ({ loaderData }) => ({\n  meta: [],\n  htmlAttributes: { Lang: loaderData.lang },\n})\n" +
			"export function other() { return { htmlAttributes: { dir: 'rtl' } } }\nexport default function P() { return null }\n",
		// Nothing a static read can see: a name, a spread, a computed key, a local of the same name.
		"clean.tsx": "const shared = { lang: 'en' }\nexport const htmlAttributes = shared\nexport const head = () => ({ htmlAttributes: { ...shared, ['lang']: 'x' } })\n" +
			"function f() { const htmlAttributes = { lang: 'x' }; return htmlAttributes }\nexport default function C() { return null }\n" +
			// An object that only has a property of that name, outside the export and outside head.
			"const example = { htmlAttributes: { lang: 'fr' } }\nexport function other() { return { htmlAttributes: { lang: 'x' } } }\n",
	})
	n := NewNode(root, io.Discard)
	defer n.Close()
	layout, page, clean := filepath.Join(root, "layout.tsx"), filepath.Join(root, "page.tsx"), filepath.Join(root, "clean.tsx")
	got, err := n.LangKeys([]string{layout, page, clean})
	if err != nil {
		t.Fatal(err)
	}
	if want := []LangKey{{Line: 3, Column: 3}}; !slices.Equal(got[layout], want) {
		t.Errorf("layout.tsx: %v, want %v", got[layout], want)
	}
	if want := []LangKey{{Line: 3, Column: 21}}; !slices.Equal(got[page], want) {
		t.Errorf("page.tsx: %v, want %v", got[page], want)
	}
	if len(got[clean]) != 0 {
		t.Errorf("clean.tsx: %v, want none", got[clean])
	}
}

// The driver reaches node as a file. As an argument of node -e it is refused once it is longer
// than the system takes: 32,767 characters on Windows, 128 KiB for one argument on Linux.
func TestNodeTakesTheDriverAsAFile(t *testing.T) {
	root := t.TempDir()
	n := NewNode(root, io.Discard)
	cmd, remove, err := n.driverCommand("node")
	if err != nil {
		t.Fatal(err)
	}
	defer remove()
	var script string
	for _, a := range cmd.Args[1:] {
		if len(a) > 1000 {
			t.Fatalf("an argument of node is %d bytes long, the driver must not go there", len(a))
		}
		if strings.HasSuffix(a, ".mjs") {
			script = a
		}
	}
	got, err := os.ReadFile(script)
	if err != nil || string(got) != js.ViteDriver {
		t.Fatalf("the script %q does not hold the driver: %v", script, err)
	}
	remove()
	if _, err := os.Stat(script); !os.IsNotExist(err) {
		t.Errorf("the script is still there after remove: %v", err)
	}
}

// The file of the driver is gone once the driver runs, so .tifa/ holds nothing of it.
func TestNodeLeavesNoDriverFile(t *testing.T) {
	root := t.TempDir()
	if err := symlinkModules(t, root); err != nil {
		t.Fatal(err)
	}
	writeTree(t, root, map[string]string{"route.tsx": "export default function Page() { return <p/> }\n"})
	n := NewNode(root, io.Discard)
	defer n.Close()
	if _, err := n.Exports([]string{filepath.Join(root, "route.tsx")}); err != nil {
		t.Fatal(err)
	}
	if left, _ := filepath.Glob(filepath.Join(root, ".tifa", "driver-*")); len(left) != 0 {
		t.Errorf("left behind: %v", left)
	}
}

// Vite 8 needs Node 20.19, 22.12 or later. On an older Node the driver says so, with the version
// it found, instead of leaving the user with an error from inside Vite.
func TestNodeTooOldForVite(t *testing.T) {
	root := t.TempDir()
	for _, version := range []string{"18.20.4", "20.18.3", "21.7.3", "22.11.0"} {
		// A module that Node loads first lets the driver see another version of itself.
		fake := filepath.Join(t.TempDir(), "version.cjs")
		writeTree(t, filepath.Dir(fake), map[string]string{
			"version.cjs": "Object.defineProperty(process.versions, 'node', { value: '" + version + "' })\n",
		})
		t.Setenv("NODE_OPTIONS", "--require "+fake)
		n := NewNode(root, io.Discard)
		_, err := n.Exports([]string{filepath.Join(root, "route.tsx")})
		n.Close()
		want := "tifa needs Node 20.19, 22.12 or later to run Vite, found Node " + version
		if err == nil || err.Error() != want {
			t.Errorf("Node %s: %v, want %q", version, err, want)
		}
	}
}
