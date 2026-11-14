package state

import (
	"strconv"
	"time"

	"example.com/libstest/app/seed"
	"github.com/liustack/tifa"
)

type Meta struct {
	SLA  SLA  `json:"sla"`
	CSAT *int `json:"csat"`
}

type SLA struct {
	Hours int `json:"hours"`
}

type Ticket struct {
	ID      string   `json:"id"`
	Title   string   `json:"title"`
	Agent   string   `json:"agent"`
	Prio    string   `json:"prio"`
	Status  string   `json:"status"`
	Opened  int64    `json:"opened"`
	Replies int      `json:"replies"`
	Tags    []string `json:"tags"`
	Meta    Meta     `json:"meta"`
}

type Event struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Who  string `json:"who"`
	Tag  string `json:"tag"`
}

type Data struct {
	Agents  []string `json:"agents"`
	Tickets []Ticket `json:"tickets"`
	Events  []Event  `json:"events"`
}

var agents = []string{"ada", "lin", "grace", "tim", "joan", "ken", "barbara", "dennis"}

// Load returns 320 tickets and 600 events. The page replays the events through a store.
func Load(c *tifa.Ctx) (Data, error) { return build(), nil }

func build() Data {
	m := seed.New(61)
	tickets := make([]Ticket, 320)
	for i := range tickets {
		t := Ticket{ID: "T" + strconv.Itoa(5000+i)}
		t.Title = m.Words(4)
		t.Agent = seed.Pick(m, agents)
		t.Prio = seed.Pick(m, []string{"low", "normal", "high", "urgent"})
		t.Status = seed.Pick(m, []string{"open", "pending", "solved", "closed"})
		day := m.Int(20)
		hour := m.Int(24)
		t.Opened = time.Date(2026, 7, 1+day, hour, 0, 0, 0, time.UTC).UnixMilli()
		t.Replies = m.Int(12)
		t.Tags = []string{}
		for n := m.Int(4); n > 0; n-- {
			tag := seed.Pick(m, []string{"billing", "bug", "login", "refund", "api", "mobile"})
			if !contains(t.Tags, tag) {
				t.Tags = append(t.Tags, tag)
			}
		}
		t.Meta.SLA.Hours = seed.Pick(m, []int{4, 8, 24, 72})
		if m.R() < 0.5 {
			v := m.Int(6)
			t.Meta.CSAT = &v
		}
		tickets[i] = t
	}
	events := make([]Event, 600)
	for i := range events {
		var e Event
		e.ID = seed.Pick(m, tickets).ID
		e.Type = seed.Pick(m, []string{"reply", "assign", "close", "reopen", "tag"})
		e.Who = seed.Pick(m, agents)
		e.Tag = seed.Pick(m, []string{"vip", "spam", "dup"})
		events[i] = e
	}
	return Data{Agents: agents, Tickets: tickets, Events: events}
}

func contains(a []string, s string) bool {
	for _, v := range a {
		if v == s {
			return true
		}
	}
	return false
}
