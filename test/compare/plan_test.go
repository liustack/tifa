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
