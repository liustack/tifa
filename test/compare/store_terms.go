package compare

import (
	"math"
	"math/rand/v2"
	"slices"
	"strings"
	"sync"
	"unicode"
)

// The words shoppers type into the search box of the storefront. A shop has no log of its searches to
// take them from, so they are made from the catalogue file by rule, the same words whenever the
// catalogue is the same, one list for each language of the storefront:
//
//   - English: the words of the English names of the products, the English tags, the words of the
//     English names of the materials, and the names of the categories and of the colours.
//   - Chinese: the Chinese tags, the Chinese names of the products whole and their last one, two and
//     three characters (a Chinese name ends with what the product is: 手冲壶, 壶), the Chinese names of
//     the materials, and the names of the categories and of the colours.
//
// The search of the shop looks at the names of the products in both languages and at their tags, and
// at nothing else (Search in examples/shop/app/store/products/product.go), so the words of materials,
// categories and colours mostly find nothing, as a word a shopper knows the shop by and the shop does
// not index. Whether a word finds a product is not written down here: the model of the list says
// (termFinds). A list with fewer words than SearchTermsMin is filled with misspellings of its words
// that find a product, each a word with one letter left out or two letters next to each other swapped,
// kept only when it finds nothing.
//
// A search draws its word by its class first and then within the class (drawSearchTerm): the class by
// the quota of the mix (SearchNoResultShare, SearchUniqueShare), the word by a Zipf law of exponent
// SearchZipf over the words of the class, ranked in the order the catalogue first has them. So the
// share of the searches that find nothing is the quota, however many of the words of the list find
// nothing: filling a list with misspellings makes more such words and not more such searches.

// The names of the categories and of the colours of the shop, in Chinese and in English
// (examples/shop/app/store/products/catalogue.go). They are not in the catalogue file.
// TestTheListModelIsTheListOfTheShop checks them against a shop that runs.
var (
	listCategoryNames = map[string][2]string{
		"drinkware": {"器皿", "Drinkware"}, "textiles": {"织物", "Textiles"}, "stationery": {"文具", "Stationery"},
		"fragrance": {"香氛", "Fragrance"}, "outdoor": {"户外", "Outdoor"},
	}
	listColorNames = map[string][2]string{
		"mist": {"雾白", "Mist"}, "moss": {"青苔绿", "Moss"}, "clay": {"陶土", "Clay"}, "oat": {"燕麦", "Oat"},
		"ink": {"墨蓝", "Ink"}, "brass": {"黄铜", "Brass"}, "charcoal": {"炭灰", "Charcoal"},
	}
)

// termStopWords are the English words left out of the list: a shopper does not search for them.
var termStopWords = []string{"and", "the", "of", "for", "with", "from", "on", "in"}

// The classes of a search term.
const (
	TermFound  = "found"  // a word of the list that finds a product
	TermNone   = "none"   // a word of the list that finds nothing
	TermUnique = "unique" // a word no other visit searches for, which finds nothing
)

// SearchTerms are the words of one language: those that find a product and those that find nothing,
// each in the order of its rank, and how many of the second are misspellings made to fill the list.
type SearchTerms struct {
	Found, None []string
	Misspelt    int
}

// termFinds is how many products the search of the shop finds for a term, by the model of the list.
func termFinds(term string) int {
	f := listFilter{Query: term}
	n := 0
	for _, r := range storeCatalogue().records {
		if f.narrows(r, skipNone) {
			n++
		}
	}
	return n
}

