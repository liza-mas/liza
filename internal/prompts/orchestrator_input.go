package prompts

import (
	"slices"
	"strings"

	"github.com/liza-mas/liza/internal/models"
)

// WakeTrigger names the situation one orchestrator wake instruction answers.
// The values are rendered verbatim; the runtime converts its own trigger type
// at the rendering boundary.
type WakeTrigger string

// Wake triggers, including the integration outcomes that replace a completion
// wake and the two values no template renders (UNKNOWN, NONE).
const (
	WakeInitialPlanning        WakeTrigger = "INITIAL_PLANNING"
	WakeBlockedTasks           WakeTrigger = "BLOCKED_TASKS"
	WakeHypothesisExhausted    WakeTrigger = "HYPOTHESIS_EXHAUSTED"
	WakeImmediateDiscovery     WakeTrigger = "IMMEDIATE_DISCOVERY"
	WakeHumanNote              WakeTrigger = "HUMAN_NOTE"
	WakePlanningComplete       WakeTrigger = "PLANNING_COMPLETE"
	WakeManyToOneReady         WakeTrigger = "MANY_TO_ONE_READY"
	WakeCodingComplete         WakeTrigger = "CODING_COMPLETE"
	WakeSprintComplete         WakeTrigger = "SPRINT_COMPLETE"
	WakeIntegrationWaiting     WakeTrigger = "INTEGRATION_WAITING"
	WakeIntegrationBlocked     WakeTrigger = "INTEGRATION_BLOCKED"
	WakeIntegrationExhausted   WakeTrigger = "INTEGRATION_EXHAUSTED"
	WakeIntegrationUnavailable WakeTrigger = "INTEGRATION_UNAVAILABLE"
	WakeUnknown                WakeTrigger = "UNKNOWN"
	WakeNone                   WakeTrigger = "NONE"
)

// IntegrationViewStatus is the status word of the integration completion view
// shown to the orchestrator and to status output.
type IntegrationViewStatus string

// Integration view statuses.
const (
	IntegrationViewUnavailable          IntegrationViewStatus = "unavailable"
	IntegrationViewComplete             IntegrationViewStatus = "complete"
	IntegrationViewBlocked              IntegrationViewStatus = "blocked"
	IntegrationViewExhausted            IntegrationViewStatus = "exhausted"
	IntegrationViewReconciliationNeeded IntegrationViewStatus = "reconciliation_needed"
	IntegrationViewWaiting              IntegrationViewStatus = "waiting"
)

// Reason codes the integration view itself assigns, as opposed to codes it
// forwards from the authoritative integration progress.
const (
	ReasonIntegrationProgressUnavailable = "integration_progress_unavailable"
	ReasonIntegrationProgressIncomplete  = "integration_progress_incomplete"
)

const (
	// activeTaskDigestLimit caps the active tasks the dashboard lists.
	activeTaskDigestLimit = 12
	// blockerDigestLimit truncates one blocker reason in the digest.
	blockerDigestLimit = 220
	// hypothesisExhaustionFailures is how many failed reviews exhaust a task's
	// hypothesis in the dashboard's raw count.
	hypothesisExhaustionFailures = 2
	// integrationRolePair is the role-pair of the global integration task.
	integrationRolePair = "integration-pair"
)

// PlanningTask is a merged planning task awaiting hand-off, as the
// PLANNING_COMPLETE wake lists it.
type PlanningTask struct {
	TaskID           string
	Output           []models.OutputEntry
	Class            models.PlanHandoffClass
	Blocker          string
	PlanRefs         []string // distinct plan files named by output[].plan_ref
	CorrectionID     string
	CorrectionStatus models.TaskStatus
}

// DistinctPlanFiles returns the plan files output entries cite, in order and
// without anchors or repeats.
func DistinctPlanFiles(entries []models.OutputEntry) []string {
	var files []string
	for _, entry := range entries {
		file, _, _ := strings.Cut(entry.PlanRef, "#")
		if file != "" && !slices.Contains(files, file) {
			files = append(files, file)
		}
	}
	return files
}

// WakeEntryPoint describes one available entry-point for the INITIAL_PLANNING
// wake.
type WakeEntryPoint struct {
	Name               string // e.g., "general-objective"
	SimpleRolePair     string // e.g., "epic-planning-pair"
	SimpleDisplayName  string // simple target doer's display name, e.g., "Epic Planner"
	SimpleTaskType     string // e.g., "epic-planning", "architecture"
	SimpleTaskIDPrefix string // configured task ID prefix, e.g., "ep"
	HasFanOutTarget    bool
	FanOutRolePair     string // e.g., "epic-planning-main-pair"
	FanOutDisplayName  string // fan-out target doer's display name
	FanOutTaskType     string
	FanOutTaskIDPrefix string
}

