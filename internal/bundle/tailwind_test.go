package bundle

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestUsesTailwind(t *testing.T) {
	cases := []struct {
		css  string
		want bool
	}{
		{`@import "tailwindcss";`, true},
		{`@import 'tailwindcss' source("../src");`, true},
		{`@import url("tailwindcss");`, true},
		{`@import "tailwindcss/utilities.css" layer(utilities);`, true},
		{`@reference "../site.css"; .btn { @apply px-4; }`, true},
		{`@theme { --color-brand: red; }`, true},
		{`@custom-variant dark (&:is(.dark *));`, true},
		{`@tailwind utilities;`, true},
		{`.page { color: red }`, false},
		{`@import "./theme.css"; @layer base { body { margin: 0 } }`, false},
		{`@import "tailwindcss-animate-like-package";`, false},
		{`@media (width >= 40rem) { .a { color: red } }`, false},
	}
	for _, tc := range cases {
		if got := usesTailwind([]byte(tc.css)); got != tc.want {
			t.Errorf("usesTailwind(%q) = %v, want %v", tc.css, got, tc.want)
		}
	}
}

// tailwindApp is fakeApp with a Tailwind stylesheet and the test npm packages linked in. It skips
// when the packages or node are not installed.
func tailwindApp(t *testing.T) string {
	t.Helper()
	modules := testModules(t)
	if _, err := os.Stat(filepath.Join(modules, "@tailwindcss", "cli", "package.json")); err != nil {
		t.Skipf("needs Tailwind: run pnpm install --frozen-lockfile in internal/bundle/testdata (%v)", err)
	}
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("Tailwind needs node, which is not installed")
	}
	root := fakeApp(t)
	if err := os.Symlink(modules, filepath.Join(root, "node_modules")); err != nil {
		t.Fatal(err)
	}
	writeTree(t, root, map[string]string{
		"app/page/page.css":  "@import \"tailwindcss\";\n@import \"./extra.css\";\n",
		"app/page/extra.css": ".brand { color: rebeccapurple }\n",
		"app/page/route.tsx": "import { route } from './+route'\nimport './page.css'\n" +
			"export const view = () => <p className=\"text-red-500 hover:underline\">{route}</p>\n",
		// tifa's own output and executables would keep old class names alive.
		"bin/app":                "\x00 bg-yellow-300 \x00",
		".tifa/dist/old/app.js":  `x="bg-green-700"`,
		".tifa/dist/old/app.css": ".bg-green-700{color:green}",
	})
	return root
}

func readStyle(t *testing.T, root string) string {
	t.Helper()
	style := readBuild(t, root)["style"]
	if style == "" {
		t.Fatal("build has no style")
	}
	data, err := os.ReadFile(filepath.Join(root, ".tifa", "dist", filepath.FromSlash(style)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestAppCompilesTailwind(t *testing.T) {
	root := tailwindApp(t)
	if _, err := App(root, filepath.Join(root, "public"), "", true); err != nil {
		t.Fatal(err)
	}
	css := readStyle(t, root)
	for _, want := range []string{".text-red-500", `.hover\:underline:hover`, ".brand", "--color-red-500"} {
		if !strings.Contains(css, want) {
			t.Errorf("style lacks %s:\n%.2000s", want, css)
		}
	}
	for _, unwanted := range []string{"@import", "@tailwind", "bg-yellow-300", "bg-green-700"} {
		if strings.Contains(css, unwanted) {
			t.Errorf("style contains %s", unwanted)
		}
	}
	ssr, err := os.ReadFile(filepath.Join(root, ".tifa", "dist", "ssr.js"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(ssr), "--color-red-500") {
		t.Error("the SSR bundle holds CSS")
	}
}

func TestTailwindReportsItsErrors(t *testing.T) {
	root := tailwindApp(t)
	writeTree(t, root, map[string]string{"app/page/extra.css": ".brand { @apply no-such-utility; }\n"})
	_, err := App(root, filepath.Join(root, "public"), "", true)
	if err == nil || !strings.Contains(err.Error(), "no-such-utility") || !strings.Contains(err.Error(), "page.css") {
		t.Fatalf("got %v, want Tailwind's error naming page.css", err)
	}
}

func TestTailwindNeedsNode(t *testing.T) {
	root := tailwindApp(t)
	t.Setenv("PATH", t.TempDir())
	_, err := App(root, filepath.Join(root, "public"), "", true)
	if err == nil || !strings.Contains(err.Error(), "app/page/page.css uses Tailwind CSS, whose CLI runs on Node, and node is not on PATH") {
		t.Fatalf("got %v, want the missing node", err)
	}
}

func TestTailwindNotInstalled(t *testing.T) {
	root := fakeApp(t)
	writeTree(t, root, map[string]string{"app/page/page.css": "@import \"tailwindcss\";\n"})
	_, err := App(root, filepath.Join(root, "public"), "", true)
	if err == nil || !strings.Contains(err.Error(), "page.css") ||
		!strings.Contains(err.Error(), "pnpm add -D tailwindcss @tailwindcss/cli") {
		t.Fatalf("got %v, want the file and what to install", err)
	}
}

func TestDevRecompilesTailwindAfterATSXEdit(t *testing.T) {
	root := tailwindApp(t)
	d, err := NewDev(root, "")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	public := filepath.Join(root, "public")
	if _, err := d.Build(public, true); err != nil {
		t.Fatal(err)
	}
	if d.TailwindTime() <= 0 {
		t.Errorf("Tailwind took %s", d.TailwindTime())
	}
	if css := readStyle(t, root); !strings.Contains(css, ".text-red-500") || strings.Contains(css, ".underline") {
		t.Fatalf("first style:\n%.2000s", css)
	}

	writeTree(t, root, map[string]string{"app/page/route.tsx": "import { route } from './+route'\nimport './page.css'\n" +
		"export const view = () => <p className=\"underline\">{route}</p>\n"})
	if _, err := d.Build(public, true); err != nil {
		t.Fatal(err)
	}
	css := readStyle(t, root)
	if !strings.Contains(css, ".underline") {
		t.Errorf("the new class is missing:\n%.2000s", css)
	}
	// The first build's output was still in .tifa/dist while Tailwind scanned.
	if strings.Contains(css, ".text-red-500") {
		t.Error("the removed class is still there")
	}
}

func TestDevWithoutTailwindTakesNoTailwindTime(t *testing.T) {
	root := fakeApp(t)
	d, err := NewDev(root, "")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := d.Build(filepath.Join(root, "public"), true); err != nil {
		t.Fatal(err)
	}
	if d.TailwindTime() != 0 {
		t.Errorf("Tailwind took %s", d.TailwindTime())
	}
}
