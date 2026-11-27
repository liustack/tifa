---
summary: '用 hooks.go 里的 OnCommand 给应用的程序加命令，比如填入示例数据、把用户设为管理员，并在本机、在测试里用 tifatest.Command、在服务器上用 tifa deploy run 运行它们'
audience: app
order: 19
translatedFrom: 0f1097c97642
read_when:
  - '给应用的程序加一个命令，比如往数据库里填示例数据，或者把一个用户设为管理员'
  - '在服务器上手动或由 agent 改一次数据'
  - '在部署启动新发布之前用它运行一个命令，比如用 deploy.release_command 运行 migrate'
  - '用 tifatest.Command 测试一个命令'
  - '应用在服务时有命令写同一个 SQLite 数据库，或者应用用 db.backup 备份数据库'
---

# 应用的命令 {#commands-of-the-app}

有些工作只在你要求时跑一次：给新数据库填示例数据，把第一个用户设为管理员，修一行被 bug 写坏的数据。在 Tifa 应用里，这类工作是应用自己程序的一个命令，和 `serve`、`config`、`version` 并列。你用 Go 和 [Cobra](https://github.com/spf13/cobra)（Tifa 本来就在用的命令行库）来写，程序运行它时用的设置和服务一样：同一个 `tifa.yaml`、`.env`、环境变量和命令行参数，所以也是同一个数据库文件。

这里的代码来自用 `tifa new` 创建的一个应用（模块是 `example.com/notes`），它的数据存放方式和[在 SQLite 里保存数据](database.zh-CN.md)一样，并且有[注册](sign-up.zh-CN.md#two-tables)里的 `users` 表。

## 加一个命令 {#add-a-command}

`hooks.go` 里的 `OnCommand` 拿到程序的根命令，把应用的命令加上去。`tifa new` 建的应用已经有一个命令 `migrate`，它和 `OpenDB` 一样运行迁移（见[启动时还是切换前](database.zh-CN.md#at-start-or-before-the-switch)）。把你的命令加在它旁边：

```go
// hooks.go
import "github.com/spf13/cobra"

// OnCommand adds the commands of the app to its program, next to serve, config and version.
func OnCommand(root *cobra.Command) {
	root.AddCommand(migrateCmd(), seedCmd(), promoteCmd())
}
```

每个命令就是一个普通的 `*cobra.Command`。`seed` 添加示例待办，数量由 `--count` 决定：

```go
// seed.go
package notes

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/spf13/cobra"
)

func seedCmd() *cobra.Command {
	count := 0
	cmd := &cobra.Command{
		Use:   "seed",
		Short: "Add sample todos to the database",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			db, err := OpenDB(DBPath)
			if err != nil {
				return err
			}
			defer db.Close()
			if err := Seed(cmd.Context(), db, count); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "added %d todos\n", count)
			return nil
		},
	}
	cmd.Flags().IntVar(&count, "count", 20, "how many todos to add")
	return cmd
}

// Seed adds count sample todos in one transaction.
func Seed(ctx context.Context, db *sql.DB, count int) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for i := range count {
		if _, err := tx.ExecContext(ctx, `INSERT INTO todos (title) VALUES (?)`, fmt.Sprintf("Sample todo %d", i+1)); err != nil {
			return err
		}
	}
	return tx.Commit()
}
```

`promote` 把一个用户设为管理员。它先要在下一个迁移里加一列：

```sql
-- migrations/003_admins.sql
ALTER TABLE users ADD COLUMN admin INTEGER NOT NULL DEFAULT 0;
```

```go
// admin.go
package notes

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func promoteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "promote <email>",
		Short: "Make a user an admin",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := OpenDB(DBPath)
			if err != nil {
				return err
			}
			defer db.Close()
			email := strings.ToLower(args[0])
			res, err := db.ExecContext(cmd.Context(), `UPDATE users SET admin = 1 WHERE email = ?`, email)
			if err != nil {
				return err
			}
			if n, err := res.RowsAffected(); err != nil || n == 0 {
				return fmt.Errorf("no user has the address %s", email)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s is an admin\n", email)
			return nil
		},
	}
}
```

子命令、别名、必填参数和 shell 补全都是 Cobra 的，按它的文档用就行：一个带 `user promote` 和 `user list` 的 `user` 组，就是 `root.AddCommand(user)` 加上 `user.AddCommand(...)`。

### 命令能拿到什么 {#what-a-command-gets}

- **服务的设置。** 命令运行前，程序会像 `serve` 一样读取 `tifa.yaml`、`-c` 给的文件、运行目录里的 `.env`、环境变量和命令行参数（见[设置从哪里来](../build-and-deploy/config.zh-CN.md#where-settings-come-from)）。`options.go` 里的选项都有值：上面的 `DBPath` 就是 `db.path`，`./bin/notes seed --db-path /tmp/try.db` 会往另一个文件里填数据。设置无效时，程序在命令运行前就停下。
- **所有设置参数。** 每个命令都接受它们，所以命令自己的参数不能和其中任何一个同名，比如 `--db-path`、`--config` 或 `-c`。同名时程序在启动时就停下，并指出是哪个参数。
- **没有 `OnStart`，也没有 `OnShutdown`。** 命令不启动服务器，不启动渲染 VM，也不跑服务需要的任何准备工作：没有后台循环，没有 session 存储，没有备份。它自己打开要用的东西，用完自己关，就像上面的 `OpenDB(DBPath)` 和 `defer db.Close()`。`OpenDB` 会跑数据库还没跑过的迁移，和在 `OnStart` 里一样。
- **按 Ctrl-C 就结束的 context。** 程序收到 SIGINT 或 SIGTERM 时，`cmd.Context()` 就结束了。把它传给每个可能等待的调用，像上面的查询那样，这样 Ctrl-C 能让命令停下。对停不下来的命令，再按一次 Ctrl-C 会立刻结束程序。
- **错误只打一行。** 命令返回的错误只打印一次，不带用法说明，程序以 1 退出。参数写错或缺参数也是这样打印。`--help` 会打印用法。
- **自己的名字。** `serve`、`config`、`version`、`help` 和 `completion` 属于程序本身，作为名字和别名都是。应用的命令占用其中一个，或者两个命令同名，程序都会在启动时停下并说明原因。

输出用 `cmd.OutOrStdout()` 和 `cmd.ErrOrStderr()`，不要用 `fmt.Println`：在程序里它们就是终端，测试也能读到它们。每个命令都在 `OnCommand` 调用的函数里新建，像 `seedCmd()` 那样，不要放在包级变量里：测试每构造一次命令行，`OnCommand` 就再跑一次，只建一次的命令会留着上一次运行的参数值。同一个命令加了两次时，会报错说明这一点。

## 在本机运行 {#run-it-on-your-machine}

`tifa dev` 跑的是 `serve`。先构建程序，再用它运行命令：

```sh
go tool tifa build
./bin/notes seed --count 50
./bin/notes promote ann@example.com
./bin/notes --help        # the commands of the app next to serve, config and version
./bin/notes seed --help   # the flags of seed, and the flags of the settings
```

程序从它运行的目录读取 `.env`，所以要在应用根目录运行，才会用到 `tifa dev` 用的那个数据库。

## 测试 {#test-it}

`tifatest.Command` 在测试里按程序的方式运行一个命令：`OnCommand`、参数、先加载设置，并把测试的 context 作为 `cmd.Context()`。它返回命令写到 `cmd.OutOrStdout()` 和 `cmd.ErrOrStderr()` 的内容，以及命令返回的错误。它和 `tifatest.Start` 一样接收 `tifaapp.App`（见[测试整个请求](../pages-and-data/testing-apps.zh-CN.md#testing-a-whole-request)），所以测试要放在一个单独的包里：

```go
// admin_test.go
package notes_test

import (
	"path/filepath"
	"testing"

	"example.com/notes"
	"example.com/notes/.tifa/tifaapp"
	"github.com/liustack/tifa/tifatest"
)

func TestPromote(t *testing.T) {
	notes.DBPath = filepath.Join(t.TempDir(), "test.db")
	db, err := notes.OpenDB(notes.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO users (email, name, hash) VALUES ('ann@example.com', 'Ann', 'x')`); err != nil {
		t.Fatal(err)
	}

	out, err := tifatest.Command(t, tifaapp.App, "promote", "Ann@example.com")
	if err != nil || out != "ann@example.com is an admin\n" {
		t.Fatalf("promote: %v, %q", err, out)
	}
	var admin bool
	db.QueryRow(`SELECT admin FROM users WHERE email = 'ann@example.com'`).Scan(&admin)
	if !admin {
		t.Error("Ann is no admin")
	}

	if _, err := tifatest.Command(t, tifaapp.App, "promote", "bo@example.com"); err == nil {
		t.Error("promote of an unknown address passed")
	}
}
```

- 和程序里一样，`OnStart` 不会运行，所以测试要自己准备好命令要读的东西，这里是 `DBPath` 指向的文件。
- 和 `Start` 一样，应用的选项在测试程序里保持默认值：在测试里给 `options.go` 里的变量赋值。用选项的参数（比如 `--db-path`）会让测试失败，并说明原因。Tifa 的设置来自 `tifa.yaml` 和传入的参数，不读环境变量，也不读 `.env`。
- 参数指向的不是应用的命令（比如 `serve`）时，测试会失败。`.tifa/tifaapp` 比代码旧时也会失败：再跑一次 `go tool tifa check`。
- 想测工作本身而不是命令行，就直接调用背后的函数，比如 `notes.Seed(ctx, db, 3)`。

## 在服务器上运行 {#run-it-on-the-server}

```sh
go tool tifa deploy run promote ann@example.com
go tool tifa deploy run seed --count 50
go tool tifa deploy run config            # the settings the service sees, and where each comes from
go tool tifa deploy run --host vps2 seed  # flags of deploy run go before the command
```

`tifa deploy run` 以用户 `tifa` 的身份，在服务所在的目录 `/srv/tifa/<app>/shared` 里，运行服务器上当前发布的程序。所以命令读的 `.env`、设置和数据库文件都和服务一样。它找服务器的方式和 `tifa deploy` 一样，并检查对方就是 `~/.tifa/config.yaml` 记住的那台机器（见[管理已部署的应用](../build-and-deploy/deploy-operate.zh-CN.md#run-a-command-of-the-app)）。

- 从命令开始的所有内容都交给程序，参数也一样。`deploy run` 自己的 `--dir` 和 `--host` 要写在命令前面。
- 输出、错误输出和退出状态都是命令自己的，脚本或 agent 可以直接读。
- 在终端里运行时，命令在服务器上也会拿到一个终端，所以它可以提问，Ctrl-C 也能传到它。没有终端时（比如 agent 来运行），`deploy run` 的输入就是命令的输入：`go tool tifa deploy run import < todos.csv`。
- 它运行的是当前发布。新发布里新加的命令，要等那个发布部署上去后才能在服务器上运行。
- 想在每次部署时用新发布运行一个命令（在它启动之前，旧发布还在服务时），把它写进 `deploy.release_command`，比如 `release_command: migrate`（见[发布命令](../build-and-deploy/deploy.zh-CN.md#the-release-command)）。它失败时部署会停下。
- 想在每次部署成功后运行一个命令（在本机或在 CI 里），见[部署之后](../build-and-deploy/deploy-operate.zh-CN.md#after-a-deploy)。

## 应用在服务时 {#while-the-app-serves}

服务器上的命令是服务旁边的第二个程序，两者打开同一个 SQLite 文件。

- **写入要排队。** SQLite 一次只让一个写入进来，跨程序也一样。默认情况下一个写入最多等另一个 5 秒，而且 `OpenDB` 让每个事务一开始就拿写锁，所以命令和服务会互相等待，不会报 `database is locked`。每个事务要短：`--count` 很大的 `Seed` 会一直拿着写锁直到提交，这期间要写入的请求都得等。几千行的数据分批写，每批几百行，各用一个事务。见[写入报 `database is locked` 时](database.zh-CN.md#when-a-write-gets-database-is-locked)。
- **备份会重新上传整个数据库。** 用了 [`db.backup`](database.zh-CN.md#back-it-up) 时，服务里的副本只跟踪它自己看到的写入。另一个程序的写入会让它把整个数据库重新上传一次。数据库很大时，要写入的命令最好一次跑完，不要拆成很多次小的。
- **不要第二份备份。** 命令不运行 `OnStart`，所以不会启动自己的副本。不要在命令里调用 `StartBackup`：两个程序往存储桶里同一个路径写，会把副本弄坏。
- **迁移已经跑过了。** 服务在 `OnStart` 里已经跑过当前版本的迁移，或者 `deploy.release_command` 在它启动前已经跑过，所以命令里的 `OpenDB` 没有要跑的迁移。
