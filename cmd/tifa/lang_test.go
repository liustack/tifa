package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/liustack/tifa/internal/analyze"
	"github.com/liustack/tifa/internal/bundle"
)

func langManifest(languages bool) *analyze.Manifest {
	m := &analyze.Manifest{
		Module:  "example.com/x",
		Layouts: []analyze.Layout{{ID: "app/layout", Dir: "app", Path: "/", TSX: true, Routes: []string{"app/store"}}},
		Routes:  []analyze.Route{{ID: "app/store", Dir: "app/store", Path: "/store", Page: true, Layouts: []string{"app/layout"}}},
	}
	if languages {
		m.I18n = &analyze.I18n{Default: "zh-CN", Languages: []string{"zh-CN", "en-US"}}
	}
	return m
}

func TestBuildFailsWhereLangIsSetOnASiteWithLanguages(t *testing.T) {
	root := t.TempDir()
	layout, page := filepath.Join(root, "app", "layout.tsx"), filepath.Join(root, "app", "store", "page.tsx")
	found := map[string][]bundle.LangKey{layout: {{Line: 5, Column: 3}}, page: {{Line: 2, Column: 40}}}
	var asked []string
	read := func(files []string) (map[string][]bundle.LangKey, error) {
		asked = files
		return found, nil
	}
	err := checkLangAttribute(root, langManifest(true), read)
	if err == nil {
		t.Fatal("a lang in htmlAttributes passed on a site with languages")
	}
	for _, want := range []string{"app/layout.tsx:5:3: htmlAttributes sets lang", "app/store/page.tsx:2:40: htmlAttributes sets lang"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error lacks %q:\n%v", want, err)
		}
	}
	if len(asked) != 2 {
		t.Errorf("the check read %v, want the layout and the page", asked)
	}
	// A site without languages keeps writing its own lang, and reads nothing.
	asked = nil
	if err := checkLangAttribute(root, langManifest(false), read); err != nil || asked != nil {
		t.Errorf("a site without languages: %v, read %v", err, asked)
	}
	// Nothing found, nothing to say.
	if err := checkLangAttribute(root, langManifest(true), func([]string) (map[string][]bundle.LangKey, error) { return nil, nil }); err != nil {
		t.Errorf("no lang keys: %v", err)
	}
}
