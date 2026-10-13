package devserver

import (
	"errors"
	"log"
	"sync"
	"testing"
)

// stallingWriter blocks the first write until release is closed.
type stallingWriter struct {
	once             sync.Once
	started, release chan struct{}
}

func (w *stallingWriter) Write(b []byte) (int, error) {
	w.once.Do(func() {
		close(w.started)
		<-w.release
	})
	return len(b), nil
}

// An exit report that is slow to finish must not cover the status of the app started after it.
func TestExitOfAReplacedAppKeepsTheNewStatus(t *testing.T) {
	out := &stallingWriter{started: make(chan struct{}), release: make(chan struct{})}
	old := &app{done: make(chan struct{}), err: errors.New("exit status 2"), gen: 1}
	d := &dev{app: old, live: old, gen: 1, hub: newHub(), log: log.New(out, "", 0)}
	d.hub.publish(status{Version: "v1"})
	reported := make(chan struct{})
	go func() {
		d.watchExit(old)
		close(reported)
	}()
	close(old.done)
	<-out.started // the exit is being reported

	// Meanwhile the next build starts its app and announces it.
	next := &app{done: make(chan struct{}), gen: 2}
	d.mu.Lock()
	d.app, d.live, d.gen, d.downErr = next, next, 2, nil
	d.hub.publish(status{Version: "v2"})
	d.mu.Unlock()

	close(out.release)
	<-reported
	if st := <-d.hub.subscribe(); st != (status{Version: "v2"}) {
		t.Fatalf("status %+v, want the new app's", st)
	}
	if d.down() != nil {
		t.Fatalf("proxy told the app is down: %v", d.down())
	}
}
