package ops

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/sessionvalidation"
	"github.com/liza-mas/liza/internal/testhelpers"
)

type assignmentPreflightFixture struct {
	root      string
	bb        *db.Blackboard
	authority models.AgentAuthority
}

func newAssignmentPreflightFixture(t *testing.T, status models.TaskStatus) assignmentPreflightFixture {
	t.Helper()
	root := t.TempDir()
	testhelpers.SetupTestGitRepo(t, root)
	stateFile, _ := testhelpers.SetupLizaDir(t, root)
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", status, time.Now().UTC())
	task.Validation = []string{"canonical check"}
	task.ValidationPrerequisites = []models.ValidationPrerequisite{{Command: "canonical check", Env: []string{"VALIDATION_TEST_REQUIRED"}}}
	if status != models.TaskStatusReady {
		wt, head := createClaimReviewWorktree(t, root, task.ID)
		base := testhelpers.MustGit(t, root, "rev-parse", "integration")
		task.Worktree = &wt
		task.BaseCommit = &base
		if task.ReviewCommit != nil {
			task.ReviewCommit = &head
		}
	}
	state.Tasks = []models.Task{task}
	state.Config.AgentTools = map[string]models.AgentToolConfig{"fixture": {ValidationExecution: "local"}}
	for _, id := range []string{"coder-1", "code-reviewer-1"} {
		role := models.RoleCoder
		if id == "code-reviewer-1" {
			role = models.RoleCodeReviewer
		}
		agent := testhelpers.RegisteredTestAgent(role)
		agent.Status = models.AgentStatusIdle
		agent.Generation = lifecycleGenerationA
		agent.Provider = "fixture"
		state.Agents[id] = agent
	}
	return assignmentPreflightFixture{root: root, bb: testhelpers.WriteInitialState(t, stateFile, state), authority: models.AgentAuthority{ID: "coder-1", Generation: lifecycleGenerationA}}
}

func (f assignmentPreflightFixture) session(present bool) *ValidationSession {
	env := []string{"PATH=/usr/bin:/bin"}
	if present {
		env = append(env, "VALIDATION_TEST_REQUIRED=fixture-value")
	}
	return &ValidationSession{Environment: env, Execution: "local"}
}

func (f assignmentPreflightFixture) state(t *testing.T) *models.State {
	t.Helper()
	state, err := f.bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func requireAssignmentPreflightError(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, sessionvalidation.ErrPreflight) {
		t.Fatalf("expected validation preflight failure, got %v", err)
	}
}

func TestClaimTaskValidationPreflightBeforeAssignment(t *testing.T) {
	for _, status := range []models.TaskStatus{models.TaskStatusReady, models.TaskStatusRejected, models.TaskStatusIntegrationFailed} {
		t.Run(string(status), func(t *testing.T) {
			f := newAssignmentPreflightFixture(t, status)
			before := f.state(t).FindTask("task-1")
			_, err := ClaimTaskWithAuthority(f.root, "task-1", f.authority, f.session(false))
			requireAssignmentPreflightError(t, err)
			after := f.state(t)
			if task := after.FindTask("task-1"); task.Status != before.Status || task.Iteration != before.Iteration {
				t.Fatalf("failed preflight changed lifecycle: %#v", task)
			}
			if after.Agents[f.authority.ID].Status == models.AgentStatusWorking {
				t.Fatal("failed preflight assigned executable work")
			}
			if record := after.ValidationReadiness[f.authority.ID]["task-1"]; record.Result != "failed" || record.Code == "" {
				t.Fatalf("missing safe failure record: %#v", record)
			}
			if _, err := ClaimTaskWithAuthority(f.root, "task-1", f.authority, f.session(true)); err != nil {
				t.Fatalf("repaired environment failed: %v", err)
			}
			if task := f.state(t).FindTask("task-1"); task.Status != models.TaskStatusImplementing {
				t.Fatalf("repaired session did not claim: %s", task.Status)
			}
		})
	}
}

