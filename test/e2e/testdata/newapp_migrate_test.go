// This file goes into the root package of an app made with tifa new (TestNewAppMigrates copies it
// there). It checks what migrate.go does beyond the tests the app has of it.
package hello

import (
	"bytes"
	"database/sql"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

// openFiles opens the database at path with the migrations in files, and fails the test on an
// error.
func openFiles(t *testing.T, path string, files fstest.MapFS) *sql.DB {
	t.Helper()
	db, err := openDB(path, files)
	if err != nil {
		t.Fatal(err)
	}
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
		t.Fatal(err)
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

// A file that changed after it ran stops the start, and the error names the safe fix only.
func TestMigrateRefusesAChangedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")
	files := fstest.MapFS{
		"migrations/001_notes.sql": {Data: []byte("CREATE TABLE notes (id INTEGER PRIMARY KEY);\n")},
		"migrations/002_title.sql": {Data: []byte("ALTER TABLE notes ADD COLUMN title TEXT;\n")},
	}
	openFiles(t, path, files).Close()
	files["migrations/002_title.sql"] = &fstest.MapFile{Data: []byte("ALTER TABLE notes ADD COLUMN title TEXT NOT NULL DEFAULT '';\n")}
	_, err := openDB(path, files)
	if err == nil || !strings.Contains(err.Error(), "migrations/002_title.sql changed after it ran") ||
		!strings.Contains(err.Error(), "git log -p migrations/002_title.sql") || !strings.Contains(err.Error(), "migrations/003_<what>.sql") {
		t.Fatalf("err %v, want one that says 002_title.sql changed, how to put it back and the next file", err)
	}
}

// A program whose files the database has all run, and more, runs on it: that is the release a
// rollback goes back to. It logs the files it does not have.
func TestMigrateRunsOnANewerDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")
	older := fstest.MapFS{"migrations/001_notes.sql": {Data: []byte("CREATE TABLE notes (id INTEGER PRIMARY KEY);\n")}}
	newer := fstest.MapFS{
		"migrations/001_notes.sql": older["migrations/001_notes.sql"],
		"migrations/002_title.sql": {Data: []byte("ALTER TABLE notes ADD COLUMN title TEXT;\n")},
	}
	openFiles(t, path, newer).Close()
	buf := logs(t)
	db := openFiles(t, path, older)
	defer db.Close()
	execSQL(t, db, "INSERT INTO notes (id) VALUES (1)")
	if line := buf.String(); !strings.Contains(line, "level=WARN") || !strings.Contains(line, "002_title.sql") || strings.Count(line, "\n") != 1 {
		t.Fatalf("the log is %q, want one warning that names 002_title.sql", line)
	}
}

// A database that the runner the docs had before migrated counts its files up to PRAGMA
// user_version as run, and records them.
func TestMigrateTakesOverFromUserVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")
	files := fstest.MapFS{
		"migrations/001_notes.sql": {Data: []byte("CREATE TABLE notes (id INTEGER PRIMARY KEY);\n")},
		"migrations/002_title.sql": {Data: []byte("ALTER TABLE notes ADD COLUMN title TEXT;\n")},
		"migrations/003_body.sql":  {Data: []byte("ALTER TABLE notes ADD COLUMN body TEXT;\n")},
	}
	old, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	execSQL(t, old, "CREATE TABLE notes (id INTEGER PRIMARY KEY, title TEXT); PRAGMA user_version = 2")
	old.Close()
	db := openFiles(t, path, files)
	defer db.Close()
	if n := countRows(t, db, "SELECT count(*) FROM schema_migrations"); n != 3 {
		t.Fatalf("schema_migrations has %d rows, want 3", n)
	}
	execSQL(t, db, "INSERT INTO notes (title, body) VALUES ('a', 'b')")
}

