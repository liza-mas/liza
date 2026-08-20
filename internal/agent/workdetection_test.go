package agent

import (
	"errors"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/embedded"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/prompts"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// testPipelineResolver creates a models.PipelineResolver from the embedded pipeline config.
// Used by workdetection tests that need a resolver without filesystem setup.
func testPipelineResolver(t *testing.T) models.PipelineResolver {
	t.Helper()
	cfg, err := pipeline.LoadFromBytes(embedded.PipelineConfigContent())
	if err != nil {
		t.Fatalf("failed to load embedded pipeline config: %v", err)
	}
	return pipeline.NewResolver(cfg)
}

func TestCountClaimableTasks(t *testing.T) {
	now := time.Now().UTC()

	tests := []struct {
		name  string
		tasks []models.Task
		want  int
	}{
		{
			name: "single unclaimed task with no dependencies",
			tasks: []models.Task{
				testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now),
			},
			want: 1,
		},
		{
			name: "unclaimed task with satisfied dependencies",
			tasks: []models.Task{
				testhelpers.BuildTaskByStatus("task-1", models.TaskStatusMerged, now),
				func() models.Task {
					task := testhelpers.BuildTaskByStatus("task-2", models.TaskStatusReady, now)
					task.DependsOn = []string{"task-1"}
					return task
				}(),
			},
			want: 1,
		},
		{
			name: "unclaimed task with unsatisfied dependencies",
			tasks: []models.Task{
				testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now),
				func() models.Task {
					task := testhelpers.BuildTaskByStatus("task-2", models.TaskStatusReady, now)
					task.DependsOn = []string{"task-1"}
					return task
				}(),
			},
			want: 0,
		},
		{
			name: "rejected task is claimable",
			tasks: []models.Task{
				testhelpers.BuildTaskByStatus("task-1", models.TaskStatusRejected, now),
			},
			want: 1,
		},
		{
			name: "integration_failed task is claimable",
			tasks: []models.Task{
				testhelpers.BuildTaskByStatus("task-1", models.TaskStatusIntegrationFailed, now),
			},
			want: 1,
		},
		{
			name: "claimed task is not claimable",
			tasks: []models.Task{
				testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now),
			},
			want: 0,
		},
		{
			name: "ready_for_review task is not claimable",
			tasks: []models.Task{
				testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReadyForReview, now),
			},
			want: 0,
		},
		{
			name: "mixed tasks with complex dependencies",
			tasks: []models.Task{
				testhelpers.BuildTaskByStatus("task-1", models.TaskStatusMerged, now),
				testhelpers.BuildTaskByStatus("task-2", models.TaskStatusMerged, now),
				func() models.Task {
					task := testhelpers.BuildTaskByStatus("task-3", models.TaskStatusReady, now)
					task.DependsOn = []string{"task-1"}
					return task
				}(),
				func() models.Task {
					task := testhelpers.BuildTaskByStatus("task-4", models.TaskStatusReady, now)
					task.DependsOn = []string{"task-1", "task-2"}
					return task
				}(),
				func() models.Task {
					task := testhelpers.BuildTaskByStatus("task-5", models.TaskStatusReady, now)
					task.DependsOn = []string{"task-3"} // task-3 not merged yet
					return task
				}(),
			},
			want: 2, // task-3 and task-4 are claimable, task-5 is not
		},
		{
			name:  "empty task list",
			tasks: []models.Task{},
			want:  0,
		},
	}

	pr := testPipelineResolver(t)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := testhelpers.CreateValidState()
			state.Tasks = tt.tasks
			got := models.CountClaimableTasks(state, models.RoleCoder, pr)
			if got != tt.want {
				t.Errorf("CountClaimableTasks() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestCountReviewableTasks(t *testing.T) {
	now := time.Now().UTC()

	tests := []struct {
		name  string
		tasks []models.Task
		want  int
	}{
		{
			name: "ready_for_review with no reviewer",
			tasks: []models.Task{
				testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReadyForReview, now),
			},
			want: 1,
		},
		{
			name: "ready_for_review with expired lease",
			tasks: []models.Task{
				func() models.Task {
					task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReadyForReview, now)
					reviewer := "code-reviewer-1"
					task.ReviewingBy = &reviewer
					expiredTime := now.Add(-1 * time.Hour)
					task.ReviewLeaseExpires = &expiredTime
					return task
				}(),
			},
			want: 1,
		},
		{
			name: "reviewing with valid lease",
			tasks: []models.Task{
				func() models.Task {
					task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReviewing, now)
					futureTime := now.Add(30 * time.Minute)
					task.ReviewLeaseExpires = &futureTime
					return task
				}(),
			},
			want: 0,
		},
		{
			name: "reviewing with reviewer but no lease (malformed)",
			tasks: []models.Task{
				func() models.Task {
					task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReviewing, now)
					task.ReviewLeaseExpires = nil
					return task
				}(),
			},
			want: 0, // Not reviewable - malformed state (no lease to check expiry)
		},
		{
			name: "reviewing with expired lease not counted (needs stale claim clearing first)",
			tasks: []models.Task{
				func() models.Task {
					task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReviewing, now)
					expiredTime := now.Add(-1 * time.Hour)
					task.ReviewLeaseExpires = &expiredTime
					return task
				}(),
			},
			want: 0,
		},
		{
			name: "mixed reviewable and non-reviewable",
			tasks: []models.Task{
				// Reviewable (READY_FOR_REVIEW, unassigned)
				testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReadyForReview, now),
				// Not reviewable (REVIEWING with expired lease — needs stale claim clearing)
				func() models.Task {
					task := testhelpers.BuildTaskByStatus("task-2", models.TaskStatusReviewing, now)
					expiredTime := now.Add(-1 * time.Hour)
					task.ReviewLeaseExpires = &expiredTime
					return task
				}(),
				// Not reviewable (REVIEWING with valid lease)
				func() models.Task {
					task := testhelpers.BuildTaskByStatus("task-3", models.TaskStatusReviewing, now)
					futureTime := now.Add(30 * time.Minute)
					task.ReviewLeaseExpires = &futureTime
					return task
				}(),
				// Not reviewable (IMPLEMENTING)
				testhelpers.BuildTaskByStatus("task-4", models.TaskStatusImplementing, now),
			},
			want: 1,
		},
		{
			name: "no ready_for_review tasks",
			tasks: []models.Task{
				testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now),
				testhelpers.BuildTaskByStatus("task-2", models.TaskStatusReady, now),
			},
			want: 0,
		},
		{
			name:  "empty task list",
			tasks: []models.Task{},
			want:  0,
		},
	}

	pr := testPipelineResolver(t)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := testhelpers.CreateValidState()
			state.Tasks = tt.tasks
			got := models.CountReviewableTasks(state, models.RoleCodeReviewer, pr)
			if got != tt.want {
				t.Errorf("CountReviewableTasks() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestOrchestratorWakeTriggerSpecs(t *testing.T) {
	// SprintComplete is handled separately in DetectOrchestratorWakeTriggers
	// (requires pipeline-aware terminal state checking), so it's not in the table.
	wantOrder := []OrchestratorWakeTrigger{
		WakeTriggerInitialPlanning,
		WakeTriggerBlocked,
		WakeTriggerHypothesisExhausted,
		WakeTriggerImmediateDiscovery,
	}

	if len(orchestratorWakeTriggerSpecs) != len(wantOrder) {
		t.Fatalf("orchestratorWakeTriggerSpecs has %d entries, want %d", len(orchestratorWakeTriggerSpecs), len(wantOrder))
	}

	for i, wantTrigger := range wantOrder {
		spec := orchestratorWakeTriggerSpecs[i]
		if spec.Trigger != wantTrigger {
			t.Errorf("orchestratorWakeTriggerSpecs[%d].Trigger = %q, want %q", i, spec.Trigger, wantTrigger)
		}
		if spec.Description == "" {
			t.Errorf("orchestratorWakeTriggerSpecs[%d].Description must not be empty", i)
		}
		if spec.Count == nil {
			t.Errorf("orchestratorWakeTriggerSpecs[%d].Count must not be nil", i)
		}
	}
}

func TestDetectOrchestratorWakeTriggers(t *testing.T) {
	now := time.Now().UTC()

	tests := []struct {
		name        string
		state       *models.State
		wantTrigger OrchestratorWakeTrigger
		wantCount   int
	}{
		{
			name: "initial planning (no tasks)",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				state.Tasks = []models.Task{}
				return state
			}(),
			wantTrigger: WakeTriggerInitialPlanning,
			wantCount:   1,
		},
		{
			name: "blocked tasks trigger (no prior assessment = actionable)",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				state.Tasks = []models.Task{
					testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now),
					testhelpers.BuildTaskByStatus("task-2", models.TaskStatusBlocked, now),
				}
				return state
			}(),
			wantTrigger: WakeTriggerBlocked,
			wantCount:   2,
		},
		{
			name: "blocked assessed no new activity no deps - no wake",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now)
				agent := "orchestrator-1"
				task.History = append(task.History, models.TaskHistoryEntry{
					Time:  now.Add(-10 * time.Minute),
					Event: models.TaskEventOrchestratorAssessment,
					Agent: &agent,
				})
				state.Tasks = []models.Task{task}
				return state
			}(),
			wantTrigger: WakeTriggerNone,
			wantCount:   0,
		},
		{
			name: "blocked assessed task has new non-assessment activity - wake",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now)
				agent := "orchestrator-1"
				coder := "coder-1"
				task.History = append(task.History,
					models.TaskHistoryEntry{
						Time:  now.Add(-10 * time.Minute),
						Event: models.TaskEventOrchestratorAssessment,
						Agent: &agent,
					},
					models.TaskHistoryEntry{
						Time:  now.Add(-5 * time.Minute),
						Event: models.TaskEventBlocked,
						Agent: &coder,
					},
				)
				state.Tasks = []models.Task{task}
				return state
			}(),
			wantTrigger: WakeTriggerBlocked,
			wantCount:   1,
		},
		{
			name: "blocked assessed dependency merged after assessment - wake",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now)
				task.DependsOn = []string{"task-2"}
				agent := "orchestrator-1"
				task.History = append(task.History, models.TaskHistoryEntry{
					Time:  now.Add(-10 * time.Minute),
					Event: models.TaskEventOrchestratorAssessment,
					Agent: &agent,
				})
				dep := testhelpers.BuildTaskByStatus("task-2", models.TaskStatusMerged, now)
				reviewer := "code-reviewer-1"
				dep.History = append(dep.History, models.TaskHistoryEntry{
					Time:  now.Add(-5 * time.Minute),
					Event: models.TaskEventMerged,
					Agent: &reviewer,
				})
				state.Tasks = []models.Task{task, dep}
				return state
			}(),
			wantTrigger: WakeTriggerBlocked,
			wantCount:   1,
		},
		{
			name: "blocked assessed dependency superseded to merged replacement after assessment - wake",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now)
				task.DependsOn = []string{"task-2"}
				agent := "orchestrator-1"
				task.History = append(task.History, models.TaskHistoryEntry{
					Time:  now.Add(-10 * time.Minute),
					Event: models.TaskEventOrchestratorAssessment,
					Agent: &agent,
				})
				dep := testhelpers.BuildTaskByStatus("task-2", models.TaskStatusSuperseded, now)
				dep.SupersededBy = []string{"task-3"}
				dep.History = append(dep.History, models.TaskHistoryEntry{
					Time:  now.Add(-5 * time.Minute),
					Event: models.TaskEventSuperseded,
					Agent: &agent,
				})
				replacement := testhelpers.BuildTaskByStatus("task-3", models.TaskStatusMerged, now)
				state.Tasks = []models.Task{task, dep, replacement}
				return state
			}(),
			wantTrigger: WakeTriggerBlocked,
			wantCount:   1,
		},
		{
			name: "blocked assessed superseded dependency replacement merged after assessment - wake",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now)
				task.DependsOn = []string{"task-2"}
				agent := "orchestrator-1"
				task.History = append(task.History, models.TaskHistoryEntry{
					Time:  now.Add(-10 * time.Minute),
					Event: models.TaskEventOrchestratorAssessment,
					Agent: &agent,
				})
				dep := testhelpers.BuildTaskByStatus("task-2", models.TaskStatusSuperseded, now)
				dep.SupersededBy = []string{"task-3"}
				dep.History = append(dep.History, models.TaskHistoryEntry{
					Time:  now.Add(-20 * time.Minute),
					Event: models.TaskEventSuperseded,
					Agent: &agent,
				})
				replacement := testhelpers.BuildTaskByStatus("task-3", models.TaskStatusMerged, now)
				reviewer := "code-reviewer-1"
				replacement.History = append(replacement.History, models.TaskHistoryEntry{
					Time:  now.Add(-5 * time.Minute),
					Event: models.TaskEventMerged,
					Agent: &reviewer,
				})
				state.Tasks = []models.Task{task, dep, replacement}
				return state
			}(),
			wantTrigger: WakeTriggerBlocked,
			wantCount:   1,
		},
		{
			name: "blocked assessed dependency submitted after assessment - no wake",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now)
				task.DependsOn = []string{"task-2"}
				agent := "orchestrator-1"
				task.History = append(task.History, models.TaskHistoryEntry{
					Time:  now.Add(-10 * time.Minute),
					Event: models.TaskEventOrchestratorAssessment,
					Agent: &agent,
				})
				dep := testhelpers.BuildTaskByStatus("task-2", models.TaskStatusReadyForReview, now)
				coder := "coder-1"
				dep.History = append(dep.History, models.TaskHistoryEntry{
					Time:  now.Add(-5 * time.Minute),
					Event: models.TaskEventSubmittedForReview,
					Agent: &coder,
				})
				state.Tasks = []models.Task{task, dep}
				return state
			}(),
			wantTrigger: WakeTriggerNone,
			wantCount:   0,
		},
		{
			name: "blocked assessed dependency approved after assessment - no wake",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now)
				task.DependsOn = []string{"task-2"}
				agent := "orchestrator-1"
				task.History = append(task.History, models.TaskHistoryEntry{
					Time:  now.Add(-10 * time.Minute),
					Event: models.TaskEventOrchestratorAssessment,
					Agent: &agent,
				})
				dep := testhelpers.BuildTaskByStatus("task-2", models.TaskStatusApproved, now)
				reviewer := "code-reviewer-1"
				dep.History = append(dep.History, models.TaskHistoryEntry{
					Time:  now.Add(-5 * time.Minute),
					Event: models.TaskEventApproved,
					Agent: &reviewer,
				})
				state.Tasks = []models.Task{task, dep}
				return state
			}(),
			wantTrigger: WakeTriggerNone,
			wantCount:   0,
		},
		{
			name: "blocked assessed dependency claimed after assessment - no wake",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now)
				task.DependsOn = []string{"task-2"}
				agent := "orchestrator-1"
				task.History = append(task.History, models.TaskHistoryEntry{
					Time:  now.Add(-10 * time.Minute),
					Event: models.TaskEventOrchestratorAssessment,
					Agent: &agent,
				})
				dep := testhelpers.BuildTaskByStatus("task-2", models.TaskStatusImplementing, now)
				coder := "coder-1"
				dep.History = append(dep.History, models.TaskHistoryEntry{
					Time:  now.Add(-5 * time.Minute),
					Event: models.TaskEventClaimed,
					Agent: &coder,
				})
				state.Tasks = []models.Task{task, dep}
				return state
			}(),
			wantTrigger: WakeTriggerNone,
			wantCount:   0,
		},
		{
			name: "blocked assessed dependency blocked after assessment - no wake",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now)
				task.DependsOn = []string{"task-2"}
				agent := "orchestrator-1"
				task.History = append(task.History, models.TaskHistoryEntry{
					Time:  now.Add(-10 * time.Minute),
					Event: models.TaskEventOrchestratorAssessment,
					Agent: &agent,
				})
				dep := testhelpers.BuildTaskByStatus("task-2", models.TaskStatusBlocked, now)
				coder := "coder-1"
				dep.History = append(dep.History,
					models.TaskHistoryEntry{
						Time:  now.Add(-5 * time.Minute),
						Event: models.TaskEventBlocked,
						Agent: &coder,
					},
					models.TaskHistoryEntry{
						Time:  now.Add(-4 * time.Minute),
						Event: models.TaskEventOrchestratorAssessment,
						Agent: &agent,
					},
				)
				state.Tasks = []models.Task{task, dep}
				return state
			}(),
			wantTrigger: WakeTriggerNone,
			wantCount:   0,
		},
		{
			name: "blocked assessed dependency has only old activity - no wake",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now)
				task.DependsOn = []string{"task-2"}
				agent := "orchestrator-1"
				task.History = append(task.History, models.TaskHistoryEntry{
					Time:  now.Add(-10 * time.Minute),
					Event: models.TaskEventOrchestratorAssessment,
					Agent: &agent,
				})
				dep := testhelpers.BuildTaskByStatus("task-2", models.TaskStatusImplementing, now)
				coder := "coder-1"
				dep.History = append(dep.History, models.TaskHistoryEntry{
					Time:  now.Add(-20 * time.Minute),
					Event: models.TaskEventClaimed,
					Agent: &coder,
				})
				state.Tasks = []models.Task{task, dep}
				return state
			}(),
			wantTrigger: WakeTriggerNone,
			wantCount:   0,
		},
		{
			name: "blocked assessed dependency has only newer assessment - no wake (cascade prevention)",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now)
				task.DependsOn = []string{"task-2"}
				agent := "orchestrator-1"
				task.History = append(task.History, models.TaskHistoryEntry{
					Time:  now.Add(-10 * time.Minute),
					Event: models.TaskEventOrchestratorAssessment,
					Agent: &agent,
				})
				dep := testhelpers.BuildTaskByStatus("task-2", models.TaskStatusBlocked, now)
				dep.History = append(dep.History, models.TaskHistoryEntry{
					Time:  now.Add(-5 * time.Minute),
					Event: models.TaskEventOrchestratorAssessment,
					Agent: &agent,
				})
				state.Tasks = []models.Task{task, dep}
				return state
			}(),
			wantTrigger: WakeTriggerNone,
			wantCount:   0,
		},
		{
			name: "blocked assessed human_note targeting task after assessment - wake",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now)
				agent := "orchestrator-1"
				task.History = append(task.History, models.TaskHistoryEntry{
					Time:  now.Add(-10 * time.Minute),
					Event: models.TaskEventOrchestratorAssessment,
					Agent: &agent,
				})
				state.Tasks = []models.Task{task}
				state.HumanNotes = []models.HumanNote{
					{
						Timestamp: now.Add(-5 * time.Minute),
						Message:   "API spec is now available",
						For:       "task-1",
					},
				}
				return state
			}(),
			wantTrigger: WakeTriggerBlocked,
			wantCount:   1,
		},
		{
			name: "blocked assessed human_note targeting all after assessment - wake",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now)
				agent := "orchestrator-1"
				task.History = append(task.History, models.TaskHistoryEntry{
					Time:  now.Add(-10 * time.Minute),
					Event: models.TaskEventOrchestratorAssessment,
					Agent: &agent,
				})
				state.Tasks = []models.Task{task}
				state.HumanNotes = []models.HumanNote{
					{
						Timestamp: now.Add(-5 * time.Minute),
						Message:   "New info available for all tasks",
						For:       "all",
					},
				}
				return state
			}(),
			wantTrigger: WakeTriggerBlocked,
			wantCount:   1,
		},
		{
			name: "mixed blocked - some actionable some not",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				// task-1: assessed, no new activity → not actionable
				task1 := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now)
				agent := "orchestrator-1"
				task1.History = append(task1.History, models.TaskHistoryEntry{
					Time:  now.Add(-10 * time.Minute),
					Event: models.TaskEventOrchestratorAssessment,
					Agent: &agent,
				})
				// task-2: never assessed → actionable
				task2 := testhelpers.BuildTaskByStatus("task-2", models.TaskStatusBlocked, now)
				state.Tasks = []models.Task{task1, task2}
				return state
			}(),
			wantTrigger: WakeTriggerBlocked,
			wantCount:   1,
		},
		{
			name: "hypothesis exhaustion trigger (no prior assessment = actionable)",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now)
				task.FailedBy = []string{"coder-1", "coder-2"}
				state.Tasks = []models.Task{task}
				return state
			}(),
			wantTrigger: WakeTriggerHypothesisExhausted,
			wantCount:   1,
		},
		{
			name: "hypothesis exhausted assessed no new activity - no wake",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now)
				task.FailedBy = []string{"coder-1", "coder-2"}
				agent := "orchestrator-1"
				task.History = append(task.History, models.TaskHistoryEntry{
					Time:  now.Add(-10 * time.Minute),
					Event: models.TaskEventOrchestratorAssessment,
					Agent: &agent,
				})
				state.Tasks = []models.Task{task}
				return state
			}(),
			wantTrigger: WakeTriggerNone,
			wantCount:   0,
		},
		{
			name: "blocked hypothesis exhausted uses blocked trigger",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now)
				task.FailedBy = []string{"coder-1", "coder-2"}
				state.Tasks = []models.Task{task}
				return state
			}(),
			wantTrigger: WakeTriggerBlocked,
			wantCount:   1,
		},
		{
			name: "hypothesis exhausted assessed new rejection activity - wake",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now)
				task.FailedBy = []string{"coder-1", "coder-2", "coder-3"}
				agent := "orchestrator-1"
				coder := "coder-3"
				task.History = append(task.History,
					models.TaskHistoryEntry{
						Time:  now.Add(-10 * time.Minute),
						Event: models.TaskEventOrchestratorAssessment,
						Agent: &agent,
					},
					models.TaskHistoryEntry{
						Time:  now.Add(-5 * time.Minute),
						Event: models.TaskEventRejected,
						Agent: &coder,
					},
				)
				state.Tasks = []models.Task{task}
				return state
			}(),
			wantTrigger: WakeTriggerHypothesisExhausted,
			wantCount:   1,
		},
		{
			name: "hypothesis exhausted assessed human note - wake",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now)
				task.FailedBy = []string{"coder-1", "coder-2"}
				agent := "orchestrator-1"
				task.History = append(task.History, models.TaskHistoryEntry{
					Time:  now.Add(-10 * time.Minute),
					Event: models.TaskEventOrchestratorAssessment,
					Agent: &agent,
				})
				state.Tasks = []models.Task{task}
				state.HumanNotes = []models.HumanNote{
					{
						Timestamp: now.Add(-5 * time.Minute),
						Message:   "Try a different approach",
						For:       "task-1",
					},
				}
				return state
			}(),
			wantTrigger: WakeTriggerHypothesisExhausted,
			wantCount:   1,
		},
		{
			name: "mixed hypothesis exhausted - some actionable some not",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				// task-1: assessed, no new activity → not actionable
				task1 := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now)
				task1.FailedBy = []string{"coder-1", "coder-2"}
				agent := "orchestrator-1"
				task1.History = append(task1.History, models.TaskHistoryEntry{
					Time:  now.Add(-10 * time.Minute),
					Event: models.TaskEventOrchestratorAssessment,
					Agent: &agent,
				})
				// task-2: never assessed → actionable
				task2 := testhelpers.BuildTaskByStatus("task-2", models.TaskStatusReady, now)
				task2.FailedBy = []string{"coder-1", "coder-2"}
				state.Tasks = []models.Task{task1, task2}
				return state
			}(),
			wantTrigger: WakeTriggerHypothesisExhausted,
			wantCount:   1,
		},
		{
			name: "immediate discovery trigger",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				state.Tasks = []models.Task{
					testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now),
				}
				state.Discovered = []models.Discovery{
					{
						ID:             "disc-1",
						By:             "coder-1",
						During:         "task-1",
						Description:    "Critical bug",
						Severity:       "critical",
						Urgency:        "immediate",
						Recommendation: "Fix now",
						Created:        now,
					},
				}
				return state
			}(),
			wantTrigger: WakeTriggerImmediateDiscovery,
			wantCount:   1,
		},
		{
			name: "no triggers (tasks in progress)",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				state.Tasks = []models.Task{
					testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now),
					testhelpers.BuildTaskByStatus("task-2", models.TaskStatusReadyForReview, now),
				}
				return state
			}(),
			wantTrigger: WakeTriggerNone,
			wantCount:   0,
		},
		{
			name: "multiple triggers (blocked takes priority over hypothesis exhausted)",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				task1 := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now)
				task2 := testhelpers.BuildTaskByStatus("task-2", models.TaskStatusReady, now)
				task2.FailedBy = []string{"coder-1", "coder-2"}
				state.Tasks = []models.Task{task1, task2}
				return state
			}(),
			wantTrigger: WakeTriggerBlocked,
			wantCount:   1,
		},
		{
			name: "deferred discovery does not trigger",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				state.Tasks = []models.Task{
					testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now),
				}
				state.Discovered = []models.Discovery{
					{
						ID:             "disc-1",
						By:             "coder-1",
						During:         "task-1",
						Description:    "Minor issue",
						Severity:       "low",
						Urgency:        "deferred",
						Recommendation: "Fix later",
						Created:        now,
					},
				}
				return state
			}(),
			wantTrigger: WakeTriggerNone,
			wantCount:   0,
		},
		{
			name: "converted discovery does not trigger",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				state.Tasks = []models.Task{
					testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now),
				}
				taskID := "task-2"
				state.Discovered = []models.Discovery{
					{
						ID:              "disc-1",
						By:              "coder-1",
						During:          "task-1",
						Description:     "Critical bug",
						Severity:        "critical",
						Urgency:         "immediate",
						Recommendation:  "Fix now",
						Created:         now,
						ConvertedToTask: &taskID,
					},
				}
				return state
			}(),
			wantTrigger: WakeTriggerNone,
			wantCount:   0,
		},
		{
			name: "sprint complete (all planned tasks merged)",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				state.Sprint.Scope.Planned = []string{"task-1", "task-2"}
				state.Tasks = []models.Task{
					testhelpers.BuildTaskByStatus("task-1", models.TaskStatusMerged, now),
					testhelpers.BuildTaskByStatus("task-2", models.TaskStatusMerged, now),
				}
				return state
			}(),
			wantTrigger: WakeTriggerSprintComplete,
			wantCount:   2,
		},
		{
			name: "sprint complete (mixed terminal states)",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				state.Sprint.Scope.Planned = []string{"task-1", "task-2", "task-3"}
				state.Tasks = []models.Task{
					testhelpers.BuildTaskByStatus("task-1", models.TaskStatusMerged, now),
					testhelpers.BuildTaskByStatus("task-2", models.TaskStatusAbandoned, now),
					testhelpers.BuildTaskByStatus("task-3", models.TaskStatusSuperseded, now),
				}
				return state
			}(),
			wantTrigger: WakeTriggerSprintComplete,
			wantCount:   3,
		},
		{
			name: "sprint not complete (some tasks in progress)",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				state.Sprint.Scope.Planned = []string{"task-1", "task-2"}
				state.Tasks = []models.Task{
					testhelpers.BuildTaskByStatus("task-1", models.TaskStatusMerged, now),
					testhelpers.BuildTaskByStatus("task-2", models.TaskStatusImplementing, now),
				}
				return state
			}(),
			wantTrigger: WakeTriggerNone,
			wantCount:   0,
		},
		{
			name: "sprint not complete (planned task not in task list)",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				state.Sprint.Scope.Planned = []string{"task-1", "task-2"}
				state.Tasks = []models.Task{
					testhelpers.BuildTaskByStatus("task-1", models.TaskStatusMerged, now),
				}
				return state
			}(),
			wantTrigger: WakeTriggerNone,
			wantCount:   0,
		},
		{
			name: "empty planned list does not trigger sprint complete",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				state.Sprint.Scope.Planned = []string{}
				state.Tasks = []models.Task{
					testhelpers.BuildTaskByStatus("task-1", models.TaskStatusMerged, now),
				}
				return state
			}(),
			wantTrigger: WakeTriggerNone,
			wantCount:   0,
		},
		{
			name: "planning complete (merged planning task with output[])",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusMerged, now)
				task.RolePair = "code-planning-pair"
				task.Output = []models.OutputEntry{
					{Desc: "implement X", DoneWhen: "tests pass", Scope: "pkg/x"},
					{Desc: "implement Y", DoneWhen: "linter green", Scope: "pkg/y"},
				}
				state.Sprint.Scope.Planned = []string{"task-1"}
				state.Tasks = []models.Task{task}
				return state
			}(),
			wantTrigger: WakeTriggerPlanningComplete,
			wantCount:   1,
		},
		{
			name: "planning complete fires with unrelated active planned task",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				readyPlan := testhelpers.BuildTaskByStatus("plan-ready", models.TaskStatusMerged, now)
				readyPlan.RolePair = "code-planning-pair"
				readyPlan.Output = []models.OutputEntry{
					{Desc: "implement X", DoneWhen: "tests pass", Scope: "pkg/x"},
				}
				activePlan := testhelpers.BuildTaskByStatus("plan-active", models.TaskStatusCodePlanning, now)
				activePlan.RolePair = "code-planning-pair"
				state.Sprint.Scope.Planned = []string{"plan-ready", "plan-active"}
				state.Tasks = []models.Task{readyPlan, activePlan}
				return state
			}(),
			wantTrigger: WakeTriggerPlanningComplete,
			wantCount:   1,
		},
		{
			name: "merged planning task with TransitionsExecuted triggers sprint complete not planning complete",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusMerged, now)
				task.RolePair = "code-planning-pair"
				task.Output = []models.OutputEntry{
					{Desc: "implement X", DoneWhen: "tests pass", Scope: "pkg/x"},
				}
				task.TransitionsExecuted = map[string]bool{"code-plan-to-coding": true}
				state.Sprint.Scope.Planned = []string{"task-1"}
				state.Tasks = []models.Task{task}
				return state
			}(),
			wantTrigger: WakeTriggerSprintComplete,
			wantCount:   1,
		},
		{
			name: "merged coding task with output[] triggers sprint complete not planning complete",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusMerged, now)
				// No RolePair or non-planning role pair — coding task
				task.Output = []models.OutputEntry{
					{Desc: "something", DoneWhen: "done", Scope: "pkg/x"},
				}
				state.Sprint.Scope.Planned = []string{"task-1"}
				state.Tasks = []models.Task{task}
				return state
			}(),
			wantTrigger: WakeTriggerSprintComplete,
			wantCount:   1,
		},
		{
			name: "merged task without output[] triggers sprint complete not planning complete",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				state.Sprint.Scope.Planned = []string{"task-1"}
				state.Tasks = []models.Task{
					testhelpers.BuildTaskByStatus("task-1", models.TaskStatusMerged, now),
				}
				return state
			}(),
			wantTrigger: WakeTriggerSprintComplete,
			wantCount:   1,
		},
		{
			name: "sprint CHECKPOINT suppresses re-wake",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				state.Sprint.Status = models.SprintStatusCheckpoint
				state.Sprint.Scope.Planned = []string{"task-1", "task-2"}
				state.Tasks = []models.Task{
					testhelpers.BuildTaskByStatus("task-1", models.TaskStatusMerged, now),
					testhelpers.BuildTaskByStatus("task-2", models.TaskStatusMerged, now),
				}
				return state
			}(),
			wantTrigger: WakeTriggerNone,
			wantCount:   0,
		},
		{
			name: "sprint CHECKPOINT suppresses planning complete re-wake",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				state.Sprint.Status = models.SprintStatusCheckpoint
				task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusMerged, now)
				task.RolePair = "code-planning-pair"
				task.Output = []models.OutputEntry{
					{Desc: "implement X", DoneWhen: "tests pass", Scope: "pkg/x"},
				}
				state.Sprint.Scope.Planned = []string{"task-1"}
				state.Tasks = []models.Task{task}
				return state
			}(),
			wantTrigger: WakeTriggerNone,
			wantCount:   0,
		},
		{
			name: "sprint COMPLETED suppresses re-wake",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				state.Sprint.Status = models.SprintStatusCompleted
				state.Sprint.Scope.Planned = []string{"task-1"}
				state.Tasks = []models.Task{
					testhelpers.BuildTaskByStatus("task-1", models.TaskStatusMerged, now),
				}
				return state
			}(),
			wantTrigger: WakeTriggerNone,
			wantCount:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DetectOrchestratorWakeTriggers(tt.state, nil, nil, nil)
			if result.Trigger != tt.wantTrigger {
				t.Errorf("DetectOrchestratorWakeTriggers() trigger = %v, want %v", result.Trigger, tt.wantTrigger)
			}
			if result.Count != tt.wantCount {
				t.Errorf("DetectOrchestratorWakeTriggers() count = %d, want %d", result.Count, tt.wantCount)
			}
		})
	}
}

