package ops

import (
	"bytes"
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func setupPendingVerdict(t *testing.T) (string, *db.Blackboard, models.AgentAuthority, string, LifecycleRequestOptions) {
	t.Helper()
	t.Cleanup(db.SetDefaultLockTimeoutForTest(10 * time.Millisecond))
	root := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, root)
	state := testhelpers.CreateValidState()
	taskID, actor, commit := "task-1", "code-reviewer-1", strings.Repeat("a", 40)
	task := testhelpers.BuildTaskByStatus(taskID, models.TaskStatusReviewing, time.Now().UTC())
	task.ReviewCommit = &commit
	state.Tasks = []models.Task{task}
	authority := models.AgentAuthority{ID: actor, Generation: "original-private-generation"}
	state.Agents[actor] = models.Agent{Role: models.RoleCodeReviewer, Status: models.AgentStatusReviewing,
		Generation: authority.Generation, CurrentTask: &taskID}
	bb := testhelpers.WriteInitialState(t, statePath, state)
	stored, err := bb.ReadSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	return root, bb, authority, commit, LifecycleRequestOptions{RequestID: "reached-verdict", ExpectedTransition: models.TaskTransitionID(stored.FindTask(taskID))}
}

func holdPendingVerdictState(t *testing.T, bb *db.Blackboard) func() {
	t.Helper()
	ready, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		done <- filelock.New(bb.GetStatePath()).WithLockOperation("verdict-contention-test", func() error {
			close(ready)
			<-release
			return nil
		})
	}()
	select {
	case <-ready:
	case err := <-done:
		t.Fatalf("hold state lock: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("state lock holder did not start")
	}
	released := false
	unlock := func() {
		if !released {
			released = true
			close(release)
			if err := <-done; err != nil {
				t.Errorf("release state lock: %v", err)
			}
		}
	}
	t.Cleanup(unlock)
	return unlock
}

func TestPendingVerdictSurvivesLockTimeoutAndCancellation(t *testing.T) {
	root, bb, authority, commit, opts := setupPendingVerdict(t)
	unlock := holdPendingVerdictState(t, bb)
	_, err := SubmitVerdictWithAuthorityAndOptions(root, "task-1", "APPROVED", "", authority, "", commit, opts)
	if !filelock.IsLockErrorType(err, filelock.LockErrorTimeout) {
		t.Fatalf("expected acquisition timeout, got %v", err)
	}
	entries, err := os.ReadDir(pendingVerdictDir(root))
	if err != nil || len(entries) != 1 {
		t.Fatalf("reached verdict was not retained: entries=%d err=%v", len(entries), err)
	}
	filename := pendingVerdictDir(root) + string(os.PathSeparator) + entries[0].Name()
	info, err := os.Stat(filename)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatal("private verdict envelope is not 0600")
	}
	pending, err := readPendingVerdict(filename)
	if err != nil {
		t.Fatal(err)
	}
	if pending.Request.RequestID != opts.RequestID || pending.Request.ExpectedTransition != opts.ExpectedTransition {
		t.Fatal("reached verdict lost its immutable lifecycle identity")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := ReplayPendingVerdicts(ctx, root, authority.ID); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled drain: %v", err)
	}
	if _, err := os.Stat(filename); err != nil {
		t.Fatal("cancelled drain discarded verdict")
	}
	unlock()
	if err := ReplayPendingVerdicts(context.Background(), root, authority.ID); err != nil {
		t.Fatal(err)
	}
	state, err := bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	task := state.FindTask("task-1")
	if task.Status != models.TaskStatusApproved || len(task.Approvals) != 1 {
		t.Fatal("durable approval did not land exactly once")
	}
	if _, err := os.Stat(filename); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("settled verdict remains pending")
	}
	result, err := SubmitVerdictWithAuthorityAndOptions(root, "task-1", "APPROVED", "", authority, "", commit, opts)
	if err != nil || result.Outcome != models.LifecycleAlreadyCompleted {
		t.Fatalf("exact retry did not replay: %v", err)
	}
	state, _ = bb.Read()
	if len(state.FindTask("task-1").Approvals) != 1 {
		t.Fatal("receipt replay duplicated approval")
	}
}

