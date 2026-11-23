#!/usr/bin/env bash
# List docs/ with the summary and read_when from each file's front matter.
#
#   scripts/docs-list.sh           print the index
#   scripts/docs-list.sh --check   also fail when a doc has no summary or no audience
#   scripts/docs-list.sh --check-translations
#                                  fail when the Chinese partners of the app docs are missing or stale
#
# Front matter format:
#
#   ---
#   summary: 'One line saying what the doc answers'
#   audience: app
#   read_when:
#     - Situation in which an agent should read it
#   ---
#
# audience says who the doc is for: app (writing, running or deploying an app) or dev (developing
# Tifa itself). `tifa docs` lists the app docs by default and the dev docs with --all.
#
# Docs are in section folders (docs/start, docs/internals and so on). A section folder has an index.md
# with its title and order. It is not listed and holds no summary.
#
# A doc with audience: app has a Chinese partner beside it, docs/<section>/<name>.zh-CN.md, with the same
# front matter keys and one more: translatedFrom, the value `scripts/doc-hash.sh docs/<path>/<name>.md`
# prints. A change to the English file changes that hash, so the check fails until the Chinese file is
# updated and carries the new hash. A doc with audience: dev has no Chinese partner. The index lists
# the English files only.
#
# read_when also accepts the inline form: read_when: [a, b]
#
# This script reads the lines with awk and does not parse YAML. A read_when item that starts with a
# backtick is not valid YAML, so quote it. TestDocFrontMatterIsValidYAML in cmd/tifa parses every doc.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
docs="$root/docs"
check=false
translations=false
case "${1:-}" in
  "") ;;
  --check) check=true ;;
  --check-translations) translations=true ;;
  *) echo "usage: $0 [--check|--check-translations]" >&2; exit 2 ;;
esac

if [ ! -d "$docs" ]; then
  echo "no docs/ directory at $root" >&2
  exit 1
fi

# Research notes are not part of the index.
files="$(cd "$root" && find docs -type f -name '*.md' -not -path 'docs/research/*' -not -name '*.zh-CN.md' -not -name index.md | LC_ALL=C sort)"

# fm_key prints the value of a key of the front matter of a file.
fm_key() {
  awk -v key="$2" 'NR == 1 { next } $0 == "---" { exit } index($0, key ":") == 1 { sub(/^[^:]*:[ \t]*/, ""); gsub(/^["\x27]|["\x27]$/, ""); print; exit }' "$1"
}

if $translations; then
  bad=0
  problem() { echo "$1" >&2; bad=$((bad + 1)); }
  all="$(cd "$root" && find docs -type f -name '*.md' -not -path 'docs/research/*' | LC_ALL=C sort)"
  while IFS= read -r file; do
    [ -n "$file" ] || continue
    case "$file" in
      *.zh-CN.md)
        en="${file%.zh-CN.md}.md"
        if [ ! -f "$root/$en" ]; then
          problem "$file has no English file $en. Delete it or move it beside its English file."
        elif [ "$(fm_key "$root/$en" audience)" != app ]; then
          problem "$file: $en is for audience dev and stays English only. Delete the Chinese file."
        else
          want="$("$root/scripts/doc-hash.sh" "$root/$en")"
          have="$(fm_key "$root/$file" translatedFrom)"
          if [ "$have" != "$want" ]; then
            problem "$file: translatedFrom is ${have:-missing} but $en hashes to $want. The English changed: update the Chinese in the same commit, then set translatedFrom to $want (scripts/doc-hash.sh $en)."
          fi
        fi
        ;;
      *)
        if [ "$(fm_key "$root/$file" audience)" = app ] && [ ! -f "$root/${file%.md}.zh-CN.md" ]; then
          problem "$file has no Chinese partner ${file%.md}.zh-CN.md. Write the Chinese in the same commit as the English."
        fi
        ;;
    esac
  done <<LIST
$all
LIST
  if [ "$bad" -gt 0 ]; then
    echo "$bad problem(s) with the Chinese docs. Update the Chinese in the same commit as the English." >&2
    exit 1
  fi
  exit 0
fi

missing=0
listed=0
while IFS= read -r file; do
  [ -n "$file" ] || continue
  out="$(awk -v path="$file" '
    function unquote(s) {
      sub(/^[ \t]+/, "", s); sub(/[ \t]+$/, "", s)
      if (s ~ /^'\''.*'\''$/ || s ~ /^".*"$/) s = substr(s, 2, length(s) - 2)
      return s
    }
    NR == 1 { if ($0 != "---") exit; inside = 1; next }
    inside && $0 == "---" { closed = 1; exit }
    inside && /^summary:/ { sub(/^summary:/, ""); summary = unquote($0); list = 0; next }
    inside && /^read_when:/ {
      rest = $0; sub(/^read_when:/, "", rest); rest = unquote(rest)
      if (rest ~ /^\[.*\]$/) {
        rest = substr(rest, 2, length(rest) - 2)
        n = split(rest, items, ",")
        for (i = 1; i <= n; i++) { item = unquote(items[i]); if (item != "") when = when (when == "" ? "" : " | ") item }
        list = 0
      } else list = 1
      next
    }
    inside && list && /^[ \t]+-[ \t]+/ {
      item = $0; sub(/^[ \t]+-[ \t]+/, "", item); item = unquote(item)
      when = when (when == "" ? "" : " | ") item
      next
    }
    inside && /^[^ \t]/ { list = 0 }
    END {
      if (!closed || summary == "") exit 1
      print path " - " summary
      if (when != "") print "  Read when: " when
    }
  ' "$root/$file")" || {
    missing=$((missing + 1))
    echo "warning: $file has no front matter summary" >&2
    continue
  }
  echo "$out"
  listed=$((listed + 1))
  if $check; then
    audience="$(awk 'NR == 1 { next } $0 == "---" { exit } /^audience:/ { sub(/^audience:[ \t]*/, ""); print; exit }' "$root/$file")"
    case "$audience" in
      app|dev) ;;
      *) echo "warning: $file needs audience: app or audience: dev in its front matter" >&2; missing=$((missing + 1)) ;;
    esac
  fi
done <<EOF
$files
EOF

if [ "$listed" -eq 0 ]; then
  echo "No docs with front matter found in docs/"
fi
echo
echo 'Reminder: before coding, check the "Read when" hints above and read the docs that apply.'

if $check && [ "$missing" -gt 0 ]; then
  exit 1
fi
