#!/usr/bin/env bash
# Builds two apps with tifa on the machine this runs on, starts each built program and asks it for
# a page: a new app made by tifa new (GET /), and examples/shop (GET /store). It fails when a
# build fails or a page does not answer 200. CI runs it on Windows, where it needs Git Bash,
# and it runs the same on Linux and macOS.
#
#   scripts/smoke-build.sh
#
# Needs go, node and pnpm on PATH. The new app lives in a temporary folder of the system, which
# on a Windows runner has a short 8.3 name (C:\Users\RUNNER~1), and the programs are built outside
# the checkout.
set -euo pipefail

repo=$(pwd -W 2>/dev/null || pwd)
work=$(mktemp -d)
mkdir -p "$work/bin"
exe=$(go env GOEXE)
port=19527
pid=

stop() {
	[ -n "$pid" ] || return 0
	kill "$pid" 2>/dev/null || true
	# A program that started from Git Bash may not end by kill on Windows.
	case "${OSTYPE:-}" in msys* | cygwin*) taskkill //F //PID "$pid" >/dev/null 2>&1 || true ;; esac
	wait "$pid" 2>/dev/null || true
	pid=
}
trap 'stop; rm -rf "$work" 2>/dev/null || true' EXIT

# serve starts the program, waits for the path to answer 200 and checks that it sent a page.
serve() {
	local program=$1 path=$2 log=$3
	"$program" --server-addr "127.0.0.1:$port" >"$log" 2>&1 &
	pid=$!
	local code=000
	for _ in $(seq 1 60); do
		code=$(curl -s -o "$work/page.html" -w '%{http_code}' "http://127.0.0.1:$port$path" || true)
		[ "$code" = 200 ] && break
		sleep 1
	done
	if [ "$code" != 200 ]; then
		echo "GET $path answered $code, the program says:" >&2
		cat "$log" >&2
		exit 1
	fi
	grep -qi '<html' "$work/page.html" || { echo "GET $path sent no HTML" >&2; exit 1; }
	echo "GET $path: 200, $(wc -c <"$work/page.html") bytes"
	stop
}

echo "== tifa"
go build -o "$work/bin/tifa$exe" ./cmd/tifa
export PATH="$work/bin:$PATH"

echo "== new app in $(pwd -W 2>/dev/null || pwd)"
tifa new smokeapp
cd smokeapp
go mod edit -replace "github.com/liustack/tifa=$repo"
go mod tidy
pnpm install
go tool tifa build
test -f "bin/smokeapp$exe" || { echo "tifa build did not write bin/smokeapp$exe" >&2; ls bin >&2; exit 1; }
serve "./bin/smokeapp$exe" / "$work/smokeapp.log"

echo "== examples/shop"
cd "$repo/examples/shop"
pnpm install --frozen-lockfile
go tool tifa build
test -f "bin/shop$exe" || { echo "tifa build did not write bin/shop$exe" >&2; ls bin >&2; exit 1; }
serve "./bin/shop$exe" /store "$work/shop.log"
echo "ok"