func TestDetectOrchestratorWakeTriggers_PipelineTerminals(t *testing.T) {
	now := time.Now().UTC()

	tests := []struct {
		name              string
		state             *models.State
		pipelineTerminals []models.TaskStatus
		planningPairs     map[string]bool
		wantTrigger       OrchestratorWakeTrigger
		wantCount         int
	}{
		{
			name: "CODING_PLAN_APPROVED is sprint-terminal with pipeline terminals",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusCodingPlanApproved, now)
				task.RolePair = "code-planning-pair"
				state.Tasks = []models.Task{task}
				state.Sprint.Scope.Planned = []string{"task-1"}
				return state
			}(),
			pipelineTerminals: []models.TaskStatus{models.TaskStatusCodingPlanApproved, models.TaskStatusMerged},
			wantTrigger:       WakeTriggerSprintComplete,
			wantCount:         1,
		},
		{
			name: "CODING_PLAN_APPROVED is NOT sprint-terminal without pipeline terminals",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusCodingPlanApproved, now)
				task.RolePair = "code-planning-pair"
				state.Tasks = []models.Task{task}
				state.Sprint.Scope.Planned = []string{"task-1"}
				return state
			}(),
			pipelineTerminals: nil,
			wantTrigger:       WakeTriggerNone,
			wantCount:         0,
		},
		{
			name: "mixed legacy and pipeline tasks — all terminal",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				legacyTask := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusMerged, now)
				pipelineTask := testhelpers.BuildTaskByStatus("task-2", models.TaskStatusCodingPlanApproved, now)
				pipelineTask.RolePair = "code-planning-pair"
				state.Tasks = []models.Task{legacyTask, pipelineTask}
				state.Sprint.Scope.Planned = []string{"task-1", "task-2"}
				return state
			}(),
			pipelineTerminals: []models.TaskStatus{models.TaskStatusCodingPlanApproved, models.TaskStatusMerged},
			wantTrigger:       WakeTriggerSprintComplete,
			wantCount:         2,
		},
		{
			name: "mixed — pipeline task not yet terminal",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				legacyTask := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusMerged, now)
				pipelineTask := testhelpers.BuildTaskByStatus("task-2", models.TaskStatusCodePlanning, now)
				pipelineTask.RolePair = "code-planning-pair"
				state.Tasks = []models.Task{legacyTask, pipelineTask}
				state.Sprint.Scope.Planned = []string{"task-1", "task-2"}
				return state
			}(),
			pipelineTerminals: []models.TaskStatus{models.TaskStatusCodingPlanApproved, models.TaskStatusMerged},
			wantTrigger:       WakeTriggerNone,
			wantCount:         0,
		},
		{
			name: "epic-planning-pair with planningPairs triggers PLANNING_COMPLETE",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusMerged, now)
				task.RolePair = "epic-planning-pair"
				task.Output = []models.OutputEntry{
					{Desc: "user story 1", DoneWhen: "accepted", Scope: "feature/x"},
				}
				state.Sprint.Scope.Planned = []string{"task-1"}
				state.Tasks = []models.Task{task}
				return state
			}(),
			planningPairs: map[string]bool{"epic-planning-pair": true, "code-planning-pair": true},
			wantTrigger:   WakeTriggerPlanningComplete,
			wantCount:     1,
		},
		{
			name: "epic-planning-pair without planningPairs falls back to SPRINT_COMPLETE",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusMerged, now)
				task.RolePair = "epic-planning-pair"
				task.Output = []models.OutputEntry{
					{Desc: "user story 1", DoneWhen: "accepted", Scope: "feature/x"},
				}
				state.Sprint.Scope.Planned = []string{"task-1"}
				state.Tasks = []models.Task{task}
				return state
			}(),
			planningPairs: nil,
			wantTrigger:   WakeTriggerSprintComplete,
			wantCount:     1,
		},
		{
			name: "cycle-blocked planning excluded from PLANNING_COMPLETE count",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				// Normal planning task → triggers PLANNING_COMPLETE
				normal := testhelpers.BuildTaskByStatus("plan-normal", models.TaskStatusMerged, now)
				normal.RolePair = "code-planning-pair"
				normal.Output = []models.OutputEntry{{Desc: "a", DoneWhen: "a", Scope: "a"}}
				// Cycle-blocked planning task → excluded
				cycled := testhelpers.BuildTaskByStatus("plan-cycled", models.TaskStatusMerged, now)
				cycled.RolePair = "code-planning-pair"
				cycled.Output = []models.OutputEntry{{Desc: "b", DoneWhen: "b", Scope: "b"}}
				cycled.History = append(cycled.History, models.TaskHistoryEntry{
					Time:  now,
					Event: models.TaskEventTransitionCycleBlocked,
					Extra: map[string]any{"transition": "code-plan-to-coding"},
				})
				state.Tasks = []models.Task{normal, cycled}
				state.Sprint.Scope.Planned = []string{"plan-normal", "plan-cycled"}
				return state
			}(),
			planningPairs: map[string]bool{"code-planning-pair": true},
			wantTrigger:   WakeTriggerPlanningComplete,
			wantCount:     1, // only normal, not cycled
		},
		{
			name: "all cycle-blocked → SPRINT_COMPLETE not PLANNING_COMPLETE",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				cycled := testhelpers.BuildTaskByStatus("plan-cycled", models.TaskStatusMerged, now)
				cycled.RolePair = "code-planning-pair"
				cycled.Output = []models.OutputEntry{{Desc: "b", DoneWhen: "b", Scope: "b"}}
				cycled.History = append(cycled.History, models.TaskHistoryEntry{
					Time:  now,
					Event: models.TaskEventTransitionCycleBlocked,
					Extra: map[string]any{"transition": "code-plan-to-coding"},
				})
				state.Tasks = []models.Task{cycled}
				state.Sprint.Scope.Planned = []string{"plan-cycled"}
				return state
			}(),
			planningPairs: map[string]bool{"code-planning-pair": true},
			wantTrigger:   WakeTriggerSprintComplete,
			wantCount:     1,
		},
		{
			name: "transitively downstream of cycle excluded from PLANNING_COMPLETE count",
			state: func() *models.State {
				state := testhelpers.CreateValidState()

				normal := testhelpers.BuildTaskByStatus("plan-normal", models.TaskStatusMerged, now)
				normal.RolePair = "code-planning-pair"
				normal.Output = []models.OutputEntry{{Desc: "normal", DoneWhen: "done", Scope: "s"}}

				planA := testhelpers.BuildTaskByStatus("plan-a", models.TaskStatusMerged, now)
				planA.RolePair = "code-planning-pair"
				planA.Output = []models.OutputEntry{{Desc: "a", DoneWhen: "done", Scope: "s"}}
				planA.History = append(planA.History, models.TaskHistoryEntry{
					Time:  now,
					Event: models.TaskEventTransitionCycleBlocked,
					Extra: map[string]any{"transition": "code-plan-to-coding", "cycle_members": []string{"plan-a", "plan-b"}},
				})

				planB := testhelpers.BuildTaskByStatus("plan-b", models.TaskStatusMerged, now)
				planB.RolePair = "code-planning-pair"
				planB.Output = []models.OutputEntry{{Desc: "b", DoneWhen: "done", Scope: "s"}}
				planB.History = append(planB.History, models.TaskHistoryEntry{
					Time:  now,
					Event: models.TaskEventTransitionCycleBlocked,
					Extra: map[string]any{"transition": "code-plan-to-coding", "cycle_members": []string{"plan-a", "plan-b"}},
				})

				planC := testhelpers.BuildTaskByStatus("plan-c", models.TaskStatusMerged, now)
				planC.RolePair = "code-planning-pair"
				planC.Output = []models.OutputEntry{{Desc: "c", DoneWhen: "done", Scope: "s"}}
				planC.DependsOn = []string{"plan-a"}

				planD := testhelpers.BuildTaskByStatus("plan-d", models.TaskStatusMerged, now)
				planD.RolePair = "code-planning-pair"
				planD.Output = []models.OutputEntry{{Desc: "d", DoneWhen: "done", Scope: "s"}}
				planD.DependsOn = []string{"plan-c"}

				state.Tasks = []models.Task{normal, planA, planB, planC, planD}
				state.Sprint.Scope.Planned = []string{"plan-normal", "plan-a", "plan-b", "plan-c", "plan-d"}
				return state
			}(),
			planningPairs: map[string]bool{"code-planning-pair": true},
			wantTrigger:   WakeTriggerPlanningComplete,
			wantCount:     1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DetectOrchestratorWakeTriggers(tt.state, tt.pipelineTerminals, tt.planningPairs, nil)
			if result.Trigger != tt.wantTrigger {
				t.Errorf("DetectOrchestratorWakeTriggers() trigger = %v, want %v", result.Trigger, tt.wantTrigger)
			}
			if result.Count != tt.wantCount {
				t.Errorf("DetectOrchestratorWakeTriggers() count = %d, want %d", result.Count, tt.wantCount)
			}
		})
	}
}

