package server

import (
	"fmt"
	"net/http"
	"slices"
	"strings"

	"encoding/json/jsontext"
	"github.com/liustack/tifa"
	"github.com/liustack/tifa/internal/core"
)

// The versions of a page in other languages. A page of an area exists in each language of the
// area at the same path under another prefix, unless a Load says otherwise with
// c.SetAlternates. The list goes with the data of the page: into the input of a render, into the
// boot data of a page without SSR and into the answer to a client navigation. The render writes the
// hreflang links from it, and the browser's useAlternates reads it for the language switch.

// pageAlternates is the list as the page carries it.
type pageAlternates struct {
	// Site is site.url without a trailing slash, left out of the answer to a navigation, which the
	// browser sends to the site it already knows.
	Site string `json:"site,omitempty"`
	// Canonical is the path of the page as it was asked for, without the query.
	Canonical string `json:"canonical"`
	// List holds a link per version, the page itself included.
	List []alternateLink `json:"list"`
}

type alternateLink struct {
	Language string `json:"language"`
	// URL is the path of the version, with its language prefix.
	URL string `json:"url"`
}

// languagePath returns the path of rest, a path without a language prefix, in language.
func (s *server) languagePath(language, rest string) string {
	if language == s.i18n.Default && !s.i18n.Always {
		return rest
	}
	if rest == "/" {
		return "/" + language
	}
	return "/" + language + rest
}

// alternatesOf works out the versions of the page c answers, once its Loads have returned. It
// returns nil for a page with fewer than two: a site without languages, a page outside every area
// and a page whose list has one version.
func (s *server) alternatesOf(c *tifa.Ctx) (*pageAlternates, error) {
	if s.i18n == nil {
		return nil, nil
	}
	langs := s.langs[c.Pattern()]
	if len(langs) == 0 {
		return nil, nil
	}
	language, rest := c.Language(), c.Request().URL.EscapedPath()
	if prefix := "/" + language; rest == prefix || strings.HasPrefix(rest, prefix+"/") {
		if rest = strings.TrimPrefix(rest, prefix); rest == "" {
			rest = "/"
		}
	}
	var list []alternateLink
	if given, set := core.AlternatesOf(c); set {
		seen := map[string]bool{}
		for _, a := range given {
			switch {
			case !slices.Contains(langs, a.Language):
				return nil, fmt.Errorf("tifa: SetAlternates: language %q is none of the languages of this page %v", a.Language, langs)
			case seen[a.Language]:
				return nil, fmt.Errorf("tifa: SetAlternates: language %q is listed twice", a.Language)
			case !strings.HasPrefix(a.Path, "/") || strings.HasPrefix(a.Path, "//") || strings.ContainsAny(a.Path, "?#<>\"\\ "):
				return nil, fmt.Errorf("tifa: SetAlternates: path %q for %s must be an escaped URL path that starts with one slash, with no query or fragment", a.Path, a.Language)
			}
			seen[a.Language] = true
			list = append(list, alternateLink{Language: a.Language, URL: s.languagePath(a.Language, a.Path)})
		}
	} else {
		for _, l := range langs {
			list = append(list, alternateLink{Language: l, URL: s.languagePath(l, rest)})
		}
	}
	if len(list) < 2 {
		return nil, nil
	}
	return &pageAlternates{Site: strings.TrimRight(s.i18n.SiteURL, "/"), Canonical: s.languagePath(language, rest), List: list}, nil
}

// alternatesJSON is the list as JSON a script element can hold, nil when the page has none.
func alternatesJSON(a *pageAlternates) (jsontext.Value, error) {
	if a == nil {
		return nil, nil
	}
	return htmlJSON(a)
}

// navigationJSON is the list as a data answer carries it, without the site. A page without versions
// says so with an empty list, so the browser drops the list it held for the path. A site without
// languages carries none.
func (s *server) navigationJSON(a *pageAlternates) (jsontext.Value, error) {
	switch {
	case s.i18n == nil:
		return nil, nil
	case a == nil:
		return jsontext.Value(`{"list":[]}`), nil
	}
	b := *a
	b.Site = ""
	return htmlJSON(&b)
}

// pageAlternatesJSON is alternatesJSON for the page c answers with status: only a page that was
// found has versions, a 404 and an error page have none.
func (s *server) pageAlternatesJSON(c *tifa.Ctx, status int) (jsontext.Value, error) {
	if status != http.StatusOK {
		return nil, nil
	}
	a, err := s.alternatesOf(c)
	if err != nil {
		return nil, err
	}
	return alternatesJSON(a)
}

// navigationAlternates is the list a data request answers with. It goes out when a Load could have
// set it: the request runs the Load of the page, the last of the chain. A Load the browser still
// holds the data of may have set a list the answer does not know, and the browser keeps the list it
// has for that path then.
func (s *server) navigationAlternates(c *tifa.Ctx, chain, selected []load) (jsontext.Value, error) {
	if len(chain) == 0 || !slices.ContainsFunc(selected, func(l load) bool { return l.id == chain[len(chain)-1].id }) {
		return nil, nil
	}
	a, err := s.alternatesOf(c)
	if err != nil {
		return nil, err
	}
	return s.navigationJSON(a)
}
