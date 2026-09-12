package ops

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/paths"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
	"gopkg.in/yaml.v3"
)

const (
	lifecycleGenerationA = "generation-a"
	lifecycleGenerationB = "generation-b"
)

func TestResumeGenerationFence(t *testing.T) {
	t.Run("handoff", func(t *testing.T) {
		const (
			taskID  = "task-handoff-resume-fence"
			agentID = "coder-1"
		)
		projectRoot := t.TempDir()
		statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
		testhelpers.SetupPipelineConfig(t, projectRoot)
		resolver, _, err := loadResolver(projectRoot)
		if err != nil {
			t.Fatalf("load resolver: %v", err)
		}
		executing, err := resolver.ExecutingStatus("coding-pair")
		if err != nil {
			t.Fatalf("resolve executing status: %v", err)
		}
		now := time.Now().UTC()
		state := testhelpers.CreateValidState()
		task := testhelpers.BuildTaskByStatus(taskID, models.TaskStatusImplementing, now)
		task.Status = executing
		task.AssignedTo = stringPtr(agentID)
		task.HandoffPending = true
		state.Tasks = []models.Task{task}
		agent := resumableOwnedAgent(models.RoleCoder, models.AgentStatusHandoff, stringPtr(taskID), now)
		agent.Generation = lifecycleGenerationA
		state.Agents[agentID] = agent
		testhelpers.WriteInitialState(t, statePath, state)
		bb := db.For(statePath)

		stale := models.AgentAuthority{ID: agentID, Generation: lifecycleGenerationA}
		current := models.AgentAuthority{ID: agentID, Generation: lifecycleGenerationB}
		var before []byte
		removeHook := func() {}
		removeHook = setLifecycleMutationTestHook(bb, func() {
			removeHook()
			setLifecycleAgentGeneration(t, bb, agentID, lifecycleGenerationB)
			before = readStateBytes(t, statePath)
		})
		t.Cleanup(removeHook)

		_, err = ResumeHandoff(ResumeHandoffInput{
			ProjectRoot: projectRoot, AgentID: agentID, Authority: &stale,
		})
		assertLifecycleAuthorityError(t, err, agentID)
		if after := readStateBytes(t, statePath); !bytes.Equal(after, before) {
			t.Fatal("stale handoff resume changed generation-B state")
		}
		if _, err := ResumeHandoff(ResumeHandoffInput{
			ProjectRoot: projectRoot, AgentID: agentID, Authority: &current,
		}); err != nil {
			t.Fatalf("current handoff resume failed: %v", err)
		}
	})

	t.Run("owned task", func(t *testing.T) {
		const (
			taskID  = "task-owned-resume-fence"
			agentID = "coder-1"
		)
		projectRoot := t.TempDir()
		testhelpers.SetupTestGitRepo(t, projectRoot)
		statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
		testhelpers.SetupPipelineConfig(t, projectRoot)
		testhelpers.CreateTestWorktree(t, projectRoot, taskID)
		resolver, _, err := loadResolver(projectRoot)
		if err != nil {
			t.Fatalf("load resolver: %v", err)
		}
		executing, err := resolver.ExecutingStatus("coding-pair")
		if err != nil {
			t.Fatalf("resolve executing status: %v", err)
		}
		now := time.Now().UTC()
		state := testhelpers.CreateValidState()
		task := testhelpers.BuildTaskByStatus(taskID, models.TaskStatusImplementing, now)
		task.Status = executing
		task.AssignedTo = stringPtr(agentID)
		state.Tasks = []models.Task{task}
		agent := resumableOwnedAgent(models.RoleCoder, models.AgentStatusIdle, nil, now)
		agent.Generation = lifecycleGenerationA
		state.Agents[agentID] = agent
		testhelpers.WriteInitialState(t, statePath, state)
		bb := db.For(statePath)

		stale := models.AgentAuthority{ID: agentID, Generation: lifecycleGenerationA}
		current := models.AgentAuthority{ID: agentID, Generation: lifecycleGenerationB}
		var before []byte
		removeHook := func() {}
		removeHook = setLifecycleMutationTestHook(bb, func() {
			removeHook()
			setLifecycleAgentGeneration(t, bb, agentID, lifecycleGenerationB)
			before = readStateBytes(t, statePath)
		})
		t.Cleanup(removeHook)

		_, err = ResumeOwnedTask(ResumeOwnedTaskInput{
			ProjectRoot: projectRoot, AgentID: agentID, Authority: &stale,
		})
		assertLifecycleAuthorityError(t, err, agentID)
		if after := readStateBytes(t, statePath); !bytes.Equal(after, before) {
			t.Fatal("stale owned-task resume changed generation-B state")
		}
		if _, err := ResumeOwnedTask(ResumeOwnedTaskInput{
			ProjectRoot: projectRoot, AgentID: agentID, Authority: &current,
		}); err != nil {
			t.Fatalf("current owned-task resume failed: %v", err)
		}
	})
}

