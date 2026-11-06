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

// countsCode is a counter of the calls of one function and the listener that answers with it. It
// goes into a file of the shop before that function. calls is the name of the counter, key its
// name in the JSON the listener answers with, and env the variable that holds the address to
// listen on.
func countsCode(calls, key, env string) string {
	return strings.NewReplacer("CALLS", calls, "KEY", key, "ENV", env).Replace(`// CALLS counts the calls of a function, for the comparison.
var CALLS atomic.Int64

func init() {
	addr := os.Getenv("ENV")
	if addr == "" {
		return
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		panic(err)
	}
	go http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("{\"KEY\":" + strconv.FormatInt(CALLS.Load(), 10) + "}"))
	}))
}

`)
}

// The environment variables that hold the addresses the counters answer on.
const (
	OrdersCountsEnv = "COMPARE_COUNTS_ADDR"
	StoreCountsEnv  = "COMPARE_STORE_COUNTS_ADDR"
)

// BuildCounted builds the shop a second time, into dir, with two counters, each with a path that
// answers with it on the address in an environment variable: the calls of ordersOf (the filter,
// copy and sort every list, detail and stats query of the workbench starts with, on
// COMPARE_COUNTS_ADDR) and the calls of FindLocale, which every Layout, Load and write of the
// storefront makes once (on COMPARE_STORE_COUNTS_ADDR). The counters are added with go build's
// -overlay, so no file of the shop changes, and they are only in this build: the one that is
// measured is the plain bin/shop. It returns the path of the program.
//
// The TypeScript port counts the calls of the same two functions. The number of calls a page makes
// is the same in both when the two do the same work.
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
			{"func ordersOf(projectID string) []orderRecord {\n", countsCode("OrdersOfCalls", "ordersOf", OrdersCountsEnv) + "func ordersOf(projectID string) []orderRecord {\n\tOrdersOfCalls.Add(1)\n"},
		}},
		{filepath.Join(e.Shop, "app", "store", "store.go"), [][2]string{
			{"\t\"math\"\n", "\t\"math\"\n" + imports},
			{"func FindLocale(code string) (Locale, error) {\n", countsCode("FindLocaleCalls", "findLocale", StoreCountsEnv) + "func FindLocale(code string) (Locale, error) {\n\tFindLocaleCalls.Add(1)\n"},
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