// A database made by the schema.sql of an older tifa new takes 001_init.sql as run, since its
// statements say IF NOT EXISTS, and keeps its rows.
func TestMigrateTakesOverFromSchemaSQL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")
	old, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	execSQL(t, old, `CREATE TABLE IF NOT EXISTS sessions (
  key    TEXT    PRIMARY KEY,
  data   BLOB    NOT NULL,
  expiry INTEGER NOT NULL
) WITHOUT ROWID;
CREATE INDEX IF NOT EXISTS sessions_by_expiry ON sessions (expiry);
INSERT INTO sessions (key, data, expiry) VALUES ('k', x'00', 1)`)
	old.Close()
	db, err := OpenDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if n := countRows(t, db, "SELECT count(*) FROM sessions"); n != 1 {
		t.Fatalf("%d sessions, want the 1 that was there", n)
	}
}

// A file that leaves a row pointing at a row that is not there runs not at all.
func TestMigrateChecksTheForeignKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")
	files := fstest.MapFS{"migrations/001_lists.sql": {Data: []byte(`
CREATE TABLE lists (id INTEGER PRIMARY KEY);
CREATE TABLE items (id INTEGER PRIMARY KEY, list_id INTEGER REFERENCES lists (id));
`)}}
	db := openFiles(t, path, files)
	execSQL(t, db, "INSERT INTO lists (id) VALUES (1); INSERT INTO items (list_id) VALUES (1)")
	db.Close()
	files["migrations/002_lists.sql"] = &fstest.MapFile{Data: []byte("-- Drops every list.\nDELETE FROM lists;\nCREATE TABLE tags (id INTEGER PRIMARY KEY);\n")}
	if _, err := openDB(path, files); err == nil || !strings.Contains(err.Error(), "table items has rows that point at rows of lists") {
		t.Fatalf("err %v, want one about the items that point at no list", err)
	}
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if lists, ran := countRows(t, db, "SELECT count(*) FROM lists"), countRows(t, db, "SELECT count(*) FROM schema_migrations"); lists != 1 || ran != 1 {
		t.Fatalf("%d lists and %d files run, want the database as it was: 1 and 1", lists, ran)
	}
}

// Without db.backup, the database is copied before files run on it, and only the newest copy is
// kept. With db.backup, the backup has every earlier state, so there is no copy.
func TestMigrateCopiesTheDatabaseFirst(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.db")
	files := fstest.MapFS{"migrations/001_notes.sql": {Data: []byte("CREATE TABLE notes (id INTEGER PRIMARY KEY);\n")}}
	copies := func() []string {
		names, err := filepath.Glob(filepath.Join(dir, "app.db.before-*"))
		if err != nil {
			t.Fatal(err)
		}
		for i, name := range names {
			names[i] = filepath.Base(name)
		}
		return names
	}
	db := openFiles(t, path, files)
	execSQL(t, db, "INSERT INTO notes (id) VALUES (1)")
	db.Close()
	if got := copies(); len(got) != 0 {
		t.Fatalf("copies %q of a database that had no tables", got)
	}
	files["migrations/002_title.sql"] = &fstest.MapFile{Data: []byte("ALTER TABLE notes ADD COLUMN title TEXT;\n")}
	openFiles(t, path, files).Close()
	files["migrations/003_body.sql"] = &fstest.MapFile{Data: []byte("ALTER TABLE notes ADD COLUMN body TEXT;\n")}
	openFiles(t, path, files).Close()
	if got := copies(); len(got) != 1 || got[0] != "app.db.before-003" {
		t.Fatalf("copies %q, want app.db.before-003 only", got)
	}
	before, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "app.db.before-003"))
	if err != nil {
		t.Fatal(err)
	}
	defer before.Close()
	if n := countRows(t, before, "SELECT count(*) FROM pragma_table_info('notes') WHERE name = 'body'"); n != 0 {
		t.Fatal("the copy has the column of 003_body.sql: it was made after the file ran")
	}
	DBBackup = "s3://bucket/app.db"
	t.Cleanup(func() { DBBackup = "" })
	files["migrations/004_done.sql"] = &fstest.MapFile{Data: []byte("ALTER TABLE notes ADD COLUMN done INTEGER;\n")}
	openFiles(t, path, files).Close()
	if got := copies(); len(got) != 1 || got[0] != "app.db.before-003" {
		t.Fatalf("copies %q with db.backup set, want only the one from before", got)
	}
}
