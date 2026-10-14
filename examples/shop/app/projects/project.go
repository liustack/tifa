// Package projects is the project domain and the project workspace Layout, which applies to
// /projects/{projectId} and every page below it.
package projects

import (
	"errors"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/examples/shop/app/projects/orders"
)

type Project struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Pending     int    `json:"pending"` // orders waiting to be shipped
}

// Get returns the project id, or tifa.ErrNotFound.
func Get(id string) (Project, error) {
	p, err := find(id)
	if errors.Is(err, errNoRows) {
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
	for _, p := range all() {
		list = append(list, withStats(p))
	}
	return list
}

func withStats(p record) Project {
	return Project{ID: p.ID, Name: p.Name, Description: p.Description, Pending: orders.CountPending(p.ID)}
}
