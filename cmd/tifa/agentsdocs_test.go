package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// The skill and the notes `tifa new` writes name commands, flags, Go names, imports from
// tifa/client, docs and files. TestAgentTextsNameWhatExists reads them as they are checked in
// and fails with the file, the line and what is missing, so they cannot drift from the code.
//
// What it checks, and what it leaves out because it cannot be checked without guessing:
//   - Commands: every code span or code block line that starts with go tool tifa, tifa, or
//     $(go tool -n tifa) names a command of the command tree, and every flag in that same span
//     or line belongs to that command. A flag in a span of its own, such as `--json` after a
//     command in a table cell, is not checked: nothing says which command it belongs to.
//   - Go names: every tifa.X and tifatest.X names an exported declaration of that package, and
//     every c.X names a method of *tifa.Ctx. Methods of other types and fields are not checked.
//   - tifa/client: the names of every `import { ... } from 'tifa/client'` line. A name written in
//     prose, as in "useSession() from tifa/client", is not checked.
//   - Docs: every docs/<file>.md, with or without #anchor, also inside a GitHub URL, is a file of
//     docs/ and the anchor is a heading of it. Links between the files of the skill resolve too.
//   - The skill: every file of references/ is listed in the table of SKILL.md and every file
//     the table names exists.
//
// Not checked: Go signatures and the arguments of a call, the names of settings in tifa.yaml,
// the table of TypeScript names that are not imported, and anything in prose.
func TestAgentTextsNameWhatExists(t *testing.T) {
	problems := checkAgentTexts(t, agentTextFiles(t))
	slices.Sort(problems)
	for _, p := range slices.Compact(problems) {
		t.Error(p)
	}
}

// agentTextFiles lists the files that are read: the skill and the template of the notes.
func agentTextFiles(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(repoRoot, "skills", "tifa", "references", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	files = append(files, filepath.Join(repoRoot, "skills", "tifa", "SKILL.md"), filepath.Join("newapp", "AGENTS.md.tmpl"))
	return files
}

var (
	spanRE      = regexp.MustCompile("`([^`]+)`")
	docRefRE    = regexp.MustCompile(`docs/([A-Za-z0-9_./-]+?\.md)(#[A-Za-z0-9_-]+)?`)
	relLinkRE   = regexp.MustCompile(`\]\(([A-Za-z0-9_-]+\.md)(#[A-Za-z0-9_-]+)?\)`)
	refFileRE   = regexp.MustCompile(`references/([A-Za-z0-9_-]+\.md)`)
	tifaNameRE  = regexp.MustCompile(`(?:^|[^\w.])(tifa|tifatest)\.([A-Z]\w*)`)
	ctxMethodRE = regexp.MustCompile(`(?:^|[^\w.])c\.([A-Z]\w*)`)
	importRE    = regexp.MustCompile(`import \{([^}]*)\} from 'tifa/client'`)
	envPrefixRE = regexp.MustCompile(`^[A-Z_]+=\S+$`)
)

func checkAgentTexts(t *testing.T, files []string) []string {
	t.Helper()
	root := newRootCmd()
	goNames := map[string]map[string]bool{
		"tifa":     exportedNames(t, filepath.Join(repoRoot), ""),
		"tifatest": exportedNames(t, filepath.Join(repoRoot, "tifatest"), ""),
	}
	ctxMethods := exportedNames(t, filepath.Join(repoRoot, "internal", "core"), "Ctx") // tifa.Ctx is an alias of core.Ctx
	clientNames := clientExports(t)
	var problems []string
	add := func(file string, line int, format string, args ...any) {
		problems = append(problems, fmt.Sprintf("%s:%d: %s", filepath.ToSlash(file), line, fmt.Sprintf(format, args...)))
	}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		inFence := false
		for i, line := range strings.Split(string(data), "\n") {
			n := i + 1
			if strings.HasPrefix(strings.TrimSpace(line), "```") {
				inFence = !inFence
				continue
			}
			// The pieces of text that stand for code: the spans of a prose line, or the line of a
			// code block.
			var code []string
			if inFence {
				code = []string{strings.TrimPrefix(strings.TrimSpace(line), "$ ")}
			} else {
				for _, m := range spanRE.FindAllStringSubmatch(line, -1) {
					code = append(code, m[1])
				}
			}
			for _, c := range code {
				for _, p := range checkCommand(root, c) {
					add(file, n, "%s", p)
				}
				for _, m := range tifaNameRE.FindAllStringSubmatch(c, -1) {
					if !goNames[m[1]][m[2]] {
						add(file, n, "%s.%s does not exist in package %s", m[1], m[2], m[1])
					}
				}
				for _, m := range ctxMethodRE.FindAllStringSubmatch(c, -1) {
					if !ctxMethods[m[1]] {
						add(file, n, "c.%s is not a method of *tifa.Ctx", m[1])
					}
				}
			}
			for _, m := range importRE.FindAllStringSubmatch(line, -1) {
				for name := range strings.SplitSeq(m[1], ",") {
					name = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(name), "type "))
					if before, _, ok := strings.Cut(name, " as "); ok {
						name = before
					}
					if name != "" && !clientNames[name] {
						add(file, n, "tifa/client does not export %s", name)
					}
				}
			}
			for _, m := range docRefRE.FindAllStringSubmatch(line, -1) {
				if p := checkDocRef(filepath.Join(repoRoot, "docs", filepath.FromSlash(m[1])), "docs/"+m[1], m[2]); p != "" {
					add(file, n, "%s", p)
				}
			}
			for _, m := range relLinkRE.FindAllStringSubmatch(line, -1) {
				if p := checkDocRef(filepath.Join(filepath.Dir(file), m[1]), m[1], m[2]); p != "" {
					add(file, n, "%s", p)
				}
			}
			if filepath.Base(file) == "SKILL.md" {
				for _, m := range refFileRE.FindAllStringSubmatch(line, -1) {
					if _, err := os.Stat(filepath.Join(filepath.Dir(file), "references", m[1])); err != nil {
						add(file, n, "references/%s does not exist", m[1])
					}
				}
			}
		}
		if filepath.Base(file) == "SKILL.md" {
			listed := string(data)
			entries, _ := filepath.Glob(filepath.Join(filepath.Dir(file), "references", "*.md"))
			for _, e := range entries {
				if !strings.Contains(listed, "references/"+filepath.Base(e)) {
					add(file, 1, "references/%s is not in the table of references", filepath.Base(e))
				}
			}
		}
	}
	return problems
}

