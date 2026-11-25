#!/bin/sh
# Fails when the repository holds a file that should not be committed: a file over
# 1 MB, a binary file that is not an image or a font, such as a program built in
# the checkout, or a secret, a credential or a profile by its name. With --staged it checks what the next commit would hold (the
# pre-commit hook), otherwise the files of HEAD (CI).
#
#   scripts/check-files.sh            check HEAD
#   scripts/check-files.sh --staged   check the index
#
# Install as a local hook: ln -s ../../scripts/check-files.sh .git/hooks/pre-commit
# (git runs it with no arguments from the hook, so the hook path checks the index).
set -eu

limit=1000000
allowed='\.(avif|png|jpe?g|webp|gif|ico|woff2?)$'

mode=head
if [ "${1:-}" = "--staged" ] || [ "$(basename "$0")" = "pre-commit" ]; then
	mode=staged
fi

if [ "$mode" = staged ]; then
	base=$(git rev-parse -q --verify HEAD || git hash-object -t tree /dev/null)
	# -z: git quotes and escapes a path with a double quote or a non-ASCII byte unless it is told not to.
	binaries=$(git diff --cached -z --numstat --diff-filter=AM "$base" | tr '\0' '\n' | awk -F'\t' '$1 == "-" { print $3 }')
	# tr, not read -d: dash has no read -d.
	# A loop, not xargs -I: BSD xargs gives up when the command with the path in it passes 255 bytes.
	big=$(git diff --cached --name-only -z --diff-filter=AM "$base" | tr '\0' '\n' |
		while IFS= read -r f; do
			s=$(git cat-file -s ":$f")
			if [ "$s" -gt "$limit" ]; then echo "$s $f"; fi
		done)
else
	empty=$(git hash-object -t tree /dev/null)
	binaries=$(git diff --numstat "$empty" HEAD | awk '$1 == "-" { print $3 }')
	big=$(git ls-tree -r -l HEAD | awk -v l="$limit" '$4 > l { print $4, $5 }')
fi

bad=$(printf '%s\n' "$binaries" | grep -Ev "$allowed" | grep . || true)
# Names that are never ours to track: secrets, credentials and profiles.
if [ "$mode" = staged ]; then
	names=$(git diff --cached --name-only -z --diff-filter=AM "$base" | tr '\0' '\n')
else
	names=$(git ls-tree -r --name-only HEAD)
fi
never=$(printf '%s\n' "$names" | grep -E '(^|/)(\.env(\.[^/]*)?|hosts\.yml|id_(rsa|ed25519|ecdsa)[^/]*|[^/]*\.(pem|key|p12|pfx|pprof|prof|pyc))$' || true)
never=$(printf '%s\n' "$never" | grep -Ev '(^|/)\.env\.example$' | grep . || true)
status=0
if [ -n "$never" ]; then
	echo "Files that must never be committed (secrets, credentials, profiles):"
	echo "$never"
	status=1
fi
if [ -n "$big" ]; then
	echo "Files over 1 MB (bytes, path):"
	echo "$big"
	status=1
fi
if [ -n "$bad" ]; then
	echo "Binary files that are not images or fonts (a program built in the checkout?):"
	echo "$bad"
	status=1
fi
if [ "$status" -ne 0 ]; then
	echo "Remove them with: git rm --cached <path>. Build programs with go build -o outside the checkout."
fi
exit "$status"
