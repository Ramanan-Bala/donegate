<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="assets/logo-dark.svg">
    <img alt="donegate" src="assets/logo.svg" width="360">
  </picture>
</p>

<p align="center"><strong>Let cheap models implement, but make them prove it.</strong></p>

<p align="center">
  <a href="https://github.com/Ramanan-Bala/donegate/actions/workflows/ci.yml"><img alt="CI" src="https://github.com/Ramanan-Bala/donegate/actions/workflows/ci.yml/badge.svg"></a>
  <a href="LICENSE"><img alt="MIT license" src="https://img.shields.io/badge/license-MIT-16a34a"></a>
  <img alt="Go 1.27+" src="https://img.shields.io/badge/go-1.27%2B-0f172a">
</p>

donegate is a verification gate for AI coding agents. It stops an agent from declaring "done" until the work is actually proven: it makes the judge (tests, snapshots, test/lint config, hooks) read-only, then re-checks the agent's claim every time it tries to stop, using rules on the diff and a test run donegate controls itself. Every judged stop leaves a hashed evidence report you can attach to a PR.

```
donegate: not done (2 failures, attempt 1/5)
- T001 src/__tests__/cart.test.ts was modified. Tests are read-only; revert it and fix the source under test instead.
- S002 test environment check detected in src/cart.ts:18; remove test-environment branching so production and test code share the same behaviour.
Fix these and stop again; donegate re-checks every stop.
```

## Why

Agents pass the check without doing the task: they edit the failing test, add `.only`/`.skip`, rewrite snapshots with `-u`, loosen the test config, special-case the test input, or branch on `NODE_ENV === 'test'`. Telling them not to barely helps, and a gate the agent can edit is not a gate (agents have been seen rewriting their own `done-gate.js`).

donegate's core rule: **the judge lives outside anything the agent can write.** Policy and baselines are snapshotted to `~/.donegate/` when the session starts; changes the agent makes to them mid-session are ignored and flagged.

See [`docs/SPEC.md`](docs/SPEC.md) for the full design, prior art and evidence.

## How it works

One Go binary does all the judging. Each harness forwards three moments to it:

| Moment | Command | What happens |
| --- | --- | --- |
| Session start | `donegate session start` | Records a baseline commit of the working tree (including uncommitted and untracked files, without touching your index or branches), hashes every protected file and snapshots `.donegate.yml` into `~/.donegate/sessions/<id>/`. |
| Every edit | `donegate guard` | Denies writes to existing tests, snapshots, test/lint config, `.donegate.yml`, donegate state and harness hook config, with a reason the agent can act on. Also catches shell writes (`sed -i`, redirects, `mv`, `rm`, `git checkout --`, ...) and snapshot updates (`vitest -u`, `jest --updateSnapshot`). New test files are allowed. |
| Every stop | `donegate gate` | Silent if nothing changed. Otherwise runs the checks for the configured tier and blocks with a short, fixable list, up to `max_blocks` attempts. After that it lets the agent stop, marks the evidence `FAILED` and tells you. |
| CI / anywhere | `donegate verify --base origin/main` | Same checks from a clean state; exit code fails the build. This is the real backstop: CI runs on a machine the agent never touched. |

Shell parsing in the guard is best-effort; the hash check at the gate catches whatever slips through.

## Checks

Checks run cheapest first, and a gate stops at the first level that fails.

| ID | Level | Catches | Default |
| --- | --- | --- | --- |
| H001 | L1 | A protected file or the policy snapshot changed since the session started | block |
| T001 | L1 | An existing test file was modified | block |
| T002 | L1 | A test file or test case was deleted | block |
| T003 | L1 | `.only`, `.skip`, `xit`, `it.todo`, `test.fails` added | block |
| T004 | L1 | Snapshot file or inline snapshot changed | block |
| C001 | L1 | Test/lint/CI config loosened (`passWithNoTests`, `\|\| true`, `strict: false`, rules turned off, new ignores, lower coverage) | block |
| S001 | L1 | Literals from test fixtures hardcoded in new source lines | warn |
| S002 | L1 | Source sniffs the test environment (`NODE_ENV === 'test'`, `JEST_WORKER_ID`, `VITEST`) | block |
| S003 | L1 | `process.exit()` added, or test setup files changed | block |
| R001 | L2 | Tests fail when donegate runs them in a clean worktree with protected files restored | block |
| R002 | L2 | Test count dropped vs the baseline run, or zero tests ran | block |
| R003 | L2 | Typecheck or lint fails | block |

Tiers: `fast` = L1 · `standard` = L1 + L2 (default for the Stop gate) · `strict` = everything (default for `verify`). Every rule's severity can be overridden in `.donegate.yml`.

L2 runs each configured project's commands in a throwaway git worktree that contains the agent's changes but with every protected file restored to its baseline content, so a rigged test or config can't make the run pass. Results are read from Vitest/Jest JSON or JUnit XML (pytest, `go-junit-report`, .NET loggers, ...), falling back to the exit code. `node_modules` is symlinked in from your repo.

## Install

Requires Go 1.27+ and git.

```sh
go install github.com/Ramanan-Bala/donegate/cmd/donegate@latest
```

Or from a clone:

```sh
git clone https://github.com/Ramanan-Bala/donegate && cd donegate
go build -o ~/.local/bin/donegate ./cmd/donegate
```

### Claude Code

```sh
donegate install claude --dry-run   # preview the merged settings
donegate install claude             # writes ~/.claude/settings.json
```

This adds user-level `SessionStart`, `PreToolUse`, `Stop` and `SubagentStop` hooks. Your existing settings and hooks are preserved, the original file is backed up once to `settings.json.donegate.bak`, and re-running is a no-op. Hooks live at user level, never in the repo, so a repo edit can't remove them. The `PreToolUse` hook fires before the permission check, so it holds even under `--dangerously-skip-permissions`.

Note that this applies to **every** Claude Code session in every git repo. To remove it:

```sh
donegate uninstall claude
```

### CI

Add the GitHub Action to verify pull requests, write the report to the job summary, and post or update a single PR comment:

```yaml
name: donegate

on:
  pull_request:

permissions:
  contents: read
  pull-requests: write

jobs:
  verify:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0
      - uses: Ramanan-Bala/donegate@main
```

`fetch-depth: 0` gives `donegate verify` full history against the PR base branch (without it, the action fetches the base ref automatically). Inputs: `base`, `tier` (default `strict`), `working-directory`, `comment` (default `true`), `github-token`, and `go-version`. Outputs: `verdict` (`PASS` or `FAIL`) and `evidence` (path to `evidence.json`).

On any other CI provider, run the CLI directly:

```sh
donegate verify --base origin/main            # strict tier by default
donegate verify --base origin/main --json     # machine-readable result
```

Exit codes: `0` pass, `1` fail, `2` usage or runtime error.

## Configuration

donegate works without config, using JS/TS defaults. Add a `.donegate.yml` at the repo root to enable L2 and tune rules:

```yaml
version: 1
projects:              # monorepo-aware: one entry per package
  - name: api
    root: backend
    test: npm test -- --json --outputFile={json}
    typecheck: npx tsc --noEmit
    lint: npm run lint
  - name: web
    root: frontend
    test: npx vitest run --reporter=json --outputFile={json}
    typecheck: npx tsc --noEmit
protect:
  tests: ["**/*.test.*", "**/*.spec.*", "**/__tests__/**", "**/__snapshots__/**", "e2e/**"]
  config: ["**/jest.config.*", "**/vitest.config.*", "**/tsconfig*.json", "**/.eslintrc*", "**/eslint.config.*", ".github/workflows/**"]
  allow_new_tests: true   # agents may add test files, not edit existing ones
gate:
  tier: standard          # fast | standard | strict
  max_blocks: 5           # below Claude Code's ~8 consecutive-block cap
  timeout_seconds: 300
rules:                    # override any rule's severity: off | warn | block
  S001: block
```

`{json}` is replaced with a temp file path that donegate reads the report from. The repo copy of `.donegate.yml` is only a proposal: the active policy is always the snapshot taken at session start (and, for `verify`, the copy in the base commit), so edits made mid-session do nothing and trip H001.

donegate dogfoods itself; see this repo's [`.donegate.yml`](.donegate.yml) for a Go example.

## Evidence

Every judged stop writes `evidence.json` and `evidence.md` to `~/.donegate/sessions/<id>/evidence/`:

```json
{
  "schema": "donegate/evidence@1",
  "session": "20261121T144011Z-f78fd7a9",
  "harness": "claude-code",
  "base": "a1b2c3d...",
  "tier": "standard",
  "verdict": "PASS",
  "blocks": 2,
  "checks": [{ "id": "T001", "status": "pass" }, { "id": "S001", "status": "warn", "detail": "..." }],
  "policy_sha256": "...",
  "diff_sha256": "...",
  "report_sha256": "..."
}
```

`report_sha256` detects accidental edits; signing with a key the agent can't read is on the roadmap.

## Tamper resistance, honestly

The agent runs as your user, so it can technically write to `~/.donegate/` and `~/.claude/settings.json`. donegate defends in layers rather than pretending otherwise: the guard denies writes to those paths, the gate re-hashes protected files and the policy snapshot on every stop, adapters install at user level, and `donegate verify` in CI is the backstop. For unattended runs, use a devcontainer that doesn't mount `~/.donegate/`.

## Status

| Milestone | State |
| --- | --- |
| M0 Cheat zoo (`testdata/fixtures/`) | done |
| M1 L1 rules + `verify` | done |
| M2 Claude Code adapter (session, guard, gate, evidence, install) | done, tested live |
| M3 L2 clean-worktree runner (R001–R003) | done |
| M4 OpenCode and pi shims, GitHub Action, `report --md` | next |
| M5 Held-out tests, mutation testing, cross-model judge | planned |

Languages: JS/TS rules first. Other languages already get the guard, hash checks and L2 runs via exit code or JUnit XML; language-specific L1 patterns for Go, Python and .NET are on the roadmap.

## Development

```sh
go build ./...
go vet ./...
go test ./...
```

`testdata/fixtures/` is the acceptance suite: each fixture is a cheating (or honest) change against a tiny Node + Vitest project, with the exact findings expected in `expected.json`. See [`AGENTS.md`](AGENTS.md) for conventions, and never edit an `expected.json` to make a test pass.

## License

[MIT](LICENSE)
