// Package app is the root Layout, the sidebar with the projects and the top bar around every page,
// and sends visitors of the site root to the first project.
package app

import (
	"github.com/liustack/tifa"
	"github.com/liustack/tifa/examples/shop/app/projects"
)

type Shell struct {
	Viewer   Viewer             `json:"viewer"`
	Projects []projects.Project `json:"projects"`
}

type Viewer struct {
	Name     string `json:"name"`
	Initials string `json:"initials"`
}

func Layout(c *tifa.Ctx) (Shell, error) {
	return Shell{Viewer: Viewer{Name: "店主", Initials: "店"}, Projects: projects.ListProjects()}, nil
}
