package compare

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
)

// recordEnv is the environment of the tests that record visits in a browser against the four
// frameworks, which are off unless TIFA_COMPARE=1: the shop and the three Node apps built
// (TIFA_COMPARE_SKIP_BUILD=1 uses the last builds).
func recordEnv(t *testing.T) Env {
	t.Helper()
	if os.Getenv("TIFA_COMPARE") != "1" {
		t.Skip("builds the apps and records in a browser, set TIFA_COMPARE=1 to run it")
	}
	if why := MissingTools(); why != "" {
		t.Skip(why)
	}
	env, err := NewEnv()
	if err != nil {
		t.Fatal(err)
	}
	if os.Getenv("TIFA_COMPARE_SKIP_BUILD") != "1" {
		var log strings.Builder
		if err := env.Prepare(context.Background(), &log); err != nil {
			t.Fatalf("%v\n%s", err, &log)
		}
	}
	return env
}

// actSample picks a few plans of a set that between them do every act a plan can hold, the shortest
// plan for each: a search for a word of the list (which has variants) in each language, a search for a
// word of the visit's own with a page after it, a filter by a plain link of each part of the list, the
// form with one condition, with a price and another condition and with three, a sort, the next page
// once and twice, and a filter and a sort after a search.
func actSample(set PlanSet) []int {
	has := func(p Plan, ok func(PlanStep) bool) bool { return slices.ContainsFunc(p.Steps, ok) }
	conds := func(s PlanStep) []PlanCondition { return conditionsOf(s.Set) }
	wants := []func(Plan) bool{
		func(p Plan) bool { return len(p.Variants) > 0 && langOf(p.Steps[0].Page) == "en-US" },
		func(p Plan) bool { return len(p.Variants) > 0 && langOf(p.Steps[0].Page) == "zh-CN" },
		func(p Plan) bool {
			at := slices.IndexFunc(p.Steps, func(s PlanStep) bool { return s.Term == SearchPlaceholder })
			return at > 0 && at < len(p.Steps)-1
		},
		func(p Plan) bool {
			return has(p, func(s PlanStep) bool { return s.Act == ActFilter && s.Part == ListPartCategory })
		},
		func(p Plan) bool {
			return has(p, func(s PlanStep) bool { return s.Act == ActFilter && s.Part == ListPartQuick })
		},
		func(p Plan) bool {
			return has(p, func(s PlanStep) bool { return s.Act == ActFilter && s.Part == ListPartBand })
		},
		func(p Plan) bool {
			return has(p, func(s PlanStep) bool { return s.Act == ActForm && len(conds(s)) == 1 })
		},
		func(p Plan) bool {
			return has(p, func(s PlanStep) bool {
				return s.Act == ActForm && len(conds(s)) == 2 && slices.ContainsFunc(conds(s), func(c PlanCondition) bool { return c.Kind == FilterPrice })
			})
		},
		func(p Plan) bool {
			return has(p, func(s PlanStep) bool { return s.Act == ActForm && len(conds(s)) == 3 })
		},
		func(p Plan) bool { return has(p, func(s PlanStep) bool { return s.Act == ActSort }) },
		func(p Plan) bool {
			return has(p, func(s PlanStep) bool { return s.Act == ActSort }) && langOf(p.Steps[0].Page) == "zh-CN"
		},
		func(p Plan) bool { return listPages(p) == 1 },
		func(p Plan) bool { return listPages(p) == 2 },
		func(p Plan) bool {
			return len(p.Variants) > 0 && has(p, func(s PlanStep) bool { return s.Act == ActForm || s.Act == ActFilter })
		},
		func(p Plan) bool {
			return len(p.Variants) > 0 && has(p, func(s PlanStep) bool { return s.Act == ActSort })
		},
	}
	var ids []int
	for _, want := range wants {
		best := -1
		for i, p := range set.Plans {
			if p.Role == RoleShopper && want(p) && (best < 0 || len(p.Steps) < len(set.Plans[best].Steps)) {
				best = i
			}
		}
		if best >= 0 && !slices.Contains(ids, set.Plans[best].ID) {
			ids = append(ids, set.Plans[best].ID)
		}
	}
	slices.Sort(ids)
	return ids
}

// listPages is how many times a plan goes to the next page of a list.
func listPages(p Plan) int {
	n := 0
	for _, s := range p.Steps {
		if s.Act == ActPage {
			n++
		}
	}
	return n
}

// actRequests counts what a browser sent for the page views of one act: the page views, those that
// loaded a document, the requests by class, those the browser cancelled, and the page views shown
// under a URL written otherwise than the plan writes it.
type actRequests struct {
	Steps, WithDoc, Doc, Nav, Prefetch, Cancelled, Respelled int
}

