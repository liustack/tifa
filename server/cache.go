package server

import (
	"container/list"
	"sync"
)

// htmlCache keeps rendered pages of Cache = true routes by the hash of their render input, up
// to size bytes of HTML. It drops the least recently used pages first.
type htmlCache struct {
	mu    sync.Mutex
	size  int64
	used  int64
	order *list.List // of *cached, most recently used first
	pages map[string]*list.Element
}

type cached struct {
	key  string
	html string
}

func newHTMLCache(size int64) *htmlCache {
	return &htmlCache{size: size, order: list.New(), pages: map[string]*list.Element{}}
}

func (c *htmlCache) get(key string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.pages[key]
	if !ok {
		return "", false
	}
	c.order.MoveToFront(e)
	return e.Value.(*cached).html, true
}

// put stores a page. A page larger than the whole cache is not kept.
func (c *htmlCache) put(key, html string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.pages[key]; ok {
		c.used -= int64(len(e.Value.(*cached).html))
		c.order.Remove(e)
		delete(c.pages, key)
	}
	if int64(len(html)) > c.size {
		return
	}
	c.pages[key] = c.order.PushFront(&cached{key: key, html: html})
	c.used += int64(len(html))
	for c.used > c.size {
		oldest := c.order.Back()
		page := oldest.Value.(*cached)
		c.order.Remove(oldest)
		delete(c.pages, page.key)
		c.used -= int64(len(page.html))
	}
}
