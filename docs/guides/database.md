---
summary: 'Keep data with SQLite: migrations and the rules that keep a rollback working, `database is locked`, `db.backup` to R2 or S3, Postgres'
audience: app
order: 2
read_when:
  - Adding a database to a Tifa app, or growing the one `tifa new` wrote
  - Changing a table of an app that is already running
  - Writing a file in `migrations/`, or the app does not start because of one
  - Deciding between SQLite and Postgres for a Tifa app
  - Writing Go tests for code that reads or writes the database
  - A write fails with `database is locked` or `SQLITE_BUSY`
  - Backing up the database, moving it to a new machine, or undoing a bad deploy by going back to an earlier time
---

# Keeping data in SQLite

Tifa has no database layer. Your app opens a database in `OnStart` and uses it with `database/sql`, like any Go program. This guide takes the SQLite setup that [`tifa new`](../build-and-deploy/create-app.md#tifa-new) writes and adds what a real app needs next: a setting for the file, migrations, a transaction, a backup and tests.

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
	"io/fs"
	"net/url"

	_ "github.com/liustack/sqlite"
)

// DB is the database of the app. OnStart opens it and OnShutdown closes it.
var DB *sql.DB

// OpenDB opens the database at path and runs the files of migrations/ that it has not run yet.
func OpenDB(path string) (*sql.DB, error) {
	return openDB(path, migrationFiles)
}

// openDB is OpenDB with the migrations/ folder of files, which the tests of migrate.go replace.
func openDB(path string, files fs.FS) (*sql.DB, error) {
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
	// migrate.go runs the files of migrations/ that the database has not run yet.
	if err := migrate(context.Background(), db, path, files); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}
