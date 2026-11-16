package ssr_test

// Instructions of one render of each page of the shop, counted by callgrind. Run it with
// scripts/count-instructions.sh, which builds this test, runs it under valgrind and reads the
// dumps. Build the shop first (see shop_test.go).
//
// One VM renders every page TIFA_COUNT_WARM times, then each page in turn TIFA_COUNT_TIMED times
// with the counters of callgrind zeroed before and dumped after. The window holds the renders
// and nothing of the start-up, so the count a render is the count of the dump over TIFA_COUNT_TIMED.
// GOGC is off in the script, so the collector of Go does not run in one window and not in the next.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liustack/tifa/internal/ssr"
	"github.com/liustack/tifa/test/ssr/cg"
)

// countPages are the pages that are counted: the six of the workbench, then the seven of the
// storefront in each of its two languages. layers is the number of Loads on the chain of the page.
var countPages = []struct {
	name, url string
	layers    int
}{
	{"wb-studio", "/projects/studio", 3},
	{"wb-weekend", "/projects/weekend", 3},
	{"wb-order-studio", "/projects/studio/orders/SH-1042", 3},
	{"wb-order-weekend", "/projects/weekend/orders/WK-0201", 3},
	{"wb-stats-studio", "/projects/studio/stats", 3},
	{"wb-stats-weekend", "/projects/weekend/stats", 3},
	{"store-en-US-home", "/store", 2},
	{"store-en-US-products", "/store/products", 2},
	{"store-en-US-product", "/store/products/pour-over-kettle", 2},
	{"store-en-US-cart", "/store/cart", 2},
	{"store-en-US-checkout", "/store/checkout", 2},
	{"store-en-US-journal", "/store/journal", 2},
	{"store-en-US-post", "/store/journal/stock-sync-api", 2},
	{"store-zh-CN-home", "/zh-CN/store", 2},
	{"store-zh-CN-products", "/zh-CN/store/products", 2},
	{"store-zh-CN-product", "/zh-CN/store/products/pour-over-kettle", 2},
	{"store-zh-CN-cart", "/zh-CN/store/cart", 2},
	{"store-zh-CN-checkout", "/zh-CN/store/checkout", 2},
	{"store-zh-CN-journal", "/zh-CN/store/journal", 2},
	{"store-zh-CN-post", "/zh-CN/store/journal/stock-sync-api", 2},
}

// countInput is a page's input as the file TIFA_COUNT_INPUTS keeps it. Texts do not go into the JSON of an
// ssr.Input, so they come beside it.
type countInput struct {
	Input ssr.Input
	Texts []string
}

// fetchCountInputs asks the running shop for the data of every page of countPages.
func fetchCountInputs(b *testing.B) []countInput {
	var built struct{ BuildID string }
	data, err := os.ReadFile(filepath.Join(shopDir, ".tifa", "dist", "build.json"))
	if err != nil {
		b.Fatalf("run tifa build in examples/shop first: %v", err)
	}
	if err := json.Unmarshal(data, &built); err != nil {
		b.Fatal(err)
	}
	base := startShop(b)
	var out []countInput
	for _, p := range countPages {
		req, err := http.NewRequest("GET", base+dataURL(p.url, built.BuildID, strings.Repeat(",", p.layers-1)), nil)
		if err != nil {
			b.Fatal(err)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			b.Fatal(err)
		}
		body, err := io.ReadAll(res.Body)
		res.Body.Close()
		if err != nil || res.StatusCode != 200 {
			b.Fatalf("data of %s: %d %v %s", p.url, res.StatusCode, err, body)
		}
		d, texts := injectedInput(b, body)
		out = append(out, countInput{ssr.Input{BuildID: built.BuildID, Language: languageOf(p.url), URL: p.url, Data: d, Session: sessionOf(b, body), SessionKey: "__Host-tifa_session:v1"}, texts})
	}
	return out
}

