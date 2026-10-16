package devserver

import (
	"bytes"
	"context"
	"fmt"
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
	a, err := startApp(cmd, t.TempDir(), addr, out, out)
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
	// The terminal has the app's output already, the browser gets it with the error.
	if strings.Contains(err.Error(), "database is down") {
		t.Errorf("the short error repeats the output: %q", err)
	}
	if got := explain(err); !strings.Contains(got, "exit status 1") || !strings.Contains(got, "fake app: OnStart: database is down") {
		t.Errorf("explain: %q", got)
	}
}

// A panic message sits above a long stack trace. It stays in what the browser shows, together
// with the end of the output, within the limits.
func TestAppExitKeepsThePanicAndTheLastLines(t *testing.T) {
	t.Setenv("TIFA_FAKE_APP_PANIC", "100")
	a, addr := startFakeApp(t, io.Discard)
	err := a.waitReady(context.Background(), addr, 5*time.Second)
	if err == nil {
		t.Fatal("the app did not fail")
	}
	got := explain(err)
	for _, want := range []string{"exit status 2", "panic: the order table is gone", "devserver.TestMain("} {
		if !strings.Contains(got, want) {
			t.Errorf("explain lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "line 0\n") || len(got) > tailBytes+1024 || strings.Count(got, "\n") > tailLines+8 {
		t.Errorf("explain keeps too much (%d bytes, %d lines):\n%s", len(got), strings.Count(got, "\n"), got)
	}
}

func TestTailKeepsTheEndOfLongLines(t *testing.T) {
	var tl tail
	_, _ = tl.Write([]byte(strings.Repeat("x", 3*tailBytes) + "\nlast\n"))
	if got := tl.String(); len(got) > tailBytes || !strings.HasSuffix(got, "\nlast\n") {
		t.Errorf("%d bytes, ends %q", len(got), got[max(0, len(got)-20):])
	}
}

// A crash line counts only while the output still ends with the report it starts: a line that
// a library logged and the app outlived does not come back in front of later output.
func TestTailForgetsACrashLineTheAppOutlived(t *testing.T) {
	logs := func(n int) string {
		var b strings.Builder
		for i := range n {
			fmt.Fprintf(&b, "2026/10/16 20:00:00 GET /orders/%d 200\n", i)
		}
		return b.String()
	}
	for name, out := range map[string]string{
		"pushed out of the lines": "panic: a library recovered from this\n" + logs(tailLines+5),
		"trimmed from the buffer": "fatal error: printed by a library\n" + logs(3*tailBytes/40),
		"in pieces, then trimmed": "pan|ic: split across writes\n|" + logs(3*tailBytes/40),
	} {
		t.Run(name, func(t *testing.T) {
			var tl tail
			for piece := range strings.SplitSeq(out, "|") {
				_, _ = tl.Write([]byte(piece))
			}
			if got := tl.String(); strings.Contains(got, "panic: ") || strings.Contains(got, "fatal error: ") || !strings.HasSuffix(got, "200\n") {
				t.Errorf("%d bytes, starts %q", len(got), got[:min(len(got), 80)])
			}
		})
	}
	// A real report after such a line still comes first.
	var tl tail
	report := "panic: the order table is gone\n\ngoroutine 1 [running]:\n"
	for i := range 2 * tailLines {
		report += fmt.Sprintf("main.deep(0x%x)\n\t/app/main.go:%d +0x1d\n", i, i)
	}
	report += "created by main.main in goroutine 1\n\t/app/main.go:3 +0x2f\n"
	_, _ = tl.Write([]byte("panic: an old one\n" + logs(5) + report))
	if got := tl.String(); !strings.HasPrefix(got, "panic: the order table is gone\n...\n") || !strings.HasSuffix(got, "+0x2f\n") {
		t.Errorf("starts %q", got[:min(len(got), 80)])
	}
}
