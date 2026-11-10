package compare

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// crawlerShop is a runner of the plans of one block of the site, with a trace of one request for every
// page view (a document for a landing, navigation data for a click), against a fake server that answers the old URLs of crawlers with 404 and everything else
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

// Crawlers are 9% of the HTML requests, which are the documents the browsers of people ask for and the
// requests of crawlers. The documents are counted from the traces. With traces whose landings alone
// are documents, every block of the cycle has 1020 documents, one for each visit, and 101 crawler
// requests, at places of a shuffle, and the visits and the crawler requests keep their order. Which
// slot a crawler request takes does not depend on how fast a server answers: the share is exact in
// every block.
func TestCrawlersAreNinePercentOfTheHTMLRequests(t *testing.T) {
	w := siteWorkload(t, 2, 3, "shop")
	bv := w.blockVisits()
	if bv != 1020 || w.Scenario.crawlerSlots(1020) != 101 {
		t.Fatalf("a block has %d visits, and 1020 documents go with %d crawler requests, want 1020 and 101", bv, w.Scenario.crawlerSlots(1020))
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
	if r.Crawler == nil || r.Crawler.Planned != 202 || r.Mix.Crawlers != 202 || r.Mix.Documents != 2040 {
		t.Errorf("two blocks plan %+v crawler requests for %d documents of people", r.Crawler, r.Mix.Documents)
	}
	// A hand-made workload has no crawlers.
	if h := (&Workload{Plans: []Plan{{}}}); h.slots(0, 5) != 5 {
		t.Error("a workload with no cycle has crawler requests")
	}
}

// A click that loads a document is an HTML request of a person, so the plans that have such clicks
// bring more crawler requests: each block has the crawler requests that make 9% with the documents of
// its own visits, counted from the traces, and the blocks differ when their documents do.
func TestTheCrawlerRequestsOfABlockFollowTheDocumentsOfItsVisits(t *testing.T) {
	const blocks = 3
	w := siteWorkloadOf(t, 2, blocks, "shop", func(p Plan) Trace {
		if p.Role != RoleShopper {
			return browserTrace(p)
		}
		return browserTrace(p, listClicks(p)...)
	})
	bv := w.blockVisits()
	staff := w.Scenario.staffVisits()
	// The documents of each block, counted here from the plans: one for each visit, and one for each
	// click of a shopper plan to a list.
	docsOf := make([]int, blocks)
	for _, p := range w.Plans {
		if p.Role == RoleShopper {
			docsOf[p.Block] += 1 + len(listClicks(p))
		}
	}
	differ := false
	t0, seq := 0, 0
	for cycle := range 2 {
		for b := range blocks {
			docs := docsOf[b] + staff
			want := int(math.Round(float64(docs) * CrawlerShare / (1 - CrawlerShare)))
			if want <= 101 {
				t.Fatalf("block %d has %d documents and %d crawler requests: no more than with the landings alone", b, docs, want)
			}
			differ = differ || b > 0 && docs != docsOf[0]+staff
			if got := w.slots(seq, bv); got != bv+want {
				t.Errorf("cycle %d block %d: %d slots for %d visits with %d documents, want %d crawler requests", cycle, b, got, bv, docs, want)
			}
			crawlers, visits := 0, 0
			for range bv + want {
				if crawler, _ := w.slot(t0); crawler {
					crawlers++
				} else {
					visits++
				}
				t0++
			}
			if crawlers != want || visits != bv {
				t.Errorf("cycle %d block %d: its slots hold %d visits and %d crawler requests, want %d and %d", cycle, b, visits, crawlers, bv, want)
			}
			if share := float64(want) / float64(docs+want); math.Abs(share-CrawlerShare) > 0.0005 {
				t.Errorf("block %d: crawlers are %.4f of the HTML requests, want about %v", b, share, CrawlerShare)
			}
			// The round of the block plans its documents and its crawler requests, and has the mix.
			var r Round
			w.plan(&r, seq, bv)
			if r.Mix.Documents != docs || r.Mix.Crawlers != want || r.Crawler.Planned != want {
				t.Errorf("cycle %d block %d plans %d documents and %d crawler requests, want %d and %d", cycle, b, r.Mix.Documents, r.Mix.Crawlers, docs, want)
			}
			if why := r.Mix.why(w.Scenario); why != "" {
				t.Errorf("cycle %d block %d: %s", cycle, b, why)
			}
			seq += bv
		}
	}
	if !differ {
		t.Error("every block has the documents of the first: the test does not show that a block has its own")
	}
	// The stream numbers the visits and the crawler requests in order over the cycles.
	humans, crawls := 0, 0
	for i := range t0 {
		crawler, n := w.slot(i)
		switch {
		case crawler && n == crawls:
			crawls++
		case !crawler && n == humans:
			humans++
		default:
			t.Fatalf("slot %d is a crawler %v number %d, want the next one (%d visits and %d crawler requests so far)", i, crawler, n, humans, crawls)
		}
	}
}

// The staff visits of a block play other staff plans in every cycle, so a staff plan with a click that
// loads a document would give a block other documents from cycle to cycle. The crawler requests of a
// block are the same in every cycle, so such a set of traces is refused.
func TestAStaffPlanWithAClickThatLoadsADocumentIsRefused(t *testing.T) {
	set := GenerateSitePlanSet(2, 1)
	traces := browserTraces(set.Plans)
	var staff Plan
	for _, p := range set.Plans {
		if p.Role == RoleStaff && len(p.Steps) > 1 {
			staff = p
			break
		}
	}
	traces[staff.ID] = browserTrace(staff, 1)
	_, err := NewWorkload(&set, traces, mustScenario("shop"), 2)
	if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("plan %d", staff.ID)) || !strings.Contains(err.Error(), "staff") {
		t.Errorf("error %v, want one that names the staff plan", err)
	}
	// A staff visit is one document, and that document is its landing: a staff plan whose landing is no
	// document and one of whose clicks is asks for one document too, and is refused.
	traces = browserTraces(set.Plans)
	late := browserTrace(staff, 1)
	late.Steps[0].Reqs[0].Class = "nav"
	traces[staff.ID] = late
	if _, err := NewWorkload(&set, traces, mustScenario("shop"), 2); err == nil || !strings.Contains(err.Error(), fmt.Sprintf("plan %d", staff.ID)) || !strings.Contains(err.Error(), "staff") {
		t.Errorf("a staff plan whose one document is not its landing: error %v, want one that names the staff plan", err)
	}
	// A plan without the trace of its page views cannot be counted.
	traces = browserTraces(set.Plans)
	traces[3] = Trace{Plan: 3}
	if _, err := NewWorkload(&set, traces, mustScenario("shop"), 2); err == nil || !strings.Contains(err.Error(), "plan 3") {
		t.Errorf("error %v, want one that names plan 3", err)
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
	if want := int64(len("404 page not found\n") * round.Crawler.Statuses["O 404"]); round.Crawler.OldBytes != want || want == 0 {
		t.Errorf("the old URLs read %d bytes, want %d", round.Crawler.OldBytes, want)
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

// The classes of crawlers are what Cloudflare reports: Googlebot alone is 4.5% of the HTML requests
// and the other AI crawlers together 4.2%. The search class is Googlebot only, with two User-Agents
// (a smartphone and a desktop one) of half the requests each, which is an assumption, and no other
// search engine is in the mix since the report has no figure for them. The two numbers are the relative
// weights of the classes, since the total is fixed at CrawlerShare, and the table of the mix says so.
func TestTheCrawlerClassesAreWhatCloudflareReports(t *testing.T) {
	var search []CrawlerAgent
	for _, a := range CrawlerAgents {
		if a.Class == CrawlerSearch {
			search = append(search, a)
		}
	}
	if len(search) != 2 || !strings.Contains(search[0].UA, "Googlebot/2.1") || !strings.Contains(search[1].UA, "Googlebot/2.1") || search[0].UA == search[1].UA || search[0].Share != search[1].Share {
		t.Errorf("the search class is %+v: want Googlebot twice, with a smartphone and a desktop User-Agent of half each", search)
	}
	for _, a := range CrawlerAgents {
		for _, gone := range []string{"Bingbot", "bingbot", "Baiduspider", "YandexBot"} {
			if strings.Contains(a.UA, gone) || strings.Contains(a.Name, gone) {
				t.Errorf("%s is in the mix, and the report has no figure for it", gone)
			}
		}
	}
	if CrawlerClasses[0].Name != CrawlerSearch || CrawlerClasses[0].Share != 4.5 || CrawlerClasses[1].Name != CrawlerAI || CrawlerClasses[1].Share != 4.2 || CrawlerShare != 0.09 {
		t.Errorf("classes %+v, share %v", CrawlerClasses, CrawlerShare)
	}
	facts := map[string]MixFact{}
	for _, f := range MixFacts() {
		facts[f.Name] = f
	}
	for _, name := range []string{"CrawlerShare", "CrawlerClasses"} {
		all := facts[name].What + " " + facts[name].Value + " " + facts[name].Source
		for _, want := range []string{"Googlebot", "https://blog.cloudflare.com/radar-2025-year-in-review/"} {
			if !strings.Contains(all, want) {
				t.Errorf("%s lacks %q: %s", name, want, all)
			}
		}
	}
	all := facts["CrawlerClasses"].What + " " + facts["CrawlerClasses"].Value + " " + facts["CrawlerClasses"].Source
	for _, want := range []string{"relative weights", "other AI crawlers", "no data"} {
		if !strings.Contains(all, want) {
			t.Errorf("CrawlerClasses lacks %q: %s", want, all)
		}
	}
	if strings.Contains(all, "search engine crawlers 4.5") || strings.Contains(facts["CrawlerShare"].Source, "search engine crawlers 4.5%") {
		t.Error("the table still says that search engine crawlers are 4.5%")
	}
}

// A crawler request that the end of the round cuts off, after it was sent, is counted as cancelled, not
// as done or failed, as a request of a visit is: the server got it, and the counts of the round and
// of the server then agree. One that was never sent (the round was over) is counted nowhere.
func TestACrawlerRequestCutOffByTheEndOfTheRoundIsCountedAsCancelled(t *testing.T) {
	sent := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sent <- struct{}{}
		<-r.Context().Done()
	}))
	defer srv.Close()
	cs := newCrawlerShop(t)
	cs.r.base = srv.URL
	ctx, cancel := context.WithCancel(context.Background())
	a := newAcc()
	done := make(chan struct{})
	go func() {
		cs.r.runCrawl(ctx, http.DefaultTransport.(*http.Transport), a, 0, time.Now())
		close(done)
	}()
	<-sent
	cancel()
	<-done
	if a.crawl.cancelled != 1 || a.crawl.failed != 0 || a.crawl.done != 0 || a.failures != 0 {
		t.Errorf("cancelled %d, failed %d, done %d, failures %d", a.crawl.cancelled, a.crawl.failed, a.crawl.done, a.failures)
	}
	a2 := newAcc()
	cs.r.runCrawl(ctx, http.DefaultTransport.(*http.Transport), a2, 1, time.Now())
	if a2.crawl.cancelled != 0 || a2.failures != 0 {
		t.Errorf("a request never sent was counted: cancelled %d, failures %d", a2.crawl.cancelled, a2.failures)
	}
	var r Round
	r.Crawler = &CrawlStats{Done: 3, Failed: 1, Cancelled: 2}
	if r.crawlerRequests() != 6 {
		t.Errorf("the requests the server got: %d, want 6", r.crawlerRequests())
	}
}