// checkCommand checks one piece of code that starts with a tifa command. It returns the problems,
// and nothing for a piece that is not a tifa command.
func checkCommand(root *cobra.Command, code string) []string {
	words := strings.Fields(code)
	for len(words) > 0 && envPrefixRE.MatchString(words[0]) {
		words = words[1:]
	}
	switch {
	case len(words) >= 3 && words[0] == "go" && words[1] == "tool" && words[2] == "tifa":
		words = words[3:]
	case len(words) >= 1 && (words[0] == "tifa" || words[0] == "$(go" && len(words) >= 4 && words[3] == "tifa)"):
		if words[0] == "tifa" {
			words = words[1:]
		} else {
			words = words[4:]
		}
	case len(words) >= 3 && words[0] == "go" && words[1] == "run" && strings.HasPrefix(words[2], tifaModule+"/cmd/tifa@"):
		words = words[3:]
	default:
		return nil
	}
	// The command ends where the shell takes over.
	if i := slices.IndexFunc(words, func(w string) bool { return strings.ContainsAny(w, "|&;><") }); i >= 0 {
		words = words[:i]
	}
	// Brackets and angle brackets in a usage line: [--module path] and <dir>.
	for i, w := range words {
		words[i] = strings.Trim(w, "[]")
	}
	cmd, _, err := root.Find(words)
	if err != nil {
		return []string{err.Error()}
	}
	var problems []string
	if cmd == root {
		for _, w := range words {
			if !strings.HasPrefix(w, "-") && !strings.HasPrefix(w, "<") && w != "help" {
				return []string{fmt.Sprintf("tifa has no command %q (in %q)", w, code)}
			}
			break
		}
	}
	for _, w := range words {
		if !strings.HasPrefix(w, "-") || w == "-" || w == "--" {
			continue
		}
		name, _, _ := strings.Cut(w, "=")
		if name == "-h" || name == "--help" {
			continue
		}
		if !hasFlag(cmd, name) {
			problems = append(problems, fmt.Sprintf("%s has no flag %s (in %q)", cmd.CommandPath(), name, code))
		}
	}
	return problems
}

