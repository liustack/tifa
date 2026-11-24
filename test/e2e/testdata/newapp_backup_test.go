// This file goes into the root package of an app made with tifa new (TestNewAppBacksUpItsDatabase
// copies it there). It runs the backup of db.go and hooks.go against a fake S3 store.
package hello

import (
	"bytes"
	"context"
	"crypto/md5"
	"database/sql"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/liustack/sqlite/replica"
)

// fakeS3 is an S3 store in memory with what the replica package asks of one: objects with
// metadata, conditional writes, multipart uploads, ranges and listing. The bucket is in the path.
type fakeS3 struct {
	mu      sync.Mutex
	objects map[string]fakeObject  // by bucket/key
	uploads map[string][][]byte    // the parts of a multipart upload, by its ID
	meta    map[string]http.Header // the metadata of a multipart upload, by its ID
	next    int                    // the last upload ID
}

type fakeObject struct {
	body     []byte
	meta     http.Header
	modified time.Time
}

func (o fakeObject) etag() string {
	sum := md5.Sum(o.body)
	return `"` + hex.EncodeToString(sum[:]) + `"`
}

func s3Error(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	fmt.Fprintf(w, "<Error><Code>%s</Code><Message>%s</Message></Error>", code, code)
}

func metaOf(h http.Header) http.Header {
	m := http.Header{}
	for name, v := range h {
		if strings.HasPrefix(strings.ToLower(name), "x-amz-meta-") {
			m[name] = v
		}
	}
	return m
}

func (s *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		s3Error(w, 400, "IncompleteBody")
		return
	}
	bucket, key, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
	q := r.URL.Query()
	name := bucket + "/" + key
	// put stores body with meta, unless If-None-Match is * and the key has an object.
	put := func(body []byte, meta http.Header) bool {
		if _, ok := s.objects[name]; ok && r.Header.Get("If-None-Match") == "*" {
			s3Error(w, http.StatusPreconditionFailed, "PreconditionFailed")
			return false
		}
		o := fakeObject{body: body, meta: meta, modified: time.Now().UTC()}
		s.objects[name] = o
		w.Header().Set("ETag", o.etag())
		return true
	}
	switch {
	case key == "" && r.Method == http.MethodGet: // ListObjectsV2, in one page
		type entry struct {
			Key          string
			Size         int
			LastModified string
			ETag         string
		}
		var list struct {
			XMLName     xml.Name `xml:"ListBucketResult"`
			IsTruncated bool
			Contents    []entry
		}
		for n, o := range s.objects {
			k, _ := strings.CutPrefix(n, bucket+"/")
			if strings.HasPrefix(n, bucket+"/") && strings.HasPrefix(k, q.Get("prefix")) && k > q.Get("start-after") {
				list.Contents = append(list.Contents, entry{k, len(o.body), o.modified.Format(time.RFC3339Nano), o.etag()})
			}
		}
		slices.SortFunc(list.Contents, func(a, b entry) int { return strings.Compare(a.Key, b.Key) })
		w.Header().Set("Content-Type", "application/xml")
		xml.NewEncoder(w).Encode(list)
	case r.Method == http.MethodPost && q.Has("uploads"):
		s.next++
		id := strconv.Itoa(s.next)
		s.uploads[id] = nil
		s.meta[id] = metaOf(r.Header)
		fmt.Fprintf(w, "<InitiateMultipartUploadResult><UploadId>%s</UploadId></InitiateMultipartUploadResult>", id)
	case r.Method == http.MethodPut && q.Has("partNumber"):
		n, _ := strconv.Atoi(q.Get("partNumber"))
		parts := s.uploads[q.Get("uploadId")]
		for len(parts) < n {
			parts = append(parts, nil)
		}
		parts[n-1] = body
		s.uploads[q.Get("uploadId")] = parts
		w.Header().Set("ETag", fmt.Sprintf(`"part%d"`, n))
	case r.Method == http.MethodPost && q.Has("uploadId"):
		// The metadata came with the start of the upload, the condition comes with its end.
		id := q.Get("uploadId")
		parts, meta := s.uploads[id], s.meta[id]
		delete(s.uploads, id)
		delete(s.meta, id)
		if put(bytes.Join(parts, nil), meta) {
			fmt.Fprint(w, "<CompleteMultipartUploadResult></CompleteMultipartUploadResult>")
		}
	case r.Method == http.MethodDelete && q.Has("uploadId"):
		delete(s.uploads, q.Get("uploadId"))
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodPut:
		put(body, metaOf(r.Header))
	case r.Method == http.MethodDelete:
		delete(s.objects, name)
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodGet || r.Method == http.MethodHead:
		o, ok := s.objects[name]
		if !ok {
			s3Error(w, http.StatusNotFound, "NoSuchKey")
			return
		}
		for n, v := range o.meta {
			w.Header()[n] = v
		}
		w.Header().Set("ETag", o.etag())
		w.Header().Set("Last-Modified", o.modified.Format(http.TimeFormat))
		data, status := o.body, http.StatusOK
		if rng, ok := strings.CutPrefix(r.Header.Get("Range"), "bytes="); ok {
			from, to, _ := strings.Cut(rng, "-")
			start, _ := strconv.Atoi(from)
			end := len(o.body) - 1
			if to != "" {
				end, _ = strconv.Atoi(to)
				end = min(end, len(o.body)-1)
			}
			if start > end {
				s3Error(w, http.StatusRequestedRangeNotSatisfiable, "InvalidRange")
				return
			}
			data, status = o.body[start:end+1], http.StatusPartialContent
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(o.body)))
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.WriteHeader(status)
		if r.Method == http.MethodGet {
			w.Write(data)
		}
	default:
		s3Error(w, http.StatusNotImplemented, "NotImplemented")
	}
}

