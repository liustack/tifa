package compare

import (
	"math/rand/v2"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func scenario(t *testing.T, name string) BrowseScenario {
	t.Helper()
	sc, ok := BrowseScenarioByName(name)
	if !ok {
		t.Fatalf("no scenario %q", name)
	}
	return sc
}

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
			ws, err := BlockWrites(3, plans, BrowseScenario{Share: tc.share, Mix: MixRename})
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
				if w.Kind != WriteRename || w.Project != w.Order.Project {
					t.Fatalf("rename write %+v", w)
				}
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
	if _, err := BlockWrites(3, set.Plans[:BlockVisits], BrowseScenario{Share: 0.3, Mix: MixRename}); err == nil {
		t.Error("a share that needs a fraction of a write was accepted")
	}
}

// The admin writes of a block come by quota: the kinds by their shares (largest remainders for
// what does not divide) and the projects half and half, at the same page views for every mix of
// writes with the same share.
func TestAdminWritesOfABlockFollowTheSharesByQuota(t *testing.T) {
	set := GeneratePlanSet(7, 2)
	for _, tc := range []struct {
		name  string
		kinds map[string]int
	}{
		{"admin-w20", map[string]int{WriteCreate: 38, WriteShip: 29, WriteNote: 19, WriteRecipient: 9}},
		{"admin-w50", map[string]int{WriteCreate: 152, WriteShip: 114, WriteNote: 76, WriteRecipient: 38}},
	} {
		sc := scenario(t, tc.name)
		for blk := range 2 {
			plans := set.Plans[blk*BlockVisits : (blk+1)*BlockVisits]
			ws, err := BlockWrites(7, plans, sc)
			if err != nil {
				t.Fatal(err)
			}
			kinds, projects := map[string]int{}, map[string]int{}
			for _, w := range ws {
				kinds[w.Kind]++
				projects[w.Project]++
				if w.Order != (WriteOrder{}) || w.Store != nil {
					t.Fatalf("an admin write names its order: %+v", w)
				}
			}
			if !reflect.DeepEqual(kinds, tc.kinds) {
				t.Errorf("%s block %d: kinds %v, want %v", tc.name, blk, kinds, tc.kinds)
			}
			if d := projects["studio"] - projects["weekend"]; d < -1 || d > 1 || projects["studio"]+projects["weekend"] != len(ws) {
				t.Errorf("%s block %d: projects %v", tc.name, blk, projects)
			}
			// The page views are those of the rename mix of the same share.
			rename, err := BlockWrites(7, plans, BrowseScenario{Share: sc.Share, Mix: MixRename})
			if err != nil {
				t.Fatal(err)
			}
			for i := range ws {
				if ws[i].Plan != rename[i].Plan || ws[i].After != rename[i].After {
					t.Fatalf("%s: write %d after %d/%d, the rename mix after %d/%d", tc.name, i, ws[i].Plan, ws[i].After, rename[i].Plan, rename[i].After)
				}
			}
		}
	}
	a, _ := BlockWrites(7, set.Plans[:BlockVisits], scenario(t, "admin-w20"))
	b, _ := BlockWrites(7, set.Plans[:BlockVisits], scenario(t, "admin-w20"))
	if !reflect.DeepEqual(a, b) {
		t.Error("two calls give other writes")
	}
}

func TestQuotaGivesEachKindItsShareAndTheRestByTheLargestRemainder(t *testing.T) {
	for _, tc := range []struct {
		n      int
		shares []float64
		want   []int
	}{
		{95, []float64{0.5, 0.5}, []int{48, 47}},
		{20, []float64{0.4, 0.3, 0.2, 0.1}, []int{8, 6, 4, 2}},
		{95, []float64{0.4, 0.3, 0.2, 0.1}, []int{38, 29, 19, 9}},
		{95, []float64{0.25, 0.35, 0.15, 0.05, 0.10, 0.10}, []int{24, 33, 14, 5, 10, 9}},
	} {
		kinds := []string{"a", "b", "c", "d", "e", "f"}[:len(tc.shares)]
		got := quota(newRand(1), tc.n, kinds, tc.shares)
		counts := make([]int, len(kinds))
		for _, k := range got {
			counts[slices.Index(kinds, k)]++
		}
		if !slices.Equal(counts, tc.want) {
			t.Errorf("%d by %v: %v, want %v", tc.n, tc.shares, counts, tc.want)
		}
	}
}

func TestAdminBodiesAreValidAndTheSameEveryTime(t *testing.T) {
	phone, tracking := regexp.MustCompile(`^1[3-9][0-9]{9}$`), regexp.MustCompile(`^[A-Za-z0-9]{8,20}$`)
	for plan := range 300 {
		for after := range 6 {
			for _, project := range []string{"studio", "weekend"} {
				c := AdminBody(7, plan, after, WriteCreate, project).(map[string]any)
				if !reflect.DeepEqual(c, AdminBody(7, plan, after, WriteCreate, project)) {
					t.Fatal("a body changed between two calls")
				}
				if !phone.MatchString(c["phone"].(string)) || c["customer"] == "" || c["address"] == "" {
					t.Fatalf("create %v", c)
				}
				items := c["items"].([]adminLine)
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
				if s := AdminBody(7, plan, after, WriteShip, project).(map[string]any); !tracking.MatchString(s["trackingNo"].(string)) || !slices.Contains(carrierCodes, s["carrier"].(string)) {
					t.Fatalf("ship %v", s)
				}
				if n := AdminBody(7, plan, after, WriteNote, project).(map[string]any)["text"].(string); n == "" || len([]rune(n)) > 200 {
					t.Fatalf("note %q", n)
				}
				if r := AdminBody(7, plan, after, WriteRecipient, project).(map[string]any); !phone.MatchString(r["phone"].(string)) || len([]rune(r["address"].(string))) > 100 {
					t.Fatalf("recipient %v", r)
				}
			}
		}
	}
	if AdminAddress(1, 2) == AdminAddress(1, 3) || AdminAddress(1, 2) == AdminAddress(2, 2) {
		t.Error("addresses repeat")
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

func TestEveryWriteGivesANewCustomerName(t *testing.T) {
	seen := map[string]int{}
	for n := 0; n < 20000; n++ {
		name := CustomerName(n)
		if prev, ok := seen[name]; ok {
			t.Fatalf("writes %d and %d give the same customer name %q", prev, n, name)
		}
		seen[name] = n
		if strings.TrimSpace(name) == "" || len([]rune(name)) > 40 {
			t.Fatalf("name %q", name)
		}
	}
	// The write step carries it, for every order, so a name never comes back after other writes.
	a, b := WriteStep(nodePath, WritableOrders[0], 1, true), WriteStep(nodePath, WritableOrders[0], 7, true)
	if string(a.Body) == string(b.Body) || a.Write.Customer == b.Write.Customer {
		t.Errorf("writes 1 and 7 (the same surname before) give the same body: %s %s", a.Body, b.Body)
	}
}

func newRand(seed uint64) *rand.Rand { return rand.New(rand.NewPCG(seed, 0)) }

// The read-only control is a scenario with no writes: its blocks get none.
func TestAScenarioWithNoWritesGetsNone(t *testing.T) {
	sc := scenario(t, "admin-w20")
	sc.Share = 0
	ws, err := BlockWrites(7, GeneratePlanSet(7, 1).Plans, sc)
	if err != nil || len(ws) != 0 {
		t.Errorf("%d writes, %v", len(ws), err)
	}
}
