---
summary: 'How tifa is released: which version number to pick, the checks before a tag, the changelog, the tag and the GitHub release, and the check of the published module'
read_when:
  - Releasing a new version of tifa
  - Deciding whether a change needs a patch or a minor version
---

# Releasing

tifa is released as Git tags on `main`, starting at `v0.0.1`. The Go module proxy serves each tag, so `go run github.com/liustack/tifa/cmd/tifa@latest new` and the `go.mod` that `tifa new` writes pick up the release.

## Which version

- **Patch** (`v0.0.1` → `v0.0.2`): the default. Fixes, new features, docs, speedups.
- **Minor** (`v0.0.x` → `v0.1.0`): only for a change that breaks apps, such as a removed or renamed public API, setting or file convention. Ask the owner first.

## Steps

1. Make sure `main` is clean and up to date with `origin/main`, and nothing that should ship is still on a branch.
2. Run the checks of [Testing](testing.md) on `main`: `go test ./...`, `go vet ./...`, `CGO_ENABLED=0 go build ./...` and `TIFA_E2E=1 go test ./test/e2e/`.
3. In `CHANGELOG.md` and `CHANGELOG.zh-CN.md`, rename `## [Unreleased]` to `## [0.0.N] - YYYY-MM-DD` and put a new empty `## [Unreleased]` above it. Commit both as `chore(release): release v0.0.N`.
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
