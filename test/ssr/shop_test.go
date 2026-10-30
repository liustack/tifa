// Package ssr_test runs the SSR pool of internal/ssr on the real React bundle of the example app
// in examples/shop, which it treats as an app from outside: it reads the build output and asks the
// built program for the data of each page. The tests skip unless TIFA_SSR_SHOP=1. Build the shop
// first, then run:
//
//	cd examples/shop && pnpm install --frozen-lockfile && go tool tifa build
//	TIFA_SSR_SHOP=1 go test -run Shop -v ./test/ssr
package ssr_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/liustack/slowjs"
	"github.com/liustack/tifa/internal/config"
	"github.com/liustack/tifa/internal/ssr"
	"github.com/liustack/tifa/internal/testproc"
)

const shopDir = "../../examples/shop"

type shopPage struct {
	input           ssr.Input
	want, forbidden string
}

// shopRoutes are the comparison pages: the four of the prototype baseline, a list and a detail
// page of each project, then the stats page of each project. Each comes with the route IDs of its
// match chain, a text its HTML must have and one of the other project that it must not have.
var shopRoutes = []struct{ url, routes, want, forbidden string }{
	{"/projects/studio", "app/layout,app/projects/project+/layout,app/projects/project+/route", "SH-1042", "WK-0201"},
	{"/projects/weekend", "app/layout,app/projects/project+/layout,app/projects/project+/route", "WK-0201", "SH-1042"},
	{"/projects/studio/orders/SH-1042", "app/layout,app/projects/project+/layout,app/projects/project+/orders/order+/route", "SH-1042", "WK-0201"},
	{"/projects/weekend/orders/WK-0201", "app/layout,app/projects/project+/layout,app/projects/project+/orders/order+/route", "WK-0201", "SH-1042"},
	{"/projects/studio/stats", "app/layout,app/projects/project+/layout,app/projects/project+/stats/route", "¥14,134.00", "¥1,689.00"},
	{"/projects/weekend/stats", "app/layout,app/projects/project+/layout,app/projects/project+/stats/route", "¥1,689.00", "¥14,134.00"},
}

