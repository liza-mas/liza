package statevalidate

import (
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestValidateAgentInvariants_ActiveReviewOwnership(t *testing.T) {
	resolver := loadTestResolver(t)
	now := time.Now().UTC()

	tests := []struct {
		name        string
		mutateState func(*models.State, string)
		wantErr     string
	}{
		{
			name: "valid reviewing ownership",
		},
		{
			name: "missing reviewer agent",
			mutateState: func(state *models.State, reviewerID string) {
				delete(state.Agents, reviewerID)
			},
			wantErr: "reviewing_by code-reviewer-1 has no matching agent",
		},
		{
			name: "wrong reviewer role",
			mutateState: func(state *models.State, reviewerID string) {
				agent := state.Agents[reviewerID]
				agent.Role = models.RoleCodePlanReviewer
				state.Agents[reviewerID] = agent
			},
			wantErr: `has role "code-plan-reviewer", want "code-reviewer"`,
		},
		{
			name: "idle reviewer agent",
			mutateState: func(state *models.State, reviewerID string) {
				agent := state.Agents[reviewerID]
				agent.Status = models.AgentStatusIdle
				state.Agents[reviewerID] = agent
			},
			wantErr: "has agent status IDLE, want REVIEWING",
		},
		{
			name: "mismatched current task",
			mutateState: func(state *models.State, reviewerID string) {
				agent := state.Agents[reviewerID]
				agent.CurrentTask = testhelpers.StringPtr("other-task")
				state.Agents[reviewerID] = agent
			},
			wantErr: "has mismatched current_task",
		},
		{
			name: "missing agent lease",
			mutateState: func(state *models.State, reviewerID string) {
				agent := state.Agents[reviewerID]
				agent.LeaseExpires = nil
				state.Agents[reviewerID] = agent
			},
			wantErr: "has agent without lease_expires",
		},
		{
			name: "missing agent pid",
			mutateState: func(state *models.State, reviewerID string) {
				agent := state.Agents[reviewerID]
				agent.PID = 0
				state.Agents[reviewerID] = agent
			},
			wantErr: "agent code-reviewer-1 has active lease but no pid",
		},
		{
			name: "reviewing-2 ownership",
			mutateState: func(state *models.State, reviewerID string) {
				state.Tasks[0].Status = models.TaskStatusReviewingCode2
				agent := state.Agents[reviewerID]
				agent.Status = models.AgentStatusIdle
				state.Agents[reviewerID] = agent
			},
			wantErr: "REVIEWING_CODE_2 task task-1 reviewing_by code-reviewer-1 has agent status IDLE, want REVIEWING",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state, reviewerID := activeReviewOwnershipState(now)
			if tt.mutateState != nil {
				tt.mutateState(state, reviewerID)
			}

			err := validateAgentInvariants(state, "", true, io.Discard, resolver)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("validateAgentInvariants() error = %v, want nil", err)
				}
				return
			}
			assertErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestValidateAgentInvariants_NonReviewingOrphanReviewingByNotActiveOwnership(t *testing.T) {
	resolver := loadTestResolver(t)
	now := time.Now().UTC()
	state := stateWithTasks(testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReadyForReview, now))
	state.Tasks[0].ReviewingBy = testhelpers.StringPtr("missing-reviewer")
	state.Tasks[0].ReviewLeaseExpires = testhelpers.TimePtr(now.Add(-time.Hour))

	err := validateAgentInvariants(state, "", true, io.Discard, resolver)
	if err != nil {
		t.Fatalf("validateAgentInvariants() error = %v, want nil for non-reviewing orphan reviewing_by", err)
	}
}

func TestValidateAgentInvariants_ActiveDoerOwnership(t *testing.T) {
	resolver := loadTestResolver(t)
	now := time.Now().UTC()

	tests := []struct {
		name        string
		mutateState func(*models.State, string)
		wantErr     string
	}{
		{
			name: "valid working ownership",
		},
		{
			name: "valid resumable owned task",
			mutateState: func(state *models.State, doerID string) {
				agent := state.Agents[doerID]
				agent.Status = models.AgentStatusIdle
				agent.CurrentTask = nil
				state.Agents[doerID] = agent
			},
		},
		{
			name: "valid handoff ownership",
			mutateState: func(state *models.State, doerID string) {
				state.Tasks[0].HandoffPending = true
				agent := state.Agents[doerID]
				agent.Status = models.AgentStatusHandoff
				agent.CurrentTask = testhelpers.StringPtr(state.Tasks[0].ID)
				state.Agents[doerID] = agent
			},
		},
		{
			name: "handoff ownership missing provider",
			mutateState: func(state *models.State, doerID string) {
				state.Tasks[0].HandoffPending = true
				agent := state.Agents[doerID]
				agent.Status = models.AgentStatusHandoff
				agent.CurrentTask = testhelpers.StringPtr(state.Tasks[0].ID)
				agent.Provider = ""
				state.Agents[doerID] = agent
			},
			wantErr: "active lease but no provider",
		},
		{
			name: "missing doer agent",
			mutateState: func(state *models.State, doerID string) {
				delete(state.Agents, doerID)
			},
			wantErr: "assigned_to coder-1 has no matching agent",
		},
		{
			name: "wrong doer role",
			mutateState: func(state *models.State, doerID string) {
				agent := state.Agents[doerID]
				agent.Role = models.RoleCodePlanner
				state.Agents[doerID] = agent
			},
			wantErr: `has role "code-planner", want "coder"`,
		},
		{
			name: "invalid doer status",
			mutateState: func(state *models.State, doerID string) {
				agent := state.Agents[doerID]
				agent.Status = models.AgentStatusWaiting
				state.Agents[doerID] = agent
			},
			wantErr: "has agent status WAITING, want WORKING or resumable IDLE",
		},
		{
			name: "mismatched current task",
			mutateState: func(state *models.State, doerID string) {
				agent := state.Agents[doerID]
				agent.CurrentTask = testhelpers.StringPtr("other-task")
				state.Agents[doerID] = agent
			},
			wantErr: "has mismatched current_task",
		},
		{
			name: "working agent with empty current task",
			mutateState: func(state *models.State, doerID string) {
				agent := state.Agents[doerID]
				agent.CurrentTask = testhelpers.StringPtr("")
				state.Agents[doerID] = agent
			},
			wantErr: "WORKING but no current_task",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state, doerID := activeDoerOwnershipState(now)
			if tt.mutateState != nil {
				tt.mutateState(state, doerID)
			}

			err := validateAgentInvariants(state, "", true, io.Discard, resolver)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("validateAgentInvariants() error = %v, want nil", err)
				}
				return
			}
			assertErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestValidateAgentInvariants_ReverseActiveOwnership(t *testing.T) {
	resolver := loadTestResolver(t)
	now := time.Now().UTC()

	t.Run("planning orchestrator current task is not task ownership", func(t *testing.T) {
		fullResolver := loadFullPipelineResolver(t)
		state := stateWithTasks()
		state.Agents["orchestrator-1"] = models.Agent{
			Role:         "orchestrator",
			Status:       models.AgentStatusPlanning,
			CurrentTask:  testhelpers.StringPtr("planning"),
			LeaseExpires: testhelpers.TimePtr(now.Add(30 * time.Minute)),
			Heartbeat:    now,
			Terminal:     "test",
			Provider:     "test",
			PID:          os.Getpid(),
		}

		err := validateAgentInvariants(state, "", true, io.Discard, fullResolver)
		if err != nil {
			t.Fatalf("validateAgentInvariants() error = %v, want nil", err)
		}
	})

	t.Run("working agent points at task that does not point back", func(t *testing.T) {
		state, _ := activeDoerOwnershipState(now)
		state.Tasks[0].AssignedTo = testhelpers.StringPtr("coder-2")
		state.Agents["coder-2"] = models.Agent{
			Role:         models.RoleCoder,
			Status:       models.AgentStatusIdle,
			LeaseExpires: testhelpers.TimePtr(now.Add(30 * time.Minute)),
			Heartbeat:    now,
			Terminal:     "test",
			Provider:     "test",
			PID:          os.Getpid(),
		}

		err := validateAgentInvariants(state, "", true, io.Discard, resolver)
		assertErrorContains(t, err, "agent coder-1 says WORKING task-1, but task assigned_to is coder-2")

	})

	t.Run("reviewing agent points at non-reviewing task", func(t *testing.T) {
		state, _ := activeReviewOwnershipState(now)
		state.Tasks[0].Status = models.TaskStatusReadyForReview

		err := validateAgentInvariants(state, "", true, io.Discard, resolver)
		assertErrorContains(t, err, "agent code-reviewer-1 says REVIEWING task-1, but task status CODE_TO_REVIEW is not active review")

	})

	t.Run("waiting doer is valid while awaiting verdict", func(t *testing.T) {
		state, doerID := activeDoerOwnershipState(now)
		state.Tasks[0].Status = models.TaskStatusReadyForReview
		agent := state.Agents[doerID]
		agent.Status = models.AgentStatusWaiting
		agent.CurrentTask = testhelpers.StringPtr(state.Tasks[0].ID)
		agent.LeaseExpires = nil
		state.Agents[doerID] = agent

		err := validateAgentInvariants(state, "", true, io.Discard, resolver)
		if err != nil {
			t.Fatalf("validateAgentInvariants() error = %v, want nil", err)
		}
	})

	t.Run("waiting doer pointing at another active owner is stale", func(t *testing.T) {
		state, doerID := activeDoerOwnershipState(now)
		state.Tasks[0].AssignedTo = testhelpers.StringPtr("coder-2")
		agent := state.Agents[doerID]
		agent.Status = models.AgentStatusWaiting
		agent.CurrentTask = testhelpers.StringPtr(state.Tasks[0].ID)
		agent.LeaseExpires = nil
		state.Agents[doerID] = agent
		state.Agents["coder-2"] = models.Agent{
			Role:         models.RoleCoder,
			Status:       models.AgentStatusWorking,
			CurrentTask:  testhelpers.StringPtr(state.Tasks[0].ID),
			LeaseExpires: testhelpers.TimePtr(now.Add(30 * time.Minute)),
			Heartbeat:    now,
			Terminal:     "test",
			Provider:     "test",
			PID:          os.Getpid(),
		}

		err := validateAgentInvariants(state, "", true, io.Discard, resolver)
		assertErrorContains(t, err, "agent coder-1 says WAITING task-1 as doer, but task assigned_to is coder-2")
	})

	t.Run("waiting reviewer is valid while awaiting resubmission", func(t *testing.T) {
		state, _ := activeDoerOwnershipState(now)
		reviewerID := "code-reviewer-1"
		state.Tasks[0].ReviewingBy = testhelpers.StringPtr(reviewerID)
		state.Tasks[0].ReviewLeaseExpires = testhelpers.TimePtr(now.Add(30 * time.Minute))
		state.Agents[reviewerID] = models.Agent{
			Role:        models.RoleCodeReviewer,
			Status:      models.AgentStatusWaiting,
			CurrentTask: testhelpers.StringPtr(state.Tasks[0].ID),
			Heartbeat:   now,
			Terminal:    "test",
			Provider:    "test",
			PID:         os.Getpid(),
		}

		err := validateAgentInvariants(state, "", true, io.Discard, resolver)
		if err != nil {
			t.Fatalf("validateAgentInvariants() error = %v, want nil", err)
		}
	})

	t.Run("waiting reviewer without review lease is stale", func(t *testing.T) {
		state, _ := activeDoerOwnershipState(now)
		reviewerID := "code-reviewer-1"
		state.Tasks[0].ReviewingBy = testhelpers.StringPtr(reviewerID)
		state.Agents[reviewerID] = models.Agent{
			Role:        models.RoleCodeReviewer,
			Status:      models.AgentStatusWaiting,
			CurrentTask: testhelpers.StringPtr(state.Tasks[0].ID),
			Heartbeat:   now,
			Terminal:    "test",
			Provider:    "test",
			PID:         os.Getpid(),
		}

		err := validateAgentInvariants(state, "", true, io.Discard, resolver)
		assertErrorContains(t, err, "agent code-reviewer-1 says WAITING task-1 as reviewer, but task has no review_lease_expires")
	})

	t.Run("waiting reviewer with expired review lease is stale", func(t *testing.T) {
		state, _ := activeDoerOwnershipState(now)
		reviewerID := "code-reviewer-1"
		state.Tasks[0].ReviewingBy = testhelpers.StringPtr(reviewerID)
		state.Tasks[0].ReviewLeaseExpires = testhelpers.TimePtr(now.Add(-time.Minute))
		state.Agents[reviewerID] = models.Agent{
			Role:        models.RoleCodeReviewer,
			Status:      models.AgentStatusWaiting,
			CurrentTask: testhelpers.StringPtr(state.Tasks[0].ID),
			Heartbeat:   now,
			Terminal:    "test",
			Provider:    "test",
			PID:         os.Getpid(),
		}

		err := validateAgentInvariants(state, "", true, io.Discard, resolver)
		assertErrorContains(t, err, "agent code-reviewer-1 says WAITING task-1 as reviewer, but review_lease_expires is not in the future")
	})

	t.Run("waiting reviewer without reviewing_by is stale", func(t *testing.T) {
		state, _ := activeDoerOwnershipState(now)
		reviewerID := "code-reviewer-1"
		state.Agents[reviewerID] = models.Agent{
			Role:        models.RoleCodeReviewer,
			Status:      models.AgentStatusWaiting,
			CurrentTask: testhelpers.StringPtr(state.Tasks[0].ID),
			Heartbeat:   now,
			Terminal:    "test",
			Provider:    "test",
			PID:         os.Getpid(),
		}

		err := validateAgentInvariants(state, "", true, io.Discard, resolver)
		assertErrorContains(t, err, "agent code-reviewer-1 says WAITING task-1 as reviewer, but task reviewing_by is <none>")
	})
}

func activeReviewOwnershipState(now time.Time) (*models.State, string) {
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReviewing, now)
	reviewerID := *task.ReviewingBy
	leaseExpires := now.Add(30 * time.Minute)
	state := stateWithTasks(task)
	state.Agents[reviewerID] = models.Agent{
		Role:         models.RoleCodeReviewer,
		Status:       models.AgentStatusReviewing,
		CurrentTask:  testhelpers.StringPtr(task.ID),
		LeaseExpires: &leaseExpires,
		Heartbeat:    now,
		Terminal:     "test",
		Provider:     "test",
		PID:          os.Getpid(),
	}
	return state, reviewerID
}

func loadFullPipelineResolver(t *testing.T) *pipeline.Resolver {
	t.Helper()
	repoRoot := testhelpers.FindRepoRoot(t)
	yamlPath := filepath.Join(repoRoot, "internal", "pipeline", "testdata", "valid-phase2-full.yaml")
	cfg, err := pipeline.Load(yamlPath)
	if err != nil {
		t.Fatalf("Failed to load full pipeline config: %v", err)
	}
	return pipeline.NewResolver(cfg)
}

func activeDoerOwnershipState(now time.Time) (*models.State, string) {
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now)
	doerID := *task.AssignedTo
	leaseExpires := now.Add(30 * time.Minute)
	state := stateWithTasks(task)
	state.Agents[doerID] = models.Agent{
		Role:         models.RoleCoder,
		Status:       models.AgentStatusWorking,
		CurrentTask:  testhelpers.StringPtr(task.ID),
		LeaseExpires: &leaseExpires,
		Heartbeat:    now,
		Terminal:     "test",
		Provider:     "test",
		PID:          os.Getpid(),
	}
	return state, doerID
}

