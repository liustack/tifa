package compare_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"golang.org/x/text/language"
)

// The storefront chooses its language from Accept-Language with golang.org/x/text/language. The
// port in domain/store/store.ts cannot call it, so it reads tables made from it here: which
// language, script and region subtags the package knows (a tag with any other is refused) and
// the tables of its matcher for the two languages of the storefront, which exportMatcher has the
// package itself write (a file added to a copy of it, see
// internal/langgen). The test
// fails when the file is out of date, and -update writes it again.

var updateLangData = flag.Bool("update-langdata", false, "write domain/store/langdata.gen.ts")

const langDataFile = "domain/store/langdata.gen.ts"

var storeMatcher = language.NewMatcher([]language.Tag{language.MustParse("zh-CN"), language.MustParse("en-US")})

// storeMatch is what the shop does for one tag: the index of the locale and the confidence, or -1.
func storeMatch(t language.Tag) (int, language.Confidence) {
	_, i, c := storeMatcher.Match(t)
	if c == language.No {
		return -1, c
	}
	return i, c
}

func letters(n int, from, to byte) []string {
	out := []string{""}
	for i := 0; i < n; i++ {
		var next []string
		for _, p := range out {
			for c := from; c <= to; c++ {
				next = append(next, p+string(c))
			}
		}
		out = next
	}
	return out
}

func buildLangData() string {
	var langs, scripts, regions []string
	for _, n := range []int{2, 3} {
		for _, s := range letters(n, 'a', 'z') {
			if len(s) == n {
				if _, err := language.ParseBase(s); err == nil {
					langs = append(langs, s)
				}
			}
		}
	}
	for _, s := range letters(4, 'a', 'z') {
		if len(s) == 4 {
			if _, err := language.ParseScript(s); err == nil {
				scripts = append(scripts, strings.ToUpper(s[:1])+s[1:])
			}
		}
	}
	for _, s := range letters(2, 'A', 'Z') {
		if len(s) == 2 {
			if _, err := language.ParseRegion(s); err == nil {
				regions = append(regions, s)
			}
		}
	}
	for i := 0; i < 1000; i++ {
		s := fmt.Sprintf("%03d", i)
		if _, err := language.ParseRegion(s); err == nil {
			regions = append(regions, s)
		}
	}
	// Three letter regions (ISO 3166 alpha-3) are the region of their two letter code.
	var alpha3 []string
	for _, s := range letters(3, 'A', 'Z') {
		if len(s) == 3 {
			if r, err := language.ParseRegion(s); err == nil {
				alpha3 = append(alpha3, s+":"+r.String())
			}
		}
	}
	// A language the package reads as another (chi as zh, iw as he) is that language.
	var canonical []string
	for _, l := range langs {
		if base, conf := language.MustParse(l).Base(); conf == language.Exact && base.String() != l {
			canonical = append(canonical, l+":"+base.String())
		}
	}
	var b strings.Builder
	b.WriteString("// Written by TestLanguageData in test/compare (go test -run TestLanguageData -update-langdata).\n")
	b.WriteString("// Do not edit. The subtags golang.org/x/text/language knows and how its matcher answers for the\n")
	b.WriteString("// languages of the storefront.\n\n")
	// The variants are the keys of variantIndex in the package's own tables, which has no way to
	// list them.
	dir, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "golang.org/x/text").Output()
	if err != nil {
		panic(err)
	}
	tables, err := os.ReadFile(strings.TrimSpace(string(dir)) + "/internal/language/tables.go")
	if err != nil {
		panic(err)
	}
	block := string(tables)
	block = block[strings.Index(block, "var variantIndex = map[string]uint8{"):]
	block = block[:strings.Index(block, "\n}\n")]
	var variants []string
	for _, m := range regexp.MustCompile(`"([a-z0-9]+)":`).FindAllStringSubmatch(block, -1) {
		variants = append(variants, m[1])
	}
	fmt.Fprintf(&b, "// regionAlpha3 maps a three letter region to the region it is.\nexport const regionAlpha3 = new Map<string, string>('%s'.split(' ').map((e) => e.split(':') as [string, string]))\n\n", strings.Join(alpha3, " "))
	fmt.Fprintf(&b, "// langCanonical maps a language the package reads as another to that one.\nexport const langCanonical = new Map<string, string>('%s'.split(' ').map((e) => e.split(':') as [string, string]))\n\n", strings.Join(canonical, " "))
	for _, kv := range []struct {
		name string
		list []string
	}{{"languages", langs}, {"scripts", scripts}, {"regions", regions}, {"variants", variants}} {
		fmt.Fprintf(&b, "export const %s = new Set('%s'.split(' '))\n\n", kv.name, strings.Join(kv.list, " "))
	}
	b.WriteString("\n")
	b.WriteString(exportMatcher(langs, scripts, regions))
	return b.String()
}

func TestLanguageData(t *testing.T) {
	want := buildLangData()
	if *updateLangData {
		if err := os.WriteFile(langDataFile, []byte(want), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	got, err := os.ReadFile(langDataFile)
	if err != nil || string(got) != want {
		t.Fatalf("%s is out of date: go test -run TestLanguageData -update-langdata ./test/compare", langDataFile)
	}
}

// exportMatcher runs the exporter in internal/langgen. The tables and the state of the matcher are
// not exported, so the exporter is a file added to a copy of the package (export.go.txt as
// language/export_gen.go in a copy of golang.org/x/text that a temporary go.mod replaces the module
// with), and prints them as TypeScript.
func exportMatcher(langs, scripts, regions []string) string {
	dirOut, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "golang.org/x/text").Output()
	if err != nil {
		panic(err)
	}
	modOut, err := exec.Command("go", "env", "GOMOD").Output()
	if err != nil {
		panic(err)
	}
	tmp, err := os.MkdirTemp("", "langgen")
	if err != nil {
		panic(err)
	}
	defer func() {
		filepath.WalkDir(tmp, func(p string, d os.DirEntry, err error) error { os.Chmod(p, 0o755); return nil })
		os.RemoveAll(tmp)
	}()
	copyOfText := filepath.Join(tmp, "text")
	if out, err := exec.Command("cp", "-R", strings.TrimSpace(string(dirOut)), copyOfText).CombinedOutput(); err != nil {
		panic(fmt.Sprintf("cp: %v: %s", err, out))
	}
	exporter, err := os.ReadFile("internal/langgen/export.go.txt")
	if err != nil {
		panic(err)
	}
	os.Chmod(filepath.Join(copyOfText, "language"), 0o755)
	if err := os.WriteFile(filepath.Join(copyOfText, "language", "export_gen.go"), exporter, 0o644); err != nil {
		panic(err)
	}
	goMod := strings.TrimSpace(string(modOut))
	mod, err := os.ReadFile(goMod)
	if err != nil {
		panic(err)
	}
	sum, _ := os.ReadFile(strings.TrimSuffix(goMod, ".mod") + ".sum")
	tmpMod := filepath.Join(tmp, "go.mod")
	os.WriteFile(tmpMod, append(mod, []byte("\nreplace golang.org/x/text => "+copyOfText+"\n")...), 0o644)
	os.WriteFile(filepath.Join(tmp, "go.sum"), sum, 0o644)
	in, _ := json.Marshal(map[string]any{"Languages": langs, "Scripts": scripts, "Regions": regions})
	cmd := exec.Command("go", "run", "-modfile="+tmpMod, "-tags", "langgen", "./internal/langgen")
	cmd.Stdin = bytes.NewReader(in)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		panic(fmt.Sprintf("the exporter: %v", err))
	}
	return string(out)
}
