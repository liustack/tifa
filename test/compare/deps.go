package compare

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"maps"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// What the data of a shared page of the storefront holds, so that the version of a page can follow the
// writes. A page of Tifa is rendered again when the data its Loads give has changed, and the data of a
// storefront page changes with the price and the stock of the products it shows (a checkout takes
// pieces from the stock, and staff change a price or a stock). store_deps.json says, for each URL a
// visit can land on and each URL of a crawler, which products it shows and which of their fields. The
// parts of a page whose products a price or a stock decides (decidedParts) are left out of it: the model
// of the versions works those out as the writes come (versionModel in versions.go). Make
// it again with `compare deps` after the pages, the catalogue or the URLs change. It carries a hash of
// everything it was made from, and a file that does not match what is checked out is refused
// (CheckStoreDeps), so it cannot go out of date silently.

//go:embed store_deps.json
var storeDepsJSON []byte

// allProducts stands in a dependency for every product of the catalogue: "*:p" is all the prices, and
// "*:f" whether each product is in stock.
const allProducts = "*"

// A dependency is written "slug" for the price and the stock of a product, "slug:p" for its price alone
// and "slug:s" for its stock alone. "f" is whether a product is in stock at all, which only "*:f" names.
const (
	depPrice = "p"
	depStock = "s"
	depFlag  = "f"
)

// StoreDeps is the content of store_deps.json.
type StoreDeps struct {
	// Sources is a hash of the files of the shop the data of the pages is made from, and URLs of the URLs
	// the file has an entry for.
	Sources string `json:"sources"`
	URLs    string `json:"urls"`
	// Query is, for each route of the pages used here, the names of the query parameters its Loads and
	// those of its Layouts read, as the build of the shop has them. A render sees these and no other.
	Query map[string][]string `json:"query"`
	// Common are the dependencies every page has (the products the Layout of the storefront shows), and
	// Pages the others, by the URL the render sees (RenderURL).
	Common []string            `json:"common"`
	Pages  map[string][]string `json:"pages"`
	// Lists are, for each product list whose products a price or a stock decides (it sorts by price, has a
	// price range or has only what is in stock), the products that pass its other filters, in its order
	// when it does not sort by price and else in the order of the catalogue. The key is the render URL of
	// the list without the range, the stock, the sort by price and the page (listBase).
	Lists map[string][]string `json:"lists"`
}

// The routes of the pages, as tifa names them.
const (
	adminList  = "app/projects/project+/route"
	adminOrder = "app/projects/project+/orders/order+/route"
	adminStats = "app/projects/project+/stats/route"
)

// pageRoutes are the routes a URL of a visit can be a page of, by the kind of the page.
var pageRoutes = map[string]string{
	StoreKindHome: storeHome, StoreKindList: storeList, StoreKindProduct: storeProduct,
	StoreKindJournal: storeJournal, StoreKindPost: storePost, StoreKindCart: storeCart, StoreKindCheckout: storeCheckout,
}

// routeOf is the route a page URL is served by.
func routeOf(page string) string {
	if _, kind := StorePageKind(page); kind != "" {
		return pageRoutes[kind]
	}
	switch kind, _, _ := PageKind(stripQuery(page)); kind {
	case "list":
		return adminList
	case "order":
		return adminOrder
	case "stats":
		return adminStats
	}
	panic("no route for the page " + page)
}

func stripQuery(u string) string {
	path, _, _ := strings.Cut(u, "?")
	return path
}

// usedRoutes are the routes the file keeps the query of.
func usedRoutes() []string {
	out := []string{adminList, adminOrder, adminStats}
	for _, r := range pageRoutes {
		out = append(out, r)
	}
	slices.Sort(out)
	return out
}

// RenderURL is the URL a server render of Tifa gets for a page URL: the path and the query parameters
// the Loads of the page read, by name, with the values of a name in the order of the request, and the
// others (a click id, an email parameter, ?x=random) left out. It is renderURL of server/page.go.
func (d *StoreDeps) RenderURL(page string) string {
	return renderedURL(page, d.Query[routeOf(page)])
}

