package compare

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
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
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/liustack/tifa/internal/analyze"
	"github.com/liustack/tifa/internal/bundle"
	"github.com/liustack/tifa/internal/codegen"
)

// What the data of a shared page of the storefront holds, so that the version of a page can follow the
// writes. A page of Tifa is rendered again when the data its Loads give has changed, and the data of a
// storefront page changes with the price and the stock of the products it shows (a checkout takes
// pieces from the stock, and staff change a price or a stock). store_deps.json says, for each URL a
// visit can land on and each URL of a crawler, which products it shows and which of their fields. The
// parts of a page whose products a price or a stock decides (decidedParts) are left out of it: the model
// of the versions works those out as the writes come (versionModel in versions.go), and the file says
// only which fields a product in each of them has (Decided). The pages of the
// product list are left out whole: a search, filters, a sort and a page make more URLs than a file can
// hold, and the list model works out what any of them shows (store_list.go, pageKeys). Make
// it again with `compare deps` after the pages, the catalogue or the URLs change. It carries a hash of
// everything it was made from, and a file that does not match what is checked out is refused
// (CheckStoreDeps), so it cannot go out of date silently.

//go:embed store_deps.json
var storeDepsJSON []byte

// A dependency names a product and what a page has of it, one letter for each: "slug:p" for its price,
// "slug:pl" for its price and what a card says of its stock, and so on. "slug" alone is the price and
// the pieces in stock ("slug:ps").
//
//   - p: the price.
//   - s: the pieces in stock, which every sale changes.
//   - l: what a card says of the stock, which is less: sold out, the pieces when few are left (lowStock
//     or fewer), or in stock, the same for any number above. A sale of a product in good supply does
//     not change it.
//   - f: whether the product is in stock at all.
//
// The pieces say all that the other two say, and what a card says holds whether the product is in
// stock, so a dependency names the most a page has (canonFields).
const (
	depPrice = "p"
	depStock = "s"
	depLow   = "l"
	depFlag  = "f"
)

