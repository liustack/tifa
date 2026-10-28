// Package n is the files page of a large pull request, which GitHub draws with 56,000 to
// 97,000 elements and, for the largest ones, over 400,000.
package n

import (
	"fmt"
	"math/rand/v2"
	"strings"

	"example.com/extremetest/app/seed"
	"github.com/liustack/tifa"
)

type Line struct {
	Kind string `json:"k"` // "+", "-" or " "
	Old  int    `json:"o"`
	New  int    `json:"n"`
	Text string `json:"t"`
}

type File struct {
	Path      string `json:"path"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Lines     []Line `json:"lines"`
}

type Diff struct {
	Elements int    `json:"elements"`
	Title    string `json:"title"`
	Files    []File `json:"files"`
}

// Each line of a diff is six elements (tr, three td, two span), each file seven more, and the
// page about twenty around them.
const (
	perLine     = 6
	perFile     = 7
	linesInFile = 300
	page        = 20
)

func Load(c *tifa.Ctx) (Diff, error) {
	n, err := seed.Size(c.Param("n"), 100, 2_000_000)
	if err != nil {
		return Diff{}, err
	}
	return Build(n), nil
}

// Build makes a diff of about n elements.
func Build(n int) Diff {
	r := seed.New("dom", n)
	d := Diff{Elements: n, Title: "Convert the order service to modules"}
	left := max(0, n-page)
	for i := 0; left > perFile+perLine; i++ {
		lines := min(linesInFile, (left-perFile)/perLine)
		f := File{Path: path(r, i)}
		old := 1 + r.IntN(400)
		nw := old
		for range lines {
			l := Line{Kind: " ", Text: code(r)}
			switch x := r.IntN(10); {
			case x < 3:
				l.Kind, l.New = "+", nw
				nw++
				f.Additions++
			case x < 5:
				l.Kind, l.Old = "-", old
				old++
				f.Deletions++
			default:
				l.Old, l.New = old, nw
				old++
				nw++
			}
			f.Lines = append(f.Lines, l)
		}
		d.Files = append(d.Files, f)
		left -= perFile + lines*perLine
	}
	return d
}

var dirs = []string{"src/orders", "src/orders/internal", "src/payments", "src/shipping", "src/catalog", "packages/ui/src", "packages/api/src", "scripts", "test/orders"}
var stems = []string{"service", "repository", "handler", "schema", "events", "client", "mapper", "queue", "retry", "money", "address", "invoice"}

func path(r *rand.Rand, i int) string {
	return fmt.Sprintf("%s/%s_%d.ts", seed.Pick(r, dirs), seed.Pick(r, stems), i)
}

var (
	names = []string{"order", "items", "total", "carrier", "address", "invoice", "refund", "status", "customer", "retries", "payload", "result"}
	forms = []string{
		"const %s = await repo.find(%s.id)",
		"if (!%s) throw new NotFoundError('%s')",
		"export function %s(input: %sInput): Promise<void> {",
		"  return { ...%s, updatedAt: clock.now(), %s }",
		"import { %s } from '../%s'",
		"for (const item of %s.items) total += item.price * item.%s",
		"logger.info('%s changed', { id: %s.id })",
		"}",
		"// %s is kept until the %s is shipped",
		"expect(%s).toEqual(%s)",
	}
)

func code(r *rand.Rand) string {
	f := seed.Pick(r, forms)
	switch strings.Count(f, "%s") {
	case 0:
		return f
	case 1:
		return fmt.Sprintf(f, seed.Pick(r, names))
	default:
		return fmt.Sprintf(f, seed.Pick(r, names), seed.Pick(r, names))
	}
}
