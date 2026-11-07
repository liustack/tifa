---
summary: 'How to keep data in a Tifa app with SQLite: the pure Go driver, opening the database in OnStart, where the file lives and how to set its path, changing the tables with numbered migrations that run at start, using it in Load and handlers, a transaction, a backup, testing on a temporary database, and when to move to Postgres'
read_when:
  - Adding a database to a Tifa app, or growing the one `tifa new` wrote
  - Changing a table of an app that is already running
  - Deciding between SQLite and Postgres for a Tifa app
  - Writing Go tests for code that reads or writes the database
---

English · [简体中文](database.zh-CN.md)

# Keeping data in SQLite

Tifa has no database layer. Your app opens a database in `OnStart` and uses it with `database/sql`, like any Go program. This guide takes the SQLite setup that [`tifa new`](../build.md#tifa-new) writes and adds what a real app needs next: a setting for the file, migrations, a transaction, a backup and tests.

The code is from a scratch app made with `tifa new`, module `example.com/notes`. Where the files go is in [Structuring an app](../structure.md#2-shared-resources-and-other-dependencies).

## The driver

Use [`github.com/liustack/sqlite`](https://github.com/liustack/sqlite). It runs SQLite as Go code, so the app still builds with `CGO_ENABLED=0` into one file ([Philosophy](../philosophy.md#do-things-the-go-way)). The cgo driver `github.com/mattn/go-sqlite3` needs a C compiler and breaks that. In the driver's own [benchmarks](https://github.com/liustack/sqlite#benchmarks) it reads faster than the cgo driver and than the other pure Go drivers, and loses on work that stays inside SQLite, such as a join. It supports Linux and macOS on amd64 and arm64, and Windows on amd64. `tifa new` already pins a version in `go.mod`. Each release names the Go versions it was tested with and refuses to build with a newer one, so after upgrading Go, upgrade the driver too.

Two habits differ from other drivers. Parameters in the DSN other than `_pragma` and `_txlock` are an error, so the `_foreign_keys=1` style of other drivers does not work. And double quotes in SQL only name columns and tables, so write string values in single quotes. Do not import another driver named `sqlite` (such as `modernc.org/sqlite`) in the same program, because the two register the same name and the program panics at start.

## Open it in OnStart

`db.go` at the root of the app holds the variable and the function that opens it:

```go
// db.go
package notes

import (
	"context"
	"database/sql"
	"net/url"

	_ "github.com/liustack/sqlite"
)

// DB is the database of the app. OnStart opens it and OnShutdown closes it.
var DB *sql.DB

func OpenDB(ctx context.Context, path string) (*sql.DB, error) {
	// The pragmas run on every connection of the pool. WAL lets reads go on during a write, and
	// SQLite checks foreign keys only when asked to. A write waits up to 5s for another one by
	// default (change it with _pragma=busy_timeout(ms)). A transaction that reads first and then
	// writes can fail with SQLITE_BUSY even with a timeout, so _txlock=immediate makes every
	// transaction take the write lock as it starts.
	q := url.Values{
		"_pragma": {"journal_mode(WAL)", "synchronous(NORMAL)", "foreign_keys(1)"},
		"_txlock": {"immediate"},
	}
	db, err := sql.Open("sqlite", "file:"+path+"?"+q.Encode())
	if err != nil {
		return nil, err
	}
	if err := migrate(ctx, db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}
```

```go
// hooks.go
func OnStart(ctx context.Context) error {
	db, err := OpenDB(ctx, DBPath)
	if err != nil {
		return err
	}
	DB = db
	return nil
}

func OnShutdown(ctx context.Context) error { return DB.Close() }
```

The parameters after `?` are read by the driver ([its README](https://github.com/liustack/sqlite#readme)). `_pragma=name(value)` runs `PRAGMA name = value` on every new connection. `OnShutdown` runs after the requests in flight have finished, so nothing uses `DB` while it closes.

## Where the file lives

The path is an option of the app, in `options.go` ([Options of the app](../config.md#options-of-the-app)):

```go
// options.go
var DBPath = tifa.String("db.path", "app.db", "SQLite database file")
```

- A relative path is resolved against the folder the program runs in. That is the app root under `tifa dev`, and the folder you start the program from on a server.
- On a server, put the file in a folder meant for data and name it with the environment: `DB_PATH=/var/lib/notes/app.db`. [Deploying](../deploy.md#run-it) says to back that folder up or mount it in a container.
- SQLite in WAL mode keeps two more files next to it, `app.db-wal` and `app.db-shm`. Do not copy the three by hand while the app runs, see the backup below. Keep all three out of git (the `.gitignore` of `tifa new` does).
- One program owns the file. Two copies of the app on the same file work for a short overlap during a deploy, but a second server needs [Postgres](#when-to-use-postgres).

## Change the tables with migrations

`tifa new` creates its tables with `CREATE TABLE IF NOT EXISTS` at every start. That cannot change a table that already holds data. For that, keep the tables as a list of numbered SQL files and apply the new ones at start.

```text
migrations/
  001_todos.sql
  002_todo_created_at.sql
  003_jobs.sql
```

```sql
-- migrations/002_todo_created_at.sql
ALTER TABLE todos ADD COLUMN created_at TEXT NOT NULL DEFAULT '';
UPDATE todos SET created_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now');
```

The runner is about fifty lines. It remembers the last file it applied in `PRAGMA user_version`, a number SQLite stores in the database file, so there is no extra table:

```go
// migrate.go
package notes

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strconv"
	"strings"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// migrate applies the files of migrations/ that the database has not seen yet, in the order of
// their numbers. The file 003_x.sql is version 3.
func migrate(ctx context.Context, db *sql.DB) error {
	names, err := fs.Glob(migrationFiles, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(names)
	for i, name := range names {
		version, err := strconv.Atoi(strings.SplitN(path.Base(name), "_", 2)[0])
		if err != nil || version != i+1 {
			return fmt.Errorf("migrations: %s must be numbered %03d", name, i+1)
		}
	}
	// One connection for the whole run, so the lock and the version belong together.
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	// BEGIN IMMEDIATE takes the write lock now, so a second copy of the app that starts at the
	// same time waits here and then finds the work done.
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			conn.ExecContext(context.WithoutCancel(ctx), "ROLLBACK")
		}
	}()
	var current int
	if err := conn.QueryRowContext(ctx, "PRAGMA user_version").Scan(&current); err != nil {
		return err
	}
	if current > len(names) {
		return fmt.Errorf("migrations: the database is at version %d, this program knows %d. A newer version of the app wrote it", current, len(names))
	}
	for i := current; i < len(names); i++ {
		text, err := migrationFiles.ReadFile(names[i])
		if err != nil {
			return err
		}
		if _, err := conn.ExecContext(ctx, string(text)); err != nil {
			return fmt.Errorf("migrations: %s: %w", names[i], err)
		}
		// PRAGMA takes no bound value. i+1 is a number we made.
		if _, err := conn.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", i+1)); err != nil {
			return err
		}
	}
	_, err = conn.ExecContext(ctx, "COMMIT")
	committed = err == nil
	return err
}
```

How it behaves:

- **All or nothing.** SQLite can roll back table changes, so the files of one start and the new version number are one transaction. A file that fails leaves the database as it was, and `OnStart` returns the error, so the app does not start.
- **Before the first request.** `OnStart` finishes before the app listens, so no request sees a half-changed table. A slow change delays the start by as long as it takes.
- **Never edit a file that has run.** Add the next number instead. The runner cannot tell that an old file changed.
- **No down files.** To go back, restore the backup taken before the deploy. A program that finds a database from a newer version refuses to start instead of guessing.
- **The build does not run migrations.** `tifa build` only reads your code ([Philosophy](../philosophy.md#do-things-the-go-way)), so it needs no database.

Why not a library? [goose](https://github.com/pressly/goose) is a Go library that applies embedded SQL files through `database/sql`, with SQLite and Postgres. Take it when you want down files or expect to move to Postgres. For one SQLite file, the runner above is less to learn and fits on a page. Both need the same habit: one numbered file per change.

## Use it in Load and handlers

A domain imports the root package and uses `DB` directly. Pass the request context so a query stops when the visitor leaves:

```go
// app/todos/todo.go
func GetTodo(ctx context.Context, id int64) (Todo, error) {
	t := Todo{ID: id}
	err := notes.DB.QueryRowContext(ctx, `SELECT title, done FROM todos WHERE id = ?`, id).Scan(&t.Title, &t.Done)
	if errors.Is(err, sql.ErrNoRows) {
		return Todo{}, tifa.ErrNotFound
	}
	return t, err
}
```

```go
// app/todos/todo+/route.go
func Load(c *tifa.Ctx) (todos.Todo, error) {
	id, err := strconv.ParseInt(c.Param("todo"), 10, 64)
	if err != nil {
		return todos.Todo{}, tifa.ErrNotFound
	}
	return todos.GetTodo(c.Context(), id)
}
```

- A missing row becomes `tifa.ErrNotFound`, which answers 404 ([route.go](../app.md#routego)).
- `Load` only reads. A `Load` can run more than once for one visit (navigation, the page cache), so writes belong in `POST`, `PUT`, `PATCH` and `DELETE` handlers.
- Use `?` placeholders for every value that comes from a request. Never build SQL with string concatenation.
- `QueryRowContext` with `RETURNING` does an insert and reads the new row in one call, as `AddTodo` in the `tifa new` app does.

### Several statements at once

Changes that must stand or fall together go in a transaction. `OpenDB` sets `_txlock=immediate`, so the transaction holds the write lock from its first line and cannot fail halfway with `SQLITE_BUSY`:

```go
tx, err := notes.DB.BeginTx(ctx, nil)
if err != nil {
	return Todo{}, err
}
defer tx.Rollback() // does nothing after Commit
err = tx.QueryRowContext(ctx, `INSERT INTO todos (title, created_at) VALUES (?, strftime('%Y-%m-%dT%H:%M:%SZ', 'now')) RETURNING id`, t.Title).Scan(&t.ID)
if err != nil {
	return Todo{}, err
}
// more statements on tx, such as queuing a job: see the guide on background work
return t, tx.Commit()
```

Keep a transaction short. SQLite lets one writer in at a time, and the others wait up to `busy_timeout`. Do not call another service, or wait on a visitor, while one is open.

## Back it up

Copying `app.db` while the app runs can give a broken copy. Ask SQLite to write one:

```go
_, err := notes.DB.ExecContext(ctx, `VACUUM INTO ?`, "/var/backups/notes/2026-11-04.db")
```

The file is a complete database, safe to move off the machine. `VACUUM INTO` fails if the target file exists. Run it from a scheduled task ([Background work](jobs.md#scheduled-work)) or before a deploy.

## Test on a temporary database

Tests replace the variable ([Structuring an app](../structure.md#2-shared-resources-and-other-dependencies)). Settings keep their defaults in `go test`, so a test opens its own database in a temporary folder. Put the helper in a small package so every domain can use it, and import it from test files only:

```go
// testdb/testdb.go
package testdb

import (
	"context"
	"path/filepath"
	"testing"

	"example.com/notes"
)

// Use puts a fresh database in a temporary folder into notes.DB for the length of the test. The
// tests that use it must not run in parallel, since notes.DB is shared.
func Use(t *testing.T) {
	t.Helper()
	db, err := notes.OpenDB(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	old := notes.DB
	notes.DB = db
	t.Cleanup(func() {
		notes.DB = old
		db.Close()
	})
}
```

```go
// app/todos/todo_test.go
func TestGetAndSetDone(t *testing.T) {
	testdb.Use(t)
	added, err := AddTodo(context.Background(), "Water the plants")
	// ...
}
```

The test database runs the real migrations, so a broken migration fails the whole suite. Test the runner itself on a file kept between two opens:

```go
func TestOpenDBMigratesOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")
	for range 2 {
		db, err := OpenDB(context.Background(), path)
		if err != nil {
			t.Fatal(err)
		}
		var version int
		if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 3 {
			t.Fatalf("user_version: %d, %v, want 3", version, err)
		}
		db.Close()
	}
}
```

The root package cannot import `testdb` in its own tests (the import would loop back), so its tests keep a three-line helper of their own. Route code is tested with `tifatest.NewCtx`, see [Testing](../app.md#testing).

## When to use Postgres

Stay on SQLite while the app runs as one program on one machine. Move to Postgres when you need a second server, when the data must outlive the machine, or when your host offers managed backups and you want them. The pure Go driver is [pgx](https://github.com/jackc/pgx) with its `database/sql` adapter:

```go
import _ "github.com/jackc/pgx/v5/stdlib"

func OpenDB(ctx context.Context, url string) (*sql.DB, error) {
	db, err := sql.Open("pgx", url)
	if err != nil {
		return nil, err
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}
```

Everything else in this guide keeps its shape: a `DB` variable opened in `OnStart`, an option for the address (`db.url`, now a `tifa.Secret` since it holds the password, see [Sending email](email.md#settings)), queries through `database/sql`. What changes: placeholders are `$1`, `$2`, the pragmas go away, and the migration runner above is SQLite's, so use a library such as goose. Tests then need a real Postgres, so give them one through a setting and skip when it is not set.

Sessions can live in the same database: [Sessions](../sessions.md#where-to-store-sessions).
