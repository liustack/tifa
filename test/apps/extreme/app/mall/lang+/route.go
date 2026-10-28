// Package lang is the product page of a large marketplace, sized after the product pages of
// Amazon and JD: about 9,000 elements, 120 photos, hundreds of headed modules and around a
// megabyte of data, in Chinese or English.
package lang

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"time"

	"example.com/extremetest/app/seed"
	"github.com/liustack/tifa"
)

type T struct{ Zh, En string }

func (t T) in(lang string) string {
	if lang == "en-US" {
		return t.En
	}
	return t.Zh
}

type Card struct {
	Slug       string  `json:"slug"`
	Title      string  `json:"title"`
	PriceFen   int     `json:"priceFen"`
	ListFen    int     `json:"listFen"`
	Rating     float64 `json:"rating"`
	Reviews    int     `json:"reviews"`
	Badge      string  `json:"badge"` // "", "deal", "best", "new"
	Fast       bool    `json:"fast"`
	Coupon     string  `json:"coupon"`
	Impression string  `json:"impression"`
}

type Color struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Hex  string `json:"hex"`
	Slug string `json:"slug"`
}

type Offer struct {
	Seller    string  `json:"seller"`
	PriceFen  int     `json:"priceFen"`
	Condition string  `json:"condition"`
	Delivery  string  `json:"delivery"`
	Rating    float64 `json:"rating"`
}

type SKU struct {
	ID       string  `json:"id"`
	Color    string  `json:"color"`
	Size     string  `json:"size"`
	PriceFen int     `json:"priceFen"`
	Stock    int     `json:"stock"`
	Ships    string  `json:"ships"`
	Offers   []Offer `json:"offers"`
	History  []int   `json:"history"` // the price of each of the last 180 days
}

type Pair struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type SpecGroup struct {
	Title string `json:"title"`
	Rows  []Pair `json:"rows"`
}

type BrandModule struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Slug  string `json:"slug"`
	View  string `json:"view"`
}

type Carousel struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Sponsored bool   `json:"sponsored"`
	Cards     []Card `json:"cards"`
}

type Review struct {
	ID       string   `json:"id"`
	Author   string   `json:"author"`
	Place    string   `json:"place"`
	Date     string   `json:"date"`
	Rating   int      `json:"rating"`
	Title    string   `json:"title"`
	Body     string   `json:"body"`
	Variant  string   `json:"variant"`
	Verified bool     `json:"verified"`
	Helpful  int      `json:"helpful"`
	Photos   []string `json:"photos"`
}

type Aspect struct {
	Label    string `json:"label"`
	Positive int    `json:"positive"`
	Negative int    `json:"negative"`
}

type Question struct {
	ID       string `json:"id"`
	Question string `json:"question"`
	Answer   string `json:"answer"`
	By       string `json:"by"`
	Date     string `json:"date"`
	Votes    int    `json:"votes"`
	Answers  int    `json:"answers"`
}

type Department struct {
	Name   string     `json:"name"`
	Groups []LinkList `json:"groups"`
}

type LinkList struct {
	Title string   `json:"title"`
	Links []string `json:"links"`
}

type Page struct {
	Lang     string `json:"lang"`
	Other    string `json:"other"`
	Currency string `json:"currency"`
	TimeZone string `json:"timeZone"`
	City     string `json:"city"`

	Departments []Department `json:"departments"`
	Trending    []string     `json:"trending"`
	Breadcrumb  []string     `json:"breadcrumb"`

	Slug         string   `json:"slug"`
	Title        string   `json:"title"`
	Brand        string   `json:"brand"`
	Rating       float64  `json:"rating"`
	RatingCount  int      `json:"ratingCount"`
	Answered     int      `json:"answered"`
	Bought       int      `json:"bought"`
	PriceFen     int      `json:"priceFen"`
	ListFen      int      `json:"listFen"`
	Coupon       string   `json:"coupon"`
	Promotions   []string `json:"promotions"`
	Bullets      []string `json:"bullets"`
	Gallery      []Pair   `json:"gallery"` // slug and view of each photo
	Colors       []Color  `json:"colors"`
	Sizes        []string `json:"sizes"`
	SKUs         []SKU    `json:"skus"`
	Earliest     string   `json:"earliest"`
	Latest       string   `json:"latest"`
	Seller       string   `json:"seller"`
	SellerRating float64  `json:"sellerRating"`
	Plans        []Pair   `json:"plans"`

	Together     []Card        `json:"together"`
	CompareNames []string      `json:"compareNames"`
	Compare      []Card        `json:"compare"`
	CompareRows  [][]string    `json:"compareRows"`
	Specs        []SpecGroup   `json:"specs"`
	BrandModules       []BrandModule `json:"brandModules"`
	Carousels    []Carousel    `json:"carousels"`

	Histogram []int      `json:"histogram"`
	Aspects   []Aspect   `json:"aspects"`
	Reviews   []Review   `json:"reviews"`
	Questions []Question `json:"questions"`
	History   []Card     `json:"history"`
	Footer    []LinkList `json:"footer"`
}

