package compare

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Prepare builds everything the comparison needs, writing the commands and their failures to log:
// the shop as a tifa program, the data of its six pages, the sources of the two Node apps copied
// from the shop, and the two apps.
func (e Env) Prepare(ctx context.Context, log io.Writer) error {
	if err := e.BuildShop(ctx, log); err != nil {
		return err
	}
	if err := e.Snapshot(ctx, log); err != nil {
		return err
	}
	for _, app := range []struct{ dir, kind string }{{e.Next, "next"}, {e.Start, "start"}} {
		if err := e.Sync(app.dir, app.kind); err != nil {
			return err
		}
		if err := e.BuildApp(ctx, log, app.dir); err != nil {
			return err
		}
	}
	return nil
}

// BuildShop installs the packages of the shop and builds bin/shop with tifa build, in place.
func (e Env) BuildShop(ctx context.Context, log io.Writer) error {
	if err := run(ctx, log, e.Shop, nil, "pnpm", "install", "--frozen-lockfile"); err != nil {
		return err
	}
	return run(ctx, log, e.Shop, nil, "go", "tool", "tifa", "build")
}

// BuildApp installs the packages of a Node app from its lockfile and builds it.
func (e Env) BuildApp(ctx context.Context, log io.Writer, dir string) error {
	env := []string{"NEXT_TELEMETRY_DISABLED=1"}
	if err := run(ctx, log, dir, env, "pnpm", "install", "--frozen-lockfile"); err != nil {
		return err
	}
	return run(ctx, log, dir, env, "pnpm", "build")
}

// Snapshot starts bin/shop and asks it, as the browser router does, for the data of the Layouts
// and the page of each of the six pages. It writes them to data.json in both apps, which the
// apps load the way tifa's Go code loads: once per request, as the data the page needs. The data
// is what the Go code of the shop returns, so the apps cannot show other numbers than tifa.
func (e Env) Snapshot(ctx context.Context, log io.Writer) error {
	var built struct{ BuildID string }
	data, err := os.ReadFile(filepath.Join(e.Shop, ".tifa", "dist", "build.json"))
	if err != nil {
		return fmt.Errorf("build the shop first: %w", err)
	}
	if err := json.Unmarshal(data, &built); err != nil {
		return err
	}
	shop, err := e.Targets()[0].Start(ctx, "")
	if err != nil {
		return err
	}
	defer shop.Stop()

	// url -> route ID -> data
	snapshot := map[string]map[string]json.RawMessage{}
	for _, page := range Pages {
		req, err := http.NewRequestWithContext(ctx, "GET", shop.Base+page.URL, nil)
		if err != nil {
			return err
		}
		req.Header.Set("X-Tifa-Route", page.Routes)
		req.Header.Set("X-Tifa-Build", built.BuildID)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		body, err := io.ReadAll(res.Body)
		res.Body.Close()
		if err != nil || res.StatusCode != 200 {
			return fmt.Errorf("data of %s: %d %v %s", page.URL, res.StatusCode, err, body)
		}
		var routes map[string]struct {
			Status int
			Data   json.RawMessage
		}
		if err := json.Unmarshal(body, &routes); err != nil {
			return fmt.Errorf("data of %s: %w", page.URL, err)
		}
		snapshot[page.URL] = map[string]json.RawMessage{}
		for id, r := range routes {
			if r.Status != 200 {
				return fmt.Errorf("data of %s: route %s answered %d", page.URL, id, r.Status)
			}
			snapshot[page.URL][id] = r.Data
		}
	}
	out, err := json.MarshalIndent(snapshot, "", " ")
	if err != nil {
		return err
	}
	for _, dir := range []string{e.Next, e.Start} {
		if err := os.WriteFile(filepath.Join(dir, "data.json"), out, 0o644); err != nil {
			return err
		}
	}
	fmt.Fprintf(log, "data of %d pages written to both apps\n", len(Pages))
	return nil
}

