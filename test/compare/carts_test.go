package compare

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// shopMaxCarts is the cap of the cart store of the shop, in Go and in the ports, which a run must
// stay under: the shop drops the cart used longest ago beyond it.
func TestTheCapOfCartsIsTheShops(t *testing.T) {
	cap := regexp.MustCompile(`maxCarts\s*=\s*([0-9_]+)`)
	for _, path := range []string{"../../examples/shop/app/store/cart/cart.go", "domain/store/cart/cart.ts"} {
		src, err := os.ReadFile(filepath.FromSlash(path))
		if err != nil {
			t.Fatal(err)
		}
		m := cap.FindSubmatch(src)
		if m == nil {
			t.Fatalf("%s has no maxCarts", path)
		}
		n, _ := strconv.Atoi(strings.ReplaceAll(string(m[1]), "_", ""))
		if n != shopMaxCarts {
			t.Errorf("%s keeps %d carts, the comparison counts on %d", path, n, shopMaxCarts)
		}
	}
}

// Before a round the runner counts the most carts it can make: one for each returning visit (made
// before the clock), one for each new visit that writes to its cart, and the one of the check after
// it. A round that would make more than the shop keeps is an error before anything is sent, since
// the shop would drop carts the round counts on.
func TestARoundThatWouldMakeMoreCartsThanTheShopKeepsFails(t *testing.T) {
	add := PlannedWrite{Kind: WriteAdd, Store: &StoreRequest{Kind: WriteAdd, Method: "POST", Path: "/store/products/oak-tray", Body: []byte(`{}`), Status: 200}}
	wl := &Workload{
		Plans: []Plan{
			{ID: 0, Role: RoleShopper, Returning: true, Steps: make([]PlanStep, 2)},
			{ID: 1, Role: RoleShopper, Steps: make([]PlanStep, 2)},
			{ID: 2, Role: RoleShopper, Steps: make([]PlanStep, 2)},
			{ID: 3, Role: RoleStaff, Steps: make([]PlanStep, 2)},
		},
		Writes: map[[2]int]PlannedWrite{
			{1, 0}: add, {1, 1}: add, // two adds of one visit make one cart
			{3, 0}: {After: 0, Kind: WriteEdit, Project: "studio"},
		},
	}
	if got := wl.cartsOf(0, 8); got != 5 {
		t.Errorf("8 visits make %d carts at most, want 5: 2 returning, 2 with adds, 1 of the check", got)
	}
	if err := wl.cartsFit(0, 8, 5); err != nil {
		t.Errorf("5 carts in a shop that keeps 5: %v", err)
	}
	if err := wl.cartsFit(0, 8, 4); err == nil || !strings.Contains(err.Error(), "5 carts") {
		t.Errorf("5 carts in a shop that keeps 4: %v", err)
	}
}
