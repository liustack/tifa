package ssr_test

// How long the Go garbage collector stops the program while VMs render, and what that does to
// the renders. Build the shop first (see shop_test.go), then run:
//
//	TIFA_SSR_SHOP=1 go test -run ShopGCPause -v ./test/ssr
//	TIFA_SSR_SHOP=1 TIFA_SSR_MEASURE=1 go test -run ShopGCPause -v ./test/ssr
//
// A pool of TIFA_SSR_VMS VMs (2) renders the shop's pages without a pause while one goroutine
// allocates TIFA_SSR_ALLOC_MB megabytes a second (512) and drops them, so that the collector runs
// all the time. The run reports the pauses from runtime/metrics: how many, the P50, the P99 and the
// largest, both for the whole stop and for the time the collector waited to stop every goroutine
// ("stopping"), which is where a VM that sits inside one long operation shows. It reports the
// renders a second and the P50 and P99 of a render too. By default the run lasts a few seconds, with
// TIFA_SSR_MEASURE=1 it lasts TIFA_SSR_SECS (60) after a warm-up of 10 seconds.

import (
	"context"
	"fmt"
	"math"
	"os"
	"runtime"
	"runtime/debug"
	"runtime/metrics"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type histo struct {
	counts []uint64
	bounds []float64
}

func readHisto(name string) histo {
	s := []metrics.Sample{{Name: name}}
	metrics.Read(s)
	h := s[0].Value.Float64Histogram()
	return histo{append([]uint64(nil), h.Counts...), h.Buckets}
}

// pauseStats is what a histogram of pauses says about the pauses between two readings, in
// microseconds. P50, P99 and Max are the upper bounds of the buckets they fall in.
type pauseStats struct {
	N             uint64
	P50, P99, Max float64
}

// delta returns the statistics of the pauses that happened between before and after.
func (after histo) delta(before histo) pauseStats {
	c := make([]uint64, len(after.counts))
	var s pauseStats
	for i := range c {
		c[i] = after.counts[i] - before.counts[i]
		s.N += c[i]
	}
	at := func(p float64) float64 {
		target := uint64(math.Ceil(float64(s.N) * p))
		var cum uint64
		for i, k := range c {
			cum += k
			if k > 0 && cum >= target {
				return after.bounds[i+1] * 1e6
			}
		}
		return 0
	}
	s.P50, s.P99 = at(.5), at(.99)
	for i := len(c) - 1; i >= 0; i-- {
		if c[i] > 0 {
			s.Max = after.bounds[i+1] * 1e6
			break
		}
	}
	return s
}

func TestHistoDelta(t *testing.T) {
	// Buckets: [0,1ms) [1ms,2ms) [2ms,4ms)
	bounds := []float64{0, 0.001, 0.002, 0.004}
	before := histo{[]uint64{5, 0, 0}, bounds}
	after := histo{[]uint64{5, 90, 10}, bounds}
	s := after.delta(before)
	if s.N != 100 || s.P50 != 2000 || s.P99 != 4000 || s.Max != 4000 {
		t.Errorf("stats = %+v", s)
	}
	if z := after.delta(after); z.N != 0 || z.P50 != 0 || z.Max != 0 {
		t.Errorf("no pauses gave %+v", z)
	}
}

func quantile(sorted []time.Duration, q float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	return sorted[min(len(sorted)-1, int(q*float64(len(sorted))))]
}

func TestQuantile(t *testing.T) {
	d := []time.Duration{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	if quantile(d, .5) != 6 || quantile(d, .99) != 10 || quantile(nil, .5) != 0 {
		t.Error("quantile")
	}
}

func TestShopGCPause(t *testing.T) {
	source, pages := shopBundle(t)
	vms := envInt("TIFA_SSR_VMS", 2)
	allocMB := envInt("TIFA_SSR_ALLOC_MB", 512)
	warmup, secs := 1*time.Second, envInt("TIFA_SSR_SECS", 3)
	if os.Getenv("TIFA_SSR_MEASURE") != "" {
		warmup, secs = 10*time.Second, envInt("TIFA_SSR_SECS", 60)
	}
	cfg := shopConfig(vms)
	cfg.Timeout = 10 * time.Second
	cfg.QueueLimit = 256
	cfg.WaitTimeout = 10 * time.Second
	p := newPool(t, source, cfg)
	old := debug.SetGCPercent(100)
	defer debug.SetGCPercent(old)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	var next, bad atomic.Int64
	lat := make([][]time.Duration, vms)
	counting := make(chan struct{})
	for w := range vms {
		wg.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
				}
				page := pages[int(next.Add(1)-1)%len(pages)]
				begin := time.Now()
				html, err := p.Render(context.Background(), page.input)
				d := time.Since(begin)
				select {
				case <-counting:
					lat[w] = append(lat[w], d)
				default:
				}
				if err != nil || !page.matches(html) {
					bad.Add(1)
				}
			}
		})
	}
	if allocMB > 0 {
		wg.Go(func() {
			// The linked nodes give the collector something to mark besides the garbage.
			type node struct{ next, other *node }
			nodes := make([]*node, 100000)
			for j := range nodes {
				nodes[j] = &node{}
				if j > 0 {
					nodes[j].next = nodes[j-1]
				}
			}
			ring := make([][]byte, 64)
			tick := time.NewTicker(2 * time.Millisecond)
			defer tick.Stop()
			chunk := allocMB << 20 / 500
			for i := 0; ; i++ {
				select {
				case <-stop:
					return
				case <-tick.C:
				}
				nodes[i%len(nodes)].other = nodes[(i*7919)%len(nodes)]
				buf := make([]byte, chunk)
				buf[0] = byte(i)
				ring[i%len(ring)] = buf
			}
		})
	}
	time.Sleep(warmup)
	total0, stopping0 := readHisto("/sched/pauses/total/gc:seconds"), readHisto("/sched/pauses/stopping/gc:seconds")
	var ms0 runtime.MemStats
	runtime.ReadMemStats(&ms0)
	close(counting)
	begin := time.Now()
	time.Sleep(time.Duration(secs) * time.Second)
	elapsed := time.Since(begin).Seconds()
	total1, stopping1 := readHisto("/sched/pauses/total/gc:seconds"), readHisto("/sched/pauses/stopping/gc:seconds")
	var ms1 runtime.MemStats
	runtime.ReadMemStats(&ms1)
	close(stop)
	wg.Wait()

	all := slices.Concat(lat...)
	slices.Sort(all)
	if bad.Load() != 0 {
		t.Errorf("%d renders failed or gave the wrong page", bad.Load())
	}
	if len(all) == 0 {
		t.Fatal("no render finished")
	}
	total, stopping := total1.delta(total0), stopping1.delta(stopping0)
	gcs := ms1.NumGC - ms0.NumGC
	t.Logf("vms=%d gomaxprocs=%d secs=%.0f gc-cycles=%d (%.1f/s)", vms, runtime.GOMAXPROCS(0), elapsed, gcs, float64(gcs)/elapsed)
	t.Logf("pauses in total: n=%d p50=%.1fus p99=%.1fus max=%.1fus", total.N, total.P50, total.P99, total.Max)
	t.Logf("pauses stopping the world: n=%d p50=%.1fus p99=%.1fus max=%.1fus", stopping.N, stopping.P50, stopping.P99, stopping.Max)
	t.Logf("renders=%d (%.1f/s) p50=%.2fms p99=%.2fms", len(all), float64(len(all))/elapsed,
		float64(quantile(all, .5))/1e6, float64(quantile(all, .99))/1e6)
	fmt.Printf("gc-pause: vms %d total-p99-us %.1f total-max-us %.1f stopping-p99-us %.1f stopping-max-us %.1f render-p50-ms %.2f render-p99-ms %.2f\n",
		vms, total.P99, total.Max, stopping.P99, stopping.Max, float64(quantile(all, .5))/1e6, float64(quantile(all, .99))/1e6)
}
