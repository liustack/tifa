package orders

import (
	"cmp"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/liustack/tifa"
)

// The reference: the reads as they were before the orders kept indexes and totals. Each one goes
// through all the orders, copied, so it has nothing to keep up to date. The tests check that the
// reads of the package give what the reference gives from the same orders.

// snapshot returns copies of all the orders, of every project.
func snapshot() []orderRecord {
	ordersMu.RLock()
	defer ordersMu.RUnlock()
	var list []orderRecord
	for _, p := range orders {
		for _, o := range p.all {
			c := *o
			c.Items, c.Notes = slices.Clone(o.Items), slices.Clone(o.Notes)
			if o.Shipment != nil {
				s := *o.Shipment
				s.Events = slices.Clone(s.Events)
				c.Shipment = &s
			}
			list = append(list, c)
		}
	}
	return list
}

// allRows are the rows of all the orders of a project, newest first.
func allRows(projectID string) []OrderRow {
	rows := []OrderRow{}
	for _, o := range refOrdersOf(snapshot(), projectID) {
		rows = append(rows, orderRow(&o))
	}
	return rows
}

// refOrdersOf returns the orders of a project in all, newest first.
func refOrdersOf(all []orderRecord, projectID string) []orderRecord {
	var list []orderRecord
	for _, o := range all {
		if o.ProjectID == projectID {
			list = append(list, o)
		}
	}
	slices.SortFunc(list, func(a, b orderRecord) int {
		return cmp.Or(b.PlacedAt.Compare(a.PlacedAt), cmp.Compare(b.ID, a.ID))
	})
	return list
}

func refListOrders(all []orderRecord, projectID string, zone *time.Location, q ListQuery) OrderList {
	q = q.clean()
	l := OrderList{Orders: []OrderRow{}, Filter: q, Carriers: []string{}, Daily: DailyCards{RevenueCents: []int{}, Orders: []int{}, Pending: []int{}, Shipped: []int{}}}
	l.Filter.Page = 1
	list := refOrdersOf(all, projectID)
	if len(list) == 0 {
		return l
	}
	l.NewestAt = list[0].PlacedAt
	keys := make([]string, listDays)
	index := map[string]int{}
	for i := range keys {
		keys[i] = l.NewestAt.Add(-time.Duration(listDays-1-i) * 24 * time.Hour).In(zone).Format(time.DateOnly)
		if _, ok := index[keys[i]]; !ok { // a day of 25 hours comes up twice
			index[keys[i]] = len(index)
		}
	}
	sums := make([][4]int, len(index))
	carriers := map[string]bool{}
	for _, o := range list {
		cents := total(o)
		l.Total++
		l.RevenueCents += cents
		if o.Shipment != nil {
			l.Shipped++
			carriers[carrierName(o.Shipment.Carrier)] = true
		} else {
			l.Pending++
			if l.OldestPending == nil || o.PlacedAt.Before(l.OldestPending.PlacedAt) {
				row := orderRow(&o)
				l.OldestPending = &row
			}
		}
		if i, ok := index[o.PlacedAt.In(zone).Format(time.DateOnly)]; ok {
			s := &sums[i]
			s[0] += cents
			s[1]++
			if o.Shipment == nil {
				s[2]++
			} else {
				s[3]++
			}
		}
	}
	for _, key := range keys {
		s := sums[index[key]]
		l.Daily.RevenueCents = append(l.Daily.RevenueCents, s[0])
		l.Daily.Orders = append(l.Daily.Orders, s[1])
		l.Daily.Pending = append(l.Daily.Pending, s[2])
		l.Daily.Shipped = append(l.Daily.Shipped, s[3])
	}
	l.Carriers = append([]string{}, slices.Sorted(maps.Keys(carriers))...)
	keep := q.keeper(l.NewestAt)
	var matched []OrderRow
	for _, o := range list {
		row := orderRow(&o)
		if !keep(row) {
			continue
		}
		l.Statuses.All++
		if row.Status == StatusPending {
			l.Statuses.Pending++
		} else {
			l.Statuses.Shipped++
		}
		if q.Status == "" || q.Status == row.Status {
			matched = append(matched, row)
		}
	}
	switch q.Sort {
	case SortAmount:
		slices.SortStableFunc(matched, func(a, b OrderRow) int {
			if q.Dir == SortDesc {
				return cmp.Compare(b.TotalCents, a.TotalCents)
			}
			return cmp.Compare(a.TotalCents, b.TotalCents)
		})
	case SortPlaced:
		if q.Dir == SortAsc {
			slices.Reverse(matched)
		}
	}
	l.Matching = len(matched)
	l.Pages = (l.Matching + ListPageSize - 1) / ListPageSize
	l.Filter.Page = min(q.Page, max(l.Pages, 1))
	from := (l.Filter.Page - 1) * ListPageSize
	l.Orders = append(l.Orders, matched[min(from, len(matched)):min(from+ListPageSize, len(matched))]...)
	return l
}

