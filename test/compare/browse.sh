#!/usr/bin/env bash
# Runs the browse comparison (whole visits against Tifa, Next.js, TanStack Start and React Router)
# from build to report, or one part of it. Run it from anywhere in the repository.
#
#   test/compare/browse.sh all       build, record the traces if they are missing or out of date, run
#   test/compare/browse.sh build     install and build the shop and the three Node apps
#   test/compare/browse.sh record    record the traces in a real browser (needs a working Chromium)
#   test/compare/browse.sh run       replay the traces at load and write the JSON and the report
#   test/compare/browse.sh stamp     print which sources the traces belong to
#
# Recording and running can be done on two machines: `record` where Chromium works, copy the folder
# $BROWSE_TRACES to the machine that runs the load (it must be built from the same commit, which
# `run` checks against the file STAMP), and `run` there.
#
# Settings, as environment variables (the defaults are those of the published runs):
#   BROWSE_OUT          folder for the JSON, the report, the log and the traces   ./browse-out
#   BROWSE_TRACES       folder of the plans and traces                            $BROWSE_OUT/traces
#   SERVER_CPUS         cpus of the server, as taskset -c takes them (Linux)      none: not pinned
#   LOAD_CPUS           cpus of the load generator                                none: not pinned
#   LOCKS               files to flock while running, space separated             none
#   SERVERS             spec ids                                                  tifa,nextjs,start,reactrouter
#   SCENARIOS           office-w5, office-w20, office-w50 (back office writes),    office-w5,office-w20,office-w50,r80w20,store-w20
#                       r95w5, r80w20, r50w50 (every write renames a customer),
#                       store-w5, store-w20, store-w50 (the storefront, where Tifa runs twice: with
#                       the whole-page cache off, and with every cache off)
#                       Only the traces the scenarios need are recorded and checked: the workbench
#                       and/or the storefront (STAMP and STAMP-store in $BROWSE_TRACES).
#   VISITS              visits to record for each framework                       100
#   WORKERS             browser contexts at once while recording                  6
#   ROUNDS WARM WINDOW SEARCH_WARM SEARCH_WINDOW MAX_USERS                         3 10s 30s 3s 10s 128
#   SOURCE              names the run in the report                               run
#   FORCE_RECORD=1      record again even if the traces are current
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
cd "$root"
out=${BROWSE_OUT:-./browse-out}
traces=${BROWSE_TRACES:-$out/traces}
servers=${SERVERS:-tifa,nextjs,start,reactrouter}
visits=${VISITS:-100}
mkdir -p "$out" "$traces"
compare=$out/compare

build_tool() { go build -o "$compare" ./test/compare/cmd/compare; }

# stamp names the sources that traces depend on: the trees of the shop and of the three apps at HEAD.
# Uncommitted changes to them make it say "dirty", and then the traces never match.
stamp() {
  local s
  s=$(git rev-parse HEAD:examples/shop HEAD:test/compare/nextjs HEAD:test/compare/start HEAD:test/compare/reactrouter HEAD:test/compare/domain HEAD:test/compare/overlay | tr '\n' ' ')
  if [ -n "$(git status --porcelain --untracked-files=no -- examples/shop test/compare/nextjs test/compare/start test/compare/reactrouter test/compare/domain test/compare/overlay)" ]; then
    s="$s dirty"
  fi
  echo "$s"
}

scenarios=${SCENARIOS:-office-w5,office-w20,office-w50,r80w20,store-w20}

# sites lists the sites the scenarios need traces of: "workbench" for any scenario but the
# storefront ones, "store" for the storefront ones. Their files are plans-m4.json and <server>-m4.jsonl,
# and plans-store-m4.json and <server>-store-m4.jsonl, each site with its own stamp.
sites() {
  local s out=""
  for s in $(echo "$scenarios" | tr ',' ' '); do
    case "$s" in
      store-*) case "$out" in *store*) ;; *) out="$out store" ;; esac ;;
      *) case "$out" in *workbench*) ;; *) out="$out workbench" ;; esac ;;
    esac
  done
  echo $out
}

