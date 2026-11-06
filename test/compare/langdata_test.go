package compare_test

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"

	"golang.org/x/text/language"
)

// The storefront chooses its language from Accept-Language with golang.org/x/text/language. The
// port in domain/store/store.ts cannot call it, so it reads tables made from it here: which
// language, script and region subtags the package knows (a tag with any other is refused) and
// what its matcher answers, for the two languages of the storefront, to a single tag. The test
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
	// The languages the matcher answers for, with the answer for every script and region.
	var rows []string
	for _, l := range langs {
		if i, _ := storeMatch(language.MustParse(l)); i < 0 {
			// A language can match only with a script or region, rarely. Check a few.
			found := false
			for _, s := range []string{"Hans", "Hant", "Latn"} {
				if t, err := language.Parse(l + "-" + s); err == nil {
					if i, _ := storeMatch(t); i >= 0 {
						found = true
					}
				}
			}
			if !found {
				continue
			}
		}
		rows = append(rows, l)
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
	for _, kv := range []struct {
		name string
		list []string
	}{{"languages", langs}, {"scripts", scripts}, {"regions", regions}, {"variants", variants}} {
		fmt.Fprintf(&b, "export const %s = new Set('%s'.split(' '))\n\n", kv.name, strings.Join(kv.list, " "))
	}
	// A class is the answer of a language and script for no region and the regions that differ ("-"
	// for no match): "<answer>" or "<answer> R=<answer> R=<answer>". Languages and scripts that share
	// a class share its number.
	conf := map[language.Confidence]string{language.Exact: "E", language.High: "H", language.Low: "L"}
	answer := func(tag string) string {
		t, err := language.Parse(tag)
		if err != nil {
			return "-"
		}
		if i, c := storeMatch(t); i >= 0 {
			// P: a later tag of another language cannot take the answer from this tag, which the matcher
			// does for a tag that is an exact match but for its region. Found by asking with such a tag.
			pin := ""
			if c != language.Exact {
				other := language.MustParse([]string{"en-US", "zh-CN"}[i])
				if _, j, _ := storeMatcher.Match(t, other); j == i {
					pin = "P"
				}
			}
			return fmt.Sprintf("%d%s%s", i, conf[c], pin)
		}
		return "-"
	}
	classes := map[string]int{}
	var classList, entries []string
	for _, l := range rows {
		for _, s := range append([]string{""}, scripts...) {
			tag := l
			if s != "" {
				tag += "-" + s
			}
			base := answer(tag)
			class := base
			any := base != "-"
			for _, r := range regions {
				if a := answer(tag + "-" + r); a != base {
					class += " " + r + "=" + a
					any = true
				}
			}
			if !any {
				continue
			}
			id, ok := classes[class]
			if !ok {
				id = len(classList)
				classes[class] = id
				classList = append(classList, class)
			}
			entries = append(entries, fmt.Sprintf("%s %s=%d", l, s, id))
		}
	}
	b.WriteString("// matching: the answer of the matcher for a language and script (and region). A key is\n")
	b.WriteString("// 'lang script', its class says what the matcher answers for it: the index of the locale (0\n")
	b.WriteString("// zh-CN, 1 en-US) and the confidence (E exact, H high, L low) for no region, then the regions that\n")
	b.WriteString("// answer otherwise, '-' for no match.\n")
	fmt.Fprintf(&b, "export const matchClasses = %q.split('|')\n\n", strings.Join(classList, "|"))
	fmt.Fprintf(&b, "export const matching = new Map<string, number>(%q.split('|').map((e) => [e.slice(0, e.lastIndexOf('=')), Number(e.slice(e.lastIndexOf('=') + 1))]))\n", strings.Join(entries, "|"))
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
