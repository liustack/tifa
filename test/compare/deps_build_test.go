package compare

import (
	"context"
	"errors"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// A file whose query table is not the one of the build in the checkout is refused, with the hashes of its
// sources and URLs right: a run would count the hits of other pages.
func TestAFileWhoseQueryTableIsNotTheBuildsIsRefused(t *testing.T) {
	e, err := NewEnv()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(shopTable(e.Shop)); err != nil {
		t.Skip("the shop is not built")
	}
	d, err := parseStoreDeps(storeDepsJSON)
	if err != nil {
		t.Fatal(err)
	}
	old := storeDepsJSON
	defer func() { storeDepsJSON = old }()
	for what, change := range map[string]func(q map[string][]string){
		"a parameter more":  func(q map[string][]string) { q[storeList] = append(slices.Clone(q[storeList]), "zzz") },
		"a parameter fewer": func(q map[string][]string) { q[storeList] = q[storeList][1:] },
		"a route fewer":     func(q map[string][]string) { delete(q, storeHome) },
	} {
		c := *d
		c.Query = maps.Clone(d.Query)
		change(c.Query)
		if storeDepsJSON, err = marshalStoreDeps(&c); err != nil {
			t.Fatal(err)
		}
		if _, err := CheckStoreDeps(e.Root); err == nil || !strings.Contains(err.Error(), "query parameters") || !strings.Contains(err.Error(), "compare deps") {
			t.Errorf("a file with %s in its query table: %v, want a refusal that names the table and says how to make the file again", what, err)
		}
	}
}

// A checkout whose shop is not built cannot check the table, and says so.
func TestAFileIsNotCheckedAgainstAShopThatIsNotBuilt(t *testing.T) {
	d, err := parseStoreDeps(storeDepsJSON)
	if err != nil {
		t.Fatal(err)
	}
	if err := checkQueryTable(t.TempDir(), d); !errors.Is(err, errShopNotBuilt) {
		t.Errorf("the table of a shop with no build: %v, want errShopNotBuilt", err)
	}
}

// The file says which build of the shop it was made from.
func TestTheFileSaysWhichBuildItWasMadeFrom(t *testing.T) {
	d, err := storeDepsOnce()
	if err != nil {
		t.Fatal(err)
	}
	if d.Build.ID == "" || d.Build.Table == "" {
		t.Errorf("the build of the file is %+v, want the ID of the front end and the fingerprint of the table of routes", d.Build)
	}
	data, err := marshalStoreDeps(d)
	if err != nil {
		t.Fatal(err)
	}
	again, err := parseStoreDeps(data)
	if err != nil || again.Build != d.Build {
		t.Errorf("the build written and read again is %+v (%v), want %+v", again.Build, err, d.Build)
	}
}

func TestTheFingerprintOfAGeneratedProgram(t *testing.T) {
	got, err := tableFingerprint([]byte("var App = server.App{\n\tRoutes: []server.Route{},\n\tFingerprint: \"c18b38708e4df22e3925777ca3ed24b2\",\n}\n"))
	if err != nil || got != "c18b38708e4df22e3925777ca3ed24b2" {
		t.Errorf("fingerprint %q, %v", got, err)
	}
	if _, err := tableFingerprint([]byte(appSource)); err == nil {
		t.Error("a program with no Fingerprint has one")
	}
}

// A program is older than its sources when one of them was written after it.
func TestAFileWrittenAfterTheProgramIsNewerThanIt(t *testing.T) {
	dir := t.TempDir()
	t0 := time.Now().Add(-3 * time.Hour)
	write := func(name string, at time.Time) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, at, at); err != nil {
			t.Fatal(err)
		}
		return path
	}
	prog := write("shop", t0.Add(time.Hour))
	before, same, after := write("a.go", t0), write("b.go", t0.Add(time.Hour)), write("c.go", t0.Add(2*time.Hour))
	if got, err := newerThan(prog, []string{before, same}); got != "" || err != nil {
		t.Errorf("files written before the program and with it: %q is newer, %v", got, err)
	}
	if got, err := newerThan(prog, []string{before, after, same}); got != after || err != nil {
		t.Errorf("a file written after the program: %q is newer (%v), want %s", got, err, after)
	}
	if _, err := newerThan(filepath.Join(dir, "none"), []string{before}); err == nil {
		t.Error("no program, and no error")
	}
	if _, err := newerThan(prog, []string{filepath.Join(dir, "gone.go")}); err == nil {
		t.Error("a source that is gone, and no error")
	}
}

// `compare deps` asks the program that is built, so it refuses one that is older than the sources of the
// checkout, as a skipped build leaves it: the file would say what another shop shows. It takes the
// build the checkout makes.
func TestCompareDepsRefusesABuildOlderThanTheCheckout(t *testing.T) {
	env := depsEnv(t)
	ctx := context.Background()
	if err := env.shopBuildCurrent(ctx); err != nil {
		t.Fatalf("the shop as it was just built: %v", err)
	}
	prog := filepath.Join(env.Shop, "bin", "shop")
	info, err := os.Stat(prog)
	if err != nil {
		t.Fatal(err)
	}
	// The program as a build before the last change of the sources left it.
	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(prog, old, old); err != nil {
		t.Fatal(err)
	}
	defer os.Chtimes(prog, info.ModTime(), info.ModTime())
	out := filepath.Join(t.TempDir(), "store_deps.json")
	err = env.WriteStoreDeps(ctx, io.Discard, out)
	if err == nil || !strings.Contains(err.Error(), "bin/shop was built before") || !strings.Contains(err.Error(), "build the shop again") {
		t.Errorf("deps with a program older than its sources: %v, want a refusal that says to build the shop again", err)
	}
	if _, statErr := os.Stat(out); statErr == nil {
		t.Error("deps wrote a file from a program it refused")
	}
}

// checkedDeps is the file, checked against this checkout, and a skip for a shop that is not built.
func checkedDeps(t *testing.T) *StoreDeps {
	t.Helper()
	e, err := NewEnv()
	if err != nil {
		t.Fatal(err)
	}
	d, err := CheckStoreDeps(e.Root)
	if errors.Is(err, errShopNotBuilt) {
		t.Skip(err.Error())
	}
	if err != nil {
		t.Fatal(err)
	}
	return d
}
