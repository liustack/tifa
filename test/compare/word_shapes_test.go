package compare

import (
	"encoding/json/v2"
	"fmt"
	"maps"
	"net/url"
	"strings"
	"testing"
)

// A call of a server function of TanStack Start as Chromium sent it in a recording of the shop: the
// data of the product list for a search with a price range and a rating, the arguments written by
// seroval and escaped into the query. It is whole. The search word, cup in the recording, is replaced
// by hand with the placeholder of a search for a word of the visit's own.
const startListCall = "/_serverFn/289ca36c3c2eaa6fab2c364bbd8b2c69fe7a033e0aa6824d96cc780fc11c8ab5?payload=" +
	"%7B%22t%22%3A%7B%22t%22%3A10%2C%22i%22%3A0%2C%22p%22%3A%7B%22k%22%3A%5B%22data%22%5D%2C%22v%22%3A%5B%7B%22t%22%3A10%2C%22i%22%3A1%2C%22p%22%3A%7B%22k%22%3A%5B%22lang%22%2C%22query%22%5D%2C%22v%22%3A%5B%7B%22t%22%3A1%2C%22s%22%3A%22en-US%22%7D%2C%7B%22t%22%3A11%2C%22i%22%3A2%2C%22p%22%3A%7B%22k%22%3A%5B%22max%22%2C%22min%22%2C%22q%22%2C%22rating%22%5D%2C%22v%22%3A%5B%7B%22t%22%3A1%2C%22s%22%3A%2259%22%7D%2C%7B%22t%22%3A1%2C%22s%22%3A%2230%22%7D%2C%7B%22t%22%3A1%2C%22s%22%3A%22" +
	"tifaq00000" +
	"%22%7D%2C%7B%22t%22%3A1%2C%22s%22%3A%2244%22%7D%5D%7D%2C%22o%22%3A0%7D%5D%7D%2C%22o%22%3A0%7D%5D%7D%2C%22o%22%3A0%7D%2C%22f%22%3A127%2C%22m%22%3A%5B%5D%7D"

// The next-router-state-tree header of Next.js 16.4.2 as Chromium sent it in a recording of the shop,
// with a click from the product list. It is whole, but for the page segment: 16.4.2 writes it as
// __PAGE__ whatever the query of the page, and the releases that put the query into it wrote
// __PAGE__ and a question mark and the search parameters as JSON (addSearchParamsIfPageSegment). The
// query of a search for the placeholder is put in here by hand in that form, a JSON text inside a
// string of the JSON of the tree, which is then escaped as a whole.
const nextStateTree = "%5B%22%22%2C%7B%22children%22%3A%5B%22store%22%2C%7B%22children%22%3A%5B%5B%22lang%22%2C%22en-US%22%2C%22d%22%2Cnull%5D%2C%7B%22children%22%3A%5B%22(site)%22%2C%7B%22children%22%3A%5B%22products%22%2C%7B%22children%22%3A%5B%22__PAGE__" +
	"%3F%7B%5C%22q%5C%22%3A%5C%22tifaq00000%5C%22%7D" +
	"%22%2C%7B%7D%2Cnull%2C%22refetch%22%2C0%5D%7D%2Cnull%2Cnull%2C0%5D%7D%2Cnull%2Cnull%2C0%5D%7D%2Cnull%2Cnull%2C16%5D%7D%2Cnull%2Cnull%2C16%5D%7D%2Cnull%2Cnull%2C16%5D"

// leaves are the strings, the numbers, the booleans and the nulls of a decoded JSON value, each by
// where it is in the value.
func leaves(v any, at string, out map[string]any) map[string]any {
	switch x := v.(type) {
	case map[string]any:
		for k, c := range x {
			leaves(c, at+"."+k, out)
		}
	case []any:
		for i, c := range x {
			leaves(c, fmt.Sprintf("%s[%d]", at, i), out)
		}
	default:
		out[at] = x
	}
	return out
}

// decoded is a JSON text decoded, by its leaves.
func decoded(t *testing.T, text string) map[string]any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(text), &v); err != nil {
		t.Fatalf("%v: %s", err, text)
	}
	return leaves(v, "", map[string]any{})
}

