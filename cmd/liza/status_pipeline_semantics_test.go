package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/commands"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/rolemodels"
	"github.com/liza-mas/liza/internal/testhelpers"
	"gopkg.in/yaml.v3"
)

type statusPipelineView struct {
	Tasks struct {
		Claimable                    int                        `json:"claimable" yaml:"claimable"`
		Reviewable                   int                        `json:"reviewable" yaml:"reviewable"`
		ClaimableByRole              []models.RoleTaskReadiness `json:"claimable_by_role" yaml:"claimable_by_role"`
		ReviewableByRole             []models.RoleTaskReadiness `json:"reviewable_by_role" yaml:"reviewable_by_role"`
		LegacyCoderClaimable         int                        `json:"legacy_coder_claimable" yaml:"legacy_coder_claimable"`
		LegacyCodeReviewerReviewable int                        `json:"legacy_code_reviewer_reviewable" yaml:"legacy_code_reviewer_reviewable"`
	} `json:"tasks" yaml:"tasks"`
	AgentCapacity struct {
		Live     int `json:"live" yaml:"live"`
		Free     int `json:"free" yaml:"free"`
		Degraded int `json:"degraded" yaml:"degraded"`
	} `json:"agent_capacity" yaml:"agent_capacity"`
	OrchestratorState struct {
		Trigger string `json:"trigger" yaml:"trigger"`
	} `json:"orchestrator_state" yaml:"orchestratorstate"`
	PhaseHandoff *struct {
		State         string                        `json:"state" yaml:"state"`
		MergeRequired []statusPipelineMergeRequired `json:"merge_required" yaml:"merge_required"`
	} `json:"phase_handoff" yaml:"phase_handoff"`
}

type statusPipelineMergeRequired struct {
	TaskID string `json:"task_id" yaml:"task_id"`
	Action string `json:"action" yaml:"action"`
}

type statusPipelineOutputs struct {
	Dashboard string
	JSON      string
	YAML      string
	JSONView  statusPipelineView
	YAMLView  statusPipelineView
}

func TestStatusPipelineSemantics(t *testing.T) {
	originalBinaryName := brand.BinaryName
	brand.BinaryName = "acme"
	t.Cleanup(func() { brand.BinaryName = originalBinaryName })

	now := time.Now().UTC()
	projectRoot, statePath := setupMutationTestProject(t, func(state *models.State) {
		configureStatusPipelineState(state, now)
	})
	addStatusPipelineCustomRolePair(t, projectRoot)

	resolver, err := ops.LoadResolverForModels(projectRoot)
	if err != nil {
		t.Fatalf("load pipeline resolver: %v", err)
	}
	state := readState(t, statePath)
	wantClaimable, wantReviewable := statusPipelineReadinessFromPredicates(t, state, resolver)
	assertStatusPipelineMissingRoles(t, state, resolver, wantClaimable, wantReviewable)
	assertStatusPipelineRoleCount(t, wantClaimable, "code-planner", 1)
	assertStatusPipelineRoleCount(t, wantClaimable, "coder", 0)
	assertStatusPipelineRoleCount(t, wantClaimable, "custom-doer", 0)
	if got := sumStatusPipelineReadiness(wantClaimable); got != 1 {
		t.Fatalf("claimable readiness = %d, want only the dependency-clear code-planning task", got)
	}
	assertStatusPipelineRoleCount(t, wantReviewable, "code-reviewer", 1)
	assertStatusPipelineRoleCount(t, wantReviewable, "custom-reviewer", 1)

	wantMerges := []statusPipelineMergeRequired{
		{TaskID: "plan-b", Action: "acme wt-merge plan-b"},
		{TaskID: "plan-a", Action: "acme wt-merge plan-a"},
	}
	beforeMerge := captureStatusPipelineOutputs(t, projectRoot, statePath)
	assertStatusPipelineStructuredViews(t, beforeMerge, wantClaimable, wantReviewable, wantMerges)
	assertStatusPipelineDashboard(t, beforeMerge.Dashboard, wantClaimable, wantReviewable, wantMerges)
	assertStatusPipelinePlanningReadiness(t, beforeMerge)
	assertStatusPipelineExplicitFields(t, beforeMerge)

	markStatusPipelinePlanningTasksMerged(t, statePath, now.Add(time.Minute), "plan-b", "plan-a")
	afterMerge := captureStatusPipelineOutputs(t, projectRoot, statePath)
	assertStatusPipelineStructuredViews(t, afterMerge, wantClaimable, wantReviewable, nil)
	if afterMerge.JSONView.PhaseHandoff == nil || afterMerge.JSONView.PhaseHandoff.State != "COMPLETED" {
		t.Fatalf("post-merge phase handoff = %+v, want COMPLETED without merge prerequisites", afterMerge.JSONView.PhaseHandoff)
	}
	if !reflect.DeepEqual(afterMerge.JSONView.Tasks, beforeMerge.JSONView.Tasks) {
		t.Fatalf("post-merge task readiness changed:\nbefore: %#v\nafter:  %#v", beforeMerge.JSONView.Tasks, afterMerge.JSONView.Tasks)
	}
	if afterMerge.JSONView.AgentCapacity != beforeMerge.JSONView.AgentCapacity {
		t.Fatalf("post-merge agent capacity changed: before=%+v after=%+v", beforeMerge.JSONView.AgentCapacity, afterMerge.JSONView.AgentCapacity)
	}
	if afterMerge.JSONView.OrchestratorState.Trigger != beforeMerge.JSONView.OrchestratorState.Trigger {
		t.Fatalf("post-merge wake trigger changed: before=%q after=%q", beforeMerge.JSONView.OrchestratorState.Trigger, afterMerge.JSONView.OrchestratorState.Trigger)
	}
	for _, output := range []string{afterMerge.Dashboard, afterMerge.JSON, afterMerge.YAML} {
		if strings.Contains(output, "MERGE_REQUIRED") || strings.Contains(output, "Merge required:") {
			t.Fatalf("post-merge status retained merge blocker:\n%s", output)
		}
		for _, merge := range wantMerges {
			if strings.Contains(output, merge.Action) {
				t.Fatalf("post-merge status retained action %q:\n%s", merge.Action, output)
			}
		}
	}
}

