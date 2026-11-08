package compare_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/liustack/tifa/test/compare"
)

// writeReply is what a write endpoint answered: its status and, for a 200, the order it answered with.
type writeReply struct {
	Status int
	Order  any
}

// call sends a JSON write and returns its status and the order of a 200, without the instants of
// the server's clock, which differ between servers that run one after another. It does not follow a
// redirect: a checkout answers 303 to the page of the order, which is then the answer.
func call(t *testing.T, base, method, path, body string) writeReply {
	t.Helper()
	req, err := http.NewRequest(method, base+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := noRedirect.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	r := writeReply{Status: res.StatusCode}
	if res.StatusCode == 303 {
		r.Order = res.Header.Get("Location")
	}
	if res.StatusCode == 200 {
		if err := json.Unmarshal(data, &r.Order); err != nil {
			t.Fatalf("%s %s: %v\n%s", method, path, err, data)
		}
		r.Order = withoutClock(r.Order)
	}
	return r
}

var noRedirect = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

// withoutClock drops every instant from an answer: placing and shipping use the clock of the server,
// and servers that run one after another have different ones.
func withoutClock(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			if k == "at" || strings.HasSuffix(k, "At") {
				delete(x, k)
				continue
			}
			x[k] = withoutClock(e)
		}
	case []any:
		for i := range x {
			x[i] = withoutClock(x[i])
		}
	}
	return v
}

// instant masks the times and dates a page writes from the server's clock.
var instant = regexp.MustCompile(`\d{1,4}[-/.]\d{1,2}[-/.]\d{1,4}|\d{1,2}:\d{2}( ?[AP]M)?|[A-Z][a-z]{2} \d{1,2}(, \d{4})?|(\d+ )?(minute|hour|day)s? ago|just now|now`)

func viewOf(t *testing.T, base string, p compare.Page) compare.View {
	t.Helper()
	body, err := compare.Fetch(base, p)
	if err != nil {
		t.Fatal(err)
	}
	v := compare.ParseView(body)
	for i, s := range v.Text {
		v.Text[i] = instant.ReplaceAllString(s, "~")
	}
	v.Bytes = 0
	return v
}

