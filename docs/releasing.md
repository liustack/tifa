---
summary: 'How Tifa is released: which version number to pick, the checks before a tag, the changelog, the tag and the GitHub release, and the check of the published module'
audience: dev
read_when:
  - Releasing a new version of Tifa
  - Deciding whether a change needs a patch or a minor version
---

# Releasing

Tifa is released as Git tags on `main`, starting at `v0.0.1`. The Go module proxy serves each tag, so `go run github.com/liustack/tifa/cmd/tifa@latest new` and the `go.mod` that `tifa new` writes pick up the release.

## Which version

- **Patch** (`v0.0.1` → `v0.0.2`): the default. Fixes, new features, docs and speedups. While Tifa is at `v0.0.x`, a change that breaks apps (a removed or renamed public API, setting or file convention) is a patch too, and the changelog says what to change.
- **Minor** (`v0.0.x` → `v0.1.0`): for the point where Tifa is stable enough to build on, not for a single change. The maintainer picks the moment. From v0.1, a change that breaks the surface [compatibility.md](compatibility.md) promises (a removed or renamed public name, file convention, command, flag or setting, or a changed HTTP name) needs a minor version, and a patch must not contain one.

## Steps

1. Make sure `main` is clean and up to date with `origin/main`, and nothing that should ship is still on a branch. If the release changes an npm version that new apps pin (React, TanStack Router, Vite), that version must have been published at least 3 days ago: a new app's `pnpm-workspace.yaml` makes pnpm refuse younger ones. `pnpm view <package>@<version> 'time[<version>]'` prints the date.
2. Run the checks of [Testing](testing.md) on `main`: `go test ./...`, `go vet ./...`, `CGO_ENABLED=0 go build ./...` and `TIFA_E2E=1 go test -timeout 30m ./test/e2e/`.
3. In `CHANGELOG.md`, rename `## [Unreleased]` to `## [0.0.N] - YYYY-MM-DD` and put a new empty `## [Unreleased]` above it. Read the entries first: an entry that needs a change in the app's code ends with an "Upgrading:" sentence that says exactly what to change, and an entry that needs nothing says nothing. Add the missing sentence now if a change skipped it. Commit it as `chore(release): release v0.0.N`.
4. Tag that commit and push both:

   ```sh
   git tag -a v0.0.N -m "tifa v0.0.N"
   git push origin main v0.0.N
   ```

5. Write the GitHub release with `gh release create v0.0.N --title "v0.0.N" --notes-file <notes>`. The notes are a short summary of what apps can see, in English, with a link to the changelog section.
6. Check the published module from an empty folder outside the checkout, with no `replace`:

   ```sh
   GOPROXY=https://proxy.golang.org go run github.com/liustack/tifa/cmd/tifa@v0.0.N new myapp
   cd myapp && go mod tidy && pnpm install && go tool tifa build && ./bin/myapp
   ```

   `go.mod` must require `github.com/liustack/tifa v0.0.N`, and the page must answer. If anything fails, fix it on `main` and release the next patch. A tag is never moved or deleted once pushed.
