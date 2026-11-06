package compare

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"
)

// BrowseOptions say how to run the browse comparison. Traces are the files `compare record`
// wrote for each framework, and the plans they were made from.
type BrowseOptions struct {
	TraceDir     string
	Seed         uint64
	Scenarios    []BrowseScenario
	Servers      []string // spec ids; the four frameworks when empty
	ServerCPUs   string
	LoadCPUs     string
	Rounds       int
	SearchWarm   time.Duration
	SearchWindow time.Duration
	Warm         time.Duration
	Window       time.Duration
	MaxUsers     int // the search stops here
	Log          io.Writer
}

// BrowseFrameworks are the specs the browse comparison runs: each framework as shipped.
var BrowseFrameworks = []string{"tifa", "nextjs", "start", "reactrouter"}

// CurvePoint is one step of the search for how many users a framework carries.
type CurvePoint struct {
	Users      int     `json:"users"`
	VisitsPerS float64 `json:"visitsPerSecond"`
	Errors     int     `json:"errors"`
	DocP99     float64 `json:"fullLoadP99Ms"`
	NavP99     float64 `json:"navigationP99Ms"`
	Pass       bool    `json:"pass"`
}

// BrowseRun is one framework in one round of one scenario, at the users the search chose.
type BrowseRun struct {
	Scenario  string      `json:"scenario"`
	Spec      string      `json:"spec"`
	Framework string      `json:"framework"`
	Label     string      `json:"label"`
	Round     int         `json:"round"`
	Load      *LoadResult `json:"load"`
}

// BrowseData is what `compare browse` adds to the record.
type BrowseData struct {
	Seed         uint64                  `json:"seed"`
	Traces       map[string]int          `json:"traces"` // by file name: the visits recorded
	Dropped      map[string]int          `json:"dropped,omitempty"`
	Curves       map[string][]CurvePoint `json:"curves"` // by scenario/spec
	Chosen       map[string]int          `json:"chosen"`
	Scenarios    []BrowseScenario        `json:"scenarios"`
	Runs         []BrowseRun             `json:"runs"`
	Warm         string                  `json:"warm"`
	Window       string                  `json:"window"`
	SearchWindow string                  `json:"searchWindow"`
}

// pass is the rule for a step of the search: no errors, and the P99 of a full load and of a whole
// navigation under a second.
func (c CurvePoint) passes() bool { return c.Errors == 0 && c.DocP99 <= 1000 && c.NavP99 <= 1000 }

type browseSet struct {
	plans  []Plan
	traces map[int]Trace
	file   string
	count  int
	drop   int
}

func loadBrowseSet(dir, spec string, mean int) (*browseSet, error) {
	var plans []Plan
	data, err := os.ReadFile(PlansPath(dir, mean))
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &plans); err != nil {
		return nil, err
	}
	file := TracePath(dir, spec, mean)
	traces, dropped, err := ReadTraces(file)
	if err != nil {
		return nil, err
	}
	set := &browseSet{plans: plans, traces: map[int]Trace{}, file: filepath.Base(file), count: len(traces), drop: dropped}
	for _, t := range traces {
		set.traces[t.Plan] = t
	}
	if len(set.traces) < 100 {
		return nil, fmt.Errorf("%s holds %d visits, too few for a replay that does not loop over the same few", file, len(set.traces))
	}
	return set, nil
}

