# Reference-First Planning Artifacts — Implementation Evidence

Date: 2026-09-11
Goal: [Prevent Redundant Spec Reading](20260909-prevent-redundant-spec-reading.md)
Decision: [ADR-0133](../architecture/ADR/0133-reference-first-planning-artifacts.md)

## Implementation Boundary

The change is prospective. New epic, story, architecture, and code-plan artifacts emit the strict
`Source References` contract. Marker-free artifacts retain legacy routing. No orchestration state
field, pipeline topology, review quorum, or bulk artifact migration is introduced.

The prompt compositor captures integration HEAD once, discovers strict carriers from task scalar
refs, complete direct-parent reviewed ranges, and the current review range, then validates every
observation before applying current-review > parent > scalar rendering precedence. Mechanical
failures wrap the existing context-build sentinel so the supervisor blocks before provider launch.

## Planning-Path Pilot

The focused pilot is `TestBuildPromptReferenceFirstPlanningPilot`. It uses a disposable Git
repository and verifies a strict producer-to-consumer path with exact Unicode/punctuation heading
text, obligation coverage, bounded resolved spans, marker-free compatibility, and stale-source
rejection. Final command/output is recorded under Validation Results.

Artifact acceptance checks:

- local statements are owned decisions, declared references, or labeled action-boundary values;
- inherited obligations map to declared reference IDs;
- mapped exact-heading spans contain the assigned obligations;
- no broad undeclared ancestor read is required;
- stale or ambiguous strict references fail before prompt launch; and
- marker-free artifacts retain their former prompt routing.

## Controlled CAP-001 Reconciliation

Disposable fixture root: `/tmp/prsr/` (not a repository dependency).
Source revision: `1691031b9c9c58ac814b4b3e63ff430beaec6dfe`.

The manifest contains 13 source-to-rewrite artifact mappings. All JSON documents parse. References
are repository-relative, declared anchors resolve, ST-004 remains actionable from its leaf carrier
and direct references, and a prose-only cross-file scan found no duplicated run of 40 words.

| Payload | Source | Rewrite | Reduction |
|---|---:|---:|---:|
| Planning artifacts | 489,020 bytes / 57,061 words | 27,257 bytes / 2,731 words | 94.43% bytes / 95.21% words |
| Task-variable rendered context | 15,816 bytes / 1,719 words | 4,954 bytes / 533 words | 68.68% bytes / 68.99% words |

The reconciliation preserves all ST-004 ACs, selected-identity forms, commitment evidence,
locking/race behavior, zero-write proof, atomic-source behavior, file ownership, exclusions, and
validation commands. Lateral CAP-002/CAP-003 material remains referenced, no p95 threshold is
invented, and both required 50% thresholds pass.

## Validation Results

- Focused parser, Git, prompt, runtime, and pilot packages passed with
  `go test ./internal/agent ./internal/git ./internal/referencecontract ./internal/ops ./internal/prompts ./internal/embedded ./internal/brandrender ./internal/commands -count=1`.
  The independent integration package run also passed with
  `go test ./internal/integration -count=1`.
- `pre-commit` passed on every touched source and documentation file.
- `make test-fast`, `make check-embedded`, `make test`, `make test-race`, and
  `make test-e2e` passed. The race run exercised every changed Go package; the
  end-to-end run completed `internal/integration` in 85.732 seconds.
- Non-default-brand coverage passed with
  `go test ./internal/brandrender ./internal/embedded -run 'TestExpectedEmbeddedFilesUseNonDefaultBrand|TestArtifactConsistencyRendersNonDefaultBrand' -count=1`.
- Touched contracts, skills, formats, and templates grew from 143,905 to
  162,958 bytes (+19,053). The increase is concentrated in the shared contract,
  stage-specific enforcement, and the resolved-reference prompt block; the CAP
  reconciliation above independently demonstrates the resulting reduction in
  rendered planning context.
- The CAP reconciliation parsed seven JSON files, resolved 17 declared
  references and anchors without error, found no repeated 40-word prose run
  across nine prose artifacts, and matched all 13 manifest mappings to the
  independently reconstructed source artifacts at the recorded revision.

Reviewer certification is supplied by the adversarial-pairing code-review rounds recorded in
`specs/adversarial-pairing/20260909-prevent-redundant-spec-reading-bb.md`.