func Load(c *tifa.Ctx) (Page, error) {
	lang := c.Param("lang")
	if lang != "zh-CN" && lang != "en-US" {
		return Page{}, tifa.ErrNotFound
	}
	return Build(lang), nil
}

const (
	carousels      = 12
	cardsEach      = 18
	reviewsLoaded  = 2000
	questionsShown = 40
)

// Build makes the page in lang, the same each time.
func Build(lang string) Page {
	r := seed.New("mall", len(lang))
	in := func(t T) string { return t.in(lang) }
	main := seed.Products[0]
	p := Page{
		Lang: lang, Other: map[string]string{"zh-CN": "en-US", "en-US": "zh-CN"}[lang],
		Currency: "CNY", TimeZone: "Asia/Shanghai", City: in(T{"杭州市西湖区", "Xihu, Hangzhou"}),
		Slug: main.Slug, Brand: in(T{"山海工作室", "Shanhai Studio"}),
		Title: in(T{
			"山海工作室 晨间手冲壶 600ml 304不锈钢鹅颈细口壶 胡桃木手柄 燃气电磁炉通用 咖啡手冲套装 送滤纸与量勺",
			"Shanhai Studio Morning Pour-Over Kettle, 600 ml 304 Stainless Steel Gooseneck with Walnut Handle, for Gas and Induction, Coffee Set with Filters and Scoop",
		}),
		Rating: 4.7, RatingCount: 18432, Answered: 268, Bought: 3000,
		PriceFen: 29800, ListFen: 32800,
		Coupon:   in(T{"领券立减 ¥20", "Save ¥20 with coupon"}),
		Earliest: seed.Base.AddDate(0, 0, 2).Format(time.DateOnly), Latest: seed.Base.AddDate(0, 0, 4).Format(time.DateOnly),
		Seller: in(T{"山海工作室官方旗舰店", "Shanhai Studio Official Store"}), SellerRating: 4.9,
	}
	p.Breadcrumb = []string{in(T{"家居厨具", "Home & Kitchen"}), in(T{"咖啡器具", "Coffee"}), in(T{"手冲壶", "Pour-over kettles"})}
	p.Trending = []string{in(T{"手冲壶", "pour over kettle"}), in(T{"磨豆机", "coffee grinder"}), in(T{"滤杯", "dripper"}), in(T{"保温杯", "tumbler"}), in(T{"羊毛毯", "wool throw"}), in(T{"香薰蜡烛", "candles"})}
	p.Promotions = []string{
		in(T{"满 299 减 30，可跨店", "¥30 off orders over ¥299, across stores"}),
		in(T{"每满 100 返 10 积分", "10 points back for every ¥100"}),
		in(T{"12 期免息，每期 ¥24.83", "12 interest-free payments of ¥24.83"}),
		in(T{"以旧换新最高补贴 ¥50", "Trade in your old kettle for up to ¥50"}),
	}
	for _, b := range bullets {
		p.Bullets = append(p.Bullets, in(b))
	}
	for _, v := range []string{"main", "detail", "scene"} {
		p.Gallery = append(p.Gallery, Pair{main.Slug, v})
	}
	for _, s := range []string{"ceramic-cup", "copper-cups", "glass-carafe", "tea-canister"} {
		p.Gallery = append(p.Gallery, Pair{s, "scene"})
	}
	p.Colors = []Color{
		{"mist", in(T{"雾白", "Mist"}), "#e7e5df", "pour-over-kettle"},
		{"charcoal", in(T{"炭灰", "Charcoal"}), "#3d3d3a", "pour-over-kettle"},
		{"brass", in(T{"黄铜", "Brass"}), "#b39854", "copper-cups"},
		{"moss", in(T{"青苔绿", "Moss"}), "#6f7f5b", "ceramic-cup"},
		{"clay", in(T{"陶土", "Clay"}), "#b76e4c", "stoneware-vase"},
		{"ink", in(T{"墨蓝", "Ink"}), "#2b3440", "insulated-bottle"},
	}
	p.Sizes = []string{"350 ml", "450 ml", "600 ml", "800 ml", "1 L", in(T{"600 ml 套装", "600 ml set"}), in(T{"600 ml 礼盒", "600 ml gift box"}), in(T{"800 ml 套装", "800 ml set"})}
	for _, c := range p.Colors {
		for i, s := range p.Sizes {
			sku := SKU{ID: fmt.Sprintf("PK-%s-%d", strings.ToUpper(c.ID[:3]), i), Color: c.ID, Size: s, PriceFen: 25800 + 2000*i, Stock: r.IntN(80),
				Ships: in(T{"杭州仓发货", "Ships from Hangzhou"})}
			for d := range 180 {
				sku.History = append(sku.History, sku.PriceFen+100*((d*7+i)%13)-600)
			}
			for k := range 4 {
				sku.Offers = append(sku.Offers, Offer{
					Seller: in(sellers[(i+k)%len(sellers)]), PriceFen: sku.PriceFen + 500*k - 300, Condition: in(conditions[k%len(conditions)]),
					Delivery: in(T{fmt.Sprintf("%d 日达", k+1), fmt.Sprintf("Arrives in %d days", k+1)}), Rating: float64(42+r.IntN(8)) / 10,
				})
			}
			p.SKUs = append(p.SKUs, sku)
		}
	}
	p.Plans = []Pair{{in(T{"一年延保", "1-year protection"}), "1900"}, {in(T{"两年延保", "2-year protection"}), "3400"}, {in(T{"三年意外保", "3-year accident protection"}), "5900"}}
	card := func(i int) Card {
		pr := seed.Pick(r, seed.Products)
		c := Card{Slug: pr.Slug, Title: cardTitle(r, pr, lang), PriceFen: pr.PriceFen - 100*r.IntN(30), Rating: pr.Rating, Reviews: 50 + r.IntN(20000),
			Fast: r.IntN(3) > 0, Impression: fmt.Sprintf("pd_rd_i=%s&pd_rd_w=%08x&pf_rd_p=%08x-%04x&pf_rd_r=%08X%08X&pd_rd_wg=%05d&ref=sr_1_%d&content-id=amzn1.sym.%08x&psc=1", pr.Slug, r.Uint32(), r.Uint32(), r.IntN(65536), r.Uint32(), r.Uint32(), r.IntN(99999), i, r.Uint32())}
		if r.IntN(3) == 0 {
			c.ListFen = c.PriceFen + 1000*(1+r.IntN(8))
		}
		c.Badge = []string{"", "", "deal", "best", "new"}[r.IntN(5)]
		if r.IntN(4) == 0 {
			c.Coupon = in(T{"券 ¥10", "¥10 coupon"})
		}
		return c
	}
	for i := range 3 {
		p.Together = append(p.Together, card(i))
	}
	p.CompareNames = []string{in(T{"容量", "Capacity"}), in(T{"材质", "Material"}), in(T{"壶嘴", "Spout"}), in(T{"适用热源", "Heat source"}), in(T{"重量", "Weight"}), in(T{"温度计", "Thermometer"}), in(T{"保修", "Warranty"}), in(T{"颜色", "Colors"}), in(T{"评分", "Rating"}), in(T{"配送", "Delivery"})}
	for i := range 6 {
		p.Compare = append(p.Compare, card(100+i))
		row := make([]string, len(p.CompareNames))
		for j := range row {
			opts := compareValues[j]
			row[j] = in(opts[(i+j)%len(opts)])
		}
		p.CompareRows = append(p.CompareRows, row)
	}
	for _, g := range specGroups {
		sg := SpecGroup{Title: in(g.title)}
		for _, row := range g.rows {
			sg.Rows = append(sg.Rows, Pair{in(row[0]), in(row[1])})
		}
		p.Specs = append(p.Specs, sg)
	}
	for i, m := range brandModules {
		p.BrandModules = append(p.BrandModules, BrandModule{Title: in(m[0]), Body: in(m[1]), Slug: seed.Products[i%len(seed.Products)].Slug, View: []string{"scene", "detail"}[i%2]})
	}
	for i := range carousels {
		cr := Carousel{ID: fmt.Sprintf("rec-%d", i), Title: in(carouselTitles[i%len(carouselTitles)]), Sponsored: i%3 == 1}
		for j := range cardsEach {
			cr.Cards = append(cr.Cards, card(1000*i+j))
		}
		p.Carousels = append(p.Carousels, cr)
	}
	p.Histogram = []int{12102, 3681, 1290, 552, 807}
	for _, a := range aspects {
		p.Aspects = append(p.Aspects, Aspect{in(a), 200 + r.IntN(3000), r.IntN(300)})
	}
	for i := range reviewsLoaded {
		rating := []int{5, 5, 5, 5, 4, 4, 3, 2, 1}[r.IntN(9)]
		rv := Review{
			ID: fmt.Sprintf("R%07d", 3000000+i), Rating: rating, Date: seed.Ago(r, 400), Verified: r.IntN(5) > 0, Helpful: r.IntN(400),
			Title: in(reviewTitles[rating][r.IntN(len(reviewTitles[rating]))]),
			Variant: fmt.Sprintf("%s · %s", seed.Pick(r, p.Colors).Name, seed.Pick(r, p.Sizes)),
		}
		if lang == "en-US" {
			rv.Author, rv.Place = seed.EnName(r), seed.Pick(r, enPlaces)
		} else {
			rv.Author, rv.Place = seed.Name(r), seed.Pick(r, seed.Cities)
		}
		var body []string
		for range 3 + r.IntN(5) {
			body = append(body, in(seed.Pick(r, reviewLines)))
		}
		rv.Body = strings.Join(body, sep(lang))
		if i%9 == 0 {
			rv.Photos = []string{seed.Pick(r, seed.Products).Slug, main.Slug}
		}
		p.Reviews = append(p.Reviews, rv)
	}
	for i := range questionsShown {
		q := questions[i%len(questions)]
		p.Questions = append(p.Questions, Question{ID: fmt.Sprintf("Q%05d", 40000+i), Question: in(q[0]), Answer: in(q[1]), By: in(T{"山海工作室客服", "Shanhai Studio support"}),
			Date: seed.Ago(r, 300), Votes: r.IntN(300), Answers: 1 + r.IntN(12)})
	}
	for i := range 20 {
		p.History = append(p.History, card(5000+i))
	}
	for _, d := range departments {
		dep := Department{Name: in(d)}
		for g := range 4 {
			ll := LinkList{Title: in(groupTitles[g])}
			for k := range 6 {
				pr := seed.Products[(g*6+k+len(dep.Name))%len(seed.Products)]
				ll.Links = append(ll.Links, in(T{pr.Zh, pr.En}))
			}
			dep.Groups = append(dep.Groups, ll)
		}
		p.Departments = append(p.Departments, dep)
	}
	for _, col := range footer {
		ll := LinkList{Title: in(col[0])}
		for _, l := range col[1:] {
			ll.Links = append(ll.Links, in(l))
		}
		p.Footer = append(p.Footer, ll)
	}
	return p
}

func sep(lang string) string {
	if lang == "en-US" {
		return " "
	}
	return ""
}

func cardTitle(r *rand.Rand, pr seed.Product, lang string) string {
	if lang == "en-US" {
		return fmt.Sprintf("%s, %s", pr.En, seed.Pick(r, []string{"Gift Box", "Set of 2", "Everyday Edition", "Studio Batch", "Limited Glaze", "Travel Size"}))
	}
	return fmt.Sprintf("%s %s", pr.Zh, seed.Pick(r, []string{"礼盒装", "两件套", "日常款", "工作室批次", "限定釉色", "便携装"}))
}
