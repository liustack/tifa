package compare

import (
	"math"
	"math/rand/v2"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// The search words of each language are made from the catalogue by rule: the same lists every time,
// SearchTermsMin to SearchTermsMax words each, every word once, in lower case. Which words find a
// product is what the model of the list says, and a list holds both kinds. The English list is short
// of the minimum from the catalogue alone, and is filled with misspellings that find nothing.
func TestTheSearchTermsAreMadeFromTheCatalogue(t *testing.T) {
	terms := StoreSearchTerms()
	for _, lang := range StoreLangs {
		l := terms[lang]
		all := slices.Concat(l.Found, l.None)
		if len(all) < SearchTermsMin || len(all) > SearchTermsMax {
			t.Errorf("%s: %d words, want %d to %d", lang, len(all), SearchTermsMin, SearchTermsMax)
		}
		t.Logf("%s: %d words, %d find a product and %d find nothing, of which %d are misspellings", lang, len(all), len(l.Found), len(l.None), l.Misspelt)
		seen := map[string]bool{}
		for _, w := range all {
			if seen[w] || w != strings.ToLower(strings.TrimSpace(w)) || w == "" {
				t.Errorf("%s: the word %q is there twice, or not trimmed and in lower case", lang, w)
			}
			seen[w] = true
		}
		for _, w := range l.Found {
			if termFinds(w) == 0 || ModelList(StoreBase(lang)+"/products?q="+encodeQueryPart(w), nil, nil).Total == 0 {
				t.Errorf("%s: %q is among the words that find a product and finds none", lang, w)
			}
		}
		for _, w := range l.None {
			if termFinds(w) != 0 {
				t.Errorf("%s: %q is among the words that find nothing and finds %d products", lang, w, termFinds(w))
			}
		}
		if len(l.Found) < 100 || len(l.None) < 20 {
			t.Errorf("%s: %d words find a product and %d find nothing: too few of one", lang, len(l.Found), len(l.None))
		}
	}
	en, zh := terms["en-US"], terms["zh-CN"]
	// The words come from where the rule says: a word of a name, a tag, a word of a material, a category and
	// a colour in English, and a tag, a name, its last characters, a material, a category and a colour in Chinese.
	for _, w := range []string{"kettle", "pour-over", "coffee", "stainless", "drinkware", "moss"} {
		if !slices.Contains(en.Found, w) && !slices.Contains(en.None, w) {
			t.Errorf("the English words lack %q", w)
		}
	}
	for _, w := range []string{"咖啡", "晨间手冲壶", "手冲壶", "冲壶", "壶", "304 不锈钢", "器皿", "青苔绿"} {
		if !slices.Contains(zh.Found, w) && !slices.Contains(zh.None, w) {
			t.Errorf("the Chinese words lack %q", w)
		}
	}
	for _, w := range slices.Concat(en.Found, en.None) {
		if isChinese(w) || slices.Contains(termStopWords, w) || len(w) < 3 {
			t.Errorf("the English words hold %q", w)
		}
	}
	// The search of the shop does not look at materials, categories or colours.
	if !slices.Contains(en.None, "stainless") || !slices.Contains(en.None, "drinkware") || !slices.Contains(zh.None, "器皿") {
		t.Error("a word of a material or a category is not among the words that find nothing")
	}
	// The rank of a word is the order the catalogue first has it: the first product comes first.
	if en.Found[0] != "morning" || zh.Found[0] != "晨间手冲壶" {
		t.Errorf("the first words are %q and %q, want those of the first product", en.Found[0], zh.Found[0])
	}
	// The English list is filled to the minimum with misspellings, each one letter away from a word that
	// finds a product, and the Chinese list has none.
	if en.Misspelt == 0 || len(en.Found)+len(en.None) != SearchTermsMin || zh.Misspelt != 0 {
		t.Errorf("%d English and %d Chinese misspellings, in lists of %d and %d words", en.Misspelt, zh.Misspelt, len(en.Found)+len(en.None), len(zh.Found)+len(zh.None))
	}
	for _, w := range en.None[len(en.None)-en.Misspelt:] {
		if !slices.ContainsFunc(en.Found, func(word string) bool { return slices.Contains(misspellings(word), w) }) {
			t.Errorf("%q is no misspelling of a word that finds a product", w)
		}
	}
	if got := misspellings("cup"); !slices.Equal(got, []string{"up", "cp", "cu", "ucp", "cpu"}) {
		t.Errorf("the misspellings of cup are %v", got)
	}
}

// A search is of a class by the quota of the mix, exactly, and draws its word within the class by the
// Zipf law. So one search in ten finds nothing, half of those with a word of the visit's own, however
// many of the words of a list find nothing.
func TestTheSearchesFindNothingByTheQuotaAndDrawTheirWordsByZipf(t *testing.T) {
	rng := rand.New(rand.NewPCG(7, 7))
	classes := searchClasses(rng, 200)
	count := map[string]int{}
	for _, c := range classes {
		count[c]++
	}
	if len(classes) != 200 || count[TermFound] != 180 || count[TermNone] != 10 || count[TermUnique] != 10 {
		t.Errorf("200 searches are of the classes %v, want 180 that find a product, 10 with a word that finds nothing and 10 with a word of their own", count)
	}
	if SearchNoResultShare != 0.10 || SearchUniqueShare != 0.05 || SearchZipf != 1.0 {
		t.Errorf("the shares %v and %v and the exponent %v are not the approved ones", SearchNoResultShare, SearchUniqueShare, SearchZipf)
	}
	for _, lang := range StoreLangs {
		terms := StoreSearchTerms()[lang]
		// Many draws of each class: every word is of its class, and the first comes about as often as the
		// Zipf law says, twice as often as the second.
		for class, list := range map[string][]string{TermFound: terms.Found, TermNone: terms.None} {
			const draws = 200_000
			seen := map[string]int{}
			for range draws {
				seen[drawSearchTerm(rng, lang, class)]++
			}
			var h float64
			for k := range list {
				h += 1 / float64(k+1)
			}
			for w := range seen {
				if !slices.Contains(list, w) {
					t.Fatalf("%s: a search of the class %s drew %q, which is not of it", lang, class, w)
				}
			}
			first, second := float64(seen[list[0]])/draws, float64(seen[list[1]])/draws
			if math.Abs(first-1/h) > 0.01 || math.Abs(second-1/h/2) > 0.01 {
				t.Errorf("%s, %s: the first word is drawn %.3f of the time and the second %.3f, want %.3f and %.3f", lang, class, first, second, 1/h, 1/h/2)
			}
			if len(seen) < len(list)*9/10 {
				t.Errorf("%s, %s: %d of %d words drawn in %d draws", lang, class, len(seen), len(list), draws)
			}
		}
		if got := drawSearchTerm(rng, lang, TermUnique); got != SearchPlaceholder {
			t.Errorf("a search with a word of its own holds %q in the plan, want the placeholder", got)
		}
	}
	if len(SearchPlaceholder) != SearchUniqueLength || termFinds(SearchPlaceholder) != 0 {
		t.Errorf("the placeholder %q has not the length of a word, or finds a product", SearchPlaceholder)
	}
}

// The word of a search of the class of its own is made from the seed and the visit: a million visits
// have a million words, every one of the length and the alphabet of the mix, and the model of the
// list finds no product for any of them.
func TestAMillionSearchTermsOfTheirOwnFindNothing(t *testing.T) {
	const visits = 1_000_000
	if a, b := uniqueTerm(1, 5), uniqueTerm(1, 5); a != b || a == uniqueTerm(2, 5) || a == uniqueTerm(1, 6) {
		t.Errorf("the word of a visit is not a function of the seed and the visit: %q %q", a, b)
	}
	workers := runtime.GOMAXPROCS(0)
	words := make([]string, visits)
	var bad atomic.Int64
	var wg sync.WaitGroup
	for w := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for seq := w; seq < visits; seq += workers {
				term := uniqueTerm(1, seq)
				words[seq] = term
				ok := len(term) == SearchUniqueLength && termFinds(term) == 0
				for i := 0; ok && i < len(term); i++ {
					ok = strings.IndexByte(uniqueTermAlphabet, term[i]) >= 0
				}
				if !ok {
					bad.Add(1)
				}
			}
		}()
	}
	wg.Wait()
	if bad.Load() > 0 {
		t.Errorf("%d of %d words find a product, or are not %d letters and digits", bad.Load(), visits, SearchUniqueLength)
	}
	slices.Sort(words)
	if n := len(slices.Compact(words)); n != visits {
		t.Errorf("%d visits have %d words: some share one", visits, n)
	}
}

