# tifa（给 AI agent 的项目说明）

## 目标

tifa 是一个 Go 全栈框架：Go 负责路由、取数、缓存和后台任务，React 页面由进程内的纯 Go QuickJS 渲染成完整 HTML，浏览器由 TanStack Router 接管。目标是在低配 VPS 或容器上一个进程跑完整个应用，不需要 Node 服务、进程管理器、反向代理和 Redis。

## 硬约束

- 纯 Go，`CGO_ENABLED=0`，交付单个可执行文件。不引入 cgo 版 QuickJS、V8 或 Node 旁路进程。
- SSR 用预热的 QuickJS VM 池，输出完整 HTML，不做流式渲染。数据由 Go 在渲染前取齐注入，渲染期间没有 I/O，没有 RSC。
- 单次渲染比 Node 慢是接受的代价。性能讨论只谈在这个前提下怎么提速、怎么提高整个进程的吞吐。
- 少魔法：包级函数和全局变量，不做依赖注入，不定义多余的结构体。构建期只做静态分析，不执行用户代码。
- 构建（`tifa build`、`tifa dev`）不需要 Node，Node 只用来装 npm 依赖和可选的类型检查。

## 仓库现状

示例应用 `examples/shop` 是按 tifa 约定写的店铺订单工作台，独立的 Go 模块（`github.com/liustack/tifa/examples/shop`），`go.mod` 用 `replace` 指向仓库根目录，并把 `cmd/tifa` 登记为工具，根模块不引用它。`test/e2e` 构建并检查它，`internal/ssr` 的真实包测试用它的构建产物，改框架行为时拿它验收。原型 go-quick 留在标签 `prototype-baseline`，性能对比都从这个标签构建。

## 文档（`docs/`）

1. 每篇文档开头写 front matter：`summary`（一句话说明这篇回答什么问题）与 `read_when`（什么情况下该读）。格式见 [scripts/docs-list.sh](scripts/docs-list.sh) 开头的注释。
2. 开工前和新建文档前先跑 `scripts/docs-list.sh` 看索引，按 Read when 读相关文档。提交前可以跑 `scripts/docs-list.sh --check`，缺 summary 的文档会让它失败。
3. `docs/archive/`、`docs/research/` 不进索引，放过时或调研性质的材料。
4. 文档写给读者用：写结论和做法，不写推理过程与工作记录。中文行文不用分号，不用破折号作标点。

## 草稿目录

计划、设计草稿、实验记录、测量原始数据放在仓库草稿目录 `.issues/<YYYY-MM-DD-主题>/`，同一任务的文件放同一目录。`.issues/` 不入库（写在 `.git/info/exclude`），入库的代码、文档、脚本不得引用其中内容。结论定下来、需要长期保留的，整理后写进 `docs/`。

## 验证与提交

- 验证命令见 [docs/testing.md](docs/testing.md)，提交规范见 [docs/commit.md](docs/commit.md)。
- 新行为先写失败的测试再实现。遇到 bug 先定位根因再改，不用兜底值掩盖。