// Browse runs the browse comparison and returns a record that holds it. Prepare must have run, and
// the traces must have been recorded against these builds.
func (e Env) Browse(ctx context.Context, o BrowseOptions) (*Record, error) {
	if o.Log == nil {
		o.Log = io.Discard
	}
	if len(o.Servers) == 0 {
		o.Servers = BrowseFrameworks
	}
	placement, err := PlacementFor(runtime.GOOS, SysTopology(), o.ServerCPUs, o.LoadCPUs)
	if err != nil {
		return nil, err
	}
	if o.LoadCPUs != "" {
		if err := pinSelf(o.LoadCPUs); err != nil {
			return nil, err
		}
	}
	var specs []Spec
	for _, name := range o.Servers {
		i := slices.IndexFunc(e.Specs(), func(s Spec) bool { return s.Name == name })
		if i < 0 {
			return nil, fmt.Errorf("no spec named %q", name)
		}
		specs = append(specs, e.Specs()[i])
	}
	buildID, err := e.tifaBuildID()
	if err != nil {
		return nil, err
	}
	rec := &Record{
		Date: time.Now().UTC().Format("2006-01-02 15:04:05 UTC"), Versions: e.versions(specs), Machine: machine(), Placement: placement,
		Settings: Settings{Rounds: o.Rounds, Steady: o.Window.String(), AcceptEncoding: "gzip"},
		Browse: &BrowseData{Seed: o.Seed, Traces: map[string]int{}, Dropped: map[string]int{}, Curves: map[string][]CurvePoint{}, Chosen: map[string]int{},
			Scenarios: o.Scenarios, Warm: o.Warm.String(), Window: o.Window.String(), SearchWindow: o.SearchWindow.String()},
	}
	fmt.Fprintf(o.Log, "placement: %s\n", placement.Note)
	for _, sc := range o.Scenarios {
		sets := map[string]*browseSet{}
		for _, s := range specs {
			set, err := loadBrowseSet(o.TraceDir, s.Name, sc.Mean)
			if err != nil {
				return nil, err
			}
			sets[s.Name] = set
			rec.Browse.Traces[set.file] = set.count
			if set.drop > 0 {
				rec.Browse.Dropped[set.file] = set.drop
			}
		}
		newReplayer := func(s Spec, server *Server) *replayer {
			r := &replayer{base: server.Base, write: s.Write, traces: sets[s.Name].traces, plans: sets[s.Name].plans, sc: sc, seed: o.Seed, cpu: server.CPUSeconds, rss: server.PeakRSS}
			if s.Framework == "tifa" {
				r.buildID = buildID
			}
			return r
		}
		for _, s := range specs {
			key := sc.Name + "/" + s.Name
			server, err := s.Launch(o.ServerCPUs)
			if err != nil {
				return nil, err
			}
			if err := server.WaitListening(ctx); err != nil {
				server.Stop()
				return nil, err
			}
			r := newReplayer(s, server)
			if err := r.verifyWrites(ctx); err != nil {
				server.Stop()
				return nil, fmt.Errorf("%s: %w", key, err)
			}
			curve, chosen, err := r.search(ctx, o)
			server.Stop()
			if err != nil {
				return nil, fmt.Errorf("%s: %w", key, err)
			}
			rec.Browse.Curves[key] = curve
			rec.Browse.Chosen[key] = chosen
			fmt.Fprintf(o.Log, "%s: chose %d users\n", key, chosen)
		}
		for round := 1; round <= o.Rounds; round++ {
			for _, s := range Rotate(specs, round-1) {
				key := sc.Name + "/" + s.Name
				if err := waitIdle(ctx, o.Log, o.ServerCPUs+","+o.LoadCPUs); err != nil {
					return nil, err
				}
				server, err := s.Launch(o.ServerCPUs)
				if err != nil {
					return nil, err
				}
				if err := server.WaitListening(ctx); err != nil {
					server.Stop()
					return nil, err
				}
				r := newReplayer(s, server)
				res, err := r.RunLoad(ctx, rec.Browse.Chosen[key], o.Warm, o.Window)
				if err == nil && round == o.Rounds {
					err = r.verifyWrites(ctx)
				}
				server.Stop()
				if err != nil {
					return nil, fmt.Errorf("%s round %d: %w", key, round, err)
				}
				rec.Browse.Runs = append(rec.Browse.Runs, BrowseRun{Scenario: sc.Name, Spec: s.Name, Framework: s.Framework, Label: s.Label, Round: round, Load: res})
				fmt.Fprintf(o.Log, "round %d %-9s %-12s %2d users %7.2f visits/s %6.1f ms CPU/visit, %d errors %s\n", round, sc.Name, s.Name, res.Users, float64(res.Visits)/res.Seconds, 1000*res.CPU/max(1, float64(res.Visits)), res.Errors, res.FirstErr)
			}
		}
	}
	return rec, nil
}

