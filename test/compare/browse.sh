#!/usr/bin/env bash
# Runs the browse comparison (whole visits against Tifa, Next.js, TanStack Start and React Router)
# from build to report, or one part of it. Run it from anywhere in the repository.
#
#   test/compare/browse.sh all       build, record the traces if they are missing or out of date, run
#   test/compare/browse.sh build     install and build the shop and the three Node apps
#   test/compare/browse.sh record    record the traces in a real browser (needs a working Chromium)
#   test/compare/browse.sh run       replay the traces at load and write the JSON and the report
#   test/compare/browse.sh report    write the report again from the JSON
#   test/compare/browse.sh stamp [site]   print what the traces of a site are bound to (runstamp: the results)
#
# Recording and running can be done on two machines: `record` where Chromium works, copy the folder
# $BROWSE_TRACES to the machine that runs the load, and `run` there from the same commit. `run`
# refuses traces that belong to other sources, plans or recorder than this checkout, and traces that
# do not hold every plan for every framework. The workbench and the storefront are two sites, each
# with its plans, its traces and its stamp (STAMP and STAMP-store in $BROWSE_TRACES), and only the
# sites the scenarios need are recorded and checked. A result is bound to the traces, the builds,
# the toolchain and the settings: `run` continues a result that stopped (it is written after every
# round) only if all of that is the same, and otherwise asks you to remove it.
#
# Settings, as environment variables (the defaults are those of the published runs):
#   BROWSE_OUT        folder for the JSON, the report, the log and the traces       ./browse-out
#   BROWSE_TRACES     folder of the plans and traces                                $BROWSE_OUT/traces
#   SERVER_CPUS       cpus of the server, as taskset -c takes them (Linux)          none: not pinned
#   LOAD_CPUS         cpus of the load generator                                    none: not pinned
#   LOCKS             files to flock while running, space separated, first taken first   none
#   SERVERS           spec ids                                                      tifa,nextjs,start,reactrouter
#   SCENARIOS         office-w5, office-w20, office-w50 (back office writes),       office-w5,office-w20,office-w50,r80w20,store-w5,store-w20
#                     r95w5, r80w20, r50w50 (every write renames a customer),
#                     store-w5, store-w20, store-w50 (the storefront)
#   PHASES            closed (throughput at equal users), saturation (the users at   closed,saturation,sustain,latency
#                     which each server saturates), sustain (the rate of Next.js), latency
#                     (sustain and latency run only for the scenarios with 5% writes)
#   PROBE=1           probe the users at which each server saturates again, and rewrite
#                     test/compare/saturation.json (else it is read, and probed where it has none)
#   USERS EXTRA_USERS users of every scenario; more users for the 5% write scenarios   32, 8,16
#   ROUNDS WARM TARGET     rounds per cell; warm-up; about how long a round takes     3 15s 75s
#   TRIAL_WINDOW FINAL_WINDOW MAX_RATE   open trials; open latency rounds, at least; rate cap   45s 240s 16384
#   BLOCKS            blocks of 95 visits to record for each site (the plans)       4
#   WORKERS           browser contexts at once while recording                      6
#   FORCE_RECORD=1    record again even if the traces are current
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
cd "$root"
out=${BROWSE_OUT:-./browse-out}
traces=${BROWSE_TRACES:-$out/traces}
servers=${SERVERS:-tifa,nextjs,start,reactrouter}
scenarios=${SCENARIOS:-office-w5,office-w20,office-w50,r80w20,store-w5,store-w20}
blocks=${BLOCKS:-4}
mkdir -p "$out" "$traces"
compare=$out/compare

sha() { if command -v sha256sum >/dev/null; then sha256sum | cut -c1-16; else shasum -a 256 | cut -c1-16; fi; }
dirsha() { # a hash of the files under a folder, by path and content, without caches
  [ -d "$1" ] || { echo none; return; }
  (cd "$1" && find . -type f -not -path '*/cache/*' | LC_ALL=C sort | while read -r f; do echo "$f"; cat "$f"; done | sha)
}
pkgver() { # version of a package in an app's node_modules
  local f=$1/node_modules/$2/package.json
  [ -f "$f" ] && sed -n 's/^  "version": "\(.*\)",$/\1/p' "$f" | head -1 || echo none
}

build_tool() { go build -o "$compare" ./test/compare/cmd/compare; }

# sites lists the sites the scenarios need traces of: "workbench" for any scenario but the
# storefront ones, "store" for the storefront ones.
sites() {
  local s list=""
  for s in $(echo "$scenarios" | tr ',' ' '); do
    case "$s" in
      store-*) case "$list" in *store*) ;; *) list="$list store" ;; esac ;;
      *) case "$list" in *workbench*) ;; *) list="$list workbench" ;; esac ;;
    esac
  done
  echo $list
}