func TestStatusLegacyPipelineMatchesResumeMergeBlocker(t *testing.T) {
	originalBinaryName := brand.BinaryName
	brand.BinaryName = "acme"
	t.Cleanup(func() { brand.BinaryName = originalBinaryName })

	now := time.Now().UTC()
	projectRoot, statePath := setupMutationTestProject(t, func(state *models.State) {
		state.Sprint.Status = models.SprintStatusCompleted
		state.Sprint.Scope.Planned = []string{"legacy-plan"}
		state.Tasks = []models.Task{statusPipelinePlanningTask("legacy-plan", now)}
	})
	if err := os.Remove(filepath.Join(projectRoot, paths.ProjectDirName(), "pipeline.yaml")); err != nil {
		t.Fatalf("remove pipeline config: %v", err)
	}

	dashboard := executeReadOnlyStatus(t, projectRoot, statePath, "status", "--format", "dashboard")
	jsonStatus := executeReadOnlyStatus(t, projectRoot, statePath, "status", "--json")
	view := decodeStatusPipelineJSON(t, jsonStatus)
	wantMerges := []statusPipelineMergeRequired{{TaskID: "legacy-plan", Action: "acme wt-merge legacy-plan"}}
	if view.PhaseHandoff == nil || view.PhaseHandoff.State != "MERGE_REQUIRED" {
		t.Fatalf("legacy phase handoff = %+v, want MERGE_REQUIRED", view.PhaseHandoff)
	}
	if !reflect.DeepEqual(view.PhaseHandoff.MergeRequired, wantMerges) {
		t.Fatalf("legacy merge prerequisites = %#v, want %#v", view.PhaseHandoff.MergeRequired, wantMerges)
	}
	for _, want := range []string{"State: MERGE_REQUIRED", "legacy-plan: acme wt-merge legacy-plan"} {
		if !strings.Contains(dashboard, want) {
			t.Errorf("legacy dashboard missing %q:\n%s", want, dashboard)
		}
	}

	beforeResume, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read state before resume: %v", err)
	}
	_, err = ops.Resume(projectRoot, "test")
	if err == nil {
		t.Fatal("resume succeeded with approved unmerged planning output")
	}
	for _, want := range []string{"legacy-plan", "acme wt-merge"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("resume error missing %q: %v", want, err)
		}
	}
	afterResume, readErr := os.ReadFile(statePath)
	if readErr != nil {
		t.Fatalf("read state after resume: %v", readErr)
	}
	if !bytes.Equal(afterResume, beforeResume) {
		t.Fatal("failed legacy resume mutated state.yaml")
	}
}

