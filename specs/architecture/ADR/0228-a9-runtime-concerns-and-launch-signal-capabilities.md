# 228 - A9 Agent Runtime Concerns and Launch and Signal Capabilities

## Status

ACCEPTED.

## Context

The architecture-consolidation run requires the agent runtime to separate its
concerns (S1, S3), the capabilities presentation and `ops` need to leave the
runtime package (K3, SI-5), and its finite vocabulary and magic values to be
named (K7), with no behaviour change
([goal](../../goals/20261023-architecture-consolidation.md#structural-constraints)).
At the A9 start (`3b9ba5d94`) the area (`internal/agent` outside the four A8
prompt-assembly files, and `internal/process`) had one file offender
(`systemctl.go`, 861 lines), twelve function offenders (`ResolveLaunchPlan` 143,
`CLIAgent.Run` 178, `CLIAgent.buildRunCommand` 148, `ACPXAgent.Run` 141,
`RunSupervisor` 521, `executeAgent` 207, `verifyOrchestratorWakeChanges` 124,
`claimDoerTaskWithOptionalAuthority` 126, `handleApprovedMergesWithOptionalAuthority`
107, `exit42RestartTracker.Handle` 111, `waitForWorkEventDriven` 132,
`ensureReviewerWorktreeLocked` 139) and three crossings: `commands` to `agent`,
`tui` to `agent` and `agent` to `db`. The decision is indexed in the
[ADR README](README.md).

[ADR-0130](0130-generation-fenced-agent-authority.md) (generation-fenced
authority, lifecycle then admission lock nesting) and
[ADR-0177](0177-liveness-side-records.md) (heartbeats publish side records)
stay binding; ADR-0045 keeps role names, role-pair names and tool names
dynamic; ADR-0220 supplies the typed role and claim vocabulary and the
conversion rules; per-area guard data follows ADR-0218, the A9 file
`internal/testguard/archguard/area_a9_test.go` being the evidence. Five coding
units delivered A9: the launch and signal extraction, the backend run concerns,
the supervisor lifetime, turn and session concerns, and the claim, recovery,
wait and restart concerns and closure this ADR belongs to.

## Decision

### D1 - Launch capability below the runtime

`internal/agentlaunch` (infrastructure, no runtime, application, prompts or
presentation import) owns what the runtime and its callers need to decide how an
agent is launched, with inputs and outputs unchanged:

| File | Contents |
|---|---|
| `launch_resolver.go` | `LaunchPlanRequest`, `LaunchPlan` (distinct `ToolName`, `ProviderKey`, `ContractKey`, argv, prompt transport, profile and model, session identity, environment-file metadata), `ResolveLaunchPlan` (56 lines) over `resolveLaunchBase`, `expandLaunchSession`, `expandLaunchArgs`, `expandACPXArgs`; `AgentToolRegistry`, `BuiltInAgentTools`, `ACPXAgentName`, `ACPXSessionName`, the runtime and embedded catalog memos |
| `launch_selection.go` | `ResolveLaunchSelection`, `ResolveProfileForRole`, `SelectionSource` and its constants, the ordered profile, model and list rules |
| `role_models.go` | `LoadValidatedRoleModels`, `ValidateRoleModels`, `RoleTypesOf` |
| `validation_policy.go` | `DefaultRoleCLIs`, `AllValidationLocal`, `ValidationExecutionLocal` |
| `cli.go` | `DefaultCLI`, `CLIResolutionConfig`, the `ResolveDefaultCLI*` and `ResolveCLI*` resolvers, `ValidCLIs`, `AvailableCLIs`, `IsValidCLI`, `CLIExecutableName`, `CheckCLIPrerequisites*`, `InteractiveCLICommand`, `ContractKeyForCLI` |

`internal/providers` stays the catalog owner and `internal/rolemodels` the file
and schema owner; no parallel registry exists. Backend constructors, execution,
masking, environment preparation and usage events stay in `agent`. Nothing in
the package starts a process, registers an agent, touches a worktree or loads an
environment file. `ResolveLaunchPlan` was reduced in place (143 to 56 lines)
before it moved. C2 reads the effective tool registry here without importing the
runtime.

### D2 - Provider signals as infrastructure

`internal/providersignal` (infrastructure) owns quota and provider-unavailable
detection (`DetectQuotaExhaustion`, `ClassifyProviderUnavailable`,
`DetectProviderUnavailable`), diagnostic-line extraction, reset-time
interpretation (`quota_reset.go`), signal paths, records and files
(`QuotaSignalPath`, `WriteQuotaSignal`, `CheckQuotaSignal`, `QuotaBlockedUntil`,
`ProviderUnavailableSignalPath` and their clear helpers), the quota sweep
(`ClearExpiredQuotaSignals`) and the alert-line writers (`LogAlert`,
`LogQuotaAlert`, `LogProviderUnavailableAlert`). The API takes a project root, a
tool identity and output and holds no blackboard handle. Per-provider
distinctions, matching order, timestamp representation, unreadable-file
handling, clear results and error text are those of the moved code.
`commitQuotaTerminationWIP` and the supervisor output discovery stay in `agent`
and call the package for classification only; the process spawn gate calls the
same checks in the same order and keeps its readiness protocol.

### D3 - Residual SI-5 crossings

Every presentation use of launch and signal capabilities migrated in the
extraction unit (`4af562fd8`). The closure scan finds two production importers of
`agent` outside `cmd/liza`: `internal/commands/status_orchestrator.go`
(`agent.DetectOrchestratorWakeTriggersForProject`, `agent.OrchestratorWakeResult`)
and `internal/tui/view_agents.go` (`agent.RolePauseReason`). Both are read-only
projections that grant no mutation authority: the wake projection keeps the
runtime's wake precedence out of infrastructure (the A9 allowlist entry for
`commands`), `RolePauseReason` is the snapshot-only pause reason the runtime
displays and avoids a second role and checkpoint predicate (the entry for `tui`).
The `agent` to `db` entry stays for C6. No entry is stale;
`TestK4ImportDirectionGuard` fails on a stale or a new edge.

### D4 - Supervisor lifetime, turn and session

`RunSupervisor` (29 lines at closure, 521 at start) is the ordered coordinator
over cohesive files; no event bus, state-machine framework or service was added.

| Concern | File (lines) | Contents |
|---|---|---|
| Coordinator | `supervisor.go` (171) | `RunSupervisor`, snapshot read helpers, signal handlers |
| Lifetime | `supervisor_lifetime.go` (264) | identity and environment withholding, frozen strategy, pending-verdict replay, registration, lifetime heartbeat, independent cleanup context |
| Turn | `supervisor_turn.go` (283) | work admission, initial-task spending, surplus retirement, role gates, prompt preparation |
| Completed turn | `supervisor_completed_turn.go` (234) | verdict replay, quota WIP before release, release witness, provider audit, background-job preservation, exit-specific tracker resets |
| Session | `session_execution.go` (294) | `executeAgent` (15 lines at closure, 207 at start): session preparation, gated start, interactive execution, watchdog lifetime, result classification |
| Launch gate | `systemctl.go` (57; 861 at start) | `newProviderLaunchGate`, `newTaskProviderLaunchGate`: lifecycle then admission lock nesting and the current-generation, review and validation checks |
| Pause gates, usage sink, verification | `pause_gate.go` (229), `usage_sink.go` (193), `orchestrator_verify.go` (256), `planning_complete_verify.go` (108) | moved whole out of `systemctl.go`; `verifyOrchestratorWakeChanges` dispatches per trigger (40 lines, 124 at start) |

Ordering, heartbeat lifetime across idle waits, heartbeat-error precedence,
start-attempted marker placement and the distinction between gate and backend
failure are those of the moved code, pinned by the PB map of the supervisor unit
(`specs/architecture/consolidation-run/a9-supervisor-evidence.md`).

### D5 - Runtime concern helpers

Backends: `CLIAgent.Run` (28 lines, 178 at start), `buildRunCommand` (43, 148) and
`ACPXAgent.Run` (36, 141) separate command and prompt-file preparation, stream
wiring, gated launch and completion mapping into `cli_agent_command.go`,
`cli_agent_streams.go`, `cli_agent_completion.go`, `acpx_agent_session.go` and
`acpx_agent_completion.go`; CLI and ACP keep their separate start and wait
behaviour and the `LLMAgent` result shape.

Claim, restart, wait and recovery (this unit), each reduced in place before any
file move and each helper under 55 lines:

| Concern | Function (lines at start, now) | Helpers (lines) |
|---|---|---|
| Doer claim | `claimDoerTaskWithOptionalAuthority` (126, 17) in `claiming.go` | `resumeDoerTask` (35), `claimableDoerTier` (28), `claimFirstDoerCandidate` (17), `claimDoerCandidate` (34), `clearDegradedAfterClaim` |
| Approved-merge batch | `handleApprovedMergesWithOptionalAuthority` (107, 34) in `claiming_merge.go` | `resolveApprovedMergeGate` (43), `mergeApprovedTask` (27), `logMergeFailure`; the merge gate, `evaluateMergeGate` and the pending-merge predicates moved whole from `claiming.go` (732 to 551 lines) into `claiming_merge.go` (272 lines) |
| Restart | `exit42RestartTracker.Handle` (111, 24) in `supervisor_restart.go` | `exit42CurrentSignature`, `advanceExit42Restart` (13), `persistExit42Restart` (23), `blockExit42RestartLoop` (50); the tracker closure stays in `agent` |
| Wait | `waitForWorkEventDriven` (132, 34) in `waitforwork.go` | `evaluateWaitState` (25), `observeWaitState`, `watchForWork` (53); polling reuses the predicate |
| Reviewer worktree recovery | `ensureReviewerWorktreeLocked` (139, 24) in `worktree_check.go` | `attachReviewerWorktree` (45), `prepareRecoveredReviewerWorktree` (27), `refreshRecoveredReviewerIndexes` (49), `recordReviewerWorktreeRecovery` (17) |

Preserved orders: handoff, then owned task, then fresh candidates, with the
nil-authority compatibility paths; per-candidate policy resolution before merge,
a halt or an authority loss stopping the batch and any other failure continuing
with the next candidate; backoff and progress calculated in memory before the
fenced write, the tracker key forgotten only after a block; cancellation, then
stop, surplus and work in that precedence, polling fallback on a watcher failure,
idle diagnostics off on the fallback tick; recovery blocking on a prior recovery
or a missing branch before attaching, setup failing closed before the index
refreshes, the history entry written last and only warned on failure.

### D6 - Vocabulary and the K7 inventory

The inventory covers all 75 production Go files of the four packages
(`internal/agent`, `internal/process`, `internal/agentlaunch`,
`internal/providersignal`), not only the offenders. Locations are `file:line`
relative to `internal/` at the closure code commit `633a39144`; the sweep was re-run
after the stream-vocabulary typing (review iteration 1) and lists the same 726
positions and kinds.

Reproduction. The program below uses only the standard library; save it as
`k7sweep.go` outside the repository and run, from the repository root,
`go run /path/to/k7sweep.go . internal/agent internal/process internal/agentlaunch internal/providersignal`
on that commit. It prints one tab-separated row per candidate: `file:line`, kind,
enclosing function, code. Kinds: `CMP-STR` (string literal operand of `==` or
`!=`), `CASE-STR` and `CASE-NUM` (literal switch labels), `CALL-STR` (a
`strings.*` call with a string literal argument), `CMP-NUM` (numeric literal
operand of `==`, `!=`, `<`, `<=`, `>` or `>=`) and `NUM` (any other numeric
literal outside a constant declaration). It lists every candidate the closure
checklist names: string literals compared in conditions or switch labels,
`strings.HasPrefix`, `HasSuffix`, `Contains` and `EqualFold` operands, and
numeric literals in `if`, `for` and `switch` comparisons, besides assignments and
arguments. 726 rows at the closure commit, none unclassified.

```go
// k7sweep lists the K7 candidates of Go production files: string literals used
// as comparison operands, switch labels or strings.* arguments, numeric literals
// in comparisons, and numeric literals anywhere else outside a const declaration.
// Usage: go run k7sweep.go <repo-root> <dir>... (non-test .go files, not recursive).
package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type row struct{ pos, kind, fn, code string }

func main() {
	root := os.Args[1]
	var rows []row
	fset := token.NewFileSet()
	for _, dir := range os.Args[2:] {
		files, _ := filepath.Glob(filepath.Join(root, dir, "*.go"))
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") {
				continue
			}
			file, err := parser.ParseFile(fset, f, nil, 0)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(2)
			}
			rel, _ := filepath.Rel(root, f)
			rows = append(rows, sweep(fset, file, rel)...)
		}
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].pos < rows[j].pos })
	for _, r := range rows {
		fmt.Printf("%s\t%s\t%s\t%s\n", r.pos, r.kind, r.fn, r.code)
	}
}

func isStr(e ast.Expr) bool { b, ok := e.(*ast.BasicLit); return ok && b.Kind == token.STRING }
func isNum(e ast.Expr) bool {
	if u, ok := e.(*ast.UnaryExpr); ok {
		e = u.X
	}
	b, ok := e.(*ast.BasicLit)
	return ok && (b.Kind == token.INT || b.Kind == token.FLOAT)
}
func src(fset *token.FileSet, n ast.Node) string {
	var b bytes.Buffer
	_ = printer.Fprint(&b, fset, n)
	s := strings.Join(strings.Fields(b.String()), " ")
	if len(s) > 110 {
		s = s[:110] + "..."
	}
	return s
}

func sweep(fset *token.FileSet, file *ast.File, rel string) []row {
	var out []row
	add := func(n ast.Node, kind, fn string) {
		p := fset.Position(n.Pos())
		out = append(out, row{fmt.Sprintf("%s:%05d", rel, p.Line), kind, fn, src(fset, n)})
	}
	for _, decl := range file.Decls {
		fn := "<pkg>"
		if fd, ok := decl.(*ast.FuncDecl); ok {
			fn = fd.Name.Name
			if fd.Recv != nil && len(fd.Recv.List) == 1 {
				t := src(fset, fd.Recv.List[0].Type)
				fn = strings.TrimPrefix(t, "*") + "." + fn
			}
		}
		if gd, ok := decl.(*ast.GenDecl); ok && gd.Tok == token.CONST {
			continue
		}
		inCompare := map[ast.Node]bool{}
		ast.Inspect(decl, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.BinaryExpr:
				switch x.Op {
				case token.EQL, token.NEQ:
					if isStr(x.X) || isStr(x.Y) {
						add(x, "CMP-STR", fn)
						markLits(inCompare, x)
					}
					if isNum(x.X) || isNum(x.Y) {
						add(x, "CMP-NUM", fn)
						markLits(inCompare, x)
					}
				case token.LSS, token.LEQ, token.GTR, token.GEQ:
					if isNum(x.X) || isNum(x.Y) {
						add(x, "CMP-NUM", fn)
						markLits(inCompare, x)
					}
				}
			case *ast.CaseClause:
				for _, e := range x.List {
					if isStr(e) {
						add(e, "CASE-STR", fn)
						inCompare[e] = true
					}
					if isNum(e) {
						add(e, "CASE-NUM", fn)
						inCompare[e] = true
					}
				}
			case *ast.CallExpr:
				if sel, ok := x.Fun.(*ast.SelectorExpr); ok {
					if id, ok := sel.X.(*ast.Ident); ok && id.Name == "strings" {
						for _, a := range x.Args {
							if isStr(a) {
								add(x, "CALL-STR", fn)
								inCompare[a] = true
								break
							}
						}
					}
				}
			}
			return true
		})
		ast.Inspect(decl, func(n ast.Node) bool {
			b, ok := n.(*ast.BasicLit)
			if !ok || inCompare[n] {
				return true
			}
			if b.Kind == token.INT || b.Kind == token.FLOAT {
				add(b, "NUM", fn)
			}
			return true
		})
	}
	return out
}

func markLits(m map[ast.Node]bool, b *ast.BinaryExpr) {
	for _, e := range []ast.Expr{b.X, b.Y} {
		if u, ok := e.(*ast.UnaryExpr); ok {
			e = u.X
		}
		if l, ok := e.(*ast.BasicLit); ok {
			m[l] = true
		}
	}
}
```

Classes (first matching rule wins; **T** typed, **N** named, **O** open,
**P** provider or platform interface spelling, **R** reserved, **S** structural,
**F** text format):

| Class | Rule | Owner or reason |
|---|---|---|
| T, N | the literal no longer appears in the sweep | adopted or named in the tables below |
| S-EMPTY | `CMP-STR` against `""` | zero value of the operand |
| S-BOUNDARY | `CMP-NUM` or `CASE-NUM` whose literals are all 0, 1 or -1 | presence, cardinality, sign and index bounds, including `len(x) == 0` |
| S-SMALL | `NUM` equal to 0, 1 or 2 | index, slice bound, capacity, arity or radix; a threshold would be a comparison and appear as `CMP-NUM` |
| S-REGEX | `NUM` in `quotaResetTime`, `populatePlannerRework`, `validateClaudeToolResultBoundary` | capture-group index or match arity |
| S-STDLIB | `NUM` in `withClaudeCommandCeiling` | `strconv.ParseInt` base and bit size |
| S-CAP | `NUM` in `agentProcessEnv` | slice capacity hint |
| S-COUNT | `CMP-NUM` in `reviewerStrategy.hasAdmittedReviewWork` | two computed counts compared; the literal is the 0 of a difference |
| S-VERSION | `CMP-NUM` in `codexSupportsToolResultHooks` | zero test of a patch component under the prerelease rule; the 154 floor is named |
| N-DECL | `NUM` at package level | already the value of a named variable (`abortTickInterval`, `executionDeadlineRecheckInterval`, `submitHandbackWindow`, `pendingMergeWakeInterval`, `maxPendingMergeStallRounds`) |
| R-C1 | `NUM` 1800 | lease duration of Scope 22: `strategy_reviewer.go` `reviewerStrategy.ClaimTask`, `supervisor_lifetime.go` `supervisorLifetime.register` |
| O-TOOL | tool-name literal compared, switched or alias-derived | catalog tool identity, dynamic (ADR-0045); the tool-name policy is reserved for C2 and C3 |
| O-SECTION | `section == "…"` | context-section identity, YAML-defined |
| O-ROLEPAIR | `"integration-pair"` | role-pair name; the `ops` constant is unexported |
| P-FLAG | provider CLI flag spelling | external interface of the provider |
| P-ENV | environment variable name or flag value | external interface of a provider or the engine |
| P-PLATFORM | `runtime.GOOS == "windows"` | standard-library vocabulary |
| F-FORMAT | any other `CALL-STR` | separator, delimiter, quoting, trim set or search token of text handling |

Occurrences by class (every `file:line`; no row is elided):

**S-EMPTY** - empty-string test (zero value of the operand) (232 occurrences)

| File | Lines |
|---|---|
| `agent/acpx_agent.go` | 46, 96, 211, 323, 332 |
| `agent/acpx_agent_completion.go` | 29, 45 |
| `agent/acpx_agent_session.go` | 77, 114 |
| `agent/agent_output_files.go` | 19 |
| `agent/checkpoint_summary.go` | 277, 404 |
| `agent/claiming.go` | 220, 460 |
| `agent/claiming_merge.go` | 216 |
| `agent/cli_agent.go` | 30, 196, 214, 222, 241, 253, 294 |
| `agent/cli_agent_command.go` | 86 |
| `agent/cli_agent_streams.go` | 62 |
| `agent/heartbeat.go` | 89, 183 |
| `agent/orchestrator_dashboard.go` | 172, 267 |
| `agent/pause_gate.go` | 51, 86, 138 |
| `agent/planning_complete_verify.go` | 37 |
| `agent/progress_watchdog.go` | 49, 174, 200 |
| `agent/prompt.go` | 79, 124, 265 |
| `agent/prompt_corrective_context.go` | 35, 49 |
| `agent/prompt_integration.go` | 42, 72, 106, 160, 163, 173, 186, 285, 320 |
| `agent/prompt_intelligence.go` | 97, 111, 124, 137 |
| `agent/prompt_planner_context.go` | 74, 117, 122, 138, 167, 191, 195, 200, 204, 207, 240, 246, 249, 333, 347, 393, 407, 436, 442, 443, 473, 483, 489, 500, 530 |
| `agent/prompt_task_context.go` | 101, 145, 155, 217 |
| `agent/prompt_task_graph.go` | 255, 277, 278, 330, 390, 418, 431, 439, 478 |
| `agent/quota_termination_wip.go` | 17 |
| `agent/reference_context.go` | 53, 139, 173, 231, 262, 347, 350, 396 |
| `agent/registration.go` | 23, 214, 247, 472 |
| `agent/review_execution.go` | 31, 40, 78, 136 |
| `agent/session_execution.go` | 104 |
| `agent/strategy_doer.go` | 84, 93, 128 |
| `agent/strategy_orchestrator.go` | 260, 383 |
| `agent/strategy_reviewer.go` | 268, 280, 353, 375, 506, 578, 636 |
| `agent/supervisor.go` | 66 |
| `agent/supervisor_background.go` | 66, 73, 130, 198 |
| `agent/supervisor_block.go` | 55, 112 |
| `agent/supervisor_completed_turn.go` | 77, 118, 190 |
| `agent/supervisor_restart.go` | 36, 70, 83, 102, 177, 278, 308, 354, 378 |
| `agent/supervisor_runtime_failure.go` | 40, 90, 159 |
| `agent/supervisor_turn.go` | 38, 176, 236, 261 |
| `agent/tool_result_acp.go` | 20 |
| `agent/tool_result_claude.go` | 22 |
| `agent/tool_result_codex.go` | 87 |
| `agent/usage_sink.go` | 63, 131 |
| `agent/waitforwork.go` | 191, 196 |
| `agent/workdetection.go` | 143 |
| `agentlaunch/acpx_session.go` | 9 |
| `agentlaunch/cli.go` | 109, 112, 124, 127, 130, 181 |
| `agentlaunch/launch_resolver.go` | 154, 175, 178, 181, 195, 197, 210, 281, 284, 287, 296, 306, 310, 315, 335, 338, 351, 362, 405, 410, 413, 416, 419, 440, 443, 446, 449, 464, 467, 470, 473, 476, 506, 532 |
| `agentlaunch/launch_selection.go` | 66, 77, 78, 90, 119 |
| `agentlaunch/role_models.go` | 57, 63 |
| `agentlaunch/validation_policy.go` | 43 |
| `process/spawn.go` | 41, 44, 92, 269 |
| `providersignal/provider_diagnostics.go` | 19, 43 |
| `providersignal/quota_reset.go` | 59, 101, 140 |

**S-SMALL** - literal 0, 1 or 2 as index, slice bound, capacity, arity or radix (209 occurrences)

| File | Lines |
|---|---|
| `agent/acpx_agent.go` | 64, 89, 97, 102, 107, 141, 149, 152, 168, 257, 258, 293, 421 |
| `agent/acpx_agent_completion.go` | 33, 37, 39, 40 |
| `agent/acpx_agent_session.go` | 33, 37, 41, 45 |
| `agent/agent_output_files.go` | 31, 44, 57 |
| `agent/checkpoint_summary.go` | 192, 401, 473 |
| `agent/claim_breaker.go` | 214, 218, 275 |
| `agent/claiming.go` | 39, 249, 339 |
| `agent/claiming_order.go` | 123, 124, 131 |
| `agent/cli_agent.go` | 89, 92, 96, 101, 150, 160, 163, 271, 305, 330 |
| `agent/cli_agent_command.go` | 135, 139 |
| `agent/cli_agent_completion.go` | 19, 39, 45, 47, 48 |
| `agent/cli_agent_streams.go` | 22 |
| `agent/execution_deadline.go` | 145, 148, 151 |
| `agent/heartbeat.go` | 169 |
| `agent/llm_agent.go` | 257 |
| `agent/orchestrator_dashboard.go` | 103, 195, 242 |
| `agent/orchestrator_verify.go` | 170, 180, 221, 222, 249 |
| `agent/output.go` | 80 |
| `agent/progress_watchdog.go` | 61 |
| `agent/prompt.go` | 235 |
| `agent/prompt_corrective_context.go` | 33, 47 |
| `agent/prompt_integration.go` | 254, 268, 323 |
| `agent/prompt_intelligence.go` | 24, 35, 46, 60, 74, 87 |
| `agent/prompt_planner_context.go` | 185, 222, 223, 324, 350, 381, 455, 456, 477, 497, 499, 529 |
| `agent/prompt_task_context.go` | 112, 130 |
| `agent/prompt_task_graph.go` | 23, 26, 27, 47, 148, 210, 250, 312, 318, 341, 405, 407, 410 |
| `agent/reference_context.go` | 125, 126, 323 |
| `agent/registration.go` | 27, 28, 40, 171, 304 |
| `agent/session_execution.go` | 80, 164, 189, 192, 228, 241, 260, 284, 293 |
| `agent/session_preflight.go` | 75 |
| `agent/strategy_orchestrator.go` | 63, 77 |
| `agent/strategy_reviewer.go` | 178, 179, 296, 364, 509 |
| `agent/supervisor_background.go` | 54, 106, 107 |
| `agent/supervisor_completed_turn.go` | 85, 130, 138 |
| `agent/supervisor_lifetime.go` | 172 |
| `agent/supervisor_restart.go` | 58, 84, 208, 219, 244, 245, 263, 264, 309, 329, 355 |
| `agent/supervisor_runtime_failure.go` | 41 |
| `agent/systemctl.go` | 45 |
| `agent/tool_result_acp.go` | 30, 35 |
| `agent/tool_result_claude.go` | 77, 78 |
| `agent/tool_result_codex.go` | 140, 145, 159, 161 |
| `agent/usage_sink.go` | 183, 185 |
| `agent/waitforwork.go` | 240 |
| `agent/workdetection.go` | 66, 68, 224, 261, 271, 300 |
| `agentlaunch/launch_resolver.go` | 165, 328, 540, 573, 584, 586 |
| `agentlaunch/launch_selection.go` | 75 |
| `agentlaunch/validation_policy.go` | 33, 48 |
| `process/spawn.go` | 79, 177 |
| `providersignal/quota_reset.go` | 53, 59, 62, 91, 101, 104, 105, 147, 149 |

**O-TOOL** - tool-name identity of the catalog (dynamic); tool-name policy is reserved for C2/C3 (21 occurrences)

| File | Lines |
|---|---|
| `agent/acpx_agent.go` | 157, 159, 161 |
| `agent/checkpoint_summary.go` | 439 |
| `agent/cli_agent.go` | 203 |
| `agent/cli_agent_command.go` | 149 |
| `agent/session_preflight.go` | 66 |
| `agent/tool_result_acp.go` | 17 |
| `agentlaunch/cli.go` | 23, 25, 27, 29 |
| `agentlaunch/launch_resolver.go` | 253, 355, 595 |
| `providersignal/quota.go` | 71 |
| `providersignal/quota_reset.go` | 41, 54 |

**S-BOUNDARY** - comparison with 0, 1 or -1 (presence, cardinality, sign, index bound) (181 occurrences)

| File | Lines |
|---|---|
| `agent/acpx_agent.go` | 165 |
| `agent/acpx_agent_session.go` | 76, 90, 101 |
| `agent/agent_output_files.go` | 40, 53 |
| `agent/checkpoint_summary.go` | 192, 284, 287, 371, 386, 400 |
| `agent/claim_breaker.go` | 61, 143, 158, 184 |
| `agent/claiming.go` | 38, 126, 230, 338 |
| `agent/claiming_merge.go` | 68 |
| `agent/claiming_order.go` | 122 |
| `agent/cli_agent.go` | 271 |
| `agent/cli_agent_streams.go` | 21 |
| `agent/heartbeat.go` | 79, 84 |
| `agent/orchestrator_dashboard.go` | 73 |
| `agent/orchestrator_verify.go` | 76, 217, 233 |
| `agent/output.go` | 79 |
| `agent/pause_gate.go` | 49, 50 |
| `agent/planning_complete_verify.go` | 72 |
| `agent/progress_watchdog.go` | 37, 49 |
| `agent/prompt.go` | 58, 107, 234, 237 |
| `agent/prompt_corrective_context.go` | 33, 47, 76 |
| `agent/prompt_integration.go` | 92, 160, 279, 291, 323 |
| `agent/prompt_intelligence.go` | 21, 32, 43, 57, 71, 84 |
| `agent/prompt_planner_context.go` | 102, 104, 109, 172, 182, 221, 272, 285, 307, 310, 315, 323, 349, 356, 371, 419, 422, 436, 452, 477, 508 |
| `agent/prompt_task_context.go` | 112, 130, 152 |
| `agent/prompt_task_graph.go` | 22, 46, 68, 173, 290, 406, 447 |
| `agent/quota_termination_wip.go` | 17 |
| `agent/reference_context.go` | 182, 205, 212, 249, 262, 300 |
| `agent/registration.go` | 28, 35, 407, 435, 636, 669 |
| `agent/session_execution.go` | 262 |
| `agent/session_preflight.go` | 140 |
| `agent/strategy_doer.go` | 27, 82, 91, 100 |
| `agent/strategy_orchestrator.go` | 70, 90, 134, 135, 296, 304, 307 |
| `agent/strategy_reviewer.go` | 78, 87, 258, 307, 311, 314, 397, 501, 541, 554 |
| `agent/supervisor.go` | 83 |
| `agent/supervisor_completed_turn.go` | 77, 105 |
| `agent/supervisor_lifetime.go` | 193, 196 |
| `agent/supervisor_restart.go` | 185, 193, 207, 210, 430, 437 |
| `agent/supervisor_runtime_failure.go` | 126 |
| `agent/systemctl.go` | 44 |
| `agent/tool_result_acp.go` | 35, 46 |
| `agent/tool_result_codex.go` | 94, 153, 159, 161 |
| `agent/usage_sink.go` | 167, 169 |
| `agent/waitforwork.go` | 26 |
| `agent/workdetection.go` | 65, 159, 181, 187, 255, 260 |
| `agent/worktree_check.go` | 251 |
| `agentlaunch/cli.go` | 53 |
| `agentlaunch/launch_resolver.go` | 247, 351, 405, 422, 425, 428, 431, 434, 437, 452, 455, 458, 461, 562, 609 |
| `agentlaunch/launch_selection.go` | 65, 72 |
| `agentlaunch/validation_policy.go` | 60 |
| `providersignal/quota_reset.go` | 90 |

**F-FORMAT** - separator, delimiter, quoting, trim set or search token of text handling; not vocabulary (45 occurrences)

| File | Lines |
|---|---|
| `agent/agent_output_files.go` | 27 |
| `agent/checkpoint_summary.go` | 403, 408 |
| `agent/claiming_merge.go` | 266 |
| `agent/cli_agent.go` | 256 |
| `agent/orchestrator_dashboard.go` | 98 |
| `agent/planning_complete_verify.go` | 74 |
| `agent/prompt_planner_context.go` | 105, 185, 362, 377 |
| `agent/prompt_task_graph.go` | 417, 418, 421 |
| `agent/reference_context.go` | 301 |
| `agent/registration.go` | 110, 113 |
| `agent/session_preflight.go` | 44, 119, 123, 141 |
| `agent/strategy_orchestrator.go` | 389 |
| `agent/supervisor_background.go` | 57, 101 |
| `agent/supervisor_runtime_failure.go` | 58, 115 |
| `agent/tool_result_claude.go` | 33 |
| `agent/tool_result_codex.go` | 87, 145, 146, 165 |
| `agentlaunch/launch_resolver.go` | 565, 567 |
| `agentlaunch/role_models.go` | 61, 83 |
| `process/spawn.go` | 54, 254 |
| `providersignal/provider_diagnostics.go` | 10, 17, 54 |
| `providersignal/quota_reset.go` | 75, 115, 193 |

**P-ENV** - environment variable name or flag value of a provider or of the engine (external interface) (2 occurrences)

| File | Lines |
|---|---|
| `agent/checkpoint_summary.go` | 475 |
| `agent/cli_agent_command.go` | 23 |

**S-CAP** - slice capacity hint (entries appended after the copy) (1 occurrences)

| File | Lines |
|---|---|
| `agent/cli_agent.go` | 305 |

**N-DECL** - already the value of a named package variable (5 occurrences)

| File | Lines |
|---|---|
| `agent/execution_deadline.go` | 15, 24 |
| `agent/strategy_reviewer.go` | 28, 34 |
| `agent/waitforwork.go` | 60 |

**O-SECTION** - context-section identity (YAML-defined, ADR-0045) (3 occurrences)

| File | Lines |
|---|---|
| `agent/prompt.go` | 287, 294 |

**S-REGEX** - regular-expression capture-group index or match arity (10 occurrences)

| File | Lines |
|---|---|
| `agent/prompt_planner_context.go` | 501, 531 |
| `agent/tool_result_claude.go` | 79 |
| `providersignal/quota_reset.go` | 49, 60, 62 |

**S-STDLIB** - standard-library argument (parse base and bit size) (3 occurrences)

| File | Lines |
|---|---|
| `agent/session_preflight.go` | 71, 81 |

**S-COUNT** - relative count comparison of two computed counts (1 occurrences)

| File | Lines |
|---|---|
| `agent/strategy_reviewer.go` | 357 |

**R-C1** - lease duration 1800 s, reserved for C1 (Scope 22) (2 occurrences)

| File | Lines |
|---|---|
| `agent/strategy_reviewer.go` | 393 |
| `agent/supervisor_lifetime.go` | 129 |

**P-FLAG** - provider CLI flag spelling (external interface) (4 occurrences)

| File | Lines |
|---|---|
| `agent/tool_result_acp.go` | 35 |
| `agent/tool_result_claude.go` | 63 |

**P-PLATFORM** - runtime.GOOS platform name (standard-library vocabulary) (1 occurrences)

| File | Lines |
|---|---|
| `agent/tool_result_codex.go` | 34 |

**S-VERSION** - zero test of a version component (prerelease rule), not a threshold (1 occurrences)

| File | Lines |
|---|---|
| `agent/tool_result_codex.go` | 159 |

**O-ROLEPAIR** - role-pair name (open string; the ops constant is unexported) (1 occurrences)

| File | Lines |
|---|---|
| `agent/workdetection.go` | 284 |

**O-TOOL** - tool-name alias derivation of agentlaunch ("acpx-" prefix, "-acp" suffix), retained as is (4 occurrences)

| File | Lines |
|---|---|
| `agentlaunch/launch_resolver.go` | 598, 599, 601, 602 |


Typed vocabulary adopted in `internal/agent` (each replaced a literal of the same
value; class T): `roles.RoleTypeDoer`, `RoleTypeReviewer` and
`RoleTypeOrchestrator` in `checkpoint_summary.go`, `registration.go`,
`review_execution.go` and `strategy.go`; `roles.ClaimDoer` and `ClaimReviewer` as
the type of `supervisorTaskSlot` in `supervisor_block.go`;
`models.DiscoveryUrgencyImmediate` in `strategy_orchestrator.go` and
`workdetection.go`; `prompts.IntegrationViewComplete` in `workdetection.go`;
`pipeline.ProviderDiversityPreferred` in `claiming_merge.go` `evaluateMergeGate`;
`models.RoleCoder` and `models.RoleCodeReviewer` in `strategy_doer.go` and
`strategy_reviewer.go`; `humanNoteTargetAll`, already named by A8, in
`strategy_orchestrator.go`; `exitCodeGracefulAbort` in
`quota_termination_wip.go`; `alerts.AlertLevelCritical` and `AlertLevelWarning`
through `alertLevelCritical` and `alertLevelWarning`.

Defined types introduced by this unit for finite sets that select a parsing
branch (`agent/stream_vocabulary.go`): `streamEventKind` with
`streamTaskNotification` ("task_notification") and `streamToolResult`
("tool_result"), read from provider session output in `supervisor_background.go`
(`detectAbandonedBackgroundTask`, `deliberatelyStoppedTaskIDs`), and
`acpSessionUpdateKind` with `acpSessionUpdateMessageChunk` ("agent_message_chunk"),
read in `acpxAgentMessageChunk`. The spellings are the providers' own, so the
types live in `agent`, next to the readers. Conversions are explicit at the
boundaries that hold plain strings: `strings.Contains(line, string(kind))`,
`streamEventKind(env.Subtype) != streamTaskNotification` for the decoded JSON
subtype, and `update[acpSessionUpdateKey] != string(acpSessionUpdateMessageChunk)`
for the ACP update decoded as `map[string]any`, where a boxed defined type would
never equal the decoded string. Unknown values behave as before (they match no
kind). `TestA9ACPUpdateKindMatchesDecodedStrings` pins that boundary, including
the boxed-value case, and `TestA9StreamEventKindsSelectOnlyTheirSpelling` the
subtype selection; removing the `string(...)` conversion makes the former fail.
`LLMAgentEventKind` (`LLMAgentEventMessage`) spells the same text but names the
runtime's own event kind, not the provider's wire value, so it is not adopted.
The JSON line prefix, the ACP discriminator key (a map key) and the verdict
command's spelling and failure code (compared as text of the command's own
output) are syntax, keys and message text, not finite kinds, and stay named
strings.

