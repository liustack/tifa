#!/usr/bin/env bash
# Runs the browse comparison (whole visits against Tifa, Next.js, TanStack Start and React Router)
# from build to report, or one part of it. Run it from anywhere in the repository.
#
#   test/compare/browse.sh all       build, record the traces if they are missing or out of date, run
#   test/compare/browse.sh build     install and build the shop and the three Node apps
#   test/compare/browse.sh record    record the traces in a real browser (needs a working Chromium)
#   test/compare/browse.sh run       replay the traces at load and write the JSON and the report
#   test/compare/browse.sh report    write the report again from the JSON
#   test/compare/browse.sh stamp     print what the traces are bound to (runstamp: the results)
#
# Recording and running can be done on two machines: `record` where Chromium works, copy the folder
# $BROWSE_TRACES to the machine that runs the load, and `run` there from the same commit. `run`
# refuses traces that belong to other sources, plans or recorder than this checkout, and traces that
# do not hold every plan for every framework. The shop is one site, shoppers and staff, with one set
# of plans (plans.json), one trace a framework (<server>.jsonl) and one stamp (STAMP in
# $BROWSE_TRACES), which the one scenario, shop, replays. A result is bound to the traces, the builds,
# the toolchain and the settings: `run` continues a result that stopped (it is written after every
# round) only if all of that is the same, and otherwise asks you to remove it.
#
# Settings, as environment variables (the defaults are those of the published runs):
#   BROWSE_OUT        folder for the JSON, the report, the log and the traces       ./browse-out
#   BROWSE_TRACES     folder of the plans and traces                                $BROWSE_OUT/traces
#   SERVER_CPUS       cpus of the server, as taskset -c takes them (Linux)          none: not pinned
#   LOAD_CPUS         cpus of the load generator                                    none: not pinned
#   LOCKS             files to flock while running, space separated, first taken first   none
#   SERVERS           spec ids (tifa-nocache: Tifa with its render memo off,         tifa,nextjs,start,reactrouter,tifa-nocache
#                     a reference that replays the trace of tifa)
#   PHASES            throughput (every server at SESSIONS, then stepped), control   throughput,control,sustain,latency
#                     (Tifa with no writes), sustain (the rate of Next.js),
#                     latency (every server at half of that rate)
#   PROBE=1           search the rate of Next.js again and write it into
#                     test/compare/rates.json (else the file is used, and a run whose
#                     conditions differ from the file is refused)
#   SESSIONS          concurrent sessions, no think time, every server starts at    8
#   ROUNDS SIZING TARGET   rounds per cell; the sizing run of a cell; about how      3 15s 75s
#                     long a closed round takes
#   TRIAL_WINDOW FINAL_WINDOW MAX_RATE   open trials; open latency rounds, at least; rate cap   45s 240s 16384
#   BLOCKS            blocks of 1050 visits to record (the plans)                   1
#   WORKERS           browser contexts at once while recording                      6
#   FORCE_RECORD=1    record again even if the traces are current
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
cd "$root"
out=${BROWSE_OUT:-./browse-out}
traces=${BROWSE_TRACES:-$out/traces}
servers=${SERVERS:-tifa,nextjs,start,reactrouter,tifa-nocache}
# trace_names are the trace files the servers replay, each once: a spec replays the trace of its
# framework, its id before the first dash (traceName in recorder.go), so tifa-nocache replays tifa.jsonl.
trace_names() { echo "$servers" | tr "," "\n" | sed "s/-.*//" | awk "!seen[\$0]++"; }
# The names before sessions are refused, so that an old command line does not run another method.
for old in USERS EXTRA_USERS; do
  if [ -n "${!old+set}" ]; then echo "$old is gone: the throughput counts concurrent sessions, set SESSIONS (8 by default)" >&2; exit 2; fi
done
if [ -n "${SCENARIOS+set}" ]; then echo "SCENARIOS is gone: the comparison has one scenario, the shop" >&2; exit 2; fi
if [ -n "${WARM+set}" ]; then echo "WARM is gone: every server gets the same warm-up of the mix, and SIZING is the length of a sizing run (15s by default)" >&2; exit 2; fi
blocks=${BLOCKS:-1}
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

stamp_file=$traces/STAMP
plans_file=$traces/plans.json

