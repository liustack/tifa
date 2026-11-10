package compare_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/liustack/tifa/test/compare"
)

// The TypeScript port reads the catalogue of the shop from the shop's own file: its products are the
// records of the file, in its order.
func TestThePortReadsTheShopsCatalogue(t *testing.T) {
	if why := compare.NodeRunsTypeScript(); why != "" {
		t.Skip(why)
	}
	env, err := compare.NewEnv()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(env.Root, compare.ShopCatalogue))
	if err != nil {
		t.Fatal(err)
	}
	var file []struct {
		Slug string `json:"slug"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatal(err)
	}
	var want []string
	for _, p := range file {
		want = append(want, p.Slug)
	}
	port := filepath.Join(env.Root, "test", "compare", "domain", "store", "products", "catalogue.ts")
	out, err := exec.Command("node", "--input-type=module", "-e", "const m = await import(process.argv[1]); console.log(m.catalogue.map((p) => p.slug).join('\\n'))", port).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	if got := strings.Fields(string(out)); !slices.Equal(got, want) || len(want) == 0 {
		t.Errorf("the port has %d products %v, the file %d %v", len(got), got, len(want), want)
	}
}

// The products the plans visit and the writes buy are those of the shop's catalogue file, in its
// order, with its stock, colours and sizes.
func TestThePlansTakeTheProductsOfTheShopsCatalogue(t *testing.T) {
	env, err := compare.NewEnv()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(env.Root, compare.ShopCatalogue))
	if err != nil {
		t.Fatal(err)
	}
	var file []struct {
		Slug   string   `json:"slug"`
		Stock  int      `json:"stock"`
		Colors []string `json:"colors"`
		Sizes  []string `json:"sizes"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatal(err)
	}
	variants := compare.StoreVariants()
	if len(variants) != len(file) || len(compare.StoreProducts()) != len(file) {
		t.Fatalf("%d variants and %d products, the file has %d", len(variants), len(compare.StoreProducts()), len(file))
	}
	for i, f := range file {
		v := variants[i]
		if compare.StoreProducts()[i] != f.Slug || v.Slug != f.Slug || v.Stock != f.Stock || !slices.Equal(v.Colors, f.Colors) || !slices.Equal(v.Sizes, f.Sizes) {
			t.Errorf("product %d is %s %+v, the file has %+v", i+1, compare.StoreProducts()[i], v, f)
		}
	}
}