Constants named by this unit (class N; values unchanged and pinned by
`TestA9NamedVocabularyKeepsStringValues`, `TestA9NamedVocabularyKeepsNumericValues`
and `TestA9BackoffAndRankConstantsKeepValues` in `internal/agent`, and the
`a9_vocabulary_test.go` files of `agentlaunch`, `providersignal` and `process`):

| Definition | Constant | Value |
|---|---|---|
| `agent/claiming_order.go:11`, `:15` | `minOrderableCandidates`, `doerClaimRank` (`rankOwnRejection`, `rankOtherRework`, `rankFresh`) | 2; 0, 1, 2 |
| `agent/orchestrator_verify.go:246` | `hypothesisExhaustionMinFailures` (mirrors the unexported `ops` constant) | 2 |
| `agent/orchestrator_dashboard.go:188` | `entryPointValueParts` | 2 |
| `agent/pause_gate.go:118` | `pausePollInterval` | 5 s |
| `agent/progress_watchdog.go:226`-`:228` | `progressPollDivisor`, `minProgressPollInterval`, `maxProgressPollInterval` | 4; 100 ms; 15 s |
| `agent/usage_sink.go:16` | `supervisorRunIDBytes` | 16 |
| `agent/output.go:14`, `:15` | `runtimeDirMode`, `runtimeFileMode` | 0755, 0644 |
| `agent/supervisor_restart.go:202`, `:203` | `exit42BackoffBaseDelay`, `restartBackoffFactor` | 2 s; 2 |
| `agent/claim_breaker.go:27` | `claimBackoffFactor` | 2 |
| `agent/tool_result_claude.go:47`-`:52`, `:57` | `claudeToolResultMinMajor`, `claudeToolResultMinMinor`, `claudeToolResultMinPatch`, `claudeToolResultHookTimeoutSeconds`, `claudeVersionMatchLen`, `toolResultVersionProbeTimeout` | 2, 1, 267; 60; 4; 5 s |
| `agent/tool_result_codex.go:131`-`:135` | `codexVersionProgram`, `codexVersionFields`, `codexVersionComponents`, `codexToolResultMinMinor`, `toolResultScriptMode` | "codex-cli"; 2; 3; 154; 0700 |
| `agent/stream_vocabulary.go` | `streamJSONObjectPrefix`, `acpSessionUpdateKey`, `submitVerdictFailedCode`, `submitVerdictCommand` (plain named strings; the typed kinds are described above) | the spellings of the provider streams and the verdict command failure code |
| `agent/alert_vocabulary.go:9`-`:22` | `alertLevelCritical`, `alertLevelWarning` and eleven `alertCategory…` | the spellings written to `alerts.log` |
| `agentlaunch/launch_resolver.go:47`, `:48` | `templateExprOpen`, `templateExprClose` | "{{", "}}" |
| `providersignal/vocabulary.go:50`, `:51` | `jsonObjectLinePrefix`, `jsonArrayLinePrefix` | "{", "[" |
| `process/spawn.go:34` | `supervisorOutputDirMode` | 0755 |

