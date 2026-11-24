package main

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tifadocs "github.com/liustack/tifa/docs"
	"github.com/liustack/tifa/skills"
)

// existingApp writes the files of an app that was made before tifa agents: a go.mod and a root
// package, and what files says (path to content).
func existingApp(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/shop-front\n\ngo 1.27.0\n")
	writeFile(t, filepath.Join(dir, "hooks.go"), "package shopfront\n")
	for name, content := range files {
		writeFile(t, filepath.Join(dir, filepath.FromSlash(name)), content)
	}
	return dir
}

func readText(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// snapshot reads every file under dir, with its modification time.
func snapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		out[p] = readText(t, p) + "@" + info.ModTime().String()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func exitCode(err error) int {
	var exit *exitError
	if errors.As(err, &exit) {
		return exit.code
	}
	if err != nil {
		return -1
	}
	return 0
}

const skillPath = ".claude/skills/tifa"

// repoRoot is the root of the checkout, seen from cmd/tifa.
const repoRoot = "../.."

// The skill folder holds the checked-in files of skills/tifa, each with one line that says which
// Tifa wrote it.
func TestAgentsWritesTheSkill(t *testing.T) {
	dir := existingApp(t, nil)
	out, _, err := run(t, "agents", "--dir", dir)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	err = fs.WalkDir(skills.FS, "tifa", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		n++
		want, err := os.ReadFile(filepath.Join(repoRoot, "skills", filepath.FromSlash(name)))
		if err != nil {
			t.Errorf("%s is embedded but not in skills/: %v", name, err)
			return nil
		}
		got := readText(t, filepath.Join(dir, ".claude", "skills", filepath.FromSlash(name)))
		line := "<!-- Written by a development build of Tifa with go tool tifa agents. Run it again after you upgrade Tifa. It overwrites edits to this file. -->"
		if !strings.Contains(got, line+"\n") {
			t.Errorf("%s has no line that says who wrote it:\n%.300s", name, got)
		}
		if strings.Replace(got, line+"\n", "", 1) != string(want) {
			t.Errorf("%s is not the checked-in file plus the line", name)
		}
		return nil
	})
	if err != nil || n < 8 {
		t.Fatalf("walked %d embedded files, %v", n, err)
	}
	// SKILL.md keeps its front matter first, which is how the agent finds the skill.
	if skill := readText(t, filepath.Join(dir, skillPath, "SKILL.md")); !strings.HasPrefix(skill, "---\nname: tifa\n") {
		t.Errorf("SKILL.md does not start with its front matter:\n%.200s", skill)
	}
	if !strings.Contains(out, ".claude/skills/tifa/SKILL.md: created") {
		t.Errorf("output does not name the skill file:\n%s", out)
	}
}

// With no AGENTS.md, one is written with the block only, and the values the template fills in come
// from the app: the module path from go.mod, the name of the root package from its Go files and the
// install command from the lock file.
func TestAgentsWritesNotesForAnExistingApp(t *testing.T) {
	dir := existingApp(t, map[string]string{"pnpm-lock.yaml": "lockfileVersion: 9\n"})
	out, _, err := run(t, "agents", "--dir", dir)
	if err != nil {
		t.Fatal(err)
	}
	notes := readText(t, filepath.Join(dir, "AGENTS.md"))
	for _, want := range []string{"# Notes for coding agents\n", "<!-- tifa:begin -->", "<!-- tifa:end -->", "| `pnpm install` |", "`example.com/shop-front/.tifa/tifaapp`", "`shopfront.DB`"} {
		if !strings.Contains(notes, want) {
			t.Errorf("AGENTS.md has no %q:\n%s", want, notes)
		}
	}
	if strings.Contains(notes, "## This app") || strings.Contains(notes, "{{") || strings.Contains(notes, "<no value>") {
		t.Errorf("AGENTS.md holds notes of a new app or an unfilled value:\n%s", notes)
	}
	if !strings.Contains(out, "AGENTS.md: created") || !strings.Contains(out, "CLAUDE.md") {
		t.Errorf("output says nothing of AGENTS.md or of the missing CLAUDE.md:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "CLAUDE.md")); err == nil {
		t.Error("tifa agents wrote CLAUDE.md")
	}
}

func TestAgentsInstallCommandFollowsTheLockFile(t *testing.T) {
	for _, tc := range []struct{ lock, want string }{
		{"pnpm-lock.yaml", "pnpm install"},
		{"package-lock.json", "npm install"},
		{"yarn.lock", "yarn install"},
		{"bun.lock", "bun install"},
	} {
		dir := existingApp(t, map[string]string{tc.lock: "x"})
		if _, _, err := run(t, "agents", "--dir", dir, "--check"); exitCode(err) != 1 {
			t.Fatalf("%s: check: %v", tc.lock, err)
		}
		if out, _, err := run(t, "agents", "--dir", dir); err != nil || strings.Contains(out, "no lock file") {
			t.Fatalf("%s: %v\n%s", tc.lock, err, out)
		}
		if notes := readText(t, filepath.Join(dir, "AGENTS.md")); !strings.Contains(notes, "| `"+tc.want+"` |") {
			t.Errorf("%s: AGENTS.md has no %s", tc.lock, tc.want)
		}
	}
	// No lock file: the command that tifa new would use, and the output says it was a guess.
	withPnpm(t, false)
	dir := existingApp(t, nil)
	out, _, err := run(t, "agents", "--dir", dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "no lock file") || !strings.Contains(readText(t, filepath.Join(dir, "AGENTS.md")), "| `npm install` |") {
		t.Errorf("no lock file: output and notes do not say npm install was assumed:\n%s", out)
	}
}

func TestAgentsTakesThePackageNameFromTheModulePathWhenThereIsNoGoFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/Order-Desk/v2\n\ngo 1.27.0\n")
	out, _, err := run(t, "agents", "--dir", dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readText(t, filepath.Join(dir, "AGENTS.md")), "`orderdesk.DB`") || !strings.Contains(out, "no Go file") {
		t.Errorf("package name or the note about it is wrong:\n%s", out)
	}
}