```

```go
// hooks.go
func OnStart(ctx context.Context) error {
	db, err := OpenDB(DBPath)
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
- On a server, put the file in a folder meant for data and name it with the environment: `DB_PATH=/var/lib/notes/app.db`. [Deploying](../build-and-deploy/deploy-by-hand.md#run-it) says to back that folder up or mount it in a container.
- SQLite in WAL mode keeps two more files next to it, `app.db-wal` and `app.db-shm`. Do not copy the three by hand while the app runs, see the backup below. Keep all three out of git (the `.gitignore` of `tifa new` does).
- One program owns the file. Two copies of the app on the same file work for a short overlap during a deploy, but a second server needs [Postgres](#when-to-use-postgres).

## Change the tables

The tables of an app from `tifa new` are a list of numbered SQL files in `migrations/`, one per change. At every start, `OpenDB` runs the files the database has not run yet, in order, before the app listens. The runner is `migrate.go`, code of your app.

```text
migrations/
  001_init.sql                the tables tifa new made
  002_todo_created_at.sql
  003_jobs.sql
migrate.go                    the runner
migrate_test.go               its tests, and the one that writes schema.sql
schema.sql                    the tables as the files leave them, for reading
```

### Add a migration

Write the change as the next file:

```sql
-- migrations/002_todo_created_at.sql
ALTER TABLE todos ADD COLUMN created_at TEXT NOT NULL DEFAULT '';
UPDATE todos SET created_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now');
```

Then write `schema.sql` again and commit both:

```sh
go test -run TestSchema -update
```

`schema.sql` is the one file that shows every table as it is now. Never edit it: `TestSchema` writes it from the migrations and fails, in `go test` and `go tool tifa check`, when it does not match them. A table changed in `schema.sql` by hand would never reach a database that exists.

A file is SQL only. Data work that needs Go, such as calling a service for every row, goes in the Go code of the app, such as a [background job](jobs.md).

### What the runner does

- **All or nothing.** SQLite can roll back table changes, so all the files of one start run in one transaction. A file that fails, or a program stopped halfway, leaves the database as it was, and `OnStart` returns the error, so the app does not start.
- **Before the first request.** `OnStart` finishes before the app listens, so no request sees a half-changed table.
- **It remembers each file.** The table `schema_migrations` holds the name and the SHA-256 of each file that ran. A file that changed after it ran stops the start with `migrations/002_todo_created_at.sql changed after it ran on this database. Put it back as it was ... and write the change in a new file, migrations/004_<what>.sql`. There is no way past it: an edit to a file that ran never reaches the databases that ran it.
- **It runs on newer tables.** When the database ran files the program does not have, because a newer release ran them and was rolled back, it logs one warning that names them and starts.
- **It keeps the rows that point at a rebuilt table.** It turns foreign keys off for its run, on a connection of its own that it closes afterwards, and runs `PRAGMA foreign_key_check` before it commits. A row that points at a row that is not there fails the run. With foreign keys on, the `DROP TABLE` of a [table rebuild](#change-a-column-type-or-a-constraint) would delete the rows of other tables that point at it with `ON DELETE CASCADE`.
- **It copies the database first, without a backup.** When the option `db.backup` is empty and files are about to run on a database that has tables, it first writes a copy next to it, `app.db.before-002` for a run that starts with file 2, and deletes the copy of the run before. With `db.backup` set, the backup already holds every earlier state, see [Undo a bad deploy](#undo-a-bad-deploy).
- **The build does not run migrations.** `tifa build` only reads your code ([Philosophy](../start/philosophy.md#do-things-the-go-way)), so it needs no database.

A file that ran only on your machine may still change, such as the newest one before you deploy it: move your local `app.db` aside, and the next start makes it again from all the files.

An app made before `tifa new` wrote `migrations/` runs `schema.sql` at every start. To move over, put its statements into `migrations/001_init.sql` as they are, with `IF NOT EXISTS`, and take `migrate.go`, `migrate_test.go` and the `openDB` part of `db.go` from a new app. On a database that exists, `001_init.sql` then changes nothing and is recorded as run.

### Keep a rollback working

[`tifa deploy rollback`](../build-and-deploy/deploy.md#roll-back), and a deploy that fails its check, start an older release on the same database. The database does not go back: it would lose what visitors wrote since. So an older release must work on the tables of every newer one:

- **Never edit a file that ran anywhere but on your machine.** Write the next file.
- **Each release must run on the tables of the next.** These are safe: add a table, add a column that may be `NULL` or has a default, add an index. These add something and still break older code: a `UNIQUE` index or a `CHECK` that the writes of the older code fail, and an `UPDATE` that changes what values mean, such as `'done'` becoming `'completed'`.
- **Name the columns in every `SELECT` and `INSERT`.** With `SELECT *`, a column added later makes `Scan` fail with `sql: expected 4 destination arguments in Scan, not 3`. `INSERT INTO todos VALUES (...)` fails on the column count.
- **Rename or drop over three releases.** First add the new column and write to both. Then read the new one. Drop the old one in a later release, once no release you could roll back to uses it: rollback reaches the last 5 releases that passed their check, which `go tool tifa deploy status` lists. A file that drops or renames a table or a column starts with a comment that says why it is safe, and `TestDropsSayWhy` in `migrate_test.go` fails without one.

### Change a column type or a constraint

SQLite's `ALTER TABLE` adds, renames and drops a column, and renames a table. Anything else, such as a column type, `NOT NULL`, a default, a `CHECK` or a foreign key, means building the table again ([SQLite's steps](https://www.sqlite.org/lang_altertable.html#otheralter)). The runner already turns foreign keys off and checks them, so the file holds the rest. `migrations/004_todo_done_check.sql`:

```sql
-- Builds todos again to add CHECK (done IN (0, 1)), with every column it had.
CREATE TABLE new_todos (
  id    INTEGER PRIMARY KEY,
  title TEXT    NOT NULL,
  done  INTEGER NOT NULL DEFAULT 0 CHECK (done IN (0, 1))
);
INSERT INTO new_todos (id, title, done) SELECT id, title, done FROM todos;
DROP TABLE todos;
ALTER TABLE new_todos RENAME TO todos;
-- DROP TABLE removed the indexes and triggers of todos: make them again here.
```

- Name the columns in the `INSERT ... SELECT`, so a column cannot land in another one.
- The new table keeps every column, so older releases still work on it, unless the new constraint fails their writes.
- The first line says what the rebuild keeps. `TestDropsSayWhy` asks for it, since the file holds a `DROP TABLE`.

### How long a migration may take

`tifa deploy` gives a new release 30 seconds to answer its [health check](../build-and-deploy/deploy.md#deploy), or the time you set with `deploy.start_timeout`. The migrations, the copy of the database when `db.backup` is empty, and the rest of `OnStart` must fit in that time. When they do not, the deploy counts the release as failed and starts the one before: the transaction never committed, so the database is as it was, and the next deploy of the same release fails the same way.

Adding a column takes no time, whatever the size of the table. Building an index on a large table, or rebuilding one, can take longer. Write such a file so that it can run twice, such as `CREATE INDEX IF NOT EXISTS todos_by_done ON todos (done);`, run that statement by hand on the server at a quiet time, then deploy: the file then finds the index and is done at once. Keep `db.backup` on for a large database, since the copy writes the whole file.

### Postgres

The runner is SQLite's. A Postgres app uses [goose](https://github.com/pressly/goose), a Go library that runs embedded SQL files, with the same rules: one numbered file per change, never edit one that ran, each release runs on the tables of the next. goose also starts on a database that a newer release migrated. See [When to use Postgres](#when-to-use-postgres).

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

- A missing row becomes `tifa.ErrNotFound`, which answers 404 ([route.go](../pages-and-data/loading-data.md#routego)).
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

SQLite lets one write in at a time. The driver queues the writes of the app in the order they came, so a write waits for the ones ahead of it instead of failing. What your code does:

- **A write on its own**, a plain `db.ExecContext`, needs nothing more: the driver queues it with the others and may commit several in one go, each with its own result.
- **A transaction that reads and then writes**, such as reading a stock and then taking from it, needs `_txlock=immediate`, as `OpenDB` above and the `db.go` of `tifa new` have. Every transaction then takes the write lock at `BEGIN` and waits its turn there. Without it, `BEGIN` takes no lock: the transaction reads, another request writes and commits, and the first write of the transaction fails however long `busy_timeout` is.
- **A transaction that only reads** starts with `db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})`. It takes no write lock and does not wait behind the writes. A query outside a transaction needs neither.

With that, `database is locked` (`SQLITE_BUSY`) means one of these:

- **Writes came faster than the disk can commit them**, for longer than `busy_timeout` (5 seconds by default). That is real overload: answer with an error that says to try again later, or let fewer writes in. A longer timeout only waits longer for the same disk.
- **Another process writes the same file**, such as a script or the `sqlite3` shell. The queue is inside one process, and between processes SQLite's own lock applies. Give the other process a `busy_timeout` too (`.timeout 5000` in the shell).
- **A transaction that read before it wrote was started without `_txlock=immediate`.** Run the whole transaction again from `BEGIN`, not only the statement that failed, or better, open the database with `_txlock=immediate`.
- **`busy_timeout` is 0.** A write then fails at once whenever another holds the lock.

Check for it:

```go
var e *sqlite.Error
if errors.As(err, &e) && e.Code() == sqlite.BUSY {
	// overload, or another process
}
```

Keep `_pragma=journal_mode(WAL)` too: without it, a write cannot commit while another connection reads, so reads can get `database is locked` as well. The driver's [database is locked](https://github.com/liustack/sqlite/blob/main/docs/database-is-locked.md) explains each case in more detail.

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
3. On the server, set the two keys first, then the URL of the bucket. Each command asks for the value and restarts the app (see [Settings and secrets on the server](../build-and-deploy/deploy-operate.md#settings-and-secrets-on-the-server)). The app refuses to start with `DB_BACKUP` and no keys, so the order matters:

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
	"path/filepath"
	"testing"

	"example.com/notes"
)

// Use puts a fresh database in a temporary folder into notes.DB for the length of the test. The
// tests that use it must not run in parallel, since notes.DB is shared.
func Use(t *testing.T) {
	t.Helper()
	db, err := notes.OpenDB(filepath.Join(t.TempDir(), "test.db"))
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

The test database runs the real migrations, so a broken migration fails the whole suite. `migrate_test.go` from `tifa new` tests the rest: `TestMigrateTwice` opens one file twice and finds nothing run the second time, `TestSchema` checks `schema.sql`, `TestDropsSayWhy` checks the first line of the files that drop or rename, and `TestRebuildKeepsTheRowsThatPointAtTheTable` rebuilds a table that other rows point at.

The root package cannot import `testdb` in its own tests (the import would loop back), so its tests keep a three-line helper of their own. Route code is tested with `tifatest.NewCtx`, see [Testing](../pages-and-data/testing-apps.md#testing).

## When to use Postgres

Stay on SQLite while the app runs as one program on one machine. Move to Postgres when you need a second server, or when your host offers managed backups and you want them. The pure Go driver is [pgx](https://github.com/jackc/pgx) with its `database/sql` adapter:

```go
import _ "github.com/jackc/pgx/v5/stdlib"

func OpenDB(url string) (*sql.DB, error) {
	db, err := sql.Open("pgx", url)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}
```

Everything else in this guide keeps its shape: a `DB` variable opened in `OnStart`, an option for the address (`db.url`, now a `tifa.Secret` since it holds the password, see [Sending email](email.md#settings)), queries through `database/sql`. What changes: placeholders are `$1`, `$2`, the pragmas go away, and the migration runner above is SQLite's, so use goose with the same rules ([Postgres](#postgres)). Tests then need a real Postgres, so give them one through a setting and skip when it is not set.

Sessions can live in the same database: [Sessions](../pages-and-data/sessions.md#where-to-store-sessions).
