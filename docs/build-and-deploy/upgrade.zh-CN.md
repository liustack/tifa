---
summary: '升级 Tifa，并让 agent 读的文件保持最新：`tifa upgrade`、`tifa agents`、AGENTS.md 里由 Tifa 管理的那一块、`tifa docs`'
audience: app
order: 9
translatedFrom: 328d7271af04
read_when:
  - '修改 `tifa upgrade`，或者把应用升级到更新的 Tifa'
  - '修改 `tifa docs` 或 `tifa new` 写出的 `AGENTS.md`，或者其中由 Tifa 管理的那一块'
  - '修改 `tifa agents`、`skills/tifa` 里的技能或应用得到的 `.claude/skills/tifa`，或者某个 agent 在用过期的文件工作'
---

# 升级 Tifa 和 agent 读的文件 {#upgrade-tifa-and-the-files-agents-read}

一条命令就能把应用升到更新的 Tifa。这些命令也让文档、技能和编程 agent 读的 `AGENTS.md` 笔记与正在用的版本保持一致。

## 升级 Tifa {#upgrading-tifa}

```sh
go tool tifa upgrade [version] [--dir app-root]
```

把应用升级到 Tifa 的另一个发布版本，并带上该版本固定或管理的东西。不带版本时取最新发布版本。请在干净的检出目录里运行，这样 `git diff` 就能显示它改了什么。

