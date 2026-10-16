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
	// SidebarOpen is the state the sidebar was left in. The sidebar component keeps it in the
	// cookie sidebar_state, and the server renders the sidebar that way, so it does not jump
	// after a reload.
	SidebarOpen bool `json:"sidebarOpen"`
}

type Viewer struct {
	Name     string `json:"name"`
	Initials string `json:"initials"`
}

func Layout(c *tifa.Ctx) (Shell, error) {
	open := true
	if cookie, err := c.Cookie("sidebar_state"); err == nil && cookie.Value == "false" {
		open = false
	}
	return Shell{
		Viewer:      Viewer{Name: "店主", Initials: "店"},
		Projects:    projects.ListProjects(),
		SidebarOpen: open,
	}, nil
}
