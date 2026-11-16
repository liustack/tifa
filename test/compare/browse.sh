#!/usr/bin/env bash
# Runs the browse comparison (whole visits against Tifa, Next.js and TanStack Start)
# from build to report, or one part of it. Run it from anywhere in the repository.
#
#   test/compare/browse.sh all       build, record the traces if they are missing or out of date, run
#   test/compare/browse.sh build     install and build the shop and the two Node apps
#   test/compare/browse.sh record    record the traces that are missing or out of date in a real browser
#                                    (needs a working Chromium)
#   test/compare/browse.sh run       replay the traces at load and write the JSON and the report
#   test/compare/browse.sh report    write the report again from the JSON
#   test/compare/browse.sh status    for each server: whether its trace is current, whether its result is
#                                    current, stale or missing, and its rounds done (changes nothing)
#   test/compare/browse.sh stamp [TRACE]   print what each trace is bound to (runstamp: the results)
#
# Recording and running can be done on two machines: `record` where Chromium works, copy the folder
# $BROWSE_TRACES to the machine that runs the load, and `run` there from the same commit. `run`
# refuses traces that belong to other sources, plans or recorder than this checkout, and traces that
# do not hold every plan for every framework. The shop is one site, shoppers and staff, with one set
# of plans (plans.json), one trace a framework (<framework>.jsonl), and a stamp for each trace
# (<framework>.stamp in $BROWSE_TRACES): what all traces share (the shop, the recorder, the plans)
# and what that framework alone is made of. A change to Tifa makes only the trace of Tifa out of
# date, and `record` records only the traces that are. A result is bound to a stamp all its servers
# share (the plans, the replayer, the toolchain and the settings) and to one stamp a server (its trace
# and the build it runs). `run` continues a result that stopped (it is written after every round):
# it refuses one of another shared stamp, and measures again only the servers whose own stamp changed.
#
# Settings, as environment variables (the defaults are those of the published runs):
#   BROWSE_OUT        folder for the JSON, the report, the log and the traces       ./browse-out
#   BROWSE_TRACES     folder of the plans and traces                                $BROWSE_OUT/traces
#   SERVER_CPUS       cpus of the server, as taskset -c takes them (Linux)          none: not pinned
#   LOAD_CPUS         cpus of the load generator                                    none: not pinned
#   LOCKS             files to flock while a server of the run runs, space separated,    none
#                     first taken first: held for one round with its warm-up at a time and
#                     given back in between, so that other work on the machine fits there
#   SERVERS           spec ids. The references: tifa-nocache (Tifa with its render   tifa,nextjs,start,
#                     memo off, replays the trace of tifa), start-gzip (Start with    tifa-nocache,start-gzip,nextjsnoprefetch
#                     gzip added, replays the trace of start), nextjsnoprefetch
#                     (Next.js built with prefetching off, a trace of its own)
#   PHASES            throughput (every server at SESSIONS, then stepped), control   throughput,control,sustain,latency
#                     (Tifa with no writes), sustain (the rate of Next.js),
#                     latency (every server at half of that rate)
#   PROBE=1           search the rate of Next.js again and write it into
#                     test/compare/rates.json (else the file is used, and a run whose
#                     conditions differ from the file is refused)
#   HOLD              the command of the machine that keeps it quiet for a timing round       none
#                     (it queues the round with the other work and takes the locks): the
#                     run starts it around each round, and needs no LOCKS or ANNOUNCE then
#   ANNOUNCE          a folder in which the run leaves a file named by its pid while it      none
#                     waits for or runs a round, for a machine whose other work starts
#                     nothing new while a timing run is announced there
#   SESSIONS          concurrent sessions, no think time, every server starts at    8
#   ROUNDS SIZING TARGET   rounds per cell; the sizing run of a cell; about how      3 15s 120s
#                     long a closed round takes
#   TRIAL_WINDOW FINAL_WINDOW MAX_RATE   open trials; open latency rounds, at least; rate cap   45s 240s 16384
#   BLOCKS            blocks of 1050 visits to record (the plans)                   1
#   WORKERS           browser contexts at once while recording                      6
#   FORCE_RECORD=1    record every trace again, even those that are current
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
cd "$root"
out=${BROWSE_OUT:-./browse-out}
traces=${BROWSE_TRACES:-$out/traces}
servers=${SERVERS:-tifa,nextjs,start,tifa-nocache,start-gzip,nextjsnoprefetch}
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

plans_file=$traces/plans.json
stamp_file() { echo "$traces/$1.stamp"; } # the stamp of the trace of a framework, beside it

# The plan generators, which every trace depends on.
plan_paths="test/compare/plan.go test/compare/landing.go test/compare/mix.go test/compare/store_plan.go test/compare/store_actions.go test/compare/store_list.go test/compare/store_terms.go test/compare/store.go test/compare/store_links.json"

