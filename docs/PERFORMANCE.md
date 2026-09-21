# Performance Guide

Operational reference for Liza's performance features: lock metrics, state caching, file system watching, and tuning.

## Lock Metrics

Optional performance monitoring for blackboard lock operations. Disabled by default.

### When to Enable

- Diagnosing slow agent performance or command execution
- Detecting lock contention with multiple agents
- Investigating timeout errors
- Profiling system behavior under load

### Interpreting Metrics

| Metric | Healthy | Warning | Problem |
|--------|---------|---------|---------|
| avg_acq | < 10ms | 10-100ms | > 100ms |
| max_acq | < 50ms | 50-500ms | > 500ms |
| avg_hold | < 500ms | 500ms-2s | > 2s |
| max_hold | < 2s | 2-5s | > 5s |

**High acquisition times** (avg_acq > 100ms): Lock contention. Multiple agents/processes competing. Reduce parallelism or operation frequency.

**High hold times** (avg_hold > 2s): Long-running operations under lock. Review code holding lock, consider moving computation outside lock.

**High max with low avg**: Occasional spikes (may be acceptable). Check for periodic batch operations or stuck processes.

### Usage

```go
bb := db.New(".liza/state.yaml")
bb.EnableMetrics()

// ... perform operations ...

recorder := bb.GetMetricsRecorder()
stats := recorder.GetStats()
fmt.Println(stats.String())
// Output: Lock statistics: count=42, avg_acq=5ms, avg_hold=120ms, max_acq=50ms, max_hold=500ms

// Clear between runs
recorder.Clear()

// Disable when done
bb.DisableMetrics()
```

Overhead is negligible (~48 bytes/op, ~1-2us/op) but disable in long-running production.

## State Caching

Liza uses **mtime-based caching** to avoid redundant YAML parsing.

### How It Works

1. First `Read()` parses state.yaml and caches result
2. Subsequent `Read()` checks file mtime first
3. If mtime unchanged, returns cached state (~10-50us vs ~1-5ms)
4. If mtime changed, re-parses and updates cache

**Cache coherence across processes**: Process A modifies -> mtime changes -> Process B reads -> detects change -> re-parses. No stale reads (assuming filesystem semantics).

### Gotchas

- **mtime granularity**: 1 second on ext3/FAT32. Rapid modifications within same second may not invalidate cache. Liza uses atomic rename, which updates mtime immediately.
- **External modifications**: Editors with auto-save, Dropbox/iCloud syncing `.liza/` will thrash the cache.
- **Cache miss on every read-after-write**: If every `Read()` follows a `Modify()`, caching doesn't help.

## File System Watching

Event-driven state change notification using `fsnotify`.

### Polling vs Watching

| | Polling | Watching |
|---|---------|---------|
| Latency | 0-30s (15s mean) | 0-50ms |
| CPU idle | Wakes every 30s | Zero |
| Response | 30-600x slower | Immediate |

All agents use a hybrid approach: event-driven primary, 30s polling fallback.

### Implementation Details

**Debouncing**: 50ms delay coalesces rapid events. Atomic rename creates multiple fsnotify events (CREATE, WRITE, RENAME) -- debouncing reduces 3-5 events per `Modify()` to 1.

**Directory watching**: Watches the `.liza/` directory, not state.yaml directly. Atomic rename creates a new inode; watching the file directly would miss it.

**Cross-platform**: Linux (inotify), macOS (FSEvents), Windows (ReadDirectoryChangesW), BSD (kqueue). Network filesystems (NFS, SMB) may not support notifications -- polling fallback recommended.

## Performance Tuning

### Configuration Parameters

| Parameter | Default | Trade-off |
|-----------|---------|-----------|
| `heartbeat_interval` | 60s | Lower = faster crash detection, more writes |
| `lease_duration` | 1800s (30min) | Lower = faster crash recovery, more renewals |
| `doer_max_wait` | 18000s (5hr) | Lower = agents exit faster when idle |
| Lock timeout | 10s (code) | Lower = fail fast, may false-positive on slow systems |

### Tuning Profiles

**Short tasks** (<10 min): `heartbeat: 30, lease: 900, max_wait: 600`

**Long tasks** (30min-5hr): `heartbeat: 60, lease: 3600, max_wait: 18000`

**Network filesystems**: `heartbeat: 90, lease: 2700` (and increase lock timeout in code)

### Lock Hold Time Optimization