// Sync copies what the workbench pages are made of from the shop into dir/shop, where the app
// imports it as `@/`. kind is "next" or "start". The folder is made new every time.
//
// Copied as they are: the shadcn components, the helpers, the root Layout, the stylesheet, the
// formatting functions and everything under app/projects that is TypeScript. The generated
// +layout and +route modules come from .tifa/app, with getRouteApi replaced for Next.js by a
// context the server component fills. The photos the pages show (their 160 pixel size) go to
// dir/public/images. Then the shared overlay (shims for what only tifa has, such as tifa/client)
// and the overlay of the app are copied over. The one change to a copied file: the stylesheet
// gets an @source line, since Tailwind skips folders that git ignores and dir/shop is one.
func (e Env) Sync(dir, kind string) error {
	dst := filepath.Join(dir, "shop")
	if err := os.RemoveAll(dst); err != nil {
		return err
	}
	if err := os.RemoveAll(filepath.Join(dir, "public", "images")); err != nil {
		return err
	}
	tifa := filepath.Join(e.Shop, ".tifa")
	if _, err := os.Stat(tifa); err != nil {
		return fmt.Errorf("build the shop first: %w", err)
	}

	copyTree := func(from, to string, keep func(rel string) bool) error {
		return filepath.WalkDir(from, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			rel, _ := filepath.Rel(from, path)
			if keep != nil && !keep(filepath.ToSlash(rel)) {
				return nil
			}
			return copyFile(path, filepath.Join(to, rel), nil)
		})
	}
	typescript := func(rel string) bool { return strings.HasSuffix(rel, ".ts") || strings.HasSuffix(rel, ".tsx") }

	if err := copyTree(filepath.Join(e.Shop, "components", "ui"), filepath.Join(dst, "components", "ui"), typescript); err != nil {
		return err
	}
	for _, name := range []string{"lib/utils.ts", "hooks/use-mobile.ts", "app/layout.tsx", "app/animated-number.tsx", "app/use-hydrated.ts", "app/format.ts"} {
		if err := copyFile(filepath.Join(e.Shop, name), filepath.Join(dst, name), nil); err != nil {
			return err
		}
	}
	err := copyFile(filepath.Join(e.Shop, "app", "styles.css"), filepath.Join(dst, "app", "styles.css"), func(b []byte) []byte {
		return append(b, "\n@source \"../\";\n"...)
	})
	if err != nil {
		return err
	}
	if err := copyTree(filepath.Join(e.Shop, "app", "projects"), filepath.Join(dst, "app", "projects"), typescript); err != nil {
		return err
	}
	if err := copyFile(filepath.Join(tifa, "runtime", "client-only.tsx"), filepath.Join(dst, "runtime", "client-only.tsx"), nil); err != nil {
		return err
	}

	// The modules tifa generates from the Go types, for the root Layout and the project pages.
	generated := func(rel string) bool {
		if !strings.HasPrefix(rel, "projects/") && rel != "+layout.ts" {
			return false
		}
		base := filepath.Base(rel)
		return base == "+layout.ts" || base == "+route.ts"
	}
	err = filepath.WalkDir(filepath.Join(tifa, "app"), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(filepath.Join(tifa, "app"), path)
		if !generated(filepath.ToSlash(rel)) {
			return nil
		}
		to := filepath.Join(dst, "app", rel)
		var edit func([]byte) []byte
		if kind == "next" {
			to += "x" // +route.tsx: it holds JSX through the loader context
			edit = nextRoute
		}
		return copyFile(path, to, edit)
	})
	if err != nil {
		return err
	}

	var photos []string
	err = copyTree(filepath.Join(e.Shop, "app", "store", "images"), filepath.Join(dir, "public", "images"), func(rel string) bool {
		if !strings.HasSuffix(rel, "-160.avif") {
			return false
		}
		photos = append(photos, rel)
		return true
	})
	if err != nil {
		return err
	}
	if err := writeImages(filepath.Join(dst, "app", "store", "images.ts"), photos); err != nil {
		return err
	}
	for _, overlay := range []string{filepath.Join(e.Root, "test", "compare", "overlay"), filepath.Join(dir, "overlay")} {
		if _, err := os.Stat(overlay); os.IsNotExist(err) {
			continue
		}
		if err := copyTree(overlay, dst, nil); err != nil {
			return err
		}
	}
	return nil
}

// nextRoute turns a generated module of the shop into one for Next.js: getRouteApi, which finds
// the data of a route in the TanStack router, becomes loaderRoute, which finds it in a React
// context that the server component of the layout or page fills.
func nextRoute(src []byte) []byte {
	s := string(src)
	s = strings.Replace(s, `import { getRouteApi } from "@tanstack/react-router"`, `import { loaderRoute } from "@/runtime/loader"`, 1)
	s = strings.Replace(s, "getRouteApi(", "loaderRoute<Data>(", 1)
	return []byte("'use client'\n" + s)
}

func copyFile(from, to string, edit func([]byte) []byte) error {
	data, err := os.ReadFile(from)
	if err != nil {
		return err
	}
	if edit != nil {
		data = edit(data)
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	return os.WriteFile(to, data, 0o644)
}

// writeImages writes the module the pages import `photo` from. The shop's own gathers the photos
// with Vite's import.meta.glob, which Next.js does not have. This one answers with the URL of the
// file in public/images when the shop has the picture, and with undefined when it does not, as the
// shop's does.
func writeImages(path string, files []string) error {
	var b strings.Builder
	b.WriteString("// Written by the comparison when it copies the shop: the photos of the products that the pages show.\n")
	b.WriteString("export type View = 'main' | 'detail' | 'scene'\nexport type Width = 160 | 480 | 1200\n\n")
	b.WriteString("const files = new Set([\n")
	for _, f := range files {
		fmt.Fprintf(&b, "  %q,\n", filepath.ToSlash(f))
	}
	b.WriteString("])\n\n")
	b.WriteString("export function photo(slug: string, view: View = 'main', width: Width = 480): string | undefined {\n")
	b.WriteString("  const file = `${slug}/${view}-${width}.avif`\n")
	b.WriteString("  return files.has(file) ? `/images/${file}` : undefined\n}\n")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}
