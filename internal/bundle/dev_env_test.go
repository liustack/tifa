package bundle

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// Under tifa dev the bundle the server renders with gets the same import.meta.env as the browser
// modules: MODE, DEV, PROD and BASE_URL, which is "/" as in a build. Otherwise a page that reads them hydrates with other
// text. React still gets the production build.
func TestDevSSRBundleHasTheEnvOfTheBrowser(t *testing.T) {
	root := devApp(t)
	writeTree(t, root, map[string]string{
		"app/page/view.tsx": "export default function View() {\n" +
			"  return <p>{'mode=' + import.meta.env.MODE + ' dev=' + import.meta.env.DEV + ' prod=' + import.meta.env.PROD + ' ssr=' + import.meta.env.SSR + ' base=' + import.meta.env.BASE_URL}</p>\n}\n",
	})
	d, err := NewDev(root, freePort(t), io.Discard, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := d.Build(context.Background(), filepath.Join(root, "public"), true); err != nil {
		t.Fatal(err)
	}
	ssr, err := os.ReadFile(filepath.Join(root, ".tifa", "dist", "ssr.js"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`mode=\W*development`, `dev=\W*true`, `prod=\W*false`, `ssr=\W*true`, "base=/[\"']"} {
		if !regexp.MustCompile(want).Match(ssr) {
			t.Errorf("the SSR bundle lacks %q", want)
		}
	}
}
