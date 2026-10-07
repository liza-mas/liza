package ops

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/statevalidate"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// D-69 (ADR-0188): draft output, which must pass a fresh approval before
// anything is generated from it, holds no provider either. It goes stale
// under the unexpanded-plan rule, and the consumer re-authors it in place.

const staleDraftOutputBlocker = "output[0].provider_dependencies[0] declares retired provider provider"

// draftOutputConsumer is the D-69 shape: a started architecture task whose
// unmerged output[0] declares the provider's output 0 directly, with no
// ordinary depends_on edge on the provider. Each status carries the fields
// state validation requires of it; setupDraftOutputTest supplies its agents
// and worktree directory.
func draftOutputConsumer(status models.TaskStatus) models.Task {
	consumer := providerOpsTask("consumer", "architecture-pair", status)
	consumer.History = append(consumer.History, models.TaskHistoryEntry{Time: time.Now().UTC(), Event: models.TaskEventClaimed, Agent: testhelpers.StringPtr("architect-1")})
	consumer.Worktree = testhelpers.StringPtr(".worktrees/consumer")
	consumer.BaseCommit = testhelpers.StringPtr("abc1234")
	output := providerOpsOutput()
	output.ProviderDependencies = providerOpsDependency("provider", 0)
	consumer.Output = []models.OutputEntry{output}
	lease := time.Now().UTC().Add(30 * time.Minute)
	switch status {
	case models.TaskStatus("ARCHITECTING"):
		consumer.AssignedTo = testhelpers.StringPtr("architect-1")
		consumer.LeaseExpires = &lease
	case models.TaskStatus("ARCHITECTURE_TO_REVIEW"):
		consumer.ReviewCommit = testhelpers.StringPtr("def5678")
	case models.TaskStatus("REVIEWING_ARCHITECTURE"):
		consumer.ReviewCommit = testhelpers.StringPtr("def5678")
		consumer.ReviewingBy = testhelpers.StringPtr("architecture-reviewer-1")
		consumer.ReviewLeaseExpires = &lease
	case models.TaskStatus("ARCHITECTURE_REJECTED"):
		consumer.RejectionReason = testhelpers.StringPtr("output misses the contract")
	case models.TaskStatusBlocked:
		consumer.AssignedTo = nil
		consumer.BlockedReason = testhelpers.StringPtr("waiting for the provider replan")
		consumer.BlockedQuestions = []string{"Replan the provider?"}
	}
	return consumer
}

