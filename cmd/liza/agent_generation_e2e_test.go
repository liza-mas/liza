package main

import (
	"bytes"
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	agentruntime "github.com/liza-mas/liza/internal/agent"
	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/testhelpers"
)

const (
	e2eGenerationA = "generation-a"
	e2eGenerationB = "generation-b"
)

type atomicFenceInventoryEntry struct {
	name             string
	command          string
	sourceFile       string
	declaration      string
	call             string
	authorityField   string
	authorityBinding string
}

var atomicGenerationFenceInventory = []atomicFenceInventoryEntry{
	{name: "submit-for-review", command: "submit-for-review", sourceFile: "cmd/liza/cmd_review.go", declaration: "submitForReviewCmd", call: "ops.SubmitForReviewWithAuthority", authorityBinding: "authority"},
	{name: "handoff", command: "handoff", sourceFile: "cmd/liza/cmd_review.go", declaration: "handoffCmd", call: "ops.Handoff", authorityField: "Authority", authorityBinding: "&authority"},
	{name: "submit-verdict", command: "submit-verdict", sourceFile: "cmd/liza/cmd_review.go", declaration: "submitVerdictCmd", call: "ops.SubmitVerdictWithAuthority", authorityBinding: "authority"},
	{name: "await-verdict", command: "await-verdict", sourceFile: "cmd/liza/cmd_review.go", declaration: "awaitVerdictCmd", call: "awaitVerdict", authorityBinding: "authority"},
	{name: "await-resubmission", command: "await-resubmission", sourceFile: "cmd/liza/cmd_review.go", declaration: "awaitResubmissionCmd", call: "awaitResubmission", authorityBinding: "authority"},
	{name: "wt-merge", command: "wt-merge", sourceFile: "cmd/liza/cmd_worktree.go", declaration: "wtMergeCmd", call: "ops.MergeWorktreeWithAuthority", authorityBinding: "authority"},
	{name: "claim-task", command: "claim-task", sourceFile: "cmd/liza/cmd_task.go", declaration: "claimTaskCmd", call: "ops.ClaimTaskWithAuthority", authorityBinding: "authority"},
	{name: "mark-blocked", command: "mark-blocked", sourceFile: "cmd/liza/cmd_task.go", declaration: "markBlockedCmd", call: "ops.MarkBlockedWithAuthority", authorityBinding: "authority"},
	{name: "write-checkpoint", command: "write-checkpoint", sourceFile: "cmd/liza/cmd_task.go", declaration: "writeCheckpointCmd", call: "ops.WriteCheckpointWithAuthority", authorityBinding: "authority"},
	{name: "set-task-output", command: "set-task-output", sourceFile: "cmd/liza/cmd_task.go", declaration: "setTaskOutputCmd", call: "ops.SetTaskOutputWithAuthority", authorityBinding: "authority"},
	{name: "add-task", command: "add-task", sourceFile: "cmd/liza/cmd_task.go", declaration: "addTaskCmd", call: "ops.AddTaskWithAuthority", authorityBinding: "authority"},
	{name: "add-tasks", command: "add-tasks", sourceFile: "cmd/liza/cmd_task.go", declaration: "addTasksCmd", call: "ops.AddTasksWithAuthority", authorityBinding: "authority"},
	{name: "supersede-task", command: "supersede-task", sourceFile: "cmd/liza/cmd_task.go", declaration: "supersedeTaskCmd", call: "ops.SupersedeTaskWithAuthority", authorityBinding: "authority"},
	{name: "retarget-dependency", command: "retarget-dependency", sourceFile: "cmd/liza/cmd_task.go", declaration: "retargetDependencyCmd", call: "ops.RetargetDependencyWithAuthority", authorityBinding: "authority"},
	{name: "apply-dependency-repair", command: "apply-dependency-repair", sourceFile: "cmd/liza/cmd_task.go", declaration: "applyDependencyRepairCmd", call: "ops.ApplyDependencyRepairWithAuthority", authorityBinding: "authority"},
	{name: "repair-superseded-dependencies", command: "repair-superseded-dependencies", sourceFile: "cmd/liza/cmd_task.go", declaration: "repairSupersededDependenciesCmd", call: "ops.RepairSupersededDependenciesWithAuthority", authorityBinding: "authority"},
	{name: "unblock-task", command: "unblock-task", sourceFile: "cmd/liza/cmd_task.go", declaration: "unblockTaskCmd", call: "ops.UnblockTaskWithAuthority", authorityBinding: "authority"},
	{name: "assess-blocked", command: "assess-blocked", sourceFile: "cmd/liza/cmd_task.go", declaration: "assessBlockedCmd", call: "ops.AssessBlockedWithAuthority", authorityBinding: "authority"},
	{name: "assess-hypothesis-exhausted", command: "assess-hypothesis-exhausted", sourceFile: "cmd/liza/cmd_task.go", declaration: "assessHypothesisExhaustedCmd", call: "ops.AssessHypothesisExhaustedWithAuthority", authorityBinding: "authority"},
	{name: "cancel-task", command: "cancel-task", sourceFile: "cmd/liza/cmd_task.go", declaration: "cancelTaskCmd", call: "ops.CancelTaskWithAuthority", authorityBinding: "authority"},
	{name: "reconcile-merged", command: "reconcile-merged", sourceFile: "cmd/liza/cmd_task.go", declaration: "reconcileMergedCmd", call: "ops.ReconcileMergedWithAuthority", authorityBinding: "authority"},
	{name: "claim-doer", sourceFile: "internal/agent/claiming.go", declaration: "claimDoerTaskWithOptionalAuthority", call: "ops.ClaimTaskWithAuthority", authorityBinding: "*authority"},
	{name: "resume-handoff", sourceFile: "internal/agent/claiming.go", declaration: "claimDoerTaskWithOptionalAuthority", call: "ops.ResumeHandoff", authorityField: "Authority", authorityBinding: "authority"},
	{name: "resume-owned-task", sourceFile: "internal/agent/claiming.go", declaration: "claimDoerTaskWithOptionalAuthority", call: "ops.ResumeOwnedTask", authorityField: "Authority", authorityBinding: "authority"},
	{name: "claim-degradation", sourceFile: "internal/agent/claiming.go", declaration: "markAgentDegradedForInfraClaim", call: "ops.MarkAgentDegraded", authorityField: "Authority", authorityBinding: "authority"},
	{name: "clear-claim-degradation", sourceFile: "internal/agent/claiming.go", declaration: "claimDoerTaskWithOptionalAuthority", call: "ops.ClearAgentDegradedWithAuthority", authorityBinding: "*authority"},
	{name: "claim-reviewer", sourceFile: "internal/agent/claiming.go", declaration: "claimReviewerTaskForRoleWithOptionalAuthority", call: "ops.ClaimReviewerTask", authorityField: "Authority", authorityBinding: "authority"},
	{name: "release-reviewer-claim", sourceFile: "internal/agent/claiming.go", declaration: "releaseReviewerClaimQuietly", call: "ops.ReleaseClaimWithAuthority", authorityBinding: "authority"},
	{name: "watchdog-block", sourceFile: "internal/agent/supervisor.go", declaration: "blockTaskFromSupervisor", call: "ops.ModifyWithAgentAuthority", authorityBinding: "authority"},
	{name: "approved-merge-dispatch", sourceFile: "internal/agent/claiming.go", declaration: "handleApprovedMergesWithOptionalAuthority", call: "ops.MergeWorktreeWithAuthority", authorityBinding: "*authority"},
}

