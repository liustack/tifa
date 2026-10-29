package compare

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"golang.org/x/net/html"
)

// View is what a reader sees of a page: its title, its text nodes in order, and how many of a few
// elements it has. Script, style and template elements do not count. React writes the content of
// a suspended boundary into a template, which a browser shows once the page runs.
type View struct {
	Title string
	Text  []string
	Count map[string]int
	Bytes int
}

// counted are the elements whose number must be the same. div is left out: TanStack Start has no
// wrapper div around the page, where tifa has #tifa-root.
var counted = []string{"tr", "a", "svg", "button", "li", "time", "img", "table", "h1", "h2", "h3", "nav", "input"}

// ParseView reads the visible text of an HTML page.
func ParseView(page []byte) View {
	v := View{Count: map[string]int{}, Bytes: len(page)}
	for _, name := range counted {
		v.Count[name] = 0
	}
	z := html.NewTokenizer(strings.NewReader(string(page)))
	skip, inTitle := 0, false
	for {
		switch z.Next() {
		case html.ErrorToken:
			return v
		case html.StartTagToken, html.SelfClosingTagToken:
			tok := z.Token()
			if _, ok := v.Count[tok.Data]; ok {
				v.Count[tok.Data]++
			}
			switch tok.Data {
			case "script", "style", "template":
				if tok.Type == html.StartTagToken {
					skip++
				}
			case "title":
				inTitle = true
			}
		case html.EndTagToken:
			switch tok := z.Token(); tok.Data {
			case "script", "style", "template":
				skip--
			case "title":
				inTitle = false
			}
		case html.TextToken:
			text := strings.Join(strings.Fields(z.Token().Data), " ")
			switch {
			case inTitle:
				v.Title += text
			case skip == 0 && text != "":
				v.Text = append(v.Text, text)
			}
		}
	}
}

// Fetch gets the page the way a browser that sends no compression does, so a server that
// compresses by default sends plain HTML like the others.
func Fetch(base string, page Page) ([]byte, error) {
	req, err := http.NewRequest("GET", base+page.URL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept-Encoding", "identity")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("GET %s: %d", page.URL, res.StatusCode)
	}
	if enc := res.Header.Get("Content-Encoding"); enc != "" {
		return nil, fmt.Errorf("GET %s: answered with Content-Encoding %s to Accept-Encoding: identity", page.URL, enc)
	}
	return body, nil
}

// Diff describes how two views differ, or returns "" when they show the same.
func Diff(a, b View) string {
	var out []string
	if a.Title != b.Title {
		out = append(out, fmt.Sprintf("title %q against %q", a.Title, b.Title))
	}
	for _, name := range counted {
		if a.Count[name] != b.Count[name] {
			out = append(out, fmt.Sprintf("<%s> %d against %d", name, a.Count[name], b.Count[name]))
		}
	}
	if len(a.Text) != len(b.Text) {
		out = append(out, fmt.Sprintf("%d text nodes against %d", len(a.Text), len(b.Text)))
	}
	shown := 0
	for i := 0; i < min(len(a.Text), len(b.Text)) && shown < 5; i++ {
		if a.Text[i] != b.Text[i] {
			out = append(out, fmt.Sprintf("text node %d: %q against %q", i, a.Text[i], b.Text[i]))
			shown++
		}
	}
	return strings.Join(out, "\n")
}
