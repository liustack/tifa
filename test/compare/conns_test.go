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
			w.Header().Set("X-Tifa-Render", "memo")
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
			renders += c.Renders["memo"]
			rate += float64(len(c.Steady)) / c.Elapsed
		}
		if len(res.Steady) != steady || res.Renders["memo"] != renders || len(res.SteadyPage) != steady {
			t.Errorf("merged %d/%d answers, %d renders", len(res.Steady), len(res.SteadyPage), res.Renders["memo"])
		}
		if res.Requests != rate || rate < 100 {
			t.Errorf("rate %.1f, sum of the connections %.1f", res.Requests, rate)
		}
		if res.Wire[0].Bytes != 2 {
			t.Errorf("wire %+v", res.Wire)
		}
	}
}

func TestUniqueURLsMakeEveryReadAskForADifferentURL(t *testing.T) {
	var mu sync.Mutex
	var gets []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gets = append(gets, r.URL.RequestURI())
		mu.Unlock()
		w.Write([]byte("ok"))
	}))
	defer srv.Close()
	for _, unique := range []bool{false, true} {
		gets = nil
		o := Options{Procs: 3, Steady: 100 * time.Millisecond, Cold: 20, UniqueURLs: unique}
		if _, err := driveConns(context.Background(), srv.URL, "identity", "fake", func() string { return "" }, nodePath, ReadScenario, o, time.Now()); err != nil {
			t.Fatal(err)
		}
		seen := map[string]bool{}
		for _, g := range gets {
			seen[g] = true
		}
		if unique && len(seen) != len(gets) {
			t.Errorf("%d reads asked for %d URLs", len(gets), len(seen))
		}
		if !unique && len(seen) > len(Pages) {
			t.Errorf("without the option the reads asked for %d URLs", len(seen))
		}
	}
}

func TestConnsOverridesTheRequestsInFlight(t *testing.T) {
	var inflight, peak atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := inflight.Add(1)
		for p := peak.Load(); n > p && !peak.CompareAndSwap(p, n); p = peak.Load() {
		}
		time.Sleep(3 * time.Millisecond)
		inflight.Add(-1)
		w.Write([]byte("ok"))
	}))
	defer srv.Close()
	o := Options{Procs: 2, Conns: 5, Steady: 100 * time.Millisecond, Cold: 5}
	res, err := driveConns(context.Background(), srv.URL, "identity", "fake", func() string { return "" }, nodePath, ReadScenario, o, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Conns) != 5 || peak.Load() != 5 {
		t.Errorf("%d connections, %d in flight at most", len(res.Conns), peak.Load())
	}
}

func TestDriveConnsReportsTheErrorThatStoppedTheOthers(t *testing.T) {
	var n atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) == 10 {
			http.Error(w, "full", http.StatusServiceUnavailable)
			return
		}
		time.Sleep(5 * time.Millisecond)
		w.Write([]byte("ok"))
	}))
	defer srv.Close()
	o := Options{Procs: 4, Steady: time.Second, Cold: 5}
	_, err := driveConns(context.Background(), srv.URL, "identity", "fake", func() string { return "" }, nodePath, ReadScenario, o, time.Now())
	if err == nil || !strings.Contains(err.Error(), "status 503") {
		t.Fatalf("got %v", err)
	}
}

func TestWritesAreCountedWithTheirOwnLatency(t *testing.T) {
	var patches atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			patches.Add(1)
			time.Sleep(4 * time.Millisecond)
		}
		w.Write([]byte("ok"))
	}))
	defer srv.Close()
	o := Options{Procs: 2, Steady: 300 * time.Millisecond}
	res, err := driveConns(context.Background(), srv.URL, "identity", "fake", func() string { return "" }, nodePath, ReadWriteScenario, o, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if patches.Load() == 0 || len(res.Writes) != int(patches.Load()) {
		t.Fatalf("server saw %d writes, result has %d", patches.Load(), len(res.Writes))
	}
	for _, l := range res.Writes {
		if l < 4 {
			t.Fatalf("a write took %.2f ms, the server held it for 4", l)
		}
	}
	if len(res.Steady) == 0 {
		t.Fatal("no reads")
	}
}