// TestWritesLandAndPagesChangeAlike sends the four writes of the shop's admin (place an order, ship
// it, change the recipient, add a note), a checkout of the storefront, which becomes an order of the
// studio shop and takes its pieces from the stock, and a staff edit of a product, with their
// refusals, to each server and checks that all answer with the same status and the same order, and
// that the pages which show the data change, and then show the same, in every server. A page is
// checked before and after each write, so a page cached across a write is caught, and the pages are
// those the write reaches: the order itself, the lists and stats of its project, the orders that
// share its product or its customer, and the product pages of the back office and the storefront.
func TestWritesLandAndPagesChangeAlike(t *testing.T) {
	env := setup(t)
	buildApps(t, env)
	ctx := context.Background()

	list, stats := compare.Page{Name: "list studio", URL: "/projects/studio"}, compare.Page{Name: "stats studio", URL: "/projects/studio/stats"}
	order := func(id string) compare.Page {
		return compare.Page{Name: "order " + id, URL: "/projects/studio/orders/" + id}
	}
	weekend := compare.Page{Name: "list weekend", URL: "/projects/weekend"}
	products, tray := compare.ProductPages[0], compare.ProductPages[1]
	weekendProducts, postcards := compare.ProductPages[2], compare.ProductPages[3]
	storeTray := compare.Page{Name: "store product oak-tray", URL: "/store/products/oak-tray"}
	const (
		newOrder  = `{"customer":" 林夏 ","phone":"138-0013 8000","email":"linxia@example.com","address":"上海市徐汇区 · 新地址","note":"请附手写卡片","items":[{"slug":"pour-over-kettle","quantity":2},{"slug":"ceramic-cup","quantity":1}]}`
		weekOrder = `{"customer":"苏禾","phone":"13500135004","email":"suhe@example.com","address":"苏州市姑苏区","note":"","items":[{"slug":"postcards","quantity":3}]}`
		shipping  = `{"carrier":"ZTO","trackingNo":" ZTO20261107001 "}`
		// checkout is the order of a visitor with no cart, who checks out the demonstration cart.
		checkout = `{"email":"emma@example.com","address":{"name":"Emma Walker","phone":"+1 415 555 0134","country":"US","region":"CA","city":"San Francisco","line1":"1 Market Street","line2":"","postalCode":"94105"},"delivery":"express","payment":"alipay","cardNumber":"","cardExpiry":"","cardCvc":"","billingSame":true,"notes":"","newsletter":false,"terms":true}`
		edit     = `{"priceCents":52000,"stock":5,"nameEn":" Oak Tray, Large ","nameZh":"橡木大托盘","descriptionEn":"White oak, carved from one block.","descriptionZh":"整块白橡木挖制。"}`
		cupEdit  = `{"priceCents":13400,"stock":3,"nameEn":"Ceramic Cup","nameZh":"手作陶瓷杯","descriptionEn":"Thrown by hand.","descriptionZh":"手工拉坯。"}`
		cardEdit = `{"priceCents":3800,"stock":40,"nameEn":" Postcards of Cities ","nameZh":"城市明信片","descriptionEn":"Six cards.","descriptionZh":"六张。"}`
	)

	// A step is a write and the pages it must change.
	type step struct {
		name           string
		method, target string // target is "create:<project>", "order:<project>:<id>", "product:<project>:<slug>" or "checkout"
		body           string
		changes        []compare.Page // pages that must show something new afterwards
	}
	steps := []step{
		{"refuse a bad body", "POST", "create:studio", `{`, nil},
		{"refuse a customer with no name", "POST", "create:studio", strings.Replace(newOrder, `" 林夏 "`, `" "`, 1), nil},
		{"refuse four products", "POST", "create:studio", strings.Replace(newOrder, `"items":[`, `"items":[{"slug":"oak-tray","quantity":1},{"slug":"oak-tray","quantity":1},`, 1), nil},
		{"refuse six pieces", "POST", "create:studio", strings.Replace(newOrder, `"quantity":2`, `"quantity":6`, 1), nil},
		{"refuse an unknown product", "POST", "create:studio", strings.Replace(newOrder, "ceramic-cup", "no-such-product", 1), nil},
		{"refuse a product of the storefront in the other shop", "POST", "create:weekend", strings.Replace(weekOrder, "postcards", "oak-tray", 1), nil},
		{"refuse an unknown project", "POST", "create:nowhere", newOrder, nil},
		// An order staff place takes its pieces from the stock, as a checkout does.
		{"place an order", "POST", "create:studio", newOrder,
			[]compare.Page{list, stats, products, order("SH-1043"), order("SH-1042"), order("SH-1041"), order("SH-1032")}},
		{"place an order in the other shop", "POST", "create:weekend", weekOrder, []compare.Page{weekend, weekendProducts, postcards}},
		{"refuse a bad carrier", "POST", "order:studio:SH-1043", `{"carrier":"XX","trackingNo":"ZTO20261107001"}`, nil},
		{"refuse a short tracking number", "POST", "order:studio:SH-1043", `{"carrier":"ZTO","trackingNo":"short"}`, nil},
		{"refuse an unknown order", "POST", "order:studio:SH-9999", shipping, nil},
		{"refuse an order of the other shop", "POST", "order:weekend:SH-1043", shipping, nil},
		{"ship", "POST", "order:studio:SH-1043", shipping,
			[]compare.Page{list, stats, order("SH-1043"), order("SH-1042"), order("SH-1041")}},
		{"refuse to ship twice", "POST", "order:studio:SH-1043", shipping, nil},
		{"refuse to change a shipped order", "PATCH", "order:studio:SH-1043", `{"customer":"林夏","phone":"13800138000","address":"上海"}`, nil},
		{"change the recipient", "PATCH", "order:studio:SH-1041", `{"customer":"陈默","phone":"13900139001","address":"杭州市西湖区 · 新地址"}`, []compare.Page{order("SH-1041")}},
		{"refuse an empty note", "PUT", "order:studio:SH-1041", `{"text":"  "}`, nil},
		{"refuse a long note", "PUT", "order:studio:SH-1041", `{"text":"` + strings.Repeat("字", 201) + `"}`, nil},
		{"refuse a note on an unknown order", "PUT", "order:studio:SH-9999", `{"text":"hi"}`, nil},
		{"add a note to a shipped order", "PUT", "order:studio:SH-1040", `{"text":" 顾客来电，已收到。 "}`, []compare.Page{order("SH-1040")}},
		{"add a note", "PUT", "order:studio:SH-1043", `{"text":"客户要求周末送达。"}`, []compare.Page{order("SH-1043")}},
		// The demonstration cart holds a kettle and cups, so the stock cards of the orders that wait for
		// them show what is left to sell.
		{"check out", "POST", "checkout", checkout, []compare.Page{list, stats, products, order("SH-1042"), order("SH-1041")}},
		{"refuse an edit with no English title", "PATCH", "product:studio:oak-tray", strings.Replace(edit, `" Oak Tray, Large "`, `"  "`, 1), nil},
		{"refuse an edit with a stock below 0", "PATCH", "product:studio:oak-tray", strings.Replace(edit, `"stock":5`, `"stock":-1`, 1), nil},
		// A title that is not a JSON string is a body Go cannot decode (400), not an empty title (422).
		{"refuse an edit whose English title is a number", "PATCH", "product:studio:oak-tray", strings.Replace(edit, `" Oak Tray, Large "`, `123`, 1), nil},
		{"refuse an edit in the other shop", "PATCH", "product:weekend:oak-tray", edit, nil},
		{"refuse an edit of an unknown product", "PATCH", "product:studio:no-such-product", edit, nil},
		{"edit a product", "PATCH", "product:studio:oak-tray", edit, []compare.Page{products, tray, storeTray}},
		// SH-1041 waits for two cups: its stock card shows the stock staff set.
		{"edit the stock of a product an order waits for", "PATCH", "product:studio:ceramic-cup", cupEdit, []compare.Page{products, order("SH-1041")}},
		{"place an order of the edited product", "POST", "create:studio", strings.Replace(newOrder, "ceramic-cup", "oak-tray", 1),
			[]compare.Page{list, stats, products, tray, storeTray, order("SH-1045")}},
		// The edit left 5 trays and the order took one: an order of 5 is refused and takes nothing.
		{"refuse an order over the stock", "POST", "create:studio", strings.Replace(newOrder, `{"slug":"ceramic-cup","quantity":1}`, `{"slug":"oak-tray","quantity":5}`, 1), nil},
		{"edit a product of the other shop", "PATCH", "product:weekend:postcards", cardEdit, []compare.Page{weekendProducts, postcards}},
		// Go decodes a body of null into the zero value, which then fails its checks (422). An int takes
		// only a whole number written without a fraction or an exponent, in the range of an int: any
		// other is a body Go cannot decode (400).
		{"refuse an order whose body is null", "POST", "create:studio", `null`, nil},
		{"refuse an edit whose body is null", "PATCH", "product:studio:oak-tray", `null`, nil},
		{"refuse a stock written 1.0", "PATCH", "product:studio:oak-tray", strings.Replace(edit, `"stock":5`, `"stock":1.0`, 1), nil},
		{"refuse a price written with an exponent", "PATCH", "product:studio:oak-tray", strings.Replace(edit, `"priceCents":52000`, `"priceCents":52e3`, 1), nil},
		{"refuse a stock past the range of an int", "PATCH", "product:studio:oak-tray", strings.Replace(edit, `"stock":5`, `"stock":9223372036854775808`, 1), nil},
		{"refuse a quantity written 2.0", "POST", "create:studio", strings.Replace(newOrder, `"quantity":2`, `"quantity":2.0`, 1), nil},
	}

	type outcome struct {
		reply writeReply
		views map[string]compare.View // the pages of the step, after it
	}
	results := map[string][]outcome{}
	var names []string
	for _, spec := range env.Specs() {
		if strings.HasSuffix(spec.Name, "-jitless") || spec.AcceptEncoding != "" || spec.Name == "tifa-nocache" {
			continue
		}
		names = append(names, spec.Name)
		t.Run(spec.Name, func(t *testing.T) {
			server, err := spec.Start(ctx, "")
			if err != nil {
				t.Fatal(err)
			}
			defer server.Stop()
			seen := map[string]compare.View{} // the last view of each page, which also puts it into Tifa's render memo
			peek := func(p compare.Page) compare.View {
				v := viewOf(t, server.Base, p)
				seen[p.URL] = v
				return v
			}
			for _, p := range []compare.Page{list, stats, weekend, order("SH-1042"), order("SH-1041"), order("SH-1032"), order("SH-1040"), products, tray, storeTray, weekendProducts, postcards} {
				peek(p)
			}
			for _, s := range steps {
				var path string
				parts := strings.Split(s.target, ":")
				switch parts[0] {
				case "create":
					path = spec.Create(parts[1])
				case "product":
					path = spec.EditProduct(parts[1], parts[2])
				case "checkout":
					path = spec.StoreWrite("/store/checkout")
				default:
					path = spec.Write(parts[1], parts[2])
				}
				out := outcome{reply: call(t, server.Base, s.method, path, s.body), views: map[string]compare.View{}}
				for _, p := range s.changes {
					v := viewOf(t, server.Base, p)
					out.views[p.URL] = v
					if old, ok := seen[p.URL]; ok && compare.Diff(old, v) == "" {
						t.Errorf("%s: %s did not change", s.name, p.Name)
					}
					seen[p.URL] = v
				}
				// A refused write changes no page. After a write that landed, the other pages are read again
				// to be the reference for the next step.
				for url, old := range seen {
					if _, reached := out.views[url]; reached {
						continue
					}
					v := viewOf(t, server.Base, compare.Page{Name: url, URL: url})
					if d := compare.Diff(old, v); d != "" && len(s.changes) == 0 {
						t.Errorf("%s: %s changed although the write was refused:\n%s", s.name, url, d)
					}
					seen[url] = v
				}
				results[spec.Name] = append(results[spec.Name], out)
			}
		})
	}
	if len(names) < 4 {
		t.Fatalf("only %v were checked", names)
	}
	wantStatus := []int{400, 422, 422, 422, 422, 422, 404, 200, 200, 422, 422, 404, 404, 200, 409, 409, 200, 422, 422, 404, 200, 200, 303, 422, 422, 400, 404, 404, 200, 200, 200, 409, 200, 422, 422, 400, 400, 400, 400}
	if len(wantStatus) != len(steps) {
		t.Fatalf("%d statuses for %d steps", len(wantStatus), len(steps))
	}
	ref := results["tifa"]
	for _, name := range names {
		for i, s := range steps {
			got := results[name][i]
			if got.reply.Status != wantStatus[i] {
				t.Errorf("%s, %s: status %d, want %d", name, s.name, got.reply.Status, wantStatus[i])
			}
			if name == "tifa" {
				continue
			}
			if !reflect.DeepEqual(got.reply.Order, ref[i].reply.Order) {
				t.Errorf("%s, %s: answer differs from tifa:\n%s", name, s.name, strings.Join(compare.DiffData(ref[i].reply.Order, got.reply.Order, 6), "\n"))
			}
			for url, v := range got.views {
				if d := compare.Diff(ref[i].views[url], v); d != "" {
					t.Errorf("%s, %s: %s differs from tifa:\n%s", name, s.name, url, d)
				}
			}
		}
	}
	if a, _ := json.Marshal(ref[7].reply.Order); !strings.Contains(string(a), `"SH-1043"`) {
		t.Errorf("the first order placed is not SH-1043: %s", a)
	}
	if a, _ := json.Marshal(ref[8].reply.Order); !strings.Contains(string(a), `"WK-0202"`) {
		t.Errorf("the first order placed in weekend is not WK-0202: %s", a)
	}
	if placed, _ := ref[22].reply.Order.(string); placed != "/store/checkout?placed=SH-1044" {
		t.Errorf("the checkout placed %q, want the order SH-1044 of the studio shop", placed)
	}
	if a, _ := json.Marshal(ref[30].reply.Order); !strings.Contains(string(a), `"name":"橡木大托盘"`) || !strings.Contains(string(a), `"priceCents":52000`) {
		t.Errorf("the order of the edited product does not take its new name and price: %s", a)
	}
}