// BenchmarkCountPages has two parts. Without the file TIFA_COUNT_INPUTS it asks the running shop for the
// data of the pages, writes them to that file and stops: nothing is counted, so this part runs
// outside valgrind, where the shop's program as a child process can start and end freely. With the
// file it counts, under valgrind, and prints a line "count-page <name> <html bytes>" for each page,
// in the order of the dumps it asks callgrind for.
func BenchmarkCountPages(b *testing.B) {
	if os.Getenv("TIFA_SSR_SHOP") == "" {
		b.Skip("set TIFA_SSR_SHOP=1 after running tifa build in examples/shop")
	}
	file := os.Getenv("TIFA_COUNT_INPUTS")
	if file == "" {
		b.Fatal("set TIFA_COUNT_INPUTS to a file name: scripts/count-instructions.sh does it, and runs this under valgrind (Linux, amd64)")
	}
	if _, err := os.Stat(file); err != nil {
		data, err := json.Marshal(fetchCountInputs(b))
		if err != nil {
			b.Fatal(err)
		}
		if err := os.WriteFile(file, data, 0o644); err != nil {
			b.Fatal(err)
		}
		b.Skip("wrote the inputs of the pages")
	}
	if !cg.Running() && os.Getenv("TIFA_COUNT_NATIVE") == "" {
		b.Fatal("run this under valgrind --tool=callgrind: scripts/count-instructions.sh does it (Linux, amd64). TIFA_COUNT_NATIVE=1 renders without counting, to check the pages")
	}
	source, err := os.ReadFile(filepath.Join(shopDir, ".tifa", "dist", "ssr.js"))
	if err != nil {
		b.Fatalf("run tifa build in examples/shop first: %v", err)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		b.Fatal(err)
	}
	var kept []countInput
	if err := json.Unmarshal(data, &kept); err != nil {
		b.Fatal(err)
	}
	var inputs []ssr.Input
	var wire [][]byte
	for _, k := range kept {
		in := k.Input
		in.Texts = k.Texts
		in, input := serverInput(b, in)
		inputs = append(inputs, in)
		wire = append(wire, input)
	}

	cfg := shopConfig(1)
	cfg.Timeout = 10 * 3600 * 1e9 // valgrind is slow
	memory := uintptr(cfg.VMMemory)
	bytecode, err := ssr.Compile(string(source), memory, cfg.Timeout)
	if err != nil {
		b.Fatal(err)
	}
	defer bytecode.Close()
	vm, err := ssr.Load(bytecode, memory, uintptr(cfg.VMStackDepth), cfg.Timeout, false)
	if err != nil {
		b.Fatal(err)
	}
	defer vm.Close()

	render := func(i int) string {
		html, err := renderVM(vm, inputs[i], wire[i], cfg.Timeout)
		if err != nil {
			b.Fatalf("%s: %v", countPages[i].url, err)
		}
		if !strings.Contains(html, "data-tsr-stream-part") {
			b.Fatalf("wrong HTML for %s", countPages[i].url)
		}
		return html
	}
	for range envInt("TIFA_COUNT_WARM", 2) {
		for i := range countPages {
			render(i)
		}
	}
	timed := envInt("TIFA_COUNT_TIMED", 3)
	for i, p := range countPages {
		// A full GC before the window and no engine GC inside it, as the pool collects between renders and
		// sets a threshold that a render rarely reaches. Without this, a GC the engine runs when its
		// memory crosses the threshold lands in the window of whichever page crosses it, and moves up to
		// 20% of a page between two builds that differ in what they allocate.
		vm.RunGC()
		vm.SetGCThreshold(memory)
		var size int
		cg.Zero()
		for range timed {
			size = len(render(i))
		}
		cg.Dump()
		fmt.Printf("count-page %s %d\n", p.name, size)
	}
	b.ReportMetric(float64(len(countPages)), "pages")
}

// languageOf is the language the server hands the render for a URL of the shop: the storefront has
// en-US at /store and zh-CN at /zh-CN/store, and the workbench has none.
func languageOf(url string) string {
	switch {
	case strings.HasPrefix(url, "/zh-CN/store"):
		return "zh-CN"
	case strings.HasPrefix(url, "/store"):
		return "en-US"
	}
	return ""
}