// The replay puts the word of a visit in place of the placeholder as text (visitSubs), and a router
// carries the query of a page inside other encodings: seroval nodes escaped into a query for TanStack
// Start, a JSON text inside a JSON string escaped as a whole for Next.js. The word and the placeholder
// are letters and digits, which every one of these layers writes as they are, so the text is the
// same at every depth. This holds it on requests of the shape the frameworks send: decoded layer by
// layer, the search word is the word of the visit, and nothing else of the request has changed.
//
// In the recordings of the shop so far the placeholder is in the URL of the document of the search
// and nowhere else, in all four frameworks: a search is a document load, and the page after it sends
// no query of the page before. So no recorded request carries the placeholder in these two places
// today. The test is for the day one does, such as a filter of the form applied to the list of a
// search for a word of the visit's own.
func TestTheWordOfAVisitIsPutInAsTheFrameworksWriteAQuery(t *testing.T) {
	const seed, seq = 1, 123_456
	plan := Plan{Steps: []PlanStep{
		{Kind: StepLoad, Page: "/store"},
		{Kind: StepClick, Page: "/store/products?q=" + SearchPlaceholder, Act: ActSearch, Term: SearchPlaceholder},
		{Kind: StepClick, Page: "/store/products?q=" + SearchPlaceholder + "&min=30&max=59&rating=44", Act: ActForm},
	}}
	subs := subsFor(seed, seq, plan)
	word := uniqueTerm(seed, seq)
	if strings.Count(startListCall, SearchPlaceholder) != 1 || strings.Count(nextStateTree, SearchPlaceholder) != 1 {
		t.Fatal("each request holds the placeholder once")
	}
	// only requires that got is want but for the leaf at, which holds the word in place of the
	// placeholder.
	only := func(what string, got, want map[string]any, at string) {
		t.Helper()
		if want[at] != SearchPlaceholder || got[at] != word {
			t.Errorf("%s: %s is %v, and %v before the word was put in, want %s and the placeholder", what, at, got[at], want[at], word)
		}
		rest := maps.Clone(got)
		rest[at] = want[at]
		if !maps.Equal(rest, want) {
			t.Errorf("%s: more than the search word changed:\n%v\nwas\n%v", what, got, want)
		}
	}

	// TanStack Start: the query of the request, its payload, the nodes of seroval.
	payload := func(call string) (string, map[string]any) {
		t.Helper()
		u, err := url.Parse(call)
		if err != nil || len(u.Query()) != 1 {
			t.Fatalf("%v: %s", err, call)
		}
		return u.Path, decoded(t, u.Query().Get("payload"))
	}
	path, before := payload(startListCall)
	sentPath, sent := payload(subs.apply(startListCall))
	// The arguments of the call are {data: {lang, query: {max, min, q, rating}}}: the keys of an object
	// are in k and its values in v, at the same places.
	const query = ".t.p.v[0].p.v[1].p"
	if before[".t.p.k[0]"] != "data" || before[".t.p.v[0].p.k[1]"] != "query" || before[query+".k[2]"] != "q" {
		t.Fatalf("the call is not the one of a product list with a search: %v", before)
	}
	only("TanStack Start", sent, before, query+".v[2].s")
	if sentPath != path || sent[query+".v[0].s"] != "59" || sent[query+".v[1].s"] != "30" || sent[query+".v[3].s"] != "44" || sent[".t.p.v[0].p.v[0].s"] != "en-US" {
		t.Errorf("TanStack Start: the function, the language or a condition changed: %s %v", sentPath, sent)
	}

	// Next.js: the header, the JSON of the tree, the page segment, the JSON of its search parameters.
	tree := func(header string) (map[string]any, map[string]any) {
		t.Helper()
		text, err := url.PathUnescape(header)
		if err != nil {
			t.Fatal(err)
		}
		all := decoded(t, text)
		for at, v := range all {
			if s, ok := v.(string); ok && strings.HasPrefix(s, "__PAGE__?") {
				params := decoded(t, strings.TrimPrefix(s, "__PAGE__?"))
				delete(all, at)
				return all, params
			}
		}
		t.Fatalf("the tree has no page segment with search parameters: %s", text)
		return nil, nil
	}
	treeBefore, paramsBefore := tree(nextStateTree)
	treeSent, paramsSent := tree(subs.apply(nextStateTree))
	only("Next.js", paramsSent, paramsBefore, ".q")
	if len(paramsSent) != 1 || !maps.Equal(treeSent, treeBefore) || len(treeSent) < 20 {
		t.Errorf("Next.js: the tree changed outside the search parameters of its page:\n%v\nwas\n%v", treeSent, treeBefore)
	}
}
