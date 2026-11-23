#!/usr/bin/env bash
# count-instructions.sh: the instructions one render of each page of the example shop takes,
# counted by callgrind. Linux on amd64 only, with valgrind installed. See "Counting instructions"
# in docs/internals/testing.md.
#
#   count-instructions.sh run OUT.tsv [REPO]
#       count the shop built in REPO/examples/shop (default: the repository of this script)
#       and write one line a page to OUT.tsv: the page, the instructions of one render.
#       Build the shop first: cd examples/shop && go tool tifa build
#   count-instructions.sh table BASE.tsv REF.tsv
#       the Markdown table of two such files: each page, both counts, the change in
#       percent, and the sum
#
# Environment:
#   COUNT_APP    shop (default), or mui for the MUI test app built in REPO/test/apps/mui
#   COUNT_WARM   renders of every page before counting (default 2)
#   COUNT_TIMED  renders of a page that are counted together (default 3)
set -euo pipefail

die() {
	echo "count-instructions.sh: $*" >&2
	exit 2
}

run() {
	[ $# -ge 1 ] && [ $# -le 2 ] || die "usage: run OUT.tsv [REPO]"
	command -v valgrind >/dev/null || die "valgrind is not installed. Counting needs valgrind on Linux (apt-get install valgrind). It does not run on macOS."
	[ "$(uname -s)-$(uname -m)" = Linux-x86_64 ] || die "counting runs on linux/amd64 only, this is $(uname -s)/$(uname -m)"
	local out repo
	out=$(cd "$(dirname "$1")" && pwd)/$(basename "$1")
	repo=$(cd "${2:-$(dirname "$0")/..}" && pwd)
	local app=${COUNT_APP:-shop} dir exe
	case $app in
	shop) dir=examples/shop exe=shop ;;
	mui) dir=test/apps/mui exe=muitest ;;
	*) die "COUNT_APP=$app: no such app, it is shop or mui" ;;
	esac
	[ -f "$repo/$dir/.tifa/dist/ssr.js" ] && [ -x "$repo/$dir/bin/$exe" ] ||
		die "build the app first: cd $repo/$dir && pnpm install --frozen-lockfile && go tool tifa build"
	work=$(mktemp -d)
	trap 'rm -rf "$work"' EXIT
	(cd "$repo" && go test -c -o "$work/ssr.test" ./test/ssr)
	# The data of the pages comes from the shop's program, started and ended outside valgrind: a
	# child that ends sends a signal to the counted process, and callgrind aborts on one now and then.
	(cd "$repo/test/ssr" && TIFA_SSR_SHOP=1 TIFA_COUNT_APP="$app" TIFA_COUNT_INPUTS="$work/inputs.json" "$work/ssr.test" -test.run '^$' -test.bench CountPages -test.benchtime 1x) >"$work/inputs.log" 2>&1 ||
		{ cat "$work/inputs.log" >&2; die "could not get the data of the pages from the shop"; }
	# One thread and no preemption by signal, so that Go does not run code of its own in one count
	# and not in the next. GOGC=off keeps the collector out of the counted renders.
	echo "counting $(date -u +%T): $(valgrind --version), $(lscpu | sed -n 's/^Model name: *//p')" >&2
	(cd "$repo/test/ssr" && GOMAXPROCS=1 GODEBUG=asyncpreemptoff=1 GOGC=off TIFA_SSR_SHOP=1 TIFA_COUNT_APP="$app" TIFA_COUNT_INPUTS="$work/inputs.json" \
		TIFA_COUNT_WARM="${COUNT_WARM:-2}" TIFA_COUNT_TIMED="${COUNT_TIMED:-3}" \
		valgrind --tool=callgrind --vgdb=no --callgrind-out-file="$work/cg" --dump-instr=no \
		"$work/ssr.test" -test.run '^$' -test.bench CountPages -test.benchtime 1x) >"$work/log" 2>&1 ||
		{ cp "$work/log" "${out%.tsv}.log"
			tail -n 150 "$work/log" >&2
			die "the run under valgrind failed (full log in ${out%.tsv}.log)"; }
	local pages dumps
	mapfile -t pages < <(awk '$1 == "count-page" { print $2 }' "$work/log")
	# the dump of the window of the Nth page is cg.N, and the file cg is the rest of the run
	mapfile -t dumps < <(for ((i = 1; i <= ${#pages[@]}; i++)); do [ -f "$work/cg.$i" ] && echo "$work/cg.$i"; done)
	[ "${#pages[@]}" -gt 0 ] || { tail -n 40 "$work/log" >&2; die "the run printed no page"; }
	[ "${#pages[@]}" = "${#dumps[@]}" ] || die "${#pages[@]} pages but ${#dumps[@]} dumps of callgrind"
	: >"$out"
	local i total
	for i in "${!pages[@]}"; do
		total=$(awk '/^totals:/ { print $2; exit }' "${dumps[$i]}")
		[ "${total:-0}" -gt 0 ] || die "no totals in ${dumps[$i]}"
		printf '%s\t%d\n' "${pages[$i]}" $((total / ${COUNT_TIMED:-3})) >>"$out"
	done
	cat "$out"
}

table() {
	[ $# = 2 ] || die "usage: table BASE.tsv REF.tsv"
	LC_ALL=C join -t "$(printf '\t')" <(LC_ALL=C sort "$1") <(LC_ALL=C sort "$2") | awk -F '\t' '
		function pct(a, b) { return a > 0 ? sprintf("%+.2f%%", 100 * (b - a) / a) : "-" }
		BEGIN {
			print "| page | base | ref | change |"
			print "| --- | ---: | ---: | ---: |"
		}
		{
			printf "| %s | %d | %d | %s |\n", $1, $2, $3, pct($2, $3)
			sb += $2; sr += $3
			if ($1 ~ /^store-/) { stb += $2; str += $3 } else if ($1 ~ /^mui-/) { mb += $2; mr += $3 } else { wbb += $2; wbr += $3 }
		}
		END {
			if (wbb) printf "| **workbench pages** | %d | %d | %s |\n", wbb, wbr, pct(wbb, wbr)
			if (stb) printf "| **storefront pages** | %d | %d | %s |\n", stb, str, pct(stb, str)
			if (mb) printf "| **MUI pages** | %d | %d | %s |\n", mb, mr, pct(mb, mr)
			printf "| **total** | %d | %d | %s |\n", sb, sr, pct(sb, sr)
		}'
}

[ $# -gt 0 ] || die "run or table (see the top of the script)"
cmd=$1
shift
case $cmd in
run) run "$@" ;;
table) table "$@" ;;
*) die "no command $cmd" ;;
esac