The earlier A9 units named their vocabulary in `56c581083` and `85371de10`
(launch selection source, backend and transport types, signal record, alert
category and provider event vocabulary), `4716fdf1d` (backend literals) and
`75eb56dde` (`exitCodeGracefulAbort`, restart and claim delays, turn outcome
type). The C1 `"terminal-1"` argument and the 1800 lease stay for C1. Role names,
role-pair names, tool names, profile names, context-section identities and
configured values stay open strings.

### Moved sites for the later C scopes

- **C6 (mutation closures in `agent`; Modify call sites 18 before and 18 after).**
  Count: `git grep -n -E 'Modify[A-Za-z]*\(' <rev> -- 'internal/agent/*.go' ':!internal/agent/*_test.go'`
  matches 19 lines at `3b9ba5d94` and at `633a39144`, one of which is the test
  hook call `supervisorBlockBeforeModifyTestHook()`; the file:line lists of both
  revisions are in the evidence record. The restart closure moved from `exit42RestartTracker.Handle` to
  `persistExit42Restart` (`supervisor_restart.go`) with the block body in
  `blockExit42RestartLoop`; the recovery history closure moved from
  `ensureReviewerWorktreeLocked` to `recordReviewerWorktreeRecovery`
  (`worktree_check.go`). The supervisor unit moved the registration, release and
  block closures into `supervisor_lifetime.go`, `supervisor_turn.go` and
  `supervisor_completed_turn.go`. No closure moved into `ops`, `providersignal`
  or `agentlaunch`.
