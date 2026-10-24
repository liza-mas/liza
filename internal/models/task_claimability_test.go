package models

import (
	"fmt"
	"testing"
)

// claimTestResolver is a minimal PipelineResolver for IsClaimable tests.
type claimTestResolver struct {
	doer      string
	reviewer  string
	initial   TaskStatus
	rejected  TaskStatus
	submitted TaskStatus
	partial   TaskStatus
}

func (r *claimTestResolver) DoerRole(string) (string, error)     { return r.doer, nil }
func (r *claimTestResolver) ReviewerRole(string) (string, error) { return r.reviewer, nil }
func (r *claimTestResolver) RoleType(role string) (string, error) {
	switch role {
	case r.doer:
		return "doer", nil
	case r.reviewer:
		return "reviewer", nil
	default:
		return "", fmt.Errorf("unknown role %q", role)
	}
}
func (r *claimTestResolver) AllRoleNames() []string { return []string{r.doer, r.reviewer} }
func (r *claimTestResolver) InitialStatus(string) (TaskStatus, error) {
	return r.initial, nil
}
func (r *claimTestResolver) RejectedStatus(string) (TaskStatus, error) {
	return r.rejected, nil
}
func (r *claimTestResolver) SubmittedStatus(string) (TaskStatus, error) {
	return r.submitted, nil
}
func (r *claimTestResolver) ReviewingStatus(string) (TaskStatus, error) {
	return "REVIEWING", nil
}
func (r *claimTestResolver) ExecutingStatus(string) (TaskStatus, error) {
	return "EXECUTING", nil
}
func (r *claimTestResolver) ApprovedStatus(string) (TaskStatus, error) {
	return "APPROVED", nil
}
func (r *claimTestResolver) PartiallyApprovedStatus(string) (TaskStatus, error) {
	if r.partial == "" {
		return "", fmt.Errorf("no partial status")
	}
	return r.partial, nil
}
func (r *claimTestResolver) Reviewing2Status(string) (TaskStatus, error) {
	return "", fmt.Errorf("no reviewing2 status")
}

