// Package app is the root Layout, the sidebar with the projects and the top bar around the pages
// of the workbench, and sends visitors of the site root to the first project. The storefront
// shows its pages without it (app/store/layout_.tsx), so this does not run there.
package app

import (
	"github.com/liustack/tifa"
	"github.com/liustack/tifa/examples/shop/app/projects"
)

func GET(c *tifa.Ctx) error {
	return tifa.Redirect("/projects/studio", 302)
}

// Shell is what every visitor of the workbench gets alike. The page of an order is cached, so nothing
// about the signed-in user goes here: the name and the avatar come from useSession in the browser.
type Shell struct {
	Projects []projects.Project `json:"projects"`
	// SidebarOpen is the state the sidebar was left in. The sidebar component keeps it in the
	// cookie sidebar_state, and the server renders the sidebar that way, so it does not jump
	// after a reload.
	SidebarOpen bool `json:"sidebarOpen"`
}

func Layout(c *tifa.Ctx) (Shell, error) {
	open := true
	if cookie, err := c.Cookie("sidebar_state"); err == nil && cookie.Value == "false" {
		open = false
	}
	return Shell{
		Projects:    projects.ListProjects(),
		SidebarOpen: open,
	}, nil
}