func TestResumeValidationPreflightPreservesWorkAndReleasesOwnership(t *testing.T) {
	for _, handoff := range []bool{false, true} {
		t.Run(map[bool]string{false: "owned", true: "handoff"}[handoff], func(t *testing.T) {
			f := newAssignmentPreflightFixture(t, models.TaskStatusImplementing)
			if err := f.bb.Modify(func(s *models.State) error { s.FindTask("task-1").HandoffPending = handoff; return nil }); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(f.root, ".worktrees/task-1/unfinished.txt")
			if err := os.WriteFile(path, []byte("preserve unfinished work"), 0600); err != nil {
				t.Fatal(err)
			}
			var err error
			if handoff {
				_, err = ResumeHandoff(ResumeHandoffInput{ProjectRoot: f.root, AgentID: f.authority.ID, Authority: &f.authority, Session: f.session(false)})
			} else {
				_, err = ResumeOwnedTask(ResumeOwnedTaskInput{ProjectRoot: f.root, AgentID: f.authority.ID, Authority: &f.authority, Session: f.session(false)})
			}
			requireAssignmentPreflightError(t, err)
			task := f.state(t).FindTask("task-1")
			if task.Status != models.TaskStatusReady || task.AssignedTo != nil || task.Worktree == nil {
				t.Fatalf("ownership was not coherently released: %#v", task)
			}
			if content, err := os.ReadFile(path); err != nil || string(content) != "preserve unfinished work" {
				t.Fatalf("lost unfinished work: %v", err)
			}
			// Existing recovery requires a clean preserved worktree. The operator
			// commits the surviving WIP before allowing another executable claim.
			testhelpers.MustGit(t, filepath.Dir(path), "add", "unfinished.txt")
			testhelpers.MustGit(t, filepath.Dir(path), "commit", "-m", "Preserve interrupted work for recovery")
			if _, err := ClaimTaskWithAuthority(f.root, "task-1", f.authority, f.session(true)); err != nil {
				t.Fatalf("repaired reclaim failed: %v", err)
			}
			if content, err := os.ReadFile(path); err != nil || string(content) != "preserve unfinished work" {
				t.Fatalf("reclaim lost unfinished work: %v", err)
			}
		})
	}
}

