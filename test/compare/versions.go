package compare

import (
	"slices"
	"strings"
)

// The data version of a page of the shop. A page has the version of the things its data is made from, and
// a write changes some of them: a version key names one. "slug:p" is the price of a product, "slug:s" its
// stock and "admin:<shop>" every page of the back office of a shop, which a write to the shop changes as
// a whole (a rough rule: a list, the stats and the page of one order are not all changed by a write that
// touches one order). What a page is made from, in the storefront, is store_deps.json.

// effect is what a write of a visit changes: the version keys, after the page view After. Only the
// writes that change the data of a page are effects: an add to the cart, a coupon and a change of a
// quantity change the cart of the visitor, which no shared page shows.
type effect struct {
	After int
	Kind  string
	Keys  []string
}

func priceKey(slug string) string { return slug + ":" + depPrice }
func stockKey(slug string) string { return slug + ":" + depStock }
func adminKey(shop string) string { return "admin:" + shop }

// versionEffects are the effects of the writes of one visit, in the order the visit sends them. A
// checkout takes the pieces of its cart from the stock of the shop and places an order in it, a create
// does the same for its items, an edit of a product changes the price or the stock of that product (as
// productChange sets it), and a ship changes only the back office: its order was placed before the
// clock, so it took its pieces then.
func versionEffects(seed uint64, v VisitWrites) []effect {
	takes := takesOf(seed, v)
	var out []effect
	for _, w := range sortedWrites(v.Writes) {
		var keys []string
		switch w.Kind {
		case WriteCheckout, WriteCreate:
			for _, t := range takes {
				if t.kind == w.Kind && t.after == w.After {
					keys = append(keys, stockKey(t.slug))
				}
			}
			shop := w.Project
			if w.Kind == WriteCheckout {
				shop = storefrontShop
			}
			keys = append(keys, adminKey(shop))
		case WriteEdit:
			slug, change := productChange(seed, w, func(string) int { return 0 })
			if _, ok := change["priceCents"]; ok {
				keys = append(keys, priceKey(slug))
			} else {
				keys = append(keys, stockKey(slug))
			}
			keys = append(keys, adminKey(w.Project))
		case WriteShip:
			keys = append(keys, adminKey(w.Project))
		default:
			continue
		}
		slices.Sort(keys)
		out = append(out, effect{After: w.After, Kind: w.Kind, Keys: slices.Compact(keys)})
	}
	return out
}

// sortedWrites are the writes of a visit in the order it sends them.
func sortedWrites(ws []PlannedWrite) []PlannedWrite {
	out := slices.Clone(ws)
	slices.SortStableFunc(out, func(a, b PlannedWrite) int { return a.After - b.After })
	return out
}

// pageKeys are the version keys of a page URL: the entries of the file for a page of the storefront, and
// the back office of its shop for a page of the staff. ok is false for a URL the file has no entry for.
func (d *StoreDeps) pageKeys(page string) (keys []string, ok bool) {
	if strings.HasPrefix(page, "/projects/") {
		// A page of one shop shows something of the other (the project list links to it with its counts), so
		// a write to either shop changes the pages of both.
		return []string{adminKey("studio"), adminKey("weekend")}, true
	}
	list, ok := d.Pages[d.RenderURL(page)]
	if !ok {
		return nil, false
	}
	for _, dep := range slices.Concat(d.Common, list) {
		slug, field, has := strings.Cut(dep, ":")
		if slug == allProducts {
			for _, r := range storeCatalogue().records {
				keys = append(keys, priceKey(r.Slug))
			}
			continue
		}
		switch {
		case !has:
			keys = append(keys, priceKey(slug), stockKey(slug))
		case field == depPrice:
			keys = append(keys, priceKey(slug))
		default:
			keys = append(keys, stockKey(slug))
		}
	}
	return keys, true
}
