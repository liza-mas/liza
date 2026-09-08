# Testing Guide

Running tests, coverage targets, and test utilities for the Liza Go codebase.

## Running Tests

### Quick Reference

| Command | What |
|---------|------|
| `make test-fast` | Short-mode iteration; skips guarded integration tests |
| `make test` | Required routine full-suite check, without race or coverage instrumentation |
| `make test-race` | Full suite with the race detector; required once during final pre-commit/merge validation |
| `make coverage` | Full suite with per-package coverage, then an HTML report |
| `go test -run TestFoo ./internal/db/` | Specific test |
| `go test -run "TestBlackboard.*" ./internal/db/` | Pattern match |
| `go test -tags e2e ./internal/integration/` | Run e2e full sprint test (gated by `//go:build e2e`, ~40s) |
| `make test-e2e` | Same as above via Makefile target |

### Windows compatibility

The Windows CI job runs the Go suite natively with Git for Windows on `PATH`.
Local cross-compilation checks platform-specific code but does not replace that
runtime check. Fixtures must use native executable names, portable shell stubs,
and the same home-directory resolver as production. POSIX permission bits are
not Windows ACLs; tests requiring privacy must check access control rather than
equating a writable attribute with a private file.

### Coverage

```bash
make coverage
```

The target creates a unique profile under `${TMPDIR:-/tmp}`, keeps it through
`go tool cover -html`, and removes it on success, failure, or interruption.
Coverage remains package-local because the target does not use
`-coverpkg=./...`; do not treat its aggregate as the cross-package coverage
basis documented in the architecture review.

### Race Detection

`make test` intentionally omits `-race` so routine iteration stays responsive.
Run the race target once for every final change set, not only changes that
obviously use goroutines or locks:

```bash
make test-race
```

This is enforced by the project-local worktree validation instructions. CI
wiring is currently deferred; the existing workflow still invokes `make test`,
so it is not the race gate.

### Faster temporary storage

State-heavy tests fsync many temporary files. On Linux, a tmpfs can reduce that
filesystem latency when it has enough free space:

```bash
mkdir -p /dev/shm/liza-tests
TMPDIR=/dev/shm/liza-tests make test
```

Use a private writable directory and monitor its capacity. Keep the default
`/tmp` on hosts without tmpfs or when the suite could exceed available memory.

### Benchmarks

```bash
go test -bench=. ./internal/db/                          # Run all
go test -bench=BenchmarkRead ./internal/db/               # Specific
go test -bench=. -benchmem ./internal/db/                 # Memory stats
go test -bench=. -benchtime=10s -count=5 ./internal/db/   # Stability
```

See [PERFORMANCE.md](PERFORMANCE.md) for benchmark targets and regression detection.

## Test Organization

```
internal/
├── db/
│   ├── blackboard.go           # Production code
│   ├── blackboard_test.go      # Unit tests
│   ├── metrics.go / metrics_test.go
│   ├── watcher.go / watcher_test.go
│   └── errors.go / errors_test.go
├── commands/
│   ├── add_task.go / add_task_test.go
│   ├── validate.go / validate_test.go
│   └── ... (each command has its test)
├── models/
│   └── state.go / state_test.go
├── git/
│   └── worktree.go / worktree_test.go
├── testhelpers/               # Shared test utilities
│   ├── fixtures.go            # State/task factory functions
│   ├── setup.go               # Git repo, .liza dir, worktree setup
│   ├── assertions.go          # Test assertion helpers
│   ├── git.go                 # Git operation helpers
│   └── utils.go               # Misc utilities
└── integration/               # End-to-end tests
    ├── e2e_workflow_test.go
    ├── concurrent_operations_test.go
    ├── full_sprint_test.go        # //go:build e2e — full pipeline test
    ├── lease_expiry_test.go
    └── sprint_and_merge_test.go
```

## Coverage Targets

**Project standards**: 80% minimum for core packages, 80% overall target.

| Package | Coverage | Status |
|---------|----------|--------|
| internal/prompts | 100% | Excellent |
| internal/analysis | ~96% | Excellent |
| internal/models | ~93% | Excellent |
| internal/git | ~88% | Very Good |
| internal/commands | ~84% | Very Good |
| internal/db | ~83% | Very Good |
| internal/agent | ~62% | Adequate |

