#!/usr/bin/env bash
# Runs tifa dev on a new app as a user does, and checks what the user sees: GET / answers 200, a
# saved edit of a page and of a Go file shows up, and once tifa dev stops no process of the app is
# left and nothing listens on its address. Three runs: in the foreground stopped by
# `tifa dev stop`, in the foreground with the process of tifa dev killed (taskkill /F on Windows,
# as the Task Manager does, kill -9 elsewhere), and with --background, `dev status`, `dev logs`
# and `dev stop`. CI runs it on Windows, where it needs Git Bash, and it runs the same on Linux and
# macOS.
#
#   scripts/smoke-dev.sh
#
# Needs go, node and pnpm on PATH. The app lives in a temporary folder of the system.
set -euo pipefail

repo=$(pwd -W 2>/dev/null || pwd)
work=$(mktemp -d)
exe=$(go env GOEXE)
port=19529
url="http://127.0.0.1:$port"
# The name of the app marks every process of it: each has the folder of the app on its command line.
# It is short, so it has no 8.3 short name on Windows that a command line could hold instead.
name="dv$RANDOM"
windows=
case "${OSTYPE:-}" in msys* | cygwin*) windows=1 ;; esac
fg=
root=

phase() {
	echo "== $1 (at ${SECONDS}s)"
}

fail() {
	echo "FAIL: $*" >&2
	for log in "$work"/*.log "$work/$name/.tifa/dev.log"; do
		[ -f "$log" ] || continue
		echo "--- $log" >&2
		tail -40 "$log" >&2
	done
	exit 1
}

cleanup() {
	if [ -d "$work/$name" ]; then
		(cd "$work/$name" && go tool tifa dev stop --dir "$root" >/dev/null 2>&1) || true
	fi
	if [ -n "$fg" ]; then
		kill "$fg" 2>/dev/null || true
		wait "$fg" 2>/dev/null || true
	fi
	rm -rf "$work" 2>/dev/null || true
}
trap cleanup EXIT

# leftovers prints the processes whose command line names the app, but for the one asking.
leftovers() {
	if [ -n "$windows" ]; then
		powershell -NoProfile -Command "Get-CimInstance Win32_Process | Where-Object { \$_.ProcessId -ne \$PID -and \$_.CommandLine -like '*$name*' } | ForEach-Object { '{0} {1}' -f \$_.ProcessId, \$_.CommandLine }"
	else
		pgrep -fl -- "$name" || true
	fi
}

# running fails unless the app and the driver of Vite show among the processes of the app, so that
# nothing_left looks where they are.
running() {
	local procs
	procs=$(leftovers)
	echo "$procs" | grep -q "tifa-dev" || fail "the app is not among the processes of $name:
$procs"
	echo "$procs" | grep -q "driver.mjs" || fail "the driver of Vite is not among the processes of $name:
$procs"
}

# nothing_left fails when a process of the app still runs or something answers at the address.
# On Unix a killed tifa dev leaves the app to stop by its lifeline and Vite by its stdin, which
# takes up to a few seconds, so it waits for them.
nothing_left() {
	local left=
	for _ in $(seq 1 30); do
		left=$(leftovers)
		[ -z "$left" ] && break
		sleep 0.5
	done
	[ -z "$left" ] || fail "$1: processes of the app still run:
$left"
	if curl -s -o /dev/null --max-time 2 "$url/"; then
		fail "$1: something still answers at $url"
	fi
	echo "$1: no process of the app left, $url is free"
}

# page waits until GET / answers 200 with want in the page, for at most the given seconds.
page() {
	local want=$1 wait=$2 code=000
	for _ in $(seq 1 "$wait"); do
		code=$(curl -s -o "$work/page.html" -w '%{http_code}' --max-time 10 "$url/" || true)
		if [ "$code" = 200 ] && grep -q "$want" "$work/page.html"; then
			echo "GET /: 200 with \"$want\""
			return
		fi
		sleep 1
	done
	fail "GET / answered $code without \"$want\" within ${wait}s"
}

# devpid prints the process ID of the tifa dev of the app, from its status.
devpid() {
	go tool tifa dev status --json --dir "$root" | sed -n 's/.*"pid": *\([0-9]*\).*/\1/p'
}

phase "tifa"
go build -o "$work/bin/tifa$exe" ./cmd/tifa
export PATH="$(cygpath -u "$work" 2>/dev/null || echo "$work")/bin:$PATH"

phase "new app $name"
cd "$work"
tifa new "$name" >/dev/null
cd "$name"
root=$(pwd -W 2>/dev/null || pwd)
go mod edit -replace "github.com/liustack/tifa=$repo"
go mod tidy
pnpm install >/dev/null

phase "foreground, stopped by tifa dev stop"
go tool tifa dev --dir "$root" --addr "127.0.0.1:$port" >"$work/foreground.log" 2>&1 &
fg=$!
page "<h1>Todos</h1>" 300
phase "edit the page"
perl -pi -e 's#<h1>Todos</h1>#<h1>Todos A</h1>#' app/page.tsx
page "<h1>Todos A</h1>" 60
# A change of Go builds the program again while the app runs from it, and restarts the app.
phase "edit Go"
perl -pi -e 's#return todos.ListTodos\(c.Context\(\)\)#list, err := todos.ListTodos(c.Context())\n\treturn append(list, todos.Todo{ID: 999, Title: "Added in Go"}), err#' app/route.go
page "Added in Go" 120
running
phase "tifa dev stop"
go tool tifa dev stop --dir "$root"
wait "$fg" || fail "tifa dev exited with status $? after tifa dev stop"
fg=
nothing_left "after tifa dev stop"

phase "foreground, killed"
go tool tifa dev --dir "$root" --addr "127.0.0.1:$port" >"$work/killed.log" 2>&1 &
fg=$!
page "Added in Go" 300
running
pid=$(devpid)
[ -n "$pid" ] || fail "tifa dev status names no process"
phase "kill tifa dev (pid $pid)"
if [ -n "$windows" ]; then
	taskkill //F //PID "$pid" >/dev/null
else
	kill -9 "$pid"
fi
wait "$fg" || true
fg=
nothing_left "after the kill"

phase "--background"
go tool tifa dev --background --dir "$root" --addr "127.0.0.1:$port" >"$work/background.log" 2>&1 || fail "tifa dev --background failed"
cat "$work/background.log"
grep -q "ready at $url" "$work/background.log" || fail "tifa dev --background did not say it was ready at $url"
status=$(go tool tifa dev status --dir "$root") || fail "tifa dev status: not running"
echo "$status"
echo "$status" | grep -q "build:   ok" || fail "tifa dev status does not say the build is ok"
page "Added in Go" 60
# Saved with CRLF line ends, as an editor on Windows may: the page and a Go file.
phase "edit the page and Go with CRLF line ends"
perl -pi -e 's#<h1>Todos A</h1>#<h1>Todos C</h1>#; s#\r?\n#\r\n#' app/page.tsx
perl -pi -e 's#Added in Go#Added in Go with CRLF#; s#\r?\n#\r\n#' app/route.go
page "<h1>Todos C</h1>" 60
page "Added in Go with CRLF" 120
running
go tool tifa dev logs --dir "$root" -n 200 | grep -q "ready in" || fail "tifa dev logs has no ready line"
phase "tifa dev stop"
go tool tifa dev stop --dir "$root"
if go tool tifa dev status --dir "$root" >/dev/null; then
	fail "tifa dev status says it runs after tifa dev stop"
fi
nothing_left "after tifa dev stop of --background"
phase "ok"
