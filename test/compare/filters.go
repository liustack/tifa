package compare

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"path/filepath"
	"strings"
)

// listURL is the URL of the order list of a project with a query string, written as a browser
// writes it: the pairs of kv are keys and values, each escaped.
func listURL(project string, kv ...string) string {
	q := url.Values{}
	for i := 0; i < len(kv); i += 2 {
		q.Add(kv[i], kv[i+1])
	}
	if len(q) == 0 {
		return "/projects/" + project
	}
	return "/projects/" + project + "?" + q.Encode()
}

// FilterPages are order lists with the filters and the page in the URL: the pages the four servers
// must show the same, and the data the port must compute as the Go code does. Rows is how many orders
// the page shows, or -1 when the test reads it from the data of the shop.
var FilterPages = []struct {
	Page
	Rows int
}{
	{Page{"list studio shipped", listURL("studio", "status", "shipped"), chain(listRoute)}, 25},
	{Page{"list studio shipped page 2", listURL("studio", "status", "shipped", "page", "2"), chain(listRoute)}, 15},
	{Page{"list studio pending", listURL("studio", "status", "pending"), chain(listRoute)}, 2},
	{Page{"list studio page 2", listURL("studio", "page", "2"), chain(listRoute)}, 17},
	{Page{"list studio past the end", listURL("studio", "page", "9"), chain(listRoute)}, 17},
	{Page{"list studio order number", listURL("studio", "q", "sh-1005"), chain(listRoute)}, 1},
	{Page{"list studio customer", listURL("studio", "q", "周宁"), chain(listRoute)}, -1},
	{Page{"list studio carrier period amount", listURL("studio", "carrier", "SF Express", "period", "30", "amount", "mid"), chain(listRoute)}, -1},
	{Page{"list studio amount high shipped", listURL("studio", "status", "shipped", "amount", "high"), chain(listRoute)}, -1},
	{Page{"list studio nothing", listURL("studio", "q", "nobody by this name"), chain(listRoute)}, 0},
	{Page{"list studio unknown values", listURL("studio", "status", "lost", "period", "365", "amount", "huge", "page", "two"), chain(listRoute)}, 25},
	{Page{"list studio by amount", listURL("studio", "sort", "totalCents", "dir", "desc"), chain(listRoute)}, 25},
	{Page{"list studio by amount page 2", listURL("studio", "sort", "totalCents", "dir", "desc", "page", "2"), chain(listRoute)}, 17},
	{Page{"list studio smallest first", listURL("studio", "sort", "totalCents", "dir", "asc"), chain(listRoute)}, 25},
	{Page{"list studio oldest first", listURL("studio", "sort", "placedAt", "dir", "asc"), chain(listRoute)}, 25},
	{Page{"list studio shipped by amount", listURL("studio", "status", "shipped", "amount", "mid", "sort", "totalCents", "dir", "asc"), chain(listRoute)}, -1},
	{Page{"list studio unknown sort", listURL("studio", "sort", "customer", "dir", "sideways"), chain(listRoute)}, 25},
	{Page{"list studio search with next line", listURL("studio", "q", "\u0085sh-1005"), chain(listRoute)}, 0},
	{Page{"list studio search with byte order mark", listURL("studio", "q", "\ufeffsh-1005"), chain(listRoute)}, 0},
	{Page{"list studio search with spaces", listURL("studio", "q", " \t sh-1005 "), chain(listRoute)}, 1},
	{Page{"list weekend pending", listURL("weekend", "status", "pending"), chain(listRoute)}, 1},
	{Page{"list weekend search", listURL("weekend", "q", "wk-02"), chain(listRoute)}, 2},
}

// FilterRoute is the route ID of the order list, whose data the filters change.
const FilterRoute = listRoute

// GoFilterData starts bin/shop and asks it, as the browser router does, for the data of the Layouts
// and the page of each of FilterPages.
func (e Env) GoFilterData(ctx context.Context) (PageData, error) {
	buildID, err := e.ShopBuildID()
	if err != nil {
		return nil, err
	}
	shop, err := e.Targets()[0].Start(ctx, "")
	if err != nil {
		return nil, err
	}
	defer shop.Stop()
	out := PageData{}
	for _, f := range FilterPages {
		u := shop.Base + f.URL + "&_tifa=" + buildID + "." + strings.Repeat("1", strings.Count(f.Routes, ",")+1)
		if !strings.Contains(f.URL, "?") {
			u = strings.Replace(u, "&_tifa=", "?_tifa=", 1)
		}
		req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
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
			return nil, fmt.Errorf("data of %s: %d %v %s", f.URL, res.StatusCode, err, body)
		}
		var answer struct {
			Routes map[string]struct {
				Status int `json:"status"`
				Data   any `json:"data"`
			} `json:"routes"`
		}
		if err := json.Unmarshal(body, &answer); err != nil {
			return nil, fmt.Errorf("data of %s: %w", f.URL, err)
		}
		out[f.URL] = map[string]any{}
		for id, r := range answer.Routes {
			if r.Status != 200 {
				return nil, fmt.Errorf("data of %s: route %s answered %d", f.URL, id, r.Status)
			}
			out[f.URL][id] = r.Data
		}
	}
	return out, nil
}

// NodeFilterData runs the ported domain code with Node and returns the data it computes for each of
// FilterPages.
func (e Env) NodeFilterData(ctx context.Context) (PageData, error) {
	urls := make([]string, len(FilterPages))
	for i, f := range FilterPages {
		urls[i] = f.URL
	}
	arg, err := json.Marshal(urls)
	if err != nil {
		return nil, err
	}
	out, err := exec.CommandContext(ctx, "node", filepath.Join(e.Root, "test", "compare", "domain", "dumpfilters.ts"), string(arg)).Output()
	if err != nil {
		return nil, fmt.Errorf("node domain/dumpfilters.ts: %w", err)
	}
	var data PageData
	if err := json.Unmarshal(out, &data); err != nil {
		return nil, err
	}
	return data, nil
}
