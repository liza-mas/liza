package ops

import (
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestClaimTask_ReleasesDormantAssignments(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	testhelpers.SetupTestGitRepo(t, root)
	stateFile, _ := testhelpers.SetupLizaDir(t, root)
	state := testhelpers.CreateValidState()
	registerClaimTaskTestAgents(state)
	now := time.Now().UTC()
	state.Tasks = []models.Task{testhelpers.BuildTaskByStatus("new", models.TaskStatusReady, now)}
	for _, id := range []string{"old-A", "old-B"} {
		task := testhelpers.BuildTaskByStatus(id, models.TaskStatusRejected, now)
		base, err := git.New(root).CreateWorktree(id, "integration")
		if err != nil {
			t.Fatal(err)
		}
		task.BaseCommit = &base
		task.ReviewCyclesCurrent = 2
		task.ReviewCyclesTotal = 3
		models.AdvanceLifecycle(&task)
		state.Tasks = append(state.Tasks, task)
	}
	testhelpers.WriteInitialState(t, stateFile, state)
	before := db.CloneState(readClaimStateForTest(t, stateFile))

	if _, err := ClaimTask(root, "new", "coder-1"); err != nil {
		t.Fatal(err)
	}
	after := readClaimStateForTest(t, stateFile)
	for _, id := range []string{"old-A", "old-B"} {
		task := after.FindTask(id)
		if task.AssignedTo != nil || task.LeaseExpires != nil {
			t.Fatalf("%s retains dormant assignment or lease after claiming new work", id)
		}
		prior := before.FindTask(id)
		if task.Lifecycle.Revision != prior.Lifecycle.Revision+1 || task.Lifecycle.Preparation != nil {
			t.Fatalf("%s release did not retire its lifecycle boundary", id)
		}
		entry := task.History[len(task.History)-1]
		if entry.Event != models.TaskEventDoerClaimReleased || entry.Agent == nil || *entry.Agent != "coder-1" ||
			entry.PreviousAssignee == nil || *entry.PreviousAssignee != "coder-1" ||
			entry.Reason == nil || !strings.Contains(*entry.Reason, "new") || entry.Extra["claimed_task"] != "new" {
			t.Fatalf("%s release lacks claim audit context: %+v", id, entry)
		}
		// Every other task field, including work/review evidence and counters,
		// must survive the ownership release exactly as persisted before it.
		preserved := *task
		preserved.AssignedTo, preserved.LeaseExpires = prior.AssignedTo, prior.LeaseExpires
		preserved.History, preserved.Lifecycle = prior.History, prior.Lifecycle
		if !reflect.DeepEqual(preserved, *prior) {
			t.Fatalf("%s release changed retained work", id)
		}
	}
	if agent := after.Agents["coder-1"]; agent.CurrentTask == nil || *agent.CurrentTask != "new" {
		t.Fatal("claimant does not own new task")
	}
	resolver, _, err := loadResolver(root)
	if err != nil {
		t.Fatal(err)
	}
	if reason := models.DoerClaimBlockedReason(after, after.FindTask("old-A"), "coder", "coder-2", resolver, now); reason != "" {
		t.Fatalf("released rejected work remains reserved: %s", reason)
	}
	if _, err := ClaimTask(root, "old-A", "coder-2"); err != nil {
		t.Fatalf("different doer cannot reclaim released rejected work: %v", err)
	}
}

func TestClaimTask_DormantStatusReleasePreservesEvidence(t *testing.T) {
	t.Parallel()
	for _, status := range []models.TaskStatus{models.TaskStatusReadyForReview, models.TaskStatusReviewing, models.TaskStatusApproved, models.TaskStatusBlocked} {
		t.Run(string(status), func(t *testing.T) {
			root, stateFile, state := assignmentClaimFixture(t, status)
			if status == models.TaskStatusReviewing {
				reviewer := testhelpers.RegisteredTestAgent("code-reviewer")
				reviewer.Status = models.AgentStatusReviewing
				reviewer.CurrentTask = testhelpers.StringPtr("old")
				state.Agents["code-reviewer-1"] = reviewer
			}
			testhelpers.WriteInitialState(t, stateFile, state)
			prior := readClaimStateForTest(t, stateFile).FindTask("old")
			if _, err := ClaimTask(root, "new", "coder-1"); err != nil {
				t.Fatal(err)
			}
			after := readClaimStateForTest(t, stateFile)
			old := after.FindTask("old")
			if old.AssignedTo != nil || old.LeaseExpires != nil {
				t.Fatal("dormant assignment retained")
			}
			preserved := *old
			preserved.AssignedTo, preserved.LeaseExpires = prior.AssignedTo, prior.LeaseExpires
			preserved.History, preserved.Lifecycle = prior.History, prior.Lifecycle
			if !reflect.DeepEqual(preserved, *prior) {
				t.Fatal("dormant release changed work/review evidence")
			}
			if status == models.TaskStatusReviewing && !reflect.DeepEqual(after.Agents["code-reviewer-1"], state.Agents["code-reviewer-1"]) {
				t.Fatal("dormant release changed the reviewer")
			}
		})
	}
}

func TestClaimTask_PreservesUnrelatedAndTerminalAssignments(t *testing.T) {
	t.Parallel()
	root, stateFile, state := assignmentClaimFixture(t, models.TaskStatusRejected)
	state.FindTask("old").AssignedTo = testhelpers.StringPtr("coder-2")
	now := time.Now().UTC()
	resolver, _, err := loadResolver(root)
	if err != nil {
		t.Fatal(err)
	}
	clean, err := resolver.CleanStatus("integration-pair")
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range []models.TaskStatus{models.TaskStatusMerged, models.TaskStatusAbandoned, models.TaskStatusSuperseded, clean} {
		task := testhelpers.BuildTaskByStatus(string(status), status, now)
		task.AssignedTo = testhelpers.StringPtr("coder-1")
		if status == clean {
			task.RolePair = "integration-pair"
		}
		state.Tasks = append(state.Tasks, task)
	}
	testhelpers.WriteInitialState(t, stateFile, state)
	before := db.CloneState(readClaimStateForTest(t, stateFile))
	if _, err := ClaimTask(root, "new", "coder-1"); err != nil {
		t.Fatal(err)
	}
	after := readClaimStateForTest(t, stateFile)
	for _, prior := range before.Tasks[1:] {
		if !reflect.DeepEqual(*after.FindTask(prior.ID), prior) {
			t.Fatalf("changed unrelated/terminal assignment %s", prior.ID)
		}
	}
}

func TestClaimTask_FailedClaimPreservesDormantAssignments(t *testing.T) {
	t.Parallel()
	for _, failure := range []string{"admission", "executing assignment", "candidate validation"} {
		t.Run(failure, func(t *testing.T) {
			root, stateFile, state := assignmentClaimFixture(t, models.TaskStatusRejected)
			want := "already working"
			switch failure {
			case "admission":
				agent := state.Agents["coder-1"]
				agent.CurrentTask = testhelpers.StringPtr("old")
				state.Agents["coder-1"] = agent
			case "executing assignment":
				// Empty CurrentTask must not authorize stripping another execution.
				state.Tasks = append(state.Tasks, testhelpers.BuildTaskByStatus("executing", models.TaskStatusImplementing, time.Now().UTC()))
				want = "already assigned to executing task executing"
			case "candidate validation":
				// Rejected assigned work can lack a base; releasing it cannot.
				state.FindTask("old").BaseCommit = nil
				want = "released task has worktree without base_commit"
			}
			testhelpers.WriteInitialState(t, stateFile, state)
			before := db.CloneState(readClaimStateForTest(t, stateFile))
			if _, err := ClaimTask(root, "new", "coder-1"); err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("claim = %v, want refusal containing %q", err, want)
			}
			after := readClaimStateForTest(t, stateFile)
			for _, prior := range before.Tasks[1:] {
				if !reflect.DeepEqual(*after.FindTask(prior.ID), prior) {
					t.Fatalf("failed claim changed %s", prior.ID)
				}
			}
			if after.FindTask("new").AssignedTo != nil {
				t.Fatal("failed claim published new assignment")
			}
		})
	}
}

