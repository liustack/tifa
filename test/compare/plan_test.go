package compare

import (
	"slices"
	"testing"
)

func TestPlanSetIsSeededAndBlocksAreBalanced(t *testing.T) {
	a, b, c := GeneratePlanSet(7, 3), GeneratePlanSet(7, 3), GeneratePlanSet(8, 3)
	same := func(x, y PlanSet) bool {
		return slices.EqualFunc(x.Plans, y.Plans, func(p, q Plan) bool { return slices.Equal(p.Steps, q.Steps) })
	}
	if !same(a, b) || same(a, c) {
		t.Fatal("the same seed must give the same plans and another seed other plans")
	}
	known := map[string]bool{}
	for _, p := range AllPages() {
		known[p.URL] = true
	}
	for blk := range 3 {
		var counts PlanCounts
		firstHub := 0
		orders := map[string]int{}
		for _, p := range a.Plans[blk*BlockVisits : (blk+1)*BlockVisits] {
			if p.Block != blk {
				t.Fatalf("plan %d is in block %d", p.ID, p.Block)
			}
			counts.Add(p, 0)
			if p.Steps[0].Kind != StepLoad {
				t.Fatalf("plan %d starts with %s", p.ID, p.Steps[0].Kind)
			}
			if k, _, o := PageKind(p.Steps[0].Page); k == "order" {
				orders[o]++
			} else {
				firstHub++
			}
			for i, s := range p.Steps {
				if !known[s.Page] {
					t.Fatalf("plan %d shows %s, which no app serves", p.ID, s.Page)
				}
				if i > 0 {
					if s.Kind != StepClick || !slices.ContainsFunc(Links(p.Steps[i-1].Page), func(l Link) bool { return l.To == s.Page }) {
						t.Fatalf("plan %d: %s -> %s is no click on a link", p.ID, p.Steps[i-1].Page, s.Page)
					}
				}
			}
		}
		if counts.Visits != BlockVisits || counts.Views != BlockViews || counts.Loads != BlockVisits || counts.Clicks != BlockViews-BlockVisits {
			t.Errorf("block %d: %+v", blk, counts)
		}
		if counts.ClickShare() != 0.75 {
			t.Errorf("block %d: click share %v", blk, counts.ClickShare())
		}
		if firstHub != 48 {
			t.Errorf("block %d: %d first pages are lists or stats, want 48", blk, firstHub)
		}
		for o, n := range orders {
			if n != 1 {
				t.Errorf("block %d: the first page of order %s is used %d times", blk, o, n)
			}
		}
	}
}

func TestBlockWritesGiveExactlyTheShareOfTheMix(t *testing.T) {
	set := GeneratePlanSet(3, 2)
	for _, tc := range []struct {
		share float64
		want  int
	}{{0.05, 20}, {0.20, 95}, {0.50, 380}} {
		for blk := range 2 {
			plans := set.Plans[blk*BlockVisits : (blk+1)*BlockVisits]
			ws, err := BlockWrites(3, plans, tc.share)
			if err != nil {
				t.Fatal(err)
			}
			if len(ws) != tc.want {
				t.Fatalf("share %v: %d writes, want %d", tc.share, len(ws), tc.want)
			}
			var counts PlanCounts
			for _, p := range plans {
				counts.Add(p, 0)
			}
			counts.Writes = len(ws)
			if got := counts.WriteShare(); got < tc.share-1e-9 || got > tc.share+1e-9 {
				t.Errorf("share %v: achieved %v", tc.share, got)
			}
			perOrder := map[string]int{}
			seen := map[[2]int]bool{}
			for _, w := range ws {
				perOrder[w.Order.Order]++
				k := [2]int{w.Plan, w.After}
				if seen[k] {
					t.Fatalf("two writes after page view %v", k)
				}
				seen[k] = true
				if w.Plan/BlockVisits != blk || w.After >= len(plans[w.Plan%BlockVisits].Steps) {
					t.Fatalf("write %+v outside block %d", w, blk)
				}
			}
			lo, hi := 1<<30, 0
			for _, o := range WritableOrders {
				lo, hi = min(lo, perOrder[o.Order]), max(hi, perOrder[o.Order])
			}
			if hi-lo > 1 {
				t.Errorf("share %v: writes per order %v", tc.share, perOrder)
			}
		}
	}
	if _, err := BlockWrites(3, set.Plans[:BlockVisits], 0.3); err == nil {
		t.Error("a share that needs a fraction of a write was accepted")
	}
}