func TestCLIStaleSupervisorGenerationEndToEnd(t *testing.T) {
	t.Run("doer claim resume heartbeat and cleanup", testE2EDoerGenerationFence)
	t.Run("reviewer command", testE2EReviewerGenerationFence)
	t.Run("orchestrator command", testE2EOrchestratorGenerationFence)
	t.Run("await command", testE2EAwaitGenerationFence)
	t.Run("merge command", testE2EMergeGenerationFence)
	t.Run("provider launch environment", testE2EProviderGenerationFence)
	t.Run("exhaustive operation inventory", testE2EAtomicFenceInventory)
}

func testE2EDoerGenerationFence(t *testing.T) {
	const (
		agentID = "coder-42"
		taskID  = "task-e2e-claim"
	)
	projectRoot, statePath := setupMutationTestProject(t, func(state *models.State) {
		state.Tasks = []models.Task{testhelpers.BuildTaskByStatus(taskID, models.TaskStatusReady, time.Now().UTC())}
		state.Agents[agentID] = e2eAgent(models.RoleCoder, e2eGenerationA)
	})

	runStaleGenerationCLI(t, projectRoot, statePath, agentID,
		"claim-task", taskID, agentID,
	)
	runCurrentGenerationCLI(t, projectRoot, e2eGenerationB,
		"claim-task", taskID, agentID,
	)

	bb := db.For(statePath)
	if err := bb.Modify(func(state *models.State) error {
		task := state.FindTask(taskID)
		if task == nil {
			return fmt.Errorf("task %s missing", taskID)
		}
		task.HandoffPending = true
		agent := state.Agents[agentID]
		agent.Status = models.AgentStatusHandoff
		agent.CurrentTask = testhelpers.StringPtr(taskID)
		state.Agents[agentID] = agent
		return nil
	}); err != nil {
		t.Fatalf("prepare handoff: %v", err)
	}

	stale := models.AgentAuthority{ID: agentID, Generation: e2eGenerationA}
	current := models.AgentAuthority{ID: agentID, Generation: e2eGenerationB}
	beforeResume := mustReadE2EStateBytes(t, statePath)
	_, err := ops.ResumeHandoff(ops.ResumeHandoffInput{ProjectRoot: projectRoot, AgentID: agentID, Authority: &stale})
	assertE2EAuthorityError(t, err, agentID)
	assertE2EStateBytes(t, statePath, beforeResume, "stale handoff resume")
	result, err := ops.ResumeHandoff(ops.ResumeHandoffInput{ProjectRoot: projectRoot, AgentID: agentID, Authority: &current})
	if err != nil || result == nil || !result.Found {
		t.Fatalf("current handoff resume = (%#v, %v), want found", result, err)
	}

	beforeHeartbeat := mustReadE2EStateBytes(t, statePath)
	heartbeatCtx, cancelHeartbeat := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelHeartbeat()
	err = agentruntime.NewHeartbeat(agentruntime.HeartbeatConfig{
		Authority:     stale,
		StatePath:     statePath,
		Interval:      time.Millisecond,
		LeaseDuration: time.Minute,
	}).Start(heartbeatCtx)
	assertE2EAuthorityError(t, err, agentID)
	assertE2EStateBytes(t, statePath, beforeHeartbeat, "stale heartbeat")

	beforeCleanup := mustReadE2EStateBytes(t, statePath)
	err = ops.ModifyWithAgentAuthority(bb, stale, func(state *models.State) error {
		delete(state.Agents, agentID)
		return nil
	})
	assertE2EAuthorityError(t, err, agentID)
	assertE2EStateBytes(t, statePath, beforeCleanup, "stale cleanup")
	assertNoE2EGenerationA(t, beforeCleanup)
}

