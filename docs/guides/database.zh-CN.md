---
summary: '怎样在 Tifa 应用里用 SQLite 保存数据：纯 Go 驱动、在 OnStart 里打开数据库、文件放在哪里以及怎样设置路径、用启动时运行的带编号迁移修改表、在 Load 和处理函数里使用、事务、拒绝重复的行、备份、在临时数据库上测试，以及什么时候该换成 Postgres'
audience: app
order: 2
translatedFrom: d04949d1ca08
read_when:
  - '给 Tifa 应用加数据库，或者扩充 `tifa new` 写出的那个'
  - '修改一个已经在运行的应用的表'
  - '为 Tifa 应用在 SQLite 和 Postgres 之间做选择'
  - '为读写数据库的代码写 Go 测试'
---

# 在 SQLite 里保存数据 {#keeping-data-in-sqlite}

Tifa 没有数据库层。你的应用在 `OnStart` 里打开一个数据库，像任何 Go 程序一样用 `database/sql` 使用它。这篇指南从 [`tifa new`](../build-and-deploy/build.zh-CN.md#tifa-new) 写出的 SQLite 配置出发，加上真实应用接下来需要的东西：文件路径的设置、迁移、事务、备份和测试。

代码来自一个用 `tifa new` 建的临时应用，模块是 `example.com/notes`。文件放在哪里，见 [组织一个应用](../start/structure.zh-CN.md#2-shared-resources-and-other-dependencies)。

## 驱动 {#the-driver}

用 [`github.com/liustack/sqlite`](https://github.com/liustack/sqlite)。它把 SQLite 作为 Go 代码来运行，所以应用仍然可以用 `CGO_ENABLED=0` 构建成一个文件（[理念](../start/philosophy.zh-CN.md#do-things-the-go-way)）。cgo 驱动 `github.com/mattn/go-sqlite3` 需要 C 编译器，会破坏这一点。在这个驱动自己的 [基准测试](https://github.com/liustack/sqlite#benchmarks) 里，它读取比 cgo 驱动和其他纯 Go 驱动都快，在完全留在 SQLite 内部的工作上（比如 join）则输给它们。它支持 amd64 和 arm64 上的 Linux、macOS 和 Windows。`tifa new` 已经在 `go.mod` 里固定了一个版本。每个发布版本都写明它测试过的 Go 版本，并且拒绝用更新的版本构建，所以升级 Go 之后，驱动也要升级。

有两个习惯和其他驱动不同。DSN 里除了 `_pragma` 和 `_txlock` 以外的参数都是错误，所以其他驱动那种 `_foreign_keys=1` 的写法不起作用。另外 SQL 里的双引号只用来命名列和表，所以字符串值要写在单引号里。不要在同一个程序里导入另一个叫 `sqlite` 的驱动（比如 `modernc.org/sqlite`），因为两者注册同一个名字，程序启动时会 panic。

## 在 OnStart 里打开它 {#open-it-in-onstart}

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

`?` 之后的参数由驱动读取（[它的 README](https://github.com/liustack/sqlite#readme)）。`_pragma=name(value)` 在每个新连接上运行 `PRAGMA name = value`。`OnShutdown` 在正在处理的请求都完成之后运行，所以关闭 `DB` 时没有任何东西在用它。

## 文件放在哪里 {#where-the-file-lives}

路径是应用的一个选项，写在 `options.go` 里（[应用的选项](../build-and-deploy/config.zh-CN.md#options-of-the-app)）：

```go
// options.go
var DBPath = tifa.String("db.path", "app.db", "SQLite database file")
```

- 相对路径相对于程序运行所在的文件夹解析。在 `tifa dev` 下那是应用根目录，在服务器上是你启动程序的文件夹。
- 在服务器上，把文件放进专门放数据的文件夹，并用环境变量来指定：`DB_PATH=/var/lib/notes/app.db`。[部署](../build-and-deploy/deploy.zh-CN.md#run-it) 说明要备份这个文件夹，或者在容器里把它挂载出来。
- WAL 模式下的 SQLite 会在它旁边再保存两个文件，`app.db-wal` 和 `app.db-shm`。应用运行时不要手动复制这三个文件，见下面的备份。让这三个文件都不进 git（`tifa new` 的 `.gitignore` 就是这样）。
- 一个程序拥有这个文件。部署时短暂重叠的两份应用共用同一个文件是可以的，但第二台服务器需要 [Postgres](#when-to-use-postgres)。

## 用迁移来改表 {#change-the-tables-with-migrations}

`tifa new` 在每次启动时用 `CREATE TABLE IF NOT EXISTS` 创建它的表。这改不了已经有数据的表。要改，就把表保存成一串带编号的 SQL 文件，启动时应用新的那些。

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

这个运行器大约五十行。它把最后应用过的文件记在 `PRAGMA user_version` 里，这是 SQLite 存在数据库文件里的一个数字，所以不需要额外的表：

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

- **要么全做，要么都不做。** SQLite 可以回滚对表的改动，所以一次启动里的文件和新的版本号是一个事务。有一个文件失败，数据库就保持原样，`OnStart` 返回错误，应用不会启动。
- **在第一个请求之前。** `OnStart` 在应用开始监听之前就完成了，所以没有请求会看到改了一半的表。很慢的改动会让启动延后相应的时间。
- **不要编辑已经运行过的文件。** 加下一个编号的文件。运行器看不出旧文件被改过。
- **没有 down 文件。** 要回退，就恢复部署之前做的备份。程序发现数据库来自更新的版本时，会拒绝启动，不去猜。
- **构建不运行迁移。** `tifa build` 只读你的代码（[理念](../start/philosophy.zh-CN.md#do-things-the-go-way)），所以它不需要数据库。

为什么不用库？[goose](https://github.com/pressly/goose) 是一个 Go 库，通过 `database/sql` 应用内嵌的 SQL 文件，支持 SQLite 和 Postgres。想要 down 文件，或预计要迁到 Postgres 时就用它。对一个 SQLite 文件来说，上面的运行器要学的更少，一页就放得下。两者需要同一个习惯：每次改动一个带编号的文件。

## 在 Load 和处理函数里使用 {#use-it-in-load-and-handlers}

域导入根包，直接使用 `DB`。传入请求的 context，这样访客离开时查询就会停止：

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

- 缺失的行变成 `tifa.ErrNotFound`，响应 404（[route.go](../pages-and-data/routes.zh-CN.md#routego)）。
- `Load` 只读。一次访问里 `Load` 可能运行不止一次（导航、页面缓存），所以写入要放在 `POST`、`PUT`、`PATCH` 和 `DELETE` 处理函数里。
- 来自请求的每个值都用 `?` 占位符。永远不要用字符串拼接来构建 SQL。
- 带 `RETURNING` 的 `QueryRowContext` 一次调用就能插入并读回新行，`tifa new` 应用里的 `AddTodo` 就是这样做的。

### 一次执行多条语句 {#several-statements-at-once}

必须一起成功或一起失败的改动，放进事务里。`OpenDB` 设置了 `_txlock=immediate`，所以事务从第一行起就持有写锁，不会因为 `SQLITE_BUSY` 在中途失败：

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

事务要保持很短。SQLite 同一时间只让一个写入者进入，其他的等待，最多等 `busy_timeout`。事务开着的时候，不要调用别的服务，也不要等访客。

### 拒绝重复 {#reject-a-duplicate}

比如两个访客不能预约同一个时段，表上有 `UNIQUE (doctor_slug, slot)`。让数据库用一条语句来决定。时段被占用时，`ON CONFLICT ... DO NOTHING` 不插入任何行，所以 `RETURNING` 不返回行，`Scan` 返回 `sql.ErrNoRows`。把它理解为「已被占用」。这是原子的，所以两个同时到达的请求不可能都成功：

```go
var id int64
err := notes.DB.QueryRowContext(ctx,
	`INSERT INTO bookings (doctor_slug, slot, name) VALUES (?, ?, ?)
	 ON CONFLICT (doctor_slug, slot) DO NOTHING RETURNING id`, doctor, slot, name).Scan(&id)
if errors.Is(err, sql.ErrNoRows) {
	return tifa.Fields{"slot": "That slot is taken."} // a 422 with a message for the form
}
if err != nil {
	return err
}
```

这个驱动不导出错误类型，所以 `errors.As` 没有东西可找，这一点和 `modernc.org/sqlite` 不同。约束失败时返回的错误，它的文字是消息加上扩展结果码，比如 `sqlite: UNIQUE constraint failed: bookings.slot (2067)`。在 `ON CONFLICT` 不适用的地方，比如 `UPDATE`，用 `strings.Contains(err.Error(), "UNIQUE constraint failed")` 来匹配这段文字。

## 备份 {#back-it-up}

应用运行时复制 `app.db` 可能得到一份损坏的副本。让 SQLite 来写一份：

```go
_, err := notes.DB.ExecContext(ctx, `VACUUM INTO ?`, "/var/backups/notes/2026-11-04.db")
```

这个文件是一个完整的数据库，可以放心地搬出这台机器。目标文件已存在时，`VACUUM INTO` 会失败。从定时任务里运行它（[后台任务](jobs.zh-CN.md#scheduled-work)），或者在部署之前运行。

## 在临时数据库上测试 {#test-on-a-temporary-database}

测试替换这个变量（[组织一个应用](../start/structure.zh-CN.md#2-shared-resources-and-other-dependencies)）。`go test` 里设置保持默认值，所以测试在临时文件夹里打开它自己的数据库。把这个辅助函数放进一个小包里，让每个域都能用，并且只从测试文件里导入它：

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

测试数据库运行真实的迁移，所以一个有问题的迁移会让整个测试套件失败。运行器本身，用一个在两次打开之间保留的文件来测试：

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

根包在它自己的测试里不能导入 `testdb`（这个导入会绕回来），所以它的测试自己带一个三行的辅助函数。路由代码用 `tifatest.NewCtx` 测试，见 [测试](../pages-and-data/testing-apps.zh-CN.md#testing)。

## 什么时候用 Postgres {#when-to-use-postgres}

只要应用是在一台机器上作为一个程序运行，就留在 SQLite 上。需要第二台服务器、数据必须比这台机器活得更久，或者你的主机提供托管备份而你想要它们时，就换到 Postgres。纯 Go 的驱动是 [pgx](https://github.com/jackc/pgx) 和它的 `database/sql` 适配器：

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

这篇指南里其他一切都保持原来的形状：一个在 `OnStart` 里打开的 `DB` 变量，一个放地址的选项（`db.url`，现在因为它含有密码，是一个 `tifa.Secret`，见 [发送邮件](email.zh-CN.md#settings)），通过 `database/sql` 做查询。变化的是：占位符是 `$1`、`$2`，pragma 没有了，上面的迁移运行器是 SQLite 专用的，所以要用 goose 这样的库。这时测试需要一个真正的 Postgres，所以通过一个设置给它们一个，没设置时就跳过。

会话可以保存在同一个数据库里：[会话](../pages-and-data/sessions.zh-CN.md#where-to-store-sessions)。
