package ssr_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/liustack/tifa/internal/ssr"
)

// warmPages returns the inputs of about 150 distinct pages of the shop: the pages a crawl of the
// running program reaches first in English, product pages for the most part, the same pages in
// Chinese, and the workbench pages it reaches.
func warmPages(t *testing.T) (string, []ssr.Input) {
	t.Helper()
	source, _ := shopBundle(t)
	var built struct{ BuildID string }
	data, err := os.ReadFile(filepath.Join(shopDir, ".tifa", "dist", "build.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &built); err != nil {
		t.Fatal(err)
	}
	base := startShop(t)
	get := func(url string) (*http.Response, []byte) {
		res, err := http.Get(base + url)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(res.Body)
		res.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		return res, body
	}
	// The pages answered with 200 at their own URL, in the order a crawl reaches them.
	var store, workbench []string
	seen := map[string]bool{}
	queue := append([]string(nil), crawlSeeds...)
	for len(queue) > 0 && len(store) < 52 {
		url := queue[0]
		queue = queue[1:]
		if seen[url] || strings.HasPrefix(url, "/zh-CN/") {
			continue
		}
		seen[url] = true
		res, body := get(url)
		queue = append(queue, crawlLinks(body)...)
		if res.StatusCode != http.StatusOK || res.Request.URL.RequestURI() != url {
			continue
		}
		switch {
		case strings.HasPrefix(url, "/store"):
			store = append(store, url)
		case strings.HasPrefix(url, "/projects/") && len(workbench) < 46:
			workbench = append(workbench, url)
		}
	}
	urls := append([]string(nil), store...)
	for _, url := range store {
		urls = append(urls, "/zh-CN"+url)
	}
	urls = append(urls, workbench...)
	var inputs []ssr.Input
	for _, url := range urls {
		// The number of Loads on the chain of the page: 2 for the storefront, 3 for the workbench.
		layers := 2
		if strings.HasPrefix(url, "/projects/") {
			layers = 3
		}
		res, body := get(dataURL(url, built.BuildID, strings.Repeat(",", layers-1)))
		if res.StatusCode != http.StatusOK {
			t.Fatalf("data of %s: %d %s", url, res.StatusCode, body)
		}
		data, texts := injectedInput(t, body)
		inputs = append(inputs, ssr.Input{BuildID: built.BuildID, Language: languageOf(url), URL: url, Data: data, Texts: texts, Session: sessionOf(t, body), SessionKey: "__Host-tifa_session:v1"})
	}
	return source, inputs
}

// countingHandler counts the records of the default logger whose message has text.
type countingHandler struct {
	slog.Handler
	text string
	n    *atomic.Int32
}

func (h countingHandler) Handle(ctx context.Context, r slog.Record) error {
	if strings.Contains(r.Message, h.text) {
		h.n.Add(1)
	}
	return h.Handler.Handle(ctx, r)
}

// countReplacements counts the VMs the pools replace for their size until the test ends.
func countReplacements(t *testing.T) *atomic.Int32 {
	var n atomic.Int32
	logger := slog.Default()
	// A handler of its own: the default one writes through the log package, which SetDefault sends back here.
	slog.SetDefault(slog.New(countingHandler{slog.NewTextHandler(os.Stderr, nil), "replaced a growing VM", &n}))
	t.Cleanup(func() { slog.SetDefault(logger) })
	return &n
}

// A VM of the shop grows as it renders new pages and stays at its new size: a fresh VM of the pool
// renders about 150 distinct pages three times without the pool replacing it, though the VM ends
// above 175% of its baseline.
func TestShopWarmVMIsNotReplaced(t *testing.T) {
	source, inputs := warmPages(t)
	if len(inputs) < 140 {
		t.Fatalf("only %d pages", len(inputs))
	}
	replaced := countReplacements(t)
	p := newPool(t, source, shopConfig(1))
	for range 3 {
		for _, in := range inputs {
			if _, err := p.Render(context.Background(), in); err != nil {
				t.Fatalf("%s: %v", in.URL, err)
			}
		}
	}
	// A replacement waits for the VM to be idle.
	time.Sleep(200 * time.Millisecond)
	if n := replaced.Load(); n != 0 {
		t.Errorf("the pool replaced its warm VM %d times", n)
	}
	// The same pages take a VM past 175% of its baseline, the baseline part of the pool's growth bound (hardBound), so a smaller ssr.vm_memory would replace it.
	cfg := shopConfig(1)
	bytecode, err := ssr.Compile(source, uintptr(cfg.VMMemory), cfg.Timeout)
	if err != nil {
		t.Fatal(err)
	}
	defer bytecode.Close()
	vm, err := ssr.Load(bytecode, uintptr(cfg.VMMemory), uintptr(cfg.VMStackDepth), cfg.Timeout, false)
	if err != nil {
		t.Fatal(err)
	}
	defer vm.Close()
	baseline := vm.MemorySize()
	for _, in := range inputs {
		data, err := json.Marshal(in)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ssr.RenderVM(vm, data, in.Texts, cfg.Timeout); err != nil {
			t.Fatalf("%s: %v", in.URL, err)
		}
		vm.RunYoungGC()
	}
	live := ssr.VMMemory(vm.VM, true).MallocSize
	t.Logf("%d pages: a VM holds %.1f MB after them, %.2f times its baseline of %.1f MB", len(inputs), float64(live)/1e6, float64(live)/float64(baseline), float64(baseline)/1e6)
	if uintptr(live) <= baseline+baseline/2+baseline/4 {
		t.Errorf("the pages took a VM to %d bytes, not past 175%% of its baseline %d: the test tests nothing", live, baseline)
	}
}

var priceCents = regexp.MustCompile(`"priceCents":\d+`)

// The caches of a VM can take thousands of renders to fill, and the VM grows meanwhile: with the
// price of every product new on every render, each storefront page formats amounts the VM has not
// seen, and i18next keeps a formatter for each. A VM of the shop does that for its storefront pages
// 3,000 times without the pool replacing it.
func TestShopChurningVMIsNotReplaced(t *testing.T) {
	source, inputs := warmPages(t)
	var store []ssr.Input
	for _, in := range inputs {
		if !strings.HasPrefix(in.URL, "/projects/") {
			store = append(store, in)
		}
	}
	replaced := countReplacements(t)
	p := newPool(t, source, shopConfig(1))
	for n := range 3000 {
		in := store[n%len(store)]
		texts := make([]string, len(in.Texts))
		for i, text := range in.Texts {
			k := 0
			texts[i] = priceCents.ReplaceAllStringFunc(text, func(string) string {
				k++
				return fmt.Sprintf(`"priceCents":%d`, 100+(n*37+k*7919)%200000)
			})
		}
		in.Texts = texts
		if _, err := p.Render(context.Background(), in); err != nil {
			t.Fatalf("render %d, %s: %v", n, in.URL, err)
		}
	}
	time.Sleep(200 * time.Millisecond)
	if n := replaced.Load(); n != 0 {
		t.Errorf("the pool replaced its VM %d times", n)
	}
}
