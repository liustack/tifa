// This file goes into the root package of an app made with tifa new (TestNewAppMigrates copies it
// there). It checks what migrate in db.go does with goose, beyond the tests the app has.
package hello

import (
	"bytes"
	"context"
	"database/sql"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

// run runs migrate on the database at path with the migrations in files, then opens the database
// as OpenDB does. It fails the test on an error.
func run(t *testing.T, path string, files fstest.MapFS) *sql.DB {
	t.Helper()
	if err := migrate(context.Background(), path, files); err != nil {
		t.Fatal(err)
	}
	db, err := open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// openPlain opens the database at path as the app does, without running migrations.
func openPlain(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func execSQL(t *testing.T, db *sql.DB, query string) {
	t.Helper()
	if _, err := db.Exec(query); err != nil {
		t.Fatal(err)
	}
}

func countRows(t *testing.T, db *sql.DB, query string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(query).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

// logs collects what slog writes during the test.
func logs(t *testing.T) *bytes.Buffer {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })
	return &buf
}

func file(text string) *fstest.MapFile { return &fstest.MapFile{Data: []byte(text)} }

// lists is a database where items point at lists with ON DELETE CASCADE.
func lists() fstest.MapFS {
	return fstest.MapFS{"migrations/001_lists.sql": file(`-- +goose Up
CREATE TABLE lists (id INTEGER PRIMARY KEY, name TEXT);
CREATE TABLE items (id INTEGER PRIMARY KEY, list_id INTEGER NOT NULL REFERENCES lists (id) ON DELETE CASCADE);
`)}
}

// withTwoItems migrates a new database with files, then adds a list and 2 items that point at it.
func withTwoItems(t *testing.T, path string, files fstest.MapFS) {
	db := run(t, path, files)
	execSQL(t, db, "INSERT INTO lists (id, name) VALUES (1, 'Home'); INSERT INTO items (list_id) VALUES (1), (1)")
	db.Close()
}

// A second start runs no file again, and records every file once.
func TestMigrateRunsEachFileOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")
	files := lists()
	files["migrations/002_title.sql"] = file("-- +goose Up\nALTER TABLE lists ADD COLUMN title TEXT;\n")
	buf := logs(t)
	run(t, path, files).Close()
	if got := strings.Count(buf.String(), "migrations: ran"); got != 2 {
		t.Fatalf("the first start logs %d files, want 2:\n%s", got, buf)
	}
	buf.Reset()
	db := run(t, path, files)
	if buf.Len() != 0 {
		t.Fatalf("the second start logs %q, want nothing", buf)
	}
	if n := countRows(t, db, "SELECT count(*) FROM goose_db_version WHERE version_id > 0"); n != 2 {
		t.Fatalf("goose_db_version has %d files, want 2", n)
	}
}

// After a rollback, the older release starts on the tables a newer release migrated: goose skips
// the files it does not have, and migrate logs one warning that names the versions.
func TestMigrateRunsOnANewerDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")
	older := lists()
	newer := lists()
	newer["migrations/002_title.sql"] = file("-- +goose Up\nALTER TABLE lists ADD COLUMN title TEXT;\n")
	newer["migrations/003_body.sql"] = file("-- +goose Up\nALTER TABLE lists ADD COLUMN body TEXT;\n")
	run(t, path, newer).Close()
	buf := logs(t)
	db := run(t, path, older)
	execSQL(t, db, "INSERT INTO lists (id, name) VALUES (1, 'Home')")
	line := buf.String()
	if !strings.Contains(line, "level=WARN") || !strings.Contains(line, "database_version=3") || !strings.Contains(line, "newest_file=1") || strings.Count(line, "\n") != 1 {
		t.Fatalf("the log is %q, want one warning with database_version=3 and newest_file=1", line)
	}
}

// Files run one by one, each in a transaction of its own. When the third of three fails, the
// start fails, the first two stay, and the third left nothing behind.
func TestMigrateStopsAtTheFileThatFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")
	files := lists()
	run(t, path, files).Close()
	files["migrations/002_a.sql"] = file("-- +goose Up\nALTER TABLE lists ADD COLUMN a TEXT;\n")
	files["migrations/003_b.sql"] = file("-- +goose Up\nALTER TABLE lists ADD COLUMN b TEXT;\n")
	files["migrations/004_c.sql"] = file("-- +goose Up\nALTER TABLE lists ADD COLUMN c TEXT;\nALTER TABLE nope ADD COLUMN d TEXT;\n")
	err := migrate(context.Background(), path, files)
	if err == nil || !strings.Contains(err.Error(), "version:4") || !strings.Contains(err.Error(), "no such table: nope") {
		t.Fatalf("err %v, want one that names version 4 and the missing table", err)
	}
	db := openPlain(t, path)
	columns := countRows(t, db, "SELECT count(*) FROM pragma_table_info('lists') WHERE name IN ('a', 'b', 'c')")
	if version := countRows(t, db, "SELECT max(version_id) FROM goose_db_version"); version != 3 || columns != 2 {
		t.Fatalf("version %d and %d of the columns a, b and c, want 3 and 2: the files before the one that failed stay", version, columns)
	}
}

