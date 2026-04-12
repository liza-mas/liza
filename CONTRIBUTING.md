# Contributing to Liza

Thank you for your interest in contributing to Liza.

## Getting Started

### Requirements

- Go 1.25.5+
- Git 2.38+
- [pre-commit](https://pre-commit.com/)

### Setup

```bash
git clone https://github.com/liza-mas/liza.git
cd liza
pre-commit install --hook-type pre-commit --hook-type commit-msg --hook-type pre-push
make build
```

### Verify

```bash
liza version
make test
```

## Development Workflow

### Build

The Go binary embeds contracts and skills via `go:embed`. The `sync-embedded` step copies
`contracts/` and `skills/` into `internal/embedded/` — this runs automatically as part of
`make build` and `make test`.

**Always use `make test` instead of bare `go test ./...`** — without the sync step,
`internal/embedded` fails to compile.

```bash
make build          # Build liza
make test           # Unit tests (includes sync + testhelpers check)
make test-e2e       # End-to-end tests (~40s)
make lint           # Format + vet + static analysis
make coverage       # Tests with HTML coverage report
```

### Embedded Files

Files in `internal/embedded/` fall into two categories:

- **Mastered in-place**: `claude-settings.json`, `mcp.json`, `hooks/` — edit directly
- **Synced from repo root**: `contracts/`, `skills/` — edit the repo-root masters,
  never the `internal/embedded/` copies

Run `make check-embedded` to verify consistency.

### Test Helpers

The `internal/testhelpers` package must only be imported from `*_test.go` files.
`make check-testhelpers` enforces this and runs as part of `make test`.

## Coding Standards

### Go

- Format with `gofmt` and `goimports` (enforced by pre-commit)
- Static analysis via `go vet` and `staticcheck`
- No `testhelpers` imports in production code

### Commit Messages

Follow [Conventional Commits](https://www.conventionalcommits.org/):

```
type(scope): short summary

Body explaining why (not just what).

BREAKING CHANGE: what breaks and migration path (if applicable)
```

Types: `feat`, `fix`, `refactor`, `docs`, `test`, `chore`, `perf`, `ci`

Enforced by [commitizen](https://commitizen-tools.github.io/commitizen/) pre-commit hook.

### Pre-commit

All hooks must pass before committing. The suite includes:

- Go: `gofmt`, `goimports`, `go vet`, `staticcheck`, `go mod tidy`
- Code quality: duplicate detection (`jscpd`), testhelpers isolation
- General: trailing whitespace, end-of-file, YAML/JSON/TOML validation
- Commit message: Conventional Commits validation

## Scope

Liza is intentionally scoped as a standalone engineering tool. Contributions that add
integrations with external systems (Jira, GitHub, CI platforms, etc.) or target
non-engineer personas are outside the current scope and will not be merged.

## Submitting Changes

1. For non-trivial changes, open an issue or discussion first to align on approach
2. Create a branch from `main`
3. Make focused, single-intent changes
4. Ensure `make lint` and `make test` pass
5. Open a pull request with a clear description of what and why

## Contracts and Skills

If your change touches behavioral contracts (`contracts/`) or skills (`skills/`):

- These are loaded as agent system prompts — every token costs context budget
- Prefer tightening existing wording over appending new text
- Compare before/after byte count; growth should not exceed semantic content added

## Architecture Decisions

For changes with architectural impact, check `specs/architecture/ADR/README.md` for
prior decisions that may constrain the design. New architectural decisions should be
recorded as ADRs using the template at `specs/architecture/ADR/TEMPLATE.md`.

## License

By contributing, you agree that your contributions will be licensed under the
[Apache 2.0 License](LICENSE).
