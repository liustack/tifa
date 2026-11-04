package ssr

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/liustack/tifa"
)

// Stats says how many VMs are loaded, rendering and waited for now, and counts the renders, the
// failed ones, their time and the renders refused as busy.
func TestPoolStats(t *testing.T) {
	cfg := testConfig(1)
	cfg.QueueLimit, cfg.WaitTimeout = 1, 50*time.Millisecond
	p := newTestPool(t, testSource(t), cfg)
	if s := p.Stats(); s.VMs != 1 || s.Busy != 0 || s.Queued != 0 || s.Renders != 0 {
		t.Fatalf("a new pool: %+v", s)
	}
	acquired, release := holdVMs(p)
	defer release()
	first := make(chan error, 1)
	go func() { _, err := p.Render(context.Background(), input("ok", "first")); first <- err }()
	<-acquired
	// One render waits in the queue of one, so the next is refused at once.
	queued := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _, err := p.Render(ctx, input("ok", "queued")); queued <- err }()
	waitFor(t, 5*time.Second, "a render waits for the VM", func() bool { return p.Stats().Queued == 1 })
	if _, err := p.Render(context.Background(), input("ok", "refused")); !errors.Is(err, tifa.ErrSSRBusy) {
		t.Fatalf("a render past the queue: %v", err)
	}
	if s := p.Stats(); s.Busy != 1 || s.Queued != 1 || s.Rejected != 1 {
		t.Errorf("while one renders and one waits: %+v", s)
	}
	cancel()
	if err := <-queued; !errors.Is(err, context.Canceled) {
		t.Fatalf("the queued render: %v", err)
	}
	release()
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if _, err := p.Render(context.Background(), input("throw", "")); err == nil {
		t.Fatal("a render that throws did not fail")
	}
	idleVM(p, func(*pooledVM) {})
	s := p.Stats()
	if s.Renders != 2 || s.Failed != 1 || s.RenderTime <= 0 || s.Busy != 0 || s.Queued != 0 || s.Rejected != 1 || s.VMs != 1 {
		t.Errorf("after two renders, one failed: %+v", s)
	}
}
