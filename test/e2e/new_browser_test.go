package e2e

import (
	"os/exec"
	"strings"
	"testing"
)

// checkNewAppRefreshesInBrowser drives the built app in a browser. After a write the page shows
// the new data at once, without loading the document again: tifa never reloads page data by
// itself, so the pages of the template have to ask for it. It needs the todo 1, which is done.
func checkNewAppRefreshesInBrowser(t *testing.T, base string) {
	t.Helper()
	if _, err := exec.LookPath("playwright-cli"); err != nil {
		t.Log("playwright-cli is not installed: the pages of the template are not driven in a browser")
		return
	}
	pw, _ := openBrowser(t, "e2e-newapp")
	hydrated := func() {
		waitFor(t, "the page to hydrate", func() bool {
			return strings.Contains(pw("eval", `() => document.readyState === "complete" && window.__TSR_ROUTER__?.stores.matches.get().length > 0 && [...document.getElementById("tifa-root").querySelectorAll("*")].every((e) => Object.keys(e).some((key) => key.startsWith("__reactFiber$")))`), "true")
		})
	}
	run := func(script string) {
		t.Helper()
		if out := pw("run-code", script); strings.Contains(out, "### Error") {
			t.Fatalf("run-code:\n%s", out)
		}
	}
	pw("open", base+"/")
	hydrated()
	run(`async (page) => {
		await page.evaluate(() => { window.sameDocument = true })
		await page.getByLabel('New todo').fill('Feed the cat')
		await page.getByRole('button', { name: 'Add' }).click()
		await page.getByRole('link', { name: 'Feed the cat' }).waitFor({ timeout: 5000 })
		if (!(await page.evaluate(() => window.sameDocument))) throw new Error('the document was loaded again')
	}`)
	pw("goto", base+"/todos/1")
	hydrated()
	run(`async (page) => {
		await page.evaluate(() => { window.sameDocument = true })
		await page.getByRole('button', { name: 'Mark as not done' }).click()
		await page.getByRole('button', { name: 'Mark as done' }).waitFor({ timeout: 5000 })
		if (!(await page.evaluate(() => window.sameDocument))) throw new Error('the document was loaded again')
	}`)
}