// shopBundle returns the shop's SSR bundle and its comparison pages. The data of each
// page is what the shop's own program injects: the test runs bin/shop and asks it for the
// navigation data of each page, and writes the data of each route as JSON text, as the program
// injects it (see injectedInput).
func shopBundle(t testing.TB) (string, []shopPage) {
	t.Helper()
	if os.Getenv("TIFA_SSR_SHOP") == "" {
		t.Skip("set TIFA_SSR_SHOP=1 after running tifa build in examples/shop")
	}
	source, err := os.ReadFile(filepath.Join(shopDir, ".tifa", "dist", "ssr.js"))
	if err != nil {
		t.Fatalf("run tifa build in examples/shop first: %v", err)
	}
	var built struct{ BuildID string }
	data, err := os.ReadFile(filepath.Join(shopDir, ".tifa", "dist", "build.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &built); err != nil {
		t.Fatal(err)
	}
	base := startShop(t)
	var pages []shopPage
	for _, r := range shopRoutes {
		req, err := http.NewRequest("GET", base+r.url, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("X-Tifa-Route", r.routes)
		req.Header.Set("X-Tifa-Build", built.BuildID)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(res.Body)
		res.Body.Close()
		if err != nil || res.StatusCode != 200 {
			t.Fatalf("data of %s: %d %v %s (rebuild the shop when its build.json changed)", r.url, res.StatusCode, err, body)
		}
		data, texts := injectedInput(t, body)
		pages = append(pages, shopPage{ssr.Input{BuildID: built.BuildID, URL: r.url, Data: data, Texts: texts}, r.want, r.forbidden})
	}
	return string(source), pages
}

// injectedInput turns the answer of a navigation data request into the data a whole-page render
// gets, as the server writes it: the statuses, errors and redirects as JSON, and the data of each
// successful route as JSON text of its own, with the escapes of ssr.EscapeAboveFFFF the server
// writes into it. A result says the number of its text, which follows the order of the route IDs.
func injectedInput(t testing.TB, answer []byte) (json.RawMessage, []string) {
	t.Helper()
	var routes map[string]struct {
		Status   int             `json:"status"`
		Data     json.RawMessage `json:"data,omitempty"`
		Error    string          `json:"error,omitempty"`
		Location string          `json:"location,omitempty"`
	}
	if err := json.Unmarshal(answer, &routes); err != nil {
		t.Fatal(err)
	}
	type result struct {
		Status   int    `json:"status"`
		Text     *int   `json:"text,omitempty"`
		Error    string `json:"error,omitempty"`
		Location string `json:"location,omitempty"`
	}
	out := make(map[string]result, len(routes))
	var texts []string
	for _, id := range slices.Sorted(maps.Keys(routes)) {
		r := routes[id]
		res := result{Status: r.Status, Error: r.Error, Location: r.Location}
		if r.Status == http.StatusOK {
			text := string(ssr.EscapeAboveFFFF(r.Data))
			if text == "" { // a nil Data, left out of the answer
				text = "null"
			}
			number := len(texts)
			res.Text = &number
			texts = append(texts, text)
		}
		out[id] = res
	}
	data, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	return data, texts
}

// injectedInput writes the data of a route as the server does, characters above U+FFFF as the
// escapes of their surrogate pairs among it, and apart from the JSON that numbers it.
func TestInjectedInputIsWhatTheServerInjects(t *testing.T) {
	data, texts := injectedInput(t, []byte(`{"app/route":{"status":200,"data":{"s":"a😀"}},"app/gone":{"status":404,"error":"Not Found"},"app/other":{"status":200,"data":[1]}}`))
	if want := `{"app/gone":{"status":404,"error":"Not Found"},"app/other":{"status":200,"text":0},"app/route":{"status":200,"text":1}}`; string(data) != want {
		t.Errorf("data = %s, want %s", data, want)
	}
	if want := []string{`[1]`, `{"s":"a\ud83d\ude00"}`}; !slices.Equal(texts, want) {
		t.Errorf("texts = %q, want %q", texts, want)
	}
}

var listening = regexp.MustCompile(`listening on (http://\S+)`)

// startShop runs the shop's program built by tifa build and returns its base URL.
func startShop(t testing.TB) string {
	t.Helper()
	return startProgram(t, filepath.Join(shopDir, "bin", "shop"))
}

// startProgram runs a program built by tifa build and returns its base URL. The system ends the
// program when this process ends (see testproc.Start), also when a panic in another goroutine or a
// timeout ends it without running t.Cleanup.
func startProgram(t testing.TB, exe string) string {
	t.Helper()
	cmd := exec.Command(exe, "--server-addr", "127.0.0.1:0")
	// The program rejects unknown TIFA_* variables as settings, TIFA_SSR_SHOP among them.
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "TIFA_") {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := testproc.Start(cmd); err != nil {
		t.Fatalf("run tifa build first: %v", err)
	}
	t.Cleanup(func() {
		cmd.Process.Kill()
		cmd.Wait()
	})
	found := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			if m := listening.FindStringSubmatch(scanner.Text()); m != nil {
				found <- m[1]
			}
		}
	}()
	select {
	case base := <-found:
		return base
	case <-time.After(10 * time.Second):
		t.Fatalf("%s did not start listening", exe)
		return ""
	}
}

func (page shopPage) check(t testing.TB, html string) {
	t.Helper()
	if !page.matches(html) {
		t.Fatalf("wrong HTML for %s", page.input.URL)
	}
}

// matches reports whether html is this page: it has the page's text, not the other project's,
// and the router's data script.
func (page shopPage) matches(html string) bool {
	return strings.Contains(html, page.want) && !strings.Contains(html, page.forbidden) && strings.Contains(html, "data-tsr-stream-part")
}

func TestShopRendersReadyData(t *testing.T) {
	source, pages := shopBundle(t)
	p := newPool(t, source, shopConfig(1))
	detail := pages[2]
	if detail.input.URL != "/projects/studio/orders/SH-1042" {
		t.Fatalf("unexpected page order: %s", detail.input.URL)
	}
	html, err := p.Render(context.Background(), detail.input)
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"SH-1042", "林夏", "晨间手冲壶", ">山海工作室</h1>", "2</span> 单待发货", "data-tsr-stream-part", "确认发货"} {
		if !strings.Contains(html, text) {
			t.Errorf("missing %q", text)
		}
	}
	for _, page := range pages {
		html, err := p.Render(context.Background(), page.input)
		if err != nil {
			t.Fatal(err)
		}
		page.check(t, html)
	}
}

