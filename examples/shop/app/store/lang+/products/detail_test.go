package products

import (
	"slices"
	"testing"
)

func TestReviewPagesHoldTenAndRunOutOldest(t *testing.T) {
	first := ReviewPage("pour-over-kettle", 1, zh)
	if first.Total != 63 || first.Pages != 7 || len(first.Reviews) != 10 || first.Page != 1 {
		t.Fatalf("first page: %d of %d on %d pages, %d reviews", first.Page, first.Total, first.Pages, len(first.Reviews))
	}
	// A page past the last shows the last, which holds what is left.
	last := ReviewPage("pour-over-kettle", 99, zh)
	if last.Page != 7 || len(last.Reviews) != 3 {
		t.Fatalf("last page: %d with %d reviews", last.Page, len(last.Reviews))
	}
	// Newest first across the pages, and never before the product was added.
	var all []Review
	for p := 1; p <= first.Pages; p++ {
		all = append(all, ReviewPage("pour-over-kettle", p, zh).Reviews...)
	}
	added := catalogue[0].added
	for i, r := range all {
		if i > 0 && !r.PostedAt.Before(all[i-1].PostedAt) {
			t.Errorf("review %d is not older than review %d", i, i-1)
		}
		if r.PostedAt.Before(added) {
			t.Errorf("review %d is dated %s, before the product was added", i, r.PostedAt)
		}
	}
	if sum := 0; len(first.Ratings) != 5 {
		t.Errorf("%d rating bars", len(first.Ratings))
	} else {
		for _, r := range first.Ratings {
			sum += r.Count
		}
		if sum != first.Total {
			t.Errorf("the bars add up to %d of %d", sum, first.Total)
		}
	}
	if ReviewPage("pour-over-kettle", 0, zh).Page != 1 || ReviewPage("pour-over-kettle", -4, zh).Page != 1 {
		t.Error("a page below 1 is not the first")
	}
	if len(first.Tags) < 5 || first.Tags[0].Count < first.Tags[len(first.Tags)-1].Count {
		t.Errorf("tags %+v", first.Tags)
	}
}

func TestEveryCategoryHasSevenQuestions(t *testing.T) {
	for _, c := range categories {
		if n := len(questions[c.id]); n != 7 {
			t.Errorf("%s has %d questions", c.id, n)
		}
	}
}

func TestSpecGroupsListWhatABuyerChecks(t *testing.T) {
	for _, l := range []string{"zh-CN", "en-US"} {
		loc := zh
		if l == "en-US" {
			loc = en
		}
		p, _ := GetProduct("pour-over-kettle", loc)
		if len(p.SpecGroups) != 3 {
			t.Fatalf("%s: %d groups", l, len(p.SpecGroups))
		}
		rows := 0
		for _, g := range p.SpecGroups {
			if g.Title == "" || len(g.Rows) < 3 {
				t.Errorf("%s: group %q has %d rows", l, g.Title, len(g.Rows))
			}
			rows += len(g.Rows)
		}
		// The kettle's own specs come first, and colours are named in the language.
		if rows < 12 || p.SpecGroups[0].Rows[0].Value != p.Specs[0].Value {
			t.Errorf("%s: %d rows, first %+v", l, rows, p.SpecGroups[0].Rows[0])
		}
	}
}

func TestBestInCategoryExcludesTheProduct(t *testing.T) {
	best := BestInCategory("pour-over-kettle", 5, zh)
	if len(best) != 5 || slices.ContainsFunc(best, func(c Card) bool { return c.Slug == "pour-over-kettle" || c.Category != best[0].Category }) {
		t.Fatalf("best %+v", best)
	}
	if best[0].Rating < best[4].Rating {
		t.Error("not best first")
	}
}
