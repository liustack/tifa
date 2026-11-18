#!/bin/sh
# Fails when the repository holds a file that should not be committed: a file over
# 1 MB, or a binary file that is not an image or a font, such as a program built in
# the checkout. With --staged it checks what the next commit would hold (the
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
	binaries=$(git diff --cached --numstat --diff-filter=AM "$base" | awk '$1 == "-" { print $3 }')
	big=$(git diff --cached --name-only --diff-filter=AM -z "$base" |
		xargs -0 -I{} sh -c 's=$(git cat-file -s ":{}"); [ "$s" -gt '"$limit"' ] && echo "$s {}" || true')
else
	empty=$(git hash-object -t tree /dev/null)
	binaries=$(git diff --numstat "$empty" HEAD | awk '$1 == "-" { print $3 }')
	big=$(git ls-tree -r -l HEAD | awk -v l="$limit" '$4 > l { print $4, $5 }')
fi

bad=$(printf '%s\n' "$binaries" | grep -Ev "$allowed" | grep . || true)
status=0
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
