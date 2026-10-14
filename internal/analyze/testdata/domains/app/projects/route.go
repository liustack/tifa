package projects

import "github.com/liustack/tifa"

func Load(c *tifa.Ctx) ([]Project, error) { return ListProjects(), nil }
