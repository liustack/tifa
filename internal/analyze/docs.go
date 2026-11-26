package analyze

// The doc sections that the messages of the build point to, in the docs of the Tifa version the
// app uses. TestEveryDocLinkPointsToASectionThatExists checks each one.
const (
	docFolders      = "docs/pages-and-data/routing.md#urls-come-from-folders"
	docRefused      = "docs/reference/conventions.md#what-the-build-refuses"
	docNames        = "docs/reference/conventions.md#names-in-routego"
	docLayout       = "docs/pages-and-data/layouts.md#the-layout-of-a-folder"
	docLayoutDrop   = "docs/pages-and-data/layouts.md#a-layout-without-the-layouts-above"
	docBreadcrumbs  = "docs/pages-and-data/layouts.md#breadcrumbs-and-the-current-page"
	docRouteGo      = "docs/pages-and-data/loading-data.md#routego"
	docQuery        = "docs/pages-and-data/loading-data.md#query-parameters"
	docHTML         = "docs/pages-and-data/loading-data.md#showing-html-the-app-trusts"
	docLoadCtx      = "docs/pages-and-data/loading-data.md#how-load-may-use-ctx"
	docHandlers     = "docs/pages-and-data/handlers.md#method-handlers-with-input-and-result"
	docFilesInput   = "docs/pages-and-data/handlers.md#files-in-the-input"
	docCache        = "docs/pages-and-data/ssr-and-cache.md#cache-for-cdns"
	docPrivate      = "docs/pages-and-data/ssr-and-cache.md#user-independent-pages-and-private-data"
	docSession      = "docs/pages-and-data/sessions.md#checks-at-build-time"
	docSessionWhere = "docs/pages-and-data/sessions.md#where-to-call-what"
	docOptions      = "docs/build-and-deploy/config.md#options-of-the-app"
	docContent      = "docs/pages-and-data/content.md#declaring-a-collection"
	docContentIDs   = "docs/pages-and-data/content.md#id-slug-path-and-languages"
	docContentFiles = "docs/pages-and-data/content.md#which-files-are-entries"
	docJSON         = "docs/reference/json.md#json-rules"
)

// see is the end of a message that points to the doc section that explains the fix.
func see(doc string) string {
	return " See " + doc
}
