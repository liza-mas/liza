# ADR-0177: Heartbeats Publish Liveness Side Records Instead of Rewriting State

## Status

ACCEPTED — 2026-10-05.

## Context

Every state mutation runs `Blackboard.Modify`. Under the exclusive state lock it
reads, fully decodes, validates, marshals, re-parses and fsyncs the whole of
`state.yaml`. The supervisor heartbeat was such a mutation: every 60 s per
agent, it rewrote the full state to change at most four timestamps:

- the agent's `heartbeat` and `lease_expires`;
- the `lease_expires` of the task the agent is assigned;
- the `review_lease_expires` of the task under its active provider session
  (ADR-0174).

With a 5.8 MB state on a loaded host, a single mutation held the lock for
1.5–12 s. A paused run with 7 idle supervisors kept the lock 89 % occupied,
past the 10 s acquire timeout. Claims, submits and merges were starved (operator
defect D-41, incidents I-180 to I-192).

About 60 source files read those liveness fields. They include correctness
paths: claim takeover, stale review clearing, registration collision, watch
recovery, and validation.

## Decision

1. **Record per generation.** A heartbeat publishes a JSON record at
   `<state path>.liveness-<first 16 hex of sha256(agent ID, NUL, generation)>`.
   It uses a unique temp file and an atomic rename, takes no state lock and
   does no fsync. The record holds the agent ID, generation, a `seq`, the
   heartbeat, the lease, and its latest review renewal (task, lease, and the
   `seq` of the beat that made it). Before publishing, the beater checks its
   authority against a lock-free snapshot with
   `ops.CheckAgentAuthoritySnapshot`. That check is advisory: it stops a fenced
   supervisor and authorizes nothing. Writer isolation comes from the path. A
   delayed beat from a replaced generation can publish only to its own file,
   which readers no longer consult.
2. **Overlay at decode.** `Read`/`ReadContext`, `ReadSnapshot`, `ReadCached`
   (on the copy it returns, never in the cache) and `Modify` overlay each
   agent's current-generation record, when its `seq` is above the row's
   persisted `liveness_seq`. The overlay applies exactly the effects of the
   former locked beat:
   - the agent heartbeat and lease;
   - the lease of the task assigned to the agent, if non-nil;
   - the review lease of the renewal's task, if non-nil and while the renewal's
     `seq` is above `liveness_seq`.

   Pre-image decodes for write checks, and `ReadRaw`, see the stored bytes only.
3. **Fold by sequence.** `Modify` overlays before its callback and persists the
   result. Every ordinary mutation therefore folds pending records into
   `state.yaml` and stores their `seq` in `liveness_seq`, at no extra write.
   The record read under the state lock is the linearization point:
   - a beat published before that read is folded, and the callback's own
     changes win over it (for example, a release clearing the agent lease);
   - a beat published after it has a higher `seq` and applies on top of the
     commit.

   Each beat replaces the previous record, so the latest review renewal is
   carried forward until a fold reaches its `seq`. It applies once, as a
   locked beat did.
4. **Cleanup.** Registration (`registerAgentLocked`), under the state lock,
   removes published record names that match no `(ID, generation)` of its
   candidate rows. Temp files never match the published-name shape.
   - A replaced generation that publishes after the sweep leaves one orphan.
     It is never read, and the next registration removes it.
   - If the registration transaction then fails, at most the registering
     agent's previous record is lost. That agent loses the beats since its
     last fold, until its next beat (60 s, against a 30 min lease).
5. **Upgrade rule.** Binaries without this ADR neither overlay nor fold
   records. A new-binary agent's liveness reaches them only through
   `state.yaml`, as of the last mutation made by a new binary. After about one
   lease (30 min) without such a mutation, an old process can see a live agent
   as expired. It could then take over its task, clear its review claim, or
   reuse its ID. An upgrade must therefore restart every supervisor, watch and
   TUI of a run together; old and new binaries must not run side by side.
   `state.yaml` stays readable both ways: `liveness_seq` is optional, and older
   binaries keep it through the agent's inline extra fields.

## Consequences

- A heartbeat costs one lock-free snapshot parse and one small file write.
  It no longer holds the state lock or rewrites the state.
- All in-repo readers observe the same logical liveness as before, through the
  db read paths. Tools that parse `state.yaml` directly see heartbeats and
  leases only as fresh as the last mutation.
- `INVARIANTS.md` §5 states the narrow exception to "all state modifications
  under the exclusive lock". Canonical lifecycle mutations stay locked.
- Other housekeeping mutations still rewrite the full state under the lock.
  The effect on live-run lock occupancy is unmeasured. D-41's other fixes
  (dropping the post-marshal re-parse, archiving terminal tasks, attributing
  lock holders, partitioning) remain open.

## Alternatives Considered

- **Each reader merges a second source.** About 60 files, with a high risk of
  missing a correctness-path reader.
- **Gate records on the state file mtime.** Under saturation, most beats fall
  inside some mutation's decode-to-publish window and would be dropped. The
  sequence read under the lock has no such window.
- **One record per agent ID.** A delayed beat from a replaced generation could
  overwrite the current generation's record.
- **Periodic locked fold by the heartbeat** (one locked write per agent per
  half lease). This would keep old binaries and raw readers fresh, at a
  fraction of the former cost. Rejected in favor of the restart-together
  upgrade rule.
