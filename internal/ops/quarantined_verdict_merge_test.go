package ops

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
	"github.com/liza-mas/liza/internal/testhelpers/perm"
)

type quarantineFixture struct {
	root, statePath, taskID, commit string
	current, stale, orchestrator    models.AgentAuthority
}

func newQuarantineFixture(t *testing.T) quarantineFixture {
	t.Helper()
	f := quarantineFixture{
		root: t.TempDir(), taskID: "task-quarantine", commit: strings.Repeat("a", 40),
		current:      models.AgentAuthority{ID: "code-reviewer-1", Generation: "current-review-fixture"},
		stale:        models.AgentAuthority{ID: "code-reviewer-1", Generation: "stale-review-fixture"},
		orchestrator: models.AgentAuthority{ID: "orchestrator-1", Generation: "current-orchestrator-fixture"},
	}
	f.statePath, _ = testhelpers.SetupLizaDir(t, f.root)
	testhelpers.SetupPipelineConfig(t, f.root)
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus(f.taskID, models.TaskStatusReviewing, time.Now().UTC())
	task.ReviewCommit = &f.commit
	state.Tasks = []models.Task{task}
	state.Agents[f.current.ID] = models.Agent{Role: "code-reviewer", Status: models.AgentStatusReviewing, Generation: f.current.Generation, CurrentTask: &f.taskID}
	state.Agents[f.orchestrator.ID] = models.Agent{Role: "orchestrator", Status: models.AgentStatusIdle, Generation: f.orchestrator.Generation}
	testhelpers.WriteInitialState(t, f.statePath, state)
	return f
}

