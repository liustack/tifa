---
summary: '怎样用 SQLite 保存数据：迁移、`database is locked`、把 `db.backup` 备份到 R2 或 S3、Postgres'
audience: app
order: 2
translatedFrom: ca0b5bb67938
read_when:
  - '给 Tifa 应用加数据库，或者扩充 `tifa new` 写出的那个'
  - '修改一个已经在运行的应用的表'
  - '为 Tifa 应用在 SQLite 和 Postgres 之间做选择'
  - '为读写数据库的代码写 Go 测试'
  - '写入时报 `database is locked` 或 `SQLITE_BUSY`'
  - '备份数据库、把它搬到新机器，或者回到更早的时间来撤销一次错误的部署'
---

# 在 SQLite 里保存数据 {#keeping-data-in-sqlite}

Tifa 没有数据库层。你的应用在 `OnStart` 里打开一个数据库，像任何 Go 程序一样用 `database/sql` 使用它。这篇指南从 [`tifa new`](../build-and-deploy/build.zh-CN.md#tifa-new) 写出的 SQLite 配置出发，加上真实应用接下来需要的东西：文件路径的设置、迁移、事务、备份和测试。

代码来自一个用 `tifa new` 建的临时应用，模块是 `example.com/notes`。文件放在哪里，见 [组织一个应用](../start/structure.zh-CN.md#2-shared-resources-and-other-dependencies)。

## 驱动 {#the-driver}

用 [`github.com/liustack/sqlite`](https://github.com/liustack/sqlite)。在服务器上不用 SQLite 的三个常见理由，它都有答案：

- **`database is locked`。** SQLite 一次只让一个写入进来。驱动把应用的写入按到达顺序排队，所以有了 `_txlock=immediate`，一个写入会等它前面的写入，而不是失败。只有当写入比磁盘提交得更快的情况持续超过 `busy_timeout` 时，它才会失败。见[写入报 `database is locked` 时](#when-a-write-gets-database-is-locked)。
- **写入速度。** 64 个 goroutine 同时插入时，它每行用 3.7 µs，cgo 驱动用 22.8 µs（linux/amd64，[它的基准测试](https://github.com/liustack/sqlite#benchmarks)）。在驱动的大多数基准测试里，它读取也比 cgo 驱动和其他纯 Go 驱动快。
- **丢掉机器。** 它在应用内部把每次提交在大约一秒后复制到 Cloudflare R2 或其他 S3 存储，一次调用就能在新机器上把数据库找回来，可以是现在的样子，也可以是更早某个时间的样子。见[备份](#back-it-up)。

它把 SQLite 作为 Go 代码来运行，所以应用仍然可以用 `CGO_ENABLED=0` 构建成一个文件（[理念](../start/philosophy.zh-CN.md#do-things-the-go-way)）。cgo 驱动 `github.com/mattn/go-sqlite3` 需要 C 编译器，会破坏这一点。它支持 amd64 和 arm64 上的 Linux、macOS 和 Windows。`tifa new` 已经在 `go.mod` 里固定了一个版本。每个发布版本都写明它测试过的 Go 版本，并且拒绝用更新的版本构建，所以升级 Go 之后，驱动也要升级。

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

- 缺失的行变成 `tifa.ErrNotFound`，响应 404（[route.go](../pages-and-data/loading-data.zh-CN.md#routego)）。
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

### 写入报 `database is locked` 时 {#when-a-write-gets-database-is-locked}

SQLite 同一时间只让一个写入进来。驱动把应用里的写入按到达顺序排队，所以一个写入会等前面的写入做完，而不是直接失败。你的代码要做的：

- **单独一次写入**，也就是直接 `db.ExecContext`，什么都不用多做：驱动把它和别的写入一起排队，可能把几个合成一次提交，每个照样拿到自己的结果。
- **先读后写的事务**，比如先读库存再扣减，需要 `_txlock=immediate`，上面的 `OpenDB` 和 `tifa new` 写出的 `db.go` 都已经加上了。这样每个事务在 `BEGIN` 时就拿写锁，在那里排队等。不加的话，`BEGIN` 不拿任何锁：事务先读，另一个请求写入并提交，这个事务接下来的第一次写入就会失败，`busy_timeout` 设多长都一样。
- **只读的事务**用 `db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})` 开始。它不拿写锁，也不会排在写入后面等。不在事务里的查询两样都不需要。

做到这些之后，`database is locked`（`SQLITE_BUSY`）只代表下面几种情况之一：

- **写入来得比磁盘提交得还快**，而且持续超过 `busy_timeout`（默认 5 秒）。这是真的过载：返回一个让对方稍后再试的错误，或者少放一些写入进来。调大这个时间只是对着同一块磁盘等得更久。
- **另一个进程在写同一个文件**，比如脚本或者 `sqlite3` 命令行。排队只发生在一个进程内部，进程之间用的是 SQLite 自己的锁。给那个进程也设上 `busy_timeout`（命令行里是 `.timeout 5000`）。
- **一个先读后写的事务没有用 `_txlock=immediate` 开始。** 从 `BEGIN` 起把整个事务重跑一遍，而不是只重跑失败的那条语句，更好的办法是用 `_txlock=immediate` 打开数据库。
- **`busy_timeout` 是 0。** 这时只要别人拿着锁，写入就立刻失败。

这样判断：

```go
var e *sqlite.Error
if errors.As(err, &e) && e.Code() == sqlite.BUSY {
	// overload, or another process
}
```

`_pragma=journal_mode(WAL)` 也要保留：没有它，别的连接在读的时候写入没法提交，所以读也可能报 `database is locked`。驱动的 [database is locked](https://github.com/liustack/sqlite/blob/main/docs/database-is-locked.zh-CN.md) 对每种情况讲得更细。

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

约束失败时返回的是一个 `*sqlite.Error`，扩展结果码是 `sqlite.CONSTRAINT_UNIQUE`。在 `ON CONFLICT` 不适用的地方，比如 `UPDATE`，这样判断：

```go
var e *sqlite.Error
if errors.As(err, &e) && e.ExtendedCode() == sqlite.CONSTRAINT_UNIQUE {
	return tifa.Fields{"slot": "That slot is taken."}
}
```

为此要按名字导入驱动，写 `"github.com/liustack/sqlite"`，而不是用 `_`。

## 备份 {#back-it-up}

`tifa new` 建的应用，在你设置了选项 `db.backup` 之后，会把数据库的每次提交在大约一秒后复制到 Cloudflare R2、AWS S3 或其他 S3 存储。复制在应用内部进行，用的是驱动的 [`replica`](https://github.com/liustack/sqlite/blob/main/docs/replication.md) 包：不用另外安装或运行任何东西。写入从不等待上传，存储宕机也从不让写入失败。

### 用 Cloudflare R2 打开它 {#turn-it-on-with-cloudflare-r2}

1. 在 Cloudflare 控制台打开 **R2 object storage**，选择 **Create bucket**。给它起个名字，比如 `notes-backup`。
2. 在 R2 的 **Overview** 页面，选择 **API Tokens** 旁边的 **Manage**，然后选 **Create Account API token**。给它 **Object Read & Write** 权限，并限定在这个 bucket 上。复制 **Access Key ID** 和 **Secret Access Key**：secret 只显示一次。这个页面也会显示你账号的 S3 端点 `https://<account>.r2.cloudflarestorage.com`（[R2 API tokens](https://developers.cloudflare.com/r2/api/tokens/)）。
3. 在服务器上，先设置两个密钥，再设置 bucket 的 URL。每条命令都会询问值，然后重启应用（见[服务器上的设置和 secret](../build-and-deploy/deploy.zh-CN.md#settings-and-secrets-on-the-server)）。设置了 `DB_BACKUP` 却没有密钥时，应用拒绝启动，所以顺序很重要：

   ```sh
   go tool tifa deploy env set AWS_ACCESS_KEY_ID
   go tool tifa deploy env set AWS_SECRET_ACCESS_KEY
   go tool tifa deploy env set DB_BACKUP   # s3://notes-backup/app.db?endpoint=https://<account>.r2.cloudflarestorage.com&region=auto
   ```

   `env set` 从隐藏的提示、管道或者浏览器里的一个页面读取值，从不从参数读取，所以编程 agent 可以运行这些命令而看不到密钥。

bucket 后面的路径（上面的 `app.db`）是这份副本在 bucket 里的名字。一个应用写一个路径：想在自己的机器上试一试，就把这三行放进应用的 `.env`，用另一个路径，比如 `s3://notes-backup/dev.db?...`，永远不要用服务器用的那个路径。

设置了 `DB_BACKUP` 之后的第一次启动会上传整个数据库，从那以后只上传改动。后台出的问题，`go tool tifa deploy logs` 会显示出来，是带着错误的 `database backup` 行。

### 应用拿它做什么 {#what-the-app-does-with-it}

全部都在 `tifa new` 写出的 `db.go` 和 `hooks.go` 里：

- **启动时**，在数据库打开之前：如果 `db.path` 指向的文件不存在，应用先从 bucket 恢复它。bucket 是空的，说明这是第一次启动，应用就用一个空数据库继续。然后它开始复制。
- **运行时**，每次提交大约一秒后就在 bucket 里了。`notes.Backup.Sync(ctx)` 会等到目前为止提交的所有内容都到了那里，用在回答请求之前必须保存好的写入上。
- **停止时**，`OnShutdown` 先关闭数据库，再关闭复制，复制会花最多 10 秒上传剩下的内容。没能上传的，留在数据库旁边的文件夹 `app.db-replica` 里，下次启动时再上传。
- **后台的错误**，比如存储没有回应，会用 `slog.Error` 写进日志。复制会自己再试。

在 `tifa new` 有这些代码之前建的应用，需要加三处。在 `options.go` 里：

```go
DBBackup           = tifa.String("db.backup", "", "S3 URL to copy the database to, empty for none")
AWSAccessKeyID     = tifa.Secret("aws.access_key_id", "Access key ID of the S3 store of db.backup")
AWSSecretAccessKey = tifa.Secret("aws.secret_access_key", "Secret access key of the S3 store of db.backup")
```

密钥是应用的选项，而不只是环境变量，因为 Tifa 把 `.env` 当作设置来读，不会把它放进进程的环境。在 `db.go` 里：

```go
import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"

	"github.com/liustack/sqlite/replica"
)

// Backup copies every commit of DB to the S3 store of DBBackup, when it is set.
var Backup *replica.Replica

// StartBackup starts copying every commit of the database at path to the S3 URL url. When the
// file is missing, as on a new machine, it first restores the database from url, unless the
// store holds none yet. Call it before OpenDB.
func StartBackup(ctx context.Context, path, url string) (*replica.Replica, error) {
	if AWSAccessKeyID == "" || AWSSecretAccessKey == "" {
		return nil, errors.New("db.backup is set: set AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY too")
	}
	cfg := replica.Config{
		URL:         url,
		Credentials: replica.StaticCredentials(replica.Credentials{AccessKeyID: AWSAccessKeyID, SecretAccessKey: AWSSecretAccessKey}),
		OnError:     func(err error) { slog.Error("database backup", "err", err) },
	}
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		_, err := replica.Restore(ctx, path, cfg, replica.RestoreOptions{})
		if err != nil && !errors.Is(err, replica.ErrNoReplica) {
			return nil, err
		}
	}
	return replica.New(ctx, path, cfg)
}
```

在 `hooks.go` 里，在 `OpenDB` 之前启动它，在数据库之后关闭它：

```go
func OnStart(ctx context.Context) error {
	if DBBackup != "" {
		b, err := StartBackup(ctx, DBPath, DBBackup)
		if err != nil {
			return err
		}
		Backup = b
	}
	db, err := OpenDB(DBPath)
	// ... as before
}

func OnShutdown(ctx context.Context) error {
	err := DB.Close()
	if Backup != nil {
		err = errors.Join(err, Backup.Close())
		Backup = nil
	}
	return err
}
```

把 `*.db-replica/` 加进 `.gitignore`。数据库必须由这个驱动、在这个程序里、在 `StartBackup` 之后打开：复制只跟着它看得到的写入走。一个用 `sqlite3` shell 写这个文件的脚本，会让它重新上传整个数据库。

### 搬到新机器 {#move-to-a-new-machine}

配置好新服务器，在那里用 `tifa deploy env set` 设置同样的三个值，然后照常部署（见[部署](../build-and-deploy/deploy.zh-CN.md)）。新服务器上没有数据库文件，所以应用在第一次启动时、回答任何请求之前，恢复最新的副本。你最多丢掉旧机器消失前最后一秒左右的写入。

如果旧机器上的应用还在运行，先停掉它。两个应用写 bucket 里的同一个路径，会弄坏这份副本：第二个在被发现时会被拒绝，但不是每次都能发现。

### 撤销一次错误的部署 {#undo-a-bad-deploy}

假设一次在 UTC 10:42 的部署删错了行。回到一秒前的数据库：在自己的机器上把那个时刻恢复到一个新文件，检查它，把它上传为最新状态，再让服务器恢复它。

恢复和上传要用一个小程序。把它放在应用里，作为 `restore/main.go`：

```go
// restore brings back the database of db.backup as it was before a time, into a new file, and
// uploads such a file as the newest state of the backup.
//
//	go run ./restore -at 2026-11-24T10:41:59Z restored.db
//	go run ./restore -upload restored.db
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"time"

	"github.com/liustack/sqlite/replica"
)

func main() {
	at := flag.String("at", "", "restore the database as it was before this time, such as 2026-11-24T10:41:59Z")
	upload := flag.Bool("upload", false, "upload the file as the newest state of the backup")
	flag.Parse()
	if flag.NArg() != 1 {
		log.Fatal("usage: go run ./restore -at <time> <file>, or go run ./restore -upload <file>")
	}
	// The keys come from AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY in the environment.
	// FreshStart lets the upload go on from a file that is behind the backup.
	cfg := replica.Config{URL: os.Getenv("DB_BACKUP"), FreshStart: true}
	ctx := context.Background()
	if *upload {
		r, err := replica.New(ctx, flag.Arg(0), cfg)
		if err != nil {
			log.Fatal(err)
		}
		// Close uploads the whole file as the newest state.
		if err := r.Close(); err != nil {
			log.Fatal(err)
		}
		return
	}
	t, err := time.Parse(time.RFC3339, *at)
	if err != nil {
		log.Fatal(err)
	}
	if _, err := replica.Restore(ctx, flag.Arg(0), cfg, replica.RestoreOptions{At: t}); err != nil {
		log.Fatal(err)
	}
}
```

然后：

1. **找到时间。** `go tool tifa deploy status` 用每次部署的 UTC 时间给发布命名，比如 `20261124T104200Z`。选它的前一秒。
2. **恢复到一个新文件**，在你自己的机器上，用服务器用的那些值。这期间应用照常运行：

   ```sh
   export DB_BACKUP='s3://notes-backup/app.db?endpoint=https://<account>.r2.cloudflarestorage.com&region=auto'
   export AWS_ACCESS_KEY_ID=<access key id>
   read -rs AWS_SECRET_ACCESS_KEY && export AWS_SECRET_ACCESS_KEY
   go run ./restore -at 2026-11-24T10:41:59Z restored.db
   ```

   恢复会检查它下载的每个文件，并在写出 `restored.db` 之前运行 `PRAGMA integrity_check`。在最近 30 分钟以内，它能精确到大约一秒。再往前，它通常落在你给的时间之前最多一分钟的状态上，因为更早的改动是按每分钟合并保存的。它至少能回到一天前。
3. **检查它**，不改动它：`sqlite3 -readonly restored.db 'SELECT count(*) FROM todos'`。
4. **停掉服务器上的应用**，这样就没有别的东西在写 bucket：

   ```sh
   ssh tifa@<server> systemctl --user stop tifa-notes.socket tifa-notes.service
   ```

5. **上传这个文件**作为最新状态：`go run ./restore -upload restored.db`。如果失败，再运行一次。
6. **把旧数据库挪开，启动应用。** 它找不到数据库文件，就恢复最新的状态，也就是你上传的那个：

   ```sh
   ssh tifa@<server>
   cd /srv/tifa/notes/shared && mkdir before-restore && mv app.db* before-restore/
   systemctl --user start tifa-notes.socket tifa-notes.service
   ```

你选的时间之后的写入，从应用里消失了，好的写入也一样。`before-restore/app.db` 里还有它们，需要的话可以手动拷回一些。

跳过第 5 步的话，应用会在第 6 步拒绝启动，报 `replica: the replica and the database went apart`（`replica.ErrDiverged`）：服务器上的文件和 bucket 对不上了，复制不会替你选一边。应用发现一个在 bucket 里没有历史的数据库文件时（比如手动拷进来的 `app.db`），也会因为同一个错误停下。像第 6 步那样把它挪开，或者先用 `-upload` 上传它。

### 在 R2 上花多少钱 {#what-it-costs-on-r2}

应用写入时，复制大约每秒上传一个文件，另外每分钟和每小时各合并一次。空闲的数据库什么都不上传。R2 把每次上传算作一次 Class A 操作：每月 100 万次免费，之后每 100 万次 4.50 美元。存储每月 10 GB 以内免费，下载免费（[R2 价格](https://developers.cloudflare.com/r2/pricing/)）。一个在 30 天的一个月里每一秒都有写入的数据库，大约上传 260 万次，每月约 7 美元。一个月里有写入的秒数加起来少于大约 11 天的数据库，留在免费额度内。想不管负载多大都留在免费额度内，就在 `StartBackup` 的 `replica.Config` 里加上 `SyncInterval: 3 * time.Second`：这样机器丢掉时，你最多丢大约 3 秒的写入，而不是 1 秒。这些数字来自驱动（[Replication](https://github.com/liustack/sqlite/blob/main/docs/replication.md#what-it-costs-on-r2)）。

### 一次性副本 {#a-one-off-copy}

想要数据库的一份单独副本时，比如在一次有风险的迁移之前，让 SQLite 来写一份。应用运行时复制 `app.db` 可能得到一份损坏的副本：

```go
_, err := notes.DB.ExecContext(ctx, `VACUUM INTO ?`, "/var/backups/notes/2026-11-04.db")
```

这个文件是一个完整的数据库，可以放心地搬出这台机器。目标文件已存在时，`VACUUM INTO` 会失败。

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

只要应用是在一台机器上作为一个程序运行，就留在 SQLite 上。需要第二台服务器，或者你的主机提供托管备份而你想要它们时，就换到 Postgres。纯 Go 的驱动是 [pgx](https://github.com/jackc/pgx) 和它的 `database/sql` 适配器：

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