var (
	renderTime = regexp.MustCompile(`u:\d{13}`)
	// scriptBreak is where the router starts a script tag of its own for a text over 16K code units.
	scriptBreak = `</script><script data-tsr-stream-part="">`
	slotLeft    = regexp.MustCompile(`~[0-9a-f]{32}\.\d+\.`)
)

// sameButTheTime makes two renders of the same page comparable: the router writes the time of the
// render into each match, and splits its hydration script where a text is long.
func sameButTheTime(html string) string {
	return strings.ReplaceAll(renderTime.ReplaceAllString(html, "u:T"), scriptBreak, ";")
}

// The pool has the VM write a slot where the page carries the text of a Load's data and fills it
// afterwards. The page it returns is the page the VM writes when it gets no nonce and writes the
// text itself, for every page of the shop and for one whose data holds what could break a page.
func TestShopSlotsGiveTheSamePageAsTheVMDoes(t *testing.T) {
	source, pages := shopBundle(t)
	cfg := shopConfig(1)
	pool := newPool(t, source, cfg)
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
	inputs := make([]ssr.Input, 0, len(pages)+1)
	for _, page := range pages {
		inputs = append(inputs, page.input)
	}
	inputs = append(inputs, withCustomer(pages[2].input, "</script><script id=\"x\">alert(1)</script> \"q\" \\ \xe2\x80\xa8 \xe2\x80\xa9 \x07 😀"))
	for _, in := range inputs {
		got, err := pool.Render(context.Background(), in)
		if err != nil {
			t.Fatal(err)
		}
		input, err := json.Marshal(in)
		if err != nil {
			t.Fatal(err)
		}
		want, err := ssr.RenderVM(vm, input, in.Texts, cfg.Timeout)
		if err != nil {
			t.Fatal(err)
		}
		if slotLeft.MatchString(got) {
			t.Errorf("%s: the page has a slot that was not filled", in.URL)
		}
		// The pool filled slots where the VM wrote any: the order list holds a text of over 4,096 code units.
		slotted := in
		slotted.Nonce = ssr.NewNonce()
		input, err = json.Marshal(slotted)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := ssr.RenderVM(vm, input, in.Texts, cfg.Timeout)
		if err != nil {
			t.Fatal(err)
		}
		if in.URL == "/projects/studio" && !strings.Contains(raw, "~"+slotted.Nonce) {
			t.Errorf("%s: the VM wrote no slot", in.URL)
		}
		if sameButTheTime(got) != sameButTheTime(want) {
			t.Errorf("%s: the page differs from the page the VM writes by itself (%d and %d bytes)", in.URL, len(got), len(want))
		}
	}
}

func TestShopEscapesScriptClosingTags(t *testing.T) {
	source, pages := shopBundle(t)
	p := newPool(t, source, shopConfig(1))
	in := withCustomer(pages[2].input, `</script><script id="injected">alert(1)</script>`)
	if slices.Equal(in.Texts, pages[2].input.Texts) {
		t.Fatal("customer name not found in the snapshot")
	}
	html, err := p.Render(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, "alert(1)") {
		t.Fatal("injected customer is missing from the HTML")
	}
	if strings.Contains(html, `<script id="injected">`) {
		t.Fatal("unescaped script in HTML or dehydrated state")
	}
}

// slowestWarmRender is the most a warm render of a shop page may take in the default run. The
// shop's slowest page takes under 200 ms on a laptop, so this catches a render that became many
// times slower, not small changes: measure those with TIFA_SSR_MEASURE=1.
const slowestWarmRender = time.Second

