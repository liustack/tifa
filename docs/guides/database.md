---
summary: 'How to keep data in a Tifa app with SQLite: the pure Go driver, opening the database in OnStart, where the file lives and how to set its path, changing the tables with numbered migrations that run at start, using it in Load and handlers, a transaction, why a write gets `database is locked` and how `_txlock=immediate` prevents it, rejecting a duplicate row, copying every commit to Cloudflare R2 or another S3 store with the option `db.backup` and restoring the database on a new machine or to the second before a bad deploy, what that costs on R2, a one-off copy with `VACUUM INTO`, testing on a temporary database, and when to move to Postgres'
audience: app
order: 2
read_when:
  - Adding a database to a Tifa app, or growing the one `tifa new` wrote
  - Changing a table of an app that is already running
  - Deciding between SQLite and Postgres for a Tifa app
  - Writing Go tests for code that reads or writes the database
  - A write fails with `database is locked` or `SQLITE_BUSY`
  - Backing up the database, moving it to a new machine, or undoing a bad deploy by going back to an earlier time
---

# Keeping data in SQLite

Tifa has no database layer. Your app opens a database in `OnStart` and uses it with `database/sql`, like any Go program. This guide takes the SQLite setup that [`tifa new`](../build-and-deploy/build.md#tifa-new) writes and adds what a real app needs next: a setting for the file, migrations, a transaction, a backup and tests.

The code is from a scratch app made with `tifa new`, module `example.com/notes`. Where the files go is in [Structuring an app](../start/structure.md#2-shared-resources-and-other-dependencies).

## The driver

Use [`github.com/liustack/sqlite`](https://github.com/liustack/sqlite). It answers the three usual reasons not to run SQLite on a server:

- **`database is locked`.** SQLite lets one write in at a time. The driver queues the writes of the app in the order they came, so with `_txlock=immediate` a write waits for the ones ahead of it instead of failing. It fails only when writes keep coming faster than the disk commits them for longer than `busy_timeout`. See [When a write gets `database is locked`](#when-a-write-gets-database-is-locked).
- **Write speed.** With 64 goroutines inserting at once it takes 3.7 µs a row, against 22.8 µs for the cgo driver (linux/amd64, [its benchmarks](https://github.com/liustack/sqlite#benchmarks)). It also reads faster than the cgo driver and the other pure Go drivers in most of the driver's benchmarks.
- **Losing the machine.** It copies every commit to Cloudflare R2 or another S3 store about a second later, from inside the app, and one call brings the database back on a new machine, as it is now or as it was at an earlier time. See [Back it up](#back-it-up).

It runs SQLite as Go code, so the app still builds with `CGO_ENABLED=0` into one file ([Philosophy](../start/philosophy.md#do-things-the-go-way)). The cgo driver `github.com/mattn/go-sqlite3` needs a C compiler and breaks that. It supports Linux, macOS and Windows, on amd64 and arm64. `tifa new` already pins a version in `go.mod`. Each release names the Go versions it was tested with and refuses to build with a newer one, so after upgrading Go, upgrade the driver too.

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

The path is an option of the app, in `options.go` ([Options of the app](../build-and-deploy/config.md#options-of-the-app)):

```go
// options.go
var DBPath = tifa.String("db.path", "app.db", "SQLite database file")
```

- A relative path is resolved against the folder the program runs in. That is the app root under `tifa dev`, and the folder you start the program from on a server.
- On a server, put the file in a folder meant for data and name it with the environment: `DB_PATH=/var/lib/notes/app.db`. [Deploying](../build-and-deploy/deploy.md#run-it) says to back that folder up or mount it in a container.
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
- **The build does not run migrations.** `tifa build` only reads your code ([Philosophy](../start/philosophy.md#do-things-the-go-way)), so it needs no database.

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

- A missing row becomes `tifa.ErrNotFound`, which answers 404 ([route.go](../pages-and-data/routes.md#routego)).
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

### When a write gets `database is locked`

SQLite lets one write in at a time. The driver queues the writes of the app in the order they came, so a write waits for the ones ahead of it instead of failing. Two things still end in `database is locked` (`SQLITE_BUSY`):

- **A transaction that does not take the write lock when it starts.** Without `_txlock=immediate`, `BEGIN` takes no lock. The transaction reads, another request writes and commits, and the first write of the transaction then fails, however long `busy_timeout` is. SQLite refuses it to keep what the transaction read consistent, and waiting longer or trying the statement again does not help. Open the database with `_txlock=immediate`, as `OpenDB` above and the `db.go` of `tifa new` do: every transaction then takes the write lock at `BEGIN` and waits its turn there.
- **Writes that come faster than the disk can commit them.** A write then waits longer than `busy_timeout` (5 seconds by default) and fails. Keep transactions short, as above, before you raise the timeout.

A transaction that only reads needs no write lock. Start it with `db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})`, so it does not wait behind the writes. A query outside a transaction needs neither. A write outside a transaction, a plain `db.ExecContext`, needs nothing either: the driver queues it with the others and may commit several in one go, each with its own result.

To tell this error from others, read its code:

```go
var e *sqlite.Error
if errors.As(err, &e) && e.Code() == sqlite.BUSY {
	// database is locked
}
```

Another process that writes the same file, such as a script or the `sqlite3` shell, is not in the queue of the driver: it meets the app at the lock of SQLite, as with any driver.

### Reject a duplicate

Say two visitors must not book the same slot, and the table has `UNIQUE (doctor_slug, slot)`. Let the database decide in one statement. `ON CONFLICT ... DO NOTHING` inserts no row when the slot is taken, so `RETURNING` returns no row, and `Scan` returns `sql.ErrNoRows`. Read that as "taken". It is atomic, so two requests at once cannot both win:

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

A failed constraint comes back as a `*sqlite.Error` with the extended code `sqlite.CONSTRAINT_UNIQUE`. Where `ON CONFLICT` does not fit, for example an `UPDATE`, check for it:

```go
var e *sqlite.Error
if errors.As(err, &e) && e.ExtendedCode() == sqlite.CONSTRAINT_UNIQUE {
	return tifa.Fields{"slot": "That slot is taken."}
}
```

Import the driver by name for this, `"github.com/liustack/sqlite"`, instead of with `_`.

## Back it up

An app from `tifa new` copies every commit of its database to Cloudflare R2, AWS S3 or another S3 store about a second later, once you set the option `db.backup`. The copy runs inside the app, with the driver's [`replica`](https://github.com/liustack/sqlite/blob/main/docs/replication.md) package: there is nothing else to install or run. A write never waits for the upload, and a store that is down never makes a write fail.

### Turn it on with Cloudflare R2

1. In the Cloudflare dashboard, open **R2 object storage** and select **Create bucket**. Name it, such as `notes-backup`.
2. On the **Overview** page of R2, select **Manage** next to **API Tokens**, then **Create Account API token**. Give it **Object Read & Write**, limited to that bucket. Copy the **Access Key ID** and the **Secret Access Key**: the secret is shown once. The page also shows the S3 endpoint of your account, `https://<account>.r2.cloudflarestorage.com` ([R2 API tokens](https://developers.cloudflare.com/r2/api/tokens/)).
3. On the server, set the two keys first, then the URL of the bucket. Each command asks for the value and restarts the app (see [Settings and secrets on the server](../build-and-deploy/deploy.md#settings-and-secrets-on-the-server)). The app refuses to start with `DB_BACKUP` and no keys, so the order matters:

   ```sh
   go tool tifa deploy env set AWS_ACCESS_KEY_ID
   go tool tifa deploy env set AWS_SECRET_ACCESS_KEY
   go tool tifa deploy env set DB_BACKUP   # s3://notes-backup/app.db?endpoint=https://<account>.r2.cloudflarestorage.com&region=auto
   ```

   `env set` reads a value from a hidden prompt, a pipe or a page in your browser, never from an argument, so a coding agent can run these commands without seeing the keys.

The path after the bucket, `app.db` above, names the copy in the bucket. One app writes to one path: to try it on your machine, put the three lines in the `.env` of the app with another path, such as `s3://notes-backup/dev.db?...`, and never the path the server uses.

The first start with `DB_BACKUP` uploads the whole database, and from then on only the changes. `go tool tifa deploy logs` shows what goes wrong in the background, as lines `database backup` with the error.

### What the app does with it

`db.go` and `hooks.go` from `tifa new` hold all of it:

- **At start**, before the database opens: when the file at `db.path` does not exist, the app first restores it from the bucket. An empty bucket means a first start, and the app goes on with an empty database. Then it starts the copy.
- **While it runs**, each commit is in the bucket about a second later. `notes.Backup.Sync(ctx)` waits until everything committed so far is there, for a write that must be safe before you answer.
- **At stop**, `OnShutdown` closes the database, then the copy, which tries for up to 10 seconds to upload what is left. What it could not upload stays in the folder `app.db-replica` next to the database and goes up at the next start.
- **Errors in the background**, such as a store that does not answer, go to the log with `slog.Error`. The copy tries again by itself.

An app made before `tifa new` had this needs three pieces. In `options.go`:

```go
DBBackup           = tifa.String("db.backup", "", "S3 URL to copy the database to, empty for none")
AWSAccessKeyID     = tifa.Secret("aws.access_key_id", "Access key ID of the S3 store of db.backup")
AWSSecretAccessKey = tifa.Secret("aws.secret_access_key", "Secret access key of the S3 store of db.backup")
```

The keys are options of the app, not only environment variables, because Tifa reads `.env` as settings and does not put it into the environment of the process. In `db.go`:

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

In `hooks.go`, start it before `OpenDB` and close it after the database:

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

Add `*.db-replica/` to `.gitignore`. The database must be opened by this driver, in this program, after `StartBackup`: the copy follows only the writes it sees. A script that writes the file with the `sqlite3` shell makes it upload the whole database again.

### Move to a new machine

Set up the new server, set the same three values there with `tifa deploy env set`, and deploy as usual (see [Deploying](../build-and-deploy/deploy.md)). The new server has no database file, so the app restores the newest copy at its first start, before it answers a request. You lose at most the last second or so of writes before the old machine went away.

Stop the app on the old machine first if it still runs. Two apps that write to the same path in the bucket break the copy: the second one is refused when it notices, but not in every case.

### Undo a bad deploy

Say a deploy at 10:42 UTC deleted the wrong rows. Go back to the database as it was a second before: restore that moment into a new file on your machine, check it, upload it as the newest state, then let the server restore it.

You need a small program for the restore and the upload. Keep it in the app as `restore/main.go`:

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

Then:

1. **Find the time.** `go tool tifa deploy status` names each release by the UTC time of its deploy, such as `20261124T104200Z`. Pick the second before it.
2. **Restore into a new file**, on your machine, with the values the server uses. The app keeps running meanwhile:

   ```sh
   export DB_BACKUP='s3://notes-backup/app.db?endpoint=https://<account>.r2.cloudflarestorage.com&region=auto'
   export AWS_ACCESS_KEY_ID=<access key id>
   read -rs AWS_SECRET_ACCESS_KEY && export AWS_SECRET_ACCESS_KEY
   go run ./restore -at 2026-11-24T10:41:59Z restored.db
   ```

   A restore checks every file it downloads and runs `PRAGMA integrity_check` before it writes `restored.db`. Within the last 30 minutes it goes back to about the second. Further back, it usually lands up to a minute before the time you gave, because older changes are kept merged a minute at a time. It goes back a day at least.
3. **Check it**, without changing it: `sqlite3 -readonly restored.db 'SELECT count(*) FROM todos'`.
4. **Stop the app** on the server, so that nothing else writes to the bucket:

   ```sh
   ssh tifa@<server> systemctl --user stop tifa-notes.socket tifa-notes.service
   ```

5. **Upload the file** as the newest state: `go run ./restore -upload restored.db`. If it fails, run it again.
6. **Move the old database aside and start the app.** It finds no database file and restores the newest state, the one you uploaded:

   ```sh
   ssh tifa@<server>
   cd /srv/tifa/notes/shared && mkdir before-restore && mv app.db* before-restore/
   systemctl --user start tifa-notes.socket tifa-notes.service
   ```

The writes after the time you picked are gone from the app, the good ones too. `before-restore/app.db` still has them, if you need to copy some back by hand.

Skip step 5 and the app refuses to start in step 6 with `replica: the replica and the database went apart` (`replica.ErrDiverged`): the file on the server and the bucket no longer tell the same story, and the copy does not pick one for you. The same error stops an app that finds a database file with no history in the bucket, such as an `app.db` copied in by hand. Move it aside as in step 6, or upload it first with `-upload`.

### What it costs on R2

While the app writes, the copy uploads about one file a second, plus a merge each minute and each hour. An idle database uploads nothing. R2 counts each upload as a Class A operation: 1 million a month are free, then $4.50 a million. Storage is free up to 10 GB a month, and downloads are free ([R2 pricing](https://developers.cloudflare.com/r2/pricing/)). A database written to in every second of a 30-day month makes about 2.6 million uploads, about $7 a month. One written to in fewer than about 11 days' worth of seconds a month stays in the free tier. To stay there whatever the load, add `SyncInterval: 3 * time.Second` to the `replica.Config` in `StartBackup`: you then lose up to about 3 seconds of writes with the machine instead of 1. The numbers are the driver's ([Replication](https://github.com/liustack/sqlite/blob/main/docs/replication.md#what-it-costs-on-r2)).

### A one-off copy

For a single copy of the database, such as before a risky migration, ask SQLite to write one. Copying `app.db` while the app runs can give a broken copy:

```go
_, err := notes.DB.ExecContext(ctx, `VACUUM INTO ?`, "/var/backups/notes/2026-11-04.db")
```

The file is a complete database, safe to move off the machine. `VACUUM INTO` fails if the target file exists.

## Test on a temporary database

Tests replace the variable ([Structuring an app](../start/structure.md#2-shared-resources-and-other-dependencies)). Settings keep their defaults in `go test`, so a test opens its own database in a temporary folder. Put the helper in a small package so every domain can use it, and import it from test files only:

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

The root package cannot import `testdb` in its own tests (the import would loop back), so its tests keep a three-line helper of their own. Route code is tested with `tifatest.NewCtx`, see [Testing](../pages-and-data/testing-apps.md#testing).

## When to use Postgres

Stay on SQLite while the app runs as one program on one machine. Move to Postgres when you need a second server, or when your host offers managed backups and you want them. The pure Go driver is [pgx](https://github.com/jackc/pgx) with its `database/sql` adapter:

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

Sessions can live in the same database: [Sessions](../pages-and-data/sessions.md#where-to-store-sessions).