1. `go get -tool github.com/liustack/tifa/cmd/tifa@<version>`，然后 `go mod tidy`。
2. 命令把剩下的事交给新的 Tifa，即 `go tool tifa upgrade --after <old version>`。旧的二进制不做这些，因为只有新的 Tifa 才知道它固定了哪些版本，以及它的 skill 写了什么。
3. `tifa new` 用精确版本写出的那些包（`react`、`react-dom`、`@tanstack/react-router`、`vite`，以及它的 `package.json` 里的其他包），当应用把它们列成更旧的精确版本时，会被设成新 Tifa 的版本。其他每个包、其他每个键和文件的排版都保持原样。比固定版本更新的包会保留，并带一行类似 `react 19.4.0 is newer than the 19.3.0 Tifa is tested with, left as it is` 的提示。写成 `^19.2.0`、`~19.2.0`、范围、标签或 `workspace:` 的版本从不去猜：它会保留，并带一行说明。例外是 `@tanstack/react-router`，`tifa build` 只接受它恰好是固定的那个版本（见[应用的 tsconfig 和 Vite 配置](../reference/generated-files.zh-CN.md#the-apps-tsconfig-and-vite-config)）：它的精确版本，不管更旧还是更新，都会被设成固定的版本，写成范围的则保留，并带一行要你手动设置的提示。然后运行安装命令，`pnpm install` 或你的锁文件对应的那个命令。
4. skill 和 `AGENTS.md` 里由 Tifa 管理的部分会重新写出，和 [`tifa agents`](#tifa-agents) 做的一样。
5. 它会打印你旧版本之后、直到新版本为止的每个 [CHANGELOG.md](../../CHANGELOG.md) 条目里的每条 `Upgrading:` 说明，每条都带着它的版本。这些是需要你动手做的步骤：这个命令从不修改你的 Go 或 TypeScript 代码。
6. 运行 `tifa check`，它的结果就是 `tifa upgrade` 的结果。

Tifa 在各个发布版本之间保持稳定的是什么，以及哪些在任何版本里都可能改变，见[兼容性](../reference/compatibility.zh-CN.md)。

```text
$ go tool tifa upgrade
==> tifa v0.0.15 to v0.0.16
==> go get -tool github.com/liustack/tifa/cmd/tifa@v0.0.16
==> go mod tidy
==> go tool tifa upgrade --after v0.0.15
==> package.json
react: 19.2.0 -> 19.3.0
==> pnpm install
==> agent files
.claude/skills/tifa/SKILL.md: updated
==> Upgrading notes, v0.0.15 to v0.0.16
v0.0.16: read the parameter in the `Load` ...
==> go vet ./...
...
check passed
```

- 它会拒绝比 `go.mod` 里更旧的版本，是同一个版本时则说明应用已经在那个版本了。它会拒绝 `go.mod` 里替换了 Tifa 的应用，因为这时 `go.mod` 里的版本并不是应用构建所用的版本。拒绝时什么都不会改。当正在使用 `go.work` 文件时（设置了 `GOWORK`，或应用里或它上层有一个），它也会拒绝，因为 `go get` 会写到工作区里，而 `go tool` 会遵循它：请改运行 `GOWORK=off go tool tifa upgrade`。
- 它从不转交给应用自己的 Tifa，所以在 Tifa 太旧、还没有这个命令的应用里也能用：用你安装的 Tifa 运行它（见[全局安装命令](create-app.zh-CN.md#installing-the-command-globally)），或者运行 `go run github.com/liustack/tifa/cmd/tifa@latest upgrade`。
- 版本按语义化版本比较，并且只比较精确版本。React 和 React DOM 可以比固定版本更新（`tifa build` 只需要 19.3.0 或更高），所以更新的版本由你自己保留。
- `Upgrading:` 说明是这个词之后的文字，直到它在更新日志里所在的列表项结束，所以条目要把它写在最后（见 [commit.md](../internals/commit.md#changelog)）。
- 它不问任何问题，也没有试运行。它可能改动的文件有 `go.mod`、`go.sum`、`package.json`、锁文件、`AGENTS.md` 和 `.claude/skills/tifa/`，所以 `git diff` 就是试运行。

当 `go get` 之后的某一步失败时（`pnpm install` 失败，或 `tifa check` 失败），命令会以那个失败的状态退出，并以一行结束：Go 已经在新版本了，后面的步骤没有完成。请修好原因，然后运行它打印的那一行 `cd <app> && go tool tifa upgrade --after <old version>`，它会把 `go mod tidy` 之后的步骤再做一遍。`--after` 接受应用原来的版本，写成 `v0.0.17` 这样。

如果某一步失败，错误会说明是哪一步，你可以手动做这些步骤：`go get -tool github.com/liustack/tifa/cmd/tifa@<version>`，`go mod tidy`，把 `package.json` 里固定的版本改成 `tifa new` 在那个版本里写的版本，安装，`go tool tifa agents`，读更新日志里的 `Upgrading:` 行，`go tool tifa check`。`go tool tifa upgrade --after <old version>` 会把 `go mod tidy` 之后的所有步骤再运行一遍。

## `tifa agents` {#tifa-agents}

```sh
tifa agents [--dir app-root] [--check] [--skills-dir .claude/skills/tifa]
```

根据应用所用的 Tifa 版本，写出编码 agent 在应用里要读的东西（`go tool tifa` 是用 `go.mod` 里的模块构建的，所以它里面的文件就是那个版本的）：

- Tifa skill，在 `.claude/skills/tifa/` 里：`SKILL.md` 和它的 `references/`。它们是 Tifa 仓库里 [`skills/tifa`](../../skills/tifa) 的文件，命令把它们带在自己里面，并多加了一行，说明是哪个 Tifa 写的这个文件，以及升级之后要再运行 `go tool tifa agents`。它里面的链接指向已安装版本的文档（`docs/testing-apps.md#testing`，在 `go tool tifa docs` 打印的文件夹里），而不是 GitHub。
- `AGENTS.md` 里标记行之间的部分（见 [AGENTS.md 里由 Tifa 管理的部分](#the-managed-block-of-agentsmd)）。
- `.github/workflows/ci.yml`，即 `tifa new` 写出的工作流，但只在应用没有 `.github/workflows/ci.yml` 也没有其他工作流文件时才写。它只写一次，从不替换，`--check` 也不要求它。用 yarn 或 bun 的应用会得到一条提示，因为这个工作流只有 pnpm 和 npm 的步骤。

`tifa new` 运行的是同一份代码，所以新应用两者都有。`go tool tifa upgrade` 在移动应用之后会运行它。要在不升级的情况下把两者都更新到你手头的版本，请运行 `go tool tifa agents`。没有别的东西会运行它：`tifa build` 不会，`tifa dev` 也不会。当 skill 或那一部分已经过期，并且应用里有其中之一时，`tifa check` 会在 `check passed` 之后打印一行，对两者都没有的应用则什么也不说。

```text
$ go tool tifa agents
AGENTS.md: refreshed the block between the markers
.claude/skills/tifa/SKILL.md: updated
.claude/skills/tifa/references/testing.md: updated
$ go tool tifa agents
AGENTS.md and .claude/skills/tifa are up to date (Tifa v0.0.11)
```

- `.claude/skills/tifa/` 里由 Tifa 写出的文件会被覆盖，即使你改过它也一样。请把你自己的规则放在 `AGENTS.md` 的标记之外，或放进你自己的文件。那个文件夹里不是 Tifa 写的文件会被放着不动。旧版 Tifa 写出而这个版本已经没有的文件会被删除。
- 再运行一次不会改变任何东西，连修改时间也不变。
- `--check` 不改变任何东西。它会点出每个已过期的文件并以状态 1 退出，这样 CI 任务可以据此失败。两者都是最新时以 0 退出。
- `--skills-dir` 是 skill 所在的文件夹，相对于应用根目录，用于从别处读取 skill 的 agent。
- 每个文件那一行里的版本，是命令构建时所用的版本：`Tifa v0.0.11`，在检出目录里构建或来自被替换模块的命令则是 `a development build of Tifa`。
- 它会从应用里读取这些说明要填入的值。模块路径来自 `go.mod`。根包是根目录里的 Go 文件声明的那个包，没有时，由模块路径决定，方式和 `tifa new` 一样。安装命令来自锁文件（`pnpm-lock.yaml`、`package-lock.json`、`yarn.lock`、`bun.lock`）。没有锁文件时，它保留 AGENTS.md 里已有的命令，没有 AGENTS.md 时，则像 `tifa new` 一样选择。这些猜测在命令写入时，每一个都会打印成一行 `note:`。
- 它不写 `CLAUDE.md`，`tifa new` 写的 `CLAUDE.md` 里有一行 `@AGENTS.md`。当应用没有它时，它会说明 Claude Code 读取的是 `CLAUDE.md`。

### AGENTS.md 里由 Tifa 管理的部分 {#the-managed-block-of-agentsmd}

`AGENTS.md` 里由 Tifa 写出的部分位于两行标记之间：

```md
# Notes for coding agents

<!-- tifa:begin -->
...
<!-- tifa:end -->

## This app
```

每个标记各占一行，只出现一次，并且 `tifa:begin` 在 `tifa:end` 之前。它们之间的一切都归 Tifa：命令、约定，以及那个 Tifa 版本里写给应用的文档索引：每个章节文件夹一行，列出其中文档的文件名。每篇文档讲什么不在里面，因为 agent 每次会话开始都会读完整个文件，这部分由 `tifa docs` 打印。索引来自命令自带的文档，所以总是和版本一致。`tifa agents` 会用当前所用 Tifa 版本的内容替换那一部分，文件的其他每一个字节都保持原样。关于应用本身的说明请放在标记之外（`tifa new` 用 `## This app` 开头）。

- 没有 `AGENTS.md` 时，它会写一个，包含标题和这一部分。
- 有 `AGENTS.md` 但没有标记时，它会把这一部分追加在末尾，并说明这一点。把标记加在这一部分周围，以后就能原地刷新它。
- 标记不是按顺序排列的一对，或没有各占一行时，它会以错误停止，既不写说明，也不写 skill。

## Tifa docs {#tifa-docs}

```sh
tifa docs [--dir app-root] [--all] [--when] [--lang zh-CN]
```

打印应用所用 Tifa 版本的文档在哪里，然后是它们的索引。默认列出用于编写应用的文档，每篇一行，包括路径和摘要，摘要说明这篇文档是什么，并点出它讲到的设置、命令和函数，总共约 5 KB。`--lang zh-CN` 列出中文文档，而不是英文文档。`--when` 会加上 `read_when` 行，`--all` 还会在各自的标题下列出关于开发 Tifa 本身的文档（发布、提交、测试、Tifa 内部如何工作、研究）。`tifa new` 写出的 `AGENTS.md` 里有应用文档的文件名，并告诉编码 agent 运行这个命令，这样 agent 读到的是应用构建所用版本的文档，而不是 GitHub 上 main 分支的文档。

Tifa 在应用根目录里向 go 命令查询（`go list -m -json github.com/liustack/tifa`），所以用的是 `go.mod` 解析出的那个文件夹：已发布版本对应模块缓存，`replace` 指令则对应它指向的文件夹。还没下载的模块会失败，并提示要运行的步骤 `go mod download github.com/liustack/tifa`。

```text
github.com/liustack/tifa v0.0.0 (replaced by ../..)
Docs: /home/me/tifa/docs
The paths below are relative to /home/me/tifa.

docs/build-and-deploy/build.md - Create, build, check and upgrade an app: `tifa new`, `build`, `check`, `gen`, `upgrade`, .tifa/, go.mod
docs/build-and-deploy/config.md - Every setting and where to set it: tifa.yaml, flags, environment, .env, server.csp, HTTPS, cross-origin writes
```

带 `--all --when` 时，索引使用 Tifa 仓库里 `scripts/docs-list.sh` 的格式：每篇文档一行，写着它的摘要，然后一行是它 front matter 里的 `read_when` 各项，用 ` | ` 连接。文档按路径排序。文档在它的 front matter 里说明自己是写给谁的，`audience: app` 或 `audience: dev`，没有这个字段的文档会让 `scripts/docs-list.sh --check` 失败。这个字段出现之前的 Tifa 版本，会把每篇文档都列为面向应用的。`docs/research/` 不在其中，没有摘要的文档也会被排除，并在 stderr 上给出一条警告。