func TestReviewerClaimGenerationFence(t *testing.T) {
	const (
		taskID  = "task-reviewer-claim-fence"
		agentID = "code-reviewer-1"
	)
	projectRoot := t.TempDir()
	testhelpers.SetupTestGitRepo(t, projectRoot)
	statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
	testhelpers.SetupPipelineConfig(t, projectRoot)
	resolver, _, err := loadResolver(projectRoot)
	if err != nil {
		t.Fatalf("load resolver: %v", err)
	}
	submitted, err := resolver.SubmittedStatus("coding-pair")
	if err != nil {
		t.Fatalf("resolve submitted status: %v", err)
	}
	state := testhelpers.CreateValidState()
	agent := testhelpers.RegisteredTestAgent(models.RoleCodeReviewer)
	agent.Generation = lifecycleGenerationA
	state.Agents[agentID] = agent
	reviewCommit := "abc123"
	task := models.Task{
		ID: taskID, Status: submitted, RolePair: "coding-pair", Priority: 1,
		Description: "reviewer generation fence", DoneWhen: "only B claims",
		Scope: "test", SpecRef: "README.md", ReviewCommit: &reviewCommit,
		Created: time.Now().UTC(),
	}
	state.Tasks = []models.Task{task}
	testhelpers.WriteInitialState(t, statePath, state)
	bb := db.For(statePath)

	stale := models.AgentAuthority{ID: agentID, Generation: lifecycleGenerationA}
	current := models.AgentAuthority{ID: agentID, Generation: lifecycleGenerationB}
	var before []byte
	removeHook := func() {}
	removeHook = setLifecycleMutationTestHook(bb, func() {
		removeHook()
		setLifecycleAgentGeneration(t, bb, agentID, lifecycleGenerationB)
		before = readStateBytes(t, statePath)
	})
	t.Cleanup(removeHook)

	_, err = ClaimReviewerTask(ClaimReviewerTaskInput{
		ProjectRoot: projectRoot, AgentID: agentID, Role: models.RoleCodeReviewer,
		TaskID: taskID, LeaseDuration: 1800, Authority: &stale,
	})
	assertLifecycleAuthorityError(t, err, agentID)
	if after := readStateBytes(t, statePath); !bytes.Equal(after, before) {
		t.Fatal("stale reviewer claim changed generation-B state")
	}
	if _, err := ClaimReviewerTask(ClaimReviewerTaskInput{
		ProjectRoot: projectRoot, AgentID: agentID, Role: models.RoleCodeReviewer,
		TaskID: taskID, LeaseDuration: 1800, Authority: &current,
	}); err != nil {
		t.Fatalf("current reviewer claim failed: %v", err)
	}
}

func TestReviewerLifecycleMutationGenerationFence(t *testing.T) {
	t.Run("submit-for-review", testSubmitForReviewMutationGenerationFence)
	t.Run("handoff", testHandoffMutationGenerationFence)
	t.Run("submit-verdict", testSubmitVerdictMutationGenerationFence)
	t.Run("wt-merge", testWtMergeMutationGenerationFence)

	for _, name := range []string{"submit_review.go", "handoff.go", "submit_verdict.go"} {
		assertNoDirectBlackboardModify(t, name)
	}
}