// A database made by the schema.sql of an older tifa new takes 001_init.sql as run, since its
// statements say IF NOT EXISTS, and keeps its rows.
func TestMigrateTakesOverFromSchemaSQL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")
	execSQL(t, openPlain(t, path), `CREATE TABLE IF NOT EXISTS sessions (
  key    TEXT    PRIMARY KEY,
  data   BLOB    NOT NULL,
  expiry INTEGER NOT NULL
) WITHOUT ROWID;
CREATE INDEX IF NOT EXISTS sessions_by_expiry ON sessions (expiry);
INSERT INTO sessions (key, data, expiry) VALUES ('k', x'00', 1)`)
	db, err := OpenDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if n := countRows(t, db, "SELECT count(*) FROM sessions"); n != 1 {
		t.Fatalf("%d sessions, want the 1 that was there", n)
	}
}

// The statements "Move over from an earlier runner" in database.md give a database that the
// runner of the earlier docs migrated (PRAGMA user_version) the goose records of its files.
func TestMigrateTakesOverFromUserVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")
	files := fstest.MapFS{
		"migrations/001_notes.sql": file("-- +goose Up\nCREATE TABLE notes (id INTEGER PRIMARY KEY);\n"),
		"migrations/002_title.sql": file("-- +goose Up\nALTER TABLE notes ADD COLUMN title TEXT;\n"),
		"migrations/003_body.sql":  file("-- +goose Up\nALTER TABLE notes ADD COLUMN body TEXT;\n"),
	}
	old := openPlain(t, path)
	execSQL(t, old, "CREATE TABLE notes (id INTEGER PRIMARY KEY, title TEXT); PRAGMA user_version = 2")
	for range 2 { // they change nothing the second time
		execSQL(t, old, `CREATE TABLE IF NOT EXISTS goose_db_version (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  version_id INTEGER NOT NULL,
  is_applied INTEGER NOT NULL,
  tstamp     TIMESTAMP DEFAULT (datetime('now'))
);
INSERT INTO goose_db_version (version_id, is_applied)
  WITH RECURSIVE v(n) AS (SELECT 0 UNION ALL SELECT n + 1 FROM v WHERE n < (SELECT user_version FROM pragma_user_version))
  SELECT n, 1 FROM v WHERE NOT EXISTS (SELECT 1 FROM goose_db_version);`)
	}
	old.Close()
	db := run(t, path, files)
	execSQL(t, db, "INSERT INTO notes (title, body) VALUES ('a', 'b')")
	if n := countRows(t, db, "SELECT count(*) FROM goose_db_version"); n != 4 {
		t.Fatalf("goose_db_version has %d rows, want 4: versions 0 to 3", n)
	}
}

// rebuild is the table rebuild of database.md, "Change a column type or a constraint", for lists:
// name becomes NOT NULL. keep is the WHERE of the copy, which a test uses to lose a list.
func rebuild(keep string) string {
	return `-- Builds lists again to make name NOT NULL, with every column it had.
-- +goose Up
-- +goose NO TRANSACTION
PRAGMA foreign_keys = OFF;
BEGIN IMMEDIATE;
CREATE TABLE new_lists (id INTEGER PRIMARY KEY, name TEXT NOT NULL);
INSERT INTO new_lists (id, name) SELECT id, name FROM lists WHERE ` + keep + `;
DROP TABLE lists;
ALTER TABLE new_lists RENAME TO lists;
-- Fails when a row points at a row that is not there, so COMMIT does not run.
CREATE TEMP TABLE foreign_keys_ok (rows_pointing_nowhere INTEGER CHECK (rows_pointing_nowhere = 0));
INSERT INTO foreign_keys_ok SELECT count(*) FROM pragma_foreign_key_check;
DROP TABLE foreign_keys_ok;
COMMIT;
PRAGMA foreign_keys = ON;
`
}

// The rebuild of database.md keeps the rows that point at the table with ON DELETE CASCADE.
func TestMigrateRebuildKeepsTheRowsThatPointAtTheTable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")
	files := lists()
	withTwoItems(t, path, files)
	files["migrations/002_list_name_not_null.sql"] = file(rebuild("true"))
	db := run(t, path, files)
	if items := countRows(t, db, "SELECT count(*) FROM items"); items != 2 {
		t.Fatalf("%d items, want the 2 that point at the list", items)
	}
	if n := countRows(t, db, `SELECT "notnull" FROM pragma_table_info('lists') WHERE name = 'name'`); n != 1 {
		t.Fatal("lists.name is not NOT NULL: the rebuild did not run")
	}
	if on := countRows(t, db, "PRAGMA foreign_keys"); on != 1 {
		t.Fatalf("PRAGMA foreign_keys is %d for the app, want 1", on)
	}
}

