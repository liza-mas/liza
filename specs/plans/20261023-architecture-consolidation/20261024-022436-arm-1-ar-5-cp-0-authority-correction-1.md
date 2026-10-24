# Code Plan: A5 Oracle Authority Correction

## Source References
Source revision: "ce525e865a837857e2c0a8340d2f26785667c539"

### Direct References
- "a5-scope": "specs/arch-plan/20261023-architecture-consolidation/20261023-182904-arm-1-ar-5.md#Scope 1: A5 oracle suite" @ "ce525e865a837857e2c0a8340d2f26785667c539"
- "a5-context": "specs/arch-plan/20261023-architecture-consolidation/20261023-182904-arm-1-ar-5.md#Context" @ "eae3edae19f55e5fbf712b764986f9a34eccc8a5"
- "a5-probes": "specs/arch-plan/20261023-architecture-consolidation/20261023-182904-arm-1-ar-5.md#Reality Probes" @ "eae3edae19f55e5fbf712b764986f9a34eccc8a5"
- "a5-inventory": "specs/arch-plan/20261023-architecture-consolidation/20261023-182904-arm-1-ar-5.md#Surface Inventory" @ "eae3edae19f55e5fbf712b764986f9a34eccc8a5"
- "a5-components": "specs/arch-plan/20261023-architecture-consolidation/20261023-182904-arm-1-ar-5.md#Components" @ "eae3edae19f55e5fbf712b764986f9a34eccc8a5"
- "a5-interfaces": "specs/arch-plan/20261023-architecture-consolidation/20261023-182904-arm-1-ar-5.md#Interfaces" @ "eae3edae19f55e5fbf712b764986f9a34eccc8a5"
- "a5-dataflow": "specs/arch-plan/20261023-architecture-consolidation/20261023-182904-arm-1-ar-5.md#Data Flow" @ "eae3edae19f55e5fbf712b764986f9a34eccc8a5"
- "a5-determinism": "specs/arch-plan/20261023-architecture-consolidation/20261023-182904-arm-1-ar-5.md#Determinism Rules" @ "eae3edae19f55e5fbf712b764986f9a34eccc8a5"
- "a5-crosscutting": "specs/arch-plan/20261023-architecture-consolidation/20261023-182904-arm-1-ar-5.md#Cross-Cutting Concerns" @ "eae3edae19f55e5fbf712b764986f9a34eccc8a5"
- "harness": "specs/arch-plan/20261023-architecture-consolidation/20261023-172626-arm-1-ar-0.md#Scope 3: Oracle harness and regeneration entry point" @ "53e8fdad0578be4019b27655d34e7bc100a284ce"
- "foundation": "specs/arch-plan/20261023-architecture-consolidation/20261023-172626-arm-1-ar-0.md#Foundation Decisions" @ "53e8fdad0578be4019b27655d34e7bc100a284ce"
- "provider-defaults": "specs/plans/20261023-architecture-consolidation/20261023-184056-arm-1-ar-0-cp-2.md#Local decisions (owned by this plan)" @ "6bfc3929fd0a60d2c5e6fa45b9e92fafe1bd0e85"
- "prepared-project": "specs/plans/20261023-architecture-consolidation/20261023-222338-arm-1-ar-0-cp-2-provider-correction-1.md#C-A5: Caller-prepared project execution" @ "0ee8b48287bf1ccac353eb5a6c68534fc1cb0f1d"
- "provider-asks": "specs/plans/20261023-architecture-consolidation/20261023-222338-arm-1-ar-0-cp-2-provider-correction-1.md#Provider Ask Reconciliation" @ "0ee8b48287bf1ccac353eb5a6c68534fc1cb0f1d"
- "goal-frozen": "specs/goals/20261023-architecture-consolidation.md#Frozen Surfaces" @ "112ccec9cd3a5f32003f043b6c06789cb0b4181e"
- "goal-tests": "specs/goals/20261023-architecture-consolidation.md#Test Policy and Sequencing" @ "112ccec9cd3a5f32003f043b6c06789cb0b4181e"
- "goal-proof": "specs/goals/20261023-architecture-consolidation.md#Proof Environment" @ "112ccec9cd3a5f32003f043b6c06789cb0b4181e"
- "goal-success": "specs/goals/20261023-architecture-consolidation.md#Success Criteria" @ "112ccec9cd3a5f32003f043b6c06789cb0b4181e"
- "goal-changes": "specs/goals/20261023-architecture-consolidation.md#Behaviour Changes (closed list)" @ "112ccec9cd3a5f32003f043b6c06789cb0b4181e"
- "guard": "GUARDRAILS.md#Tier 1 (Hard Constraints)" @ "112ccec9cd3a5f32003f043b6c06789cb0b4181e"
- "shared-files": "specs/arch-plan/20261023-architecture-consolidation/20261023-162147-arm-1.md#Shared-File Ownership" @ "74212192df00f1ba028f1af7dd4bed8cbc66b87d"
- "oracle-budget": "specs/arch-plan/20261023-architecture-consolidation/20261023-162147-arm-1.md#SI-3 Oracle Harness" @ "74212192df00f1ba028f1af7dd4bed8cbc66b87d"

