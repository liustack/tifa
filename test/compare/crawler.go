package compare

import (
	"context"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Crawlers are a stream of their own beside the visits of people: each request is one GET of a
// document, with no cookie, that runs no JavaScript, so there is no data request, no prefetch, no click and
// no write after it. A crawler request is not a visit and not a page view. The visits, their blocks,
// the sizing and the data prepared before the clock all count people only. The stream puts the
// requests of crawlers among the visits (the slots of a cycle), and which URL and which crawler each
// one is comes from the seed and its number in the stream, the same for every server. How many there
// are comes from the documents the browsers of people ask for, counted from the traces
// (Workload.blockDocuments), which every framework has alike: the two together are the HTML requests,
// and crawlers are CrawlerShare of them.

// CrawlerAgent is a crawler a request can come from: its class (search, which is Googlebot, or AI), its
// name, its share among the crawlers of its class, and the User-Agent it sends. The Node frameworks may
// read it. What the installed sources show (React Router, TanStack Start, Next.js 16.4.2): the
// default `entry.server` of React Router asks `isbot` and waits for the whole page (`onAllReady`) for a
// bot, TanStack Start asks `isbot` in `renderRouterToStream`, and `isbot` says yes to every User-Agent
// of the mix. Next.js reads it with its `is-bot`, which takes Googlebot for a "dom" bot, and no
// User-Agent of the AI crawlers is in its `htmlLimitedBots` list or taken for Googlebot. What each
// framework then does for a bot beyond that was not checked, and each answers as it ships.
type CrawlerAgent struct {
	Class, Name string
	Share       float64
	UA          string
}

// The classes of crawlers, with the shares of the HTML requests each has.
const (
	CrawlerSearch = "search"
	CrawlerAI     = "ai"
)

// crawlReq is a request of a crawler: the URL it asks for, whether it is one of the old URLs that
// answer 404 (crawl.go), and the crawler.
type crawlReq struct {
	URL   string
	Old   bool
	Agent CrawlerAgent
}

// want is the status the shop answers a crawler: 200 for a public page, 404 for an old URL.
func (c crawlReq) want() int {
	if c.Old {
		return http.StatusNotFound
	}
	return http.StatusOK
}

// crawlOf is the request that crawler request number c of a run is: the URL from the seed and the
// number alone, CrawlOldShare of them from the old URLs and the others from the public ones, all
// alike, so that a long run does not go round a short list, and the crawler by the shares of the
// classes and of the crawlers in a class.
func (w *Workload) crawlOf(c int) crawlReq {
	rng := rand.New(rand.NewPCG(w.Seed^0x510e527fade682d1, uint64(c)))
	public, old := CrawlURLs()
	var req crawlReq
	if rng.Float64() < CrawlOldShare {
		req.Old, req.URL = true, old[rng.IntN(len(old))]
	} else {
		req.URL = public[rng.IntN(len(public))]
	}
	var total float64
	for _, k := range CrawlerClasses {
		total += k.Share
	}
	class := CrawlerClasses[len(CrawlerClasses)-1].Name
	for x, i := rng.Float64()*total, 0; i < len(CrawlerClasses); i++ {
		if x -= CrawlerClasses[i].Share; x < 0 {
			class = CrawlerClasses[i].Name
			break
		}
	}
	var of []CrawlerAgent
	total = 0
	for _, a := range CrawlerAgents {
		if a.Class == class {
			of, total = append(of, a), total+a.Share
		}
	}
	req.Agent = of[len(of)-1]
	for x, i := rng.Float64()*total, 0; i < len(of); i++ {
		if x -= of[i].Share; x < 0 {
			req.Agent = of[i]
			break
		}
	}
	return req
}

// crawlLayout says which slots of a cycle are crawler requests. A cycle holds, for each block of the
// plans, its visits (blockVisits) and the crawler requests of the block among them, at places of a
// seeded shuffle, so the share of crawlers is exact in every block. A block has the crawler requests
// that go with the documents its own visits ask for (BrowseScenario.crawlerSlots), so the blocks can
// hold other numbers of them. The visits keep their order, and so do the crawler requests.
type crawlLayout struct {
	blocks, visits int
	total          int    // the crawler requests of a cycle
	start          []int  // the first slot of each block of the cycle
	crawl          []bool // by slot of the cycle
	humanBefore    []int  // the visits in the slots before it
	crawlBefore    []int  // the crawler requests in the slots before it
	humanSlot      []int  // the slot of each visit of the cycle
}

// newCrawlLayout lays out a cycle of len(per) blocks of visits visits each, block b with per[b] crawler
// requests.
func newCrawlLayout(seed uint64, visits int, per []int) *crawlLayout {
	l := &crawlLayout{blocks: len(per), visits: visits}
	h, c := 0, 0
	for b, per := range per {
		l.start = append(l.start, len(l.crawl))
		l.total += per
		n := visits + per
		rng := rand.New(rand.NewPCG(seed^0x5be0cd19137e2179, uint64(b)))
		at := make([]bool, n)
		for _, i := range rng.Perm(n)[:per] {
			at[i] = true
		}
		for _, crawl := range at {
			l.crawl = append(l.crawl, crawl)
			l.humanBefore, l.crawlBefore = append(l.humanBefore, h), append(l.crawlBefore, c)
			if crawl {
				c++
			} else {
				l.humanSlot = append(l.humanSlot, len(l.crawl)-1)
				h++
			}
		}
	}
	return l
}

// slot says what slot t of the stream is: a crawler request (and its number in the stream of crawler
// requests) or a visit (and its number in the global sequence of visits).
func (w *Workload) slot(t int) (crawler bool, n int) {
	l := w.crawlers
	if l == nil {
		return false, t
	}
	cycle, i := t/len(l.crawl), t%len(l.crawl)
	if l.crawl[i] {
		return true, cycle*l.total + l.crawlBefore[i]
	}
	return false, cycle*l.blocks*l.visits + l.humanBefore[i]
}

// humanStart is the slot where visit h of the global sequence starts a range of visits: the first slot
// of its block when it is the first visit of one, so that a round of whole blocks has all the crawler
// requests of its blocks, and its own slot otherwise.
func (w *Workload) humanStart(h int) int {
	l := w.crawlers
	if l == nil {
		return h
	}
	per := l.blocks * l.visits
	cycle, r := h/per, h%per
	if r%l.visits == 0 {
		return cycle*len(l.crawl) + l.start[r/l.visits]
	}
	return cycle*len(l.crawl) + l.humanSlot[r]
}

// slots is how many slots the stream has for visits seq0 to seq0+n-1: the visits and the crawler
// requests among them.
func (w *Workload) slots(seq0, n int) int { return w.humanStart(seq0+n) - w.humanStart(seq0) }

// crawlersIn is how many crawler requests the stream has among visits seq0 to seq0+n-1.
func (w *Workload) crawlersIn(seq0, n int) int { return w.slots(seq0, n) - n }

// CrawlStats is what the crawler requests of a round did. Planned are the ones the round's slots
// held, Done the ones answered with the status that was expected (200 for a public URL, 404 for an old
// one), Failed the others, which fail the round, and Statuses all the answers by set (U or O) and
// status. Their latency is in Types["crawler"]. Renders counts how Tifa answered them (X-Tifa-Render),
// by set, language and kind of URL ("U|en-US|list").
type CrawlStats struct {
	Planned int `json:"planned"`
	Done    int `json:"done"`
	Failed  int `json:"failed"`
	// Cancelled are the requests that the end of the round cut off after they were sent: the server got them.
	Cancelled int   `json:"cancelled,omitempty"`
	Bytes     int64 `json:"bytes"`
	// OldBytes are the bytes of the answers to the old URLs (the set O), which are 404 pages.
	OldBytes int64                     `json:"oldBytes,omitempty"`
	Statuses map[string]int            `json:"statuses,omitempty"`
	Renders  map[string]map[string]int `json:"renders,omitempty"`
	// Why splits the renders in a VM by the same key and why (RenderFirst, RenderChanged, RenderSame).
	Why map[string]map[string]int `json:"why,omitempty"`
}

// crawlCounts are what an acc keeps of the crawler requests.
type crawlCounts struct {
	// why splits the renders in a VM of a kind of URL (the key of renders) by the class of the document.
	why                     map[string]map[string]int
	done, failed, cancelled int
	bytes                   int64
	oldBytes                int64
	statuses                map[string]int
	renders                 map[string]map[string]int
}

// crawlKey is the key of a request in CrawlStats.Renders.
func crawlKey(c crawlReq) string {
	set := "U"
	if c.Old {
		set = "O"
	}
	lang, _ := StorePageKind(c.URL)
	kind := crawlKind(c.URL)
	return strings.Join([]string{set, lang, kind}, "|")
}

// crawlFail counts a crawler request that failed: an error of the connection, or another status than
// the one expected. It is a failure of the round, not of a visit.
func (a *acc) crawlFail(err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.failures++
	a.crawl.failed++
	if a.firstErr == "" {
		a.firstErr = err.Error()
	}
}

// crawlGone counts a request that ended with an error after it was sent: a failure, or, when the end
// of the round cut it off, a cancelled request, which is no failure of the server.
func (a *acc) crawlGone(ctx context.Context, err error) {
	if ctx.Err() == nil {
		a.crawlFail(err)
		return
	}
	a.mu.Lock()
	a.crawl.cancelled++
	a.mu.Unlock()
}

// runCrawl plays crawler request number c of the stream: one GET of a document with the User-Agent of
// its crawler, no cookie, no validator, gzip accepted, and no redirect followed. It is answered with
// the status it expects or it is a failure. sched is when it was meant to start, from which its
// latency is counted. A request that the end of the round cuts off is counted as neither.
func (r *runner) runCrawl(ctx context.Context, tr *http.Transport, a *acc, c int, sched time.Time) {
	q := r.wl.crawlOf(c)
	client := &http.Client{Transport: tr, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.base+q.URL, nil)
	if err != nil {
		a.crawlFail(err)
		return
	}
	req.Header.Set("User-Agent", q.Agent.UA)
	req.Header.Set("Accept", "text/html")
	req.Header.Set("Accept-Encoding", "gzip")
	if ctx.Err() != nil {
		return // the round is over: nothing was sent
	}
	r.inflight.Add(1)
	defer r.inflight.Add(-1)
	res, err := client.Do(req)
	if err != nil {
		a.crawlGone(ctx, fmt.Errorf("crawler GET %s: %w", q.URL, err))
		return
	}
	a.refuse(res.StatusCode)
	set := "U"
	if q.Old {
		set = "O"
	}
	// The status is judged as soon as the headers arrive, as that of a visit is: a body that does not
	// end before the round does cannot hide a status that was wrong, and the request is counted once.
	if res.StatusCode != q.want() {
		res.Body.Close()
		a.mu.Lock()
		a.crawl.statuses[set+" "+strconv.Itoa(res.StatusCode)]++
		a.mu.Unlock()
		a.crawlFail(fmt.Errorf("crawler GET %s: status %d, want %d", q.URL, res.StatusCode, q.want()))
		return
	}
	n, err := io.Copy(io.Discard, res.Body)
	res.Body.Close()
	end := time.Now()
	if err != nil {
		a.crawlGone(ctx, fmt.Errorf("crawler GET %s: %w", q.URL, err))
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.crawl.done++
	a.crawl.bytes += n
	if q.Old {
		a.crawl.oldBytes += n
	}
	a.crawl.statuses[set+" "+strconv.Itoa(res.StatusCode)]++
	a.lat["crawler"] = append(a.lat["crawler"], ms(end.Sub(sched)))
	if how := res.Header.Get(RenderHeader); how != "" && how != RenderNone {
		key := crawlKey(q)
		if a.crawl.renders[key] == nil {
			a.crawl.renders[key] = map[string]int{}
		}
		a.crawl.renders[key][how]++
		if class, ok := r.expect.crawlClass(c); ok && how == RenderVM {
			if a.crawl.why[key] == nil {
				a.crawl.why[key] = map[string]int{}
			}
			a.crawl.why[key][docWhy(class)]++
		}
	}
}

// playSlot plays slot t of the stream: a visit, or a crawler request. It reports whether a visit
// ended (completed or failed), which is what the sizing counts.
func (r *runner) playSlot(ctx context.Context, tr *http.Transport, a *acc, t int, p pacer) (visitEnded bool) {
	if crawler, n := r.wl.slot(t); crawler {
		sched := p.start
		if !p.open {
			sched = time.Now()
		}
		r.runCrawl(ctx, tr, a, n, sched)
		return false
	} else {
		return r.runVisit(ctx, tr, a, n, p) || ctx.Err() == nil
	}
}