func TestAwaitMutationGenerationFence(t *testing.T) {
	const (
		doerID     = "coder-1"
		reviewerID = "code-reviewer-1"
		taskID     = "task-await-fence"
	)

	tests := []struct {
		name    string
		agentID string
		prepare func(*models.State)
		mutate  func(*db.Blackboard, models.AgentAuthority) error
	}{
		{
			name:    "await-verdict acquire",
			agentID: doerID,
			mutate: func(bb *db.Blackboard, authority models.AgentAuthority) error {
				return acquireAwaitOwnership(bb, authority.ID, taskID, &authority)
			},
		},
		{
			name:    "await-verdict release",
			agentID: doerID,
			prepare: func(state *models.State) {
				agent := state.Agents[doerID]
				agent.CurrentTask = stringPtr(taskID)
				state.Agents[doerID] = agent
			},
			mutate: func(bb *db.Blackboard, authority models.AgentAuthority) error {
				return releaseOwnership(bb, authority.ID, &authority)
			},
		},
		{
			name:    "await-verdict budget cleanup",
			agentID: doerID,
			prepare: func(state *models.State) {
				state.Tasks[0].AssignedTo = stringPtr(doerID)
				state.Tasks[0].LeaseExpires = timePtr(time.Now().UTC().Add(time.Hour))
			},
			mutate: func(_ *db.Blackboard, authority models.AgentAuthority) error {
				return ReleaseDepartedDoerAssignmentWithAuthority(t.TempDir(), taskID, authority)
			},
		},
		{
			name:    "await-resubmission acquire",
			agentID: reviewerID,
			mutate: func(bb *db.Blackboard, authority models.AgentAuthority) error {
				return acquireReviewOwnership(bb, authority.ID, taskID, &authority, time.Minute)
			},
		},
		{
			name:    "await-resubmission release",
			agentID: reviewerID,
			prepare: func(state *models.State) {
				agent := state.Agents[reviewerID]
				agent.CurrentTask = stringPtr(taskID)
				state.Agents[reviewerID] = agent
				state.Tasks[0].ReviewingBy = stringPtr(reviewerID)
				state.Tasks[0].ReviewLeaseExpires = timePtr(time.Now().UTC().Add(time.Hour))
			},
			mutate: func(bb *db.Blackboard, authority models.AgentAuthority) error {
				return releaseReviewOwnership(bb, authority.ID, taskID, &authority)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			projectRoot := t.TempDir()
			statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
			state := lifecycleFenceState(taskID, doerID, reviewerID)
			if tt.prepare != nil {
				tt.prepare(state)
			}
			bb := testhelpers.WriteInitialState(t, statePath, state)
			stale := models.AgentAuthority{ID: tt.agentID, Generation: lifecycleGenerationA}
			current := models.AgentAuthority{ID: tt.agentID, Generation: lifecycleGenerationB}

			if tt.name == "await-verdict budget cleanup" {
				// The public helper resolves its own blackboard from projectRoot.
				tt.mutate = func(_ *db.Blackboard, authority models.AgentAuthority) error {
					return ReleaseDepartedDoerAssignmentWithAuthority(projectRoot, taskID, authority)
				}
			}

			var before []byte
			previousHook := lifecycleBeforeModifyTestHook
			lifecycleBeforeModifyTestHook = func() {
				lifecycleBeforeModifyTestHook = nil
				setLifecycleAgentGeneration(t, bb, tt.agentID, lifecycleGenerationB)
				before = readStateBytes(t, statePath)
			}
			t.Cleanup(func() { lifecycleBeforeModifyTestHook = previousHook })

			err := tt.mutate(bb, stale)
			assertLifecycleAuthorityError(t, err, tt.agentID)
			if after := readStateBytes(t, statePath); !bytes.Equal(after, before) {
				t.Fatalf("stale %s changed generation-B state", tt.name)
			}
			if err := tt.mutate(bb, current); err != nil {
				t.Fatalf("current %s failed: %v", tt.name, err)
			}
		})
	}

	t.Run("await-verdict timeout cleanup", func(t *testing.T) {
		projectRoot := t.TempDir()
		statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
		now := time.Now().UTC()
		state := testhelpers.CreateValidState()
		task := testhelpers.BuildTaskByStatus(taskID, models.TaskStatusReadyForReview, now)
		task.History = append(task.History, models.TaskHistoryEntry{
			Time:  now,
			Event: models.TaskEventSubmittedForReview,
			Agent: stringPtr(doerID),
		})
		state.Tasks = []models.Task{task}
		state.Agents[doerID] = models.Agent{
			Role:       "coder",
			Status:     models.AgentStatusWaiting,
			Generation: lifecycleGenerationA,
		}
		bb := testhelpers.WriteInitialState(t, statePath, state)

		previousWatcher := newAwaitVerdictWatcher
		newAwaitVerdictWatcher = func(*db.Blackboard) (awaitVerdictWatcher, error) {
			return silentAwaitVerdictWatcher{}, nil
		}
		t.Cleanup(func() { newAwaitVerdictWatcher = previousWatcher })

		stale := models.AgentAuthority{ID: doerID, Generation: lifecycleGenerationA}
		var before []byte
		modifyCount := 0
		previousHook := lifecycleBeforeModifyTestHook
		lifecycleBeforeModifyTestHook = func() {
			modifyCount++
			if modifyCount == 2 {
				lifecycleBeforeModifyTestHook = nil
				setLifecycleAgentGeneration(t, bb, doerID, lifecycleGenerationB)
				before = readStateBytes(t, statePath)
			}
		}
		t.Cleanup(func() { lifecycleBeforeModifyTestHook = previousHook })

		result, err := AwaitVerdictWithAuthorityOptions(
			context.Background(), projectRoot, taskID, stale, 10*time.Millisecond,
			AwaitVerdictOptions{AbortPollInterval: time.Hour},
		)
		assertLifecycleAuthorityError(t, err, doerID)
		if result == nil || result.Verdict != VerdictTimeout {
			t.Fatalf("result = %#v, want TIMEOUT with authority error", result)
		}
		if after := readStateBytes(t, statePath); !bytes.Equal(after, before) {
			t.Fatal("stale await-verdict timeout cleanup changed generation-B state")
		}
	})

	t.Run("await-resubmission timeout cleanup", func(t *testing.T) {
		projectRoot := t.TempDir()
		statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
		now := time.Now().UTC()
		state := testhelpers.CreateValidState()
		task := testhelpers.BuildTaskByStatus(taskID, models.TaskStatusRejected, now)
		task.History = append(task.History, models.TaskHistoryEntry{
			Time:  now,
			Event: models.TaskEventRejected,
			Agent: stringPtr(reviewerID),
		})
		state.Tasks = []models.Task{task}
		state.Agents[reviewerID] = models.Agent{
			Role:       "code-reviewer",
			Status:     models.AgentStatusIdle,
			Generation: lifecycleGenerationA,
		}
		bb := testhelpers.WriteInitialState(t, statePath, state)

		previousWatcher := newAwaitResubmissionWatcher
		newAwaitResubmissionWatcher = func(*db.Blackboard) (awaitResubmissionWatcher, error) {
			return silentAwaitVerdictWatcher{}, nil
		}
		t.Cleanup(func() { newAwaitResubmissionWatcher = previousWatcher })

		stale := models.AgentAuthority{ID: reviewerID, Generation: lifecycleGenerationA}
		var before []byte
		modifyCount := 0
		previousHook := lifecycleBeforeModifyTestHook
		lifecycleBeforeModifyTestHook = func() {
			modifyCount++
			if modifyCount == 2 {
				lifecycleBeforeModifyTestHook = nil
				setLifecycleAgentGeneration(t, bb, reviewerID, lifecycleGenerationB)
				before = readStateBytes(t, statePath)
			}
		}
		t.Cleanup(func() { lifecycleBeforeModifyTestHook = previousHook })

		result, err := AwaitResubmissionWithAuthorityOptions(
			context.Background(), projectRoot, taskID, stale, 10*time.Millisecond,
			AwaitResubmissionOptions{AbortPollInterval: time.Hour},
		)
		assertLifecycleAuthorityError(t, err, reviewerID)
		if result == nil || result.Verdict != ResubmissionTimeout {
			t.Fatalf("result = %#v, want TIMEOUT with authority error", result)
		}
		if after := readStateBytes(t, statePath); !bytes.Equal(after, before) {
			t.Fatal("stale await-resubmission timeout cleanup changed generation-B state")
		}
	})

	t.Run("cleanup preserves primary error", func(t *testing.T) {
		authorityErr := &AgentAuthorityError{
			AgentID:           doerID,
			LosingGeneration:  lifecycleGenerationA,
			CurrentGeneration: lifecycleGenerationB,
		}
		err := joinAwaitCleanupError(context.Canceled, authorityErr)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context cancellation preserved", err)
		}
		assertLifecycleAuthorityError(t, err, doerID)
	})

	assertNoDirectBlackboardModify(t, "await_verdict.go")
	assertNoDirectBlackboardModify(t, "await_resubmission.go")
}