// renderedURL is renderURL of server/page.go for the URL page, and keep the names of the parameters
// that are read, sorted. The values are spelled as url.QueryEscape writes them, and what
// url.ParseQuery drops is left out.
func renderedURL(page string, keep []string) string {
	path, query, _ := strings.Cut(page, "?")
	if len(keep) == 0 || query == "" {
		return path
	}
	values := map[string][]string{}
	for part := range strings.SplitSeq(query, "&") {
		if part == "" {
			continue
		}
		rawName, rawValue, _ := strings.Cut(part, "=")
		name, err1 := url.QueryUnescape(rawName)
		value, err2 := url.QueryUnescape(rawValue)
		if err1 != nil || err2 != nil || strings.Contains(part, ";") {
			continue
		}
		if _, ok := slices.BinarySearch(keep, name); ok {
			values[name] = append(values[name], value)
		}
	}
	var b strings.Builder
	for _, k := range keep {
		for _, v := range values[k] {
			if b.Len() == 0 {
				b.WriteByte('?')
			} else {
				b.WriteByte('&')
			}
			b.WriteString(url.QueryEscape(k) + "=" + url.QueryEscape(v))
		}
	}
	return path + b.String()
}

// routeQueries reads from the generated program of the shop (.tifa/tifaapp/app.go) the names of the
// query parameters each route reads: its own, and those of the Layouts above it. They are sorted.
func routeQueries(src []byte) (map[string][]string, error) {
	f, err := parser.ParseFile(token.NewFileSet(), "app.go", src, 0)
	if err != nil {
		return nil, err
	}
	type entry struct {
		query, layouts []string
	}
	layouts, routes := map[string]entry{}, map[string]entry{}
	strs := func(e ast.Expr) []string {
		lit, ok := e.(*ast.CompositeLit)
		if !ok {
			return nil
		}
		var out []string
		for _, x := range lit.Elts {
			if b, ok := x.(*ast.BasicLit); ok && b.Kind == token.STRING {
				s, _ := strconv.Unquote(b.Value)
				out = append(out, s)
			}
		}
		return out
	}
	ast.Inspect(f, func(n ast.Node) bool {
		kv, ok := n.(*ast.KeyValueExpr)
		if !ok {
			return true
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok || (key.Name != "Layouts" && key.Name != "Routes") {
			return true
		}
		list, ok := kv.Value.(*ast.CompositeLit)
		if !ok {
			return true
		}
		for _, el := range list.Elts {
			item, ok := el.(*ast.CompositeLit)
			if !ok {
				continue
			}
			var id string
			var e entry
			for _, x := range item.Elts {
				field, ok := x.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				name, _ := field.Key.(*ast.Ident)
				if name == nil {
					continue
				}
				switch name.Name {
				case "ID":
					if b, ok := field.Value.(*ast.BasicLit); ok {
						id, _ = strconv.Unquote(b.Value)
					}
				case "Query":
					e.query = strs(field.Value)
				case "Layouts":
					e.layouts = strs(field.Value)
				}
			}
			if key.Name == "Layouts" {
				layouts[id] = e
			} else {
				routes[id] = e
			}
		}
		return false
	})
	if len(routes) == 0 {
		return nil, fmt.Errorf("no route in the generated program")
	}
	out := map[string][]string{}
	for id, r := range routes {
		set := map[string]bool{}
		for _, l := range r.layouts {
			for _, q := range layouts[l].query {
				set[q] = true
			}
		}
		for _, q := range r.query {
			set[q] = true
		}
		names := make([]string, 0, len(set))
		for q := range set {
			names = append(names, q)
		}
		sort.Strings(names)
		out[id] = names
	}
	return out, nil
}

// depsURLs are the URLs the file has an entry for: every page of the storefront a visit can land on and
// every URL of a crawler (U). The cart and the checkout are private and have none.
func depsURLs() []string {
	set := map[string]bool{}
	public, _ := CrawlURLs()
	for _, u := range slices.Concat(StoreVisitPages(), public) {
		if _, kind := StorePageKind(u); kind != StoreKindCart && kind != StoreKindCheckout {
			set[u] = true
		}
	}
	out := make([]string, 0, len(set))
	for u := range set {
		out = append(out, u)
	}
	slices.Sort(out)
	return out
}

// hashOf is a short hash of the lines.
func hashOf(lines []string) string {
	sum := sha256.New()
	for _, l := range lines {
		sum.Write([]byte(l))
		sum.Write([]byte{0})
	}
	return hex.EncodeToString(sum.Sum(nil))[:16]
}

// depsSources are the files of the shop the data of the pages is made from: the Go, TypeScript and JSON
// files of its app, and its own Go files. The photos are not in it.
func depsSources(root string) ([]string, error) {
	var out []string
	shop := filepath.Join(root, "examples", "shop")
	err := filepath.WalkDir(filepath.Join(shop, "app"), func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			switch filepath.Ext(path) {
			case ".go", ".ts", ".tsx", ".json":
				out = append(out, path)
			}
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	top, err := filepath.Glob(filepath.Join(shop, "*.go"))
	if err != nil {
		return nil, err
	}
	out = append(out, top...)
	out = append(out, filepath.Join(shop, "tifa.yaml"))
	slices.Sort(out)
	return out, nil
}

// depsSourcesHash is a hash of the names and the contents of depsSources.
func depsSourcesHash(root string) (string, error) {
	files, err := depsSources(root)
	if err != nil {
		return "", err
	}
	sum := sha256.New()
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return "", err
		}
		rel, _ := filepath.Rel(root, f)
		fmt.Fprintf(sum, "%s\x00%d\x00", filepath.ToSlash(rel), len(data))
		sum.Write(data)
	}
	return hex.EncodeToString(sum.Sum(nil))[:16], nil
}