func configureStatusPipelineState(state *models.State, now time.Time) {
	state.Sprint.Status = models.SprintStatusCompleted
	state.Sprint.Scope.Planned = []string{"plan-b", "plan-a"}
	state.Agents = map[string]models.Agent{}
	state.AgentHealth = map[string]models.AgentHealth{}

	dependency := testhelpers.BuildTaskByStatus("merged-dependency", models.TaskStatusMerged, now)
	builtinPlanner := testhelpers.BuildTaskByStatus("builtin-planner", models.TaskStatusDraftCodingPlan, now)
	builtinPlanner.Type = models.TaskTypePlanning
	builtinPlanner.DependsOn = []string{dependency.ID}
	builtinReviewer := testhelpers.BuildTaskByStatus("builtin-reviewer", models.TaskStatusReadyForReview, now)
	blockedPlanningDependency := statusPipelinePlanningTask("blocked-planning-dependency", now)
	customDoer := testhelpers.BuildTaskByStatus("custom-doer", models.TaskStatusReady, now)
	customDoer.RolePair = "custom-pair"
	customDoer.Status = "CUSTOM_READY"
	customDoer.DependsOn = []string{blockedPlanningDependency.ID}
	customReviewer := testhelpers.BuildTaskByStatus("custom-reviewer", models.TaskStatusReadyForReview, now)
	customReviewer.RolePair = "custom-pair"
	customReviewer.Status = "CUSTOM_TO_REVIEW"

	state.Tasks = []models.Task{
		statusPipelinePlanningTask("plan-a", now),
		builtinReviewer,
		customDoer,
		dependency,
		blockedPlanningDependency,
		statusPipelinePlanningTask("plan-b", now),
		customReviewer,
		builtinPlanner,
	}
}

func statusPipelinePlanningTask(id string, now time.Time) models.Task {
	task := testhelpers.BuildTaskByStatus(id, models.TaskStatusCodingPlanApproved, now)
	task.Type = models.TaskTypePlanning
	task.Output = []models.OutputEntry{{Desc: "implement", DoneWhen: "done", Scope: "scope"}}
	reviewCommit := id + "-review"
	approvedBy := "code-plan-reviewer-1"
	worktree := ".worktrees/" + id
	task.ReviewCommit = &reviewCommit
	task.ApprovedBy = &approvedBy
	task.Worktree = &worktree
	return task
}

func addStatusPipelineCustomRolePair(t *testing.T, projectRoot string) {
	t.Helper()
	pipelinePath := filepath.Join(projectRoot, paths.ProjectDirName(), "pipeline.yaml")
	cfg, err := pipeline.Load(pipelinePath)
	if err != nil {
		t.Fatalf("load fixture pipeline: %v", err)
	}
	cfg.Pipeline.Roles["custom-doer"] = pipeline.RoleDef{Type: "doer", DisplayName: "Custom Doer"}
	cfg.Pipeline.Roles["custom-reviewer"] = pipeline.RoleDef{Type: "reviewer", DisplayName: "Custom Reviewer"}
	cfg.Pipeline.RolePairs["custom-pair"] = pipeline.RolePairDef{
		Doer:     "custom-doer",
		Reviewer: "custom-reviewer",
		States: pipeline.RolePairStates{
			Initial:   "CUSTOM_READY",
			Executing: "CUSTOM_EXECUTING",
			Submitted: "CUSTOM_TO_REVIEW",
			Reviewing: "CUSTOM_REVIEWING",
			Approved:  "CUSTOM_APPROVED",
			Rejected:  "CUSTOM_REJECTED",
		},
	}
	encoded, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal fixture pipeline: %v", err)
	}
	if err := os.WriteFile(pipelinePath, encoded, 0644); err != nil {
		t.Fatalf("write fixture pipeline: %v", err)
	}
}

