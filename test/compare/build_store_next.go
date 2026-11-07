package compare

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// syncNextStore copies what the storefront pages are made of into the Next.js app, after Sync has
// copied the workbench. Under dir/shop/app/store: every TypeScript file and the two message files (_locales)
// of examples/shop/app/store, and the route.gen modules tifa generated for the pages below
// app/store (with getRouteApi turned into loaderRoute, as for the workbench). All the photos go to
// dir/public/images, and images.ts, which the shop writes with Vite's import.meta.glob, is written
// again for all of them.
func (e Env) syncNextStore(dir string) error {
	src := filepath.Join(e.Shop, "app", "store")
	dst := filepath.Join(dir, "shop", "app", "store")
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		rel = filepath.ToSlash(rel)
		keep := (strings.HasSuffix(rel, ".ts") || strings.HasSuffix(rel, ".tsx")) && rel != "images.ts" ||
			strings.HasPrefix(rel, "_locales/") && strings.HasSuffix(rel, ".json")
		if !keep {
			return nil
		}
		return copyFile(path, filepath.Join(dst, rel), nil)
	})
	if err != nil {
		return err
	}

	gen := filepath.Join(e.Shop, ".tifa", "app", "store")
	err = filepath.WalkDir(gen, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Base(path) != "route.gen.ts" {
			return err
		}
		rel, _ := filepath.Rel(filepath.Join(e.Shop, ".tifa", "app", "store"), path)
		return copyFile(path, filepath.Join(dst, rel+"x"), nextRoute)
	})
	if err != nil {
		return err
	}

	var photos []string
	images := filepath.Join(src, "images")
	err = filepath.WalkDir(images, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".avif") {
			return err
		}
		rel, _ := filepath.Rel(images, path)
		photos = append(photos, filepath.ToSlash(rel))
		return copyFile(path, filepath.Join(dir, "public", "images", rel), nil)
	})
	if err != nil {
		return err
	}
	return writeStoreImages(filepath.Join(dst, "images.ts"), photos)
}

// writeStoreImages writes the images module of the storefront: the same three functions as the
// shop's, over the files in public/images.
func writeStoreImages(path string, files []string) error {
	var b strings.Builder
	b.WriteString("// Written by the comparison when it copies the shop: the pictures of the storefront in public/images.\n")
	b.WriteString("export type View = 'main' | 'detail' | 'scene'\nexport type Width = 160 | 480 | 1200\n\n")
	b.WriteString("const files = new Set([\n")
	for _, f := range files {
		fmt.Fprintf(&b, "  %q,\n", f)
	}
	b.WriteString("])\n\n")
	b.WriteString("const url = (name: string) => (files.has(`${name}.avif`) ? `/images/${name}.avif` : undefined)\n\n")
	b.WriteString("export function photo(slug: string, view: View = 'main', width: Width = 480): string | undefined {\n  return url(`${slug}/${view}-${width}`)\n}\n\n")
	b.WriteString("export function photoSet(slug: string, view: View = 'main'): string | undefined {\n")
	b.WriteString("  const small = photo(slug, view, 480)\n  const large = photo(slug, view, 1200)\n  return small && large ? `${small} 480w, ${large} 1200w` : undefined\n}\n\n")
	b.WriteString("export function picture(name: string): string | undefined {\n  return url(name)\n}\n")
	return os.WriteFile(path, []byte(b.String()), 0o644)
}
