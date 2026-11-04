---
summary: 'How to do work outside a request in a tifa app: goroutines started in OnStart that stop cleanly with OnShutdown, what tifa does when it is told to stop, tasks that run on a timer, a jobs table in SQLite for work that must survive a restart and be retried, and what not to do inside a request'
read_when:
  - Sending mail, calling a slow service or doing any work that should not make a visitor wait
  - Running something every minute, hour or day in a tifa app
  - Starting a goroutine in a tifa app and wanting it to stop cleanly on deploy
  - Work must not be lost when the program restarts, and a failure must be tried again
---

English · [简体中文](jobs.zh-CN.md)

# Background work

A tifa app is one program, so background work is a goroutine of that program. There is no queue service to run. The app starts its goroutines in `OnStart` and stops them in `OnShutdown`, like any other shared resource ([Structuring an app](../structure.md#2-shared-resources-and-other-dependencies)).

There are three cases, and each one needs a little more than the one before:

1. **A loop that may be lost.** Clearing a cache, refreshing a number. A goroutine and a ticker are enough.
2. **A task on a timer that must not be skipped.** A daily summary. The timer asks a table whether today's run was done.
3. **Work that must survive a restart and be retried.** Sending a mail. A table of jobs in the database.

The code is from a scratch app made with `tifa new` (module `example.com/notes`) that keeps its data as in [Keeping data in SQLite](database.md). The jobs table is in the same database file, so a job can be saved in the same transaction as the change that needs it.

## What tifa does when it stops

On SIGINT or SIGTERM (a deploy, Ctrl-C), in this order ([`server.shutdown_timeout`](../config.md#settings) is 10 seconds by default):

1. It closes the channel of `tifa.ShuttingDown`, which streams watch to end at once ([Streaming](streaming.md#when-the-program-stops)), stops accepting connections and waits up to `shutdown_timeout` for the requests in flight.
2. If some are still running, it cancels their contexts and waits up to `shutdown_timeout` again.
3. It calls `OnShutdown`, which gets a context with its own `shutdown_timeout`. If a request never returned, `OnShutdown` is skipped, and the program exits with an error.

tifa knows nothing of the goroutines you start. Two things follow:

- **Stop them yourself.** Start them with a context of their own and cancel it in `OnShutdown`. The context `OnStart` receives is for the calls `OnStart` makes, and [hooks.go](../app.md#hooksgo) says not to keep it. A goroutine on its own context also keeps working while the last requests finish, and one of those may still queue a job.
- **Catch their panics.** tifa turns a panic in a request into a 500 and goes on. A panic in your own goroutine ends the whole program. Every loop below recovers.

## The pieces

### Start and stop in the hooks

```go
// hooks.go
func OnStart(ctx context.Context) error {
	db, err := OpenDB(ctx, DBPath)
	if err != nil {
		return err
	}
	DB = db
	startBackground(db) // last, so an error above leaves nothing running
	return nil
}

// OnShutdown runs after a stop signal, once the requests in flight have finished. Stop the
// goroutines and wait for them before the database closes.
func OnShutdown(ctx context.Context) error {
	return errors.Join(stopBackground(ctx), DB.Close())
}
```

```go
// worker.go
package notes

import (
	"context"
	"database/sql"
	"log/slog"
	"sync"
	"time"

	"example.com/notes/jobs"
)

var (
	background       sync.WaitGroup     // the goroutines OnStart starts, which OnShutdown waits for
	cancelBackground context.CancelFunc // tells them to stop
)

// startBackground registers the handlers and starts the worker and the scheduled tasks. They run
// on a context of their own, which stopBackground ends. They keep working while the requests in
// flight finish, since one of those may still queue a job.
func startBackground(db *sql.DB) {
	var ctx context.Context
	ctx, cancelBackground = context.WithCancel(context.Background())
	jobs.Handle("mail", mailJob) // see the guide on email
	jobs.Handle("digest", digestJob)

	background.Go(func() { jobs.Run(ctx, db) })
	// Once a minute: if it is past 08:00 UTC and today's digest is not queued yet, queue it. The
	// key makes this safe to repeat, and safe across restarts.
	background.Go(func() { jobs.Every(ctx, time.Minute, queueDigest) })
	// Once an hour: forget finished jobs older than a week.
	background.Go(func() {
		jobs.Every(ctx, time.Hour, func(ctx context.Context) error {
			return jobs.Purge(ctx, db, time.Now().AddDate(0, 0, -7))
		})
	})
}

// stopBackground tells the goroutines to stop and waits until they have returned, or until ctx
// ends. The job the worker is running gets the signal too, and goes back in the queue.
func stopBackground(ctx context.Context) error {
	cancelBackground()
	done := make(chan struct{})
	go func() {
		background.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
```

`sync.WaitGroup.Go` starts a goroutine and counts it. `OnShutdown` waits for the count to reach zero, then closes the database, so no worker meets a closed database.

### The table

```sql
-- migrations/003_jobs.sql
CREATE TABLE jobs (
  id         INTEGER PRIMARY KEY,
  kind       TEXT    NOT NULL,
  payload    TEXT    NOT NULL,
  key        TEXT    UNIQUE,  -- set it for a job that must exist once, such as one per day
  run_at     INTEGER NOT NULL,  -- unix seconds, the job waits until then
  attempts   INTEGER NOT NULL DEFAULT 0,
  state      TEXT    NOT NULL DEFAULT 'queued' CHECK (state IN ('queued', 'running', 'done', 'failed')),
  last_error TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX jobs_due ON jobs (state, run_at);
```

### The package

`jobs/jobs.go` knows nothing of the rest of the app, so any domain may import it ([which way imports go](../structure.md#which-way-imports-go)). It takes the database as an argument and does not import the root package.

```go
// Package jobs is a small queue of background work kept in the SQLite database. A job is a row:
// it survives a restart, and a failed job runs again later. It knows nothing else of the app, so
// any domain may import it.
package jobs

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// A Handler does one kind of job. It returns an error to have the job tried again. It should
// stop when ctx ends.
type Handler func(ctx context.Context, payload []byte) error

var (
	handlers = map[string]Handler{}

	// MaxAttempts is how many times a job runs before it is marked failed.
	MaxAttempts = 5
	// Backoff is the wait after the first failure. It doubles after each one.
	Backoff = 30 * time.Second
	// Timeout limits one run of a handler.
	Timeout = time.Minute
	// PollEvery is how often the worker looks for due jobs.
	PollEvery = time.Second
)

// Handle sets the handler of a kind of job. Call it in OnStart, before Run.
func Handle(kind string, h Handler) { handlers[kind] = h }

// Execer is what Enqueue writes through: a *sql.DB, or a *sql.Tx to queue the job in the same
// transaction as the change that needs it.
type Execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// Enqueue adds a job that runs as soon as the worker is free.
func Enqueue(ctx context.Context, db Execer, kind string, payload any) error {
	return enqueue(ctx, db, kind, payload, "")
}

// EnqueueOnce adds a job unless one with the same key was added before, done or not. It returns
// false when the key was taken. Keys stay as long as the row does, see Purge.
func EnqueueOnce(ctx context.Context, db Execer, key, kind string, payload any) (bool, error) {
	if key == "" {
		return false, errors.New("jobs: EnqueueOnce needs a key")
	}
	err := enqueue(ctx, db, kind, payload, key)
	if errors.Is(err, errKeyTaken) {
		return false, nil
	}
	return err == nil, err
}

var errKeyTaken = errors.New("jobs: key taken")

func enqueue(ctx context.Context, db Execer, kind string, payload any, key string) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("jobs: payload of %s: %w", kind, err)
	}
	var k any // NULL, so that jobs without a key never collide
	if key != "" {
		k = key
	}
	res, err := db.ExecContext(ctx, `INSERT INTO jobs (kind, payload, key, run_at) VALUES (?, ?, ?, ?) ON CONFLICT (key) DO NOTHING`,
		kind, string(body), k, time.Now().Unix())
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errKeyTaken
	}
	return nil
}

// Run works through the due jobs, one at a time, and waits for more, until ctx ends. A run that
// ends on ctx puts the job it was doing back in the queue. Run returns after that job has been
// put back.
func Run(ctx context.Context, db *sql.DB) {
	// This is the only process that works on the table, so a job still marked running was cut
	// off by a crash or a kill.
	if _, err := db.ExecContext(ctx, `UPDATE jobs SET state = 'queued' WHERE state = 'running'`); err != nil {
		slog.Error("jobs: requeue interrupted jobs", "err", err)
	}
	tick := time.NewTicker(PollEvery)
	defer tick.Stop()
	for {
		for runOne(ctx, db) {
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// runOne runs the next due job and reports whether there was one.
func runOne(ctx context.Context, db *sql.DB) bool {
	if ctx.Err() != nil {
		return false
	}
	var id int64
	var kind, payload string
	var attempts int
	err := db.QueryRowContext(ctx, `
		UPDATE jobs SET state = 'running', attempts = attempts + 1
		WHERE id = (SELECT id FROM jobs WHERE state = 'queued' AND run_at <= ? ORDER BY run_at, id LIMIT 1)
		RETURNING id, kind, payload, attempts`, time.Now().Unix()).Scan(&id, &kind, &payload, &attempts)
	if errors.Is(err, sql.ErrNoRows) {
		return false
	}
	if err != nil {
		if ctx.Err() == nil {
			slog.Error("jobs: claim a job", "err", err)
		}
		return false
	}
	runErr := call(ctx, kind, []byte(payload))
	// Writing the result must work during a shutdown, when ctx has ended.
	wctx := context.WithoutCancel(ctx)
	switch {
	case runErr == nil:
		_, err = db.ExecContext(wctx, `UPDATE jobs SET state = 'done', last_error = '' WHERE id = ?`, id)
	case ctx.Err() != nil:
		// The app is stopping. This run does not count as an attempt.
		_, err = db.ExecContext(wctx, `UPDATE jobs SET state = 'queued', attempts = attempts - 1 WHERE id = ?`, id)
	case attempts >= MaxAttempts:
		slog.Error("jobs: giving up", "id", id, "kind", kind, "attempts", attempts, "err", runErr)
		_, err = db.ExecContext(wctx, `UPDATE jobs SET state = 'failed', last_error = ? WHERE id = ?`, runErr.Error(), id)
	default:
		wait := Backoff << (attempts - 1)
		slog.Warn("jobs: will retry", "id", id, "kind", kind, "attempts", attempts, "in", wait, "err", runErr)
		_, err = db.ExecContext(wctx, `UPDATE jobs SET state = 'queued', run_at = ?, last_error = ? WHERE id = ?`,
			time.Now().Add(wait).Unix(), runErr.Error(), id)
	}
	if err != nil {
		slog.Error("jobs: record the result", "id", id, "err", err)
	}
	return true
}

// call runs the handler of kind. A panic in it becomes an error: a panic in a goroutine of your
// own would otherwise stop the whole program.
func call(ctx context.Context, kind string, payload []byte) (err error) {
	h := handlers[kind]
	if h == nil {
		return fmt.Errorf("jobs: no handler for %q", kind)
	}
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("jobs: %s panicked: %v", kind, r)
		}
	}()
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	return h(ctx, payload)
}

// Purge deletes the jobs that finished before the given time. Failed jobs stay, for you to look
// at. Keep the age longer than the span of any key you use with EnqueueOnce.
func Purge(ctx context.Context, db *sql.DB, before time.Time) error {
	_, err := db.ExecContext(ctx, `DELETE FROM jobs WHERE state = 'done' AND run_at < ?`, before.Unix())
	return err
}

// Every calls fn each time d has passed, until ctx ends. A panic in fn is logged, and the next
// round still runs. A slow fn delays the next round, it never runs twice at once.
func Every(ctx context.Context, d time.Duration, fn func(context.Context) error) {
	tick := time.NewTicker(d)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			func() {
				defer func() {
					if r := recover(); r != nil {
						slog.Error("jobs: scheduled task panicked", "panic", r)
					}
				}()
				if err := fn(ctx); err != nil && ctx.Err() == nil {
					slog.Error("jobs: scheduled task failed", "err", err)
				}
			}()
		}
	}
}
```

The choices in it, and why:

- **One job at a time, polled every second.** SQLite lets one writer in at a time, and a small VPS has no spare cores for a pool of workers. A poll that finds nothing is one cheap statement. Raise concurrency only when a measured queue backs up.
- **The claim is one statement.** `UPDATE ... RETURNING` picks the next due job and marks it running in a single step, so a job is never taken twice.
- **At least once.** If the program is killed after a handler did its work and before the row says `done`, the job runs again at the next start. Write handlers that are safe to repeat: check first, or use an id the receiving side can recognize.
- **Retries wait longer each time.** 30 seconds, 1 minute, 2 minutes, and so on, up to `MaxAttempts`. Then the row says `failed` with the last error, and stays for you to read: `SELECT id, kind, last_error FROM jobs WHERE state = 'failed'`.
- **A stop is not a failure.** A job cut off by a shutdown goes back in the queue without using up an attempt. A job still `running` after a crash is queued again when `Run` starts.
- **A handler gets a time limit** (`Timeout`) and its panic becomes an error, so one bad job does not stop the worker or the program.

## Queue a job

Any code can queue a job with the database or with a transaction. With a transaction, the job exists only if the change is saved ([a transaction](database.md#several-statements-at-once)):

```go
// from a POST handler, in the same transaction as the insert
if err := jobs.Enqueue(ctx, tx, "mail", notes.Mail{To: "me@example.com", Subject: "New todo", Text: "..."}); err != nil {
	return err
}
```

A handler is a function of the app that takes the payload as JSON:

```go
func digestJob(ctx context.Context, _ []byte) error {
	var open int
	if err := DB.QueryRowContext(ctx, `SELECT count(*) FROM todos WHERE NOT done`).Scan(&open); err != nil {
		return err
	}
	slog.InfoContext(ctx, "daily digest", "open_todos", open)
	return nil
}
```

[Sending email](email.md) writes the `mail` handler.

## Scheduled work

`jobs.Every(ctx, d, fn)` calls `fn` each time `d` has passed. That is enough for work that can be skipped, like the hourly clean-up in `worker.go`. A timer starts again from zero when the program restarts, so a deploy at 07:59 can make a "daily at 08:00" task wait a whole day.

For work that must run once per day even across restarts, let the timer ask a question that the database answers. `queueDigest` runs every minute and queues today's digest once it is past 08:00 UTC. The job has a key made of the date, and a second queue attempt with the same key does nothing:

```go
func queueDigest(ctx context.Context) error {
	now := time.Now().UTC()
	if now.Hour() < 8 {
		return nil
	}
	_, err := jobs.EnqueueOnce(ctx, DB, "digest-"+now.Format("2006-01-02"), "digest", struct{}{})
	return err
}
```

Restart at any minute of the day and the program still queues today's digest if it was not queued, and never twice. Because the key lives as long as the row, `Purge` is called with an age of a week, longer than any key repeats. The hour is in UTC here. Use `time.LoadLocation` if the task follows a local clock.

## What not to do in a request

A `Load` or a method handler holds a visitor waiting. Keep these out of it:

- **Slow calls to other services.** A mail server can take seconds. The server gives a render `ssr.timeout` (3 seconds) and an answer about 10 seconds to be written. Queue a job and answer at once.
- **`go func() { ... }()` with `c.Context()`.** That context ends when the request ends, so the goroutine is canceled as soon as the answer is sent. Use `context.WithoutCancel(c.Context())` only for work you can afford to lose, and prefer a job.
- **A bare goroutine for work that matters.** It dies with the program on every deploy, and a panic in it ends the program.
- **Writes in `Load`.** `Load` may run more than once for a visit. Queue from `POST`, `PUT`, `PATCH` and `DELETE` handlers.
- **Waiting for the job.** If the visitor needs the result, show a "working" state and let the page ask again, or use a page that polls. Do not block the request until the job is done.

## Test it

The queue is tested with a handler that fails on purpose, on a temporary database. `PollEvery` and `Backoff` are variables, so a test makes them tiny:

```go
func TestRunDoesAJobAndRetriesAFailure(t *testing.T) {
	db := open(t) // a temporary database with the jobs table; Backoff 0, PollEvery 10ms
	var calls atomic.Int32
	Handle("flaky", func(ctx context.Context, p []byte) error {
		if calls.Add(1) < 3 {
			return errors.New("not yet")
		}
		return nil
	})
	if err := Enqueue(context.Background(), db, "flaky", map[string]int{"n": 1}); err != nil {
		t.Fatal(err)
	}
	defer start(t, db)() // runs Run in a goroutine, returns a function that stops it and waits
	if attempts, _ := state(t, db, 1, "done"); attempts != 3 {
		t.Fatalf("attempts: %d, want 3", attempts)
	}
}
```

The same file tests a job that panics, `EnqueueOnce` with a taken key, a job left `running` by a crash, a stop in the middle of a job (it goes back to `queued` with no attempt used), and `Every` after a panic. The hooks are tested together: `OnStart` with a temporary `DBPath`, then `OnShutdown`, which must return without error and leave the database closed. See [Testing](../app.md#testing) for tests of route code.

## Check it on the real program

`tifa build`, start the program, queue a job, then stop it with SIGTERM while a job is waiting for its retry. The program exits at once, and the row is still `queued` with its attempt count. Start it again and the job runs when its time comes.