func TestHasCoderWork(t *testing.T) {
	now := time.Now().UTC()

	tests := []struct {
		name  string
		state *models.State
		want  bool
	}{
		{
			name: "claimable task available",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				state.Tasks = []models.Task{
					testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now),
				}
				return state
			}(),
			want: true,
		},
		{
			name: "no claimable tasks",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				state.Tasks = []models.Task{
					testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now),
				}
				return state
			}(),
			want: false,
		},
		{
			name: "empty tasks",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				state.Tasks = []models.Task{}
				return state
			}(),
			want: false,
		},
	}

	pr := testPipelineResolver(t)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := models.CountClaimableTasks(tt.state, models.RoleCoder, pr) > 0
			if got != tt.want {
				t.Errorf("CountClaimableTasks() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHasReviewerWork(t *testing.T) {
	now := time.Now().UTC()

	tests := []struct {
		name  string
		state *models.State
		want  bool
	}{
		{
			name: "reviewable task available",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				state.Tasks = []models.Task{
					testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReadyForReview, now),
				}
				return state
			}(),
			want: true,
		},
		{
			name: "no reviewable tasks",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				state.Tasks = []models.Task{
					testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now),
				}
				return state
			}(),
			want: false,
		},
		{
			name: "empty tasks",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				state.Tasks = []models.Task{}
				return state
			}(),
			want: false,
		},
	}

	pr := testPipelineResolver(t)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := models.CountReviewableTasks(tt.state, models.RoleCodeReviewer, pr) > 0
			if got != tt.want {
				t.Errorf("CountReviewableTasks() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHasOrchestratorWork(t *testing.T) {
	now := time.Now().UTC()

	tests := []struct {
		name  string
		state *models.State
		want  bool
	}{
		{
			name: "initial planning needed",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				state.Tasks = []models.Task{}
				return state
			}(),
			want: true,
		},
		{
			name: "blocked tasks trigger",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				state.Tasks = []models.Task{
					testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now),
				}
				return state
			}(),
			want: true,
		},
		{
			name: "no triggers",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				state.Tasks = []models.Task{
					testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now),
				}
				return state
			}(),
			want: false,
		},
		{
			name: "sprint complete triggers orchestrator work",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				state.Sprint.Scope.Planned = []string{"task-1", "task-2"}
				state.Tasks = []models.Task{
					testhelpers.BuildTaskByStatus("task-1", models.TaskStatusMerged, now),
					testhelpers.BuildTaskByStatus("task-2", models.TaskStatusMerged, now),
				}
				return state
			}(),
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DetectOrchestratorWakeTriggers(tt.state, nil, nil, nil)
			got := result.Trigger != WakeTriggerNone
			if got != tt.want {
				t.Errorf("HasOrchestratorWork() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetCoderWorkDiagnostics(t *testing.T) {
	now := time.Now().UTC()

	tests := []struct {
		name    string
		state   *models.State
		wantMsg string
	}{
		{
			name: "no tasks at all",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				state.Tasks = []models.Task{}
				return state
			}(),
			wantMsg: "No claimable tasks",
		},
		{
			name: "tasks blocked by dependencies",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				task1 := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now)
				task1.DependsOn = []string{"task-0"}
				task2 := testhelpers.BuildTaskByStatus("task-2", models.TaskStatusReady, now)
				task2.DependsOn = []string{"task-0"}
				state.Tasks = []models.Task{task1, task2}
				return state
			}(),
			wantMsg: "No claimable tasks; 2 blocked by dependencies",
		},
		{
			name: "tasks in progress",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				state.Tasks = []models.Task{
					testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now),
					testhelpers.BuildTaskByStatus("task-2", models.TaskStatusReadyForReview, now),
				}
				return state
			}(),
			wantMsg: "No claimable tasks; 2 in progress",
		},
		{
			name: "claimable tasks available",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				state.Tasks = []models.Task{
					testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now),
					testhelpers.BuildTaskByStatus("task-2", models.TaskStatusRejected, now),
				}
				return state
			}(),
			wantMsg: "Found 2 claimable task(s)",
		},
		{
			name: "mixed: blocked and in progress",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				task1 := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now)
				task1.DependsOn = []string{"task-0"}
				task2 := testhelpers.BuildTaskByStatus("task-2", models.TaskStatusImplementing, now)
				state.Tasks = []models.Task{task1, task2}
				return state
			}(),
			wantMsg: "No claimable tasks; 1 blocked by dependencies; 1 in progress",
		},
		{
			name: "claimable with integration failed",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				state.Tasks = []models.Task{
					testhelpers.BuildTaskByStatus("task-1", models.TaskStatusIntegrationFailed, now),
				}
				return state
			}(),
			wantMsg: "Found 1 claimable task(s)",
		},
		{
			name: "mixed: some claimable, some blocked",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				task1 := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now)
				task2 := testhelpers.BuildTaskByStatus("task-2", models.TaskStatusReady, now)
				task2.DependsOn = []string{"task-0"}
				state.Tasks = []models.Task{task1, task2}
				return state
			}(),
			wantMsg: "Found 1 claimable task(s)",
		},
	}

	pr := testPipelineResolver(t)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := models.GetCoderWorkDiagnostics(tt.state, pr)
			if got != tt.wantMsg {
				t.Errorf("GetCoderWorkDiagnostics() = %q, want %q", got, tt.wantMsg)
			}
		})
	}
}

