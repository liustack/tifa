package server

import (
	"fmt"
	"strings"
	"sync"
	"testing"
)

// slruPage returns a page of n bytes.
func slruPage(n int) cachedPage { return cachedPage{html: strings.Repeat("x", n)} }

// checkMemo fails when the counters of the memo do not match the pages in its two lists.
func checkMemo(t *testing.T, c *renderMemo) {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	var used, prot int64
	n := 0
	for e := c.probation.Front(); e != nil; e = e.Next() {
		v := e.Value.(*cached)
		if v.prot || c.pages[v.key] != e {
			t.Fatalf("%s in probation is wrong: prot %v", v.key, v.prot)
		}
		used += v.page.size()
		n++
	}
	for e := c.protected.Front(); e != nil; e = e.Next() {
		v := e.Value.(*cached)
		if !v.prot || c.pages[v.key] != e {
			t.Fatalf("%s in protected is wrong: prot %v", v.key, v.prot)
		}
		used += v.page.size()
		prot += v.page.size()
		n++
	}
	if used != c.used || prot != c.protUsed || n != c.entries || n != len(c.pages) {
		t.Fatalf("lists hold %d bytes (%d protected) in %d pages, counters say %d (%d), %d, map %d", used, prot, n, c.used, c.protUsed, c.entries, len(c.pages))
	}
	if c.used > c.size || c.protUsed > c.protCap {
		t.Fatalf("used %d of %d, protected %d of %d", c.used, c.size, c.protUsed, c.protCap)
	}
}

// A run of pages that come once does not push out the pages that are asked for again and again.
func TestTheMemoKeepsHotPagesAgainstAFlood(t *testing.T) {
	c := newRenderMemo(1000)
	hot := []string{"home", "shop", "about"}
	for _, k := range hot {
		c.put(k, slruPage(50))
		c.get(k) // the second request: protected
	}
	for i := 0; i < 5000; i++ {
		c.put(fmt.Sprintf("once-%d", i), slruPage(50))
		if i%100 == 0 {
			for _, k := range hot {
				c.get(k)
			}
		}
	}
	for _, k := range hot {
		if _, ok := c.get(k); !ok {
			t.Errorf("%s was pushed out by pages that came once", k)
		}
	}
	checkMemo(t, c)
}

// A page that came once is stored like any other: its second request hits.
func TestANewPageIsStoredAndHitOnItsSecondRequest(t *testing.T) {
	c := newRenderMemo(1000)
	for i := 0; i < 200; i++ { // far over the size, all new
		k := fmt.Sprintf("p%d", i)
		c.put(k, slruPage(50))
		if _, ok := c.get(k); !ok {
			t.Fatalf("%s missed on its second request", k)
		}
		checkMemo(t, c)
	}
}

func TestAHitInProbationPromotesAndAFullProtectedDemotes(t *testing.T) {
	c := newRenderMemo(100) // protected holds 80
	for _, k := range []string{"a", "b", "c"} {
		c.put(k, slruPage(30))
	}
	where := func(k string) string {
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.pages[k] == nil {
			return "none"
		}
		if c.pages[k].Value.(*cached).prot {
			return "protected"
		}
		return "probation"
	}
	c.get("a")
	c.get("b")
	if where("a") != "protected" || where("b") != "protected" || where("c") != "probation" {
		t.Fatalf("a %s, b %s, c %s", where("a"), where("b"), where("c"))
	}
	c.get("c") // 90 > 80: a, the least recently used of protected, goes back to probation
	if where("a") != "probation" || where("b") != "protected" || where("c") != "protected" {
		t.Fatalf("a %s, b %s, c %s", where("a"), where("b"), where("c"))
	}
	c.mu.Lock()
	if c.probation.Front().Value.(*cached).key != "a" {
		t.Error("the demoted page is not at the head of probation")
	}
	c.mu.Unlock()
	if s, _ := c.stats(); s.Entries != 3 || s.Evictions != 0 {
		t.Errorf("a demotion changed the stats: %+v", s)
	}
	checkMemo(t, c)
	// Only probation drops: d (30) needs room, a is the tail of probation.
	c.put("d", slruPage(30))
	if where("a") != "none" || where("b") != "protected" || where("d") != "probation" {
		t.Errorf("a %s, b %s, d %s", where("a"), where("b"), where("d"))
	}
	if s, used := c.stats(); s.Evictions != 1 || s.Entries != 3 || used != 90 {
		t.Errorf("stats %+v, %d bytes", s, used)
	}
	checkMemo(t, c)
}

// A page larger than the protected share stays in probation, and a hit makes it the most recent
// there. A new page that fits the memo is kept even when protected is full.
func TestAPageLargerThanTheProtectedShare(t *testing.T) {
	c := newRenderMemo(100)
	c.put("big", slruPage(90))
	c.get("big")
	c.mu.Lock()
	if c.protected.Len() != 0 || c.protUsed != 0 {
		t.Fatal("a page over the protected share was promoted")
	}
	c.mu.Unlock()
	c.put("s", slruPage(5))
	c.get("big") // the most recent of probation again, so s goes first
	c.put("t", slruPage(10))
	if _, ok := c.get("big"); !ok {
		t.Error("big was dropped before the pages after it")
	}
	if _, ok := c.get("s"); ok {
		t.Error("s should have gone first")
	}
	checkMemo(t, c)

	c = newRenderMemo(100)
	for _, k := range []string{"a", "b"} {
		c.put(k, slruPage(40))
		c.get(k) // 80 protected
	}
	c.put("n", slruPage(60)) // 140: probation holds n alone, so the protected page least recently used goes
	if _, ok := c.get("n"); !ok {
		t.Error("a new page that fits the memo was not kept")
	}
	if _, ok := c.get("a"); ok {
		t.Error("a should have been dropped for n")
	}
	checkMemo(t, c)
}

func TestAReplacedPageKeepsItsPlace(t *testing.T) {
	c := newRenderMemo(100)
	c.put("a", slruPage(20))
	c.get("a")
	c.put("a", slruPage(30))
	c.mu.Lock()
	ok := c.pages["a"].Value.(*cached).prot
	c.mu.Unlock()
	if !ok || c.used != 30 || c.protUsed != 30 {
		t.Errorf("protected %v, used %d, protected bytes %d", ok, c.used, c.protUsed)
	}
	c.put("a", slruPage(90)) // over the protected share: probation
	checkMemo(t, c)
}

// Promotion and demotion with many goroutines, for go test -race. The lists and counters stay right.
func TestTheMemoUnderConcurrentReadsAndWrites(t *testing.T) {
	c := newRenderMemo(2000)
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 3000; i++ {
				k := fmt.Sprintf("k%d", (i*7+g)%90)
				switch i % 4 {
				case 0:
					c.put(k, slruPage(20+i%60))
				case 1:
					c.get(k)
				case 2:
					c.removeFor(k)
				default:
					c.stats()
				}
			}
		}()
	}
	wg.Wait()
	checkMemo(t, c)
}

func (c *renderMemo) removeFor(key string) {
	c.mu.Lock()
	c.removeLocked(key)
	c.mu.Unlock()
}
