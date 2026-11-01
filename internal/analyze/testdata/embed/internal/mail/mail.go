package mail

import "embed"

//go:embed templates/*.html
var templates embed.FS

func Count() int {
	names, _ := templates.ReadDir("templates")
	return len(names)
}
