package compare

import (
	"math/rand/v2"
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
}

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
	{"gclid", placeholderOf("tifa-gclid-", LandingValueLengths["gclid"])},
	{"fbclid", placeholderOf("tifa-fbclid-", LandingValueLengths["fbclid"])},
	{"msclkid", placeholderOf("tifa-msclkid-", LandingValueLengths["msclkid"])},
	{"_kx", placeholderOf("tifa-kx-", LandingValueLengths["_kx"])},
	{"mc_eid", placeholderOf("tifa0mceid", LandingValueLengths["mc_eid"])},
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
