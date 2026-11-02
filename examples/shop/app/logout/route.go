// Package logout signs the visitor out of the workbench.
package logout

import "github.com/liustack/tifa"

// POST ends the session and goes to the sign-in page. It is a POST, so another site cannot
// sign a visitor out with a link or an image.
func POST(c *tifa.Ctx) error {
	if err := tifa.ClearSession(c); err != nil {
		return err
	}
	return tifa.Redirect("/login", 303)
}
