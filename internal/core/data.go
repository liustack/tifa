package core

import (
	"context"
	"net/http"
	"net/url"
	"strings"
)

// DataParam is the query parameter of a navigation data request: the browser asks for the data of
// a page with the URL of the page and `_tifa=<build>.<layers>`, or `<build>.<layers>.p` when the
// router preloads. layers has one digit for each Load on the chain of the page, from the root
// Layout to the page, and a 1 asks for the data of that Load. The server takes the parameter off
// the request before any code of the application sees it.
const DataParam = "_tifa"

// DataRequest is what the `_tifa` parameter of a request said.
type DataRequest struct {
	Build   string
	Layers  string // the digits, "101"
	Preload bool
	// Valid is false when the parameter does not have the form `<build>.<digits>[.p]`.
	Valid bool
	// Original is the request the server was given, which still has the parameter in its URL.
	Original *http.Request
}

type dataKey struct{}

// SplitData returns r without the `_tifa` parameter in its URL, carrying what the parameter said
// (see DataOf). A request without it is returned as it is. The copy has its own URL, and the
// original request stays what a middleware outside the server sees.
func SplitData(r *http.Request) *http.Request {
	raw := r.URL.RawQuery
	if raw == "" {
		return r
	}
	var kept []string
	var value string
	found := false
	for _, part := range strings.Split(raw, "&") {
		key, val, _ := strings.Cut(part, "=")
		if k, err := url.QueryUnescape(key); err == nil && k == DataParam {
			if !found {
				v, err := url.QueryUnescape(val)
				if err != nil {
					v = "\x00" // not a value of the form
				}
				value = v
			}
			found = true
			continue
		}
		kept = append(kept, part)
	}
	if !found {
		return r
	}
	d := parseData(value)
	d.Original = r
	u := *r.URL
	u.RawQuery = strings.Join(kept, "&")
	out := r.WithContext(context.WithValue(r.Context(), dataKey{}, d))
	out.URL = &u
	if r.RequestURI != "" {
		out.RequestURI = u.RequestURI()
	}
	return out
}

func parseData(value string) *DataRequest {
	d := &DataRequest{}
	if rest, ok := strings.CutSuffix(value, ".p"); ok {
		d.Preload, value = true, rest
	}
	i := strings.LastIndex(value, ".")
	if i < 0 {
		return d
	}
	d.Build, d.Layers = value[:i], value[i+1:]
	d.Valid = d.Build != "" && d.Layers != ""
	for _, b := range []byte(d.Layers) {
		d.Valid = d.Valid && (b == '0' || b == '1')
	}
	return d
}

// DataOf returns what the `_tifa` parameter of r said, nil when it had none.
func DataOf(r *http.Request) *DataRequest {
	d, _ := r.Context().Value(dataKey{}).(*DataRequest)
	return d
}

// Data returns what the `_tifa` parameter of the request said, nil for a request without it.
func Data(c *Ctx) *DataRequest { return DataOf(c.r) }