func hasFlag(cmd *cobra.Command, name string) bool {
	for _, set := range []*pflag.FlagSet{cmd.LocalFlags(), cmd.InheritedFlags()} {
		if strings.HasPrefix(name, "--") && set.Lookup(name[2:]) != nil {
			return true
		}
		if !strings.HasPrefix(name, "--") && len(name) == 2 && set.ShorthandLookup(name[1:]) != nil {
			return true
		}
	}
	return false
}

// checkDocRef checks that the doc at path exists and has a heading for the anchor, and returns the
// problem or "".
func checkDocRef(path, shown, anchor string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Sprintf("%s does not exist", shown)
	}
	if anchor == "" {
		return ""
	}
	if !slices.Contains(headingAnchors(string(data)), strings.TrimPrefix(anchor, "#")) {
		return fmt.Sprintf("%s has no heading for %s", shown, anchor)
	}
	return ""
}

// headingAnchors returns the anchors GitHub makes of the headings of a Markdown file: lower case,
// punctuation but hyphens and underscores dropped, spaces turned into hyphens, and -1, -2 for a
// heading that comes again. The front matter and fenced code are skipped.
func headingAnchors(text string) []string {
	var out []string
	seen := map[string]int{}
	inFence := false
	for line := range strings.SplitSeq(text, "\n") {
		if strings.HasPrefix(line, "```") {
			inFence = !inFence
		}
		if inFence || !strings.HasPrefix(line, "#") {
			continue
		}
		title := strings.TrimSpace(strings.TrimLeft(line, "#"))
		if title == line || len(line)-len(strings.TrimLeft(line, "#")) > 6 {
			continue
		}
		title = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`).ReplaceAllString(title, "$1")
		slug := strings.Map(func(r rune) rune {
			switch {
			case r == ' ':
				return '-'
			case r == '-' || r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z':
				return r
			case r >= 'A' && r <= 'Z':
				return r + 'a' - 'A'
			case r > 127:
				return r
			}
			return -1
		}, title)
		if n := seen[slug]; n > 0 {
			out = append(out, fmt.Sprintf("%s-%d", slug, n))
		} else {
			out = append(out, slug)
		}
		seen[slug]++
	}
	return out
}

// exportedNames returns the exported names declared in the Go files of dir that are not tests. With
// recv empty it is the functions, types, variables and constants. With recv set it is the methods
// of that type.
func exportedNames(t *testing.T, dir, recv string) map[string]bool {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, e.Name()), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range f.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if (d.Recv == nil) == (recv == "") && d.Name.IsExported() && (recv == "" || receiverName(d.Recv.List[0].Type) == recv) {
					names[d.Name.Name] = true
				}
			case *ast.GenDecl:
				if recv != "" {
					continue
				}
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.TypeSpec:
						if s.Name.IsExported() {
							names[s.Name.Name] = true
						}
					case *ast.ValueSpec:
						for _, n := range s.Names {
							if n.IsExported() {
								names[n.Name] = true
							}
						}
					}
				}
			}
		}
	}
	if len(names) == 0 {
		t.Fatalf("no exported names found in %s", dir)
	}
	return names
}

func receiverName(e ast.Expr) string {
	for {
		switch x := e.(type) {
		case *ast.StarExpr:
			e = x.X
		case *ast.IndexExpr:
			e = x.X
		case *ast.Ident:
			return x.Name
		default:
			return ""
		}
	}
}

// clientExports returns the names js/runtime/client.ts exports, which is what tifa/client is.
func clientExports(t *testing.T) map[string]bool {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot, "js", "runtime", "client.ts"))
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, m := range regexp.MustCompile(`export (?:type )?\{([^}]*)\}`).FindAllStringSubmatch(string(data), -1) {
		for name := range strings.SplitSeq(m[1], ",") {
			if name = strings.TrimSpace(name); name != "" {
				names[name] = true
			}
		}
	}
	if len(names) == 0 {
		t.Fatal("no exports found in js/runtime/client.ts")
	}
	return names
}

// The notes of a new app say that a call does not reload page data and where to read about it.
func TestAgentNotesPointToReloadingAfterAWrite(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("newapp", "AGENTS.md.tmpl"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"router.invalidate()", "docs/pages-and-data/handlers.md#calling-method-handlers-from-the-browser"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("AGENTS.md.tmpl does not mention %q", want)
		}
	}
}
