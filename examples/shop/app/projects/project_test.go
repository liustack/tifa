package projects

import (
	"errors"
	"testing"

	"github.com/liustack/tifa"
)

// The orders package holds its demonstration data in this test binary, and nothing here ships an
// order, so the counts are those of the demonstration data.
func TestProjectsCountPendingOrders(t *testing.T) {
	list := ListProjects()
	if len(list) != 2 || list[0].ID != "studio" || list[0].Pending != 2 || list[1].Pending != 1 {
		t.Fatalf("projects %+v", list)
	}
	// Pages format times in the zone of the shop, so the server and every browser show the same.
	for _, p := range list {
		if p.TimeZone != "Asia/Shanghai" {
			t.Errorf("%s trades in %q", p.ID, p.TimeZone)
		}
	}
	if _, err := GetProject("nowhere"); !errors.Is(err, tifa.ErrNotFound) {
		t.Fatalf("unknown project: %v", err)
	}
}