// requestsByAct counts the requests of recorded visits by the act of their page views ("landing" for
// the first).
func requestsByAct(plans map[int]Plan, traces []Trace) map[string]actRequests {
	out := map[string]actRequests{}
	for _, t := range traces {
		form := planForm(plans[t.Plan], t.Variant)
		for i, s := range t.Steps {
			act := form.Steps[i].Act
			if i == 0 {
				act = "landing"
			}
			c := out[act]
			c.Steps++
			if s.document() {
				c.WithDoc++
			}
			if s.Shown != "" {
				c.Respelled++
			}
			for _, q := range s.Reqs {
				switch q.Class {
				case "doc":
					c.Doc++
				case "prefetch":
					c.Prefetch++
				default:
					c.Nav++
				}
				if q.Cancel != "" {
					c.Cancelled++
				}
			}
			out[act] = c
		}
	}
	return out
}

// sampled keeps the sample the tests of this file record, so that they record it once.
var sampled struct {
	once   sync.Once
	plans  map[int]Plan
	traces map[string][]Trace
}

// strayPreloads are the requests of the traces of Tifa or of TanStack Start for the data of a page
// that is not the page of their step, each with its plan, its step and its URL. Both routers preload
// the link a pointer lies on, and the recorder keeps the pointer off the page but for its clicks, so
// there must be none: one means the pointer lay on a link that the page drew under it. For Tifa it is
// a data request of the router (_tifa) for another URL than the page. TanStack Start marks a preload
// in no way, so for it this finds the calls of server functions that their arguments tell from the
// calls of the page: one for a product that is not the product of the page, and, on a product list,
// one for a list of other conditions. A product card and a link to another list are what a pointer
// comes to lie on there.
func strayPreloads(t *testing.T, fw string, plans map[int]Plan, traces []Trace) []string {
	t.Helper()
	var out []string
	for _, tr := range traces {
		for i, s := range tr.Steps {
			shown, err := url.Parse(s.Page)
			if err != nil {
				t.Fatal(err)
			}
			page := shown.Path
			act := "landing"
			if i > 0 {
				act = planForm(plans[tr.Plan], tr.Variant).Steps[i].Act
			}
			for _, q := range s.Reqs {
				u, err := url.Parse(q.URL)
				if err != nil {
					t.Fatal(err)
				}
				stray := false
				switch {
				case fw == "tifa" && u.Query().Has("_tifa"):
					asked := u.Query()
					asked.Del("_tifa")
					stray = u.Path != page || !maps.EqualFunc(asked, shown.Query(), slices.Equal[[]string])
				case fw == "start" && strings.HasPrefix(u.Path, "/_serverFn/"):
					var payload any
					if err := json.Unmarshal([]byte(u.Query().Get("payload")), &payload); err != nil {
						t.Fatalf("%v: %s", err, q.URL)
					}
					args, _ := serovalValue(t, payload.(map[string]any)["t"]).(map[string]any)
					data, _ := args["data"].(map[string]any)
					product, ofProduct := data["product"].(string)
					query, ofList := data["query"].(map[string]any)
					switch _, kind := StorePageKind(s.Page); {
					case ofProduct:
						stray = !strings.HasSuffix(page, "/products/"+product)
					case ofList && kind == StoreKindList:
						asked := url.Values{}
						for name, v := range query {
							switch x := v.(type) {
							case string:
								asked.Add(name, x)
							case []any:
								for _, one := range x {
									asked.Add(name, one.(string))
								}
							default:
								t.Fatalf("the query of %s has %s as %v", q.URL, name, v)
							}
						}
						stray = !maps.EqualFunc(asked, shown.Query(), slices.Equal[[]string])
					}
				}
				if stray {
					out = append(out, fmt.Sprintf("plan %d variant %d page view %d (%s, class %s): %s", tr.Plan, tr.Variant, i, act, q.Class, q.URL))
				}
			}
		}
	}
	return out
}

// serovalValue is the value of a node that seroval wrote, for the nodes the arguments of the server
// functions of the shop are made of: a string (s), an array (a), and an object, which has its keys (k)
// and its values (v) at the same places.
func serovalValue(t *testing.T, node any) any {
	t.Helper()
	n, _ := node.(map[string]any)
	switch {
	case n["p"] != nil:
		p := n["p"].(map[string]any)
		keys, values := p["k"].([]any), p["v"].([]any)
		out := map[string]any{}
		for i, k := range keys {
			out[k.(string)] = serovalValue(t, values[i])
		}
		return out
	case n["a"] != nil:
		var out []any
		for _, one := range n["a"].([]any) {
			out = append(out, serovalValue(t, one))
		}
		return out
	case n["s"] != nil:
		return n["s"]
	}
	t.Fatalf("a node of seroval this test does not read: %v", node)
	return nil
}

