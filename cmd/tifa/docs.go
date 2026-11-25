package main

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

func newDocsCmd() *cobra.Command {
	var dir string
	var all, when bool
	var lang string
	cmd := &cobra.Command{
		Use:   "docs",
		Short: "Print where the docs of the tifa version this app uses are, and what each one covers",
		Long: `docs finds the tifa module the application's go.mod requires (go list -m, so a replace
directive works too) and prints the folder that holds its docs, then one line for each doc about
writing an app: its path and what it covers. --when adds when to read each, and --all also lists the
docs about developing tifa itself. The docs are those of the tifa version the application builds with, not those of the
main branch.

The docs for apps also exist in Chinese: --lang zh-CN lists those files instead, with their Chinese
summaries. The language is never chosen for you, and without --lang the English docs are listed.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if lang != "" && lang != zhCN {
				return fmt.Errorf("--lang %q is not available, the only value is %s (the docs are in English when it is not set)", lang, zhCN)
			}
			root, err := filepath.Abs(dir)
			if err != nil {
				return err
			}
			mod, err := findTifaModule(cmd.Context(), root)
			if err != nil {
				return err
			}
			docs, warnings, err := readDocs(mod.Dir, lang)
			if err != nil {
				return err
			}
			if lang == zhCN && !slices.ContainsFunc(docs, func(d docEntry) bool { return strings.HasSuffix(d.path, ".zh-CN.md") }) {
				return fmt.Errorf("%s %s has no Chinese docs", tifaModule, mod.Version)
			}
			for _, w := range warnings {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", w)
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "%s %s", tifaModule, mod.Version)
			if mod.Replace != nil {
				fmt.Fprintf(out, " (replaced by %s)", mod.Replace.Path)
			}
			fmt.Fprintf(out, "\nDocs: %s\nThe paths below are relative to %s.\n\n", filepath.Join(mod.Dir, "docs"), mod.Dir)
			fmt.Fprint(out, docsOverview(docs, all, when))
			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "dir", ".", "application root, the directory holding go.mod and app/")
	cmd.Flags().BoolVar(&all, "all", false, "also list the docs about developing tifa itself")
	cmd.Flags().StringVar(&lang, "lang", "", "list the docs in this language: zh-CN lists the Chinese docs (English when not set)")
	cmd.Flags().BoolVar(&when, "when", false, "also print when to read each doc")
	return cmd
}

// goModule is what go list -m -json prints about a module, as far as tifa docs reads it.
type goModule struct {
	Version string
	Dir     string
	Replace *struct{ Path string }
}

// findTifaModule asks the go command for the tifa module of the application at root.
func findTifaModule(ctx context.Context, root string) (goModule, error) {
	list := exec.CommandContext(ctx, "go", "list", "-m", "-json", tifaModule)
	list.Dir = root
	var stderr bytes.Buffer
	list.Stderr = &stderr
	out, err := list.Output()
	if err != nil {
		return goModule{}, fmt.Errorf("go list -m %s in %s: %w\n%s", tifaModule, root, err, strings.TrimSpace(stderr.String()))
	}
	return parseModule(out)
}

// parseModule reads the JSON of go list -m. A module without a Dir is not in the module cache yet.
func parseModule(data []byte) (goModule, error) {
	var mod goModule
	if err := json.Unmarshal(data, &mod); err != nil {
		return goModule{}, fmt.Errorf("go list -m -json: %w", err)
	}
	if mod.Dir == "" {
		return goModule{}, fmt.Errorf("the files of %s %s are not downloaded, run: go mod download %s", tifaModule, mod.Version, tifaModule)
	}
	return mod, nil
}

// Who a doc is for, the value of `audience` in its front matter. scripts/docs-list.sh --check and a
// test make every doc carry one, so a new doc cannot be left out of both groups.
const (
	audienceApp = "app" // writing, running and deploying an app
	audienceDev = "dev" // developing tifa itself
)

// docEntry is one doc of the tifa module as its front matter describes it.
type docEntry struct {
	path     string // relative to the module folder, with slashes
	summary  string
	when     []string
	audience string
}

// isSectionIndex says whether the doc at path is the index.md of a section folder, which holds the
// title and the order of the section and is not a doc to list.
func isSectionIndex(path string) bool {
	base := filepath.Base(path)
	return base == "index.md" || base == "index.zh-CN.md"
}

// zhCN is the language of the Chinese docs, whose files end in .zh-CN.md beside the English ones.
const zhCN = "zh-CN"

// wantedInLang says whether the doc at path is listed for lang. In English, the .zh-CN.md files are
// left out so each doc shows once. In Chinese, the Chinese file stands for its English one, and the
// docs for developing tifa, which have no Chinese file, stay in English.
func wantedInLang(path, audience, lang string) bool {
	chinese := strings.HasSuffix(path, ".zh-CN.md")
	if lang == zhCN {
		return chinese || audience == audienceDev
	}
	return !chinese
}

// readDocs reads the front matter of every Markdown file under docs/ in dir but docs/research,
// in byte order of the paths. lang is "" for the English docs or zhCN for the Chinese ones.
// A doc without a summary is left out, and a warning says so.
func readDocs(dir, lang string) (docs []docEntry, warnings []string, err error) {
	var paths []string
	err = fs.WalkDir(os.DirFS(dir), "docs", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			if p == "docs/research" {
				return fs.SkipDir
			}
		case strings.HasSuffix(p, ".md") && !isSectionIndex(p):
			paths = append(paths, p)
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	slices.Sort(paths)
	for _, p := range paths {
		text, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(p)))
		if err != nil {
			return nil, nil, err
		}
		meta, ok := frontMatter(string(text))
		if !ok {
			warnings = append(warnings, p+" has no front matter summary")
			continue
		}
		if wantedInLang(p, meta.audience, lang) {
			docs = append(docs, docEntry{p, meta.summary, meta.when, meta.audience})
		}
	}
	return docs, warnings, nil
}

// docsIndex lists the docs of the tifa module in dir like scripts/docs-list.sh: a line with the
// path and the summary of each, and a line with when to read it.
func docsIndex(dir string) (index string, warnings []string, err error) {
	docs, warnings, err := readDocs(dir, "")
	if err != nil {
		return "", nil, err
	}
	var out strings.Builder
	for _, d := range docs {
		writeDoc(&out, d, d.summary, true)
	}
	return out.String(), warnings, nil
}

func writeDoc(out *strings.Builder, d docEntry, summary string, when bool) {
	fmt.Fprintf(out, "%s - %s\n", d.path, summary)
	if when && len(d.when) > 0 {
		fmt.Fprintf(out, "  Read when: %s\n", strings.Join(d.when, " | "))
	}
}

// docsOverview is what tifa docs prints after the folder. By default it lists the docs for the
// author of an app, a line each with the whole summary: what the doc is and the names, settings
// and commands it covers, which AGENTS.md leaves out. all adds the docs about developing tifa, and when the
// lines that say when to read each doc. A tifa that has no audience in any doc (a version from
// before the field) lists every doc as one for apps.
func docsOverview(docs []docEntry, all, when bool) string {
	fieldExists := slices.ContainsFunc(docs, func(d docEntry) bool { return d.audience != "" })
	var app, dev []docEntry
	for _, d := range docs {
		if d.audience == audienceDev && fieldExists {
			dev = append(dev, d)
		} else {
			app = append(app, d)
		}
	}
	var out strings.Builder
	if all && len(dev) > 0 {
		out.WriteString("For writing an app:\n")
	}
	for _, d := range app {
		writeDoc(&out, d, d.summary, when)
	}
	if all && len(dev) > 0 {
		out.WriteString("\nFor developing tifa itself:\n")
		for _, d := range dev {
			writeDoc(&out, d, d.summary, when)
		}
	}
	out.WriteString("\n")
	if !all && len(dev) > 0 {
		fmt.Fprintf(&out, "%d more docs are about developing tifa itself: run `go tool tifa docs --all` to list them.\n", len(dev))
	}
	if !when {
		out.WriteString("Run `go tool tifa docs --when` to see when to read each doc.\n")
	}
	out.WriteString("Read the docs that apply before you change code.\n")
	return out.String()
}

// docMeta is what the front matter of a doc says.
type docMeta struct {
	summary  string
	when     []string
	audience string
	order    int // the place of the doc in its section folder, 0 when the front matter has none
}

// frontMatter reads the summary, read_when, audience and order of the front matter that starts text, in
// the two forms scripts/docs-list.sh reads: a list of "- item" lines, or [a, b] on the key's line.
// ok is false when there is no front matter or no summary.
func frontMatter(text string) (meta docMeta, ok bool) {
	lines := strings.Split(text, "\n")
	if lines[0] != "---" {
		return docMeta{}, false
	}
	closed, inList := false, false
	for _, line := range lines[1:] {
		if line == "---" {
			closed = true
			break
		}
		switch {
		case strings.HasPrefix(line, "summary:"):
			meta.summary = unquote(strings.TrimPrefix(line, "summary:"))
			inList = false
		case strings.HasPrefix(line, "audience:"):
			meta.audience = unquote(strings.TrimPrefix(line, "audience:"))
			inList = false
		case strings.HasPrefix(line, "order:"):
			meta.order, _ = strconv.Atoi(unquote(strings.TrimPrefix(line, "order:")))
			inList = false
		case strings.HasPrefix(line, "read_when:"):
			rest := unquote(strings.TrimPrefix(line, "read_when:"))
			inList = true
			if strings.HasPrefix(rest, "[") && strings.HasSuffix(rest, "]") {
				inList = false
				for item := range strings.SplitSeq(rest[1:len(rest)-1], ",") {
					if item = unquote(item); item != "" {
						meta.when = append(meta.when, item)
					}
				}
			}
		case inList && isListItem(line):
			meta.when = append(meta.when, unquote(strings.TrimLeft(strings.TrimLeft(line, " \t")[1:], " \t")))
		case line != "" && line[0] != ' ' && line[0] != '\t':
			inList = false
		}
	}
	return meta, closed && meta.summary != ""
}

// isListItem reports whether line is an indented "- item" line.
func isListItem(line string) bool {
	rest := strings.TrimLeft(line, " \t")
	return len(rest) < len(line) && strings.HasPrefix(rest, "-") && len(rest) > 1 && (rest[1] == ' ' || rest[1] == '\t')
}

// unquote trims the spaces around s and one pair of single or double quotes.
func unquote(s string) string {
	s = strings.Trim(s, " \t")
	if len(s) >= 2 && (s[0] == '\'' || s[0] == '"') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}
