package ops

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/taskkind"
	"github.com/liza-mas/liza/internal/testhelpers"
)

const providerOpsTransition = "architecture-to-code-plan"

func providerOpsDependency(id string, outputs ...int) []models.ProviderDependency {
	return []models.ProviderDependency{{ProviderTask: id, Transition: providerOpsTransition, Outputs: outputs}}
}

func providerOpsTask(id, rolePair string, status models.TaskStatus) models.Task {
	task := testhelpers.BuildTaskByStatus(id, status, time.Now().UTC())
	task.RolePair = rolePair
	return task
}

func providerOpsOutput() models.OutputEntry {
	return models.OutputEntry{Desc: "Plan the shared contract", DoneWhen: "Contract reviewed", Scope: "contract", SpecRef: "README.md"}
}

func setupProviderOpsTest(t *testing.T, tasks ...models.Task) (string, string, *db.Blackboard) {
	t.Helper()
	root := t.TempDir()
	testhelpers.SetupTestGitRepo(t, root)
	statePath, _ := testhelpers.SetupLizaDir(t, root)
	state := testhelpers.CreateValidState()
	state.Goal.SpecRef = "README.md"
	state.Tasks = tasks
	for _, task := range tasks {
		state.Sprint.Scope.Planned = append(state.Sprint.Scope.Planned, task.ID)
	}
	for _, role := range []string{"architect", "code-planner", "coder", "orchestrator"} {
		state.Agents[role+"-1"] = testhelpers.RegisteredTestAgent(role)
	}
	return root, statePath, testhelpers.WriteInitialState(t, statePath, state)
}

func requireProviderOpsAtomicRefusal(t *testing.T, statePath string, before []byte, err error, fragments ...string) {
	t.Helper()
	if err == nil {
		t.Fatal("operation accepted an invalid provider prerequisite")
	}
	for _, fragment := range fragments {
		if !strings.Contains(err.Error(), fragment) {
			t.Fatalf("error = %v, want %q", err, fragment)
		}
	}
	if !bytes.Equal(before, replacementBytes(t, statePath)) {
		t.Fatal("refused operation changed the state file")
	}
}

func requireProviderOpsUnclaimed(t *testing.T, root, statePath, taskID, agentID string) {
	t.Helper()
	state := readClaimStateForTest(t, statePath)
	task := state.FindTask(taskID)
	if task == nil || task.AssignedTo != nil || task.LeaseExpires != nil || task.Iteration != 0 || task.Worktree != nil || task.BaseCommit != nil {
		t.Fatalf("refused claim published ownership or attempt state: %+v", task)
	}
	if task.Status != models.TaskStatusDraftCodingPlan && task.Status != models.TaskStatus("DRAFT_ARCHITECTURE") {
		t.Fatalf("refused claim changed initial status: %s", task.Status)
	}
	agent := state.Agents[agentID]
	if agent.CurrentTask != nil || agent.Status != models.AgentStatusWaiting || agent.IterationsTotal != 0 {
		t.Fatalf("refused claim changed agent ownership: %+v", agent)
	}
	if _, err := os.Stat(filepath.Join(root, paths.WorktreesDirName, taskID)); !os.IsNotExist(err) {
		t.Fatalf("refused claim left a worktree: %v", err)
	}
}

func TestSetTaskOutputProviderDependenciesAcceptsUnbornSelection(t *testing.T) {
	t.Parallel()
	owner := providerOpsTask("owner", "architecture-pair", models.TaskStatusImplementing)
	owner.Status = models.TaskStatus("ARCHITECTING")
	owner.AssignedTo = testhelpers.StringPtr("architect-1")
	provider := providerOpsTask("provider", "architecture-pair", models.TaskStatus("DRAFT_ARCHITECTURE"))
	root, statePath, _ := setupProviderOpsTest(t, owner, provider)
	output := providerOpsOutput()
	output.ProviderDependencies = providerOpsDependency(provider.ID, 2, 0)
	if err := SetTaskOutput(root, &SetTaskOutputInput{TaskID: owner.ID, AgentID: "architect-1", Output: []models.OutputEntry{output}}); err != nil {
		t.Fatalf("unborn selected provider outputs refused: %v", err)
	}
	after := readClaimStateForTest(t, statePath)
	if !reflect.DeepEqual(after.FindTask(owner.ID).Output[0].ProviderDependencies, output.ProviderDependencies) {
		t.Fatalf("provider intent was not persisted: %+v", after.FindTask(owner.ID).Output)
	}
	if after.FindTask("provider-cp-0") != nil || after.FindTask("provider-cp-2") != nil {
		t.Fatal("authoring provider intent materialized future tasks")
	}
}

func TestSetTaskOutputProviderDependenciesRejectsLaterLatentCycleAtomically(t *testing.T) {
	t.Parallel()
	provider := providerOpsTask("provider", "architecture-pair", models.TaskStatusImplementing)
	provider.Status = models.TaskStatus("ARCHITECTING")
	provider.AssignedTo = testhelpers.StringPtr("architect-1")
	consumer := providerOpsTask("consumer", "code-planning-pair", models.TaskStatusDraftCodingPlan)
	consumer.ProviderDependencies = providerOpsDependency(provider.ID, 0)
	root, statePath, _ := setupProviderOpsTest(t, provider, consumer)
	before := replacementBytes(t, statePath)
	output := providerOpsOutput()
	output.TaskDependsOn = []string{consumer.ID}
	err := SetTaskOutput(root, &SetTaskOutputInput{TaskID: provider.ID, AgentID: "architect-1", Output: []models.OutputEntry{output}})
	requireProviderOpsAtomicRefusal(t, statePath, before, err, "cycle", "consumer", "provider-cp-0")
	if len(readClaimStateForTest(t, statePath).FindTask(provider.ID).Output) != 0 {
		t.Fatal("cycle-closing output was published")
	}
}

