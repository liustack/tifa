package compare

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// countsCode is the counters of one file of the shop and the listener that answers with them. Each
// counter is a pair: the name of the variable and its name in the JSON the listener answers with.
// env is the variable that holds the address to listen on.
func countsCode(env string, counters ...[2]string) string {
	var b strings.Builder
	for _, c := range counters {
		fmt.Fprintf(&b, "// %s counts the calls of a function, for the comparison.\nvar %s atomic.Int64\n\n", c[0], c[0])
	}
	b.WriteString("func init() {\n\taddr := os.Getenv(\"" + env + "\")\n\tif addr == \"\" {\n\t\treturn\n\t}\n")
	b.WriteString("\tln, err := net.Listen(\"tcp\", addr)\n\tif err != nil {\n\t\tpanic(err)\n\t}\n")
	b.WriteString("\tgo http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {\n\t\tw.Header().Set(\"Content-Type\", \"application/json\")\n")
	b.WriteString("\t\tw.Write([]byte(\"{\"")
	for i, c := range counters {
		sep := ","
		if i == 0 {
			sep = ""
		}
		fmt.Fprintf(&b, " + \"%s\\\"%s\\\":\" + strconv.FormatInt(%s.Load(), 10)", sep, c[1], c[0])
	}
	b.WriteString(" + \"}\"))\n\t}))\n}\n\n")
	return b.String()
}

// The environment variables that hold the addresses the counters answer on.
const (
	OrdersCountsEnv = "COMPARE_COUNTS_ADDR"
	StoreCountsEnv  = "COMPARE_STORE_COUNTS_ADDR"
)

// BuildCounted builds the shop a second time, into dir, with counters and a path for each group
// that answers with them on the address in an environment variable:
//
//   - ordersOf: the calls of ordersOf, the filter, copy and sort every list, detail and stats
//     query of the workbench starts with (COMPARE_COUNTS_ADDR);
//   - findLocale: the calls of FindLocale, which every Layout and Load of the storefront makes
//     once, and products: the products built whole (description, care, specs and spec groups
//     included), which is what GetProduct does (COMPARE_STORE_COUNTS_ADDR).
//
// The counters are added with go build's -overlay, so no file of the shop changes, and they are
// only in this build: the one that is measured is the plain bin/shop. It returns the path of the
// program. The TypeScript port counts the same work. The numbers are the same for a page when the
// two do the same work.
func (e Env) BuildCounted(ctx context.Context, log io.Writer, dir string) (string, error) {
	// The counters are in the files of the shop itself, since tifa reads the files of the app from
	// the disk when it checks the conventions and a file that only the overlay has would not be
	// found. The paths that answer are served on listeners of their own: the shop's OnRequest
	// only guards the workbench, and tifa finds hooks by reading the files on the disk, which the
	// overlay does not change.
	imports := "\t\"net\"\n\t\"net/http\"\n\t\"os\"\n\t\"strconv\"\n\t\"sync/atomic\"\n"
	files := []struct {
		path  string
		edits [][2]string
	}{
		{filepath.Join(e.Shop, "app", "projects", "project+", "orders", "order.go"), [][2]string{
			{"\t\"sync\"\n", imports + "\t\"sync\"\n"},
			{"func ordersOf(projectID string) []orderRecord {\n", countsCode(OrdersCountsEnv, [2]string{"OrdersOfCalls", "ordersOf"}) + "func ordersOf(projectID string) []orderRecord {\n\tOrdersOfCalls.Add(1)\n"},
		}},
		{filepath.Join(e.Shop, "app", "store", "storekit", "storekit.go"), [][2]string{
			{"\t\"math\"\n", "\t\"math\"\n" + imports},
			{"func FindLocale(code string) (Locale, error) {\n", countsCode(StoreCountsEnv, [2]string{"FindLocaleCalls", "findLocale"}, [2]string{"ProductBuilds", "products"}) + "func FindLocale(code string) (Locale, error) {\n\tFindLocaleCalls.Add(1)\n"},
		}},
		{filepath.Join(e.Shop, "app", "store", "products", "product.go"), [][2]string{
			{"func (p productRecord) product(l storekit.Locale) Product {\n", "func (p productRecord) product(l storekit.Locale) Product {\n\tstorekit.ProductBuilds.Add(1)\n"},
		}},
	}
	replace := map[string]string{}
	for i, f := range files {
		src, err := os.ReadFile(f.path)
		if err != nil {
			return "", err
		}
		text := string(src)
		for _, edit := range f.edits {
			if !strings.Contains(text, edit[0]) {
				return "", fmt.Errorf("%s no longer has %q: update BuildCounted", f.path, strings.TrimSpace(edit[0]))
			}
			text = strings.Replace(text, edit[0], edit[1], 1)
		}
		patched := filepath.Join(dir, fmt.Sprintf("patched-%d-%s", i, filepath.Base(f.path)))
		if err := os.WriteFile(patched, []byte(text), 0o644); err != nil {
			return "", err
		}
		replace[f.path] = patched
	}

	overlay, err := json.Marshal(map[string]any{"Replace": replace})
	if err != nil {
		return "", err
	}
	overlayFile := filepath.Join(dir, "overlay.json")
	if err := os.WriteFile(overlayFile, overlay, 0o644); err != nil {
		return "", err
	}
	exe := filepath.Join(dir, "shop-counted")
	env := []string{"GOFLAGS=" + strings.TrimSpace(os.Getenv("GOFLAGS")+" -overlay="+overlayFile)}
	if err := run(ctx, log, e.Shop, env, "go", "tool", "tifa", "build", "-o", exe); err != nil {
		return "", err
	}
	return exe, nil
}
