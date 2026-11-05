package compare

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"runtime"
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