// A word of a visit's own finds nothing by how it is made, and not because one that found a product
// was drawn again: it starts with a mark that no name and no tag of the catalogue has, in either
// language, and the search of the shop looks at nothing else. The rest is the number of the visit,
// one-to-one. Drawing again is what let two visits share a word: with seed 0, visit 966845150756 drew
// "handmade", which a product has in its name, and was given the word of visit 536705963581.
func TestASearchTermOfAVisitsOwnFindsNothingByHowItIsMade(t *testing.T) {
	// A catalogue with the mark in a name or a tag fails here: give uniqueTermMark other letters.
	for _, r := range storeCatalogue().records {
		for _, text := range slices.Concat(r.Name[:], r.Tags) {
			if strings.Contains(strings.ToLower(text), uniqueTermMark) {
				t.Fatalf("%q of %s has the mark %q of the search words of a visit's own: the mark must be letters no name and no tag has", text, r.Slug, uniqueTermMark)
			}
		}
	}
	if termFinds("handmade") == 0 {
		t.Fatal("the model finds nothing for a word that a product has in its name: the case below shows nothing")
	}
	a, b := uniqueTerm(0, 966845150756), uniqueTerm(0, 536705963581)
	if a == b {
		t.Errorf("visits 966845150756 and 536705963581 of seed 0 both search for %q", a)
	}
	for _, w := range []string{a, b, uniqueTerm(0, 0), uniqueTerm(7, 1<<40-1)} {
		if !strings.HasPrefix(w, uniqueTermMark) || len(w) != SearchUniqueLength || termFinds(w) != 0 || w == SearchPlaceholder {
			t.Errorf("the word %q does not start with the mark, is not %d characters, finds a product or is the placeholder", w, SearchUniqueLength)
		}
	}
	// The placeholder of the plans is no word of a visit: it does not start with the mark.
	if strings.HasPrefix(SearchPlaceholder, uniqueTermMark) {
		t.Errorf("the placeholder %q starts with the mark, so a visit could have it for its word", SearchPlaceholder)
	}
}
