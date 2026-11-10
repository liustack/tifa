package compare

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// crawlerShop is a runner of the plans of one block of the site, with a trace of a document for every
// page view, against a fake server that answers the old URLs of crawlers with 404 and everything else
// with 200, and notes the requests of crawlers apart. The scenario has no writes.
type crawlerShop struct {
	r        *runner
	srv      *httptest.Server
	mu       sync.Mutex
	crawlers []crawlSeen
	others   int
}

type crawlSeen struct {
	url, ua, cookie, inm, accept, encoding string
	at                                     time.Time
}

func newCrawlerShop(t *testing.T) *crawlerShop {
	t.Helper()
	wl := siteWorkload(t, 2, 1, "shop")
	wl.Scenario.ReadOnly = true
	for _, p := range wl.Plans {
		wl.Traces[p.ID] = traceOf(p)
	}
	_, old := CrawlURLs()
	cs := &crawlerShop{}
	cs.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua := r.Header.Get("User-Agent")
		if slices.ContainsFunc(CrawlerAgents, func(a CrawlerAgent) bool { return a.UA == ua }) {
			cs.mu.Lock()
			cs.crawlers = append(cs.crawlers, crawlSeen{r.URL.RequestURI(), ua, r.Header.Get("Cookie"), r.Header.Get("If-None-Match"), r.Header.Get("Accept"), r.Header.Get("Accept-Encoding"), time.Now()})
			cs.mu.Unlock()
		} else {
			cs.mu.Lock()
			cs.others++
			cs.mu.Unlock()
		}
		if slices.Contains(old, r.URL.RequestURI()) {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte("ok"))
	}))
	t.Cleanup(cs.srv.Close)
	cs.r = &runner{base: cs.srv.URL, write: nodePath, create: nodeCreate, storeWrite: samePath, products: nodeProducts, editProduct: nodeEditProduct, wl: wl, seed: 2,
		cpu: func() float64 { return 0 }, rss: func() int64 { return 0 }}
	return cs
}

// Crawlers are 9% of the HTML requests: in every block of the cycle, the visits of people (each has one
// full load) are 1020 and the crawler requests 101, at places of a shuffle, and the two keep their
// order. Which slot a crawler request takes does not depend on how fast a server answers: the share
// is exact in every block.
func TestCrawlersAreNinePercentOfTheHTMLRequests(t *testing.T) {
	w := siteWorkload(t, 2, 3, "shop")
	bv := w.blockVisits()
	if bv != 1020 || w.Scenario.crawlerSlots() != 101 {
		t.Fatalf("a block has %d visits and %d crawler requests, want 1020 and 101", bv, w.Scenario.crawlerSlots())
	}
	per := bv + 101
	if share := 101.0 / float64(per); math.Abs(share-CrawlerShare) > 0.0005 {
		t.Errorf("crawlers are %.4f of the HTML requests, want about %v", share, CrawlerShare)
	}
	// Over two cycles of the stream, the slots are the visits in order and the crawler requests in order.
	humans, crawls := 0, 0
	for blk := range 6 {
		inBlock := 0
		for t0 := blk * per; t0 < (blk+1)*per; t0++ {
			crawler, n := w.slot(t0)
			switch {
			case crawler && n == crawls:
				crawls++
				inBlock++
			case !crawler && n == humans:
				humans++
			default:
				t.Fatalf("slot %d is a crawler %v number %d, want the next one (%d visits and %d crawler requests so far)", t0, crawler, n, humans, crawls)
			}
		}
		if inBlock != 101 {
			t.Errorf("block %d has %d crawler requests, want 101", blk, inBlock)
		}
	}
	if humans != 6*bv || crawls != 6*101 {
		t.Errorf("two cycles of three blocks hold %d visits and %d crawler requests", humans, crawls)
	}
	// A round of whole blocks has the slots of its blocks.
	for _, c := range []struct{ seq0, n, want int }{{0, bv, per}, {bv, 2 * bv, 2 * per}, {3 * bv, 3 * bv, 3 * per}, {0, 6 * bv, 6 * per}} {
		if got := w.slots(c.seq0, c.n); got != c.want {
			t.Errorf("visits %d to %d have %d slots, want %d", c.seq0, c.seq0+c.n-1, got, c.want)
		}
	}
	var r Round
	w.plan(&r, bv, 2*bv)
	if r.Crawler == nil || r.Crawler.Planned != 202 || r.Mix.Crawlers != 202 {
		t.Errorf("two blocks plan %+v crawler requests", r.Crawler)
	}
	// A hand-made workload has no crawlers.
	if h := (&Workload{Plans: []Plan{{}}}); h.slots(0, 5) != 5 {
		t.Error("a workload with no cycle has crawler requests")
	}
}

