package compare

import (
	"math"
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
		for _, w := range PlanWrites(5, p, 1.0/20) {
			often++
			if w.After < 0 || w.After >= len(p.Steps) {
				t.Fatalf("write after view %d of %d", w.After, len(p.Steps))
			}
			if !slices.ContainsFunc(WritableOrders, func(o WriteOrder) bool { return o.Order == w.Order.Order }) {
				t.Fatalf("write to %s", w.Order.Order)
			}
		}
		for _, w := range PlanWrites(5, p, 1.0/500) {
			rare[[2]int{p.ID, w.After}] = true
		}
	}
	if got := float64(often) / float64(views); math.Abs(got-0.05) > 0.005 {
		t.Errorf("write rate %.4f, want 0.05", got)
	}
	// The writes of the lower rate are among those of the higher rate, at the same views.
	for _, p := range plans {
		high := map[int]bool{}
		for _, w := range PlanWrites(5, p, 1.0/20) {
			high[w.After] = true
		}
		for _, w := range PlanWrites(5, p, 1.0/500) {
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
