// Package projects is the project domain and the project list page.
package projects

import (
	"github.com/liustack/tifa"

	"example.com/domains"
	"example.com/domains/app/projects/project+/orders"
)

type Project struct {
	ID      string `json:"id"`
	Pending int    `json:"pending"`
}

func ListProjects() []Project {
	_ = domains.DB
	return []Project{{ID: "a", Pending: orders.CountPending("a")}}
}

func GetProject(id string) (Project, error) {
	for _, p := range ListProjects() {
		if p.ID == id {
			return p, nil
		}
	}
	return Project{}, tifa.ErrNotFound
}