// A crawler request is one GET of a document with the User-Agent of a crawler, no cookie and no
// validator, that accepts HTML and gzip. The round plays every slot of its blocks: the requests that
// reach the server are the 101 of a block with the headers of a crawler, and none of them is a visit.
func TestACrawlerRequestIsOneDocumentWithNoCookie(t *testing.T) {
	cs := newCrawlerShop(t)
	round := &Round{}
	cs.r.playClosed(context.Background(), round, 8, 0, cs.r.wl.blockVisits(), time.Minute, nil)
	if len(cs.crawlers) != 101 {
		t.Fatalf("the server saw %d requests of crawlers, want 101", len(cs.crawlers))
	}
	public, old := CrawlURLs()
	for _, c := range cs.crawlers {
		if c.cookie != "" || c.inm != "" || c.accept != "text/html" || c.encoding != "gzip" {
			t.Errorf("%s: cookie %q, If-None-Match %q, Accept %q, Accept-Encoding %q", c.url, c.cookie, c.inm, c.accept, c.encoding)
		}
		if !slices.Contains(public, c.url) && !slices.Contains(old, c.url) {
			t.Errorf("a crawler asked for %s, which is in neither U nor O", c.url)
		}
	}
	if c := round.Crawler; c == nil || c.Planned != 101 || c.Done != 101 || c.Failed != 0 || round.Failures != 0 {
		t.Fatalf("the round counts %+v crawler requests, failures %d (%s)", c, round.Failures, round.FirstErr)
	}
	// A redirect is not followed and is not what was expected: it fails the request.
	redirecting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/elsewhere", 301) }))
	defer redirecting.Close()
	cs.r.base = redirecting.URL
	a := newAcc()
	cs.r.runCrawl(context.Background(), http.DefaultTransport.(*http.Transport), a, 0, time.Now())
	if a.crawl.failed != 1 || a.failures != 1 || !strings.Contains(a.firstErr, "status 301") {
		t.Errorf("a redirect: %d failed, %d failures (%s)", a.crawl.failed, a.failures, a.firstErr)
	}
}

// A crawler request is not a visit and not a page view: the visits, the page views, the requests and
// the bytes of a round count people alone, and the crawler requests have their own counters and a
// latency of their own.
func TestCrawlerRequestsAreNotPageViews(t *testing.T) {
	cs := newCrawlerShop(t)
	round := &Round{}
	cs.r.playClosed(context.Background(), round, 8, 0, cs.r.wl.blockVisits(), time.Minute, nil)
	if round.VisitsPlanned != 1020 || round.VisitsStarted != 1020 || round.Visits != 1020 {
		t.Errorf("visits planned %d, started %d, done %d, want 1020", round.VisitsPlanned, round.VisitsStarted, round.Visits)
	}
	if round.Requests != round.PageViews || round.PageViews != round.FullLoads+round.Clicks || round.FullLoads != 1020 {
		t.Errorf("%d requests, %d page views, %d full loads: a crawler request was counted as one", round.Requests, round.PageViews, round.FullLoads)
	}
	if n := round.Types["crawler"].N; n != 101 {
		t.Errorf("%d crawler latencies, want 101", n)
	}
	if round.Types["load"].N != 1020 {
		t.Errorf("%d full load latencies: the crawler requests are in them", round.Types["load"].N)
	}
	if want := int64(2*round.Crawler.Statuses["U 200"] + len("404 page not found\n")*round.Crawler.Statuses["O 404"]); round.Crawler.Bytes != want || want == 0 {
		t.Errorf("the crawler requests read %d bytes, want %d", round.Crawler.Bytes, want)
	}
	if round.Mix == nil || round.Mix.Crawlers != 101 {
		t.Fatalf("the planned mix has %+v", round.Mix)
	}
	if why := round.Mix.why(cs.r.wl.Scenario); why != "" {
		t.Error(why)
	}
	// The statuses: public URLs answer 200 and old ones 404, as expected.
	for k := range round.Crawler.Statuses {
		if k != "U 200" && k != "O 404" {
			t.Errorf("the crawlers got %s", k)
		}
	}
}