### Obligation Coverage
- "AC-SR1" -> "a5-inventory"
- "AC-SR2" -> "a5-inventory"
- "AC-AE1" -> "a5-inventory"
- "AC-AE2" -> "a5-inventory"
- "AC-AE3" -> "a5-inventory"
- "AC-SV1" -> "a5-inventory"
- "AC-SV2" -> "a5-inventory"
- "AC-SV3" -> "a5-inventory"
- "AC-SV4" -> "a5-inventory"
- "AC-SV5" -> "a5-inventory"
- "AC-SV6" -> "a5-inventory"
- "AC-AV1" -> "a5-inventory"
- "AC-AV2" -> "a5-inventory"
- "AC-AV3" -> "a5-inventory"
- "AC-AR1" -> "a5-inventory"
- "AC-AR2" -> "a5-inventory"
- "AC-AR3" -> "a5-inventory"
- "AC-RC1" -> "a5-inventory"
- "AC-RC2" -> "a5-inventory"
- "AC-RC3" -> "a5-inventory"
- "AC-RC4" -> "a5-inventory"
- "AC-RC5" -> "a5-inventory"
- "AC-RCA1" -> "a5-inventory"
- "AC-RCA2" -> "a5-inventory"
- "AC-RCA3" -> "a5-inventory"
- "AC-RCA4" -> "a5-inventory"
- "AC-VP1" -> "a5-inventory"
- "AC-VP2" -> "a5-inventory"
- "AC-UR1" -> "a5-inventory"
- "AC-UR2" -> "a5-inventory"
- "AC-CS1" -> "a5-inventory"
- "AC-CS2" -> "a5-inventory"
- "AC-RV1" -> "a5-inventory"
- "AC-RV2" -> "a5-inventory"
- "AC-RP1" -> "a5-inventory"
- "AC-RP2" -> "a5-inventory"
- "AC-AA1" -> "a5-inventory"
- "AC-AA2" -> "a5-inventory"
- "AC-RA1" -> "a5-inventory"
- "AC-RA2" -> "a5-inventory"
- "AC-inventory" -> "a5-inventory", "a5-scope"
- "AC-capture" -> "a5-scope", "harness", "goal-frozen"
- "AC-preparation" -> "a5-scope", "a5-dataflow", "prepared-project"
- "AC-entrypoints" -> "a5-scope", "a5-interfaces"
- "AC-determinism" -> "a5-determinism", "a5-crosscutting"
- "AC-isolation" -> "a5-context", "a5-determinism", "goal-proof", "guard"
- "AC-adapters" -> "harness", "a5-components"
- "AC-provenance" -> "a5-scope", "harness", "goal-proof"
- "AC-sensitivity" -> "a5-scope"
- "AC-failure-signals" -> "a5-crosscutting", "harness"
- "AC-boundary" -> "a5-scope", "a5-context", "shared-files"

## Correction and Replacement Lineage

This plan reauthors the allocation from
`specs/plans/20261023-architecture-consolidation/20261024-012644-arm-1-ar-5-cp-0.md`
at `be0c4d4ca9b896017b1885016da71a815821fe73`. It changes the consuming
authority, following applied, independently reviewed architecture amendment
`arm-1-ar-5-amend-2`; it does not amend the old plan in place.

The new `a5-scope` pin is
`ce525e865a837857e2c0a8340d2f26785667c539`. The other 21 direct-reference
sections are byte-identical to their original pins. All 40 case IDs, modes and
outcomes, 51 acceptance obligations, captures, additive ownership, provider
requirements, canonical command order, 1800-second estimated budget, deferred
indices 4–7, and independent AC-boundary proof are preserved. The Acceptance
Contract JSON is unchanged; its approved proof now resolves through the newly
reviewed scope pin.

Exactly **one** output replaces **arm-1-ar-5-cp-0-code-0**, using structured
`supersedes` and nonempty `changed`. The original architecture and
`arm-1-ar-5-cp-0` provider remain valid and are not superseded. Selected
inheritance excludes the old plan's blocked child; no dependency on that child
is introduced. The already merged architecture supplies the corrected contract;
the already merged original plan supplies the complete allocation to preserve,
so neither needs an additional downstream barrier.

Narrower alternatives were checked: `allocationProofPolicy` retains the own
allocation's approved-proof IDs, and `ResolveDirectReferences` refuses changed
sections under those pins. `observeProofDrift` compares declared references
in the reviewed and current carrier, both still naming the old pin, so it has
nothing to reaffirm. The old task's history records that refusal; this planner
does not invoke an orchestrator-only repair. ADR-0198 Decision 2 restricts
expanded identity-preserving amendments to architecture and forbids changing
accepted resolved proofs. Dependency repair or scope exceptions cannot repin a
reviewed carrier. A new reviewed allocation is therefore the bounded recovery.

Normal child generation must atomically retain individual supersession lineage,
retire the blocked original and retarget its consumers, or change nothing
(`internal/ops/plan_replacement.go`). Independent plan approval and merge alone
are not A5 delivery. After normal generation, the successor must reach RA1/RA2's
original outcomes and then pass all original proofs. Generation and coding are
downstream lifecycle work, not actions performed by this planning task.

## Intent and Allocation

Based on: the applied A5 carrier and all original direct-reference spans; the
full goal; the original reviewed allocation; merged provider plans and acceptance
receipt; current reference-policy, repair-entry and harness source; targeted task
state and history; archive inventory; and the probes below. No production
behavior is designed here.

Success means one reviewed replacement allocation authorizes the complete A5 oracle under the corrected scope.
Validation is the exact inventory-to-proof allocation, strict source/payload
validation, pre-commit, a clean submission commit, and independent plan review.
Doc Impact: this plan and its output JSON. Test Impact: no planner test changes;
Task 1 owns all new executable proofs and its manifest.

