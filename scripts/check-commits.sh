#!/usr/bin/env bash
# Check every commit message in a git range with the commit-msg hook (PRD-001 REL-01).
# req: REL-01
# req: CTL-CHG-10
# Usage: scripts/check-commits.sh <range>     e.g. origin/main..HEAD
set -euo pipefail
range="${1:?usage: check-commits.sh <git range>}"
here="$(cd "$(dirname "$0")" && pwd)"
tmp="$(mktemp)"; trap 'rm -f "$tmp"' EXIT
fail=0
while read -r sha; do
  git log -1 --format=%B "$sha" > "$tmp"
  if ! "$here/hooks/commit-msg" "$tmp" 2>/dev/null; then
    echo "✖ $(git log -1 --format='%h %s' "$sha")" >&2
    fail=1
  fi
done < <(git rev-list --no-merges "$range")
if [ "$fail" -ne 0 ]; then
  echo "Some commits are not Conventional Commits (see AGENTS.md § Commits)." >&2
  exit 1
fi
echo "✔ All commits in $range follow Conventional Commits"
