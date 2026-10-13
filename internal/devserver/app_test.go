package devserver

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// syncBuffer collects process output written from the exec goroutines.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func startFakeApp(t *testing.T, out io.Writer) (*app, string) {
	t.Helper()
	cmd := fakeAppCommand(t)
	addr, err := freeAddr()
	if err != nil {
		t.Fatal(err)
	}
	a, err := startApp(cmd, addr, out, out)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		select {
		case <-a.done:
		default:
			_ = a.cmd.Process.Kill()
			<-a.done
		}
	})
	return a, addr
}

func TestAppStopsWithInterrupt(t *testing.T) {
	var out syncBuffer
	a, addr := startFakeApp(t, &out)
	if err := a.waitReady(context.Background(), addr, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	resp, err := http.Get("http://" + addr + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(body) != "none" {
		t.Fatalf("got %q from the app", body)
	}
	if err := a.stop(5 * time.Second); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "shutdown done") {
		t.Fatalf("app did not shut down cleanly, output %q", out.String())
	}
}

func TestAppIsKilledAfterStopTimeout(t *testing.T) {
	t.Setenv("TIFA_FAKE_APP_ON_INTERRUPT", "ignore")
	a, addr := startFakeApp(t, io.Discard)
	if err := a.waitReady(context.Background(), addr, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	err := a.stop(100 * time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "killed") {
		t.Fatalf("got %v, want an error saying the app was killed", err)
	}
	select {
	case <-a.done:
	default:
		t.Fatal("process still running after stop returned")
	}
}

func TestAppReadyFailsWhenTheAppExits(t *testing.T) {
	t.Setenv("TIFA_FAKE_APP_EXIT", "OnStart: database is down")
	var out syncBuffer
	a, addr := startFakeApp(t, &out)
	err := a.waitReady(context.Background(), addr, 5*time.Second)
	if err == nil || !strings.Contains(err.Error(), "exited") {
		t.Fatalf("got %v, want an error saying the app exited", err)
	}
	if !strings.Contains(out.String(), "database is down") {
		t.Fatalf("output %q misses the app's own message", out.String())
	}
}