func testE2EReviewerGenerationFence(t *testing.T) {
	const (
		agentID = "code-reviewer-1"
		taskID  = "task-e2e-review"
	)
	projectRoot, statePath := setupMutationTestProject(t, func(state *models.State) {
		state.Tasks = []models.Task{testhelpers.BuildTaskByStatus(taskID, models.TaskStatusReviewing, time.Now().UTC())}
		state.Agents[agentID] = e2eAgent(models.RoleCodeReviewer, e2eGenerationA)
	})

	runStaleGenerationCLI(t, projectRoot, statePath, agentID,
		"submit-verdict", taskID, "APPROVED", "--agent-id", agentID,
	)
	runCurrentGenerationCLI(t, projectRoot, e2eGenerationB,
		"submit-verdict", taskID, "APPROVED", "--agent-id", agentID,
	)
	assertNoE2EGenerationA(t, mustReadE2EStateBytes(t, statePath))
}

func testE2EOrchestratorGenerationFence(t *testing.T) {
	const (
		agentID = "orchestrator-1"
		taskID  = "task-e2e-cancel"
	)
	projectRoot, statePath := setupMutationTestProject(t, func(state *models.State) {
		state.Tasks = []models.Task{testhelpers.BuildTaskByStatus(taskID, models.TaskStatusReady, time.Now().UTC())}
		state.Agents[agentID] = e2eAgent(models.RoleOrchestrator, e2eGenerationA)
	})

	runStaleGenerationCLI(t, projectRoot, statePath, agentID,
		"cancel-task", taskID, "e2e cancellation", "--agent-id", agentID,
	)
	runCurrentGenerationCLI(t, projectRoot, e2eGenerationB,
		"cancel-task", taskID, "e2e cancellation", "--agent-id", agentID,
	)
	assertNoE2EGenerationA(t, mustReadE2EStateBytes(t, statePath))
}

