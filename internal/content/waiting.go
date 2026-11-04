package content

import (
	"fmt"
	"maps"
	"path"
	"slices"
	"strings"
	"time"
)

// Waiting says, for each collection of f in the order of its name, which entries wait for a date
// after now, such as "2 entries of blog wait for their date: zh-CN/202611/launch and
// en/202611/release-notes at 2026-11-20 09:00 +08:00". tifa build prints it, so the dates can be
// checked before the program ships.
func (f File) Waiting(now time.Time) []string {
	var lines []string
	for _, name := range slices.Sorted(maps.Keys(f)) {
		var waiting []Record
		for _, r := range f[name] {
			if r.Date.After(now) {
				waiting = append(waiting, r)
			}
		}
		if len(waiting) == 0 {
			continue
		}
		slices.SortStableFunc(waiting, func(a, b Record) int { return a.Date.Compare(b.Date) })
		var groups []string
		for i := 0; i < len(waiting); {
			j := i
			var names []string
			for ; j < len(waiting) && waiting[j].Date.Equal(waiting[i].Date); j++ {
				names = append(names, path.Join(waiting[j].Lang, waiting[j].ID))
			}
			groups = append(groups, joinAnd(names)+" at "+FormatDate(waiting[i].Date))
			i = j
		}
		noun := "entries of %s wait for their date"
		if len(waiting) == 1 {
			noun = "entry of %s waits for its date"
		}
		lines = append(lines, fmt.Sprintf("%d "+noun+": %s", len(waiting), name, strings.Join(groups, ", ")))
	}
	return lines
}
