package models

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/roles"
)

func TestTaskReadinessConfiguredRoles(t *testing.T) {
	resolver := &taskReadinessTestResolver{
		roleNames: []string{
			"plan-reviewer",
			"coder",
			"idle-reviewer",
			"orchestrator",
			"planner",
			"planner",
			"code-reviewer",
			"idle-doer",
		},
		roleTypes: map[string]string{
			"coder":         "doer",
			"code-reviewer": "reviewer",
			"planner":       "doer",
			"plan-reviewer": "reviewer",
			"idle-doer":     "doer",
			"idle-reviewer": "reviewer",
			"orchestrator":  "orchestrator",
		},
		pairs: map[string]taskReadinessTestPair{
			"coding-pair": {
				doer: RoleCoder, reviewer: RoleCodeReviewer,
				initial: TaskStatusReady, rejected: TaskStatusRejected,
				submitted: TaskStatusReadyForReview, reviewing: TaskStatusReviewing,
				executing: TaskStatusImplementing, approved: TaskStatusApproved,
			},
			"planning-pair": {
				doer: "planner", reviewer: "plan-reviewer",
				initial: TaskStatusDraftCodingPlan, rejected: TaskStatusCodingPlanRejected,
				submitted: TaskStatusCodingPlanToReview, reviewing: TaskStatusReviewingCodingPlan,
				executing: TaskStatusCodePlanning, approved: TaskStatusCodingPlanApproved,
			},
			"alternate-planning-pair": {
				doer: "planner", reviewer: "plan-reviewer",
				initial: "ALT_PLAN_READY", rejected: "ALT_PLAN_REJECTED",
				submitted: "ALT_PLAN_TO_REVIEW", reviewing: "ALT_PLAN_REVIEWING",
				executing: "ALT_PLAN_EXECUTING", approved: "ALT_PLAN_APPROVED",
			},
			"idle-pair": {
				doer: "idle-doer", reviewer: "idle-reviewer",
				initial: "IDLE_READY", rejected: "IDLE_REJECTED",
				submitted: "IDLE_TO_REVIEW", reviewing: "IDLE_REVIEWING",
				executing: "IDLE_EXECUTING", approved: "IDLE_APPROVED",
			},
		},
	}

	state := &State{Tasks: []Task{
		{ID: "merged-dependency", Status: TaskStatusMerged, RolePair: "coding-pair"},
		{ID: "coding-ready", Status: TaskStatusReady, RolePair: "coding-pair"},
		{ID: "coding-executing", Status: TaskStatusImplementing, RolePair: "coding-pair"},
		{ID: "coding-submitted", Status: TaskStatusReadyForReview, RolePair: "coding-pair", ReviewCommit: strPtr("coding-sha")},
		{ID: "planning-ready", Status: TaskStatusDraftCodingPlan, RolePair: "planning-pair", DependsOn: []string{"merged-dependency"}},
		{ID: "planning-executing", Status: TaskStatusCodePlanning, RolePair: "planning-pair"},
		{ID: "planning-blocked", Status: TaskStatusDraftCodingPlan, RolePair: "planning-pair", DependsOn: []string{"coding-executing"}},
		{ID: "planning-submitted", Status: TaskStatusCodingPlanToReview, RolePair: "planning-pair", ReviewCommit: strPtr("planning-sha")},
		{ID: "planning-reviewing", Status: TaskStatusReviewingCodingPlan, RolePair: "planning-pair", ReviewCommit: strPtr("planning-review-sha")},
		{ID: "alternate-planning-ready", Status: "ALT_PLAN_READY", RolePair: "alternate-planning-pair"},
		{ID: "alternate-planning-submitted", Status: "ALT_PLAN_TO_REVIEW", RolePair: "alternate-planning-pair", ReviewCommit: strPtr("alternate-planning-sha")},
	}}

	got := GetTaskReadiness(state, resolver)
	wantClaimable := []RoleTaskReadiness{
		{Role: "coder", Count: 1},
		{Role: "idle-doer", Count: 0},
		{Role: "planner", Count: 2},
	}
	wantReviewable := []RoleTaskReadiness{
		{Role: "code-reviewer", Count: 1},
		{Role: "idle-reviewer", Count: 0},
		{Role: "plan-reviewer", Count: 2},
	}
	assertRoleTaskReadiness(t, "claimable", got.ClaimableByRole, wantClaimable)
	assertRoleTaskReadiness(t, "reviewable", got.ReviewableByRole, wantReviewable)

	if got.Claimable != sumRoleTaskReadiness(got.ClaimableByRole) {
		t.Errorf("Claimable = %d, want sum of role counts %d", got.Claimable, sumRoleTaskReadiness(got.ClaimableByRole))
	}
	if got.Reviewable != sumRoleTaskReadiness(got.ReviewableByRole) {
		t.Errorf("Reviewable = %d, want sum of role counts %d", got.Reviewable, sumRoleTaskReadiness(got.ReviewableByRole))
	}

	for _, roleReadiness := range append(got.ClaimableByRole, got.ReviewableByRole...) {
		want := 0
		for i := range state.Tasks {
			if state.Tasks[i].IsClaimable(roleReadiness.Role, state.Tasks, resolver) {
				want++
			}
		}
		if roleReadiness.Count != want {
			t.Errorf("readiness for role %q = %d, want IsClaimable count %d", roleReadiness.Role, roleReadiness.Count, want)
		}
	}
}

