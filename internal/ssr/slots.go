package ssr

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
	"unsafe"
)

// A page carries the JSON text of each Load's data to the browser inside a script, as a string of
// the router's hydration data. Writing that string in the VM costs a scan of the text, a copy of
// it with the quotes escaped, and one more copy of the page when it crosses back to Go. So the VM
// writes a short slot in its place (for a text of 4,096 code units or more), and Pool.Render puts the escaped text there after the VM is
// free (FillSlots).
//
// A slot is a ~, the nonce of the render, a dot, the number of the text, and a dot. The ~ is
// there because Go finds the slots by searching for it: a byte that rarely starts anything else in a
// page is found by a fast search, where the first digit of a nonce would match all the time. The
// nonce is 128 random bits that Pool.Render draws for each render and puts into the input, after
// the Loads ran and before anything of the page is written, so no data can hold it: data that
// spells a slot of another nonce stays as it is, and only the slots the bundle wrote are filled. The page the VM
// returns holds the ~ and the nonce only in those slots, and FillSlots fails when one is not a whole
// slot, so a broken slot never reaches a browser.

const (
	nonceBytes = 16
	// slotMark starts every slot.
	slotMark = "~"
)

// NewNonce draws the nonce of one render, 32 hexadecimal digits.
func NewNonce() string {
	var b [nonceBytes]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand does not fail on the platforms tifa runs on, see its documentation.
		panic(fmt.Errorf("ssr: read random bytes: %w", err))
	}
	return hex.EncodeToString(b[:])
}

// ErrSlot is the error of a page whose slots cannot be filled.
var ErrSlot = errors.New("ssr: the page has a data slot that cannot be filled")

// FillSlots returns html with each slot of nonce replaced by the text it numbers, written as the
// inside of a JavaScript string (ScriptString), which is what the VM would have written. An html
// without a slot comes back as it is. It and NewNonce are what Render does around the VM, exported
// so that measurements outside the package can time a render the way Render does it.
func FillSlots(html, nonce string, texts []string) (string, error) {
	mark := slotMark + nonce
	i := strings.Index(html, mark)
	if i < 0 {
		return html, nil
	}
	room := len(html)
	for _, text := range texts {
		room += len(text) + len(text)/8
	}
	out := make([]byte, 0, room)
	done := 0
	for i >= 0 {
		rest := html[i+len(mark):]
		number, size, ok := slotNumber(rest)
		if !ok {
			return "", fmt.Errorf("%w: %.40q follows a nonce", ErrSlot, rest)
		}
		if number >= len(texts) {
			return "", fmt.Errorf("%w: slot %d, but there are %d texts", ErrSlot, number, len(texts))
		}
		out = append(out, html[done:i]...)
		out = appendScriptString(out, texts[number])
		done = i + len(mark) + size
		j := strings.Index(html[done:], mark)
		if j < 0 {
			break
		}
		i = done + j
	}
	out = append(out, html[done:]...)
	// out is not used again, so the page is its bytes and not a copy of them.
	return unsafe.String(unsafe.SliceData(out), len(out)), nil
}

// slotNumber reads ".<digits>." from the start of s and returns the number and the length read.
func slotNumber(s string) (number, size int, ok bool) {
	if len(s) < 3 || s[0] != '.' {
		return 0, 0, false
	}
	i := 1
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		if number > 1<<24 {
			return 0, 0, false
		}
		number = number*10 + int(s[i]-'0')
		i++
	}
	if i == 1 || i >= len(s) || s[i] != '.' {
		return 0, 0, false
	}
	return number, i + 1, true
}

// ScriptString writes text as seroval writes a string into a script: the inside of a JavaScript
// string literal that a script element can hold, where the VM writes the text of a Load's data. It
// escapes the quote and the backslash, the line breaks and tab, backspace and form feed, < as
// \x3C so that no </script or <!-- can end the element, and U+2028 and U+2029. Every other
// character stays as it is, and a byte that is no UTF-8 becomes U+FFFD, one for each byte, as it
// does when a string crosses into the VM.
func ScriptString(text string) string {
	return string(appendScriptString(make([]byte, 0, len(text)+len(text)/8), text))
}

// scriptEscape says, for each byte, whether appendScriptString has to look at it.
var scriptEscape = func() (table [256]bool) {
	for _, c := range []byte{'"', '\\', '\n', '\r', '\b', '\t', '\f', '<', 0xE2} {
		table[c] = true
	}
	return table
}()

// appendScriptString appends the ScriptString of text to out. It copies byte by byte into room it
// made first, since a text of JSON is full of quotes and appending the run between two of them
// costs more than the run does.
func appendScriptString(out []byte, text string) []byte {
	if !utf8.ValidString(text) {
		fixed := make([]byte, 0, len(text)+8)
		for _, r := range text {
			fixed = utf8.AppendRune(fixed, r)
		}
		text = string(fixed)
	}
	start := len(out)
	out = slices.Grow(out, len(text)+len(text)/8+16)
	dst := out[:cap(out)]
	d := start
	for i := 0; i < len(text); i++ {
		c := text[i]
		if !scriptEscape[c] {
			dst[d] = c
			d++
			continue
		}
		// The rest of the text as it is, and the longest escape, must fit.
		if d+len(text)-i+6 > len(dst) {
			grown := make([]byte, 2*len(dst)+len(text)-i+6)
			copy(grown, dst[:d])
			dst = grown
		}
		var escape string
		switch c {
		case '"':
			escape = `\"`
		case '\\':
			escape = `\\`
		case '\n':
			escape = `\n`
		case '\r':
			escape = `\r`
		case '\b':
			escape = `\b`
		case '\t':
			escape = `\t`
		case '\f':
			escape = `\f`
		case '<':
			escape = `\x3C`
		default: // 0xE2: U+2028 and U+2029 are E2 80 A8 and E2 80 A9
			if i+2 < len(text) && text[i+1] == 0x80 && text[i+2] == 0xA8 {
				escape = "\\u2028"
			} else if i+2 < len(text) && text[i+1] == 0x80 && text[i+2] == 0xA9 {
				escape = "\\u2029"
			} else {
				dst[d] = c
				d++
				continue
			}
			i += 2
		}
		d += copy(dst[d:], escape)
	}
	return dst[:d]
}
