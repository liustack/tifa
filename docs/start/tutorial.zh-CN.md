---
summary: '入门教程：`tifa new`、`tifa dev`、页面、表单、测试、一个可执行文件'
audience: app
order: 1
translatedFrom: f6746dec36ac
read_when:
  - '第一次用 Tifa，想从头跟着做一个应用'
  - '编写或检查新用户从 `tifa new` 到跑起可执行文件的步骤'
  - '修改 `tifa new` 的模板，并核对教程是否仍然对得上'
---

# 教程：一个可执行文件里的待办列表 {#tutorial-a-todo-list-in-one-executable}

你从 `tifa new` 写出的待办列表开始，把它改成自己的：加一个已完成待办的页面，加一个能重命名待办并显示 Go 返回的字段错误的表单，在页头显示计数，再写上测试。最后构建出一个可执行文件并运行它。全程需要 30 到 45 分钟。每一步只讲一件事，细节留给它链接到的参考文档。

你需要 Go 1.27、Node 20.19 或 22.12 及以上版本，以及 [pnpm](https://pnpm.io)。Node 只用来构建前端，你发布的可执行文件完全不需要它。`tifa new`、`tifa dev`、`tifa check` 和 `tifa build` 在 Windows 上同样能运行。

## 1. 创建应用 {#1-create-the-app}

```sh
go run github.com/liustack/tifa/cmd/tifa@latest new tutorial-app
cd tutorial-app
go mod tidy
pnpm install
```

在一台从没构建过 Tifa 应用的机器上，`go mod tidy`、`pnpm install` 以及第一次 `go tool tifa dev` 或 `go tool tifa build` 都要下载 Go 和 npm 包并编译 Tifa，可能要几分钟。之后的运行会用上缓存，只要几秒。

`pnpm install` 也会运行 `go tool tifa gen`（`package.json` 里的 `prepare` 脚本），它写出 `.tifa/` 里的类型，编辑器要读它们。请在这之后再打开编辑器：之前，对 `./route.gen` 的 import 会报错。

`tifa new` 只写文件，并打印接下来的步骤。请保留名字 `tutorial-app`，因为下面的代码会 import `tutorial-app/app/todos`。文件夹名就是模块路径，根包叫 `tutorialapp`。像 `todos` 这样的名字会被拒绝，因为应用会 import 同名的包。见 [`tifa new`](../build-and-deploy/create-app.zh-CN.md#tifa-new)。

## 2. 运行并四处看看 {#2-run-it-and-look-around}

```sh
go tool tifa dev
```

```text
tifa dev: serving http://127.0.0.1:9527
tifa dev: 19:48:19 static analysis done in 230ms
tifa dev: 19:48:19 route manifest changed
tifa dev: 19:48:19 code generation done in 5ms
tifa dev: 19:48:20 bundle done in 1.496s
tifa dev: 19:48:20 go build done in 103ms
tifa dev: 19:48:20 ready in 2.147s, open http://127.0.0.1:9527
```

第一次启动要编译 SQLite 和 JavaScript 引擎，所以比这里显示的更久。打开 http://127.0.0.1:9527，试四件事：

1. 输入框为空时按 Add。输入框下面会出现消息 "Write what needs doing."。这条消息是 Go 发来的，见第 4 步。
2. 添加 "Buy milk"。它马上出现在列表里。这个待办链接到它自己的页面 `/todos/1`，页面上有一个把它标为完成的按钮。
3. 打开 http://127.0.0.1:9527/nope 和 http://127.0.0.1:9527/todos/99。两个都显示 404 页面，前者来自根布局，后者来自单个待办的页面。
4. 把 `app/page.tsx` 里的标题从 `Todos` 改成 `My todos` 并保存。终端会显示 `tifa dev` 做了什么，已打开的页面会原地更新。刷新后，服务器渲染出来的也是新标题。再改回去。

```text
tifa dev: 19:57:43 app/page.tsx modified
tifa dev: 19:57:44 bundle done in 721ms
tifa dev: 19:57:44 ready in 804ms
```

`app/` 下的每个文件夹都是一个 URL，里面的文件说明这个 URL 做什么：

| 文件 | 它是什么 |
| --- | --- |
| `app/route.go`、`app/page.tsx` | 页面 `/`。Go 里的 `Load` 返回数据，页面把它显示出来。`POST` 接收表单 |
| `app/layout.tsx` | 包在每个页面外面的根布局，里面有导航栏和显示 404 的 `ErrorBoundary` |
| `app/todos/todo.go` | 待办业务域：类型和 SQLite 查询，放在用到它的页面旁边 |
| `app/todos/todo+/` | `/todos/{todo}`。以 `+` 结尾的文件夹是 URL 参数，用 `c.Param("todo")` 读取 |
| `app/about/page.tsx` | 不需要数据的页面：只有 `page.tsx` |
| `db.go`、`sessions.go`、`hooks.go`、`options.go` | 在根目录：整个应用共享的东西，比如数据库，以及把会话存在数据库里的存储 |

见[编写应用](app.zh-CN.md#folders)和[应用的结构](structure.zh-CN.md)。

### 写入不会刷新页面 {#a-write-does-not-refresh-the-page}

看一下 `app/page.tsx` 里的 `AddTodo`。`POST` 成功之后，它会调用 `router.invalidate`：

```tsx
      await POST({ input: { title } })
      await router.invalidate({ sync: true })
```

这是要记住的规则：Tifa 在写入之后从不自己重新加载页面数据，所以页面得自己去要。没有这一行，待办虽然保存了，但列表会一直是旧的，直到你刷新。`router.invalidate` 会在一次请求里重新加载页面和它的布局，`sync: true` 会等到页面显示出新数据为止。`/todos/1` 上把待办标为完成的按钮也是这么做的。你以后加的每个写入，都需要同样的一行。见[在浏览器里调用方法处理函数](../pages-and-data/handlers.zh-CN.md#calling-method-handlers-from-the-browser)。

## 3. 添加页面 {#3-add-a-page}

新建一个文件夹，就是新建一个 URL。来做 `/done`，一个已完成待办的列表。

先写查询。在 `app/todos/todo.go` 里，把 `ListTodos` 的函数体改成一个辅助函数，并在旁边加上 `ListDone`：

```go
// ListTodos returns every todo, open ones first, newest first.
func ListTodos(ctx context.Context) ([]Todo, error) {
	return queryTodos(ctx, `SELECT id, title, done FROM todos ORDER BY done, id DESC`)
}

// ListDone returns the todos that are done, newest first.
func ListDone(ctx context.Context) ([]Todo, error) {
	return queryTodos(ctx, `SELECT id, title, done FROM todos WHERE done ORDER BY id DESC`)
}

func queryTodos(ctx context.Context, query string) ([]Todo, error) {
	rows, err := tutorialapp.DB.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
```

旧 `ListTodos` 剩下的部分（从 `defer rows.Close()` 起）原样留作 `queryTodos` 的结尾。

然后是页面。创建 `app/done/route.go` 和 `app/done/page.tsx`：

```go
// Package done is the page of the todos that are done, at /done.
package done

import (
	"github.com/liustack/tifa"

	"tutorial-app/app/todos"
)

// Load returns the todos that are done.
func Load(c *tifa.Ctx) ([]todos.Todo, error) {
	return todos.ListDone(c.Context())
}
```

```tsx
import { Link } from '@tanstack/react-router'
import { page } from './route.gen'

export default function DonePage() {
  const done = page.useLoaderData()
  return (
    <>
      <h1>Done</h1>
      {done.length === 0 ? (
        <p>Nothing is done yet.</p>
      ) : (
        <ul className="todos">
          {done.map((todo) => (
            <li key={todo.id} className="done">
              <Link to="/todos/$todo" params={{ todo: String(todo.id) }}>
                {todo.title}
              </Link>
            </li>
          ))}
        </ul>
      )}
    </>
  )
}

export const head = () => ({
  meta: [{ title: 'Done · tutorial-app' }],
})
```

在 `app/layout.tsx` 里，给导航栏加一个链接：

```diff
           <Link to="/">Todos</Link>
+          <Link to="/done">Done</Link>
           <Link to="/about">About</Link>
```

先保存 `page.tsx`，或者和 `route.go` 一起保存。只有 `Load` 而没有 `page.tsx` 的 `route.go` 会让构建失败，报 `Load is not allowed without page.tsx`，在页面出现之前，`tifa dev` 会一直打印这一行。

打开 `/todos/1`，把待办标为完成，然后点导航栏里的 Done。这个待办就在那里。`page.useLoaderData()` 的类型来自 `Load` 返回的内容。第 7 步会展示 Go 类型改变时会发生什么。见 [route.go](../pages-and-data/loading-data.zh-CN.md#routego) 和 [page.tsx](../pages-and-data/loading-data.zh-CN.md#pagetsx)。

## 4. 带字段错误的表单 {#4-a-form-with-field-errors}

首页的表单已经会在字段下面显示 Go 返回的消息。再加第二个表单来重命名待办，并让两者共用同一条规则。

在 `app/todos/todo.go` 里加上规则和查询：

```go
// CheckTitle returns a tifa.Fields error when title is empty or too long for a todo, and nil
// otherwise. Every input that carries a title calls it from its Validate.
func CheckTitle(title string) error {
	switch n := utf8.RuneCountInString(strings.TrimSpace(title)); {
	case n == 0:
		return tifa.Fields{"title": "Write what needs doing."}
	case n > 200:
		return tifa.Fields{"title": "Keep it under 200 characters."}
	}
	return nil
}
```

```go
// Rename gives the todo id a new title, without the spaces around it, and returns it, or
// tifa.ErrNotFound.
func Rename(ctx context.Context, id int64, title string) (Todo, error) {
	t := Todo{ID: id}
	err := tutorialapp.DB.QueryRowContext(ctx, `UPDATE todos SET title = ? WHERE id = ? RETURNING title, done`, strings.TrimSpace(title), id).Scan(&t.Title, &t.Done)
	if errors.Is(err, sql.ErrNoRows) {
		return Todo{}, tifa.ErrNotFound
	}
	return t, err
}
```

`CheckTitle` 需要在那个文件的 import 里加上 `"unicode/utf8"`。然后 `app/route.go` 就可以删掉自己那份规则的副本，连同 `strings` 和 `unicode/utf8` 这两个 import：

```go
func (in AddInput) Validate() error {
	return todos.CheckTitle(in.Title)
}
```

方法处理函数在 `Ctx` 之后接收一个有类型的输入。当输入有 `Validate` 方法时，Tifa 会在处理函数之前运行它，而 `tifa.Fields` 会变成一个 422 响应，每个字段一条消息，字段名取自它的 `json` 标签。在 `app/todos/todo+/route.go` 里加一个 `PATCH`：

```go
// RenameInput is the body of PATCH.
type RenameInput struct {
	Title string `json:"title"`
}

// Validate runs before PATCH and gives the same messages as the form that adds a todo.
func (in RenameInput) Validate() error {
	return todos.CheckTitle(in.Title)
}

// PATCH gives the todo a new title and answers with it.
func PATCH(c *tifa.Ctx, in RenameInput) (todos.Todo, error) {
	id, err := todoID(c)
	if err != nil {
		return todos.Todo{}, err
	}
	return todos.Rename(c.Context(), id, in.Title)
}
```

保存后，`tifa dev` 会重新生成 `./route.gen`，它现在会为页面导出一个 `PATCH` 函数。在 `app/todos/todo+/page.tsx` 里使用它。修改 import，引入 `FormEvent` 和 `PATCH`，在 `TodoPage` 的错误段落下面加上 `<RenameForm />`，再加上这个组件：

```tsx
import { useState, type FormEvent } from 'react'
import { Link, useRouter } from '@tanstack/react-router'
import { TifaError } from 'tifa/client'
import { ErrorBoundary as RootErrorBoundary } from '../../layout'
import { page, PATCH, POST, type PageData } from './route.gen'
```

```tsx
      {error && (
        <p role="alert" className="error">
          {error}
        </p>
      )}
      <RenameForm />
    </article>
```

```tsx
function RenameForm() {
  const todo = page.useLoaderData()
  const params = page.useParams()
  const router = useRouter()
  const [title, setTitle] = useState(todo.title)
  const [errors, setErrors] = useState<Record<string, string>>({})
  async function rename(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    try {
      await PATCH({ params, input: { title } })
      await router.invalidate({ sync: true })
      setErrors({})
    } catch (error) {
      if (!(error instanceof TifaError)) throw error
      setErrors(Object.keys(error.fields).length > 0 ? error.fields : { form: error.message })
    }
  }
  return (
    <form onSubmit={rename} noValidate>
      <label htmlFor="rename">Rename</label>
      <input id="rename" name="title" value={title} onChange={(event) => setTitle(event.target.value)} />
      <button type="submit">Save</button>
      {errors.title && <p className="error">{errors.title}</p>}
      {errors.form && (
        <p role="alert" className="error">
          {errors.form}
        </p>
      )}
    </form>
  )
}
```

在 `/todos/1` 上，清空输入框并按 Save：下面会显示 "Write what needs doing."。输入 "Buy oat milk" 再按 Save：标题会变。浏览器的控制台里会显示那个 422，这是响应按设计在工作。

`tifa.Fields` 到达浏览器时是一个 `TifaError`，它的 `fields` 里是各条消息。`Validate` 返回的其他任何错误，和处理函数返回的一样算应用自己出的错：答 500，文字只进日志。见[带输入和结果的方法处理函数](../pages-and-data/handlers.zh-CN.md#method-handlers-with-input-and-result)。

## 5. 布局里的数据 {#5-data-in-the-layout}

布局可以有自己的 Go 数据。在每个页面的导航栏里，显示有多少待办未完成、多少已完成。

把查询加进 `app/todos/todo.go`：

```go
// Counts is how many todos are open and how many are done.
type Counts struct {
	Open int `json:"open"`
	Done int `json:"done"`
}

// Count returns the number of open and done todos.
func Count(ctx context.Context) (Counts, error) {
	var n Counts
	err := tutorialapp.DB.QueryRowContext(ctx, `SELECT COALESCE(SUM(NOT done), 0), COALESCE(SUM(done), 0) FROM todos`).Scan(&n.Open, &n.Done)
	return n, err
}
```

文件夹的 `route.go` 里的 `Layout` 函数，给出这个文件夹的布局的数据。根文件夹的布局包着每个页面。把它加进 `app/route.go`：

```go
// Layout returns the data of the root Layout, which wraps every page: the numbers in the nav.
func Layout(c *tifa.Ctx) (todos.Counts, error) {
	return todos.Count(c.Context())
}
```

然后在 `app/layout.tsx` 里读取它：

```diff
 import { TifaError } from 'tifa/client'
+import { layout } from './route.gen'
 import './styles.css'
```

```diff
 export default function Root() {
+  const counts = layout.useLoaderData()
   return (
```

```diff
-          <Link to="/">Todos</Link>
-          <Link to="/done">Done</Link>
+          <Link to="/">Todos ({counts.open})</Link>
+          <Link to="/done">Done ({counts.done})</Link>
```

添加一个待办，看导航栏：`Todos (1)`。把它标为完成：`Todos (0)` 和 `Done (1)`。数字会变，是因为第 2 步里的 `router.invalidate` 调用，它们也会重新加载布局。

如果在你保存 `layout.tsx` 之后，`tifa dev` 马上打印出一长段 "Hydration failed" 消息，那是改动期间开着的那个标签页引起的。刷新页面就没了。见[布局](../pages-and-data/layouts.zh-CN.md#the-layout-of-a-folder)和[日志](../build-and-deploy/dev-errors.zh-CN.md#log)。

## 6. 测试 {#6-tests}

`Load`、`Layout` 和方法处理函数都是普通的 Go 函数，所以用 `go test` 来测试它们。`tifatest.NewCtx` 从一个 `httptest` 请求生成 `*tifa.Ctx`，`tifa.Use` 按服务器的方式运行一个 `Load` 或 `Layout`。应用里已经有待办的测试，每个测试都会在临时文件夹里得到一个全新的 SQLite 数据库（见 `useTestDB`）。

给布局加一个测试，放进 `app/route_test.go`，并把 `"tutorial-app/app/todos"` 加进它的 import：

```go
func TestLayoutCountsOpenAndDoneTodos(t *testing.T) {
	useTestDB(t)
	c, _ := tifatest.NewCtx(t, httptest.NewRequest("GET", "/", nil))
	for _, title := range []string{"Buy milk", "Call mum", "Pay rent"} {
		if _, err := POST(c, AddInput{Title: title}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := todos.SetDone(c.Context(), 1, true); err != nil {
		t.Fatal(err)
	}
	got, err := tifa.Use(c, Layout)
	if want := (todos.Counts{Open: 2, Done: 1}); err != nil || got != want {
		t.Fatalf("Layout: %+v, %v, want %+v", got, err, want)
	}
}
```

给重命名加两个测试，放进 `app/todos/todo+/route_test.go`，那里已经有 `todoCtx` 和 `useTestDB`：

```go
func TestPATCHRenamesTheTodo(t *testing.T) {
	useTestDB(t)
	if _, err := todos.AddTodo(context.Background(), "Buy milk"); err != nil {
		t.Fatal(err)
	}
	if got, err := PATCH(todoCtx(t, "PATCH", "1"), RenameInput{Title: " Buy oat milk "}); err != nil || got.Title != "Buy oat milk" {
		t.Fatalf("PATCH: %+v, %v", got, err)
	}
	if _, err := PATCH(todoCtx(t, "PATCH", "2"), RenameInput{Title: "Water the plants"}); !errors.Is(err, tifa.ErrNotFound) {
		t.Fatalf("PATCH of a missing todo: %v", err)
	}
}

func TestRenameInputNeedsATitle(t *testing.T) {
	var fields tifa.Fields
	err := RenameInput{Title: "  "}.Validate()
	if !errors.As(err, &fields) || fields["title"] == "" {
		t.Fatalf("Validate of a blank title: %v, want a message for title", err)
	}
	if err := (RenameInput{Title: "Buy oat milk"}).Validate(); err != nil {
		t.Fatalf("Validate of a title: %v", err)
	}
}
```

再给新页面加一个，放进 `app/done/route_test.go`：

```go
package done

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/liustack/tifa"
	"github.com/liustack/tifa/tifatest"

	"tutorial-app"
	"tutorial-app/app/todos"
)

// useTestDB gives the test a fresh, empty database.
func useTestDB(t *testing.T) {
	t.Helper()
	db, err := tutorialapp.OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	tutorialapp.DB = db
	t.Cleanup(func() { db.Close() })
}

func TestLoadListsOnlyTheDoneTodos(t *testing.T) {
	useTestDB(t)
	ctx := context.Background()
	for _, title := range []string{"Buy milk", "Call mum"} {
		if _, err := todos.AddTodo(ctx, title); err != nil {
			t.Fatal(err)
		}
	}
	finished, err := todos.SetDone(ctx, 2, true)
	if err != nil {
		t.Fatal(err)
	}

	c, _ := tifatest.NewCtx(t, httptest.NewRequest("GET", "/done", nil))
	got, err := tifa.Use(c, Load)
	if err != nil || len(got) != 1 || got[0] != finished {
		t.Fatalf("Load: %+v, %v, want [%+v]", got, err, finished)
	}
}
```

```sh
go test ./...
```

```text
ok  	tutorial-app	0.638s
ok  	tutorial-app/app	0.333s
ok  	tutorial-app/app/done	0.961s
ok  	tutorial-app/app/todos	1.611s
ok  	tutorial-app/app/todos/todo+	1.302s
```

这些测试检查的是函数返回了什么。空标题的 422、JSON 和重定向都来自服务器，所以要在浏览器里或对构建出的应用才能看到。见[测试](../pages-and-data/testing-apps.zh-CN.md#testing)。

## 7. 检查、构建并运行一个文件 {#7-check-build-and-run-one-file}

用 Ctrl-C 停掉 `tifa dev`。`tifa check` 会做 `go test` 做的所有事，还更多：它分析路由、运行 `go vet`、运行测试，并对 TypeScript 做类型检查。

```sh
go tool tifa check
```

```text
==> analyze
==> generate
==> go vet ./...
==> go test ./...
ok  	tutorial-app	0.362s
ok  	tutorial-app/app	0.979s
ok  	tutorial-app/app/done	0.676s
ok  	tutorial-app/app/todos	1.294s
ok  	tutorial-app/app/todos/todo+	1.614s
==> typescript (tsc --noEmit)
check passed
```

故意破坏一个约定，看构建怎么提醒你。把 `app/done/page.tsx` 改名为 `page.tsx.bak`，再运行一次 `go tool tifa check`。它会在第一步就停下，报 `app/done/route.go:11:6: Load is not allowed without page.tsx`。把文件名改回来。类型也来自 Go：把 `app/todos/todo.go` 里 `Title` 的标签从 `json:"title"` 改成 `json:"name"`，再运行一次检查。TypeScript 那一步会列出 React 里每个仍然读取 `title` 的地方，比如 `app/page.tsx(37,15): error TS2339: Property 'title' does not exist on type '{ id: number; name: string; done: boolean; }'`。再把标签改回去。

现在构建：

```sh
go tool tifa build
./bin/tutorial-app
```

```text
built /path/to/tutorial-app/bin/tutorial-app (build 2e121916557701e0)
2026/11/04 19:53:48 INFO tifa: listening on http://127.0.0.1:9527
```

打开 http://127.0.0.1:9527。一切都和 `tifa dev` 里一样：页面以完整的 HTML 送达，之后由浏览器接手。`bin/tutorial-app` 里有 Go 代码、页面、前端文件和服务端渲染器，大约 35 MB。整个过程不涉及 Node。`bin/` 里还有 `tifa-dev`，是 `tifa dev` 用的，这个文件不用发布。

程序把数据存在它运行所在文件夹里的 `app.db` 中，所以从应用文件夹运行，你在 `tifa dev` 里添加的待办就在那里。端口和数据库可以用设置来改：`PORT=8080 DB_PATH=/var/data/todos.db ./bin/tutorial-app`。用 Ctrl-C 停止它。它会先把正在处理的请求做完。

## 接下来去哪 {#where-to-go-next}

- 放到服务器上：[用 tifa deploy 部署](../build-and-deploy/deploy.zh-CN.md)，或者[手动部署](../build-and-deploy/deploy-by-hand.zh-CN.md)，后者讲了为 Linux 构建、运行程序、容器、端口和 HTTPS。放在代理或 CDN 后面时要设置什么：[在前面放 nginx 或 CDN](../build-and-deploy/deploy-proxy.zh-CN.md)。
- 设置、HTTPS、已渲染页面的缓存：[配置](../build-and-deploy/config.zh-CN.md)。
- 让用户登录：[会话](../pages-and-data/sessions.zh-CN.md)。应用把会话存在 `app.db`（`sessions.go`）里，所以重启不会让任何人退出登录。
- 样式和 shadcn/ui：[样式](../pages-and-data/styles.zh-CN.md)。多语言站点：[编写应用](app.zh-CN.md#a-site-in-several-languages)。缓存页面：[渲染开关和缓存](../pages-and-data/ssr-and-cache.zh-CN.md#cache-for-cdns)。
- 一个可以读的更大的应用：[examples/shop](../../examples/shop)。
- `tifa dev` 如何对每次改动做出反应：[每种改动触发什么](../build-and-deploy/dev-reloads.zh-CN.md)。如何读它的日志：[日志](../build-and-deploy/dev-errors.zh-CN.md#log)。
- 文件夹为什么是这个样子：[应用的结构](structure.zh-CN.md)和[设计理念](philosophy.zh-CN.md)。
