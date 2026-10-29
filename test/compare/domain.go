package compare

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// PageData is the data of each of the six pages by URL and then by route ID: what the Go Layout
// and Load functions return.
type PageData map[string]map[string]any

// GoData starts bin/shop and asks it, as the browser router does, for the data of the Layouts and
// the page of each of the six pages.
func (e Env) GoData(ctx context.Context) (PageData, error) {
	var built struct{ BuildID string }
	data, err := os.ReadFile(filepath.Join(e.Shop, ".tifa", "dist", "build.json"))
	if err != nil {
		return nil, fmt.Errorf("build the shop first: %w", err)
	}
	if err := json.Unmarshal(data, &built); err != nil {
		return nil, err
	}
	shop, err := e.Targets()[0].Start(ctx, "")
	if err != nil {
		return nil, err
	}
	defer shop.Stop()

	out := PageData{}
	for _, page := range Pages {
		req, err := http.NewRequestWithContext(ctx, "GET", shop.Base+page.URL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("X-Tifa-Route", page.Routes)
		req.Header.Set("X-Tifa-Build", built.BuildID)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		body, err := io.ReadAll(res.Body)
		res.Body.Close()
		if err != nil || res.StatusCode != 200 {
			return nil, fmt.Errorf("data of %s: %d %v %s", page.URL, res.StatusCode, err, body)
		}
		var routes map[string]struct {
			Status int
			Data   any
		}
		if err := json.Unmarshal(body, &routes); err != nil {
			return nil, fmt.Errorf("data of %s: %w", page.URL, err)
		}
		out[page.URL] = map[string]any{}
		for id, r := range routes {
			if r.Status != 200 {
				return nil, fmt.Errorf("data of %s: route %s answered %d", page.URL, id, r.Status)
			}
			out[page.URL][id] = r.Data
		}
	}
	return out, nil
}

var nodeVersion = regexp.MustCompile(`^v(\d+)\.(\d+)`)

// NodeRunsTypeScript says why `node file.ts` does not work here, or "" when it does: Node 22.18
// and later remove the types of a file as they run it.
func NodeRunsTypeScript() string {
	out, err := exec.Command("node", "--version").Output()
	if err != nil {
		return "node is not installed"
	}
	m := nodeVersion.FindStringSubmatch(strings.TrimSpace(string(out)))
	if m == nil {
		return "cannot read the version of node: " + string(out)
	}
	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	if major < 22 || major == 22 && minor < 18 {
		return fmt.Sprintf("node %s.%s does not run TypeScript, which Node 22.18 and later do", m[1], m[2])
	}
	return ""
}

// NodeData runs the ported domain code with Node and returns the data it computes for the six pages.
func (e Env) NodeData(ctx context.Context) (PageData, error) {
	cmd := exec.CommandContext(ctx, "node", filepath.Join(e.Root, "test", "compare", "domain", "dump.ts"))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("node domain/dump.ts: %w\n%s", err, stderr.String())
	}
	var data PageData
	if err := json.Unmarshal(out, &data); err != nil {
		return nil, err
	}
	return data, nil
}

// DiffData lists where two data sets differ, as paths such as
// /projects/studio/app/layout/projects/0/pending, at most max of them. Go's JSON and Node's are
// compared as values, so the order of the fields does not matter.
func DiffData(want, got any, max int) []string {
	var out []string
	var walk func(path string, a, b any)
	walk = func(path string, a, b any) {
		if len(out) >= max {
			return
		}
		switch x := a.(type) {
		case map[string]any:
			y, ok := b.(map[string]any)
			if !ok {
				out = append(out, fmt.Sprintf("%s: Go has an object, Node %s", path, show(b)))
				return
			}
			keys := map[string]bool{}
			for k := range x {
				keys[k] = true
			}
			for k := range y {
				keys[k] = true
			}
			var sorted []string
			for k := range keys {
				sorted = append(sorted, k)
			}
			sort.Strings(sorted)
			for _, k := range sorted {
				av, aok := x[k]
				bv, bok := y[k]
				switch {
				case !bok:
					out = append(out, fmt.Sprintf("%s/%s: Go has %s, Node has no such field", path, strings.TrimPrefix(k, "/"), show(av)))
				case !aok:
					out = append(out, fmt.Sprintf("%s/%s: Go has no such field, Node has %s", path, strings.TrimPrefix(k, "/"), show(bv)))
				default:
					walk(path+"/"+strings.TrimPrefix(k, "/"), av, bv)
				}
			}
		case []any:
			y, ok := b.([]any)
			if !ok {
				out = append(out, fmt.Sprintf("%s: Go has a list of %d, Node %s", path, len(x), show(b)))
				return
			}
			if len(x) != len(y) {
				out = append(out, fmt.Sprintf("%s: Go has %d items, Node %d", path, len(x), len(y)))
				return
			}
			for i := range x {
				walk(path+"/"+strconv.Itoa(i), x[i], y[i])
			}
		default:
			if !reflect.DeepEqual(a, b) {
				out = append(out, fmt.Sprintf("%s: Go has %s, Node %s", path, show(a), show(b)))
			}
		}
	}
	walk("", want, got)
	return out
}

func show(v any) string {
	data, _ := json.Marshal(v)
	if len(data) > 80 {
		return string(data[:80]) + "..."
	}
	return string(data)
}
