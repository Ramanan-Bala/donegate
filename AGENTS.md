# donegate — agent instructions

donegate is a cross-harness verification gate for AI coding agents. The full design is in `docs/SPEC.md`; read the relevant section before changing anything.

## Stack

- Go 1.27, module `github.com/Ramanan-Bala/donegate`, single binary at `cmd/donegate`.
- Dependencies allowed: `spf13/cobra`, `gopkg.in/yaml.v3`, `bmatcuk/doublestar/v4`. Ask before adding anything else.
- Git is used by shelling out to the `git` binary (no go-git).

## Layout

- `cmd/donegate/` — cobra CLI entry and subcommands.
- `internal/<concern>/` — one package per concern (policy, diff, rules, verify, session, ...). See "Repo layout and stack" in `docs/SPEC.md`.
- `internal/rules/` — one file per rule (`t001_test_modified.go`) plus its test (`t001_test_modified_test.go`). Rules register themselves with `func init() { Register(...) }`; never maintain a central list.
- `testdata/fixtures/` — the cheat zoo (see `testdata/fixtures/README.md`).

## Conventions

- Table-driven tests with Go's `testing` package. No test frameworks.
- `gofmt` clean; `go vet ./...` clean.
- Errors are wrapped with context: `fmt.Errorf("load policy: %w", err)`.
- Every finding carries a rule ID, a path, and a fix hint the agent can act on.

## Verify

```
go build ./...
go vet ./...
go test ./...
```

## Rules for agents working on this repo

- Never edit `testdata/fixtures/**/expected.json` or an existing `*_test.go` to make a test pass. If an expectation is wrong, say so in your report.
- Never commit or push.