// In the open model the crawler requests arrive with the visits, in the proportion of the stream, over
// the whole of the round, and the rate stays in visits of people a second.
func TestCrawlerRequestsArriveWithTheVisitsInTheOpenModel(t *testing.T) {
	cs := newCrawlerShop(t)
	// One page view a visit, so that the round does not take the seconds the actions of a long visit are spaced by.
	for i := range cs.r.wl.Plans {
		p := &cs.r.wl.Plans[i]
		p.Steps = p.Steps[:1]
		tr := cs.r.wl.Traces[p.ID]
		tr.Steps = tr.Steps[:1]
		cs.r.wl.Traces[p.ID] = tr
	}
	round := &Round{}
	rate := 1500.0
	start := time.Now()
	cs.r.playOpen(context.Background(), round, rate, 1020, 5*time.Second, 0, nil)
	if len(cs.crawlers) != 101 {
		t.Fatalf("the server saw %d requests of crawlers, want 101", len(cs.crawlers))
	}
	span := 1020 / rate
	var first, last time.Duration = time.Hour, 0
	for _, c := range cs.crawlers {
		d := c.at.Sub(start)
		first, last = min(first, d), max(last, d)
	}
	if first.Seconds() > 0.2*span || last.Seconds() < 0.6*span {
		t.Errorf("the crawler requests arrive from %v to %v of a round of %.2f s: not spread over it", first, last, span)
	}
	if round.Crawler == nil || round.Crawler.Done != 101 || round.VisitsStarted != 1020 || round.Failures != 0 {
		t.Errorf("crawler %+v, %d visits started, %d failures (%s)", round.Crawler, round.VisitsStarted, round.Failures, round.FirstErr)
	}
}

// The request number c is the same for every server and every round: the URL and the crawler come from the
// seed and the number alone. The URLs are uniform over U, with CrawlOldShare from O, and a long run
// does not go round a short list: every URL of U is asked for.
func TestEveryServerIsAskedByTheSameCrawlers(t *testing.T) {
	a, b, c := siteWorkload(t, 2, 1, "shop"), siteWorkload(t, 2, 1, "shop"), siteWorkload(t, 3, 1, "shop")
	public, old := CrawlURLs()
	seen := map[string]int{}
	oldN, differs := 0, 0
	classes, agents := map[string]int{}, map[string]int{}
	const n = 40_000
	for i := range n {
		x := a.crawlOf(i)
		if x != b.crawlOf(i) {
			t.Fatalf("request %d is not the same in two workloads of one seed", i)
		}
		if x != c.crawlOf(i) {
			differs++
		}
		seen[x.URL]++
		if x.Old {
			oldN++
			if !slices.Contains(old, x.URL) {
				t.Fatalf("%s is no old URL", x.URL)
			}
		}
		classes[x.Agent.Class]++
		agents[x.Agent.Name]++
	}
	if differs < n/2 {
		t.Errorf("another seed gives the same request %d times of %d", n-differs, n)
	}
	if share := float64(oldN) / n; math.Abs(share-CrawlOldShare) > 0.005 {
		t.Errorf("%.4f of the requests are old URLs, want %v", share, CrawlOldShare)
	}
	for _, u := range public {
		if seen[u] == 0 {
			t.Errorf("%s is never asked for in %d requests", u, n)
		}
	}
	want := CrawlerClasses[0].Share / (CrawlerClasses[0].Share + CrawlerClasses[1].Share)
	if got := float64(classes[CrawlerClasses[0].Name]) / n; math.Abs(got-want) > 0.01 {
		t.Errorf("%s crawlers are %.3f of the requests, want %.3f", CrawlerClasses[0].Name, got, want)
	}
	for _, ag := range CrawlerAgents {
		if agents[ag.Name] == 0 {
			t.Errorf("%s never asks", ag.Name)
		}
	}
}

// A request that has not the status expected, or fails, fails the round (the crawler gets a 200 for a
// public URL and a 404 for an old one), and the P99 of the crawler requests is reported and is no part
// of the rule of the round, as the rule is for people who wait.
func TestAFailedCrawlerRequestFailsTheRoundAndItsLatencyDoesNot(t *testing.T) {
	sc := BrowseScenario{}
	slow := plannedRound()
	slow.Types["crawler"] = TypeStat{N: 101, P50: 900, P99: 8000}
	if slow.Judge(sc); !slow.Pass {
		t.Errorf("a P99 of the crawler requests of 8 s fails the round: %s", slow.Why)
	}
	a := newAcc()
	a.crawlFail(http_error("crawler GET /store: status 500, want 200"))
	if a.failures != 1 || a.failedVisits != 0 || a.crawl.failed != 1 {
		t.Fatalf("a failed crawler request counts %d failures, %d failed visits, %d failed crawler requests", a.failures, a.failedVisits, a.crawl.failed)
	}
	round := plannedRound()
	round.Failures, round.FirstErr = a.failures, a.firstErr
	if round.Judge(sc); round.Pass || !strings.Contains(round.Why, "crawler") || !round.Valid {
		t.Errorf("a failed crawler request: pass %v, valid %v, why %q", round.Pass, round.Valid, round.Why)
	}
}

