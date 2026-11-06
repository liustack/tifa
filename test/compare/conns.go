package compare

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"sync"
	"time"
)

// connStride is how far apart the connections start in the scenario. It is prime to 6 and 10, so
// connection i starts i pages further on and writes at other reads than the others do, and it is
// larger than the reads of any run, so no two connections write the same address.
const connStride = 1_000_003

// ConnOffset is the unit of work that connection i starts the scenario at.
func ConnOffset(i int) int { return i * connStride }

// WithProcs gives a Tifa spec n VMs and n CPUs for the Go runtime (--ssr-vms and GOMAXPROCS), where
// the spec has one of each. The other frameworks run one process and take only 1.
func (s Spec) WithProcs(n int) (Spec, error) {
	if n <= 1 {
		return s, nil
	}
	if s.Framework != "tifa" {
		return s, fmt.Errorf("%s runs one process: only Tifa takes more than 1 proc", s.Name)
	}
	s.Args = slices.Clone(s.Args)
	s.Env = slices.Clone(s.Env)
	if i := slices.Index(s.Args, "--ssr-vms"); i >= 0 && i+1 < len(s.Args) {
		s.Args[i+1] = strconv.Itoa(n)
	}
	if i := slices.Index(s.Env, "GOMAXPROCS=1"); i >= 0 {
		s.Env[i] = "GOMAXPROCS=" + strconv.Itoa(n)
	}
	return s, nil
}

// connRun is what one connection saw.
type connRun struct {
	Cold, Steady         []float64 // latencies in ms
	Writes               []float64 // latencies in ms of the writes of the steady phase
	ColdPage, SteadyPage []int
	Wire                 map[int]PageWire
	Renders              map[string]int
	First                time.Duration // from t0 to its first answer
	Elapsed              float64       // seconds of its steady phase
}

// driven is what all the connections saw. Requests is the sum of their rates in the steady phase.
type driven struct {
	Conns      []*connRun
	Steady     []float64
	SteadyPage []int
	Writes     []float64
	Requests   float64
	Renders    map[string]int
	Wire       []PageWire
}

// driveConns runs the scenario on o.Conns connections (o.Procs when 0, and 1 when that is 0), each its own client with its own
// offset (ConnOffset), each with a cold series and then the steady phase. output is the server's
// output, for an error.
func driveConns(ctx context.Context, base, acceptEncoding, name string, output func() string, w Writer, sc Scenario, o Options, t0 time.Time) (*driven, error) {
	n := o.Conns
	if n == 0 {
		n = max(o.Procs, 1)
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	conns := make([]*connRun, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			conns[i], errs[i] = runConn(ctx, newClient(base, acceptEncoding), name, output, w, sc, o, ConnOffset(i), t0)
			if errs[i] != nil {
				cancel()
			}
		}()
	}
	wg.Wait()
	// A connection that fails cancels the others, so report the first error that is not a cancel.
	var first error
	for _, err := range errs {
		if err == nil {
			continue
		}
		if !errors.Is(err, context.Canceled) {
			return nil, err
		}
		first = cmp.Or(first, err)
	}
	if first != nil {
		return nil, first
	}
	d := &driven{Conns: conns, Renders: map[string]int{}}
	wire := map[int]PageWire{}
	for _, c := range conns {
		d.Steady = append(d.Steady, c.Steady...)
		d.SteadyPage = append(d.SteadyPage, c.SteadyPage...)
		d.Writes = append(d.Writes, c.Writes...)
		if len(c.Steady) > 0 {
			d.Requests += float64(len(c.Steady)) / c.Elapsed
		}
		for k, v := range c.Renders {
			d.Renders[k] += v
		}
		for p, v := range c.Wire {
			if _, ok := wire[p]; !ok {
				wire[p] = v
			}
		}
	}
	for i := range Pages {
		if v, ok := wire[i]; ok {
			d.Wire = append(d.Wire, v)
		}
	}
	return d, nil
}

// runConn is one connection: Cold units of work, then units for o.Steady. The steady phase keeps
// the latency of its writes apart from its timed reads. Its rate counts the time of both.
func runConn(ctx context.Context, c *client, name string, output func() string, w Writer, sc Scenario, o Options, offset int, t0 time.Time) (*connRun, error) {
	r := &connRun{Wire: map[int]PageWire{}, Renders: map[string]int{}}
	read := func(n int, lat *[]float64, pages *[]int, writes *[]float64) error {
		for _, st := range sc.Steps(w, offset+n) {
			if o.UniqueURLs && st.Timed {
				st.Path += "?_=" + strconv.Itoa(offset+n)
			}
			a, err := c.do(ctx, st)
			if err != nil {
				return fmt.Errorf("%s: request %d %s %s: %w\n%s", name, n+1, st.Method, st.Path, err, output())
			}
			if st.Write != nil && writes != nil {
				*writes = append(*writes, float64(a.Latency)/float64(time.Millisecond))
			}
			if st.Timed {
				*lat = append(*lat, float64(a.Latency)/float64(time.Millisecond))
				if _, ok := r.Wire[st.Page]; !ok {
					r.Wire[st.Page] = PageWire{Page: Pages[st.Page].Name, Bytes: a.Bytes, Encoding: a.Encoding}
				}
				*pages = append(*pages, st.Page)
				if a.Render != "" {
					r.Renders[a.Render]++
				}
			}
		}
		return nil
	}
	n := 0
	for ; n < o.Cold; n++ {
		if err := read(n, &r.Cold, &r.ColdPage, nil); err != nil {
			return nil, err
		}
		if n == 0 {
			r.First = time.Since(t0)
		}
	}
	start := time.Now()
	deadline := start.Add(o.Steady)
	for time.Now().Before(deadline) {
		if err := read(n, &r.Steady, &r.SteadyPage, &r.Writes); err != nil {
			return nil, err
		}
		n++
	}
	r.Elapsed = time.Since(start).Seconds()
	return r, nil
}