// TestShopWarmLatency renders the comparison pages in one reused VM, timing the whole Render. By
// default it renders each page a few times, checks the HTML and that no warm render is slower than
// slowestWarmRender. With TIFA_SSR_MEASURE=1 it uses the method of the prototype baseline, 512
// warm renders then 2,048 timed ones, for numbers worth comparing.
func TestShopWarmLatency(t *testing.T) {
	source, pages := shopBundle(t)
	warm, timed := 2*len(pages), 8*len(pages)
	if os.Getenv("TIFA_SSR_MEASURE") != "" {
		warm, timed = 512, 2048
	}
	start := time.Now()
	p := newPool(t, source, shopConfig(1))
	t.Logf("startup (compile and load one VM): %s", time.Since(start))
	for i := range warm {
		page := pages[i%len(pages)]
		html, err := p.Render(context.Background(), page.input)
		if err != nil {
			t.Fatal(err)
		}
		page.check(t, html)
	}
	latencies := make([]float64, timed)
	for i := range latencies {
		page := pages[i%len(pages)]
		begin := time.Now()
		html, err := p.Render(context.Background(), page.input)
		took := time.Since(begin)
		latencies[i] = float64(took) / 1e6
		if err != nil {
			t.Fatal(err)
		}
		page.check(t, html)
		if took > slowestWarmRender {
			t.Errorf("warm render of %s took %s, more than %s", page.input.URL, took, slowestWarmRender)
		}
	}
	slices.Sort(latencies)
	at := func(q float64) float64 { return latencies[int(math.Ceil(q*float64(len(latencies))))-1] }
	t.Logf("warm render over %d requests: P50 %.3f ms, P95 %.3f ms, P99 %.3f ms", len(latencies), at(.50), at(.95), at(.99))
}

// newPool returns a pool of the bundle source, closed when the test ends.
func newPool(t testing.TB, source string, cfg config.SSR) *ssr.Pool {
	t.Helper()
	p, err := ssr.NewPool(source, nil, cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := p.Close(); err != nil {
			t.Error(err)
		}
	})
	return p
}

func shopConfig(vms int) config.SSR {
	return config.SSR{VMs: vms, VMMemory: 128 << 20, VMStackDepth: config.Default().SSR.VMStackDepth, Timeout: 10 * time.Second, QueueLimit: 32, WaitTimeout: time.Second}
}

// withCustomer returns the order detail input with the customer renamed. The data of each route
// is a JSON text of its own in the input, so the name is replaced as that text spells it inside a
// JSON string. Callers check that the new name reaches the HTML.
func withCustomer(detail ssr.Input, name string) ssr.Input {
	inString := func(s string) string {
		quoted, _ := json.Marshal(s)
		return string(quoted[1 : len(quoted)-1])
	}
	texts := make([]string, len(detail.Texts))
	for i, text := range detail.Texts {
		texts[i] = strings.ReplaceAll(text, inString("林夏"), inString(name))
	}
	detail.Texts = texts
	return detail
}

// One VM renders the same route with a new customer each time, the steps a Pool takes for each
// render. The current name must appear and the previous one must be gone. A mark the test leaves on
// the VM before the first render must still be there after each one, so every render ran in it.
func TestShopReuseKeepsRequestsApart(t *testing.T) {
	source, pages := shopBundle(t)
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
	if _, err := vm.Eval("globalThis.reuseMark = 'the VM of this test'", slowjs.EvalGlobal); err != nil {
		t.Fatal(err)
	}
	previous := ""
	for i := range 12 {
		name := fmt.Sprintf("customer-%03d-only", i)
		in := withCustomer(pages[2].input, name)
		html, err := ssr.RenderVM(vm, renderInput(t, in), in.Texts, cfg.Timeout)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(html, name) {
			t.Fatalf("request %d: missing current customer", i)
		}
		if previous != "" && strings.Contains(html, previous) {
			t.Fatalf("request %d: previous customer leaked into HTML", i)
		}
		if mark, err := vm.Eval("globalThis.reuseMark", slowjs.EvalGlobal); err != nil || mark != "the VM of this test" {
			t.Fatalf("request %d: the mark is %v, %v", i, mark, err)
		}
		previous = name
	}
}

// units is the length of s in UTF-16 code units, which is what the bundle compares with the length
// from which a text becomes a slot.
func units(s string) int {
	n := 0
	for _, r := range s {
		n++
		if r > 0xFFFF {
			n++
		}
	}
	return n
}

// dataLiterals returns the strings that a page passes to the deserializer of the tifa.data adapter,
// read as JavaScript reads them, which is what the browser parses the data of the router from.
func dataLiterals(t *testing.T, vm *slowjs.VM, html string) []string {
	t.Helper()
	var out []string
	for _, m := range regexp.MustCompile(`\$_TSR\.t\.get\("tifa\.data"\)\("((?:[^"\\]|\\.)*)"\)`).FindAllStringSubmatch(html, -1) {
		v, err := vm.Eval(`"`+m[1]+`"`, slowjs.EvalGlobal)
		if err != nil {
			t.Fatalf("read %.60s: %v", m[1], err)
		}
		out = append(out, v.(string))
	}
	return out
}