- **C1.** `reviewerStrategy.ClaimTask` (`strategy_reviewer.go`) and
  `supervisorLifetime.register` (`supervisor_lifetime.go`) keep their 1800 s
  arguments and the `"terminal-1"` literal; heartbeat constants are untouched.
- **C2.** `evaluateMergeGate` moved with the merge batch from `claiming.go` to
  `claiming_merge.go` and still compares stored provider values; the O-TOOL rows
  above are the tool-name sites C2 and C3 own, in `agent`, `agentlaunch`
  (`launch_resolver.go`, `cli.go`) and `providersignal` (`quota.go`,
  `quota_reset.go`).
- **C3.** Stop-signal matching lives in `providersignal` unchanged
  (`provider_unavailable.go`, `quota.go`); the alias derivation is
  `agentlaunch.ACPXAgentName`.
- **D33.** Git or `exec` inside the `Modify` callbacks of the moved sites
  (`persistExit42Restart` with `blockExit42RestartLoop`,
  `recordReviewerWorktreeRecovery`, `blockReviewerTask`, `handleCleanTaskCleanup`):
  0 before, 0 after, read in full; `blockExit42RestartLoop` still reads the pipeline
  configuration files inside the callback exactly as `Handle` did. `git grep -h -E
  'CheckCurrent\(|CheckLaunchCurrent\(' <rev> -- 'internal/agent/*.go'
  ':!internal/agent/*_test.go'` prints 1 line at `3b9ba5d94`, `bf0cb8359` and
  `633a39144` (`newTaskProviderLaunchGate`, outside any `Modify`).