func (f quarantineFixture) read(t *testing.T) *models.State {
	t.Helper()
	state, err := db.New(f.statePath).Read()
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func (f quarantineFixture) mutate(t *testing.T, fn func(*models.State)) {
	t.Helper()
	if err := db.New(f.statePath).Modify(func(state *models.State) error { fn(state); return nil }); err != nil {
		t.Fatal(err)
	}
}

func (f quarantineFixture) capture(t *testing.T, verdict, reason, commit string) models.QuarantinedVerdict {
	t.Helper()
	_, err := SubmitVerdictWithAuthority(f.root, f.taskID, verdict, reason, f.stale, "", commit)
	if !IsAgentAuthorityError(err) {
		t.Fatalf("capture must retain generation fence, got %v", err)
	}
	state := f.read(t)
	if len(state.QuarantinedVerdicts) == 0 {
		t.Fatal("fenced judgment not retained")
	}
	finding := state.QuarantinedVerdicts[len(state.QuarantinedVerdicts)-1]
	if !strings.Contains(err.Error(), finding.ID) {
		t.Fatal("fencing diagnostic lacks retained finding ID")
	}
	return finding
}

func TestQuarantinedVerdictDeduplicatesAndSurvivesConcurrentRestart(t *testing.T) {
	f := newQuarantineFixture(t)
	first := f.capture(t, "REJECTED", "  missing optimistic concurrency  ", f.commit)
	before := readStateBytes(t, f.statePath)
	f.capture(t, "REJECTED", "missing optimistic concurrency", f.commit)
	if !bytes.Equal(before, readStateBytes(t, f.statePath)) {
		t.Fatal("identical retry rewrote durable state")
	}
	f.stale.Generation = "another-stale-review-fixture"
	f.capture(t, "REJECTED", "missing optimistic concurrency", f.commit)
	finding := f.read(t).QuarantinedVerdicts[0]
	if finding.ID != first.ID || !finding.Timestamp.Equal(first.Timestamp) || len(finding.GenerationFingerprints) != 2 {
		t.Fatal("cross-generation retry changed judgment identity/timestamp or lost provenance")
	}
	var wg sync.WaitGroup
	errCh := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := SubmitVerdictWithAuthority(f.root, f.taskID, "REJECTED", "independent blocker", f.stale, "", f.commit)
			if !IsAgentAuthorityError(err) {
				errCh <- fmt.Errorf("concurrent fence: %v", err)
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Error(err)
	}
	// New Blackboard instance reads persisted evidence, without process-local state.
	if findings := f.read(t).QuarantinedVerdicts; len(findings) != 2 {
		t.Fatalf("concurrent duplicate records: got %d, want 2", len(findings))
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestQuarantinedVerdictRestartReader$")
	cmd.Env = append(os.Environ(), "QUARANTINE_RESTART_STATE="+f.statePath)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fresh process lost evidence: %v\n%s", err, output)
	}
}

func TestQuarantinedVerdictRestartReader(t *testing.T) {
	statePath := os.Getenv("QUARANTINE_RESTART_STATE")
	if statePath == "" {
		return
	}
	state, err := db.New(statePath).Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(state.QuarantinedVerdicts) != 2 || state.QuarantinedVerdicts[0].Reason != "missing optimistic concurrency" || len(state.QuarantinedVerdicts[0].GenerationFingerprints) != 2 {
		t.Fatal("restart did not restore judgment and provenance")
	}
}

func TestQuarantinedVerdictSeparateProcessSubmission(t *testing.T) {
	if root := os.Getenv("QUARANTINE_SUBMIT_ROOT"); root != "" {
		_, err := SubmitVerdictWithAuthority(root, "task-quarantine", "REJECTED", "cross-process blocker", models.AgentAuthority{ID: "code-reviewer-1", Generation: "stale-review-fixture"}, "", strings.Repeat("a", 40))
		if !IsAgentAuthorityError(err) {
			t.Fatalf("child submission lost fence: %v", err)
		}
		return
	}
	f := newQuarantineFixture(t)
	const children = 4
	commands := make([]*exec.Cmd, children)
	outputs := make([]bytes.Buffer, children)
	for i := range commands {
		commands[i] = exec.Command(os.Args[0], "-test.run=^TestQuarantinedVerdictSeparateProcessSubmission$")
		commands[i].Env = append(os.Environ(), "QUARANTINE_SUBMIT_ROOT="+f.root)
		commands[i].Stdout = &outputs[i]
		commands[i].Stderr = &outputs[i]
		if err := commands[i].Start(); err != nil {
			t.Fatal(err)
		}
	}
	for i, cmd := range commands {
		if err := cmd.Wait(); err != nil {
			t.Errorf("child submission %d: %v\n%s", i, err, outputs[i].String())
		}
	}
	findings := f.read(t).QuarantinedVerdicts
	if len(findings) != 1 || findings[0].Reason != "cross-process blocker" || len(findings[0].GenerationFingerprints) != 1 {
		t.Fatal("cross-process submissions lost or duplicated evidence")
	}
}

func TestQuarantinedVerdictPersistenceFailureNeverClaimsSaved(t *testing.T) {
	f := newQuarantineFixture(t)
	before := readStateBytes(t, f.statePath)
	// Preserve existing lock metadata writes while denying atomic state creation.
	if err := os.WriteFile(f.statePath+".lock.pid", []byte("0"), 0644); err != nil {
		t.Fatal(err)
	}
	restore, err := perm.DenyWrites(filepath.Dir(f.statePath))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := restore(); err != nil {
			t.Error(err)
		}
	})
	_, err = SubmitVerdictWithAuthority(f.root, f.taskID, "REJECTED", "persist this blocker", f.stale, "", f.commit)
	if err == nil || !strings.Contains(err.Error(), "not saved") || strings.Contains(err.Error(), "evidence saved as") {
		t.Fatalf("persistence failure misreported: %v", err)
	}
	if !bytes.Equal(before, readStateBytes(t, f.statePath)) {
		t.Fatal("failed persistence partially changed state")
	}
}

