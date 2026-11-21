# Private Real-Diff Fixtures (Local-Only)

This document describes the fixture format for running `donegate`'s Level 1 verification rules against real commits from private repositories without committing any private source code to this public repository.

All private fixtures live under `testdata/private/`, which is ignored by git (`/testdata/private/` in `.gitignore`). When `testdata/private/` does not exist (for example, in CI or a fresh clone), `TestPrivateFixtures` in `internal/verify/private_fixtures_test.go` automatically skips.

## Fixture Format

Unlike the public cheat zoo (`testdata/fixtures/`), which layers every fixture onto a single shared `_base/`, each private fixture carries its own self-contained `base/` snapshot of the repository state immediately before the commit:

```text
testdata/private/
  <fixture-name>/
    base/              # repo-relative tree before the change (at <commit>^)
    overlay/           # OPTIONAL if delete.txt is present: complete files added or modified by the commit
    delete.txt         # OPTIONAL: one repo-relative path per line deleted by the commit
    expected.json      # expected rule findings and provenance metadata
```

For each fixture directory under `testdata/private/` (ignoring directories starting with `_` or `.`), `TestPrivateFixtures`:

1. Copies `base/` into a temporary git repository and creates the base commit.
2. Copies all files from `overlay/` (if present) over the repository root and removes any paths listed in `delete.txt` (if present).
3. Commits the resulting changes as the head commit.
4. Runs `verify.Run` with `MaxLevel: 1` against the base commit and compares the `(rule, path)` finding set against `expected.json` (skipping expected rules that are not registered).

### `expected.json` Schema

Uses the same schema as `testdata/fixtures/`, plus a `"source"` field recording the short commit SHA:

```json
{
  "description": "commit subject line",
  "kind": "honest",
  "source": "a1b2c3d",
  "findings": []
}
```

- `description`: summary of the commit (typically the git commit subject).
- `kind`: `"honest"` or `"cheat"`.
- `source`: short git commit SHA from the source repository.
- `findings`: list of `{ "rule", "path" }` objects. Honest fixtures have `"findings": []`. Each `(rule, path)` pair is listed once, using the repo-relative path (new path for renames).

## Generating Fixtures

Use `scripts/fixture-from-commit.sh` (read-only against the source repo) to extract a fixture from a git commit:

```sh
scripts/fixture-from-commit.sh /path/to/private-repo <commit-sha> testdata/private/real-01-short-slug
```

The script populates `base/` from `<commit>^` with:
- every file modified or deleted by the commit,
- every file matching `donegate`'s default protected globs (tests, snapshots, test/lint/TS/workflow config), and
- every `package.json` file,

so rules that inspect the base tree (`S001`, `S003`, `C001`) see realistic repository context. Binary files are skipped.

## Running

```sh
go test -count=1 -run TestPrivateFixtures -v ./internal/verify/
```