func TestIsClaimable(t *testing.T) {
	pr := &claimTestResolver{
		doer:      "coder",
		reviewer:  "code-reviewer",
		initial:   "DRAFT_CODE",
		rejected:  "CODE_REJECTED",
		submitted: "CODE_READY_FOR_REVIEW",
		partial:   "CODE_PARTIALLY_APPROVED",
	}

	t.Run("doer claimable at initial status", func(t *testing.T) {
		task := &Task{
			RolePair: "coding-pair",
			Status:   "DRAFT_CODE",
		}
		// Role uses runtime/hyphenated form directly — no ToRuntime conversion.
		if !task.IsClaimable("coder", nil, pr) {
			t.Error("doer should be claimable at initial status")
		}
	})

	t.Run("doer claimable at rejected status", func(t *testing.T) {
		task := &Task{
			RolePair: "coding-pair",
			Status:   "CODE_REJECTED",
		}
		if !task.IsClaimable("coder", nil, pr) {
			t.Error("doer should be claimable at rejected status")
		}
	})

	t.Run("doer claimable at integration failed", func(t *testing.T) {
		task := &Task{
			RolePair: "coding-pair",
			Status:   TaskStatusIntegrationFailed,
		}
		if !task.IsClaimable("coder", nil, pr) {
			t.Error("doer should be claimable at INTEGRATION_FAILED")
		}
	})

	t.Run("doer not claimable at submitted status", func(t *testing.T) {
		task := &Task{
			RolePair: "coding-pair",
			Status:   "CODE_READY_FOR_REVIEW",
		}
		if task.IsClaimable("coder", nil, pr) {
			t.Error("doer should not be claimable at submitted status")
		}
	})

	t.Run("reviewer claimable at submitted status", func(t *testing.T) {
		rc := "abc123"
		task := &Task{
			RolePair:     "coding-pair",
			Status:       "CODE_READY_FOR_REVIEW",
			ReviewCommit: &rc,
		}
		if !task.IsClaimable("code-reviewer", nil, pr) {
			t.Error("reviewer should be claimable at submitted status")
		}
	})

	t.Run("reviewer claimable at partially approved", func(t *testing.T) {
		rc := "abc123"
		task := &Task{
			RolePair:     "coding-pair",
			Status:       "CODE_PARTIALLY_APPROVED",
			ReviewCommit: &rc,
		}
		if !task.IsClaimable("code-reviewer", nil, pr) {
			t.Error("reviewer should be claimable at partially approved status")
		}
	})

	t.Run("reviewer not claimable without review_commit", func(t *testing.T) {
		task := &Task{
			RolePair: "coding-pair",
			Status:   "CODE_READY_FOR_REVIEW",
		}
		if task.IsClaimable("code-reviewer", nil, pr) {
			t.Error("reviewer should not be claimable without review_commit (corrupted state)")
		}
	})

	t.Run("reviewer not claimable at initial status", func(t *testing.T) {
		task := &Task{
			RolePair: "coding-pair",
			Status:   "DRAFT_CODE",
		}
		if task.IsClaimable("code-reviewer", nil, pr) {
			t.Error("reviewer should not be claimable at initial status")
		}
	})

	t.Run("reviewer not claimable when ReviewingBy is set", func(t *testing.T) {
		rc := "abc123"
		reviewer := "reviewer-1"
		task := &Task{
			RolePair:     "coding-pair",
			Status:       "CODE_READY_FOR_REVIEW",
			ReviewCommit: &rc,
			ReviewingBy:  &reviewer,
		}
		if task.IsClaimable("code-reviewer", nil, pr) {
			t.Error("reviewer should not be claimable when ReviewingBy is set")
		}
	})

	t.Run("unknown role not claimable", func(t *testing.T) {
		task := &Task{
			RolePair: "coding-pair",
			Status:   "DRAFT_CODE",
		}
		if task.IsClaimable("unknown-role", nil, pr) {
			t.Error("unknown role should not be claimable")
		}
	})

	t.Run("nil resolver returns false", func(t *testing.T) {
		task := &Task{
			RolePair: "coding-pair",
			Status:   "DRAFT_CODE",
		}
		if task.IsClaimable("coder", nil, nil) {
			t.Error("nil resolver should return false")
		}
	})

	t.Run("empty role_pair returns false", func(t *testing.T) {
		task := &Task{
			Status: "DRAFT_CODE",
		}
		if task.IsClaimable("coder", nil, pr) {
			t.Error("empty role_pair should return false")
		}
	})

	t.Run("dependency not satisfied blocks claim", func(t *testing.T) {
		allTasks := []Task{
			{ID: "dep-1", Status: TaskStatusImplementing},
		}
		task := &Task{
			RolePair:  "coding-pair",
			Status:    "DRAFT_CODE",
			DependsOn: []string{"dep-1"},
		}
		if task.IsClaimable("coder", allTasks, pr) {
			t.Error("unmet dependency should block claim")
		}
	})

	t.Run("dependency satisfied allows claim", func(t *testing.T) {
		allTasks := []Task{
			{ID: "dep-1", Status: TaskStatusMerged},
		}
		task := &Task{
			RolePair:  "coding-pair",
			Status:    "DRAFT_CODE",
			DependsOn: []string{"dep-1"},
		}
		if !task.IsClaimable("coder", allTasks, pr) {
			t.Error("met dependency should allow claim")
		}
	})

	t.Run("superseded dependency without replacement blocks claim", func(t *testing.T) {
		allTasks := []Task{
			{ID: "dep-1", Status: TaskStatusSuperseded},
		}
		task := &Task{
			RolePair:  "coding-pair",
			Status:    "DRAFT_CODE",
			DependsOn: []string{"dep-1"},
		}
		if task.IsClaimable("coder", allTasks, pr) {
			t.Error("superseded dependency without replacement should block claim")
		}
	})

	t.Run("superseded dependency with merged replacement blocks claim until edge is rewritten", func(t *testing.T) {
		allTasks := []Task{
			{ID: "dep-1", Status: TaskStatusSuperseded, SupersededBy: []string{"dep-2"}},
			{ID: "dep-2", Status: TaskStatusMerged},
		}
		task := &Task{
			RolePair:  "coding-pair",
			Status:    "DRAFT_CODE",
			DependsOn: []string{"dep-1"},
		}
		if task.IsClaimable("coder", allTasks, pr) {
			t.Error("superseded dependency should not be resolved at claim time")
		}
	})
}

func TestIsClaimable_SentinelAssignedToReturnsFalse(t *testing.T) {
	pr := &claimTestResolver{
		doer:      "coder",
		reviewer:  "code-reviewer",
		initial:   "DRAFT_CODE",
		rejected:  "CODE_REJECTED",
		submitted: "CODE_READY_FOR_REVIEW",
	}
	sentinel := "$transitioning"
	task := &Task{
		RolePair:   "coding-pair",
		Status:     "DRAFT_CODE",
		AssignedTo: &sentinel,
	}
	if task.IsClaimable("coder", nil, pr) {
		t.Error("IsClaimable should return false when AssignedTo starts with '$'")
	}
}