func TestQuarantinedVerdictAdministrativeFailuresDoNotCreateEvidence(t *testing.T) {
	for _, tc := range []struct{ name, verdict, reason, commit, generation, task string }{
		{name: "missing authority", verdict: "REJECTED", reason: "blocker", commit: strings.Repeat("a", 40)},
		{name: "missing commit", verdict: "REJECTED", reason: "blocker", generation: "stale"},
		{name: "malformed commit", verdict: "REJECTED", reason: "blocker", commit: "abc123", generation: "stale"},
		{name: "invalid verdict", verdict: "MAYBE", reason: "blocker", commit: strings.Repeat("a", 40), generation: "stale"},
		{name: "blank rejection", verdict: "REJECTED", reason: " \n\t", commit: strings.Repeat("a", 40), generation: "stale"},
		{name: "oversize rejection", verdict: "REJECTED", reason: strings.Repeat("x", 4097), commit: strings.Repeat("a", 40), generation: "stale"},
		{name: "oversize approval", verdict: "APPROVED", reason: strings.Repeat("x", 4097), commit: strings.Repeat("a", 40), generation: "stale"},
		{name: "unknown task", task: "absent", verdict: "REJECTED", reason: "blocker", commit: strings.Repeat("a", 40), generation: "stale"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newQuarantineFixture(t)
			if tc.task == "" {
				tc.task = f.taskID
			}
			before := f.read(t)
			_, err := SubmitVerdictWithAuthority(f.root, tc.task, tc.verdict, tc.reason, models.AgentAuthority{ID: f.stale.ID, Generation: tc.generation}, "", tc.commit)
			if err == nil {
				t.Fatal("invalid submission accepted")
			}
			after := f.read(t)
			if len(after.QuarantinedVerdicts) != 0 || !reflect.DeepEqual(before.Tasks, after.Tasks) || !reflect.DeepEqual(before.Agents, after.Agents) {
				t.Fatal("administrative failure produced evidence or lifecycle mutation")
			}
		})
	}
}

func TestQuarantinedVerdictMasksAllKnownGenerations(t *testing.T) {
	f := newQuarantineFixture(t)
	reason := "缺少 version: " + f.stale.Generation + "; current=" + f.current.Generation + "; operator=" + f.orchestrator.Generation
	finding := f.capture(t, "REJECTED", reason, f.commit)
	serialized, err := json.Marshal(finding)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{f.stale.Generation, f.current.Generation, f.orchestrator.Generation} {
		if strings.Contains(string(serialized), secret) {
			t.Fatal("stored evidence exposes registration authority")
		}
	}
	if !utf8.ValidString(finding.Reason) || !strings.Contains(finding.Reason, "缺少 version:") || len(finding.Reason) > 4096 {
		t.Fatal("masking corrupted or discarded bounded substantive reason")
	}
}

func TestQuarantinedVerdictApprovalBarrierAndLineage(t *testing.T) {
	for _, tc := range []struct {
		name, verdict                            string
		hops                                     int
		unrelated, unmatched, reverse, ambiguous bool
		blocked                                  bool
	}{
		{name: "same boundary rejection", verdict: "REJECTED", blocked: true},
		{name: "matching approval", verdict: "APPROVED"},
		{name: "updated boundary", verdict: "REJECTED", hops: 1, blocked: true},
		{name: "two update edges", verdict: "REJECTED", hops: 2, blocked: true},
		{name: "unrelated resubmission", verdict: "REJECTED", unrelated: true},
		{name: "unmatched stays non-gating", verdict: "REJECTED", unmatched: true},
		{name: "reversed chronology", verdict: "REJECTED", hops: 2, reverse: true, blocked: true},
		{name: "ambiguous update", verdict: "REJECTED", hops: 1, ambiguous: true, blocked: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newQuarantineFixture(t)
			captureCommit := f.commit
			if tc.unmatched {
				captureCommit = strings.Repeat("d", 40)
			}
			finding := f.capture(t, tc.verdict, "boundary judgment", captureCommit)
			if finding.Matched == tc.unmatched {
				t.Fatal("capture binding classification incorrect")
			}
			f.mutate(t, func(state *models.State) {
				task := state.FindTask(f.taskID)
				prev := f.commit
				edges := []models.TaskHistoryEntry{}
				for i := 0; i < tc.hops; i++ {
					next := strings.Repeat(string(rune('b'+i)), 40)
					edges = append(edges, models.TaskHistoryEntry{Time: time.Now().UTC().Add(time.Duration(i) * time.Second), Event: models.TaskEventReviewCommitUpdated, Agent: &f.orchestrator.ID, Commit: &next, Extra: map[string]any{"old_review_commit": prev, "new_review_commit": next}})
					prev = next
				}
				if tc.reverse {
					edges[0], edges[1] = edges[1], edges[0]
				}
				if tc.ambiguous {
					other := strings.Repeat("e", 40)
					edges = append(edges, models.TaskHistoryEntry{Time: time.Now().UTC().Add(3 * time.Second), Event: models.TaskEventReviewCommitUpdated, Agent: &f.orchestrator.ID, Commit: &other, Extra: map[string]any{"old_review_commit": f.commit, "new_review_commit": other}})
				}
				task.History = append(task.History, edges...)
				if tc.unrelated {
					prev = strings.Repeat("c", 40)
				}
				if tc.unmatched {
					prev = captureCommit
				}
				task.ReviewCommit = &prev
			})
			before := f.read(t)
			_, err := SubmitVerdictWithAuthority(f.root, f.taskID, "APPROVED", "", f.current, "", *before.Tasks[0].ReviewCommit)
			if tc.blocked {
				if err == nil || !strings.Contains(err.Error(), finding.ID) {
					t.Fatalf("approval did not report quarantine hold: %v", err)
				}
				if after := f.read(t); !reflect.DeepEqual(before.Tasks, after.Tasks) || !reflect.DeepEqual(before.Agents, after.Agents) {
					t.Fatal("blocked approval changed lifecycle state")
				}
			} else if err != nil {
				t.Fatalf("nonconflicting evidence blocked approval: %v", err)
			}
		})
	}
}