// An AGENTS.md with no markers keeps its text. The block is added at the end, and the output says so.
func TestAgentsAppendsTheBlockWhenThereAreNoMarkers(t *testing.T) {
	mine := "# My notes\n\nUse tabs.\n"
	dir := existingApp(t, map[string]string{"AGENTS.md": mine})
	out, _, err := run(t, "agents", "--dir", dir)
	if err != nil {
		t.Fatal(err)
	}
	notes := readText(t, filepath.Join(dir, "AGENTS.md"))
	if !strings.HasPrefix(notes, mine+"\n<!-- tifa:begin -->") || !strings.HasSuffix(notes, "<!-- tifa:end -->\n") {
		t.Errorf("the block is not appended after the user's text:\n%s", notes)
	}
	if !strings.Contains(out, "appended") || !strings.Contains(out, "no tifa:begin") {
		t.Errorf("output does not say the block was appended:\n%s", out)
	}
	// And the next run finds the markers.
	if out, _, err := run(t, "agents", "--dir", dir); err != nil || !strings.Contains(out, "up to date") {
		t.Errorf("second run: %v\n%s", err, out)
	}
}

// Only the text between the markers is replaced. Everything outside is kept to the byte.
func TestAgentsRefreshesOnlyTheBlock(t *testing.T) {
	before := "# Notes for coding agents\n\nMy rule before.\n\n<!-- tifa:begin -->\nan old block\n<!-- tifa:end -->\n\n## This app\n\nMy rule after.\n\n<!-- a comment -->"
	dir := existingApp(t, map[string]string{"AGENTS.md": before})
	out, _, err := run(t, "agents", "--dir", dir)
	if err != nil {
		t.Fatal(err)
	}
	notes := readText(t, filepath.Join(dir, "AGENTS.md"))
	head, rest, _ := strings.Cut(before, "<!-- tifa:begin -->")
	_, tail, _ := strings.Cut(rest, "<!-- tifa:end -->")
	if !strings.HasPrefix(notes, head+"<!-- tifa:begin -->\n") || !strings.HasSuffix(notes, "\n<!-- tifa:end -->"+tail) {
		t.Errorf("the text outside the markers changed:\n%s", notes)
	}
	if strings.Contains(notes, "an old block") || !strings.Contains(notes, "## Commands") || !strings.Contains(out, "AGENTS.md: refreshed") {
		t.Errorf("the block was not refreshed:\n%s\n%s", out, notes)
	}
}

