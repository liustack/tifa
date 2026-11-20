package main

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// A read that repeats inside one plan runs once, even when two callers ask at the same time, and a
// failure is remembered as well.
func TestPlanMemoRunsAReadOnce(t *testing.T) {
	f := newFakeCF("example.com")
	f.install(t)
	ctx := withReadMemo(context.Background())
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := findZone(ctx, "shop.example.com"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if _, err := findZone(ctx, "shop.example.com"); err != nil {
		t.Fatal(err)
	}
	if n := countCalls(f, "zones list --name shop.example.com"); n != 1 {
		t.Errorf("zones list for shop.example.com ran %d times:\n%v", n, f.calls)
	}
	if n := countCalls(f, "zones list --name example.com"); n != 1 {
		t.Errorf("zones list for example.com ran %d times:\n%v", n, f.calls)
	}

	var runs atomic.Int32
	runCF = func(context.Context, ...string) ([]byte, error) {
		runs.Add(1)
		return nil, &cfError{stderr: "boom", err: context.DeadlineExceeded}
	}
	for i := 0; i < 3; i++ {
		if _, err := readCF(ctx, "dns", "records", "list", "-z", "z"); err == nil {
			t.Fatal("no error")
		}
	}
	if runs.Load() != 1 {
		t.Errorf("a failed read ran %d times", runs.Load())
	}
}

// A call that changes something, and a read nobody listed, is never remembered, and without a memo
// in the context every call runs. This is what the apply phase relies on.
func TestPlanMemoSkipsWritesAndApply(t *testing.T) {
	f := newFakeCF("example.com")
	f.install(t)
	ctx := withReadMemo(context.Background())
	for i := 0; i < 2; i++ {
		if err := cfWrite(ctx, true, "dns", "records", "create", "-z", "example.com", "--body", `{"name":"a"}`); err != nil {
			t.Fatal(err)
		}
		if _, err := readCF(ctx, "tunnels", "token", "get", "t1"); err != nil {
			t.Fatal(err)
		}
	}
	if n := countCalls(f, "dns records create"); n != 2 {
		t.Errorf("a write ran %d times", n)
	}
	if n := countCalls(f, "tunnels token get t1"); n != 2 {
		t.Errorf("an unlisted read ran %d times", n)
	}
	plain := context.Background()
	for i := 0; i < 2; i++ {
		if _, err := findZone(plain, "example.com"); err != nil {
			t.Fatal(err)
		}
	}
	if n := countCalls(f, "zones list --name example.com"); n != 2 {
		t.Errorf("without a memo the read ran %d times", n)
	}
}

func TestPlanMemoGH(t *testing.T) {
	g := newFakeGH()
	g.install(t)
	ctx := withReadMemo(context.Background())
	for i := 0; i < 3; i++ {
		if _, err := readGH(ctx, "/app", nil, "secret", "list", "--repo", "acme/shop", "--json", "name"); err != nil {
			t.Fatal(err)
		}
		if _, err := readGH(ctx, "/other", nil, "secret", "list", "--repo", "acme/shop", "--json", "name"); err != nil {
			t.Fatal(err)
		}
		if _, err := readGH(ctx, "/app", strings.NewReader("v"), "secret", "set", "X", "--repo", "acme/shop"); err != nil {
			t.Fatal(err)
		}
	}
	n := 0
	for _, c := range g.calls {
		if strings.HasPrefix(c, "secret list") {
			n++
		}
	}
	if n != 2 || len(g.calls) != 5 {
		t.Errorf("calls: %v", g.calls)
	}
}

func countCalls(f *fakeCF, prefix string) (n int) {
	fakeMu.Lock()
	defer fakeMu.Unlock()
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}