func TestQuarantinedVerdictRejectionDoesNotConsumeStaleApproval(t *testing.T) {
	f := newQuarantineFixture(t)
	finding := f.capture(t, "APPROVED", "", f.commit)
	if _, err := SubmitVerdictWithAuthority(f.root, f.taskID, "REJECTED", "current reviewer confirms defect", f.current, "", f.commit); err != nil {
		t.Fatalf("stale approval prevented current rejection: %v", err)
	}
	rejected := f.read(t)
	if rejected.Tasks[0].Status != models.TaskStatusRejected || rejected.Tasks[0].ReviewCommit != nil {
		t.Fatal("rejection did not preserve attempt-clearing invariant")
	}
	f.mutate(t, func(state *models.State) {
		task := state.FindTask(f.taskID)
		task.Status = models.TaskStatusReviewing
		task.ReviewCommit = &f.commit
		task.ReviewingBy = &f.current.ID
		lease := time.Now().UTC().Add(time.Hour)
		task.ReviewLeaseExpires = &lease
	})
	if _, err := SubmitVerdictWithAuthority(f.root, f.taskID, "APPROVED", "", f.current, "", f.commit); err == nil || !strings.Contains(err.Error(), finding.ID) {
		t.Fatalf("unchanged boundary lost approval/rejection conflict: %v", err)
	}
}

func TestQuarantinedVerdictReconciliationAuditAndAuthority(t *testing.T) {
	for _, disposition := range []string{"accepted", "refuted", "superseded", "escalated"} {
		t.Run(disposition, func(t *testing.T) {
			f := newQuarantineFixture(t)
			finding := f.capture(t, "REJECTED", "known defect", f.commit)
			before := f.read(t)
			for _, authority := range []models.AgentAuthority{{ID: f.orchestrator.ID}, {ID: f.orchestrator.ID, Generation: "expired"}, f.current} {
				if err := ReconcileVerdict(f.root, f.taskID, finding.ID, disposition, "operator judgment", authority); err == nil {
					t.Fatal("unauthorized reconciliation succeeded")
				}
				if after := f.read(t); !reflect.DeepEqual(before, after) {
					t.Fatal("unauthorized reconciliation mutated state")
				}
			}
			if err := ReconcileVerdict(f.root, f.taskID, finding.ID, disposition, "operator judgment", f.orchestrator); err != nil {
				t.Fatal(err)
			}
			after := f.read(t)
			if !reflect.DeepEqual(before.Tasks, after.Tasks) || !reflect.DeepEqual(before.Agents, after.Agents) {
				t.Fatal("reconciliation manufactured lifecycle authority")
			}
			audit := after.QuarantinedVerdicts[0].Reconciliations
			if len(audit) != 1 || audit[0].Actor != f.orchestrator.ID || audit[0].Disposition != disposition || audit[0].Reason != "operator judgment" || audit[0].Timestamp.IsZero() {
				t.Fatal("reconciliation audit incomplete")
			}
			if err := ReconcileVerdict(f.root, f.taskID, finding.ID, disposition, "operator judgment", f.orchestrator); err != nil {
				t.Fatal(err)
			}
			if len(f.read(t).QuarantinedVerdicts[0].Reconciliations) != 1 {
				t.Fatal("identical reconciliation duplicated audit")
			}
			_, err := SubmitVerdictWithAuthority(f.root, f.taskID, "APPROVED", "", f.current, "", f.commit)
			if disposition == "refuted" || disposition == "superseded" {
				if err != nil {
					t.Fatalf("resolved finding blocked approval: %v", err)
				}
			} else {
				if err == nil {
					t.Fatal("accepted/escalated rejection lost hold")
				}
				if err := ReconcileVerdict(f.root, f.taskID, finding.ID, "refuted", "later evidence disproves finding", f.orchestrator); err != nil {
					t.Fatal(err)
				}
				if audit := f.read(t).QuarantinedVerdicts[0].Reconciliations; len(audit) != 2 || audit[0].Disposition != disposition || audit[1].Disposition != "refuted" {
					t.Fatal("later decision overwrote previous audit")
				}
			}
		})
	}
}

