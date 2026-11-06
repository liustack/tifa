package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/liustack/tifa/internal/analyze"
	"github.com/liustack/tifa/internal/bundle"
	"github.com/liustack/tifa/internal/codegen"
)

// checkLangAttribute fails a build of a site with languages where a component file sets lang in the
// htmlAttributes it writes out: tifa writes the lang of <html> as the language of the page. What a
// static read cannot see, such as a name or a spread, fails the render instead (htmlAttributesOf in
// js/runtime/head.ts). langKeys reads the files, such as bundle.Node.LangKeys.
func checkLangAttribute(root string, m *analyze.Manifest, langKeys func([]string) (map[string][]bundle.LangKey, error)) error {
	if m.I18n == nil {
		return nil
	}
	files := codegen.ComponentFiles(root, m)
	if len(files) == 0 {
		return nil
	}
	found, err := langKeys(files)
	if err != nil {
		return err
	}
	var lines []string
	for _, file := range files {
		for _, k := range found[file] {
			rel, err := filepath.Rel(root, file)
			if err != nil {
				rel = file
			}
			lines = append(lines, fmt.Sprintf("%s:%d:%d: htmlAttributes sets lang, but this site has languages: tifa writes the lang of <html> as the language of the page, remove it", filepath.ToSlash(rel), k.Line, k.Column))
		}
	}
	if len(lines) > 0 {
		return fmt.Errorf("%s", strings.Join(lines, "\n"))
	}
	return nil
}
