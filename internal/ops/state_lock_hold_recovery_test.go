package ops

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	gitpkg "github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func stateLockRecoveryFixture(t *testing.T, publishLater bool) (string, string, *db.Blackboard, models.AgentAuthority) {
	t.Helper()
	root, path, _ := metadataLifecycleFixture(t, "assess-blocked")
	testhelpers.SetupPipelineConfig(t, root)
	bb := db.For(path)
	testhelpers.RegisterTestAgent(t, bb, "orchestrator-1", "orchestrator")
	if err := bb.Modify(func(state *models.State) error {
		task := state.FindTask("target")
		task.RolePair = "coding-pair"
		task.Worktree, task.BaseCommit, task.AssignedTo, task.LeaseExpires = nil, nil, nil, nil
		task.Iteration = 4
		reason := "state acquisition timed out"
		task.BlockedReason = &reason
		task.BlockedQuestions = []string{"Resume when contention clears?"}
		task.History = []models.TaskHistoryEntry{{Time: task.Created, Event: models.TaskEventBlocked, Reason: &reason}}
		installStateLockHold(state, task)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if publishLater {
		if err := bb.Modify(func(state *models.State) error { state.Config.DiagnosticLogging = true; return nil }); err != nil {
			t.Fatal(err)
		}
	}
	return root, path, bb, models.AgentAuthority{ID: "orchestrator-1", Generation: testhelpers.TestAgentGeneration}
}

func TestStateLockHoldRecoveryContinuation(t *testing.T) {
	t.Parallel()
	root, path, bb, authority := stateLockRecoveryFixture(t, false)
	before, _ := os.ReadFile(path)
	if count, err := RecoverStateLockHolds(context.Background(), bb, root, authority); err != nil || count != 0 {
		t.Fatalf("own publication recovered: %d %v", count, err)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("no-work recovery wrote state")
	}
	if err := bb.Modify(func(state *models.State) error { state.Config.DiagnosticLogging = true; return nil }); err != nil {
		t.Fatal(err)
	}
	if count, err := RecoverStateLockHolds(context.Background(), bb, root, authority); err != nil || count != 1 {
		t.Fatalf("later publication did not recover: %d %v", count, err)
	}
	state := readStateForTest(t, path)
	task := state.FindTask("target")
	if task.Status != models.TaskStatusReady || task.AssignedTo != nil || task.LeaseExpires != nil || task.Iteration != 4 || !task.Continuation || task.StateLockHold != nil || task.BlockedReason != nil {
		t.Fatalf("unexpected recovery state: %+v", task)
	}
	if task.History[len(task.History)-1].Event != models.TaskEventUnblocked || task.Lifecycle == nil || len(task.Lifecycle.Receipts) != 1 {
		t.Fatal("normal unblock audit/receipt missing")
	}
	before, _ = os.ReadFile(path)
	if count, err := RecoverStateLockHolds(context.Background(), bb, root, authority); err != nil || count != 0 {
		t.Fatalf("duplicate recovery: %d %v", count, err)
	}
	after, _ = os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("completed recovery rewrote state")
	}
}