### Shim and exported-API audit

Over `git diff -U0 3b9ba5d94 633a39144` of the `agent`, `agentlaunch`,
`providersignal`, `process`, `commands` and `tui` production files, piped to
`rg -c '^\+\s*(type \w+ = |(var|const) \w+ = (agentlaunch|providersignal|agent)\.\w+$|\w+ += (agentlaunch|providersignal|agent)\.\w+$)'`
(no match, exit 1) and read for single-return delegations: no added
`type X = Y` alias, no added `var` or `const` of the form `X = agentlaunch.Y`,
`providersignal.Y` or `agent.Y`, and no added single-return function delegating
to `agentlaunch` or `providersignal`. `LogProviderAuditDegradedAlert` pre-dates
the unit and formats the audit alert message and category, so it is not a
forwarder. The Shim Register stays empty.

### Assertion preservation

White-box tests moved with their code in the earlier units. This unit changes one
existing test file, `cmd/liza/agent_generation_e2e_test.go`, by location only:
the exhaustive fence inventory finds each authority-bound call by the declaration
that holds it, so its entries `claim-doer`, `resume-handoff`, `resume-owned-task`,
`clear-claim-degradation` and `approved-merge-dispatch` now name `claimDoerCandidate`,
`resumeDoerTask` (twice), `clearDegradedAfterClaim` and `mergeApprovedTask` (in
`claiming_merge.go`); operations, authority fields and bindings asserted are
unchanged. It adds `a9_concerns_pb_test.go` and `a9_vocabulary_test.go`
(agent), `a9_vocabulary_test.go` in `agentlaunch`, `providersignal` and
`process`, and `TestAreaA9RatchetIsEmpty`. The PB map of this unit:

| PB | Concern | Tests |
|---|---|---|
| 1 | handoff, owned, fresh priority | `TestClaimDoerTask_ResumesHandoffBeforeOwnedTask`, `TestClaimDoerTask_ResumesOwnedTaskBeforeFreshClaim`, `TestClaimDoerScheduling*`, `TestClaimCoderTask_*`, `TestA9ClaimPB1NilAuthorityFreshClaimNamesNoClaimableTasks`, `TestOracleA9DoerClaim` |
| 2 | nil-authority compatibility paths | `TestClaimCoderTask_*`, `TestHandleApprovedMerges_DoesNotEmitCheckpointSummary`, `TestApprovedMergeTakeoverAfterFinalApproverExit`, `TestSupervisorOwnedMutationGenerationFence` |
| 3 | per-candidate merge policy and execution, takeover | `TestMergeGate*`, `TestApprovedMergeTakeoverAfterFinalApproverExit`, `TestA9ClaimPB3MergeBatchContinuesPastFailedCandidate`, `TestA9ClaimPB3MergeBatchIgnoresMergesOwnedByAnotherReviewer`, `TestOracleA9PendingMergeStall` |
| 4 | in-memory progress and backoff | `TestExit42RestartTracker_ExponentialBackoffAndCap`, `TestExit42TaskProgressSignatureIgnoresClaimIteration`, `TestA9RestartPB4ProgressResetsCountAndTaskLessKeyIsPerAgent`, `TestOracleA9Restart42` |
| 5 | fenced persistence and the restart-loop block | `TestExit42RestartTracker_Blocking`, `TestExit42RestartTracker_BlocksNonCoderRoles`, `TestA9RestartPB5BlockRecordsReasonQuestionsAndHistory`, `TestA9RestartPB5UnownedTaskIsCountedButNeverBlocked`, `TestOracleA9Restart42Limit` |
| 6 | cancellation, stop and surplus precedence, polling fallback, max wait | `TestWaitForWorkCancellation`, `TestWaitForWorkTimeout`, `TestAbortPrecedenceOverWork`, `TestWaitForWork_AbortTickerDetectsWork_TOCTOU`, `TestA9WaitPB6*`, `TestOracleA9SurplusRetire` |
| 7 | eligibility, attachment, preparation, history, failure classes | `TestEnsureReviewerWorktree_*`, `TestOracleA9ReviewerWorktree` |

