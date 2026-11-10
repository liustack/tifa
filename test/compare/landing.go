package compare

import (
	"fmt"
	"math/rand/v2"
	"net/url"
	"reflect"
	"strconv"
	"strings"
)

// The parameters a landing page of a shopper visit carries when the visitor comes from an ad or an
// email: a click id that is different for every click, or a parameter that is different for every
// recipient. A shop ignores them, so a server that renders the page for the URL with one renders the
// same page as for the URL without it, and one that keeps pages by URL keeps one for every visit.
//
// The plans hold a placeholder of the length of the real value (landingPlaceholder), the recorder
// opens the landing page with it, and the replay puts a value of its own into every request of
// every visit that has it (landingValues).

// landingValue is one parameter of a landing page: its name, the placeholder the plans and the traces
// carry.
type landingValue struct {
	Name string
	// Placeholder has the length of the real value (LandingValueLengths).
	Placeholder string
	// Alphabet is what a value is made of.
	Alphabet string
	// Bijective says that the value is a one-to-one function of the visit, so that no two visits have
	// the same: a value as short as 10 characters could repeat by chance in a long run.
	Bijective bool
}

const (
	alphaURL = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	alphaHex = "0123456789abcdef"
)

// placeholderOf is prefix padded with zeros to n characters.
func placeholderOf(prefix string, n int) string {
	if len(prefix) > n {
		panic("the placeholder " + prefix + " is longer than its value")
	}
	return prefix + strings.Repeat("0", n-len(prefix))
}

// landingValues are the parameters a landing page carries, in the order of the report. A new kind of
// landing parameter is one more row (a search term that is different for every visit would be one).
var landingValues = []landingValue{
	{"gclid", placeholderOf("tifa-gclid-", LandingValueLengths["gclid"]), alphaURL, false},
	{"fbclid", placeholderOf("tifa-fbclid-", LandingValueLengths["fbclid"]), alphaURL, false},
	{"msclkid", placeholderOf("tifa-msclkid-", LandingValueLengths["msclkid"]), alphaHex, false},
	{"_kx", placeholderOf("tifa-kx-", LandingValueLengths["_kx"]), alphaURL, false},
	{"mc_eid", placeholderOf("tifa0mceid", LandingValueLengths["mc_eid"]), alphaHex, true},
}

// landingValueOf finds the row of a parameter by its name.
func landingValueOf(name string) (landingValue, bool) {
	for _, v := range landingValues {
		if v.Name == name {
			return v, true
		}
	}
	return landingValue{}, false
}

// withLanding puts the parameter name, with its placeholder, on the URL of a page.
func withLanding(page, name string) string {
	v, ok := landingValueOf(name)
	if !ok {
		panic("no landing parameter " + name)
	}
	sep := "?"
	if strings.Contains(page, "?") {
		sep = "&"
	}
	return page + sep + name + "=" + v.Placeholder
}

// landingParam is the name of the landing parameter a URL carries, "" when it carries none.
func landingParam(url string) string {
	_, q, _ := strings.Cut(url, "?")
	for kv := range strings.SplitSeq(q, "&") {
		name, _, _ := strings.Cut(kv, "=")
		if _, ok := landingValueOf(name); ok {
			return name
		}
	}
	return ""
}

// stripLanding is the URL without its landing parameter: the page it asks for.
func stripLanding(url string) string {
	path, q, ok := strings.Cut(url, "?")
	if !ok {
		return url
	}
	var keep []string
	for kv := range strings.SplitSeq(q, "&") {
		if name, _, _ := strings.Cut(kv, "="); name != "" {
			if _, ok := landingValueOf(name); ok {
				continue
			}
		}
		keep = append(keep, kv)
	}
	if len(keep) == 0 {
		return path
	}
	return path + "?" + strings.Join(keep, "&")
}

// landingKinds lists, for the landing parameters in plans, the quota of each kind among the
// shopper visits of a block: the name, and the share of the visits. "" is no parameter.
func landingKinds() (names []string, shares []float64) {
	for _, c := range ClickIDNames {
		names, shares = append(names, c.Name), append(shares, ClickIDShare*c.Share)
	}
	for _, e := range EmailParamNames {
		names, shares = append(names, e.Name), append(shares, EmailParamShare*e.Share)
	}
	return append(names, ""), append(shares, 1-ClickIDShare-EmailParamShare)
}

// putLandingParams gives the landing pages of the shopper plans of block b their parameters by
// quota: exactly ClickIDShare of them a click id and EmailParamShare an email parameter, a visit with
// at most one of them, by the names' shares and in a seeded shuffle of its own (so that the pages of
// the plans are the same whatever the parameters). The clicks of a visit are chosen before, from the
// page without the parameter.
func putLandingParams(seed uint64, b int, shoppers []Plan) {
	rng := rand.New(rand.NewPCG(seed^0xa54ff53a5f1d36f1, uint64(b)))
	names, shares := landingKinds()
	for i, name := range quota(rng, len(shoppers), names, shares) {
		if name != "" {
			shoppers[i].Steps[0].Page = withLanding(shoppers[i].Steps[0].Page, name)
		}
	}
}

