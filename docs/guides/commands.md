---
summary: 'Add commands to the program of an app with OnCommand in hooks.go, such as seeding sample data or making a user an admin, and run them on your machine, in tests with tifatest.Command and on the server with tifa deploy run'
audience: app
order: 19
read_when:
  - Adding a command to the program of an app, such as one that fills the database with sample data or makes a user an admin
  - Changing data on the server once, by hand or from an agent
  - Testing a command with tifatest.Command
  - A command writes the SQLite database while the app serves, or the app backs its database up with db.backup
---

# Commands of the app

Some work runs once, when you ask for it: fill a new database with sample data, make the first user an admin, fix a row a bug wrote. In a Tifa app that work is a command of the app's own program, next to `serve`, `config` and `version`. You write it in Go with [Cobra](https://github.com/spf13/cobra), the command line library Tifa already uses, and the program runs it with the same settings as the service: the same `tifa.yaml`, `.env`, environment and flags, and so the same database file.

The code is from an app made with `tifa new` (module `example.com/notes`) that keeps its data as in [Keeping data in SQLite](database.md) and has the `users` table of [Sign up](sign-up.md#two-tables).

## Add a command

`OnCommand` in `hooks.go` gets the root command of the program and adds the commands of the app to it:

```go
// hooks.go
import "github.com/spf13/cobra"

// OnCommand adds the commands of the app to its program, next to serve, config and version.
func OnCommand(root *cobra.Command) {
	root.AddCommand(seedCmd(), promoteCmd())
}
```

Each command is a plain `*cobra.Command`. `seed` adds sample todos, as many as `--count` says:

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

`promote` makes a user an admin. It needs a column first, in the next migration:

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

Subcommands, aliases, required flags and shell completion are Cobra's, and work as its docs say: a group `user` with `user promote` and `user list` is `root.AddCommand(user)` with `user.AddCommand(...)`.

### What a command gets

- **The settings of the service.** The program loads `tifa.yaml`, the file of `-c`, `.env` in the folder it runs in, the environment and the flags before the command runs, as `serve` does (see [Where settings come from](../build-and-deploy/config.md#where-settings-come-from)). The options of `options.go` have their values: `DBPath` above is `db.path`, and `./bin/notes seed --db-path /tmp/try.db` seeds another file. An invalid setting stops the program before the command runs.
- **Every flag of the settings.** Each command takes them, so a flag of a command may not have the name of one, such as `--db-path`, `--config` or `-c`. The program stops at start and names the flag.
- **No `OnStart` and no `OnShutdown`.** A command starts no server and no render VMs, and runs none of the setup of serving: no background loops, no session store, no backup. It opens what it needs itself and closes it when it is done, as `OpenDB(DBPath)` and `defer db.Close()` above. `OpenDB` runs the migrations the database has not run yet, as it does in `OnStart`.
- **A context that ends on Ctrl-C.** `cmd.Context()` is done when the program gets SIGINT or SIGTERM. Pass it to every call that can wait, as the queries above do, so Ctrl-C stops the command. A second Ctrl-C ends the program at once, for a command that does not stop.
- **One line for an error.** A returned error is printed once, without the usage text, and the program exits with 1. A wrong flag or a missing argument is printed the same way. `--help` prints the usage.
- **A name of its own.** `serve`, `config`, `version`, `help` and `completion` belong to the program, as names and as aliases. A command of the app that takes one of them, or two commands with the same name, stop the program at start with the reason.

Write output with `cmd.OutOrStdout()` and `cmd.ErrOrStderr()`, not `fmt.Println`: they are the terminal in the program, and a test reads them. Make each command inside the function `OnCommand` calls, as `seedCmd()` does, not in a package-level variable: `OnCommand` runs again for each command line a test builds, and a command made once would keep the flags of the run before. A command added twice fails with a message that says so.

## Run it on your machine

`tifa dev` runs `serve`. Build the program and run the command with it:

```sh
go tool tifa build
./bin/notes seed --count 50
./bin/notes promote ann@example.com
./bin/notes --help        # the commands of the app next to serve, config and version
./bin/notes seed --help   # the flags of seed, and the flags of the settings
```

The program reads `.env` from the folder it runs in, so run it from the root of the app to use the database `tifa dev` uses.

## Test it

`tifatest.Command` runs a command in the test the way the program does: `OnCommand`, the flags, the settings loaded first, and the context of the test as `cmd.Context()`. It returns what the command wrote to `cmd.OutOrStdout()` and `cmd.ErrOrStderr()`, and the error the command returned. Like `tifatest.Start`, it takes `tifaapp.App` (see [Testing a whole request](../pages-and-data/testing-apps.md#testing-a-whole-request)), so the test goes in a package of its own:

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

- `OnStart` does not run, as in the program, so the test sets up what the command reads, here the file at `DBPath`.
- The options of the app keep their defaults in a test binary, as for `Start`: set the variables of `options.go` in the test. A flag of an option, such as `--db-path`, fails the test and says so. The settings of Tifa come from `tifa.yaml` and the arguments, not from the environment or `.env`.
- The test fails when the arguments name no command of the app, such as `serve`, and when `.tifa/tifaapp` is older than the code: run `go tool tifa check` again.
- To test the work and not the command line, call the function behind it, as `notes.Seed(ctx, db, 3)`.

## Run it on the server

```sh
go tool tifa deploy run promote ann@example.com
go tool tifa deploy run seed --count 50
go tool tifa deploy run config            # the settings the service sees, and where each comes from
go tool tifa deploy run --host vps2 seed  # flags of deploy run go before the command
```

`tifa deploy run` runs the program of the release that is live on the server, as the user `tifa`, in `/srv/tifa/<app>/shared`, the folder of the service. So the command reads the same `.env`, the same settings and the same database file as the service. It finds the server the way `tifa deploy` does, and checks that it is the machine `~/.tifa/config.yaml` remembers (see [Run a deployed app](../build-and-deploy/deploy-operate.md#run-a-command-of-the-app)).

- Everything from the command on goes to the program, flags included. `--dir` and `--host` of `deploy run` go before it.
- The output, the error output and the exit status are those of the command, so a script or an agent can read them.
- On a terminal, the command gets a terminal on the server too, so it can ask a question and Ctrl-C reaches it. Without one, as an agent runs it, the input of `deploy run` is the input of the command: `go tool tifa deploy run import < todos.csv`.
- It runs the release that is live. A command added in a new release runs on the server once that release is deployed.
- To run a command after each good deploy, on your machine or in CI, see [After a deploy](../build-and-deploy/deploy-operate.md#after-a-deploy).

## While the app serves

A command on the server is a second program next to the service, and both open the same SQLite file.

- **Writes take turns.** SQLite lets one write in at a time, also across programs. A write waits up to 5 seconds for another by default, and `OpenDB` makes each transaction take the write lock as it starts, so the command and the service wait for each other instead of failing with `database is locked`. Keep each transaction short: `Seed` with a large `--count` holds the write lock until it commits, and requests that write wait meanwhile. Write thousands of rows in batches of a few hundred, one transaction each. See [When a write gets `database is locked`](database.md#when-a-write-gets-database-is-locked).
- **The backup sends the whole database again.** With [`db.backup`](database.md#back-it-up), the copy in the service follows only the writes it sees. A write from another program makes it upload the whole database again, once. For a large database, run commands that write in one go rather than many small ones.
- **No second backup.** A command does not run `OnStart`, so it does not start a copy of its own. Do not call `StartBackup` in a command: two programs that write to the same path in the bucket break the copy.
- **Migrations ran already.** The service ran the migrations of the live release in its `OnStart`, so `OpenDB` in the command finds nothing to run.
