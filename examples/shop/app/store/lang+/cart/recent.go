package cart

import (
	"container/list"
	"sync"
	"time"
)

// recent holds values by key in memory, at most limit of them, and forgets a value that has not
// been used for longer than idle. When it is full, the least recently used value goes first. It
// stands in for a table with a cleanup job: a plain map would grow with every visitor and never
// shrink. Callers hold mu around every call.
type recent[V any] struct {
	mu    sync.Mutex
	limit int
	idle  time.Duration
	items map[string]*list.Element
	order *list.List // of *entry[V], the most recently used in front
}

type entry[V any] struct {
	key   string
	value V
	used  time.Time
}

func newRecent[V any](limit int, idle time.Duration) *recent[V] {
	return &recent[V]{limit: limit, idle: idle, items: map[string]*list.Element{}, order: list.New()}
}

// get returns the value of key and counts it as used at now. A value idle for too long is
// forgotten and not returned.
func (r *recent[V]) get(key string, now time.Time) (V, bool) {
	el, ok := r.items[key]
	if !ok {
		var zero V
		return zero, false
	}
	e := el.Value.(*entry[V])
	if now.Sub(e.used) > r.idle {
		r.remove(el)
		var zero V
		return zero, false
	}
	e.used = now
	r.order.MoveToFront(el)
	return e.value, true
}

// has reports whether key holds a value, without counting it as used.
func (r *recent[V]) has(key string) bool {
	_, ok := r.items[key]
	return ok
}

// add stores value under key as used at now. It then forgets the values idle for too long, and
// the least recently used ones past the limit.
func (r *recent[V]) add(key string, value V, now time.Time) {
	if el, ok := r.items[key]; ok {
		r.remove(el)
	}
	r.items[key] = r.order.PushFront(&entry[V]{key: key, value: value, used: now})
	for back := r.order.Back(); back != nil; back = r.order.Back() {
		if r.order.Len() <= r.limit && now.Sub(back.Value.(*entry[V]).used) <= r.idle {
			break
		}
		r.remove(back)
	}
}

func (r *recent[V]) len() int { return r.order.Len() }

func (r *recent[V]) remove(el *list.Element) {
	delete(r.items, el.Value.(*entry[V]).key)
	r.order.Remove(el)
}
