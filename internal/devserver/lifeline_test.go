//go:build unix || windows

package devserver

import (
	"testing"
	"time"
)

// The app reads the end of its lifeline when the write end closes, which the system does when
// tifa dev ends. It must not be held by another app that tifa dev starts later, or the first one
// would not stop until that one did.
func TestLifelineEndsWhenOnlyTifaDevHoldsTheWriteEnd(t *testing.T) {
	first, firstEnded := startWaiting(t, "first")
	second, secondEnded := startWaiting(t, "second")

	select {
	case line := <-firstEnded:
		t.Fatalf("the first app ended with its lifeline open: %q", line)
	case line := <-secondEnded:
		t.Fatalf("the second app ended with its lifeline open: %q", line)
	case <-time.After(300 * time.Millisecond):
	}
	first.close()
	select {
	case line := <-firstEnded:
		if line != "first ended\n" {
			t.Fatalf("the first app printed %q", line)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the first app did not see the end of its lifeline, something else holds the write end")
	}
	select {
	case line := <-secondEnded:
		t.Fatalf("the second app ended when the first lifeline closed: %q", line)
	case <-time.After(300 * time.Millisecond):
	}
	second.close()
	select {
	case <-secondEnded:
	case <-time.After(10 * time.Second):
		t.Fatal("the second app did not see the end of its lifeline")
	}
}