func TestPendingVerdictInvalidEnvelopesDoNotBlockOtherAgentsOrValidReplay(t *testing.T) {
	root, bb, authority, commit, opts := setupPendingVerdict(t)
	unlock := holdPendingVerdictState(t, bb)
	if _, err := SubmitVerdictWithAuthorityAndOptions(root, "task-1", "APPROVED", "", authority, "", commit, opts); !filelock.IsLockErrorType(err, filelock.LockErrorTimeout) {
		t.Fatalf("expected acquisition timeout, got %v", err)
	}
	unlock()
	entries, err := os.ReadDir(pendingVerdictDir(root))
	if err != nil || len(entries) != 1 {
		t.Fatalf("expected one reached verdict: entries=%d err=%v", len(entries), err)
	}
	ownFile := filepath.Join(pendingVerdictDir(root), entries[0].Name())
	pending, err := readPendingVerdict(ownFile)
	if err != nil {
		t.Fatal(err)
	}
	foreign := pending
	foreign.Authority.ID, foreign.Request.Actor = "other-reviewer", "other-reviewer"
	foreignFile, err := savePendingVerdict(root, foreign)
	if err != nil {
		t.Fatal(err)
	}
	// A correctly checksummed envelope can still fail schema validation.
	invalid := foreign
	invalid.Request.Operation = "unknown-future-operation"
	invalidFile, err := savePendingVerdict(root, invalid)
	if err != nil {
		t.Fatal(err)
	}
	retained := []string{invalidFile, foreignFile}
	for name, data := range map[string]string{
		"000-invalid-json.json":     "invalid-json-" + authority.Generation,
		"001-invalid-checksum.json": "{}",
	} {
		filename := filepath.Join(pendingVerdictDir(root), name)
		if err := os.WriteFile(filename, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		retained = append(retained, filename)
	}
	var warnings bytes.Buffer
	previousOutput := log.Writer()
	log.SetOutput(&warnings)
	t.Cleanup(func() { log.SetOutput(previousOutput) })
	if err := ReplayPendingVerdicts(context.Background(), root, "unrelated-reviewer"); err != nil {
		t.Fatalf("invalid foreign envelope blocked unrelated registration: %v", err)
	}
	if err := ReplayPendingVerdicts(context.Background(), root, authority.ID); err != nil {
		t.Fatalf("invalid envelope blocked valid reached verdict: %v", err)
	}
	state, err := bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	if task := state.FindTask("task-1"); task.Status != models.TaskStatusApproved || len(task.Approvals) != 1 {
		t.Fatal("valid reached verdict did not land exactly once")
	}
	if _, err := os.Stat(ownFile); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("settled own verdict remains pending")
	}
	for _, filename := range retained {
		if _, err := os.Stat(filename); err != nil {
			t.Fatalf("foreign or invalid evidence was not retained: %v", err)
		}
	}
	if !strings.Contains(warnings.String(), "retained for manual recovery") || strings.Contains(warnings.String(), authority.Generation) {
		t.Fatal("invalid-envelope warning missing or contains private envelope contents")
	}
}

