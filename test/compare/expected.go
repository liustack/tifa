package compare

import (
	"fmt"
	"maps"
	"slices"
)

// The hit rate the plans allow: how many of the documents of a round a render memo that never drops a
// page could answer, worked out from the plans, the writes and the crawler requests alone. It looks at
// no answer of any server, so it is one number for every framework, and Tifa's measured rate goes next
// to it. A document hits when the page it asks for (the URL a render sees, RenderURL) was asked for
// before as a document, in the warm-up too, and none of the data it shows has changed since. A document
// of a page not asked for before is new, and one of a page asked for with other data is changed. Only
// documents count, and every document of a visit does: its landing, and every action the page does as
// a document load (a plain link, a form with no router). A click the router takes gets the data of a
// page and no HTML, which no render memo holds, so it is not a request that puts a page in the memo and
// it does not make a page seen. Which page views are documents is what the browser did, read from the
// trace of the plan (Trace.documents), the same in every framework (LoadTraces), and not an answer of a
// server. A 404 (an old URL of a crawler) is rendered for every request and never kept: it is neither a
// hit nor seen, and is counted apart. A document of a private page (the cart, the checkout) is left out:
// it shows the cart of the visitor, which the pages of a plan do not tell, and the replay splits its
// renders by the cart cookie.
//
// The writes and the documents are taken in the order of the plans, and within a visit in the order of
// its page views, a write after the page view it follows. The servers play them at once, so two writes or a write and a document next to it can
// come in another order, and a render memo of a size (ssr.memo_size) drops pages, so the rate Tifa
// measures can be lower than this one. It is hardly ever higher.

// docClass says how a document stands to the ones before it.
type docClass uint8

const (
	docNew     docClass = iota + 1 // the page was not asked for before
	docChanged                     // asked for before, with other data
	docSame                        // asked for before, with the same data: a hit
)

func (c docClass) String() string {
	return [...]string{"none", "new", "changed", "same"}[c]
}

// ExpectedCell counts the documents of a kind of page: all of them, the hits, the changed ones and the
// new ones, which add up to the documents.
type ExpectedCell struct {
	Docs    int `json:"docs"`
	Hits    int `json:"hits"`
	Changed int `json:"changed"`
	New     int `json:"new"`
}

func (c *ExpectedCell) add(class docClass) {
	c.Docs++
	switch class {
	case docSame:
		c.Hits++
	case docChanged:
		c.Changed++
	default:
		c.New++
	}
}

// Sum adds the cells of o to c.
func (c ExpectedCell) Sum(o ExpectedCell) ExpectedCell {
	return ExpectedCell{c.Docs + o.Docs, c.Hits + o.Hits, c.Changed + o.Changed, c.New + o.New}
}

// Rate is the share of the documents that hit, 0 when there are none.
func (c ExpectedCell) Rate() float64 {
	if c.Docs == 0 {
		return 0
	}
	return float64(c.Hits) / float64(c.Docs)
}

// Expected is the count of a round: the documents of people by kind of page (pageClass) and by the
// landing parameter they carry (the storefront alone), the documents of crawlers by kind of URL
// (crawlKind), and the crawler requests for old URLs.
type Expected struct {
	Humans   map[string]ExpectedCell `json:"humans,omitempty"`
	Crawlers map[string]ExpectedCell `json:"crawlers,omitempty"`
	Groups   map[string]ExpectedCell `json:"groups,omitempty"`
	Old      int                     `json:"old,omitempty"`
}

// Expectation is Expected with the class of each counted document: of each document of a visit, by the
// number of the visit in the sequence of visits and the page view, and of each crawler request, by its number in the stream of
// crawler requests. The replay counts what Tifa rendered in a VM by them.
type Expectation struct {
	Expected
	human map[[2]int]docClass
	crawl map[int]docClass
}

// expectSpan is a run of visits in the order they are played: visits seq0 to seq0+n-1 of the sequence, and the
// crawler requests among them. A span that is not counted (a warm-up) only changes what has been seen
// and what the data is.
type expectSpan struct {
	seq0, n int
	count   bool
}

// docSteps says which page views of a plan are documents, as the browser recorded them: the landing,
// and each action the page did as a document load. The plan is a form of a plan with variants when a
// visit takes one (planForm), and the trace is the one of that form. It fails for a plan whose trace
// has not its page views, since nothing else tells which they are.
func (w *Workload) docSteps(p Plan) ([]bool, error) {
	t, ok := w.traceOf(p)
	if !ok || len(t.Steps) != len(p.Steps) {
		what := fmt.Sprintf("plan %d", p.ID)
		if p.form > 0 {
			what = fmt.Sprintf("variant %d of plan %d", p.form, p.ID)
		}
		return nil, fmt.Errorf("%s has %d page views and its trace %d: the trace says which page views are documents", what, len(p.Steps), len(t.Steps))
	}
	return t.documents(), nil
}

