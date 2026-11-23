package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// docsTree copies the docs scripts into a temp tree, so the checks run on docs the test writes.
func docsTree(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash is not installed")
	}
	root := t.TempDir()
	for _, name := range []string{"docs-list.sh", "doc-hash.sh"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "scripts", name))
		if err != nil {
			t.Fatal(err)
		}
		dst := filepath.Join(root, "scripts", name)
		writeFile(t, dst, string(data))
		if err := os.Chmod(dst, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func docHash(t *testing.T, root, file string) string {
	t.Helper()
	out, err := exec.Command("bash", filepath.Join(root, "scripts", "doc-hash.sh"), file).Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

func checkTranslations(t *testing.T, root string) (string, error) {
	t.Helper()
	cmd := exec.Command("bash", filepath.Join(root, "scripts", "docs-list.sh"), "--check-translations")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

const appFront = "---\nsummary: Something\naudience: app\n---\n\n# Title\n"

func zhDoc(hash string) string {
	return "---\nsummary: 某事\naudience: app\ntranslatedFrom: " + hash + "\n---\n\n# 标题 {#title}\n"
}

func TestTranslationCheckPassesWhenPartnersMatchAtAnyDepth(t *testing.T) {
	root := docsTree(t)
	for _, rel := range []string{"docs/a.md", "docs/guides/b.md", "docs/deep/er/c.md"} {
		en := filepath.Join(root, rel)
		writeFile(t, en, appFront)
		writeFile(t, strings.TrimSuffix(en, ".md")+".zh-CN.md", zhDoc(docHash(t, root, en)))
	}
	writeFile(t, filepath.Join(root, "docs/dev.md"), "---\nsummary: Dev\naudience: dev\n---\n")
	writeFile(t, filepath.Join(root, "docs/research/x.md"), "---\nsummary: Idea\n---\n")
	if out, err := checkTranslations(t, root); err != nil {
		t.Fatalf("check failed: %v\n%s", err, out)
	}
}

func TestTranslationCheckNamesEachProblem(t *testing.T) {
	root := docsTree(t)
	missing := filepath.Join(root, "docs/missing.md")
	writeFile(t, missing, appFront)
	stale := filepath.Join(root, "docs/sub/stale.md")
	writeFile(t, stale, appFront)
	writeFile(t, strings.TrimSuffix(stale, ".md")+".zh-CN.md", zhDoc("000000000000"))
	writeFile(t, filepath.Join(root, "docs/orphan.zh-CN.md"), zhDoc("000000000000"))
	writeFile(t, filepath.Join(root, "docs/sec/index.md"), "---\ntitle: Sec\norder: 1\naudience: app\n---\n")
	dev := filepath.Join(root, "docs/dev.md")
	writeFile(t, dev, "---\nsummary: Dev\naudience: dev\n---\n")
	writeFile(t, filepath.Join(root, "docs/dev.zh-CN.md"), zhDoc(docHash(t, root, dev)))
	out, err := checkTranslations(t, root)
	if err == nil {
		t.Fatalf("check passed:\n%s", out)
	}
	for _, want := range []string{
		"docs/missing.md has no Chinese partner docs/missing.zh-CN.md",
		"docs/sub/stale.zh-CN.md: translatedFrom is 000000000000 but docs/sub/stale.md hashes to " + docHash(t, root, stale),
		"docs/sec/index.md has no Chinese partner docs/sec/index.zh-CN.md",
		"docs/orphan.zh-CN.md has no English file docs/orphan.md",
		"docs/dev.zh-CN.md: docs/dev.md is for audience dev and stays English only",
		"Update the Chinese in the same commit",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the output has no %q:\n%s", want, out)
		}
	}
}

// The normal listing shows each doc once, in English.
func TestDocsListSkipsChinesePartners(t *testing.T) {
	root := docsTree(t)
	en := filepath.Join(root, "docs/a.md")
	writeFile(t, en, appFront)
	writeFile(t, filepath.Join(root, "docs/a.zh-CN.md"), zhDoc(docHash(t, root, en)))
	out, err := exec.Command("bash", filepath.Join(root, "scripts", "docs-list.sh")).CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if strings.Contains(string(out), "zh-CN") || !strings.Contains(string(out), "docs/a.md - Something") {
		t.Errorf("listing:\n%s", out)
	}
}
