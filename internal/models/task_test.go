package models

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"
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

	t.Run("superseded dependency satisfies claim", func(t *testing.T) {
		allTasks := []Task{
			{ID: "dep-1", Status: TaskStatusSuperseded},
		}
		task := &Task{
			RolePair:  "coding-pair",
			Status:    "DRAFT_CODE",
			DependsOn: []string{"dep-1"},
		}
		if !task.IsClaimable("coder", allTasks, pr) {
			t.Error("superseded dependency should allow claim")
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

func TestEffectiveAttempt_ZeroReturnsOne(t *testing.T) {
	task := &Task{Attempt: 0}
	if got := task.EffectiveAttempt(); got != 1 {
		t.Errorf("EffectiveAttempt() = %d, want 1 for Attempt=0", got)
	}
}

func TestEffectiveAttempt_ReturnsActualValue(t *testing.T) {
	tests := []struct {
		attempt int
		want    int
	}{
		{1, 1},
		{2, 2},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("Attempt=%d", tt.attempt), func(t *testing.T) {
			task := &Task{Attempt: tt.attempt}
			if got := task.EffectiveAttempt(); got != tt.want {
				t.Errorf("EffectiveAttempt() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestMigrateAttemptedField_ConvertsLegacyList(t *testing.T) {
	task := &Task{
		Extra: map[string]any{
			"attempted": []any{"agent-1"},
		},
	}
	changed := task.MigrateAttemptedField()

	if !changed {
		t.Error("MigrateAttemptedField() = false, want true")
	}
	if task.Attempt != 2 {
		t.Errorf("Attempt = %d, want 2", task.Attempt)
	}
	if _, exists := task.Extra["attempted"]; exists {
		t.Error("Extra[\"attempted\"] should be absent after migration")
	}
}

func TestMigrateAttemptedField_EmptyList(t *testing.T) {
	task := &Task{
		Extra: map[string]any{
			"attempted": []any{},
		},
	}
	changed := task.MigrateAttemptedField()

	if !changed {
		t.Error("MigrateAttemptedField() = false, want true (key deleted)")
	}
	if task.Attempt != 0 {
		t.Errorf("Attempt = %d, want 0 (unchanged)", task.Attempt)
	}
	if _, exists := task.Extra["attempted"]; exists {
		t.Error("Extra[\"attempted\"] should be deleted even for empty list")
	}
}

func TestMigrateAttemptedField_AlreadyMigrated(t *testing.T) {
	task := &Task{
		Attempt: 2,
		Extra: map[string]any{
			"attempted": []any{"agent-1"},
		},
	}
	changed := task.MigrateAttemptedField()

	if changed {
		t.Error("MigrateAttemptedField() = true, want false for already-migrated task")
	}
	if task.Attempt != 2 {
		t.Errorf("Attempt = %d, want 2 (unchanged)", task.Attempt)
	}
}

func TestMigrateAttemptedField_NoLegacyField(t *testing.T) {
	task := &Task{
		Extra: map[string]any{"other": "value"},
	}
	changed := task.MigrateAttemptedField()

	if changed {
		t.Error("MigrateAttemptedField() = true, want false when no legacy field")
	}
	if task.Attempt != 0 {
		t.Errorf("Attempt = %d, want 0 (unchanged)", task.Attempt)
	}
}

func TestMigrateAttemptedField_CapsAtTwo(t *testing.T) {
	task := &Task{
		Extra: map[string]any{
			"attempted": []any{"a", "b", "c"},
		},
	}
	changed := task.MigrateAttemptedField()

	if !changed {
		t.Error("MigrateAttemptedField() = false, want true")
	}
	if task.Attempt != 2 {
		t.Errorf("Attempt = %d, want 2 (capped)", task.Attempt)
	}
	if _, exists := task.Extra["attempted"]; exists {
		t.Error("Extra[\"attempted\"] should be absent after migration")
	}
}

func TestMigrateAttemptedField_WrongType(t *testing.T) {
	task := &Task{
		Extra: map[string]any{
			"attempted": "not-a-list",
		},
	}
	changed := task.MigrateAttemptedField()

	if !changed {
		t.Error("MigrateAttemptedField() = false, want true (key deleted)")
	}
	if task.Attempt != 0 {
		t.Errorf("Attempt = %d, want 0 (unchanged)", task.Attempt)
	}
	if _, exists := task.Extra["attempted"]; exists {
		t.Error("Extra[\"attempted\"] should be deleted even for wrong type")
	}
}

func TestEffectiveAttempt_LegacyFallback(t *testing.T) {
	task := &Task{
		Attempt: 0,
		Extra:   map[string]any{"attempted": []any{"agent-1"}},
	}
	if got := task.EffectiveAttempt(); got != 2 {
		t.Errorf("EffectiveAttempt() = %d, want 2", got)
	}
}

func TestEffectiveAttempt_LegacyFallbackEmptyList(t *testing.T) {
	task := &Task{
		Attempt: 0,
		Extra:   map[string]any{"attempted": []any{}},
	}
	if got := task.EffectiveAttempt(); got != 1 {
		t.Errorf("EffectiveAttempt() = %d, want 1 (empty list falls through to default)", got)
	}
}

func TestApprovalHelpers(t *testing.T) {
	t.Run("ApprovalCount", func(t *testing.T) {
		t.Run("empty list", func(t *testing.T) {
			task := &Task{}
			if got := task.ApprovalCount(); got != 0 {
				t.Errorf("ApprovalCount() = %d, want 0", got)
			}
		})

		t.Run("nil list", func(t *testing.T) {
			task := &Task{Approvals: nil}
			if got := task.ApprovalCount(); got != 0 {
				t.Errorf("ApprovalCount() = %d, want 0", got)
			}
		})

		t.Run("single approval", func(t *testing.T) {
			task := &Task{
				Approvals: []Approval{
					{Agent: "reviewer-1", Provider: "claude", Timestamp: time.Now()},
				},
			}
			if got := task.ApprovalCount(); got != 1 {
				t.Errorf("ApprovalCount() = %d, want 1", got)
			}
		})

		t.Run("multiple approvals", func(t *testing.T) {
			task := &Task{
				Approvals: []Approval{
					{Agent: "reviewer-1", Provider: "claude", Timestamp: time.Now()},
					{Agent: "reviewer-2", Provider: "codex", Timestamp: time.Now()},
				},
			}
			if got := task.ApprovalCount(); got != 2 {
				t.Errorf("ApprovalCount() = %d, want 2", got)
			}
		})
	})

	t.Run("HasProviderDiversity", func(t *testing.T) {
		t.Run("empty list", func(t *testing.T) {
			task := &Task{}
			if task.HasProviderDiversity() {
				t.Error("HasProviderDiversity() = true, want false for empty list")
			}
		})

		t.Run("single approval", func(t *testing.T) {
			task := &Task{
				Approvals: []Approval{
					{Agent: "reviewer-1", Provider: "claude", Timestamp: time.Now()},
				},
			}
			if task.HasProviderDiversity() {
				t.Error("HasProviderDiversity() = true, want false for single approval")
			}
		})

		t.Run("same provider", func(t *testing.T) {
			task := &Task{
				Approvals: []Approval{
					{Agent: "reviewer-1", Provider: "claude", Timestamp: time.Now()},
					{Agent: "reviewer-2", Provider: "claude", Timestamp: time.Now()},
				},
			}
			if task.HasProviderDiversity() {
				t.Error("HasProviderDiversity() = true, want false for same provider")
			}
		})

		t.Run("diverse providers", func(t *testing.T) {
			task := &Task{
				Approvals: []Approval{
					{Agent: "reviewer-1", Provider: "claude", Timestamp: time.Now()},
					{Agent: "reviewer-2", Provider: "codex", Timestamp: time.Now()},
				},
			}
			if !task.HasProviderDiversity() {
				t.Error("HasProviderDiversity() = false, want true for diverse providers")
			}
		})

		t.Run("three approvals mixed providers", func(t *testing.T) {
			task := &Task{
				Approvals: []Approval{
					{Agent: "reviewer-1", Provider: "claude", Timestamp: time.Now()},
					{Agent: "reviewer-2", Provider: "claude", Timestamp: time.Now()},
					{Agent: "reviewer-3", Provider: "codex", Timestamp: time.Now()},
				},
			}
			if !task.HasProviderDiversity() {
				t.Error("HasProviderDiversity() = false, want true when at least 2 distinct providers exist")
			}
		})
	})

	t.Run("ClearApprovals", func(t *testing.T) {
		t.Run("clears non-empty list", func(t *testing.T) {
			task := &Task{
				Approvals: []Approval{
					{Agent: "reviewer-1", Provider: "claude", Timestamp: time.Now()},
					{Agent: "reviewer-2", Provider: "codex", Timestamp: time.Now()},
				},
			}
			task.ClearApprovals()
			if len(task.Approvals) != 0 {
				t.Errorf("ClearApprovals() left %d approvals, want 0", len(task.Approvals))
			}
		})

		t.Run("clears empty list", func(t *testing.T) {
			task := &Task{}
			task.ClearApprovals()
			if task.Approvals != nil {
				t.Error("ClearApprovals() on empty task should leave nil")
			}
		})

		t.Run("clears nil list", func(t *testing.T) {
			task := &Task{Approvals: nil}
			task.ClearApprovals()
			if task.Approvals != nil {
				t.Error("ClearApprovals() on nil should leave nil")
			}
		})
	})

	t.Run("LastApprover", func(t *testing.T) {
		t.Run("empty list", func(t *testing.T) {
			task := &Task{}
			if got := task.LastApprover(); got != "" {
				t.Errorf("LastApprover() = %q, want empty string", got)
			}
		})

		t.Run("single approval", func(t *testing.T) {
			task := &Task{
				Approvals: []Approval{
					{Agent: "reviewer-1", Provider: "claude", Timestamp: time.Now()},
				},
			}
			if got := task.LastApprover(); got != "reviewer-1" {
				t.Errorf("LastApprover() = %q, want %q", got, "reviewer-1")
			}
		})

		t.Run("multiple approvals returns last", func(t *testing.T) {
			task := &Task{
				Approvals: []Approval{
					{Agent: "reviewer-1", Provider: "claude", Timestamp: time.Now()},
					{Agent: "reviewer-2", Provider: "codex", Timestamp: time.Now()},
				},
			}
			if got := task.LastApprover(); got != "reviewer-2" {
				t.Errorf("LastApprover() = %q, want %q", got, "reviewer-2")
			}
		})
	})
}

func TestOutputEntry_JSONUnmarshal(t *testing.T) {
	// Wire format documented in doer_tools.tmpl and set-task-output CLI help.
	input := `[
		{
			"desc": "Implement auth middleware",
			"done_when": "GET /protected returns 401 without token",
			"scope": "internal/auth",
			"spec_ref": "specs/auth.md",
			"plan_ref": "specs/plans/plan-1.md",
			"arch_ref": "specs/arch-plan/arch-1.md",
			"depends_on": ["0", "2"]
		}
	]`

	var entries []OutputEntry
	if err := json.Unmarshal([]byte(input), &entries); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	e := entries[0]
	if e.Desc != "Implement auth middleware" {
		t.Errorf("Desc = %q", e.Desc)
	}
	if e.DoneWhen != "GET /protected returns 401 without token" {
		t.Errorf("DoneWhen = %q", e.DoneWhen)
	}
	if e.Scope != "internal/auth" {
		t.Errorf("Scope = %q", e.Scope)
	}
	if e.SpecRef != "specs/auth.md" {
		t.Errorf("SpecRef = %q", e.SpecRef)
	}
	if e.PlanRef != "specs/plans/plan-1.md" {
		t.Errorf("PlanRef = %q", e.PlanRef)
	}
	if e.ArchRef != "specs/arch-plan/arch-1.md" {
		t.Errorf("ArchRef = %q", e.ArchRef)
	}
	if len(e.DependsOn) != 2 || e.DependsOn[0] != "0" || e.DependsOn[1] != "2" {
		t.Errorf("DependsOn = %v", e.DependsOn)
	}
}
