package dates

import (
	"time"

	"example.com/libstest/app/seed"
	"github.com/liustack/tifa"
)

// Event is one calendar entry. Start is an ISO time without a zone (UTC), Logged is the same
// moment the way a form or a CSV file writes it.
type Event struct {
	ID      int    `json:"id"`
	Title   string `json:"title"`
	Start   string `json:"start"`
	Minutes int    `json:"minutes"`
	Logged  string `json:"logged"`
	Who     string `json:"who"`
}

type Data struct {
	Now    string  `json:"now"`
	Events []Event `json:"events"`
}

// Load returns the fixed "now" of the page and 360 events over four months.
func Load(c *tifa.Ctx) (Data, error) {
	m := seed.New(23)
	events := make([]Event, 360)
	for i := range events {
		month := 5 + m.Int(4)
		day := 1 + m.Int(28)
		hour := 7 + m.Int(11)
		min := seed.Pick(m, []int{0, 15, 30, 45})
		t := time.Date(2026, time.Month(month+1), day, hour, min, 0, 0, time.UTC)
		e := &events[i]
		e.ID = i
		e.Title = m.Words(3)
		e.Start = t.Format("2006-01-02T15:04:05")
		e.Minutes = seed.Pick(m, []int{15, 30, 45, 60, 90, 120, 240})
		e.Logged = t.Format("02/01/2006 15:04")
		e.Who = seed.Pick(m, []string{"Ada", "Lin", "Grace", "Tim", "Joan", "Ken"})
	}
	return Data{Now: "2026-07-21T09:30:00Z", Events: events}, nil
}
