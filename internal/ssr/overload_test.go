package ssr

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/internal/config"
)

func TestPoolWaitHasServerDeadline(t *testing.T) {
	cfg := config.Default().SSR
	cfg.VMs = 1
	p := newTestPool(t, testSource(t), cfg)
	acquired, release := holdVMs(p)
	defer release()
	first := make(chan error, 1)
	go func() { _, err := p.Render(context.Background(), input("ok", "first")); first <- err }()
	<-acquired
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	start := time.Now()
	_, err := p.Render(ctx, input("ok", "queued"))
	elapsed := time.Since(start)
	release()
	if firstErr := <-first; firstErr != nil {
		t.Fatal(firstErr)
	}
	if !errors.Is(err, tifa.ErrSSRBusy) {
		t.Fatalf("want a server overload error before the client deadline, got %v after %s", err, elapsed)
	}
	if elapsed < 500*time.Millisecond || elapsed > 1500*time.Millisecond {
		t.Fatalf("server wait took %s, want about 1s", elapsed)
	}
	if s := p.stats(); s.created != 1 || s.closed != 0 {
		t.Fatalf("waiting replaced a VM: %+v", s)
	}
}

func TestPoolRejectsExcessWaiters(t *testing.T) {
	cfg := config.Default().SSR
	cfg.VMs = 1
	p := newTestPool(t, testSource(t), cfg)
	acquired, release := holdVMs(p)
	defer release()
	var wg sync.WaitGroup
	wg.Go(func() { _, _ = p.Render(context.Background(), input("ok", "first")) })
	<-acquired
	ctx, cancel := context.WithCancel(context.Background())
	defer func() { cancel(); release(); wg.Wait() }()
	results := make(chan error, 34)
	for range 34 {
		wg.Go(func() { _, err := p.Render(ctx, input("ok", "queued")); results <- err })
	}
	for range 2 {
		select {
		case err := <-results:
			if !errors.Is(err, tifa.ErrSSRBusy) {
				t.Fatalf("want a full queue error, got %v", err)
			}
		case <-time.After(500 * time.Millisecond):
			t.Fatal("more than 32 requests kept waiting for one busy VM")
		}
	}
	cancel()
	for range 32 {
		if err := <-results; !errors.Is(err, context.Canceled) {
			t.Fatalf("one of the 32 waiting requests was rejected: %v", err)
		}
	}
}

func TestPoolQueueDisabledAndWaitSlotReleased(t *testing.T) {
	for _, limit := range []int{0, 1} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			cfg := testConfig(1)
			cfg.QueueLimit, cfg.WaitTimeout = limit, 20*time.Millisecond
			p := newTestPool(t, testSource(t), cfg)
			acquired, release := holdVMs(p)
			defer release()
			first := make(chan error, 1)
			go func() { _, err := p.Render(context.Background(), input("ok", "first")); first <- err }()
			<-acquired
			for range 2 {
				_, err := p.Render(context.Background(), input("ok", "waiting"))
				if !errors.Is(err, tifa.ErrSSRBusy) {
					t.Fatalf("got %v", err)
				}
				if len(p.waiters) != 0 {
					t.Fatal("wait slot leaked")
				}
			}
			release()
			if err := <-first; err != nil {
				t.Fatal(err)
			}
			// The VM is busy until the GC after the first render ends.
			idleVM(p, func(*pooledVM) {})
			if _, err := p.Render(context.Background(), input("ok", "after")); err != nil {
				t.Fatalf("idle VM rejected: %v", err)
			}
			if s := p.stats(); s.created != 1 || s.closed != 0 {
				t.Fatalf("overload replaced a VM: %+v", s)
			}
		})
	}
}

func TestPoolRejectsInvalidQueueConfig(t *testing.T) {
	for _, change := range []func(*config.SSR){func(c *config.SSR) { c.QueueLimit = -1 }, func(c *config.SSR) { c.WaitTimeout = 0 }} {
		cfg := testConfig(1)
		change(&cfg)
		if p, err := NewPool(testSource(t), nil, cfg, false); err == nil {
			p.Close()
			t.Fatal("accepted invalid queue settings")
		}
	}
}

func TestWaitExpiresAsVMBecomesAvailable(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p := newTestPool(t, testSource(t), testConfig(1))
		vm := <-p.idle
		p.waiting = func(timer *time.Timer) {
			// Consume the timer at its deadline, then offer the VM. Only the VM case
			// can win select, so the deadline check after acquisition must reject it.
			<-timer.C
			p.idle <- vm
		}
		if _, err := p.Render(context.Background(), input("ok", "expired")); !errors.Is(err, tifa.ErrSSRBusy) {
			t.Fatalf("expired wait: %v", err)
		}
		if len(p.waiters) != 0 || len(p.idle) != 1 {
			t.Fatalf("waiters=%d idle=%d", len(p.waiters), len(p.idle))
		}
		html, err := p.Render(context.Background(), input("ok", "after"))
		if err != nil || !strings.Contains(html, `data-renders="1"`) {
			t.Fatalf("expired request used or lost the VM: %s, %v", html, err)
		}
		if err := p.Close(); err != nil {
			t.Fatal(err)
		}
		if s := p.stats(); s.created != 1 || s.closed != s.created {
			t.Fatalf("VMs replaced or left open: %+v", s)
		}
	})
}
