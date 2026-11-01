package compare

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// countsCode goes into order.go before ordersOf.
const countsCode = `// OrdersOfCalls counts the calls of ordersOf, for the comparison.
var OrdersOfCalls atomic.Int64

func init() {
	addr := os.Getenv("COMPARE_COUNTS_ADDR")
	if addr == "" {
		return
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		panic(err)
	}
	go http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("{\"ordersOf\":" + strconv.FormatInt(OrdersOfCalls.Load(), 10) + "}"))
	}))
}

`

// BuildCounted builds the shop a second time, into dir, with a counter of the calls of ordersOf and
// a path /compare-counts that answers with it, on the address the program finds in
// COMPARE_COUNTS_ADDR. The counter is added with go build's -overlay, so
// no file of the shop changes, and it is only in this build: the one that is measured is the
// plain bin/shop. It returns the path of the program.
//
// ordersOf is the filter, copy and sort every list, detail and stats query starts with, the same
// function as in the TypeScript port, which counts its calls too. The number of calls a page makes
// is the same in both when the two do the same work.
func (e Env) BuildCounted(ctx context.Context, log io.Writer, dir string) (string, error) {
	orders := filepath.Join(e.Shop, "app", "projects", "project+", "orders")
	file := filepath.Join(orders, "order.go")
	src, err := os.ReadFile(file)
	if err != nil {
		return "", err
	}
	text := string(src)
	// The counter is in order.go itself, since tifa reads the files of the app from the disk when
	// it checks the conventions and a file that only the overlay has would not be found.
	// The path that answers with the counter is served on a listener of its own, at the address in
	// COMPARE_COUNTS_ADDR: the shop has no OnRequest to add it to, and tifa finds hooks by reading
	// the files on the disk, which the overlay does not change.
	edits := [][2]string{
		{"\t\"sync\"\n", "\t\"net\"\n\t\"net/http\"\n\t\"os\"\n\t\"strconv\"\n\t\"sync\"\n\t\"sync/atomic\"\n"},
		{"func ordersOf(projectID string) []orderRecord {\n", countsCode + "func ordersOf(projectID string) []orderRecord {\n\tOrdersOfCalls.Add(1)\n"},
	}
	for _, edit := range edits {
		if !strings.Contains(text, edit[0]) {
			return "", fmt.Errorf("%s no longer has %q: update BuildCounted", file, strings.TrimSpace(edit[0]))
		}
		text = strings.Replace(text, edit[0], edit[1], 1)
	}
	patchedOrders := filepath.Join(dir, "patched-order.go")
	if err := os.WriteFile(patchedOrders, []byte(text), 0o644); err != nil {
		return "", err
	}
	replace := map[string]string{file: patchedOrders}

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