func statusPipelineReadinessFromPredicates(t *testing.T, state *models.State, resolver models.PipelineResolver) ([]models.RoleTaskReadiness, []models.RoleTaskReadiness) {
	t.Helper()
	var claimable []models.RoleTaskReadiness
	var reviewable []models.RoleTaskReadiness
	for _, role := range resolver.AllRoleNames() {
		roleType, err := resolver.RoleType(role)
		if err != nil {
			t.Fatalf("resolve role type for %q: %v", role, err)
		}
		count := 0
		for i := range state.Tasks {
			if state.Tasks[i].IsClaimable(role, state.Tasks, resolver) {
				count++
			}
		}
		switch roleType {
		case "doer":
			claimable = append(claimable, models.RoleTaskReadiness{Role: role, Count: count})
		case "reviewer":
			reviewable = append(reviewable, models.RoleTaskReadiness{Role: role, Count: count})
		}
	}
	return claimable, reviewable
}

func assertStatusPipelineMissingRoles(t *testing.T, state *models.State, resolver models.PipelineResolver, readiness ...[]models.RoleTaskReadiness) {
	t.Helper()
	missingCounts := make(map[string]int)
	for _, missing := range commands.FindMissingRolesWithClaimableWork(state, resolver, rolemodels.File{}) {
		missingCounts[missing.Role] = missing.TaskCount
	}
	wantMissingCounts := make(map[string]int)
	for _, byRole := range readiness {
		for _, role := range byRole {
			if role.Count > 0 {
				wantMissingCounts[role.Role] = role.Count
			}
		}
	}
	if !reflect.DeepEqual(missingCounts, wantMissingCounts) {
		t.Fatalf("missing-role readiness = %#v, want exact claimable-role counts %#v", missingCounts, wantMissingCounts)
	}
}

func assertStatusPipelineRoleCount(t *testing.T, readiness []models.RoleTaskReadiness, role string, want int) {
	t.Helper()
	for _, entry := range readiness {
		if entry.Role == role {
			if entry.Count != want {
				t.Fatalf("readiness for %q = %d, want %d", role, entry.Count, want)
			}
			return
		}
	}
	t.Fatalf("readiness omitted configured role %q", role)
}

func captureStatusPipelineOutputs(t *testing.T, projectRoot, statePath string) statusPipelineOutputs {
	t.Helper()
	dashboard := executeReadOnlyStatus(t, projectRoot, statePath, "status")
	jsonStatus := executeReadOnlyStatus(t, projectRoot, statePath, "status", "--json")
	yamlStatus := executeReadOnlyStatus(t, projectRoot, statePath, "status", "--format", "yaml")
	return statusPipelineOutputs{
		Dashboard: dashboard,
		JSON:      jsonStatus,
		YAML:      yamlStatus,
		JSONView:  decodeStatusPipelineJSON(t, jsonStatus),
		YAMLView:  decodeStatusPipelineYAML(t, yamlStatus),
	}
}

func executeReadOnlyStatus(t *testing.T, projectRoot, statePath string, args ...string) string {
	t.Helper()
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read state snapshot before %v: %v", args, err)
	}
	var stdout string
	if len(args) == 2 && args[1] == "--json" {
		stdout, err = executeRootCommandCapture(t, projectRoot, args...)
	} else {
		stdout, err = executeStatusPipelineCobraCapture(t, projectRoot, args...)
	}
	if err != nil {
		t.Fatalf("%v failed: %v\n%s", args, err, stdout)
	}
	after, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read state snapshot after %v: %v", args, err)
	}
	if !bytes.Equal(after, before) {
		t.Fatalf("%v mutated state.yaml", args)
	}
	return stdout
}

func executeStatusPipelineCobraCapture(t *testing.T, projectRoot string, args ...string) (string, error) {
	t.Helper()
	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer func() {
		if chdirErr := os.Chdir(oldDir); chdirErr != nil {
			t.Fatalf("restore working directory: %v", chdirErr)
		}
	}()
	if err := os.Chdir(projectRoot); err != nil {
		t.Fatalf("chdir to project root: %v", err)
	}

	resetRootCmdForTest(t)
	var stdout bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetArgs(args)
	err = rootCmd.Execute()
	return stdout.String(), err
}

