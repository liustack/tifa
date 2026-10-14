---
summary: 'Commit rules: commit message format, the type list, atomic commits, branches and worktrees'
read_when:
  - Getting ready to commit
  - Writing a commit message
  - Splitting up a set of changes
---

# Commit rules

## Format

```
<type>[scope]: <imperative summary>

<optional body: why the change was made, and any trade-offs that are not obvious>
```

- The summary is at most 72 characters, with no period at the end.
- type: `feat` `fix` `docs` `style` `refactor` `perf` `test` `build` `ci` `chore` `revert`.
- scope is the affected package or area, such as `server`, `config`, `ssr`.
- For a breaking change, add `!` after the type, or write `BREAKING CHANGE:` at the end of the body.
- Do not use semicolons in commit messages, and do not use dashes as punctuation.

Examples:

```
feat(config): load layered runtime settings and render an example file
fix(core): reject trailing content after a JSON body
refactor: rename Go module to github.com/liustack/tifa
```

## Atomic commits

- One commit does one thing, and can be verified and reverted on its own.
- A behavior change and its tests go in the same commit.
- Mechanical changes such as pure refactors, formatting and renames are committed separately, not mixed with behavior changes.
- Do not commit build output (`.tifa/`, and `internal/ssr/assets/` and `web/generated/` in `examples/shop` and the like are already in `.gitignore`).
- Do not use summaries like "WIP" or "fix stuff" that do not say what changed.

## Branches

- Do not develop directly on `main`. Run `git switch -c <branch>` first.
- Independent pieces of work can each get their own worktree and run in parallel. Put them next to the repository, such as `../tifa-<topic>`. Delete the worktree after merging.
- Before committing, run the checks in [testing.md](testing.md).