func TestStateLockHoldRecoveryRefusalsDoNotRepeat(t *testing.T) {
	t.Parallel()
	for _, guard := range []string{"paused", "stopped", "checkpoint", "human", "repair", "rca", "preparation", "dependency", "provider", "worktree"} {
		t.Run(guard, func(t *testing.T) {
			t.Parallel()
			root, path, bb, authority := stateLockRecoveryFixture(t, true)
			if err := bb.Modify(func(state *models.State) error {
				task := state.FindTask("target")
				switch guard {
				case "paused":
					state.Config.Mode = models.SystemModePaused
				case "stopped":
					state.Config.Mode = models.SystemModeStopped
				case "checkpoint":
					state.Sprint.Status = models.SprintStatusCheckpoint
				case "human":
					task.History = append(task.History, models.TaskHistoryEntry{Time: time.Now().UTC(), Event: models.TaskEventOrchestratorAssessment, Extra: map[string]any{models.AwaitingHumanExtraKey: "Inspect the lock holder"}})
				case "repair":
					task.RepairRequest = &models.RepairRequest{Operation: "repair", Target: "target"}
				case "rca":
					task.RejectionRCA = &models.RejectionRCARecord{}
				case "preparation":
					task.Lifecycle = &models.TaskLifecycle{Preparation: &models.LifecyclePreparation{}}
				case "dependency":
					task.DependsOn = []string{"missing"}
				case "provider":
					task.ProviderDependencies = []models.ProviderDependency{{ProviderTask: "missing", Transition: "missing", Outputs: []int{0}}}
				case "worktree":
					task.Worktree = testhelpers.StringPtr(".worktrees/target")
					task.BaseCommit = testhelpers.StringPtr("missing-base")
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if count, err := RecoverStateLockHolds(context.Background(), bb, root, authority); err != nil || count != 0 {
				t.Fatalf("refusal recovered: %d %v", count, err)
			}
			state := readStateForTest(t, path)
			if state.FindTask("target").Status != models.TaskStatusBlocked || state.FindTask("target").StateLockHold.RefusedReason == "" || CountActionableBlockedTasks(state) != 0 {
				t.Fatal("refusal failed to retain block and suppress provider wake")
			}
			before, _ := os.ReadFile(path)
			if count, err := RecoverStateLockHolds(context.Background(), bb, root, authority); err != nil || count != 0 {
				t.Fatalf("repeat refusal: %d %v", count, err)
			}
			after, _ := os.ReadFile(path)
			if string(before) != string(after) {
				t.Fatal("unchanged refusal rewrote state")
			}
			if err := bb.Modify(func(state *models.State) error {
				state.Config.DiagnosticLogging = !state.Config.DiagnosticLogging
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			before, _ = os.ReadFile(path)
			if count, err := RecoverStateLockHolds(context.Background(), bb, root, authority); err != nil || count != 0 {
				t.Fatalf("unrelated publication retried refusal: %d %v", count, err)
			}
			after, _ = os.ReadFile(path)
			if string(before) != string(after) {
				t.Fatal("unrelated publication caused repeated refusal write")
			}
		})
	}
}

func TestStateLockHoldRecoveryAuthorityAndEpisode(t *testing.T) {
	t.Parallel()
	root, path, bb, authority := stateLockRecoveryFixture(t, true)
	before, _ := os.ReadFile(path)
	stale := authority
	stale.Generation = "retired"
	if _, err := RecoverStateLockHolds(context.Background(), bb, root, stale); !IsAgentAuthorityError(err) {
		t.Fatalf("stale authority accepted: %v", err)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("stale authority wrote state")
	}
	if err := bb.Modify(func(state *models.State) error {
		state.FindTask("target").BlockedQuestions = []string{"Changed blocker"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	before, _ = os.ReadFile(path)
	if count, err := RecoverStateLockHolds(context.Background(), bb, root, authority); err != nil || count != 0 {
		t.Fatalf("changed blocker recovered: %d %v", count, err)
	}
	after, _ = os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("stale tag wrote state")
	}
}

func TestStateLockHoldRecoveryRechecksWorktreeHealth(t *testing.T) {
	t.Parallel()
	root, path, bb, authority := stateLockRecoveryFixture(t, true)
	testhelpers.SetupTestGitRepo(t, root)
	g := gitpkg.New(root)
	base, err := g.GetCommitSHA("integration")
	if err != nil {
		t.Fatal(err)
	}
	if err := bb.Modify(func(state *models.State) error {
		task := state.FindTask("target")
		task.Worktree = testhelpers.StringPtr(g.GetWorktreeRelPath(task.ID))
		task.BaseCommit = &base
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if count, err := RecoverStateLockHolds(context.Background(), bb, root, authority); err != nil || count != 0 {
		t.Fatalf("missing worktree recovered: %d %v", count, err)
	}
	if _, err := g.CreateWorktree("target", "integration"); err != nil {
		t.Fatal(err)
	}
	if count, err := RecoverStateLockHolds(context.Background(), bb, root, authority); err != nil || count != 1 {
		t.Fatalf("healthy preserved worktree did not recover: %d %v", count, err)
	}
	if task := readStateForTest(t, path).FindTask("target"); task.Worktree == nil || *task.Worktree != g.GetWorktreeRelPath(task.ID) || task.BaseCommit == nil || *task.BaseCommit != base {
		t.Fatal("recovery changed preserved Git provenance")
	}
}

func TestStateLockHoldAssessmentExplicitAdoptionAndPayloadIdentity(t *testing.T) {
	t.Parallel()
	root, path, bb, _ := stateLockRecoveryFixture(t, false)
	if err := bb.Modify(func(state *models.State) error { state.FindTask("target").StateLockHold = nil; return nil }); err != nil {
		t.Fatal(err)
	}
	state := readStateForTest(t, path)
	request := LifecycleRequestOptions{RequestID: "assessment-original", ExpectedTransition: models.TaskTransitionID(state.FindTask("target"))}
	if _, err := AssessBlockedWithOptions(root, "target", "wait for healthy publication", "orchestrator-1", AssessBlockedOptions{Request: request}); err != nil {
		t.Fatal(err)
	}
	if readStateForTest(t, path).FindTask("target").StateLockHold != nil {
		t.Fatal("prose implicitly adopted hold")
	}
	before, _ := os.ReadFile(path)
	if _, err := AssessBlockedWithOptions(root, "target", "wait for healthy publication", "orchestrator-1", AssessBlockedOptions{Request: request, StateLockTimeout: true}); err == nil {
		t.Fatal("same request ID accepted changed typed payload")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("conflicting typed payload wrote state")
	}
	result, err := AssessBlockedWithOptions(root, "target", "wait for healthy publication", "orchestrator-1", AssessBlockedOptions{StateLockTimeout: true})
	if err != nil || result.Outcome != models.LifecycleCompleted {
		t.Fatalf("explicit adoption was suppressed: %+v %v", result, err)
	}
	state = readStateForTest(t, path)
	task := state.FindTask("target")
	if !models.CurrentStateLockHold(task) || task.StateLockHold.AfterSequence != state.MutationSequence || CountActionableBlockedTasks(state) != 0 {
		t.Fatal("adoption did not bind its own publication/episode/blocker")
	}
	result, err = AssessBlockedWithOptions(root, "target", "wait for healthy publication", "orchestrator-1", AssessBlockedOptions{StateLockTimeout: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != models.LifecycleNoChange {
		t.Fatalf("equivalent hold adoption mutated: %+v", result)
	}
	if _, err := AssessBlockedWithOptions(root, "target", "changed blocker", "orchestrator-1", AssessBlockedOptions{Reason: "product requirement needs clarification", Questions: []string{"Which requirement?"}}); err != nil {
		t.Fatal(err)
	}
	if readStateForTest(t, path).FindTask("target").StateLockHold != nil {
		t.Fatal("typed hold silently carried onto changed blocker")
	}
}

func TestStateLockHoldRecoveryWithSatisfiedProviderDependencies(t *testing.T) {
	t.Parallel()
	root, path, bb, authority := stateLockRecoveryFixture(t, true)
	if err := bb.Modify(func(state *models.State) error {
		provider := providerOpsTask("arch", "architecture-pair", models.TaskStatusMerged)
		provider.Output = []models.OutputEntry{providerOpsOutput()}
		provider.TransitionsExecuted = map[string]bool{providerOpsTransition: true}
		child := providerOpsTask("arch-cp-0", "code-planning-pair", models.TaskStatusMerged)
		child.ParentTask = &provider.ID
		state.Tasks = append(state.Tasks, provider, child)
		state.FindTask("target").ProviderDependencies = providerOpsDependency("arch", 0)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if count, err := RecoverStateLockHolds(context.Background(), bb, root, authority); err != nil || count != 1 {
		t.Fatalf("satisfied provider route failed recovery: %d %v", count, err)
	}
	if task := readStateForTest(t, path).FindTask("target"); task.Status != models.TaskStatusReady || len(task.ProviderDependencies) != 1 {
		t.Fatal("recovery lost provider intent")
	}
}

func TestStateLockHoldRecoveryUnblockRefusalRecordedOnce(t *testing.T) {
	t.Parallel()
	root, path, bb, authority := stateLockRecoveryFixture(t, true)
	if err := bb.Modify(func(state *models.State) error {
		task := state.FindTask("target")
		boundary := models.TaskTransitionID(task)
		request, err := NewLifecycleRequest("unblock-task", task, authority.ID, &authority, LifecycleRequestOptions{RequestID: "state-lock-hold-" + boundary, ExpectedTransition: boundary}, "conflicting recovery payload")
		if err != nil {
			return err
		}
		task.Lifecycle = &models.TaskLifecycle{Receipts: []models.LifecycleReceipt{{LifecycleIdentity: request, Sequence: 1, TransitionID: boundary}}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if count, err := RecoverStateLockHolds(context.Background(), bb, root, authority); err != nil || count != 0 {
		t.Fatalf("conflicting unblock request recovered: %d %v", count, err)
	}
	state := readStateForTest(t, path)
	if state.FindTask("target").Status != models.TaskStatusBlocked || state.FindTask("target").StateLockHold.RefusedReason != "unblock_domain_refused" || CountActionableBlockedTasks(state) != 0 {
		t.Fatal("actual unblock refusal did not suppress wake")
	}
	before, _ := os.ReadFile(path)
	if count, err := RecoverStateLockHolds(context.Background(), bb, root, authority); err != nil || count != 0 {
		t.Fatalf("repeated domain refusal: %d %v", count, err)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("unchanged domain refusal rewrote state")
	}
	if err := bb.Modify(func(state *models.State) error { state.FindTask("target").Lifecycle.Receipts = nil; return nil }); err != nil {
		t.Fatal(err)
	}
	if count, err := RecoverStateLockHolds(context.Background(), bb, root, authority); err != nil || count != 1 {
		t.Fatalf("material repair did not resume recovery: %d %v", count, err)
	}
}

func TestStateLockHoldMarkBlockedInstallsOwnPublication(t *testing.T) {
	t.Parallel()
	root, path, bb, _ := stateLockRecoveryFixture(t, false)
	testhelpers.RegisterTestAgent(t, bb, "coder-1", "coder")
	if err := bb.Modify(func(state *models.State) error {
		task := state.FindTask("target")
		task.Status = models.TaskStatusImplementing
		task.StateLockHold = nil
		task.AssignedTo = testhelpers.StringPtr("coder-1")
		expires := time.Now().UTC().Add(time.Hour)
		task.LeaseExpires = &expires
		task.History = nil
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	authority := models.AgentAuthority{ID: "coder-1", Generation: testhelpers.TestAgentGeneration}
	result, err := MarkBlockedWithAuthority(root, "target", "state lock acquisition timed out", []string{"Resume after contention clears?"}, authority, MarkBlockedOptions{StateLockTimeout: true})
	if err != nil || result.Outcome != models.LifecycleCompleted {
		t.Fatalf("typed block failed: %+v %v", result, err)
	}
	state := readStateForTest(t, path)
	task := state.FindTask("target")
	if !models.CurrentStateLockHold(task) || task.StateLockHold.AfterSequence != state.MutationSequence || task.AssignedTo != nil || task.Iteration != 4 || CountActionableBlockedTasks(state) != 0 {
		t.Fatal("typed block was not bound to its own successful publication")
	}
}