func TestGetReviewerWorkDiagnostics(t *testing.T) {
	now := time.Now().UTC()

	tests := []struct {
		name    string
		state   *models.State
		wantMsg string
	}{
		{
			name: "no reviewable tasks",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				state.Tasks = []models.Task{
					testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now),
				}
				return state
			}(),
			wantMsg: "No reviewable tasks",
		},
		{
			name: "unassigned reviewable tasks",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				state.Tasks = []models.Task{
					testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReadyForReview, now),
					testhelpers.BuildTaskByStatus("task-2", models.TaskStatusReadyForReview, now),
				}
				return state
			}(),
			wantMsg: "Found 2 reviewable task(s)",
		},
		{
			name: "tasks with expired leases only",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				task1 := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReviewing, now)
				expiredTime := now.Add(-1 * time.Hour)
				task1.ReviewLeaseExpires = &expiredTime
				state.Tasks = []models.Task{task1}
				return state
			}(),
			wantMsg: "No reviewable tasks; 1 with stale leases (pending reclamation)",
		},
		{
			name: "mixed unassigned and expired leases",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				task1 := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReadyForReview, now)
				task2 := testhelpers.BuildTaskByStatus("task-2", models.TaskStatusReviewing, now)
				expiredTime := now.Add(-1 * time.Hour)
				task2.ReviewLeaseExpires = &expiredTime
				state.Tasks = []models.Task{task1, task2}
				return state
			}(),
			wantMsg: "Found 1 reviewable task(s); 1 with stale leases (pending reclamation)",
		},
		{
			name: "actively being reviewed",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				task1 := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReviewing, now)
				state.Tasks = []models.Task{task1}
				return state
			}(),
			wantMsg: "No reviewable tasks; 1 actively being reviewed",
		},
		{
			name: "multiple actively being reviewed",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				task1 := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReviewing, now)
				task2 := testhelpers.BuildTaskByStatus("task-2", models.TaskStatusReviewing, now)
				state.Tasks = []models.Task{task1, task2}
				return state
			}(),
			wantMsg: "No reviewable tasks; 2 actively being reviewed",
		},
		{
			name: "no ready_for_review tasks at all",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				state.Tasks = []models.Task{
					testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now),
					testhelpers.BuildTaskByStatus("task-2", models.TaskStatusImplementing, now),
					testhelpers.BuildTaskByStatus("task-3", models.TaskStatusMerged, now),
				}
				return state
			}(),
			wantMsg: "No reviewable tasks",
		},
		{
			name: "empty task list",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				state.Tasks = []models.Task{}
				return state
			}(),
			wantMsg: "No reviewable tasks",
		},
	}

	pr := testPipelineResolver(t)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := models.GetReviewerWorkDiagnostics(tt.state, pr)
			if got != tt.wantMsg {
				t.Errorf("GetReviewerWorkDiagnostics() = %q, want %q", got, tt.wantMsg)
			}
		})
	}
}