# trace_stamp lists what the traces depend on, one thing a line: the trees of the shop,
# the three apps, the shared domain code, the overlays, the recorder and the plan generators
# (committed content, and "dirty" when any of it has uncommitted changes), the framework versions
# that were installed, the recorder's browser, the site and its plans.
trace_stamp() {
  # The shop, the three apps and everything that builds or serves the shop: Tifa's runtime, server,
  # command and the Go and JavaScript the build is made from.
  local paths="examples/shop test/compare/nextjs test/compare/start test/compare/reactrouter test/compare/domain test/compare/overlay test/compare/recorder test/compare/plan.go test/compare/landing.go test/compare/mix.go test/compare/store_plan.go test/compare/store_actions.go test/compare/store_list.go test/compare/store_terms.go test/compare/store.go test/compare/store_links.json js internal server cmd go.mod go.sum"
  local p
  for p in $paths; do echo "tree $p $(git rev-parse HEAD:"$p")"; done
  echo "tree root-go $(git ls-tree HEAD | awk '$4 ~ /\.go$/' | sha)"
  if [ -n "$(git status --porcelain --untracked-files=no -- $paths ':(glob)*.go')" ]; then echo "dirty"; fi
  echo "next $(pkgver test/compare/nextjs next)"
  echo "start $(pkgver test/compare/start @tanstack/react-start)"
  echo "reactrouter $(pkgver test/compare/reactrouter react-router)"
  echo "playwright $(sed -n 's/^    "playwright": "\(.*\)"$/\1/p' test/compare/recorder/package.json)"
  echo "site shop"
  echo "plans $( [ -f "$plans_file" ] && sha <"$plans_file" || echo none)"
}

# run_stamp adds what a result depends on besides the traces: the toolchain, the builds that were
# started and the settings.
run_stamp() {
  local f
  echo "traces $(trace_stamp | sha)"
  # What is replayed: the contents of the trace files, and the replayer, the report and this script.
  echo "trace-files $(for f in $(trace_names); do cat "$traces/$f.jsonl" 2>/dev/null || true; done | sha)"
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
  echo "settings servers=$servers phases=${PHASES:-} probe=${PROBE:-} sessions=${SESSIONS:-} rounds=${ROUNDS:-} sizing=${SIZING:-} target=${TARGET:-} trial=${TRIAL_WINDOW:-} final=${FINAL_WINDOW:-} maxrate=${MAX_RATE:-} server=${SERVER_CPUS:-} load=${LOAD_CPUS:-}"
}

# traces_current is true when the traces were recorded from these sources, with these plans and
# recorder, and hold a trace for every plan of every server, one for each form of the plan: the
# visit itself and each of its variants.
traces_current() {
  local n f
  [ -s "$stamp_file" ] || { echo "no STAMP in $traces"; return 1; }
  if ! diff <(trace_stamp) "$stamp_file" >"$out/stamp.diff"; then
    echo "the traces belong to other sources than this checkout:"; sed 's/^/  /' "$out/stamp.diff"; return 1
  fi
  if trace_stamp | grep -q '^dirty'; then echo "the sources have uncommitted changes"; return 1; fi
  n=$(node -e 'const s = JSON.parse(require("fs").readFileSync(process.argv[1], "utf8")); console.log(s.plans.reduce((n, p) => n + 1 + (p.variants ? p.variants.length : 0), 0))' "$plans_file") || { echo "cannot read $plans_file"; return 1; }
  for f in $(trace_names); do
    f=$traces/$f.jsonl
    [ -s "$f" ] || { echo "no $(basename "$f")"; return 1; }
    [ "$(wc -l <"$f" | tr -d ' ')" = "$n" ] || { echo "$(basename "$f") does not hold $n visits"; return 1; }
    if grep -q '"error"' "$f"; then echo "$(basename "$f") has visits that failed to record"; return 1; fi
  done
}

do_build() {
  build_tool
  "$compare" build
}

do_record() {
  build_tool
  if [ -z "${FORCE_RECORD:-}" ] && traces_current >"$out/traces.why" 2>&1; then
    echo "the traces in $traces are current"
    return
  fi
  "$compare" plans -dir "$traces" -blocks "$blocks"
  (cd test/compare/recorder && pnpm install --frozen-lockfile && pnpm exec playwright install chromium)
  # An empty stamp is never current: a recording that stops leaves the traces to be recorded again.
  : >"$stamp_file"
  "$compare" record -dir "$traces" -skip-build -servers "$servers" -workers "${WORKERS:-6}"
  trace_stamp >"$stamp_file"
  traces_current || { echo "the traces just recorded are not complete" >&2; exit 1; }
}

do_run() {
  build_tool
  local why
  if ! why=$(traces_current); then
    echo "$why" >&2
    echo "record the traces with '$0 record' (on a machine with Chromium), from this commit, and copy the folder here" >&2
    exit 1
  fi
  local cmd=("$compare" browse -dir "$traces" -skip-build -servers "$servers" -out "$out/browse.json" -stamp "$(run_stamp | sha)")
  [ -n "${PHASES:-}" ] && cmd+=(-phases "$PHASES")
  [ "${PROBE:-}" = 1 ] && cmd+=(-probe)
  [ -n "${SESSIONS:-}" ] && cmd+=(-sessions "$SESSIONS")
  [ -n "${ROUNDS:-}" ] && cmd+=(-rounds "$ROUNDS")
  [ -n "${SIZING:-}" ] && cmd+=(-sizing "$SIZING")
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
  stamp) trace_stamp ;;
  runstamp) run_stamp ;;
  *) sed -n '2,/^set -euo/p' "$0" | sed 's/^# \{0,1\}//; /^set -euo/d'; exit 2 ;;
esac
