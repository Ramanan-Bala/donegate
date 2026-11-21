#!/usr/bin/env bash
set -euo pipefail

usage() {
  echo "Usage: $0 <repo> <commit> <dest-dir>" >&2
  exit 1
}

if [[ $# -ne 3 ]]; then
  usage
fi

repo="$1"
commit="$2"
dest_dir="$3"

if [[ ! -d "$repo" ]]; then
  echo "error: repo directory does not exist: $repo" >&2
  exit 1
fi

if [[ -e "$dest_dir" ]]; then
  echo "error: destination already exists: $dest_dir" >&2
  exit 1
fi

commit_sha="$(git -C "$repo" rev-parse --verify "${commit}^{commit}")"
parent_sha="$(git -C "$repo" rev-parse --verify "${commit_sha}^")"
short_sha="$(git -C "$repo" rev-parse --short "$commit_sha")"
subject="$(git -C "$repo" show -s --format=%s "$commit_sha")"

work_tmp="$(mktemp -d "${TMPDIR:-/tmp}/fixture-from-commit.XXXXXX")"
cleanup() {
  rm -f "$work_tmp/blob" "$work_tmp/mod-base" "$work_tmp/mod-head" "$work_tmp/deleted.txt"
  rmdir "$work_tmp" 2>/dev/null || true
}
trap cleanup EXIT

is_regular_mode() {
  case "$1" in
    100644|100755) return 0 ;;
    *) return 1 ;;
  esac
}

is_protected_or_pkg() {
  local p="$1"
  local base="${p##*/}"

  case "$base" in
    *.test.*|*.spec.*|jest.config.*|vitest.config.*|tsconfig*.json|.eslintrc*|eslint.config.*|package.json)
      return 0
      ;;
  esac

  case "/$p/" in
    */__tests__/*|*/__snapshots__/*)
      return 0
      ;;
  esac

  case "$p" in
    e2e/*|.github/workflows/*)
      return 0
      ;;
  esac

  return 1
}

is_binary_file() {
  local file="$1"
  local nul_count
  nul_count="$(dd bs=8192 count=1 < "$file" 2>/dev/null | LC_ALL=C tr -cd '\000' | wc -c)"
  [[ "${nul_count// /}" -gt 0 ]]
}

# Extracts <rev>:<path> to <out_path> if it is a non-binary blob.
# Returns 0 if written, 1 if skipped (binary or missing).
extract_text_blob() {
  local rev="$1"
  local rel="$2"
  local out="$3"
  local tmp_file="$work_tmp/blob"

  if ! git -C "$repo" show "${rev}:${rel}" > "$tmp_file" 2>/dev/null; then
    rm -f "$tmp_file"
    return 1
  fi

  if is_binary_file "$tmp_file"; then
    rm -f "$tmp_file"
    return 1
  fi

  mkdir -p "$(dirname "$out")"
  mv "$tmp_file" "$out"
  return 0
}

json_escape() {
  awk 'BEGIN {
    s = ARGV[1]
    gsub(/\\/, "\\\\", s)
    gsub(/"/, "\\\"", s)
    gsub(/\t/, "\\t", s)
    gsub(/\r/, "\\r", s)
    gsub(/\n/, "\\n", s)
    printf "%s", s
  }' "$1"
}

mkdir -p "$dest_dir/base"

# 1. Populate base/ with protected test/config files and package.json at <commit>^.
while IFS=$'\t' read -r meta rel; do
  mode="${meta%% *}"
  if ! is_regular_mode "$mode"; then
    continue
  fi
  if is_protected_or_pkg "$rel"; then
    extract_text_blob "$parent_sha" "$rel" "$dest_dir/base/$rel" || true
  fi
done < <(git -C "$repo" -c core.quotepath=false ls-tree -r "$parent_sha")

# 2. Process files changed by <commit> relative to <commit>^ (renames as delete + add).
deleted_tmp="$work_tmp/deleted.txt"
: > "$deleted_tmp"

while IFS=$'\t' read -r meta rel; do
  [[ -z "$meta" ]] && continue
  # Format of meta: :<old_mode> <new_mode> <old_sha> <new_sha> <status>
  meta="${meta#:}"
  read -r old_mode new_mode _old_sha _new_sha status <<< "$meta"

  case "$status" in
    A)
      if is_regular_mode "$new_mode"; then
        extract_text_blob "$commit_sha" "$rel" "$dest_dir/overlay/$rel" || true
      fi
      ;;
    M)
      if is_regular_mode "$old_mode" && is_regular_mode "$new_mode"; then
        tmp_base="$work_tmp/mod-base"
        tmp_head="$work_tmp/mod-head"
        if git -C "$repo" show "${parent_sha}:${rel}" > "$tmp_base" 2>/dev/null \
          && git -C "$repo" show "${commit_sha}:${rel}" > "$tmp_head" 2>/dev/null \
          && ! is_binary_file "$tmp_base" \
          && ! is_binary_file "$tmp_head"; then
          mkdir -p "$(dirname "$dest_dir/base/$rel")"
          mv "$tmp_base" "$dest_dir/base/$rel"
          mkdir -p "$(dirname "$dest_dir/overlay/$rel")"
          mv "$tmp_head" "$dest_dir/overlay/$rel"
        else
          rm -f "$tmp_base" "$tmp_head" "$dest_dir/base/$rel"
        fi
      fi
      ;;
    D)
      if is_regular_mode "$old_mode"; then
        if extract_text_blob "$parent_sha" "$rel" "$dest_dir/base/$rel"; then
          printf '%s\n' "$rel" >> "$deleted_tmp"
        fi
      fi
      ;;
    *)
      ;;
  esac
done < <(git -C "$repo" -c core.quotepath=false diff --no-renames --raw "$parent_sha" "$commit_sha")

if [[ -s "$deleted_tmp" ]]; then
  LC_ALL=C sort -u "$deleted_tmp" > "$dest_dir/delete.txt"
fi

escaped_subject="$(json_escape "$subject")"
escaped_sha="$(json_escape "$short_sha")"

cat > "$dest_dir/expected.json" <<EOF
{
  "description": "${escaped_subject}",
  "kind": "honest",
  "source": "${escaped_sha}",
  "findings": []
}
EOF
