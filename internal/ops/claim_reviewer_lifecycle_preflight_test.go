package ops

import (
	"bytes"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestReviewerClaimLifecyclePreflightRepairAndReplay(t *testing.T) {
	shell := testhelpers.ResolveBashForScripts(t)
	f := newAssignmentPreflightFixture(t, models.TaskStatusReadyForReview)
	authority := models.AgentAuthority{ID: "code-reviewer-1", Generation: lifecycleGenerationA}
	setupCount := filepath.Join(t.TempDir(), "setup-count")
	probeCount := filepath.Join(t.TempDir(), "probe-count")
	if err := f.bb.Modify(func(state *models.State) error {
		setup := "printf 'setup\\n' >> " + testhelpers.ShellArg(setupCount)
		state.Config.PostWorktreeCmd = &setup
		state.FindTask("task-1").ValidationPrerequisites[0].Probes = [][]string{{
			shell, "-c", `printf 'probe\n' >> "$1"`, "probe", probeCount,
		}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	input := ClaimReviewerTaskInput{
		ProjectRoot: f.root, TaskID: "task-1", AgentID: authority.ID, Authority: &authority,
		Session: f.session(false), RequestOptions: ownershipRequestOptions(t, f.bb, "missing-prerequisite"),
	}
	_, err := ClaimReviewerTask(input)
	requireAssignmentPreflightError(t, err)
	failed := f.state(t)
	task := failed.FindTask("task-1")
	if task.Status != models.TaskStatusReadyForReview || task.ReviewingBy != nil || task.Lifecycle == nil ||
		task.Lifecycle.Preparation != nil || task.Lifecycle.CompletionSequence != 0 || len(task.Lifecycle.Receipts) != 0 {
		t.Fatalf("failed preflight assigned work or retained its preparation: %+v", task)
	}
	if models.TaskTransitionID(task) == input.RequestOptions.ExpectedTransition {
		t.Fatal("failed preflight did not retire the original request")
	}
	if record := failed.ValidationReadiness[authority.ID][task.ID]; record.Result != "failed" {
		t.Fatalf("failed preflight did not preserve readiness evidence: %+v", record)
	}
	statePath := paths.New(f.root).StatePath()
	before := ownershipStateBytes(t, statePath)
	_, err = ClaimReviewerTask(input)
	var stale *LifecycleError
	if !errors.As(err, &stale) || stale.Outcome.Outcome != models.LifecycleStateChanged || stale.Outcome.SafeAction != "requery" {
		t.Fatalf("retired claim request = %v, want STATE_CHANGED/requery", err)
	}
	if !bytes.Equal(before, ownershipStateBytes(t, statePath)) {
		t.Fatal("retired claim request changed state")
	}
	input.Session = f.session(true)
	input.RequestOptions = ownershipRequestOptions(t, f.bb, "repaired-prerequisite")
	first, err := ClaimReviewerTask(input)
	if err != nil || first == nil || first.Outcome != models.LifecycleCompleted {
		t.Fatalf("repaired reviewer claim = %+v, %v", first, err)
	}
	completed := f.state(t)
	task = completed.FindTask("task-1")
	if task.Status != models.TaskStatusReviewing || task.ReviewingBy == nil || *task.ReviewingBy != authority.ID ||
		task.Lifecycle.Preparation != nil || len(task.Lifecycle.Receipts) != 1 || task.Lifecycle.Receipts[0].RequestID != input.RequestOptions.RequestID {
		t.Fatalf("claim did not commit one matching receipt and owner: %+v", task)
	}
	claims := 0
	for _, event := range task.History {
		if event.Event == models.TaskEventClaimed && event.Agent != nil && *event.Agent == authority.ID {
			claims++
		}
	}
	if claims != 1 || completed.ValidationReadiness[authority.ID][task.ID].Result != "passed" {
		t.Fatal("claim did not preserve successful preflight and exactly one reviewer claim event")
	}
	before = ownershipStateBytes(t, statePath)
	// Receipt replay must precede even session preparation errors and must not
	// rerun the configured setup or either prerequisite probe.
	input.Session = &ValidationSession{PreparationError: errors.New("session unavailable on replay")}
	replay, err := ClaimReviewerTask(input)
	if err != nil || replay == nil || replay.Outcome != models.LifecycleAlreadyCompleted || replay.CompletedTransitionID != first.CompletedTransitionID {
		t.Fatalf("reviewer replay = %+v, %v", replay, err)
	}
	if !bytes.Equal(before, ownershipStateBytes(t, statePath)) {
		t.Fatal("reviewer replay rewrote claim or readiness evidence")
	}
	for filename, want := range map[string]string{setupCount: "setup\nsetup\n", probeCount: "probe\n"} {
		got, err := os.ReadFile(filename)
		if err != nil || string(got) != want {
			t.Fatalf("external work count in %s = %q, %v; want %q", filename, got, err, want)
		}
	}
}

func TestReviewerClaimLifecyclePreflightRejectsGenerationTurnover(t *testing.T) {
	f := newAssignmentPreflightFixture(t, models.TaskStatusReadyForReview)
	authority := models.AgentAuthority{ID: "code-reviewer-1", Generation: lifecycleGenerationA}
	listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err := listener.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := f.bb.Modify(func(state *models.State) error {
		state.FindTask("task-1").ValidationPrerequisites[0].Probes = [][]string{{binary, "-test.run=^TestValidationPreflightProbeHandshake$"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	session := f.session(true)
	session.Environment = append(session.Environment, "OPS_PREFLIGHT_HANDSHAKE="+listener.Addr().String())
	input := ClaimReviewerTaskInput{
		ProjectRoot: f.root, TaskID: "task-1", AgentID: authority.ID, Authority: &authority,
		Session: session, RequestOptions: ownershipRequestOptions(t, f.bb, "interrupted-preflight"),
	}
	done := make(chan struct{})
	var result *ClaimReviewerTaskResult
	var claimErr error
	go func() {
		result, claimErr = ClaimReviewerTask(input)
		close(done)
	}()
	var connection net.Conn
	defer func() {
		if connection != nil {
			connection.Close()
		}
		listener.Close()
		select {
		case <-done:
		case <-time.After(15 * time.Second):
			t.Error("reviewer preflight did not terminate")
		}
	}()
	connection, err = listener.AcceptTCP()
	if err != nil {
		t.Fatalf("reviewer probe did not reach its handshake: %v", err)
	}
	if err := connection.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	statePath := paths.New(f.root).StatePath()
	lock := filelock.New(claimTaskWorktreeLockPath(statePath, "task-1")).WithTimeout(50 * time.Millisecond)
	if err := lock.WithLockOperation("test-reviewer-preflight-lock", func() error { return nil }); !filelock.IsLockErrorType(err, filelock.LockErrorTimeout) {
		t.Fatalf("reviewer preflight did not hold the task worktree lock: %v", err)
	}
	if err := f.bb.Modify(func(state *models.State) error {
		task := state.FindTask("task-1")
		if task.Lifecycle == nil || task.Lifecycle.Preparation == nil || task.Lifecycle.Preparation.RequestID != input.RequestOptions.RequestID || task.ReviewingBy != nil {
			return errors.New("probe started without an unassigned durable claim preparation")
		}
		agent := state.Agents[authority.ID]
		agent.Generation = lifecycleGenerationB
		state.Agents[authority.ID] = agent
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	before := ownershipStateBytes(t, statePath)
	if _, err := connection.Write([]byte{1}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("reviewer preflight did not finish")
	}
	if result != nil || !IsAgentAuthorityError(claimErr) {
		t.Fatalf("stale reviewer preflight = %+v, %v", result, claimErr)
	}
	if !bytes.Equal(before, ownershipStateBytes(t, statePath)) {
		t.Fatal("stale preflight changed the replacement generation's state")
	}
}
