package pages

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/liustack/tifa/examples/shop/internal/workspace"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

var ErrBadURL = errors.New("invalid page URL")
var segment = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

type Binding struct {
	RouteParam string `json:"routeParam"`
}
type Requirement struct {
	Source string             `json:"source"`
	Params map[string]Binding `json:"params"`
}
type Route struct {
	ID      string                 `json:"id"`
	Kind    string                 `json:"kind"`
	Parent  string                 `json:"parent"`
	Pattern string                 `json:"pattern"`
	Data    map[string]Requirement `json:"data"`
}
type Snapshot struct {
	Kind     string                    `json:"kind"`
	Protocol int                       `json:"protocol"`
	BuildID  string                    `json:"buildId"`
	URL      string                    `json:"url"`
	PageID   string                    `json:"pageId"`
	Title    string                    `json:"title"`
	Scopes   map[string]map[string]any `json:"scopes"`
}

type Loader struct {
	buildID   string
	routes    map[string]Route
	pages     []Route
	providers map[string]workspace.Provider
}

func New(manifest []byte, buildID string, providers map[string]workspace.Provider) (*Loader, error) {
	var routes []Route
	if err := json.Unmarshal(manifest, &routes); err != nil {
		return nil, err
	}
	l := &Loader{buildID: buildID, routes: map[string]Route{}, providers: providers}
	patterns := map[string]bool{}
	for _, r := range routes {
		if r.ID == "" || (r.Kind != "page" && r.Kind != "layout") || !strings.HasPrefix(r.Pattern, "/") {
			return nil, fmt.Errorf("invalid route: %q", r.ID)
		}
		if _, exists := l.routes[r.ID]; exists {
			return nil, fmt.Errorf("duplicate route: %s", r.ID)
		}
		l.routes[r.ID] = r
		if r.Kind == "page" {
			if patterns[r.Pattern] {
				return nil, fmt.Errorf("duplicate page: %s", r.Pattern)
			}
			patterns[r.Pattern] = true
			l.pages = append(l.pages, r)
		}
		for _, req := range r.Data {
			provider, ok := providers[req.Source]
			if !ok || len(req.Params) != len(provider.Params) {
				return nil, fmt.Errorf("invalid provider contract: %s in %s", req.Source, r.ID)
			}
			for _, name := range provider.Params {
				binding, exists := req.Params[name]
				if !exists || binding.RouteParam == "" || !strings.Contains(r.Pattern, "["+binding.RouteParam+"]") {
					return nil, fmt.Errorf("invalid parameter %s in %s", name, r.ID)
				}
			}
		}
	}
	for _, r := range routes {
		seen := map[string]bool{r.ID: true}
		for parent := r.Parent; parent != ""; {
			p, exists := l.routes[parent]
			if !exists || p.Kind != "layout" || seen[parent] {
				return nil, fmt.Errorf("invalid layout ancestry for %s", r.ID)
			}
			seen[parent] = true
			parent = p.Parent
		}
	}
	// Static segments outrank parameters at the first differing segment.
	sort.SliceStable(l.pages, func(i, j int) bool {
		a, b := parts(l.pages[i].Pattern), parts(l.pages[j].Pattern)
		for n := 0; n < len(a) && n < len(b); n++ {
			if dynamic(a[n]) != dynamic(b[n]) {
				return !dynamic(a[n])
			}
		}
		return l.pages[i].Pattern < l.pages[j].Pattern
	})
	return l, nil
}

func (l *Loader) Load(ctx context.Context, rawURL string) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	u, err := url.ParseRequestURI(rawURL)
	if err != nil || !strings.HasPrefix(rawURL, "/") || strings.HasPrefix(rawURL, "//") || strings.Contains(rawURL, "#") || u.IsAbs() || u.Host != "" || u.EscapedPath() != u.Path {
		return Snapshot{}, ErrBadURL
	}
	var page Route
	var params map[string]string
	for _, candidate := range l.pages {
		if p, ok := match(candidate.Pattern, u.Path); ok {
			page, params = candidate, p
			break
		}
	}
	if page.ID == "" {
		return Snapshot{}, workspace.ErrNotFound
	}
	chain := []Route{page}
	for parent := page.Parent; parent != ""; {
		r := l.routes[parent]
		chain = append(chain, r)
		parent = r.Parent
	}
	snapshot := Snapshot{Kind: "data", Protocol: 1, BuildID: l.buildID, URL: rawURL, PageID: page.ID, Title: "山海工作台", Scopes: map[string]map[string]any{}}
	resolved := map[string]any{}
	for i := len(chain) - 1; i >= 0; i-- {
		route := chain[i]
		scope := map[string]any{}
		names := make([]string, 0, len(route.Data))
		for name := range route.Data {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			if err := ctx.Err(); err != nil {
				return Snapshot{}, err
			}
			req := route.Data[name]
			input := map[string]string{}
			for key, binding := range req.Params {
				value, ok := params[binding.RouteParam]
				if !ok {
					return Snapshot{}, fmt.Errorf("missing route parameter %s", binding.RouteParam)
				}
				input[key] = value
			}
			encoded, err := json.Marshal(input)
			if err != nil {
				return Snapshot{}, err
			}
			key := req.Source + ":" + string(encoded)
			value, exists := resolved[key]
			if !exists {
				value, err = l.providers[req.Source].Load(ctx, input)
				if err != nil {
					return Snapshot{}, fmt.Errorf("load %s: %w", req.Source, err)
				}
				resolved[key] = value
			}
			scope[name] = value
			switch data := value.(type) {
			case workspace.Project:
				snapshot.Title = data.Name + " · 订单"
			case workspace.Order:
				snapshot.Title = data.ID + " · 山海工作台"
			}
		}
		snapshot.Scopes[route.ID] = scope
	}
	return snapshot, nil
}

func parts(path string) []string {
	if path == "/" {
		return nil
	}
	return strings.Split(strings.TrimPrefix(path, "/"), "/")
}
func dynamic(part string) bool { return strings.HasPrefix(part, "[") && strings.HasSuffix(part, "]") }
func match(pattern, path string) (map[string]string, bool) {
	a, b := parts(pattern), parts(path)
	if len(a) != len(b) {
		return nil, false
	}
	params := map[string]string{}
	for i, part := range a {
		if dynamic(part) {
			if !segment.MatchString(b[i]) {
				return nil, false
			}
			params[part[1:len(part)-1]] = b[i]
		} else if part != b[i] {
			return nil, false
		}
	}
	return params, true
}