func TestMergeMutationGenerationFence(t *testing.T) {
	t.Run("integration failure", func(t *testing.T) {
		const (
			taskID  = "task-merge-failure-fence"
			agentID = "code-reviewer-1"
		)
		projectRoot, statePath := setupMergeTestRepo(t, taskID, agentID)
		bb := db.For(statePath)
		setLifecycleAgentGeneration(t, bb, agentID, lifecycleGenerationA)
		pb, err := loadPipelineBundle(projectRoot)
		if err != nil {
			t.Fatalf("load pipeline bundle: %v", err)
		}
		stale := models.AgentAuthority{ID: agentID, Generation: lifecycleGenerationA}
		current := models.AgentAuthority{ID: agentID, Generation: lifecycleGenerationB}
		var before []byte
		previousHook := lifecycleBeforeModifyTestHook
		lifecycleBeforeModifyTestHook = func() {
			lifecycleBeforeModifyTestHook = nil
			setLifecycleAgentGeneration(t, bb, agentID, lifecycleGenerationB)
			before = readStateBytes(t, statePath)
		}
		t.Cleanup(func() { lifecycleBeforeModifyTestHook = previousHook })

		err = markIntegrationFailedWithAuthority(bb, taskID, stale, IntegrationReasonMergeConflict, "", pb)
		assertLifecycleAuthorityError(t, err, agentID)
		if after := readStateBytes(t, statePath); !bytes.Equal(after, before) {
			t.Fatal("stale integration-failure write changed generation-B state")
		}
		if err := markIntegrationFailedWithAuthority(bb, taskID, current, IntegrationReasonMergeConflict, "", pb); err != nil {
			t.Fatalf("current integration-failure write failed: %v", err)
		}
	})

	t.Run("final merge", testWtMergeMutationGenerationFence)

	assertNoDirectBlackboardModify(t, "wt_merge.go")
}

