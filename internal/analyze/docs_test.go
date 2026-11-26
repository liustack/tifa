package analyze

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode"
)

// docLink finds a pointer to a doc section in a string: docs/<folder>/<file>.md#<id>.
var docLink = regexp.MustCompile(`docs/[A-Za-z0-9_./-]+\.md#[A-Za-z0-9_-]+`)

var explicitID = regexp.MustCompile(`\s\{#([A-Za-z0-9_-]+)\}\s*$`)

// headingIDs returns the ids of the headings of a doc, as GitHub makes them: the text in lower
// case, without punctuation except - and _, with - for each space, and -1, -2 for a heading
// that repeats. An explicit {#id} at the end of a heading is its id.
func headingIDs(t *testing.T, file string) map[string]bool {
	t.Helper()
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	seen := map[string]int{}
	inCode := false
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "```") {
			inCode = !inCode
		}
		if inCode || !strings.HasPrefix(line, "#") {
			continue
		}
		if m := explicitID.FindStringSubmatch(line); m != nil {
			ids[m[1]] = true
			continue
		}
		var b strings.Builder
		for _, r := range strings.ToLower(strings.TrimSpace(strings.TrimLeft(line, "#"))) {
			switch {
			case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_':
				b.WriteRune(r)
			case r == ' ':
				b.WriteByte('-')
			}
		}
		id := b.String()
		if n := seen[id]; n > 0 {
			ids[id+"-"+strconv.Itoa(n)] = true
		} else {
			ids[id] = true
		}
		seen[id]++
	}
	return ids
}

// Every doc section a build error points to exists. The check reads each string in the Go code of
// this package and of internal/foreign, so a link written in a message and not in a constant is
// checked too.
func TestEveryDocLinkPointsToASectionThatExists(t *testing.T) {
	root := filepath.Join("..", "..")
	found := map[string][]string{} // link to the places that write it
	for _, dir := range []string{".", filepath.Join("..", "foreign")} {
		files, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range files {
			if strings.HasSuffix(file, "_test.go") {
				continue
			}
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, file, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			ast.Inspect(f, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				s, err := strconv.Unquote(lit.Value)
				if err != nil {
					t.Fatal(err)
				}
				for _, link := range docLink.FindAllString(s, -1) {
					found[link] = append(found[link], fset.Position(lit.Pos()).String())
				}
				return true
			})
		}
	}
	if len(found) < 20 {
		t.Fatalf("found %d doc links in the code, want many more: does the scan still read the strings?", len(found))
	}
	ids := map[string]map[string]bool{}
	for link, places := range found {
		file, id, _ := strings.Cut(link, "#")
		if ids[file] == nil {
			if _, err := os.Stat(filepath.Join(root, file)); err != nil {
				t.Errorf("%s: %s is not a doc", places[0], file)
				ids[file] = map[string]bool{}
				continue
			}
			ids[file] = headingIDs(t, filepath.Join(root, file))
		}
		if !ids[file][id] {
			t.Errorf("%s: %s has no section %q", places[0], file, id)
		}
	}
}
