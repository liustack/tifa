package diff

import (
	"strings"

	"example.com/libstest/app/seed"
	"github.com/liustack/tifa"
)

type Toc struct {
	Depth    int  `json:"depth"`
	Numbered bool `json:"numbered"`
}

type Page struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
	Draft bool   `json:"draft"`
}

type Settings struct {
	Title   string   `json:"title"`
	Version int      `json:"version"`
	Authors []string `json:"authors"`
	Toc     Toc      `json:"toc"`
	Pages   []Page   `json:"pages"`
}

// Data holds two versions of a document, one string per line, and two versions of its settings.
type Data struct {
	Before         []string `json:"before"`
	After          []string `json:"after"`
	SettingsBefore Settings `json:"settingsBefore"`
	SettingsAfter  Settings `json:"settingsAfter"`
}

// Load returns a handbook of about 1400 lines before and after an edit.
func Load(c *tifa.Ctx) (Data, error) {
	m := seed.New(113)
	var before, after []string
	for i := 0; i < 1400; i++ {
		prefix := ""
		if i%40 == 0 {
			prefix = "## "
		} else if i%7 == 0 {
			prefix = "- "
		}
		n := 6 + m.Int(10)
		line := prefix + m.Words(n)
		before = append(before, line)
		k := m.R()
		if k < 0.04 {
			continue // deleted
		}
		if k < 0.12 {
			w := strings.Split(line, " ")
			idx := m.Int(len(w))
			w[idx] = m.Words(1)
			if m.R() < 0.5 {
				at := m.Int(len(w))
				w = append(w[:at], append([]string{m.Words(2)}, w[at:]...)...)
			}
			after = append(after, strings.Join(w, " "))
			continue
		}
		if k < 0.15 {
			after = append(after, m.Words(8))
		}
		after = append(after, line)
	}
	sa := Settings{Title: "Handbook", Version: 3, Authors: []string{"ada", "lin"}, Toc: Toc{Depth: 2}, Pages: make([]Page, 40)}
	for i := range sa.Pages {
		p := &sa.Pages[i]
		p.ID = i
		p.Title = m.Words(3)
		p.Draft = m.R() < 0.2
	}
	sb := sa
	sb.Authors = append([]string{}, sa.Authors...)
	sb.Pages = append([]Page{}, sa.Pages...)
	sb.Version = 4
	sb.Authors = append(sb.Authors, "grace")
	sb.Toc.Numbered = true
	for i := range sb.Pages {
		if m.R() < 0.15 {
			sb.Pages[i].Title = m.Words(3)
		}
	}
	return Data{Before: before, After: after, SettingsBefore: sa, SettingsAfter: sb}, nil
}