func testE2EAwaitGenerationFence(t *testing.T) {
	const (
		agentID = "coder-1"
		taskID  = "task-e2e-await"
	)
	projectRoot, statePath := setupMutationTestProject(t, func(state *models.State) {
		now := time.Now().UTC()
		task := testhelpers.BuildTaskByStatus(taskID, models.TaskStatusReadyForReview, now)
		task.History = append(task.History, models.TaskHistoryEntry{
			Time: now.Add(-2 * time.Second), Event: models.TaskEventSubmittedForReview, Agent: testhelpers.StringPtr(agentID),
		})
		state.Tasks = []models.Task{task}
		state.Agents[agentID] = e2eAgent(models.RoleCoder, e2eGenerationA)
	})

	runStaleGenerationCLI(t, projectRoot, statePath, agentID,
		"await-verdict", taskID, "--agent-id", agentID, "--timeout-seconds", "1",
	)
	beforeCurrent := mustReadE2EStateBytes(t, statePath)
	envelope := runCurrentGenerationCLI(t, projectRoot, e2eGenerationB,
		"await-verdict", taskID, "--agent-id", agentID, "--timeout-seconds", "1",
	)
	result, ok := envelope["result"].(map[string]any)
	if !ok || result["verdict"] != ops.VerdictTimeout {
		t.Fatalf("current await result = %#v, want terminal %s", envelope["result"], ops.VerdictTimeout)
	}
	state := readState(t, statePath)
	task := state.FindTask(taskID)
	if task == nil || task.AssignedTo != nil {
		t.Fatalf("current await task = %#v, want released doer assignment", task)
	}
	afterCurrent := mustReadE2EStateBytes(t, statePath)
	if bytes.Equal(afterCurrent, beforeCurrent) {
		t.Fatal("current await did not persist generation-B budget cleanup")
	}
	assertNoE2EGenerationA(t, afterCurrent)
}

func testE2EMergeGenerationFence(t *testing.T) {
	const (
		agentID = "code-reviewer-1"
		taskID  = "task-e2e-merge"
	)
	projectRoot, statePath, integrationBefore := setupE2EMergeProject(t, taskID, agentID)
	runStaleGenerationCLI(t, projectRoot, statePath, agentID,
		"wt-merge", taskID, "--agent-id", agentID,
	)
	if integrationAfter := testhelpers.MustGit(t, projectRoot, "rev-parse", "refs/heads/integration"); integrationAfter != integrationBefore {
		t.Fatalf("stale merge advanced integration to %s, want %s", integrationAfter, integrationBefore)
	}

	runCurrentGenerationCLI(t, projectRoot, e2eGenerationB,
		"wt-merge", taskID, "--agent-id", agentID,
	)
	state := readState(t, statePath)
	if task := state.FindTask(taskID); task == nil || task.Status != models.TaskStatusMerged {
		t.Fatalf("current merge task = %#v, want MERGED", task)
	}
	assertNoE2EGenerationA(t, mustReadE2EStateBytes(t, statePath))
}

