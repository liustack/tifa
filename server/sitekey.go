package server

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"hash"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/internal/content"
)

// siteKey is the quoted hash of everything the answer to a request for a file of the site is made
// from, which is the ETag of the answer and its key in the render memo, as renderKey is for a page.
// The input is:
//   - what the function of SetSitemap returned: every field of tifa.Sitemap and of each
//     tifa.SitemapPage. A content entry counts by its identity in the build (collection, language,
//     ID and file), since its Markdown is in the build and does not change within one.
//     TestSiteKeyCoversEveryFieldOfTheSitemap fails for a field the key does not hold.
//   - the file asked for: its escaped path, which names the page of a .md and the part of a sitemap
//     index, and the language of the request (the list key is all of it but the path)
//   - the build ID, which changes the Markdown of a build, and siteFixed (siteBuild). Under tifa dev
//     the build ID leaves the content out, and the file of a body, which is in the entry, tells.
//
// What the process fixes comes in through siteBuild: site.url, the routes and the languages, which a
// deploy can change. A page holds them in its input (Alternates), and the files hold a hash of them.
func siteKey(build, file, language string, sm tifa.Sitemap) string {
	return siteFileKey(siteListKey(build, language, sm), file)
}

// etagListed is etagMatches without *, which only stands for a file that exists.
func etagListed(header, etag string) bool {
	for _, candidate := range strings.Split(header, ",") {
		if strings.TrimSpace(candidate) != "*" && etagMatches(candidate, etag) {
			return true
		}
	}
	return false
}

// siteBuild is what of the build and the process the site files depend on, besides what the
// function returns: the ID of the front end, when the server has one, and siteFixed.
func (s *server) siteBuild() string {
	id := ""
	if f := s.front.Load(); f != nil {
		id = f.build.ID
	}
	return id + "\x00" + s.siteFixed()
}

// siteFixed is the hash of what the process fixes and a deploy can change, and the files depend on:
// site.url, the languages and the routes. A page holds the same in its input (the site, the default
// language and the versions of the page in Alternates). It is made once, on the first request.
func (s *server) siteFixed() string {
	s.fixedOnce.Do(func() {
		k := siteHash{sha256.New()}
		k.str(s.site)
		k.bool(s.sitemap != nil)
		k.n(sitemapURLs)
		k.n(sitemapBytes)
		if s.i18n == nil {
			k.str("no languages")
		} else {
			k.str(s.i18n.Default)
			k.bool(s.i18n.Always)
			k.n(len(s.i18n.Languages))
			for _, l := range s.i18n.Languages {
				k.str(l)
			}
		}
		patterns := slices.Sorted(maps.Keys(s.routes))
		k.n(len(patterns))
		for _, p := range patterns {
			k.str(p)
			k.bool(s.pages[p])
			methods := slices.Sorted(maps.Keys(s.routes[p]))
			k.n(len(methods))
			for _, m := range methods {
				k.str(m)
			}
		}
		pages := slices.Sorted(maps.Keys(s.pages))
		k.n(len(pages))
		for _, p := range pages {
			k.str(p)
			k.bool(s.pages[p])
			k.n(len(s.langs[p]))
			for _, l := range s.langs[p] {
				k.str(l)
			}
		}
		s.fixedKey = hex.EncodeToString(k.Sum(nil)[:16])
	})
	return s.fixedKey
}

// siteFileKey is the key of the file at the escaped path file for the list that listKey stands for.
func siteFileKey(listKey, file string) string {
	k := siteHash{sha256.New()}
	k.str("site")
	k.str(listKey)
	k.str(file)
	return `"` + hex.EncodeToString(k.Sum(nil)[:16]) + `"`
}

// siteListKey is the hash of the part of the input that decides which pages the list has and what
// they say: everything of siteKey but the file. The pages that the checks (sitePages) make of a
// list are the same for every file of it, so they are kept by this key (listedPages).
func siteListKey(build, language string, sm tifa.Sitemap) string {
	k := siteHash{sha256.New()}
	k.str("list")
	k.str(build)
	k.str(language)
	k.str(sm.Name)
	k.str(sm.Summary)
	k.n(len(sm.Exclude))
	for _, e := range sm.Exclude {
		k.str(e)
	}
	k.n(len(sm.Pages))
	for _, p := range sm.Pages {
		k.str(p.Language)
		k.str(p.Path)
		k.bool(p.Modified.IsZero())
		if !p.Modified.IsZero() {
			// Seconds and nanoseconds apart: UnixNano overflows for a time far from 1970.
			k.n(int(p.Modified.Unix()))
			k.n(p.Modified.Nanosecond())
		}
		k.str(p.Title)
		k.str(p.Description)
		switch {
		case p.Content == nil:
			k.str("no entry")
		case content.BodyOf(p.Content) == nil:
			k.str("an entry of no collection")
		default:
			b := content.BodyOf(p.Content)
			k.str("entry")
			k.str(b.Collection)
			k.str(b.Language)
			k.str(b.ID)
			k.str(b.File)
		}
		k.n(len(p.Alternates))
		for _, a := range p.Alternates {
			k.str(a.Language)
			k.str(a.Path)
		}
	}
	return hex.EncodeToString(k.Sum(nil)[:16])
}

// siteHash writes values to a hash so that two lists of values never write the same bytes:
// a string comes with its length.
type siteHash struct{ hash.Hash }

func (h siteHash) n(n int) { h.Write(binary.AppendVarint(nil, int64(n))) }

func (h siteHash) str(s string) {
	h.n(len(s))
	hashString(h, s)
}

func (h siteHash) bool(b bool) { h.str(strconv.FormatBool(b)) }
