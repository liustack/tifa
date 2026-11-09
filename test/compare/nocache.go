package compare

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// CacheSigns lists what in the headers of an answer lets a cache keep it or ask for it again cheaply:
// an ETag, a Last-Modified, a max-age or s-maxage above 0, and the headers Next.js sends with a page
// it keeps (x-nextjs-cache) or made at build time (x-nextjs-prerender). It is empty for an answer
// that must be made again for the next request.
func CacheSigns(h http.Header) []string {
	var out []string
	for _, name := range []string{"ETag", "Last-Modified"} {
		if h.Get(name) != "" {
			out = append(out, name)
		}
	}
	for _, d := range strings.Split(h.Get("Cache-Control"), ",") {
		name, value, ok := strings.Cut(strings.TrimSpace(d), "=")
		if !ok || (name != "max-age" && name != "s-maxage") {
			continue
		}
		if n, err := strconv.Atoi(strings.Trim(value, `"`)); err != nil || n > 0 {
			out = append(out, "Cache-Control "+name+"="+value)
		}
	}
	for _, name := range []string{"x-nextjs-cache", "x-nextjs-prerender"} {
		if h.Get(name) != "" {
			out = append(out, name)
		}
	}
	return out
}

// replayedGets are the requests of the traces of a workload that the check of the caches asks for
// (CheckNoCache): the documents and the navigation data the browser fetched and did not cancel, one
// of each path (the query of a navigation, such as the _rsc of Next.js, changes with the visit). The
// route manifest of React Router (__manifest) is not among them: it holds the routes of the build and
// no data of a page, and React Router sends it with a max-age of a year, as it does its assets.
func replayedGets(wl *Workload) []TraceReq {
	var out []TraceReq
	seen := map[string]bool{}
	for _, p := range wl.Plans {
		for _, s := range wl.Traces[p.ID].Steps {
			for _, q := range s.Reqs {
				if q.Method != "GET" || q.Cancel != "" || (q.Class != "doc" && q.Class != "nav") {
					continue
				}
				path, _, _ := strings.Cut(q.URL, "?")
				if strings.HasSuffix(path, "/__manifest") {
					continue
				}
				if key := q.Class + " " + path; !seen[key] {
					seen[key] = true
					out = append(out, q)
				}
			}
		}
	}
	return out
}

// CheckNoCache asks the server at base twice for each request, with the headers the browser sent,
// following no redirect, and fails when an answer has a sign that a cache could keep it
// (CacheSigns), naming every such request.
func CheckNoCache(ctx context.Context, base string, reqs []TraceReq) error {
	c := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	var bad []string
	for _, q := range reqs {
		for try := range 2 {
			req, err := http.NewRequestWithContext(ctx, "GET", base+q.URL, nil)
			if err != nil {
				return err
			}
			for k, v := range q.Headers {
				req.Header.Set(k, v)
			}
			res, err := c.Do(req)
			if err != nil {
				return fmt.Errorf("GET %s: %w", q.URL, err)
			}
			io.Copy(io.Discard, res.Body)
			res.Body.Close()
			if signs := CacheSigns(res.Header); len(signs) > 0 {
				bad = append(bad, fmt.Sprintf("GET %s (%d of 2): %s", q.URL, try+1, strings.Join(signs, ", ")))
			}
		}
	}
	if len(bad) > 0 {
		return errors.New("an answer a cache could keep: " + strings.Join(bad, "; "))
	}
	return nil
}