**Bad** -- expensive computation under lock:
```go
bb.Modify(func(s *models.State) error {
    result := computeExpensiveMetrics(s)
    s.Sprint.Metrics = result
    return nil
})
```

**Good** -- compute outside, update under lock:
```go
state, _ := bb.Read()
result := computeExpensiveMetrics(state)
bb.Modify(func(s *models.State) error {
    s.Sprint.Metrics = result
    return nil
})
```

**Batch operations**: Multiple `bb.Modify()` calls in a loop should be a single `bb.Modify()` with the loop inside.

### Parallel Agent Scaling

**Optimal**: 1 planner, 1-3 coders, 1-2 reviewers.

**Contention indicators**: avg lock acquisition > 100ms, agents waiting frequently, high CPU on filesystem ops. **Solution**: reduce agents rather than adding more.

## Test Suite

Measured on the same 8-CPU development host on 2026-08-24. The historical default included
race instrumentation and coverage; the current default intentionally includes neither, with
those checks available through `make test-race` and `make coverage`.

| Default `make test` | Wall | User | Sys | CPU | Max RSS |
|---------------------|------|------|-----|-----|---------|
| Historical | 314.82s | 878.00s | 159.20s | 329% | 522,588 KiB |
| Current, test-result cache cleared (isolated worktree) | 122.32s | 310.77s | 123.81s | 355% | 494,676 KiB |
| Current, unchanged main checkout (repeat 1) | 103.09s | 126.39s | 89.21s | 209% | 265,184 KiB |
| Current, unchanged main checkout (repeat 2) | 101.61s | 123.86s | 88.07s | 208% | 270,464 KiB |

The uncached isolated-worktree run reduced wall time by 61% and user CPU by 65%. Its original
19.65s immediate-repeat observation did not reproduce on the active main checkout: an
independent post-merge review measured 139.6s, 112.8s, and 102.2s before this follow-up.
After bounding the testguard source walk so live `.liza/` state no longer invalidates its
cache, the two unchanged repeats above cached every test-bearing package except `cmd/liza`
and `internal/integration`; those packages still read run-specific temporary paths during
their tests. The reproducible active-checkout repeat therefore remains above the original
60s target. An isolated final `internal/integration` run completed in 56.226s;
representative full-suite runs completed in 77.932s, while a heat-soaked run immediately
after three uncached race suites took 92.878s.

Exact pre-change/candidate coverage comparison found every package unchanged or higher.
`internal/commands`, the only package whose statement ratio changed, increased from
3140/3924 (80.020%) to 3146/3928 (80.092%). Three consecutive uncached `make test-race`
runs passed in 251.18s, 253.22s, and 251.34s.

`TestSlicedIntegrationLifecycle` fell from 81.30s wall under the historical race-instrumented
measurement to 15.43s without race instrumentation; the test package reported 9.380s.

The conservative `internal/ops` parallelization deliberately excludes tests that mutate
process or package globals. A prebuilt non-race binary produced these execution-only results:

| `internal/ops` scope | Wall | User | Sys | CPU | Max RSS |
|----------------------|------|------|-----|-----|---------|
| Historical full package | 56.42s | 36.3s | 18.0s | 96% | — |
| Current full package | 46.59s | 40.05s | 16.55s | 121% | 67,444 KiB |
| Current 486-test parallel-safe partition | 3.61s | 10.24s | 4.13s | 397% | 32,284 KiB |
| Current 451-test sequential partition | 42.51s | 29.59s | 12.31s | 98% | 67,456 KiB |

The parallel-safe partition exceeds the 300% utilization target, but the full package does
not: Go defers parallel tests until sequential top-level tests finish, and the excluded
partition remains the critical path. Broadening that partition without first removing its
shared-global coupling would trade speed for flaky or false-positive tests.

## Benchmarking

### Commands

```bash
go test -bench=. ./internal/db/                          # All benchmarks
go test -bench=BenchmarkRead ./internal/db/               # Specific benchmark
go test -bench=. -benchmem ./internal/db/                 # With memory stats
go test -bench=. -benchtime=10s -count=5 ./internal/db/   # Multiple runs
```

### Targets

| Operation | Target | Acceptable | Poor |
|-----------|--------|------------|------|
| Read (cached) | < 100us | < 500us | > 1ms |
| Read (uncached) | < 5ms | < 20ms | > 50ms |
| Modify (small) | < 10ms | < 50ms | > 100ms |
| Modify (large) | < 50ms | < 200ms | > 500ms |
| Lock acquire | < 10ms | < 100ms | > 500ms |

