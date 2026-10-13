---
summary: '提交规范：提交信息格式、type 列表、原子提交、分支与 worktree'
read_when:
  - 准备提交代码
  - 写提交信息
  - 拆分一组改动
---

# 提交规范

## 格式

```
<type>[scope]: <祈使句摘要>

<可选正文：为什么这么改，以及不明显的取舍>
```

- 摘要不超过 72 个字符，末尾不加句号。
- type：`feat` `fix` `docs` `style` `refactor` `perf` `test` `build` `ci` `chore` `revert`。
- scope 写受影响的包或领域，如 `server`、`config`、`ssr`。
- 破坏性变更在 type 后加 `!`，或在正文末尾写 `BREAKING CHANGE:`。
- 提交信息里不用分号，也不用破折号作标点。

例：

```
feat(config): load layered runtime settings and render an example file
fix(core): reject trailing content after a JSON body
refactor: rename Go module to github.com/liustack/tifa
```

## 原子提交

- 一个提交只做一件事，能单独验证，也能单独回退。
- 行为变更和它的测试放在同一个提交里。
- 纯重构、格式化、改名这类机械改动单独提交，不和行为变更混在一起。
- 不提交构建产物（`.tifa/`、`examples/shop` 的 `internal/ssr/assets/` 与 `web/generated/` 等已在 `.gitignore` 中）。
- 不用 "WIP"、"fix stuff" 这类说不清改了什么的摘要。

## 分支

- 不直接在 `main` 上开发，先 `git switch -c <分支>`。
- 互不依赖的工作可以各开一个 worktree 并行做，放在仓库同级目录，如 `../tifa-<主题>`。合并后删除 worktree。
- 提交前跑完 [testing.md](testing.md) 里的检查。