func TestReviewerValidationPreflightBeforeAssignmentAndReclaim(t *testing.T) {
	for _, path := range []string{"claim", "await-resubmission", "early-resubmission"} {
		t.Run(path, func(t *testing.T) {
			f := newAssignmentPreflightFixture(t, models.TaskStatusReadyForReview)
			authority := models.AgentAuthority{ID: "code-reviewer-1", Generation: lifecycleGenerationA}
			if path == "early-resubmission" {
				if err := f.bb.Modify(func(s *models.State) error {
					task := s.FindTask("task-1")
					task.History = append(task.History, models.TaskHistoryEntry{Time: time.Now().UTC(), Event: models.TaskEventRejected, Agent: &authority.ID})
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			before := f.state(t).FindTask("task-1")
			call := func(present bool) error {
				if path == "claim" {
					_, err := ClaimReviewerTask(ClaimReviewerTaskInput{ProjectRoot: f.root, AgentID: authority.ID, Authority: &authority, Session: f.session(present)})
					return err
				}
				t.Setenv(brand.EnvName("AGENT_ID"), authority.ID)
				t.Setenv(brand.EnvName("AGENT_GENERATION"), authority.Generation)
				t.Setenv("VALIDATION_TEST_REQUIRED", "")
				if present {
					t.Setenv("VALIDATION_TEST_REQUIRED", "fixture-value")
				}
				if path == "early-resubmission" {
					_, err := AwaitResubmissionWithAuthority(context.Background(), f.root, "task-1", authority, time.Minute)
					return err
				}
				if err := acquireReviewOwnership(f.bb, authority.ID, "task-1", &authority, time.Minute); err != nil {
					t.Fatal(err)
				}
				resolver, _, err := loadResolver(f.root)
				if err != nil {
					t.Fatal(err)
				}
				_, err = reclaimForReview(f.root, f.bb, "task-1", authority.ID, &authority, resolver, "coding-pair")
				return err
			}
			requireAssignmentPreflightError(t, call(false))
			task := f.state(t).FindTask("task-1")
			if task.Status != models.TaskStatusReadyForReview || task.ReviewingBy != nil || *task.ReviewCommit != *before.ReviewCommit {
				t.Fatalf("failed review preflight changed reviewed work: %#v", task)
			}
			if err := call(true); err != nil {
				t.Fatalf("repaired reviewer rejected: %v", err)
			}
			if task := f.state(t).FindTask("task-1"); task.Status != models.TaskStatusReviewing || task.ReviewingBy == nil || *task.ReviewingBy != authority.ID {
				t.Fatalf("reviewer not assigned: %#v", task)
			}
		})
	}
}

func TestValidationPreflightRejectsStaleStateAndNeverReusesSuccess(t *testing.T) {
	f := newAssignmentPreflightFixture(t, models.TaskStatusImplementing)
	p, err := PrepareValidationPreflight(f.root, "task-1", f.authority.ID, "", f.session(true))
	if err != nil {
		t.Fatal(err)
	}
	if p.SessionScope() == "" {
		t.Fatal("missing retained-session identity")
	}
	if err := f.bb.Modify(func(s *models.State) error { s.FindTask("task-1").Validation[0] = "edited"; return nil }); err != nil {
		t.Fatal(err)
	}
	requireAssignmentPreflightError(t, p.CheckCurrent(f.state(t)))
	_, err = PrepareValidationPreflight(f.root, "task-1", f.authority.ID, "", f.session(true))
	requireAssignmentPreflightError(t, err)
}

func TestValidationPreflightLaunchRejectsPendingHandoff(t *testing.T) {
	f := newAssignmentPreflightFixture(t, models.TaskStatusImplementing)
	p, err := PrepareValidationPreflight(f.root, "task-1", f.authority.ID, "", f.session(true))
	if err != nil {
		t.Fatal(err)
	}
	if err := p.CheckLaunchCurrent(f.state(t)); err != nil {
		t.Fatal(err)
	}
	if err := f.bb.Modify(func(s *models.State) error { s.FindTask("task-1").HandoffPending = true; return nil }); err != nil {
		t.Fatal(err)
	}
	requireAssignmentPreflightError(t, p.CheckLaunchCurrent(f.state(t)))
	p, err = PrepareValidationPreflight(f.root, "task-1", f.authority.ID, "", f.session(true))
	if err != nil {
		t.Fatal(err)
	}
	requireAssignmentPreflightError(t, p.CheckLaunchCurrent(f.state(t)))
}

func TestValidationPreflightOperatorAssignmentRejectedWithoutMutation(t *testing.T) {
	f := newAssignmentPreflightFixture(t, models.TaskStatusBlocked)
	before := f.state(t).FindTask("task-1")
	_, err := UnblockTask(f.root, "task-1", "coder-1", "repaired", "orchestrator-1")
	if err == nil || !strings.Contains(err.Error(), "without --assign-to") {
		t.Fatalf("operator assignment accepted: %v", err)
	}
	if task := f.state(t).FindTask("task-1"); task.Status != before.Status || optionalValue(task.AssignedTo) != optionalValue(before.AssignedTo) {
		t.Fatalf("operator rejection changed ownership: %#v", task)
	}
}

func TestValidationPreflightCooldownTracksActualContext(t *testing.T) {
	f := newAssignmentPreflightFixture(t, models.TaskStatusImplementing)
	_, err := PrepareValidationPreflight(f.root, "task-1", f.authority.ID, "", f.session(false))
	requireAssignmentPreflightError(t, err)
	state := f.state(t)
	task := state.FindTask("task-1")
	if !ValidationRetryPending(f.root, state, task, f.authority.ID, f.session(false)) {
		t.Fatal("unchanged failure did not enter cooldown")
	}
	if ValidationRetryPending(f.root, state, task, f.authority.ID, f.session(true)) {
		t.Fatal("changed environment retained cooldown")
	}
	forced := f.session(false)
	forced.ForceCheck = true
	if ValidationRetryPending(f.root, state, task, f.authority.ID, forced) {
		t.Fatal("explicit recheck retained cooldown")
	}
	state.Config.AgentTools["other"] = models.AgentToolConfig{ValidationExecution: "local"}
	if ValidationRetryPending(f.root, state, task, f.authority.ID, f.session(false)) {
		t.Fatal("changed config retained cooldown")
	}
	state = f.state(t)
	task = state.FindTask("task-1")
	testhelpers.MustGit(t, filepath.Join(f.root, *task.Worktree), "commit", "--allow-empty", "-m", "new worktree identity")
	if ValidationRetryPending(f.root, state, task, f.authority.ID, f.session(false)) {
		t.Fatal("changed commit retained cooldown")
	}
}

func TestReviewerValidationPreflightCooldownDoesNotStarveOtherWork(t *testing.T) {
	f := newAssignmentPreflightFixture(t, models.TaskStatusReadyForReview)
	authority := models.AgentAuthority{ID: "code-reviewer-1", Generation: lifecycleGenerationA}
	input := ClaimReviewerTaskInput{ProjectRoot: f.root, AgentID: authority.ID, Authority: &authority, Session: f.session(false)}
	_, err := ClaimReviewerTask(input)
	requireAssignmentPreflightError(t, err)
	if err := f.bb.Modify(func(s *models.State) error {
		other := testhelpers.BuildTaskByStatus("other", models.TaskStatusReadyForReview, time.Now().UTC())
		other.Priority = 2
		other.Worktree = nil
		s.Tasks = append(s.Tasks, other)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	result, err := ClaimReviewerTask(input)
	if err != nil || result == nil || result.TaskID != "other" {
		t.Fatalf("cooling failed task starved unrelated work: %#v, %v", result, err)
	}
}

func TestReviewerValidationPreflightPreservesEarlierBoundaryFailure(t *testing.T) {
	for _, ready := range []bool{false, true} {
		t.Run(map[bool]string{false: "preflight fails", true: "preflight succeeds"}[ready], func(t *testing.T) {
			f := newAssignmentPreflightFixture(t, models.TaskStatusReadyForReview)
			authority := models.AgentAuthority{ID: "code-reviewer-1", Generation: lifecycleGenerationA}
			const brokenID = "boundary-broken"
			if err := f.bb.Modify(func(s *models.State) error {
				protected := s.FindTask("task-1")
				protected.Priority = 2
				broken := testhelpers.BuildTaskByStatus(brokenID, models.TaskStatusReadyForReview, time.Now().UTC())
				// Its commit metadata is valid, but its recorded worktree is gone.
				// That non-repairable boundary failure must survive B's preflight.
				broken.BaseCommit = protected.BaseCommit
				broken.ReviewCommit = protected.ReviewCommit
				lease := time.Now().Add(time.Minute)
				broken.LeaseExpires = &lease
				s.Tasks = append(s.Tasks, broken)
				coder := s.Agents["coder-1"]
				coder.Status = models.AgentStatusWorking
				id := brokenID
				coder.CurrentTask = &id
				s.Agents["coder-1"] = coder
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			assertBoundaryFailure := func() {
				t.Helper()
				state := f.state(t)
				broken := state.FindTask(brokenID)
				if broken.Status != models.TaskStatusIntegrationFailed || broken.AssignedTo != nil || broken.LeaseExpires != nil {
					t.Fatalf("earlier boundary failure was not persisted: %#v", broken)
				}
				marks := 0
				for _, entry := range broken.History {
					if entry.Event == models.TaskEventIntegrationFailed {
						marks++
					}
				}
				if marks != 1 || len(broken.FailedBy) != 1 || broken.FailedBy[0] != authority.ID {
					t.Fatalf("boundary failure must be recorded exactly once: marks=%d failed_by=%v", marks, broken.FailedBy)
				}
				if coder := state.Agents["coder-1"]; coder.CurrentTask != nil || coder.Status == models.AgentStatusWorking {
					t.Fatalf("boundary-failed task kept its executable owner: %#v", coder)
				}
			}
			input := ClaimReviewerTaskInput{ProjectRoot: f.root, AgentID: authority.ID, Authority: &authority, Session: f.session(ready)}
			result, err := ClaimReviewerTask(input)
			if !ready {
				requireAssignmentPreflightError(t, err)
				assertBoundaryFailure()
				if task := f.state(t).FindTask("task-1"); task.Status != models.TaskStatusReadyForReview || task.ReviewingBy != nil {
					t.Fatalf("failed preflight assigned protected candidate: %#v", task)
				}
				input.Session = f.session(true)
				result, err = ClaimReviewerTask(input)
			}
			if err != nil || result == nil || result.TaskID != "task-1" {
				t.Fatalf("protected candidate claim failed: %#v, %v", result, err)
			}
			assertBoundaryFailure()
		})
	}
}

func TestValidationPreflightDiscardsGenerationChangedDuringProbe(t *testing.T) {
	f := newAssignmentPreflightFixture(t, models.TaskStatusImplementing)
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
	if err := f.bb.Modify(func(s *models.State) error {
		s.FindTask("task-1").ValidationPrerequisites[0].Probes = [][]string{{binary, "-test.run=^TestValidationPreflightProbeHandshake$"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	session := f.session(true)
	session.Environment = append(session.Environment, "OPS_PREFLIGHT_HANDSHAKE="+listener.Addr().String())
	done := make(chan struct{})
	var probeErr error
	go func() {
		_, probeErr = PrepareValidationPreflight(f.root, "task-1", f.authority.ID, "", session)
		close(done)
	}()
	// Closing the connection releases a waiting child on every failure path;
	// wait for Prepare to reap it before temporary fixture state is removed.
	var connection net.Conn
	defer func() {
		if connection != nil {
			connection.Close()
		}
		listener.Close()
		select {
		case <-done:
		case <-time.After(15 * time.Second):
			t.Error("preflight child did not terminate during cleanup")
		}
	}()
	accepted, err := listener.AcceptTCP()
	if err != nil {
		t.Fatalf("probe readiness handshake failed: %v", err)
	}
	connection = accepted
	if err := connection.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	// This mutation completes while the subprocess waits, proving the probe
	// does not hold the blackboard lock. Its later result must not be published.
	if err := f.bb.Modify(func(s *models.State) error {
		agent := s.Agents[f.authority.ID]
		agent.Generation = lifecycleGenerationB
		s.Agents[f.authority.ID] = agent
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Write([]byte{1}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
		if !IsAgentAuthorityError(probeErr) {
			t.Fatalf("stale-generation probe accepted: %v", probeErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("probe did not finish")
	}
	if _, exists := f.state(t).ValidationReadiness[f.authority.ID]["task-1"]; exists {
		t.Fatal("stale-generation success was persisted")
	}
}

func TestValidationPreflightProbeHandshake(t *testing.T) {
	address := os.Getenv("OPS_PREFLIGHT_HANDSHAKE")
	if address == "" {
		return
	}
	connection, err := net.DialTimeout("tcp", address, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if err := connection.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(connection, make([]byte, 1)); err != nil {
		t.Fatal(err)
	}
}

func TestResumeValidationPreflightSetupFailureStillDegrades(t *testing.T) {
	f := newAssignmentPreflightFixture(t, models.TaskStatusImplementing)
	if err := f.bb.Modify(func(s *models.State) error { cmd := "exit 7"; s.Config.PostWorktreeCmd = &cmd; return nil }); err != nil {
		t.Fatal(err)
	}
	_, err := ResumeOwnedTask(ResumeOwnedTaskInput{ProjectRoot: f.root, AgentID: f.authority.ID, Authority: &f.authority, Session: f.session(true)})
	if !errors.Is(err, ErrAgentDegraded) {
		t.Fatalf("setup failure lost degradation: %v", err)
	}
	state := f.state(t)
	if state.AgentHealth[f.authority.ID].State != models.AgentHealthDegraded {
		t.Fatal("setup failure did not persist global degraded health")
	}
	if task := state.FindTask("task-1"); task.AssignedTo != nil || task.Status != models.TaskStatusReady || task.Worktree == nil {
		t.Fatalf("setup failure left executable ownership: %#v", task)
	}
}

func TestValidationPreflightForcedRepairClearsCooldown(t *testing.T) {
	f := newAssignmentPreflightFixture(t, models.TaskStatusImplementing)
	marker := filepath.Join(t.TempDir(), "repaired")
	if err := f.bb.Modify(func(s *models.State) error {
		s.FindTask("task-1").ValidationPrerequisites[0].Probes = [][]string{{"/bin/sh", "-c", `test -f "$1"`, "probe", marker}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	_, err := PrepareValidationPreflight(f.root, "task-1", f.authority.ID, "", f.session(true))
	requireAssignmentPreflightError(t, err)
	if err := os.WriteFile(marker, nil, 0600); err != nil {
		t.Fatal(err)
	}
	session := f.session(true)
	session.ForceCheck = true
	if _, err := PrepareValidationPreflight(f.root, "task-1", f.authority.ID, "", session); err != nil {
		t.Fatalf("forced repair failed: %v", err)
	}
	if _, err := PrepareValidationPreflight(f.root, "task-1", f.authority.ID, "", f.session(true)); err != nil {
		t.Fatalf("successful repair retained failure cooldown: %v", err)
	}
}

func TestValidationPreflightArtifactPolicyExplainsUnsupportedVersion(t *testing.T) {
	f := newAssignmentPreflightFixture(t, models.TaskStatusImplementing)
	if err := f.bb.Modify(func(s *models.State) error {
		tool := s.Config.AgentTools["fixture"]
		tool.ValidationExecution = "artifact-only"
		s.Config.AgentTools["fixture"] = tool
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	session := f.session(true)
	session.Execution = "artifact-only"
	_, err := PrepareValidationPreflight(f.root, "task-1", f.authority.ID, "", session)
	requireAssignmentPreflightError(t, err)
	if !strings.Contains(err.Error(), "validation artifacts are not implemented in this version") {
		t.Fatalf("missing explicit unsupported-artifact guidance: %v", err)
	}
}