# trace_paths prints the trees the trace of a framework depends on. Every trace depends on the shop,
# whose pages every app serves, on the recorder and on the plan generators. The trace of Tifa depends
# on Tifa's runtime, server, command and the Go and JavaScript its build is made from, the traces of
# Next.js (with prefetching on or off) on the Next.js app, and the trace of Start on the Start app,
# the two Node apps on the domain code and the overlays they share. The order is the one the rate of
# Next.js reads its lines in (stampHash in rates.go).
trace_paths() {
  case $1 in
    tifa) echo "examples/shop test/compare/recorder $plan_paths js internal server cmd go.mod go.sum" ;;
    nextjs|nextjsnoprefetch) echo "examples/shop test/compare/nextjs test/compare/domain test/compare/overlay test/compare/recorder $plan_paths" ;;
    start) echo "examples/shop test/compare/start test/compare/domain test/compare/overlay test/compare/recorder $plan_paths" ;;
    *) echo "no trace named $1" >&2; return 1 ;;
  esac
}

# trace_stamp lists what the trace of a framework depends on, one thing a line: its trees (committed
# content, and "dirty" when any of it has uncommitted changes), the version of the framework that was
# installed, the recorder's browser, the site and its plans.
trace_stamp() {
  local name=$1 paths p glob=
  paths=$(trace_paths "$name") || return 1
  for p in $paths; do echo "tree $p $(git rev-parse HEAD:"$p")"; done
  if [ "$name" = tifa ]; then
    echo "tree root-go $(git ls-tree HEAD | awk '$4 ~ /\.go$/' | sha)"
    glob=':(glob)*.go'
  fi
  if [ -n "$(git status --porcelain --untracked-files=no -- $paths $glob)" ]; then echo "dirty"; fi
  case $name in
    nextjs|nextjsnoprefetch) echo "next $(pkgver test/compare/nextjs next)" ;;
    start) echo "start $(pkgver test/compare/start @tanstack/react-start)" ;;
  esac
  echo "playwright $(sed -n 's/^    "playwright": "\(.*\)"$/\1/p' test/compare/recorder/package.json)"
  echo "site shop"
  echo "plans $( [ -f "$plans_file" ] && sha <"$plans_file" || echo none)"
}

# run_stamp is what every result of the run depends on, whatever the server: the plans, the replayer,
# the report and this script, the toolchain and the settings.
run_stamp() {
  echo "plans $( [ -f "$plans_file" ] && sha <"$plans_file" || echo none)"
  echo "replayer $(git ls-tree HEAD test/compare/ | awk '$4 ~ /\.go$|browse\.sh$/' | sha)"
  echo "replayer-cmd $(git rev-parse HEAD:test/compare/cmd)"
  if [ -n "$(git status --porcelain --untracked-files=no -- test/compare/*.go test/compare/cmd test/compare/browse.sh)" ]; then echo "replayer-dirty"; fi
  echo "go $(go version | cut -d' ' -f3)"
  echo "node $(node --version)"
  echo "pnpm $(pnpm --version 2>/dev/null || echo none)"
  echo "settings servers=$servers phases=${PHASES:-} probe=${PROBE:-} sessions=${SESSIONS:-} rounds=${ROUNDS:-} sizing=${SIZING:-} target=${TARGET:-} trial=${TRIAL_WINDOW:-} final=${FINAL_WINDOW:-} maxrate=${MAX_RATE:-} server=${SERVER_CPUS:-} load=${LOAD_CPUS:-}"
}

# server_stamp is what the results of one server depend on besides run_stamp: the stamp of the trace
# it replays, the contents of that trace, and the executables it runs, not only their build ids (the
# shop's binary, or the built output of its Node app, without caches, which a server may write).
server_stamp() {
  local name=${1%%-*}
  echo "trace $(trace_stamp "$name" | sha)"
  echo "trace-file $( [ -f "$traces/$name.jsonl" ] && sha <"$traces/$name.jsonl" || echo none)"
  case $name in
    tifa)
      echo "tifa-build $(sed -n 's/.*"buildId": *"\([^"]*\)".*/\1/p' examples/shop/.tifa/dist/build.json 2>/dev/null | head -1)"
      echo "shop-binary $( [ -f examples/shop/bin/shop ] && sha <examples/shop/bin/shop || echo none)" ;;
    nextjs) echo "next-output $(dirsha test/compare/nextjs/.next)" ;;
    nextjsnoprefetch) echo "next-noprefetch-output $(dirsha test/compare/nextjs/.next-noprefetch)" ;;
    start) echo "start-output $(dirsha test/compare/start/.output)" ;;
  esac
}

# server_stamps is the stamp of every server of the run, as -server-stamps takes them: spec=hash.
server_stamps() {
  local s list=
  for s in $(echo "$servers" | tr "," " "); do list="$list${list:+,}$s=$(server_stamp "$s" | sha)"; done
  echo "$list"
}

# all_trace_stamps prints the stamp of every trace the servers replay, each under its name.
all_trace_stamps() {
  local name
  for name in $(trace_names); do echo "# $name"; trace_stamp "$name"; done
}