func testE2EProviderGenerationFence(t *testing.T) {
	const agentID = "coder-1"
	projectRoot, statePath := setupMutationTestProject(t, func(state *models.State) {
		state.Agents[agentID] = e2eAgent(models.RoleCoder, e2eGenerationB)
	})
	binDir := t.TempDir()
	providerPath := filepath.Join(binDir, "gemini")
	envOutputPath := filepath.Join(binDir, "provider-env.txt")
	testhelpers.WriteShellStub(t, providerPath, "#!/bin/sh\nenv > \"$E2E_PROVIDER_ENV_OUT\"\n")
	t.Setenv("E2E_PROVIDER_ENV_OUT", filepath.ToSlash(envOutputPath))
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	provider := agentruntime.NewCLIAgent("")
	stale := models.AgentAuthority{ID: agentID, Generation: e2eGenerationA}
	_, err := provider.Run(context.Background(), agentruntime.LLMAgentRunRequest{
		BackendName: "gemini", AgentID: agentID, Generation: stale.Generation,
		TaskID: "task-provider-a", ProjectRoot: projectRoot, Prompt: "prompt",
		LaunchGate: e2eProviderLaunchGate(projectRoot, statePath, stale),
	})
	assertE2EAuthorityError(t, err, agentID)
	if _, err := os.Stat(envOutputPath); !os.IsNotExist(err) {
		t.Fatalf("stale provider side effect exists: %v", err)
	}

	current := models.AgentAuthority{ID: agentID, Generation: e2eGenerationB}
	_, err = provider.Run(context.Background(), agentruntime.LLMAgentRunRequest{
		BackendName: "gemini", AgentID: agentID, Generation: current.Generation,
		TaskID: "task-provider-b", ProjectRoot: projectRoot, Prompt: "prompt",
		LaunchGate: e2eProviderLaunchGate(projectRoot, statePath, current),
	})
	if err != nil {
		t.Fatalf("current provider launch: %v", err)
	}
	providerEnv := mustReadE2EStateBytes(t, envOutputPath)
	for _, want := range []string{
		brand.EnvName("AGENT_ID") + "=" + agentID,
		brand.EnvName("AGENT_GENERATION") + "=" + e2eGenerationB,
		brand.LegacyEnvName("AGENT_ID") + "=" + agentID,
		brand.LegacyEnvName("AGENT_GENERATION") + "=" + e2eGenerationB,
	} {
		if !bytes.Contains(providerEnv, []byte(want)) {
			t.Errorf("provider environment missing %q", want)
		}
	}
	if bytes.Contains(providerEnv, []byte(e2eGenerationA)) {
		t.Fatal("provider environment retained losing generation")
	}
}

func testE2EAtomicFenceInventory(t *testing.T) {
	expected := []string{
		"add-task", "add-tasks", "apply-dependency-repair", "approved-merge-dispatch",
		"assess-blocked", "assess-hypothesis-exhausted", "await-resubmission", "await-verdict",
		"cancel-task", "claim-degradation", "claim-doer", "claim-reviewer", "claim-task",
		"clear-claim-degradation", "handoff", "mark-blocked", "reconcile-merged",
		"release-reviewer-claim", "repair-superseded-dependencies", "resume-handoff",
		"resume-owned-task", "retarget-dependency", "set-task-output", "submit-for-review",
		"submit-verdict", "supersede-task", "unblock-task", "watchdog-block", "write-checkpoint",
		"wt-merge",
	}
	actual := make([]string, 0, len(atomicGenerationFenceInventory))
	for _, entry := range atomicGenerationFenceInventory {
		actual = append(actual, entry.name)
	}
	sort.Strings(actual)
	if !slices.Equal(actual, expected) {
		t.Fatalf("atomic fence inventory = %v, want exhaustive Tasks 5-7 inventory %v", actual, expected)
	}

	_, sourcePath, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve inventory source path")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(sourcePath), "..", ".."))
	parsedFiles := make(map[string]*ast.File)
	for _, entry := range atomicGenerationFenceInventory {
		t.Run(entry.name, func(t *testing.T) {
			if entry.command != "" {
				cmd, _, err := rootCmd.Find([]string{entry.command})
				if err != nil || cmd == nil || cmd.Name() != entry.command {
					t.Fatalf("Cobra command %q is not registered: command=%v error=%v", entry.command, cmd, err)
				}
			}
			parsed := parsedFiles[entry.sourceFile]
			if parsed == nil {
				var err error
				parsed, err = parser.ParseFile(token.NewFileSet(), filepath.Join(repoRoot, entry.sourceFile), nil, 0)
				if err != nil {
					t.Fatalf("parse %s: %v", entry.sourceFile, err)
				}
				parsedFiles[entry.sourceFile] = parsed
			}
			declaration := findE2EDeclaration(parsed, entry.declaration)
			if declaration == nil {
				t.Fatalf("%s lacks declaration %q", entry.sourceFile, entry.declaration)
			}
			if !declarationBindsE2EAuthority(declaration, entry) {
				t.Fatalf(
					"%s declaration %s lacks authority binding %s=%s on %s",
					entry.sourceFile, entry.declaration, entry.authorityField, entry.authorityBinding, entry.call,
				)
			}
		})
	}
}

func findE2EDeclaration(file *ast.File, name string) ast.Node {
	for _, declaration := range file.Decls {
		if function, ok := declaration.(*ast.FuncDecl); ok && function.Name.Name == name {
			return function
		}
		generated, ok := declaration.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range generated.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for index, identifier := range value.Names {
				if identifier.Name == name && index < len(value.Values) {
					return value.Values[index]
				}
			}
		}
	}
	return nil
}