func TestQuarantinedVerdictMergeOrderingAndAlreadyAncestor(t *testing.T) {
	for _, order := range []string{"evidence first", "already ancestor", "merge first"} {
		t.Run(order, func(t *testing.T) {
			root, statePath := setupMergeTestRepo(t, "quarantine-merge", "coder-1")
			state := readStateForTest(t, statePath)
			commit := *state.Tasks[0].ReviewCommit
			f := quarantineFixture{root: root, statePath: statePath, taskID: "quarantine-merge", commit: commit, stale: models.AgentAuthority{ID: "code-reviewer-1", Generation: "old-reviewer"}}
			f.mutate(t, func(state *models.State) {
				state.Agents[f.stale.ID] = models.Agent{Role: "code-reviewer", Status: models.AgentStatusIdle, Generation: "replacement-reviewer"}
			})
			if order == "merge first" {
				if _, err := MergeWorktree(root, f.taskID, "coder-1"); err != nil {
					t.Fatal(err)
				}
				before := f.read(t)
				f.capture(t, "REJECTED", "late discovered defect", commit)
				if after := f.read(t); !reflect.DeepEqual(before.Tasks, after.Tasks) || after.Tasks[0].Status != models.TaskStatusMerged {
					t.Fatal("late finding reopened completed merge")
				}
				return
			}
			if order == "already ancestor" {
				if output, err := exec.Command("git", "-C", root, "merge", "--ff-only", commit).CombinedOutput(); err != nil {
					t.Fatalf("prepare already advanced integration: %v %s", err, output)
				}
			}
			finding := f.capture(t, "REJECTED", "late discovered defect", commit)
			before := f.read(t)
			beforeHEAD, err := exec.Command("git", "-C", root, "rev-parse", "integration").Output()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := MergeWorktree(root, f.taskID, "coder-1"); err == nil || !strings.Contains(err.Error(), finding.ID) {
				t.Fatalf("merge bypassed quarantine: %v", err)
			}
			afterHEAD, err := exec.Command("git", "-C", root, "rev-parse", "integration").Output()
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(beforeHEAD, afterHEAD) || !reflect.DeepEqual(before.Tasks, f.read(t).Tasks) {
				t.Fatal("held merge changed ref or task")
			}
		})
	}
}

