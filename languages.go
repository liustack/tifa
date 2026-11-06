package tifa

import (
	"slices"
	"strings"
	"sync"

	"github.com/liustack/tifa/internal/langs"
)

// languagesFlag is set by tifa build and tifa dev with -ldflags -X as "list:" and the languages
// joined with commas, so that an app with no language (list:) is told from a program that was
// built some other way (empty).
var languagesFlag string

var (
	languagesOnce sync.Once
	languages     []string
)

// Languages returns the languages of the site, the default language first and then the others in
// byte order, nil for an app without languages. They are the names of the <language>.json files
// of every _locales folder under app/ (a draft does not count), and the default language is
// i18n.default of tifa.yaml. Use it where the app lists its languages, such as the Languages of a
// content collection:
//
//	var Journal = content.Define[Post](content.Source{Dir: "journal", Languages: tifa.Languages()})
//
// It works while the package initializes, so a package-level var may call it. tifa build and
// tifa dev build it into the program. Anywhere else, such as go test and go run, it reads those
// files and tifa.yaml in the module of the current folder, once.
func Languages() []string {
	languagesOnce.Do(func() { languages = readLanguages(languagesFlag, ".") })
	return slices.Clone(languages)
}

// readLanguages is Languages for the value of languagesFlag, reading the module that holds dir
// when the flag is not set.
func readLanguages(flag, dir string) []string {
	if list, ok := strings.CutPrefix(flag, "list:"); ok {
		if list == "" {
			return nil
		}
		return strings.Split(list, ",")
	}
	if root := langs.Module(dir); root != "" {
		return langs.Site(root)
	}
	return nil
}
