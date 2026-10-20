package ssr

import (
	"unicode/utf16"
	"unicode/utf8"
)

// EscapeAboveFFFF writes each character above U+FFFF in the JSON text of the data of a route as
// the escapes of its surrogate pair, such as \ud83d\ude00 for 😀. seroval, which writes the text
// into the page, takes JSON.stringify only for a string without surrogates and otherwise goes over
// each code unit, which is much slower in QuickJS: one emoji in the 80 KB of data of a table of
// 1,000 rows made its render 2.3 times as long. JSON is ASCII outside its strings, and in a string
// the escapes stand for the same character, so the text means the same. Bytes that are no UTF-8,
// which a MarshalJSON method may write, stay as they are. A text without such a character comes
// back as it is.
func EscapeAboveFFFF(text []byte) []byte {
	const hex = "0123456789abcdef"
	var out []byte
	done := 0
	for i := 0; i < len(text); {
		// Only the first byte of a character above U+FFFF is 0xF0 or more.
		if text[i] < 0xF0 {
			i++
			continue
		}
		r, size := utf8.DecodeRune(text[i:])
		if size != 4 {
			i++
			continue
		}
		if out == nil {
			out = make([]byte, 0, len(text)+8)
		}
		out = append(out, text[done:i]...)
		hi, lo := utf16.EncodeRune(r)
		for _, u := range [2]rune{hi, lo} {
			out = append(out, '\\', 'u', hex[u>>12&0xF], hex[u>>8&0xF], hex[u>>4&0xF], hex[u&0xF])
		}
		i += size
		done = i
	}
	if out == nil {
		return text
	}
	return append(out, text[done:]...)
}