// search raises the users 1, 2, 4, ... until visits/s rises by under 3% twice in a row (a plateau, confirmed by one more step),
// or a step has errors or a P99 over a second, or o.MaxUsers is reached. It returns the whole curve
// and the highest step that passes.
func (r *replayer) search(ctx context.Context, o BrowseOptions) ([]CurvePoint, int, error) {
	var curve []CurvePoint
	prev, flat := 0.0, 0
	for users := 1; users <= max(o.MaxUsers, 1); users *= 2 {
		res, err := r.RunLoad(ctx, users, o.SearchWarm, o.SearchWindow)
		if err != nil {
			return nil, 0, err
		}
		p := CurvePoint{Users: users, VisitsPerS: float64(res.Visits) / res.Seconds, Errors: res.Errors, DocP99: res.Types["doc"].P99, NavP99: res.Types["nav"].P99}
		p.Pass = p.passes()
		curve = append(curve, p)
		fmt.Fprintf(o.Log, "  %2d users: %7.2f visits/s, %d errors, full load P99 %.0f ms, navigation P99 %.0f ms %s\n", users, p.VisitsPerS, p.Errors, p.DocP99, p.NavP99, res.FirstErr)
		if !p.Pass {
			break
		}
		if prev > 0 && p.VisitsPerS < prev*1.03 {
			flat++
		} else {
			flat = 0
		}
		if flat >= 2 {
			break
		}
		prev = p.VisitsPerS
	}
	chosen := 0
	for _, p := range curve {
		if p.Pass {
			chosen = p.Users
		}
	}
	if chosen == 0 {
		return curve, 0, fmt.Errorf("not even one user passes: %+v", curve)
	}
	return curve, chosen, nil
}

func (e Env) tifaBuildID() (string, error) {
	data, err := os.ReadFile(filepath.Join(e.Shop, ".tifa", "dist", "build.json"))
	if err != nil {
		return "", fmt.Errorf("build the shop first: %w", err)
	}
	var built struct {
		BuildID string `json:"buildId"`
	}
	if err := json.Unmarshal(data, &built); err != nil {
		return "", err
	}
	return built.BuildID, nil
}

// verifyWrites makes a write to each writable order, both kinds, and checks that the document of
// the order shows it and that so does the data a client navigation fetches for it (as the browser
// asked for it in a trace). It uses the addresses of the run, so the server is not what it was.
func (r *replayer) verifyWrites(ctx context.Context) error {
	client := &http.Client{}
	get := func(url string, headers map[string]string) (string, error) {
		req, err := http.NewRequestWithContext(ctx, "GET", r.base+url, nil)
		if err != nil {
			return "", err
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		req.Header.Set("Accept-Encoding", "identity")
		if headers != nil {
			req.Header.Set("Sec-Fetch-Site", "same-origin")
		}
		res, err := client.Do(req)
		if err != nil {
			return "", err
		}
		defer res.Body.Close()
		b, err := io.ReadAll(res.Body)
		if err != nil || res.StatusCode != 200 {
			return "", fmt.Errorf("GET %s: %d %v", url, res.StatusCode, err)
		}
		return string(b), nil
	}
	for _, fan := range []bool{false, true} {
		for _, w := range WritableOrders {
			n := int(r.writes.Add(1))
			st := WriteStep(r.write, w, n, fan)
			if err := Send(ctx, r.base, st); err != nil {
				return err
			}
			digits := fmt.Sprintf("%08d", n)
			url := "/projects/" + w.Project + "/orders/" + w.Order
			doc, err := get(url, nil)
			if err != nil {
				return err
			}
			if !strings.Contains(doc, digits) || fan && !strings.Contains(doc, st.Write.Customer) {
				return fmt.Errorf("the document of %s does not show write %d", w.Order, n)
			}
			q, ok := r.navRequest(url)
			if !ok {
				continue
			}
			if r.buildID != "" {
				q.URL = tifaBuild.ReplaceAllString(q.URL, "_tifa="+r.buildID+".")
			}
			body, err := get(q.URL, q.Headers)
			if err != nil {
				return err
			}
			if !strings.Contains(body, digits) {
				return fmt.Errorf("the navigation data of %s (%s) does not show write %d", w.Order, q.URL, n)
			}
		}
	}
	return nil
}

// navRequest finds, in the traces, the data request of a click to a page.
func (r *replayer) navRequest(page string) (TraceReq, bool) {
	for id := 0; id < len(r.plans); id++ {
		t, ok := r.traces[id]
		if !ok {
			continue
		}
		for _, s := range t.Steps {
			if s.Kind != StepClick || s.Page != page {
				continue
			}
			for _, q := range s.Reqs {
				if q.Class == "nav" && q.Method == "GET" && !strings.Contains(q.URL, "__manifest") {
					return q, true
				}
			}
		}
	}
	return TraceReq{}, false
}
