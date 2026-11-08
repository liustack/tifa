package compare

import (
	"fmt"
	"slices"
	"strings"
)

// TraceCheck is what one server answered to the replayed traces in one round: the counts by type
// of request, status and content type. It shows that the traces recorded on one machine replay
// the same way on another: data requests get their data and not a page, a redirect or a reload.
type TraceCheck struct {
	Label     string         `json:"label"`
	Requests  int            `json:"requests"`
	Responses map[string]int `json:"responses"`
	// HTMLToData counts answers of type text/html to requests that are not documents.
	HTMLToData int `json:"htmlToDataRequests"`
	// ReloadHeaders counts answers that ask the browser to reload the document.
	ReloadHeaders int `json:"reloadDocumentHeaders"`
	// NotOK counts answers other than 200, 204 and 304. A redirect is not followed and is an error.
	NotOK int `json:"notOk"`
}

// NewTraceCheck counts the answers of a round.
func NewTraceCheck(label string, res *Round) *TraceCheck {
	c := &TraceCheck{Label: label, Requests: res.Requests, Responses: res.Responses}
	for k, n := range res.Responses {
		f := strings.Fields(k)
		switch {
		case k == "reload-document header":
			c.ReloadHeaders += n
		case len(f) == 3:
			if f[0] != "doc" && f[2] == "text/html" {
				c.HTMLToData += n
			}
			if f[1] != "200" && f[1] != "204" && f[1] != "304" {
				c.NotOK += n
			}
		}
	}
	return c
}

// Summary is one line: the answers by type, status and content type, and the three things that
// would show a trace that does not replay.
func (c *TraceCheck) Summary() string {
	var keys []string
	for k := range c.Responses {
		if k != "reload-document header" {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	var parts []string
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s x%d", k, c.Responses[k]))
	}
	return fmt.Sprintf("%d answers: %s. HTML answers to data requests: %d, answers other than 200, 204 and 304: %d, reload-document headers: %d.", c.Requests, strings.Join(parts, ", "), c.HTMLToData, c.NotOK, c.ReloadHeaders)
}