// recordedSample is the act sample of the first block of the formal seed, recorded in a browser
// against the four frameworks once for the tests of a run: the plans by id and the traces of each
// framework, every one checked against its plan.
func recordedSample(t *testing.T, env Env) (map[int]Plan, map[string][]Trace) {
	t.Helper()
	sampled.once.Do(func() { sampled.plans, sampled.traces = recordSample(t, env, BrowseFrameworks[:4]) })
	if sampled.traces == nil {
		t.Fatal("the sample could not be recorded: see the test that recorded it")
	}
	return sampled.plans, sampled.traces
}

// recordSample records the plans of an act sample of the first block of the formal seed in a browser
// against the frameworks, and returns the plans by id and the traces of each framework, every one
// checked against its plan.
func recordSample(t *testing.T, env Env, frameworks []string) (map[int]Plan, map[string][]Trace) {
	t.Helper()
	set := GenerateSitePlanSet(FormalBrowse.Seed, 1)
	ids := actSample(set)
	if len(ids) < 10 {
		t.Fatalf("the sample is the plans %v: too few to do every act", ids)
	}
	dir := t.TempDir()
	data, err := json.Marshal(set)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(PlansPath(dir), data, 0o644); err != nil {
		t.Fatal(err)
	}
	var log strings.Builder
	if err := env.Record(context.Background(), &log, dir, frameworks, 4, 0, ids); err != nil {
		t.Fatalf("%v\n%s", err, &log)
	}
	plans := map[int]Plan{}
	visits := 0
	for _, p := range set.Plans {
		if slices.Contains(ids, p.ID) {
			plans[p.ID] = p
			visits += 1 + len(p.Variants)
		}
	}
	traces := map[string][]Trace{}
	for _, fw := range frameworks {
		list, dropped, err := ReadTraces(TracePath(dir, fw))
		if err != nil {
			t.Fatal(err)
		}
		if dropped > 0 || len(list) != visits {
			t.Fatalf("%s: %d of the %d visits of the sample were recorded, %d failed:\n%s", fw, len(list), visits, dropped, &log)
		}
		for _, tr := range list {
			if err := checkTrace(planForm(plans[tr.Plan], tr.Variant), tr); err != nil {
				t.Errorf("%s: variant %d: %v", fw, tr.Variant, err)
			}
		}
		slices.SortFunc(list, func(a, b Trace) int { return (a.Plan-b.Plan)*10 + a.Variant - b.Variant })
		traces[fw] = list
	}
	return plans, traces
}

// TestStorefrontRecorderDoesTheActsOfThePlans records a sample of plans that do every act a plan can
// hold, with their variants, in a real browser against the four frameworks. The recorder does each
// act with the control of the page that does it, so every visit of the sample must record, each page
// view must show the page of its plan, and the browser must do what the act says where the page
// leaves no choice (checkTrace): a document for a search, a filter by a link and the next page, and
// none for the form and the sort box. The four frameworks serve the same pages, so they must agree on
// which page views are documents, and a variant must load a document where the visit of its plan
// does. The recorder takes the pointer off the page after each click, so Tifa and TanStack Start,
// which preload the link under a pointer, must ask for no page but those of the plans
// (strayPreloads). The test logs what each browser sent for each act.
func TestStorefrontRecorderDoesTheActsOfThePlans(t *testing.T) {
	env := recordEnv(t)
	plans, traces := recordedSample(t, env)
	acts := []string{"landing", ActLink, ActSearch, ActFilter, ActPage, ActForm, ActSort}
	first := traces[BrowseFrameworks[0]]
	for _, fw := range BrowseFrameworks[:4] {
		list := traces[fw]
		by := requestsByAct(plans, list)
		for _, act := range acts {
			c := by[act]
			if c.Steps == 0 {
				t.Errorf("%s: the sample has no page view that is a %s", fw, act)
			}
			t.Logf("%-11s %-7s %3d page views, %3d with a document: %3d documents, %4d data requests, %4d prefetches, %3d cancelled, %d shown under another spelling", fw, act, c.Steps, c.WithDoc, c.Doc, c.Nav, c.Prefetch, c.Cancelled, c.Respelled)
		}
		// The pointer is off the page but for its clicks, so no router preloads a link under it.
		if strays := strayPreloads(t, fw, plans, list); len(strays) > 0 {
			t.Errorf("%s: %d requests preload a page that no act of the plans asked for, the first at %s", fw, len(strays), strays[0])
		}
		for i, tr := range list {
			if tr.Plan != first[i].Plan || tr.Variant != first[i].Variant || !slices.Equal(tr.documents(), first[i].documents()) {
				t.Errorf("%s: variant %d of plan %d loads a document at the page views %v, and %s at %v", fw, tr.Variant, tr.Plan, tr.documents(), BrowseFrameworks[0], first[i].documents())
			}
			// A variant loads a document where the visit of its plan does.
			if at := slices.IndexFunc(list, func(o Trace) bool { return o.Plan == tr.Plan && o.Variant == 0 }); !slices.Equal(tr.documents(), list[at].documents()) {
				t.Errorf("%s: variant %d of plan %d loads a document at the page views %v, and the visit of the plan at %v", fw, tr.Variant, tr.Plan, tr.documents(), list[at].documents())
			}
		}
	}
	if t.Failed() {
		t.Log(fmt.Sprint("the plans of the sample: ", slices.Sorted(func(yield func(int) bool) {
			for id := range plans {
				if !yield(id) {
					return
				}
			}
		})))
	}
}

