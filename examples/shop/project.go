package shop

import (
	"slices"
	"sync"

	"github.com/liustack/tifa"
)

// A project is one shop of the workspace.
type Project struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Pending     int    `json:"pending"` // orders waiting to be shipped
}

// GetProject returns the project id, or tifa.ErrNotFound.
func GetProject(id string) (Project, error) {
	projectsMu.RLock()
	defer projectsMu.RUnlock()
	for _, p := range projects {
		if p.ID == id {
			return withStats(p), nil
		}
	}
	return Project{}, tifa.ErrNotFound
}

// ListProjects returns all projects.
func ListProjects() []Project {
	projectsMu.RLock()
	list := slices.Clone(projects)
	projectsMu.RUnlock()
	var out []Project
	for _, p := range list {
		out = append(out, withStats(p))
	}
	return out
}

func withStats(p projectRecord) Project {
	return Project{ID: p.ID, Name: p.Name, Description: p.Description, Pending: countPending(p.ID)}
}

// The projects are kept in memory, standing in for the project table of a real database. They
// start with demonstration data.

type projectRecord struct {
	ID          string
	Name        string
	Description string
}

var (
	projectsMu sync.RWMutex
	projects   = demoProjects()
)

func demoProjects() []projectRecord {
	return []projectRecord{
		{ID: "studio", Name: "山海工作室", Description: "为日常生活，挑选一些值得留下的物件。"},
		{ID: "weekend", Name: "周末小店", Description: "把轻松带回家。"},
	}
}