func testWtMergeMutationGenerationFence(t *testing.T) {
	const (
		taskID  = "task-final-merge-fence"
		agentID = "code-reviewer-1"
	)
	projectRoot, statePath := setupMergeTestRepo(t, taskID, agentID)
	bb := db.For(statePath)
	setLifecycleAgentGeneration(t, bb, agentID, lifecycleGenerationA)
	stale := models.AgentAuthority{ID: agentID, Generation: lifecycleGenerationA}
	current := models.AgentAuthority{ID: agentID, Generation: lifecycleGenerationB}

	var before []byte
	previousHook := mergeFinalStateTestHook
	mergeFinalStateTestHook = func() {
		setLifecycleAgentGeneration(t, bb, agentID, lifecycleGenerationB)
		before = readStateBytes(t, statePath)
	}
	t.Cleanup(func() { mergeFinalStateTestHook = previousHook })

	_, err := MergeWorktreeWithAuthority(projectRoot, taskID, stale)
	assertLifecycleAuthorityError(t, err, agentID)
	if after := readStateBytes(t, statePath); !bytes.Equal(after, before) {
		t.Fatal("stale final-merge write changed generation-B state")
	}

	mergeFinalStateTestHook = nil
	if _, err := MergeWorktreeWithAuthority(projectRoot, taskID, current); err != nil {
		t.Fatalf("current final merge failed: %v", err)
	}
}