// TestStorefrontEveryServerFindsNothingForTheWordOfAVisit replays, against each of the four
// frameworks, the recorded visit of the sample that searches for a word of its own, with the word of
// a visit put in place of the placeholder as the replay does it (subsFor), in every request: the URL,
// the headers and the body. Every server must answer every request as it answered the recording, and
// the document of the search must be the list of that word, which finds nothing: an HTML page that
// has the word and the text of the empty list, and not the placeholder. The test logs where each
// framework carries the word.
func TestStorefrontEveryServerFindsNothingForTheWordOfAVisit(t *testing.T) {
	env := recordEnv(t)
	plans, traces := recordedSample(t, env)
	buildID, err := env.ShopBuildID()
	if err != nil {
		t.Fatal(err)
	}
	empty := map[string]string{"en-US": "No products match", "zh-CN": "没有找到符合条件的商品"}
	ctx := context.Background()
	const seq = 123_456
	for _, fw := range BrowseFrameworks[:4] {
		target, err := env.Target(fw)
		if err != nil {
			t.Fatal(err)
		}
		server, err := target.Start(ctx, "")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(server.Stop)
		played := 0
		for _, tr := range traces[fw] {
			plan := planForm(plans[tr.Plan], tr.Variant)
			at := slices.IndexFunc(plan.Steps, func(s PlanStep) bool { return s.Term == SearchPlaceholder })
			if at < 0 {
				continue
			}
			played++
			subs := subsFor(FormalBrowse.Seed, seq, plan)
			word := uniqueTerm(FormalBrowse.Seed, seq)
			jar, _ := cookiejar.New(nil)
			client := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
			where := map[string]int{}
			for i, s := range tr.Steps {
				for _, q := range s.Reqs {
					url := q.URL
					if fw == "tifa" {
						url = tifaBuild.ReplaceAllString(url, "_tifa="+buildID+".")
					}
					url = subs.apply(url)
					var body io.Reader
					if q.Body != "" {
						body = strings.NewReader(subs.apply(q.Body))
						if strings.Contains(q.Body, SearchPlaceholder) {
							where["body"]++
						}
					}
					req, err := http.NewRequestWithContext(ctx, q.Method, server.Base+url, body)
					if err != nil {
						t.Fatal(err)
					}
					for k, val := range q.Headers {
						req.Header.Set(k, subs.apply(val))
						if strings.Contains(val, SearchPlaceholder) {
							where["header "+k]++
						}
					}
					if strings.Contains(q.URL, SearchPlaceholder) {
						where[q.Class+" URL"]++
					}
					if q.Class != "doc" {
						req.Header.Set("Sec-Fetch-Site", "same-origin")
						req.Header.Set("Sec-Fetch-Mode", "cors")
						req.Header.Set("Sec-Fetch-Dest", "empty")
					}
					res, err := client.Do(req)
					if err != nil {
						t.Fatalf("%s: %s %s: %v", fw, q.Method, url, err)
					}
					data, _ := io.ReadAll(res.Body)
					res.Body.Close()
					text := string(data)
					if res.StatusCode != 200 && res.StatusCode != 204 {
						t.Errorf("%s: %s %s answered %d, and %d when it was recorded", fw, q.Method, url, res.StatusCode, q.Status)
					}
					if strings.Contains(text, SearchPlaceholder) {
						t.Errorf("%s: the answer to %s %s has the placeholder: the server was asked for it", fw, q.Method, url)
					}
					if i == at && q.Class == "doc" {
						lang := langOf(plan.Steps[0].Page)
						if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") || !strings.Contains(text, word) || !strings.Contains(text, empty[lang]) {
							t.Errorf("%s: the document of the search for %s is %s, has the word %v and the text of the empty list %v", fw, word, ct, strings.Contains(text, word), strings.Contains(text, empty[lang]))
						}
					}
				}
			}
			if where["doc URL"] == 0 {
				t.Errorf("%s: no document of the visit of plan %d has the placeholder in its URL", fw, tr.Plan)
			}
			t.Logf("%-11s plan %d: the word of the visit is put into %v", fw, tr.Plan, where)
		}
		server.Stop()
		if played == 0 {
			t.Errorf("%s: the sample has no visit that searches for a word of its own", fw)
		}
	}
}