func decodeStatusPipelineJSON(t *testing.T, stdout string) statusPipelineView {
	t.Helper()
	envelope := parseEnvelope(t, stdout)
	if envelope["ok"] != true {
		t.Fatalf("status JSON envelope ok = %v, want true", envelope["ok"])
	}
	result, err := json.Marshal(envelope["result"])
	if err != nil {
		t.Fatalf("marshal status JSON result: %v", err)
	}
	var view statusPipelineView
	if err := json.Unmarshal(result, &view); err != nil {
		t.Fatalf("decode status JSON result: %v", err)
	}
	return view
}

func decodeStatusPipelineYAML(t *testing.T, stdout string) statusPipelineView {
	t.Helper()
	var view statusPipelineView
	if err := yaml.Unmarshal([]byte(stdout), &view); err != nil {
		t.Fatalf("decode status YAML: %v\n%s", err, stdout)
	}
	return view
}

func assertStatusPipelineStructuredViews(t *testing.T, outputs statusPipelineOutputs, wantClaimable, wantReviewable []models.RoleTaskReadiness, wantMerges []statusPipelineMergeRequired) {
	t.Helper()
	if !reflect.DeepEqual(outputs.JSONView, outputs.YAMLView) {
		t.Fatalf("JSON and YAML status semantics differ:\nJSON: %#v\nYAML: %#v", outputs.JSONView, outputs.YAMLView)
	}
	view := outputs.JSONView
	if !reflect.DeepEqual(view.Tasks.ClaimableByRole, wantClaimable) {
		t.Fatalf("claimable_by_role = %#v, want %#v", view.Tasks.ClaimableByRole, wantClaimable)
	}
	if !reflect.DeepEqual(view.Tasks.ReviewableByRole, wantReviewable) {
		t.Fatalf("reviewable_by_role = %#v, want %#v", view.Tasks.ReviewableByRole, wantReviewable)
	}
	if view.Tasks.Claimable != sumStatusPipelineReadiness(wantClaimable) || view.Tasks.Reviewable != sumStatusPipelineReadiness(wantReviewable) {
		t.Fatalf("aggregate readiness = (%d, %d), want role sums (%d, %d)", view.Tasks.Claimable, view.Tasks.Reviewable, sumStatusPipelineReadiness(wantClaimable), sumStatusPipelineReadiness(wantReviewable))
	}
	if view.Tasks.LegacyCoderClaimable != 0 || view.Tasks.LegacyCodeReviewerReviewable != 1 {
		t.Fatalf("legacy readiness = (%d, %d), want (0, 1)", view.Tasks.LegacyCoderClaimable, view.Tasks.LegacyCodeReviewerReviewable)
	}
	if view.AgentCapacity.Live != 0 || view.AgentCapacity.Free != 0 || view.AgentCapacity.Degraded != 0 {
		t.Fatalf("agent capacity = %+v, want all zero independently of ready work", view.AgentCapacity)
	}
	if view.OrchestratorState.Trigger == "" {
		t.Fatal("wake trigger is empty; want a distinct reported trigger")
	}
	if view.PhaseHandoff == nil {
		t.Fatal("phase_handoff is absent")
	}
	if !reflect.DeepEqual(view.PhaseHandoff.MergeRequired, wantMerges) {
		t.Fatalf("merge prerequisites = %#v, want %#v", view.PhaseHandoff.MergeRequired, wantMerges)
	}
	if len(wantMerges) > 0 && view.PhaseHandoff.State != "MERGE_REQUIRED" {
		t.Fatalf("phase_handoff state = %q, want MERGE_REQUIRED", view.PhaseHandoff.State)
	}
}

