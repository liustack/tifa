package compare

import (
	"strings"

	"golang.org/x/net/html"
)

// attrNames are the attributes ParseAttrs keeps, by element: what a reader follows or sees, or
// what a form holds.
var attrNames = map[string][]string{
	"html":     {"lang"},
	"a":        {"href"},
	"img":      {"src", "srcset", "alt"},
	"source":   {"srcset", "media", "type"},
	"input":    {"type", "name", "value", "checked", "disabled", "placeholder", "autocomplete"},
	"select":   {"name", "disabled"},
	"option":   {"value", "selected"},
	"textarea": {"name", "placeholder"},
	"button":   {"type", "disabled"},
	"form":     {"action", "method"},
	"meta":     {"name", "property", "content", "media"},
	"link":     {"rel", "href"},
}

// ParseAttrs lists the attributes of a page that ParseView does not see, in document order, as
// "element attribute=value". A boolean attribute is "attribute=true" when it is there. Script,
// style and template elements are left out like ParseView leaves them, and so are the stylesheet
// and icon links, whose files are named by a hash. A meta tag is kept with its name or property, content and media.
func ParseAttrs(page []byte) []string {
	var out []string
	z := html.NewTokenizer(strings.NewReader(string(page)))
	skip := 0
	for {
		switch z.Next() {
		case html.ErrorToken:
			return out
		case html.EndTagToken:
			switch tok := z.Token(); tok.Data {
			case "script", "style", "template":
				skip--
			}
		case html.StartTagToken, html.SelfClosingTagToken:
			tok := z.Token()
			switch tok.Data {
			case "script", "style", "template":
				if tok.Type == html.StartTagToken {
					skip++
				}
			}
			names, ok := attrNames[tok.Data]
			if !ok || skip > 0 {
				continue
			}
			have := map[string]string{}
			for _, a := range tok.Attr {
				have[a.Key] = a.Val
			}
			if tok.Data == "meta" && have["name"] == "" && have["property"] == "" || tok.Data == "link" && have["rel"] != "canonical" {
				continue
			}
			for _, name := range names {
				val, there := have[name]
				if !there {
					continue
				}
				switch name {
				case "checked", "disabled", "selected":
					val = "true"
				}
				out = append(out, tok.Data+" "+name+"="+val)
			}
		}
	}
}
