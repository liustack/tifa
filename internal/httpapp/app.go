package httpapp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go-quick/internal/pages"
	"go-quick/internal/ssr"
	"go-quick/internal/workspace"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type App struct {
	bundle   *ssr.Bundle
	loader   *pages.Loader
	renderer *ssr.Renderer
	store    *workspace.Store
	mux      *http.ServeMux
}

func New(store *workspace.Store) (*App, error) {
	bundle, err := ssr.LoadBundle()
	if err != nil {
		return nil, err
	}
	loader, err := pages.New(bundle.Routes, bundle.Build.BuildID, workspace.Providers(store))
	if err != nil {
		return nil, err
	}
	a := &App{bundle: bundle, loader: loader, renderer: ssr.New(bundle, 2, 3*time.Second), store: store, mux: http.NewServeMux()}
	a.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]string{"status": "ok", "engine": "QuickJS", "buildId": bundle.Build.BuildID})
	})
	a.mux.HandleFunc("GET /_framework/page-data", a.pageData)
	a.mux.HandleFunc("GET /assets/{file...}", a.asset)
	a.mux.HandleFunc("POST /api/projects/{projectId}/orders/{orderId}/ship", a.ship)
	a.mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/projects/studio", http.StatusFound)
	})
	a.mux.HandleFunc("GET /", a.document)
	return a, nil
}

func (a *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	a.mux.ServeHTTP(w, r)
}

func (a *App) pageData(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.Query().Get("build") != a.bundle.Build.BuildID {
		writeJSON(w, http.StatusConflict, map[string]string{"kind": "reload"})
		return
	}
	start := time.Now()
	snapshot, err := a.loader.Load(r.Context(), r.URL.Query().Get("url"))
	if err != nil {
		status, message := publicError(err)
		writeJSON(w, status, map[string]string{"error": message})
		return
	}
	w.Header().Set("Server-Timing", fmt.Sprintf("data;dur=%.2f", milliseconds(time.Since(start))))
	writeJSON(w, http.StatusOK, snapshot)
}

var documentTemplate = template.Must(template.New("document").Parse(`<!doctype html>
<html lang="zh-CN"><head><meta charset="UTF-8"><meta name="viewport" content="width=device-width, initial-scale=1"><meta name="theme-color" content="#f7f8f5"><link rel="icon" href="data:,"><title>{{.Title}}</title><link rel="stylesheet" href="{{.Style}}"></head>
<body><div id="app">{{.HTML}}</div><script id="go-quick-config" type="application/json">{{.Config}}</script><script type="module" src="{{.Script}}"></script></body></html>`))

func (a *App) document(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if strings.HasSuffix(r.URL.Path, "/") && r.URL.Path != "/" {
		u := *r.URL
		u.Path = strings.TrimSuffix(u.Path, "/")
		http.Redirect(w, r, u.String(), http.StatusPermanentRedirect)
		return
	}
	start := time.Now()
	snapshot, err := a.loader.Load(r.Context(), r.URL.RequestURI())
	if err != nil {
		a.documentError(w, err)
		return
	}
	dataTime := time.Since(start)
	result, err := a.renderer.Render(r.Context(), snapshot)
	if err != nil {
		log.Printf("SSR %s: %v", r.URL.Path, err)
		a.documentError(w, err)
		return
	}
	config, err := json.Marshal(struct {
		BuildID string `json:"buildId"`
	}{a.bundle.Build.BuildID})
	if err != nil {
		a.documentError(w, err)
		return
	}
	var html bytes.Buffer
	err = documentTemplate.Execute(&html, struct {
		Title, Style, Script string
		HTML                 template.HTML
		Config               template.JS
	}{
		snapshot.Title, a.bundle.Build.Style, a.bundle.Build.Script, template.HTML(result.HTML), template.JS(config),
	})
	if err != nil {
		a.documentError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-SSR-Engine", "QuickJS")
	w.Header().Set("Server-Timing", fmt.Sprintf("data;dur=%.2f, vm;dur=%.2f, render;dur=%.2f", milliseconds(dataTime), milliseconds(result.Init), milliseconds(result.Render)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(html.Bytes())
}

func (a *App) documentError(w http.ResponseWriter, err error) {
	status, message := publicError(err)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, `<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>%s</title><body><main><h1>%s</h1><a href="/projects/studio">返回工作区</a></main></body></html>`, template.HTMLEscapeString(message), template.HTMLEscapeString(message))
}

func (a *App) asset(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("file")
	if !fs.ValidPath(name) || name == "." {
		http.NotFound(w, r)
		return
	}
	info, err := fs.Stat(a.bundle.Public, name)
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	http.ServeFileFS(w, r, a.bundle.Public, name)
}

func (a *App) ship(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Header.Get("X-Requested-With") != "go-quick" {
		writeJSON(w, 403, map[string]string{"error": "请求来源无效"})
		return
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		if err != nil || u.Host != r.Host || u.Scheme != scheme {
			writeJSON(w, 403, map[string]string{"error": "请求来源无效"})
			return
		}
	}
	order, err := a.store.Ship(r.PathValue("projectId"), r.PathValue("orderId"))
	if err != nil {
		status, message := publicError(err)
		writeJSON(w, status, map[string]string{"error": message})
		return
	}
	writeJSON(w, 200, order)
}

func publicError(err error) (int, string) {
	switch {
	case errors.Is(err, workspace.ErrNotFound):
		return 404, "没有找到这个页面或订单"
	case errors.Is(err, pages.ErrBadURL):
		return 400, "页面地址无效"
	case errors.Is(err, workspace.ErrAlreadyShipped):
		return 409, "订单已发货，请刷新页面"
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return 503, "页面加载超时，请稍后重试"
	default:
		return 500, "页面暂时无法打开"
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	data, err := json.Marshal(value)
	if err != nil {
		http.Error(w, "JSON encoding failed", 500)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(data)
}

func milliseconds(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }
