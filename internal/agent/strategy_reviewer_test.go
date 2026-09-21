package agent

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"errors"

	"github.com/liza-mas/liza/internal/db"
	lizagit "github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// TestReviewerPreWork_ExecutesAutoTransitions verifies that reviewer PreWork
// executes auto transitions (e.g., integration-to-fix) for merged tasks,
// creating child fix tasks with DRAFT_CODE status and coding-pair role_pair.
func TestReviewerPreWork_ExecutesAutoTransitions(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	const reviewerID = "code-reviewer-1"

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2

	// Integration-analyst task: MERGED (goes through merge path like all tasks).
	reviewCommit := "abc123"
	mergeCommit := "def456"
	parentID := "integration-task-1"
	task := models.Task{
		ID:           parentID,
		Type:         models.TaskTypeIntegration,
		RolePair:     "integration-pair",
		Description:  "Integration analysis for goal",
		Status:       models.TaskStatusMerged,
		Priority:     1,
		Created:      now,
		SpecRef:      "specs/goals/test.md",
		DoneWhen:     "Analysis approved",
		Scope:        "full branch",
		ReviewCommit: &reviewCommit,
		MergeCommit:  &mergeCommit,
		Output: []models.OutputEntry{
			{Desc: "Fix type alignment in auth", DoneWhen: "Types match across modules", Scope: "internal/auth", SpecRef: "specs/goals/test.md"},
			{Desc: "Fix error mapping in handler", DoneWhen: "All errors propagated", Scope: "internal/handler", SpecRef: "specs/goals/test.md"},
		},
		History: []models.TaskHistoryEntry{},
	}

	state.Tasks = []models.Task{task}
	state.Sprint.Scope.Planned = []string{parentID}
	state.Agents[reviewerID] = testhelpers.RegisteredTestAgent(models.RoleCodeReviewer)
	testhelpers.WriteInitialState(t, statePath, state)

	bb := db.New(statePath)
	authority := testSupervisorAuthority(t, bb, reviewerID)
	resolver := testResolver(t)
	s, err := NewRoleStrategy("code-reviewer", resolver)
	if err != nil {
		t.Fatalf("NewRoleStrategy() error = %v", err)
	}

	shouldContinue, err := s.PreWork(context.Background(), bb, SupervisorConfig{
		AgentID:     reviewerID,
		Role:        models.RoleCodeReviewer,
		ProjectRoot: tmpDir,
		Authority:   authority,
	})
	if err != nil {
		t.Fatalf("PreWork() error = %v", err)
	}
	if shouldContinue {
		t.Error("PreWork() shouldContinue = true, want false")
	}

	// Read final state
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	// Verify: parent is in MERGED state (integration tasks now go through merge)
	parent := readState.FindTask(parentID)
	if parent == nil {
		t.Fatal("Parent task not found")
	}
	if parent.Status != models.TaskStatusMerged {
		t.Errorf("Parent status = %q, want MERGED", parent.Status)
	}

	// Verify: child fix tasks were created
	if !parent.TransitionsExecuted["integration-to-fix"] {
		t.Fatalf("Parent TransitionsExecuted missing 'integration-to-fix': %v", parent.TransitionsExecuted)
	}

	// Find children (should be 2, one per output entry)
	var children []*models.Task
	for i := range readState.Tasks {
		if readState.Tasks[i].ID != parentID {
			children = append(children, &readState.Tasks[i])
		}
	}
	if len(children) != 2 {
		t.Fatalf("Children count = %d, want 2", len(children))
	}

	for i, child := range children {
		if child.Status != "DRAFT_CODE" {
			t.Errorf("Child[%d] status = %q, want DRAFT_CODE", i, child.Status)
		}
		if child.RolePair != "coding-pair" {
			t.Errorf("Child[%d] role_pair = %q, want coding-pair", i, child.RolePair)
		}
		if !slices.Contains(readState.Sprint.Scope.Planned, child.ID) {
			t.Errorf("Child %q not in Sprint.Scope.Planned", child.ID)
		}
	}
}

// TestReviewerPreWork_DoesNotExecuteManualTransitions verifies that reviewer
// PreWork does NOT execute manual transitions. Manual transitions (e.g.,
// code-plan-to-coding) remain gated by the orchestrator checkpoint flow.
func TestReviewerPreWork_DoesNotExecuteManualTransitions(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	const reviewerID = "code-reviewer-1"

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2

	// Code-planning task: MERGED with output (manual transition available).
	reviewCommit := "abc123"
	mergeCommit := "def456"
	parentID := "planning-task-1"
	task := models.Task{
		ID:           parentID,
		Type:         models.TaskTypeCoding,
		RolePair:     "code-planning-pair",
		Description:  "Plan implementation of feature X",
		Status:       models.TaskStatusMerged,
		Priority:     1,
		Created:      now,
		SpecRef:      "specs/goals/test.md",
		DoneWhen:     "Plan approved",
		Scope:        "internal/pkg",
		ReviewCommit: &reviewCommit,
		MergeCommit:  &mergeCommit,
		Output: []models.OutputEntry{
			{Desc: "Implement feature X", DoneWhen: "Tests pass", Scope: "internal/pkg", SpecRef: "specs/goals/test.md"},
		},
		History: []models.TaskHistoryEntry{},
	}

	state.Tasks = []models.Task{task}
	state.Sprint.Scope.Planned = []string{parentID}
	state.Agents[reviewerID] = testhelpers.RegisteredTestAgent(models.RoleCodeReviewer)
	testhelpers.WriteInitialState(t, statePath, state)

	bb := db.New(statePath)
	authority := testSupervisorAuthority(t, bb, reviewerID)
	resolver := testResolver(t)
	s, err := NewRoleStrategy("code-reviewer", resolver)
	if err != nil {
		t.Fatalf("NewRoleStrategy() error = %v", err)
	}

	shouldContinue, err := s.PreWork(context.Background(), bb, SupervisorConfig{
		AgentID:     reviewerID,
		Role:        models.RoleCodeReviewer,
		ProjectRoot: tmpDir,
		Authority:   authority,
	})
	if err != nil {
		t.Fatalf("PreWork() error = %v", err)
	}
	if shouldContinue {
		t.Error("PreWork() shouldContinue = true, want false")
	}

	// Read final state
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	// Verify: parent is still MERGED
	parent := readState.FindTask(parentID)
	if parent == nil {
		t.Fatal("Parent task not found")
	}
	if parent.Status != models.TaskStatusMerged {
		t.Errorf("Parent status = %q, want MERGED", parent.Status)
	}

	// Verify: NO child tasks created (manual transitions not fired by reviewer)
	if len(readState.Tasks) != 1 {
		t.Errorf("Task count = %d, want 1 (no children should be created from manual transitions)", len(readState.Tasks))
	}

	// Verify: TransitionsExecuted should NOT include code-plan-to-coding
	if parent.TransitionsExecuted["code-plan-to-coding"] {
		t.Error("code-plan-to-coding should NOT be in TransitionsExecuted (manual transitions not fired by reviewer)")
	}
}

func TestReviewerClaimTask_InitialTaskClaimsSpecifiedReviewTask(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Agents["code-reviewer-1"] = testhelpers.RegisteredTestAgent(models.RoleCodeReviewer)

	lowPriority := testhelpers.BuildTaskByStatus("task-low", models.TaskStatusReadyForReview, now)
	lowPriority.Priority = 3
	lowPriority.Worktree = nil
	highPriority := testhelpers.BuildTaskByStatus("task-high", models.TaskStatusReadyForReview, now)
	highPriority.Priority = 1
	highPriority.Worktree = nil
	state.Tasks = []models.Task{lowPriority, highPriority}
	testhelpers.WriteInitialState(t, statePath, state)

	wtPath := filepath.Join(tmpDir, paths.WorktreesDirName, "task-low")
	if err := os.MkdirAll(wtPath, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", wtPath, err)
	}

	resolver := testResolver(t)
	strategy, err := NewRoleStrategy(models.RoleCodeReviewer, resolver)
	if err != nil {
		t.Fatalf("NewRoleStrategy() error = %v", err)
	}

	taskID, claimedTaskID, err := strategy.ClaimTask(SupervisorConfig{
		AgentID:     "code-reviewer-1",
		Role:        models.RoleCodeReviewer,
		ProjectRoot: tmpDir,
		StatePath:   statePath,
		InitialTask: "task-low",
		Authority:   testSupervisorAuthority(t, db.New(statePath), "code-reviewer-1"),
	}, db.New(statePath))
	if err != nil {
		t.Fatalf("ClaimTask() error = %v", err)
	}
	if taskID != "task-low" || claimedTaskID != "" {
		t.Fatalf("ClaimTask() = (%q, %q), want (task-low, empty claimed ID)", taskID, claimedTaskID)
	}

	readState, err := db.New(statePath).Read()
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	low := readState.FindTask("task-low")
	if low == nil {
		t.Fatal("task-low not found")
	}
	if low.Status != models.TaskStatusReviewing {
		t.Fatalf("task-low status = %s, want %s", low.Status, models.TaskStatusReviewing)
	}
	high := readState.FindTask("task-high")
	if high == nil {
		t.Fatal("task-high not found")
	}
	if high.Status != models.TaskStatusReadyForReview {
		t.Fatalf("task-high status = %s, want %s", high.Status, models.TaskStatusReadyForReview)
	}
}

func TestEnsureReviewerPromptClaimed_RejectsSubmittedUnclaimedTask(t *testing.T) {
	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-stale", models.TaskStatusReadyForReview, now),
	}

	resolver := testResolver(t)
	err := ensureReviewerPromptClaimed(state, "code-reviewer-1", "task-stale", resolver)
	if err == nil {
		t.Fatal("ensureReviewerPromptClaimed() error = nil, want stale submitted task rejection")
	}
	if !strings.Contains(err.Error(), "requires reviewing state") {
		t.Fatalf("ensureReviewerPromptClaimed() error = %v, want reviewing-state rejection", err)
	}
}

// Policy-boundary test: exercises reviewerStrategy.ClaimTask itself, so removing
// or reordering the claim release or the degradation at that call site fails
// here rather than passing on helper-level coverage.
func TestReviewerStrategy_ClaimTask_PostWorktreeCmdFailureReleasesClaimAndDegrades(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	postCmd := "exit 1"
	state.Config.PostWorktreeCmd = &postCmd
	reviewCommit := "abc123"
	state.Tasks = []models.Task{
		{
			ID:           "task-1",
			Status:       models.TaskStatusReadyForReview,
			RolePair:     "coding-pair",
			ReviewCommit: &reviewCommit,
			Created:      now,
		},
	}
	const reviewerID = "code-reviewer-1"
	state.Agents[reviewerID] = testhelpers.RegisteredTestAgent("code-reviewer")
	testhelpers.WriteInitialState(t, stateFile, state)

	// Intact worktree: the reviewer path runs setup before the provider session.
	wtPath := filepath.Join(tmpDir, paths.WorktreesDirName, "task-1")
	if err := os.MkdirAll(wtPath, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", wtPath, err)
	}

	strategy, err := NewRoleStrategy("code-reviewer", testResolver(t))
	if err != nil {
		t.Fatalf("NewRoleStrategy() error = %v", err)
	}

	bb := db.New(stateFile)
	taskID, claimedTaskID, claimErr := strategy.ClaimTask(SupervisorConfig{
		ProjectRoot: tmpDir,
		AgentID:     reviewerID,
		Role:        "code-reviewer",
		Authority:   testSupervisorAuthority(t, bb, reviewerID),
	}, bb)

	if claimErr == nil {
		t.Fatal("ClaimTask() error = nil, want setup failure")
	}
	if !errors.Is(claimErr, ErrAgentDegraded) {
		t.Fatalf("ClaimTask() error = %v, want ErrAgentDegraded", claimErr)
	}
	// No task returned means the supervisor never builds a prompt or launches a
	// provider for this worktree.
	if taskID != "" || claimedTaskID != "" {
		t.Errorf("ClaimTask() = (%q, %q), want empty", taskID, claimedTaskID)
	}

	after, readErr := bb.Read()
	if readErr != nil {
		t.Fatalf("Read() error = %v", readErr)
	}

	task := after.FindTask("task-1")
	if task == nil {
		t.Fatal("FindTask(task-1) = nil")
	}
	// Exact status, not merely "not BLOCKED": a task stranded in REVIEWING with
	// no reviewer is unreviewable and unreclaimable, which would also pass a
	// negative assertion.
	if task.Status != models.TaskStatusReadyForReview {
		t.Errorf("task.Status = %s, want %s so another reviewer can claim it",
			task.Status, models.TaskStatusReadyForReview)
	}
	if task.ReviewingBy != nil {
		t.Errorf("task.ReviewingBy = %q, want the reviewer claim released", *task.ReviewingBy)
	}
	if task.ReviewLeaseExpires != nil {
		t.Errorf("task.ReviewLeaseExpires = %v, want cleared", *task.ReviewLeaseExpires)
	}

	// The reviewer agent must also be free, not just detached from the task.
	reviewer, ok := after.Agents[reviewerID]
	if !ok {
		t.Fatal("Agents missing reviewer entry")
	}
	if reviewer.CurrentTask != nil && *reviewer.CurrentTask != "" {
		t.Errorf("reviewer.CurrentTask = %q, want released", *reviewer.CurrentTask)
	}
	// Exact status: the claim sets REVIEWING, and only ReleaseAgent resets to
	// IDLE. Asserting "not WORKING" would accept a reviewer left in REVIEWING.
	if reviewer.Status != models.AgentStatusIdle {
		t.Errorf("reviewer.Status = %s, want %s", reviewer.Status, models.AgentStatusIdle)
	}

	health, ok := after.AgentHealth[reviewerID]
	if !ok {
		t.Fatal("AgentHealth missing entry, want the reviewer degraded")
	}
	if health.Reason != ops.AgentDegradedWorktreeSetupFailed {
		t.Errorf("health.Reason = %q, want %q", health.Reason, ops.AgentDegradedWorktreeSetupFailed)
	}
}

// reviewClaimProject is the RCA fixture behind the claim breaker: a git repo
// whose reviewable tasks each own a worktree, with a review_commit that either
// matches that worktree's HEAD (healthy) or is the stale integration SHA
// (broken). A broken task fails the real review-boundary validator inside
// ops.ClaimReviewerTask on every claim and writes nothing, so its boundary
// version never moves on its own.
type reviewClaimProject struct {
	root        string
	statePath   string
	bb          *db.Blackboard
	reviewerID  string
	staleCommit string
}

func setupReviewClaimProject(t *testing.T) *reviewClaimProject {
	t.Helper()
	root := t.TempDir()
	testhelpers.SetupTestGitRepo(t, root)
	statePath, _ := testhelpers.SetupLizaDir(t, root)

	state := testhelpers.CreateValidState()
	state.Config.ReviewerPollInterval = 1
	state.Config.ReviewerMaxWait = 1
	state.Config.LeaseDuration = 300
	const reviewerID = "code-reviewer-1"
	state.Agents[reviewerID] = testhelpers.RegisteredTestAgent(models.RoleCodeReviewer)
	bb := testhelpers.WriteInitialState(t, statePath, state)

	return &reviewClaimProject{
		root:        root,
		statePath:   statePath,
		bb:          bb,
		reviewerID:  reviewerID,
		staleCommit: testhelpers.MustGit(t, root, "rev-parse", "integration"),
	}
}

// addReviewableTask creates a worktree holding one commit and a reviewable
// task on it. Returns the worktree HEAD, which is the review_commit the
// documented repair would write for a broken task.
func (p *reviewClaimProject) addReviewableTask(t *testing.T, taskID string, broken bool) string {
	t.Helper()
	testhelpers.MustGit(t, p.root, "checkout", "integration")
	g := lizagit.New(p.root)
	if _, err := g.CreateWorktree(taskID, "integration"); err != nil {
		t.Fatalf("CreateWorktree(%s) error = %v", taskID, err)
	}
	wtPath := g.GetWorktreePath(taskID)
	if err := os.WriteFile(filepath.Join(wtPath, taskID+".txt"), []byte(taskID+" work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	testhelpers.MustGit(t, wtPath, "add", taskID+".txt")
	testhelpers.MustGit(t, wtPath, "commit", "-m", "Add "+taskID)
	head := testhelpers.MustGit(t, wtPath, "rev-parse", "HEAD")
	if head == p.staleCommit {
		t.Fatalf("test setup failed: %s worktree HEAD matches the stale review commit", taskID)
	}
	base := testhelpers.MustGit(t, p.root, "merge-base", head, "integration")
	worktree := g.GetWorktreeRelPath(taskID)
	reviewCommit := head
	if broken {
		reviewCommit = p.staleCommit
	}

	if err := p.bb.Modify(func(state *models.State) error {
		task := testhelpers.BuildTaskByStatus(taskID, models.TaskStatusReadyForReview, time.Now().UTC())
		task.Worktree = &worktree
		task.BaseCommit = &base
		task.ReviewCommit = &reviewCommit
		state.Tasks = append(state.Tasks, task)
		return nil
	}); err != nil {
		t.Fatalf("add task %s: %v", taskID, err)
	}
	return head
}

// repairReviewBoundary applies the documented repair: review_commit becomes
// the worktree HEAD, which moves the task's boundary version.
func (p *reviewClaimProject) repairReviewBoundary(t *testing.T, taskID, head string) {
	t.Helper()
	if err := p.bb.Modify(func(state *models.State) error {
		task := state.FindTask(taskID)
		if task == nil {
			return errors.New("task not found: " + taskID)
		}
		task.ReviewCommit = &head
		return nil
	}); err != nil {
		t.Fatalf("repair %s: %v", taskID, err)
	}
}

func (p *reviewClaimProject) supervisorConfig(t *testing.T) SupervisorConfig {
	t.Helper()
	return SupervisorConfig{
		AgentID:     p.reviewerID,
		Role:        models.RoleCodeReviewer,
		ProjectRoot: p.root,
		StatePath:   p.statePath,
		Authority:   testSupervisorAuthority(t, p.bb, p.reviewerID),
	}
}

func (p *reviewClaimProject) anomaliesOfType(t *testing.T, anomalyType string) []models.Anomaly {
	t.Helper()
	state, err := p.bb.Read()
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	var found []models.Anomaly
	for _, anomaly := range state.Anomalies {
		if anomaly.Type == anomalyType {
			found = append(found, anomaly)
		}
	}
	return found
}

func newReviewerStrategyForTest(t *testing.T) *reviewerStrategy {
	t.Helper()
	strategy, err := NewRoleStrategy(models.RoleCodeReviewer, testResolver(t))
	if err != nil {
		t.Fatalf("NewRoleStrategy() error = %v", err)
	}
	reviewer, ok := strategy.(*reviewerStrategy)
	if !ok {
		t.Fatalf("NewRoleStrategy() = %T, want *reviewerStrategy", strategy)
	}
	return reviewer
}

// quarantineThroughRealClaims drives the strategy's claim path against the
// broken boundary until the breaker opens, the way the supervisor loop does:
// a real ops.ClaimReviewerTask failure, then the strategy's observation of it.
func quarantineThroughRealClaims(t *testing.T, reviewer *reviewerStrategy, config SupervisorConfig, bb *db.Blackboard) claimBreakerDecision {
	t.Helper()
	var decision claimBreakerDecision
	for i := 0; i < claimBreakerThreshold; i++ {
		taskID, _, err := reviewer.ClaimTask(config, bb)
		if err == nil {
			t.Fatalf("ClaimTask() claimed %s, want a review-boundary failure", taskID)
		}
		decision = reviewer.ObserveClaimFailure(err)
	}
	return decision
}

func captureAgentLogsAtLevel(t *testing.T, level slog.Level) *bytes.Buffer {
	t.Helper()
	var logs bytes.Buffer
	previous := logger
	logger = slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: level}))
	t.Cleanup(func() { logger = previous })
	return &logs
}

func countLogLines(logs string, fragments ...string) int {
	count := 0
	for _, line := range strings.Split(logs, "\n") {
		matched := true
		for _, fragment := range fragments {
			if !strings.Contains(line, fragment) {
				matched = false
				break
			}
		}
		if matched {
			count++
		}
	}
	return count
}

