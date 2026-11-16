package main

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/liustack/tifa/internal/foreign"
)

// scanForeign reads the file names and route.go files of the app for idioms of other frameworks
// (see internal/foreign). It prints the warnings to warnings, unless that is nil, and returns the
// errors as lines.
func scanForeign(root string, warnings io.Writer) (errs []string, err error) {
	found, err := foreign.ScanTree(root)
	if err != nil {
		return nil, err
	}
	return report(found, warnings), nil
}

// report prints the warnings among the findings to warnings, when it is not nil, and returns the
// lines of the errors.
func report(found []foreign.Finding, warnings io.Writer) (errs []string) {
	for _, f := range found {
		switch {
		case !f.Warning:
			errs = append(errs, f.String())
		case warnings != nil:
			fmt.Fprintln(warnings, f)
		}
	}
	return errs
}

// checkForeign fails an app with pages where its TypeScript uses an import, a directive, an export
// or a call that belongs to another framework (see internal/foreign). read reads the files, such as
// bundle.Node.Foreign. Warnings go to warnings, unless it is nil. An app without pages has no
// TypeScript to read, and Node is not started for it.
func checkForeign(root string, pages bool, warnings io.Writer, read func([]string) (map[string]*foreign.Facts, error)) error {
	if !pages {
		return nil
	}
	files, err := foreign.SourceFiles(root)
	if err != nil || len(files) == 0 {
		return err
	}
	facts, err := read(files)
	if err != nil {
		return err
	}
	var found []foreign.Finding
	for _, file := range files {
		f := facts[file]
		if f == nil {
			continue
		}
		rel, err := filepath.Rel(root, file)
		if err != nil {
			rel = file
		}
		found = append(found, foreign.CheckFacts(filepath.ToSlash(rel), f)...)
	}
	if errs := report(found, warnings); len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "\n"))
	}
	return nil
}

// hasPages says whether the generated files hold a browser entry, which only an app with pages has.
func hasPages(files map[string][]byte) bool {
	_, pages := files["client.ts"]
	return pages
}
