package projects

import (
	"errors"
	"testing"

	"github.com/liustack/tifa"
)

func TestProjectsCountPendingOrders(t *testing.T) {
	list := List()
	if len(list) != 2 || list[0].ID != "studio" || list[0].Pending != 2 || list[1].Pending != 1 {
		t.Fatalf("projects %+v", list)
	}
	if _, err := Get("nowhere"); !errors.Is(err, tifa.ErrNotFound) {
		t.Fatalf("unknown project: %v", err)
	}
}
