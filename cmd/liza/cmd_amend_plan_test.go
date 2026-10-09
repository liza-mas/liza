package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/agent"
	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/prompts"
	"github.com/liza-mas/liza/internal/testhelpers"
)

const amendmentPlanRef = "specs/acceptance-plan.md#Task 1"

// The CLI strings and existing state fields keep these regressions compiling
// before amend-plan and validation_notes exist.
func setupAmendmentCLI(t *testing.T) (string, string) {
	t.Helper()
	t.Setenv(brand.EnvName("AGENT_ID"), "")
	t.Setenv(brand.LegacyEnvName("AGENT_ID"), "")
	root, statePath := setupMutationTestProject(t, func(s *models.State) {
		s.Tasks = nil
		s.Goal.SpecRef = "specs/acceptance-goal.md"
		for _, role := range []string{"code-planner", "code-plan-reviewer", "coder"} {
			s.Agents[role+"-1"] = mutationTestAgent(role)
		}
		s.Agents["coder-2"] = mutationTestAgent("coder")
	})
	writeAmendmentFile(t, root, "specs/acceptance-goal.md", "# Boundary\n\n## Identity\nReject malformed identity.\n")
	testhelpers.MustGit(t, root, "add", "specs/acceptance-goal.md")
	testhelpers.MustGit(t, root, "commit", "-m", "test: record boundary requirements")
	base := testhelpers.MustGit(t, root, "rev-parse", "HEAD")
	carrier := fmt.Sprintf("# Code plan\n\n## Source References\nSource revision: %q\n\n### Direct References\n- \"identity\": \"specs/acceptance-goal.md#Identity\"\n\n### Obligation Coverage\n- \"AC-identity\" -> \"identity\"\n\n## Task 1\n\n### Acceptance Contract\n```json\n{\"version\":1,\"manifest\":\"acceptance/task-1.json\",\"obligations\":[\"AC-identity\"],\"validation\":[\"sh boundary_test.sh\"],\"timeout_seconds\":10,\"approved_proofs\":[]}\n```\n", base)
	writeAmendmentFile(t, root, "specs/acceptance-plan.md", carrier)
	writeAmendmentFile(t, root, "README.md", "# Fixture\n")
	testhelpers.MustGit(t, root, "add", "specs/acceptance-plan.md", "README.md")
	testhelpers.MustGit(t, root, "commit", "-m", "test: independently reviewed allocation")
	review := testhelpers.MustGit(t, root, "rev-parse", "HEAD")
	testhelpers.MustGit(t, root, "branch", "-f", "integration", review)
	if err := db.For(statePath).Modify(func(s *models.State) error {
		now := time.Now().UTC()
		original := testhelpers.BuildTaskByStatus("original", models.TaskStatusMerged, now)
		original.Type, original.RolePair = models.TaskTypePlanning, "code-planning-pair"
		original.SpecRef = "specs/acceptance-goal.md"
		original.AssignedTo = testhelpers.StringPtr("code-planner-9")
		original.ApprovedBy = testhelpers.StringPtr("code-plan-reviewer-9")
		original.Approvals = []models.Approval{{Agent: "code-plan-reviewer-9", Timestamp: now}}
		original.BaseCommit, original.ReviewCommit, original.MergeCommit = &base, &review, &review
		original.Output = []models.OutputEntry{{Desc: "task 1", DoneWhen: "identity rejected", Scope: "identity", SpecRef: original.SpecRef, PlanRef: amendmentPlanRef, Validation: []string{"sh boundary_test.sh"}}}
		original.History = []models.TaskHistoryEntry{{Time: now, Event: models.TaskEventSubmittedForReview, Agent: original.AssignedTo, Commit: &review}, {Time: now, Event: models.TaskEventMerged, Agent: original.ApprovedBy, Commit: &review}}
		consumer := testhelpers.BuildTaskByStatus("started-consumer", models.TaskStatusReady, now)
		consumer.History = []models.TaskHistoryEntry{{Time: now, Event: models.TaskEventClaimed, Agent: testhelpers.StringPtr("coder-1")}}
		consumer.ProviderDependencies = []models.ProviderDependency{{ProviderTask: original.ID, Transition: "code-plan-to-coding", Outputs: []int{0}}}
		ordinary := testhelpers.BuildTaskByStatus("ordinary", models.TaskStatusReady, now)
		ordinary.DependsOn = []string{original.ID}
		s.Tasks = []models.Task{original, consumer, ordinary}
		s.Sprint.Scope.Planned = []string{original.ID, consumer.ID, ordinary.ID}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	runAmendmentCLI(t, root, "claim-task", "ordinary", "coder-2", "--json")
	return root, statePath
}

func writeAmendmentFile(t *testing.T, root, name, contents string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0644); err != nil {
		t.Fatal(err)
	}
}

func runAmendmentCLI(t *testing.T, root string, args ...string) string {
	t.Helper()
	stdout, err := executeRootCommandCapture(t, root, args...)
	if err != nil {
		t.Fatalf("%v: %v\n%s", args, err, stdout)
	}
	return stdout
}

func beginAmendmentCLI(t *testing.T, root, statePath string, extra ...string) string {
	t.Helper()
	before := readState(t, statePath)
	args := append([]string{"amend-plan", "original", "--reason", "fix reviewed handoff ordering", "--agent-id", "orchestrator-1", "--json"}, extra...)
	runAmendmentCLI(t, root, args...)
	after := readState(t, statePath)
	for _, task := range after.Tasks {
		if before.FindTask(task.ID) == nil {
			return task.ID
		}
	}
	t.Fatal("begin did not create correction review work")
	return ""
}

// Complete a correction through the real claim, output, submission,
// independent review and merge boundaries, rather than synthesizing approval.
func mergeAmendmentCLI(t *testing.T, root, statePath, correction string, output []models.OutputEntry) string {
	t.Helper()
	runAmendmentCLI(t, root, "claim-task", correction, "code-planner-1", "--json")
	runAmendmentCLI(t, root, "write-checkpoint", correction,
		"--intent", "Preserve the allocation and correct reviewed handoff scheduling",
		"--validation-plan", "Review the unchanged Task 1 allocation and corrected output manifest",
		"--files-to-modify", "specs/acceptance-plan.md",
		"--tdd-not-required", "Planning artifact correction; executable regression coverage is provided by this integration test",
		"--agent-id", "code-planner-1", "--json")
	task := mustFindTask(t, readState(t, statePath), correction)
	wt := filepath.Join(root, *task.Worktree)
	if filepath.IsAbs(*task.Worktree) {
		wt = *task.Worktree
	}
	data, err := os.ReadFile(filepath.Join(wt, "specs/acceptance-plan.md"))
	if err != nil {
		t.Fatal(err)
	}
	// Leave Task 1's acceptance section unchanged relative to this fresh base.
	carrier := strings.Replace(string(data), "## Task 1", "## Handoff guidance "+correction+"\nUse the reviewed scheduling metadata.\n\n## Task 1", 1)
	writeAmendmentFile(t, wt, "specs/acceptance-plan.md", carrier)
	testhelpers.MustGit(t, wt, "add", "specs/acceptance-plan.md")
	testhelpers.MustGit(t, wt, "commit", "-m", "fix: correct handoff guidance")
	manifest, err := json.Marshal(output)
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(t.TempDir(), "output.json")
	if err := os.WriteFile(manifestPath, manifest, 0644); err != nil {
		t.Fatal(err)
	}
	runAmendmentCLI(t, root, "set-task-output", correction, "--output", manifestPath, "--agent-id", "code-planner-1", "--json")
	runAmendmentCLI(t, root, "submit-for-review", correction, "HEAD", "--agent-id", "code-planner-1", "--json")
	authority := models.AgentAuthority{ID: "code-plan-reviewer-1", Generation: testhelpers.TestAgentGeneration}
	if _, err := ops.ClaimReviewerTask(ops.ClaimReviewerTaskInput{ProjectRoot: root, TaskID: correction, AgentID: authority.ID, Role: "code-plan-reviewer", Authority: &authority}); err != nil {
		t.Fatalf("claim independent correction review: %v", err)
	}
	review := *mustFindTask(t, readState(t, statePath), correction).ReviewCommit
	runAmendmentCLI(t, root, "submit-verdict", correction, "APPROVED", "--review-commit", review, "--agent-id", authority.ID, "--json")
	runAmendmentCLI(t, root, "wt-merge", correction, "--agent-id", authority.ID, "--json")
	return review
}

func TestAmendPlanCLI_D88PreservesStartedProviderAndUnchangedAllocation(t *testing.T) {
	root, statePath := setupAmendmentCLI(t)
	before := readState(t, statePath)
	bytesBefore := readStateBytes(t, statePath)
	if _, err := executeRootCommandCapture(t, root, "replan", "original", "--reason", "repair ordering"); err == nil || !strings.Contains(err.Error(), "live provider_dependencies") {
		t.Fatalf("started provider retirement must remain refused: %v", err)
	}
	if readStateBytes(t, statePath) != bytesBefore {
		t.Fatal("refused replan changed state")
	}
	correction := beginAmendmentCLI(t, root, statePath)
	// Even operator admission cannot escape the pending amendment fence.
	if _, err := ops.ExecuteTransitionsReportWith(root, "", ops.AdmitOperator); err != nil {
		t.Fatal(err)
	}
	for _, task := range readState(t, statePath).Tasks {
		if reflect.DeepEqual(task.EffectiveParentTasks(), []string{"original"}) {
			t.Fatal("operator generation escaped pending amendment")
		}
	}
	output := append([]models.OutputEntry(nil), before.FindTask("original").Output...)
	output[0].InheritInputs = &models.InheritInputs{Mode: models.InheritModeNone}
	review := mergeAmendmentCLI(t, root, statePath, correction, output)
	mergedState := readState(t, statePath)
	detection, err := ops.LoadDetectionContext(root)
	if err != nil {
		t.Fatal(err)
	}
	wake := agent.DetectOrchestratorWakeTriggersForProject(root, mergedState, detection.SprintTerminals, detection.PlanningPairs, detection.ManyToOneTransitions)
	if wake.Trigger != agent.WakeTriggerPlanningComplete {
		t.Fatalf("merged correction did not wake its adoption: %+v", wake)
	}
	_, instructions, err := prompts.RenderOrchestratorDashboard(mergedState, root, "orchestrator-1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(instructions, brand.Command("amend-plan", "original", "--apply", correction)) {
		t.Fatalf("adoption command absent from resumed wake: %s", instructions)
	}
	runAmendmentCLI(t, root, "amend-plan", "original", "--apply", correction, "--agent-id", "orchestrator-1", "--json")
	after := readState(t, statePath)
	original := mustFindTask(t, after, "original")
	if original.Status != models.TaskStatusMerged || *original.ReviewCommit != *before.FindTask("original").ReviewCommit || !reflect.DeepEqual(original.Output, output) {
		t.Fatalf("amendment lost provider identity, original review facts or reviewed output: %+v", original)
	}
	for _, id := range []string{"started-consumer", "ordinary"} {
		if !reflect.DeepEqual(before.FindTask(id), after.FindTask(id)) {
			t.Fatalf("amendment changed consumer %s", id)
		}
	}
	runAmendmentCLI(t, root, "plan-check", "original", "--pass", "--agent-id", "orchestrator-1", "--json")
	report, err := ops.ExecuteTransitionsReportWith(root, "", ops.AdmitReviewed)
	if err != nil || len(report.Failures) != 0 {
		t.Fatalf("reviewed generation: %v %+v", err, report)
	}
	after = readState(t, statePath)
	var childID string
	for _, task := range after.Tasks {
		if reflect.DeepEqual(task.EffectiveParentTasks(), []string{"original"}) {
			childID = task.ID
		}
		if reflect.DeepEqual(task.EffectiveParentTasks(), []string{correction}) {
			t.Fatal("correction generated children under a replacement identity")
		}
	}
	if childID == "" {
		t.Fatal("original output did not generate its child")
	}
	runAmendmentCLI(t, root, "claim-task", childID, "coder-1", "--json")
	source := mustFindTask(t, readState(t, statePath), childID).AcceptanceSource
	if source == nil || source.ParentTask != "original" || source.ParentReviewCommit != review {
		t.Fatalf("unchanged inherited allocation lacks current independent correction authority: %+v", source)
	}
}

// Validate the downstream fixture independently of the missing amendment
// command, so a missing ordinary lifecycle step cannot masquerade as D-88.
func TestD88CorrectionLifecycleFixture(t *testing.T) {
	root, statePath := setupAmendmentCLI(t)
	output := readState(t, statePath).FindTask("original").Output
	if err := db.For(statePath).Modify(func(s *models.State) error {
		correction := models.Task{ID: "fixture-correction", Type: models.TaskTypePlanning, RolePair: "code-planning-pair",
			Status: models.TaskStatusDraftCodingPlan, Description: "Correct reviewed handoff guidance", Priority: 1,
			SpecRef: "specs/acceptance-goal.md", DoneWhen: "Guidance reviewed", Scope: "planning artifact", Created: time.Now().UTC()}
		s.Tasks = append(s.Tasks, correction)
		s.Sprint.Scope.Planned = append(s.Sprint.Scope.Planned, correction.ID)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	review := mergeAmendmentCLI(t, root, statePath, "fixture-correction", output)
	merged := mustFindTask(t, readState(t, statePath), "fixture-correction")
	if merged.Status != models.TaskStatusMerged || merged.ReviewCommit == nil || *merged.ReviewCommit != review || merged.ApprovedBy == nil || *merged.ApprovedBy != "code-plan-reviewer-1" {
		t.Fatalf("ordinary correction fixture did not obtain independent merged review: %+v", merged)
	}
}

func TestPlanCheckCLI_D88ValidationNotesReachOnlySelectedChildAndBothRoles(t *testing.T) {
	root := setupPlanCheckCLI(t)
	statePath := paths.New(root).StatePath()
	head := testhelpers.MustGit(t, root, "rev-parse", "HEAD")
	if err := db.For(statePath).Modify(func(s *models.State) error {
		s.Tasks[0].Output = append(s.Tasks[0].Output, models.OutputEntry{Desc: "sibling", Scope: "pkg/y", DoneWhen: "tests pass", SpecRef: "README.md"})
		s.Tasks[0].BaseCommit, s.Tasks[0].ReviewCommit, s.Tasks[0].MergeCommit = &head, &head, &head
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	notes := filepath.Join(t.TempDir(), "notes.json")
	message := "Confirm the declared database probe exits successfully before validation."
	writeAmendmentFile(t, filepath.Dir(notes), filepath.Base(notes), fmt.Sprintf(`[{"output_index":0,"message":%q}]`, message))
	before := readState(t, statePath)
	runAmendmentCLI(t, root, "plan-check", "plan-1", "--pass", "--notes-file", notes, "--agent-id", "orchestrator-1", "--json")
	report, err := ops.ExecuteTransitionsReportWith(root, "", ops.AdmitReviewed)
	if err != nil || len(report.Failures) != 0 {
		t.Fatalf("generation: %v %+v", err, report)
	}
	s := readState(t, statePath)
	if !reflect.DeepEqual(before.FindTask("plan-1").Output, s.FindTask("plan-1").Output) {
		t.Fatal("advisory note changed canonical output metadata")
	}
	cfg, err := pipeline.LoadFrozen(root)
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for i := range s.Tasks {
		child := &s.Tasks[i]
		if !reflect.DeepEqual(child.EffectiveParentTasks(), []string{"plan-1"}) {
			continue
		}
		seen++
		expect := child.Description == "implement"
		stored := stateTaskField(t, statePath, child.ID, "validation_notes")
		if (stored != nil) != expect {
			t.Fatalf("child %s note selection: %v", child.ID, stored)
		}
		child.BaseCommit, child.ReviewCommit = &head, &head
		for _, role := range []string{"coder", "code-reviewer"} {
			strategy, err := agent.NewRoleStrategy(role, pipeline.NewResolver(cfg))
			if err != nil {
				t.Fatal(err)
			}
			prompt, err := strategy.BuildPrompt(s, agent.SupervisorConfig{Role: role, AgentID: role + "-1", ProjectRoot: root}, child.ID)
			if err != nil || strings.Contains(prompt, message) != expect {
				t.Fatalf("%s prompt note selection for %s: err=%v contains=%v expected=%v", role, child.ID, err, strings.Contains(prompt, message), expect)
			}
		}
	}
	if seen != 2 {
		t.Fatalf("generated %d children, want two", seen)
	}
}

// A separately merged correction can become stale before application. It must
// remain recoverable without reopening terminal state or dropping the fence.
func TestAmendPlanCLI_D88RecoversMergedUnappliableCorrection(t *testing.T) {
	root, statePath := setupAmendmentCLI(t)
	if err := db.For(statePath).Modify(func(s *models.State) error {
		provider := testhelpers.BuildTaskByStatus("prerequisite", models.TaskStatus("DRAFT_ARCHITECTURE"), time.Now().UTC())
		provider.Type, provider.RolePair = models.TaskTypePlanning, "architecture-pair"
		provider.Output = []models.OutputEntry{{Desc: "plan prerequisite", Scope: "prerequisite", DoneWhen: "reviewed", SpecRef: "README.md"}}
		s.Tasks = append(s.Tasks, provider)
		s.Sprint.Scope.Planned = append(s.Sprint.Scope.Planned, provider.ID)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	baseline := readState(t, statePath)
	first := beginAmendmentCLI(t, root, statePath)
	output := append([]models.OutputEntry(nil), baseline.FindTask("original").Output...)
	output[0].ProviderDependencies = []models.ProviderDependency{{ProviderTask: "prerequisite", Transition: "architecture-to-code-plan", Outputs: []int{0}}}
	mergeAmendmentCLI(t, root, statePath, first, output)
	if _, err := ops.CancelTask(root, "prerequisite", "retire obsolete prerequisite", "orchestrator-1"); err != nil {
		t.Fatalf("retire provider under merged unexpanded output: %v", err)
	}
	before := readStateBytes(t, statePath)
	stdout, err := executeRootCommandCapture(t, root, "amend-plan", "original", "--apply", first, "--agent-id", "orchestrator-1", "--json")
	if err == nil || !strings.Contains(stdout, "retired") || readStateBytes(t, statePath) != before {
		t.Fatalf("unsafe apply must refuse atomically: err=%v stdout=%s", err, stdout)
	}
	runAmendmentCLI(t, root, "plan-check", "original", "--hold", "confirm provisioned runtime", "--agent-id", "orchestrator-1", "--json")
	second := beginAmendmentCLI(t, root, statePath, "--replace-pending", first)
	if readState(t, statePath).FindTask(first).Status != models.TaskStatusMerged {
		t.Fatal("recovery reopened a terminal correction")
	}
	if _, err := ops.ExecuteTransitionsReportWith(root, "", ops.AdmitOperator); err != nil {
		t.Fatal(err)
	}
	for _, task := range readState(t, statePath).Tasks {
		if reflect.DeepEqual(task.EffectiveParentTasks(), []string{"original"}) {
			t.Fatal("recovery dropped the expansion fence")
		}
	}
	output[0].ProviderDependencies = nil
	review := mergeAmendmentCLI(t, root, statePath, second, output)
	runAmendmentCLI(t, root, "amend-plan", "original", "--apply", second, "--agent-id", "orchestrator-1", "--json")
	if check := readState(t, statePath).FindTask("original").PlanCheck; check == nil || check.Verdict != models.PlanCheckHeld || check.Ask != "confirm provisioned runtime" {
		t.Fatalf("recovered apply released the sticky human hold: %+v", check)
	}
	before = readStateBytes(t, statePath)
	runAmendmentCLI(t, root, "amend-plan", "original", "--apply", second, "--agent-id", "orchestrator-1", "--json")
	if readStateBytes(t, statePath) != before {
		t.Fatal("apply replay changed history or released a hold")
	}
	runAmendmentCLI(t, root, "plan-check", "original", "--clear", "--json")
	runAmendmentCLI(t, root, "plan-check", "original", "--pass", "--agent-id", "orchestrator-1", "--json")
	report, err := ops.ExecuteTransitionsReportWith(root, "", ops.AdmitReviewed)
	if err != nil || len(report.Failures) != 0 {
		t.Fatalf("recovered generation: %v %+v", err, report)
	}
	s := readState(t, statePath)
	for _, task := range s.Tasks {
		if reflect.DeepEqual(task.EffectiveParentTasks(), []string{first}) || reflect.DeepEqual(task.EffectiveParentTasks(), []string{second}) {
			t.Fatal("recovery correction generated under its own identity")
		}
		if reflect.DeepEqual(task.EffectiveParentTasks(), []string{"original"}) {
			runAmendmentCLI(t, root, "claim-task", task.ID, "coder-1", "--json")
			source := mustFindTask(t, readState(t, statePath), task.ID).AcceptanceSource
			if source == nil || source.ParentTask != "original" || source.ParentReviewCommit != review {
				t.Fatalf("recovery lost current review authority: %+v", source)
			}
			return
		}
	}
	t.Fatal("recovered original generated no claimable child")
}
