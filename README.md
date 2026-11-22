<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="assets/logo-dark.svg">
    <img alt="donegate" src="assets/logo.svg" width="360">
  </picture>
</p>

<p align="center"><strong>Let cheap AI models write your code, but make them prove it works.</strong></p>

<p align="center">
  <a href="https://github.com/Ramanan-Bala/donegate/actions/workflows/ci.yml"><img alt="CI" src="https://github.com/Ramanan-Bala/donegate/actions/workflows/ci.yml/badge.svg"></a>
  <a href="LICENSE"><img alt="MIT license" src="https://img.shields.io/badge/license-MIT-16a34a"></a>
  <img alt="Go 1.27+" src="https://img.shields.io/badge/go-1.27%2B-0f172a">
</p>

<p align="center">
  <img src="assets/agents/claude-code.svg" height="28" alt="Claude Code" title="Claude Code">&nbsp;&nbsp;
  <img src="assets/agents/opencode.svg" height="28" alt="OpenCode" title="OpenCode">&nbsp;&nbsp;
  <img src="assets/agents/pi.svg" height="28" alt="pi" title="pi">&nbsp;&nbsp;
  <img src="assets/agents/antigravity.png" height="28" alt="Antigravity" title="Antigravity">&nbsp;&nbsp;
  <img src="assets/agents/github-actions.svg" height="28" alt="GitHub Actions" title="GitHub Actions">
</p>

---

## What is donegate?

AI coding assistants are great at finishing tasks quickly. Sometimes too quickly: when a check fails, some of them "fix" it by **changing the check instead of the code**. It is like a student who can't solve a problem, so they quietly edit the answer key.

**donegate is the teacher who locks the answer key away and re-grades the work before the assistant is allowed to say "done".**

It works with the AI coding assistant you already use. You install it once, and from then on it quietly watches in the background.

## What it does, in three steps

| | Step | In plain words |
| :-: | --- | --- |
| 🔒 | **Locks the answer key** | When the assistant starts working, donegate takes a snapshot of your project's tests and settings and keeps it somewhere the assistant can't touch. |
| 👀 | **Watches every change** | If the assistant tries to edit an existing test or loosen a setting to make a failure disappear, donegate stops it and explains what to do instead: fix the real code. New tests are always welcome. |
| ✅ | **Checks before "done"** | When the assistant says it has finished, donegate checks the work itself. If something is wrong, the assistant gets a short to-do list and has to keep going. If everything is right, you get a signed-off report. |

## What you will see

When the assistant tries to take a shortcut, it is sent back to work with a clear message:

```
donegate: not done (2 failures, attempt 1/5)
- T001 src/cart.test.ts was modified. Tests are read-only; revert it and fix the source under test instead.
- S002 test environment check detected in src/cart.ts:18; remove test-environment branching.
Fix these and stop again; donegate re-checks every stop.
```

When the work really is done, you see:

```
donegate: verified (0 warnings). Evidence: ~/.donegate/sessions/<id>/evidence/evidence.json
```

If the assistant still can't get it right after 5 tries, donegate lets it stop but clearly marks the work as **NOT verified**, so you never get a false "done".

---

## Get started

You need two things on your computer:

