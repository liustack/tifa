package scan

import (
	"regexp"
	"strings"
	"unicode"
)

var (
	// listItem starts a list item, whose indented lines below are no code.
	listItem = regexp.MustCompile(`^ {0,3}([-*+]|\d{1,9}[.)])(\s|$)`)
	// fence opens or closes a fenced code block.
	fence = regexp.MustCompile("^ {0,3}(`{3,}|~{3,})")
	// linkDef is a link reference definition, [label]: url "title".
	linkDef = regexp.MustCompile(`^ {0,3}\[[^\]]+\]:\s*\S+.*$`)
	// linkURL is the address part of a link or an image, ](url) or ](url "title").
	linkURL = regexp.MustCompile(`\]\([^)]*\)`)
	// autolink is <https://...> or <a@b.c>.
	autolink = regexp.MustCompile(`<[A-Za-z][A-Za-z0-9+.-]*:[^\s<>]*>|<[^\s<>@]+@[^\s<>]+>`)
	// tag is an HTML tag, its attributes included.
	tag = regexp.MustCompile(`</?[A-Za-z][^<>]*>`)
	// comment is an HTML comment, which may span lines.
	comment = regexp.MustCompile(`(?s)<!--.*?-->`)
)

// countWords counts the words of a Markdown body: each Han, kana or Hangul character is one, and
// so is each other run of letters and digits. Code blocks, fenced and indented, HTML comments and
// tags, and the addresses of links and images do not count.
func countWords(body []byte) int {
	text := comment.ReplaceAllString(string(body), " ")
	var kept strings.Builder
	var fenceMark string
	blankBefore, inList, inCode := true, false, false
	for line := range strings.SplitSeq(text, "\n") {
		line = strings.TrimRight(line, "\r")
		if fenceMark != "" {
			if m := fence.FindStringSubmatch(line); m != nil && m[1][0] == fenceMark[0] && len(m[1]) >= len(fenceMark) && strings.TrimSpace(line[strings.Index(line, m[1])+len(m[1]):]) == "" {
				fenceMark = ""
			}
			continue
		}
		if m := fence.FindStringSubmatch(line); m != nil {
			fenceMark = m[1]
			continue
		}
		blank := strings.TrimSpace(line) == ""
		indented := strings.HasPrefix(line, "    ") || strings.HasPrefix(line, "\t")
		switch {
		case blank:
			blankBefore = true
			continue
		case indented && !inList && (blankBefore || inCode):
			inCode = true
			continue
		case !indented:
			inCode = false
			inList = listItem.MatchString(line)
		}
		blankBefore = false
		if linkDef.MatchString(line) {
			continue
		}
		kept.WriteString(line)
		kept.WriteByte('\n')
	}
	s := kept.String()
	s = linkURL.ReplaceAllString(s, "]")
	s = autolink.ReplaceAllString(s, " ")
	s = tag.ReplaceAllString(s, " ")
	return count(s)
}

// count counts the words of plain text.
func count(s string) int {
	n := 0
	inWord := false
	runes := []rune(s)
	for i, r := range runes {
		switch {
		case unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul):
			n++
			inWord = false
		case unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.Is(unicode.Mn, r):
			if !inWord {
				n++
				inWord = true
			}
		case inWord && (r == '\'' || r == '’') && i+1 < len(runes) && unicode.IsLetter(runes[i+1]):
			// don't is one word
		default:
			inWord = false
		}
	}
	return n
}