func testSubmitForReviewMutationGenerationFence(t *testing.T) {
	projectRoot, taskID, commit, agentID, bb := setupSuccessfulSubmitScenario(t)
	statePath := filepath.Join(projectRoot, paths.ProjectDirName(), "state.yaml")
	setLifecycleAgentGeneration(t, bb, agentID, lifecycleGenerationA)
	stale := models.AgentAuthority{ID: agentID, Generation: lifecycleGenerationA}
	current := models.AgentAuthority{ID: agentID, Generation: lifecycleGenerationB}

	var before []byte
	previousHook := submitReviewBeforeModifyTestHook
	submitReviewBeforeModifyTestHook = func() {
		setLifecycleAgentGeneration(t, bb, agentID, lifecycleGenerationB)
		before = readStateBytes(t, statePath)
	}
	t.Cleanup(func() { submitReviewBeforeModifyTestHook = previousHook })

	_, err := SubmitForReviewWithAuthority(projectRoot, taskID, commit, stale)
	assertLifecycleAuthorityError(t, err, agentID)
	if after := readStateBytes(t, statePath); !bytes.Equal(after, before) {
		t.Fatal("stale submit-for-review changed generation-B state")
	}

	submitReviewBeforeModifyTestHook = nil
	if _, err := SubmitForReviewWithAuthority(projectRoot, taskID, "HEAD", current); err != nil {
		t.Fatalf("current submit-for-review failed: %v", err)
	}
}

func testHandoffMutationGenerationFence(t *testing.T) {
	const (
		taskID  = "task-handoff-fence"
		agentID = "coder-1"
	)
	projectRoot := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
	testhelpers.SetupPipelineConfig(t, projectRoot)
	state := lifecycleFenceState(taskID, agentID, "code-reviewer-1")
	state.Tasks[0].Status = models.TaskStatusImplementing
	state.Tasks[0].AssignedTo = stringPtr(agentID)
	state.Agents[agentID] = models.Agent{
		Role:        "coder",
		Status:      models.AgentStatusWorking,
		CurrentTask: stringPtr(taskID),
		Generation:  lifecycleGenerationA,
	}
	bb := testhelpers.WriteInitialState(t, statePath, state)
	stale := models.AgentAuthority{ID: agentID, Generation: lifecycleGenerationA}
	current := models.AgentAuthority{ID: agentID, Generation: lifecycleGenerationB}

	var before []byte
	previousHook := handoffBeforeModifyTestHook
	handoffBeforeModifyTestHook = func() {
		setLifecycleAgentGeneration(t, bb, agentID, lifecycleGenerationB)
		before = readStateBytes(t, statePath)
	}
	t.Cleanup(func() { handoffBeforeModifyTestHook = previousHook })

	input := &HandoffInput{
		ProjectRoot: projectRoot,
		TaskID:      taskID,
		Summary:     "done",
		NextAction:  "continue",
		AgentID:     agentID,
		Authority:   &stale,
	}
	_, err := Handoff(input)
	assertLifecycleAuthorityError(t, err, agentID)
	if after := readStateBytes(t, statePath); !bytes.Equal(after, before) {
		t.Fatal("stale handoff changed generation-B state")
	}

	handoffBeforeModifyTestHook = nil
	input.Authority = &current
	if _, err := Handoff(input); err != nil {
		t.Fatalf("current handoff failed: %v", err)
	}
}

