package hostile

import (
	"strings"

	"github.com/liustack/tifa"
)

// Banner is the data of the Layout above /hostile and /hostile/gone. Its text is long enough to go
// into a page as a slot, so the page of a Load that failed below it, the error page, carries the
// data of a Layout as a slot too.
type Banner struct {
	Title string `json:"title"`
	Text  string `json:"text"`
}

func Layout(c *tifa.Ctx) (Banner, error) {
	return Banner{
		Title: "</script><b>banner</b>",
		Text:  strings.Repeat("banner, </script> \"quotes\" \\ and \xe2\x80\xa8 ", 150),
	}, nil
}