func TestReviewerWaitForWorkQuarantineReportsNoWorkDuringCooldown(t *testing.T) {
	project := setupReviewClaimProject(t)
	project.addReviewableTask(t, "task-broken", true)
	if err := project.bb.Modify(func(state *models.State) error {
		state.Config.DiagnosticLogging = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	config := project.supervisorConfig(t)
	reviewer := newReviewerStrategyForTest(t)

	// Before the breaker opens the broken task is offered as work.
	hasWork, err := reviewer.WaitForWork(context.Background(), project.bb, config, time.Second, time.Second)
	if err != nil || !hasWork {
		t.Fatalf("WaitForWork() before quarantine = (%v, %v), want (true, nil)", hasWork, err)
	}

	decision := quarantineThroughRealClaims(t, reviewer, config, project.bb)
	if len(decision.Opened) != 1 || decision.Delay < claimQuarantineCooldown {
		t.Fatalf("decision = %+v, want one opened key and a delay of at least %v", decision, claimQuarantineCooldown)
	}

	logs := captureAgentLogsAtLevel(t, slog.LevelInfo)
	start := time.Now()
	hasWork, err = reviewer.WaitForWork(context.Background(), project.bb, config, 200*time.Millisecond, 300*time.Millisecond)
	if err != nil || hasWork {
		t.Fatalf("WaitForWork() during cooldown = (%v, %v), want (false, nil)", hasWork, err)
	}
	if elapsed := time.Since(start); elapsed < 300*time.Millisecond {
		t.Fatalf("WaitForWork() returned after %v, want it to park for the full wait", elapsed)
	}
	if !strings.Contains(logs.String(), "quarantined") {
		t.Fatalf("no-work diagnostic does not name the quarantine:\n%s", logs.String())
	}
}

func TestReviewerWaitForWorkQuarantineReleasesOnBoundaryRepair(t *testing.T) {
	project := setupReviewClaimProject(t)
	head := project.addReviewableTask(t, "task-broken", true)
	config := project.supervisorConfig(t)
	reviewer := newReviewerStrategyForTest(t)
	quarantineThroughRealClaims(t, reviewer, config, project.bb)

	hasWork, err := reviewer.WaitForWork(context.Background(), project.bb, config, 100*time.Millisecond, 200*time.Millisecond)
	if err != nil || hasWork {
		t.Fatalf("WaitForWork() during cooldown = (%v, %v), want (false, nil)", hasWork, err)
	}

	// The documented repair moves review_commit, which changes the boundary
	// version the key was counted against: the task is work again at once.
	project.repairReviewBoundary(t, "task-broken", head)
	hasWork, err = reviewer.WaitForWork(context.Background(), project.bb, config, 100*time.Millisecond, 200*time.Millisecond)
	if err != nil || !hasWork {
		t.Fatalf("WaitForWork() after repair = (%v, %v), want (true, nil)", hasWork, err)
	}
	taskID, _, err := reviewer.ClaimTask(config, project.bb)
	if err != nil || taskID != "task-broken" {
		t.Fatalf("ClaimTask() after repair = (%q, %v), want (task-broken, nil)", taskID, err)
	}
}

func TestReviewerWaitForWorkQuarantineCoversInitialTask(t *testing.T) {
	project := setupReviewClaimProject(t)
	project.addReviewableTask(t, "task-broken", true)
	config := project.supervisorConfig(t)
	config.InitialTask = "task-broken"
	reviewer := newReviewerStrategyForTest(t)
	quarantineThroughRealClaims(t, reviewer, config, project.bb)

	hasWork, err := reviewer.WaitForWork(context.Background(), project.bb, config, 100*time.Millisecond, 200*time.Millisecond)
	if err != nil || hasWork {
		t.Fatalf("WaitForWork() for a quarantined initial task = (%v, %v), want (false, nil)", hasWork, err)
	}
}

func TestReviewerWaitForWorkQuarantineKeepsOtherCandidates(t *testing.T) {
	project := setupReviewClaimProject(t)
	project.addReviewableTask(t, "task-broken", true)
	config := project.supervisorConfig(t)
	reviewer := newReviewerStrategyForTest(t)
	quarantineThroughRealClaims(t, reviewer, config, project.bb)

	project.addReviewableTask(t, "task-healthy", false)
	hasWork, err := reviewer.WaitForWork(context.Background(), project.bb, config, 100*time.Millisecond, 200*time.Millisecond)
	if err != nil || !hasWork {
		t.Fatalf("WaitForWork() with a healthy sibling = (%v, %v), want (true, nil)", hasWork, err)
	}
}

func TestReviewerClaimTaskQuarantinesBrokenCandidateWhileClaimingHealthyTasks(t *testing.T) {
	project := setupReviewClaimProject(t)
	project.addReviewableTask(t, "task-broken", true)
	if err := project.bb.Modify(func(state *models.State) error {
		// Always evaluate the broken candidate before any healthy candidate.
		state.FindTask("task-broken").Priority = 0
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	config := project.supervisorConfig(t)
	reviewer := newReviewerStrategyForTest(t)
	state, err := project.bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	boundary := ops.ReviewClaimBoundaryVersion(state.FindTask("task-broken"))
	key := claimBreakerKey{Role: config.Role, TaskID: "task-broken", Class: ops.ReviewClaimClassReviewBoundaryRepair, BoundaryVersion: boundary}
	attempts := 0
	previousHook := reviewerClaimAttemptHook
	reviewerClaimAttemptHook = func() { attempts++ }
	t.Cleanup(func() { reviewerClaimAttemptHook = previousHook })

	// The extra cycle proves healthy progress cannot clear the broken key or
	// produce another anomaly during its cooldown.
	for i := 0; i <= claimBreakerThreshold; i++ {
		healthyID := fmt.Sprintf("task-healthy-%d", i)
		project.addReviewableTask(t, healthyID, false)
		taskID, _, err := reviewer.ClaimTask(config, project.bb)
		if err != nil || taskID != healthyID {
			t.Fatalf("cycle %d: ClaimTask() = (%q, %v), want (%q, nil)", i, taskID, err, healthyID)
		}
		if err := releaseReviewerClaimQuietly(project.root, taskID, config.Authority); err != nil {
			t.Fatal(err)
		}
		state, err := project.bb.Read()
		if err != nil {
			t.Fatal(err)
		}
		wantQuarantine := i+1 >= claimBreakerThreshold
		if got := reviewer.activeBreaker().Quarantined(state.FindTask("task-broken"), config.Role, time.Now()); got != wantQuarantine {
			t.Fatalf("cycle %d: quarantined = %v, want %v", i, got, wantQuarantine)
		}
		wantAnomalies := 0
		if wantQuarantine {
			wantAnomalies = 1
		}
		if got := len(project.anomaliesOfType(t, models.AnomalyTypeReviewerClaimCircuitOpen)); got != wantAnomalies {
			t.Fatalf("cycle %d: circuit-open anomalies = %d, want %d", i, got, wantAnomalies)
		}
	}
	if attempts != claimBreakerThreshold+1 {
		t.Fatalf("claim hook calls = %d, want %d", attempts, claimBreakerThreshold+1)
	}
	state, err = project.bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	broken := state.FindTask("task-broken")
	if ops.ReviewClaimBoundaryVersion(broken) != boundary {
		t.Fatal("broken candidate boundary changed during healthy claims")
	}
	if !reviewer.activeBreaker().Quarantined(broken, config.Role, time.Now()) {
		t.Fatal("broken candidate was not quarantined after successful healthy claims")
	}
	counters, ok := reviewer.activeBreaker().Counters(key)
	if !ok || counters.Attempts != claimBreakerThreshold+1 {
		t.Fatalf("broken candidate counters = (%+v, %v), want %d attempts", counters, ok, claimBreakerThreshold+1)
	}
	anomalies := project.anomaliesOfType(t, models.AnomalyTypeReviewerClaimCircuitOpen)
	if len(anomalies) != 1 {
		t.Fatalf("circuit-open anomalies = %d, want 1", len(anomalies))
	}
}

func TestReviewerObserveClaimFailureLogsErrorOnceWhenKeyOpens(t *testing.T) {
	project := setupReviewClaimProject(t)
	project.addReviewableTask(t, "task-broken", true)
	config := project.supervisorConfig(t)
	reviewer := newReviewerStrategyForTest(t)

	logs := captureAgentLogsAtLevel(t, slog.LevelDebug)
	quarantineThroughRealClaims(t, reviewer, config, project.bb)

	// The keyed site owns the error-level line: once, when the key opens.
	if got := countLogLines(logs.String(), "level=ERROR", `msg="Review claim error"`); got != 1 {
		t.Fatalf("error-level Review claim error lines = %d, want 1:\n%s", got, logs.String())
	}
	// Every attempt stays visible at debug level from the claim helper.
	if got := countLogLines(logs.String(), "level=DEBUG", `msg="Review claim error"`); got < claimBreakerThreshold {
		t.Fatalf("debug-level Review claim error lines = %d, want at least %d:\n%s", got, claimBreakerThreshold, logs.String())
	}

	anomalies := project.anomaliesOfType(t, models.AnomalyTypeReviewerClaimCircuitOpen)
	if len(anomalies) != 1 {
		t.Fatalf("circuit-open anomalies = %d, want 1", len(anomalies))
	}
	details := anomalies[0].Details
	if anomalies[0].Task != "task-broken" || details["role"] != models.RoleCodeReviewer || details["failure_class"] != ops.ReviewClaimClassReviewBoundaryRepair {
		t.Fatalf("anomaly = %+v, want task-broken/%s/%s", anomalies[0], models.RoleCodeReviewer, ops.ReviewClaimClassReviewBoundaryRepair)
	}
	if details["attempts"] != claimBreakerThreshold {
		t.Fatalf("attempts = %v, want %d", details["attempts"], claimBreakerThreshold)
	}
	if recovery, _ := details["recovery"].(string); !strings.Contains(recovery, "update-review-commit") {
		t.Fatalf("recovery = %q, want the update-review-commit hint", recovery)
	}
}

func TestReviewerObserveClaimFailureStopsOnRejectedAnomalyAuthority(t *testing.T) {
	project := setupReviewClaimProject(t)
	project.addReviewableTask(t, "task-broken", true)
	config := project.supervisorConfig(t)
	reviewer := newReviewerStrategyForTest(t)

	var decision claimBreakerDecision
	for i := 0; i < claimBreakerThreshold; i++ {
		_, _, err := reviewer.ClaimTask(config, project.bb)
		if err == nil {
			t.Fatal("ClaimTask() succeeded, want a review-boundary failure")
		}
		if i == claimBreakerThreshold-1 {
			// The generation moves under the supervisor between its last
			// claim and its anomaly write: the write is rejected and the
			// losing supervisor must stop rather than retry.
			if err := project.bb.Modify(func(state *models.State) error {
				agent := state.Agents[project.reviewerID]
				agent.Generation = "successor-generation"
				state.Agents[project.reviewerID] = agent
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		}
		decision = reviewer.ObserveClaimFailure(err)
	}
	if !decision.Stop {
		t.Fatalf("decision = %+v, want Stop after a rejected anomaly authority", decision)
	}
	if anomalies := project.anomaliesOfType(t, models.AnomalyTypeReviewerClaimCircuitOpen); len(anomalies) != 0 {
		t.Fatalf("circuit-open anomalies = %d, want none under a rejected authority", len(anomalies))
	}
}

func TestReviewerObserveClaimFailureUsesTypedClassification(t *testing.T) {
	reviewer := &reviewerStrategy{role: models.RoleCodeReviewer}

	decision := reviewer.ObserveClaimFailure(&ops.AgentAuthorityError{AgentID: "code-reviewer-1"})
	if !decision.Stop {
		t.Fatalf("authority decision = %+v, want Stop", decision)
	}
	decision = reviewer.ObserveClaimFailure(&ops.PreconditionError{Reason: "no reviewable tasks found"})
	if decision.Stop || decision.Delay != claimBackoffBase || len(decision.Opened) != 0 {
		t.Fatalf("no-work decision = %+v, want the base delay only", decision)
	}
	decision = reviewer.ObserveClaimFailure(nil)
	if decision.Stop || decision.Delay != 0 || len(decision.Opened) != 0 {
		t.Fatalf("nil decision = %+v, want the zero decision", decision)
	}
}