// englishWords are the words of an English text as a shopper would type them: in lower case, without
// the punctuation around them, and without the stop words and the words of one or two characters.
func englishWords(text string) []string {
	var out []string
	for _, w := range strings.Fields(strings.ToLower(text)) {
		w = strings.TrimFunc(w, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
		if len([]rune(w)) > 2 && !slices.Contains(termStopWords, w) {
			out = append(out, w)
		}
	}
	return out
}

// isChinese says whether a text has a Chinese character.
func isChinese(text string) bool {
	return strings.ContainsFunc(text, func(r rune) bool { return unicode.Is(unicode.Han, r) })
}

// misspellings are the ways to misspell a word that the lists use, in order: the word with one
// character left out, for each character, then with two characters next to each other swapped, for
// each pair that differs.
func misspellings(word string) []string {
	r := []rune(word)
	var out []string
	for i := range r {
		out = append(out, string(slices.Concat(r[:i], r[i+1:])))
	}
	for i := 0; i+1 < len(r); i++ {
		if r[i] != r[i+1] {
			s := slices.Clone(r)
			s[i], s[i+1] = s[i+1], s[i]
			out = append(out, string(s))
		}
	}
	return out
}

// termSeed is the seed of the misspellings that fill a list. It is a constant: the lists follow the
// catalogue and not the seed of a run.
const termSeed = 0x7465726d73

// StoreSearchTerms are the search words of each language of the storefront, made from the catalogue
// file once.
var StoreSearchTerms = sync.OnceValue(func() map[string]SearchTerms {
	recs := storeCatalogue().records
	lists := map[string][]string{}
	seen := map[string]map[string]bool{"en-US": {}, "zh-CN": {}}
	add := func(lang string, terms ...string) {
		for _, t := range terms {
			if t = strings.TrimSpace(strings.ToLower(t)); t != "" && !seen[lang][t] {
				seen[lang][t] = true
				lists[lang] = append(lists[lang], t)
			}
		}
	}
	// The words of each product, in the order of the catalogue, which is the order of their rank.
	for _, r := range recs {
		add("en-US", englishWords(r.Name[1])...)
		name := []rune(r.Name[0])
		add("zh-CN", r.Name[0])
		for n := 1; n <= 3 && n < len(name); n++ {
			add("zh-CN", string(name[len(name)-n:]))
		}
		for _, tag := range r.Tags {
			if isChinese(tag) {
				add("zh-CN", tag)
			} else {
				add("en-US", tag)
			}
		}
		for _, m := range r.Materials {
			add("zh-CN", m[0])
			add("en-US", englishWords(m[1])...)
		}
	}
	// Then the names the filters of the list have, which a shopper may search for as well.
	for _, id := range listCategories {
		add("zh-CN", listCategoryNames[id][0])
		add("en-US", englishWords(listCategoryNames[id][1])...)
	}
	for _, id := range listColors {
		add("zh-CN", listColorNames[id][0])
		add("en-US", englishWords(listColorNames[id][1])...)
	}
	out := map[string]SearchTerms{}
	for _, lang := range StoreLangs {
		var t SearchTerms
		for _, term := range lists[lang] {
			if termFinds(term) > 0 {
				t.Found = append(t.Found, term)
			} else {
				t.None = append(t.None, term)
			}
		}
		// A list that is short is filled with misspellings that find nothing: of its words that find a
		// product, in turn and again, one misspelling of each a turn, drawn from the seed of the lists.
		rng := rand.New(rand.NewPCG(termSeed, uint64(len(lang))))
		for turn := 0; len(t.Found)+len(t.None) < SearchTermsMin && turn < 100; turn++ {
			for _, word := range t.Found {
				if len(t.Found)+len(t.None) >= SearchTermsMin {
					break
				}
				var free []string
				for _, m := range misspellings(word) {
					if !seen[lang][m] && len([]rune(m)) > 2 && termFinds(m) == 0 {
						free = append(free, m)
					}
				}
				if len(free) > 0 {
					m := free[rng.IntN(len(free))]
					seen[lang][m] = true
					t.None = append(t.None, m)
					t.Misspelt++
				}
			}
		}
		out[lang] = t
	}
	return out
})

// zipfPick draws a number from 0 to n-1 by a Zipf law of exponent s: the number k comes 1/(k+1)^s as
// often as the first.
func zipfPick(rng *rand.Rand, n int, s float64) int {
	var total float64
	for k := 1; k <= n; k++ {
		total += math.Pow(float64(k), -s)
	}
	x := rng.Float64() * total
	for k := 1; k <= n; k++ {
		if x -= math.Pow(float64(k), -s); x < 0 {
			return k - 1
		}
	}
	return n - 1
}

// searchClasses deals the classes of n searches by the quota of the mix: SearchNoResultShare of them
// find nothing, SearchUniqueShare of all with a word of their own and the rest of those with a word of
// the list that finds nothing, and the others search for a word that finds a product. They are in a
// seeded shuffle.
func searchClasses(rng *rand.Rand, n int) []string {
	return quota(rng, n, []string{TermFound, TermNone, TermUnique}, []float64{1 - SearchNoResultShare, SearchNoResultShare - SearchUniqueShare, SearchUniqueShare})
}

// SearchPlaceholder is what a plan and a trace hold in the place of the word of a search of the class
// TermUnique. It has the length of a real one (SearchUniqueLength) and finds nothing, as a real one
// does, so the page a browser records with it is the page of every such search.
var SearchPlaceholder = placeholderOf("tifaq", SearchUniqueLength)

// drawSearchTerm draws the word of a search of a class in a language: a word of the list by the Zipf
// law of SearchZipf over the words of its class, or the placeholder of a word of its own.
func drawSearchTerm(rng *rand.Rand, lang, class string) string {
	terms := StoreSearchTerms()[lang]
	switch class {
	case TermFound:
		return terms.Found[zipfPick(rng, len(terms.Found), SearchZipf)]
	case TermNone:
		return terms.None[zipfPick(rng, len(terms.None), SearchZipf)]
	}
	return SearchPlaceholder
}

// uniqueTermAlphabet is what a word of the class TermUnique is made of. Lower case letters and digits
// are written the same in a query, in a JSON body and in a header, whatever the encoding, so the replay
// can put the word in place of the placeholder as text.
const uniqueTermAlphabet = "abcdefghijklmnopqrstuvwxyz0123456789"

// uniqueTerm is the word that visit seq of a run searches for when its search is of the class
// TermUnique: SearchUniqueLength characters from the seed and the visit, the same for every server.
// It is a one-to-one function of the visit, so no two visits of a run search for the same, and it
// finds nothing: a word that finds a product (one that is in a name or a tag of the catalogue) or is
// the placeholder is drawn again, from the next number, which is the one case where two visits could
// share a word.
func uniqueTerm(seed uint64, seq int) string {
	if seq < 0 || seq >= 1<<40 {
		panic("visit out of range for a search term of its own")
	}
	for x := uint64(seq) ^ seed*0x9e3779b97f4a7c15; ; x += 0x632be59bd9b4e019 {
		n := bijection40(x)
		out := make([]byte, SearchUniqueLength)
		for i := range out {
			out[i] = uniqueTermAlphabet[n%uint64(len(uniqueTermAlphabet))]
			n /= uint64(len(uniqueTermAlphabet))
		}
		if term := string(out); term != SearchPlaceholder && termFinds(term) == 0 {
			return term
		}
	}
}