func TestAgentsRefusesBrokenMarkers(t *testing.T) {
	for name, text := range map[string]string{
		"no end":     "x\n<!-- tifa:begin -->\ny\n",
		"no begin":   "x\n<!-- tifa:end -->\n",
		"reversed":   "<!-- tifa:end -->\nx\n<!-- tifa:begin -->\n",
		"two blocks": "<!-- tifa:begin -->\n<!-- tifa:end -->\n<!-- tifa:begin -->\n<!-- tifa:end -->\n",
		"not alone":  "see <!-- tifa:begin --> and <!-- tifa:end -->\n",
	} {
		dir := existingApp(t, map[string]string{"AGENTS.md": text})
		_, _, err := run(t, "agents", "--dir", dir)
		if err == nil || !strings.Contains(err.Error(), "AGENTS.md") {
			t.Errorf("%s: error %v does not name AGENTS.md", name, err)
		}
		if readText(t, filepath.Join(dir, "AGENTS.md")) != text {
			t.Errorf("%s: AGENTS.md changed", name)
		}
		if _, err := os.Stat(filepath.Join(dir, skillPath)); err == nil {
			t.Errorf("%s: the skill was written although the notes were refused", name)
		}
	}
}

// A file Tifa wrote is overwritten, also when the user edited it. A file it did not write stays.
// A file an older Tifa wrote that this one no longer has is removed.
func TestAgentsOverwritesItsFilesAndKeepsTheOthers(t *testing.T) {
	dir := existingApp(t, nil)
	if _, _, err := run(t, "agents", "--dir", dir); err != nil {
		t.Fatal(err)
	}
	skill := filepath.Join(dir, skillPath, "SKILL.md")
	writeFile(t, skill, readText(t, skill)+"\nMy own edit.\n")
	writeFile(t, filepath.Join(dir, skillPath, "mine.md"), "my notes\n")
	writeFile(t, filepath.Join(dir, skillPath, "references", "notes.txt"), "mine too\n")
	gone := filepath.Join(dir, skillPath, "references", "retired.md")
	writeFile(t, gone, "<!-- Written by Tifa v0.0.1 with go tool tifa agents. Run it again after you upgrade Tifa. It overwrites edits to this file. -->\n# Retired\n")
	out, _, err := run(t, "agents", "--dir", dir)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(readText(t, skill), "My own edit.") || !strings.Contains(out, "SKILL.md: updated") {
		t.Errorf("SKILL.md was not refreshed:\n%s", out)
	}
	if readText(t, filepath.Join(dir, skillPath, "mine.md")) != "my notes\n" || readText(t, filepath.Join(dir, skillPath, "references", "notes.txt")) != "mine too\n" {
		t.Error("a file Tifa did not write changed")
	}
	if _, err := os.Stat(gone); err == nil || !strings.Contains(out, "retired.md: removed") {
		t.Errorf("a file of an older Tifa that is gone from this one stayed (%v):\n%s", err, out)
	}
}

// Stale files by an older version: both the notes and the skill are named, and nothing changes.
func TestAgentsCheckChangesNothingAndNamesTheStaleFiles(t *testing.T) {
	dir := existingApp(t, nil)
	if out, _, err := run(t, "agents", "--dir", dir, "--check"); exitCode(err) != 1 || !strings.Contains(out, "out of date: AGENTS.md") || !strings.Contains(out, "out of date: "+skillPath+"/SKILL.md") || !strings.Contains(out, "go tool tifa agents") {
		t.Errorf("check on an app with neither: exit %d\n%s", exitCode(err), out)
	}
	if _, err := os.Stat(filepath.Join(dir, "AGENTS.md")); err == nil {
		t.Fatal("--check wrote AGENTS.md")
	}
	if _, _, err := run(t, "agents", "--dir", dir); err != nil {
		t.Fatal(err)
	}
	if out, _, err := run(t, "agents", "--dir", dir, "--check"); err != nil || !strings.Contains(out, "up to date") {
		t.Fatalf("check on a fresh app: %v\n%s", err, out)
	}
	// Only the notes are stale.
	notes := filepath.Join(dir, "AGENTS.md")
	writeFile(t, notes, strings.Replace(readText(t, notes), "## Routes", "## Old routes", 1))
	before := snapshot(t, dir)
	out, _, err := run(t, "agents", "--dir", dir, "--check")
	if exitCode(err) != 1 || !strings.Contains(out, "out of date: AGENTS.md") || strings.Contains(out, skillPath) {
		t.Errorf("check with stale notes: exit %d\n%s", exitCode(err), out)
	}
	// Only the skill is stale.
	if _, _, err := run(t, "agents", "--dir", dir); err != nil {
		t.Fatal(err)
	}
	skill := filepath.Join(dir, skillPath, "references", "testing.md")
	writeFile(t, skill, "old")
	out, _, err = run(t, "agents", "--dir", dir, "--check")
	if exitCode(err) != 1 || strings.Contains(out, "AGENTS.md") || !strings.Contains(out, "out of date: "+skillPath+"/references/testing.md") {
		t.Errorf("check with a stale reference: exit %d\n%s", exitCode(err), out)
	}
	_ = before
	if readText(t, skill) != "old" {
		t.Error("--check wrote a file")
	}
}

