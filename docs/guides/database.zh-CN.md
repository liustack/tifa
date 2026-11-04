---
summary: '怎么在 tifa 应用里用 SQLite 存数据：纯 Go 驱动、在 OnStart 里打开数据库、文件放哪以及怎么设路径、用启动时执行的编号迁移改表、在 Load 和处理函数里使用、事务、备份、用临时数据库测试，以及什么时候换 Postgres'
read_when:
  - 给 tifa 应用加数据库，或扩展 tifa new 写好的那一个
  - 要改一个已经在运行的应用的表
  - 在 SQLite 和 Postgres 之间做选择
  - 给读写数据库的代码写 Go 测试
---

[English](database.md) · 简体中文

# 用 SQLite 存数据

tifa 没有数据库层。应用在 `OnStart` 里打开数据库，用 `database/sql` 使用它，和任何 Go 程序一样。这篇从 [`tifa new`](../build.zh-CN.md#tifa-new) 写好的 SQLite 配置出发，补上真实应用接下来要用的东西：文件路径的设置、迁移、事务、备份和测试。

代码来自用 `tifa new` 生成的试验应用，模块名 `example.com/notes`。文件该放哪，见[应用的代码结构](../structure.zh-CN.md#2-共享资源和其他依赖)。

## 驱动

用 [`github.com/liustack/sqlite`](https://github.com/liustack/sqlite)。它把 SQLite 当 Go 代码运行，所以应用仍然能用 `CGO_ENABLED=0` 编译成一个文件（见[哲学](../philosophy.zh-CN.md#按-go-的方式)）。cgo 驱动 `github.com/mattn/go-sqlite3` 需要 C 编译器，会破坏这一点。在驱动自己的[评测](https://github.com/liustack/sqlite/blob/main/README.zh-CN.md)里，它读得比 cgo 驱动和其他纯 Go 驱动都快，只在完全在 SQLite 内部完成的工作（比如连接查询）上落后。它支持 amd64 和 arm64 上的 Linux 和 macOS，以及 amd64 上的 Windows。`tifa new` 已经在 `go.mod` 里固定了一个版本。每个版本写明测过的 Go 版本，遇到更新的 Go 会拒绝编译，所以升级 Go 之后也要升级驱动。

有两个习惯和别的驱动不同。DSN 里除了 `_pragma` 和 `_txlock` 的参数都会报错，所以别的驱动那种 `_foreign_keys=1` 的写法不能用。SQL 里的双引号只表示列名和表名，字符串值要写成单引号。同一个程序里不要再导入另一个叫 `sqlite` 的驱动（比如 `modernc.org/sqlite`），两者注册同一个名字，程序启动时会 panic。

## 在 OnStart 里打开

应用根目录的 `db.go` 放变量和打开它的函数：

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

`?` 后面的参数由驱动读取（见[驱动说明](https://github.com/liustack/sqlite#readme)）。`_pragma=name(value)` 会在每个新连接上执行 `PRAGMA name = value`。`OnShutdown` 在处理中的请求结束后才运行，所以关闭 `DB` 时没有人在用它。

## 文件放哪

路径是应用的一项设置，写在 `config.go`（见[应用自己的设置](../config.zh-CN.md#应用的配置)）：

```go
// config.go
var DBPath = tifa.String("db.path", "app.db", "SQLite database file")
```

- 相对路径按程序运行所在的目录解析。`tifa dev` 下是应用根目录，服务器上是你启动程序的目录。
- 在服务器上，把文件放进专门存数据的目录，用环境变量指定：`DB_PATH=/var/lib/notes/app.db`。[部署](../deploy.zh-CN.md#运行)说要备份这个目录，或在容器里挂载它。
- WAL 模式下，SQLite 会在旁边多放两个文件：`app.db-wal` 和 `app.db-shm`。应用运行时不要手动复制这三个文件，见下面的备份。三个都不要进 git（`tifa new` 的 `.gitignore` 已经排除了）。
- 一个程序独占这个文件。部署时两份应用短暂地同时用一个文件是可以的，但要第二台服务器就得用 [Postgres](#什么时候用-postgres)。

## 用迁移改表

`tifa new` 在每次启动时用 `CREATE TABLE IF NOT EXISTS` 建表。这改不了已经有数据的表。要改表，就把表的定义存成一串编号的 SQL 文件，启动时执行新的那些。

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

执行器大约五十行。它把最后执行的文件号记在 `PRAGMA user_version` 里，这是 SQLite 存在数据库文件里的一个数字，所以不用多建一张表：

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

它的行为：

- **要么全成，要么全不成。** SQLite 能回滚对表的修改，所以一次启动要执行的所有文件和新的版本号是同一个事务。某个文件失败，数据库保持原样，`OnStart` 返回错误，应用不会启动。
- **在第一个请求之前。** `OnStart` 跑完应用才开始监听，所以没有请求会看到改了一半的表。改得慢，启动就晚多久。
- **不要改已经执行过的文件。** 加下一个编号的新文件。执行器看不出旧文件被改过。
- **没有回退文件。** 想退回去，就还原部署前做的备份。程序发现数据库来自更新的版本时会拒绝启动，不去猜。
- **构建不执行迁移。** `tifa build` 只读你的代码（见[哲学](../philosophy.zh-CN.md#按-go-的方式)），所以不需要数据库。

为什么不用库？[goose](https://github.com/pressly/goose) 是一个 Go 库，能通过 `database/sql` 执行嵌入的 SQL 文件，支持 SQLite 和 Postgres。想要回退文件，或预计会换 Postgres，就用它。只有一个 SQLite 文件时，上面的执行器要学的更少，一页就放得下。两种做法的习惯是一样的：每次改动一个编号文件。

## 在 Load 和处理函数里使用

领域包引入根包，直接用 `DB`。传入请求的 context，访客离开时查询就会停：

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

- 找不到的行变成 `tifa.ErrNotFound`，页面答 404（见 [route.go](../app.zh-CN.md#routego)）。
- `Load` 只读。一次访问里 `Load` 可能运行不止一次（导航、页面缓存），所以写操作放在 `POST`、`PUT`、`PATCH`、`DELETE` 处理函数里。
- 来自请求的每个值都用 `?` 占位符。不要用字符串拼接 SQL。
- 带 `RETURNING` 的 `QueryRowContext` 一次调用就能插入并读回新行，`tifa new` 应用的 `AddTodo` 就是这样做的。

### 一次执行几条语句

必须同时成功或同时失败的改动放进事务。`OpenDB` 设了 `_txlock=immediate`，所以事务从第一行起就持有写锁，不会因为 `SQLITE_BUSY` 在中途失败：

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

事务要短。SQLite 同一时刻只让一个写入者进来，其他的最多等 `busy_timeout`。事务打开时不要去调用别的服务，也不要等访客。

## 备份

应用运行时直接复制 `app.db` 可能得到损坏的副本。让 SQLite 自己写一份：

```go
_, err := notes.DB.ExecContext(ctx, `VACUUM INTO ?`, "/var/backups/notes/2026-11-04.db")
```

写出的文件是完整的数据库，可以放心搬离这台机器。目标文件已存在时 `VACUUM INTO` 会失败。可以在定时任务里运行它（见[后台任务](jobs.zh-CN.md#定时任务)），或在部署前运行。

## 用临时数据库测试

测试替换变量（见[应用的代码结构](../structure.zh-CN.md#2-共享资源和其他依赖)）。`go test` 里设置保持默认值，所以测试在临时目录里自己开一个数据库。把辅助函数放进一个小包，让每个领域都能用，并且只在测试文件里引入它：

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

测试数据库执行的是真实的迁移，所以一个写坏的迁移会让整个测试都失败。执行器本身用一个在两次打开之间保留的文件来测：

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

根包在自己的测试里不能引入 `testdb`（引入会绕回来形成循环），所以它的测试自带一个三行的辅助函数。路由代码用 `tifatest.NewCtx` 测，见[测试](../app.zh-CN.md#测试)。

## 什么时候用 Postgres

应用还是一个程序跑在一台机器上，就留在 SQLite。需要第二台服务器、数据要比这台机器活得久，或者你的主机提供托管备份并且你想用，就换 Postgres。纯 Go 的驱动是 [pgx](https://github.com/jackc/pgx)，用它的 `database/sql` 适配器：

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

这篇其余的部分形状不变：在 `OnStart` 里打开的 `DB` 变量，地址的设置（`db.url`，密码放环境变量，见[发送邮件](email.zh-CN.md#设置)），通过 `database/sql` 查询。会变的是：占位符写成 `$1`、`$2`，pragma 没有了，上面的迁移执行器是为 SQLite 写的，所以改用 goose 这样的库。测试这时需要一个真的 Postgres，用一项设置把它交给测试，没设就跳过。

会话也可以放在同一个数据库里：见[会话](../sessions.zh-CN.md#会话存在哪)。
