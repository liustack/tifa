package core

import (
	"container/heap"
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// SessionStore keeps the records of sessions by key. See tifa.SessionStore.
type SessionStore interface {
	Get(ctx context.Context, token string) (data []byte, found bool, err error)
	Put(ctx context.Context, token string, data []byte, expiry time.Time) error
	Delete(ctx context.Context, token string) error
}

// entryOverhead is what an entry of the memory store costs besides its key and data: the entry,
// its map slot and its place in the heap.
const entryOverhead = 128

// sweepEvery is how often the memory store removes expired records, and sweepBatch how many it
// removes under one hold of the lock.
const (
	sweepEvery = time.Minute
	sweepBatch = 1024
)

// MemoryStore is a SessionStore that keeps records in the memory of the process, up to a byte limit.
// A full store drops the records that expire first. A restart loses every record.
type MemoryStore struct {
	max int64
	now func() time.Time

	mu      sync.Mutex
	used    int64
	entries map[string]*memEntry
	byExp   expiryHeap
	closed  bool
	sweeper sync.Once
	stop    chan struct{}
}

type memEntry struct {
	key    string
	data   []byte
	expiry time.Time
	cost   int64
	index  int // position in the heap
}

// expiryHeap is a min-heap on expiry, so the record that expires first is at the top.
type expiryHeap []*memEntry

func (h expiryHeap) Len() int           { return len(h) }
func (h expiryHeap) Less(i, j int) bool { return h[i].expiry.Before(h[j].expiry) }
func (h expiryHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index, h[j].index = i, j
}
func (h *expiryHeap) Push(x any) {
	e := x.(*memEntry)
	e.index = len(*h)
	*h = append(*h, e)
}
func (h *expiryHeap) Pop() any {
	old := *h
	e := old[len(old)-1]
	old[len(old)-1] = nil
	*h = old[:len(old)-1]
	e.index = -1
	return e
}

// NewMemoryStore returns a store that holds at most maxBytes bytes, counting for each record its
// key, its data and a fixed cost. It panics when maxBytes is not positive.
func NewMemoryStore(maxBytes int64) *MemoryStore {
	if maxBytes <= 0 {
		panic(fmt.Sprintf("tifa: NewMemoryStore needs a positive size, got %d", maxBytes))
	}
	return &MemoryStore{max: maxBytes, now: time.Now, entries: map[string]*memEntry{}, stop: make(chan struct{})}
}

// Close stops the goroutine that removes expired records. The store keeps working without it, as
// Get never returns an expired record. Close can be called more than once.
func (m *MemoryStore) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.closed {
		m.closed = true
		close(m.stop)
	}
}

func (m *MemoryStore) Get(ctx context.Context, token string) ([]byte, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.entries[token]
	if e == nil {
		return nil, false, nil
	}
	if !m.now().Before(e.expiry) {
		m.remove(e)
		return nil, false, nil
	}
	return append([]byte(nil), e.data...), true, nil
}

// ErrSessionRecordTooLarge is what Put returns for a record that alone costs more than the store may hold.
var ErrSessionRecordTooLarge = errors.New("tifa: session record is larger than the memory store's limit")

func (m *MemoryStore) Put(ctx context.Context, token string, data []byte, expiry time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	cost := int64(len(token)+len(data)) + entryOverhead
	if cost > m.max {
		return ErrSessionRecordTooLarge
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if old := m.entries[token]; old != nil {
		m.remove(old)
	}
	if !m.now().Before(expiry) {
		return nil // already expired: Get would not return it
	}
	for m.used+cost > m.max {
		m.remove(m.byExp[0]) // not empty: cost alone fits, so something is using the rest
	}
	e := &memEntry{key: token, data: append([]byte(nil), data...), expiry: expiry, cost: cost}
	m.entries[token] = e
	heap.Push(&m.byExp, e)
	m.used += cost
	m.sweeper.Do(func() { go m.sweep() })
	return nil
}

func (m *MemoryStore) Delete(ctx context.Context, token string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if e := m.entries[token]; e != nil {
		m.remove(e)
	}
	return nil
}

// remove drops e from the map and the heap. The caller holds the lock.
func (m *MemoryStore) remove(e *memEntry) {
	heap.Remove(&m.byExp, e.index)
	delete(m.entries, e.key)
	m.used -= e.cost
}

// sweep removes the expired records once a minute, up to sweepBatch under one hold of the lock so a
// large backlog does not stop requests.
func (m *MemoryStore) sweep() {
	t := time.NewTicker(sweepEvery)
	defer t.Stop()
	for {
		select {
		case <-m.stop:
			return
		case <-t.C:
			for m.sweepBatch() == sweepBatch {
			}
		}
	}
}

// sweepBatch removes up to sweepBatch expired records and returns how many it removed.
func (m *MemoryStore) sweepBatch() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	n := 0
	for n < sweepBatch && len(m.byExp) > 0 && !now.Before(m.byExp[0].expiry) {
		m.remove(m.byExp[0])
		n++
	}
	return n
}
