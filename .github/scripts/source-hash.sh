#!/usr/bin/env bash
# Canonical reeve source hash - the single definition shared by action.yml
# (binary cache key + edge-asset match) and .github/workflows/edge-build.yml
# (edge-asset naming), so the two can never drift.
#
# Hashes the content AND repo-relative path of every *.go / go.mod / go.sum
# file. Paths are made relative (cd + find .) and the sort is pinned to the
# C locale so the same tree yields the same hash no matter where it is
# checked out (the action tree lives under _actions/<owner>/<repo>/<ref>,
# the edge build under the normal workspace).
#
# Usage: source-hash.sh [root]   (root defaults to the current directory)
set -euo pipefail

ROOT="${1:-.}"
cd "$ROOT"
find . \( -name '*.go' -o -name 'go.mod' -o -name 'go.sum' \) -type f \
  -exec sha256sum {} + | LC_ALL=C sort | sha256sum | cut -d' ' -f1
