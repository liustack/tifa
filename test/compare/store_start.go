package compare

import (
	"io/fs"
	"path/filepath"
	"strings"
)

// syncStoreStart copies what the storefront pages of the shop are made of into dst, the shop/
// folder of the TanStack Start app: every TypeScript file, JSON file (the two languages) and
// photo under app/store, and the route.gen modules tifa build wrote for the storefront. The
// photos keep their place, so the shop's own images.ts finds them (Vite gives the files their
// hashed URLs under /assets/, as it does in the shop's build), and that file replaces the short
// one that Sync wrote for the workbench.
//
// The route.gen modules work as they are but for two route IDs. Tifa gives the list of products
// and the list of journal posts the IDs /store/products and /store/journal, with the
// product and the post below them as pages of their own. A TanStack file route that has children
// is a Layout, so the lists are index routes here, with the trailing slash in their ID.
func (e Env) syncStoreStart(dst string) error {
	store := filepath.Join(e.Shop, "app", "store")
	err := filepath.WalkDir(store, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		ext := filepath.Ext(path)
		if ext != ".ts" && ext != ".tsx" && ext != ".json" && ext != ".avif" {
			return nil
		}
		rel, _ := filepath.Rel(store, path)
		return copyFile(path, filepath.Join(dst, "app", "store", rel), nil)
	})
	if err != nil {
		return err
	}
	generated := filepath.Join(e.Shop, ".tifa", "app", "store")
	return filepath.WalkDir(generated, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Base(path) != "route.gen.ts" {
			return err
		}
		rel, _ := filepath.Rel(generated, path)
		return copyFile(path, filepath.Join(dst, "app", "store", rel), startRoute)
	})
}

// startRoute edits a generated module of the storefront for the file routes of the Start app.
func startRoute(src []byte) []byte {
	s := string(src)
	for _, id := range []string{"/store/products", "/store/journal"} {
		s = strings.Replace(s, `getRouteApi("`+id+`")`, `getRouteApi("`+id+`/")`, 1)
	}
	return []byte(s)
}