# all_run_stamps prints what the results of the run are bound to: the part every server shares, then
# the part of each server, each under its name.
all_run_stamps() {
  local s
  echo "# shared"; run_stamp
  for s in $(echo "$servers" | tr "," " "); do echo "# $s"; server_stamp "$s"; done
}

# traces_current is true when the trace of a framework was recorded from these sources, with these
# plans and recorder, and holds a trace for every plan, one for each form of the plan: the visit itself
# and each of its variants. Otherwise it says why.
traces_current() {
  local name=$1 n d
  local f=$traces/$name.jsonl stamp
  stamp=$(stamp_file "$name")
  [ -s "$stamp" ] || { echo "no $(basename "$stamp") in $traces"; return 1; }
  if ! d=$(diff <(trace_stamp "$name") "$stamp"); then
    echo "recorded from other sources than this checkout:"; echo "$d" | sed 's/^/  /'; return 1
  fi
  if trace_stamp "$name" | grep -q '^dirty'; then echo "its sources have uncommitted changes"; return 1; fi
  n=$(node -e 'const s = JSON.parse(require("fs").readFileSync(process.argv[1], "utf8")); console.log(s.plans.reduce((n, p) => n + 1 + (p.variants ? p.variants.length : 0), 0))' "$plans_file") || { echo "cannot read $plans_file"; return 1; }
  [ -s "$f" ] || { echo "no $(basename "$f")"; return 1; }
  [ "$(wc -l <"$f" | tr -d ' ')" = "$n" ] || { echo "$(basename "$f") does not hold $n visits"; return 1; }
  if grep -q '"error"' "$f"; then echo "$(basename "$f") has visits that failed to record"; return 1; fi
}

do_build() {
  build_tool
  "$compare" build
}

do_record() {
  build_tool
  # The plans are made into a folder of their own and replace those of the traces only when they
  # differ: other plans make every trace out of date, the same plans none.
  local fresh name why stale=
  fresh=$(mktemp -d "$out/plans.XXXXXX")
  "$compare" plans -dir "$fresh" -blocks "$blocks"
  cmp -s "$fresh/plans.json" "$plans_file" || mv "$fresh/plans.json" "$plans_file"
  rm -rf "$fresh"
  for name in $(trace_names); do
    if [ -n "${FORCE_RECORD:-}" ]; then
      stale="$stale $name"
    elif ! why=$(traces_current "$name"); then
      echo "the trace of $name is not current: $why"
      stale="$stale $name"
    fi
  done
  if [ -z "$stale" ]; then
    echo "the traces in $traces are current"
    return
  fi
  (cd test/compare/recorder && pnpm install --frozen-lockfile && pnpm exec playwright install chromium)
  for name in $stale; do
    # An empty stamp is never current: a recording that stops leaves the trace to be recorded again.
    : >"$(stamp_file "$name")"
    "$compare" record -dir "$traces" -skip-build -servers "$name" -workers "${WORKERS:-6}"
    trace_stamp "$name" >"$(stamp_file "$name")"
    traces_current "$name" || { echo "the trace of $name just recorded is not complete" >&2; exit 1; }
  done
}

do_run() {
  build_tool
  local why name
  for name in $(trace_names); do
    if ! why=$(traces_current "$name"); then
      echo "the trace of $name is not current: $why" >&2
      echo "record the traces with '$0 record' (on a machine with Chromium), from this commit, and copy the folder here" >&2
      exit 1
    fi
  done
  local cmd=("$compare" browse -dir "$traces" -skip-build -servers "$servers" -out "$out/browse.json" -stamp "$(run_stamp | sha)" -server-stamps "$(server_stamps)")
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
  all_run_stamps >"$out/run.stamp"
  # The tool takes the lock files itself, around each server it starts, first listed first.
  local locks=(${LOCKS:-})
  if [ ${#locks[@]} -gt 0 ]; then cmd+=(-locks "$(IFS=,; echo "${locks[*]}")"); fi
  [ -n "${ANNOUNCE:-}" ] && cmd+=(-announce "$ANNOUNCE")
  [ -n "${HOLD:-}" ] && cmd+=(-hold "$HOLD")
  "${cmd[@]}" >/dev/null 2> >(tee -a "$out/browse.log" >&2)
  do_report
}

do_status() {
  build_tool
  local s name why shared stamps
  shared=$(run_stamp | sha)
  stamps=$(server_stamps)
  for s in $(echo "$servers" | tr "," " "); do
    name=${s%%-*}
    echo "$s"
    if why=$(traces_current "$name"); then
      echo "  trace $name: current"
    else
      echo "  trace $name: not current: $why" | sed '2,$s/^/  /'
    fi
    "$compare" status -dir "$traces" -servers "$s" -out "$out/browse.json" -stamp "$shared" -server-stamps "$stamps"
  done
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
  status) do_status ;;
  stamp) if [ -n "${2:-}" ]; then trace_stamp "$2"; else all_trace_stamps; fi ;;
  runstamp) all_run_stamps ;;
  *) sed -n '2,/^set -euo/p' "$0" | sed 's/^# \{0,1\}//; /^set -euo/d'; exit 2 ;;
esac
