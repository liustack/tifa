// Package n injects data nested n levels deep: a category tree as some catalogues keep it, each
// category inside its parent.
package n

import (
	"fmt"

	"example.com/extremetest/app/seed"
	"github.com/liustack/tifa"
)

type Node struct {
	Level int    `json:"level"`
	Name  string `json:"name"`
	Child *Node  `json:"child,omitempty"`
}

// Tree is the data of the page. With the tree below it, its JSON text nests Depth objects deep.
type Tree struct {
	Depth int   `json:"depth"`
	Root  *Node `json:"root"`
}

func Load(c *tifa.Ctx) (Tree, error) {
	n, err := seed.Size(c.Param("n"), 2, 9_000)
	if err != nil {
		return Tree{}, err
	}
	t := Tree{Depth: n}
	// The Tree is one level, so the chain below it has n-1.
	var next *Node
	for level := n - 1; level >= 1; level-- {
		next = &Node{Level: level, Name: fmt.Sprintf("分类 %d", level), Child: next}
	}
	t.Root = next
	return t, nil
}