// isClickID says whether name is one of the click ids (ClickIDNames), and not an email parameter.
func isClickID(name string) bool {
	for _, c := range ClickIDNames {
		if c.Name == name {
			return true
		}
	}
	return false
}

// bijection40 is a one-to-one function of the 40 low bits of x: xors and multiplications by odd
// numbers modulo 2^40, which can each be undone.
func bijection40(x uint64) uint64 {
	const mask = 1<<40 - 1
	x &= mask
	x = x * 0x9e3779b97f4a7c15 & mask
	x ^= x >> 20
	x = x * 0xbf58476d1ce4e5b9 & mask
	x ^= x >> 20
	return x
}

// value is the value of the parameter that visit seq of the run has: drawn from the seed and the
// visit, so that the visits of one run, the ones of its warm-up too, have values of their own, and
// every framework is sent the same ones.
func (v landingValue) value(seed uint64, seq int) string {
	n := len(v.Placeholder)
	if v.Bijective {
		if seq >= 1<<40 {
			panic("visit " + strconv.Itoa(seq) + " has no value of its own")
		}
		h := strconv.FormatUint(bijection40(uint64(seq)^seed*0x9e3779b97f4a7c15), 16)
		return strings.Repeat("0", n-len(h)) + h
	}
	var salt uint64
	for _, c := range v.Name {
		salt = salt*131 + uint64(c)
	}
	rng := rand.New(rand.NewPCG(seed^salt, uint64(seq)))
	out := make([]byte, n)
	for i := range out {
		out[i] = v.Alphabet[rng.IntN(len(v.Alphabet))]
	}
	return string(out)
}

// landingSub is what a visit puts in the place of the placeholder of its landing page in every request
// it sends. The zero value puts nothing: a visit that lands with no parameter.
type landingSub struct{ placeholder, value string }

// landingFor is the substitution of visit seq of a run, which plays plan p.
func landingFor(seed uint64, seq int, p Plan) landingSub {
	if len(p.Steps) == 0 {
		return landingSub{}
	}
	v, ok := landingValueOf(landingParam(p.Steps[0].Page))
	if !ok {
		return landingSub{}
	}
	return landingSub{v.Placeholder, v.value(seed, seq)}
}

// apply puts the value in place of the placeholder in s.
func (s landingSub) apply(x string) string {
	if s.placeholder == "" {
		return x
	}
	return strings.ReplaceAll(x, s.placeholder, s.value)
}

// checkLanding says whether the trace of a plan that lands with a parameter has, as the document
// request of its first page view, the very page of the plan: the same path and the same parameters,
// the landing parameter once with the whole placeholder as its value, read as a server reads a query
// (by whole keys and whole values, not by a piece of text). Without it the visit would not be the one
// planned, and the replay has nothing right to put the value of the visit into.
func checkLanding(p Plan, t Trace) error {
	name := landingParam(p.Steps[0].Page)
	if name == "" {
		return nil
	}
	want, err := url.Parse(p.Steps[0].Page)
	if err != nil {
		return fmt.Errorf("plan %d: %w", p.ID, err)
	}
	wantQuery, err := url.ParseQuery(want.RawQuery)
	if err != nil {
		return fmt.Errorf("plan %d: %w", p.ID, err)
	}
	if len(t.Steps) > 0 {
		for _, q := range t.Steps[0].Reqs {
			if q.Class != "doc" {
				continue
			}
			got, err := url.Parse(q.URL)
			if err != nil || got.Path != want.Path {
				continue
			}
			if gotQuery, err := url.ParseQuery(got.RawQuery); err == nil && reflect.DeepEqual(gotQuery, wantQuery) {
				return nil
			}
		}
	}
	return fmt.Errorf("plan %d lands on %s and no document request of its first page view has that path and the landing parameter %s once, with its placeholder as the whole value", p.ID, p.Steps[0].Page, name)
}

// The groups the landing documents of Tifa are counted in (Round.Landing).
const (
	LandingClickID = "click id"
	LandingEmail   = "email parameter"
	LandingNone    = "no parameter"
)

// LandingGroups are the groups of Round.Landing, in the order of the report.
var LandingGroups = []string{LandingClickID, LandingEmail, LandingNone}

// landingGroup is the group of a request URL by the landing parameter it carries.
func landingGroup(url string) string {
	switch name := landingParam(url); {
	case name == "":
		return LandingNone
	case isClickID(name):
		return LandingClickID
	}
	return LandingEmail
}

// isStorePage says whether a URL is a page of the storefront, and not of the back office.
func isStorePage(url string) bool {
	_, kind := StorePageKind(url)
	return kind != ""
}
