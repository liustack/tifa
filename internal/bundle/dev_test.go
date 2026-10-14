package bundle

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDevRebuildsDist(t *testing.T) {
	root := fakeApp(t)
	d, err := NewDev(root, "")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	public := filepath.Join(root, "public")
	dist := filepath.Join(root, ".tifa", "dist")
	id, err := d.Build(public, true)
	if err != nil {
		t.Fatal(err)
	}
	first := readBuild(t, root)
	if first["buildId"] != id || !strings.HasPrefix(first["script"], "/assets/app-") || first["style"] == "" {
		t.Fatalf("build.json %v, id %s", first, id)
	}
	ssr, err := os.ReadFile(filepath.Join(dist, "ssr.js"))
	if err != nil {
		t.Fatal(err)
	}
	if want := `globalThis.__tifaAssets={"script":"` + first["script"] + `"`; !strings.HasPrefix(string(ssr), want) {
		t.Fatalf("ssr.js starts %.80q, want %q", ssr, want)
	}
	if again, err := d.Build(public, true); err != nil || again != id {
		t.Fatalf("unchanged rebuild: %s %v, want %s", again, err, id)
	}

	writeTree(t, root, map[string]string{"app/page/page.css": ".page { color: blue }\n"})
	id2, err := d.Build(public, true)
	if err != nil {
		t.Fatal(err)
	}
	second := readBuild(t, root)
	if id2 == id || second["style"] == first["style"] {
		t.Fatalf("edit did not reach the bundle: %v then %v", first, second)
	}
	if _, err := os.Stat(filepath.Join(dist, filepath.FromSlash(first["style"]))); !os.IsNotExist(err) {
		t.Errorf("old style %s still in dist: %v", first["style"], err)
	}
	if _, err := os.Stat(filepath.Join(dist, "public", "robots.txt")); err != nil {
		t.Error(err)
	}

	// An application that lost its pages keeps only the public files.
	if _, err := d.Build(public, false); err != nil {
		t.Fatal(err)
	}
	for _, gone := range []string{"assets", "ssr.js"} {
		if _, err := os.Stat(filepath.Join(dist, gone)); !os.IsNotExist(err) {
			t.Errorf("%s exists without pages: %v", gone, err)
		}
	}
}

func TestDevReportsErrorsAndRecovers(t *testing.T) {
	root := fakeApp(t)
	d, err := NewDev(root, "")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	public := filepath.Join(root, "public")
	writeTree(t, root, map[string]string{"app/page/route.tsx": "export default {\n"})
	if _, err := d.Build(public, true); err == nil || !strings.Contains(err.Error(), "route.tsx") {
		t.Fatalf("got %v, want the syntax error in route.tsx", err)
	}
	writeTree(t, root, map[string]string{"app/page/route.tsx": "import { route } from './+route'\nexport default route\n"})
	if _, err := d.Build(public, true); err != nil {
		t.Fatal(err)
	}
}