func refGetOrder(all []orderRecord, projectID, id string) (Order, error) {
	i := slices.IndexFunc(all, func(o orderRecord) bool { return o.ProjectID == projectID && o.ID == id })
	if i < 0 {
		return Order{}, tifa.ErrNotFound
	}
	o := all[i]
	order := Order{
		ID: o.ID, ProjectID: o.ProjectID, Customer: o.Customer, Initials: initials(o.Customer),
		Phone: o.Phone, Email: o.Email, Address: o.Address, Note: o.Note, PlacedAt: o.PlacedAt, Status: status(o),
		TotalCents: total(o), Items: []LineItem{}, Payment: payment(o), Log: logOf(o), Packing: packing(o),
		Notes: slices.Clone(o.Notes),
	}
	if s := o.Shipment; s != nil {
		order.Shipment = &Shipment{
			Carrier: s.Carrier, CarrierName: carrierName(s.Carrier), TrackingNo: s.TrackingNo, ShippedAt: s.ShippedAt,
			Events: slices.Clone(s.Events),
		}
	}
	for _, item := range o.Items {
		sold, waiting := 0, 0
		for _, other := range all {
			if other.ProjectID != projectID {
				continue
			}
			for _, it := range other.Items {
				if it.Slug == item.Slug {
					sold += it.Quantity
					if other.Shipment == nil {
						waiting += it.Quantity
					}
				}
			}
		}
		order.Items = append(order.Items, LineItem{
			Slug: item.Slug, Name: item.Name, Description: item.Description, Quantity: item.Quantity,
			PriceCents: item.PriceCents, Color: item.Color, Stock: productStock(projectID, item.Slug), Sold: sold, Demand: waiting,
		})
	}
	return order, nil
}

func refCountPending(all []orderRecord, projectID string) int {
	n := 0
	for _, o := range refOrdersOf(all, projectID) {
		if o.Shipment == nil {
			n++
		}
	}
	return n
}

func refCustomerOrders(all []orderRecord, projectID, id string) (others []OrderRow, count, spentCents int) {
	o, err := refGetOrder(all, projectID, id)
	if err != nil {
		return []OrderRow{}, 0, 0
	}
	others = []OrderRow{}
	for _, r := range refOrdersOf(all, projectID) {
		if r.Customer != o.Customer {
			continue
		}
		count++
		spentCents += total(r)
		if r.ID != id && len(others) < SideListSize {
			others = append(others, orderRow(&r))
		}
	}
	return others, count, spentCents
}

func refOrdersWith(all []orderRecord, projectID, slug, exceptID string, n int) []OrderRow {
	rows := []OrderRow{}
	for _, o := range refOrdersOf(all, projectID) {
		if o.ID == exceptID || !slices.ContainsFunc(o.Items, func(i itemRecord) bool { return i.Slug == slug }) {
			continue
		}
		rows = append(rows, orderRow(&o))
		if len(rows) == n {
			break
		}
	}
	return rows
}

