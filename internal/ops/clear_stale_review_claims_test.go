package ops

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestClearStaleReviewClaims_NoStale(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	setupLogFile(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()

	// REVIEWING task with future lease — not stale
	futureLease := now.Add(30 * time.Minute)
	reviewer := "code-reviewer-1"
	state.Tasks = []models.Task{
		{
			ID: "t1", Description: "Active review", Status: models.TaskStatusReviewing,
			Priority: 1, Created: now, SpecRef: "README.md", DoneWhen: "Done", Scope: "Test",
			RolePair:    "coding-pair",
			ReviewingBy: &reviewer, ReviewLeaseExpires: &futureLease,
			History: []models.TaskHistoryEntry{},
		},
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	cleared, err := ClearStaleReviewClaims(tmpDir)
	if err != nil {
		t.Fatalf("ClearStaleReviewClaims() error: %v", err)
	}
	if cleared != 0 {
		t.Errorf("cleared = %d, want 0", cleared)
	}
}

func TestClearStaleReviewClaims_ExpiredLease(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	setupLogFile(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()

	// REVIEWING task with expired lease
	expiredLease := now.Add(-5 * time.Minute)
	reviewer := "code-reviewer-1"
	coder := "coder-1"
	state.Tasks = []models.Task{
		{
			ID: "t1", Description: "Stale review", Status: models.TaskStatusReviewing,
			Priority: 1, Created: now, SpecRef: "README.md", DoneWhen: "Done", Scope: "Test",
			RolePair:   "coding-pair",
			AssignedTo: &coder, ReviewingBy: &reviewer, ReviewLeaseExpires: &expiredLease,
			History: []models.TaskHistoryEntry{},
		},
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	cleared, err := ClearStaleReviewClaims(tmpDir)
	if err != nil {
		t.Fatalf("ClearStaleReviewClaims() error: %v", err)
	}
	if cleared != 1 {
		t.Errorf("cleared = %d, want 1", cleared)
	}

	// Verify state: should be READY_FOR_REVIEW, reviewer cleared
	readState := readStateForTest(t, stateFile)
	task := readState.FindTask("t1")
	if task == nil {
		t.Fatal("Task not found")
	}
	if task.Status != models.TaskStatusReadyForReview {
		t.Errorf("Status = %v, want CODE_READY_FOR_REVIEW", task.Status)
	}
	if task.ReviewingBy != nil {
		t.Errorf("ReviewingBy should be nil, got %v", *task.ReviewingBy)
	}
	if task.ReviewLeaseExpires != nil {
		t.Error("ReviewLeaseExpires should be nil")
	}
}

func TestClearStaleReviewClaims_MissingLease(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	setupLogFile(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()

	// REVIEWING task with reviewer but no lease (malformed state)
	reviewer := "code-reviewer-1"
	state.Tasks = []models.Task{
		{
			ID: "t1", Description: "Malformed review", Status: models.TaskStatusReviewing,
			Priority: 1, Created: now, SpecRef: "README.md", DoneWhen: "Done", Scope: "Test",
			RolePair:    "coding-pair",
			ReviewingBy: &reviewer, // no ReviewLeaseExpires
			History:     []models.TaskHistoryEntry{},
		},
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	cleared, err := ClearStaleReviewClaims(tmpDir)
	if err != nil {
		t.Fatalf("ClearStaleReviewClaims() error: %v", err)
	}
	if cleared != 1 {
		t.Errorf("cleared = %d, want 1 (malformed lease treated as expired)", cleared)
	}
}

func TestClearStaleReviewClaims_SkipsNonReviewing(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	setupLogFile(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()

	// IMPLEMENTING task should be skipped entirely
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("t1", models.TaskStatusImplementing, now),
		testhelpers.BuildTaskByStatus("t2", models.TaskStatusReady, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	cleared, err := ClearStaleReviewClaims(tmpDir)
	if err != nil {
		t.Fatalf("ClearStaleReviewClaims() error: %v", err)
	}
	if cleared != 0 {
		t.Errorf("cleared = %d, want 0", cleared)
	}
}

func TestClearStaleReviewClaims_MultipleStale(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	setupLogFile(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()

	expiredLease := now.Add(-10 * time.Minute)
	reviewer1 := "code-reviewer-1"
	reviewer2 := "code-reviewer-2"
	state.Tasks = []models.Task{
		{
			ID: "t1", Description: "Stale 1", Status: models.TaskStatusReviewing,
			Priority: 1, Created: now, SpecRef: "README.md", DoneWhen: "Done", Scope: "Test",
			RolePair:    "coding-pair",
			ReviewingBy: &reviewer1, ReviewLeaseExpires: &expiredLease,
			History: []models.TaskHistoryEntry{},
		},
		{
			ID: "t2", Description: "Stale 2", Status: models.TaskStatusReviewing,
			Priority: 1, Created: now, SpecRef: "README.md", DoneWhen: "Done", Scope: "Test",
			RolePair:    "coding-pair",
			ReviewingBy: &reviewer2, ReviewLeaseExpires: &expiredLease,
			History: []models.TaskHistoryEntry{},
		},
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	cleared, err := ClearStaleReviewClaims(tmpDir)
	if err != nil {
		t.Fatalf("ClearStaleReviewClaims() error: %v", err)
	}
	if cleared != 2 {
		t.Errorf("cleared = %d, want 2", cleared)
	}
}

func TestClearStaleReviewingTwo(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	setupLogFile(t, tmpDir)

	now := time.Now().UTC()

	t.Run("expired reviewing_2 reverts to partially_approved", func(t *testing.T) {
		state := testhelpers.CreateValidState()
		expiredLease := now.Add(-5 * time.Minute)
		reviewer := "code-reviewer-2"
		coder := "coder-1"
		state.Tasks = []models.Task{
			{
				ID: "t1", Description: "Second review stale", Status: "REVIEWING_CODE_2",
				Priority: 1, Created: now, SpecRef: "README.md", DoneWhen: "Done", Scope: "Test",
				RolePair:   "coding-pair",
				AssignedTo: &coder, ReviewingBy: &reviewer, ReviewLeaseExpires: &expiredLease,
				History: []models.TaskHistoryEntry{},
			},
		}
		testhelpers.WriteInitialState(t, stateFile, state)

		cleared, err := ClearStaleReviewClaims(tmpDir)
		if err != nil {
			t.Fatalf("ClearStaleReviewClaims() error: %v", err)
		}
		if cleared != 1 {
			t.Errorf("cleared = %d, want 1", cleared)
		}

		readState := readStateForTest(t, stateFile)
		task := readState.FindTask("t1")
		if task == nil {
			t.Fatal("Task not found")
		}
		if task.Status != "CODE_PARTIALLY_APPROVED" {
			t.Errorf("Status = %v, want CODE_PARTIALLY_APPROVED", task.Status)
		}
		if task.ReviewingBy != nil {
			t.Errorf("ReviewingBy should be nil, got %v", *task.ReviewingBy)
		}
		if task.ReviewLeaseExpires != nil {
			t.Error("ReviewLeaseExpires should be nil")
		}
	})

	t.Run("expired reviewing still reverts to submitted", func(t *testing.T) {
		state := testhelpers.CreateValidState()
		expiredLease := now.Add(-5 * time.Minute)
		reviewer := "code-reviewer-1"
		coder := "coder-1"
		state.Tasks = []models.Task{
			{
				ID: "t1", Description: "First review stale", Status: models.TaskStatusReviewing,
				Priority: 1, Created: now, SpecRef: "README.md", DoneWhen: "Done", Scope: "Test",
				RolePair:   "coding-pair",
				AssignedTo: &coder, ReviewingBy: &reviewer, ReviewLeaseExpires: &expiredLease,
				History: []models.TaskHistoryEntry{},
			},
		}
		testhelpers.WriteInitialState(t, stateFile, state)

		cleared, err := ClearStaleReviewClaims(tmpDir)
		if err != nil {
			t.Fatalf("ClearStaleReviewClaims() error: %v", err)
		}
		if cleared != 1 {
			t.Errorf("cleared = %d, want 1", cleared)
		}

		readState := readStateForTest(t, stateFile)
		task := readState.FindTask("t1")
		if task == nil {
			t.Fatal("Task not found")
		}
		if task.Status != models.TaskStatusReadyForReview {
			t.Errorf("Status = %v, want CODE_READY_FOR_REVIEW", task.Status)
		}
	})

	t.Run("mixed reviewing and reviewing_2 both cleared", func(t *testing.T) {
		state := testhelpers.CreateValidState()
		expiredLease := now.Add(-5 * time.Minute)
		reviewer1 := "code-reviewer-1"
		reviewer2 := "code-reviewer-2"
		coder := "coder-1"
		state.Tasks = []models.Task{
			{
				ID: "t1", Description: "First review stale", Status: models.TaskStatusReviewing,
				Priority: 1, Created: now, SpecRef: "README.md", DoneWhen: "Done", Scope: "Test",
				RolePair:   "coding-pair",
				AssignedTo: &coder, ReviewingBy: &reviewer1, ReviewLeaseExpires: &expiredLease,
				History: []models.TaskHistoryEntry{},
			},
			{
				ID: "t2", Description: "Second review stale", Status: "REVIEWING_CODE_2",
				Priority: 1, Created: now, SpecRef: "README.md", DoneWhen: "Done", Scope: "Test",
				RolePair:   "coding-pair",
				AssignedTo: &coder, ReviewingBy: &reviewer2, ReviewLeaseExpires: &expiredLease,
				History: []models.TaskHistoryEntry{},
			},
		}
		testhelpers.WriteInitialState(t, stateFile, state)

		cleared, err := ClearStaleReviewClaims(tmpDir)
		if err != nil {
			t.Fatalf("ClearStaleReviewClaims() error: %v", err)
		}
		if cleared != 2 {
			t.Errorf("cleared = %d, want 2", cleared)
		}

		readState := readStateForTest(t, stateFile)
		t1 := readState.FindTask("t1")
		if t1.Status != models.TaskStatusReadyForReview {
			t.Errorf("t1 Status = %v, want CODE_READY_FOR_REVIEW", t1.Status)
		}
		t2 := readState.FindTask("t2")
		if t2.Status != "CODE_PARTIALLY_APPROVED" {
			t.Errorf("t2 Status = %v, want CODE_PARTIALLY_APPROVED", t2.Status)
		}
	})
}

func TestClearStaleReviewClaims_OrphanedOnRejected(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	setupLogFile(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()

	// REJECTED task with ReviewingBy set and expired lease — orphaned from await_resubmission crash
	expiredLease := now.Add(-5 * time.Minute)
	reviewer := "code-reviewer-1"
	state.Tasks = []models.Task{
		{
			ID: "t1", Description: "Rejected with orphaned reviewer", Status: models.TaskStatusRejected,
			Priority: 1, Created: now, SpecRef: "README.md", DoneWhen: "Done", Scope: "Test",
			RolePair:    "coding-pair",
			ReviewingBy: &reviewer, ReviewLeaseExpires: &expiredLease,
			History: []models.TaskHistoryEntry{},
		},
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	cleared, err := ClearStaleReviewClaims(tmpDir)
	if err != nil {
		t.Fatalf("ClearStaleReviewClaims() error: %v", err)
	}
	if cleared != 1 {
		t.Errorf("cleared = %d, want 1", cleared)
	}

	readState := readStateForTest(t, stateFile)
	task := readState.FindTask("t1")
	if task == nil {
		t.Fatal("Task not found")
	}
	if task.Status != models.TaskStatusRejected {
		t.Errorf("Status = %v, want CODE_REJECTED (status should not change)", task.Status)
	}
	if task.ReviewingBy != nil {
		t.Errorf("ReviewingBy should be nil, got %v", *task.ReviewingBy)
	}
	if task.ReviewLeaseExpires != nil {
		t.Error("ReviewLeaseExpires should be nil")
	}
}

func TestClearStaleReviewClaims_OrphanedOnSubmitted(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	setupLogFile(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()

	// SUBMITTED task with ReviewingBy set and expired lease — orphaned from await_resubmission crash
	expiredLease := now.Add(-5 * time.Minute)
	reviewer := "code-reviewer-1"
	state.Tasks = []models.Task{
		{
			ID: "t1", Description: "Submitted with orphaned reviewer", Status: models.TaskStatusReadyForReview,
			Priority: 1, Created: now, SpecRef: "README.md", DoneWhen: "Done", Scope: "Test",
			RolePair:    "coding-pair",
			ReviewingBy: &reviewer, ReviewLeaseExpires: &expiredLease,
			History: []models.TaskHistoryEntry{},
		},
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	cleared, err := ClearStaleReviewClaims(tmpDir)
	if err != nil {
		t.Fatalf("ClearStaleReviewClaims() error: %v", err)
	}
	if cleared != 1 {
		t.Errorf("cleared = %d, want 1", cleared)
	}

	readState := readStateForTest(t, stateFile)
	task := readState.FindTask("t1")
	if task == nil {
		t.Fatal("Task not found")
	}
	if task.Status != models.TaskStatusReadyForReview {
		t.Errorf("Status = %v, want CODE_READY_FOR_REVIEW (status should not change)", task.Status)
	}
	if task.ReviewingBy != nil {
		t.Errorf("ReviewingBy should be nil, got %v", *task.ReviewingBy)
	}
	if task.ReviewLeaseExpires != nil {
		t.Error("ReviewLeaseExpires should be nil")
	}
}

func TestClearStaleReviewClaims_ActiveLeaseOnRejected(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	setupLogFile(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()

	// REJECTED task with ReviewingBy set and valid (future) lease — should NOT be cleared
	futureLease := now.Add(30 * time.Minute)
	reviewer := "code-reviewer-1"
	state.Tasks = []models.Task{
		{
			ID: "t1", Description: "Rejected with active reviewer", Status: models.TaskStatusRejected,
			Priority: 1, Created: now, SpecRef: "README.md", DoneWhen: "Done", Scope: "Test",
			RolePair:    "coding-pair",
			ReviewingBy: &reviewer, ReviewLeaseExpires: &futureLease,
			History: []models.TaskHistoryEntry{},
		},
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	cleared, err := ClearStaleReviewClaims(tmpDir)
	if err != nil {
		t.Fatalf("ClearStaleReviewClaims() error: %v", err)
	}
	if cleared != 0 {
		t.Errorf("cleared = %d, want 0 (active lease should not be cleared)", cleared)
	}

	readState := readStateForTest(t, stateFile)
	task := readState.FindTask("t1")
	if task == nil {
		t.Fatal("Task not found")
	}
	if task.ReviewingBy == nil {
		t.Error("ReviewingBy should still be set")
	} else if *task.ReviewingBy != reviewer {
		t.Errorf("ReviewingBy = %v, want %v", *task.ReviewingBy, reviewer)
	}
}

// setupLogFile creates the log.yaml file that ClearStaleReviewClaims needs.
func setupLogFile(t *testing.T, tmpDir string) {
	t.Helper()
	logPath := filepath.Join(tmpDir, ".liza", "log.yaml")
	if err := os.WriteFile(logPath, []byte(""), 0644); err != nil {
		t.Fatalf("Failed to create log file: %v", err)
	}
	// Also create the lock file for log
	lockPath := logPath + ".lock"
	if err := os.WriteFile(lockPath, []byte(""), 0644); err != nil {
		t.Fatalf("Failed to create log lock file: %v", err)
	}
}
