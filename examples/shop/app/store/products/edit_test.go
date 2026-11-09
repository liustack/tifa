package products

import (
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/examples/shop/app/store/storekit"
)

// resetCatalogue puts the demonstration catalogue back, so each test starts from it.
func resetCatalogue() {
	catalogueMu.Lock()
	defer catalogueMu.Unlock()
	catalogue = demoCatalogue()
}

var kettleEdit = Edit{
	PriceCents: 29900, Stock: 3,
	NameEN: "Dawn Kettle", NameZH: "黎明手冲壶",
	DescriptionEN: "A kettle for the first light.", DescriptionZH: "清晨第一缕光里的手冲壶。",
}

func TestAnEditShowsOnTheProductTheListTheSearchAndTheHomePage(t *testing.T) {
	t.Cleanup(resetCatalogue)
	if err := Update("pour-over-kettle", kettleEdit); err != nil {
		t.Fatal(err)
	}
	for _, l := range []storekit.Locale{zh, en} {
		p, err := GetProduct("pour-over-kettle", l)
		if err != nil {
			t.Fatal(err)
		}
		name, description := T{kettleEdit.NameZH, kettleEdit.NameEN}.In(l), T{kettleEdit.DescriptionZH, kettleEdit.DescriptionEN}.In(l)
		if p.Name != name || p.Description != description || p.PriceCents != storekit.Price(29900, l) || p.Stock != 3 || !p.InStock {
			t.Errorf("%s: product page shows %q %q, %d, stock %d", l.Code, p.Name, p.Description, p.PriceCents, p.Stock)
		}
		list := Search(Filter{}, l).Products
		i := slices.IndexFunc(list, func(c Card) bool { return c.Slug == "pour-over-kettle" })
		if i < 0 || list[i].Name != name || list[i].PriceCents != storekit.Price(29900, l) || list[i].Stock != 3 {
			t.Errorf("%s: the list shows %+v", l.Code, list[i])
		}
		home := Featured(len(catalogue), l)
		if i := slices.IndexFunc(home, func(c Card) bool { return c.Slug == "pour-over-kettle" }); i < 0 || home[i].Name != name {
			t.Errorf("%s: the new products of the home page do not show the new name", l.Code)
		}
	}
	if r := Search(Filter{Query: "dawn"}, zh); r.Total != 1 || r.Products[0].Slug != "pour-over-kettle" {
		t.Errorf("a search for the new English name finds %d", r.Total)
	}
	if r := Search(Filter{Query: "黎明"}, en); r.Total != 1 {
		t.Errorf("a search for the new Chinese name finds %d", r.Total)
	}
	if r := Search(Filter{Query: "morning pour-over"}, en); r.Total != 0 {
		t.Errorf("the old name still finds %d", r.Total)
	}
	// A price band counts the new price: 299 yuan is in the band from 200 to 399.
	if r := Search(Filter{Min: 200, Max: 399, Query: "dawn"}, zh); r.Total != 1 {
		t.Errorf("the price filter does not see the new price")
	}
	e, err := GetEntry("pour-over-kettle")
	if err != nil || e.Edit != kettleEdit || e.Number != 1 || e.Slug != "pour-over-kettle" || e.Category != "Drinkware" {
		t.Errorf("the entry is %+v, %v", e, err)
	}
}

func TestAnEditOfAnUnknownProductIsNotFound(t *testing.T) {
	t.Cleanup(resetCatalogue)
	if err := Update("no-such-product", kettleEdit); !errors.Is(err, tifa.ErrNotFound) {
		t.Errorf("Update: %v", err)
	}
	if _, err := GetEntry("no-such-product"); !errors.Is(err, tifa.ErrNotFound) {
		t.Errorf("GetEntry: %v", err)
	}
}

func TestEntriesListTheCatalogueInItsOrder(t *testing.T) {
	list := Entries()
	if len(list) != len(catalogue) {
		t.Fatalf("%d entries, the catalogue has %d", len(list), len(catalogue))
	}
	for i, e := range list {
		p := catalogue[i]
		if e.Slug != p.slug || e.Number != i+1 || e.PriceCents != p.priceFen || e.Stock != p.stock || e.NameZH != p.name[0] || e.NameEN != p.name[1] || e.DescriptionEN != p.description[1] {
			t.Errorf("entry %d is %+v", i, e)
		}
	}
}

func TestAnEditIsCheckedFieldByField(t *testing.T) {
	if err := kettleEdit.Validate(); err != nil {
		t.Fatalf("a good edit: %v", err)
	}
	bad := Edit{
		PriceCents: 99, Stock: -1, NameEN: " ", NameZH: strings.Repeat("长", 61),
		DescriptionEN: "", DescriptionZH: strings.Repeat("长", 1001),
	}
	fields, ok := bad.Validate().(tifa.Fields)
	if !ok {
		t.Fatalf("Validate: %v", bad.Validate())
	}
	for _, name := range []string{"priceCents", "stock", "nameEn", "nameZh", "descriptionEn", "descriptionZh"} {
		if fields[name] == "" {
			t.Errorf("no error for %s: %v", name, fields)
		}
	}
	if err := (Edit{PriceCents: 10_000_000, Stock: 100_000, NameEN: "a", NameZH: "a", DescriptionEN: "a", DescriptionZH: "a"}).Validate(); err == nil {
		t.Error("a price and a stock over the limits pass")
	}
	at := Edit{PriceCents: 100, Stock: 0, NameEN: strings.Repeat("a", 60), NameZH: "a", DescriptionEN: strings.Repeat("a", 1000), DescriptionZH: "a"}
	if err := at.Validate(); err != nil {
		t.Errorf("the edit at the limits: %v", err)
	}
}