func TestProceedProviderDependenciesPreservesFutureIntentUntilSelectedChildMerged(t *testing.T) {
	t.Parallel()
	owner := providerOpsTask("owner", "architecture-pair", models.TaskStatusMerged)
	output := providerOpsOutput()
	output.ProviderDependencies = providerOpsDependency("provider", 0)
	owner.Output = []models.OutputEntry{output}
	provider := providerOpsTask("provider", "architecture-pair", models.TaskStatus("DRAFT_ARCHITECTURE"))
	root, statePath, bb := setupProviderOpsTest(t, owner, provider)
	if err := bb.Modify(func(s *models.State) error { s.Sprint.Status = models.SprintStatusCompleted; return nil }); err != nil {
		t.Fatal(err)
	}
	generated, err := Proceed(root, owner.ID, providerOpsTransition)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(generated.ChildTaskIDs, []string{"owner-cp-0"}) {
		t.Fatalf("generated children = %v", generated.ChildTaskIDs)
	}
	child := mustReadTask(t, statePath, "owner-cp-0")
	if !reflect.DeepEqual(child.ProviderDependencies, output.ProviderDependencies) {
		t.Fatalf("generated consumer lost provider intent: %+v", child.ProviderDependencies)
	}
	_, err = ClaimTask(root, child.ID, "code-planner-1")
	if err == nil || !strings.Contains(err.Error(), "provider") {
		t.Fatalf("consumer claimed before provider expansion: %v", err)
	}
	requireProviderOpsUnclaimed(t, root, statePath, child.ID, "code-planner-1")
	if err := bb.Modify(func(s *models.State) error {
		p := providerOpsTask(provider.ID, provider.RolePair, models.TaskStatusMerged)
		p.Output = []models.OutputEntry{providerOpsOutput()}
		*s.FindTask(provider.ID) = p
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := Proceed(root, provider.ID, providerOpsTransition); err != nil {
		t.Fatal(err)
	}
	_, err = ClaimTask(root, child.ID, "code-planner-1")
	if err == nil || !strings.Contains(err.Error(), "provider-cp-0") {
		t.Fatalf("consumer claimed before selected child merged: %v", err)
	}
	requireProviderOpsUnclaimed(t, root, statePath, child.ID, "code-planner-1")
	if err := bb.Modify(func(s *models.State) error {
		merged := providerOpsTask("provider-cp-0", "code-planning-pair", models.TaskStatusMerged)
		merged.ParentTasks = []string{provider.ID}
		*s.FindTask(merged.ID) = merged
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := ClaimTask(root, child.ID, "code-planner-1"); err != nil {
		t.Fatalf("merged selected provider child did not release claim: %v", err)
	}
	after := mustReadTask(t, statePath, child.ID)
	if after.Status != models.TaskStatusCodePlanning || after.AssignedTo == nil || *after.AssignedTo != "code-planner-1" || after.Iteration != 1 {
		t.Fatalf("released consumer was not claimed: %+v", after)
	}
}

func TestProceedProviderDependenciesCrashRecoveryIsAtomic(t *testing.T) {
	for _, scenario := range []string{"restore missing", "conflicting declaration", "claimed child", "foreign child", "wrong role"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			owner := providerOpsTask("owner", "architecture-pair", models.TaskStatusMerged)
			output := providerOpsOutput()
			output.ProviderDependencies = providerOpsDependency("provider", 0)
			owner.Output = []models.OutputEntry{output, output}
			owner.TransitionsExecuted = map[string]bool{providerOpsTransition: true}
			provider := providerOpsTask("provider", "architecture-pair", models.TaskStatus("DRAFT_ARCHITECTURE"))
			child := providerOpsTask("owner-cp-0", "code-planning-pair", models.TaskStatusDraftCodingPlan)
			child.ParentTasks = []string{owner.ID}
			switch scenario {
			case "conflicting declaration":
				child.ProviderDependencies = providerOpsDependency(provider.ID, 1)
			case "claimed child":
				child.Status = models.TaskStatusCodePlanning
				child.AssignedTo = testhelpers.StringPtr("code-planner-1")
				child.LeaseExpires = testhelpers.TimePtr(time.Now().Add(time.Hour))
				child.Worktree = testhelpers.StringPtr(".worktrees/owner-cp-0")
				child.BaseCommit = testhelpers.StringPtr("abc1234")
			case "foreign child":
				child.ParentTasks = []string{provider.ID}
			case "wrong role":
				child.RolePair = "architecture-pair"
				child.Status = models.TaskStatus("DRAFT_ARCHITECTURE")
			}
			root, statePath, bb := setupProviderOpsTest(t, owner, provider, child)
			if err := bb.Modify(func(s *models.State) error { s.Sprint.Status = models.SprintStatusCompleted; return nil }); err != nil {
				t.Fatal(err)
			}
			before := replacementBytes(t, statePath)
			_, err := Proceed(root, owner.ID, providerOpsTransition)
			if scenario != "restore missing" {
				wantError := "conflicting or claimed child"
				if scenario == "foreign child" || scenario == "wrong role" {
					wantError = "incorrect transition provenance"
				}
				requireProviderOpsAtomicRefusal(t, statePath, before, err, wantError, child.ID)
				if readClaimStateForTest(t, statePath).FindTask("owner-cp-1") != nil {
					t.Fatal("failed recovery partially created missing sibling")
				}
				return
			}
			if err != nil {
				t.Fatalf("initial child declaration was not recovered: %v", err)
			}
			for _, id := range []string{"owner-cp-0", "owner-cp-1"} {
				if got := mustReadTask(t, statePath, id).ProviderDependencies; !reflect.DeepEqual(got, output.ProviderDependencies) {
					t.Fatalf("recovered %s declarations = %+v", id, got)
				}
			}
		})
	}
}

func TestProviderDependenciesMetadataOnlyRecoveryKeepsProducerOutputLive(t *testing.T) {
	t.Parallel()
	provider := providerOpsTask("provider", "architecture-pair", models.TaskStatus("DRAFT_ARCHITECTURE"))
	owner := providerOpsTask("owner", "architecture-pair", models.TaskStatusMerged)
	output := providerOpsOutput()
	output.ProviderDependencies = providerOpsDependency(provider.ID, 0)
	owner.Output = []models.OutputEntry{output}
	owner.TransitionsExecuted = map[string]bool{providerOpsTransition: true}
	child := providerOpsTask("owner-cp-0", "code-planning-pair", models.TaskStatusDraftCodingPlan)
	child.ParentTasks = []string{owner.ID}
	root, statePath, bb := setupProviderOpsTest(t, provider, owner, child)
	before := replacementBytes(t, statePath)
	_, err := ClaimTask(root, child.ID, "code-planner-1")
	requireProviderOpsAtomicRefusal(t, statePath, before, err, "generated provider_dependencies", "recover transition")
	requireProviderOpsUnclaimed(t, root, statePath, child.ID, "code-planner-1")
	_, err = CancelTask(root, provider.ID, "reviewed provider retirement", "orchestrator-1")
	requireProviderOpsAtomicRefusal(t, statePath, before, err, "live provider_dependencies", "owner output[0]")
	if err := bb.Modify(func(s *models.State) error { s.Sprint.Status = models.SprintStatusCompleted; return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := Proceed(root, owner.ID, providerOpsTransition); err != nil {
		t.Fatalf("metadata-only recovery refused: %v", err)
	}
	if got := mustReadTask(t, statePath, child.ID).ProviderDependencies; !reflect.DeepEqual(got, output.ProviderDependencies) {
		t.Fatalf("recovery did not transfer provider declaration to consumer: %+v", got)
	}
	before = replacementBytes(t, statePath)
	_, err = CancelTask(root, provider.ID, "reviewed provider retirement", "orchestrator-1")
	requireProviderOpsAtomicRefusal(t, statePath, before, err, "live provider_dependencies", child.ID)
	if _, err := CancelTask(root, child.ID, "retire repaired consumer declaration", "orchestrator-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := CancelTask(root, provider.ID, "reviewed provider retirement", "orchestrator-1"); err != nil {
		t.Fatalf("retired repaired consumer did not release provider: %v", err)
	}
	after := readClaimStateForTest(t, statePath)
	if after.FindTask(provider.ID).Status != models.TaskStatusAbandoned || after.FindTask(child.ID).Status != models.TaskStatusAbandoned {
		t.Fatal("provider and repaired consumer retirements were not persisted")
	}
	if !reflect.DeepEqual(after.FindTask(owner.ID).Output, owner.Output) {
		t.Fatal("provider retirement altered consumed producer output audit")
	}
}

func TestClaimTaskGeneratedProviderMetadataMustMatchReviewedOutput(t *testing.T) {
	for _, metadata := range []string{"missing", "conflicting"} {
		t.Run(metadata, func(t *testing.T) {
			t.Parallel()
			provider := providerOpsTask("provider", "architecture-pair", models.TaskStatusMerged)
			provider.Output = []models.OutputEntry{providerOpsOutput(), providerOpsOutput()}
			provider.TransitionsExecuted = map[string]bool{providerOpsTransition: true}
			selected0 := providerOpsTask("provider-cp-0", "code-planning-pair", models.TaskStatusMerged)
			selected0.ParentTasks = []string{provider.ID}
			selected1 := providerOpsTask("provider-cp-1", "code-planning-pair", models.TaskStatusMerged)
			selected1.ParentTasks = []string{provider.ID}
			owner := providerOpsTask("owner", "architecture-pair", models.TaskStatusMerged)
			output := providerOpsOutput()
			output.ProviderDependencies = providerOpsDependency(provider.ID, 0)
			owner.Output = []models.OutputEntry{output}
			owner.TransitionsExecuted = map[string]bool{providerOpsTransition: true}
			child := providerOpsTask("owner-cp-0", "code-planning-pair", models.TaskStatusDraftCodingPlan)
			child.ParentTasks = []string{owner.ID}
			if metadata == "conflicting" {
				child.ProviderDependencies = providerOpsDependency(provider.ID, 1)
			}
			root, statePath, bb := setupProviderOpsTest(t, provider, selected0, selected1, owner, child)
			before := replacementBytes(t, statePath)
			_, err := ClaimTask(root, child.ID, "code-planner-1")
			requireProviderOpsAtomicRefusal(t, statePath, before, err, "generated provider_dependencies", "owner output[0]", "recover transition")
			requireProviderOpsUnclaimed(t, root, statePath, child.ID, "code-planner-1")
			if metadata == "conflicting" {
				return
			}
			if err := bb.Modify(func(s *models.State) error { s.Sprint.Status = models.SprintStatusCompleted; return nil }); err != nil {
				t.Fatal(err)
			}
			if _, err := Proceed(root, owner.ID, providerOpsTransition); err != nil {
				t.Fatalf("missing generated declaration could not be recovered: %v", err)
			}
			if _, err := ClaimTask(root, child.ID, "code-planner-1"); err != nil {
				t.Fatalf("recovered metadata did not release already-satisfied consumer: %v", err)
			}
			after := mustReadTask(t, statePath, child.ID)
			if after.Status != models.TaskStatusCodePlanning || after.AssignedTo == nil || *after.AssignedTo != "code-planner-1" || !reflect.DeepEqual(after.ProviderDependencies, output.ProviderDependencies) {
				t.Fatalf("recovered consumer was not claimed with reviewed declaration: %+v", after)
			}
		})
	}
}

func TestProviderDependenciesTerminalChildMissingMetadataLeavesOutputAsAudit(t *testing.T) {
	t.Parallel()
	provider := providerOpsTask("provider", "architecture-pair", models.TaskStatus("DRAFT_ARCHITECTURE"))
	owner := providerOpsTask("owner", "architecture-pair", models.TaskStatusMerged)
	output := providerOpsOutput()
	output.ProviderDependencies = providerOpsDependency(provider.ID, 0)
	owner.Output = []models.OutputEntry{output}
	owner.TransitionsExecuted = map[string]bool{providerOpsTransition: true}
	child := providerOpsTask("owner-cp-0", "code-planning-pair", models.TaskStatusAbandoned)
	child.ParentTasks = []string{owner.ID}
	root, statePath, _ := setupProviderOpsTest(t, provider, owner, child)
	if _, err := CancelTask(root, provider.ID, "all generated consumers retired", "orchestrator-1"); err != nil {
		t.Fatalf("terminal child's missing metadata kept audit output live: %v", err)
	}
	after := readClaimStateForTest(t, statePath)
	if after.FindTask(provider.ID).Status != models.TaskStatusAbandoned || !reflect.DeepEqual(after.FindTask(owner.ID).Output, owner.Output) {
		t.Fatal("provider retirement was not persisted with producer audit intact")
	}
	if got := after.FindTask(child.ID).ProviderDependencies; len(got) != 0 {
		t.Fatalf("retirement restored metadata on terminal child: %+v", got)
	}
}

func TestProceedProviderDependenciesRepairsDeclarationWhenAllChildrenExist(t *testing.T) {
	t.Parallel()
	owner := providerOpsTask("owner", "architecture-pair", models.TaskStatusMerged)
	output := providerOpsOutput()
	output.ProviderDependencies = providerOpsDependency("provider", 0)
	owner.Output = []models.OutputEntry{output}
	owner.TransitionsExecuted = map[string]bool{providerOpsTransition: true}
	provider := providerOpsTask("provider", "architecture-pair", models.TaskStatus("DRAFT_ARCHITECTURE"))
	child := providerOpsTask("owner-cp-0", "code-planning-pair", models.TaskStatusDraftCodingPlan)
	child.ParentTasks = []string{owner.ID}
	root, statePath, bb := setupProviderOpsTest(t, owner, provider, child)
	if err := bb.Modify(func(s *models.State) error { s.Sprint.Status = models.SprintStatusCompleted; return nil }); err != nil {
		t.Fatal(err)
	}
	result, err := Proceed(root, owner.ID, providerOpsTransition)
	if err != nil {
		t.Fatalf("declaration-only recovery refused: %v", err)
	}
	if result.SourceTaskID != owner.ID || result.TransitionName != providerOpsTransition || len(result.ChildTaskIDs) != 0 {
		t.Fatalf("declaration repair reported new tasks: %+v", result)
	}
	after := readClaimStateForTest(t, statePath)
	repaired := after.FindTask(child.ID)
	if !reflect.DeepEqual(repaired.ProviderDependencies, output.ProviderDependencies) {
		t.Fatalf("declaration-only recovery was not persisted: %+v", repaired)
	}
	if len(after.Tasks) != 3 || !slices.Equal(after.Sprint.Scope.Planned, []string{owner.ID, provider.ID, child.ID}) {
		t.Fatalf("declaration-only repair duplicated tasks or sprint scope: %+v", after.Sprint.Scope.Planned)
	}
	requireProviderOpsUnclaimed(t, root, statePath, child.ID, "code-planner-1")
	beforeReplay := replacementBytes(t, statePath)
	_, err = Proceed(root, owner.ID, providerOpsTransition)
	if err == nil || !strings.Contains(err.Error(), "already executed") {
		t.Fatalf("completed declaration repair was not idempotent: %v", err)
	}
	if !bytes.Equal(beforeReplay, replacementBytes(t, statePath)) {
		t.Fatal("completed recovery replay changed state")
	}
}

func TestExecuteAvailableTransitionsProviderDependenciesRepairsDeclarationWithoutMissingChildren(t *testing.T) {
	t.Parallel()
	const transition = "arch-decompose"
	owner := providerOpsTask("owner", "architecture-main-pair", models.TaskStatusMerged)
	owner.Type = models.TaskTypeArchitecture
	output := providerOpsOutput()
	output.ArchRef = "README.md"
	output.Decomposition = &models.DecompositionManifest{
		OwnedFiles: []string{"contract.md"}, CoverageNotes: "Own the shared contract boundary.",
	}
	output.ProviderDependencies = providerOpsDependency("provider", 0)
	owner.Output = []models.OutputEntry{output}
	owner.TransitionsExecuted = map[string]bool{transition: true}
	provider := providerOpsTask("provider", "architecture-pair", models.TaskStatus("DRAFT_ARCHITECTURE"))
	child := providerOpsTask("owner-ar-0", "architecture-pair", models.TaskStatus("DRAFT_ARCHITECTURE"))
	child.Type = models.TaskTypeArchitecture
	child.ParentTasks = []string{owner.ID}
	child.ArchRef = output.ArchRef
	root, statePath, _ := setupProviderOpsTest(t, owner, provider, child)
	results, err := ExecuteAvailableTransitions(root, "auto")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].SourceTaskID != owner.ID || results[0].TransitionName != transition || len(results[0].ChildTaskIDs) != 0 {
		t.Fatalf("automatic scan did not report declaration repair: %+v", results)
	}
	after := readClaimStateForTest(t, statePath)
	if got := after.FindTask(child.ID).ProviderDependencies; !reflect.DeepEqual(got, output.ProviderDependencies) {
		t.Fatalf("automatic declaration repair was not persisted: %+v", got)
	}
	if len(after.Tasks) != 3 || !slices.Equal(after.Sprint.Scope.Planned, []string{owner.ID, provider.ID, child.ID}) {
		t.Fatalf("automatic repair duplicated tasks or sprint scope: %+v", after.Sprint.Scope.Planned)
	}
	_, err = ClaimTask(root, child.ID, "architect-1")
	if err == nil || !strings.Contains(err.Error(), provider.ID) {
		t.Fatalf("automatically repaired child bypassed prerequisite: %v", err)
	}
	requireProviderOpsUnclaimed(t, root, statePath, child.ID, "architect-1")
	results, err = ExecuteAvailableTransitions(root, "auto")
	if err != nil || len(results) != 0 {
		t.Fatalf("automatic repair remained incomplete: results=%+v err=%v", results, err)
	}
}

func TestProviderDependenciesKindChildMetadataRecovery(t *testing.T) {
	for _, mode := range []string{"manual", "automatic"} {
		for _, metadata := range []string{"missing", "conflicting"} {
			t.Run(mode+"/"+metadata, func(t *testing.T) {
				t.Parallel()
				transition := providerOpsTransition
				owner := providerOpsTask("owner", "architecture-pair", models.TaskStatusMerged)
				child := providerOpsTask("owner-cp-0", "code-planning-pair", models.TaskStatusDraftCodingPlan)
				output := providerOpsOutput()
				output.Kind = taskkind.PreCommitBootstrap
				output.ProviderDependencies = providerOpsDependency("provider", 0)
				if mode == "automatic" {
					transition = "arch-decompose"
					owner.RolePair = "architecture-main-pair"
					owner.Type = models.TaskTypeArchitecture
					child = providerOpsTask("owner-ar-0", "architecture-pair", models.TaskStatus("DRAFT_ARCHITECTURE"))
					child.Type = models.TaskTypeArchitecture
					output.ArchRef = "README.md"
					output.Decomposition = &models.DecompositionManifest{OwnedFiles: []string{"contract.md"}, CoverageNotes: "Own the contract boundary."}
					child.ArchRef = output.ArchRef
				}
				owner.Output = []models.OutputEntry{output}
				owner.TransitionsExecuted = map[string]bool{transition: true}
				child.Kind = output.Kind
				child.ParentTasks = []string{owner.ID}
				if metadata == "conflicting" {
					child.ProviderDependencies = providerOpsDependency("provider", 1)
				}
				provider := providerOpsTask("provider", "architecture-pair", models.TaskStatus("DRAFT_ARCHITECTURE"))
				root, statePath, bb := setupProviderOpsTest(t, owner, provider, child)
				if err := bb.Modify(func(s *models.State) error { s.Sprint.Status = models.SprintStatusCompleted; return nil }); err != nil {
					t.Fatal(err)
				}
				before := replacementBytes(t, statePath)
				_, retirementErr := CancelTask(root, provider.ID, "retire before owned Kind recovery", "orchestrator-1")
				requireProviderOpsAtomicRefusal(t, statePath, before, retirementErr, "live provider_dependencies", "owner output[0]")
				if mode == "manual" {
					result, err := Proceed(root, owner.ID, transition)
					if metadata == "conflicting" {
						requireProviderOpsAtomicRefusal(t, statePath, before, err, "conflicting or claimed child", child.ID)
						return
					}
					if err != nil || result == nil || len(result.ChildTaskIDs) != 0 {
						t.Fatalf("owned Kind child repair: result=%+v err=%v", result, err)
					}
				} else {
					report, err := ExecuteAvailableTransitionsReport(root, "auto")
					if err != nil {
						t.Fatal(err)
					}
					if metadata == "conflicting" {
						if len(report.Results) != 0 || len(report.Failures) != 1 || !strings.Contains(report.Failures[0].Error, "conflicting or claimed child") || !strings.Contains(report.Failures[0].Error, child.ID) {
							t.Fatalf("automatic recovery ignored conflicting Kind metadata: %+v", report)
						}
						if !bytes.Equal(before, replacementBytes(t, statePath)) {
							t.Fatal("automatic conflict refusal changed state")
						}
						return
					}
					if len(report.Failures) != 0 || len(report.Results) != 1 || report.Results[0].SourceTaskID != owner.ID || report.Results[0].TransitionName != transition || len(report.Results[0].ChildTaskIDs) != 0 {
						t.Fatalf("automatic owned Kind child repair: %+v", report)
					}
				}
				after := readClaimStateForTest(t, statePath)
				repaired := after.FindTask(child.ID)
				if !reflect.DeepEqual(repaired.ProviderDependencies, output.ProviderDependencies) || repaired.Kind != output.Kind || repaired.AssignedTo != nil || repaired.Iteration != 0 {
					t.Fatalf("owned Kind child metadata was not repaired safely: %+v", repaired)
				}
				if len(after.Tasks) != 3 || !slices.Equal(after.Sprint.Scope.Planned, []string{owner.ID, provider.ID, child.ID}) {
					t.Fatal("owned Kind recovery duplicated tasks or sprint scope")
				}
			})
		}
	}
}

func TestProviderDependenciesConsumedForeignKindOutputDoesNotHoldProvider(t *testing.T) {
	t.Parallel()
	owner := providerOpsTask("owner", "architecture-pair", models.TaskStatusMerged)
	output := providerOpsOutput()
	output.Kind = taskkind.PreCommitBootstrap
	output.ProviderDependencies = providerOpsDependency("provider", 0)
	owner.Output = []models.OutputEntry{output}
	owner.TransitionsExecuted = map[string]bool{providerOpsTransition: true}
	provider := providerOpsTask("provider", "architecture-pair", models.TaskStatus("DRAFT_ARCHITECTURE"))
	otherProvider := providerOpsTask("other-provider", "architecture-pair", models.TaskStatus("DRAFT_ARCHITECTURE"))
	incumbent := providerOpsTask("foreign-cp-0", "code-planning-pair", models.TaskStatusDraftCodingPlan)
	incumbent.Kind = output.Kind
	incumbent.ProviderDependencies = providerOpsDependency(otherProvider.ID, 1)
	consumer := providerOpsTask("live-consumer", "architecture-pair", models.TaskStatus("DRAFT_ARCHITECTURE"))
	consumer.ProviderDependencies = providerOpsDependency(provider.ID, 0)
	root, statePath, bb := setupProviderOpsTest(t, owner, provider, otherProvider, incumbent, consumer)
	if err := bb.Modify(func(s *models.State) error { s.Sprint.Status = models.SprintStatusCompleted; return nil }); err != nil {
		t.Fatal(err)
	}
	baseline := readClaimStateForTest(t, statePath)
	before := replacementBytes(t, statePath)
	_, err := Proceed(root, owner.ID, providerOpsTransition)
	requireProviderOpsAtomicRefusal(t, statePath, before, err, "already executed")
	_, err = CancelTask(root, provider.ID, "retire consumed provider", "orchestrator-1")
	requireProviderOpsAtomicRefusal(t, statePath, before, err, "live provider_dependencies", consumer.ID)
	if _, err := CancelTask(root, consumer.ID, "retire genuine consumer declaration", "orchestrator-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := CancelTask(root, provider.ID, "retire consumed provider", "orchestrator-1"); err != nil {
		t.Fatalf("consumed foreign-Kind output held provider retirement: %v", err)
	}
	after := readClaimStateForTest(t, statePath)
	if after.FindTask(provider.ID).Status != models.TaskStatusAbandoned || after.FindTask("owner-cp-0") != nil {
		t.Fatal("retirement did not preserve the consumed deduplication outcome")
	}
	if !reflect.DeepEqual(after.FindTask(incumbent.ID), baseline.FindTask(incumbent.ID)) || !reflect.DeepEqual(after.FindTask(owner.ID).Output, baseline.FindTask(owner.ID).Output) {
		t.Fatal("recovery or retirement changed foreign incumbent or producer output audit")
	}
}

func TestClaimTaskProviderDependenciesPreflightHasNoEffects(t *testing.T) {
	t.Parallel()
	provider := providerOpsTask("provider", "architecture-pair", models.TaskStatus("DRAFT_ARCHITECTURE"))
	consumer := providerOpsTask("consumer", "architecture-pair", models.TaskStatus("DRAFT_ARCHITECTURE"))
	consumer.ProviderDependencies = providerOpsDependency(provider.ID, 0)
	root, statePath, _ := setupProviderOpsTest(t, provider, consumer)
	before := replacementBytes(t, statePath)
	_, err := ClaimTask(root, consumer.ID, "architect-1")
	requireProviderOpsAtomicRefusal(t, statePath, before, err, "provider")
	requireProviderOpsUnclaimed(t, root, statePath, consumer.ID, "architect-1")
}

func TestClaimTaskProviderDependenciesRechecksSelectedChildUnderLock(t *testing.T) {
	provider := providerOpsTask("provider", "architecture-pair", models.TaskStatusMerged)
	provider.Output = []models.OutputEntry{providerOpsOutput()}
	provider.TransitionsExecuted = map[string]bool{providerOpsTransition: true}
	selected := providerOpsTask("provider-cp-0", "code-planning-pair", models.TaskStatusMerged)
	selected.ParentTasks = []string{provider.ID}
	consumer := providerOpsTask("consumer", "architecture-pair", models.TaskStatus("DRAFT_ARCHITECTURE"))
	consumer.ProviderDependencies = providerOpsDependency(provider.ID, 0)
	root, statePath, bb := setupProviderOpsTest(t, provider, selected, consumer)
	previousHooks := testClaimTaskHooks
	hookRan := false
	testClaimTaskHooks = &claimTaskTestHooks{beforePhase3Modify: func() {
		hookRan = true
		if err := bb.Modify(func(s *models.State) error {
			pending := providerOpsTask(selected.ID, selected.RolePair, models.TaskStatusDraftCodingPlan)
			pending.ParentTasks = selected.ParentTasks
			*s.FindTask(selected.ID) = pending
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}}
	t.Cleanup(func() { testClaimTaskHooks = previousHooks })
	_, err := ClaimTask(root, consumer.ID, "architect-1")
	if !hookRan {
		t.Fatal("claim never reached the preflight-to-commit race boundary")
	}
	if err == nil || !strings.Contains(err.Error(), selected.ID) {
		t.Fatalf("claim ignored selected-child change before commit: %v", err)
	}
	requireProviderOpsUnclaimed(t, root, statePath, consumer.ID, "architect-1")
}

func TestUnblockTaskProviderDependenciesRequiresReadinessOnlyWhenAssigning(t *testing.T) {
	for _, assignTo := range []string{"architect-1", ""} {
		t.Run("assign-to="+assignTo, func(t *testing.T) {
			t.Parallel()
			provider := providerOpsTask("provider", "architecture-pair", models.TaskStatus("DRAFT_ARCHITECTURE"))
			consumer := providerOpsTask("consumer", "architecture-pair", models.TaskStatusBlocked)
			consumer.ProviderDependencies = providerOpsDependency(provider.ID, 0)
			consumer.Worktree = nil
			if assignTo != "" {
				consumer.Worktree = testhelpers.StringPtr(".worktrees/consumer")
			}
			root, statePath, _ := setupProviderOpsTest(t, provider, consumer)
			before := replacementBytes(t, statePath)
			result, err := UnblockTaskWithOptions(root, consumer.ID, "prerequisite declaration repaired", "orchestrator-1", UnblockTaskOptions{AssignTo: assignTo})
			if assignTo != "" {
				requireProviderOpsAtomicRefusal(t, statePath, before, err, "unmet dependencies", provider.ID)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			after := mustReadTask(t, statePath, consumer.ID)
			if result.Claimable || after.Status != models.TaskStatus("DRAFT_ARCHITECTURE") || after.AssignedTo != nil || after.LeaseExpires != nil {
				t.Fatalf("unassigned restore bypassed prerequisite: result=%+v task=%+v", result, after)
			}
			if !reflect.DeepEqual(after.ProviderDependencies, consumer.ProviderDependencies) {
				t.Fatal("unblock erased provider declaration")
			}
		})
	}
}

func TestProviderDependenciesRetirementRequiresRetiringConsumerFirst(t *testing.T) {
	for _, scenario := range []string{"cancel provider", "supersede provider", "cancel selected child", "supersede selected child", "replan provider"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			provider := providerOpsTask("provider", "architecture-pair", models.TaskStatus("DRAFT_ARCHITECTURE"))
			consumer := providerOpsTask("consumer", "architecture-pair", models.TaskStatus("DRAFT_ARCHITECTURE"))
			consumer.ProviderDependencies = providerOpsDependency(provider.ID, 0)
			targetID := provider.ID
			tasks := []models.Task{provider, consumer}
			if strings.Contains(scenario, "selected child") {
				tasks[0] = providerOpsTask(provider.ID, provider.RolePair, models.TaskStatusMerged)
				tasks[0].Output = []models.OutputEntry{providerOpsOutput()}
				tasks[0].TransitionsExecuted = map[string]bool{providerOpsTransition: true}
				child := providerOpsTask("provider-cp-0", "code-planning-pair", models.TaskStatusDraftCodingPlan)
				child.ParentTasks = []string{provider.ID}
				tasks = append(tasks, child)
				targetID = child.ID
			}
			if scenario == "replan provider" {
				tasks[0] = providerOpsTask(provider.ID, provider.RolePair, models.TaskStatusMerged)
				tasks[0].Output = []models.OutputEntry{providerOpsOutput()}
			}
			if strings.HasPrefix(scenario, "supersede") {
				target := &tasks[0]
				if targetID != provider.ID {
					target = &tasks[2]
				}
				replacement := providerOpsTask("replacement", target.RolePair, target.Status)
				replacement.ParentTasks = target.ParentTasks
				tasks = append(tasks, replacement)
			}
			root, statePath, _ := setupProviderOpsTest(t, tasks...)
			retire := func() error {
				switch {
				case strings.HasPrefix(scenario, "cancel"):
					_, err := CancelTask(root, targetID, "reviewed retirement", "orchestrator-1")
					return err
				case strings.HasPrefix(scenario, "supersede"):
					_, err := SupersedeTask(root, targetID, []string{"replacement"}, "reviewed replacement", "orchestrator-1")
					return err
				default:
					_, err := Replan(root, &ReplanInput{TaskID: targetID, ChangedBy: "human"})
					return err
				}
			}
			before := replacementBytes(t, statePath)
			requireProviderOpsAtomicRefusal(t, statePath, before, retire(), "live provider_dependencies", consumer.ID)
			if _, err := CancelTask(root, consumer.ID, "retire consumer declaration first", "orchestrator-1"); err != nil {
				t.Fatal(err)
			}
			if err := retire(); err != nil {
				t.Fatalf("retired consumer did not release provider identity: %v", err)
			}
		})
	}
}

func TestProviderDependenciesHistoricalOutputDoesNotBlockProviderRetirement(t *testing.T) {
	for _, operation := range []string{"cancel", "replan"} {
		for _, historical := range []string{"replanned", "retired handoff"} {
			t.Run(operation+"/"+historical, func(t *testing.T) {
				t.Parallel()
				provider := providerOpsTask("provider", "architecture-pair", models.TaskStatus("DRAFT_ARCHITECTURE"))
				if operation == "replan" {
					provider = providerOpsTask(provider.ID, provider.RolePair, models.TaskStatusMerged)
					provider.Output = []models.OutputEntry{providerOpsOutput()}
				}
				producer := providerOpsTask("historical-producer", "architecture-pair", models.TaskStatusMerged)
				output := providerOpsOutput()
				output.ProviderDependencies = providerOpsDependency(provider.ID, 0)
				producer.Output = []models.OutputEntry{output}
				correction := providerOpsTask("correction", "architecture-pair", models.TaskStatusMerged)
				correction.Output = []models.OutputEntry{providerOpsOutput()}
				if historical == "replanned" {
					producer.TransitionsExecuted = map[string]bool{"replanned": true, providerOpsTransition: true}
				} else {
					producer.PlanCheck = &models.PlanCheck{
						Verdict: models.PlanCheckReplaced, ReplacedBy: correction.ID, By: "human", At: time.Now().UTC(),
					}
				}
				root, statePath, _ := setupProviderOpsTest(t, provider, producer, correction)
				if operation == "cancel" {
					if _, err := CancelTask(root, provider.ID, "retire provider with historical consumer output", "orchestrator-1"); err != nil {
						t.Fatalf("historical output blocked cancellation: %v", err)
					}
					if got := mustReadTask(t, statePath, provider.ID).Status; got != models.TaskStatusAbandoned {
						t.Fatalf("provider status = %s, want ABANDONED", got)
					}
				} else {
					result, err := Replan(root, &ReplanInput{TaskID: provider.ID, ChangedBy: "human"})
					if err != nil {
						t.Fatalf("historical output blocked replan: %v", err)
					}
					after := readClaimStateForTest(t, statePath)
					if !after.FindTask(provider.ID).TransitionsExecuted["replanned"] || after.FindTask(result.NewTaskID) == nil {
						t.Fatal("provider replan was not persisted")
					}
				}
				if got := mustReadTask(t, statePath, producer.ID).Output; !reflect.DeepEqual(got, producer.Output) {
					t.Fatalf("retirement rewrote historical output audit: %+v", got)
				}
			})
		}
	}
}

func TestReplanProviderDependenciesPreservesConsumerDeclaration(t *testing.T) {
	t.Parallel()
	provider := providerOpsTask("provider", "architecture-pair", models.TaskStatus("DRAFT_ARCHITECTURE"))
	consumer := providerOpsTask("consumer", "architecture-pair", models.TaskStatusMerged)
	consumer.Output = []models.OutputEntry{providerOpsOutput()}
	consumer.ProviderDependencies = providerOpsDependency(provider.ID, 0)
	root, statePath, _ := setupProviderOpsTest(t, provider, consumer)
	result, err := Replan(root, &ReplanInput{TaskID: consumer.ID, ChangedBy: "human", Reason: "revise consumer plan"})
	if err != nil {
		t.Fatal(err)
	}
	replacement := mustReadTask(t, statePath, result.NewTaskID)
	if replacement.Status != models.TaskStatus("DRAFT_ARCHITECTURE") || !reflect.DeepEqual(replacement.ProviderDependencies, consumer.ProviderDependencies) {
		t.Fatalf("replan lost consumer prerequisite: %+v", replacement)
	}
	_, err = ClaimTask(root, replacement.ID, "architect-1")
	if err == nil || !strings.Contains(err.Error(), provider.ID) {
		t.Fatalf("replanned consumer bypassed provider prerequisite: %v", err)
	}
}

func TestPlanCheckPassProviderDependenciesRejectsInvalidReferenceAtomically(t *testing.T) {
	t.Parallel()
	plan := handoffPlan("plan", "architecture-pair")
	plan.Output[0].ProviderDependencies = providerOpsDependency("missing-provider", 0)
	root, statePath := setupPlanCheckTest(t, plan)
	before := replacementBytes(t, statePath)
	_, err := RecordPlanCheck(root, PlanCheckInput{TaskID: plan.ID, Action: PlanCheckActionPass, Authority: orchestratorAuthority()})
	requireProviderOpsAtomicRefusal(t, statePath, before, err, "provider dependencies", "missing-provider")
	if readPlanCheck(t, statePath, plan.ID) != nil {
		t.Fatal("invalid provider declaration received plan-check pass")
	}
}

func TestAddTaskProviderDependenciesPersistsDeclaration(t *testing.T) {
	t.Parallel()
	provider := providerOpsTask("provider", "architecture-pair", models.TaskStatus("DRAFT_ARCHITECTURE"))
	root, statePath, _ := setupProviderOpsTest(t, provider)
	input := AddTaskInput{ID: "consumer", RolePair: "architecture-pair", Description: "Consume reviewed contract", SpecRef: "README.md", DoneWhen: "Consumer reviewed", Scope: "consumer", Priority: 1, ProviderDependencies: providerOpsDependency(provider.ID, 0)}
	if _, err := AddTask(statePath, paths.New(root).LogPath(), &input, "orchestrator-1"); err != nil {
		t.Fatal(err)
	}
	consumer := mustReadTask(t, statePath, input.ID)
	if !reflect.DeepEqual(consumer.ProviderDependencies, input.ProviderDependencies) {
		t.Fatalf("add-task lost provider declaration: %+v", consumer.ProviderDependencies)
	}
	_, err := ClaimTask(root, consumer.ID, "architect-1")
	if err == nil || !strings.Contains(err.Error(), provider.ID) {
		t.Fatalf("added consumer bypassed prerequisite: %v", err)
	}
}

func TestReplaceTaskProviderDependenciesPersistsNewInput(t *testing.T) {
	t.Parallel()
	f := newReplacementFixture(t)
	bb := db.For(f.statePath)
	if err := bb.Modify(func(s *models.State) error {
		provider := providerOpsTask("provider", "architecture-pair", models.TaskStatus("DRAFT_ARCHITECTURE"))
		s.Tasks = append(s.Tasks, provider)
		s.Sprint.Scope.Planned = append(s.Sprint.Scope.Planned, provider.ID)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	f.input.Replacement.ProviderDependencies = providerOpsDependency("provider", 0)
	if _, err := f.run(); err != nil {
		t.Fatal(err)
	}
	after := replacementState(t, f)
	assertReplacementCommitted(t, after)
	if !reflect.DeepEqual(after.FindTask("replacement").ProviderDependencies, f.input.Replacement.ProviderDependencies) {
		t.Fatal("replace-task lost new consumer provider declaration")
	}
}

func TestRetargetDependencyPreservesProviderDependencies(t *testing.T) {
	t.Parallel()
	provider := providerOpsTask("provider", "architecture-pair", models.TaskStatus("DRAFT_ARCHITECTURE"))
	old := providerOpsTask("old", "architecture-pair", models.TaskStatus("DRAFT_ARCHITECTURE"))
	newDep := providerOpsTask("new", "architecture-pair", models.TaskStatus("DRAFT_ARCHITECTURE"))
	consumer := providerOpsTask("consumer", "architecture-pair", models.TaskStatus("DRAFT_ARCHITECTURE"))
	consumer.DependsOn = []string{old.ID}
	consumer.ProviderDependencies = providerOpsDependency(provider.ID, 0)
	root, statePath, _ := setupProviderOpsTest(t, provider, old, newDep, consumer)
	if _, err := RetargetDependency(root, consumer.ID, old.ID, []string{newDep.ID}, "reviewed ordinary prerequisite", "orchestrator-1"); err != nil {
		t.Fatal(err)
	}
	after := mustReadTask(t, statePath, consumer.ID)
	if !slices.Equal(after.DependsOn, []string{newDep.ID}) || !reflect.DeepEqual(after.ProviderDependencies, consumer.ProviderDependencies) {
		t.Fatalf("ordinary rewrite erased provider intent: %+v", after)
	}
}

func TestRetargetDependencyRejectsCombinedProviderCycleAtomically(t *testing.T) {
	t.Parallel()
	provider := providerOpsTask("provider", "architecture-pair", models.TaskStatusMerged)
	provider.Output = []models.OutputEntry{providerOpsOutput()}
	provider.TransitionsExecuted = map[string]bool{providerOpsTransition: true}
	consumer := providerOpsTask("consumer", "architecture-pair", models.TaskStatus("DRAFT_ARCHITECTURE"))
	consumer.ProviderDependencies = providerOpsDependency(provider.ID, 0)
	selected := providerOpsTask("provider-cp-0", "code-planning-pair", models.TaskStatusDraftCodingPlan)
	selected.ParentTasks = []string{provider.ID}
	selected.DependsOn = []string{"middle"}
	middle := providerOpsTask("middle", "code-planning-pair", models.TaskStatusDraftCodingPlan)
	middle.DependsOn = []string{"old"}
	old := providerOpsTask("old", "architecture-pair", models.TaskStatus("DRAFT_ARCHITECTURE"))
	root, statePath, _ := setupProviderOpsTest(t, provider, consumer, selected, middle, old)
	before := replacementBytes(t, statePath)
	_, err := RetargetDependency(root, middle.ID, old.ID, []string{consumer.ID}, "reviewed prerequisite replacement", "orchestrator-1")
	requireProviderOpsAtomicRefusal(t, statePath, before, err, "dependency cycle")
	var operational *OperationalError
	if !errors.As(err, &operational) {
		t.Fatalf("retarget refusal lost structured cycle diagnostic: %v", err)
	}
	cyclePath, ok := operational.SafeDetails()["cycle_path"].([]string)
	if !ok || !slices.Contains(cyclePath, consumer.ID) || !slices.Contains(cyclePath, selected.ID) {
		t.Fatalf("combined cycle path = %v, want consumer and selected provider child", cyclePath)
	}
}

func TestRepairSupersededDependenciesPreservesProviderDeclarationForAudit(t *testing.T) {
	t.Parallel()
	provider := providerOpsTask("provider", "architecture-pair", models.TaskStatus("DRAFT_ARCHITECTURE"))
	retired := providerOpsTask("retired", "architecture-pair", models.TaskStatusSuperseded)
	retired.SupersededBy = []string{"replacement"}
	retired.DependsOn = []string{"downstream"}
	retired.ProviderDependencies = providerOpsDependency(provider.ID, 0)
	replacement := providerOpsTask("replacement", "architecture-pair", models.TaskStatus("DRAFT_ARCHITECTURE"))
	downstream := providerOpsTask("downstream", "code-planning-pair", models.TaskStatusDraftCodingPlan)
	root, statePath, _ := setupProviderOpsTest(t, provider, retired, replacement, downstream)
	if _, err := RepairSupersededDependencies(root, retired.ID, "remove obsolete downstream edge", "orchestrator-1"); err != nil {
		t.Fatal(err)
	}
	after := mustReadTask(t, statePath, retired.ID)
	if len(after.DependsOn) != 0 || !reflect.DeepEqual(after.ProviderDependencies, retired.ProviderDependencies) {
		t.Fatalf("terminal graph repair changed typed audit declaration: %+v", after)
	}
}
