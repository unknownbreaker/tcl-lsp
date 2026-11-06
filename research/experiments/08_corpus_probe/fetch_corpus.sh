#!/usr/bin/env bash
# Clone the 10 corpus repos into <dir> at their pinned commits.
#
# Usage: fetch_corpus.sh <dir>
#
# Each repo is fetched by full SHA, so upstream movement cannot change the
# baseline. Re-running is safe: a checkout already at its pinned commit is
# skipped, and one at any other commit is an error rather than being moved.
# Nothing in the corpus is executed.
set -euo pipefail

readonly BASE_URL="https://github.com/flightaware"

if [ "$#" -ne 1 ]; then
  echo "usage: $0 <dir>" >&2
  exit 2
fi
corpus_dir=$1

fetch_repo() {
  local name=$1
  local sha=$2
  local dest="$corpus_dir/$name"
  local head

  if [ -e "$dest" ]; then
    head=$(git -C "$dest" rev-parse HEAD 2>/dev/null || true)
    if [ "$head" = "$sha" ]; then
      echo "$name: already at $sha"
      return 0
    fi
    echo "$name: $dest exists but is at '${head:-not a git checkout}', want $sha" >&2
    return 1
  fi

  git init --quiet "$dest"
  git -C "$dest" remote add origin "$BASE_URL/$name"
  git -C "$dest" fetch --quiet --depth 1 origin "$sha"
  git -C "$dest" -c advice.detachedHead=false checkout --quiet --detach FETCH_HEAD

  head=$(git -C "$dest" rev-parse HEAD)
  if [ "$head" != "$sha" ]; then
    echo "$name: checked out $head, want $sha" >&2
    return 1
  fi
  echo "$name: $sha"
}

mkdir -p "$corpus_dir"

fetch_repo piaware        8480286b449e88d8446bc56068ef80eb2d8c8c82
fetch_repo piaware-gui    a12dbc1cb8e4369f5c5800930d12ccac6147ceea
fetch_repo prometheus-tcl 4ecc609dc51e8a17f098adfb5f45cc76b69f4157
fetch_repo speedtables    0fe25e1569c936e2a1bb477b7afecb8a74aad976
fetch_repo tcl-jira-api   2dd47714611b44e235b87fd8069d3d3e77f416cd
fetch_repo TclProDebug    fb1dc77690d6386b4df2b19d27363a90a9d39a19
fetch_repo tclrmq         15318be735d0f757f14dd711aa6ffc31d74de22b
fetch_repo tsp            dad8752cc29950af9f5dd04188e5c7b2eb4ab2e2
fetch_repo turtles        ca942b32e4d10dbb07b9ca9cf967e115cf32d869
fetch_repo yajl-tcl       fa5da41df4f825b3e604187a423f9afbf2f8f2d2
