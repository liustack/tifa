package bundle

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
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