func TestUpdateKeepsTheTextsTrimmed(t *testing.T) {
	t.Cleanup(resetCatalogue)
	e := kettleEdit
	e.NameEN, e.DescriptionZH = "  Dawn Kettle \n", "\t清晨第一缕光里的手冲壶。 "
	if err := Update("pour-over-kettle", e); err != nil {
		t.Fatal(err)
	}
	if got, _ := GetEntry("pour-over-kettle"); got.Edit != kettleEdit {
		t.Errorf("the entry is %+v", got.Edit)
	}
}

func TestStockIsTakenWholeOrNotAtAll(t *testing.T) {
	t.Cleanup(resetCatalogue)
	// oak-tray has 6. Two lines of one product count together.
	if _, err := Buy([]Take{{"oak-tray", "oat", "M", 4}, {"ceramic-cup", "moss", "", 1}, {"oak-tray", "oat", "L", 3}}); !errors.Is(err, ErrNoStock) {
		t.Fatalf("taking 7 of 6: %v", err)
	}
	if e, _ := GetEntry("oak-tray"); e.Stock != 6 {
		t.Errorf("a refused take left %d", e.Stock)
	}
	if e, _ := GetEntry("ceramic-cup"); e.Stock != 58 {
		t.Errorf("a refused take changed another product: %d", e.Stock)
	}
	if _, err := Buy([]Take{{"oak-tray", "oat", "M", 4}, {"oak-tray", "oat", "L", 2}, {"ceramic-cup", "moss", "", 1}}); err != nil {
		t.Fatalf("taking all 6: %v", err)
	}
	if p, _ := GetProduct("oak-tray", en); p.Stock != 0 || p.InStock {
		t.Errorf("after taking all, the page shows %d, in stock %v", p.Stock, p.InStock)
	}
	if e, _ := GetEntry("ceramic-cup"); e.Stock != 57 {
		t.Errorf("ceramic-cup has %d", e.Stock)
	}
	if _, err := Buy([]Take{{"oak-tray", "oat", "M", 1}}); !errors.Is(err, ErrNoStock) {
		t.Errorf("taking from none: %v", err)
	}
	if _, err := Buy([]Take{{"no-such-product", "", "", 1}}); !errors.Is(err, tifa.ErrNotFound) {
		t.Errorf("taking an unknown product: %v", err)
	}
}

// A purchase keeps the products as they were when their stock was taken.
func TestAPurchaseKeepsTheProductsAsTheyWereBought(t *testing.T) {
	t.Cleanup(resetCatalogue)
	bought, err := Buy([]Take{{"linen-apron", "ink", "M", 2}, {"linen-apron", "ink", "L", 1}})
	if err != nil {
		t.Fatal(err)
	}
	if len(bought.Lines) != 2 || bought.Lines[0].Variant != "Ink / M" || bought.Lines[0].Quantity != 2 || bought.Lines[1].Variant != "Ink / L" ||
		bought.Lines[1].Quantity != 1 || bought.Lines[0].PriceFen != 16800 || bought.Lines[0].Name != "Cotton Linen Apron" {
		t.Errorf("the lines are %+v", bought.Lines)
	}
	e, _ := GetEntry("linen-apron")
	e.PriceCents, e.NameZH = 20000, "新围裙"
	if err := Update("linen-apron", e.Edit); err != nil {
		t.Fatal(err)
	}
	if p, err := bought.Product("linen-apron", zh); err != nil || p.PriceCents != 16800 || p.Name != "棉麻围裙" || p.Stock != 34-3 {
		t.Errorf("after an edit the purchase has %+v, %v", p, err)
	}
	if _, err := bought.Product("oak-tray", zh); !errors.Is(err, tifa.ErrNotFound) {
		t.Errorf("a product it did not buy: %v", err)
	}
	if _, err := Buy([]Take{{"linen-apron", "ink", "XXL", 1}}); err == nil {
		t.Error("a size the apron does not come in passes")
	}
	if e, _ := GetEntry("linen-apron"); e.Stock != 34-3 {
		t.Errorf("a refused purchase took stock: %d", e.Stock)
	}
}

func TestTakesAtOnceNeverSellMoreThanTheStock(t *testing.T) {
	t.Cleanup(resetCatalogue)
	var wg sync.WaitGroup
	var mu sync.Mutex
	sold := 0
	for range 20 {
		wg.Go(func() {
			if _, err := Buy([]Take{{"oak-tray", "oat", "M", 1}}); err == nil {
				mu.Lock()
				sold++
				mu.Unlock()
			}
			_, _ = GetProduct("oak-tray", zh)
		})
	}
	wg.Wait()
	if e, _ := GetEntry("oak-tray"); sold != 6 || e.Stock != 0 {
		t.Errorf("sold %d, %d left", sold, e.Stock)
	}
}