// Expect works out the expected hits of the spans, played one after the other from nothing seen and
// the data as it is at the start: the catalogue, with the stock the restock before the clock gave
// (restocked, by slug: runner.restocked). It fails for a page the dependencies (store_deps.json) have no
// entry for, which means the file is not the one of these plans.
func (w *Workload) Expect(d *StoreDeps, restocked map[string]int, spans ...expectSpan) (*Expectation, error) {
	e := &Expectation{human: map[[2]int]docClass{}, crawl: map[int]docClass{}}
	m := newVersionModel(d, restocked)
	// A page's data is as old as the last change of anything it shows (a page changes when one of them
	// changed since it was last asked for), and lastSeen is that time as of the last document.
	lastSeen := map[string]int{}
	doc := func(page string) (docClass, error) {
		at, err := m.stampOf(page)
		if err != nil {
			return 0, err
		}
		render := d.RenderURL(stripLanding(page))
		before, seen := lastSeen[render]
		lastSeen[render] = at
		switch {
		case !seen:
			return docNew, nil
		case before == at:
			return docSame, nil
		}
		return docChanged, nil
	}
	for _, sp := range spans {
		for t := w.humanStart(sp.seq0); t < w.humanStart(sp.seq0+sp.n); t++ {
			crawler, k := w.slot(t)
			if crawler {
				req := w.crawlOf(k)
				if req.Old {
					if sp.count {
						e.Old++
					}
					continue
				}
				class, err := doc(req.URL)
				if err != nil {
					return nil, err
				}
				if sp.count {
					e.crawl[k] = class
					add(&e.Crawlers, crawlKind(req.URL), class)
				}
				continue
			}
			plan := w.planFor(k)
			docs, err := w.docSteps(plan)
			if err != nil {
				return nil, err
			}
			effects := versionEffects(w.Seed, w.stockOf(k, 1)[0])
			// The page of a step is the one the replay asks for: without the landing parameter, which no
			// render sees, and with the word of the visit where the plan holds the placeholder of a search
			// for a word of its own, which is a page no other visit asks for.
			subs := subsFor(w.Seed, k, plan)
			for i, step := range plan.Steps {
				if page := subs.apply(stripLanding(step.Page)); docs[i] && !privateKind(pageClass(page)) {
					class, err := doc(page)
					if err != nil {
						return nil, err
					}
					if sp.count {
						e.human[[2]int{k, i}] = class
						add(&e.Humans, renderKind(page), class)
						if i == 0 && isStorePage(page) {
							add(&e.Groups, landingGroup(step.Page), class)
						}
					}
				}
				for len(effects) > 0 && effects[0].After <= i {
					m.apply(effects[0])
					effects = effects[1:]
				}
			}
			for _, eff := range effects {
				m.apply(eff) // a write after a page view the plan has not: none in the plans of the mix
			}
		}
	}
	return e, nil
}

// add counts a document of a kind in cells.
func add(cells *map[string]ExpectedCell, kind string, class docClass) {
	if *cells == nil {
		*cells = map[string]ExpectedCell{}
	}
	c := (*cells)[kind]
	c.add(class)
	(*cells)[kind] = c
}

// Total is every cell of cells added together.
func Total(cells map[string]ExpectedCell) ExpectedCell {
	var t ExpectedCell
	for _, k := range slices.Sorted(maps.Keys(cells)) {
		t = t.Sum(cells[k])
	}
	return t
}

// humanClass is the class of the document that page view step of visit seq of the sequence asks for. It
// is not there for a page view that asks for none, for one of a private page, for a visit the
// expectation did not count (a warm-up's), and for no expectation at all (e is nil).
func (e *Expectation) humanClass(seq, step int) (docClass, bool) {
	if e == nil {
		return 0, false
	}
	c, ok := e.human[[2]int{seq, step}]
	return c, ok
}

// crawlClass is the class of crawler request number n of the stream.
func (e *Expectation) crawlClass(n int) (docClass, bool) {
	if e == nil {
		return 0, false
	}
	c, ok := e.crawl[n]
	return c, ok
}

// expectRound works out the expected hit rate of a round of Tifa that plays visits visits from seq0 on,
// after the warm-up that follows the prepared visits (r.warm of them, from r.prepared), and keeps it in
// the round and for the split of its renders. A server with no metrics route is not Tifa and has none.
func (r *runner) expectRound(round *Round, visits, seq0 int) {
	r.expect = nil
	if r.deps == nil || r.metrics == "" {
		return
	}
	var spans []expectSpan
	if r.warm > 0 {
		spans = append(spans, expectSpan{r.prepared, r.warm, false})
	}
	spans = append(spans, expectSpan{seq0, visits, true})
	e, err := r.wl.Expect(r.deps, r.restocked, spans...)
	if err != nil {
		round.ExpectedError = err.Error()
		return
	}
	r.expect, round.Expected = e, &e.Expected
}