func assertStatusPipelineDashboard(t *testing.T, dashboard string, wantClaimable, wantReviewable []models.RoleTaskReadiness, wantMerges []statusPipelineMergeRequired) {
	t.Helper()
	claimableSum := sumStatusPipelineReadiness(wantClaimable)
	reviewableSum := sumStatusPipelineReadiness(wantReviewable)
	for _, want := range []string{
		fmt.Sprintf("Claimable: %d tasks", claimableSum),
		fmt.Sprintf("Reviewable: %d tasks", reviewableSum),
		"Legacy coder claimable: 0 tasks",
		"Legacy code-reviewer reviewable: 1 tasks",
		"=== AGENT CAPACITY ===\nLive: 0, Free: 0, Degraded: 0",
		"=== ORCHESTRATOR ===\nWake Trigger: ",
		"=== PHASE HANDOFF ===\nState: MERGE_REQUIRED",
	} {
		if !strings.Contains(dashboard, want) {
			t.Errorf("dashboard missing %q:\n%s", want, dashboard)
		}
	}

	orderedReadiness := []string{"Claimable by role:\n"}
	for _, role := range wantClaimable {
		orderedReadiness = append(orderedReadiness, fmt.Sprintf("  %s: %d\n", role.Role, role.Count))
	}
	orderedReadiness = append(orderedReadiness, "Reviewable by role:\n")
	for _, role := range wantReviewable {
		orderedReadiness = append(orderedReadiness, fmt.Sprintf("  %s: %d\n", role.Role, role.Count))
	}
	assertStatusPipelineOrderedSubstrings(t, dashboard, orderedReadiness...)

	orderedMerges := []string{"Merge required:\n"}
	for _, merge := range wantMerges {
		orderedMerges = append(orderedMerges, fmt.Sprintf("  %s: %s\n", merge.TaskID, merge.Action))
	}
	assertStatusPipelineOrderedSubstrings(t, dashboard, orderedMerges...)
}

func assertStatusPipelinePlanningReadiness(t *testing.T, outputs statusPipelineOutputs) {
	t.Helper()
	for format, view := range map[string]statusPipelineView{
		"JSON": outputs.JSONView,
		"YAML": outputs.YAMLView,
	} {
		if view.Tasks.Claimable != 1 {
			t.Errorf("%s aggregate claimable readiness = %d, want 1", format, view.Tasks.Claimable)
		}
		assertStatusPipelineRoleCount(t, view.Tasks.ClaimableByRole, "code-planner", 1)
		assertStatusPipelineRoleCount(t, view.Tasks.ClaimableByRole, "coder", 0)
	}
	for _, want := range []string{"Claimable: 1 tasks", "  code-planner: 1\n", "  coder: 0\n"} {
		if !strings.Contains(outputs.Dashboard, want) {
			t.Errorf("dashboard missing planning-only readiness %q:\n%s", want, outputs.Dashboard)
		}
	}
}

func assertStatusPipelineExplicitFields(t *testing.T, outputs statusPipelineOutputs) {
	t.Helper()
	for _, key := range []string{`"claimable_by_role"`, `"reviewable_by_role"`, `"legacy_coder_claimable"`, `"legacy_code_reviewer_reviewable"`, `"agent_capacity"`, `"orchestrator_state"`, `"phase_handoff"`} {
		if !strings.Contains(outputs.JSON, key) {
			t.Errorf("JSON status missing explicit field %s:\n%s", key, outputs.JSON)
		}
	}
	for _, key := range []string{"claimable_by_role:", "reviewable_by_role:", "legacy_coder_claimable:", "legacy_code_reviewer_reviewable:", "agent_capacity:", "orchestratorstate:", "phase_handoff:"} {
		if !strings.Contains(outputs.YAML, key) {
			t.Errorf("YAML status missing explicit field %q:\n%s", key, outputs.YAML)
		}
	}
}

func assertStatusPipelineOrderedSubstrings(t *testing.T, value string, ordered ...string) {
	t.Helper()
	remainder := value
	for _, want := range ordered {
		index := strings.Index(remainder, want)
		if index < 0 {
			t.Fatalf("output missing ordered substring %q:\n%s", want, value)
		}
		remainder = remainder[index+len(want):]
	}
}

func sumStatusPipelineReadiness(readiness []models.RoleTaskReadiness) int {
	total := 0
	for _, role := range readiness {
		total += role.Count
	}
	return total
}

func markStatusPipelinePlanningTasksMerged(t *testing.T, statePath string, now time.Time, taskIDs ...string) {
	t.Helper()
	state := readState(t, statePath)
	for _, taskID := range taskIDs {
		task := state.FindTask(taskID)
		if task == nil {
			t.Fatalf("planning task %q not found", taskID)
		}
		mergeCommit := taskID + "-merge"
		task.Status = models.TaskStatusMerged
		task.MergeCommit = &mergeCommit
		task.HandoffEvents = append(task.HandoffEvents, models.HandoffEvent{
			Timestamp: now,
			Agent:     "code-plan-reviewer-1",
			Trigger:   models.HandoffTriggerCompletion,
		})
	}
	testhelpers.WriteInitialState(t, statePath, state)
}
