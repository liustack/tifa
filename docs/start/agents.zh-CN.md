---
summary: '和编程 agent 一起开发 Tifa 应用：AGENTS.md、Tifa skill、Claude Code、Cursor、Codex、`tifa agents`'
audience: app
order: 5
translatedFrom: 60e828e28d5a
read_when:
  - '用编程 agent 新建 Tifa 应用，或者把一个早先建的应用交给 agent'
  - 'agent 好像不知道 Tifa 的约定'
  - '在 Tifa 应用上用 Codex 或 Gemini CLI'
---

# 和编程 agent 一起用 Tifa {#use-tifa-with-a-coding-agent}

## 新建应用 {#start-an-app}

```sh
go run github.com/liustack/tifa/cmd/tifa@latest new myapp
```

`tifa new` 往应用里写两样给编程 agent 的东西，都来自应用所用的 Tifa 版本：

- `AGENTS.md`：大多数编程 agent 会自己读的说明，讲约定、命令，以及这个版本的文档在哪里。`<!-- tifa:begin -->` 和 `<!-- tifa:end -->` 之间的部分由 Tifa 管理，你自己的说明写在它外面。
- `.claude/skills/tifa/` 里的 Tifa skill，讲得更细，并链到文档。

## 哪些 agent 读这个 skill {#which-agents-read-the-skill}

| Agent | 从哪里读 skill |
| --- | --- |
| Claude Code | `.claude/skills/` |
| Cursor | `.claude/skills/`（也读 `.cursor/skills/`） |
| GitHub Copilot | `.claude/skills/`（也读 `.github/skills/`、`.agents/skills/`） |
| Codex | `.agents/skills/` |
| Gemini CLI | `.agents/skills/`（也读 `.gemini/skills/`） |

用 Codex 或 Gemini CLI 时，把这个文件夹复制过去：

```sh
mkdir -p .agents/skills && cp -R .claude/skills/tifa .agents/skills/
```

升级 Tifa 之后要再复制一次，因为 Tifa 只写 `.claude/skills/tifa/`。

## 保持最新 {#keep-them-current}

`go tool tifa upgrade` 把应用升到新版 Tifa 时，会重新写 `AGENTS.md` 和 skill。`go tool tifa agents` 不升级也能做同样的事，应用里还没有这两样时也可以用。`go tool tifa agents --check` 什么都不改，两样不是最新时报错。见 [`tifa agents`](../build-and-deploy/build.zh-CN.md#tifa-agents)。

## agent 用的命令 {#commands-an-agent-uses}

- `go tool tifa docs` 列出应用所用 Tifa 版本的文档，每篇一行。读这些，不要读 GitHub 上的文档，那可能是别的版本。
- `go tool tifa dev --background` 启动开发服务器，等它能响应了再返回。`go tool tifa dev status`、`logs` 和 `stop` 用来查看和停止它。见 [智能体与脚本](../build-and-deploy/dev.zh-CN.md#agents-and-scripts)。
- `go tool tifa errors` 列出上次构建以来的错误，包括浏览器里的错误。见 [Tifa 错误](../build-and-deploy/dev.zh-CN.md#tifa-errors)。
- `go tool tifa check` 在 agent 收尾前跑完所有检查。

[tifago.com](https://tifago.com) 上的文档也有 Markdown 版本：在页面地址后加 `.md`，或者请求时带上 `Accept: text/markdown`。[/llms.txt](https://tifago.com/llms.txt) 列出了它们。