// setupDraftOutputTest is setupProviderOpsTest with the consumer's worktree
// directory and the agents its status names.
func setupDraftOutputTest(t *testing.T, tasks ...models.Task) (string, string, *db.Blackboard) {
	t.Helper()
	root, statePath, bb := setupProviderOpsTest(t, tasks...)
	if err := os.MkdirAll(filepath.Join(root, ".worktrees", "consumer"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := bb.Modify(func(s *models.State) error {
		consumer := s.FindTask("consumer")
		switch consumer.Status {
		case models.TaskStatus("ARCHITECTING"):
			agent := s.Agents["architect-1"]
			agent.Status, agent.CurrentTask = models.AgentStatusWorking, &consumer.ID
			s.Agents["architect-1"] = agent
		case models.TaskStatus("REVIEWING_ARCHITECTURE"):
			agent := testhelpers.RegisteredTestAgent("architecture-reviewer")
			agent.Status, agent.CurrentTask = models.AgentStatusReviewing, &consumer.ID
			s.Agents["architecture-reviewer-1"] = agent
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return root, statePath, bb
}

// draftOutputStatuses are the owner statuses from which output reaches MERGED
// only through a fresh approval, or through external reconciliation of an
// integration failure (ADR-0188).
var draftOutputStatuses = []models.TaskStatus{
	models.TaskStatus("DRAFT_ARCHITECTURE"),
	models.TaskStatus("ARCHITECTING"),
	models.TaskStatus("ARCHITECTURE_TO_REVIEW"),
	models.TaskStatus("REVIEWING_ARCHITECTURE"),
	models.TaskStatus("ARCHITECTURE_REJECTED"),
	models.TaskStatusBlocked,
	models.TaskStatusIntegrationFailed,
}

func TestStaleDeclarationInDraftOutputDoesNotBlockRetirement(t *testing.T) {
	for _, scenario := range staleRetirements() {
		for _, status := range draftOutputStatuses {
			t.Run(scenario.name+"/"+string(status), func(t *testing.T) {
				t.Parallel()
				// GIVEN a started consumer whose draft output declares the provider
				consumer := draftOutputConsumer(status)
				root, statePath, _ := setupDraftOutputTest(t, append(scenario.tasks(), consumer)...)

				// WHEN the provider is retired
				if err := scenario.retire(root); err != nil {
					t.Fatalf("draft output blocked the provider retirement: %v", err)
				}

				// THEN the retirement persisted
				state := readClaimStateForTest(t, statePath)
				if !scenario.retired(state.FindTask("provider")) {
					t.Fatalf("provider retirement not persisted: %+v", state.FindTask("provider"))
				}

				// AND the consumer keeps its identity, ownership and output
				after := state.FindTask(consumer.ID)
				if after.Status != status || !reflect.DeepEqual(after.Output, consumer.Output) ||
					!reflect.DeepEqual(after.AssignedTo, consumer.AssignedTo) || !reflect.DeepEqual(after.ReviewingBy, consumer.ReviewingBy) ||
					!reflect.DeepEqual(after.Worktree, consumer.Worktree) || !reflect.DeepEqual(after.BlockedReason, consumer.BlockedReason) {
					t.Fatalf("retirement changed the draft consumer: %+v", after)
				}

				// AND the stale declaration leaves the state valid
				resolver, _, err := loadResolver(root)
				if err != nil {
					t.Fatal(err)
				}
				if err := statevalidate.ValidateProviderDependencies(state, resolver); err != nil {
					t.Fatalf("stale draft output left the state invalid: %v", err)
				}
			})
		}
	}
}

// Fences: approved output reaches MERGED without a new verdict, and a child
// replan's lineage would resolve the slot silently; both keep holding.
func TestStaleDraftOutputExceptionKeepsApprovedAndChildReplanHolds(t *testing.T) {
	for _, tc := range []struct {
		name   string
		tasks  func() []models.Task
		retire func(root string) error
	}{
		{
			name: "approved output awaiting merge",
			tasks: func() []models.Task {
				provider := providerOpsTask("provider", "architecture-pair", models.TaskStatus("DRAFT_ARCHITECTURE"))
				return []models.Task{provider, draftOutputConsumer(models.TaskStatus("ARCHITECTURE_APPROVED"))}
			},
			retire: func(root string) error {
				_, err := CancelTask(root, "provider", "reviewed retirement", "orchestrator-1")
				return err
			},
		},
		{
			name: "draft output under a child replan",
			tasks: func() []models.Task {
				provider, child := transitionedLineagePlan("provider")
				child.Status = models.TaskStatusMerged
				child.Output = []models.OutputEntry{providerOpsOutput()}
				return []models.Task{provider, child, draftOutputConsumer(models.TaskStatusBlocked)}
			},
			retire: func(root string) error {
				_, err := Replan(root, &ReplanInput{TaskID: "provider-cp-0", ChangedBy: "human"})
				return err
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root, statePath, _ := setupDraftOutputTest(t, tc.tasks()...)
			before := replacementBytes(t, statePath)
			requireProviderOpsAtomicRefusal(t, statePath, before, tc.retire(root), "live provider_dependencies", "consumer output[0]")
		})
	}
}

func TestReplanWarnsAboutStaleDraftOutput(t *testing.T) {
	t.Parallel()
	root, _, _ := setupDraftOutputTest(t, mergedProviderPlan(), draftOutputConsumer(models.TaskStatus("ARCHITECTURE_TO_REVIEW")))
	result, err := Replan(root, &ReplanInput{TaskID: "provider", ChangedBy: "human"})
	if err != nil {
		t.Fatalf("draft output blocked provider replan: %v", err)
	}
	want := "task consumer output[0] declares replanned provider provider; re-author consumer's output before it is submitted or approved"
	if !slices.Contains(result.Warnings, want) {
		t.Fatalf("replan warnings %q do not name the stale draft (%q)", result.Warnings, want)
	}
}

// An already-assessed BLOCKED draft with no ordinary edge on the provider must
// still wake the orchestrator: nothing else would route it to re-authoring.
func TestStaleDraftOutputWakesAssessedBlockedConsumer(t *testing.T) {
	for _, scenario := range staleRetirements() {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			// GIVEN a BLOCKED draft consumer whose current assessment is recorded
			consumer := draftOutputConsumer(models.TaskStatusBlocked)
			root, statePath, bb := setupDraftOutputTest(t, append(scenario.tasks(), consumer)...)
			if err := bb.Modify(func(s *models.State) error {
				task := s.FindTask(consumer.ID)
				note := "waits for the provider"
				candidate := currentBlockerCandidate(s, task)
				candidate.Note = note
				task.History = append(task.History, models.TaskHistoryEntry{
					Time: time.Now().UTC(), Event: models.TaskEventOrchestratorAssessment, Note: &note,
					Extra: map[string]any{AssessmentFingerprintExtraKey: BuildAssessmentFingerprint(s, task, candidate)},
				})
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if ids := ActionableBlockedTaskIDs(readClaimStateForTest(t, statePath)); slices.Contains(ids, consumer.ID) {
				t.Fatalf("precondition: assessed consumer already actionable: %v", ids)
			}

			// WHEN the provider is retired
			if err := scenario.retire(root); err != nil {
				t.Fatalf("draft output blocked the provider retirement: %v", err)
			}

			// THEN the consumer stays BLOCKED with its owner fields and reason
			state := readClaimStateForTest(t, statePath)
			after := state.FindTask(consumer.ID)
			if after.Status != models.TaskStatusBlocked || !reflect.DeepEqual(after.AssignedTo, consumer.AssignedTo) || !reflect.DeepEqual(after.Worktree, consumer.Worktree) ||
				!reflect.DeepEqual(after.BlockedReason, consumer.BlockedReason) || !reflect.DeepEqual(after.Output, consumer.Output) {
				t.Fatalf("retirement disturbed the blocked consumer: %+v", after)
			}

			// AND it gains the re-authoring question and a durable record
			if len(after.BlockedQuestions) != 2 || after.BlockedQuestions[0] != consumer.BlockedQuestions[0] ||
				!strings.Contains(after.BlockedQuestions[1], "Re-author consumer output[0]") ||
				!strings.Contains(after.BlockedQuestions[1], "provider") || !strings.Contains(after.BlockedQuestions[1], "set-task-output") {
				t.Fatalf("blocked questions do not route re-authoring: %q", after.BlockedQuestions)
			}
			last := after.History[len(after.History)-1]
			if last.Event != "provider_declaration_stale" || last.Extra["provider_retirement"] != "provider" ||
				last.Reason == nil || !strings.Contains(*last.Reason, "output[0]") {
				t.Fatalf("stale declaration not recorded: %+v", last)
			}

			// AND the BLOCKED_TASKS wake names it
			if ids := ActionableBlockedTaskIDs(state); !slices.Contains(ids, consumer.ID) {
				t.Fatalf("stale blocked draft not actionable: %v", ids)
			}
		})
	}
}

func TestSetTaskOutputRefusesStaleProviderDeclaration(t *testing.T) {
	t.Parallel()
	consumer := draftOutputConsumer(models.TaskStatus("ARCHITECTING"))
	consumer.Output = nil
	provider := providerOpsTask("provider", "architecture-pair", models.TaskStatusAbandoned)
	root, statePath, _ := setupDraftOutputTest(t, provider, consumer)
	before := replacementBytes(t, statePath)
	output := providerOpsOutput()
	output.ProviderDependencies = providerOpsDependency("provider", 0)
	err := SetTaskOutput(root, &SetTaskOutputInput{TaskID: consumer.ID, AgentID: "architect-1", Output: []models.OutputEntry{output}})
	requireProviderOpsAtomicRefusal(t, statePath, before, err, "retired provider provider")
}

func TestSubmitForReviewRefusesStaleDraftOutput(t *testing.T) {
	t.Parallel()
	tmpDir, taskID, wtCommit, agentID, bb := setupSuccessfulSubmitScenario(t)
	g := git.New(tmpDir)
	headBefore, err := g.GetWorktreeHEAD(taskID)
	if err != nil {
		t.Fatal(err)
	}
	// GIVEN the task's output declares a provider retired since it was set
	if err := bb.Modify(func(s *models.State) error {
		provider := providerOpsTask("provider", "architecture-pair", models.TaskStatusAbandoned)
		s.Tasks = append(s.Tasks, provider)
		output := providerOpsOutput()
		output.ProviderDependencies = providerOpsDependency("provider", 0)
		s.FindTask(taskID).Output = []models.OutputEntry{output}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	statePath := paths.New(tmpDir).StatePath()
	before := replacementBytes(t, statePath)

	// WHEN the author submits it unchanged
	_, err = SubmitForReview(tmpDir, taskID, wtCommit, agentID)

	// THEN submission is refused naming the stale declaration, with no effects
	requireProviderOpsAtomicRefusal(t, statePath, before, err, staleDraftOutputBlocker, "set-task-output")
	if headAfter, err := g.GetWorktreeHEAD(taskID); err != nil || headAfter != headBefore {
		t.Fatalf("worktree HEAD moved before the refusal: %s -> %s (%v)", headBefore, headAfter, err)
	}
}

func TestSubmitVerdictRefusesApprovingStaleDraftOutput(t *testing.T) {
	t.Parallel()
	// GIVEN a reviewed task whose output declares a retired provider
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReviewing, time.Now().UTC())
	output := providerOpsOutput()
	output.ProviderDependencies = providerOpsDependency("provider", 0)
	task.Output = []models.OutputEntry{output}
	root, statePath, bb := setupProviderOpsTest(t, task, providerOpsTask("provider", "architecture-pair", models.TaskStatusAbandoned))
	if err := bb.Modify(func(s *models.State) error {
		s.Agents["code-reviewer-1"] = models.Agent{Role: "code-reviewer", Status: models.AgentStatusWorking}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	before := replacementBytes(t, statePath)

	// WHEN the reviewer approves it
	_, err := SubmitVerdict(root, "task-1", "APPROVED", "", "code-reviewer-1", "")

	// THEN approval is refused, telling the reviewer to reject, with no effects
	requireProviderOpsAtomicRefusal(t, statePath, before, err, staleDraftOutputBlocker, "REJECTED")

	// AND a rejection is accepted, so the author re-authors in place
	if _, err := SubmitVerdict(root, "task-1", "REJECTED", "output[0] declares a retired provider", "code-reviewer-1", ""); err != nil {
		t.Fatalf("rejecting stale draft output refused: %v", err)
	}
	if got := mustReadTask(t, statePath, "task-1"); got.Status == models.TaskStatusReviewing || len(got.Approvals) != 0 {
		t.Fatalf("rejection not recorded: status %s, approvals %+v", got.Status, got.Approvals)
	}
}

// Recovery boundaries: a draft retired under review can become
// INTEGRATION_FAILED without a provider check, and external reconciliation
// moves it to MERGED without a verdict. Both records stay valid; the merged
// one is ADR-0185's stale unexpanded plan, which generation refuses.
func TestStaleDraftOutputAcrossIntegrationRecovery(t *testing.T) {
	t.Parallel()
	// GIVEN a submitted draft left stale by the provider's replan
	root, statePath, bb := setupDraftOutputTest(t, mergedProviderPlan(), draftOutputConsumer(models.TaskStatus("ARCHITECTURE_TO_REVIEW")))
	if _, err := Replan(root, &ReplanInput{TaskID: "provider", ChangedBy: "human"}); err != nil {
		t.Fatalf("draft output blocked provider replan: %v", err)
	}
	resolver, _, err := loadResolver(root)
	if err != nil {
		t.Fatal(err)
	}

	// WHEN review-boundary recovery fails its integration
	if err := bb.Modify(func(s *models.State) error {
		return markReviewBoundaryIntegrationFailed(s, s.FindTask("consumer"), "architecture-reviewer-1", BuildPipelineTransitions(resolver), os.ErrInvalid)
	}); err != nil {
		t.Fatal(err)
	}

	// THEN the INTEGRATION_FAILED record is valid
	state := readClaimStateForTest(t, statePath)
	if got := state.FindTask("consumer").Status; got != models.TaskStatusIntegrationFailed {
		t.Fatalf("consumer status = %s, want INTEGRATION_FAILED", got)
	}
	if err := statevalidate.ValidateProviderDependencies(state, resolver); err != nil {
		t.Fatalf("stale INTEGRATION_FAILED output left the state invalid: %v", err)
	}

	// WHEN an operator reconciles an external merge
	if _, err := ReconcileMerged(root, "consumer", "HEAD", "", "merged externally", "orchestrator-1"); err != nil {
		t.Fatalf("reconciling the external merge refused: %v", err)
	}

	// THEN the MERGED record is valid, a visible reconcile blocker, and generates nothing
	state = readClaimStateForTest(t, statePath)
	if got := state.FindTask("consumer").Status; got != models.TaskStatusMerged {
		t.Fatalf("consumer status = %s, want MERGED", got)
	}
	if err := statevalidate.ValidateProviderDependencies(state, resolver); err != nil {
		t.Fatalf("reconciled stale output left the state invalid: %v", err)
	}
	if class, blocker := classifyForTest(t, root, statePath, "consumer"); class != PlanHandoffNeedsReview || !strings.Contains(blocker, staleProviderBlocker) {
		t.Fatalf("Classify = (%q, %q), want (%q, containing %q)", class, blocker, PlanHandoffNeedsReview, staleProviderBlocker)
	}
	if _, err := ExecuteTransitionsReportWith(root, "manual", AdmitOperator); err != nil {
		t.Fatal(err)
	}
	if child := readClaimStateForTest(t, statePath).FindTask("consumer-cp-0"); child != nil {
		t.Fatalf("generation consumed a stale reconciled declaration: %+v", child)
	}
}
