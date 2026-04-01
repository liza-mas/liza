package models

import (
	"strings"
	"testing"
	"time"
)

func TestCountClaimableTasks(t *testing.T) {
	pr := &mockPipelineResolver{
		doer:      "coder",         // runtime form
		reviewer:  "code-reviewer", // runtime form
		initial:   TaskStatusReady,
		rejected:  TaskStatusRejected,
		submitted: TaskStatusReadyForReview,
		reviewing: TaskStatusReviewing,
		executing: TaskStatusImplementing,
		approved:  TaskStatusApproved,
	}

	tests := []struct {
		name  string
		state *State
		role  string
		want  int
	}{
		{
			name:  "empty state",
			state: &State{},
			role:  RoleCoder,
			want:  0,
		},
		{
			name: "one READY coding task for coder",
			state: &State{
				Tasks: []Task{
					{ID: "t1", Status: TaskStatusReady, Type: TaskTypeCoding, RolePair: "coding-pair"},
				},
			},
			role: RoleCoder,
			want: 1,
		},
		{
			name: "READY task not claimable by reviewer",
			state: &State{
				Tasks: []Task{
					{ID: "t1", Status: TaskStatusReady, Type: TaskTypeCoding, RolePair: "coding-pair"},
				},
			},
			role: "code-reviewer",
			want: 0,
		},
		{
			name: "mixed statuses",
			state: &State{
				Tasks: []Task{
					{ID: "t1", Status: TaskStatusReady, Type: TaskTypeCoding, RolePair: "coding-pair"},
					{ID: "t2", Status: TaskStatusImplementing, Type: TaskTypeCoding, RolePair: "coding-pair"},
					{ID: "t3", Status: TaskStatusRejected, Type: TaskTypeCoding, RolePair: "coding-pair"},
					{ID: "t4", Status: TaskStatusMerged, Type: TaskTypeCoding, RolePair: "coding-pair"},
				},
			},
			role: RoleCoder,
			want: 2, // READY + REJECTED
		},
		{
			name: "READY_FOR_REVIEW claimable by reviewer",
			state: &State{
				Tasks: []Task{
					{ID: "t1", Status: TaskStatusReadyForReview, Type: TaskTypeCoding, RolePair: "coding-pair", ReviewCommit: strPtr("rc")},
					{ID: "t2", Status: TaskStatusReadyForReview, Type: TaskTypeCoding, RolePair: "coding-pair", ReviewCommit: strPtr("rc")},
				},
			},
			role: "code-reviewer",
			want: 2,
		},
		{
			name: "blocked by unsatisfied dependency",
			state: &State{
				Tasks: []Task{
					{ID: "t1", Status: TaskStatusReady, Type: TaskTypeCoding, RolePair: "coding-pair", DependsOn: []string{"t2"}},
					{ID: "t2", Status: TaskStatusImplementing, Type: TaskTypeCoding, RolePair: "coding-pair"},
				},
			},
			role: RoleCoder,
			want: 0,
		},
		{
			name: "dependency satisfied",
			state: &State{
				Tasks: []Task{
					{ID: "t1", Status: TaskStatusReady, Type: TaskTypeCoding, RolePair: "coding-pair", DependsOn: []string{"t2"}},
					{ID: "t2", Status: TaskStatusMerged, Type: TaskTypeCoding, RolePair: "coding-pair"},
				},
			},
			role: RoleCoder,
			want: 1,
		},
		{
			name: "superseded dependency satisfied",
			state: &State{
				Tasks: []Task{
					{ID: "t1", Status: TaskStatusReady, Type: TaskTypeCoding, RolePair: "coding-pair", DependsOn: []string{"t2"}},
					{ID: "t2", Status: TaskStatusSuperseded, Type: TaskTypeCoding, RolePair: "coding-pair"},
				},
			},
			role: RoleCoder,
			want: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CountClaimableTasks(tt.state, tt.role, pr)
			if got != tt.want {
				t.Errorf("CountClaimableTasks() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestCountReviewableTasks(t *testing.T) {
	pr := &mockPipelineResolver{
		doer:      "coder",         // runtime form
		reviewer:  "code-reviewer", // runtime form
		initial:   TaskStatusReady,
		rejected:  TaskStatusRejected,
		submitted: TaskStatusReadyForReview,
		reviewing: TaskStatusReviewing,
		executing: TaskStatusImplementing,
		approved:  TaskStatusApproved,
	}

	tests := []struct {
		name  string
		state *State
		role  string
		want  int
	}{
		{
			name:  "empty state",
			state: &State{},
			role:  "code-reviewer",
			want:  0,
		},
		{
			name: "one READY_FOR_REVIEW coding task",
			state: &State{
				Tasks: []Task{
					{ID: "t1", Status: TaskStatusReadyForReview, Type: TaskTypeCoding, RolePair: "coding-pair", ReviewCommit: strPtr("rc")},
				},
			},
			role: "code-reviewer",
			want: 1,
		},
		{
			name: "REVIEWING tasks not counted",
			state: &State{
				Tasks: []Task{
					{ID: "t1", Status: TaskStatusReviewing, Type: TaskTypeCoding, RolePair: "coding-pair"},
				},
			},
			role: "code-reviewer",
			want: 0,
		},
		{
			name: "wrong role not counted",
			state: &State{
				Tasks: []Task{
					{ID: "t1", Status: TaskStatusReadyForReview, Type: TaskTypeCoding, RolePair: "coding-pair", ReviewCommit: strPtr("rc")},
				},
			},
			role: "orchestrator",
			want: 0,
		},
		{
			name: "multiple reviewable tasks",
			state: &State{
				Tasks: []Task{
					{ID: "t1", Status: TaskStatusReadyForReview, Type: TaskTypeCoding, RolePair: "coding-pair", ReviewCommit: strPtr("rc")},
					{ID: "t2", Status: TaskStatusReadyForReview, Type: TaskTypeCoding, RolePair: "coding-pair", ReviewCommit: strPtr("rc")},
					{ID: "t3", Status: TaskStatusReady, Type: TaskTypeCoding, RolePair: "coding-pair"},
				},
			},
			role: "code-reviewer",
			want: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CountReviewableTasks(tt.state, tt.role, pr)
			if got != tt.want {
				t.Errorf("CountReviewableTasks() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestGetCoderWorkDiagnostics(t *testing.T) {
	pr := &mockPipelineResolver{
		doer:      "coder",         // runtime form
		reviewer:  "code-reviewer", // runtime form
		initial:   TaskStatusReady,
		rejected:  TaskStatusRejected,
		submitted: TaskStatusReadyForReview,
		reviewing: TaskStatusReviewing,
		executing: TaskStatusImplementing,
		approved:  TaskStatusApproved,
	}

	tests := []struct {
		name         string
		state        *State
		wantContains []string
	}{
		{
			name:         "empty state",
			state:        &State{},
			wantContains: []string{"No claimable tasks"},
		},
		{
			name: "claimable tasks found",
			state: &State{
				Tasks: []Task{
					{ID: "t1", Status: TaskStatusReady, Type: TaskTypeCoding, RolePair: "coding-pair"},
					{ID: "t2", Status: TaskStatusReady, Type: TaskTypeCoding, RolePair: "coding-pair"},
				},
			},
			wantContains: []string{"Found 2 claimable task(s)"},
		},
		{
			name: "blocked by dependencies reported",
			state: &State{
				Tasks: []Task{
					{ID: "t1", Status: TaskStatusReady, Type: TaskTypeCoding, RolePair: "coding-pair", DependsOn: []string{"t2"}},
					{ID: "t2", Status: TaskStatusImplementing, Type: TaskTypeCoding, RolePair: "coding-pair"},
				},
			},
			wantContains: []string{"No claimable tasks", "1 blocked by dependencies"},
		},
		{
			name: "in-progress tasks reported",
			state: &State{
				Tasks: []Task{
					{ID: "t1", Status: TaskStatusImplementing, Type: TaskTypeCoding, RolePair: "coding-pair"},
					{ID: "t2", Status: TaskStatusReviewing, Type: TaskTypeCoding, RolePair: "coding-pair"},
				},
			},
			wantContains: []string{"No claimable tasks", "2 in progress"},
		},
		{
			name: "superseded dependency not counted as blocked",
			state: &State{
				Tasks: []Task{
					{ID: "t1", Status: TaskStatusReady, Type: TaskTypeCoding, RolePair: "coding-pair", DependsOn: []string{"t2"}},
					{ID: "t2", Status: TaskStatusSuperseded, Type: TaskTypeCoding, RolePair: "coding-pair"},
				},
			},
			wantContains: []string{"Found 1 claimable task(s)"},
		},
		{
			name: "both blocked and in-progress reported",
			state: &State{
				Tasks: []Task{
					{ID: "t1", Status: TaskStatusReady, Type: TaskTypeCoding, RolePair: "coding-pair", DependsOn: []string{"t3"}},
					{ID: "t2", Status: TaskStatusImplementing, Type: TaskTypeCoding, RolePair: "coding-pair"},
					{ID: "t3", Status: TaskStatusReadyForReview, Type: TaskTypeCoding, RolePair: "coding-pair"},
				},
			},
			wantContains: []string{"No claimable tasks", "1 blocked by dependencies", "2 in progress"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetCoderWorkDiagnostics(tt.state, pr)
			for _, want := range tt.wantContains {
				if !strings.Contains(got, want) {
					t.Errorf("GetCoderWorkDiagnostics() = %q, want it to contain %q", got, want)
				}
			}
		})
	}
}

func TestGetReviewerWorkDiagnostics(t *testing.T) {
	pr := &mockPipelineResolver{
		doer:      "coder",         // runtime form
		reviewer:  "code-reviewer", // runtime form
		initial:   TaskStatusReady,
		rejected:  TaskStatusRejected,
		submitted: TaskStatusReadyForReview,
		reviewing: TaskStatusReviewing,
		executing: TaskStatusImplementing,
		approved:  TaskStatusApproved,
	}

	now := time.Now().UTC()
	pastTime := now.Add(-10 * time.Minute)
	futureTime := now.Add(10 * time.Minute)

	tests := []struct {
		name         string
		state        *State
		wantContains []string
	}{
		{
			name:         "empty state",
			state:        &State{},
			wantContains: []string{"No reviewable tasks"},
		},
		{
			name: "unassigned reviewable tasks",
			state: &State{
				Tasks: []Task{
					{ID: "t1", Status: TaskStatusReadyForReview, Type: TaskTypeCoding, RolePair: "coding-pair", ReviewCommit: strPtr("rc")},
					{ID: "t2", Status: TaskStatusReadyForReview, Type: TaskTypeCoding, RolePair: "coding-pair", ReviewCommit: strPtr("rc")},
				},
			},
			wantContains: []string{"Found 2 reviewable task(s)"},
		},
		{
			name: "expired lease reported alongside reviewable",
			state: &State{
				Tasks: []Task{
					{ID: "t1", Status: TaskStatusReadyForReview, Type: TaskTypeCoding, RolePair: "coding-pair", ReviewCommit: strPtr("rc")},
					{ID: "t2", Status: TaskStatusReviewing, Type: TaskTypeCoding, RolePair: "coding-pair", ReviewLeaseExpires: &pastTime},
				},
			},
			wantContains: []string{"Found 1 reviewable task(s)", "1 with stale leases"},
		},
		{
			name: "expired lease with no reviewable",
			state: &State{
				Tasks: []Task{
					{ID: "t1", Status: TaskStatusReviewing, Type: TaskTypeCoding, RolePair: "coding-pair", ReviewLeaseExpires: &pastTime},
				},
			},
			wantContains: []string{"No reviewable tasks", "1 with stale leases"},
		},
		{
			name: "actively reviewing reported",
			state: &State{
				Tasks: []Task{
					{ID: "t1", Status: TaskStatusReviewing, Type: TaskTypeCoding, RolePair: "coding-pair", ReviewLeaseExpires: &futureTime},
				},
			},
			wantContains: []string{"No reviewable tasks", "1 actively being reviewed"},
		},
		{
			name: "reviewing with nil lease counts as active",
			state: &State{
				Tasks: []Task{
					{ID: "t1", Status: TaskStatusReviewing, Type: TaskTypeCoding, RolePair: "coding-pair"},
				},
			},
			wantContains: []string{"No reviewable tasks", "1 actively being reviewed"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetReviewerWorkDiagnostics(tt.state, pr)
			for _, want := range tt.wantContains {
				if !strings.Contains(got, want) {
					t.Errorf("GetReviewerWorkDiagnostics() = %q, want it to contain %q", got, want)
				}
			}
		})
	}
}

func TestDiagnosticsQuorumStates(t *testing.T) {
	// Resolver with quorum states configured.
	pr := &mockPipelineResolver{
		doer:              "coder",
		reviewer:          "code-reviewer",
		initial:           TaskStatusReady,
		rejected:          TaskStatusRejected,
		submitted:         TaskStatusReadyForReview,
		reviewing:         TaskStatusReviewing,
		executing:         TaskStatusImplementing,
		approved:          TaskStatusApproved,
		partiallyApproved: "CODE_PARTIALLY_APPROVED",
		reviewing2:        "REVIEWING_CODE_2",
	}

	now := time.Now().UTC()
	pastTime := now.Add(-10 * time.Minute)
	futureTime := now.Add(10 * time.Minute)

	t.Run("CountReviewableTasks counts partially_approved", func(t *testing.T) {
		state := &State{
			Tasks: []Task{
				{ID: "t1", Status: "CODE_PARTIALLY_APPROVED", Type: TaskTypeCoding, RolePair: "coding-pair", ReviewCommit: strPtr("rc")},
				{ID: "t2", Status: TaskStatusReadyForReview, Type: TaskTypeCoding, RolePair: "coding-pair", ReviewCommit: strPtr("rc")},
				{ID: "t3", Status: TaskStatusReviewing, Type: TaskTypeCoding, RolePair: "coding-pair"},
			},
		}
		got := CountReviewableTasks(state, "code-reviewer", pr)
		if got != 2 { // submitted + partially_approved
			t.Errorf("CountReviewableTasks() = %d, want 2 (submitted + partially_approved)", got)
		}
	})

	t.Run("CountReviewableTasks partially_approved only", func(t *testing.T) {
		state := &State{
			Tasks: []Task{
				{ID: "t1", Status: "CODE_PARTIALLY_APPROVED", Type: TaskTypeCoding, RolePair: "coding-pair", ReviewCommit: strPtr("rc")},
			},
		}
		got := CountReviewableTasks(state, "code-reviewer", pr)
		if got != 1 {
			t.Errorf("CountReviewableTasks() = %d, want 1", got)
		}
	})

	t.Run("diagnostics reports partially_approved as awaiting second review", func(t *testing.T) {
		state := &State{
			Tasks: []Task{
				{ID: "t1", Status: "CODE_PARTIALLY_APPROVED", Type: TaskTypeCoding, RolePair: "coding-pair", ReviewCommit: strPtr("rc")},
			},
		}
		got := GetReviewerWorkDiagnostics(state, pr)
		if !strings.Contains(got, "awaiting second review") {
			t.Errorf("GetReviewerWorkDiagnostics() = %q, want it to contain 'awaiting second review'", got)
		}
	})

	t.Run("diagnostics reports reviewing_2 as in second review", func(t *testing.T) {
		state := &State{
			Tasks: []Task{
				{ID: "t1", Status: "REVIEWING_CODE_2", Type: TaskTypeCoding, RolePair: "coding-pair", ReviewLeaseExpires: &futureTime},
			},
		}
		got := GetReviewerWorkDiagnostics(state, pr)
		if !strings.Contains(got, "in second review") {
			t.Errorf("GetReviewerWorkDiagnostics() = %q, want it to contain 'in second review'", got)
		}
	})

	t.Run("diagnostics reports reviewing_2 with expired lease as stale", func(t *testing.T) {
		state := &State{
			Tasks: []Task{
				{ID: "t1", Status: "REVIEWING_CODE_2", Type: TaskTypeCoding, RolePair: "coding-pair", ReviewLeaseExpires: &pastTime},
			},
		}
		got := GetReviewerWorkDiagnostics(state, pr)
		if !strings.Contains(got, "stale leases") {
			t.Errorf("GetReviewerWorkDiagnostics() = %q, want it to contain 'stale leases'", got)
		}
	})

	t.Run("diagnostics mixed quorum and regular states", func(t *testing.T) {
		state := &State{
			Tasks: []Task{
				{ID: "t1", Status: TaskStatusReadyForReview, Type: TaskTypeCoding, RolePair: "coding-pair", ReviewCommit: strPtr("rc")},
				{ID: "t2", Status: "CODE_PARTIALLY_APPROVED", Type: TaskTypeCoding, RolePair: "coding-pair", ReviewCommit: strPtr("rc")},
				{ID: "t3", Status: "REVIEWING_CODE_2", Type: TaskTypeCoding, RolePair: "coding-pair", ReviewLeaseExpires: &futureTime},
				{ID: "t4", Status: TaskStatusReviewing, Type: TaskTypeCoding, RolePair: "coding-pair", ReviewLeaseExpires: &futureTime},
			},
		}
		got := GetReviewerWorkDiagnostics(state, pr)
		// t1 = unassigned (submitted), t2 = awaiting second review
		if !strings.Contains(got, "reviewable") {
			t.Errorf("GetReviewerWorkDiagnostics() = %q, want it to contain 'reviewable'", got)
		}
		if !strings.Contains(got, "awaiting second review") {
			t.Errorf("GetReviewerWorkDiagnostics() = %q, want it to contain 'awaiting second review'", got)
		}
		if !strings.Contains(got, "in second review") {
			t.Errorf("GetReviewerWorkDiagnostics() = %q, want it to contain 'in second review'", got)
		}
	})

	t.Run("corrupted tasks without review_commit excluded from diagnostics", func(t *testing.T) {
		state := &State{
			Tasks: []Task{
				{ID: "t1", Status: TaskStatusReadyForReview, Type: TaskTypeCoding, RolePair: "coding-pair"},                                // no ReviewCommit
				{ID: "t2", Status: "CODE_PARTIALLY_APPROVED", Type: TaskTypeCoding, RolePair: "coding-pair"},                               // no ReviewCommit
				{ID: "t3", Status: TaskStatusReadyForReview, Type: TaskTypeCoding, RolePair: "coding-pair", ReviewCommit: strPtr("valid")}, // valid
			},
		}
		got := GetReviewerWorkDiagnostics(state, pr)
		// Only t3 should be counted as reviewable
		if !strings.Contains(got, "1 reviewable") {
			t.Errorf("GetReviewerWorkDiagnostics() = %q, want '1 reviewable' (corrupted tasks excluded)", got)
		}
		if strings.Contains(got, "awaiting second review") {
			t.Errorf("GetReviewerWorkDiagnostics() = %q, corrupted partially_approved should not appear", got)
		}
	})

	t.Run("quorum states without quorum config fall back gracefully", func(t *testing.T) {
		// Resolver without quorum states (quorum 1 scenario).
		prNoQuorum := &mockPipelineResolver{
			doer:      "coder",
			reviewer:  "code-reviewer",
			initial:   TaskStatusReady,
			rejected:  TaskStatusRejected,
			submitted: TaskStatusReadyForReview,
			reviewing: TaskStatusReviewing,
			executing: TaskStatusImplementing,
			approved:  TaskStatusApproved,
			// partiallyApproved and reviewing2 left empty (zero value)
		}
		state := &State{
			Tasks: []Task{
				{ID: "t1", Status: TaskStatusReadyForReview, Type: TaskTypeCoding, RolePair: "coding-pair", ReviewCommit: strPtr("rc")},
			},
		}
		got := GetReviewerWorkDiagnostics(state, prNoQuorum)
		if !strings.Contains(got, "Found 1 reviewable task(s)") {
			t.Errorf("GetReviewerWorkDiagnostics() = %q, want it to contain 'Found 1 reviewable task(s)'", got)
		}
	})
}
