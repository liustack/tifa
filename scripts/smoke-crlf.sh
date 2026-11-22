#!/usr/bin/env bash
# Checks that a new app builds the same from a clone made with core.autocrlf=true, which git uses
# on Windows by default and which would check text files out with CRLF line ends. It makes a new
# app with tifa new, commits it, and builds it twice at the same path: as tifa new wrote it, and as
# a clone of that commit. Each time it runs tifa check and tifa build and asks the program for a
# page. It fails when core.autocrlf is not true, when the clone has CRLF line ends (the
# .gitattributes of tifa new keeps LF), when a step fails, or when the two builds differ: the files
# of .tifa/, the program, or the page.
#
#   git config --global core.autocrlf true   # what Windows does, CI sets it
#   scripts/smoke-crlf.sh
#
# Needs go, node, pnpm and git on PATH. It runs on Windows in Git Bash, and on Linux and macOS.
set -euo pipefail

repo=$(pwd -W 2>/dev/null || pwd)
work=$(mktemp -d)
if command -v cygpath >/dev/null; then work=$(cygpath -d "$work"); fi
mkdir -p "$work/bin"
exe=$(go env GOEXE)
port=19531
pid=

stop() {
	[ -n "$pid" ] || return 0
	kill "$pid" 2>/dev/null || true
	case "${OSTYPE:-}" in msys* | cygwin*) taskkill //F //PID "$pid" >/dev/null 2>&1 || true ;; esac
	wait "$pid" 2>/dev/null || true
	pid=
}
trap 'stop; rm -rf "$work" 2>/dev/null || true' EXIT

# build checks and builds the app in $work/app, starts it, and saves .tifa/, the program and the
# page under $work/$1.
build() {
	local out="$work/$1"
	mkdir -p "$out"
	(
		cd "$work/app"
		pnpm install --frozen-lockfile >/dev/null
		go tool tifa check >"$out/check.log" 2>&1 || { cat "$out/check.log" >&2; exit 1; }
		go tool tifa build -o "$out/program$exe" >"$out/build.log" 2>&1 || { cat "$out/build.log" >&2; exit 1; }
		cp -R .tifa "$out/tifa"
	)
	"$out/program$exe" --server-addr "127.0.0.1:$port" >"$out/program.log" 2>&1 &
	pid=$!
	local code=000
	for _ in $(seq 1 60); do
		code=$(curl -s -o "$out/page.html" -w '%{http_code}' "http://127.0.0.1:$port/" || true)
		[ "$code" = 200 ] && break
		sleep 1
	done
	stop
	if [ "$code" != 200 ]; then
		echo "$1: GET / answered $code, the program says:" >&2
		cat "$out/program.log" >&2
		exit 1
	fi
	echo "$1: tifa check, tifa build and GET / passed"
}

echo "== tifa"
go build -o "$work/bin/tifa$exe" ./cmd/tifa
export PATH="$(cygpath -u "$work" 2>/dev/null || echo "$work")/bin:$PATH"
autocrlf=$(git config core.autocrlf || true)
[ "$autocrlf" = true ] || { echo "core.autocrlf is \"$autocrlf\": set it to true first, as git on Windows has it" >&2; exit 1; }

cd "$work"
tifa new crlfapp >/dev/null
mv crlfapp app
(
	cd app
	go mod edit -replace "github.com/liustack/tifa=$repo"
	go mod tidy
	pnpm install >/dev/null
	git init -q
	git add -A
	git -c user.name=smoke -c user.email=smoke@example.com commit -qm "new app"
)
build lf

mv app lf-app
git clone -q lf-app app
# git ls-files --eol shows the line ends of each file in the index (i/) and in the work tree (w/).
crlf=$(cd app && git ls-files --eol | grep -E '(^| )w/(crlf|mixed)' || true)
[ -z "$crlf" ] || { echo "the clone has CRLF line ends in:" >&2; echo "$crlf" >&2; exit 1; }
echo "the clone has LF line ends"
build crlf

same=1
diff -r -x dev.lock -x dev.log "$work/lf/tifa" "$work/crlf/tifa" >"$work/tifa.diff" || { same=0; echo "== .tifa/ differs:"; head -40 "$work/tifa.diff"; }
cmp -s "$work/lf/program$exe" "$work/crlf/program$exe" || { same=0; echo "== the programs differ"; }
cmp -s "$work/lf/page.html" "$work/crlf/page.html" || { same=0; echo "== GET / differs:"; diff "$work/lf/page.html" "$work/crlf/page.html" | head -20; }
[ "$same" = 1 ] || exit 1
echo "ok: the clone builds the same .tifa/, program and page"