# site_tag is what a site adds to the names of its files: plans.json and <server>.jsonl for the
# workbench, plans-store.json and <server>-store.jsonl for the storefront. site_stamp is its stamp.
site_tag() { if [ "$1" = store ]; then echo "-store"; fi; }
site_stamp() { echo "$traces/STAMP$(site_tag "$1")"; }

# trace_stamp lists what the traces of a site depend on, one thing a line: the trees of the shop,
# the three apps, the shared domain code, the overlays, the recorder and the plan generators
# (committed content, and "dirty" when any of it has uncommitted changes), the framework versions
# that were installed, the recorder's browser, the site and its plans.
trace_stamp() {
  local site=${1:-workbench}
  # The shop, the three apps and everything that builds or serves the shop: Tifa's runtime, server,
  # command and the Go and JavaScript the build is made from.
  local paths="examples/shop test/compare/nextjs test/compare/start test/compare/reactrouter test/compare/domain test/compare/overlay test/compare/recorder test/compare/plan.go test/compare/store_plan.go test/compare/store_links.json js internal server cmd go.mod go.sum"
  local p
  for p in $paths; do echo "tree $p $(git rev-parse HEAD:"$p")"; done
  echo "tree root-go $(git ls-tree HEAD | awk '$4 ~ /\.go$/' | sha)"
  if [ -n "$(git status --porcelain --untracked-files=no -- $paths ':(glob)*.go')" ]; then echo "dirty"; fi
  echo "next $(pkgver test/compare/nextjs next)"
  echo "start $(pkgver test/compare/start @tanstack/react-start)"
  echo "reactrouter $(pkgver test/compare/reactrouter react-router)"
  echo "playwright $(sed -n 's/^    "playwright": "\(.*\)"$/\1/p' test/compare/recorder/package.json)"
  echo "site $site"
  local plans
  plans="$traces/plans$(site_tag "$site").json"
  echo "plans $( [ -f "$plans" ] && sha <"$plans" || echo none)"
}