Pre-edit evidence is in
`specs/architecture/consolidation-run/a9-closure-evidence.md`: the focused
command printed `ok  github.com/liza-mas/liza/internal/agent  52.911s` on the
unchanged production tree `bf0cb8359`, where the PB tests were first committed.
The PB-3 batch test was then revised (its first fixture never reached the gate,
found by the negative check), so the final file was reproduced on the same
production tree in a disposable detached worktree, removed afterwards: every
`TestA9Claim*`, `TestA9Restart*` and `TestA9Wait*` test passed. Negative checks
on the final code, each reverted: the batch test fails when a non-halt merge
failure returns instead of continuing; the wait fallback tests fail without the
polling fallback; the surplus test fails without the surplus check.

### Chain-document references

For `INVARIANTS.md`, `state-machines.md`, `blackboard-schema.md`,
`supervision-model.md`, `specs/protocols` and `lessons`, every backticked Go file
name was resolved against the tree, and every function or type that moved or left
the four packages since `3b9ba5d94` was searched. The earlier units updated the
citations they invalidated. This unit's files (`claiming.go`,
`supervisor_restart.go`, `waitforwork.go`, `worktree_check.go`) are cited only by
`INVARIANTS.md:253`, which still describes `claiming.go` and `worktree_check.go`
correctly, and `supervision-model.md:59`, which names `handleApprovedMerges`
without a path and keeps its name in `claiming_merge.go`. No location needed
correcting. Pre-existing drift, not A9: `blackboard-schema.md` lines 350 and 358
carry `auth.go` inside a sample YAML scope value, not a repository citation.