**Note**: High coverage != good tests. Tests must validate requirements, not just exercise code. Focus on critical paths (state machine, locking, validation), error handling, edge cases (empty lists, nil values, concurrent access), and integration points.

## Test Utilities

The `internal/testhelpers` package provides utilities for writing tests.

### Test State Fixtures

```go
import "github.com/liza-mas/liza/internal/testhelpers"

// Complete valid state with sensible defaults
state := testhelpers.CreateValidState()

// Customize as needed
state.Goal.Description = "Custom goal"
state.Tasks = []models.Task{
    testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, time.Now().UTC()),
}

// Write state to blackboard
tmpDir := t.TempDir()
statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
bb := testhelpers.WriteInitialState(t, statePath, state)
```

### Task Fixtures by Status

```go
now := time.Now().UTC()

// BuildTaskByStatus sets all required fields for the given status
task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now)
// IMPLEMENTING task has AssignedTo, LeaseExpires, BaseCommit, Worktree set

task := testhelpers.BuildTaskByStatus("task-2", models.TaskStatusBlocked, now)
// BLOCKED task has BlockedReason, BlockedQuestions set

// Pointer helpers for optional fields
task.AssignedTo = testhelpers.StringPtr("coder-1")
task.LeaseExpires = testhelpers.TimePtr(now.Add(30 * time.Minute))
```

### Setup Helpers

```go
dir := t.TempDir()  // Automatic cleanup, parallel-safe

// Git repo with initial commit + integration branch
testhelpers.SetupTestGitRepo(t, dir)

// .liza directory with state.yaml and lock file
statePath, lockPath := testhelpers.SetupLizaDir(t, dir)

// Worktree directory (filesystem only, not git worktree)
testhelpers.CreateTestWorktree(t, dir, "task-1")

// Spec file
testhelpers.CreateSpecFile(t, dir, "vision.md", "# Vision\nTest spec")

// Register agent in blackboard
testhelpers.RegisterTestAgent(t, bb, "coder-1", "coder")
```

### Time Helpers

```go
// Fixed time for reproducible tests
now := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)

// For time-based tests, truncate to avoid flakiness
now := time.Now().UTC().Truncate(time.Second)
```

## Writing Tests

### Table-Driven Tests (Preferred Pattern)

```go
func TestAddTaskCommand(t *testing.T) {
    tests := []struct {
        name        string
        taskID      string
        wantErr     bool
        errContains string
    }{
        {name: "add basic task", taskID: "task-1", wantErr: false},
        {name: "duplicate ID", taskID: "task-1", wantErr: true, errContains: "already exists"},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            dir := t.TempDir()
            bb := setupTestBlackboard(t, dir)

            err := AddTaskCommand(/* ... */)

            if tt.wantErr {
                if err == nil {
                    t.Errorf("Expected error containing %q, got nil", tt.errContains)
                }
            } else if err != nil {
                t.Errorf("Unexpected error: %v", err)
            }
        })
    }
}
```

### Assertion Guidelines

```go
// Use Fatal for setup failures (stops test immediately)
state, err := bb.Read()
if err != nil {
    t.Fatalf("Failed to read state: %v", err)
}

// Use Error for assertions (continues to report all failures)
if state.Config.Mode != models.SystemModeRunning {
    t.Errorf("Expected RUNNING mode, got %s", state.Config.Mode)
}
```

### Integration Tests

Located in `internal/integration/`. These tests are long-running end-to-end checks and skip automatically when `go test -short` is used.

```go
func TestFullWorkflow(t *testing.T) {
    dir := t.TempDir()
    // Set up real git repo, init liza, add tasks, claim, submit, review, merge
}
```

Integration tests cover: init -> add-task -> claim -> submit -> review -> merge, multiple agent interaction, concurrent operations, lease expiry, and sprint lifecycle.

### E2E Tests (build tag: `e2e`)

The full sprint sequence test (`full_sprint_test.go`) exercises the complete supervisor pipeline — epic-planning, US-writing, code-planning, and coding — using a mock CLI executor. It's gated by the `e2e` build tag so it doesn't run in the default test suite (~40s).

```bash
go test -tags e2e -v ./internal/integration/   # or: make test-e2e
```
