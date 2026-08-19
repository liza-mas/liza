package ops

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	gitpkg "github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestEffectiveIntegrationCompletionGate(t *testing.T) {
	t.Run("stale clean evidence rejects every completion-capable path before progression mutation", func(t *testing.T) {
		for _, path := range effectiveCompletionPaths() {
			t.Run(path.name, func(t *testing.T) {
				fixture := newEffectiveCompletionFixture(t, false)
				path.prepare(t, fixture)

				err := path.invoke(fixture.projectRoot)
				requireEffectiveCompletionPrecondition(t, err)
				path.assertRejected(t, fixture)
			})
		}
	})

	t.Run("pending replacement generation rejects every completion-capable path", func(t *testing.T) {
		for _, path := range effectiveCompletionPaths() {
			t.Run(path.name, func(t *testing.T) {
				fixture := newEffectiveCompletionFixture(t, true)
				fixture.installPendingReplacement(t)
				path.prepare(t, fixture)

				err := path.invoke(fixture.projectRoot)
				requireEffectiveCompletionPrecondition(t, err)
				path.assertRejected(t, fixture)
			})
		}
	})

	t.Run("current clean evidence permits every completion-capable path", func(t *testing.T) {
		for _, path := range effectiveCompletionPaths() {
			t.Run(path.name, func(t *testing.T) {
				fixture := newEffectiveCompletionFixture(t, true)
				path.prepare(t, fixture)

				if err := path.invoke(fixture.projectRoot); err != nil {
					t.Fatalf("operation error = %v", err)
				}
				path.assertAllowed(t, fixture)
			})
		}
	})

	t.Run("settled empty cohort cannot bypass global closure", func(t *testing.T) {
		fixture := newEffectiveCompletionFixture(t, true)
		fixture.mutateState(t, func(state *models.State) {
			state.Goal.Integration = &models.IntegrationLifecycle{
				ContributingSet: &models.IntegrationContributingSet{Scopes: []models.IntegrationScopeSnapshot{}},
			}
			state.Sprint.Status = models.SprintStatusCheckpoint
		})

		err := invokeAdvanceSprint(fixture.projectRoot)
		requireEffectiveCompletionPrecondition(t, err)
		if state := fixture.readState(t); state.Sprint.Status != models.SprintStatusCheckpoint || len(state.SprintHistory) != 0 {
			t.Fatalf("empty cohort advanced sprint: status=%s history=%v", state.Sprint.Status, state.SprintHistory)
		}
	})

	t.Run("nil cohort preserves pre-integration handoffs but rejects explicit completion", func(t *testing.T) {
		t.Run("explicit sprint complete checkpoint rejects", func(t *testing.T) {
			fixture := newPreIntegrationCompletionFixture(t)
			_, err := SprintCheckpoint(fixture.projectRoot, models.CheckpointTriggerSprintComplete)
			requireEffectiveCompletionPrecondition(t, err)
			if state := fixture.readState(t); state.Sprint.Status != models.SprintStatusInProgress {
				t.Fatalf("explicit completion changed sprint status to %s", state.Sprint.Status)
			}
		})

		t.Run("ordinary checkpoint resume can complete the phase", func(t *testing.T) {
			fixture := newPreIntegrationCompletionFixture(t)
			fixture.mutateState(t, func(state *models.State) { state.Sprint.Status = models.SprintStatusCheckpoint })
			if _, err := Resume(fixture.projectRoot, "tester"); err != nil {
				t.Fatalf("Resume() error = %v", err)
			}
			if state := fixture.readState(t); state.Sprint.Status != models.SprintStatusCompleted || (state.Goal.Integration != nil && state.Goal.Integration.ContributingSet != nil) {
				t.Fatalf("pre-integration resume state = %#v integration=%#v", state.Sprint, state.Goal.Integration)
			}
		})

		t.Run("completed sprint resume carries unconsumed planning output", func(t *testing.T) {
			fixture := newPreIntegrationCompletionFixture(t)
			fixture.mutateState(t, func(state *models.State) { state.Sprint.Status = models.SprintStatusCompleted })
			if _, err := Resume(fixture.projectRoot, "tester"); err != nil {
				t.Fatalf("Resume() error = %v", err)
			}
			if state := fixture.readState(t); state.Sprint.Number != 2 || state.FindTask("plan-single-code-0") == nil {
				t.Fatalf("pre-integration completed resume did not hand off: sprint=%#v", state.Sprint)
			}
		})

		t.Run("direct advance carries unconsumed planning output", func(t *testing.T) {
			fixture := newPreIntegrationCompletionFixture(t)
			fixture.mutateState(t, func(state *models.State) { state.Sprint.Status = models.SprintStatusCheckpoint })
			if _, err := AdvanceSprint(fixture.projectRoot); err != nil {
				t.Fatalf("AdvanceSprint() error = %v", err)
			}
			if state := fixture.readState(t); state.Sprint.Number != 2 || !slices.Contains(state.Sprint.Scope.Planned, fixture.planID) {
				t.Fatalf("pre-integration advance did not carry plan: %#v", state.Sprint)
			}
		})

		t.Run("manual proceed creates downstream work", func(t *testing.T) {
			fixture := newPreIntegrationCompletionFixture(t)
			fixture.mutateState(t, func(state *models.State) { state.Sprint.Status = models.SprintStatusCompleted })
			if _, err := Proceed(fixture.projectRoot, fixture.planID, "code-plan-to-coding"); err != nil {
				t.Fatalf("Proceed() error = %v", err)
			}
			if state := fixture.readState(t); state.FindTask("plan-single-code-0") == nil {
				t.Fatal("pre-integration proceed did not create child")
			}
		})

		t.Run("transition checkpoint resumes without finality", func(t *testing.T) {
			fixture := newPreIntegrationCompletionFixture(t)
			fixture.mutateState(t, func(state *models.State) {
				state.Sprint.Status = models.SprintStatusCheckpoint
				state.Sprint.CheckpointTrigger = models.CheckpointTriggerPlanningComplete
			})
			if _, err := Resume(fixture.projectRoot, "tester"); err != nil {
				t.Fatalf("Resume() error = %v", err)
			}
			if state := fixture.readState(t); state.Sprint.Status != models.SprintStatusInProgress {
				t.Fatalf("transition checkpoint resumed to %s", state.Sprint.Status)
			}
		})
	})

	t.Run("public integration mutation immediately invalidates resume and advance", func(t *testing.T) {
		for _, path := range []effectiveCompletionPath{effectiveCompletionPaths()[1], effectiveCompletionPaths()[3]} {
			t.Run(path.name, func(t *testing.T) {
				fixture := newEffectiveCompletionFixture(t, true)
				taskID, agentID := fixture.installPublicIntegrationMutation(t)
				path.prepare(t, fixture)
				if _, err := MergeWorktree(fixture.projectRoot, taskID, agentID); err != nil {
					t.Fatalf("MergeWorktree() error = %v", err)
				}

				err := path.invoke(fixture.projectRoot)
				requireEffectiveCompletionPrecondition(t, err)
				path.assertRejected(t, fixture)
				state := fixture.readState(t)
				liveHead := mustCommit(t, gitpkg.New(fixture.projectRoot), "refs/heads/integration")
				next := state.FindTask("integration-global-2")
				if next == nil || next.IntegrationAnalysis.SourceCommit != liveHead {
					t.Fatalf("replacement analysis = %#v, want live HEAD %s", next, liveHead)
				}
				receipts := state.Goal.Integration.MutationReceipts
				if len(receipts) < 2 || receipts[len(receipts)-1].AfterCommit != liveHead {
					t.Fatalf("mutation receipts = %#v, want final HEAD %s", receipts, liveHead)
				}
			})
		}
	})

	t.Run("paused integration mutation receipt blocks stale resume and advance", func(t *testing.T) {
		for _, path := range []effectiveCompletionPath{effectiveCompletionPaths()[1], effectiveCompletionPaths()[3]} {
			t.Run(path.name, func(t *testing.T) {
				fixture := newEffectiveCompletionFixture(t, true)
				path.prepare(t, fixture)
				taskID, agentID := fixture.installPublicIntegrationMutation(t)
				receiptPersisting := make(chan struct{})
				releaseReceipt := make(chan struct{})
				previousReceiptHook := integrationMutationReceiptPersistTestHook
				integrationMutationReceiptPersistTestHook = func(models.IntegrationMutationReceipt) {
					close(receiptPersisting)
					<-releaseReceipt
				}
				defer func() { integrationMutationReceiptPersistTestHook = previousReceiptHook }()

				mergeDone := make(chan error, 1)
				go func() {
					_, err := MergeWorktree(fixture.projectRoot, taskID, agentID)
					mergeDone <- err
				}()
				select {
				case <-receiptPersisting:
				case <-time.After(2 * time.Second):
					close(releaseReceipt)
					t.Fatal("timed out waiting for integration ref mutation")
				}

				progressionAttempted := make(chan struct{})
				previousLinearizationHook := beforeEffectiveIntegrationCompletionLinearizationTestHook
				beforeEffectiveIntegrationCompletionLinearizationTestHook = func(operation string) {
					if strings.HasPrefix(operation, "progression ") {
						close(progressionAttempted)
					}
				}
				defer func() { beforeEffectiveIntegrationCompletionLinearizationTestHook = previousLinearizationHook }()
				progressionDone := make(chan error, 1)
				go func() { progressionDone <- path.invoke(fixture.projectRoot) }()
				select {
				case <-progressionAttempted:
				case <-time.After(2 * time.Second):
					close(releaseReceipt)
					t.Fatal("timed out waiting for stale progression attempt")
				}
				select {
				case err := <-progressionDone:
					close(releaseReceipt)
					t.Fatalf("progression returned before mutation receipt persisted: %v", err)
				default:
				}
				path.assertRejected(t, fixture)

				close(releaseReceipt)
				select {
				case err := <-mergeDone:
					if err != nil {
						t.Fatalf("MergeWorktree() error = %v", err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("timed out waiting for integration mutation")
				}
				select {
				case err := <-progressionDone:
					requireEffectiveCompletionPrecondition(t, err)
				case <-time.After(5 * time.Second):
					t.Fatal("timed out waiting for stale progression rejection")
				}
				path.assertRejected(t, fixture)
			})
		}
	})

	t.Run("authorized progression serializes a later public integration mutation", func(t *testing.T) {
		for _, path := range []effectiveCompletionPath{effectiveCompletionPaths()[1], effectiveCompletionPaths()[3]} {
			t.Run(path.name, func(t *testing.T) {
				fixture := newEffectiveCompletionFixture(t, true)
				path.prepare(t, fixture)
				var taskID, agentID string

				progressionAuthorized := make(chan struct{})
				releaseProgression := make(chan struct{})
				previousHook := beforeEffectiveIntegrationProgressionMutationTestHook
				beforeEffectiveIntegrationProgressionMutationTestHook = func() {
					taskID, agentID = fixture.installPublicIntegrationMutation(t)
					close(progressionAuthorized)
					<-releaseProgression
				}
				defer func() { beforeEffectiveIntegrationProgressionMutationTestHook = previousHook }()

				progressionDone := make(chan error, 1)
				go func() { progressionDone <- path.invoke(fixture.projectRoot) }()
				select {
				case <-progressionAuthorized:
				case <-time.After(2 * time.Second):
					t.Fatal("timed out waiting for progression authorization")
				}

				mutationAttempted := make(chan struct{})
				previousLinearizationHook := beforeEffectiveIntegrationCompletionLinearizationTestHook
				beforeEffectiveIntegrationCompletionLinearizationTestHook = func(operation string) {
					if strings.HasPrefix(operation, "forward ") {
						close(mutationAttempted)
					}
				}
				defer func() { beforeEffectiveIntegrationCompletionLinearizationTestHook = previousLinearizationHook }()
				receiptPersisting := make(chan struct{})
				releaseReceipt := make(chan struct{})
				previousReceiptHook := integrationMutationReceiptPersistTestHook
				integrationMutationReceiptPersistTestHook = func(models.IntegrationMutationReceipt) {
					close(receiptPersisting)
					<-releaseReceipt
				}
				defer func() { integrationMutationReceiptPersistTestHook = previousReceiptHook }()
				mergeDone := make(chan error, 1)
				go func() {
					_, err := MergeWorktree(fixture.projectRoot, taskID, agentID)
					mergeDone <- err
				}()
				select {
				case <-mutationAttempted:
				case <-time.After(2 * time.Second):
					close(releaseProgression)
					t.Fatal("timed out waiting for integration mutation linearization attempt")
				}
				select {
				case <-receiptPersisting:
					close(releaseProgression)
					close(releaseReceipt)
					t.Fatal("integration ref advanced while authorized progression was pending")
				default:
				}

				close(releaseProgression)
				select {
				case err := <-progressionDone:
					if err != nil {
						t.Fatalf("progression error = %v", err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("timed out waiting for authorized progression")
				}
				path.assertAllowed(t, fixture)
				select {
				case <-receiptPersisting:
				case <-time.After(2 * time.Second):
					t.Fatal("timed out waiting for integration ref mutation")
				}
				close(releaseReceipt)
				select {
				case err := <-mergeDone:
					if err != nil {
						t.Fatalf("MergeWorktree() error = %v", err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("timed out waiting for integration mutation")
				}
				snapshot, err := readEffectiveIntegrationCompletionSnapshot(fixture.projectRoot)
				if err != nil {
					t.Fatalf("readEffectiveIntegrationCompletionSnapshot() error = %v", err)
				}
				if snapshot.decision.IntegrationComplete {
					t.Fatal("later integration mutation left prior completion effective")
				}
			})
		}
	})

	t.Run("sprint complete state race leaves existing summary unchanged", func(t *testing.T) {
		fixture := newEffectiveCompletionFixture(t, true)
		path := effectiveCompletionPaths()[0]
		path.prepare(t, fixture)
		reportPath := filepath.Join(fixture.projectRoot, ".liza", "sprint_summary.md")
		const originalSummary = "existing summary\n"
		if err := os.WriteFile(reportPath, []byte(originalSummary), 0o644); err != nil {
			t.Fatalf("write existing summary: %v", err)
		}
		previousHook := beforeEffectiveIntegrationProgressionMutationTestHook
		beforeEffectiveIntegrationProgressionMutationTestHook = func() {
			fixture.mutateState(t, func(state *models.State) {
				state.Goal.Integration.Closure = nil
			})
		}
		defer func() { beforeEffectiveIntegrationProgressionMutationTestHook = previousHook }()

		err := path.invoke(fixture.projectRoot)
		requireEffectiveCompletionPrecondition(t, err)
		got, readErr := os.ReadFile(reportPath)
		if readErr != nil {
			t.Fatalf("read existing summary: %v", readErr)
		}
		if string(got) != originalSummary {
			t.Fatalf("summary = %q, want unchanged %q", got, originalSummary)
		}
	})
}

type effectiveCompletionFixture struct {
	projectRoot string
	stateFile   string
	planID      string
	terminalID  string
}

func newEffectiveCompletionFixture(t *testing.T, currentClean bool) *effectiveCompletionFixture {
	t.Helper()
	reconcile := newReconcileFixture(t, false)
	fixture := &effectiveCompletionFixture{
		projectRoot: reconcile.projectRoot,
		stateFile:   reconcile.statePath,
		planID:      "plan-single",
		terminalID:  "integration-global-1",
	}
	reconcile.mutateState(t, func(state *models.State) {
		state.Goal.SpecRef = "README.md"
		state.Tasks = []models.Task{*state.FindTask("plan-single"), *state.FindTask("coding-single")}
		state.Sprint.Scope.Planned = []string{"plan-single", "coding-single"}
	})
	if _, err := ReconcileIntegrationAnalyses(fixture.projectRoot); err != nil {
		t.Fatalf("create global analysis: %v", err)
	}
	fixture.mutateState(t, func(state *models.State) {
		analysis := state.FindTask("integration-global-1")
		analysis.Status = models.TaskStatus("INTEGRATION_ANALYSIS_CLEAN")
		analysis.ReviewCommit = progressString("global-report")
		state.Goal.Integration.GlobalGenerations = []models.IntegrationGlobalGeneration{{
			Generation: 1, AnalysisTaskID: analysis.ID, AnalysisKey: analysis.IntegrationAnalysis.Key,
			Verdict: models.IntegrationAnalysisVerdictClean, SourceCommit: analysis.IntegrationAnalysis.SourceCommit, ReportCommit: "global-report",
		}}
		state.Goal.Integration.Closure = &models.IntegrationClosure{
			Status: models.IntegrationClosureStatusClean, Generation: 1,
			AnalysisKey: analysis.IntegrationAnalysis.Key, SourceCommit: analysis.IntegrationAnalysis.SourceCommit,
		}
		state.Goal.Integration.MutationReceipts = []models.IntegrationMutationReceipt{{
			TaskID: "earlier-mutation", BeforeCommit: "earlier-before", AfterCommit: "earlier-after",
		}}
	})
	if !currentClean {
		writeFixtureFile(t, fixture.projectRoot, "stale-marker.txt", "new integration head\n")
		testhelpers.MustGit(t, fixture.projectRoot, "add", "stale-marker.txt")
		testhelpers.MustGit(t, fixture.projectRoot, "commit", "-m", "advance integration head")
		newHead := mustCommit(t, gitpkg.New(fixture.projectRoot), "HEAD")
		testhelpers.MustGit(t, fixture.projectRoot, "update-ref", "refs/heads/integration", newHead)
	}
	return fixture
}

func (fixture *effectiveCompletionFixture) installPublicIntegrationMutation(t *testing.T) (string, string) {
	t.Helper()
	const taskID = "post-clean-mutation"
	const agentID = "coder-1"
	worktreePath := filepath.Join(fixture.projectRoot, ".worktrees", taskID)
	if err := os.MkdirAll(filepath.Dir(worktreePath), 0o755); err != nil {
		t.Fatalf("create worktree parent: %v", err)
	}
	testhelpers.MustGit(t, fixture.projectRoot, "worktree", "add", "-b", "task/"+taskID, worktreePath, "integration")
	writeFixtureFile(t, worktreePath, "post-clean.txt", "mutation after clean integration\n")
	testhelpers.MustGit(t, worktreePath, "add", "post-clean.txt")
	testhelpers.MustGit(t, worktreePath, "commit", "-m", "mutate integration after clean analysis")
	baseCommit := mustCommit(t, gitpkg.New(fixture.projectRoot), "refs/heads/integration")
	reviewCommit := mustCommit(t, gitpkg.New(worktreePath), "HEAD")
	relativeWorktree := filepath.Join(".worktrees", taskID)
	approvedBy := "code-reviewer-1"
	fixture.mutateState(t, func(state *models.State) {
		now := time.Now().UTC()
		state.Tasks = append(state.Tasks, models.Task{
			ID: taskID, Type: models.TaskTypeCoding, RolePair: "coding-pair",
			Description: "Post-clean integration mutation", Status: models.TaskStatusApproved, Priority: 1,
			Created: now, SpecRef: "README.md", DoneWhen: "mutation merged", Scope: "post-clean.txt",
			ParentTask: progressString(fixture.terminalID), Worktree: &relativeWorktree, AssignedTo: progressString(agentID),
			BaseCommit: &baseCommit, ReviewCommit: &reviewCommit, ApprovedBy: &approvedBy,
			History: []models.TaskHistoryEntry{}, HandoffEvents: []models.HandoffEvent{{
				Timestamp: now, Agent: agentID, Trigger: models.HandoffTriggerSubmission,
			}},
		})
	})
	return taskID, agentID
}

func newPreIntegrationCompletionFixture(t *testing.T) *effectiveCompletionFixture {
	t.Helper()
	projectRoot := t.TempDir()
	testhelpers.SetupTestGitRepo(t, projectRoot)
	stateFile, _ := testhelpers.SetupLizaDir(t, projectRoot)
	testhelpers.SetupPipelineConfig(t, projectRoot)
	testhelpers.CreateSpecFile(t, projectRoot, "vision.md", "# Pre-integration handoff\n")
	head := mustCommit(t, gitpkg.New(projectRoot), "HEAD")
	testhelpers.MustGit(t, projectRoot, "update-ref", "refs/heads/integration", head)
	plan := reconcileMergedPlan(time.Now().UTC(), "plan-single")
	plan.TransitionsExecuted = nil
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{plan}
	state.Sprint.Scope.Planned = []string{plan.ID}
	testhelpers.WriteInitialState(t, stateFile, state)
	return &effectiveCompletionFixture{
		projectRoot: projectRoot,
		stateFile:   stateFile,
		planID:      plan.ID,
		terminalID:  plan.ID,
	}
}

func (fixture *effectiveCompletionFixture) installPendingReplacement(t *testing.T) {
	t.Helper()
	fixture.mutateState(t, func(state *models.State) {
		state.Goal.Integration.GlobalGenerations[0].Verdict = models.IntegrationAnalysisVerdictFindings
		state.Goal.Integration.Closure = nil
		now := time.Now().UTC()
		repair := progressChild(testhelpers.BuildTaskByStatus("pending-repair", models.TaskStatusSuperseded, now), fixture.terminalID)
		repair.RolePair = "coding-pair"
		repair.SupersededBy = []string{"pending-replacement"}
		repair.RescopeReason = progressString("replacement required")
		replacement := testhelpers.BuildTaskByStatus("pending-replacement", models.TaskStatusReady, now)
		replacement.Supersedes = progressString(repair.ID)
		state.Tasks = append(state.Tasks, repair, replacement)
	})
}

func (fixture *effectiveCompletionFixture) readState(t *testing.T) *models.State {
	t.Helper()
	state, err := db.New(fixture.stateFile).Read()
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	return state
}

func (fixture *effectiveCompletionFixture) mutateState(t *testing.T, mutate func(*models.State)) {
	t.Helper()
	state := fixture.readState(t)
	mutate(state)
	testhelpers.WriteInitialState(t, fixture.stateFile, state)
}

type effectiveCompletionPath struct {
	name           string
	prepare        func(*testing.T, *effectiveCompletionFixture)
	invoke         func(string) error
	assertRejected func(*testing.T, *effectiveCompletionFixture)
	assertAllowed  func(*testing.T, *effectiveCompletionFixture)
}

func effectiveCompletionPaths() []effectiveCompletionPath {
	return []effectiveCompletionPath{
		{
			name: "sprint complete checkpoint",
			prepare: func(t *testing.T, fixture *effectiveCompletionFixture) {
				fixture.mutateState(t, func(state *models.State) {
					state.Sprint.Status = models.SprintStatusInProgress
					state.Sprint.Timeline.CheckpointAt = nil
					state.Sprint.CheckpointTrigger = ""
				})
			},
			invoke: func(root string) error {
				_, err := SprintCheckpoint(root, models.CheckpointTriggerSprintComplete)
				return err
			},
			assertRejected: func(t *testing.T, fixture *effectiveCompletionFixture) {
				state := fixture.readState(t)
				if state.Sprint.Status != models.SprintStatusInProgress || state.Sprint.Timeline.CheckpointAt != nil || state.Sprint.CheckpointTrigger != "" {
					t.Fatalf("checkpoint progression persisted: %#v", state.Sprint)
				}
				if _, err := os.Stat(filepath.Join(fixture.projectRoot, ".liza", "sprint_summary.md")); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("sprint summary exists after rejection: %v", err)
				}
			},
			assertAllowed: func(t *testing.T, fixture *effectiveCompletionFixture) {
				if state := fixture.readState(t); state.Sprint.Status != models.SprintStatusCheckpoint || state.Sprint.CheckpointTrigger != models.CheckpointTriggerSprintComplete {
					t.Fatalf("checkpoint was not applied: %#v", state.Sprint)
				}
			},
		},
		{
			name: "checkpoint resume to completed",
			prepare: func(t *testing.T, fixture *effectiveCompletionFixture) {
				fixture.mutateState(t, func(state *models.State) {
					state.Sprint.Status = models.SprintStatusCheckpoint
					state.Sprint.CheckpointTrigger = ""
					state.Sprint.Scope.Planned = []string{fixture.terminalID}
				})
			},
			invoke: func(root string) error {
				_, err := Resume(root, "tester")
				return err
			},
			assertRejected: func(t *testing.T, fixture *effectiveCompletionFixture) {
				if state := fixture.readState(t); state.Sprint.Status != models.SprintStatusCheckpoint {
					t.Fatalf("resume completed stale sprint: %s", state.Sprint.Status)
				}
			},
			assertAllowed: func(t *testing.T, fixture *effectiveCompletionFixture) {
				if state := fixture.readState(t); state.Sprint.Status != models.SprintStatusCompleted {
					t.Fatalf("resume did not complete sprint: %s", state.Sprint.Status)
				}
			},
		},
		{
			name: "completed sprint resume archive",
			prepare: func(t *testing.T, fixture *effectiveCompletionFixture) {
				fixture.mutateState(t, func(state *models.State) {
					state.Sprint.Status = models.SprintStatusCompleted
					state.Sprint.Scope.Planned = []string{fixture.terminalID}
				})
			},
			invoke: func(root string) error {
				_, err := Resume(root, "tester")
				return err
			},
			assertRejected: assertEffectiveCompletionArchiveRejected,
			assertAllowed: func(t *testing.T, fixture *effectiveCompletionFixture) {
				if state := fixture.readState(t); state.Sprint.Number != 2 || len(state.SprintHistory) != 1 {
					t.Fatalf("completed sprint was not advanced: sprint=%#v history=%v", state.Sprint, state.SprintHistory)
				}
			},
		},
		{
			name: "direct advance",
			prepare: func(t *testing.T, fixture *effectiveCompletionFixture) {
				fixture.mutateState(t, func(state *models.State) {
					state.Sprint.Status = models.SprintStatusCheckpoint
					state.Sprint.Scope.Planned = []string{fixture.terminalID}
				})
			},
			invoke:         invokeAdvanceSprint,
			assertRejected: assertEffectiveCompletionArchiveRejected,
			assertAllowed: func(t *testing.T, fixture *effectiveCompletionFixture) {
				if state := fixture.readState(t); state.Sprint.Number != 2 || len(state.SprintHistory) != 1 {
					t.Fatalf("sprint was not advanced: sprint=%#v history=%v", state.Sprint, state.SprintHistory)
				}
			},
		},
		{
			name: "manual proceed",
			prepare: func(t *testing.T, fixture *effectiveCompletionFixture) {
				fixture.mutateState(t, func(state *models.State) {
					state.Sprint.Status = models.SprintStatusCompleted
					plan := state.FindTask(fixture.planID)
					plan.TransitionsExecuted = nil
				})
			},
			invoke: func(root string) error {
				_, err := Proceed(root, "plan-single", "code-plan-to-coding")
				return err
			},
			assertRejected: func(t *testing.T, fixture *effectiveCompletionFixture) {
				state := fixture.readState(t)
				plan := state.FindTask(fixture.planID)
				if plan.TransitionsExecuted["code-plan-to-coding"] || state.FindTask("plan-single-code-0") != nil {
					t.Fatalf("proceed mutation persisted: plan=%#v", plan)
				}
			},
			assertAllowed: func(t *testing.T, fixture *effectiveCompletionFixture) {
				state := fixture.readState(t)
				if !state.FindTask(fixture.planID).TransitionsExecuted["code-plan-to-coding"] || state.FindTask("plan-single-code-0") == nil {
					t.Fatal("proceed did not create its child")
				}
			},
		},
	}
}

func invokeAdvanceSprint(root string) error {
	_, err := AdvanceSprint(root)
	return err
}

func requireEffectiveCompletionPrecondition(t *testing.T, err error) {
	t.Helper()
	var precondition *PreconditionError
	if !errors.As(err, &precondition) || !strings.Contains(precondition.Reason, "integration") {
		t.Fatalf("error = %v, want integration PreconditionError", err)
	}
}

func assertEffectiveCompletionArchiveRejected(t *testing.T, fixture *effectiveCompletionFixture) {
	t.Helper()
	state := fixture.readState(t)
	if state.Sprint.Number != 1 || len(state.SprintHistory) != 0 {
		t.Fatalf("archive progression persisted: sprint=%#v history=%v", state.Sprint, state.SprintHistory)
	}
	archivePath := filepath.Join(fixture.projectRoot, ".liza", "archive", "sprint-1.yaml")
	if _, err := os.Stat(archivePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("archive exists after rejection: %v", err)
	}
}

// setupPipelineTest creates a test directory with a frozen pipeline config and a valid state.
// Returns (projectRoot, stateFile) paths.
func setupPipelineTest(t *testing.T) (string, string) {
	t.Helper()
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	// Copy the valid pipeline YAML to .liza/pipeline.yaml (frozen config).
	src, err := os.ReadFile(filepath.Join(testhelpers.FindRepoRoot(t), "internal", "pipeline", "testdata", "valid-coding-subpipeline.yaml"))
	if err != nil {
		t.Fatalf("Failed to read pipeline testdata: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, ".liza", "pipeline.yaml"), src, 0644); err != nil {
		t.Fatalf("Failed to write frozen pipeline config: %v", err)
	}

	return tmpDir, stateFile
}

func TestLoadDetectionContext_PipelineGoal(t *testing.T) {
	tmpDir, _ := setupPipelineTest(t)
	ctx, err := LoadDetectionContext(tmpDir)
	if err != nil {
		t.Fatalf("LoadDetectionContext() error: %v", err)
	}
	if len(ctx.SprintTerminals) == 0 {
		t.Error("expected non-empty SprintTerminals")
	}
	if len(ctx.PlanningPairs) == 0 {
		t.Error("expected non-empty PlanningPairs")
	}
}

func TestLoadDetectionContext_NoPipeline(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	testhelpers.SetupLizaDir(t, tmpDir)
	os.Remove(filepath.Join(tmpDir, ".liza", "pipeline.yaml"))
	_, err := LoadDetectionContext(tmpDir)
	if err == nil {
		t.Fatal("expected error for missing pipeline config")
	}
}

func TestLoadPhaseHandoffDetectionContext_NoPipelineUsesLegacyPlanning(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	testhelpers.SetupLizaDir(t, tmpDir)
	if err := os.Remove(filepath.Join(tmpDir, ".liza", "pipeline.yaml")); err != nil && !os.IsNotExist(err) {
		t.Fatalf("remove pipeline config: %v", err)
	}

	ctx, err := LoadPhaseHandoffDetectionContext(tmpDir)
	if err != nil {
		t.Fatalf("LoadPhaseHandoffDetectionContext() error: %v", err)
	}
	if !IsPlanningPair("code-planning-pair", ctx.PlanningPairs) {
		t.Fatal("legacy code-planning-pair is not recognized")
	}
	if got := ctx.PlanningApprovedStatuses["code-planning-pair"]; got != models.TaskStatusCodingPlanApproved {
		t.Fatalf("legacy approved status = %q, want %q", got, models.TaskStatusCodingPlanApproved)
	}
}

func TestLoadPhaseHandoffDetectionContext_MalformedPipelineFailsClosed(t *testing.T) {
	tmpDir, _ := setupPipelineTest(t)
	pipelinePath := filepath.Join(tmpDir, ".liza", "pipeline.yaml")
	if err := os.WriteFile(pipelinePath, []byte("pipeline: [not-valid"), 0644); err != nil {
		t.Fatalf("write malformed pipeline config: %v", err)
	}

	ctx, err := LoadPhaseHandoffDetectionContext(tmpDir)
	if err == nil {
		t.Fatal("LoadPhaseHandoffDetectionContext() error = nil, want malformed config error")
	}
	if ctx != nil {
		t.Fatalf("LoadPhaseHandoffDetectionContext() context = %+v, want nil on malformed config", ctx)
	}
}

func TestLoadResolver_PipelineGoal(t *testing.T) {
	tmpDir, _ := setupPipelineTest(t)

	resolver, _, err := loadResolver(tmpDir)
	if err != nil {
		t.Fatalf("loadResolver() error: %v", err)
	}
	if resolver == nil {
		t.Fatal("expected non-nil resolver for pipeline goal")
	}
}

func TestBuildPipelineTransitions_BlockedCanReturnToExecutingStatuses(t *testing.T) {
	tmpDir, _ := setupPipelineTest(t)

	resolver, _, err := loadResolver(tmpDir)
	if err != nil {
		t.Fatalf("loadResolver() error: %v", err)
	}

	transitions := BuildPipelineTransitions(resolver)
	got := transitions[models.TaskStatusBlocked]
	for _, want := range []models.TaskStatus{
		models.TaskStatusCodePlanning,
		models.TaskStatusImplementing,
	} {
		if !testContainsStatus(got, want) {
			t.Fatalf("BLOCKED transitions = %v, want %s", got, want)
		}
	}
}

func TestBuildPipelineTransitions_AllowsOperatorCancelBeforeApproval(t *testing.T) {
	tmpDir, _ := setupPipelineTest(t)

	resolver, _, err := loadResolver(tmpDir)
	if err != nil {
		t.Fatalf("loadResolver() error: %v", err)
	}

	transitions := BuildPipelineTransitions(resolver)
	for _, tt := range []struct {
		from models.TaskStatus
	}{
		{from: models.TaskStatusImplementing},
		{from: models.TaskStatusReadyForReview},
		{from: models.TaskStatusReviewing},
		{from: models.TaskStatusReviewingCode2},
	} {
		t.Run(string(tt.from), func(t *testing.T) {
			if !testContainsStatus(transitions[tt.from], models.TaskStatusAbandoned) {
				t.Fatalf("%s transitions = %v, want ABANDONED", tt.from, transitions[tt.from])
			}
		})
	}
}

func TestBuildPipelineTransitions_DoesNotCancelApprovedState(t *testing.T) {
	tmpDir, _ := setupPipelineTest(t)

	resolver, _, err := loadResolver(tmpDir)
	if err != nil {
		t.Fatalf("loadResolver() error: %v", err)
	}

	transitions := BuildPipelineTransitions(resolver)
	if testContainsStatus(transitions[models.TaskStatusApproved], models.TaskStatusAbandoned) {
		t.Fatalf("CODE_APPROVED transitions = %v, want no ABANDONED edge", transitions[models.TaskStatusApproved])
	}
}

func testContainsStatus(values []models.TaskStatus, want models.TaskStatus) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestTransitionSourcePairs_PipelineGoal(t *testing.T) {
	tmpDir, _ := setupPipelineTest(t)

	pairs, err := TransitionSourcePairs(tmpDir)
	if err != nil {
		t.Fatalf("TransitionSourcePairs() error: %v", err)
	}
	// valid-coding-subpipeline.yaml has code-planning-pair as a transition source
	if !pairs["code-planning-pair"] {
		t.Error("expected code-planning-pair to be a transition source")
	}
	// coding-pair is not a transition source (it's the terminal pair)
	if pairs["coding-pair"] {
		t.Error("coding-pair should not be a transition source")
	}
}

func TestIsPlanningPair(t *testing.T) {
	pairs := map[string]bool{"code-planning-pair": true, "epic-planning-pair": true}

	// With explicit pairs map
	if !IsPlanningPair("code-planning-pair", pairs) {
		t.Error("IsPlanningPair(code-planning-pair, pairs) = false, want true")
	}
	if !IsPlanningPair("epic-planning-pair", pairs) {
		t.Error("IsPlanningPair(epic-planning-pair, pairs) = false, want true")
	}
	if IsPlanningPair("coding-pair", pairs) {
		t.Error("IsPlanningPair(coding-pair, pairs) = true, want false")
	}

	// With nil (legacy fallback)
	if !IsPlanningPair("code-planning-pair", nil) {
		t.Error("IsPlanningPair(code-planning-pair, nil) = false, want true")
	}
	if IsPlanningPair("epic-planning-pair", nil) {
		t.Error("IsPlanningPair(epic-planning-pair, nil) should be false in legacy mode")
	}
}

func TestTransitionSourcePairs_NoPipeline(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	testhelpers.SetupLizaDir(t, tmpDir)
	os.Remove(filepath.Join(tmpDir, ".liza", "pipeline.yaml"))

	_, err := TransitionSourcePairs(tmpDir)
	if err == nil {
		t.Fatal("expected error for missing pipeline config")
	}
}

func TestLoadResolver_NoPipeline(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	testhelpers.SetupLizaDir(t, tmpDir)
	os.Remove(filepath.Join(tmpDir, ".liza", "pipeline.yaml"))

	_, _, err := loadResolver(tmpDir)
	if err == nil {
		t.Fatal("expected error for missing pipeline config")
	}
}

// --- ClaimTask pipeline tests ---

func TestClaimTask_PipelineCodingPair(t *testing.T) {
	tmpDir, stateFile := setupPipelineTest(t)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Agents["coder-1"] = testhelpers.RegisteredTestAgent("coder")
	state.PipelineVersion = 2
	task := models.Task{
		ID:          "task-1",
		Type:        models.TaskTypeCoding,
		RolePair:    "coding-pair",
		Description: "Pipeline coding task",
		Status:      models.TaskStatus("DRAFT_CODE"),
		Priority:    1,
		Created:     now,
		SpecRef:     "README.md",
		DoneWhen:    "done",
		Scope:       "scope",
		History:     []models.TaskHistoryEntry{},
	}
	state.Tasks = []models.Task{task}
	state.Sprint.Scope.Planned = []string{"task-1"}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := ClaimTask(tmpDir, "task-1", "coder-1")
	if err != nil {
		t.Fatalf("ClaimTask() error: %v", err)
	}
	if result.TaskID != "task-1" {
		t.Errorf("TaskID = %q, want %q", result.TaskID, "task-1")
	}

	// Verify the task transitioned to the pipeline executing state, not hardcoded IMPLEMENTING
	bb := db.For(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	readTask := readState.FindTask("task-1")
	if readTask == nil {
		t.Fatal("Task not found")
	}
	if readTask.Status != models.TaskStatus("IMPLEMENTING_CODE") {
		t.Errorf("Task status = %v, want IMPLEMENTING_CODE", readTask.Status)
	}
}

func TestClaimTask_PipelineCodePlanningPair(t *testing.T) {
	tmpDir, stateFile := setupPipelineTest(t)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Agents["code-planner-1"] = testhelpers.RegisteredTestAgent("code-planner")
	state.PipelineVersion = 2
	task := models.Task{
		ID:          "plan-1",
		Type:        models.TaskTypeCoding,
		RolePair:    "code-planning-pair",
		Description: "Pipeline planning task",
		Status:      models.TaskStatus("DRAFT_CODING_PLAN"),
		Priority:    1,
		Created:     now,
		SpecRef:     "README.md",
		DoneWhen:    "done",
		Scope:       "scope",
		History:     []models.TaskHistoryEntry{},
	}
	state.Tasks = []models.Task{task}
	state.Sprint.Scope.Planned = []string{"plan-1"}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := ClaimTask(tmpDir, "plan-1", "code-planner-1")
	if err != nil {
		t.Fatalf("ClaimTask() error: %v", err)
	}
	if result.TaskID != "plan-1" {
		t.Errorf("TaskID = %q, want %q", result.TaskID, "plan-1")
	}

	// Verify pipeline executing state
	bb := db.For(stateFile)
	readState, _ := bb.Read()
	readTask := readState.FindTask("plan-1")
	if readTask.Status != models.TaskStatus("CODE_PLANNING") {
		t.Errorf("Task status = %v, want CODE_PLANNING", readTask.Status)
	}
}

func TestClaimTask_NoPipelineReturnsError(t *testing.T) {
	// No pipeline.yaml → should fail now that pipeline is mandatory
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	os.Remove(filepath.Join(tmpDir, ".liza", "pipeline.yaml"))

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now),
	}
	state.Sprint.Scope.Planned = []string{"task-1"}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := ClaimTask(tmpDir, "task-1", "coder-1")
	if err == nil {
		t.Fatal("expected error when pipeline config is missing")
	}
}

func TestClaimTask_PipelineRejectedReclaim(t *testing.T) {
	tmpDir, stateFile := setupPipelineTest(t)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Agents["coder-2"] = testhelpers.RegisteredTestAgent("coder")
	state.PipelineVersion = 2

	// Create a CODE_REJECTED task with no assigned coder (recovered state)
	task := models.Task{
		ID:          "task-1",
		Type:        models.TaskTypeCoding,
		RolePair:    "coding-pair",
		Description: "Pipeline coding task after rejection",
		Status:      models.TaskStatus("CODE_REJECTED"),
		Priority:    1,
		Created:     now,
		SpecRef:     "README.md",
		DoneWhen:    "done",
		Scope:       "scope",
		History:     []models.TaskHistoryEntry{},
	}
	state.Tasks = []models.Task{task}
	state.Sprint.Scope.Planned = []string{"task-1"}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := ClaimTask(tmpDir, "task-1", "coder-2")
	if err != nil {
		t.Fatalf("ClaimTask() error: %v", err)
	}
	if result.TaskID != "task-1" {
		t.Errorf("TaskID = %q, want %q", result.TaskID, "task-1")
	}

	// Verify the task transitioned to IMPLEMENTING_CODE
	bb := db.For(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	readTask := readState.FindTask("task-1")
	if readTask == nil {
		t.Fatal("Task not found")
	}
	if readTask.Status != models.TaskStatus("IMPLEMENTING_CODE") {
		t.Errorf("Task status = %v, want IMPLEMENTING_CODE", readTask.Status)
	}
}

func TestClaimTask_PipelineRejectedIterationLimit(t *testing.T) {
	tmpDir, stateFile := setupPipelineTest(t)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Agents["coder-1"] = testhelpers.RegisteredTestAgent("coder")
	state.PipelineVersion = 2
	state.Config.MaxCoderIterations = 3

	agent := "coder-1"
	worktree := ".worktrees/task-1"
	baseCommit := "abc1234"
	leaseExpires := now.Add(30 * time.Minute)
	task := models.Task{
		ID:           "task-1",
		Type:         models.TaskTypeCoding,
		RolePair:     "coding-pair",
		Description:  "Pipeline coding task at iteration limit",
		Status:       models.TaskStatus("CODE_REJECTED"),
		Priority:     1,
		AssignedTo:   &agent,
		LeaseExpires: &leaseExpires,
		BaseCommit:   &baseCommit,
		Worktree:     &worktree,
		Iteration:    3, // at limit
		Attempt:      2, // attempt 2: iteration cap → BLOCKED
		Created:      now,
		SpecRef:      "README.md",
		DoneWhen:     "done",
		Scope:        "scope",
		History:      []models.TaskHistoryEntry{},
	}
	state.Tasks = []models.Task{task}
	state.Sprint.Scope.Planned = []string{"task-1"}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := ClaimTask(tmpDir, "task-1", "coder-1")
	if err == nil {
		t.Fatal("Expected error for iteration limit exceeded")
	}

	// Verify the task was transitioned to BLOCKED (not stuck in CODE_REJECTED)
	bb := db.For(stateFile)
	readState, readErr := bb.Read()
	if readErr != nil {
		t.Fatalf("Failed to read state: %v", readErr)
	}
	readTask := readState.FindTask("task-1")
	if readTask == nil {
		t.Fatal("Task not found")
	}
	if readTask.Status != models.TaskStatusBlocked {
		t.Errorf("Task status = %v, want BLOCKED", readTask.Status)
	}
}

// --- AddTask pipeline tests ---

func TestInitialTaskStatus_PipelineGoal(t *testing.T) {
	tmpDir, _ := setupPipelineTest(t)

	resolver, _, err := loadResolver(tmpDir)
	if err != nil {
		t.Fatalf("loadResolver error: %v", err)
	}

	// Pipeline goal: coding-pair → DRAFT_CODE
	status, err := resolver.InitialStatus("coding-pair")
	if err != nil {
		t.Fatalf("resolver.InitialStatus(coding-pair) error: %v", err)
	}
	if status != models.TaskStatus("DRAFT_CODE") {
		t.Errorf("InitialStatus(coding-pair) = %v, want DRAFT_CODE", status)
	}

	// Pipeline goal: code-planning-pair → DRAFT_CODING_PLAN
	status, err = resolver.InitialStatus("code-planning-pair")
	if err != nil {
		t.Fatalf("resolver.InitialStatus(code-planning-pair) error: %v", err)
	}
	if status != models.TaskStatus("DRAFT_CODING_PLAN") {
		t.Errorf("InitialStatus(code-planning-pair) = %v, want DRAFT_CODING_PLAN", status)
	}
}

func TestInitialTaskStatus_UnknownRolePair(t *testing.T) {
	tmpDir, _ := setupPipelineTest(t)

	resolver, _, err := loadResolver(tmpDir)
	if err != nil {
		t.Fatalf("loadResolver error: %v", err)
	}

	_, err = resolver.InitialStatus("nonexistent-pair")
	if err == nil {
		t.Fatal("expected error for unknown role-pair")
	}
}

// --- SubmitForReview pipeline tests ---

func TestSubmitForReview_PipelineCodingPairTransition(t *testing.T) {
	tmpDir, stateFile := setupPipelineTest(t)

	// Create a real git worktree so SubmitForReview can complete the full flow
	g := gitpkg.New(tmpDir)
	if _, err := g.CreateWorktree("task-1", "integration"); err != nil {
		t.Fatalf("CreateWorktree() error: %v", err)
	}
	wtPath := g.GetWorktreePath("task-1")

	// Add a test file to satisfy TDD enforcement, then commit
	testFile := filepath.Join(wtPath, "feature_test.go")
	if err := os.WriteFile(testFile, []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	testhelpers.MustGit(t, wtPath, "add", "feature_test.go")
	testhelpers.MustGit(t, wtPath, "commit", "-m", "Add feature with test")

	commitSHA := testhelpers.MustGit(t, wtPath, "rev-parse", "HEAD")
	baseCommit := testhelpers.MustGit(t, tmpDir, "rev-parse", "integration")

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2

	agent := "coder-1"
	leaseExpires := now.Add(30 * time.Minute)
	worktree := ".worktrees/task-1"
	task := models.Task{
		ID:           "task-1",
		Type:         models.TaskTypeCoding,
		RolePair:     "coding-pair",
		Description:  "Pipeline coding task",
		Status:       models.TaskStatus("IMPLEMENTING_CODE"),
		Priority:     1,
		AssignedTo:   &agent,
		LeaseExpires: &leaseExpires,
		BaseCommit:   &baseCommit,
		Worktree:     &worktree,
		Created:      now,
		SpecRef:      "README.md",
		DoneWhen:     "done",
		Scope:        "scope",
		History: []models.TaskHistoryEntry{
			{Time: now, Event: models.TaskEventPreExecutionCheckpoint, Agent: &agent},
		},
	}
	state.Tasks = []models.Task{task}
	state.Sprint.Scope.Planned = []string{"task-1"}
	state.Agents = map[string]models.Agent{
		"coder-1": {
			Role:         "coder",
			Status:       models.AgentStatusWorking,
			CurrentTask:  &task.ID,
			LeaseExpires: &leaseExpires,
			Heartbeat:    now,
		},
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := SubmitForReview(tmpDir, "task-1", commitSHA, "coder-1")
	if err != nil {
		t.Fatalf("SubmitForReview() error: %v", err)
	}
	if result.TaskID != "task-1" {
		t.Errorf("TaskID = %q, want %q", result.TaskID, "task-1")
	}

	// Verify the task transitioned to the pipeline submitted state
	bb := db.For(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	readTask := readState.FindTask("task-1")
	if readTask == nil {
		t.Fatal("Task not found")
	}
	if readTask.Status != models.TaskStatusReadyForReview {
		t.Errorf("Task status = %v, want %s", readTask.Status, models.TaskStatusReadyForReview)
	}
}

// --- SubmitVerdict pipeline tests ---

func TestSubmitVerdict_PipelineApproved(t *testing.T) {
	tmpDir, stateFile := setupPipelineTest(t)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2

	reviewingBy := "code-plan-reviewer-1"
	reviewLeaseExpires := now.Add(30 * time.Minute)
	reviewCommit := "review123"
	agent := "code-planner-1"
	worktree := ".worktrees/plan-1"
	task := models.Task{
		ID:                 "plan-1",
		Type:               models.TaskTypeCoding,
		RolePair:           "code-planning-pair",
		Description:        "Pipeline planning task",
		Status:             models.TaskStatus("REVIEWING_CODING_PLAN"),
		Priority:           1,
		AssignedTo:         &agent,
		ReviewingBy:        &reviewingBy,
		ReviewLeaseExpires: &reviewLeaseExpires,
		ReviewCommit:       &reviewCommit,
		Worktree:           &worktree,
		Created:            now,
		SpecRef:            "README.md",
		DoneWhen:           "done",
		Scope:              "scope",
		History:            []models.TaskHistoryEntry{},
	}
	state.Tasks = []models.Task{task}
	state.Sprint.Scope.Planned = []string{"plan-1"}
	state.Agents = map[string]models.Agent{
		"code-plan-reviewer-1": {
			Role:   "code-plan-reviewer",
			Status: models.AgentStatusReviewing,
		},
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := SubmitVerdict(tmpDir, "plan-1", "APPROVED", "", "code-plan-reviewer-1", "")
	if err != nil {
		t.Fatalf("SubmitVerdict() error: %v", err)
	}
	if result.Verdict != "APPROVED" {
		t.Errorf("Verdict = %q, want APPROVED", result.Verdict)
	}

	bb := db.For(stateFile)
	readState, _ := bb.Read()
	readTask := readState.FindTask("plan-1")
	if readTask.Status != models.TaskStatus("CODING_PLAN_APPROVED") {
		t.Errorf("Task status = %v, want CODING_PLAN_APPROVED", readTask.Status)
	}
}

func TestSubmitVerdict_PipelineCodingPairApproved(t *testing.T) {
	tmpDir, stateFile := setupPipelineTest(t)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2

	reviewingBy := "code-reviewer-1"
	reviewLeaseExpires := now.Add(30 * time.Minute)
	reviewCommit := "review123"
	agent := "coder-1"
	worktree := ".worktrees/task-1"
	task := models.Task{
		ID:                 "task-1",
		Type:               models.TaskTypeCoding,
		RolePair:           "coding-pair",
		Description:        "Pipeline coding task",
		Status:             models.TaskStatus("REVIEWING_CODE"),
		Priority:           1,
		AssignedTo:         &agent,
		ReviewingBy:        &reviewingBy,
		ReviewLeaseExpires: &reviewLeaseExpires,
		ReviewCommit:       &reviewCommit,
		Worktree:           &worktree,
		Created:            now,
		SpecRef:            "README.md",
		DoneWhen:           "done",
		Scope:              "scope",
		History:            []models.TaskHistoryEntry{},
	}
	state.Tasks = []models.Task{task}
	state.Sprint.Scope.Planned = []string{"task-1"}
	state.Agents = map[string]models.Agent{
		"code-reviewer-1": {
			Role:   "code-reviewer",
			Status: models.AgentStatusReviewing,
		},
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := SubmitVerdict(tmpDir, "task-1", "APPROVED", "", "code-reviewer-1", "")
	if err != nil {
		t.Fatalf("SubmitVerdict() error: %v", err)
	}
	if result.Verdict != "APPROVED" {
		t.Errorf("Verdict = %q, want APPROVED", result.Verdict)
	}

	bb := db.For(stateFile)
	readState, _ := bb.Read()
	readTask := readState.FindTask("task-1")
	if readTask.Status != models.TaskStatus("CODE_APPROVED") {
		t.Errorf("Task status = %v, want CODE_APPROVED", readTask.Status)
	}
}

// --- ResumeHandoff pipeline tests ---

func TestResumeHandoff_PipelineExecutingState(t *testing.T) {
	tmpDir, stateFile := setupPipelineTest(t)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2

	agent := "coder-1"
	leaseExpires := now.Add(30 * time.Minute)
	worktree := ".worktrees/task-1"
	baseCommit := "abc1234"
	task := models.Task{
		ID:             "task-1",
		Type:           models.TaskTypeCoding,
		RolePair:       "coding-pair",
		Description:    "Pipeline coding task",
		Status:         models.TaskStatus("IMPLEMENTING_CODE"),
		Priority:       1,
		AssignedTo:     &agent,
		LeaseExpires:   &leaseExpires,
		BaseCommit:     &baseCommit,
		Worktree:       &worktree,
		HandoffPending: true,
		Created:        now,
		SpecRef:        "README.md",
		DoneWhen:       "done",
		Scope:          "scope",
		History:        []models.TaskHistoryEntry{},
	}
	state.Tasks = []models.Task{task}
	state.Sprint.Scope.Planned = []string{"task-1"}
	state.Agents = map[string]models.Agent{
		"coder-1": {
			Role:         "coder",
			Status:       models.AgentStatusHandoff,
			CurrentTask:  &task.ID,
			LeaseExpires: &leaseExpires,
			Heartbeat:    now,
		},
	}

	// Create the worktree directory on disk
	testhelpers.CreateTestWorktree(t, tmpDir, "task-1")

	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := ResumeHandoff(ResumeHandoffInput{
		ProjectRoot: tmpDir,
		AgentID:     "coder-1",
	})
	if err != nil {
		t.Fatalf("ResumeHandoff() error: %v", err)
	}
	if !result.Found {
		t.Fatal("Expected to find resumable handoff")
	}
	if result.TaskID != "task-1" {
		t.Errorf("TaskID = %q, want %q", result.TaskID, "task-1")
	}
}