func declarationBindsE2EAuthority(declaration ast.Node, entry atomicFenceInventoryEntry) bool {
	found := false
	ast.Inspect(declaration, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || e2eExpressionName(call.Fun) != entry.call {
			return true
		}
		for _, argument := range call.Args {
			if entry.authorityField == "" && matchesE2EAuthorityExpression(argument, entry.authorityBinding) {
				found = true
				return false
			}
			ast.Inspect(argument, func(node ast.Node) bool {
				keyValue, ok := node.(*ast.KeyValueExpr)
				if !ok || e2eExpressionName(keyValue.Key) != entry.authorityField {
					return true
				}
				if matchesE2EAuthorityExpression(keyValue.Value, entry.authorityBinding) {
					found = true
				}
				return false
			})
		}
		return !found
	})
	return found
}

func e2eExpressionName(expression ast.Expr) string {
	switch value := expression.(type) {
	case *ast.Ident:
		return value.Name
	case *ast.SelectorExpr:
		return e2eExpressionName(value.X) + "." + value.Sel.Name
	default:
		return ""
	}
}

func matchesE2EAuthorityExpression(expression ast.Expr, binding string) bool {
	switch binding {
	case "authority":
		identifier, ok := expression.(*ast.Ident)
		return ok && identifier.Name == "authority"
	case "&authority":
		unary, ok := expression.(*ast.UnaryExpr)
		return ok && unary.Op == token.AND && matchesE2EAuthorityExpression(unary.X, "authority")
	case "*authority":
		star, ok := expression.(*ast.StarExpr)
		return ok && matchesE2EAuthorityExpression(star.X, "authority")
	default:
		return false
	}
}

func runStaleGenerationCLI(t *testing.T, projectRoot, statePath, agentID string, args ...string) {
	t.Helper()
	previousHook := afterRBACAdmissionTestHook
	var admitted bool
	var winnerBytes []byte
	afterRBACAdmissionTestHook = func(admittedID string) {
		if admitted {
			return
		}
		admitted = true
		if admittedID != agentID {
			t.Fatalf("RBAC admitted %q, want %q", admittedID, agentID)
		}
		bb := db.For(statePath)
		if err := bb.Modify(func(state *models.State) error {
			agent, ok := state.Agents[agentID]
			if !ok {
				return fmt.Errorf("agent %s missing", agentID)
			}
			agent.Generation = e2eGenerationB
			state.Agents[agentID] = agent
			return nil
		}); err != nil {
			t.Fatalf("replace admitted generation: %v", err)
		}
		winnerBytes = mustReadE2EStateBytes(t, statePath)
	}
	t.Cleanup(func() { afterRBACAdmissionTestHook = previousHook })

	stdout, err := executeGenerationRootCommandCapture(t, projectRoot, e2eGenerationA, args...)
	afterRBACAdmissionTestHook = nil
	if !admitted {
		t.Fatal("command did not reach the post-RBAC admission fence")
	}
	if err == nil {
		t.Fatal("stale command succeeded")
	}
	assertE2EJSONAuthorityError(t, stdout, agentID)
	assertE2EStateBytes(t, statePath, winnerBytes, "stale CLI command")
}

func runCurrentGenerationCLI(t *testing.T, projectRoot, generation string, args ...string) map[string]any {
	t.Helper()
	afterRBACAdmissionTestHook = nil
	stdout, err := executeGenerationRootCommandCapture(t, projectRoot, generation, args...)
	if err != nil {
		t.Fatalf("current command %q failed: %v; stdout=%s", args[0], err, stdout)
	}
	envelope := parseEnvelope(t, stdout)
	if envelope["ok"] != true {
		t.Fatalf("current command %q envelope = %v", args[0], envelope)
	}
	return envelope
}