// lowStock is the most pieces left that a card of the shop counts (lowStock in
// examples/shop/app/store/products/product.go).
const lowStock = 10

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
	// Pages the others, by the URL the render sees (RenderURL). No page of the product list is in Pages:
	// it has the common ones and what the list model works out for it.
	Common []string            `json:"common"`
	Pages  map[string][]string `json:"pages"`
	// Decided is, for each part of a page whose products a price or a stock decides (decidedParts), the
	// fields a product in it has, as the letters of a dependency. A file with none is one of a shop
	// whose cards hold the price and the pieces in stock.
	Decided map[string]string `json:"decided,omitempty"`
	// Build is the build of the shop the file was made from.
	Build BuildInfo `json:"build"`
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
// url.ParseQuery drops is left out. It is for the URLs of the plans and the crawlers, which have an
// ASCII path and no fragment: the server escapes a path as url.URL.EscapedPath does and never sees a
// fragment, and this takes the path as it is written, up to the first question mark.
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
// every URL of a crawler (U), but the pages of the product list, which the list model works out. The
// cart and the checkout are private and have none.
func depsURLs() []string {
	set := map[string]bool{}
	public, _ := CrawlURLs()
	for _, u := range slices.Concat(StoreVisitPages(), public) {
		if _, kind := StorePageKind(u); kind != StoreKindCart && kind != StoreKindCheckout && kind != StoreKindList {
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
// other query parameters than the build of the shop has its routes read, another shop source, another
// set of URLs, or a URL it has no entry for. Make it again with `compare deps`. The shop must be built:
// without its generated program the error is errShopNotBuilt.
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
	if err := checkQueryTable(filepath.Join(root, "examples", "shop"), d); err != nil {
		return nil, fmt.Errorf("%w: %s", err, again)
	}
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
	}
	for key := range d.Pages {
		if _, kind := StorePageKind(key); kind == StoreKindList {
			return nil, fmt.Errorf("store_deps.json has an entry for %s, a page of the product list, which the list model works out: %s", key, again)
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
// (priceCents) shows the price, with the pieces (stock) the pieces, with the pieces when few are left
// (lowStock) what a card says of the stock, and with inStock whether it is in stock.
func productsIn(v any, found map[string]string) {
	switch x := v.(type) {
	case map[string]any:
		if slug, ok := x["slug"].(string); ok {
			if _, ok := x["priceCents"].(float64); ok {
				found[slug] += depPrice
			}
			if _, ok := x["stock"].(float64); ok {
				found[slug] += depStock
			}
			if _, ok := x["lowStock"].(float64); ok {
				found[slug] += depLow
			}
			if _, ok := x["inStock"].(bool); ok {
				found[slug] += depFlag
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

// canonFields is the fields of a product as a set, written price first: a product shown in two places
// is seen with the same fields, however often. Of what it has of the stock only the most is kept: the
// pieces, or else what a card says, or else whether it is in stock.
func canonFields(f string) string {
	out := ""
	if strings.Contains(f, depPrice) {
		out += depPrice
	}
	switch {
	case strings.Contains(f, depStock):
		out += depStock
	case strings.Contains(f, depLow):
		out += depLow
	case strings.Contains(f, depFlag):
		out += depFlag
	}
	return out
}

// allFields is the fields of a product with those they imply: the pieces say what a card says of the
// stock, and that says whether the product is in stock.
func allFields(f string) string {
	if strings.Contains(f, depStock) {
		f += depLow
	}
	if strings.Contains(f, depLow) {
		f += depFlag
	}
	return f
}

// sharedFields is the fields that both a and b have or imply.
func sharedFields(a, b string) string {
	a, b = allFields(a), allFields(b)
	out := ""
	for _, f := range []string{depPrice, depStock, depLow, depFlag} {
		if strings.Contains(a, f) && strings.Contains(b, f) {
			out += f
		}
	}
	return canonFields(out)
}

// depOf writes a product and its fields as a dependency, "" for no field.
func depOf(slug, fields string) string {
	switch fields = canonFields(fields); fields {
	case "":
		return ""
	case depPrice + depStock:
		return slug
	}
	return slug + ":" + fields
}

// fieldsOf reads a dependency: the product and its fields.
func fieldsOf(dep string) (slug, fields string) {
	slug, fields, has := strings.Cut(dep, ":")
	if !has {
		return slug, depPrice + depStock
	}
	return slug, fields
}

// fieldKeys are the version keys of the fields of a product.
func fieldKeys(slug, fields string) []string {
	var keys []string
	for _, f := range fields {
		switch string(f) {
		case depPrice:
			keys = append(keys, priceKey(slug))
		case depStock:
			keys = append(keys, stockKey(slug))
		case depLow:
			keys = append(keys, lowKey(slug))
		case depFlag:
			keys = append(keys, flagKey(slug))
		}
	}
	return keys
}

// depList writes what a page shows of its products as dependencies, in order.
func depList(found map[string]string) []string {
	out := []string{}
	for slug, fields := range found {
		if dep := depOf(slug, fields); dep != "" {
			out = append(out, dep)
		}
	}
	slices.Sort(out)
	return out
}

// collectStoreDeps asks the server at base (a Tifa with its memo off, with the data it starts with) for
// the data of each URL the way the browser router does, with the build ID of the build, and makes the
// dependencies of the pages. The keep table is the one RenderURL uses. A page of the product list is
// none of its URLs: the list model works those out, and one list is asked for (listProbe) only to see
// which fields the products of a list have.
func collectStoreDeps(ctx context.Context, base, buildID string, query map[string][]string, urls []string) (*StoreDeps, error) {
	d := &StoreDeps{Query: query, Pages: map[string][]string{}, Decided: map[string]string{}}
	found := map[string]map[string]string{}
	var common map[string]string
	// decided takes the parts of the data of the route of u whose products a price or a stock decides
	// out of it, and notes which fields their products have: the same for every product of a part, on
	// every page, since the model takes them from the file.
	decided := func(u string, data any) error {
		fields, ok := data.(map[string]any)
		for _, part := range decidedParts(u) {
			if _, has := fields[part]; !ok || !has {
				return fmt.Errorf("GET %s: the data of %s has no %s", u, routeOf(u), part)
			}
			in := map[string]string{}
			productsIn(fields[part], in)
			for slug, has := range in {
				if was, seen := d.Decided[part]; seen && was != canonFields(has) {
					return fmt.Errorf("GET %s: %s has the fields %q of %s and %q of another product", u, part, canonFields(has), slug, was)
				}
				d.Decided[part] = canonFields(has)
			}
			delete(fields, part)
		}
		return nil
	}
	for _, u := range urls {
		if _, kind := StorePageKind(u); kind == StoreKindList {
			return nil, fmt.Errorf("%s is a page of the product list, which the list model works out", u)
		}
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
				if err := decided(u, r.Data); err != nil {
					return nil, err
				}
			}
			productsIn(r.Data, page)
		}
		for k, v := range page {
			page[k] = canonFields(v)
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
			// What every page has of a product is what this one and the others before it have of it.
			for k, v := range common {
				if common[k] = sharedFields(v, page[k]); common[k] == "" {
					delete(common, k)
				}
			}
		}
	}
	data, err := fetchData(ctx, base, buildID, listProbe)
	if err != nil {
		return nil, err
	}
	if r := data.Routes[routeOf(listProbe)]; r.Status != 200 {
		return nil, fmt.Errorf("GET %s: the data of the list has status %d", listProbe, r.Status)
	} else if err := decided(listProbe, r.Data); err != nil {
		return nil, err
	}
	d.Common = depList(common)
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

// listProbe is the list that collectStoreDeps asks for to see the fields of the products of a list.
const listProbe = "/store/products"

// WriteStoreDeps starts the shop with its render memo off and the data it starts with, makes
// store_deps.json for it and writes it to out. It refuses a build of the shop that is not the one the
// checkout makes (shopBuildCurrent), and writes which build it asked.
func (e Env) WriteStoreDeps(ctx context.Context, log io.Writer, out string) error {
	if err := e.shopBuildCurrent(ctx); err != nil {
		return err
	}
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
	if d.Build, err = e.buildInfoOf(); err != nil {
		return err
	}
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
	fmt.Fprintf(&b, "{\n \"sources\": %s,\n \"urls\": %s,\n \"build\": %s,\n", line(d.Sources), line(d.URLs), line(d.Build))
	b.WriteString(" \"query\": {\n")
	routes := slices.Sorted(maps.Keys(d.Query))
	for i, r := range routes {
		fmt.Fprintf(&b, "  %s: %s%s\n", line(r), line(d.Query[r]), comma(i, len(routes)))
	}
	b.WriteString(" },\n")
	if len(d.Decided) > 0 {
		fmt.Fprintf(&b, " \"decided\": %s,\n", line(d.Decided))
	}
	fmt.Fprintf(&b, " \"common\": %s,\n \"pages\": {\n", line(d.Common))
	keys := slices.Sorted(maps.Keys(d.Pages))
	for i, k := range keys {
		fmt.Fprintf(&b, "  %s: %s%s\n", line(k), line(d.Pages[k]), comma(i, len(keys)))
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

// Which build of the shop store_deps.json is of. The file is made by asking a running shop for the data
// of its pages, so it is only as good as the program that answered: `compare deps` refuses a program
// that is not the one the checkout builds (shopBuildCurrent), with the build skipped too, and writes
// which build it asked (BuildInfo). A run checks the file against the build it is about to measure
// (checkQueryTable, from CheckStoreDeps).

// BuildInfo says which build of the shop store_deps.json was asked of: the ID of its front end
// (buildId in .tifa/dist/build.json) and the fingerprint of its table of routes, with the query
// parameters each reads (App.Fingerprint in .tifa/tifaapp/app.go).
type BuildInfo struct {
	ID    string `json:"id"`
	Table string `json:"table"`
}

// errShopNotBuilt says that the shop has no generated program to read the query parameters from.
var errShopNotBuilt = errors.New("the shop is not built")

// shopTable is the generated program of the shop, which holds its routes.
func shopTable(shop string) string {
	return filepath.Join(shop, ".tifa", "tifaapp", "app.go")
}

// checkQueryTable fails when the query parameters the file keeps for the routes are not those of the
// generated program of the shop as it is built now. The parameters a render sees decide which
// requests are one page, so a file with another table counts the hits of other pages.
func checkQueryTable(shop string, d *StoreDeps) error {
	src, err := os.ReadFile(shopTable(shop))
	if err != nil {
		return fmt.Errorf("%w: %v", errShopNotBuilt, err)
	}
	all, err := routeQueries(src)
	if err != nil {
		return err
	}
	if len(d.Query) != len(usedRoutes()) {
		return fmt.Errorf("store_deps.json keeps the query parameters of %d routes, not %d", len(d.Query), len(usedRoutes()))
	}
	for _, r := range usedRoutes() {
		if !slices.Equal(d.Query[r], all[r]) {
			return fmt.Errorf("store_deps.json says %s reads the query parameters %v, and the build of the shop says %v", r, d.Query[r], all[r])
		}
	}
	return nil
}

var fingerprintLine = regexp.MustCompile(`(?m)^\s*Fingerprint:\s*"([0-9a-f]+)",\s*$`)

// tableFingerprint is App.Fingerprint of a generated program: the hash tifa build gives the table of
// routes it wrote.
func tableFingerprint(src []byte) (string, error) {
	m := fingerprintLine.FindSubmatch(src)
	if m == nil {
		return "", errors.New("the generated program of the shop has no Fingerprint")
	}
	return string(m[1]), nil
}

// newerThan returns the first of files that changed after the file at built was written, by the times
// of the files, and "" when none did.
func newerThan(built string, files []string) (string, error) {
	info, err := os.Stat(built)
	if err != nil {
		return "", err
	}
	for _, f := range files {
		x, err := os.Stat(f)
		if err != nil {
			return "", err
		}
		if x.ModTime().After(info.ModTime()) {
			return f, nil
		}
	}
	return "", nil
}

// programInputs are the files under root that go build compiles into the program of the shop: the Go
// files and the embedded files (the front end in .tifa/dist) of every package of the program that is in
// the checkout, and the go.mod and go.sum that pick the others. go list names them.
func programInputs(ctx context.Context, root, shop string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "go", "list", "-deps", "-json=Dir,GoFiles,EmbedFiles,Standard", "./.tifa")
	cmd.Dir = shop
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0") // as tifa build compiles it
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go list of the program of the shop: %w: %s", err, &stderr)
	}
	var files []string
	dec := json.NewDecoder(bytes.NewReader(out))
	for dec.More() {
		var p struct {
			Dir                 string
			GoFiles, EmbedFiles []string
			Standard            bool
		}
		if err := dec.Decode(&p); err != nil {
			return nil, err
		}
		if rel, err := filepath.Rel(root, p.Dir); p.Standard || err != nil || strings.HasPrefix(rel, "..") {
			continue // the standard library and the modules of the module cache, which do not change
		}
		for _, f := range slices.Concat(p.GoFiles, p.EmbedFiles) {
			files = append(files, filepath.Join(p.Dir, f))
		}
	}
	for _, dir := range []string{root, shop} {
		for _, name := range []string{"go.mod", "go.sum"} {
			if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
				files = append(files, filepath.Join(dir, name))
			}
		}
	}
	if len(files) == 0 {
		return nil, errors.New("go list names no file of the program of the shop")
	}
	return files, nil
}

// shopBuildCurrent fails unless the build of the shop in the checkout at root is the one its sources
// make now, so that the program `compare deps` starts answers as the checkout would:
//
//   - the table of routes in .tifa/tifaapp/app.go has the fingerprint of a fresh analysis of the app
//     (as tifatest checks a table), which a change of a route, of a query parameter a Load reads or of
//     the analysis of tifa itself changes;
//   - no file the front end in .tifa/dist was built from has changed since (bundle.ChangedInputs, by
//     the hash of each file);
//   - bin/shop is not older than any file go build compiles into it, the front end it embeds among
//     them. go build keeps no hash of its sources in the program, so this one check goes by the times
//     of the files: a checkout whose files were touched after the build is refused too, which is the
//     safe way to be wrong, and the build fixes it.
func (e Env) shopBuildCurrent(ctx context.Context) error {
	const again = "build the shop again (run `compare deps` without -skip-build)"
	src, err := os.ReadFile(shopTable(e.Shop))
	if err != nil {
		return fmt.Errorf("%w (%v): %s", errShopNotBuilt, err, again)
	}
	built, err := tableFingerprint(src)
	if err != nil {
		return err
	}
	m, err := analyze.Analyze(e.Shop)
	if err != nil {
		return fmt.Errorf("analyzing the shop: %w", err)
	}
	fresh, err := codegen.AppFingerprint(e.Shop, m)
	if err != nil {
		return err
	}
	if built != fresh {
		return fmt.Errorf("the routes of the shop changed since its table in .tifa/tifaapp/app.go was generated (fingerprint %s, now %s): %s", built, fresh, again)
	}
	changed, err := bundle.ChangedInputs(e.Shop, filepath.Join(e.Shop, ".tifa", "dist"))
	if err != nil {
		return fmt.Errorf("the front end of the shop: %w: %s", err, again)
	}
	if len(changed) > 0 {
		return fmt.Errorf("the front end of the shop was built before %s changed (%d files in all): %s", changed[0], len(changed), again)
	}
	inputs, err := programInputs(ctx, e.Root, e.Shop)
	if err != nil {
		return err
	}
	prog := filepath.Join(e.Shop, "bin", "shop")
	newer, err := newerThan(prog, inputs)
	if err != nil {
		return fmt.Errorf("%w (%v): %s", errShopNotBuilt, err, again)
	}
	if newer != "" {
		rel, _ := filepath.Rel(e.Root, newer)
		return fmt.Errorf("bin/shop was built before %s changed: %s", filepath.ToSlash(rel), again)
	}
	return nil
}

// buildInfoOf is the build of the shop in the checkout: the ID of its front end and the fingerprint of
// its table of routes.
func (e Env) buildInfoOf() (BuildInfo, error) {
	id, err := e.ShopBuildID()
	if err != nil {
		return BuildInfo{}, err
	}
	src, err := os.ReadFile(shopTable(e.Shop))
	if err != nil {
		return BuildInfo{}, err
	}
	table, err := tableFingerprint(src)
	if err != nil {
		return BuildInfo{}, err
	}
	return BuildInfo{ID: id, Table: table}, nil
}
