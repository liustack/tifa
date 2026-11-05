---
summary: '入门教程：用 `tifa new` 新建应用，用 `tifa dev` 运行，再加一个页面、一个带字段错误的表单、Layout 数据和测试，最后构建出一个可执行文件并运行'
read_when:
  - 第一次用 Tifa，想找一份带着走一遍应用的入门材料
  - 编写或核对新用户从 `tifa new` 到跑起可执行文件的步骤
  - 改 `tifa new` 的模板，要确认教程还对得上
---

[English](tutorial.md) · 简体中文

# 教程：一个待办清单，一个可执行文件

你从 `tifa new` 写出的待办清单开始，把它改成自己的：加一个已完成待办的页面，加一个由 Go 返回字段错误的改名表单，在页头显示数量，再写测试。最后构建出一个可执行文件并运行。全程 30 到 45 分钟。每一步只讲一件事，细节放在它链接的参考文档里。

你需要 Go 1.27、Node 20.19 或 22.12 及以上，以及 [pnpm](https://pnpm.io)。Node 只用来构建前端，你要发布的可执行文件不需要它。`tifa dev` 暂不支持 Windows，请用 WSL。

## 1. 新建应用

```sh
go run github.com/liustack/tifa/cmd/tifa@latest new tutorial-app
cd tutorial-app
go mod tidy
pnpm install
```

`tifa new` 只写文件，并打印后续步骤。请保持 `tutorial-app` 这个名字，因为下面的代码会导入 `tutorial-app/app/todos`。目录名就是模块路径，根包叫 `tutorialapp`。像 `todos` 这样的名字会被拒绝，因为应用会导入同名的包。详见 [`tifa new`](build.zh-CN.md#tifa-new)。

## 2. 运行并四处看看

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

第一次启动要编译 SQLite 和 JavaScript 引擎，会比这里慢。打开 http://127.0.0.1:9527，试四件事：

1. 输入框留空，点 Add。框下面出现 "Write what needs doing."。这句话是 Go 发来的，见第 4 步。
2. 添加 "Buy milk"。它立刻出现在列表里。这条待办链接到它自己的页面 `/todos/1`，页面上有个按钮可以标记完成。
3. 打开 http://127.0.0.1:9527/nope 和 http://127.0.0.1:9527/todos/99。两个都显示 404 页面，前者来自根 Layout，后者来自单个待办的页面。
4. 把 `app/page.tsx` 里的标题从 `Todos` 改成 `My todos` 并保存。终端会显示 `tifa dev` 做了什么，刷新后看到新标题。再改回来。

```text
tifa dev: 19:57:43 app/page.tsx modified
tifa dev: 19:57:44 bundle done in 721ms
tifa dev: 19:57:44 ready in 804ms
```

`app/` 下的每个目录是一个 URL，目录里的文件决定这个 URL 做什么：

| 文件 | 作用 |
| --- | --- |
| `app/route.go`、`app/page.tsx` | 页面 `/`。Go 的 `Load` 返回数据，页面负责显示。`POST` 接收表单 |
| `app/layout.tsx` | 包住所有页面的根 Layout，含导航和显示 404 的 `Error` |
| `app/todos/todo.go` | 待办这个域：类型和 SQLite 查询，与用到它们的页面放在一起 |
| `app/todos/todo+/` | `/todos/{todo}`。以 `+` 结尾的目录是 URL 参数，用 `c.Param("todo")` 读取 |
| `app/about/page.tsx` | 不需要数据的页面：只有 `page.tsx` |
| `db.go`、`sessions.go`、`hooks.go`、`config.go` | 放在根目录：整个应用共用的东西，比如数据库，以及把会话存进数据库的存储 |

详见[编写应用](app.zh-CN.md#目录)和[应用的代码结构](structure.zh-CN.md)。

### 写入不会自动刷新页面

看 `app/page.tsx` 里的 `AddTodo`。`POST` 成功之后它调用了 `router.invalidate`：

```tsx
      await POST({ input: { title } })
      await router.invalidate({ sync: true })
```

这是要记住的规则：Tifa 在写入之后不会自己重新加载页面数据，要由页面来要求。没有这一行，待办虽然保存了，列表却要刷新页面才会更新。`router.invalidate` 用一次请求把当前页面和它的各层 Layout 的数据重新加载，`sync: true` 会等到页面显示了新数据再返回。`/todos/1` 上标记完成的按钮也是这样做的。以后你加的每个写入操作都要有同样的一行。详见[在浏览器里调用方法函数](app.zh-CN.md#在浏览器里调用方法函数)。

## 3. 加一个页面

新目录就是新 URL。做一个 `/done`，列出已完成的待办。

先写查询。在 `app/todos/todo.go` 里，把 `ListTodos` 的函数体变成一个辅助函数，再在旁边加上 `ListDone`：

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

旧 `ListTodos` 剩下的部分（从 `defer rows.Close()` 起）原样留在 `queryTodos` 的末尾。

再写页面。新建 `app/done/route.go` 和 `app/done/page.tsx`：

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

在 `app/layout.tsx` 的导航里加一个链接：

```diff
           <Link to="/">Todos</Link>
+          <Link to="/done">Done</Link>
           <Link to="/about">About</Link>
```

请先保存 `page.tsx`，或与 `route.go` 一起保存。只有带 `Load` 的 `route.go` 而没有 `page.tsx` 时，构建会失败并报 `Load is not allowed without page.tsx`，页面建好之前 `tifa dev` 会一直打印这一行。

打开 `/todos/1`，把待办标记为完成，再点导航里的 Done。这条待办就在那里。`page.useLoaderData()` 的类型来自 `Load` 的返回值。Go 的类型改了会怎样，第 7 步会演示。详见 [route.go](app.zh-CN.md#routego) 和 [page.tsx](app.zh-CN.md#pagetsx)。

## 4. 带字段错误的表单

首页的表单已经能在输入框下面显示来自 Go 的消息。再加第二个表单来给待办改名，并让两个表单共用同一条规则。

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

`CheckTitle` 需要在这个文件的 import 里加上 `"unicode/utf8"`。然后 `app/route.go` 就可以删掉自己那份规则，连同 `strings` 和 `unicode/utf8` 两个 import：

```go
func (in AddInput) Validate() error {
	return todos.CheckTitle(in.Title)
}
```

方法函数在 `Ctx` 之后可以带一个有类型的输入。输入类型有 `Validate` 方法时，Tifa 会在处理函数之前运行它，`tifa.Fields` 会变成 422 响应，每个字段一条消息，字段名取自 `json` 标签。在 `app/todos/todo+/route.go` 里加一个 `PATCH`：

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

保存后，`tifa dev` 会重新生成 `./route.gen`，它现在给这个页面多导出了一个 `PATCH` 函数。在 `app/todos/todo+/page.tsx` 里使用它。把 import 改成引入 `FormEvent` 和 `PATCH`，在 `TodoPage` 的错误段落下面加上 `<RenameForm />`，再加上这个组件：

```tsx
import { useState, type FormEvent } from 'react'
import { Link, useRouter } from '@tanstack/react-router'
import { RouteError, TifaError } from 'tifa/client'
import { Error as RootError } from '../../layout'
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

在 `/todos/1` 上清空输入框再点 Save，框下面出现 "Write what needs doing."。输入 "Buy oat milk" 再点 Save，标题随之改变。浏览器控制台会显示那个 422，这正是这个响应按设计在工作。

`tifa.Fields` 到浏览器里是一个 `TifaError`，它的 `fields` 里是各条消息。`Validate` 返回的其他错误是 400，文字在 `message` 里。详见[带入参和返回值的方法函数](app.zh-CN.md#带入参和返回值的方法函数)。

## 5. Layout 里的数据

Layout 可以有自己的 Go 数据。在每个页面的导航里显示还有几条待办、已完成几条。

先把查询加到 `app/todos/todo.go`：

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

目录的 `route.go` 里的 `Layout` 函数给这个目录的 Layout 提供数据。根目录的 Layout 包住所有页面。把它加到 `app/route.go`：

```go
// Layout returns the data of the root Layout, which wraps every page: the numbers in the nav.
func Layout(c *tifa.Ctx) (todos.Counts, error) {
	return todos.Count(c.Context())
}
```

再在 `app/layout.tsx` 里读取：

```diff
 import { RouteError } from 'tifa/client'
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

添加一条待办，看导航：`Todos (1)`。把它标记完成：`Todos (0)` 和 `Done (1)`。数字能跟着变，靠的是第 2 步加的 `router.invalidate`，它会把各层 Layout 一起重新加载。

如果保存 `layout.tsx` 之后 `tifa dev` 立刻打印一长段 "Hydration failed"，那是改动发生时还开着的那个标签页引起的。刷新页面就没了。详见 [Layout](app.zh-CN.md#layout) 和[日志](dev.zh-CN.md#日志)。

## 6. 测试

`Load`、`Layout` 和方法函数都是普通的 Go 函数，所以用 `go test` 测它们。`tifatest.NewCtx` 把 `httptest` 请求变成 `*tifa.Ctx`，`tifa.Use` 按服务器的规则运行 `Load` 或 `Layout`。应用里已经有待办的测试，每个测试在临时目录里得到一个全新的 SQLite 数据库（见 `useTestDB`）。

在 `app/route_test.go` 里加一个 Layout 的测试，并在它的 import 里加上 `"tutorial-app/app/todos"`：

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

在 `app/todos/todo+/route_test.go` 里为改名加两个测试，这个文件里已经有 `todoCtx` 和 `useTestDB`：

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

再为新页面在 `app/done/route_test.go` 里写一个：

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

这些测试检查的是函数返回了什么。空标题得到的 422、JSON 和重定向是服务器做的，要在浏览器里或对构建出的应用去看。详见[测试](app.zh-CN.md#测试)。

## 7. 检查、构建、运行一个文件

用 Ctrl-C 停掉 `tifa dev`。`tifa check` 做的事比 `go test` 多：分析路由、运行 `go vet`、运行测试、检查 TypeScript 类型。

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

故意破坏一条约定，看构建怎么开口。把 `app/done/page.tsx` 改名为 `page.tsx.bak`，再运行 `go tool tifa check`。它在第一步就停下，报 `app/done/route.go:11:6: Load is not allowed without page.tsx`。把文件名改回来。类型也来自 Go：把 `app/todos/todo.go` 里 `Title` 的标签从 `json:"title"` 改成 `json:"name"`，再运行一次检查。TypeScript 那一步会列出 React 里每个仍在读 `title` 的地方，例如 `app/page.tsx(37,15): error TS2339: Property 'title' does not exist on type '{ id: number; name: string; done: boolean; }'`。再把标签改回来。

现在构建：

```sh
go tool tifa build
./bin/tutorial-app
```

```text
built /path/to/tutorial-app/bin/tutorial-app (build 2e121916557701e0)
2026/11/04 19:53:48 INFO tifa: listening on http://127.0.0.1:9527
```

打开 http://127.0.0.1:9527。一切和 `tifa dev` 里一样：页面以完整的 HTML 送达，之后由浏览器接手。`bin/tutorial-app` 里有 Go 代码、页面、前端文件和服务端渲染器，大约 32 MB，全程用不到 Node。`bin/` 里还有一个 `tifa-dev`，是 `tifa dev` 用的，不用发布它。

程序把数据放在它运行所在目录的 `app.db` 里，所以从应用目录运行，你在 `tifa dev` 里加的待办还在。端口和数据库用设置来改：`PORT=8080 DB_PATH=/var/data/todos.db ./bin/tutorial-app`。用 Ctrl-C 停止它，它会先让正在处理的请求结束。

## 接下来看什么

- 放到服务器上：[部署](deploy.zh-CN.md)讲了为 Linux 构建、运行程序、容器、端口和 HTTPS，以及放在代理或 CDN 后面要设置什么。
- 设置、HTTPS、渲染页面的缓存：[配置](config.zh-CN.md)。
- 让用户登录：[会话](sessions.zh-CN.md)。应用把会话存在 `app.db` 里（`sessions.go`），所以重启不会让任何人登出。
- 样式、shadcn/ui、多语言站点、缓存页面：[编写应用](app.zh-CN.md)。
- 一个更大的应用可以读：[examples/shop](../examples/shop)。
- `tifa dev` 对每种改动怎么反应、日志怎么读：[开发服务器](dev.zh-CN.md)。
- 目录为什么是这个样子：[应用的代码结构](structure.zh-CN.md)和[设计理念](philosophy.zh-CN.md)。
