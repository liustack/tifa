---
summary: '怎样在 Tifa 应用里做请求之外的工作：根包可以导入的某个包里每分钟运行一次的循环、在 OnStart 里启动并随 OnShutdown 干净停下的 goroutine、Tifa 被告知停止时做什么、定时运行的任务、在 SQLite 里放一张 jobs 表来保存必须撑过重启并且可以重试的工作，以及在请求里不要做什么'
audience: app
order: 6
translatedFrom: 72043643f2cc
read_when:
  - '发邮件、调用慢的服务，或者做任何不该让访客等待的工作'
  - '在 Tifa 应用里每分钟、每小时或每天运行某件事'
  - '在 Tifa 应用里启动 goroutine，并希望它在部署时干净地停下'
  - '程序重启时工作不能丢，失败要重试'
  - '某个任务需要 app/ 下某个领域的代码，go 命令报告导入循环'
---

# 后台任务 {#background-work}

Tifa 应用就是一个程序，所以后台任务就是这个程序里的一个 goroutine。不需要另外运行队列服务。应用在 `OnStart` 里启动自己的 goroutine，在 `OnShutdown` 里停掉它们，和其他任何共享资源一样（[应用的结构](../start/structure.zh-CN.md#2-shared-resources-and-other-dependencies)）。

有三种情况，每一种都比前一种多需要一点东西：

1. **可以丢失的循环。** 清理缓存，刷新一个数字。一个 goroutine 加一个 ticker 就够了。
2. **定时执行、不能被跳过的任务。** 每日摘要。定时器去问一张表，今天的任务是否已经完成。
3. **必须在重启后还在、并且要重试的工作。** 发送邮件。在数据库里放一张任务表。

这里的代码来自用 `tifa new` 创建的一个临时应用（模块是 `example.com/notes`），它的数据存放方式和[用 SQLite 保存数据](database.zh-CN.md)一样。任务表和它在同一个数据库文件里，所以任务可以和需要它的那次改动在同一个事务里保存。

## 简单的情况：一个循环 {#the-simple-case-one-loop}

大多数应用只需要情况 1：每分钟做点什么，比如删除过期的预订。不需要表，也不需要队列。有三点要紧：函数放在一个把数据库作为参数传入、并且不 import 根包的包里，根包在 `OnStart` 里用一个自己的 context 启动循环，`OnShutdown` 在关闭数据库之前先停掉循环并等它结束。

```go
// bookings/purge.go: a package next to the root, not under app/. It imports nothing of the app,
// so the root may import it.
package bookings

import (
	"context"
	"database/sql"
	"time"
)

// Purge deletes the bookings that ended before now.
func Purge(ctx context.Context, db *sql.DB, now time.Time) error {
	_, err := db.ExecContext(ctx, `DELETE FROM bookings WHERE ends_at < ?`, now.Unix())
	return err
}
```

```go
// hooks.go, in the root package
var (
	loops     sync.WaitGroup
	stopLoops context.CancelFunc
)

func OnStart(ctx context.Context) error {
	db, err := OpenDB(DBPath)
	if err != nil {
		return err
	}
	DB = db
	var loopCtx context.Context
	loopCtx, stopLoops = context.WithCancel(context.Background())
	loops.Go(func() {
		every(loopCtx, time.Minute, func(ctx context.Context) error { return bookings.Purge(ctx, DB, time.Now()) })
	})
	return nil
}

func OnShutdown(ctx context.Context) error {
	stopLoops()
	loops.Wait()
	return DB.Close()
}

// every calls fn each time d has passed, until ctx ends. A panic in your own goroutine ends the
// whole program, so it is recovered and logged.
func every(ctx context.Context, d time.Duration, fn func(context.Context) error) {
	for t := time.NewTicker(d); ; {
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
			func() {
				defer func() {
					if r := recover(); r != nil {
						slog.Error("loop panicked", "panic", r)
					}
				}()
				if err := fn(ctx); err != nil && ctx.Err() == nil {
					slog.Error("loop failed", "err", err)
				}
			}()
		}
	}
}
```

测试的是函数，不是定时器：在临时数据库上，用一个过去的预订和一个未来的预订调用 `bookings.Purge(ctx, db, now)`，检查只有过去的那个被删掉。测试 `every` 时，把几毫秒作为 `d` 传进去，然后取消 context。

如果这个函数需要 `app/` 下某个业务域的代码，根包就不能 import 那个业务域（[原因](#a-job-that-needs-code-of-a-domain)）。把函数需要的东西挪到像上面那样的包里，或者用下面的队列，让业务域注册它自己的处理函数。

## Tifa 停止时做什么 {#what-tifa-does-when-it-stops}

收到 SIGINT 或 SIGTERM（部署、Ctrl-C）时，按这个顺序（[`server.shutdown_timeout`](../build-and-deploy/config.zh-CN.md#settings) 默认是 10 秒）：

1. 它关闭 `tifa.ShuttingDown` 的 channel，让监听它的流立即结束（[流式传输](streaming.zh-CN.md#when-the-program-stops)），停止接受连接，并最多等 `shutdown_timeout` 让正在处理的请求完成。
2. 如果还有请求在运行，它会取消它们的 context，再最多等一个 `shutdown_timeout`。
3. 它调用 `OnShutdown`，后者会得到一个带自己的 `shutdown_timeout` 的 context。如果有请求始终没有返回，`OnShutdown` 会被跳过，程序以错误退出。

Tifa 对你启动的 goroutine 一无所知。由此得出两点：

- **自己停掉它们。** 用自己的 context 启动它们，在 `OnShutdown` 里取消它。`OnStart` 收到的 context 仍然有效，但停止信号一来它就结束了，而这时最后几个请求还在运行（见 [hooks.go](../pages-and-data/hooks.zh-CN.md#hooksgo)）。用自己 context 的 goroutine，在这些请求完成的过程中会继续工作，而其中某个请求可能还会往队列里放任务。
- **接住它们的 panic。** Tifa 会把请求里的 panic 变成 500 并继续运行。你自己的 goroutine 里的 panic 会让整个程序退出。下面的每个循环都做了 recover。

## 各个部分 {#the-pieces}

### 在 hook 里启动和停止 {#start-and-stop-in-the-hooks}

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

`sync.WaitGroup.Go` 启动一个 goroutine 并给它计数。`OnShutdown` 等计数降到零，然后关闭数据库，所以没有哪个 worker 会遇到已关闭的数据库。

### 表 {#the-table}

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

### 这个包 {#the-package}

`jobs/jobs.go` 对应用的其余部分一无所知，所以任何业务域都可以 import 它（[import 的方向](../start/structure.zh-CN.md#which-way-imports-go)）。它把数据库作为参数传入，不 import 根包。

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

// Handle sets the handler of a kind of job. Call it in OnStart or in an init, before Run.
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

其中的各个选择，以及原因：

- **一次一个任务，每秒轮询一次。** SQLite 一次只允许一个写入者，小 VPS 也没有多余的核心去跑一个 worker 池。什么也没找到的一次轮询只是一条很便宜的语句。只有当量出来队列确实积压时，才提高并发。
- **认领任务只用一条语句。** `UPDATE ... RETURNING` 在一步里挑出下一个到期的任务并标记为运行中，所以一个任务绝不会被取走两次。
- **至少一次。** 如果程序在处理函数干完活之后、该行记成 `done` 之前被杀掉，这个任务会在下次启动时再运行一次。请把处理函数写成重复执行也安全的：先检查，或者使用接收方能识别的 id。
- **每次重试等得更久。** 30 秒、1 分钟、2 分钟，依此类推，直到 `MaxAttempts`。然后这一行变成 `failed`，带着最后一个错误，留在那里供你查看：`SELECT id, kind, last_error FROM jobs WHERE state = 'failed'`。
- **停止不算失败。** 因关闭而被切断的任务会回到队列里，不消耗一次尝试。崩溃之后仍是 `running` 的任务，会在 `Run` 启动时重新排队。
- **处理函数有时间上限**（`Timeout`），它的 panic 会变成错误，所以一个坏任务不会让 worker 或程序停下。

## 排入一个任务 {#queue-a-job}

任何代码都可以用数据库或事务来排入任务。用事务时，只有改动被保存了，任务才存在（[事务](database.zh-CN.md#several-statements-at-once)）：

```go
// from a POST handler, in the same transaction as the insert
if err := jobs.Enqueue(ctx, tx, "mail", notes.Mail{To: "me@example.com", Subject: "New todo", Text: "..."}); err != nil {
	return err
}
```

处理函数是应用里的一个函数，以 JSON 形式接收 payload：

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

[发送邮件](email.zh-CN.md)里写了 `mail` 处理函数。

### 需要业务域代码的任务 {#a-job-that-needs-code-of-a-domain}

`worker.go` 在根包里。假设有个任务需要业务域 `app/issues` 的代码，而 `app/issues` 为了 `DB` 要 import 根包。根包就不能 import `issues`：go 命令会以 `import cycle not allowed` 停下（[import 的方向](../start/structure.zh-CN.md#which-way-imports-go)）。所以由业务域注册它自己的处理函数。`jobs.Handle` 只是把一个函数放进 map，不读取任何资源，所以它可以在 `init` 里运行，就像 `database/sql` 驱动注册自己的方式：

```go
// app/issues/digest.go
package issues

import (
	"context"
	"log/slog"

	"example.com/notes"
	"example.com/notes/jobs"
)

func init() { jobs.Handle("issue-digest", digest) }

// digest runs after OnStart, so it may use notes.DB.
func digest(ctx context.Context, _ []byte) error {
	var open int
	if err := notes.DB.QueryRowContext(ctx, `SELECT count(*) FROM issues WHERE NOT closed`).Scan(&open); err != nil {
		return err
	}
	slog.InfoContext(ctx, "open issues", "count", open)
	return nil
}
```

`worker.go` 没有列出这种任务类型。`jobs.Run` 按任务的 kind 找到处理函数，任何代码都可以用 `jobs.Enqueue(ctx, notes.DB, "issue-digest", ...)` 排入这个任务。

Tifa 只链接路由 import 的包。如果某个页面 import 了 `issues`，它的 `init` 就会运行。如果只有任务用到它，就没有东西 import 它，处理函数永远不会被注册，任务会以 `no handler for "issue-digest"` 失败，直到被标记为失败。请在某个路由包里（比如 `app/route.go`）为了它的副作用而 import 它：`import _ "example.com/notes/app/issues"`。

另一种办法，是把 `DB` 从根包里拿出来，放进它自己的包，比如 `example.com/notes/db`，业务域 import 它而不是根包。这样 `app/` 下就没有东西 import 根包了，根包可以 import `app/issues`，并在 `startBackground` 里自己调用 `jobs.Handle`。这打破了「共享资源放在根包」的规则，所以只在你不想用 `init` 时才这样做。

## 定时任务 {#scheduled-work}

`jobs.Every(ctx, d, fn)` 每过 `d` 就调用一次 `fn`。对可以被跳过的工作，这就够了，比如 `worker.go` 里每小时一次的清理。程序重启时定时器会从零重新开始，所以在 07:59 部署，会让「每天 08:00」的任务整整等上一天。

对于必须每天运行一次、重启也不例外的工作，让定时器去问一个由数据库来回答的问题。`queueDigest` 每分钟运行一次，一旦过了 08:00 UTC，就排入今天的摘要。这个任务有一个由日期组成的 key，用同一个 key 再次排队什么也不会发生：

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

一天里的任何一分钟重启，程序仍然会在今天的摘要没排过队时排入它，并且绝不会排两次。因为 key 和这一行活得一样久，所以调用 `Purge` 时用一周的时长，比任何 key 重复的间隔都长。这里的小时按 UTC 计算。如果任务要跟随本地时钟，请使用 `time.LoadLocation`。

## 不要在请求里做什么 {#what-not-to-do-in-a-request}

`Load` 或方法处理函数会让访问者等着。下面这些事别放进去：

- **对其他服务的慢调用。** 邮件服务器可能要几秒。服务器给一次渲染 `ssr.timeout`（3 秒），给写出响应大约 10 秒。请排入一个任务，然后立刻响应。
- **带着 `c.Context()` 的 `go func() { ... }()`。** 这个 context 在请求结束时就结束了，所以响应一发出，这个 goroutine 就被取消。只有对丢了也无所谓的工作才用 `context.WithoutCancel(c.Context())`，而且最好还是用任务。
- **用裸 goroutine 做重要的工作。** 每次部署它都会随程序一起死掉，它里面的 panic 会让程序退出。
- **在 `Load` 里写入。** 一次访问中 `Load` 可能运行不止一次。请在 `POST`、`PUT`、`PATCH` 和 `DELETE` 处理函数里排队。
- **等任务做完。** 如果访问者需要结果，就显示一个「处理中」的状态，让页面再去问，或者用一个会轮询的页面。不要让请求一直阻塞到任务做完。

## 测试 {#test-it}

队列用一个故意失败的处理函数，在临时数据库上测试。`PollEvery` 和 `Backoff` 是变量，所以测试里可以把它们设得很小：

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

同一个文件还测试了这些情况：panic 的任务、key 已被占用时的 `EnqueueOnce`、因崩溃而停留在 `running` 的任务、任务进行到一半时停止（它回到 `queued`，不消耗尝试次数），以及 panic 之后的 `Every`。hook 一起测试：用一个临时的 `DBPath` 运行 `OnStart`，然后运行 `OnShutdown`，它必须不出错地返回，并让数据库处于已关闭状态。路由代码的测试见[测试](../pages-and-data/testing-apps.zh-CN.md#testing)。

## 在真实程序上检查 {#check-it-on-the-real-program}

`tifa build`，启动程序，排入一个任务，然后在有任务等着重试的时候用 SIGTERM 停掉它。程序会立刻退出，这一行仍是 `queued`，带着它的尝试次数。再次启动，任务会在到时间时运行。