// CheckStoreDeps loads store_deps.json and refuses it when it is not what the checkout at root makes:
// another shop source, another set of URLs, or a URL it has no entry for. Make it again with
// `compare deps`.
func CheckStoreDeps(root string) (*StoreDeps, error) {
	d, err := parseStoreDeps(storeDepsJSON)
	if err != nil {
		return nil, err
	}
	sources, err := depsSourcesHash(root)
	if err != nil {
		return nil, err
	}
	const again = "make it again with `compare deps`"

	if d.Sources != sources {
		return nil, fmt.Errorf("store_deps.json was made from other sources of the shop (%s, now %s): %s", d.Sources, sources, again)
	}
	urls := depsURLs()
	if d.URLs != hashOf(urls) {
		return nil, fmt.Errorf("store_deps.json was made for another set of URLs: %s", again)
	}
	for _, u := range urls {
		if _, ok := d.Pages[d.RenderURL(u)]; !ok {
			return nil, fmt.Errorf("store_deps.json has no entry for %s: %s", u, again)
		}
		if _, kind := StorePageKind(u); kind == StoreKindList && priceDecidesList(u) && d.Lists[d.listBase(u)] == nil {
			return nil, fmt.Errorf("store_deps.json has no products for the list of %s: %s", u, again)
		}
	}
	return d, nil
}

// parseStoreDeps reads the file, with no check against a checkout.
func parseStoreDeps(data []byte) (*StoreDeps, error) {
	var d StoreDeps
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("store_deps.json: %w", err)
	}
	return &d, nil
}

// productsIn finds the products a JSON value shows, and what of them: an object with a slug and a price
// shows the price, with a slug and a stock the stock.
func productsIn(v any, found map[string]string) {
	switch x := v.(type) {
	case map[string]any:
		if slug, ok := x["slug"].(string); ok {
			_, price := x["priceCents"].(float64)
			_, stock := x["stock"].(float64)
			if price {
				found[slug] += depPrice
			}
			if stock {
				found[slug] += depStock
			}
		}
		for _, e := range x {
			productsIn(e, found)
		}
	case []any:
		for _, e := range x {
			productsIn(e, found)
		}
	}
}

// canonFields is the fields of a product as a set, written price first: a product shown in two places is // seen with the same fields, however often.
func canonFields(f string) string {
	out := ""
	if strings.Contains(f, depPrice) {
		out += depPrice
	}
	if strings.Contains(f, depStock) {
		out += depStock
	}
	if strings.Contains(f, depFlag) {
		out += depFlag
	}
	return out
}

// depList writes what a page shows of its products as dependencies, in order.
func depList(found map[string]string) []string {
	out := []string{}
	for slug, fields := range found {
		if slug == allProducts {
			for _, f := range []string{depPrice, depFlag} {
				if strings.Contains(fields, f) {
					out = append(out, slug+":"+f)
				}
			}
			continue
		}
		switch {
		case strings.Contains(fields, depPrice) && strings.Contains(fields, depStock):
			out = append(out, slug)
		case strings.Contains(fields, depPrice):
			out = append(out, slug+":"+depPrice)
		case strings.Contains(fields, depStock):
			out = append(out, slug+":"+depStock)
		}
	}
	slices.Sort(out)
	return out
}

