package server

import (
	"strings"
	"testing"
)

func TestHTMLCacheEvictsLeastRecentlyUsedByBytes(t *testing.T) {
	c := newHTMLCache(10)
	c.put("a", cachedPage{html: "1234"})
	c.put("b", cachedPage{html: "1234"})
	if _, ok := c.get("a"); !ok { // a is now the most recent
		t.Fatal("a missing")
	}
	c.put("c", cachedPage{html: "1234"}) // 12 bytes, b goes
	if _, ok := c.get("b"); ok {
		t.Error("b should be evicted")
	}
	for _, k := range []string{"a", "c"} {
		if _, ok := c.get(k); !ok {
			t.Errorf("%s missing", k)
		}
	}
	c.put("big", cachedPage{html: "12345678901"}) // larger than the cache, not kept
	if _, ok := c.get("big"); ok {
		t.Error("entry larger than the cache was kept")
	}
	c.put("a", cachedPage{html: "123456"}) // replace, 6+4 = 10 fits
	if v, _ := c.get("a"); v.html != "123456" {
		t.Errorf("a = %q", v.html)
	}
	if _, ok := c.get("c"); !ok {
		t.Error("c evicted although the cache fits")
	}
}

func TestHTMLCacheZeroSizeKeepsNothing(t *testing.T) {
	c := newHTMLCache(0)
	c.put("a", cachedPage{html: "x"})
	if _, ok := c.get("a"); ok {
		t.Error("zero-size cache kept an entry")
	}
}

func TestHTMLCacheCountsTheCompressedForms(t *testing.T) {
	c := newHTMLCache(10)
	c.put("a", cachedPage{html: "1234", gzip: []byte("123"), zstd: []byte("123")})
	if _, ok := c.get("a"); !ok {
		t.Fatal("a page of 10 bytes in all was not kept")
	}
	c.put("b", cachedPage{html: "1"})
	if _, ok := c.get("a"); ok {
		t.Error("11 bytes kept in a cache of 10")
	}
}

func TestHTMLCacheKeepsTheHTMLWhenItsFormsDoNotFit(t *testing.T) {
	c := newHTMLCache(10)
	c.put("a", cachedPage{html: "123456", gzip: []byte("123"), zstd: []byte("123")})
	if p, ok := c.get("a"); !ok || p.html != "123456" || p.gzip != nil || p.zstd != nil || c.used != 6 {
		t.Errorf("kept %+v, %d bytes used", p, c.used)
	}
}

// The Content-Security-Policy of a page counts toward the size of the cache, as page.size says, and
// is given back when the page leaves.
func TestHTMLCacheChargesThePolicyOfAPage(t *testing.T) {
	c := newHTMLCache(100)
	page := cachedPage{html: "1234", policy: "abcdef"}
	c.put("a", page)
	if c.used != page.size() || c.used != 10 {
		t.Errorf("%d used, the page says %d, want 10", c.used, page.size())
	}
	c.put("b", cachedPage{html: "12", policy: strings.Repeat("p", 95)})
	if _, ok := c.get("a"); ok || c.used != 97 {
		t.Errorf("a stayed with %d used of 100, want 97 with a evicted", c.used)
	}
	c.removeLocked("b")
	if c.used != 0 {
		t.Errorf("%d used after the page left", c.used)
	}
}