func TestDetectOrchestratorWakeTriggers_CodingComplete(t *testing.T) {
	now := time.Now().UTC()
	sha := "abc123"

	state := testhelpers.CreateValidState()
	state.Goal.BaseCommit = &sha
	state.Sprint.Scope.Planned = []string{"task-1", "task-2"}
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusMerged, now),
		testhelpers.BuildTaskByStatus("task-2", models.TaskStatusMerged, now),
	}

	result := DetectOrchestratorWakeTriggers(state, nil, nil, nil)
	if result.Trigger != WakeTriggerCodingComplete {
		t.Errorf("trigger = %v, want %v", result.Trigger, WakeTriggerCodingComplete)
	}
	if result.Count != 1 {
		t.Errorf("count = %d, want 1", result.Count)
	}
}

func TestDetectOrchestratorWakeTriggers_CodingComplete_NoBaseCommit(t *testing.T) {
	now := time.Now().UTC()

	state := testhelpers.CreateValidState()
	// goal.BaseCommit is nil (no coding happened)
	state.Sprint.Scope.Planned = []string{"task-1"}
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusMerged, now),
	}

	result := DetectOrchestratorWakeTriggers(state, nil, nil, nil)
	if result.Trigger != WakeTriggerSprintComplete {
		t.Errorf("trigger = %v, want %v", result.Trigger, WakeTriggerSprintComplete)
	}
}

