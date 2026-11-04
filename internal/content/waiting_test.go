package content

import (
	"slices"
	"testing"
	"time"
)

func TestWaitingListsTheEntriesStillToCome(t *testing.T) {
	now := time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC)
	at9 := time.Date(2026, 11, 20, 9, 0, 0, 0, time.FixedZone("", 8*3600))
	f := File{
		"blog": {
			{ID: "202608/free-search", Lang: "zh-CN", Date: now.AddDate(0, -3, 0)},
			{ID: "202611/launch", Lang: "zh-CN", Date: at9},
			{ID: "202611/release-notes", Lang: "en", Date: at9},
			{ID: "202612/later", Lang: "en", Date: time.Date(2026, 12, 1, 0, 0, 30, 0, time.UTC)},
			{ID: "now", Lang: "en", Date: now},
		},
		"docs":  {{ID: "guide", Date: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)}},
		"notes": {{ID: "a"}},
	}
	want := []string{
		"3 entries of blog wait for their date: zh-CN/202611/launch and en/202611/release-notes at 2026-11-20 09:00 +08:00, en/202612/later at 2026-12-01 00:00:30 +00:00",
		"1 entry of docs waits for its date: guide at 2027-01-01 00:00 +00:00",
	}
	if got := f.Waiting(now); !slices.Equal(got, want) {
		t.Errorf("Waiting =\n%q\nwant\n%q", got, want)
	}
	if got := f.Waiting(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)); got != nil {
		t.Errorf("Waiting after every date = %q", got)
	}
}
