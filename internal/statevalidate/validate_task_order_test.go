package statevalidate

import (
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// TestValidateTaskInvariants_ReportsDefectsInCheckOrder pins the sequence in
// which validateTaskInvariants emits diagnostics: task by task in state order,
// each task's checks in their fixed order (status fields included, in their
// status-class order), and the cross-task duplicate-assignment pairs last.
func TestValidateTaskInvariants_ReportsDefectsInCheckOrder(t *testing.T) {
	cfg := loadTestConfig(t)
	resolver := pipeline.NewResolver(cfg)
	now := time.Now().UTC()

	broken := testhelpers.BuildTaskByStatus("task-a", models.TaskStatusImplementing, now)
	broken.TerminalArchive = &models.TerminalArchiveRef{SHA256: "zz"}
	broken.StateLockHold = &models.StateLockHold{RefusedFingerprint: "fp"}
	broken.AssignedTo = nil
	broken.Worktree = testhelpers.StringPtr(".worktrees/gone")
	broken.BaseCommit = nil
	broken.DoneWhen = ""
	broken.SpecRef = ""
	broken.EpicRef = ".worktrees/epic.md"
	broken.IntegrationFix = true
	broken.FailedBy = []string{"coder-9", "coder-9"}
	broken.ParentTask = testhelpers.StringPtr("ghost")
	broken.Output = []models.OutputEntry{{}}
	broken.Attempt = 3

	secondExecuting := testhelpers.BuildTaskByStatus("task-b", models.TaskStatusImplementing, now)
	secondExecuting.AssignedTo = testhelpers.StringPtr("coder-1")
	secondExecuting.BaseCommit = nil

	initial := testhelpers.BuildTaskByStatus("task-c", models.TaskStatusReady, now)
	initial.AssignedTo = testhelpers.StringPtr("coder-1")
	initial.Attempt = 2
	initial.Iteration = 3
	initial.ReviewCyclesCurrent = 4

	blocked := testhelpers.BuildTaskByStatus("task-d", models.TaskStatusBlocked, now)
	blocked.AssignedTo = testhelpers.StringPtr("coder-1")
	blocked.BlockedReason = nil
	blocked.BlockedQuestions = nil
	blocked.RepairRequest = &models.RepairRequest{}

	rejected := testhelpers.BuildTaskByStatus("task-e", models.TaskStatusRejected, now)
	rejected.RejectionReason = nil
	rejected.Worktree = testhelpers.StringPtr("elsewhere")
	rejected.AssignedTo = testhelpers.StringPtr("coder-1")
	rejected.LeaseExpires = nil

	err := taskInvariantsErr(stateWithTasks(broken, secondExecuting, initial, blocked, rejected), t.TempDir(), true, resolver, cfg)
	if err == nil {
		t.Fatal("taskInvariantsErr = nil, want the ordered defects")
	}
	got := strings.Split(err.Error(), "\n")
	want := []string{
		"task task-a terminal_archive requires a lowercase SHA-256 digest and archived_at",
		"task task-a state_lock_hold requires an episode, lowercase blocker digest and nonzero publication sequence",
		"task task-a state_lock_hold refusal requires both fingerprint and reason",
		"IMPLEMENTING_CODE task without assigned_to: task-a",
		"IMPLEMENTING_CODE task task-a has worktree=.worktrees/gone but directory does not exist",
		"non-DRAFT task missing done_when: task-a",
		"non-DRAFT task missing spec_ref: task-a",
		"task task-a epic_ref contains worktree prefix (must be repo-relative): .worktrees/epic.md",
		"task task-a has integration_fix:true but no INTEGRATION_FAILED event in history",
		"task task-a has duplicate agent IDs in failed_by (manually edit .liza/state.yaml to remove duplicates)",
		"task task-a has parent_task referencing non-existent task 'ghost'",
		"task task-a output[0] missing desc",
		"task task-a output[0] missing done_when",
		"task task-a output[0] missing scope",
		"task task-a output[0] missing spec_ref",
		"task task-a has invalid attempt value 3",
		"IMPLEMENTING_CODE task without base_commit: task-b",
		"IMPLEMENTING_CODE task task-b has worktree=.worktrees/task-b but directory does not exist",
		"DRAFT_CODE task with assigned_to: task-c",
		"task task-c at attempt 2 in initial status has non-zero iteration 3",
		"task task-c at attempt 2 in initial status has non-zero review_cycles_current 4",
		"BLOCKED task without blocked_reason: task-d",
		"BLOCKED task without blocked_questions: task-d",
		"BLOCKED task repair_request without operation: task-d",
		"BLOCKED task repair_request without target: task-d",
		"BLOCKED task repair_request without command: task-d",
		"BLOCKED task repair_request without evidence: task-d",
		"BLOCKED task repair_request without validation: task-d",
		"CODE_REJECTED task without rejection_reason: task-e",
		"CODE_REJECTED task has worktree=\"elsewhere\", want \".worktrees/task-e\"",
		"CODE_REJECTED task has assigned_to without lease_expires: task-e",
		"agent coder-1 assigned to multiple active tasks simultaneously: [task-b task-c]",
		"agent coder-1 assigned to multiple active tasks simultaneously: [task-b task-d]",
		"agent coder-1 assigned to multiple active tasks simultaneously: [task-b task-e]",
		"agent coder-1 assigned to multiple active tasks simultaneously: [task-c task-d]",
		"agent coder-1 assigned to multiple active tasks simultaneously: [task-c task-e]",
		"agent coder-1 assigned to multiple active tasks simultaneously: [task-d task-e]",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("diagnostics =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
