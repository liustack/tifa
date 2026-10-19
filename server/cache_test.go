package server

import "testing"

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
