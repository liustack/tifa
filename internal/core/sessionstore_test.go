package core

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeNow struct {
	mu sync.Mutex
	t  time.Time
}

func (f *fakeNow) now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.t
}

func (f *fakeNow) advance(d time.Duration) {
	f.mu.Lock()
	f.t = f.t.Add(d)
	f.mu.Unlock()
}

func newTestStore(t *testing.T, max int64) (*MemoryStore, *fakeNow) {
	t.Helper()
	m := NewMemoryStore(max)
	t.Cleanup(m.Close)
	clock := &fakeNow{t: time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)}
	m.now = clock.now
	return m, clock
}

// consistent fails when the heap, the map and the byte count disagree.
func consistent(t *testing.T, m *MemoryStore) {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.byExp) != len(m.entries) {
		t.Fatalf("heap holds %d nodes for %d entries", len(m.byExp), len(m.entries))
	}
	var used int64
	for i, e := range m.byExp {
		if e.index != i || m.entries[e.key] != e {
			t.Fatalf("heap node %d of %q is out of place", i, e.key)
		}
		used += e.cost
	}
	if used != m.used || used > m.max {
		t.Fatalf("used %d, counted %d, max %d", m.used, used, m.max)
	}
}

func TestMemoryStoreBasics(t *testing.T) {
	m, clock := newTestStore(t, 1<<20)
	ctx := context.Background()
	if _, found, err := m.Get(ctx, "nope"); found || err != nil {
		t.Fatalf("missing key: found %v, err %v", found, err)
	}
	if err := m.Put(ctx, "k", []byte("one"), clock.now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := m.Put(ctx, "k", []byte("two"), clock.now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if got, found, _ := m.Get(ctx, "k"); !found || string(got) != "two" {
		t.Fatalf("overwritten: %q %v", got, found)
	}
	consistent(t, m)
	if err := m.Delete(ctx, "k"); err != nil {
		t.Fatal(err)
	}
	if err := m.Delete(ctx, "k"); err != nil {
		t.Fatalf("deleting a missing key: %v", err)
	}
	if _, found, _ := m.Get(ctx, "k"); found {
		t.Fatal("deleted record found")
	}
	consistent(t, m)
}

func TestMemoryStoreDoesNotShareSlices(t *testing.T) {
	m, clock := newTestStore(t, 1<<20)
	ctx := context.Background()
	data := []byte("abc")
	m.Put(ctx, "k", data, clock.now().Add(time.Hour))
	data[0] = 'X'
	got, _, _ := m.Get(ctx, "k")
	if string(got) != "abc" {
		t.Fatalf("Put kept the caller's slice: %q", got)
	}
	got[0] = 'Y'
	again, _, _ := m.Get(ctx, "k")
	if string(again) != "abc" {
		t.Fatalf("Get returned the internal slice: %q", again)
	}
}

func TestMemoryStoreExpiry(t *testing.T) {
	m, clock := newTestStore(t, 1<<20)
	ctx := context.Background()
	m.Put(ctx, "k", []byte("x"), clock.now().Add(time.Minute))
	clock.advance(time.Minute - time.Nanosecond)
	if _, found, _ := m.Get(ctx, "k"); !found {
		t.Fatal("record gone before its expiry")
	}
	clock.advance(time.Nanosecond)
	if _, found, err := m.Get(ctx, "k"); found || err != nil {
		t.Fatalf("expired record: found %v, err %v", found, err)
	}
	consistent(t, m)
	if len(m.entries) != 0 {
		t.Fatal("Get left the expired record in the store")
	}
	// A record that is already expired is not kept and replaces the old one.
	m.Put(ctx, "k", []byte("x"), clock.now().Add(time.Hour))
	m.Put(ctx, "k", []byte("y"), clock.now().Add(-time.Second))
	if _, found, _ := m.Get(ctx, "k"); found {
		t.Fatal("expired Put left the old record")
	}
	consistent(t, m)
}

func TestMemoryStoreCanceledContext(t *testing.T) {
	m, clock := newTestStore(t, 1<<20)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := m.Get(ctx, "k"); !errors.Is(err, context.Canceled) {
		t.Errorf("Get: %v", err)
	}
	if err := m.Put(ctx, "k", []byte("x"), clock.now().Add(time.Hour)); !errors.Is(err, context.Canceled) {
		t.Errorf("Put: %v", err)
	}
	if err := m.Delete(ctx, "k"); !errors.Is(err, context.Canceled) {
		t.Errorf("Delete: %v", err)
	}
}

func TestMemoryStoreLimit(t *testing.T) {
	if msg := panicOf(func() { NewMemoryStore(0) }); !strings.Contains(msg, "positive") {
		t.Errorf("0: %q", msg)
	}
	if msg := panicOf(func() { NewMemoryStore(-1) }); !strings.Contains(msg, "positive") {
		t.Errorf("-1: %q", msg)
	}
	m, clock := newTestStore(t, 1000)
	ctx := context.Background()
	if err := m.Put(ctx, "big", bytes.Repeat([]byte("x"), 1000), clock.now().Add(time.Hour)); !errors.Is(err, ErrSessionRecordTooLarge) {
		t.Fatalf("a record over the limit: %v", err)
	}
	if len(m.entries) != 0 {
		t.Fatal("a refused record was kept")
	}
}

func TestMemoryStoreEvictsTheEarliestExpiry(t *testing.T) {
	// Each record costs 2 + 100 + entryOverhead, so the store holds three.
	per := int64(2 + 100 + entryOverhead)
	m, clock := newTestStore(t, per*3)
	ctx := context.Background()
	data := bytes.Repeat([]byte("x"), 100)
	m.Put(ctx, "a2", data, clock.now().Add(2*time.Hour))
	m.Put(ctx, "a1", data, clock.now().Add(1*time.Hour))
	m.Put(ctx, "a3", data, clock.now().Add(3*time.Hour))
	m.Put(ctx, "a4", data, clock.now().Add(4*time.Hour)) // makes room by dropping a1
	for key, want := range map[string]bool{"a1": false, "a2": true, "a3": true, "a4": true} {
		if _, found, _ := m.Get(ctx, key); found != want {
			t.Errorf("%s found %v, want %v", key, found, want)
		}
	}
	consistent(t, m)
	// A new record that expires first is still stored, and the one before it goes.
	m.Put(ctx, "a0", data, clock.now().Add(30*time.Minute))
	if _, found, _ := m.Get(ctx, "a2"); found {
		t.Error("a2 stayed")
	}
	if _, found, _ := m.Get(ctx, "a0"); !found {
		t.Error("a0 missing")
	}
	consistent(t, m)
}

func TestMemoryStoreOverwriteAndDeleteLeaveNoHeapNodes(t *testing.T) {
	m, clock := newTestStore(t, 1<<20)
	ctx := context.Background()
	for i := 0; i < 1000; i++ {
		m.Put(ctx, "same", []byte("x"), clock.now().Add(time.Duration(i+1)*time.Minute))
	}
	if len(m.byExp) != 1 {
		t.Fatalf("heap holds %d nodes after 1000 overwrites", len(m.byExp))
	}
	for i := 0; i < 100; i++ {
		m.Put(ctx, fmt.Sprint("k", i), []byte("x"), clock.now().Add(time.Duration(i+1)*time.Minute))
	}
	for i := 0; i < 100; i += 2 {
		m.Delete(ctx, fmt.Sprint("k", i))
	}
	consistent(t, m)
	if len(m.byExp) != 51 {
		t.Fatalf("heap holds %d nodes, want 51", len(m.byExp))
	}
}

func TestMemoryStoreSweep(t *testing.T) {
	m, clock := newTestStore(t, 1<<24)
	ctx := context.Background()
	for i := 0; i < sweepBatch*2+10; i++ {
		m.Put(ctx, fmt.Sprint("old", i), []byte("x"), clock.now().Add(time.Minute))
	}
	m.Put(ctx, "young", []byte("x"), clock.now().Add(time.Hour))
	clock.advance(2 * time.Minute)
	if n := m.sweepBatch(); n != sweepBatch {
		t.Fatalf("first batch removed %d", n)
	}
	for m.sweepBatch() > 0 {
	}
	if len(m.entries) != 1 || m.entries["young"] == nil {
		t.Fatalf("%d records left", len(m.entries))
	}
	consistent(t, m)
}

func TestMemoryStoreCloseStopsTheSweeper(t *testing.T) {
	m, clock := newTestStore(t, 1<<20)
	m.Put(context.Background(), "k", []byte("x"), clock.now().Add(time.Minute))
	clock.advance(time.Hour)
	m.Close()
	m.Close()
	select {
	case <-m.stop:
	default:
		t.Fatal("Close did not stop the sweeper")
	}
}

func TestMemoryStoreConcurrent(t *testing.T) {
	m, clock := newTestStore(t, 20_000)
	ctx := context.Background()
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				key := fmt.Sprint("k", (g*31+i)%40)
				m.Put(ctx, key, []byte("value"), clock.now().Add(time.Duration(i)*time.Second))
				m.Get(ctx, key)
				if i%7 == 0 {
					m.Delete(ctx, key)
				}
				if i%50 == 0 {
					clock.advance(time.Second)
					m.sweepBatch()
				}
			}
		}()
	}
	wg.Wait()
	consistent(t, m)
}

func TestMemoryStoreEvictionIsNotLinear(t *testing.T) {
	m, clock := newTestStore(t, 1<<20)
	ctx := context.Background()
	per := int64(len("k000000") + 8 + entryOverhead)
	n := int(m.max/per) + 50000
	start := time.Now()
	for i := 0; i < n; i++ {
		m.Put(ctx, fmt.Sprintf("k%06d", i), []byte("12345678"), clock.now().Add(time.Duration(i+1)*time.Second))
	}
	// A full scan per Put would take n * (max / per) steps, over a billion here.
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("%d Puts into a full store took %v", n, d)
	}
	consistent(t, m)
}