func TestPendingVerdictGenerationReplacementQuarantinesOriginalFinding(t *testing.T) {
	root, bb, authority, commit, opts := setupPendingVerdict(t)
	unlock := holdPendingVerdictState(t, bb)
	_, err := SubmitVerdictWithAuthorityAndOptions(root, "task-1", "REJECTED", "finding "+authority.Generation, authority, "", commit, opts)
	if !filelock.IsLockErrorType(err, filelock.LockErrorTimeout) {
		t.Fatalf("expected acquisition timeout, got %v", err)
	}
	unlock()
	if err := bb.Modify(func(s *models.State) error {
		a := s.Agents[authority.ID]
		a.Generation = "replacement-generation"
		s.Agents[authority.ID] = a
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := ReplayPendingVerdicts(context.Background(), root, authority.ID); err != nil {
		t.Fatal(err)
	}
	state, err := bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	if state.FindTask("task-1").Status != models.TaskStatusReviewing || state.Agents[authority.ID].Generation != "replacement-generation" {
		t.Fatal("replay crossed the registration generation fence")
	}
	if len(state.QuarantinedVerdicts) != 1 || state.QuarantinedVerdicts[0].Verdict != "REJECTED" {
		t.Fatal("original reached finding was not quarantined")
	}
	if strings.Contains(state.QuarantinedVerdicts[0].Reason, authority.Generation) {
		t.Fatal("quarantined reason exposes authority credential")
	}
}

func TestPendingVerdictMovedBoundaryRetiresWithoutMutation(t *testing.T) {
	root, bb, authority, commit, opts := setupPendingVerdict(t)
	unlock := holdPendingVerdictState(t, bb)
	_, err := SubmitVerdictWithAuthorityAndOptions(root, "task-1", "APPROVED", "", authority, "", commit, opts)
	if !filelock.IsLockErrorType(err, filelock.LockErrorTimeout) {
		t.Fatalf("expected acquisition timeout, got %v", err)
	}
	unlock()
	newCommit := strings.Repeat("b", 40)
	if err := bb.Modify(func(state *models.State) error {
		task := state.FindTask("task-1")
		task.ReviewCommit = &newCommit
		models.AdvanceLifecycle(task)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := ReplayPendingVerdicts(context.Background(), root, authority.ID); err != nil {
		t.Fatal(err)
	}
	state, err := bb.ReadSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	task := state.FindTask("task-1")
	if task.Status != models.TaskStatusReviewing || *task.ReviewCommit != newCommit || len(task.Approvals) != 0 {
		t.Fatal("pending verdict mutated a replacement review boundary")
	}
	entries, err := os.ReadDir(pendingVerdictDir(root))
	if err != nil || len(entries) != 0 {
		t.Fatal("obsolete verdict was not retired")
	}
}

func TestInvalidVerdictPreflightDoesNotLeavePendingReplay(t *testing.T) {
	root, bb, authority, commit, _ := setupPendingVerdict(t)
	if err := bb.Modify(func(state *models.State) error {
		task := state.FindTask("task-1")
		task.History = append(task.History, models.TaskHistoryEntry{Event: models.TaskEventPreExecutionCheckpoint,
			Extra: map[string]any{"impact": "architecture"}})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	_, err := SubmitVerdictWithAuthorityAndOptions(root, "task-1", "APPROVED", "", authority, "standard", commit, LifecycleRequestOptions{})
	var precondition *PreconditionError
	if !errors.As(err, &precondition) || !strings.Contains(err.Error(), "cannot downgrade impact") {
		t.Fatalf("expected nonretryable verdict preflight refusal, got %v", err)
	}
	entries, err := os.ReadDir(pendingVerdictDir(root))
	if err != nil || len(entries) != 0 {
		t.Fatal("invalid request was left for supervisor replay")
	}
	if err := ReplayPendingVerdicts(context.Background(), root, authority.ID); err != nil {
		t.Fatal(err)
	}
	state, err := bb.ReadSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if state.FindTask("task-1").Status != models.TaskStatusReviewing || len(state.FindTask("task-1").Approvals) != 0 {
		t.Fatal("preflight refusal mutated task state")
	}
}

func TestPendingVerdictSanitizedReasonPreservesExactReplayIdentity(t *testing.T) {
	root, bb, authority, commit, opts := setupPendingVerdict(t)
	reason := "review finding " + authority.Generation
	first, err := SubmitVerdictWithAuthorityAndOptions(root, "task-1", "REJECTED", reason, authority, "", commit, opts)
	if err != nil || first.Outcome != models.LifecycleCompleted {
		t.Fatalf("initial rejection: %v", err)
	}
	second, err := SubmitVerdictWithAuthorityAndOptions(root, "task-1", "REJECTED", reason, authority, "", commit, opts)
	if err != nil || second.Outcome != models.LifecycleAlreadyCompleted {
		t.Fatalf("exact rejection replay: %v", err)
	}
	state, err := bb.ReadSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	task := state.FindTask("task-1")
	if task.ReviewCyclesTotal != 1 || task.RejectionReason == nil || strings.Contains(*task.RejectionReason, authority.Generation) {
		t.Fatal("rejection replay duplicated effects or exposed authority")
	}
}

func TestPendingVerdictPublicationRetriesEveryDurabilityBarrier(t *testing.T) {
	root, bb, authority, commit, opts := setupPendingVerdict(t)
	state, err := bb.ReadSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	request, err := NewLifecycleRequest("submit-verdict", state.FindTask("task-1"), authority.ID, &authority, opts,
		map[string]string{"verdict": "APPROVED", "reason": "", "impact": "", "review_commit": commit})
	if err != nil {
		t.Fatal(err)
	}
	entry := pendingVerdict{TaskID: "task-1", Verdict: "APPROVED", ReviewCommit: commit, Authority: authority, Request: request}
	leaf, parent := pendingVerdictDir(root), filepath.Dir(pendingVerdictDir(root))
	barrierErr := errors.New("injected runtime-directory durability failure")
	failed := false
	restore := recordArchiveDirSyncs(t, func(dir string) error {
		if dir == parent && !failed {
			failed = true
			return barrierErr
		}
		return nil
	})
	t.Cleanup(restore)
	filename, err := savePendingVerdict(root, entry)
	if !errors.Is(err, barrierErr) {
		t.Fatalf("publication skipped containing runtime-directory barrier: %v", err)
	}
	if _, err := os.Stat(filename); err != nil {
		t.Fatal("test did not exercise failure after envelope rename")
	}
	if _, err := savePendingVerdict(root, entry); err != nil {
		t.Fatalf("retry of existing envelope did not finish durability barriers: %v", err)
	}
	if !slices.Equal(*archiveDirSyncsForTest, []string{leaf, parent, leaf, parent}) {
		t.Fatal("existing-envelope retry skipped leaf or containing-directory fsync")
	}
}

func TestPendingVerdictReusedIdentityRetiresConflictingPayload(t *testing.T) {
	root, bb, authority, commit, opts := setupPendingVerdict(t)
	if _, err := SubmitVerdictWithAuthorityAndOptions(root, "task-1", "APPROVED", "", authority, "", commit, opts); err != nil {
		t.Fatal(err)
	}
	_, err := SubmitVerdictWithAuthorityAndOptions(root, "task-1", "REJECTED", "conflicting payload", authority, "", commit, opts)
	if !errors.Is(err, ErrLifecycleIdentityReused) {
		t.Fatalf("expected conflicting-payload refusal, got %v", err)
	}
	if err := ReplayPendingVerdicts(context.Background(), root, authority.ID); err != nil {
		t.Fatal("refused reused identity blocked supervisor replay")
	}
	entries, err := os.ReadDir(pendingVerdictDir(root))
	if err != nil || len(entries) != 0 {
		t.Fatal("conflicting request identity remained pending")
	}
	state, err := bb.ReadSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if task := state.FindTask("task-1"); task.Status != models.TaskStatusApproved || len(task.Approvals) != 1 || task.ReviewCyclesTotal != 0 {
		t.Fatal("conflicting-payload replay changed the completed verdict")
	}
}