**Priority:** Must, inherited from a5-scope. H7's runtime target remains Should
and has a nonblocking measurement/disposition below. No optional capability is
promoted into a prerequisite. No architecture, existing oracle, production,
ledger, or installed binary change is allocated.

One fixed-cost bundle is appropriate: the 40 cases share a package driver,
materialisation, capture and normalization, and jointly constitute the only
required deliverable. A kit-first chain would have no independently needed
intermediate artifact; group splits would share driver definitions and require
ordering or another registration mechanism. Keep implementation files separated
by concern for review, within one coding unit. This is not a feature/refactor mix.

## Provider and Dependency Reconciliation

No epic exists for this run; provider-asks and a5-interfaces carry the reconciled
SI-3 asks. This plan consumes the already reviewed interface; it freezes none.

| Ask / reference | Delivered answer | Earliest required input |
|---|---|---|
| CLI binary, JSON/human/state forms, golden comparison and missing-golden refusal / a5-interfaces | Existing harness API and provider-defaults | arm-1-ar-0-cp-2 output 0, merged harness |
| Starting-tree write-new and bounded regeneration / harness | Existing regen with -mode and -pkg | arm-1-ar-0-cp-2 output 1, merged entry point and ledger |
| Reusable prepared project, ordered CLI preparation, explicit environment / prepared-project | RunInProject with EnvBase and RunOptions | arm-1-ar-0-cp-2-provider-correction-1 output 1, merged implementation |
| A5-specific labels, leases and elapsed preprocessing / a5-determinism | Package-local driver before harness normalization | Task 1; no provider change |
| Supervisor-only entry calls / a5-interfaces | Untagged adapter files, with starting-tree rule from harness | Task 1 |

Task 1 explicitly selects original provider outputs **[0,1]** and correction
output **[1]** through inherit_inputs. Both upstream plans are current dependencies
and MERGED. The correction's output 1 transitively requires its output 0 for the
shared starting-tree proof helper; that is a Must shared prerequisite, not a new
A8 capability requirement. Correction output 2 supplies nothing to A5 and is not
selected. No sibling edge, cross-stage provider wait, or whole-phase barrier is
needed. All selected providers are Must; no unchecked optional path remains.

The earlier prepared-project provider blocker was closed by the merged API and receipt
(RP2/RP3), not by the unblock narrative alone. Case reachability remains exactly
the final coding-stage hold in a5-scope and a5-probes: a prospective case that
cannot reach its stated outcome at start blocks Task 1 naming that ID. No case
may be silently replaced, dropped, or turned into a different refusal.

## Reality Probes

Current integration snapshot and clean task branch:
**ce525e865a837857e2c0a8340d2f26785667c539**, inspected
2026-10-24T02:24:36Z. The table distinguishes fresh observations from inherited
receipts. Commands use this assigned worktree; OMNI queries select the project
state only. No invocation of a live repair or old coding worktree occurred.

| ID | Integration SHA / command or source | Observation |
|---|---|---|
| C1 | ce525e865…; git log -1 --format='%H %D'; git status --short | Clean task/arm-1-ar-5-cp-0-authority-correction-1 at the applied amendment commit |
| C2 | ce525e865…; omni-ee get arm-1-ar-5 --json and get arm-1-ar-5-amend-2 --json | Amendment MERGED at ce525e865…; original's applied list includes amend-2, no pending fence |
| C3 | ce525e865…; git diff cd3a741f968f8f681e47edeca85a7c1f101771fc ce525e865a837857e2c0a8340d2f26785667c539 -- specs/arch-plan/20261023-architecture-consolidation/20261023-182904-arm-1-ar-5.md | Only CONTRACT Entry points changes: RA1/RA2 operator flag/environment exception; inventory unchanged |
| C4 | ce525e865…; source read internal/agent/reference_context.go allocationProofPolicy; internal/referencecontract/resolve.go ResolveDirectReferences; internal/ops/reaffirm_proof.go observeProofDrift; ADR-0198 Decision 2 | Own approved-proof drift refuses; unchanged declared pin cannot be reaffirmed; expanded nonarchitecture amendment unsupported |
| C5 | ce525e865…; omni-ee get arm-1-ar-5-cp-0-code-0 --json and its history | BLOCKED with original be0c4d4… acceptance source; history records reaffirm refusal and 52-file preservation, not oracle success |
| C6 | ce525e865…; compare each original pinned exact section via git show with worktree section | Only a5-scope differs; remaining 21 spans unchanged; original plan equals reviewed be0c4d4… bytes |
| C7 | Starting 96c9ad69aee983ce2b929dd681b6ba43bf920535; git show cmd/liza/cmd_init.go lines 438–463, 678–680 and cmd/liza/main.go lines 308–318 | Repair entry dispatches directly, registers only integration/dry-run and no inherited agent-id; no runtime success inferred |
| C8 | ce525e865…; read internal/testhelpers/oracle/cli.go and env.go; provider output summaries; get arm-1-ar-0-cp-2-provider-correction-1-code-1 --json | RunInProject and ChildEnv support caller-owned directories and explicit env; same selected provider outputs MERGED; receipt a830ae229606199e85ba134da53b7da7a608053c proves prepared-project and start execution |
| C9 | ce525e865…; read internal/testhelpers/oracle/cmd/regen/main.go; inherited provider receipt command 3 | Exact clean A5 package filter accepted by source; sample H1 regeneration passed 2026-10-24T00:54:13Z. A5 goldens and commands 0–3 remain PROSPECTIVE, Task 1; canonical flags/setup grounded by inherited V0–V3 below and provider receipt, not new A5 executions |
| C10 | ce525e865…; env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp make -n lint test test-e2e test-race | Exit 0; selects sync, platform vet, archguard, full tests, named race/e2e selection and full race -timeout 30m -p 2. Bounded probe of commands 4–7; no regression-suite pass claimed |
| C11 | ce525e865…; read .pre-commit-config.yaml | Markdown/JSON activates text and check-json hooks; Go hooks apply only to downstream Go files |
| C12 | ce525e865…; read internal/ops/plan_replacement.go and set_task_output.go validateOutputSupersedes | Structured supersedes/changed required; same-pair eligible original; generation retires/retargets in one validated candidate; original cannot be child dependency |
| C13 | ce525e865…; Python tarfile inventory and SHA256 of the named preserved archive, without extraction | 52 regular A5 files, 40 case manifests, no goldens; checksum in reuse instructions. Contents not validated as an oracle |