// collectStoreDeps asks the server at base (a Tifa with its memo off, with the data it starts with) for
// the data of each URL the way the browser router does, with the build ID of the build, and makes the
// dependencies of the pages. The keep table is the one RenderURL uses.
func collectStoreDeps(ctx context.Context, base, buildID string, query map[string][]string, urls []string) (*StoreDeps, error) {
	d := &StoreDeps{Query: query, Pages: map[string][]string{}}
	found := map[string]map[string]string{}
	var common map[string]string
	for _, u := range urls {
		data, err := fetchData(ctx, base, buildID, u)
		if err != nil {
			return nil, err
		}
		page := map[string]string{}
		for id, r := range data.Routes {
			if r.Status != 200 {
				return nil, fmt.Errorf("GET %s: the data of %s has status %d", u, id, r.Status)
			}
			if id == routeOf(u) {
				// The parts whose products a price or a stock decides are the model's (versionModel).
				fields, ok := r.Data.(map[string]any)
				for _, part := range decidedParts(u) {
					if _, has := fields[part]; !ok || !has {
						return nil, fmt.Errorf("GET %s: the data of %s has no %s", u, id, part)
					}
					delete(fields, part)
				}
			}
			productsIn(r.Data, page)
		}
		for k, v := range page {
			page[k] = canonFields(v)
		}
		if _, kind := StorePageKind(u); kind == StoreKindList {
			// A list shows the range of all the prices (the bands and the dearest price) and its best rated
			// products are the cheaper first, and with only what is in stock it counts the products in stock.
			page[allProducts] = depPrice
			if _, _, _, _, inStock := listQuery(u); inStock {
				page[allProducts] += depFlag
			}
		}
		key := d.RenderURL(u)
		if old, ok := found[key]; ok {
			// Two URLs the render sees alike hold the same data.
			if !slices.Equal(depList(old), depList(page)) {
				return nil, fmt.Errorf("%s and another URL with the render URL %s show other products", u, key)
			}
			continue
		}
		found[key] = page
		if common == nil {
			common = map[string]string{}
			for k, v := range page {
				common[k] = v
			}
		} else {
			for k, v := range common {
				if page[k] != v {
					delete(common, k)
				}
			}
		}
	}
	d.Common = depList(common)
	d.Lists = map[string][]string{}
	for _, u := range urls {
		if _, kind := StorePageKind(u); kind != StoreKindList || !priceDecidesList(u) {
			continue
		}
		if b := d.listBase(u); d.Lists[b] == nil {
			list, err := collectList(ctx, base, buildID, b)
			if err != nil {
				return nil, err
			}
			d.Lists[b] = list
		}
	}
	for key, page := range found {
		rest := map[string]string{}
		for k, v := range page {
			if common[k] != v {
				rest[k] = v
			}
		}
		d.Pages[key] = depList(rest)
	}
	return d, nil
}

// WriteStoreDeps starts the shop with its render memo off and the data it starts with, makes
// store_deps.json for it and writes it to out.
func (e Env) WriteStoreDeps(ctx context.Context, log io.Writer, out string) error {
	out, err := filepath.Abs(out)
	if err != nil {
		return err
	}
	src, err := os.ReadFile(filepath.Join(e.Shop, ".tifa", "tifaapp", "app.go"))
	if err != nil {
		return fmt.Errorf("build the shop first: %w", err)
	}
	all, err := routeQueries(src)
	if err != nil {
		return err
	}
	query := map[string][]string{}
	for _, r := range usedRoutes() {
		q, ok := all[r]
		if !ok {
			return fmt.Errorf("the build of the shop has no route %s", r)
		}
		query[r] = q
	}
	buildID, err := e.ShopBuildID()
	if err != nil {
		return err
	}
	i := slices.IndexFunc(e.Specs(), func(s Spec) bool { return s.Name == "tifa-nocache" })
	server, err := e.Specs()[i].Start(ctx, "")
	if err != nil {
		return err
	}
	defer server.Stop()
	urls := depsURLs()
	d, err := collectStoreDeps(ctx, server.Base, buildID, query, urls)
	if err != nil {
		return err
	}
	if d.Sources, err = depsSourcesHash(e.Root); err != nil {
		return err
	}
	d.URLs = hashOf(urls)

	data, err := marshalStoreDeps(d)
	if err != nil {
		return err
	}
	fmt.Fprintf(log, "store_deps.json: %d URLs, %d bytes\n", len(d.Pages), len(data))
	return os.WriteFile(out, data, 0o644)
}

