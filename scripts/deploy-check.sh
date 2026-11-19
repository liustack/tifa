#!/bin/sh
# Checks tifa deploy against a real server: setup, a deploy, a second deploy while the server
# asks the app for a page every 10 ms (no request may fail), and a rollback. It needs a Linux
# server with systemd that `ssh root@<host>` reaches, and changes it: it creates the user tifa
# and the folder and units of the app. CI does not run it. See docs/testing.md.
#
#   scripts/deploy-check.sh <host> <app root> [path to ask for, default /]
#
# Run it from the root of the checkout. The app needs a git commit only for the deploy records.
set -eu

host=${1:?usage: scripts/deploy-check.sh <host> <app root> [path]}
app=${2:?usage: scripts/deploy-check.sh <host> <app root> [path]}
path=${3:-/}
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
go build -o "$tmp/tifa" ./cmd/tifa
tifa() { "$tmp/tifa" "$@" --dir "$app" --host "$host"; }

name=$(basename "$(sed -n 's/^module //p' "$app/go.mod")")
step() { printf '\n== %s\n' "$*"; }

step setup
tifa deploy setup
port=$(ssh -l root -- "$host" "cat /srv/tifa/$name/port")

step deploy
tifa deploy
ssh -l root -- "$host" "curl -s -o /dev/null -w 'GET $path answered %{http_code}\n' http://127.0.0.1:$port$path"

step "deploy again while the server asks for $path every 10 ms"
# The loop runs on the server until the deploy has ended.
ssh -l root -- "$host" "rm -f /tmp/tifa-deploy-check.stop; ok=0; bad=0; slowest=0
	while [ ! -e /tmp/tifa-deploy-check.stop ]; do
		r=\$(curl -s -o /dev/null -w '%{http_code} %{time_total}' --max-time 10 http://127.0.0.1:$port$path) || true
		case \"\$r\" in 2*|3*) ok=\$((ok + 1)) ;; *) bad=\$((bad + 1)); echo \"failed: \$r\" ;; esac
		t=\${r#* }; slowest=\$(echo \"\$t \$slowest\" | awk '{ print (\$1 > \$2) ? \$1 : \$2 }')
		sleep 0.01
	done
	rm -f /tmp/tifa-deploy-check.stop
	echo \"sent \$((ok + bad)), failed \$bad, slowest \${slowest}s\"
	[ \"\$bad\" = 0 ]" > "$tmp/loop" 2>&1 &
loop=$!
sleep 3
tifa deploy
sleep 3
ssh -l root -- "$host" "touch /tmp/tifa-deploy-check.stop"
status=0
wait "$loop" || status=$?
cat "$tmp/loop"

step rollback
start=$(date +%s)
tifa deploy rollback
echo "rollback took $(($(date +%s) - start))s"
ssh -l root -- "$host" "cat /srv/tifa/$name/deploys.log"
exit "$status"
