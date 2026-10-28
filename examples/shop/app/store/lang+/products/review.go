package products

import (
	"slices"
	"time"

	"github.com/liustack/tifa/examples/shop/app/store"
)

// Review is what a customer wrote about a product. PostedAt is an instant that pages format in
// the time zone of the locale.
type Review struct {
	Author   string    `json:"author"`
	Rating   int       `json:"rating"`
	Title    string    `json:"title"`
	Body     string    `json:"body"`
	PostedAt time.Time `json:"postedAt"`
	Verified bool      `json:"verified"` // bought in this shop
	Helpful  int       `json:"helpful"`  // how many found it helpful
	// Photos are the views of the product the customer took pictures of: "detail" or "scene".
	Photos []string `json:"photos"`
	// Reply is what the shop answered, empty when it did not.
	Reply string `json:"reply"`
}

// RatingCount is how many reviews gave a number of stars, for the bars above the reviews.
type RatingCount struct {
	Stars int `json:"stars"`
	Count int `json:"count"`
}

// ReviewsPerPage is how many reviews a page of a product's reviews holds.
const ReviewsPerPage = 10

// ReviewTag is something customers say about a product, with how many reviews say it.
type ReviewTag struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// ReviewsPage is one page of the reviews of a product, newest first, with how all of its reviews
// spread over the stars and what they most often mention.
type ReviewsPage struct {
	Reviews []Review      `json:"reviews"`
	Ratings []RatingCount `json:"ratings"`
	Tags    []ReviewTag   `json:"tags"`
	Total   int           `json:"total"`
	Page    int           `json:"page"`
	Pages   int           `json:"pages"`
}

func productBySlug(slug string) productRecord {
	for _, r := range catalogue {
		if r.slug == slug {
			return r
		}
	}
	return productRecord{}
}

// ReviewPage returns page of the reviews of a product, from 1. A page past the last shows the last.
func ReviewPage(slug string, page int, l store.Locale) ReviewsPage {
	p := productBySlug(slug)
	total := reviewCount(p)
	out := ReviewsPage{Total: total, Pages: (total + ReviewsPerPage - 1) / ReviewsPerPage}
	out.Page = min(max(page, 1), out.Pages)
	for i := (out.Page - 1) * ReviewsPerPage; i < min(out.Page*ReviewsPerPage, total); i++ {
		out.Reviews = append(out.Reviews, reviewAt(p, i, l))
	}
	five := total * int(p.rating*10-40) / 10
	four := (total - five) * 2 / 3
	three := (total - five - four) * 2 / 3
	two := (total - five - four - three) / 2
	out.Ratings = []RatingCount{{5, five}, {4, four}, {3, three}, {2, two}, {1, total - five - four - three - two}}
	for i, tag := range reviewTags {
		out.Tags = append(out.Tags, ReviewTag{Name: tag.In(l), Count: total * (len(reviewTags) - i) / (len(reviewTags) + 2)})
	}
	return out
}

// Reviews returns the n newest reviews of a product.
func Reviews(slug string, n int, l store.Locale) []Review {
	p := productBySlug(slug)
	var list []Review
	for i := range min(n, reviewCount(p)) {
		list = append(list, reviewAt(p, i, l))
	}
	return list
}

// reviewAt writes the ith newest review of a product. Reviews follow the days since it was added
// at a steady pace, the newest 75 days after it went on sale.
func reviewAt(p productRecord, i int, l store.Locale) Review {
	t := reviewTexts[(i+len(p.slug))%len(reviewTexts)]
	stars := 5 - (i*7+len(p.slug))%3/2
	if p.rating < 4.5 && i%3 == 2 {
		stars = 3
	}
	return Review{
		Author:   reviewers[(i*5+len(p.slug))%len(reviewers)].In(l),
		Rating:   stars,
		Title:    t.title.In(l),
		Body:     t.body.In(l),
		PostedAt: p.added.Add(75*24*time.Hour - time.Duration(i)*(20*time.Hour+97*time.Minute)),
		Verified: i%4 != 3,
		Helpful:  (i*13 + len(p.slug)) % 17,
		Photos:   reviewPhotos(i),
		Reply:    shopReply(i, l),
	}
}

// shopReply is what the shop wrote back to a review: to every fifth, and to a thank you.
func shopReply(i int, l store.Locale) string {
	if i%5 != 1 {
		return ""
	}
	return T{"谢谢你的反馈，我们会转给做这件器物的工坊。有任何问题随时联系客服。", "Thank you for the feedback. We will pass it on to the workshop that made it. Write to us any time."}.In(l)
}