// marshalStoreDeps writes the file with one line for each URL, in order, so that a change shows in a diff.
func marshalStoreDeps(d *StoreDeps) ([]byte, error) {
	line := func(v any) string {
		b, _ := json.Marshal(v)
		return string(b)
	}
	var b bytes.Buffer
	fmt.Fprintf(&b, "{\n \"sources\": %s,\n \"urls\": %s,\n", line(d.Sources), line(d.URLs))
	b.WriteString(" \"query\": {\n")
	routes := slices.Sorted(maps.Keys(d.Query))
	for i, r := range routes {
		fmt.Fprintf(&b, "  %s: %s%s\n", line(r), line(d.Query[r]), comma(i, len(routes)))
	}
	fmt.Fprintf(&b, " },\n \"common\": %s,\n \"pages\": {\n", line(d.Common))
	keys := slices.Sorted(maps.Keys(d.Pages))
	for i, k := range keys {
		fmt.Fprintf(&b, "  %s: %s%s\n", line(k), line(d.Pages[k]), comma(i, len(keys)))
	}
	b.WriteString(" },\n \"lists\": {\n")
	bases := slices.Sorted(maps.Keys(d.Lists))
	for i, k := range bases {
		fmt.Fprintf(&b, "  %s: %s%s\n", line(k), line(d.Lists[k]), comma(i, len(bases)))
	}
	b.WriteString(" }\n}\n")
	return b.Bytes(), nil
}

func comma(i, n int) string {
	if i < n-1 {
		return ","
	}
	return ""
}

// storeDepsOnce is the embedded file, read once, with no check against a checkout.
var storeDepsOnce = sync.OnceValues(func() (*StoreDeps, error) { return parseStoreDeps(storeDepsJSON) })

// routesData is the answer to a request for the data of a page: the result of each route.
type routesData struct {
	Routes map[string]struct {
		Status int `json:"status"`
		Data   any `json:"data"`
	} `json:"routes"`
}

// fetchData asks the server at base for the data of the Layout and the page of u, as the browser router does.
func fetchData(ctx context.Context, base, buildID, u string) (routesData, error) {
	var data routesData
	sep := "?"
	if strings.Contains(u, "?") {
		sep = "&"
	}
	req, err := http.NewRequestWithContext(ctx, "GET", base+u+sep+"_tifa="+buildID+".11", nil)
	if err != nil {
		return data, err
	}
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return data, err
	}
	body, err := io.ReadAll(res.Body)
	res.Body.Close()
	if err != nil || res.StatusCode != 200 {
		return data, fmt.Errorf("GET %s: %d %v %.200s", u, res.StatusCode, err, body)
	}
	if err := json.Unmarshal(body, &data); err != nil || len(data.Routes) == 0 {
		return data, fmt.Errorf("GET %s: no data of routes: %v %.200s", u, err, body)
	}
	return data, nil
}

// collectList asks for every page of the list at base (StoreDeps.listBase: a list with no range, no
// stock and no sort by price) and returns the products on them in order: the products that pass its
// filters, in the order of the catalogue or of its sort.
func collectList(ctx context.Context, server, buildID, base string) ([]string, error) {
	var out []string
	for page := 1; ; page++ {
		u := base + "?page=" + strconv.Itoa(page)
		if strings.Contains(base, "?") {
			u = base + "&page=" + strconv.Itoa(page)
		}
		data, err := fetchData(ctx, server, buildID, u)
		if err != nil {
			return nil, err
		}
		list, ok := data.Routes[storeList].Data.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("GET %s: no data of the list", u)
		}
		products, _ := list["products"].([]any)
		for _, p := range products {
			card, _ := p.(map[string]any)
			slug, _ := card["slug"].(string)
			out = append(out, slug)
		}
		if pages, _ := list["pages"].(float64); float64(page) >= pages {
			return out, nil
		}
	}
}
