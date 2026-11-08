package ssr

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// A Content-Security-Policy lets an inline script run only when the policy lists the hash of its
// content, and the same for an inline style once the policy limits styles. Go cannot tell from the
// HTML which scripts the framework wrote: a component may put the markup of a Load's data into the
// page (tifa.HTML, dangerouslySetInnerHTML), and a script there must not get a hash. So with
// Input.CSP the bundle marks the elements it writes itself, the hydration scripts of the router,
// the scroll restoration script of its ScriptOnce and the scripts and styles of the head
// functions, with the attribute nonce="<the nonce of the render>" right after the tag name.
// RenderPage takes each mark out again and hashes what the element holds. The nonce is drawn
// after the Loads ran, as for the slots (see slots.go), so no data can carry a mark. The page that
// goes out is byte for byte the page without CSP, so its ETag, the render memo and the browser's
// copy do not depend on a nonce, and the hashes, which follow from the page, can go into a header
// that a cached copy keeps.

// Page is a rendered document. Scripts and Styles hold, with Input.CSP, the hashes of the content
// of the elements the bundle marked, as 'sha256-<base64>' sources of a policy, each once, in the
// order of the page.
type Page struct {
	HTML            string
	Scripts, Styles []string
}

// ErrMark is the error of a page whose marks for the hashes of a policy cannot be read.
var ErrMark = errors.New("ssr: the page has a mark for a Content-Security-Policy hash that is not on a script or a style")

// takeMarks returns html without the marks of nonce and the hashes of the elements they mark.
func takeMarks(html, nonce string) (Page, error) {
	mark := ` nonce="` + nonce + `"`
	i, err := nextMark(html, 0, nonce)
	if err != nil {
		return Page{}, err
	}
	if i < 0 {
		return Page{HTML: html}, nil
	}
	var page Page
	var b strings.Builder
	b.Grow(len(html))
	done := 0
	for i >= 0 {
		var closing string
		var list *[]string
		switch {
		case strings.HasSuffix(html[:i], "<script"):
			closing, list = "</script", &page.Scripts
		case strings.HasSuffix(html[:i], "<style"):
			closing, list = "</style", &page.Styles
		default:
			return Page{}, fmt.Errorf("%w: %.60q", ErrMark, html[max(0, i-30):])
		}
		b.WriteString(html[done:i])
		done = i + len(mark)
		_, start, ok := readAttributes(html, done)
		var content string
		if ok {
			content, ok = elementContent(html, start, closing)
		}
		if !ok {
			return Page{}, fmt.Errorf("%w: the element does not end: %.60q", ErrMark, html[max(0, i-30):])
		}
		addHash(list, content)
		if i, err = nextMark(html, done, nonce); err != nil {
			return Page{}, err
		}
		if i < 0 {
			break
		}
	}
	b.WriteString(html[done:])
	page.HTML = b.String()
	return page, nil
}

// nextMark returns where the next mark of nonce starts in html from from on, or -1. It searches for
// the nonce, whose first byte rarely matches the way the space at the start of a mark does in HTML,
// and the nonce is only in marks once the slots are filled.
func nextMark(html string, from int, nonce string) (int, error) {
	j := strings.Index(html[from:], nonce)
	if j < 0 {
		return -1, nil
	}
	j += from
	const before = ` nonce="`
	if j < len(before) || html[j-len(before):j] != before || j+len(nonce) >= len(html) || html[j+len(nonce)] != '"' {
		return 0, fmt.Errorf("%w: the nonce is not in a mark: %.60q", ErrMark, html[max(0, j-30):])
	}
	return j - len(before), nil
}

// InlineHashes returns the hashes of the inline scripts and of the styles of doc, HTML that Go
// wrote, such as the head of the build or a document of its own: each script without src that
// runs, of no JSON type, and each style. It does not do for HTML that holds data, whose scripts
// it would allow.
func InlineHashes(doc string) (scripts, styles []string) {
	for i := 0; i < len(doc); {
		j := strings.IndexByte(doc[i:], '<')
		if j < 0 {
			break
		}
		i += j + 1
		var closing string
		var list *[]string
		switch {
		case tagNamed(doc[i:], "script"):
			closing, list, i = "</script", &scripts, i+len("script")
		case tagNamed(doc[i:], "style"):
			closing, list, i = "</style", &styles, i+len("style")
		default:
			continue
		}
		attrs, start, ok := readAttributes(doc, i)
		if !ok {
			break
		}
		content, ok := elementContent(doc, start, closing)
		if !ok {
			break
		}
		i = start + len(content)
		if closing == "</script" {
			if _, src := attrs["src"]; src || strings.HasSuffix(strings.ToLower(strings.TrimSpace(attrs["type"])), "json") {
				continue
			}
		}
		addHash(list, content)
	}
	return scripts, styles
}

// tagNamed reports whether s starts with the name of a tag, in any case, followed by what may follow
// a tag name.
func tagNamed(s, name string) bool {
	if len(s) <= len(name) || !strings.EqualFold(s[:len(name)], name) {
		return false
	}
	switch s[len(name)] {
	case ' ', '\t', '\n', '\r', '\f', '/', '>':
		return true
	}
	return false
}

// readAttributes reads the attributes of the tag whose name ends at html[i:], by their names in lower
// case, and returns where the content of the element starts.
func readAttributes(html string, i int) (attrs map[string]string, end int, ok bool) {
	attrs = map[string]string{}
	for i < len(html) {
		switch html[i] {
		case '>':
			return attrs, i + 1, true
		case ' ', '\t', '\n', '\r', '\f', '/':
			i++
			continue
		}
		start := i
		for i < len(html) && !strings.ContainsRune(" \t\n\r\f/>=", rune(html[i])) {
			i++
		}
		name := strings.ToLower(html[start:i])
		value := ""
		if i < len(html) && html[i] == '=' {
			i++
			if i < len(html) && (html[i] == '"' || html[i] == '\'') {
				quote := html[i]
				k := strings.IndexByte(html[i+1:], quote)
				if k < 0 {
					return nil, 0, false
				}
				value, i = html[i+1:i+1+k], i+2+k
			} else {
				start := i
				for i < len(html) && !strings.ContainsRune(" \t\n\r\f>", rune(html[i])) {
					i++
				}
				value = html[start:i]
			}
		}
		if _, seen := attrs[name]; !seen {
			attrs[name] = value
		}
	}
	return nil, 0, false
}

// elementContent returns what the element whose content starts at html[start:] holds: up to
// closing, the first </script or </style in any case.
func elementContent(html string, start int, closing string) (string, bool) {
	for j := start; ; {
		k := strings.Index(html[j:], "</")
		if k < 0 {
			return "", false
		}
		j += k
		if len(html)-j >= len(closing) && strings.EqualFold(html[j:j+len(closing)], closing) {
			return html[start:j], true
		}
		j += 2
	}
}

// addHash adds the hash of content to list, unless content is empty or the list has it. The hash
// is of the text the browser reads from the HTML: the parser makes every line break \n and reads NUL
// as U+FFFD inside a script and a style.
func addHash(list *[]string, content string) {
	if content == "" {
		return
	}
	if strings.ContainsAny(content, "\r\x00") {
		content = strings.NewReplacer("\r\n", "\n", "\r", "\n", "\x00", "�").Replace(content)
	}
	sum := sha256.Sum256([]byte(content))
	hash := "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
	if !slices.Contains(*list, hash) {
		*list = append(*list, hash)
	}
}
