package intl

import (
	"example.com/libstest/app/seed"
	"github.com/liustack/tifa"
)

// Stay is one result of a hotel search. Times are milliseconds (UTC). Updated is minutes ago, as
// a negative number.
type Stay struct {
	ID        int      `json:"id"`
	Name      string   `json:"name"`
	Score     float64  `json:"score"`
	Reviews   int      `json:"reviews"`
	Rooms     int      `json:"rooms"`
	Km        float64  `json:"km"`
	PerNight  float64  `json:"perNight"`
	Nights    int      `json:"nights"`
	Guests    int      `json:"guests"`
	FreeUntil int64    `json:"freeUntil"`
	From      int64    `json:"from"`
	Amenities []string `json:"amenities"`
	Country   string   `json:"country"`
	Currency  string   `json:"currency"`
	Updated   int      `json:"updated"`
}

type Data struct {
	Now   int64  `json:"now"`
	Stays []Stay `json:"stays"`
}

const now = 1784592000000 // 2026-07-21T00:00:00Z

// Load returns 48 stays, which the page shows in English, German and Polish.
func Load(c *tifa.Ctx) (Data, error) {
	m := seed.New(31)
	d := Data{Now: now}
	for i := 0; i < 48; i++ {
		s := Stay{ID: i, Name: m.Words(3)}
		s.Score = 6 + m.R()*4
		s.Reviews = m.Int(3000)
		s.Rooms = 1 + m.Int(9)
		s.Km = m.R() * 12
		s.PerNight = 40 + m.R()*400
		s.Nights = 1 + m.Int(14)
		s.Guests = 1 + m.Int(5)
		s.FreeUntil = now + int64(m.Int(30))*86400000 + int64(m.Int(1440))*60000
		s.From = now + int64(m.Int(60))*86400000
		for n := 2 + m.Int(4); n > 0; n-- {
			s.Amenities = append(s.Amenities, seed.Pick(m, []string{"wifi", "pool", "parking", "breakfast", "gym", "spa"}))
		}
		s.Country = seed.Pick(m, []string{"DE", "FR", "PL", "JP", "BR", "US", "IT"})
		s.Currency = seed.Pick(m, []string{"EUR", "USD", "JPY", "PLN", "GBP"})
		s.Updated = -1 - m.Int(500)
		d.Stays = append(d.Stays, s)
	}
	return d, nil
}
