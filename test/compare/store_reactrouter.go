package compare

import (
	"io/fs"
	"path/filepath"
	"strings"
)

// syncStoreReactRouter copies what the storefront pages are made of from the shop into dir/shop,
// next to what Sync copied for the workbench: everything under app/store that is TypeScript, the
// two translation files, every photo (the shop's own app/store/images.ts finds them with Vite's
// import.meta.glob, which React Router has too, so it replaces the one Sync wrote), and the
// generated route.gen modules of the storefront with getRouteApi turned into loaderRoute.
func (e Env) syncStoreReactRouter(dir string) error {
	dst := filepath.Join(dir, "shop", "app", "store")
	store := filepath.Join(e.Shop, "app", "store")
	copyTree := func(from, to string, keep func(rel string) bool, edit func([]byte) []byte) error {
		return filepath.WalkDir(from, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			rel, _ := filepath.Rel(from, path)
			if !keep(filepath.ToSlash(rel)) {
				return nil
			}
			return copyFile(path, filepath.Join(to, rel), edit)
		})
	}
	source := func(rel string) bool {
		return strings.HasSuffix(rel, ".ts") || strings.HasSuffix(rel, ".tsx") ||
			strings.HasPrefix(rel, "locales/") && strings.HasSuffix(rel, ".json") ||
			strings.HasPrefix(rel, "images/") && strings.HasSuffix(rel, ".avif")
	}
	if err := copyTree(store, dst, source, nil); err != nil {
		return err
	}
	// route.gen.ts is a name of tifa's output and the shop's sources have none, so the generated
	// ones are copied from .tifa.
	return copyTree(filepath.Join(e.Shop, ".tifa", "app", "store"), dst, func(rel string) bool {
		return filepath.Base(rel) == "route.gen.ts"
	}, reactRouterRoute)
}
