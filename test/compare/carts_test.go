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
