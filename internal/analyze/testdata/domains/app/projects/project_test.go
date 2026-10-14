package projects

import "testing"

func TestListProjects(t *testing.T) {
	if len(ListProjects()) != 1 {
		t.Fatal("one project")
	}
}