func executeGenerationRootCommandCapture(t *testing.T, projectRoot, generation string, args ...string) (string, error) {
	t.Helper()
	t.Setenv(brand.EnvName("AGENT_GENERATION"), generation)

	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(projectRoot); err != nil {
		t.Fatalf("chdir project root: %v", err)
	}
	defer func() {
		if err := os.Chdir(oldDir); err != nil {
			t.Fatalf("restore working directory: %v", err)
		}
	}()

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("capture stdout: %v", err)
	}
	os.Stdout = w
	defer func() { os.Stdout = oldStdout }()

	resetRootCmdForTest(t)
	rootCmd.SetArgs(append(slices.Clone(args), "--json"))
	cmdErr := rootCmd.Execute()
	if err := w.Close(); err != nil {
		t.Fatalf("close captured stdout: %v", err)
	}
	os.Stdout = oldStdout

	var output bytes.Buffer
	if _, err := io.Copy(&output, r); err != nil {
		t.Fatalf("read captured stdout: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("close captured stdout reader: %v", err)
	}
	return output.String(), cmdErr
}

func setupE2EMergeProject(t *testing.T, taskID, agentID string) (string, string, string) {
	t.Helper()
	projectRoot := t.TempDir()
	testhelpers.SetupTestGitRepo(t, projectRoot)
	statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
	testhelpers.SetupPipelineConfig(t, projectRoot)

	integrationBefore := testhelpers.MustGit(t, projectRoot, "rev-parse", "HEAD")
	testhelpers.MustGit(t, projectRoot, "branch", "-f", "integration", integrationBefore)
	changePath := filepath.Join(projectRoot, "generation-e2e-merge.txt")
	if err := os.WriteFile(changePath, []byte("generation fence\n"), 0o600); err != nil {
		t.Fatalf("write merge fixture: %v", err)
	}
	testhelpers.MustGit(t, projectRoot, "add", "generation-e2e-merge.txt")
	testhelpers.MustGit(t, projectRoot, "commit", "-m", "Add generation fence fixture")
	reviewCommit := testhelpers.MustGit(t, projectRoot, "rev-parse", "HEAD")

	state := testhelpers.CreateValidState()
	state.Config.IntegrationBranch = "integration"
	state.Goal.SpecRef = "README.md"
	state.Agents[agentID] = e2eAgent(models.RoleCodeReviewer, e2eGenerationA)
	task := testhelpers.BuildTaskByStatus(taskID, models.TaskStatusApproved, time.Now().UTC())
	task.Worktree = nil
	task.ReviewCommit = &reviewCommit
	state.Tasks = []models.Task{task}
	testhelpers.WriteInitialState(t, statePath, state)
	return projectRoot, statePath, integrationBefore
}

func e2eProviderLaunchGate(projectRoot, statePath string, authority models.AgentAuthority) agentruntime.LLMAgentLaunchGate {
	return func(ctx context.Context, start func() error) error {
		return ops.WithAgentLifecycleLock(ctx, projectRoot, authority.ID, "e2e-provider-start", func() error {
			state, err := db.For(statePath).ReadContext(ctx)
			if err != nil {
				return err
			}
			if err := ops.RequireAgentAuthority(state, authority); err != nil {
				return err
			}
			return start()
		})
	}
}

func e2eAgent(role, generation string) models.Agent {
	agent := testhelpers.RegisteredTestAgent(role)
	agent.Generation = generation
	return agent
}

func assertE2EJSONAuthorityError(t *testing.T, stdout, agentID string) {
	t.Helper()
	envelope := parseEnvelope(t, stdout)
	if envelope["ok"] != false {
		t.Fatalf("stale JSON envelope = %v, want ok=false", envelope)
	}
	for _, want := range []string{agentID, e2eGenerationA, e2eGenerationB} {
		if !strings.Contains(stdout, want) {
			t.Errorf("JSON error = %s, want %q", stdout, want)
		}
	}
}

func assertE2EAuthorityError(t *testing.T, err error, agentID string) {
	t.Helper()
	if !ops.IsAgentAuthorityError(err) {
		t.Fatalf("error = %T %v, want AgentAuthorityError", err, err)
	}
	for _, want := range []string{agentID, e2eGenerationA, e2eGenerationB} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want %q", err, want)
		}
	}
}

func assertE2EStateBytes(t *testing.T, statePath string, want []byte, operation string) {
	t.Helper()
	if got := mustReadE2EStateBytes(t, statePath); !bytes.Equal(got, want) {
		t.Fatalf("%s changed generation-B blackboard bytes", operation)
	}
}

func assertNoE2EGenerationA(t *testing.T, stateBytes []byte) {
	t.Helper()
	if bytes.Contains(stateBytes, []byte(e2eGenerationA)) {
		t.Fatal("final blackboard retains a generation-A-owned mutation")
	}
}

func mustReadE2EStateBytes(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}
