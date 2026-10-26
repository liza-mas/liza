package commands

import (
	"fmt"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
)

// plannedHandoffScan is what one pass over the sprint's planned tasks finds:
// which planning hand-offs are ready, failed or undecided, which planned tasks
// are still active, and which of those hold a stale agent assignment.
type plannedHandoffScan struct {
	ready     []string
	failed    []ops.PlanHandoffFailure
	undecided []ops.PlanHandoffUndecided
	blockers  []phaseHandoffTask
	stale     []phaseHandoffTask
}

func buildPhaseHandoffStatus(state *models.State, projectRoot string) *phaseHandoffStatus {
	detCtx, err := ops.LoadPhaseHandoffDetectionContext(projectRoot)
	if err != nil {
		return nil
	}

	approvedUnmerged := ops.ApprovedPlanningTasksWithUnmergedOutput(
		state,
		detCtx.PlanningPairs,
		detCtx.PlanningApprovedStatuses,
	)
	scan := scanPlannedHandoffs(state, detCtx)

	if state.Sprint.Status == models.SprintStatusCompleted && len(approvedUnmerged) > 0 {
		return mergeRequiredHandoff(scan, approvedUnmerged)
	}
	if len(scan.ready) == 0 && len(scan.failed) == 0 && len(scan.undecided) == 0 {
		return nil
	}
	return classifyPhaseHandoff(state, scan)
}

// scanPlannedHandoffs walks the sprint's planned tasks in order, collecting
// hand-off readiness, failures and the non-terminal tasks that block advance.
func scanPlannedHandoffs(state *models.State, detCtx *ops.PipelineDetectionContext) plannedHandoffScan {
	var scan plannedHandoffScan
	seenStale := make(map[string]bool)

	for _, taskID := range state.Sprint.Scope.Planned {
		task := state.FindTask(taskID)
		if task == nil {
			scan.blockers = append(scan.blockers, phaseHandoffTask{ID: taskID, Status: phaseHandoffMissingTaskStatus})
			continue
		}

		scan.failed = append(scan.failed, detCtx.PlanHandoff.Failures(state, task)...)
		if observation := detCtx.PlanHandoff.UndecidedHandoff(state, task); observation != nil {
			scan.undecided = append(scan.undecided, *observation)
		}
		// A refused stale-provider plan is eligible only for its replan
		// (ADR-0189): repair work, not ready work.
		if detCtx.PlanHandoff.PlanningCompleteEligible(state, task) && detCtx.PlanHandoff.HasUnfailedHandoff(state, task) {
			scan.ready = append(scan.ready, task.ID)
		}

		if isSprintTerminal(task, detCtx.SprintTerminals) {
			continue
		}

		blocker := newPhaseHandoffBlocker(state, task)
		if task.AssignedTo != nil && isStaleAssignment(state, task, blocker) && !seenStale[*task.AssignedTo] {
			scan.stale = append(scan.stale, blocker)
			seenStale[*task.AssignedTo] = true
		}
		scan.blockers = append(scan.blockers, blocker)
	}
	return scan
}

// newPhaseHandoffBlocker describes a non-terminal planned task with its
// assignee's lease and process status.
func newPhaseHandoffBlocker(state *models.State, task *models.Task) phaseHandoffTask {
	blocker := phaseHandoffTask{
		ID:       task.ID,
		Status:   string(task.Status),
		RolePair: task.RolePair,
	}
	if task.AssignedTo == nil {
		return blocker
	}
	blocker.AssignedTo = *task.AssignedTo
	if task.LeaseExpires != nil {
		blocker.LeaseExpires = task.LeaseExpires.Format(time.RFC3339)
	}
	if assignedAgent, ok := state.Agents[*task.AssignedTo]; ok {
		blocker.AgentStatus = string(assignedAgent.Status)
		processInfo := getAgentProcessStatusInfo(*task.AssignedTo, assignedAgent)
		blocker.AgentProcessStatus = processInfo.Status
		blocker.ProcessStatusSource = processInfo.Source
		blocker.ProcessStatusDetail = processInfo.Detail
	}
	return blocker
}