func testSubmitVerdictMutationGenerationFence(t *testing.T) {
	const (
		taskID  = "task-verdict-fence"
		agentID = "code-reviewer-1"
	)
	projectRoot := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{testhelpers.BuildTaskByStatus(taskID, models.TaskStatusReviewing, time.Now().UTC())}
	commit := strings.Repeat("a", 40)
	state.Tasks[0].ReviewCommit = &commit
	state.Agents[agentID] = models.Agent{Role: "code-reviewer", Status: models.AgentStatusWorking, Generation: lifecycleGenerationA}
	bb := testhelpers.WriteInitialState(t, statePath, state)
	stale := models.AgentAuthority{ID: agentID, Generation: lifecycleGenerationA}
	current := models.AgentAuthority{ID: agentID, Generation: lifecycleGenerationB}

	var before []byte
	previousHooks := testSubmitVerdictHooks
	testSubmitVerdictHooks = &submitVerdictTestHooks{beforeModify: func() {
		setLifecycleAgentGeneration(t, bb, agentID, lifecycleGenerationB)
		before = readStateBytes(t, statePath)
	}}
	t.Cleanup(func() { testSubmitVerdictHooks = previousHooks })

	_, err := SubmitVerdictWithAuthority(projectRoot, taskID, "APPROVED", "", stale, "", commit)
	assertLifecycleAuthorityError(t, err, agentID)
	var beforeState models.State
	if err := yaml.Unmarshal(before, &beforeState); err != nil {
		t.Fatal(err)
	}
	after, readErr := bb.Read()
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !reflect.DeepEqual(after.Tasks, beforeState.Tasks) || !reflect.DeepEqual(after.Agents, beforeState.Agents) {
		t.Fatal("stale submit-verdict changed generation-B task or agent state")
	}
	if len(after.QuarantinedVerdicts) != 1 || after.QuarantinedVerdicts[0].ReviewCommit != commit {
		t.Fatal("generation replacement at final write lost fenced verdict evidence")
	}

	testSubmitVerdictHooks = nil
	if _, err := SubmitVerdictWithAuthority(projectRoot, taskID, "APPROVED", "", current, "", commit); err != nil {
		t.Fatalf("current submit-verdict failed: %v", err)
	}
}

func lifecycleFenceState(taskID, doerID, reviewerID string) *models.State {
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{{
		ID:          taskID,
		Description: "generation-fence fixture",
		DoneWhen:    "stale generation cannot mutate",
		Scope:       "test",
		SpecRef:     "README.md",
		RolePair:    "coding-pair",
		Status:      models.TaskStatusReady,
		Created:     time.Now().UTC(),
	}}
	state.Agents[doerID] = models.Agent{Role: "coder", Status: models.AgentStatusWaiting, Generation: lifecycleGenerationA}
	state.Agents[reviewerID] = models.Agent{Role: "code-reviewer", Status: models.AgentStatusWaiting, Generation: lifecycleGenerationA}
	return state
}

func setLifecycleAgentGeneration(t *testing.T, bb *db.Blackboard, agentID, generation string) {
	t.Helper()
	if err := bb.Modify(func(state *models.State) error {
		agent := state.Agents[agentID]
		agent.Generation = generation
		state.Agents[agentID] = agent
		return nil
	}); err != nil {
		t.Fatalf("replace agent generation: %v", err)
	}
}

func assertLifecycleAuthorityError(t *testing.T, err error, agentID string) {
	t.Helper()
	var authorityErr *AgentAuthorityError
	if !errors.As(err, &authorityErr) {
		t.Fatalf("error = %T %v, want *AgentAuthorityError", err, err)
	}
	for _, want := range []string{agentID, generationFingerprint(lifecycleGenerationA), generationFingerprint(lifecycleGenerationB)} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want %q", err, want)
		}
	}
}

func assertNoDirectBlackboardModify(t *testing.T, filename string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(".", filename))
	if err != nil {
		t.Fatalf("read %s: %v", filename, err)
	}
	if bytes.Contains(data, []byte("bb.Modify(")) {
		t.Errorf("%s retains a direct blackboard mutation outside the authority-routing helper", filename)
	}
}

func readStateBytes(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	return data
}

func stringPtr(value string) *string { return &value }

func timePtr(value time.Time) *time.Time { return &value }