### Closure evidence for the A9 boundary (Scope 25)

- **Commands Layer Imports Agent Runtime** (`issue-crossing`): resolved for every
  capability that is not the runtime loop. `commands` and `tui` import `agent`
  only for the three read-only queries of D3; the two remaining allowlist entries
  carry per-symbol rationale. The related **No Query Layer** issue stays open:
  the wake projection and the pause reason stay queries in `agent`, no query
  package was created.
- **Decompose supervisor.go** (`issue-supervisor`): delivered. `supervisor.go` is
  171 lines, `RunSupervisor` 29 lines over the files of D4; the A9 ratchet and S2
  are empty (`TestAreaA9RatchetIsEmpty`); the largest A9 function is
  `releaseTaskClaim` at 99 lines and the largest A9 file `registration.go` at 678
  lines (`go/parser` measurement at the closure commit).
- **Provider Stop-Signal Recovery Has Multiple Owners** (`issue-stop-signal`)
  stays open: the primitives moved to `providersignal`, but the signal cleanup,
  target-state selection and reporting policies of `commands/resume` and `tui`
  are as before; one owning operation is a later change.
- **Control-Flow Vocabulary Bypasses Domain Ownership** (`issue-vocabulary`)
  stays open: A9 adopted the delivered constants and named its own, but the sets
  of other areas, the tool-name policy (C2, C3) and the unexported `ops` constants
  are outside it.

## Consequences

- `ops` can evaluate launch and provider-signal facts without importing the
  runtime, and presentation depends on `agent` for three read-only queries only.
- Every runtime function is under 100 lines and every file under 800; the guard
  keeps it so with an empty ratchet.
- The restart tracker, merge batch, wait loop and recovery each have one place
  for their calculation and one for their effect, so the C6 move of the fenced
  closures does not edit the calculation.
- `agent` keeps a mirrored copy of one `ops` threshold
  (`hypothesisExhaustionMinFailures`) until `ops` exports an owner.

## Alternatives Considered

1. **Merging `agentlaunch` into `providers`**: rejected; `providers` stays the
   catalog owner and the resolver reads configuration and role models.
2. **A shared query package for the wake projection**: rejected; the projection
   needs `ops` and prompts, which infrastructure must not import.
3. **A generic backend algorithm for CLI and ACP**: rejected; they differ in start
   and wait and share only existing mechanics.
4. **Fixed-size function slices** instead of concern helpers: rejected (S3).
5. **Exporting the `ops` hypothesis threshold** from this unit: rejected as an
   edit of a closed area for one number; recorded as the owner gap above.
