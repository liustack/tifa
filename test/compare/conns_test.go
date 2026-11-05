package compare

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestWithProcsGivesTifaThatManyVMsAndCPUs(t *testing.T) {
	env := Env{Shop: "/shop", Next: "/next", Start: "/start", React: "/react"}
	var tifa, next Spec
	for _, s := range env.Specs() {
		switch s.Name {
		case "tifa":
			tifa = s
		case "nextjs":
			next = s
		}
	}
	same, err := tifa.WithProcs(1)
	if err != nil || !slices.Equal(same.Args, tifa.Args) || !slices.Equal(same.Env, tifa.Env) {
		t.Fatalf("1 changed the spec: %v %+v", err, same)
	}
	got, err := tifa.WithProcs(3)
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Join(got.Args, " ")
	if !strings.Contains(args, "--ssr-vms 3") || strings.Contains(args, "--ssr-vms 1") {
		t.Errorf("args %q", args)
	}
	if !slices.Contains(got.Env, "GOMAXPROCS=3") || slices.Contains(got.Env, "GOMAXPROCS=1") {
		t.Errorf("env %q", got.Env)
	}
	if !slices.Contains(tifa.Env, "GOMAXPROCS=1") || !slices.Contains(tifa.Args, "1") {
		t.Errorf("the original changed: %+v", tifa)
	}
	if _, err := next.WithProcs(2); err == nil {
		t.Error("Next.js was given 2 procs")
	}
	if _, err := next.WithProcs(1); err != nil {
		t.Errorf("1 proc for Next.js: %v", err)
	}
}

func TestConnOffsetsStartOnOtherPagesAndNeverWriteTheSameAddress(t *testing.T) {
	const conns, reads = 8, 5000
	if ConnOffset(0) != 0 {
		t.Fatalf("connection 0 has offset %d", ConnOffset(0))
	}
	firstPage := map[int]bool{}
	addresses := map[string]int{}
	for c := range conns {
		steps := ReadWriteScenario.Steps(nodePath, ConnOffset(c))
		firstPage[timed(t, steps).Page] = true
		for n := range reads {
			for _, st := range ReadWriteScenario.Steps(nodePath, ConnOffset(c)+n) {
				if st.Write == nil {
					continue
				}
				if other, ok := addresses[st.Write.Address]; ok {
					t.Fatalf("connections %d and %d write the address %q", other, c, st.Write.Address)
				}
				addresses[st.Write.Address] = c
			}
		}
	}
	if len(firstPage) < 6 {
		t.Errorf("8 connections start on %d different pages", len(firstPage))
	}
}

func TestDriveConnsRunsThemAtOnceAndMergesWhatTheySaw(t *testing.T) {
	for _, conns := range []int{1, 3} {
		var inflight, peak atomic.Int64
		var mu sync.Mutex
		paths := map[string]int{}
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			n := inflight.Add(1)
			for {
				p := peak.Load()
				if n <= p || peak.CompareAndSwap(p, n) {
					break
				}
			}
			time.Sleep(2 * time.Millisecond)
			inflight.Add(-1)
			mu.Lock()
			paths[r.Method+" "+r.URL.Path]++
			mu.Unlock()
			w.Header().Set("X-Tifa-Render", "cache")
			w.Write([]byte("ok"))
		}))
		o := Options{Procs: conns, Steady: 300 * time.Millisecond, Cold: 20}
		res, err := driveConns(context.Background(), srv.URL, "identity", "fake", func() string { return "" }, nodePath, ReadWriteScenario, o, time.Now())
		srv.Close()
		if err != nil {
			t.Fatal(err)
		}
		if got := peak.Load(); got != int64(conns) {
			t.Errorf("%d conns: %d requests in flight at most", conns, got)
		}
		if len(res.Conns) != conns {
			t.Fatalf("%d conns: %d results", conns, len(res.Conns))
		}
		var steady, renders int
		var rate float64
		for _, c := range res.Conns {
			if len(c.Cold) != 20 {
				t.Errorf("a connection has %d cold answers", len(c.Cold))
			}
			steady += len(c.Steady)
			renders += c.Renders["cache"]
			rate += float64(len(c.Steady)) / c.Elapsed
		}
		if len(res.Steady) != steady || res.Renders["cache"] != renders || len(res.SteadyPage) != steady {
			t.Errorf("merged %d/%d answers, %d renders", len(res.Steady), len(res.SteadyPage), res.Renders["cache"])
		}
		if res.Requests != rate || rate < 100 {
			t.Errorf("rate %.1f, sum of the connections %.1f", res.Requests, rate)
		}
		if res.Wire[0].Bytes != 2 {
			t.Errorf("wire %+v", res.Wire)
		}
	}
}