### Inherited Original Probes and Budget

The following evidence belongs to the original reviewed plan at
be0c4d4ca9b896017b1885016da71a815821fe73. It is retained as historical
measurement/receipt evidence, not claimed as rerun by this correction.
Original worktree snapshot: **cd3a741f968f8f681e47edeca85a7c1f101771fc**,
observed 2026-10-24T01:26:44Z. Commands below run in the assigned worktree;
root-level OMNI queries select state only. EVIDENCED means a source read,
command result, or the explicitly identified receipt; PROSPECTIVE means Task 1
must implement and prove it. The inherited starting probes remain at a5-probes,
not recast as fresh executions.

| ID | Command or source at this snapshot | Observation |
|---|---|---|
| RP1 | git rev-parse HEAD; git status --short --branch | cd3a741f…; clean task/arm-1-ar-5-cp-0 |
| RP2 | Read internal/testhelpers/oracle/cli.go and env.go | RunInProject validates absolute existing project/HOME/TMPDIR, reuses executeCLI and ChildEnv, captures stdout/stderr/exit separately; caller owns sequencing and directories; EnvBase.Go=false for tested CLI |
| RP3 | omni-ee get arm-1-ar-0-cp-2-provider-correction-1-code-1 --json | MERGED; acceptance review a830ae229606199e85ba134da53b7da7a608053c. Sequence/isolation/environment/directory and starting-tree mappings passed; harness race passed; sample H1 regeneration passed at 2026-10-24T00:54:13Z |
| RP4 | Targeted output summaries of both providers; git diff --quiet 0ee8b48287bf1ccac353eb5a6c68534fc1cb0f1d HEAD -- specs/plans/20261023-architecture-consolidation/20261023-222338-arm-1-ar-0-cp-2-provider-correction-1.md | Output indexes [0,1] and correction [1] resolve; correction file diff exit 0 |
| RP5 | Read internal/testhelpers/oracle/kind.go and golden.go | JSON preserves numbers and requires one document; Human preserves line/token order; CheckState builds before/after; WriteEnv=ORACLE_WRITE; live Check fails on missing/different golden |
| RP6 | Read internal/testhelpers/oracle/cmd/regen/main.go; ADR0219 Decision | -pkg accepts clean oracle paths; -mode check/write-new; committed ledger authority; selected packages passed to regeneration |
| RP7 | env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp make sync-embedded | Files synced successfully; required worktree setup completed |
| V0 | env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp go test -count=1 ./internal/oracle/a5/... | A5 package and its 40 subtests are PROSPECTIVE, producer Task 1. Same command against existing ./internal/oracle/sample/... passed in 1.254s; full A5 execution is a coding acceptance hold |
| V1 | env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp go test -race -count=1 -run '^TestOracleA5$' ./internal/oracle/a5/... | A5 selector is PROSPECTIVE. Existing sample package with go test -race -count=1 passed, 16.641s; race toolchain and oracle CLI build work here |
| V2 | env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp go run ./internal/testhelpers/oracle/cmd/regen -pkg ./internal/oracle/a5/... | A5 filter is PROSPECTIVE. Identical entry point with -pkg ./internal/oracle/sample/... exited 0, reporting help.txt H1 and error-envelope.json H1; main.go verifies A5 path syntax |
| V3 | env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp ORACLE_A5_TAMPER=1 go test -count=1 -run '^TestA5GoldenSensitivity$' ./internal/oracle/a5/... | TestA5GoldenSensitivity and ORACLE_A5_TAMPER are PROSPECTIVE, owned by Task 1. Existing env, go-test, Git and regen composition is bounded by RP2/RP6 and V0/V2; no A5 result is claimed before implementation |
| V4–V7 | env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp make -n lint test test-e2e test-race | Exit 0; selects sync-embedded, embedded check, cross-platform vet, archguard, go test ./..., race e2e selection, and full race with -timeout 30m -p 2. Regression-only commands are deferred, with no proof mapping |

