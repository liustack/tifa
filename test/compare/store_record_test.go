package compare

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"os"
	"slices"
	"strings"
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
// word of the visit's own, a filter by a plain link of each part of the list, the form with one
// condition, with a price and another condition and with three, a sort, the next page once and twice,
// and a filter and a sort after a search.
func actSample(set PlanSet) []int {
	has := func(p Plan, ok func(PlanStep) bool) bool { return slices.ContainsFunc(p.Steps, ok) }
	conds := func(s PlanStep) []PlanCondition { return conditionsOf(s.Set) }
	wants := []func(Plan) bool{
		func(p Plan) bool { return len(p.Variants) > 0 && langOf(p.Steps[0].Page) == "en-US" },
		func(p Plan) bool { return len(p.Variants) > 0 && langOf(p.Steps[0].Page) == "zh-CN" },
		func(p Plan) bool {
			return has(p, func(s PlanStep) bool { return s.Term == SearchPlaceholder }) && len(p.Steps) > 2
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
// does. The test logs what each browser sent for each act.
func TestStorefrontRecorderDoesTheActsOfThePlans(t *testing.T) {
	env := recordEnv(t)
	plans, traces := recordSample(t, env, BrowseFrameworks[:4])
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
