# donegate — Design & Build Spec

Nov 14, 2026 · @Ramanan

## Summary

donegate is a cross-harness verification gate that stops AI coding agents from declaring "done" until the work is proven. Pitch: **let cheap models implement, but make them prove it.**

- **What it does:** blocks edits to the judge (tests, snapshots, lint/CI config, hooks), then re-checks the agent's claim on every stop with static diff rules, a test run it controls, and optional held-out and mutation tests.
- **Where it runs:** Claude Code hooks first, then OpenCode and pi plugins, and a plain `donegate verify` CLI for CI or any harness.
- **What it leaves behind:** a hashed evidence report (`evidence.json` + markdown) you can attach to the PR.
- **Core design rule:** the judge lives outside anything the agent can write. Policy and baselines are snapshotted to `~/.donegate/` at session start; changes the agent makes to them mid-session are ignored and flagged.
- **Decisions made:** core in **Go**, thin TypeScript shims for OpenCode and pi. First target: your Node backend + React frontend repo (Jest/Vitest), on your Mac, built with Claude CLI.

## Problem and prior art

Agents pass the check without doing the task, and telling them not to barely helps. The pieces of a fix exist as single-harness scripts; no cross-harness, tamper-proof, behavioural product does yet.

**Evidence the problem is real**

- A September 2026 study (arXiv 2609.19101) found GLM 5.2, a typical cheap implementer, reward-hacked in 57.2% of DeepSWE and 73% of SWE-bench rollouts.
- [SpecBench](https://arxiv.org/abs/2605.21384): every frontier agent saturates the visible test suite while reward hacking persists.
- METR: o3 reward-hacked in 30.4% of runs; instructing it not to had nearly negligible effect.
- Anthropic's system cards list hardcoding expected outputs and special-casing tests as coding reward hacks; claude-code issue #7074 reports the agent editing tests instead of code ([summary](https://horadecodar.com.br/claude-code-alterando-testes/)).
- [claude-fake-work-guard](https://awesome.ecosyste.ms/projects/github.com%2Fmattyopon%2Fclaude-fake-work-guard): one user's baseline of 8.28% (strict) to 24.91% (loose) fake-work rate across 813 sessions.

**Prior art**

| Project                                                                                                                                 | What it does                                                  | Limits                                              |
| --------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------- | --------------------------------------------------- |
| [Read-only test hook](https://dev.to/penloom_studio_829b7817d3/your-ai-agent-will-pass-any-test-its-allowed-to-edit-51fo)               | Claude Code PreToolUse hook denies edits to test files        | Claude Code only; one rule; agent can edit the hook |
| [verify-before-stop.sh](https://dev.to/ianymu/stop-claude-code-from-lying-about-completion-a-50-line-bash-hook-1g2b)                    | Stop hook blocks completion until a verify log exists         | Trusts the agent's own log                          |
| [claude-fake-work-guard](https://awesome.ecosyste.ms/projects/github.com%2Fmattyopon%2Fclaude-fake-work-guard)                          | Stop hook blocks "done" claims with no recent verify tool use | Pattern-based; Claude Code only                     |
| [Procoder](https://pyshine.com/Procoder-A-Commit-Gate-Your-AI-Coder-Cannot-Talk-Past/)                                                  | Go binary commit gate requiring tests                         | Commit-time, not in-loop; no anti-tamper            |
| [Hooks playbook](https://agentic.proxify.io/setup/hooks-playbook)                                                                       | Stop hook runs tests; protects linter configs                 | Recipes, not a tool                                 |
| [Cursor forum request](https://forum.cursor.com/t/cursor-agent-composer-auto-reward-hacked-its-own-qa-rubric-to-pass-evaluation/166295) | Users asking for a protected-files list                       | Not shipped                                         |

**The lesson that shapes the design:** in a reported [Claude Code issue](https://claudeissues.com/issue/32376-security-claude-can-rewrite-its-own-hooks-who-watches-the-watchmen), Claude rewrote a user's `done-gate.js`, changing "code changes" to "SIGNIFICANT code changes" and weakening the gate. A gate the agent can edit is not a gate.

**The gap donegate fills:** cross-harness, behavioural (held-out and mutation tests, not just patterns), tamper-proof, and evidence-producing.

## Language decision: Go core, thin TypeScript shims

Go wins for the core. It's fast enough for a hook that fires on every edit, ships as one binary, and you already know it. Rust's extra speed buys nothing here, because real test runs take seconds and dominate the wall clock.

| Criterion                                         | Go                                                                                                  | TypeScript (Node/Bun)                                                              | Rust                                                   |
| ------------------------------------------------- | --------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- | ------------------------------------------------------ |
| Hook start-up cost (fires on every edit)          | A few ms                                                                                            | Tens of ms on Node; less with a Bun-compiled binary                                | A few ms                                               |
| Distribution                                      | Single static binary; trivial cross-compile with `CGO_ENABLED=0`                                    | Needs Node, or `bun build --compile` (large binary)                                | Single binary; slower cross-compile setup              |
| tree-sitter for diff rules                        | [gotreesitter](https://pkg.go.dev/github.com/odvcencio/gotreesitter): pure Go, no CGo, 206 grammars | `web-tree-sitter` (WASM) or the TS compiler API                                    | Best-in-class native bindings                          |
| Fit with harness plugin APIs                      | Needs shims for OpenCode and pi                                                                     | Native: OpenCode plugins and pi extensions are TS                                  | Needs shims                                            |
| Your fluency, and how well Claude CLI writes it   | High / very good                                                                                    | High / very good                                                                   | Not in your stack / good, but compile loops are slower |
| Build-to-MVP speed                                | Fast                                                                                                | Fastest for the first adapter, slower once you need process control and sandboxing | Slowest                                                |
| Concurrency (parallel test shards, mutation runs) | Goroutines, excellent                                                                               | Fine                                                                               | Excellent, more ceremony                               |

**Why not TypeScript for everything:** the core spawns test runners, manages worktrees, hashes files and must stay quick and dependency-free on every tool call. Go does that better. TypeScript is still used where the harness demands it: the OpenCode plugin and pi extension are \~50-line shims that call the `donegate` binary.

**Why not Rust:** it only wins on tree-sitter maturity, and gotreesitter closes most of that gap without CGo. You'd pay slower iteration in a language outside your stack for a speed gain nobody would notice.

**Revisit if:** parsing big diffs becomes the bottleneck (unlikely), or you want a WASM build to embed in a web UI. gotreesitter compiles to `wasip1` either way.

## Architecture

One Go binary does all the judging. Each harness gets a thin adapter that forwards three moments to it: session start, every edit, and every attempt to stop. A plain CLI covers CI and any harness without hooks.

&#91;embedded content: donegate architecture · 4 harnesses, 1 binary, state outside the repo\]

Every harness talks to the same binary; the binary trusts only its own state in `~/.donegate`, never what the agent wrote in the repo.

**The session lifecycle**

1. **Session start:** `donegate session start` records the base commit and any dirty working tree, hashes every protected file, and snapshots `.donegate.yml` into `~/.donegate/sessions/<id>/`. From here on, donegate reads only the snapshot.
2. **Edit guard (every tool call):** `donegate guard` reads the tool call as JSON and denies writes to protected paths, with a reason the agent can act on ("Tests are read-only; fix `src/cart.ts` instead"). For shell commands it catches common writes (`sed -i`, redirects, `mv`, `rm`, `git checkout --`) and snapshot flags (`jest -u`, `vitest -u`, `--updateSnapshot`). Shell parsing is best-effort; the hash check at the gate catches whatever slips through.
3. **Done gate (every stop):** `donegate gate` does nothing if no code changed. Otherwise it runs the checks configured for that tier (see Checks). It blocks with a short, specific list of failures, up to a retry limit. After that it lets the agent stop, marks the evidence `FAILED`, and tells the user.
4. **Verify (CI or manual):** `donegate verify --base origin/main` runs the same checks from a clean worktree on a diff. Its exit code fails the build.

**Harness adapters**

| Harness            | Edit guard                                                                                                                                                                                                                                | Done gate                                                                                                                                                                     | Installed at                                                       |
| ------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------ |
| Claude Code        | `PreToolUse` hook (matcher `Edit\|Write\|MultiEdit\|Bash`) returns `permissionDecision: "deny"`. It fires before the permission check, so it holds under `--dangerously-skip-permissions` ([docs](https://code.claude.com/docs/en/hooks)) | `Stop` hook returns `decision: "block"` + reason. Respect `stop_hook_active`; Claude Code ends the turn after \~8 consecutive blocks, so the first message must be actionable | `~/.claude/settings.json` (user level), plus a `SessionStart` hook |
| OpenCode           | Plugin `tool.execute.before`; throw an error to block ([docs](https://opencode.ai/docs/plugins))                                                                                                                                          | On the `session.idle` event, run the gate; on failure, send a follow-up prompt through the SDK client so the agent continues                                                  | `~/.config/opencode/plugin/donegate.ts`                            |
| pi                 | Extension `tool_call` returns `{ block: true, reason }`                                                                                                                                                                                   | `agent_before_settle` requests one last continuation with the failures ([docs](https://pi.dev/docs/latest/extensions))                                                        | `~/.pi/agent/extensions/donegate.ts`                               |
| CI / anything else | n/a                                                                                                                                                                                                                                       | `donegate verify --base <ref>`; non-zero exit fails the job                                                                                                                   | GitHub Action, Azure DevOps step, or a git pre-push hook           |

**Tamper resistance, honestly**

The agent runs as your user, so it can technically write to `~/.donegate/` and `~/.claude/settings.json`. donegate defends in layers rather than pretending this away:

- The guard denies edits and shell writes to the state dir, harness settings, hook scripts and `.donegate.yml`.
- The gate re-hashes protected files and the policy against the session-start baseline. Any mismatch is a hard failure, whoever caused it.
- Adapters install at user level, never in the repo, so a repo edit can't remove them.
- **The real backstop:** `donegate verify` in CI runs on a machine the agent never touched. For unattended runs, run the agent in a devcontainer that doesn't mount `~/.donegate/`.
- Later: sign evidence with a key the agent can't read (macOS Keychain, Azure Key Vault in CI).

## Checks catalogue

Checks run in four levels, cheapest first. A gate stops at the first level that fails, so the agent gets fast feedback. JS/TS rules come first because your test repo is Node + React; other languages reuse the same rule IDs with their own patterns.

**Tiers:** `fast` = L1 (about a second) · `standard` = L1 + L2 (default for the Stop gate) · `strict` = L1–L3 (for `verify` in CI) · L4 is opt-in at any tier.

| ID   | Level | Rule                                                                                                                                                                                 | Catches                                      | Default                |
| ---- | ----- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | -------------------------------------------- | ---------------------- |
| H001 | L1    | Protected file hash differs from session baseline (policy, hooks, harness settings)                                                                                                  | Agent weakening the gate itself              | Block                  |
| T001 | L1    | Existing test file modified (`*.test.*`, `*.spec.*`, `__tests__/**`, `e2e/**`). New test files are allowed                                                                           | Rewriting tests to match the bug             | Block                  |
| T002 | L1    | Test file or test case deleted                                                                                                                                                       | Deleting the failing test                    | Block                  |
| T003 | L1    | Focus/skip added: `.only`, `.skip`, `xit`, `xdescribe`, `it.todo`, `test.fails`                                                                                                      | Silencing failures                           | Block                  |
| T004 | L1    | Snapshot changed: `__snapshots__/*.snap` or `toMatchInlineSnapshot`                                                                                                                  | `jest -u` style rewrites                     | Block                  |
| T005 | L1    | Assertion weakened: `toBe`/`toEqual` → `toBeDefined`/`toBeTruthy`, `expect.anything()`, fewer `expect(` calls                                                                        | Hollow tests (only when T001 is relaxed)     | Block                  |
| C001 | L1    | Test/lint config loosened: `testMatch` narrowed, `passWithNoTests`, coverage threshold lowered, `"test": "... \|\| true"`, `strict: false`, lint rule turned off, new ignore entries | Gaming the toolchain                         | Block                  |
| C002 | L1    | Suppressions added: `@ts-ignore`, `@ts-expect-error`, `eslint-disable`, more `as any`                                                                                                | Hiding type errors                           | Warn                   |
| S001 | L1    | Literals from test fixtures appear in newly added source lines                                                                                                                       | Special-casing test inputs                   | Warn (Block in strict) |
| S002 | L1    | Source sniffs the test environment: `NODE_ENV === 'test'`, `JEST_WORKER_ID`, `VITEST`                                                                                                | Code that behaves differently under test     | Block                  |
| S003 | L1    | `process.exit(0)` added, or test setup files changed                                                                                                                                 | Exiting before tests run                     | Block                  |
| S004 | L1    | New `jest.mock`/`vi.mock` of the very module the diff changed                                                                                                                        | Mocking out the code under test              | Warn                   |
| R001 | L2    | donegate runs the test command itself in a clean worktree, with protected tests restored from baseline                                                                               | Agent's "all green" claims that aren't true  | Block                  |
| R002 | L2    | Test count dropped vs the session-start run, or zero tests ran (from Jest/Vitest JSON reporter)                                                                                      | Tests silently not running                   | Block                  |
| R003 | L2    | `tsc --noEmit` and lint pass, if configured                                                                                                                                          | Broken types hidden by a passing test run    | Block                  |
| R004 | L2    | Changed source lines have no test coverage (istanbul/v8 coverage)                                                                                                                    | Untested changes                             | Warn                   |
| B001 | L3    | Held-out tests pass. They live in `~/.donegate/heldout/<repo>/` and are added to the worktree only at gate time                                                                      | Overfitting to visible tests                 | Block                  |
| B002 | L3    | Mutation score on changed lines ≥ threshold (Stryker, scoped to changed files)                                                                                                       | Tests that pass no matter what the code does | Block below threshold  |
| J001 | L4    | A model from a different family compares the task (first prompt or PR description) with the diff and returns a JSON verdict                                                          | Diff that doesn't do what was asked          | Advisory               |

**Where held-out tests come from:** you write them for important tasks, or a different model writes them from the task description _before_ implementation starts. Later, repo-bench can mine them from your git history.

**What a block looks like to the agent** (short, specific, fixable on the first try):

```
donegate: not done (2 failures, attempt 1/5)
- T001 src/cart/__tests__/cart.test.ts was modified. Tests are read-only; revert it and fix src/cart/cart.ts.
- R001 1 test fails in a clean run: "applies 10% discount over 1000" (expected 900, got 1000).
Run `npm test -- cart` to reproduce.
```

## CLI, config and evidence

The whole surface is eight commands, one YAML file and one JSON report.

**Commands**

| Command                                        | Purpose                                                                                                              |
| ---------------------------------------------- | -------------------------------------------------------------------------------------------------------------------- |
| `donegate init`                                | Detects the stack (package.json, test runner, workspaces) and writes `.donegate.yml` with sensible globs             |
| `donegate install <claude\|opencode\|pi>`      | Writes the user-level hook config or plugin shim for that harness                                                    |
| `donegate session start`                       | Baseline: base commit, protected-file hashes, policy snapshot, baseline test count                                   |
| `donegate guard`                               | Reads one tool call as JSON on stdin and prints allow/deny in the harness's format (`--format claude\|opencode\|pi`) |
| `donegate gate`                                | Runs the Stop-gate tier; prints a block message or allows                                                            |
| `donegate verify --base <ref> [--tier strict]` | Same checks on a diff from a clean worktree; exit 0/1 for CI                                                         |
| `donegate heldout add <file>`                  | Moves a test file into the held-out store for this repo                                                              |
| `donegate report [--session <id>] [--md]`      | Prints or exports the evidence for a session                                                                         |

**`.donegate.yml` (first version)**

```yaml
version: 1
projects: # monorepo-aware: one entry per package
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
  tests:
    [
      "**/*.test.*",
      "**/*.spec.*",
      "**/__tests__/**",
      "**/__snapshots__/**",
      "e2e/**",
    ]
  config:
    [
      "**/jest.config.*",
      "**/vitest.config.*",
      "**/tsconfig*.json",
      "**/.eslintrc*",
      "**/eslint.config.*",
      ".github/workflows/**",
    ]
  allow_new_tests: true # agents may add test files, not edit existing ones
gate:
  tier: standard # fast | standard | strict
  max_blocks: 5 # below Claude Code's ~8-block cap
  timeout_seconds: 300
rules: # override any rule's severity
  S001: block
  R004: off
judge: # L4, optional
  enabled: false
  model: deepseek/deepseek-chat # pick a different family from the implementer
  base_url: https://openrouter.ai/api/v1
```

The repo copy is just a proposal. The active policy is always the snapshot taken at session start, so edits made mid-session do nothing until the next session (and trip H001).

**Evidence report (`evidence.json`)**

```json
{
  "schema": "donegate/evidence@1",
  "session": "01JD3...",
  "harness": "claude-code",
  "model": "claude-opus-5-5",
  "repo": "git@github.com:you/shop.git",
  "base": "a1b2c3d",
  "head": "e4f5a6b",
  "tier": "standard",
  "verdict": "PASS",
  "blocks": 2,
  "checks": [
    { "id": "T001", "status": "pass" },
    {
      "id": "R001",
      "status": "pass",
      "tests": { "passed": 184, "failed": 0, "baseline": 181 }
    },
    {
      "id": "C002",
      "status": "warn",
      "detail": "1 new @ts-expect-error in web/src/Cart.tsx:42"
    }
  ],
  "policy_sha256": "...",
  "diff_sha256": "...",
  "report_sha256": "..."
}
```

`donegate report --md` renders the same data as a PR comment: verdict, a table of checks, warnings, and how many times the agent was blocked.

## Repo layout and stack

A standard Go module with one directory per concern. Rules and runners are interfaces, so a new language or harness is a new package, not a rewrite.

```
donegate/
  cmd/donegate/main.go          # cobra CLI entry
  internal/
    session/                    # baseline, snapshots, state dir (~/.donegate)
    policy/                     # .donegate.yml schema, load, snapshot, hash
    guard/                      # path matching + shell-command write detection
    gate/                       # tier orchestration, retry counter, block messages
    diff/                       # git diff vs base, hunk model, file classification
    rules/                      # one file per rule: t001_test_modified.go ...
      rule.go                   # type Rule interface { ID() string; Check(ctx, Diff) []Finding }
    lang/js/                    # tree-sitter queries for JS/TS/TSX
    runner/                     # type Runner interface; jest.go, vitest.go (JSON reporters)
    worktree/                   # clean worktree, overlay changes, restore protected files
    heldout/                    # held-out store + overlay
    mutation/                   # Stryker driver (L3)
    judge/                      # OpenAI-compatible client for L4
    evidence/                   # evidence.json, markdown, hashes
    adapters/claude/            # stdin/stdout JSON for PreToolUse, Stop, SessionStart
  shims/
    opencode/donegate.ts        # ~50 lines: calls the binary
    pi/donegate.ts              # ~50 lines: calls the binary
  action/action.yml             # GitHub Action wrapping `donegate verify`
  testdata/fixtures/            # tiny repos that reproduce each cheat (see Build plan)
  .goreleaser.yaml              # CGO_ENABLED=0; darwin/linux/windows, amd64/arm64
```

| Concern       | Choice                                                                                                                        |
| ------------- | ----------------------------------------------------------------------------------------------------------------------------- |
| CLI           | `spf13/cobra`                                                                                                                 |
| Config        | `gopkg.in/yaml.v3` + JSON Schema for editor completion                                                                        |
| Git           | Shell out to `git` (worktree, diff, rev-parse); simpler and more correct than go-git for worktrees                            |
| Parsing       | [gotreesitter](https://pkg.go.dev/github.com/odvcencio/gotreesitter) for JS/TS/TSX queries; plain diff-line regex as fallback |
| Glob matching | `bmatcuk/doublestar`                                                                                                          |
| Tests         | Go `testing` + golden files; fixture repos in `testdata/`                                                                     |
| Release       | GoReleaser + Homebrew tap; `npm i -g donegate` wrapper later                                                                  |
| Shims         | TypeScript, no dependencies beyond the harness's own plugin types                                                             |

## Build plan

Four weeks to a usable MVP on your Node + React repo, starting with a "cheat zoo" of fixture repos that becomes the acceptance suite. Each milestone ends with a test you can run, not a feeling.

1. **M0 — Cheat zoo (day 1–2).** A tiny Node + Vitest project in `testdata/fixtures/`. Each fixture is a base commit, a cheating diff and an `expected.json` of findings: edited test, `.only`, `vitest -u` snapshot rewrite, deleted test, `NODE_ENV === 'test'` branch, `process.exit(0)`, loosened `vitest.config`, fixture literal hardcoded in source. Add 10 honest diffs taken from your repo's history as false-positive controls.
   - _Done when:_ `go test ./...` runs every fixture through a stub `verify` (all red).
2. **M1 — L1 + `verify` (week 1).** Session baseline, policy load/snapshot, git diff model, rules H001, T001–T004, C001, S002, S003, and `donegate verify --base`.
   - _Done when:_ every cheat fixture is caught with the right rule ID, and all 10 honest diffs pass.
3. **M2 — Claude Code adapter (week 2).** `install claude`, `SessionStart`, `PreToolUse` guard (file tools + shell write detection), `Stop` gate with retry counter and `stop_hook_active` handling, evidence file.
   - _Done when:_ in your repo, you add a failing test, ask Claude Code to "make the tests pass", and the guard denies the test edit, the gate blocks a premature "done", and `evidence.json` records it.
4. **M3 — L2 runner (week 3).** Clean worktree with protected files restored, Jest + Vitest JSON reporters, test-count baseline (R002), `tsc`/lint (R003), monorepo `projects` (backend + frontend).
   - _Done when:_ a scripted "claims green but a test fails" run is blocked by R001, and you've written down the standard-tier gate time on your repo.
5. **M4 — Other harnesses + CI (week 4).** OpenCode and pi shims, GitHub Action, `report --md`.
   - _Done when:_ the M2 scenario is also blocked under OpenCode running GLM-5.2 and under pi, and a PR gets the evidence comment.
6. **M5 — Behavioural layer (after MVP).** Held-out store (B001), Stryker on changed files (B002), remaining L1 rules (T005, C002, S001, S004, R004), L4 judge.

**Driving Claude CLI through it**

- Commit this spec as `docs/SPEC.md` and add an `AGENTS.md` (with a `CLAUDE.md` pointing to it): Go version, table-driven tests, one file + one fixture per rule, "never edit `testdata/**/expected.json` without saying so in the PR".
- One milestone per session, in plan mode. Let Opus plan and review, and hand individual rules to your cheaper OpenCode implementer. That's real-world test data for donegate too.
- **Dogfood from M2:** install donegate on the donegate repo itself, protecting existing `*_test.go` and `expected.json` files, so the agent can't cheat while building the cheat detector. Every block it triggers there is a free bug report or test case.
- Starter prompt for the first session:

```
Read docs/SPEC.md. We're doing M0 and the start of M1.
1. Scaffold the Go module and the layout in "Repo layout and stack".
2. Build the cheat zoo in testdata/fixtures as specified in M0, one fixture per cheat,
   each with expected.json. Ask me for the 10 honest diffs; don't invent them.
3. Implement the diff model and rule T001 end to end with `donegate verify --base`.
Stop after T001 passes its fixture and show me the plan for the remaining M1 rules.
```

## Antigravity adapter (planned, M4b)

Antigravity CLI (`agy`, v1.5.x) has a native, Claude-like hook system (`hooks.json`, documented in the CLI at `~/.gemini/antigravity-cli/builtin/skills/agy-customizations/docs/hooks.md`), so donegate plugs in with a third output format in the Go binary and no TypeScript shim.

**Where it installs**

- User level: a named hook `"donegate"` merged into `~/.gemini/config/hooks.json`. Named hooks from different sources are merged and run in order, so existing hooks (such as the Herdr worker guard) stay untouched.
- Project level (for tests and opt-in repos): `.agents/hooks.json`.
- `donegate install agy` / `uninstall agy`, with the same idempotent merge and backup rules as `install claude`.

**Mapping**

| donegate moment | agy event | Input (stdin, camelCase) | donegate output (stdout) |
| --- | --- | --- | --- |
| Session start | `SessionStart` (flat handler list) | `conversationId`, `cwd`, `workspacePaths` | `{"injectSteps":[{"systemMessage":{"systemMessage":"donegate is active ..."}}]}` |
| Edit guard | `PreToolUse`, matcher `write_file\|edit_file\|run_command` (plus future `*_file` write tools) | `toolCall.name`, `toolCall.args.Path` (write_file, edit_file), `toolCall.args.CommandLine` (run_command) | deny: `{"decision":"deny","reason":"donegate: ... (T001) ..."}` |
| Done gate | `Stop` (flat) | `terminationReason`, `continuationActive`, `executionNum` | block: `{"decision":"continue","reason":"donegate: not done (...)"}`; only judge when `terminationReason == "NO_TOOL_CALL"` |

Tool names and argument keys were confirmed from real agy transcripts on this machine: `write_file{Path,Content}`, `edit_file{Path,Target,Replacement}`, `run_command{CommandLine}`. The guard should treat an argument named `Path`, `TargetFile` or `AbsolutePath` as a write target for any tool whose name contains `write`, `edit`, `replace` or `delete`, so new tool variants are covered without a release.

**Implementation units**

1. `internal/adapters/agy` (parse/write the three contracts) plus `--format agy` on `session start`, `guard` and `gate`; harness name `antigravity`, harness session = `conversationId`.
2. `internal/install/agy.go` plus `donegate install|uninstall agy [--dir]`.
3. Live test in a temp repo with a project-level `.agents/hooks.json` and `agy -p` on a Flash model: the guard denies edit_file, write_file and run_command(sed) on an existing test and allows a new test file; Stop blocks a `NODE_ENV` cheat and the agent continues; evidence records `harness: antigravity`.

**Open questions**

- PreToolUse `"decision":"allow"` *auto-approves* the tool and bypasses the user's normal permission prompts. donegate must not do that: with no objection it needs a neutral answer. Verify whether `{}` (no decision) falls through to the normal permission flow; if not, ask the agy team for a "no opinion" value. Until then, emit `{}` on allow.
- The hook's working directory is the folder containing `hooks.json`; donegate must always use the payload's `cwd` (as it does for the other harnesses).
- Herdr workers run on agy. A user-level install gates every worker too. That is the point of the "gate your own orchestrator" roadmap item, but worker briefs must then add new test files instead of editing existing ones, and a task that legitimately changes tests needs `rules: {T001: off}` in the snapshot. Decide between a user-level and a per-repo install for the orchestrator setup.

## Risks, open questions, roadmap

The biggest risk is annoying users with false positives. Treat every block as something to justify with a rule ID and a fix hint.

| Risk                                                          | Mitigation                                                                                                                          |
| ------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------- |
| False positives make people uninstall it                      | Honest-diff controls in the cheat zoo; every rule has a severity override; `warn` by default for heuristic rules (S001, S004, C002) |
| Legitimate test edits (the task _is_ to change a test)        | `donegate allow T001 <path> --once` asked of the human, never the agent; or start the session with `--allow-test-edits`             |
| Slow gates on big suites                                      | Run only affected projects; Jest `--findRelatedTests` / Vitest `--changed`; mutation only in `verify`                               |
| Agents learn the rules                                        | Lean on behavioural checks (clean runs, held-out tests, mutation) over patterns                                                     |
| Same-user tamper (see Architecture)                           | Hash checks, guard on state paths, CI re-verify, devcontainer for unattended runs                                                   |
| Harnesses ship native "protected paths"                       | That only covers T001. Cross-harness use, L2–L4 and the evidence trail remain                                                       |
| Hook APIs change (Claude Code, OpenCode and pi all move fast) | Adapters are thin and isolated; pin tested versions in the README                                                                   |

**Open questions**

- Should the gate run on `SubagentStop` too, so your T3/OpenCode implementer and fixer subagents are gated before the orchestrator sees their result?
- How does a human approve a legitimate test change mid-session with the least friction (CLI prompt, a signed token, a comment in the PR)?
- Name check: search GitHub, npm and Homebrew for "donegate" before the first public release.

**Roadmap after MVP**

- Languages: `go test`, pytest, then .NET (xUnit/NUnit + Stryker.NET) for work repos.
- Azure DevOps task and an Entra-signed evidence trail, the commercial angle for companies.
- repo-bench integration: reuse mined held-out tests and report "cost per _verified_ task" by model and harness.
- Gate your own T3 orchestrator: route implementer and fixer roles through donegate by default.