// A text goes into the page as a slot from 4,096 code units. At 4,095, 4,096 and 4,097, for text in
// ASCII, in Chinese and with a character above U+FFFF (written as the escapes of its surrogate pair,
// as the server writes it), the pool's page is the page the VM writes by itself, and the strings it
// passes to the adapter are the texts, which is the data the browser reads.
func TestShopSlotsAtTheLengthWhereTheyStart(t *testing.T) {
	source, pages := shopBundle(t)
	cfg := shopConfig(1)
	pool := newPool(t, source, cfg)
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
	reader, err := slowjs.NewVM()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	detail := pages[2].input
	target := -1
	for i, text := range detail.Texts {
		if strings.Contains(text, "林夏") {
			if target >= 0 {
				t.Fatal("two texts of the order page hold the customer")
			}
			target = i
		}
	}
	if target < 0 {
		t.Fatal("no text holds the customer")
	}
	inString := func(s string) string {
		quoted, _ := json.Marshal(s)
		return string(quoted[1 : len(quoted)-1])
	}
	for _, kind := range []struct {
		name string
		unit string // what the name grows by
		size int    // its code units in the text, after the escape of the characters above U+FFFF
	}{{"ascii", "x", 1}, {"chinese", "林", 1}, {"astral", "😀", 12}} {
		for _, want := range []int{4095, 4096, 4097} {
			t.Run(fmt.Sprintf("%s %d", kind.name, want), func(t *testing.T) {
				in := detail
				in.Texts = slices.Clone(detail.Texts)
				base := units(in.Texts[target])
				if base >= want {
					t.Fatalf("the order is already %d code units", base)
				}
				k := (want - base) / kind.size
				pad := strings.Repeat(kind.unit, k) + strings.Repeat("x", want-base-k*kind.size)
				in.Texts[target] = string(ssr.EscapeAboveFFFF([]byte(strings.Replace(in.Texts[target], inString("林夏"), inString("林夏"+pad), 1))))
				if got := units(in.Texts[target]); got != want {
					t.Fatalf("the text has %d code units, want %d", got, want)
				}
				got, err := pool.Render(context.Background(), in)
				if err != nil {
					t.Fatal(err)
				}
				input, err := json.Marshal(in)
				if err != nil {
					t.Fatal(err)
				}
				plain, err := ssr.RenderVM(vm, input, in.Texts, cfg.Timeout)
				if err != nil {
					t.Fatal(err)
				}
				if slotLeft.MatchString(got) || sameButTheTime(got) != sameButTheTime(plain) {
					t.Errorf("the page differs from the page the VM writes by itself (%d and %d bytes)", len(got), len(plain))
				}
				// The VM writes a slot for each object or array of 4,096 code units or more, no other.
				slotted := in
				slotted.Nonce = ssr.NewNonce()
				input, err = json.Marshal(slotted)
				if err != nil {
					t.Fatal(err)
				}
				raw, err := ssr.RenderVM(vm, input, in.Texts, cfg.Timeout)
				if err != nil {
					t.Fatal(err)
				}
				var objects []string
				expected := 0
				for i, text := range in.Texts {
					if text != "" && (text[0] == '{' || text[0] == '[') {
						objects = append(objects, text)
						// The text of the order is the one that grows, so it is a slot from 4,096 code units.
						if units(text) >= 4096 {
							expected++
						} else if i == target && want >= 4096 {
							t.Fatalf("the text of the order is %d code units", units(text))
						}
					}
				}
				if n := strings.Count(raw, "~"+slotted.Nonce); n != expected {
					t.Errorf("the VM wrote %d slots, want %d", n, expected)
				}
				if (units(in.Texts[target]) >= 4096) != (want >= 4096) {
					t.Fatalf("the text of the order is %d code units", units(in.Texts[target]))
				}
				// What the browser reads is the same with the slot filled and with the VM's own text.
				literals := dataLiterals(t, reader, got)
				slices.Sort(literals)
				slices.Sort(objects)
				if !slices.Equal(literals, objects) {
					t.Errorf("the page passes %d strings to the adapter, which differ from the %d texts of the data", len(literals), len(objects))
				}
				if plainLiterals := dataLiterals(t, reader, plain); len(plainLiterals) != len(literals) {
					t.Errorf("the VM's own page passes %d strings", len(plainLiterals))
				}
			})
		}
	}
}
