---
summary: 'A getting-started tutorial: start an app with `tifa new`, run it with `tifa dev`, add a page, a form with field errors, Layout data and tests, then build one executable and run it'
read_when:
  - Using Tifa for the first time and looking for a first walk through an app
  - Writing or checking the steps a new user follows from `tifa new` to a running executable
  - Changing the template of `tifa new` and checking that the tutorial still matches it
---

English · [简体中文](tutorial.zh-CN.md)

# Tutorial: a todo list in one executable

You start from the todo list that `tifa new` writes and make it your own: a page for finished todos, a form that renames a todo and shows field errors from Go, counts in the header, and tests. At the end you build one executable and run it. It takes 30 to 45 minutes. Each step teaches one thing, and the details live in the reference docs it links to.

You need Go 1.27, Node 20.19 or 22.12 and later, and [pnpm](https://pnpm.io). Node only builds the front end. The executable you ship never needs it. `tifa dev` does not run on Windows yet, so use WSL there.

## 1. Create the app

```sh
go run github.com/liustack/tifa/cmd/tifa@latest new tutorial-app
cd tutorial-app
go mod tidy
pnpm install
```

`tifa new` writes files only and prints the next steps. Keep the name `tutorial-app`, because the code below imports `tutorial-app/app/todos`. The folder name is the module path, and the root package is named `tutorialapp`. Names such as `todos` are refused, since the app imports a package of that name. See [`tifa new`](build.md#tifa-new).

## 2. Run it and look around

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

The first start compiles SQLite and the JavaScript engine, so it takes longer than this. Open http://127.0.0.1:9527 and try four things:

1. Press Add with an empty box. The message "Write what needs doing." appears under the box. Go sent it, see step 4.
2. Add "Buy milk". It shows up in the list at once. The todo links to its own page, `/todos/1`, where a button marks it done.
3. Open http://127.0.0.1:9527/nope and http://127.0.0.1:9527/todos/99. Both show a 404 page, the first from the root Layout and the second from the page of one todo.
4. Change the heading in `app/page.tsx` from `Todos` to `My todos` and save. The terminal shows what `tifa dev` did, and a reload shows the new heading. Change it back.

```text
tifa dev: 19:57:43 app/page.tsx modified
tifa dev: 19:57:44 bundle done in 721ms
tifa dev: 19:57:44 ready in 804ms
```

Every folder under `app/` is a URL, and the files in it say what the URL does:

| File | What it is |
| --- | --- |
| `app/route.go`, `app/page.tsx` | The page `/`. `Load` in Go returns the data, the page shows it. `POST` takes the form |
| `app/layout.tsx` | The root Layout around every page, with the nav and the `Error` that shows the 404 |
| `app/todos/todo.go` | The todos domain: the type and the SQLite queries, next to the pages that use it |
| `app/todos/todo+/` | `/todos/{todo}`. A folder that ends in `+` is a URL parameter, read with `c.Param("todo")` |
| `app/about/page.tsx` | A page that needs no data: `page.tsx` alone |
| `db.go`, `sessions.go`, `hooks.go`, `config.go` | At the root: what the whole app shares, such as the database and the store that keeps sessions in it |

See [Writing an app](app.md#folders) and [Structuring an app](structure.md).

### A write does not refresh the page

Look at `AddTodo` in `app/page.tsx`. After `POST` succeeds it calls `router.invalidate`:

```tsx
      await POST({ input: { title } })
      await router.invalidate({ sync: true })
```

This is the rule to remember: Tifa never reloads page data after a write on its own, so the page has to ask. Without that line the todo is saved but the list stays old until you reload. `router.invalidate` loads the page and its Layouts again in one request, and `sync: true` waits until the page shows the new data. The button on `/todos/1` that marks a todo done does the same. Every write you add needs the same line. See [Calling method handlers from the browser](app.md#calling-method-handlers-from-the-browser).

## 3. Add a page

A new folder is a new URL. Make `/done`, a list of finished todos.

First the query. In `app/todos/todo.go`, turn the body of `ListTodos` into a helper and add `ListDone` next to it:

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

The rest of the old `ListTodos` (from `defer rows.Close()` on) stays as the end of `queryTodos`.

Then the page. Create `app/done/route.go` and `app/done/page.tsx`:

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

In `app/layout.tsx`, add a link to the nav:

```diff
           <Link to="/">Todos</Link>
+          <Link to="/done">Done</Link>
           <Link to="/about">About</Link>
```

Save `page.tsx` before or together with `route.go`. A `route.go` with a `Load` and no `page.tsx` fails the build with `Load is not allowed without page.tsx`, and `tifa dev` prints that line until the page exists.

Open `/todos/1`, mark the todo done, then press Done in the nav. The todo is there. `page.useLoaderData()` is typed from what `Load` returns. Step 7 shows what happens when the Go type changes. See [route.go](app.md#routego) and [page.tsx](app.md#pagetsx).

## 4. A form with field errors

The home page form already shows a message from Go under its field. Add a second one that renames a todo, and share the rule between them.

In `app/todos/todo.go`, add the rule and the query:

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

`CheckTitle` needs `"unicode/utf8"` in the imports of that file. Then `app/route.go` can drop its own copy of the rule, and with it the `strings` and `unicode/utf8` imports:

```go
func (in AddInput) Validate() error {
	return todos.CheckTitle(in.Title)
}
```

A method handler takes a typed input after the `Ctx`. When the input has a `Validate` method, Tifa runs it before the handler, and `tifa.Fields` becomes a 422 answer with one message per field, named by its `json` tag. In `app/todos/todo+/route.go`, add a `PATCH`:

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

When you save, `tifa dev` regenerates `./route.gen`, and it now exports a `PATCH` function for the page. Use it in `app/todos/todo+/page.tsx`. Change the imports to bring in `FormEvent` and `PATCH`, add `<RenameForm />` under the error paragraph of `TodoPage`, and add the component:

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

On `/todos/1`, clear the box and press Save: "Write what needs doing." shows under it. Type "Buy oat milk" and press Save: the heading changes. The browser shows the 422 in its console, which is the answer working as designed.

A `tifa.Fields` reaches the browser as a `TifaError` whose `fields` hold the messages. Any other error from `Validate` is a 400 with its text in `message`. See [Method handlers with input and result](app.md#method-handlers-with-input-and-result).

## 5. Data in the Layout

A Layout can have its own Go data. Show how many todos are open and done in the nav, on every page.

Add the query to `app/todos/todo.go`:

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

A `Layout` function in the `route.go` of a folder gives the data of the Layout of that folder. The root folder's wraps every page. Add it to `app/route.go`:

```go
// Layout returns the data of the root Layout, which wraps every page: the numbers in the nav.
func Layout(c *tifa.Ctx) (todos.Counts, error) {
	return todos.Count(c.Context())
}
```

And read it in `app/layout.tsx`:

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

Add a todo and watch the nav: `Todos (1)`. Mark it done: `Todos (0)` and `Done (1)`. The numbers move because of the `router.invalidate` calls from step 2, which load the Layouts again too.

If `tifa dev` prints a long "Hydration failed" message right after you save `layout.tsx`, it came from the tab that was open during the change. Reload the page and it is gone. See [Layout](app.md#layout) and [Log](dev.md#log).

## 6. Tests

`Load`, `Layout` and the method handlers are plain Go functions, so you test them with `go test`. `tifatest.NewCtx` makes the `*tifa.Ctx` from an `httptest` request, and `tifa.Use` runs a `Load` or `Layout` the way the server does. The app already has tests for the todos, and each test gets a fresh SQLite database in a temporary folder (see `useTestDB`).

Add a test for the Layout to `app/route_test.go`, and `"tutorial-app/app/todos"` to its imports:

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

Add two tests for the rename to `app/todos/todo+/route_test.go`, which has `todoCtx` and `useTestDB` already:

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

And one for the new page, in `app/done/route_test.go`:

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

These tests check what a function returns. The 422 for a blank title, the JSON and the redirects come from the server, so you see them in the browser or against the built app. See [Testing](app.md#testing).

## 7. Check, build and run one file

Stop `tifa dev` with Ctrl-C. `tifa check` does everything `go test` does and more: it analyzes the routes, runs `go vet`, runs the tests and type-checks the TypeScript.

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

Break a convention to see the build speak up. Rename `app/done/page.tsx` to `page.tsx.bak` and run `go tool tifa check` again. It stops at the first step with `app/done/route.go:11:6: Load is not allowed without page.tsx`. Rename the file back. The types come from Go too: change the tag of `Title` in `app/todos/todo.go` from `json:"title"` to `json:"name"` and run the check again. The TypeScript step lists every place in React that still reads `title`, such as `app/page.tsx(37,15): error TS2339: Property 'title' does not exist on type '{ id: number; name: string; done: boolean; }'`. Change the tag back.

Now build:

```sh
go tool tifa build
./bin/tutorial-app
```

```text
built /path/to/tutorial-app/bin/tutorial-app (build 2e121916557701e0)
2026/11/04 19:53:48 INFO tifa: listening on http://127.0.0.1:9527
```

Open http://127.0.0.1:9527. Everything works as in `tifa dev`: the pages arrive as complete HTML, and the browser takes over after that. `bin/tutorial-app` holds the Go code, the pages, the front end files and the server-side renderer, about 32 MB. Node is not involved. `bin/` also holds `tifa-dev`, which `tifa dev` uses. You do not ship that one.

The program keeps its data in `app.db` in the folder it runs in, so run from the app folder and the todos you added in `tifa dev` are there. Change the port and the database with settings: `PORT=8080 DB_PATH=/var/data/todos.db ./bin/tutorial-app`. Stop it with Ctrl-C. It finishes the requests in flight first.

## Where to go next

- Put it on a server: [Deploying](deploy.md) covers building for Linux, running the program, containers, ports and HTTPS, and what to set behind a proxy or CDN.
- Settings, HTTPS, the cache of rendered pages: [Configuration](config.md).
- Sign users in: [Sessions](sessions.md). The app keeps sessions in `app.db` (`sessions.go`), so a restart signs nobody out.
- Styles, shadcn/ui, a site in several languages, caching a page: [Writing an app](app.md).
- A bigger app to read: [examples/shop](../examples/shop).
- How `tifa dev` reacts to each change, and how to read its log: [Dev server](dev.md).
- Why the folders look the way they do: [Structuring an app](structure.md) and [Philosophy](philosophy.md).