**Batch budget: estimate, 1800 seconds.** Basis: a5-crosscutting estimates about
15 seconds for one 40-case run; the existing sample race run took 16.641 seconds
including build. Allocate 120s live, 180s scoped race, 300s starting-tree
regeneration, 600s scratch sensitivity proof, plus 600s load/build headroom.
This estimates future A5 cost, not a measurement of it. H7's +15% target is a
Should, not a per-command ceiling. Record actual A5 elapsed time in validation
evidence and an N4 disposition if it overruns the target; do not weaken proof.
The four broad Make suites are regression-only and deferred to integration
batching; none supplies a manifest proof.

## Task 1: Complete A5 oracle suite

**Direct references:** ["a5-scope","a5-context","a5-probes","a5-inventory","a5-components","a5-interfaces","a5-dataflow","a5-determinism","a5-crosscutting","harness","foundation","provider-defaults","prepared-project","provider-asks","goal-frozen","goal-tests","goal-proof","goal-success","goal-changes","guard","shared-files","oracle-budget"]

**Intent:** Must — deliver the 40-case A5 frozen-surface oracle.
**Done when:** all 40 TestOracleA5/<id> subtests pass at integration HEAD;
the A5-filtered entry point reproduces every golden from the named starting
commit; one altered golden byte fails live and regeneration comparisons; the
task diff adds files only under internal/oracle/a5/.
The semantic contract, modes and expected outcomes are a5-scope/a5-inventory.

### Owned Files and Change Units

All paths in this table are new and relative to internal/oracle/a5/. Go code is
test-only. The names allocate concerns, not a new shared interface.