// A doer keeps WAITING on its task until its await-verdict call observes the
// verdict and releases current_task, so the verdict and the doer row land in
// separate writes. The bounded grace accepts that handoff and nothing else.
func TestValidateAgentInvariants_WaitingDoerVerdictHandoffGrace(t *testing.T) {
	resolver := loadTestResolver(t)
	now := time.Now().UTC()

	waitingDoerAfterVerdict := func(status models.TaskStatus, event string, verdictAt time.Time) *models.State {
		state, doerID := activeDoerOwnershipState(now)
		state.Tasks[0].Status = status
		state.Tasks[0].History = append(state.Tasks[0].History, models.TaskHistoryEntry{
			Time:  verdictAt,
			Event: event,
			Agent: testhelpers.StringPtr("code-reviewer-1"),
		})
		agent := state.Agents[doerID]
		agent.Status = models.AgentStatusWaiting
		agent.CurrentTask = testhelpers.StringPtr(state.Tasks[0].ID)
		agent.LeaseExpires = nil
		state.Agents[doerID] = agent
		return state
	}

	t.Run("rejected verdict within grace is valid", func(t *testing.T) {
		state := waitingDoerAfterVerdict(models.TaskStatusRejected, models.TaskEventRejected, now.Add(-30*time.Second))

		if err := validateAgentInvariants(state, "", true, io.Discard, resolver); err != nil {
			t.Fatalf("validateAgentInvariants() error = %v, want nil during verdict handoff", err)
		}
	})

	t.Run("approved verdict within grace is valid", func(t *testing.T) {
		state := waitingDoerAfterVerdict(models.TaskStatusApproved, models.TaskEventApproved, now.Add(-30*time.Second))

		if err := validateAgentInvariants(state, "", true, io.Discard, resolver); err != nil {
			t.Fatalf("validateAgentInvariants() error = %v, want nil during verdict handoff", err)
		}
	})

	t.Run("verdict older than grace is invalid", func(t *testing.T) {
		state := waitingDoerAfterVerdict(models.TaskStatusRejected, models.TaskEventRejected, now.Add(-models.VerdictHandoffGrace-time.Second))

		err := validateAgentInvariants(state, "", true, io.Discard, resolver)
		assertErrorContains(t, err, "agent coder-1 says WAITING task-1 as doer, but task status CODE_REJECTED is not awaiting review verdict")
	})

	t.Run("future verdict grants no grace", func(t *testing.T) {
		state := waitingDoerAfterVerdict(models.TaskStatusRejected, models.TaskEventRejected, now.Add(time.Minute))

		err := validateAgentInvariants(state, "", true, io.Discard, resolver)
		assertErrorContains(t, err, "is not awaiting review verdict")
	})

	t.Run("later history entry ends the grace", func(t *testing.T) {
		state := waitingDoerAfterVerdict(models.TaskStatusRejected, models.TaskEventRejected, now.Add(-30*time.Second))
		state.Tasks[0].History = append(state.Tasks[0].History, models.TaskHistoryEntry{
			Time:  now.Add(-10 * time.Second),
			Event: models.TaskEventRejectionRCARecorded,
		})

		err := validateAgentInvariants(state, "", true, io.Discard, resolver)
		assertErrorContains(t, err, "is not awaiting review verdict")
	})

	t.Run("verdict without history grants no grace", func(t *testing.T) {
		state := waitingDoerAfterVerdict(models.TaskStatusRejected, models.TaskEventRejected, now)
		state.Tasks[0].History = nil

		err := validateAgentInvariants(state, "", true, io.Discard, resolver)
		assertErrorContains(t, err, "is not awaiting review verdict")
	})

	t.Run("owner check still applies within grace", func(t *testing.T) {
		state := waitingDoerAfterVerdict(models.TaskStatusRejected, models.TaskEventRejected, now.Add(-30*time.Second))
		state.Tasks[0].AssignedTo = testhelpers.StringPtr("coder-2")

		err := validateAgentInvariants(state, "", true, io.Discard, resolver)
		assertErrorContains(t, err, "agent coder-1 says WAITING task-1 as doer, but task assigned_to is coder-2")
	})
}
