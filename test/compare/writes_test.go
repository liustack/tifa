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
// the server's clock, which differ between servers that run one after another.
func call(t *testing.T, base, method, path, body string) writeReply {
	t.Helper()
	req, err := http.NewRequest(method, base+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	r := writeReply{Status: res.StatusCode}
	if res.StatusCode == 200 {
		if err := json.Unmarshal(data, &r.Order); err != nil {
			t.Fatalf("%s %s: %v\n%s", method, path, err, data)
		}
		r.Order = withoutClock(r.Order)
	}
	return r
}

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

// TestWritesLandAndPagesChangeAlike sends the four writes of the back office (place an order, ship
// it, change the recipient, add a note) and their refusals to each server and checks that all
// answer with the same status and the same order, and that the pages which show the data change,
// and then show the same, in every server. A page is checked before and after each write, so a
// page cached across a write is caught, and the pages are those the write reaches: the order
// itself, the lists and stats of its project, the orders that share its product or its customer,
// and the orders of the project that show the carriers.
func TestWritesLandAndPagesChangeAlike(t *testing.T) {
	env := setup(t)
	buildApps(t, env)
	ctx := context.Background()

	list, stats := compare.Page{Name: "list studio", URL: "/projects/studio"}, compare.Page{Name: "stats studio", URL: "/projects/studio/stats"}
	order := func(id string) compare.Page {
		return compare.Page{Name: "order " + id, URL: "/projects/studio/orders/" + id}
	}
	weekend := compare.Page{Name: "list weekend", URL: "/projects/weekend"}
	const (
		newOrder  = `{"customer":" 林夏 ","phone":"138-0013 8000","email":"linxia@example.com","address":"上海市徐汇区 · 新地址","note":"请附手写卡片","items":[{"slug":"pour-over-kettle","quantity":2},{"slug":"ceramic-cup","quantity":1}]}`
		weekOrder = `{"customer":"苏禾","phone":"13500135004","email":"suhe@example.com","address":"苏州市姑苏区","note":"","items":[{"slug":"postcards","quantity":3}]}`
		shipping  = `{"carrier":"ZTO","trackingNo":" ZTO20261107001 "}`
	)

	// A step is a write and the pages it must change.
	type step struct {
		name           string
		method, target string // target is "create:<project>" or "order:<project>:<id>"
		body           string
		changes        []compare.Page // pages that must show something new afterwards
	}
	steps := []step{
		{"refuse a bad body", "POST", "create:studio", `{`, nil},
		{"refuse a customer with no name", "POST", "create:studio", strings.Replace(newOrder, `" 林夏 "`, `" "`, 1), nil},
		{"refuse four products", "POST", "create:studio", strings.Replace(newOrder, `"items":[`, `"items":[{"slug":"oak-tray","quantity":1},{"slug":"oak-tray","quantity":1},`, 1), nil},
		{"refuse six pieces", "POST", "create:studio", strings.Replace(newOrder, `"quantity":2`, `"quantity":6`, 1), nil},
		{"refuse a product of the other shop", "POST", "create:studio", strings.Replace(newOrder, "ceramic-cup", "canvas-tote", 1), nil},
		{"refuse an unknown project", "POST", "create:nowhere", newOrder, nil},
		{"place an order", "POST", "create:studio", newOrder,
			[]compare.Page{list, stats, order("SH-1043"), order("SH-1042"), order("SH-1041"), order("SH-1032")}},
		{"place an order in the other shop", "POST", "create:weekend", weekOrder, []compare.Page{weekend}},
		{"refuse a bad carrier", "POST", "order:studio:SH-1043", `{"carrier":"XX","trackingNo":"ZTO20261107001"}`, nil},
		{"refuse a short tracking number", "POST", "order:studio:SH-1043", `{"carrier":"ZTO","trackingNo":"short"}`, nil},
		{"refuse an unknown order", "POST", "order:studio:SH-9999", shipping, nil},
		{"refuse an order of the other shop", "POST", "order:weekend:SH-1043", shipping, nil},
		{"ship", "POST", "order:studio:SH-1043", shipping,
			[]compare.Page{list, stats, order("SH-1043"), order("SH-1042"), order("SH-1041"), order("SH-1040")}},
		{"refuse to ship twice", "POST", "order:studio:SH-1043", shipping, nil},
		{"refuse to change a shipped order", "PATCH", "order:studio:SH-1043", `{"customer":"林夏","phone":"13800138000","address":"上海"}`, nil},
		{"change the recipient", "PATCH", "order:studio:SH-1041", `{"customer":"陈默","phone":"13900139001","address":"杭州市西湖区 · 新地址"}`, []compare.Page{order("SH-1041")}},
		{"refuse an empty note", "PUT", "order:studio:SH-1041", `{"text":"  "}`, nil},
		{"refuse a long note", "PUT", "order:studio:SH-1041", `{"text":"` + strings.Repeat("字", 201) + `"}`, nil},
		{"refuse a note on an unknown order", "PUT", "order:studio:SH-9999", `{"text":"hi"}`, nil},
		{"add a note to a shipped order", "PUT", "order:studio:SH-1040", `{"text":" 顾客来电，已收到。 "}`, []compare.Page{order("SH-1040")}},
		{"add a note", "PUT", "order:studio:SH-1043", `{"text":"客户要求周末送达。"}`, []compare.Page{order("SH-1043")}},
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
			seen := map[string]compare.View{} // the last view of each page, which also puts it into Tifa's render cache
			peek := func(p compare.Page) compare.View {
				v := viewOf(t, server.Base, p)
				seen[p.URL] = v
				return v
			}
			for _, p := range []compare.Page{list, stats, weekend, order("SH-1042"), order("SH-1041"), order("SH-1032"), order("SH-1040")} {
				peek(p)
			}
			for _, s := range steps {
				var path string
				parts := strings.Split(s.target, ":")
				if parts[0] == "create" {
					path = spec.Create(parts[1])
				} else {
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
	wantStatus := []int{400, 422, 422, 422, 422, 404, 200, 200, 422, 422, 404, 404, 200, 409, 409, 200, 422, 422, 404, 200, 200}
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
	if a, _ := json.Marshal(ref[6].reply.Order); !strings.Contains(string(a), `"SH-1043"`) {
		t.Errorf("the first order placed is not SH-1043: %s", a)
	}
	if a, _ := json.Marshal(ref[7].reply.Order); !strings.Contains(string(a), `"WK-0202"`) {
		t.Errorf("the first order placed in weekend is not WK-0202: %s", a)
	}
}

// backOfficeWrites are the four kinds of write of the back office, in an order every one of them is
// allowed in: three orders are placed (SH-1043, WK-0202 and SH-1044), SH-1043 is shipped and then
// gets a note (the port counts milliseconds and Go nanoseconds, so a note and a shipment of one
// order made in the same millisecond must come in the order the log lists them), and an order of
// the data is shipped too.
func backOfficeWrites() []compare.DomainWrite {
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
		place("studio", item("oak-tray", 1)),
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
