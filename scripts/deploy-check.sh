#!/bin/sh
# Checks tifa deploy against a real server: setup, a deploy, a second deploy while the server
# asks the app for a page every 10 ms (no request may fail), and a rollback. It needs a Linux
# server with systemd that `ssh root@<host>` reaches, and changes it: it creates the user tifa
# and the folder and units of the app. CI does not run it. See docs/internals/testing.md.
#
#   scripts/deploy-check.sh <host> <app root> [path to ask for, default /]
#
# With DEPLOY_CHECK_URL set to the public address of the app behind Cloudflare or nginx, such as
# https://shop.example.com, the second deploy also gets 16 loops of requests from this machine
# through that front, and the script prints what the Cloudflare cache did with an asset, the page
# and /__tifa/session (nothing for nginx). DEPLOY_CHECK_CURL_ARGS adds arguments to those curl
# calls, such as -k for a certificate of the Let's Encrypt staging server. DEPLOY_CHECK_SETUP_ARGS
# adds arguments to setup, such as --yes when there is no terminal to answer on.
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
# shellcheck disable=SC2086
tifa deploy setup ${DEPLOY_CHECK_SETUP_ARGS:-}
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
public=""
if [ -n "${DEPLOY_CHECK_URL:-}" ]; then
	rm -f "$tmp/stop"
	for i in $(seq 1 16); do
		(while [ ! -e "$tmp/stop" ]; do
			curl -s -o /dev/null -w '%{http_code} %{time_total}\n' --max-time 15 ${DEPLOY_CHECK_CURL_ARGS:-} "$DEPLOY_CHECK_URL$path" || echo "000 0"
		done) > "$tmp/public-$i" &
		public="$public $!"
	done
fi
sleep 3
tifa deploy
sleep 3
ssh -l root -- "$host" "touch /tmp/tifa-deploy-check.stop"
status=0
wait "$loop" || status=$?
cat "$tmp/loop"
if [ -n "$public" ]; then
	touch "$tmp/stop"
	# shellcheck disable=SC2086
	wait $public
	cat "$tmp"/public-* | awk '{ n++; if ($1 !~ /^[23]/) bad++; if ($2 > m) m = $2 } END { printf "through the front: sent %d, failed %d, slowest %ss\n", n, bad, m; exit bad > 0 }' || status=1
	asset=$(curl -s ${DEPLOY_CHECK_CURL_ARGS:-} "$DEPLOY_CHECK_URL$path" | grep -o '/assets/[^"]*\.js' | head -n 1)
	for p in "$asset" "$asset" "$asset" "$path" "$path" /__tifa/session; do
		printf '%s %s\n' "$(curl -s ${DEPLOY_CHECK_CURL_ARGS:-} -o /dev/null -D - "$DEPLOY_CHECK_URL$p" | tr -d '\r' | sed -n 's/^cf-cache-status: //p')" "$p"
	done
fi

step rollback
start=$(date +%s)
tifa deploy rollback
echo "rollback took $(($(date +%s) - start))s"
ssh -l root -- "$host" "cat /srv/tifa/$name/deploys.log"
exit "$status"
