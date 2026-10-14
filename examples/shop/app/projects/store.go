package projects

import (
	"errors"
	"slices"
	"sync"
)

// The project store keeps the demonstration projects in memory and stands in for the project
// table of a real database. It starts with demonstration data, and changes are lost when the
// process exits.

var errNoRows = errors.New("projects: no rows")

type record struct {
	ID          string
	Name        string
	Description string
}

var (
	mu       sync.RWMutex
	projects = demo()
)

// demo returns the demonstration projects the store starts with.
func demo() []record {
	return []record{
		{ID: "studio", Name: "山海工作室", Description: "为日常生活，挑选一些值得留下的物件。"},
		{ID: "weekend", Name: "周末小店", Description: "把轻松带回家。"},
	}
}

func all() []record {
	mu.RLock()
	defer mu.RUnlock()
	return slices.Clone(projects)
}

func find(id string) (record, error) {
	mu.RLock()
	defer mu.RUnlock()
	for _, p := range projects {
		if p.ID == id {
			return p, nil
		}
	}
	return record{}, errNoRows
}