// WakeRoutes is the entry-point data the runtime resolves from the frozen
// pipeline: every available entry-point in name order, and the one the goal
// named, if any.
type WakeRoutes struct {
	EntryPoints []WakeEntryPoint
	Resolved    *WakeEntryPoint
}

// OrchestratorDashboardInput is everything one orchestrator render reads that
// is not a formatting-only function of the state snapshot. The runtime
// resolves it before rendering; prompts never reaches back for it.
type OrchestratorDashboardInput struct {
	State   *models.State
	AgentID string

	// WakeOpen reports whether the sprint still accepts wakes: neither at a
	// checkpoint nor completed.
	WakeOpen bool
	// SprintTerminals are the pipeline's sprint-terminal states; nil when the
	// detection context could not be loaded.
	SprintTerminals []models.TaskStatus
	// CycleBlocked counts merged planning tasks held by a transition cycle.
	CycleBlocked int
	// PlanningTasks are the planned merged tasks awaiting hand-off, in planned
	// order. Empty while the sprint is not open for wakes.
	PlanningTasks []PlanningTask
	// ReadyManyToOne counts many-to-one cohorts ready to transition; zero
	// while the sprint is not open for wakes.
	ReadyManyToOne int
	// HeldPlan reports a planned task held for a human action.
	HeldPlan bool
	// BlockedAwaitPlanningHandoff reports blocked tasks that only wait for a
	// planning hand-off; it ranks PLANNING_COMPLETE above BLOCKED_TASKS.
	BlockedAwaitPlanningHandoff bool
	// ActionableBlockedIDs are the BLOCKED tasks the wake must assess, in state
	// order. The displayed Blocked count stays raw.
	ActionableBlockedIDs []string
	// ActionableHypothesisExhausted counts the exhausted tasks worth a wake.
	// The displayed Hypothesis exhausted count stays raw.
	ActionableHypothesisExhausted int
	UnseenNotes                   []models.HumanNote
	// TestFilePatterns is the submission test-file matcher display text.
	TestFilePatterns string
	Routes           WakeRoutes
}

// OrchestratorWakeDecision carries the supervisor's revalidated invocation.
// Both the dashboard and instructions must describe this same decision.
type OrchestratorWakeDecision struct {
	Trigger     WakeTrigger
	Integration EffectiveIntegrationCompletion
}

// SelectOrchestratorWake picks the standalone wake for the resolved input,
// before any integration outcome replaces a completion wake. A held plan keeps
// the sprint open, and blocked tasks waiting only for a planning hand-off take
// BLOCKED_TASKS' rank for PLANNING_COMPLETE.
func SelectOrchestratorWake(in *OrchestratorDashboardInput) WakeTrigger {
	state := in.State
	var trigger WakeTrigger
	if len(in.PlanningTasks) > 0 && in.BlockedAwaitPlanningHandoff {
		trigger = WakePlanningComplete
	} else {
		sprintComplete := state.AllPlannedTasksTerminalWith(in.SprintTerminals) && in.WakeOpen && !in.HeldPlan
		codingComplete := state.Goal.BaseCommit != nil && !hasIntegrationTaskInSprint(state)
		trigger = determineWakeTrigger(len(state.Tasks), len(in.ActionableBlockedIDs), in.ActionableHypothesisExhausted,
			countImmediateDiscoveries(state.Discovered), len(in.UnseenNotes), sprintComplete, codingComplete,
			in.PlanningTasks, in.ReadyManyToOne)
	}
	if !in.WakeOpen && trigger == WakeUnknown {
		return WakeNone
	}
	return trigger
}

// wakeTemplateData flattens the resolved routes and the integration view into
// the data the wake templates read.
func (in *OrchestratorDashboardInput) wakeTemplateData(integration EffectiveIntegrationCompletion) wakeTemplateData {
	data := wakeTemplateData{
		GoalSpecRef:      in.State.Goal.SpecRef,
		GoalEntryPoint:   in.State.Goal.EntryPoint,
		EntryPoints:      in.Routes.EntryPoints,
		Integration:      integration,
		TestFilePatterns: in.TestFilePatterns,
	}
	if ep := in.Routes.Resolved; ep != nil {
		data.ResolvedEntryPoint = *ep
		data.ResolvedRolePair = ep.SimpleRolePair
		data.ResolvedDisplayName = ep.SimpleDisplayName
		data.ResolvedTaskIDPrefix = ep.SimpleTaskIDPrefix
		data.ResolvedTaskType = ep.SimpleTaskType
		data.ResolvedHasFanOutTarget = ep.HasFanOutTarget
		data.ResolvedFanOutRolePair = ep.FanOutRolePair
		data.ResolvedFanOutDisplayName = ep.FanOutDisplayName
		data.ResolvedFanOutTaskIDPrefix = ep.FanOutTaskIDPrefix
		data.ResolvedFanOutTaskType = ep.FanOutTaskType
	}
	return data
}