// isStaleAssignment reports whether the task's assigned agent claims it as
// current work while its process is not running.
func isStaleAssignment(state *models.State, task *models.Task, blocker phaseHandoffTask) bool {
	assignedAgent, ok := state.Agents[*task.AssignedTo]
	return ok &&
		assignedAgent.CurrentTask != nil &&
		*assignedAgent.CurrentTask == task.ID &&
		assignedAgent.Status != models.AgentStatusIdle &&
		blocker.AgentProcessStatus != processStatusRunning
}

// mergeRequiredHandoff reports a completed sprint whose approved planning
// tasks must be merged before it can advance.
func mergeRequiredHandoff(scan plannedHandoffScan, approvedUnmerged []string) *phaseHandoffStatus {
	mergeRequired := make([]phaseMergeRequired, 0, len(approvedUnmerged))
	for _, taskID := range approvedUnmerged {
		mergeRequired = append(mergeRequired, phaseMergeRequired{
			TaskID: taskID,
			Action: brand.Command("wt-merge", taskID),
		})
	}
	return &phaseHandoffStatus{
		State:               phaseHandoffMergeRequired,
		Explanation:         fmt.Sprintf("%d approved planning task(s) must be merged before the completed sprint can advance.", len(mergeRequired)),
		ReadyPlanningTasks:  scan.ready,
		FailedPlanningTasks: scan.failed,
		MergeRequired:       mergeRequired,
		BlockingTasks:       scan.blockers,
		StaleAssignedAgents: scan.stale,
	}
}

// classifyPhaseHandoff picks the hand-off state and explanation from the scan.
// Later conditions override earlier ones: sprint status, then undecided plans,
// then failed hand-offs.
func classifyPhaseHandoff(state *models.State, scan plannedHandoffScan) *phaseHandoffStatus {
	ready, failed, undecided, blockers := len(scan.ready), len(scan.failed), len(scan.undecided), len(scan.blockers)

	stateName := phaseHandoffReady
	explanation := fmt.Sprintf("%d merged planning task(s) have unconsumed output and are ready for transition execution.", ready)
	if blockers > 0 {
		stateName = phaseHandoffPartialReady
		explanation = fmt.Sprintf("%d merged planning task(s) have unconsumed output; %d non-terminal planned task(s) are still active. %s can checkpoint PLANNING_COMPLETE and create implementation tasks after resume without waiting for the active tasks to finish.", ready, blockers, brand.NameTitle)
	}
	if state.Sprint.Status == models.SprintStatusCheckpoint {
		stateName = phaseHandoffCheckpointed
		explanation = fmt.Sprintf("%d merged planning task(s) are waiting behind a checkpoint; resume the sprint to execute their pipeline transitions.", ready)
	}
	if state.Sprint.Status == models.SprintStatusCompleted {
		stateName = phaseHandoffCompleted
		explanation = fmt.Sprintf("%d merged planning task(s) are waiting in a completed sprint; resume/advance to execute their pipeline transitions.", ready)
	}
	if undecided > 0 {
		stateName = phaseHandoffDispositionRequired
		explanation = fmt.Sprintf("%d plan(s) were left undecided by the orchestrator and will not re-wake it until their inputs change; ask for a disposition with %s or expand them with %s. %d other plan(s) are ready.",
			undecided, brand.Command("add-human-note", "<task-id>", "--note-file", "<path>"), brand.Command("proceed", "<task-id>", "<transition>"), ready)
	}
	if failed > 0 {
		stateName = phaseHandoffRepairRequired
		explanation = fmt.Sprintf("%d planning hand-off(s) failed with unchanged inputs; repair inputs or explicitly retire unused hand-offs by a merged correction. %d plan(s) await a disposition; %d other plan(s) are ready.", failed, undecided, ready)
	}

	return &phaseHandoffStatus{
		State:                  stateName,
		Explanation:            explanation,
		ReadyPlanningTasks:     scan.ready,
		FailedPlanningTasks:    scan.failed,
		UndecidedPlanningTasks: scan.undecided,
		BlockingTasks:          scan.blockers,
		StaleAssignedAgents:    scan.stale,
	}
}

func isSprintTerminal(task *models.Task, pipelineTerminals []models.TaskStatus) bool {
	if task == nil {
		return false
	}
	if task.RolePair != "" {
		return task.Status.IsPipelineSprintTerminal(pipelineTerminals)
	}
	return task.Status.IsSprintTerminal()
}