### Regression Detection

```bash
go test -bench=. ./internal/db/ > baseline.txt
# ... make changes ...
go test -bench=. ./internal/db/ > current.txt
benchstat baseline.txt current.txt
```

Acceptable variance: +/-10% run-to-run. Investigate if >20% slower.

## Prompt Payload: Assigned-Section Peer Elision

The assigned-section peer-elision change shipped in `a2349382`; this comparison closes its
outstanding identical-fixture, per-role byte measurement. It uses current
templates and the calibrated synthetic carrier set, clearing only
`Carrier.AssignedHeading` in the control. Ancestor-reference pointers and
duplicate-reference suppression remain enabled in both arms. The explanatory
preamble is identical too: these are isolated feature-off/on totals, not a
reconstruction of historical prompts.

Every task variant receives the **same synthetic assigned-fragment payload**.
The absolute saving is therefore uniform by construction; only prompt totals
and percentages vary with role instructions. This measures template response,
not whether real tasks for each role carry a section-fragment reference.
Orchestrator wakes omit the carrier payload and are unchanged. The fixture's
assigned Section 25 has three peers; it does not estimate typical production
section counts or savings.

Measured on 2026-09-21 with the default embedded pipeline and branding:

| Role | Pair variant | Before (bytes) | After (bytes) | Change |
|------|--------------|---------------:|--------------:|-------:|
| architect | specialized | 288,548 | 283,772 | −1.66% |
| architect | decomposition root | 291,018 | 286,242 | −1.64% |
| architecture-reviewer | specialized | 286,015 | 281,239 | −1.67% |
| architecture-reviewer | decomposition root | 288,157 | 283,381 | −1.66% |
| code-planner | specialized | 293,608 | 288,832 | −1.63% |
| code-planner | decomposition root | 287,764 | 282,988 | −1.66% |
| code-plan-reviewer | specialized | 289,445 | 284,669 | −1.65% |
| code-plan-reviewer | decomposition root | 286,402 | 281,626 | −1.67% |
| coder | specialized | 289,235 | 284,459 | −1.65% |
| code-reviewer | specialized | 289,741 | 284,965 | −1.65% |
| epic-planner | specialized | 288,783 | 284,007 | −1.65% |
| epic-planner | decomposition root | 291,253 | 286,477 | −1.64% |
| epic-plan-reviewer | specialized | 287,908 | 283,132 | −1.66% |
| epic-plan-reviewer | decomposition root | 290,050 | 285,274 | −1.65% |
| integration-analyst | integration and slice pairs | 290,220 | 285,444 | −1.65% |
| integration-reviewer | integration and slice pairs | 288,807 | 284,031 | −1.65% |
| us-writer | specialized | 284,586 | 279,810 | −1.68% |
| us-reviewer | specialized | 287,031 | 282,255 | −1.66% |
| orchestrator | all nine wakes | 18,085–27,223 | unchanged | 0% |

Each task row saves **4,776 bytes**, entirely in the resolved reference context.
The [JSON artifact](../internal/prompts/promptbench/testdata/peer-scope.json)
records every pair and wake separately, including zero deltas. Configured
skills, mandatory documents, and specialized/root section lists are rendered;
role-pair metadata is aligned locally in the comparison test. External contract
and skill file reads are not part of these rendered-prompt totals.

Reproduce from the repository root:

```bash
make sync-embedded
go test ./internal/prompts/promptbench -run '^TestPeerScopePayload$' -count=1 -v
```

The control is derived from the treatment by clearing only `AssignedHeading`.
The test checks preservation of assigned/shared content and declared references,
and exact equality of all instructions outside the reference context. The original
`TestBaseline` also remains unchanged. To update the new artifact deliberately
after reviewing a measured change, run the same test with
`PROMPTBENCH_UPDATE=1`; ordinary validation fails if the artifact is missing.

No binding rule or production template was removed. A prior run's reported
four copies of the same coder contract were attributed to artifact inclusion through multiple
routes, addressed by the earlier reference reductions; it does not justify
another contract cut. Duplicate agent-side skill reads remain a separate,
unverified attribution question. Real per-role fragment applicability,
`git show` reread frequency, and cache-read tokens per merged task remain
next-run checks: this synthetic comparison cannot establish cost savings.