# run_stamp adds what a result depends on besides the traces: the toolchain, the builds that were
# started and the settings.
run_stamp() {
  local site f
  echo "traces $(for site in $(sites); do trace_stamp "$site"; done | sha)"
  # What is replayed: the contents of the trace files of the sites, and the replayer, the report and
  # this script.
  echo "trace-files $(for site in $(sites); do for f in $(echo "$servers" | tr ',' ' '); do cat "$traces/$f$(site_tag "$site").jsonl" 2>/dev/null || true; done; done | sha)"
  echo "replayer $(git ls-tree HEAD test/compare/ | awk '$4 ~ /\.go$|browse\.sh$/' | sha)"
  echo "replayer-cmd $(git rev-parse HEAD:test/compare/cmd)"
  if [ -n "$(git status --porcelain --untracked-files=no -- test/compare/*.go test/compare/cmd test/compare/browse.sh)" ]; then echo "replayer-dirty"; fi
  echo "go $(go version | cut -d' ' -f3)"
  echo "node $(node --version)"
  echo "pnpm $(pnpm --version 2>/dev/null || echo none)"
  # The executables that run, not only their build ids: the shop's binary and the built output of the
  # three Node apps (without caches, which a server may write).
  echo "tifa-build $(sed -n 's/.*"buildId": *"\([^"]*\)".*/\1/p' examples/shop/.tifa/dist/build.json 2>/dev/null | head -1)"
  echo "shop-binary $(sha <examples/shop/bin/shop 2>/dev/null || echo none)"
  echo "next-output $(dirsha test/compare/nextjs/.next)"
  echo "start-output $(dirsha test/compare/start/.output)"
  echo "reactrouter-output $(dirsha test/compare/reactrouter/build)"
  echo "settings servers=$servers scenarios=$scenarios phases=${PHASES:-} probe=${PROBE:-} users=${USERS:-} extra=${EXTRA_USERS-} rounds=${ROUNDS:-} warm=${WARM:-} target=${TARGET:-} trial=${TRIAL_WINDOW:-} final=${FINAL_WINDOW:-} maxrate=${MAX_RATE:-} server=${SERVER_CPUS:-} load=${LOAD_CPUS:-}"
}

# traces_current is true when the traces of a site were recorded from these sources, with these
# plans and recorder, and hold a trace for every plan of every server.
traces_current() {
  local site=$1 tag stamp plans n f
  tag=$(site_tag "$site")
  stamp=$(site_stamp "$site")
  [ -s "$stamp" ] || { echo "no $(basename "$stamp") in $traces"; return 1; }
  if ! diff <(trace_stamp "$site") "$stamp" >"$out/stamp.diff"; then
    echo "the $site traces belong to other sources than this checkout:"; sed 's/^/  /' "$out/stamp.diff"; return 1
  fi
  if trace_stamp "$site" | grep -q '^dirty'; then echo "the sources have uncommitted changes"; return 1; fi
  plans=$traces/plans$tag.json
  n=$(sed -n 's/.*"blocks": *\([0-9]*\).*/\1/p' "$plans" | head -1)
  n=$((${n:-0} * 95))
  for f in $(echo "$servers" | tr ',' ' '); do
    f=$traces/$f$tag.jsonl
    [ -s "$f" ] || { echo "no $(basename "$f")"; return 1; }
    [ "$(wc -l <"$f" | tr -d ' ')" = "$n" ] || { echo "$(basename "$f") does not hold $n visits"; return 1; }
    if grep -q '"error"' "$f"; then echo "$(basename "$f") has visits that failed to record"; return 1; fi
  done
}

# stale lists the sites whose traces must be recorded.
stale() {
  local site list=""
  for site in $(sites); do
    if [ -n "${FORCE_RECORD:-}" ] || ! traces_current "$site" >"$out/traces-$site.why" 2>&1; then
      list="$list $site"
    fi
  done
  echo $list
}

do_build() {
  build_tool
  "$compare" build
}

do_record() {
  build_tool
  local todo site
  todo=$(stale)
  if [ -z "$todo" ]; then
    echo "the traces in $traces are current"
    return
  fi
  # The plans of both sites are written again. They are the same for the same blocks and seed, so a
  # site whose traces were current stays current unless BLOCKS changed.
  "$compare" plans -dir "$traces" -blocks "$blocks"
  todo=$(stale)
  (cd test/compare/recorder && pnpm install --frozen-lockfile && pnpm exec playwright install chromium)
  for site in $todo; do
    # An empty stamp is never current: a recording that stops leaves the site to be recorded again.
    : >"$(site_stamp "$site")"
    "$compare" record -dir "$traces" -skip-build -servers "$servers" -site "$site" -workers "${WORKERS:-6}"
    trace_stamp "$site" >"$(site_stamp "$site")"
    traces_current "$site" || { echo "the $site traces just recorded are not complete" >&2; exit 1; }
  done
}

do_run() {
  build_tool
  local site why
  for site in $(sites); do
    if ! why=$(traces_current "$site"); then
      echo "$why" >&2
      echo "record the traces with '$0 record' (on a machine with Chromium), from this commit, and copy the folder here" >&2
      exit 1
    fi
  done
  local cmd=("$compare" browse -dir "$traces" -skip-build -servers "$servers" -scenarios "$scenarios" -out "$out/browse.json" -stamp "$(run_stamp | sha)")
  [ -n "${PHASES:-}" ] && cmd+=(-phases "$PHASES")
  [ "${PROBE:-}" = 1 ] && cmd+=(-probe)
  [ -n "${USERS:-}" ] && cmd+=(-users "$USERS")
  [ "${EXTRA_USERS+set}" = set ] && cmd+=(-extra-users "$EXTRA_USERS")
  [ -n "${ROUNDS:-}" ] && cmd+=(-rounds "$ROUNDS")
  [ -n "${WARM:-}" ] && cmd+=(-warm "$WARM")
  [ -n "${TARGET:-}" ] && cmd+=(-target "$TARGET")
  [ -n "${TRIAL_WINDOW:-}" ] && cmd+=(-trial-window "$TRIAL_WINDOW")
  [ -n "${FINAL_WINDOW:-}" ] && cmd+=(-final-window "$FINAL_WINDOW")
  [ -n "${MAX_RATE:-}" ] && cmd+=(-max-rate "$MAX_RATE")
  [ -n "${SERVER_CPUS:-}" ] && cmd+=(-server-cpus "$SERVER_CPUS")
  [ -n "${LOAD_CPUS:-}" ] && cmd+=(-load-cpus "$LOAD_CPUS")
  run_stamp >"$out/run.stamp"
  # The first lock listed is taken first, so it is wrapped last.
  local locks=(${LOCKS:-}) i
  for ((i = ${#locks[@]} - 1; i >= 0; i--)); do
    cmd=(flock -o "${locks[i]}" "${cmd[@]}")
  done
  "${cmd[@]}" >/dev/null 2> >(tee -a "$out/browse.log" >&2)
  do_report
}

do_report() {
  build_tool
  "$compare" report "$out/browse.json" >"$out/browse.md"
  echo "wrote $out/browse.json and $out/browse.md"
}

case "${1:-}" in
  all) do_build; do_record; do_run ;;
  build) do_build ;;
  record) do_record ;;
  run) do_run ;;
  report) do_report ;;
  stamp) trace_stamp "${2:-workbench}" ;;
  runstamp) run_stamp ;;
  *) sed -n '2,/^set -euo/p' "$0" | sed 's/^# \{0,1\}//; /^set -euo/d'; exit 2 ;;
esac