type http_error string

func (e http_error) Error() string { return string(e) }

// The check for caches (R9) asks a document as every crawler: Node frameworks answer a crawler in
// another way, and a sign of a cache in that answer is found too.
func TestTheCheckForCachesAsksAsEveryCrawler(t *testing.T) {
	reqs := crawlerGets()
	if len(reqs) < len(CrawlerAgents) {
		t.Fatalf("%d requests for %d crawlers", len(reqs), len(CrawlerAgents))
	}
	for _, a := range CrawlerAgents {
		if !slices.ContainsFunc(reqs, func(q TraceReq) bool { return q.Headers["User-Agent"] == a.UA && q.Class == "doc" && q.Method == "GET" }) {
			t.Errorf("no request as %s", a.Name)
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.Header.Get("User-Agent"), "GPTBot") {
			w.Header().Set("ETag", `"x"`)
		}
		w.Write([]byte("ok"))
	}))
	defer srv.Close()
	if err := CheckNoCache(context.Background(), srv.URL, reqs); err == nil || !strings.Contains(err.Error(), "ETag") {
		t.Errorf("an ETag sent to GPTBot passed the check: %v", err)
	}
}

// answerOld answers a GET of an old URL of crawlers (the set O) with a 404, as the shop does, and says
// whether it did. The fake servers of the other tests call it, since a round now has crawler requests.
func answerOld(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodGet || !oldURLs()[r.URL.RequestURI()] {
		return false
	}
	http.NotFound(w, r)
	return true
}

var oldURLs = sync.OnceValue(func() map[string]bool {
	_, old := CrawlURLs()
	set := map[string]bool{}
	for _, u := range old {
		set[u] = true
	}
	return set
})

// A status that is not the one expected is a failure as soon as the headers arrive, and a body that
// then does not end before the round does is no reason to lose it, nor to count it twice. The server
// sends the status and holds the body until the request is cancelled.
func TestACrawlerStatusIsJudgedWhenTheHeadersArrive(t *testing.T) {
	_, old := CrawlURLs()
	for _, tc := range []struct {
		name   string
		status int
		url    string
	}{{"a 500 for a public URL", 500, "/store"}, {"a 200 for an old URL", 200, old[0]}, {"a redirect", 302, "/store"}} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.status)
			w.Write([]byte("start"))
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		}))
		cs := newCrawlerShop(t)
		cs.r.base = srv.URL
		// Find the crawler request that asks for the URL of the case.
		c := 0
		for ; c < 1_000_000; c++ {
			if q := cs.r.wl.crawlOf(c); q.URL == tc.url {
				break
			}
		}
		if c == 1_000_000 {
			t.Fatalf("%s: no crawler request asks for %s", tc.name, tc.url)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		a := newAcc()
		cs.r.runCrawl(ctx, http.DefaultTransport.(*http.Transport), a, c, time.Now())
		cancel()
		srv.Close()
		if a.crawl.failed != 1 || a.failures != 1 || a.crawl.done != 0 || !strings.Contains(a.firstErr, "status") {
			t.Errorf("%s: %d failed, %d failures, %d done (%s)", tc.name, a.crawl.failed, a.failures, a.crawl.done, a.firstErr)
		}
	}
}

// The visits in flight that the backlog is judged on are those of people. With the crawler requests
// in flight counted in, 400 steady ones raise the threshold of the growth so that a backlog of people
// goes unseen, which counting people alone does not.
func TestABacklogIsJudgedOnTheVisitsOfPeopleAlone(t *testing.T) {
	var f flight
	var people, mixed []float64
	for i := range 60 {
		// 400 crawler requests in flight all along, and a visit of people more in flight every sample.
		if i == 0 {
			for range 400 {
				f.start(true)
			}
		}
		f.start(false)
		people = append(people, float64(f.visits()))
		mixed = append(mixed, float64(f.visits()+f.crawlers.Load()))
	}
	if !Growing(people, 2) {
		t.Error("a pile of visits of people that grows by 1 a sample is not a backlog")
	}
	if Growing(mixed, 2) {
		t.Error("the check no longer shows why people alone are counted: the mixed series was judged growing")
	}
	f.end(true)
	if f.crawlers.Load() != 399 || f.visits() != 60 {
		t.Errorf("%d crawler requests and %d visits in flight", f.crawlers.Load(), f.visits())
	}
}
