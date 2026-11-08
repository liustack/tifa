package compare

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"fmt"
	"io"
	"math"
	"net/http"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// PageData is the data of each of every page of CheckedPages by URL and then by route ID: what the Go Layout
// and Load functions return.
type PageData map[string]map[string]any

// DomainWrite is a write that the domain check makes to the shop and to the port before it compares
// their data: Kind is one of the kinds of write (WriteCreate has no Order, and the Order of
// WriteProduct is the slug of the product), Body its JSON.
type DomainWrite struct {
	Kind    string `json:"kind"`
	Project string `json:"project"`
	Order   string `json:"order,omitempty"`
	Body    any    `json:"body"`
}

// RecipientWrites are the writes that change the recipient of orders, for the check.
func RecipientWrites(list []Recipient) []DomainWrite {
	out := make([]DomainWrite, len(list))
	for i, w := range list {
		out[i] = DomainWrite{Kind: WriteRecipient, Project: w.Project, Order: w.Order, Body: w}
	}
	return out
}

// createdPages are the order pages of the orders that the creates among writes place: the shop numbers
// them after the highest number of the project, one after the other.
func createdPages(writes []DomainWrite) []Page {
	var out []Page
	last := map[string]int{"studio": OrderIDs["studio"][1], "weekend": OrderIDs["weekend"][1]}
	for _, w := range writes {
		if w.Kind != WriteCreate {
			continue
		}
		last[w.Project]++
		id := fmt.Sprintf("%s%04d", orderPrefix[w.Project], last[w.Project])
		out = append(out, Page{"order " + id, "/projects/" + w.Project + "/orders/" + id, chain(orderRoute)})
	}
	return out
}

// shopRequest is the request that makes a write to the shop itself.
func (w DomainWrite) shopRequest() Step {
	order := "/projects/" + w.Project + "/orders/" + w.Order
	switch w.Kind {
	case WriteCreate:
		return Step{Method: http.MethodPost, Path: "/projects/" + w.Project + "/orders", Body: mustJSON(w.Body)}
	case WriteShip:
		return Step{Method: http.MethodPost, Path: order, Body: mustJSON(w.Body)}
	case WriteNote:
		return Step{Method: http.MethodPut, Path: order, Body: mustJSON(w.Body)}
	case WriteProduct:
		return Step{Method: http.MethodPatch, Path: "/projects/" + w.Project + "/products/" + w.Order, Body: mustJSON(w.Body)}
	}
	return Step{Method: http.MethodPatch, Path: order, Body: mustJSON(w.Body)}
}

// GoData starts bin/shop and asks it, as the browser router does, for the data of the Layouts and
// the page of each of every page of CheckedPages.
func (e Env) GoData(ctx context.Context, writes ...DomainWrite) (PageData, error) {
	buildID, err := e.ShopBuildID()
	if err != nil {
		return nil, err
	}
	shop, err := e.Targets()[0].Start(ctx, "")
	if err != nil {
		return nil, err
	}
	defer shop.Stop()
	for _, w := range writes {
		if err := Send(ctx, shop.Base, w.shopRequest()); err != nil {
			return nil, err
		}
	}

	out := PageData{}
	for _, page := range append(CheckedPages(), createdPages(writes)...) {
		// Every Load of the chain, which Routes lists: a digit 1 for each.
		url := shop.Base + page.URL + "?_tifa=" + buildID + "." + strings.Repeat("1", strings.Count(page.Routes, ",")+1)
		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return nil, err
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		body, err := io.ReadAll(res.Body)
		res.Body.Close()
		if err != nil || res.StatusCode != 200 {
			return nil, fmt.Errorf("data of %s: %d %v %s", page.URL, res.StatusCode, err, body)
		}
		var answer struct {
			Routes map[string]struct {
				Status int `json:"status"`
				Data   any `json:"data"`
			} `json:"routes"`
		}
		if err := json.Unmarshal(body, &answer); err != nil {
			return nil, fmt.Errorf("data of %s: %w", page.URL, err)
		}
		out[page.URL] = map[string]any{}
		for id, r := range answer.Routes {
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

// NodeData runs the ported domain code with Node and returns the data it computes for every page of CheckedPages.
func (e Env) NodeData(ctx context.Context, writes ...DomainWrite) (PageData, error) {
	args := []string{filepath.Join(e.Root, "test", "compare", "domain", "dump.ts")}
	if len(writes) > 0 {
		data, err := json.Marshal(writes)
		if err != nil {
			return nil, err
		}
		args = append(args, string(data))
	}
	cmd := exec.CommandContext(ctx, "node", args...)
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

// mustJSON encodes with the keys of a map in order, so that the same write is the same bytes every time.
func mustJSON(v any) []byte {
	data, err := json.Marshal(v, json.Deterministic(true))
	if err != nil {
		panic(err)
	}
	return data
}

// WithoutInstants drops every instant from page data: the keys "at" and those that end in "At",
// and rounds the average dispatch hours of a carrier to 2 decimals, since an order that is shipped
// some milliseconds after it was placed adds a little to it. Writes that place or ship an order use
// the clock of the process that makes them, and the shop and the port run at different moments.
func WithoutInstants(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			if k == "at" || strings.HasSuffix(k, "At") {
				delete(x, k)
				continue
			}
			if f, ok := e.(float64); ok && k == "avgDispatchHours" {
				x[k] = math.Round(f*100) / 100
				continue
			}
			x[k] = WithoutInstants(e)
		}
	case []any:
		for i := range x {
			x[i] = WithoutInstants(x[i])
		}
	}
	return v
}
