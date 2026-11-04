---
summary: '怎么在 tifa 应用里做请求之外的工作：在 OnStart 里启动、随 OnShutdown 干净停止的 goroutine，tifa 收到停止信号时做什么，按定时器跑的任务，用 SQLite 里的 jobs 表保证重启后不丢并能重试的工作，以及请求里不该做什么'
read_when:
  - 发邮件、调用慢服务，或做任何不该让访客等的工作
  - 在 tifa 应用里每分钟、每小时或每天跑一件事
  - 在 tifa 应用里启动 goroutine，并希望部署时它能干净地停下
  - 程序重启时工作不能丢，失败了要重试
---

[English](jobs.md) · 简体中文

# 后台任务

tifa 应用是一个程序，所以后台工作就是这个程序里的 goroutine，不用另外跑队列服务。应用在 `OnStart` 里启动 goroutine，在 `OnShutdown` 里停掉它们，和别的共享资源一样（见[应用的代码结构](../structure.zh-CN.md#2-共享资源和其他依赖)）。

有三种情况，每一种比前一种多要一点东西：

1. **丢了也没关系的循环。** 清缓存、刷新一个数字。一个 goroutine 加一个定时器就够。
2. **不能漏的定时任务。** 每日汇总。定时器去问表，今天的任务做过没有。
3. **重启后不能丢、失败要重试的工作。** 发邮件。数据库里的一张任务表。

代码来自用 `tifa new` 生成的试验应用（模块 `example.com/notes`），数据的存法见[用 SQLite 存数据](database.zh-CN.md)。任务表在同一个数据库文件里，所以任务可以和需要它的改动在同一个事务里保存。

## tifa 停止时做什么

收到 SIGINT 或 SIGTERM（部署、Ctrl-C）后，按这个顺序（[`server.shutdown_timeout`](../config.zh-CN.md#配置项)默认 10 秒）：

1. 关闭 `tifa.ShuttingDown` 的通道（推流看着它立刻结束，见[流式输出](streaming.zh-CN.md#程序停下时)），停止接受新连接，最多等 `shutdown_timeout` 让处理中的请求结束。
2. 如果还有没结束的，取消它们的 context，再等最多一个 `shutdown_timeout`。
3. 调用 `OnShutdown`，它有自己的 `shutdown_timeout`。如果有请求一直没返回，`OnShutdown` 会被跳过，程序带着错误退出。

tifa 不知道你启动的 goroutine。由此得出两点：

- **自己停它们。** 用它们自己的 context 启动，在 `OnShutdown` 里取消。`OnStart` 收到的 context 一直有效，但停止信号一到它就结束，这时最后几个请求还在跑（见 [hooks.go](../app.zh-CN.md#hooksgo)）。goroutine 用自己的 context，在这些请求结束前继续工作，而这些请求可能还会排入任务。
- **接住它们的 panic。** tifa 把请求里的 panic 变成 500 然后继续运行。你自己 goroutine 里的 panic 会让整个程序退出。下面每个循环都做了 recover。

## 各个部分

### 在钩子里启动和停止

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

`sync.WaitGroup.Go` 启动一个 goroutine 并计数。`OnShutdown` 等计数归零，然后才关数据库，所以没有 worker 会碰到已关闭的数据库。

### 表

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

### 包

`jobs/jobs.go` 不知道应用的其他部分，所以任何领域都可以引入它（见 [import 的方向](../structure.zh-CN.md#import-的方向)）。数据库作为参数传进来，它不引入根包。

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

其中的取舍和原因：

- **一次一个任务，每秒轮询一次。** SQLite 同一时刻只让一个写入者进来，小 VPS 也没有多余的核给一池 worker。没有任务时，一次轮询只是一条便宜的语句。只有量出来队列确实堵了，才提高并发。
- **领取任务只用一条语句。** `UPDATE ... RETURNING` 一步选出下一个到期的任务并标成运行中，所以一个任务不会被领两次。
- **至少执行一次。** 如果处理函数做完了事，程序在行标成 `done` 之前被杀，下次启动这个任务会再跑一遍。处理函数要写成重复执行也安全：先检查，或者用对方能认出的 id。
- **每次重试等得更久。** 30 秒、1 分钟、2 分钟，依此类推，直到 `MaxAttempts`。之后这一行是 `failed`，带着最后一个错误留着给你看：`SELECT id, kind, last_error FROM jobs WHERE state = 'failed'`。
- **停机不算失败。** 被关机打断的任务回到队列，不消耗一次尝试。崩溃后仍是 `running` 的任务，会在 `Run` 启动时重新排队。
- **处理函数有时间上限**（`Timeout`），它的 panic 会变成错误，所以一个坏任务不会让 worker 或整个程序停下。

## 排入任务

任何代码都可以用数据库或事务排入任务。用事务时，只有改动保存成功，任务才存在（见[事务](database.zh-CN.md#一次执行几条语句)）：

```go
// from a POST handler, in the same transaction as the insert
if err := jobs.Enqueue(ctx, tx, "mail", notes.Mail{To: "me@example.com", Subject: "New todo", Text: "..."}); err != nil {
	return err
}
```

处理函数是应用里的一个函数，把负载当 JSON 接收：

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

`mail` 的处理函数在[发送邮件](email.zh-CN.md)里写。

## 定时任务

`jobs.Every(ctx, d, fn)` 每过 `d` 就调用一次 `fn`。对漏一次也无所谓的事，这就够了，比如 `worker.go` 里每小时一次的清理。程序重启时定时器从零开始，所以 07:59 部署一次，可能让一个「每天 08:00」的任务多等一整天。

对必须每天跑一次、重启也不能漏的事，让定时器去问数据库一个问题。`queueDigest` 每分钟运行一次，过了 UTC 08:00 就排入今天的汇总。这个任务有个由日期组成的 key，用同一个 key 再排一次什么也不会发生：

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

一天里任何一分钟重启，程序都会在今天的汇总还没排入时排入它，而且不会排两次。key 和行活得一样久，所以 `Purge` 用一周的时间，比任何 key 重复的间隔都长。这里的钟点按 UTC。如果任务跟着本地时钟走，用 `time.LoadLocation`。

## 请求里不要做什么

`Load` 或方法处理函数会让访客等着。别在里面做这些：

- **调用别的慢服务。** 邮件服务器可能要几秒。服务器给一次渲染 `ssr.timeout`（3 秒），给写出一个回答约 10 秒。排入任务，马上回答。
- **用 `c.Context()` 启动 `go func() { ... }()`。** 这个 context 在请求结束时就结束了，所以回答一发出，goroutine 就被取消。只有丢了也无所谓的工作才用 `context.WithoutCancel(c.Context())`，更好的是用任务。
- **用裸 goroutine 做重要的事。** 每次部署它都随程序一起死，里面的 panic 还会让程序退出。
- **在 `Load` 里写。** 一次访问里 `Load` 可能运行不止一次。从 `POST`、`PUT`、`PATCH`、`DELETE` 处理函数里排任务。
- **等任务做完。** 访客要结果的话，显示「处理中」的状态，让页面再来问，或者用一个轮询的页面。不要让请求一直阻塞到任务完成。

## 测试

队列用一个故意失败的处理函数，在临时数据库上测。`PollEvery` 和 `Backoff` 是变量，测试可以把它们改得很小：

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

同一个文件还测了：会 panic 的任务、key 已被占用的 `EnqueueOnce`、崩溃后留下 `running` 的任务、在任务中途停止（回到 `queued`，不消耗尝试），以及 panic 之后的 `Every`。钩子一起测：用临时的 `DBPath` 调 `OnStart`，再调 `OnShutdown`，它必须无错返回，并留下已关闭的数据库。路由代码的测试见[测试](../app.zh-CN.md#测试)。

## 在真实程序上检查

`tifa build`，启动程序，排入一个任务，然后在一个任务正等着重试时用 SIGTERM 停掉它。程序马上退出，那一行仍然是 `queued`，尝试次数还在。再次启动，到了时间任务就会运行。