func TestTaskReadinessExcludesRejectedWorkReservedByOwnership(t *testing.T) {
	pr := &mockPipelineResolver{
		doer:      "coder",
		reviewer:  "code-reviewer",
		initial:   TaskStatusReady,
		rejected:  TaskStatusRejected,
		submitted: TaskStatusReadyForReview,
		reviewing: TaskStatusReviewing,
		executing: TaskStatusImplementing,
		approved:  TaskStatusApproved,
	}
	now := time.Now().UTC()
	owner := "coder-2"
	futureLease := now.Add(time.Hour)
	expiredLease := now.Add(-time.Hour)
	state := &State{Tasks: []Task{
		{ID: "unowned", Status: TaskStatusRejected, RolePair: "coding-pair"},
		{ID: "expired", Status: TaskStatusRejected, RolePair: "coding-pair", AssignedTo: &owner, LeaseExpires: &expiredLease},
		{ID: "protected", Status: TaskStatusRejected, RolePair: "coding-pair", AssignedTo: &owner, LeaseExpires: &futureLease},
		{ID: "malformed", Status: TaskStatusRejected, RolePair: "coding-pair", AssignedTo: &owner},
	}}

	got := GetTaskReadiness(state, pr)
	if got.Claimable != 2 {
		t.Fatalf("Claimable = %d, want 2 unowned or expired rejected tasks", got.Claimable)
	}
	if want := []RoleTaskReadiness{{Role: RoleCoder, Count: 2}}; !slices.Equal(got.ClaimableByRole, want) {
		t.Fatalf("ClaimableByRole = %#v, want %#v", got.ClaimableByRole, want)
	}
	if legacy := CountClaimableTasks(state, RoleCoder, pr); legacy != 4 {
		t.Fatalf("legacy CountClaimableTasks = %d, want historical status-level count 4", legacy)
	}
}

func assertRoleTaskReadiness(t *testing.T, name string, got, want []RoleTaskReadiness) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s role count length = %d, want %d: %#v", name, len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%s[%d] = %#v, want %#v", name, i, got[i], want[i])
		}
	}
}

func sumRoleTaskReadiness(readiness []RoleTaskReadiness) int {
	total := 0
	for _, role := range readiness {
		total += role.Count
	}
	return total
}

type taskReadinessTestPair struct {
	doer      string
	reviewer  string
	initial   TaskStatus
	rejected  TaskStatus
	submitted TaskStatus
	reviewing TaskStatus
	executing TaskStatus
	approved  TaskStatus
}

type taskReadinessTestResolver struct {
	roleNames []string
	roleTypes map[string]string
	pairs     map[string]taskReadinessTestPair
}

func (r *taskReadinessTestResolver) RoleType(role string) (roles.RoleType, error) {
	roleType, ok := r.roleTypes[role]
	if !ok {
		return "", fmt.Errorf("unknown role %q", role)
	}
	return roles.RoleType(roleType), nil
}

func (r *taskReadinessTestResolver) AllRoleNames() []string {
	return r.roleNames
}

func (r *taskReadinessTestResolver) pair(rolePair string) (taskReadinessTestPair, error) {
	pair, ok := r.pairs[rolePair]
	if !ok {
		return taskReadinessTestPair{}, fmt.Errorf("unknown role-pair %q", rolePair)
	}
	return pair, nil
}

func (r *taskReadinessTestResolver) DoerRole(rolePair string) (string, error) {
	pair, err := r.pair(rolePair)
	return pair.doer, err
}

func (r *taskReadinessTestResolver) ReviewerRole(rolePair string) (string, error) {
	pair, err := r.pair(rolePair)
	return pair.reviewer, err
}

func (r *taskReadinessTestResolver) InitialStatus(rolePair string) (TaskStatus, error) {
	pair, err := r.pair(rolePair)
	return pair.initial, err
}

func (r *taskReadinessTestResolver) RejectedStatus(rolePair string) (TaskStatus, error) {
	pair, err := r.pair(rolePair)
	return pair.rejected, err
}

func (r *taskReadinessTestResolver) SubmittedStatus(rolePair string) (TaskStatus, error) {
	pair, err := r.pair(rolePair)
	return pair.submitted, err
}