// The rebuild as SQLite's steps give it, in goose's transaction: there PRAGMA foreign_keys does
// nothing, and the DROP TABLE deletes the items. This is why the file of database.md turns
// foreign keys off with NO TRANSACTION, before its own BEGIN.
func TestMigrateRebuildInGooseTransactionDeletesTheRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")
	files := lists()
	withTwoItems(t, path, files)
	files["migrations/002_list_name_not_null.sql"] = file(`-- Builds lists again to make name NOT NULL, with every column it had.
-- +goose Up
PRAGMA foreign_keys = OFF;
CREATE TABLE new_lists (id INTEGER PRIMARY KEY, name TEXT NOT NULL);
INSERT INTO new_lists (id, name) SELECT id, name FROM lists;
DROP TABLE lists;
ALTER TABLE new_lists RENAME TO lists;
PRAGMA foreign_keys = ON;
`)
	db := run(t, path, files)
	if items := countRows(t, db, "SELECT count(*) FROM items"); items != 0 {
		t.Fatalf("%d items: the rebuild in goose's transaction kept them, so the NO TRANSACTION of database.md may no longer be needed", items)
	}
}

// A rebuild that leaves an item pointing at no list fails the start before its COMMIT, and the
// database stays as it was: the rebuild is undone, the version is not recorded, and the app gets
// foreign keys on.
func TestMigrateBrokenRebuildLeavesTheDatabaseAsItWas(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")
	files := lists()
	withTwoItems(t, path, files)
	files["migrations/002_list_name_not_null.sql"] = file(rebuild("id <> 1"))
	err := migrate(context.Background(), path, files)
	if err == nil || !strings.Contains(err.Error(), "CHECK constraint failed: rows_pointing_nowhere = 0") {
		t.Fatalf("err %v, want the CHECK of foreign_keys_ok", err)
	}
	db := openPlain(t, path)
	if l, i := countRows(t, db, "SELECT count(*) FROM lists"), countRows(t, db, "SELECT count(*) FROM items"); l != 1 || i != 2 {
		t.Fatalf("%d lists and %d items, want 1 and 2", l, i)
	}
	if n := countRows(t, db, "SELECT count(*) FROM sqlite_schema WHERE name = 'new_lists'"); n != 0 {
		t.Fatal("new_lists is left behind")
	}
	if v := countRows(t, db, "SELECT max(version_id) FROM goose_db_version"); v != 1 {
		t.Fatalf("version %d, want 1", v)
	}
	if on := countRows(t, db, "PRAGMA foreign_keys"); on != 1 {
		t.Fatalf("PRAGMA foreign_keys is %d, want 1", on)
	}
}

// A file that turns foreign keys off and never on again leaves them off on its connection only:
// goose runs on a pool of its own, which migrate closes, and the app opens its own.
func TestMigrateKeepsForeignKeysOffOutOfTheApp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")
	files := lists()
	withTwoItems(t, path, files)
	files["migrations/002_off.sql"] = file("-- +goose Up\n-- +goose NO TRANSACTION\nPRAGMA foreign_keys = OFF;\nCREATE TABLE tags (id INTEGER PRIMARY KEY);\n")
	db := run(t, path, files)
	db.SetMaxOpenConns(1) // so a connection goose used, if the pool had one, would serve the DELETE
	execSQL(t, db, "DELETE FROM lists")
	if items := countRows(t, db, "SELECT count(*) FROM items"); items != 0 {
		t.Fatalf("%d items after their list was deleted: the app runs with foreign keys off", items)
	}
}

// Without db.backup, the database is copied before files run on a database that has run some,
// and only the newest copy is kept. With db.backup, the backup has every earlier state, so there
// is no copy.
func TestMigrateCopiesTheDatabaseFirst(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.db")
	copies := func() []string {
		names, err := filepath.Glob(filepath.Join(dir, "app.db.before-*"))
		if err != nil {
			t.Fatal(err)
		}
		return names
	}
	files := lists()
	withTwoItems(t, path, files)
	if got := copies(); len(got) != 0 {
		t.Fatalf("copies %q of a new database", got)
	}
	files["migrations/002_title.sql"] = file("-- +goose Up\nALTER TABLE lists ADD COLUMN title TEXT;\n")
	run(t, path, files).Close()
	files["migrations/003_body.sql"] = file("-- +goose Up\nALTER TABLE lists ADD COLUMN body TEXT;\n")
	run(t, path, files).Close()
	if got := copies(); len(got) != 1 || filepath.Base(got[0]) != "app.db.before-migrations" {
		t.Fatalf("copies %q, want app.db.before-migrations only", got)
	}
	before := openPlain(t, path+".before-migrations")
	if n := countRows(t, before, "SELECT count(*) FROM pragma_table_info('lists') WHERE name IN ('title', 'body')"); n != 1 {
		t.Fatal("the copy is not the database from before 003_body.sql")
	}
	before.Close()
	DBBackup = "s3://bucket/app.db"
	t.Cleanup(func() { DBBackup = "" })
	files["migrations/004_done.sql"] = file("-- +goose Up\nALTER TABLE lists ADD COLUMN done INTEGER;\n")
	run(t, path, files).Close()
	before = openPlain(t, path+".before-migrations")
	if n := countRows(t, before, "SELECT count(*) FROM pragma_table_info('lists') WHERE name = 'body'"); n != 0 {
		t.Fatal("the copy was written again with db.backup set")
	}
}
