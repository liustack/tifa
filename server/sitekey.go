package server

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"hash"
	"strconv"

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
//     index, and the language of the request
//   - the build ID, which changes the Markdown under tifa dev
//
// What the build and the process fix is not in it: the routes, site.url and the languages of the
// site. A page key does not hold them either.
func siteKey(build, file, language string, sm tifa.Sitemap) string {
	k := siteHash{sha256.New()}
	k.str("site")
	k.str(build)
	k.str(file)
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
			k.n(int(p.Modified.UnixNano()))
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
	return `"` + hex.EncodeToString(k.Sum(nil)[:16]) + `"`
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
