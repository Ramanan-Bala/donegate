# TODO

Pending work after v0.2.0. See `docs/SPEC.md` for the design behind each item.

## 1. Easier install and setup

- [ ] Publish prebuilt binaries with GoReleaser (macOS, Linux, Windows; amd64 and arm64), built by a release workflow when a tag is pushed.
- [ ] Add a Homebrew tap so people can run `brew install ramanan-bala/tap/donegate`.
- [ ] Add `donegate init` (in the spec, not built yet): detect the stack (npm/pnpm/yarn, Vitest/Jest, `tsc`, monorepo packages) and write `.donegate.yml`.
- [ ] Update the README install steps once these ship, so readers no longer need Go.

## 2. Use it on real work

- [ ] Add `.donegate.yml` with project test commands to spendtrails so the clean test run (Level 2) works there.
- [ ] Record how long the standard-tier gate takes on a real repo (the open M3 acceptance criterion).
- [ ] Run agy workers under donegate for about a week and log every unnecessary block.
- [ ] Reduce false positives:
  - [ ] C001 flags any `include`/`exclude` change, even one that widens coverage.
  - [ ] T003 fires when an existing skip is only re-indented.

## 3. M5: checks on behaviour

- [ ] B001 hidden tests, plus `donegate heldout add`: tests the agent never sees, run only at the gate.
- [ ] B002 mutation testing (Stryker) limited to changed lines.
- [ ] T005: weakened assertions (`toEqual` → `toBeDefined`, deleted `expect`s).
- [ ] C002: new `@ts-ignore`, `@ts-expect-error`, `eslint-disable`.
- [ ] S004: mocking the module under test.
- [ ] R004: changed lines with no test coverage.
- [ ] L4: an optional LLM judge.

## 4. More languages

- [ ] Go cheat patterns for Level 1 (`t.Skip`, `//go:build ignore`, `-run` filters in test scripts).
- [ ] .NET patterns (`[Ignore]`, `Skip=`, `<Compile Remove>`).
- [ ] Python patterns (`pytest.mark.skip`, `conftest.py`, `pytest.ini`/`pyproject.toml` test config).

## 5. Later

- [ ] Sign evidence with a key the agent cannot read (macOS Keychain locally; Azure Key Vault or a GitHub OIDC key in CI). `report_sha256` is not signed yet.
- [ ] Azure DevOps pipeline task.
- [ ] Report "cost per verified task" for each model.

## Small fixes

- [ ] Upload `assets/social-preview.png` under GitHub Settings → Social preview (manual).
- [x] Pin the README's Action example to `@v0.2.0` instead of `@main`.
- [ ] Pass the session id in the OpenCode and pi guard events. They send none today, so the guard falls back to the repo's current session.
- [x] Make `shims/tsconfig.json` portable (it uses machine-local paths).
- [ ] Add `git pull` to the worker deny list in `~/.agents/orchestration` (it changes git state and isn't blocked today).