- **git**, which most developers already have.
- **Go**, a free programming toolkit used to install donegate. Download it from [go.dev/dl](https://go.dev/dl/) and run the installer.

### Step 1: Install donegate

Open a terminal (on a Mac: the *Terminal* app) and paste:

```sh
go install github.com/Ramanan-Bala/donegate/cmd/donegate@latest
```

Check that it worked:

```sh
donegate version
```

### Step 2: Connect it to your AI assistant

Pick the assistant you use and run its one command. You can connect more than one.

<table>
<tr>
<td width="56" align="center"><img src="assets/agents/claude-code.svg" width="40" alt="Claude Code"></td>
<td>

**Claude Code**

```sh
donegate install claude
```

Your existing Claude Code settings are kept, and a backup is saved first. To undo: `donegate uninstall claude`

</td>
</tr>
<tr>
<td align="center"><img src="assets/agents/opencode.svg" width="40" alt="OpenCode"></td>
<td>

**OpenCode**

```sh
donegate install opencode
```

Adds a small donegate plugin to OpenCode. To undo: `donegate uninstall opencode`

</td>
</tr>
<tr>
<td align="center"><img src="assets/agents/pi.svg" width="40" alt="pi"></td>
<td>

**pi**

```sh
donegate install pi
```

Adds a small donegate extension to pi. To undo: `donegate uninstall pi`

</td>
</tr>
<tr>
<td align="center"><img src="assets/agents/antigravity.png" width="40" alt="Antigravity"></td>
<td>

**Antigravity**

```sh
donegate install agy
```

Adds a donegate hook to Antigravity's hook settings, keeping any hooks you already have. To undo: `donegate uninstall agy`

</td>
</tr>
<tr>
<td align="center"><img src="assets/agents/github-actions.svg" width="40" alt="GitHub Actions"></td>
<td>

**GitHub pull requests** (works with any assistant, or none)

Add a file called `.github/workflows/donegate.yml` to your project with:

```yaml
name: donegate
on: pull_request
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
      - uses: Ramanan-Bala/donegate@v0.2.0
```

Every pull request then gets a donegate report as a comment, and the check fails if the work isn't proven. This is the strongest protection, because it runs on GitHub's computers where no assistant can interfere.

</td>
</tr>
</table>

That's it. Start your assistant as usual; donegate does the rest.

> **Good to know:** once connected, donegate is active in **every** project you open with that assistant. Existing tests become read-only for the assistant (not for you).

### Step 3 (optional): Let donegate run your tests too

Out of the box, donegate catches shortcuts by looking at what changed. To have it also **run your tests itself** before saying "done", add a file named `.donegate.yml` to the top folder of your project:

```yaml
version: 1
projects:
  - name: app
    root: ""
    test: npm test
```

Replace `npm test` with whatever command runs your tests. donegate runs it in a clean copy of your project, with the original tests put back, so a rigged test can't fool it.

---

## Common questions

**Does it send my code anywhere?**
No. donegate runs entirely on your computer (or on GitHub, if you use the pull request check). Nothing is uploaded.

**Will it slow my assistant down?**
Barely. Watching changes takes a few thousandths of a second. The check at the end takes about a second, plus the time your own tests take if you turned on Step 3. If nothing changed, there is no check at all.

**What if my task really is to change a test?**
Then it isn't cheating, and you can approve it. In your own terminal (not the assistant's), run:

```sh
donegate allow T001 src/cart.test.ts
```

donegate shows what you are approving and asks you to type `yes`. The approval lasts for the current session only and is listed in the report under "Approved exceptions", so reviewers can see it. Assistants cannot approve their own exceptions: donegate refuses to run this command outside a real terminal and blocks assistants from running it. Use `all` instead of `T001` to allow any protected change to that file, `donegate allow --list` to see approvals and `donegate allow --revoke <file>` to undo one.

**Can the assistant just turn donegate off?**
donegate blocks it from editing its own settings, its records and the assistant's hook settings, and it re-checks fingerprints of everything important on every "done". The pull request check on GitHub is the final safety net, since it runs where the assistant has no access at all.

**How do I remove it completely?**
Run the matching `donegate uninstall ...` command for each assistant, then delete the `~/.donegate` folder.

---

## For developers

<details>
<summary><strong>How it works</strong></summary>

One Go binary does all the judging. Each assistant ("harness") forwards three moments to it:

| Moment | Command | What happens |
| --- | --- | --- |
| Session start | `donegate session start` | Records a baseline commit of the working tree (including uncommitted and untracked files, without touching your index or branches), hashes every protected file and snapshots `.donegate.yml` into `~/.donegate/sessions/<id>/`. |
| Every edit | `donegate guard` | Denies writes to existing tests, snapshots, test/lint config, `.donegate.yml`, donegate state and harness hook config. Also catches shell writes (`sed -i`, redirects, `mv`, `rm`, `git checkout --`, ...) and snapshot updates (`vitest -u`, `jest --updateSnapshot`). New test files are allowed. |
| Every stop | `donegate gate` | Silent if nothing changed. Otherwise runs the checks for the configured tier and blocks with a short, fixable list, up to `max_blocks` attempts. After that it lets the agent stop, marks the evidence `FAILED` and says so. |
| CI / anywhere | `donegate verify --base origin/main` | Same checks from a clean state; the exit code fails the build. |

Claude Code uses native hooks (`SessionStart`, `PreToolUse`, `Stop`, `SubagentStop`); the `PreToolUse` hook fires before the permission check, so it holds even under `--dangerously-skip-permissions`. Antigravity also uses native hooks (`SessionStart`, `PreToolUse`, `Stop` in `hooks.json`, via `--format agy`); when donegate has no objection it answers `"ask"`, which defers to Antigravity's own permission rules. OpenCode and pi use small TypeScript shims (`shims/`) that talk to the binary through a harness-neutral JSON format (`--format json`). Shell parsing in the guard is best-effort; the hash check at the gate catches whatever slips through.

Core design rule: **the judge lives outside anything the agent can write.** Policy and baselines are snapshotted at session start; changes made mid-session are ignored and flagged (H001). See [`docs/SPEC.md`](docs/SPEC.md) for the full design and prior art.

