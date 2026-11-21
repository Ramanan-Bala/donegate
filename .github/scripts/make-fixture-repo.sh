#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -ne 2 ]; then
  echo "usage: $0 <fixture> <dest>" >&2
  exit 2
fi

fixture="$1"
dest="$2"

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/../.." && pwd)"
fixtures_dir="$repo_root/testdata/fixtures"
base_dir="$fixtures_dir/_base"
fixture_dir="$fixtures_dir/$fixture"

if [ ! -d "$base_dir" ]; then
  echo "error: base fixture directory not found: $base_dir" >&2
  exit 1
fi

if [ ! -d "$fixture_dir" ]; then
  echo "error: fixture directory not found: $fixture_dir" >&2
  exit 1
fi

mkdir -p "$dest"
cp -R "$base_dir/." "$dest/"

git -C "$dest" -c init.defaultBranch=main init -q
git -C "$dest" add -A
git -C "$dest" \
  -c user.name=ci \
  -c user.email=ci@example.com \
  -c commit.gpgsign=false \
  commit -q -m "base"

if [ -d "$fixture_dir/overlay" ]; then
  cp -R "$fixture_dir/overlay/." "$dest/"
fi

if [ -f "$fixture_dir/delete.txt" ]; then
  while IFS= read -r line || [ -n "$line" ]; do
    rel="${line#"${line%%[![:space:]]*}"}"
    rel="${rel%"${rel##*[![:space:]]}"}"
    if [ -z "$rel" ]; then
      continue
    fi
    rm -f "$dest/$rel"
  done < "$fixture_dir/delete.txt"
fi

git -C "$dest" add -A
git -C "$dest" \
  -c user.name=ci \
  -c user.email=ci@example.com \
  -c commit.gpgsign=false \
  commit -q -m "fixture: $fixture"