// A second run changes nothing: not a byte and not a modification time.
func TestAgentsIsIdempotent(t *testing.T) {
	dir := existingApp(t, map[string]string{"AGENTS.md": "# Mine\n"})
	if _, _, err := run(t, "agents", "--dir", dir); err != nil {
		t.Fatal(err)
	}
	first := snapshot(t, dir)
	time.Sleep(20 * time.Millisecond)
	out, _, err := run(t, "agents", "--dir", dir)
	if err != nil {
		t.Fatal(err)
	}
	second := snapshot(t, dir)
	for p, v := range first {
		if second[p] != v {
			t.Errorf("%s changed on the second run", p)
		}
	}
	if len(first) != len(second) || !strings.Contains(out, "up to date") || strings.Contains(out, "created") || strings.Contains(out, "updated") {
		t.Errorf("second run output:\n%s", out)
	}
}

func TestAgentsNeedsAnApp(t *testing.T) {
	_, _, err := run(t, "agents", "--dir", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "go.mod") {
		t.Errorf("error %v does not mention go.mod", err)
	}
}

func TestAgentsSkillsDirFlag(t *testing.T) {
	dir := existingApp(t, nil)
	if _, _, err := run(t, "agents", "--dir", dir, "--skills-dir", ".agents/skills/tifa"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".agents", "skills", "tifa", "SKILL.md")); err != nil {
		t.Error(err)
	}
	if _, err := os.Stat(filepath.Join(dir, skillPath)); err == nil {
		t.Error("the default folder was written too")
	}
}

// tifa new writes the same files, and a run of tifa agents in the new app has nothing to do.
func TestNewWritesTheSkillAndAgentsHasNothingToDo(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "app")
	if _, _, err := run(t, "new", dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, skillPath, "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	if out, _, err := run(t, "agents", "--dir", dir, "--check"); err != nil {
		t.Errorf("a new app is out of date:\n%s", out)
	}
	before := snapshot(t, dir)
	if _, _, err := run(t, "agents", "--dir", dir); err != nil {
		t.Fatal(err)
	}
	after := snapshot(t, dir)
	for p, v := range before {
		if after[p] != v {
			t.Errorf("%s changed on tifa agents after tifa new", p)
		}
	}
}

// tifa check mentions the files when the app has opted in and they are out of date, and says
// nothing for an app that has neither.
func TestAgentsHintOfCheck(t *testing.T) {
	dir := existingApp(t, nil)
	if hint := agentsHint(dir); hint != "" {
		t.Errorf("an app with neither got a hint: %q", hint)
	}
	if _, _, err := run(t, "agents", "--dir", dir); err != nil {
		t.Fatal(err)
	}
	if hint := agentsHint(dir); hint != "" {
		t.Errorf("an app that is up to date got a hint: %q", hint)
	}
	writeFile(t, filepath.Join(dir, skillPath, "SKILL.md"), "old")
	if hint := agentsHint(dir); !strings.Contains(hint, "go tool tifa agents") || strings.Contains(hint, "\n") {
		t.Errorf("hint for a stale skill: %q", hint)
	}
}

// With no lock file the install command stays what AGENTS.md says, so a machine with another PATH,
// such as a CI job, sees no change.
func TestAgentsKeepsTheInstallCommandWithoutALockFile(t *testing.T) {
	withPnpm(t, false)
	dir := existingApp(t, nil)
	if _, _, err := run(t, "agents", "--dir", dir); err != nil {
		t.Fatal(err)
	}
	notes := filepath.Join(dir, "AGENTS.md")
	writeFile(t, notes, strings.Replace(readText(t, notes), "| `npm install` |", "| `yarn install` |", 1))
	if out, _, err := run(t, "agents", "--dir", dir, "--check"); err != nil {
		t.Errorf("the command in AGENTS.md was taken for a change:\n%s", out)
	}
}

