// Package hostile loads data made of what could break a page that carries it: the end of a script
// element, the line and paragraph separators, quotes and backslashes, control characters, a
// character above U+FFFF, and text that looks like what the server writes into a page itself.
package hostile

import (
	"strings"

	"github.com/liustack/tifa"
)

// The page is cached, so the HTML cache and the compressed forms of the page hold the data too.
const Cache = true

type Notes struct {
	Script    string   `json:"script"`
	Separator string   `json:"separator"`
	Quotes    string   `json:"quotes"`
	Control   string   `json:"control"`
	Emoji     string   `json:"emoji"`
	Slot      string   `json:"slot"`
	List      []string `json:"list"`
	// Filler makes the text of the data long enough to go into the page as a slot (4,096 code units).
	Filler string `json:"filler"`
}

func Load(c *tifa.Ctx) (Notes, error) {
	return Notes{
		Script:    `</script><script>window.pwned = 1</script><!-- <script>`,
		Separator: "line\xe2\x80\xa8paragraph\xe2\x80\xa9end",
		Quotes:    `he said "hi" \ and 'bye' \" \\`,
		Control:   "tab\tnull\x00bell\x07",
		Emoji:     "grin 😀 and 𝒳",
		Slot:      "~0123456789abcdef0123456789abcdef.0. ~00000000000000000000000000000000.1.",
		List:      []string{"</script>", "\xe2\x80\xa8", "😀"},
		Filler:    strings.Repeat("filler, </p> and \"quotes\" \\ ", 200),
	}, nil
}
