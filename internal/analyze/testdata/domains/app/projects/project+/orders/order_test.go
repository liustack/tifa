package orders_test

import (
	"testing"

	"example.com/domains/app/projects"
	"example.com/domains/app/projects/project+/orders"
)

func TestCountPending(t *testing.T) {
	if orders.CountPending("a") != projects.ListProjects()[0].Pending {
		t.Fatal("pending")
	}
}
