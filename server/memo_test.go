package server

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestHTMLCacheEvictsLeastRecentlyUsedByBytes(t *testing.T) {
	c := newRenderMemo(10)
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
	c := newRenderMemo(0)
	c.put("a", cachedPage{html: "x"})
	if _, ok := c.get("a"); ok {
		t.Error("zero-size cache kept an entry")
	}
}

// A page that has its gzip form is kept as that alone, and charged for it.
func TestHTMLCacheKeepsTheGzipFormAlone(t *testing.T) {
	c := newRenderMemo(10)
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
	c := newRenderMemo(100)
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

// Stats count the pages the memo dropped to make room for another, and not a page it replaced with
// a newer render of its key or one too large to keep.
// The memo remembers at most failCount failed renders, whose pages come to at most failBytes, and
// drops the oldest first. A page larger than that is not remembered at all.
func TestRememberedFailuresAreBounded(t *testing.T) {
	c := newRenderMemo(0)
	failed := errors.New("ssr: render page: Error: the page threw")
	remember := func(key string, page cachedPage, err error) {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.rememberLocked(key, page, err)
	}
	has := func(key string) bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		_, ok := c.failed[key]
		return ok
	}
	for i := range failCount + 3 {
		remember(fmt.Sprint(i), cachedPage{}, failed)
	}
	if len(c.failed) != failCount || has("2") || !has("3") || !has(fmt.Sprint(failCount+2)) {
		t.Fatalf("%d remembered, 2 kept %v, 3 kept %v", len(c.failed), has("2"), has("3"))
	}

	c = newRenderMemo(0)
	half := cachedPage{html: strings.Repeat("x", failBytes/2)}
	remember("a", half, nil)
	remember("b", half, nil)
	remember("c", cachedPage{html: "x"}, nil) // a goes to make room
	remember("big", cachedPage{html: strings.Repeat("x", failBytes+1)}, nil)
	if has("a") || !has("b") || !has("c") || has("big") || c.failUsed != failBytes/2+1 {
		t.Errorf("a %v, b %v, c %v, big %v, %d bytes", has("a"), has("b"), has("c"), has("big"), c.failUsed)
	}
}

func TestTheMemoCountsThePagesItDrops(t *testing.T) {
	c := newRenderMemo(10)
	c.put("a", cachedPage{html: "1234"})
	c.put("b", cachedPage{html: "1234"})
	c.put("a", cachedPage{html: "123"})           // a newer render of a: replaced, not dropped
	c.put("big", cachedPage{html: "12345678901"}) // never kept
	if s, _ := c.stats(); s.Evictions != 0 {
		t.Fatalf("%d evictions before the memo was full", s.Evictions)
	}
	c.put("c", cachedPage{html: "1234"})     // 11 bytes: b goes
	c.put("d", cachedPage{html: "12345678"}) // a and c go
	if s, _ := c.stats(); s.Evictions != 3 || s.Entries != 1 {
		t.Errorf("render memo %+v, want 3 evictions and 1 entry", s)
	}
}
