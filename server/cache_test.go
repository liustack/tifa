package server

import "testing"

func TestHTMLCacheEvictsLeastRecentlyUsedByBytes(t *testing.T) {
	c := newHTMLCache(10)
	c.put("a", "1234")
	c.put("b", "1234")
	if _, ok := c.get("a"); !ok { // a is now the most recent
		t.Fatal("a missing")
	}
	c.put("c", "1234") // 12 bytes, b goes
	if _, ok := c.get("b"); ok {
		t.Error("b should be evicted")
	}
	for _, k := range []string{"a", "c"} {
		if _, ok := c.get(k); !ok {
			t.Errorf("%s missing", k)
		}
	}
	c.put("big", "12345678901") // larger than the cache, not kept
	if _, ok := c.get("big"); ok {
		t.Error("entry larger than the cache was kept")
	}
	c.put("a", "123456") // replace, 6+4 = 10 fits
	if v, _ := c.get("a"); v != "123456" {
		t.Errorf("a = %q", v)
	}
	if _, ok := c.get("c"); !ok {
		t.Error("c evicted although the cache fits")
	}
}

func TestHTMLCacheZeroSizeKeepsNothing(t *testing.T) {
	c := newHTMLCache(0)
	c.put("a", "x")
	if _, ok := c.get("a"); ok {
		t.Error("zero-size cache kept an entry")
	}
}
