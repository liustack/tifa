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
// documents count: a click gets the data of a page and no HTML, which no render memo holds, so it is not
// a request that puts a page in the memo and it does not make a page seen. A 404 (an old URL of a
// crawler) is rendered for every request and never kept: it is neither a hit nor seen, and is counted
// apart.
//
// The writes and the documents are taken in the order of the plans, a visit's document first and then
// its writes. The servers play them at once, so two writes or a write and a document next to it can
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

// Expectation is Expected with the class of each counted document: of the first page of each visit, by
// its number in the sequence of visits, and of each crawler request, by its number in the stream of
// crawler requests. The replay counts what Tifa rendered in a VM by them.
type Expectation struct {
	Expected
	human map[int]docClass
	crawl map[int]docClass
}

// expectSpan is a run of visits in the order they are played: visits seq0 to seq0+n-1 of the sequence, and the
// crawler requests among them. A span that is not counted (a warm-up) only changes what has been seen
// and what the data is.
type expectSpan struct {
	seq0, n int
	count   bool
}

// pageInfo is what Expect keeps of a page URL: the URL a render sees and the version keys of its data.
type pageInfo struct {
	render string
	keys   []int
}

// Expect works out the expected hits of the spans, played one after the other from nothing seen and
// the data as it is at the start. It fails for a page the dependencies (store_deps.json) have no entry
// for, which means the file is not the one of these plans.
func (w *Workload) Expect(d *StoreDeps, spans ...expectSpan) (*Expectation, error) {
	e := &Expectation{human: map[int]docClass{}, crawl: map[int]docClass{}}
	ids := map[string]int{}
	var stamp []int // the last time (a count of writes) each key changed
	id := func(key string) int {
		i, ok := ids[key]
		if !ok {
			i = len(stamp)
			ids[key] = i
			stamp = append(stamp, 0)
		}
		return i
	}
	pages := map[string]pageInfo{}
	info := func(page string) (pageInfo, error) {
		if p, ok := pages[page]; ok {
			return p, nil
		}
		keys, ok := d.pageKeys(page)
		if !ok {
			return pageInfo{}, fmt.Errorf("the dependencies have no entry for %s: make store_deps.json again with `compare deps`", page)
		}
		p := pageInfo{render: d.RenderURL(page)}
		for _, k := range keys {
			p.keys = append(p.keys, id(k))
		}
		pages[page] = p
		return p, nil
	}
	// A page's data is as old as the last change of anything it shows (a page changes when one of them
	// changed since it was last asked for), and lastSeen is that time as of the last document.
	lastSeen := map[string]int{}
	clock := 0
	doc := func(page string) (docClass, error) {
		p, err := info(page)
		if err != nil {
			return 0, err
		}
		at := 0
		for _, k := range p.keys {
			at = max(at, stamp[k])
		}
		before, seen := lastSeen[p.render]
		lastSeen[p.render] = at
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
			page := stripLanding(plan.Steps[0].Page)
			class, err := doc(page)
			if err != nil {
				return nil, err
			}
			if sp.count {
				e.human[k] = class
				add(&e.Humans, pageClass(page), class)
				if isStorePage(page) {
					add(&e.Groups, landingGroup(plan.Steps[0].Page), class)
				}
			}
			for _, eff := range versionEffects(w.Seed, w.stockOf(k, 1)[0]) {
				clock++
				for _, key := range eff.Keys {
					stamp[id(key)] = clock
				}
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
