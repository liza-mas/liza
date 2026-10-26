package commands

import (
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
)

// statusData contains all status information
type statusData struct {
	Goal               goalStatus                    `json:"goal"`
	Sprint             sprintStatus                  `json:"sprint"`
	Config             configStatus                  `json:"config"`
	Tasks              taskStatus                    `json:"tasks"`
	Agents             []agentStatus                 `json:"agents"`
	AgentHealth        map[string]models.AgentHealth `json:"agent_health,omitempty"`
	AgentCapacity      agentCapacityStatus           `json:"agent_capacity" yaml:"agent_capacity"`
	OrchestratorState  orchestratorStatus            `json:"orchestrator_state"`
	Integration        *integrationLifecycleStatus   `json:"integration,omitempty" yaml:"integration,omitempty"`
	WorkQueues         workQueuesStatus              `json:"work_queues"`
	PendingTransitions []pendingTransition           `json:"pending_transitions,omitempty"`
	PhaseHandoff       *phaseHandoffStatus           `json:"phase_handoff,omitempty" yaml:"phase_handoff,omitempty"`
	Anomalies          *[]string                     `json:"anomalies,omitempty"`
	CircuitBreaker     *circuitBreakerStatus         `json:"circuit_breaker,omitempty"`
}

type pendingTransition struct {
	TaskID      string   `json:"task_id"`
	Transitions []string `json:"transitions"`
}

type phaseHandoffStatus struct {
	State               phaseHandoffState        `json:"state" yaml:"state"`
	Explanation         string                   `json:"explanation" yaml:"explanation"`
	ReadyPlanningTasks  []string                 `json:"ready_planning_tasks" yaml:"ready_planning_tasks"`
	FailedPlanningTasks []ops.PlanHandoffFailure `json:"failed_planning_tasks,omitempty" yaml:"failed_planning_tasks,omitempty"`
	// UndecidedPlanningTasks are plans an orchestrator turn left without a
	// disposition; they wait for new input (D-49).
	UndecidedPlanningTasks []ops.PlanHandoffUndecided `json:"undecided_planning_tasks,omitempty" yaml:"undecided_planning_tasks,omitempty"`
	MergeRequired          []phaseMergeRequired       `json:"merge_required,omitempty" yaml:"merge_required,omitempty"`
	BlockingTasks          []phaseHandoffTask         `json:"blocking_tasks,omitempty" yaml:"blocking_tasks,omitempty"`
	StaleAssignedAgents    []phaseHandoffTask         `json:"stale_assigned_agents,omitempty" yaml:"stale_assigned_agents,omitempty"`
}

type phaseMergeRequired struct {
	TaskID string `json:"task_id" yaml:"task_id"`
	Action string `json:"action" yaml:"action"`
}

type phaseHandoffTask struct {
	ID                  string `json:"id"`
	Status              string `json:"status"`
	RolePair            string `json:"role_pair,omitempty"`
	AssignedTo          string `json:"assigned_to,omitempty"`
	AgentStatus         string `json:"agent_status,omitempty"`
	AgentProcessStatus  string `json:"agent_process_status,omitempty"`
	ProcessStatusSource string `json:"process_status_source,omitempty"`
	ProcessStatusDetail string `json:"process_status_detail,omitempty"`
	LeaseExpires        string `json:"lease_expires,omitempty"`
}

type goalStatus struct {
	Description string `json:"description"`
	Status      string `json:"status"`
	SpecRef     string `json:"spec_ref"`
}

type sprintStatus struct {
	ID                string `json:"id"`
	Number            int    `json:"number"`
	Status            string `json:"status"`
	CheckpointTrigger string `json:"checkpoint_trigger,omitempty"`
	CheckpointNotice  string `json:"checkpoint_notice,omitempty"`
	StartTime         string `json:"start_time"`
	TasksDone         int    `json:"tasks_done"`
	TasksTotal        int    `json:"tasks_total"`
}

type configStatus struct {
	Mode        string  `json:"mode"`
	PausedBy    *string `json:"paused_by,omitempty"`
	PauseReason *string `json:"pause_reason,omitempty"`
}

type taskStatus struct {
	Total                        int                        `json:"total"`
	Active                       int                        `json:"active"`
	Terminal                     int                        `json:"terminal"`
	ByStatus                     map[string]int             `json:"by_status"`
	Claimable                    int                        `json:"claimable"`
	Reviewable                   int                        `json:"reviewable"`
	ClaimableByRole              []models.RoleTaskReadiness `json:"claimable_by_role" yaml:"claimable_by_role"`
	ReviewableByRole             []models.RoleTaskReadiness `json:"reviewable_by_role" yaml:"reviewable_by_role"`
	LegacyCoderClaimable         int                        `json:"legacy_coder_claimable" yaml:"legacy_coder_claimable"`
	LegacyCodeReviewerReviewable int                        `json:"legacy_code_reviewer_reviewable" yaml:"legacy_code_reviewer_reviewable"`
	Blocked                      int                        `json:"blocked"`
	BlockedByDeps                int                        `json:"blocked_by_deps"`
}

type agentCapacityStatus struct {
	Live     int                 `json:"live"`
	Free     int                 `json:"free"`
	Degraded int                 `json:"degraded"`
	ByRole   []roleAgentCapacity `json:"by_role" yaml:"by_role"`
}

type roleAgentCapacity struct {
	Role     string `json:"role" yaml:"role"`
	Live     int    `json:"live" yaml:"live"`
	Free     int    `json:"free" yaml:"free"`
	Degraded int    `json:"degraded" yaml:"degraded"`
}

type agentStatus struct {
	ID                  string `json:"id"`
	Role                string `json:"role"`
	Status              string `json:"status"`
	Health              string `json:"health,omitempty"`
	HealthReason        string `json:"health_reason,omitempty"`
	RecoverHint         string `json:"recover_hint,omitempty"`
	PID                 int    `json:"pid"`
	CurrentTask         string `json:"current_task"`
	TimeSinceHeartbeat  string `json:"time_since_heartbeat"`
	ProcessStatus       string `json:"process_status"`
	ProcessStatusSource string `json:"process_status_source"`
	ProcessStatusDetail string `json:"process_status_detail"`
}

type orchestratorStatus struct {
	Trigger      string                      `json:"trigger"`
	TriggerCount int                         `json:"trigger_count"`
	Reason       string                      `json:"reason"`
	Integration  *integrationLifecycleStatus `json:"integration,omitempty" yaml:"integration,omitempty"`
}

type integrationLifecycleStatus struct {
	Status         string   `json:"status" yaml:"status"`
	ReasonCode     string   `json:"reason_code,omitempty" yaml:"reason_code,omitempty"`
	TaskIDs        []string `json:"task_ids,omitempty" yaml:"task_ids,omitempty"`
	RequestKeys    []string `json:"request_keys,omitempty" yaml:"request_keys,omitempty"`
	CreatedTaskIDs []string `json:"created_task_ids,omitempty" yaml:"created_task_ids,omitempty"`
	Guidance       string   `json:"guidance,omitempty" yaml:"guidance,omitempty"`
}

type workQueuesStatus struct {
	Coder    queueStatus `json:"coder"`
	Reviewer queueStatus `json:"reviewer"`
}

type queueStatus struct {
	Available int    `json:"available"`
	Reason    string `json:"reason"`
}

type circuitBreakerStatus struct {
	Status   string   `json:"status"`
	Triggers []string `json:"triggers,omitempty"`
}