func TestQuarantinedVerdictConcurrentMergeSubmission(t *testing.T) {
	for _, evidenceFirst := range []bool{true, false} {
		t.Run(fmt.Sprintf("evidence-first=%t", evidenceFirst), func(t *testing.T) {
			root, statePath := setupMergeTestRepo(t, "concurrent-quarantine", "coder-1")
			state := readStateForTest(t, statePath)
			commit := *state.Tasks[0].ReviewCommit
			f := quarantineFixture{root: root, statePath: statePath, taskID: "concurrent-quarantine", commit: commit, stale: models.AgentAuthority{ID: "code-reviewer-1", Generation: "old-reviewer"}}
			f.mutate(t, func(state *models.State) {
				generation := "replacement-reviewer"
				if evidenceFirst {
					generation = f.stale.Generation
					task := state.FindTask(f.taskID)
					task.Status = models.TaskStatusReviewing
					task.ApprovedBy = nil
					task.ReviewingBy = &f.stale.ID
					lease := time.Now().UTC().Add(time.Hour)
					task.ReviewLeaseExpires = &lease
				}
				state.Agents[f.stale.ID] = models.Agent{Role: "code-reviewer", Status: models.AgentStatusReviewing, Generation: generation, CurrentTask: &f.taskID}
			})
			entered := make(chan struct{})
			release := make(chan struct{})
			var releaseOnce sync.Once
			releaseBarrier := func() { releaseOnce.Do(func() { close(release) }) }
			defer releaseBarrier()
			previousSubmit, previousMerge := testSubmitVerdictHooks, mergeFinalStateTestHook
			t.Cleanup(func() { testSubmitVerdictHooks = previousSubmit; mergeFinalStateTestHook = previousMerge })
			if evidenceFirst {
				testSubmitVerdictHooks = &submitVerdictTestHooks{beforeModify: func() {
					// The winning generation approved this same commit while the
					// losing rejection was in flight. Without quarantined evidence,
					// the pending merge now has a valid approved task to consume.
					f.mutate(t, func(state *models.State) {
						now := time.Now().UTC()
						agent := state.Agents[f.stale.ID]
						agent.Generation = "replacement-reviewer"
						agent.Status = models.AgentStatusIdle
						agent.CurrentTask = nil
						state.Agents[f.stale.ID] = agent
						task := state.FindTask(f.taskID)
						task.Status = models.TaskStatusApproved
						task.ApprovedBy = &f.stale.ID
						task.Approvals = []models.Approval{{Agent: f.stale.ID, Timestamp: now}}
						task.ReviewingBy = nil
						task.ReviewLeaseExpires = nil
						task.History = append(task.History, models.TaskHistoryEntry{Time: now, Event: models.TaskEventApproved, Agent: &f.stale.ID, Commit: &commit})
					})
					close(entered)
					<-release
				}}
			} else {
				mergeFinalStateTestHook = func() { close(entered); <-release }
			}
			submitResult, mergeResult := make(chan error, 1), make(chan error, 1)
			submit := func() {
				_, err := SubmitVerdictWithAuthority(root, f.taskID, "REJECTED", "concurrent substantive blocker", f.stale, "", commit)
				submitResult <- err
			}
			merge := func() { _, err := MergeWorktree(root, f.taskID, "coder-1"); mergeResult <- err }
			if evidenceFirst {
				go submit()
			} else {
				go merge()
			}
			select {
			case <-entered:
			case <-time.After(10 * time.Second):
				t.Fatal("first operation never reached ordering barrier")
			}
			if evidenceFirst {
				go merge()
			} else {
				go submit()
			}
			secondResult := submitResult
			if evidenceFirst {
				secondResult = mergeResult
			}
			select {
			case err := <-secondResult:
				t.Fatalf("second operation escaped held task review lock: %v", err)
			case <-time.After(100 * time.Millisecond):
			}
			if len(f.read(t).QuarantinedVerdicts) != 0 {
				t.Fatal("evidence became visible before first operation released barrier")
			}
			releaseBarrier()
			var submitErr, mergeErr error
			select {
			case submitErr = <-submitResult:
			case <-time.After(10 * time.Second):
				t.Fatal("submission did not finish")
			}
			select {
			case mergeErr = <-mergeResult:
			case <-time.After(10 * time.Second):
				t.Fatal("merge did not finish")
			}
			if !IsAgentAuthorityError(submitErr) {
				t.Fatalf("submission lost authority fence: %v", submitErr)
			}
			after := f.read(t)
			if len(after.QuarantinedVerdicts) != 1 {
				t.Fatal("concurrent ordering lost finding")
			}
			if evidenceFirst {
				if mergeErr == nil || !strings.Contains(mergeErr.Error(), after.QuarantinedVerdicts[0].ID) {
					t.Fatalf("merge ignored earlier evidence: %v", mergeErr)
				}
				if after.Tasks[0].Status == models.TaskStatusMerged {
					t.Fatal("evidence-first task merged")
				}
			} else if mergeErr != nil || after.Tasks[0].Status != models.TaskStatusMerged || !strings.Contains(submitErr.Error(), "already merged") {
				t.Fatalf("merge-first ordering lost terminal state or guidance: merge=%v submit=%v", mergeErr, submitErr)
			}
		})
	}
}