| Files | Task 1 responsibility |
|---|---|
| oracle_test.go | TestMain via oracle.RunMain; TestOracleA5 table, inventory exactness and sequential orchestration |
| fixture_test.go | Load committed base/case data, materialise disposable directories and ordered labelled Git history |
| capture_test.go | Capture step output and before/after state, local label/lease/elapsed substitution, harness comparison |
| adapter_review_test.go | Module-importing adapter for reviewer claim, preflight and acceptance-refusal claim/block, plus brand variables and embedded pipeline |
| adapter_fixture_test.go, if needed | Module-specific fixture construction/serialization kept out of driver imports |
| sensitivity_test.go | TestA5GoldenSensitivity: scratch-only single-byte mismatch proof for live and regeneration |
| testdata/fixtures/base-<name>/** | Committed base state templates, repository content and labelled commit recipes |
| testdata/fixtures/cases/<id> | One committed manifest for each of the 40 inventory IDs |
| testdata/golden/json/**, human/**, state/** | Exactly the captures required by the inventory, generated by starting-tree write-new |
| testdata/acceptance-a5.json | Complete acceptance mapping for the allocated obligation IDs |

Helpers may be split into additional *_test.go files in this package if needed
for cohesion. No production file, existing test, shared harness, PROVENANCE,
other area's files, ADR/registry, or separate documentation file is owned.
Fixture, golden and manifest data are included in the allowed subtree.

### Corrected Operator Entry and Preserved WIP

Task 1 consumes the corrected a5-scope CONTRACT. For RA1/RA2's
`repair-acceptance-commits` step only, invoke the harness-built binary in the
prepared project without `--agent-id` and omit both agent-ID and
agent-generation environment variables, including legacy aliases. This is the
existing operator invocation, not an added runtime capability. Keep RA1's
`--dry-run` human capture and RA2's repair human/state capture. All other agent
entry, preparation, explicit environment and determinism requirements stay as
allocated. Inspect the archived driver's shared invocation path before reuse
so the correction applies to these operator steps only.

The original unfinished scaffold is preserved at
`/tmp/arm-1-ar-5-cp-0-code-0-scaffold-20261024.tar`, SHA256
`72c0c9cd10be3f0a7259e2522ec6367889ebe87e6a1c88f211cbb8738d1422e9`.
Its 52 regular files comprise five test files, four base fixture JSON files,
40 case manifests and three RCA input files; it contains no goldens.
The archive is optional unvalidated WIP, never a dependency or proof. After
supported replacement generation, Task 1 may inspect the archive's members,
content and checksum and reuse A5 files in its own worktree. Keep the archive
unchanged; never copy live runtime state or mutate the original worktree.
If the archive is unavailable, implement the same allocation from its sources;
no requirement depends on those bytes. Reused files may be corrected within
the additive A5 scope before their first merge; they are not an immutable
merged oracle. Inspect and complete RP1/RA2 preparations, sensitivity test and
acceptance manifest, and generate every golden via starting-tree write-new.
Prior scaffold hook passes do not establish compilation, reachability, golden
correctness or provenance. All original validation and independent review
remain required.

### Implementation Sequence

1. Establish the base/case data and driver using the supplied component split.
   Every case manifest names base, deltas, ordered preparation, tested step,
   and modes. Assert the exact 40-ID set and mode coverage independently of
   discovery, so missing files cannot silently reduce coverage.
2. Connect CLI steps exclusively through oracle.BuildCLI and RunInProject
   (prepared-project). Materialise a fresh project/HOME/TMPDIR for every output
   mode; preserve it through preparation and the tested step. Preparation must
   succeed before a snapshot or comparison. Bind state-only cases to one adapter
   execution; for CLI cases capture each requested mode's state independently.
3. Implement only the exported adapter calls allocated by a5-interfaces. Put
   every non-stdlib/non-harness import in adapter files, including YAML/model
   use for fixture setup. Verify starting-tree compilation; retain the harness
   adapter rule if a moved dependency requires an oraclestart twin. Do not make
   a production hook or bypass a public refusal boundary.
4. Generate missing goldens with the committed regen entry point in write-new
   mode; then run the canonical live, race, provenance and sensitivity proofs.
   Commit inputs with goldens and acceptance manifest. Review the additive diff
   and run pre-commit on every new file before final validation/submission.

### Case Allocation

The table materializes IDs for dispatch and proof mapping; a5-inventory owns
their expected behavior and modes. All rows are Must and belong to Task 1.
Each AC-<id> maps to its actual TestOracleA5/<id> assertion under command index 0,
including every mode in that inventory row.

| Case IDs | Concern | Obligations |
|---|---|---|
| SR1 SR2 | Submission | AC-SR1, AC-SR2 |
| AE1 AE2 AE3 | Acceptance evidence | AC-AE1, AC-AE2, AC-AE3 |
| SV1 SV2 SV3 SV4 SV5 SV6 | Verdicts | AC-SV1, AC-SV2, AC-SV3, AC-SV4, AC-SV5, AC-SV6 |
| AV1 AV2 AV3 | Await verdict | AC-AV1, AC-AV2, AC-AV3 |
| AR1 AR2 AR3 | Await resubmission | AC-AR1, AC-AR2, AC-AR3 |
| RC1 RC2 RC3 RC4 RC5 | Reviewer claims | AC-RC1, AC-RC2, AC-RC3, AC-RC4, AC-RC5 |
| RCA1 RCA2 RCA3 RCA4 | Rejection RCA | AC-RCA1, AC-RCA2, AC-RCA3, AC-RCA4 |
| VP1 VP2 | Validation preflight | AC-VP1, AC-VP2 |
| UR1 UR2 | Review commit maintenance | AC-UR1, AC-UR2 |
| CS1 CS2 | Stale review claim maintenance | AC-CS1, AC-CS2 |
| RV1 RV2 | Quarantined verdict reconciliation | AC-RV1, AC-RV2 |
| RP1 RP2 | Proof reaffirmation | AC-RP1, AC-RP2 |
| AA1 AA2 | Receipt archival | AC-AA1, AC-AA2 |
| RA1 RA2 | Acceptance commit repair | AC-RA1, AC-RA2 |

Reachability must be demonstrated with the approved fixture and preparation
through the named entry. For the inherited PROSPECTIVE rows, failing to reach
the outcome at start is a plan defect and blocks this unit; no expected result
is invented from an unrelated error. Refusal preparations still succeed; only
the step under test produces the declared refusal.

### Capture and Proof Allocation

- **AC-capture:** capture each json exit code plus the unchanged decoded stdout
  document in a JSON observation; capture nonempty JSON stderr as its separate
  human golden. Human captures identify exit code, stdout and stderr separately.
  Use CheckState for each required before/after pair, including refusals.
  Adapter return objects never become goldens. Exact naming follows a5-inventory;
  include mode suffixes where the state capture differs between fresh modes.
- **AC-preparation / AC-entrypoints:** fixture tests exercise a failed preparation
  and prove the tested step was not invoked. A5 rows use RunInProject and the
  three exported adapter paths with the inputs fixed by a5-scope. Verify the
  driver contains no private entry seam or replacement CLI runner.
- **AC-determinism / AC-isolation:** apply a5-determinism before shared comparison.
  Preserve every labelled commit distinction. Convert only changed lease fields
  to the specified offset token using step start; do not erase the 30-minute
  default. Pinned VP/claim validation sessions must not hash host paths.
  Use fixture time, fixed Git identity/dates, explicit env and nonblocking await
  setup. Cases are sequential; Windows skips with the POSIX fixture reason.
  Driver tests should demonstrate labels and lease duration survive normalization,
  and environment assertions report names/booleans only.
- **AC-inventory / AC-adapters / AC-failure-signals:** package-local test assertions
  cover the independent 40-ID/mode table, test-only import boundary and failure
  signals (missing golden, failed preparation, comparison path). Reuse the
  delivered comparator rather than duplicate its algorithm.
- **AC-provenance:** command index 2 proves all committed A5 goldens reproduce
  under the existing starting-tree entry point. Allocate its mapping to
  oracle_test.go's TestOracleA5 execution as invoked by regen, not an unrelated
  harness unit test. Only write-new creates the initial goldens; no ledger record.
- **AC-sensitivity:** command index 3 executes TestA5GoldenSensitivity. Ordinary
  suite runs skip this extra proof unless ORACLE_A5_TAMPER=1, with an explicit
  skip reason. In a disposable repository/worktree retaining starting history,
  first require the intact A5 live and regen checks to pass. Change exactly one
  content byte of one existing A5 golden, without changing code/fixtures/ledger,
  then require both checks to fail naming that golden. Missing build/setup,
  missing package or ledger errors are not accepted as mismatch proof. Restore
  bytes/clean up scratch even on failure. Never alter the source task worktree
  or live runtime state. Nested children get ChildEnv without the opt-in flag,
  and the nested live command selects ^TestOracleA5$, preventing recursion.
  The normal A5 write-mode run must not attempt this sensitivity proof.
- **AC-boundary:** the independent reviewer inspects the complete base-to-review
  diff under the approved exception below. Include every support and acceptance
  file in the child scope; no code-owned prerequisite remains outside it.

The capture envelope is local test data, not a change to CLI output: the stdout
JSON document and its codes remain intact as a value in the observation.
Use stdlib JSON with json.RawMessage/UseNumber, not float coercion. Human stream
markers are fixed driver labels; do not concatenate streams without boundaries.

All executable obligations other than provenance/sensitivity map to index 0,
to actual assertions rather than claims in comments. The manifest has exactly
one mapping per allocated obligation. No mapping targets deferred indices 4–7.
RC4 freezes the starting tool-name behavior; C2 owns its later changed record.
Default lease goldens remain unchanged by C1. No C behavior change is made here.

### Validation and Environment

Run pre-commit on all touched paths, including Go tests and JSON manifests,
after worktree setup with make sync-embedded. Existing hooks apply by suffix;
Go files activate Go validation, JSON activates check-json. Do not install tools
or change hook configuration to obtain a pass.

Initial generation, outside acceptance execution:
`env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp go run ./internal/testhelpers/oracle/cmd/regen -mode write-new -pkg ./internal/oracle/a5/...`

The following exact ordered commands are canonical; V0–V7 above explain evidence,
future targets and budget. Commands 4–7 remain deferred regression checks.

0. `env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp go test -count=1 ./internal/oracle/a5/...`

1. `env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp go test -race -count=1 -run '^TestOracleA5$' ./internal/oracle/a5/...`

2. `env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp go run ./internal/testhelpers/oracle/cmd/regen -pkg ./internal/oracle/a5/...`

3. `env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp ORACLE_A5_TAMPER=1 go test -count=1 -run '^TestA5GoldenSensitivity$' ./internal/oracle/a5/...`

4. `env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp make lint`

5. `env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp make test`

6. `env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp make test-e2e`

7. `env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp make test-race`

Prerequisites are executables and HOME/PATH only, checked before implementation:
go/git/sh/env for scoped proofs, gcc for race, make for Make targets; no built
binary or future fixture is a claim prerequisite. Task-owned test names, package,
fixture and goldens are PROSPECTIVE deliverables, with acceptance/submission held
until implemented and passing.

### Acceptance Contract

```json
{
  "version": 1,
  "manifest": "internal/oracle/a5/testdata/acceptance-a5.json",
  "obligations": [
    "AC-SR1",
    "AC-SR2",
    "AC-AE1",
    "AC-AE2",
    "AC-AE3",
    "AC-SV1",
    "AC-SV2",
    "AC-SV3",
    "AC-SV4",
    "AC-SV5",
    "AC-SV6",
    "AC-AV1",
    "AC-AV2",
    "AC-AV3",
    "AC-AR1",
    "AC-AR2",
    "AC-AR3",
    "AC-RC1",
    "AC-RC2",
    "AC-RC3",
    "AC-RC4",
    "AC-RC5",
    "AC-RCA1",
    "AC-RCA2",
    "AC-RCA3",
    "AC-RCA4",
    "AC-VP1",
    "AC-VP2",
    "AC-UR1",
    "AC-UR2",
    "AC-CS1",
    "AC-CS2",
    "AC-RV1",
    "AC-RV2",
    "AC-RP1",
    "AC-RP2",
    "AC-AA1",
    "AC-AA2",
    "AC-RA1",
    "AC-RA2",
    "AC-inventory",
    "AC-capture",
    "AC-preparation",
    "AC-entrypoints",
    "AC-determinism",
    "AC-isolation",
    "AC-adapters",
    "AC-provenance",
    "AC-sensitivity",
    "AC-failure-signals",
    "AC-boundary"
  ],
  "validation": [
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp go test -count=1 ./internal/oracle/a5/...",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp go test -race -count=1 -run '^TestOracleA5$' ./internal/oracle/a5/...",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp go run ./internal/testhelpers/oracle/cmd/regen -pkg ./internal/oracle/a5/...",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp ORACLE_A5_TAMPER=1 go test -count=1 -run '^TestA5GoldenSensitivity$' ./internal/oracle/a5/...",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp make lint",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp make test",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp make test-e2e",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp make test-race"
  ],
  "timeout_seconds": 1800,
  "deferred": [
    4,
    5,
    6,
    7
  ],
  "approved_proofs": [
    {
      "obligation_id": "AC-boundary",
      "reference_id": "a5-scope",
      "rationale": "Per-task additive diff ownership and the exclusion of production, existing tests, harness, ledger and other areas require independent inspection of the reviewed base-to-review range. Runtime oracle success alone cannot prove that boundary; the reviewer verifies it against this pinned scope."
    }
  ]
}
```

## Spec Compliance Matrix

| # | Requirement / effective priority | Source | Task(s) | Status |
|---|---|---|---|---|
| 1 | Must: inventory cases SR1 SR2 with every listed mode | a5-inventory / Surface Inventory | Task 1 | Covered |
| 2 | Must: inventory cases AE1 AE2 AE3 with every listed mode | a5-inventory / Surface Inventory | Task 1 | Covered |
| 3 | Must: inventory cases SV1 SV2 SV3 SV4 SV5 SV6 with every listed mode | a5-inventory / Surface Inventory | Task 1 | Covered |
| 4 | Must: inventory cases AV1 AV2 AV3 with every listed mode | a5-inventory / Surface Inventory | Task 1 | Covered |
| 5 | Must: inventory cases AR1 AR2 AR3 with every listed mode | a5-inventory / Surface Inventory | Task 1 | Covered |
| 6 | Must: inventory cases RC1 RC2 RC3 RC4 RC5 with every listed mode | a5-inventory / Surface Inventory | Task 1 | Covered |
| 7 | Must: inventory cases RCA1 RCA2 RCA3 RCA4 with every listed mode | a5-inventory / Surface Inventory | Task 1 | Covered |
| 8 | Must: inventory cases VP1 VP2 with every listed mode | a5-inventory / Surface Inventory | Task 1 | Covered |
| 9 | Must: inventory cases UR1 UR2 with every listed mode | a5-inventory / Surface Inventory | Task 1 | Covered |
| 10 | Must: inventory cases CS1 CS2 with every listed mode | a5-inventory / Surface Inventory | Task 1 | Covered |
| 11 | Must: inventory cases RV1 RV2 with every listed mode | a5-inventory / Surface Inventory | Task 1 | Covered |
| 12 | Must: inventory cases RP1 RP2 with every listed mode | a5-inventory / Surface Inventory | Task 1 | Covered |
| 13 | Must: inventory cases AA1 AA2 with every listed mode | a5-inventory / Surface Inventory | Task 1 | Covered |
| 14 | Must: inventory cases RA1 RA2 with every listed mode | a5-inventory / Surface Inventory | Task 1 | Covered |
| 15 | Must: exactly 40 named subtests; declared start outcome or BLOCKED | a5-scope / Cases; a5-probes | Task 1 | Covered |
| 16 | Must: fresh modes, ordered successful preparation, before/after observations | a5-scope / Cases and Capture; a5-dataflow | Task 1 | Covered |
| 17 | Must: json document and exit; separate nonempty stderr; human content; state-only adapters | a5-scope / Capture per step under test; goal-frozen | Task 1 | Covered |
| 18 | Must: harness CLI, project cwd, agent --agent-id except corrected RA1/RA2 operator steps with neither identity env variable nor legacy alias; RC lease unset; VP pinned local session; AE3 claim refusal into block | a5-scope / Entry points; prepared-project | Task 1 | Covered |
| 19 | Must: committed bases/deltas/recipes/manifests and component separation | a5-components; a5-dataflow | Task 1 | Covered |
| 20 | Must: root/commit labels, changed-lease offsets, timestamps and elapsed preprocessing | a5-determinism; foundation | Task 1 | Covered |
| 21 | Must: explicit child env, pinned validation env, no secrets or live state/provider access | a5-context OC3; guard G1.4; goal-proof E2–E4 | Task 1 | Covered |
| 22 | Must: one eligible/preferred claim, immediate awaits, sequential POSIX fixtures | a5-determinism | Task 1 | Covered |
| 23 | Must: test-only package and adapter import/tag rule; default-brand test exception | a5-context OC1–OC2; harness / Adapter rule; guard G1.3 | Task 1 | Covered |
| 24 | Must: normalized forms, diagnostics, live missing-golden failure and no live writes | harness / Golden form and Golden I/O; a5-crosscutting | Task 1 | Covered |
| 25 | Must: write-new from 96c9ad69a, filtered all-golden reproduction, inputs committed, no ledger edit | a5-scope / Goldens; foundation; goal-proof E5 | Task 1 | Covered |
| 26 | Must: altered-byte failure in live and regen; additive A5-only diff | a5-scope / Done when | Task 1 | Covered |
| 27 | Must: preserve current RC4 until C2, C1 default unchanged; later immutability exceptions | a5-inventory / C column; goal-tests; goal-changes | Task 1 | Covered |
| 28 | Must: only A5 surface ownership, A9 split, no common docs or runtime changes | a5-inventory / Boundary with A9; shared-files | Task 1 | Covered |
| 29 | Must: local proof and regression coverage before area refactor | goal-proof E1; goal-tests / Sequencing; goal-success S4 | Task 1 | Covered |
| 30 | Should: runtime target and recorded nonblocking N4 disposition | oracle-budget H7; a5-crosscutting / Proof budget | Task 1 | Covered |
| 31 | Must: one replacement of the blocked coding task; fresh ce525e865… scope pin and unchanged accepted allocation; no old-provider supersession or blocked-child wait | Assigned authority-correction task; Correction and Replacement Lineage | Task 1 | Covered |
| 32 | Must: inspect optional archived scaffold as unvalidated WIP; preserve original worktree and archive; no inherited success | Assigned authority-correction task; Corrected Operator Entry and Preserved WIP | Task 1 | Covered |
| E2E | e2e coverage for new behavior | Cross-cutting | N/A: test-only preservation oracle adds no user-visible behavior; existing e2e regression command retained | N/A |
| DOC | Documentation for changed behavior | Cross-cutting | N/A: no changed user-visible behavior; architecture and harness docs remain authoritative | N/A |

## Shared-File and Output Audit

Only Task 1 writes internal/oracle/a5/**, all additions. Other area oracle writers
have disjoint paths. This task does not join the ordered shared-doc chain.
The new acceptance manifest lives inside the allowed A5 subtree.
No task dependency is stated solely in prose; the two provider selections are
encoded in output[0]. Task 1 = output[0], with the exact anchored plan_ref and
identical command order. No production, migration, public API, authentication
or concurrency change is implied by these test-only additions.

Self-challenge: a green suite with fewer cases, wrong refusal setup, collapsed
commit/lease distinctions, host-dependent fingerprints, overwritten live goldens
or a setup-only negative failure would not meet the contract. The ID/mode check,
entry-boundary fixtures, local normalization assertions, explicit environments,
starting-tree comparison and scratch mismatch proof address those failure modes.
Remaining prospective reachability is owned by Task 1 under the carrier's explicit
BLOCKED rule, not presumed solved by the provider correction.
