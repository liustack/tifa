package shadcn

import (
	"example.com/libstest/app/seed"
	"github.com/liustack/tifa"
)

// Row is one section of a proposal in the data table. Reviewer is empty when nobody is assigned.
type Row struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Status   string `json:"status"`
	Target   int    `json:"target"`
	Limit    int    `json:"limit"`
	Reviewer string `json:"reviewer"`
	Done     int    `json:"done"`
}

type Person struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	Role  string `json:"role"`
}

type Note struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

type Data struct {
	Now    string   `json:"now"`
	Rows   []Row    `json:"rows"`
	People []Person `json:"people"`
	Notes  []Note   `json:"notes"`
	Bio    string   `json:"bio"`
	Pages  []string `json:"pages"`
}

// Load returns a dashboard of a project: 60 sections, 12 people and 8 notes.
func Load(c *tifa.Ctx) (Data, error) {
	m := seed.New(5)
	types := []string{"Cover page", "Table of contents", "Narrative", "Technical content", "Legal", "Plain language", "Research", "Visual"}
	status := []string{"Done", "In Process", "Not Started", "Blocked"}
	d := Data{Now: "2026-07-21", Rows: make([]Row, 60), People: make([]Person, 12), Notes: make([]Note, 8)}
	for i := range d.Rows {
		r := &d.Rows[i]
		r.ID = i + 1
		r.Name = m.Words(3)
		r.Type = seed.Pick(m, types)
		r.Status = seed.Pick(m, status)
		r.Target = 5 + m.Int(40)
		r.Limit = 5 + m.Int(40)
		if m.R() >= 0.2 {
			r.Reviewer = m.Words(2)
		}
		r.Done = m.Int(101)
	}
	for i := range d.People {
		d.People[i] = Person{Name: m.Words(2), Email: "member" + string(rune('a'+i)) + "@example.com", Role: seed.Pick(m, []string{"Owner", "Member", "Viewer"})}
	}
	for i := range d.Notes {
		d.Notes[i] = Note{Title: m.Words(4), Body: m.Words(26)}
	}
	d.Bio = m.Words(30)
	for i := 0; i < 14; i++ {
		d.Pages = append(d.Pages, m.Words(2))
	}
	return d, nil
}
