package devserver

import (
	"context"
	"slices"
	"sync/atomic"
	"testing"
	"time"
)

// runLoop runs loop until the test ends and returns the batches it rebuilds.
// hold, when not nil, is called inside each rebuild before it returns.
func runLoop(t *testing.T, c *changes, debounce time.Duration, hold func()) <-chan []change {
	t.Helper()
	batches := make(chan []change, 10)
	var running atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		loop(ctx, c, debounce, func(batch []change) {
			if running.Add(1) != 1 {
				t.Error("two rebuilds ran at the same time")
			}
			defer running.Add(-1)
			batches <- batch
			if hold != nil {
				hold()
			}
		})
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	return batches
}

func receive(t *testing.T, batches <-chan []change) []change {
	t.Helper()
	select {
	case b := <-batches:
		return b
	case <-time.After(5 * time.Second):
		t.Fatal("no rebuild within 5s")
		return nil
	}
}

func TestLoopMergesChangesIntoOneRebuild(t *testing.T) {
	c := newChanges()
	batches := runLoop(t, c, 200*time.Millisecond, nil)
	c.add("app/a.go", "s1", "s2")
	c.add("app/b.tsx", "", "s3")
	c.add("app/a.go", "s2", "s4")
	want := []change{{"app/a.go", modified}, {"app/b.tsx", created}}
	if got := receive(t, batches); !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	select {
	case b := <-batches:
		t.Fatalf("unexpected second rebuild %v", b)
	default:
	}
}

func TestLoopSkipsBatchesThatCancelOut(t *testing.T) {
	c := newChanges()
	batches := runLoop(t, c, 10*time.Millisecond, nil)
	c.add("app/a.go", "s1", "s2")
	c.add("app/a.go", "s2", "s1")
	c.add("app/b.go", "s1", "s2") // proves the loop went past the first batch without a rebuild
	if got := receive(t, batches); !slices.Equal(got, []change{{"app/b.go", modified}}) {
		t.Fatalf("got %v, want only app/b.go", got)
	}
}

func TestLoopRunsAgainForChangesDuringARebuild(t *testing.T) {
	c := newChanges()
	started := make(chan struct{})
	release := make(chan struct{})
	first := true
	batches := runLoop(t, c, 10*time.Millisecond, func() {
		if first {
			first = false
			close(started)
			<-release
		}
	})
	c.add("app/a.go", "s1", "s2")
	if got := receive(t, batches); !slices.Equal(got, []change{{"app/a.go", modified}}) {
		t.Fatalf("first rebuild got %v", got)
	}
	<-started
	c.add("app/b.tsx", "s1", "s2")
	c.add("app/c.tsx", "s1", "s2")
	close(release)
	want := []change{{"app/b.tsx", modified}, {"app/c.tsx", modified}}
	if got := receive(t, batches); !slices.Equal(got, want) {
		t.Fatalf("second rebuild got %v, want %v", got, want)
	}
}
