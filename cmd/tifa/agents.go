package main

import (
	"bytes"
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"slices"
	"strings"

	"github.com/liustack/tifa/skills"
	"github.com/spf13/cobra"
	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
)

const (
	defaultSkillsDir = ".claude/skills/tifa"
	beginMarker      = "<!-- tifa:begin -->"
	endMarker        = "<!-- tifa:end -->"
	// writtenPrefix and writtenSuffix surround the line that every skill file carries. A file
	// with such a line in its first lines is one that tifa agents wrote.
	writtenPrefix = "<!-- Written by "
	writtenSuffix = " with go tool tifa agents. Run it again after you upgrade Tifa. It overwrites edits to this file. -->"
)

func newAgentsCmd() *cobra.Command {
	var dir, skillsDir string
	var check bool
	cmd := &cobra.Command{
		Use:   "agents",
		Short: "Write the Tifa skill and the managed notes of AGENTS.md for the tifa version this app uses",
		Long: `agents writes the skill of this version of tifa into the app (.claude/skills/tifa/SKILL.md and
its references) and refreshes the part of AGENTS.md between the lines <!-- tifa:begin --> and
<!-- tifa:end -->, which comes from the same template as the AGENTS.md of tifa new. Text outside
the markers is left alone. With no AGENTS.md it writes one. With an AGENTS.md that has no markers
it appends the block at the end and says so.

The module path, the name of the root package and the install command that the notes name are read
from the app: go.mod, the Go files of the root and the lock file. Anything it had to guess is
printed.

Run it again after you upgrade tifa. A file in the skill folder that tifa wrote is overwritten,
edits included, and one that an older tifa wrote and this one no longer has is removed. A file
there that tifa did not write is left alone. Nothing runs this by itself but tifa new.

With --check nothing is written. It names the files that are out of date and exits with status 1,
for a CI job.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := filepath.Abs(dir)
			if err != nil {
				return err
			}
			plan, err := planAgents(root, skillsDir)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			changes := plan.files
			if check {
				if len(changes) == 0 {
					fmt.Fprintf(out, "AGENTS.md and %s are up to date (%s)\n", skillsDir, plan.written)
					return nil
				}
				for _, c := range changes {
					fmt.Fprintf(out, "out of date: %s\n", c.rel)
				}
				fmt.Fprintln(out, "Run: go tool tifa agents")
				return &exitError{code: 1}
			}
			if len(changes) == 0 {
				fmt.Fprintf(out, "AGENTS.md and %s are up to date (%s)\n", skillsDir, plan.written)
				return nil
			}
			for _, n := range plan.notes {
				fmt.Fprintf(out, "note: %s\n", n)
			}
			for _, c := range changes {
				if err := c.apply(root); err != nil {
					return err
				}
				fmt.Fprintf(out, "%s: %s\n", c.rel, c.what)
			}
			if _, err := os.Stat(filepath.Join(root, "CLAUDE.md")); errors.Is(err, fs.ErrNotExist) {
				fmt.Fprintln(out, "note: there is no CLAUDE.md. Claude Code reads CLAUDE.md, not AGENTS.md, so put the line @AGENTS.md in one.")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "dir", ".", "application root, the directory holding go.mod and app/")
	cmd.Flags().BoolVar(&check, "check", false, "change nothing, name the files that are out of date and exit with status 1")
	cmd.Flags().StringVar(&skillsDir, "skills-dir", defaultSkillsDir, "folder for the skill, relative to the application root")
	return cmd
}

// agentChange is one file that differs from what this version writes.
type agentChange struct {
	rel     string // slash path relative to the app root
	content []byte // nil when the file is to be removed
	what    string // created, updated, removed, refreshed the block, ...
}

func (c agentChange) apply(root string) error {
	target := filepath.Join(root, filepath.FromSlash(c.rel))
	if c.content == nil {
		return os.Remove(target)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	return os.WriteFile(target, c.content, 0o644)
}

// agentPlan is what tifa agents would do.
type agentPlan struct {
	written string // who writes: Tifa v0.0.5, or a development build of Tifa
	notes   []string
	files   []agentChange // every file in the skill folder and AGENTS.md that differs
}

// planAgents compares the files of the app at root with what this version of tifa writes.
func planAgents(root, skillsDir string) (agentPlan, error) {
	app, notes, err := existingAppValues(root)
	if err != nil {
		return agentPlan{}, err
	}
	plan := agentPlan{written: app.Written, notes: notes}
	notesChange, err := planNotes(root, app)
	if err != nil {
		return agentPlan{}, err
	}
	if notesChange != nil {
		plan.files = append(plan.files, *notesChange)
	}
	skillChanges, err := planSkill(root, skillsDir, app.Written)
	if err != nil {
		return agentPlan{}, err
	}
	plan.files = append(plan.files, skillChanges...)
	return plan, nil
}

// syncAgents writes the notes and the skill for app into the new app at root, as tifa agents does.
func syncAgents(root string, app newApp) ([]agentChange, error) {
	var changes []agentChange
	notesChange, err := planNotes(root, app)
	if err != nil {
		return nil, err
	}
	if notesChange != nil {
		changes = append(changes, *notesChange)
	}
	skillChanges, err := planSkill(root, defaultSkillsDir, app.Written)
	if err != nil {
		return nil, err
	}
	changes = append(changes, skillChanges...)
	for _, c := range changes {
		if err := c.apply(root); err != nil {
			return nil, err
		}
	}
	return changes, nil
}

// writtenBy says which tifa writes the files: the release the command was built from, or a
// development build when it was built in a checkout or from a replaced module.
func writtenBy(info *debug.BuildInfo) string {
	if v := releaseVersion(info); v != "" {
		return "Tifa " + v
	}
	return "a development build of Tifa"
}

// existingAppValues reads the values the template of AGENTS.md fills in from the app at root, and
// says what it had to guess.
func existingAppValues(root string) (app newApp, notes []string, err error) {
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return app, nil, fmt.Errorf("%s has no go.mod to read (%v): run tifa agents in the app root or pass --dir", root, err)
	}
	mf, err := modfile.ParseLax("go.mod", data, nil)
	if err != nil {
		return app, nil, fmt.Errorf("go.mod: %w", err)
	}
	if mf.Module == nil {
		return app, nil, errors.New("go.mod has no module line")
	}
	app.Module = mf.Module.Mod.Path
	prefix, _, _ := module.SplitPathVersion(app.Module)
	app.Title, app.Package = deriveNames(prefix)
	if pkg := rootPackageName(root); pkg != "" {
		app.Package = pkg
	} else {
		notes = append(notes, fmt.Sprintf("the root has no Go file, so the root package is %s, from the module path %s", app.Package, app.Module))
	}
	app.Install, app.Run, notes = installCommand(root, notes)
	info, _ := debug.ReadBuildInfo()
	app.Written = writtenBy(info)
	return app, notes, nil
}

// rootPackageName returns the package of the first Go file in root that is not a test, or "".
func rootPackageName(root string) string {
	entries, _ := os.ReadDir(root)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), filepath.Join(root, e.Name()), nil, parser.PackageClauseOnly)
		if err == nil && f.Name.Name != "main" {
			return f.Name.Name
		}
	}
	return ""
}

// installRowRE finds the install command in the first row of the command table of the block.
var installRowRE = regexp.MustCompile("(?m)^\\| `(pnpm|npm|yarn|bun) install` \\|")

var runOf = map[string]string{"pnpm": "pnpm", "npm": "npm run", "yarn": "yarn", "bun": "bun run"}

// installCommand finds the package manager of the app from its lock file. With none, it keeps the
// command that the managed block of AGENTS.md already names, so a CI machine with another PATH does
// not see a change, and otherwise uses the one tifa new would pick. Either is a guess and gets a note.
func installCommand(root string, notes []string) (install, run string, _ []string) {
	for _, lock := range []struct{ file, install, run string }{
		{"pnpm-lock.yaml", "pnpm install", "pnpm"},
		{"package-lock.json", "npm install", "npm run"},
		{"yarn.lock", "yarn install", "yarn"},
		{"bun.lock", "bun install", "bun run"},
		{"bun.lockb", "bun install", "bun run"},
	} {
		if _, err := os.Stat(filepath.Join(root, lock.file)); err == nil {
			return lock.install, lock.run, notes
		}
	}
	if text, err := os.ReadFile(filepath.Join(root, "AGENTS.md")); err == nil {
		if m := installRowRE.FindStringSubmatch(string(text)); m != nil {
			return m[1] + " install", runOf[m[1]], append(notes, fmt.Sprintf("no lock file, so the install command stays as AGENTS.md has it: %s install", m[1]))
		}
	}
	install, run = packageManager()
	return install, run, append(notes, fmt.Sprintf("no lock file, so the install command in the notes is %s, the one tifa new picks", install))
}

// notesBlock renders the managed block of AGENTS.md for app, markers included, without a final
// newline.
func notesBlock(app newApp) (string, error) {
	text, err := renderFile(newapp, "newapp/AGENTS.md.tmpl", app)
	if err != nil {
		return "", err
	}
	begin, end, ok := blockLines(string(text))
	if !ok {
		return "", errors.New("the template of AGENTS.md has no managed block")
	}
	lines := strings.SplitAfter(string(text), "\n")
	return strings.TrimSuffix(strings.Join(lines[begin:end+1], ""), "\n"), nil
}

// blockLines returns the numbers of the begin and end marker lines. A marker is alone on its line.
func blockLines(text string) (begin, end int, ok bool) {
	var begins, ends []int
	for i, line := range strings.Split(text, "\n") {
		switch strings.TrimSpace(line) {
		case beginMarker:
			begins = append(begins, i)
		case endMarker:
			ends = append(ends, i)
		}
	}
	if len(begins) != 1 || len(ends) != 1 || begins[0] >= ends[0] {
		return 0, 0, false
	}
	return begins[0], ends[0], true
}

// planNotes returns the change to AGENTS.md at root, or nil when it is as this version writes it.
func planNotes(root string, app newApp) (*agentChange, error) {
	block, err := notesBlock(app)
	if err != nil {
		return nil, err
	}
	target := filepath.Join(root, "AGENTS.md")
	existing, err := os.ReadFile(target)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return &agentChange{rel: "AGENTS.md", content: []byte("# Notes for coding agents\n\n" + block + "\n"), what: "created"}, nil
	case err != nil:
		return nil, err
	}
	text := string(existing)
	if !strings.Contains(text, "tifa:begin") && !strings.Contains(text, "tifa:end") {
		sep := "\n"
		if !strings.HasSuffix(text, "\n") && text != "" {
			sep = "\n\n"
		}
		return &agentChange{rel: "AGENTS.md", content: []byte(text + sep + block + "\n"), what: "appended the Tifa block, there were no tifa:begin and tifa:end lines (put them around the block to have it refreshed in place)"}, nil
	}
	begin, end, ok := blockLines(text)
	if !ok {
		return nil, errors.New("AGENTS.md has tifa:begin and tifa:end lines that are not one pair in order, each alone on its line. Fix them by hand, or remove both to have the block appended. Nothing was written")
	}
	lines := strings.SplitAfter(text, "\n")
	current := strings.TrimSuffix(strings.Join(lines[begin:end+1], ""), "\n")
	if current == block {
		return nil, nil
	}
	// Whatever the marker lines carry besides the markers is theirs: only whole lines are replaced.
	out := strings.Join(lines[:begin], "") + block
	rest := strings.Join(lines[end+1:], "")
	if strings.HasSuffix(strings.Join(lines[begin:end+1], ""), "\n") {
		out += "\n"
	}
	return &agentChange{rel: "AGENTS.md", content: []byte(out + rest), what: "refreshed the block between the markers"}, nil
}

// planSkill returns the files of the skill folder that differ from this version, and the files of an
// older tifa that this one no longer has.
func planSkill(root, skillsDir, written string) ([]agentChange, error) {
	line := writtenPrefix + written + writtenSuffix
	want := map[string][]byte{} // path under the skill folder, slash
	err := fs.WalkDir(skills.FS, "tifa", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := fs.ReadFile(skills.FS, name)
		if err != nil {
			return err
		}
		want[strings.TrimPrefix(name, "tifa/")] = withWrittenLine(data, line)
		return nil
	})
	if err != nil {
		return nil, err
	}
	var changes []agentChange
	base := filepath.ToSlash(filepath.Clean(skillsDir))
	if filepath.IsAbs(skillsDir) {
		return nil, fmt.Errorf("--skills-dir %s must be relative to the app root", skillsDir)
	}
	names := make([]string, 0, len(want))
	for n := range want {
		names = append(names, n)
	}
	slices.Sort(names)
	for _, n := range names {
		rel := path.Join(base, n)
		have, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		switch {
		case errors.Is(err, fs.ErrNotExist):
			changes = append(changes, agentChange{rel: rel, content: want[n], what: "created"})
		case err != nil:
			return nil, err
		case !bytes.Equal(have, want[n]):
			changes = append(changes, agentChange{rel: rel, content: want[n], what: "updated"})
		}
	}
	// Files of an older tifa: they carry its line, and this version has no such file.
	folder := filepath.Join(root, filepath.FromSlash(base))
	_ = filepath.WalkDir(folder, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".md") {
			return nil
		}
		sub, _ := filepath.Rel(folder, p)
		sub = filepath.ToSlash(sub)
		if _, ok := want[sub]; ok {
			return nil
		}
		if head, err := os.ReadFile(p); err == nil && hasWrittenLine(head) {
			changes = append(changes, agentChange{rel: path.Join(base, sub), what: "removed, this version of Tifa has no such file"})
		}
		return nil
	})
	return changes, nil
}

// withWrittenLine adds the line that says which tifa wrote a skill file. SKILL.md keeps its front
// matter first, so the line goes after it.
func withWrittenLine(data []byte, line string) []byte {
	text := string(data)
	if rest, ok := strings.CutPrefix(text, "---\n"); ok {
		if front, body, ok := strings.Cut(rest, "\n---\n"); ok {
			return []byte("---\n" + front + "\n---\n" + line + "\n" + body)
		}
	}
	return []byte(line + "\n" + text)
}

// hasWrittenLine tells whether the first lines of a file carry the line of tifa agents.
func hasWrittenLine(data []byte) bool {
	for i, l := range strings.SplitN(string(data), "\n", 12) {
		if i < 11 && strings.HasPrefix(l, writtenPrefix) && strings.HasSuffix(l, writtenSuffix) {
			return true
		}
	}
	return false
}

// agentsHint is the one line tifa check prints when the app has the skill or the managed block and
// they are out of date, and "" when they are up to date, the app has neither, or something is wrong
// (tifa agents says what).
func agentsHint(root string) string {
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(defaultSkillsDir))); err != nil {
		text, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
		if err != nil || !strings.Contains(string(text), beginMarker) {
			return ""
		}
	}
	plan, err := planAgents(root, defaultSkillsDir)
	if err != nil || len(plan.files) == 0 {
		return ""
	}
	return "note: AGENTS.md or " + defaultSkillsDir + " is out of date for this Tifa. Run: go tool tifa agents"
}
