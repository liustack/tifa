package ssr

import (
	"context"
	"strconv"
	"testing"
	"time"
)

// slowSource is the test bundle with top level code that takes about d to run.
func slowSource(t *testing.T, d time.Duration) string {
	return testSource(t) + "\n{ const end = Date.now() + " + strconv.FormatInt(d.Milliseconds(), 10) + "; while (Date.now() < end) {} }\n"
}

// A VM that replaces a failed one loads without a time limit: the bundle loaded when the pool
// started, so it ends, and on a machine short of CPU it may take longer than startTimeout. With
// the limit, every try was interrupted and the pool stayed without the VM until the load eased.
func TestRefillHasNoTimeLimit(t *testing.T) {
	cfg := testConfig(1)
	cfg.Timeout = 100 * time.Millisecond
	p := newTestPool(t, slowSource(t, 300*time.Millisecond), cfg)
	defer swap(&startTimeout, 50*time.Millisecond)()
	if _, err := p.Render(context.Background(), input("loop", "")); err == nil {
		t.Fatal("the loop rendered")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := p.Render(ctx, input("ok", "")); err != nil {
		t.Fatalf("no VM came back: %v", err)
	}
	if s := p.stats(); s.created != 2 || s.closed != 1 {
		t.Fatalf("want one VM replaced, got %+v", s)
	}
}

// Close does not wait for a replacement to finish loading: it interrupts it.
func TestCloseInterruptsALoadingVM(t *testing.T) {
	started := make(chan struct{})
	loads := 0
	defer swap(&loadVM, func(p *Pool) (*pooledVM, error) {
		if loads++; loads == 2 {
			close(started)
		}
		return openVM(p)
	})()
	cfg := testConfig(1)
	cfg.Timeout = 100 * time.Millisecond
	p, err := NewPool(slowSource(t, 1500*time.Millisecond), nil, cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Render(context.Background(), input("loop", "")); err == nil {
		t.Fatal("the loop rendered")
	}
	<-started
	time.Sleep(100 * time.Millisecond)
	begin := time.Now()
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(begin); d > 700*time.Millisecond {
		t.Errorf("Close waited %s for the load", d)
	}
	if s := p.stats(); s.created != s.closed {
		t.Errorf("a VM is left open: %+v", s)
	}
}
