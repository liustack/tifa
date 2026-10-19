package server

import (
	"container/list"
	"sync"
)

// htmlCache keeps rendered pages of Cache = true routes by the hash of their render input, up
// to size bytes of HTML and its compressed forms. It drops the least recently used pages first.
type htmlCache struct {
	mu    sync.Mutex
	size  int64
	used  int64
	order *list.List // of *cached, most recently used first
	pages map[string]*list.Element
}

type cached struct {
	key  string
	page cachedPage
}

// cachedPage is a rendered page as the cache keeps it: the HTML and, with server.compress, its
// gzip and zstd forms (compressPage), so a hit is not compressed again. compressed reports that
// the forms were made, also when they did not fit and only the HTML is kept.
type cachedPage struct {
	html       string
	gzip, zstd []byte
	compressed bool
}

func (p cachedPage) size() int64 { return int64(len(p.html) + len(p.gzip) + len(p.zstd)) }

func newHTMLCache(size int64) *htmlCache {
	return &htmlCache{size: size, order: list.New(), pages: map[string]*list.Element{}}
}

func (c *htmlCache) get(key string) (cachedPage, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.pages[key]
	if !ok {
		return cachedPage{}, false
	}
	c.order.MoveToFront(e)
	return e.Value.(*cached).page, true
}

// holds reports whether the cache can keep the HTML of a page. Pages it cannot hold are not
// compressed for it.
func (c *htmlCache) holds(html string) bool { return int64(len(html)) <= c.size }

// put stores a page. A page whose compressed forms do not fit in the whole cache is kept without
// them, and one whose HTML does not fit is not kept.
func (c *htmlCache) put(key string, page cachedPage) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.pages[key]; ok {
		c.used -= e.Value.(*cached).page.size()
		c.order.Remove(e)
		delete(c.pages, key)
	}
	if page.size() > c.size {
		page.gzip, page.zstd = nil, nil
	}
	if page.size() > c.size {
		return
	}
	c.pages[key] = c.order.PushFront(&cached{key: key, page: page})
	c.used += page.size()
	for c.used > c.size {
		oldest := c.order.Back()
		entry := oldest.Value.(*cached)
		c.order.Remove(oldest)
		delete(c.pages, entry.key)
		c.used -= entry.page.size()
	}
}