func (r *taskReadinessTestResolver) ReviewingStatus(rolePair string) (TaskStatus, error) {
	pair, err := r.pair(rolePair)
	return pair.reviewing, err
}

func (r *taskReadinessTestResolver) ExecutingStatus(rolePair string) (TaskStatus, error) {
	pair, err := r.pair(rolePair)
	return pair.executing, err
}

func (r *taskReadinessTestResolver) ApprovedStatus(rolePair string) (TaskStatus, error) {
	pair, err := r.pair(rolePair)
	return pair.approved, err
}

func (r *taskReadinessTestResolver) PartiallyApprovedStatus(string) (TaskStatus, error) {
	return "", fmt.Errorf("no partially-approved state declared")
}

func (r *taskReadinessTestResolver) Reviewing2Status(string) (TaskStatus, error) {
	return "", fmt.Errorf("no reviewing-2 state declared")
}

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
			name: "superseded dependency without replacement blocked",
			state: &State{
				Tasks: []Task{
					{ID: "t1", Status: TaskStatusReady, Type: TaskTypeCoding, RolePair: "coding-pair", DependsOn: []string{"t2"}},
					{ID: "t2", Status: TaskStatusSuperseded, Type: TaskTypeCoding, RolePair: "coding-pair"},
				},
			},
			role: RoleCoder,
			want: 0,
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