func TestDetectOrchestratorWakeTriggers_IntegrationExists(t *testing.T) {
	now := time.Now().UTC()
	sha := "abc123"

	state := testhelpers.CreateValidState()
	state.Goal.BaseCommit = &sha
	// integration-pair task exists (also terminal)
	integrationTask := testhelpers.BuildTaskByStatus("integration-1", models.TaskStatusMerged, now)
	integrationTask.RolePair = "integration-pair"
	state.Sprint.Scope.Planned = []string{"task-1", "integration-1"}
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusMerged, now),
		integrationTask,
	}

	result := DetectOrchestratorWakeTriggers(state, nil, nil, nil)
	if result.Trigger != WakeTriggerSprintComplete {
		t.Errorf("trigger = %v, want %v", result.Trigger, WakeTriggerSprintComplete)
	}
}

func TestDetectOrchestratorWakeTriggersWithIntegrationProjection_Outcomes(t *testing.T) {
	tests := []struct {
		name       string
		decision   ops.IntegrationProgressDecision
		evaluation error
		want       OrchestratorWakeTrigger
		shouldWake bool
	}{
		{name: "complete", decision: ops.IntegrationProgressDecision{IntegrationComplete: true}, want: WakeTriggerSprintComplete, shouldWake: true},
		{name: "waiting", decision: ops.IntegrationProgressDecision{Waiting: &ops.IntegrationProgressReason{Code: "integration_repairs_pending"}}, want: WakeTriggerIntegrationWaiting},
		{name: "blocked", decision: ops.IntegrationProgressDecision{Blocked: &ops.IntegrationProgressReason{Code: "integration_analysis_blocked"}}, want: WakeTriggerIntegrationBlocked},
		{name: "exhausted", decision: ops.IntegrationProgressDecision{Exhausted: true, Blocked: &ops.IntegrationProgressReason{Code: "global_generations_exhausted"}}, want: WakeTriggerIntegrationExhausted},
		{name: "unavailable", evaluation: errors.New("read integration HEAD: missing ref"), want: WakeTriggerIntegrationUnavailable},
		{name: "reconciliation needed", decision: ops.IntegrationProgressDecision{FreezeContributingSet: true, GlobalRequest: &ops.IntegrationAnalysisRequest{Key: "global:2"}}, want: WakeTriggerCodingComplete, shouldWake: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := testhelpers.CreateValidState()
			baseCommit := "base-commit"
			state.Goal.BaseCommit = &baseCommit
			state.Sprint.Scope.Planned = []string{"task-1"}
			state.Tasks = []models.Task{testhelpers.BuildTaskByStatus("task-1", models.TaskStatusMerged, time.Now().UTC())}
			projection := prompts.ProjectEffectiveIntegrationCompletion(tt.decision, nil, tt.evaluation)

			result := DetectOrchestratorWakeTriggersWithIntegrationProjection(state, nil, nil, nil, projection)
			if result.Trigger != tt.want {
				t.Fatalf("trigger = %q, want %q (projection %#v)", result.Trigger, tt.want, projection)
			}
			if result.ShouldWake() != tt.shouldWake {
				t.Fatalf("ShouldWake() = %t, want %t", result.ShouldWake(), tt.shouldWake)
			}
			if result.Integration.Status != projection.Status || result.Integration.ReasonCode != projection.ReasonCode {
				t.Fatalf("integration = %#v, want projection %#v", result.Integration, projection)
			}
		})
	}
}