// adminWrites are the four kinds of write of the admin, in an order every one of them is
// allowed in: three orders are placed (SH-1043, WK-0202 and SH-1044), SH-1043 is shipped and then
// gets a note (the port counts milliseconds and Go nanoseconds, so a note and a shipment of one
// order made in the same millisecond must come in the order the log lists them), and an order of
// the data is shipped too.
func adminWrites() []compare.DomainWrite {
	place := func(project string, items ...map[string]any) compare.DomainWrite {
		return compare.DomainWrite{Kind: compare.WriteCreate, Project: project, Body: map[string]any{
			"customer": " 林夏 ", "phone": "138-0013 8000", "email": "linxia@example.com", "address": "上海市徐汇区 · 新地址", "note": "请附手写卡片", "items": items}}
	}
	item := func(slug string, quantity int) map[string]any {
		return map[string]any{"slug": slug, "quantity": quantity}
	}
	return []compare.DomainWrite{
		place("studio", item("pour-over-kettle", 2), item("ceramic-cup", 1)),
		place("weekend", item("postcards", 3)),
		// Two sizes of the tray in one order: one order of the product in the stats.
		place("studio", map[string]any{"slug": "oak-tray", "color": "oat", "size": "M", "quantity": 1}, map[string]any{"slug": "oak-tray", "color": "oat", "size": "L", "quantity": 1}),
		{Kind: compare.WriteNote, Project: "studio", Order: "SH-1040", Body: map[string]any{"text": " 顾客来电，已收到。 "}},
		{Kind: compare.WriteRecipient, Project: "studio", Order: "SH-1044", Body: map[string]any{"customer": "陈默", "phone": "13900139001", "address": "杭州市西湖区 · 新地址"}},
		{Kind: compare.WriteShip, Project: "studio", Order: "SH-1043", Body: map[string]any{"carrier": "ZTO", "trackingNo": " ZTO20261107001 "}},
		{Kind: compare.WriteNote, Project: "studio", Order: "SH-1043", Body: map[string]any{"text": "客户要求周末送达。"}},
		{Kind: compare.WriteShip, Project: "studio", Order: "SH-1042", Body: map[string]any{"carrier": "SF", "trackingNo": "SF20261107002"}},
		{Kind: compare.WriteNote, Project: "studio", Order: "SH-1042", Body: map[string]any{"text": "已发出。"}},
	}
}