func TestCountDoerClaimableTasksForAgent_RejectedOwnership(t *testing.T) {
	pr := &mockPipelineResolver{
		doer:      "coder",
		reviewer:  "code-reviewer",
		initial:   TaskStatusReady,
		rejected:  TaskStatusRejected,
		submitted: TaskStatusReadyForReview,
		reviewing: TaskStatusReviewing,
		executing: TaskStatusImplementing,
		approved:  TaskStatusApproved,
	}
	now := time.Now().UTC()

	owner := "coder-2"
	futureLease := now.Add(30 * time.Minute)
	expiredLease := now.Add(-time.Minute)

	tests := []struct {
		name    string
		task    Task
		agentID string
		want    int
	}{
		{
			name: "unowned rejected task allows any coder",
			task: Task{
				ID:       "t1",
				Status:   TaskStatusRejected,
				Type:     TaskTypeCoding,
				RolePair: "coding-pair",
			},
			agentID: "coder-1",
			want:    1,
		},
		{
			name: "active lease blocks different coder",
			task: Task{
				ID:           "t1",
				Status:       TaskStatusRejected,
				Type:         TaskTypeCoding,
				RolePair:     "coding-pair",
				AssignedTo:   &owner,
				LeaseExpires: &futureLease,
			},
			agentID: "coder-1",
			want:    0,
		},
		{
			name: "active lease allows owner",
			task: Task{
				ID:           "t1",
				Status:       TaskStatusRejected,
				Type:         TaskTypeCoding,
				RolePair:     "coding-pair",
				AssignedTo:   &owner,
				LeaseExpires: &futureLease,
			},
			agentID: "coder-2",
			want:    1,
		},
		{
			name: "expired lease allows different coder",
			task: Task{
				ID:           "t1",
				Status:       TaskStatusRejected,
				Type:         TaskTypeCoding,
				RolePair:     "coding-pair",
				AssignedTo:   &owner,
				LeaseExpires: &expiredLease,
			},
			agentID: "coder-1",
			want:    1,
		},
		{
			name: "assigned without lease fails closed",
			task: Task{
				ID:         "t1",
				Status:     TaskStatusRejected,
				Type:       TaskTypeCoding,
				RolePair:   "coding-pair",
				AssignedTo: &owner,
			},
			agentID: "coder-2",
			want:    0,
		},
		{
			name: "ready task remains agent claimable",
			task: Task{
				ID:       "t1",
				Status:   TaskStatusReady,
				Type:     TaskTypeCoding,
				RolePair: "coding-pair",
			},
			agentID: "coder-1",
			want:    1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := &State{Tasks: []Task{tt.task}}
			got := CountDoerClaimableTasksForAgent(state, RoleCoder, tt.agentID, pr)
			if got != tt.want {
				t.Errorf("CountDoerClaimableTasksForAgent() = %d, want %d", got, tt.want)
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
			name: "explicit blocked tasks reported",
			state: &State{
				Tasks: []Task{
					{ID: "t1", Status: TaskStatusBlocked, Type: TaskTypeCoding, RolePair: "coding-pair"},
					{ID: "t2", Status: TaskStatusBlocked, Type: TaskTypeCoding, RolePair: "coding-pair"},
				},
			},
			wantContains: []string{"No claimable tasks", "2 blocked tasks"},
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
			name: "superseded dependency without replacement counted as blocked",
			state: &State{
				Tasks: []Task{
					{ID: "t1", Status: TaskStatusReady, Type: TaskTypeCoding, RolePair: "coding-pair", DependsOn: []string{"t2"}},
					{ID: "t2", Status: TaskStatusSuperseded, Type: TaskTypeCoding, RolePair: "coding-pair"},
				},
			},
			wantContains: []string{"No claimable tasks", "1 blocked by dependencies"},
		},
		{
			name: "superseded dependency with merged replacement still counted as blocked until edge is rewritten",
			state: &State{
				Tasks: []Task{
					{ID: "t1", Status: TaskStatusReady, Type: TaskTypeCoding, RolePair: "coding-pair", DependsOn: []string{"t2"}},
					{ID: "t2", Status: TaskStatusSuperseded, Type: TaskTypeCoding, RolePair: "coding-pair", SupersededBy: []string{"t3"}},
					{ID: "t3", Status: TaskStatusMerged, Type: TaskTypeCoding, RolePair: "coding-pair"},
				},
			},
			wantContains: []string{"No claimable tasks", "1 blocked by dependencies"},
		},
		{
			name: "blocked by dependencies, explicit blocked, and in-progress reported",
			state: &State{
				Tasks: []Task{
					{ID: "t1", Status: TaskStatusReady, Type: TaskTypeCoding, RolePair: "coding-pair", DependsOn: []string{"t3"}},
					{ID: "t2", Status: TaskStatusImplementing, Type: TaskTypeCoding, RolePair: "coding-pair"},
					{ID: "t3", Status: TaskStatusReadyForReview, Type: TaskTypeCoding, RolePair: "coding-pair"},
					{ID: "t4", Status: TaskStatusBlocked, Type: TaskTypeCoding, RolePair: "coding-pair"},
					{ID: "t5", Status: TaskStatusBlocked, Type: TaskTypeCoding, RolePair: "coding-pair"},
				},
			},
			wantContains: []string{"No claimable tasks", "2 blocked tasks", "1 blocked by dependencies", "2 in progress"},
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

func TestGetCoderWorkDiagnosticsForAgent_ProtectedRejectedTask(t *testing.T) {
	pr := &mockPipelineResolver{
		doer:      "coder",
		reviewer:  "code-reviewer",
		initial:   TaskStatusReady,
		rejected:  TaskStatusRejected,
		submitted: TaskStatusReadyForReview,
		reviewing: TaskStatusReviewing,
		executing: TaskStatusImplementing,
		approved:  TaskStatusApproved,
	}

	now := time.Now().UTC()
	owner := "coder-2"
	futureLease := now.Add(30 * time.Minute)
	state := &State{
		Tasks: []Task{
			{
				ID:           "t1",
				Status:       TaskStatusRejected,
				Type:         TaskTypeCoding,
				RolePair:     "coding-pair",
				AssignedTo:   &owner,
				LeaseExpires: &futureLease,
			},
		},
	}

	wrongCoder := GetCoderWorkDiagnosticsForAgent(state, "coder-1", pr)
	if strings.Contains(wrongCoder, "Found 1 claimable task(s)") {
		t.Errorf("GetCoderWorkDiagnosticsForAgent() for wrong coder = %q, want no claimable task", wrongCoder)
	}

	owningCoder := GetCoderWorkDiagnosticsForAgent(state, "coder-2", pr)
	if !strings.Contains(owningCoder, "Found 1 claimable task(s)") {
		t.Errorf("GetCoderWorkDiagnosticsForAgent() for owner = %q, want claimable task", owningCoder)
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

	t.Run("CountReviewableTasksForAgent excludes prior approver", func(t *testing.T) {
		// reviewer-1 already approved this task — for them it must not be
		// reviewable any more, but reviewer-2 should still see it.
		approver := "code-reviewer-1"
		state := &State{
			Tasks: []Task{
				{
					ID:           "t1",
					Status:       "CODE_PARTIALLY_APPROVED",
					Type:         TaskTypeCoding,
					RolePair:     "coding-pair",
					ReviewCommit: strPtr("rc"),
					Approvals: []Approval{
						{Agent: approver, Provider: "anthropic", Timestamp: now},
					},
				},
			},
		}
		if got := CountReviewableTasksForAgent(state, "code-reviewer", approver, pr); got != 0 {
			t.Errorf("CountReviewableTasksForAgent(prior approver) = %d, want 0", got)
		}
		if got := CountReviewableTasksForAgent(state, "code-reviewer", "code-reviewer-2", pr); got != 1 {
			t.Errorf("CountReviewableTasksForAgent(other reviewer) = %d, want 1", got)
		}
		// Sanity: role-level count is still 1 (it's claimable for the role).
		if got := CountReviewableTasks(state, "code-reviewer", pr); got != 1 {
			t.Errorf("CountReviewableTasks(role) = %d, want 1", got)
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