// A file with CRLF line ends keeps them: the block is compared without the carriage returns, so a
// second run changes nothing, and a refresh or an appended block is written with CRLF.
func TestAgentsKeepsCRLFLineEnds(t *testing.T) {
	lf := "# Notes\n\nMy rule.\n\n<!-- tifa:begin -->\nan old block\n<!-- tifa:end -->\n\nAfter.\n"
	crlf := strings.ReplaceAll(lf, "\n", "\r\n")
	dir := existingApp(t, map[string]string{"AGENTS.md": crlf})
	if _, _, err := run(t, "agents", "--dir", dir); err != nil {
		t.Fatal(err)
	}
	notes := readText(t, filepath.Join(dir, "AGENTS.md"))
	if strings.Contains(strings.ReplaceAll(notes, "\r\n", ""), "\n") || strings.Contains(strings.ReplaceAll(notes, "\r\n", ""), "\r") {
		t.Errorf("the refreshed file mixes line ends:\n%q", notes)
	}
	if !strings.HasPrefix(notes, "# Notes\r\n\r\nMy rule.\r\n\r\n<!-- tifa:begin -->\r\n") || !strings.HasSuffix(notes, "<!-- tifa:end -->\r\n\r\nAfter.\r\n") || strings.Contains(notes, "an old block") {
		t.Errorf("the refresh changed more than the block, or none:\n%q", notes)
	}
	// The second run has nothing to do and leaves the bytes alone.
	out, _, err := run(t, "agents", "--dir", dir)
	if err != nil || !strings.Contains(out, "up to date") || readText(t, filepath.Join(dir, "AGENTS.md")) != notes {
		t.Errorf("second run: %v\n%s", err, out)
	}
	if _, _, err := run(t, "agents", "--dir", dir, "--check"); err != nil {
		t.Errorf("--check on a CRLF file that is up to date: %v", err)
	}
	// A file without markers gets the block appended with its line ends.
	dir = existingApp(t, map[string]string{"AGENTS.md": "# My notes\r\n\r\nUse tabs.\r\n"})
	if _, _, err := run(t, "agents", "--dir", dir); err != nil {
		t.Fatal(err)
	}
	notes = readText(t, filepath.Join(dir, "AGENTS.md"))
	if strings.Contains(strings.ReplaceAll(notes, "\r\n", ""), "\n") || !strings.HasPrefix(notes, "# My notes\r\n\r\nUse tabs.\r\n\r\n<!-- tifa:begin -->\r\n") {
		t.Errorf("the appended block does not follow the line ends:\n%q", notes)
	}
}

// testNotesApp is an app to render the managed block with.
var testNotesApp = newApp{Module: "example.com/shop", Package: "shop", Install: "pnpm install", Run: "pnpm", Written: "Tifa v9.9.9"}

// The managed block holds an index of the docs for writing an app, made from the docs of this
// version: a line per section folder, then one line per doc with its file name and its whole summary.
// Docs about developing Tifa are left out.
func TestNotesIndexTheAppDocs(t *testing.T) {
	block, err := notesBlock(testNotesApp)
	if err != nil {
		t.Fatal(err)
	}
	all, _, err := readDocs(repoRoot, "")
	if err != nil {
		t.Fatal(err)
	}
	var app int
	for _, d := range all {
		section, name := path.Split(d.path)
		if d.audience != audienceApp {
			if strings.Contains(block, section) {
				t.Errorf("the block names %s, a folder of docs about developing Tifa", section)
			}
			continue
		}
		app++
		head := "`" + section + "`"
		line := "- `" + name + "`: " + strings.Join(strings.Fields(d.summary), " ")
		i, j := strings.Index(block, "\n"+head+"\n"), strings.Index(block, "\n"+line+"\n")
		if i < 0 || j < i {
			t.Errorf("the block has no line %q under %q", line, head)
		}
	}
	if app < 20 {
		t.Fatalf("only %d docs for apps in %s", app, repoRoot)
	}
	// The agent opens the files directly, so the block says where the folder is.
	if !strings.Contains(block, "`go tool tifa docs`") || !strings.Contains(block, ".zh-CN.md") {
		t.Errorf("the block does not say how to find the docs folder or the Chinese copies:\n%s", block)
	}
}

// Long context files lower the success of coding agents, so the block stays small.
func TestNotesBlockIsSmall(t *testing.T) {
	block, err := notesBlock(testNotesApp)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(block); n > 12*1024 {
		t.Errorf("the managed block of AGENTS.md is %d bytes, keep it at most 12 KB: move details to the docs and leave the path", n)
	}
}

// The docs that tifa carries for the index are the English docs for apps, every one of them and
// nothing else.
func TestEmbeddedDocsAreTheAppDocs(t *testing.T) {
	all, _, err := readDocs(repoRoot, "")
	if err != nil {
		t.Fatal(err)
	}
	var want []string
	for _, d := range all {
		if d.audience == audienceApp {
			want = append(want, strings.TrimPrefix(d.path, "docs/"))
		}
	}
	var got []string
	err = fs.WalkDir(tifadocs.FS, ".", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && !isSectionIndex(p) {
			got = append(got, p)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, want) {
		t.Errorf("embedded docs:\n%s\nwant the English docs for apps:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
