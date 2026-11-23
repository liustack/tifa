#!/usr/bin/env bash
# Print the value for `translatedFrom` in the front matter of a Chinese doc: the first 12 hex
# digits of the SHA-256 of the English file it translates.
#
#   scripts/doc-hash.sh docs/start/app.md
#
# Run it after the last edit of the English file, and put the result in docs/app.zh-CN.md.
set -euo pipefail

if [ "$#" -ne 1 ] || [ ! -f "$1" ]; then
  echo "usage: $0 <english doc>" >&2
  exit 2
fi
if command -v sha256sum >/dev/null 2>&1; then
  sum="$(sha256sum "$1")"
else
  sum="$(shasum -a 256 "$1")"
fi
echo "${sum:0:12}"
