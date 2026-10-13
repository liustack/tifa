// Package projects is the project domain and the project workspace Layout, which applies to
// /projects/{projectId} and every page below it.
package projects

import (
	"errors"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/examples/shop/db"
)

type Project struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Pending     int    `json:"pending"` // orders waiting to be shipped
}

// Get returns the project id, or tifa.ErrNotFound.
func Get(id string) (Project, error) {
	p, err := db.FindProject(id)
	if errors.Is(err, db.ErrNoRows) {
		return Project{}, tifa.ErrNotFound
	}
	if err != nil {
		return Project{}, err
	}
	return withStats(p), nil
}

// List returns all projects.
func List() []Project {
	var list []Project
	for _, p := range db.Projects() {
		list = append(list, withStats(p))
	}
	return list
}

func withStats(p db.Project) Project {
	project := Project{ID: p.ID, Name: p.Name, Description: p.Description}
	for _, o := range db.Orders(p.ID) {
		if !o.Shipped {
			project.Pending++
		}
	}
	return project
}