# site_tag is what a site adds to the names of its files, and site_stamp is its stamp file.
site_tag() { [ "$1" = store ] && echo "-store" || echo ""; }
site_stamp() { [ "$1" = store ] && echo "$traces/STAMP-store" || echo "$traces/STAMP"; }

# traces_current says whether the traces of a site are there and belong to the sources of this checkout.
traces_current() {
  local site=$1 tag f
  tag=$(site_tag "$site")
  [ -f "$(site_stamp "$site")" ] || return 1
  [ "$(cat "$(site_stamp "$site")")" = "$(stamp)" ] || return 1
  case "$(stamp)" in *dirty*) return 1 ;; esac
  for f in $(echo "$servers" | tr ',' ' '); do
    [ -s "$traces/$f$tag-m4.jsonl" ] || return 1
  done
  [ -s "$traces/plans$tag-m4.json" ]
}

do_build() {
  build_tool
  "$compare" build
}

do_record() {
  build_tool
  local site todo=""
  for site in $(sites); do
    if [ -z "${FORCE_RECORD:-}" ] && traces_current "$site"; then
      echo "the $site traces in $traces are current: $(cat "$(site_stamp "$site")")"
    else
      todo="$todo $site"
    fi
  done
  [ -n "$todo" ] || return 0
  "$compare" plans -dir "$traces" -visits 400
  (cd test/compare/recorder && pnpm install --frozen-lockfile && pnpm exec playwright install chromium)
  for site in $todo; do
    "$compare" record -dir "$traces" -skip-build -servers "$servers" -site "$site" -means 4 -limit "$visits" -workers "${WORKERS:-6}"
    stamp >"$(site_stamp "$site")"
  done
}

do_run() {
  build_tool
  local site
  for site in $(sites); do
    if ! traces_current "$site"; then
      echo "the $site traces in $traces are missing, or belong to other sources than this checkout:" >&2
      echo "  traces: $(cat "$(site_stamp "$site")" 2>/dev/null || echo none)" >&2
      echo "  here:   $(stamp)" >&2
      echo "record them with '$0 record' (on a machine with Chromium), build from the same commit, and copy the folder here" >&2
      exit 1
    fi
  done
  local cmd=("$compare" browse -dir "$traces" -skip-build -servers "$servers" -scenarios "$scenarios"
    -rounds "${ROUNDS:-3}" -warm "${WARM:-10s}" -window "${WINDOW:-30s}" -search-warm "${SEARCH_WARM:-3s}" -search-window "${SEARCH_WINDOW:-10s}"
    -max-users "${MAX_USERS:-128}" -check -source "${SOURCE:-run}" -json "$out/browse.json")
  [ -n "${SERVER_CPUS:-}" ] && cmd+=(-server-cpus "$SERVER_CPUS")
  [ -n "${LOAD_CPUS:-}" ] && cmd+=(-load-cpus "$LOAD_CPUS")
  # The first lock listed is taken first, so it is wrapped last.
  local locks=(${LOCKS:-}) i
  for ((i = ${#locks[@]} - 1; i >= 0; i--)); do
    cmd=(flock -o "${locks[i]}" "${cmd[@]}")
  done
  "${cmd[@]}" >"$out/browse.stdout" 2> >(tee "$out/browse.log" >&2)
  "$compare" report "$out/browse.json" >"$out/browse.md"
  echo "wrote $out/browse.json and $out/browse.md"
}

case "${1:-}" in
  all) do_build; do_record; do_run ;;
  build) do_build ;;
  record) do_record ;;
  run) do_run ;;
  stamp) stamp ;;
  *) sed -n '2,/^set -euo/p' "$0" | sed 's/^# \{0,1\}//; /^set -euo/d'; exit 2 ;;
esac
