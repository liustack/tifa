package compare

import "regexp"

// Where the storefront's writes are in each app. Tifa and TanStack Start take them on the URL of
// the page they belong to (a POST to a product, a PATCH to the cart). Next.js and React Router
// cannot have a handler next to a page, so their ports take them on a path of their own. The path
// of the shop, with its language, goes in, and the path in the app comes out. /store/mine is where
// it is in all four.
var (
	storePageWrite = regexp.MustCompile(`^(?:/(zh-CN))?/store/(cart|checkout|products/)`)
	storeMinePath  = regexp.MustCompile(`^(?:/zh-CN)?/store/mine`)
)

// writeLanguage is the language of a write: the first group of storePageWrite, empty for the
// default language, which has no prefix.
func writeLanguage(prefix string) string {
	if prefix == "" {
		return "en-US"
	}
	return prefix
}

func storeWriteAt(p string, to func(lang, rest string) string) string {
	if storeMinePath.MatchString(p) {
		return p
	}
	return storePageWrite.ReplaceAllStringFunc(p, func(m string) string {
		parts := storePageWrite.FindStringSubmatch(m)
		return to(writeLanguage(parts[1]), parts[2])
	})
}

// SameStorePath is the path of a write in an app that takes it where the shop does.
func SameStorePath(p string) string { return p }

// NextStorePath is the path of a write in the Next.js app: /store/compare-write/<lang>/...
func NextStorePath(p string) string {
	return storeWriteAt(p, func(lang, rest string) string { return "/store/compare-write/" + lang + "/" + rest })
}

// ReactRouterStorePath is the path of a write in the React Router app: /store/<lang>/_write/...
func ReactRouterStorePath(p string) string {
	return storeWriteAt(p, func(lang, rest string) string { return "/store/" + lang + "/_write/" + rest })
}
