package compare

import (
	"math"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func TestPlansAreTheSameForTheSameSeedAndDifferForAnother(t *testing.T) {
	a := GeneratePlans(7, 50, 4)
	b := GeneratePlans(7, 50, 4)
	c := GeneratePlans(8, 50, 4)
	if !slices.EqualFunc(a, b, func(x, y Plan) bool { return slices.Equal(x.Steps, y.Steps) }) {
		t.Error("the same seed gave different plans")
	}
	if slices.EqualFunc(a, c, func(x, y Plan) bool { return slices.Equal(x.Steps, y.Steps) }) {
		t.Error("another seed gave the same plans")
	}
}

func TestPlanShapes(t *testing.T) {
	for _, mean := range []float64{4, 2} {
		plans := GeneratePlans(1, 20000, mean)
		var views, reloads, later, firstHub int
		for _, p := range plans {
			views += len(p.Steps)
			if p.Steps[0].Kind != StepLoad {
				t.Fatalf("plan %d starts with %s", p.ID, p.Steps[0].Kind)
			}
			if k, _, _ := PageKind(p.Steps[0].Page); k != "order" {
				firstHub++
			}
			for i, s := range p.Steps[1:] {
				later++
				cur := p.Steps[i].Page
				switch s.Kind {
				case StepReload:
					reloads++
					if s.Page != cur {
						t.Fatalf("plan %d reloads %s on %s", p.ID, s.Page, cur)
					}
				case StepClick:
					if !slices.ContainsFunc(Links(cur), func(l Link) bool { return l.To == s.Page }) {
						t.Fatalf("plan %d clicks %s on %s, which has no such link", p.ID, s.Page, cur)
					}
				default:
					t.Fatalf("plan %d has a %s step after the first", p.ID, s.Kind)
				}
			}
		}
		if got := float64(views) / float64(len(plans)); math.Abs(got-mean) > 0.05*mean {
			t.Errorf("mean %v: %.2f page views a visit", mean, got)
		}
		if got := float64(reloads) / float64(later); math.Abs(got-0.1) > 0.01 {
			t.Errorf("mean %v: %.3f of the later views are reloads, want 0.1", mean, got)
		}
		if got := float64(firstHub) / float64(len(plans)); math.Abs(got-0.5) > 0.02 {
			t.Errorf("mean %v: %.3f of the visits start on a list or stats page, want 0.5", mean, got)
		}
	}
}

func TestEveryPageOfAPlanIsAPageOfTheApps(t *testing.T) {
	known := map[string]bool{}
	for _, p := range AllPages() {
		known[p.URL] = true
	}
	for _, p := range GeneratePlans(3, 2000, 4) {
		for _, s := range p.Steps {
			if !known[s.Page] {
				t.Fatalf("plan %d shows %s, which no app serves", p.ID, s.Page)
			}
		}
	}
}

func TestWritesGoToTheThreeOrdersThatAcceptThem(t *testing.T) {
	plans := GeneratePlans(5, 20000, 4)
	rare, often := map[[2]int]bool{}, 0
	views := 0
	for _, p := range plans {
		views += len(p.Steps)
		for _, w := range PlanWrites(5, p, 1.0/20, MixRename) {
			often++
			if w.After < 0 || w.After >= len(p.Steps) {
				t.Fatalf("write after view %d of %d", w.After, len(p.Steps))
			}
			if !slices.ContainsFunc(WritableOrders, func(o WriteOrder) bool { return o.Order == w.Order.Order }) {
				t.Fatalf("write to %s", w.Order.Order)
			}
		}
		for _, w := range PlanWrites(5, p, 1.0/500, MixRename) {
			rare[[2]int{p.ID, w.After}] = true
		}
	}
	if got := float64(often) / float64(views); math.Abs(got-0.05) > 0.005 {
		t.Errorf("write rate %.4f, want 0.05", got)
	}
	// The writes of the lower rate are among those of the higher rate, at the same views.
	for _, p := range plans {
		high := map[int]bool{}
		for _, w := range PlanWrites(5, p, 1.0/20, MixRename) {
			high[w.After] = true
		}
		for _, w := range PlanWrites(5, p, 1.0/500, MixRename) {
			if !high[w.After] {
				t.Fatalf("plan %d: a write at 1/500 after view %d is not in the writes at 1/20", p.ID, w.After)
			}
		}
	}
}

func TestFanOutWriteChangesTheCustomerAndTheDefaultOnlyTheAddress(t *testing.T) {
	o := WritableOrders[0]
	addr := WriteStep(nodePath, o, 12, false)
	fan := WriteStep(nodePath, o, 12, true)
	if !strings.Contains(string(addr.Body), o.Customer) || strings.Contains(string(fan.Body), `"customer":"`+o.Customer+`"`) {
		t.Errorf("address %s, fan-out %s", addr.Body, fan.Body)
	}
	if !strings.Contains(string(fan.Body), Address(12)) || fan.Method != "PATCH" || fan.Write == nil {
		t.Errorf("fan-out step %+v", fan)
	}
}

func TestOfficeWritesFollowTheSharesAndTheProjectsAreEven(t *testing.T) {
	plans := GeneratePlans(7, 20000, 4)
	kinds, projects, total := map[string]int{}, map[string]int{}, 0
	for _, p := range plans {
		for _, w := range PlanWrites(7, p, 1, MixOffice) {
			kinds[w.Kind]++
			projects[w.Project]++
			total++
			if w.Plan != p.ID {
				t.Fatalf("write of plan %d says plan %d", p.ID, w.Plan)
			}
		}
	}
	for _, s := range OfficeShares {
		if got := float64(kinds[s.Kind]) / float64(total); math.Abs(got-s.Share) > 0.01 {
			t.Errorf("%s is %.3f of the writes, want %.2f", s.Kind, got, s.Share)
		}
	}
	if kinds[WriteRename] != 0 || len(kinds) != 4 {
		t.Errorf("kinds %v", kinds)
	}
	if got := float64(projects["studio"]) / float64(total); math.Abs(got-0.5) > 0.01 || projects["weekend"]+projects["studio"] != total {
		t.Errorf("projects %v", projects)
	}
}

// A lower rate keeps the writes of a higher rate at the same views, whatever they are.
func TestOfficeWritesOfALowerRateAreAmongTheHigherRatesWithTheSameKind(t *testing.T) {
	for _, p := range GeneratePlans(7, 3000, 4) {
		high := map[int]PlannedWrite{}
		for _, w := range PlanWrites(7, p, 1.0/4, MixOffice) {
			high[w.After] = w
		}
		for _, w := range PlanWrites(7, p, 1.0/19, MixOffice) {
			if h, ok := high[w.After]; !ok || h != w {
				t.Fatalf("plan %d view %d: %+v at 1/19, %+v at 1/4", p.ID, w.After, w, h)
			}
		}
		again := PlanWrites(7, p, 1.0/4, MixOffice)
		if len(again) != len(high) {
			t.Fatalf("plan %d: two calls differ", p.ID)
		}
	}
}

func TestOfficeBodiesAreValidAndTheSameEveryTime(t *testing.T) {
	phone, tracking := regexp.MustCompile(`^1[3-9][0-9]{9}$`), regexp.MustCompile(`^[A-Za-z0-9]{8,20}$`)
	for plan := range 300 {
		for after := range 6 {
			for _, project := range []string{"studio", "weekend"} {
				c := OfficeBody(7, plan, after, WriteCreate, project).(map[string]any)
				if !reflect.DeepEqual(c, OfficeBody(7, plan, after, WriteCreate, project)) {
					t.Fatal("a body changed between two calls")
				}
				if !phone.MatchString(c["phone"].(string)) || c["customer"] == "" || c["address"] == "" {
					t.Fatalf("create %v", c)
				}
				items := c["items"].([]officeLine)
				if len(items) < 1 || len(items) > 3 {
					t.Fatalf("%d items", len(items))
				}
				seen := map[string]bool{}
				for _, l := range items {
					if !slices.Contains(shopProducts[project], l.Slug) || seen[l.Slug] || l.Quantity < 1 || l.Quantity > 5 {
						t.Fatalf("%s item %+v", project, l)
					}
					seen[l.Slug] = true
				}
				if s := OfficeBody(7, plan, after, WriteShip, project).(map[string]any); !tracking.MatchString(s["trackingNo"].(string)) || !slices.Contains(carrierCodes, s["carrier"].(string)) {
					t.Fatalf("ship %v", s)
				}
				if n := OfficeBody(7, plan, after, WriteNote, project).(map[string]any)["text"].(string); n == "" || len([]rune(n)) > 200 {
					t.Fatalf("note %q", n)
				}
				if r := OfficeBody(7, plan, after, WriteRecipient, project).(map[string]any); !phone.MatchString(r["phone"].(string)) || len([]rune(r["address"].(string))) > 100 {
					t.Fatalf("recipient %v", r)
				}
			}
		}
	}
	if OfficeAddress(1, 2) == OfficeAddress(1, 3) || OfficeAddress(1, 2) == OfficeAddress(2, 2) {
		t.Error("addresses repeat")
	}
}