// reviewTags are what reviews mention, the most mentioned first.
var reviewTags = []T{
	{"做工扎实", "Well made"}, {"颜色好看", "Lovely colour"}, {"尺寸合适", "Just the right size"},
	{"包装用心", "Thoughtful packaging"}, {"适合送礼", "Good as a gift"}, {"物流较快", "Quick delivery"}, {"性价比高", "Good value"},
}

// reviewPhotos returns the views the ith review has pictures of: every third review has one or two.
func reviewPhotos(i int) []string {
	switch i % 6 {
	case 0:
		return []string{"scene", "detail"}
	case 3:
		return []string{"scene"}
	}
	return []string{}
}

// ReviewCard is a review with the product it is about, for the reviews of the home page.
type ReviewCard struct {
	Review
	Product Card `json:"product"`
}

// LatestReviews returns the newest review with pictures of each of the n newest products.
func LatestReviews(n int, l store.Locale) []ReviewCard {
	list := slices.Clone(catalogue)
	slices.SortStableFunc(list, func(a, b productRecord) int { return b.added.Compare(a.added) })
	var out []ReviewCard
	for _, p := range list[:min(n, len(list))] {
		reviews := Reviews(p.slug, 1, l)
		out = append(out, ReviewCard{Review: reviews[0], Product: p.card(l)})
	}
	return out
}

func reviewCount(p productRecord) int { return 9 + len(p.slug)*3 + int(p.rating*10)%7 }

var reviewers = []T{
	{"林夏", "Lin Xia"}, {"陈默", "Mo Chen"}, {"周宁", "Ning Zhou"}, {"许知", "Zhi Xu"},
	{"苏禾", "He Su"}, {"何安", "An He"}, {"沈星", "Xing Shen"}, {"江屿", "Yu Jiang"},
	{"Emma W.", "Emma W."}, {"Noah K.", "Noah K."}, {"Olivia R.", "Olivia R."}, {"Liam P.", "Liam P."},
}

var reviewTexts = []struct{ title, body T }{
	{T{"比想象中更好", "Better than I expected"}, T{
		"收到后第一感觉是做工很扎实，细节处理得很干净。用了两周，每天都在用，没有任何问题。包装也很用心，送人也合适。",
		"The first thing I noticed was how solid it feels, and how clean the details are. Two weeks of daily use and no trouble at all. The packaging is lovely too, good enough for a gift.",
	}},
	{T{"颜色很温柔", "Such a gentle colour"}, T{
		"实物颜色比图片更柔和一点，放在家里很协调。唯一的小遗憾是物流慢了一天，不过客服提前通知了。",
		"The colour is a touch softer than in the photos and fits right in at home. My only gripe is that delivery was a day late, but the shop told me ahead of time.",
	}},
	{T{"已经回购第二次", "My second one already"}, T{
		"第一次买给自己，这次买给妈妈。她很喜欢，说用起来顺手。希望以后能出更多颜色。",
		"I bought the first for myself and this one for my mother. She loves it and says it is easy to use. I hope more colours come out.",
	}},
	{T{"性价比不错", "Good value"}, T{
		"价格不算便宜，但用料和手感对得起这个价。和之前在别家买的相比，明显更耐用。",
		"Not cheap, but the materials and the feel are worth it. It has clearly held up better than the one I bought elsewhere.",
	}},
	{T{"有一点小瑕疵", "A small flaw"}, T{
		"整体满意，但边缘有一处很小的痕迹。联系客服后很快就给了补偿，处理态度很好。",
		"Happy overall, but there was a tiny mark on one edge. The shop made it right quickly and was very kind about it.",
	}},
	{T{"每天都离不开", "I use it every day"}, T{
		"已经成了我早晨的固定流程。尺寸刚好，不占地方。清洗也方便，几秒钟就好。",
		"It is part of my morning now. Just the right size and it takes no room. Easy to clean, too: a few seconds and done.",
	}},
	{T{"送礼很体面", "A gift that looks the part"}, T{
		"买来送同事，礼盒很精致，拿出手很体面。同事说质感很好，问我在哪里买的。",
		"I gave it to a colleague. The box is beautiful and it looked great. She said it feels lovely and asked where I got it.",
	}},
}
