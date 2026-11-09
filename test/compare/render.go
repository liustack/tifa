package compare

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
)

// RenderResult is the render of one page in a warm VM of Tifa, without HTTP, in milliseconds.
type RenderResult struct {
	Page string  `json:"page"`
	P50  float64 `json:"p50Ms"`
	P99  float64 `json:"p99Ms"`
}

// pin puts taskset in front of a command when cpus are given.
func pin(cpus string, name string, args ...string) (string, []string) {
	if cpus == "" || runtime.GOOS != "linux" {
		return name, args
	}
	return "taskset", append([]string{"-c", cpus, name}, args...)
}

// renderBench runs BenchmarkShop of test/ssr, which renders the six pages in a warm VM with their
// data in hand, and returns the P50 and P99 of a render of each. It is the same engine, bundle and
// data as the Tifa program uses, without the request around it. It is a measurement of its own: it
// warms its VM for its own purpose and has nothing to do with the cold runs over HTTP.
func (e Env) renderBench(ctx context.Context, serverCPUs string) ([]RenderResult, error) {
	name, args := pin(serverCPUs, "go", "test", "-run", "^$", "-bench", "Shop/^render", "-benchtime", "2000x", "-count", "1", "./test/ssr")
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = e.Root
	cmd.Env = append(os.Environ(), "TIFA_SSR_SHOP=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("render benchmark: %w\n%s", err, out)
	}
	return parseRender(string(out))
}

// parseRender reads the lines of BenchmarkShop/render<url> with p50-ms and p99-ms.
func parseRender(out string) ([]RenderResult, error) {
	var results []RenderResult
	suffix := regexp.MustCompile(`^-\d+$`)
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 4 || !strings.HasPrefix(f[0], "BenchmarkShop/render") {
			continue
		}
		var p50, p99 float64
		for i := 2; i+1 < len(f); i += 2 {
			switch f[i+1] {
			case "p50-ms":
				p50, _ = strconv.ParseFloat(f[i], 64)
			case "p99-ms":
				p99, _ = strconv.ParseFloat(f[i], 64)
			}
		}
		for _, p := range Pages {
			want := "BenchmarkShop/render" + strings.ReplaceAll(p.URL, "/", "-")
			if f[0] == want || strings.HasPrefix(f[0], want) && suffix.MatchString(f[0][len(want):]) {
				results = append(results, RenderResult{Page: p.Name, P50: p50, P99: p99})
			}
		}
	}
	if len(results) != len(Pages) {
		return nil, fmt.Errorf("render benchmark printed %d of the %d pages:\n%s", len(results), len(Pages), out)
	}
	return results, nil
}

// The values of X-Tifa-Render that the comparison reads: a render in a VM, an answer that waited for
// a render of the same page in flight, an answer with no render (a redirect, a write), and a hit of the
// render memo, the one that skips the render. Every check and table reads the hit through RenderHit.
const (
	RenderHeader = "X-Tifa-Render"
	RenderVM     = "vm"
	RenderShared = "shared"
	RenderNone   = "none"
	RenderHit    = "memo"
)

// Why Tifa rendered a page in a VM, as far as the client can tell from the URL, the cookies it sent
// and X-Tifa-Render. The client does not see the data of the Loads, so RenderAgain holds a page whose
// data changed since (a write, or data that changes by itself, such as the dates of a day) and one
// the memo dropped when it was full alike.
const (
	RenderFirst       = "first"        // the first request of its URL that Tifa answered in the round
	RenderAgain       = "again"        // a URL Tifa had answered before in the round
	RenderPrivate     = "private"      // a private page, the visit without a cart cookie
	RenderPrivateCart = "private cart" // a private page, the visit with a cart cookie
)

// StoreCartCookie is the cookie of the shop that holds the ID of the visitor's cart (cart.CookieName
// in examples/shop).
const StoreCartCookie = "store_cart"

// privateKind says whether a kind of page is private: its data is the visitor's own. In the shop the
// cart and the checkout are the pages whose Loads read cookies (the cart and the products looked at).
func privateKind(kind string) bool { return kind == StoreKindCart || kind == StoreKindCheckout }

// renderWhy says why Tifa rendered a page of a kind in a VM: a private page by its cart cookie, any
// other by whether Tifa had answered its URL before in the round.
func renderWhy(kind string, seen, cart bool) string {
	switch {
	case privateKind(kind) && cart:
		return RenderPrivateCart
	case privateKind(kind):
		return RenderPrivate
	case seen:
		return RenderAgain
	}
	return RenderFirst
}

// hasCookie says whether a jar sends a cookie of that name to a URL.
func hasCookie(jar http.CookieJar, u *url.URL, name string) bool {
	return slices.ContainsFunc(jar.Cookies(u), func(c *http.Cookie) bool { return c.Name == name })
}