func TestDetectOrchestratorWakeTriggers_PlanningCompletePrecedence(t *testing.T) {
	now := time.Now().UTC()
	sha := "abc123"

	state := testhelpers.CreateValidState()
	state.Goal.BaseCommit = &sha
	// Merged planning task with output[] — PlanningComplete should take priority
	planningTask := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusMerged, now)
	planningTask.RolePair = "code-planning-pair"
	planningTask.Output = []models.OutputEntry{
		{Desc: "implement X", DoneWhen: "tests pass", Scope: "pkg/x"},
	}
	state.Sprint.Scope.Planned = []string{"task-1"}
	state.Tasks = []models.Task{planningTask}

	result := DetectOrchestratorWakeTriggers(state, nil, nil, nil)
	if result.Trigger != WakeTriggerPlanningComplete {
		t.Errorf("trigger = %v, want %v", result.Trigger, WakeTriggerPlanningComplete)
	}
}

func TestDetectOrchestratorWakeTriggers_ManyToOneReady(t *testing.T) {
	now := time.Now().UTC()

	m2oTransitions := []ops.ManyToOneTransitionInfo{
		{Name: "us-to-coding", SourceRolePair: "us-writing-pair"},
	}

	// Helper to build a MERGED us-writing-pair task with shared parent.
	buildUSTask := func(id, parentID string) models.Task {
		task := testhelpers.BuildTaskByStatus(id, models.TaskStatusMerged, now)
		task.RolePair = "us-writing-pair"
		task.ParentTask = &parentID
		return task
	}

	t.Run("complete cohort triggers ManyToOneReady", func(t *testing.T) {
		state := testhelpers.CreateValidState()
		us1 := buildUSTask("us-1", "epic-1")
		us2 := buildUSTask("us-2", "epic-1")
		us3 := buildUSTask("us-3", "epic-1")
		state.Tasks = []models.Task{us1, us2, us3}
		state.Sprint.Scope.Planned = []string{"us-1", "us-2", "us-3"}

		result := DetectOrchestratorWakeTriggers(state, []models.TaskStatus{models.TaskStatusMerged}, nil, m2oTransitions)
		if result.Trigger != WakeTriggerManyToOneReady {
			t.Errorf("trigger = %v, want %v", result.Trigger, WakeTriggerManyToOneReady)
		}
		if result.Count != 1 {
			t.Errorf("count = %d, want 1", result.Count)
		}
	})

	t.Run("incomplete cohort does not trigger", func(t *testing.T) {
		state := testhelpers.CreateValidState()
		us1 := buildUSTask("us-1", "epic-1")
		us2 := buildUSTask("us-2", "epic-1")
		// us3 is not MERGED — still in progress
		us3 := testhelpers.BuildTaskByStatus("us-3", models.TaskStatusImplementing, now)
		us3.RolePair = "us-writing-pair"
		parentID := "epic-1"
		us3.ParentTask = &parentID
		state.Tasks = []models.Task{us1, us2, us3}
		state.Sprint.Scope.Planned = []string{"us-1", "us-2", "us-3"}

		result := DetectOrchestratorWakeTriggers(state, nil, nil, m2oTransitions)
		if result.Trigger == WakeTriggerManyToOneReady {
			t.Errorf("trigger = %v, should not be ManyToOneReady for incomplete cohort", result.Trigger)
		}
	})

	t.Run("already-executed cohort does not trigger", func(t *testing.T) {
		state := testhelpers.CreateValidState()
		us1 := buildUSTask("us-1", "epic-1")
		us1.TransitionsExecuted = map[string]bool{"us-to-coding": true}
		us2 := buildUSTask("us-2", "epic-1")
		us2.TransitionsExecuted = map[string]bool{"us-to-coding": true}
		us3 := buildUSTask("us-3", "epic-1")
		us3.TransitionsExecuted = map[string]bool{"us-to-coding": true}
		state.Tasks = []models.Task{us1, us2, us3}
		state.Sprint.Scope.Planned = []string{"us-1", "us-2", "us-3"}

		result := DetectOrchestratorWakeTriggers(state, []models.TaskStatus{models.TaskStatusMerged}, nil, m2oTransitions)
		if result.Trigger == WakeTriggerManyToOneReady {
			t.Errorf("trigger = %v, should not be ManyToOneReady for already-executed cohort", result.Trigger)
		}
	})
}

func TestCountReadyManyToOneCohorts_TwoCohorts(t *testing.T) {
	now := time.Now().UTC()

	m2oTransitions := []ops.ManyToOneTransitionInfo{
		{Name: "us-to-coding", SourceRolePair: "us-writing-pair"},
	}

	buildUSTask := func(id, parentID string) models.Task {
		task := testhelpers.BuildTaskByStatus(id, models.TaskStatusMerged, now)
		task.RolePair = "us-writing-pair"
		task.ParentTask = &parentID
		return task
	}

	state := testhelpers.CreateValidState()
	// Cohort 1: epic-1
	us1 := buildUSTask("us-1", "epic-1")
	us2 := buildUSTask("us-2", "epic-1")
	// Cohort 2: epic-2
	us3 := buildUSTask("us-3", "epic-2")
	us4 := buildUSTask("us-4", "epic-2")
	state.Tasks = []models.Task{us1, us2, us3, us4}
	state.Sprint.Scope.Planned = []string{"us-1", "us-2", "us-3", "us-4"}

	count := countReadyManyToOneCohorts(state, m2oTransitions)
	if count != 2 {
		t.Errorf("countReadyManyToOneCohorts() = %d, want 2", count)
	}
}