</details>

<details>
<summary><strong>Checks</strong></summary>

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

Tiers: `fast` = L1 · `standard` = L1 + L2 (default for the Stop gate) · `strict` = everything (default for `verify`).

L2 runs each configured project's commands in a throwaway git worktree that contains the agent's changes but with every protected file restored to its baseline content. Results are read from Vitest/Jest JSON or JUnit XML (pytest, `go-junit-report`, .NET loggers, ...), falling back to the exit code. `node_modules` is symlinked in from your repo.

</details>

<details>
<summary><strong>Full configuration (<code>.donegate.yml</code>)</strong></summary>

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

`{json}` is replaced with a temp file path that donegate reads the test report from. The repo copy of `.donegate.yml` is only a proposal: the active policy is always the snapshot taken at session start (and, for `verify`, the copy in the base commit). donegate dogfoods itself; see this repo's [`.donegate.yml`](.donegate.yml) for a Go example.

</details>

<details>
<summary><strong>CLI reference</strong></summary>

| Command | Purpose |
| --- | --- |
| `donegate install claude\|opencode\|pi\|agy` | Connect a harness (`--dry-run` to preview, `--dir` / `--settings` to override the location) |
| `donegate uninstall claude\|opencode\|pi\|agy` | Disconnect a harness |
| `donegate session start` | Take the baseline (`--format claude\|json` for hooks) |
| `donegate guard` | Judge one tool call from stdin (`--format claude\|json`) |
| `donegate gate` | Run the stop gate (`--format text\|claude\|json`) |
| `donegate verify --base <ref>` | Check a diff from a clean state (`--tier`, `--json`, `--md`, `--evidence-dir`) |
| `donegate report` | Show the latest evidence for this repo (`--md`, `--json`, `--session`) |
| `donegate allow <RULE\|all> <path>...` | Approve a protected change for the current session; human-only (interactive terminal + typed `yes`); `--list`, `--revoke` |
| `donegate version` | Print the version |

`verify` exit codes: `0` pass, `1` fail, `2` usage or runtime error.

GitHub Action inputs: `base`, `tier` (default `strict`), `working-directory`, `comment` (default `true`), `github-token`, `go-version`. Outputs: `verdict` (`PASS`/`FAIL`) and `evidence`. Only run it on `pull_request`, not `pull_request_target`: L2 executes the pull request's code, which must never run with a write token or secrets.

</details>

<details>
<summary><strong>Evidence reports</strong></summary>

Every judged stop writes `evidence.json` and `evidence.md` to `~/.donegate/sessions/<id>/evidence/` (`donegate report --md` prints the markdown):

```json
{
  "schema": "donegate/evidence@1",
  "session": "20261121T144011Z-f78fd7a9",
  "harness": "claude-code",
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

</details>

<details>
<summary><strong>Tamper resistance, honestly</strong></summary>

The agent runs as your user, so it can technically write to `~/.donegate/` and the harness settings. donegate defends in layers rather than pretending otherwise: the guard denies writes to those paths, the gate re-hashes protected files and the policy snapshot on every stop, adapters install at user level (never in the repo), and `donegate verify` in CI is the backstop. For unattended runs, use a devcontainer that doesn't mount `~/.donegate/`.

</details>

<details>
<summary><strong>Building from source and contributing</strong></summary>

```sh
git clone https://github.com/Ramanan-Bala/donegate && cd donegate
go build ./...
go vet ./...
go test ./...
```

`testdata/fixtures/` is the acceptance suite (the "cheat zoo"): each fixture is a cheating or honest change against a tiny Node + Vitest project, with the exact findings expected in `expected.json`. See [`AGENTS.md`](AGENTS.md) for conventions, and never edit an `expected.json` to make a test pass.

</details>

## Status

| | Feature | State |
| :-: | --- | --- |
| <img src="assets/agents/claude-code.svg" width="18"> | Claude Code | ✅ tested live |
| <img src="assets/agents/pi.svg" width="18"> | pi | ✅ tested live |
| <img src="assets/agents/opencode.svg" width="18"> | OpenCode | ✅ tested live |
| <img src="assets/agents/github-actions.svg" width="18"> | GitHub pull request check | ✅ |
| <img src="assets/agents/antigravity.png" width="18"> | Antigravity | ✅ tested live |
| | Hidden tests, mutation testing, second-opinion AI judge | 🗓️ planned |

Languages: JavaScript and TypeScript checks first. Other languages already get the edit protection, fingerprint checks and test runs; language-specific checks for Go, Python and .NET are on the roadmap.

## License

[MIT](LICENSE). Agent logos belong to their respective owners and are shown only to identify the supported tools.
