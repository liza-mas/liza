# ADR-0215: Archived History Leaves the State-Lock Hold

## Status

ACCEPTED — 2026-10-22. Amends ADR-0183 §7 (archival default) and refines its
cache and validation cost model.

## Context

Terminal archival (ADR-0183) shrank `state.yaml`, yet state-lock timeouts
continued in long runs (D-189). On a run with 1166 archived tasks (3 MB live
state, 58 MB of objects), a `Modify` from a fresh process held the lock for
7-20 s at host load 32-35, beyond the 10 s acquisition wait. Inside that hold,
restoring archived tasks (decoding every object on a cache miss) took 43% of
the CPU, hygiene validation of the whole logical state 26%, the live YAML 19%.
CLI commands are fresh processes, and instances derived for fenced requests or
patient waits started with an empty decoded-object cache. ADR-0183 had warned
that smaller YAML alone does not prove contention relief.

Pool auto-repair read state with an exclusive locked read for an observation,
and its timeouts were most lock alerts. Prompts told agents to retry a
structured `safe_action=retry` "under the bounded retry policy", which nothing
defined, so doers blocked finished work after two or three attempts.

## Decision

1. **Decode archived objects before taking the lock.** Each `Modify` first
   reads the published state without the lock. A byte scan of its
   `terminal_archive` digests decides, as a hint only, whether any is not yet
   decoded and validated by this process; if so, the state is parsed and those
   objects are verified, decoded and hygiene-validated. The locked transaction
   then finds them in the cache. It still reads every object and checks digest,
   envelope and identity on every read (ADR-0183 §4), so evidence changed after
   the warm-up fails as before; objects published after the warm-up decode
   under the lock. Warm-up errors are left to the locked path. A cancelled
   fenced request stops warming and returns before acquisition.
2. **Derived instances share the decoded-object cache.** Instances derived
   with a lock timeout, patient wait or retry context use their parent's cache.
3. **Validate an unchanged archived object once per process.** A cache entry
   records that its payload passed hygiene validation. A write validates such a
   row as its stub when its payload equals the entry; every other row
   (changed payloads, cache misses, objects the cache refused, rollback rows) is
   validated in full at its own index. First use always validates.
4. **Observation reads stay off the lock.** Pool repair reads a snapshot; each
   start still registers under the lock, where max-instances is enforced.
5. **Longer ordinary state-lock wait.** The state lock's ordinary acquisition
   wait is 30 s (other file locks keep 10 s). Patient waits (60 s) and fenced
   retries are unchanged.
6. **A defined retry schedule.** Agents repeat a request returned with
   `safe_action=retry` serially after about 30 s, 1, 2 and 4 minutes. Retry
   does not name its cause (state lock, admission slot or runtime-input check),
   so exhaustion stops and reports; it never adds a state-lock-timeout tag or
   enters the no-result CLI failure recovery.
7. **New runs archive terminal tasks.** Initialization sets
   `config.terminal_task_archival`; post-merge maintenance then archives
   continuously. Existing runs keep their setting.

## Consequences

- A fresh process's transaction pays object reads and digests under the lock,
  not their decoding; unchanged archived rows cost a comparison instead of
  validation. The decoding moves to before acquisition: one extra unlocked
  parse of the live state when a digest is new to the process.
- A stuck holder is reported after 30 s instead of 10 s; a registration holding
  its per-agent lifecycle lock may wait that long for the state lock.
- Objects the bounded cache refuses are decoded and validated on every write,
  as before.
- A binary that predates ADR-0183 cannot run on a new run's state; all of a
  run's processes must use one binary (unchanged requirement).
- Live-state growth (notes, anomalies, circuit-breaker history, validation
  readiness, integration records) remains in every transaction (TECH_DEBT).

## Alternatives Considered

- Only lengthen the wait: holds keep growing with history.
- Transactions that hydrate archived tasks lazily: removes the remaining
  per-row cost but changes ADR-0183's complete-logical-task boundary.
- Tag exhausted retries as state-lock holds: retry does not identify its cause,
  so the tag would misclassify admission and runtime-input refusals.