// startBackupStore starts a fake S3 store over HTTPS, sets the options of the app to back up a
// database in a temporary folder to it, and returns the URL of the replica and the folder.
func startBackupStore(t *testing.T) (url, dir string) {
	t.Helper()
	srv := httptest.NewTLSServer(&fakeS3{objects: map[string]fakeObject{}, uploads: map[string][][]byte{}, meta: map[string]http.Header{}})
	t.Cleanup(srv.Close)
	// The replica package makes its client from http.DefaultTransport: this one trusts the
	// certificate of the fake store.
	old := http.DefaultTransport
	http.DefaultTransport = srv.Client().Transport
	t.Cleanup(func() { http.DefaultTransport = old })

	dir = t.TempDir()
	url = "s3://backups/app.db?endpoint=" + srv.URL + "&region=auto"
	path, backup, id, secret := DBPath, DBBackup, AWSAccessKeyID, AWSSecretAccessKey
	DBPath, DBBackup = filepath.Join(dir, "app.db"), url
	AWSAccessKeyID, AWSSecretAccessKey = "test-key", "test-secret"
	t.Cleanup(func() { DBPath, DBBackup, AWSAccessKeyID, AWSSecretAccessKey = path, backup, id, secret })
	return url, dir
}

// notesIn returns the notes in db, in the order they were added, and none before the table is
// there.
func notesIn(t *testing.T, db *sql.DB) []string {
	t.Helper()
	var tables int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_schema WHERE name = 'notes'`).Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if tables == 0 {
		return nil
	}
	rows, err := db.Query(`SELECT text FROM notes ORDER BY rowid`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var notes []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		notes = append(notes, s)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return notes
}

func addNotes(t *testing.T, notes ...string) {
	t.Helper()
	if _, err := DB.Exec(`CREATE TABLE IF NOT EXISTS notes (text TEXT)`); err != nil {
		t.Fatal(err)
	}
	for _, n := range notes {
		if _, err := DB.Exec(`INSERT INTO notes (text) VALUES (?)`, n); err != nil {
			t.Fatal(err)
		}
	}
}

// restoreNotes restores the replica at url into a new file, and returns its notes.
func restoreNotes(t *testing.T, url string, opt replica.RestoreOptions) []string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "restored.db")
	if _, err := replica.Restore(context.Background(), path, replica.Config{URL: url, Credentials: backupCredentials()}, opt); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	return notesIn(t, db)
}

func backupCredentials() replica.CredentialsProvider {
	return replica.StaticCredentials(replica.Credentials{AccessKeyID: AWSAccessKeyID, SecretAccessKey: AWSSecretAccessKey})
}

// removeDatabase removes the database at DBPath with the files beside it, as on a new machine.
func removeDatabase(t *testing.T) {
	t.Helper()
	files, err := filepath.Glob(DBPath + "*")
	if err != nil || len(files) == 0 {
		t.Fatalf("no database files at %s: %v", DBPath, err)
	}
	for _, f := range files {
		if err := os.RemoveAll(f); err != nil {
			t.Fatal(err)
		}
	}
}

// With db.backup set, every commit reaches the store about a second later, without a call to
// Sync, and a start without the database file restores it from there.
func TestBackupCopiesCommitsAndRestoresThem(t *testing.T) {
	url, _ := startBackupStore(t)
	ctx := context.Background()
	if err := OnStart(ctx); err != nil {
		t.Fatalf("the first start, with an empty store: %v", err)
	}
	addNotes(t, "one", "two", "three")
	want := []string{"one", "two", "three"}
	for start := time.Now(); ; time.Sleep(200 * time.Millisecond) {
		if got := restoreNotes(t, url, replica.RestoreOptions{}); slices.Equal(got, want) {
			break
		} else if time.Since(start) > 10*time.Second {
			t.Fatalf("the store holds %q 10s after the commits, want %q", got, want)
		}
	}
	if err := OnShutdown(ctx); err != nil {
		t.Fatal(err)
	}

	removeDatabase(t)
	if err := OnStart(ctx); err != nil {
		t.Fatalf("a start without the database: %v", err)
	}
	if got := notesIn(t, DB); !slices.Equal(got, want) {
		t.Errorf("after the restore: %q, want %q", got, want)
	}
	// The backup goes on from the restored database.
	addNotes(t, "four")
	if err := Backup.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if err := OnShutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if got, want := restoreNotes(t, url, replica.RestoreOptions{}), []string{"one", "two", "three", "four"}; !slices.Equal(got, want) {
		t.Errorf("the store after the restart: %q, want %q", got, want)
	}
}

// The steps of docs/guides/database.md to undo a bad write: restore the state from before it into
// a new file, upload that file as the newest state with FreshStart, then start the app without
// its database file.
func TestBackupGoesBackToAnEarlierTime(t *testing.T) {
	url, _ := startBackupStore(t)
	ctx := context.Background()
	if err := OnStart(ctx); err != nil {
		t.Fatal(err)
	}
	addNotes(t, "kept")
	if err := Backup.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	before := time.Now()
	time.Sleep(10 * time.Millisecond)
	if _, err := DB.Exec(`DELETE FROM notes`); err != nil {
		t.Fatal(err)
	}
	addNotes(t, "wrong")
	if err := Backup.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if err := OnShutdown(ctx); err != nil {
		t.Fatal(err)
	}

	restored := filepath.Join(t.TempDir(), "restored.db")
	cfg := replica.Config{URL: url, Credentials: backupCredentials(), FreshStart: true}
	if _, err := replica.Restore(ctx, restored, cfg, replica.RestoreOptions{At: before}); err != nil {
		t.Fatal(err)
	}
	// Check it, as the docs do with sqlite3 -readonly.
	check, err := sql.Open("sqlite", "file:"+restored+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := notesIn(t, check), []string{"kept"}; !slices.Equal(got, want) {
		t.Errorf("restored before the bad write: %q, want %q", got, want)
	}
	check.Close()
	r, err := replica.New(ctx, restored, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}

	// Without the upload above, the app would refuse to start on the restored file
	// (ErrDiverged). Without its database file, it restores the newest state.
	removeDatabase(t)
	if err := OnStart(ctx); err != nil {
		t.Fatal(err)
	}
	defer OnShutdown(ctx)
	if got, want := notesIn(t, DB), []string{"kept"}; !slices.Equal(got, want) {
		t.Errorf("after going back: %q, want %q", got, want)
	}
}

// db.backup without the keys stops the start with what to set.
func TestBackupNeedsTheKeys(t *testing.T) {
	startBackupStore(t)
	AWSAccessKeyID, AWSSecretAccessKey = "", ""
	err := OnStart(context.Background())
	if err == nil {
		OnShutdown(context.Background())
		t.Fatal("OnStart started without the keys")
	}
	if !strings.Contains(err.Error(), "AWS_ACCESS_KEY_ID") {
		t.Errorf("the error does not say what to set: %v", err)
	}
}
