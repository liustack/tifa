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

// A page that has its gzip form is kept as that alone, and charged for it.
func TestHTMLCacheKeepsTheGzipFormAlone(t *testing.T) {
	c := newHTMLCache(10)
	c.put("a", cachedPage{html: "123456789012", gzip: []byte("1234"), n: 12})
	p, ok := c.get("a")
	if !ok || p.html != "" || string(p.gzip) != "1234" || p.n != 12 || c.used != 4 {
		t.Fatalf("kept %+v, %d bytes used", p, c.used)
	}
	c.put("b", cachedPage{html: "123456789012", gzip: []byte("1234567"), n: 12})
	if _, ok := c.get("a"); ok || c.used != 7 {
		t.Errorf("11 bytes kept in a cache of 10, %d used", c.used)
	}
	c.put("c", cachedPage{html: "1", gzip: []byte("12345678901"), n: 1})
	if _, ok := c.get("c"); ok {
		t.Error("a gzip form larger than the cache was kept")
	}
}
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