// TestPagesStayTheSizeOfAPageAsOrdersGrow places 40 orders for a customer who has orders already and
// checks, in each server, that the pages that list orders hold the newest few and not all: the
// order list holds one page of 25 rows (a header row and 25), the other orders of the customer on
// an order page are at most 10, and the numbers over all orders (the count in the tabs of the list
// and the orders of the customer) went up. The four must show the same.
func TestPagesStayTheSizeOfAPageAsOrdersGrow(t *testing.T) {
	env := setup(t)
	buildApps(t, env)
	ctx := context.Background()
	list := compare.Page{Name: "list studio", URL: "/projects/studio"}
	customer := compare.Page{Name: "order SH-1032", URL: "/projects/studio/orders/SH-1032"} // 林夏, who has orders
	newest := compare.Page{Name: "order SH-1082", URL: "/projects/studio/orders/SH-1082"}
	stats := compare.Page{Name: "stats studio", URL: "/projects/studio/stats"}
	views := map[string][]compare.View{}
	var names []string
	for _, spec := range env.Specs() {
		if strings.HasSuffix(spec.Name, "-jitless") || spec.AcceptEncoding != "" || spec.Name == "tifa-nocache" {
			continue
		}
		names = append(names, spec.Name)
		t.Run(spec.Name, func(t *testing.T) {
			server, err := spec.Start(ctx, "")
			if err != nil {
				t.Fatal(err)
			}
			defer server.Stop()
			before := viewOf(t, server.Base, customer)
			listBefore := viewOf(t, server.Base, list)
			body := `{"customer":"林夏","phone":"13800138000","email":"linxia@example.com","address":"上海市徐汇区","note":"","items":[{"slug":"oak-tray","quantity":1}]}`
			for range 40 {
				if r := call(t, server.Base, "POST", spec.Create("studio"), body); r.Status != 200 {
					t.Fatalf("place an order: %d", r.Status)
				}
			}
			var got []compare.View
			for _, p := range []compare.Page{list, customer, newest, stats} {
				got = append(got, viewOf(t, server.Base, p))
			}
			views[spec.Name] = got
			if rows := got[0].Count["tr"]; rows != 26 {
				t.Errorf("the order list has %d rows after 40 orders, want a header and 25", rows)
			}
			if got[0].Count["a"] > listBefore.Count["a"]+10 {
				t.Errorf("the order list has %d links, from %d", got[0].Count["a"], listBefore.Count["a"])
			}
			if li := got[1].Count["li"]; li > before.Count["li"]+SideRows {
				t.Errorf("the page of an order of the customer has %d list items, from %d", li, before.Count["li"])
			}
			if !strings.Contains(strings.Join(got[1].Text, "|"), "earlier orders") {
				t.Errorf("the page of an order of the customer does not say how many orders are left out")
			}
		})
	}
	if len(names) < 4 {
		t.Fatalf("only %v were checked", names)
	}
	for _, name := range names[1:] {
		for i, p := range []string{"list", "customer", "newest", "stats"} {
			if d := compare.Diff(views[names[0]][i], views[name][i]); d != "" {
				t.Errorf("%s: the %s page differs from %s:\n%s", name, p, names[0], d)
			}
		}
	}
}

