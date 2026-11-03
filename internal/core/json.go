package core

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
)

// JSONWrite holds the options of every JSON tifa writes from a value of the app: the data of Load
// and Layout, the result of a method handler, c.JSON and the session. It is json v2 with its
// defaults (a nil slice is [], a nil map is {}, no HTML escapes) and two changes:
//
//   - Deterministic sorts map keys, so the same value always gives the same text. The render
//     cache, the ETag of a page and the whole-page cache rely on it.
//   - AllowInvalidUTF8 writes each byte of a string that is no UTF-8 as U+FFFD, as the VM and the
//     browser read such a string, instead of failing. Otherwise one bad string a visitor sent in a
//     form would make every page that shows it fail.
//
// Reading JSON from a request or the session store uses the defaults of json v2 (UnmarshalJSON).
var JSONWrite = json.JoinOptions(json.Deterministic(true), jsontext.AllowInvalidUTF8(true))

// MarshalJSON writes v as JSON with JSONWrite.
func MarshalJSON(v any) ([]byte, error) { return json.Marshal(v, JSONWrite) }

// UnmarshalJSON reads JSON the app sent or stored into v, with the defaults of json v2: names must
// match exactly, a name may appear once in an object, and a string must be UTF-8.
func UnmarshalJSON(data []byte, v any) error { return json.Unmarshal(data, v) }