func refSalesStats(all []orderRecord, projectID string, zone *time.Location) Sales {
	s := Sales{Days: []DaySales{}, TopProducts: []ProductSales{}, Products: []ProductRow{}, Carriers: refCarrierStats(all, projectID), Customers: []CustomerSales{}, Heat: make([][]int, 7), Dispatch: dispatchBands(), Recent: []OrderRow{}}
	for i := range s.Heat {
		s.Heat[i] = make([]int, 12)
	}
	list := refOrdersOf(all, projectID)
	if len(list) == 0 {
		return s
	}
	last := list[0].PlacedAt.In(zone)
	first := time.Date(last.Year(), last.Month(), last.Day()-(statsDays-1), 0, 0, 0, 0, time.UTC)
	index := map[string]int{}
	for i := range statsDays {
		date := first.AddDate(0, 0, i).Format(time.DateOnly)
		index[date] = i
		s.Days = append(s.Days, DaySales{Date: date})
	}
	products := map[string]*ProductSales{}
	rows := map[string]*ProductRow{}
	customers := map[string]*CustomerSales{}
	var dispatched time.Duration
	for _, o := range list {
		local := o.PlacedAt.In(zone)
		s.Heat[(int(local.Weekday())+6)%7][local.Hour()/2]++
		c := customers[o.Customer]
		if c == nil {
			c = &CustomerSales{Name: o.Customer, Initials: initials(o.Customer)}
			customers[o.Customer] = c
		}
		c.Orders++
		c.SpentCents += total(o)
		if sh := o.Shipment; sh != nil {
			h := sh.ShippedAt.Sub(o.PlacedAt)
			dispatched += h
			s.Dispatch[dispatchBand(h)].Orders++
		} else {
			s.Totals.PendingOrders++
			s.Totals.PendingCents += total(o)
		}
		i, ok := index[local.Format(time.DateOnly)]
		if !ok {
			continue
		}
		d := &s.Days[i]
		d.Orders++
		d.RevenueCents += total(o)
		if o.Shipment != nil {
			d.Shipped++
		}
		counted := map[string]bool{}
		for _, item := range o.Items {
			p := products[item.Slug]
			if p == nil {
				name := productName(projectID, item.Slug)
				p = &ProductSales{Slug: item.Slug, Name: name}
				products[item.Slug] = p
				rows[item.Slug] = &ProductRow{Slug: item.Slug, Name: name, Daily: make([]int, statsDays)}
			}
			p.Quantity += item.Quantity
			p.RevenueCents += item.Quantity * item.PriceCents
			r := rows[item.Slug]
			r.Quantity += item.Quantity
			if !counted[item.Slug] {
				counted[item.Slug] = true
				r.Orders++
			}
			r.RevenueCents += item.Quantity * item.PriceCents
			r.Daily[i] += item.Quantity
			d.Pieces += item.Quantity
			s.Totals.Pieces += item.Quantity
		}
	}
	if shipped := len(list) - s.Totals.PendingOrders; shipped > 0 {
		s.Totals.AvgDispatchHours = dispatched.Hours() / float64(shipped)
	}
	for _, c := range customers {
		s.Customers = append(s.Customers, *c)
		s.Totals.Customers++
		if c.Orders > 1 {
			s.Totals.Repeat++
		}
	}
	slices.SortFunc(s.Customers, func(a, b CustomerSales) int {
		return cmp.Or(cmp.Compare(b.SpentCents, a.SpentCents), cmp.Compare(a.Name, b.Name))
	})
	s.Customers = s.Customers[:min(8, len(s.Customers))]
	for _, r := range rows {
		s.Products = append(s.Products, *r)
	}
	slices.SortFunc(s.Products, func(a, b ProductRow) int {
		return cmp.Or(cmp.Compare(b.Quantity, a.Quantity), cmp.Compare(b.RevenueCents, a.RevenueCents), cmp.Compare(a.Name, b.Name))
	})
	for _, o := range list[:min(8, len(list))] {
		s.Recent = append(s.Recent, orderRow(&o))
	}
	for _, p := range products {
		s.TopProducts = append(s.TopProducts, *p)
	}
	slices.SortFunc(s.TopProducts, func(a, b ProductSales) int {
		return cmp.Or(cmp.Compare(b.Quantity, a.Quantity), cmp.Compare(b.RevenueCents, a.RevenueCents), cmp.Compare(a.Name, b.Name))
	})
	if len(s.TopProducts) > 5 {
		s.TopProducts = s.TopProducts[:5]
	}
	return s
}

func refCarrierStats(all []orderRecord, projectID string) []CarrierSales {
	out := make([]CarrierSales, len(Carriers))
	dispatch := make([]time.Duration, len(Carriers))
	transit := make([]time.Duration, len(Carriers))
	delivered := make([]int, len(Carriers))
	for i, c := range Carriers {
		out[i] = CarrierSales{Code: c.Code, Name: c.Name}
	}
	for _, o := range refOrdersOf(all, projectID) {
		s := o.Shipment
		i := -1
		if s != nil {
			i = slices.IndexFunc(Carriers, func(c Carrier) bool { return c.Code == s.Carrier })
		}
		if i < 0 {
			continue
		}
		out[i].Orders++
		dispatch[i] += s.ShippedAt.Sub(o.PlacedAt)
		if n := len(s.Events); n > 0 && strings.HasPrefix(s.Events[n-1].Note, "Delivered") {
			delivered[i]++
			transit[i] += s.Events[n-1].At.Sub(s.ShippedAt)
		}
	}
	for i := range out {
		if out[i].Orders > 0 {
			out[i].AvgDispatchHours = dispatch[i].Hours() / float64(out[i].Orders)
		}
		if delivered[i] > 0 {
			out[i].AvgTransitHours = transit[i].Hours() / float64(delivered[i])
		}
	}
	return out
}