// SideRows is how many orders of the same customer an order page lists at most.
const SideRows = 10

// TestStatsCarrierTableCountsAShip places an order and ships it with ZTO in each server, and checks
// that the carrier table of the stats page, as the admin check of the browse comparison reads it
// (CarrierParcels), counts one more ZTO parcel, the same in all four.
func TestStatsCarrierTableCountsAShip(t *testing.T) {
	env := setup(t)
	buildApps(t, env)
	ctx := context.Background()
	stats := compare.Page{Name: "stats studio", URL: "/projects/studio/stats"}
	const order = `{"customer":"林夏","phone":"13800138000","email":"linxia@example.com","address":"上海市徐汇区","note":"","items":[{"slug":"oak-tray","quantity":1}]}`
	counts := map[string][2]int{}
	for _, spec := range env.Specs() {
		if strings.HasSuffix(spec.Name, "-jitless") || spec.AcceptEncoding != "" || spec.Name == "tifa-nocache" {
			continue
		}
		server, err := spec.Start(ctx, "")
		if err != nil {
			t.Fatal(err)
		}
		parcels := func() int {
			doc, err := compare.Fetch(server.Base, stats)
			if err != nil {
				t.Fatal(err)
			}
			n, err := compare.CarrierParcels(string(doc), "ZTO Express")
			if err != nil {
				t.Fatalf("%s: %v", spec.Name, err)
			}
			return n
		}
		before := parcels()
		if r := call(t, server.Base, "POST", spec.Create("studio"), order); r.Status != 200 {
			t.Fatalf("%s: place: %d", spec.Name, r.Status)
		}
		if r := call(t, server.Base, "POST", spec.Write("studio", "SH-1043"), `{"carrier":"ZTO","trackingNo":"ZTO20261108001"}`); r.Status != 200 {
			t.Fatalf("%s: ship: %d", spec.Name, r.Status)
		}
		counts[spec.Name] = [2]int{before, parcels()}
		server.Stop()
	}
	for name, c := range counts {
		if c[1] != c[0]+1 || c != counts["tifa"] {
			t.Errorf("%s: %d ZTO parcels before the ship and %d after, tifa %v", name, c[0], c[1], counts["tifa"])
		}
	}
}
