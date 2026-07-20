#!/usr/bin/env bash
# Fetch a prebuilt reeve binary so the GitHub Action can skip the source
# build. Called by action.yml on a cache miss; never fails the job - every
# error path logs why and reports fetched=false so the action falls back to
# building from source.
#
# Ref semantics (github.action_ref):
#   vX.Y.Z        -> that release's goreleaser tarball, verified against the
#                    release's checksums.txt (signed release pipeline).
#   master | next -> the rolling edge-<branch> prerelease asset whose name
#                    embeds the source hash the action just computed. A name
#                    match proves the binary was built from exactly the
#                    checked-out action source; no match (edge build still
#                    running, or an older build) falls back to source.
#   anything else -> source build (SHA pins, feature branches, forks).
#
# Inputs (env):
#   REEVE_REF      github.action_ref       (may be empty, e.g. local runs)
#   REEVE_REPO     github.action_repository ("owner/repo"; empty on some runners)
#   REEVE_SRCHASH  output of source-hash.sh over the action tree
#   REEVE_OS       runner.os   (Linux, macOS, Windows)
#   REEVE_ARCH     runner.arch (X64, ARM64, ...)
#   REEVE_DEST     where to place the binary (the cached path)
#   GH_TOKEN       token for gh (dodges rate limits; repo is public)
#
# Output: fetched=true|false appended to $GITHUB_OUTPUT (stdout if unset).
set -euo pipefail

# classify_ref <ref> -> version | edge | other
classify_ref() {
  local ref="${1:-}"
  if [[ "$ref" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    echo version
  elif [[ "$ref" == "master" || "$ref" == "next" ]]; then
    echo edge
  else
    echo other
  fi
}

# map_arch <runner.arch> -> amd64 | arm64 | "" (unsupported)
map_arch() {
  case "${1:-}" in
    X64) echo amd64 ;;
    ARM64) echo arm64 ;;
    *) echo "" ;;
  esac
}

# verify_sha256 <file> <checksums-file>
# Checks <file> against its (basename-keyed) line in a sha256sum-format
# checksums file. Returns non-zero on a missing entry or a mismatch.
verify_sha256() {
  local file="$1" sums="$2" base expected actual
  base=$(basename "$file")
  expected=$(awk -v f="$base" '$2 == f || $2 == "*" f {print $1; exit}' "$sums")
  if [[ -z "$expected" ]]; then
    echo "no checksum entry for $base in $(basename "$sums")" >&2
    return 1
  fi
  actual=$(sha256sum "$file" | cut -d' ' -f1)
  if [[ "$expected" != "$actual" ]]; then
    echo "checksum mismatch for $base: expected $expected, got $actual" >&2
    return 1
  fi
}

fetch_main() {
  local out="${GITHUB_OUTPUT:-/dev/stdout}"
  local ref="${REEVE_REF:-}" repo="${REEVE_REPO:-}" srchash="${REEVE_SRCHASH:-}"
  local dest="${REEVE_DEST:?REEVE_DEST is required}"
  local arch kind workdir asset sums ok=false

  fallback() {
    echo "$1 - falling back to source build"
    echo "fetched=false" >> "$out"
  }

  # Never hardcode the repo: forks and local runs (empty contexts) degrade
  # gracefully to the source build.
  if [[ -z "$ref" || -z "$repo" ]]; then
    fallback "action ref/repository not available"
    return 0
  fi
  if [[ "${REEVE_OS:-}" != "Linux" ]]; then
    fallback "prebuilt binaries are only published for Linux (runner.os=${REEVE_OS:-unset})"
    return 0
  fi
  arch=$(map_arch "${REEVE_ARCH:-}")
  if [[ -z "$arch" ]]; then
    fallback "unsupported runner.arch '${REEVE_ARCH:-unset}'"
    return 0
  fi
  if ! command -v gh > /dev/null 2>&1; then
    fallback "gh CLI not available"
    return 0
  fi

  kind=$(classify_ref "$ref")
  if [[ "$kind" == "other" ]]; then
    fallback "ref '$ref' is not a release tag or edge branch"
    return 0
  fi

  workdir=$(mktemp -d)
  # shellcheck disable=SC2064
  trap "rm -rf '$workdir'" EXIT

  case "$kind" in
    version)
      asset="reeve_${ref#v}_linux_${arch}.tar.gz"
      sums="checksums.txt"
      echo "Pinned release $ref: downloading $asset from $repo"
      if gh release download "$ref" --repo "$repo" \
        --pattern "$asset" --pattern "$sums" --dir "$workdir"; then
        ok=true
      else
        echo "download failed (missing release/asset, or network error)" >&2
      fi
      ;;
    edge)
      if [[ -z "$srchash" ]]; then
        fallback "source hash not available for edge lookup"
        return 0
      fi
      asset="reeve_edge_linux_${arch}_${srchash}.tar.gz"
      sums="checksums_${srchash}.txt"
      echo "Edge ref '$ref': looking for source-hash-matched asset $asset on edge-$ref"
      if gh release download "edge-$ref" --repo "$repo" \
        --pattern "$asset" --pattern "$sums" --dir "$workdir"; then
        ok=true
      else
        echo "no edge binary matching source hash $srchash (edge build may still be running)" >&2
      fi
      ;;
  esac

  if [[ "$ok" == true ]]; then
    if ! verify_sha256 "$workdir/$asset" "$workdir/$sums"; then
      ok=false
    elif ! tar -xzf "$workdir/$asset" -C "$workdir" reeve; then
      echo "could not extract reeve from $asset" >&2
      ok=false
    fi
  fi

  if [[ "$ok" == true ]]; then
    mkdir -p "$(dirname "$dest")"
    install -m 0755 "$workdir/reeve" "$dest"
    # Sanity-run the binary; a broken download must not poison the cache.
    if ! "$dest" --version > /dev/null 2>&1; then
      rm -f "$dest"
      fallback "downloaded binary failed to execute"
      return 0
    fi
    echo "Using prebuilt binary: $("$dest" --version)"
    echo "fetched=true" >> "$out"
    return 0
  fi

  fallback "prebuilt fetch failed"
  return 0
}

# Run only when executed, so tests can source the helper functions.
if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  fetch_main
fi