func TestClaimTask_ReconcilesConcurrentDormantAssignment(t *testing.T) {
	root, stateFile, state := assignmentClaimFixture(t, models.TaskStatusRejected)
	state.FindTask("old").AssignedTo = testhelpers.StringPtr("coder-2")
	testhelpers.WriteInitialState(t, stateFile, state)
	testClaimTaskHooks = &claimTaskTestHooks{beforePhase3Modify: func() {
		if err := db.For(stateFile).Modify(func(state *models.State) error {
			state.FindTask("old").AssignedTo = testhelpers.StringPtr("coder-1")
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}}
	t.Cleanup(func() { testClaimTaskHooks = nil })
	if _, err := ClaimTask(root, "new", "coder-1"); err != nil {
		t.Fatal(err)
	}
	if old := readClaimStateForTest(t, stateFile).FindTask("old"); old.AssignedTo != nil || old.LeaseExpires != nil {
		t.Fatal("finalization used stale reverse assignments")
	}
}

func TestClaimTask_ConcurrentClaimsKeepSingleAssignment(t *testing.T) {
	t.Parallel()
	root, stateFile, state := assignmentClaimFixture(t, models.TaskStatusRejected)
	state.Tasks = append(state.Tasks, testhelpers.BuildTaskByStatus("new-2", models.TaskStatusReady, time.Now().UTC()))
	testhelpers.WriteInitialState(t, stateFile, state)
	start := make(chan struct{})
	results := make(chan error, 2)
	var workers sync.WaitGroup
	for _, id := range []string{"new", "new-2"} {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			_, err := ClaimTask(root, id, "coder-1")
			results <- err
		}()
	}
	close(start)
	workers.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("successful claims = %d, want exactly one", successes)
	}
	after := readClaimStateForTest(t, stateFile)
	assignments := 0
	for _, task := range after.Tasks {
		if task.AssignedTo != nil && *task.AssignedTo == "coder-1" {
			assignments++
			if task.ID != *after.Agents["coder-1"].CurrentTask {
				t.Fatalf("assignment %s disagrees with CurrentTask", task.ID)
			}
		}
	}
	if assignments != 1 {
		t.Fatalf("assignments = %d, want exactly one", assignments)
	}
}

func assignmentClaimFixture(t *testing.T, oldStatus models.TaskStatus) (string, string, *models.State) {
	t.Helper()
	root := t.TempDir()
	testhelpers.SetupTestGitRepo(t, root)
	stateFile, _ := testhelpers.SetupLizaDir(t, root)
	state := testhelpers.CreateValidState()
	registerClaimTaskTestAgents(state)
	now := time.Now().UTC()
	old := testhelpers.BuildTaskByStatus("old", oldStatus, now)
	old.AssignedTo = testhelpers.StringPtr("coder-1")
	lease := now.Add(time.Hour)
	old.LeaseExpires = &lease
	base, err := git.New(root).CreateWorktree("old", "integration")
	if err != nil {
		t.Fatal(err)
	}
	old.BaseCommit = &base
	state.Tasks = []models.Task{testhelpers.BuildTaskByStatus("new", models.TaskStatusReady, now), old}
	return root, stateFile, state
}
