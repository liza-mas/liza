# 113 - Sliced Integration Analysis and Final Closure

## Context and Problem Statement

ADR-0055 introduced one branch-wide integration analysis after coding and
accepted a no-rescan limitation after integration fixes. That topology gives a
single analyst both local composition and aggregate review responsibility, and
it can declare integration complete even though no branch-wide reviewer has
observed the repaired integration HEAD.

Broad goals need bounded local evidence without treating that evidence as a
substitute for an independent global judgment. Completion also needs to remain
correct when an approved task mutates integration HEAD concurrently with final
closure.

## Decision Outcome

Extend ADR-0055 with a two-level integration model: bounded per-plan slice
coverage followed by independent global review and bounded closure rescans.
This decision supersedes ADR-0055's accepted no-rescan limitation; the rest of
ADR-0055 remains the historical basis for the integration sub-pipeline.

### Settled contributing cohort and local coverage

The contributing cohort is evaluated exactly once after pre-integration
planning is settled. Settlement requires every planning source that could
produce coding work to be terminal, every eligible coding-producing output or
transition to be consumed, and every resulting coding lineage to be terminal.
The resulting contributing set is then frozen as goal-scoped persisted state.
Code-planning work created later by integration escalation remains repair
lineage outside the frozen cohort and is covered by a later global generation.

Upstream producers are derived from the frozen pipeline's transition graph,
including epic, story, architecture, and master planning stages. An absent
code-planning task is not proof of settlement. Pending one-to-one and
many-to-one transitions also prevent settlement when their sources have no
`output[]`; an empty per-subtask source produces no children.

If fewer than two plan scopes contribute merged work, the workflow persists no
coverage records, creates no slice analyses, and proceeds directly to global
analysis through the existing valid global integration pair. Only when at
least two scopes contribute does every scope supply one bounded coverage
record:

- A one-lineage scope reuses its coding-review approval attestation and creates
  no slice. The attestation records the reviewed task and acceptance criteria,
  reviewed commit, approver, validation commands, and merge commit. It records
  one-lineage approval attestation facts without persisted reviewer reasoning.
- A qualifying multi-lineage scope with at least two distinct merged coding
  lineages receives exactly one slice analysis. Its deterministic analysis key
  prevents duplicate materialization.

Slice metadata is bounded immutable coverage evidence: it identifies the
originating plan and root lineages, attributes merged descendant commits,
records affected paths and the source snapshot paths, and binds the verdict to
an immutable source commit and reviewed report commit. Slice analyses can run
concurrently. A later sibling mutation does not reopen a completed slice;
overlapping or cross-scope effects belong to independent global review.

A clean slice resolves immediately. Findings resolve only through merged fix
or replacement leaves. Blocked or abandoned repair leaves fail closed rather
than allowing global analysis to begin.

### Independent global review and bounded rescans

Global analysis waits for settled planning and terminal coding and integration
repair work. When the frozen cohort has at least two scopes, it also waits for
complete coverage for every contributing scope and resolution of every created
slice; a zero- or one-scope cohort has no coverage records to await. The
coverage map, when present, is navigation evidence, not proof that the
aggregate branch is correct. The global analyst independently reviews the
current goal-wide integration source for cross-scope interactions,
specification and test agreement, architectural drift, and merge readiness.

Each global generation has an ordered deterministic key, immutable source
commit, reviewed report commit, and clean-or-findings verdict. Findings and any
later integration-HEAD mutation require another global generation while the
configured generation budget remains. The persisted generation limit is
normalized to a deterministic default, and reaching it without clean evidence
for current HEAD records explicit exhaustion rather than success.

Reconciliation is idempotent. Deterministic slice and global analysis keys and
collision checks make repeated wakes and restart recovery reuse an identical
materialization instead of creating duplicate analyses. Approval coverage,
slice reports, global generations, and mutation receipts are append-only
evidence.

### Recovery of a premature initial empty cohort

The operator-only `recover-integration` command repairs the historical case
where global generation 1 started before upstream planning settled. It requires
a paused run, an empty frozen cohort, unfinished upstream planning, a clean
submitted initial analysis, and no accepted integration verdict, coverage, or
repair descendants. A dry run exposes the proposed recovery without mutation.

Recovery preserves the report under a durable Git ref, retires the analysis
through the task lifecycle, and atomically records an immutable recovery receipt
with the prior empty cohort and report/source identity before clearing that
cohort. Task metadata and historical evidence remain intact. Cleanup follows
preservation and the state transaction; unrelated task work is retained.
Repeated recovery returns the existing receipt. The next global identity is
generation 2, and only valid reviewed generations consume the configured budget.
Ordinary lifecycle mutations still cannot clear a frozen cohort or alter the
receipt. All state writers must run a recovery-aware engine before this repair.

### Clean current-HEAD completion and linearizable finalization

A clean global verdict is effective only when its immutable reviewed source
commit equals live integration HEAD. The clean closure records that generation,
analysis key, and source commit; progression and terminal consumers re-evaluate
those facts against the current HEAD instead of inferring completion from task
terminal states.

Finalization and integration-HEAD mutation have one linearizable order. Clean
source verification reads live HEAD under the integration mutation lock.
Cooperating progression and mutation paths are ordered by the project-scoped
integration-completion lock, and progression rechecks closure identity and the
mutation-receipt count before committing its state change. A mutation ordered
before finalization prevents clean completion. A mutation ordered after
finalization appends a receipt and performs mutation-side invalidation, making
the stale closure and any goal-complete stop non-successful at the mutation's
linearization point; reconciliation then requests the next bounded global
generation.

This protocol preserves ADR-0112. Lock ordering is integration completion,
then integration mutation, then blackboard read. The integration mutation lock
is released before receipt, invalidation, closure, or progression state is
persisted: there is no blackboard write while the mutation lock is held.

### Frozen pipeline compatibility

When a frozen cohort with at least two scopes requires a slice, the frozen
pipeline is capability-checked for the slice pair, global pair, coding repair
step, clean states, and both findings-to-fix transitions. A qualifying cohort
whose frozen pipeline lacks that required topology fails closed with
`pipeline_upgrade_required`; operators must use a fresh workspace or perform a
manual topology update. A zero- or one-scope cohort requires neither coverage
records nor sliced-integration capability and may use the direct-global bypass
through an existing valid global integration pair, even when the unused slice
topology is incomplete.

## Consequences

**Positive:**

- Local composition review receives a bounded surface while global review
  remains independently responsible for aggregate correctness.
- Exactly-once cohort freezing and idempotent materialization make concurrent
  wakes and restart recovery deterministic.
- Clean completion is evidence about the current integration HEAD, and later
  mutations cannot leave durable success tied to stale evidence.
- Explicit blocked and exhausted outcomes replace indefinite barriers and
  false terminal success.

**Trade-offs:**

- Goals with multiple contributing scopes can add one slice pair per
  qualifying multi-lineage scope and multiple bounded global generations.
- Persisted coverage and mutation receipts add lifecycle state in exchange for
  reproducibility and fail-closed recovery.
- Legacy frozen pipelines need an explicit topology upgrade before a qualifying
  cohort can materialize required slices.

## Relationship to Prior Decisions

- **Extends ADR-0055:** retains its integration roles, reviewed findings, and
  coding-pair repair lifecycle while replacing the single pass with sliced
  coverage and bounded global closure.
- **Supersedes ADR-0055's no-rescan limitation:** every repaired or otherwise
  mutated integration HEAD must receive another global analysis within budget.
- **Preserves ADR-0112:** integration ref/index mutation remains serialized,
  retains its lock order, and performs no blackboard state write until after
  releasing the mutation lock.
