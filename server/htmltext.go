package server

import (
	"encoding/json/jsontext"
	"encoding/json/v2"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/internal/core"
	"github.com/liustack/tifa/internal/ssr"
)

// htmlMarker is what the hydration data of a page the server renders holds in place of a tifa.HTML
// value: the page has the markup already, and <HTML> reads it back from there when the browser
// hydrates the page (js/runtime/html.tsx).
const htmlMarker = `{"$tifa":"html"}`

// pageOptions write the page text: the options of every JSON tifa writes (core.JSONWrite), so that
// the page text differs from the text the render reads only where a tifa.HTML value is, and a
// marker for each tifa.HTML value, without writing the markup. An empty value stays "": omitempty
// looks at the JSON written, and a marker would keep a field the text the render reads leaves out.
var pageOptions = json.JoinOptions(core.JSONWrite, json.WithMarshalers(json.MarshalToFunc(func(enc *jsontext.Encoder, v tifa.HTML) error {
	if v == "" {
		return enc.WriteToken(jsontext.String(""))
	}
	return enc.WriteValue(jsontext.Value(htmlMarker))
})))

// pageText writes the text that the hydration data of a page the server renders carries for data,
// whose JSON text is full (loaderText): full with a marker in place of each tifa.HTML value. Only
// data of a Load the build marked (Route.HTML and Layout.HTML) gets one. differs is false when data
// holds no tifa.HTML value that is not empty after all, such as a nil pointer, and the page then carries full.
func pageText(data any, full string) (text string, differs bool, err error) {
	b, err := json.Marshal(data, pageOptions)
	if err != nil {
		return "", false, err
	}
	text = string(ssr.EscapeAboveFFFF(b))
	return text, text != full, nil
}
