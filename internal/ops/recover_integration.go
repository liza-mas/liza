package ops

import (
	"fmt"
	"io"
	"reflect"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/secretmask"
	"github.com/liza-mas/liza/internal/statevalidate"
)

// RecoverIntegrationResult names the evidence preserved by operator recovery.
type RecoverIntegrationResult struct {
	models.LifecycleOutcome
	TaskID   string                               `json:"task_id"`
	DryRun   bool                                 `json:"dry_run"`
	Replayed bool                                 `json:"replayed"`
	Recovery *models.IntegrationPrematureRecovery `json:"recovery"`
	Warnings []string                             `json:"warnings,omitempty"`
}

// RecoverIntegration retires only a premature, unreviewed global:1 analysis.
// It is an operator operation: no agent authority is inferred or manufactured.
func RecoverIntegration(projectRoot, taskID, reason string, dryRun bool) (result *RecoverIntegrationResult, retErr error) {
	defer func() {
		retErr = WrapLifecycleError("recover-integration", nil, retErr, models.LifecycleInvalidInput, "correct_input", "none")
	}()
	if err := paths.ValidateTaskID(taskID); err != nil {
		return nil, err
	}
	reason = strings.TrimSpace(secretmask.New().MaskText(reason))
	if taskID == "" || reason == "" {
		return nil, fmt.Errorf("task ID and recovery reason are required")
	}
	err := withOwnershipTaskLock(projectRoot, taskID, "recover-integration", func() error {
		return withTaskReviewLock(projectRoot, taskID, "recover-integration", func() error {
			var err error
			result, err = recoverIntegrationLocked(projectRoot, taskID, reason, dryRun)
			return err
		})
	})
	return result, err
}

func recoverIntegrationLocked(projectRoot, taskID, reason string, dryRun bool) (*RecoverIntegrationResult, error) {
	bb := db.For(paths.New(projectRoot).StatePath())
	pb, err := loadPipelineBundle(projectRoot)
	if err != nil {
		return nil, err
	}
	state, err := bb.Read()
	if err != nil {
		return nil, err
	}
	if state.Goal.Integration != nil && state.Goal.Integration.PrematureRecovery != nil {
		r := state.Goal.Integration.PrematureRecovery
		if r.AnalysisTaskID != taskID || r.Reason != reason {
			return nil, fmt.Errorf("integration was already recovered with a different task or reason")
		}
		if err := statevalidate.ValidateState(state, projectRoot, false, io.Discard); err != nil {
			return nil, err
		}
		return &RecoverIntegrationResult{LifecycleOutcome: NewLifecycleOutcome("recover-integration", state.FindTask(taskID), models.LifecycleAlreadyCompleted, "continue", "none"), TaskID: taskID, DryRun: dryRun, Replayed: true, Recovery: r}, nil
	}
	task, err := validatePrematureIntegrationRecovery(state, taskID, pb.resolver)
	if err != nil {
		return nil, err
	}
	gw := git.New(projectRoot)
	if err := validateRecoveryReport(gw, task); err != nil {
		return nil, err
	}
	recovery := &models.IntegrationPrematureRecovery{
		At: time.Now().UTC(), Reason: reason, AnalysisTaskID: taskID,
		PreviousContributingSet: *state.Goal.Integration.ContributingSet,
		SourceCommit:            task.IntegrationAnalysis.SourceCommit, ReportCommit: *task.ReviewCommit,
		PreservationRef: "refs/integration-recovery/" + taskID,
	}
	result := &RecoverIntegrationResult{LifecycleOutcome: NewLifecycleOutcome("recover-integration", task, models.LifecycleCompleted, "continue", "none"), TaskID: taskID, DryRun: dryRun, Recovery: recovery}
	existingPin, pinErr := gw.ResolveCommit(recovery.PreservationRef)
	if pinErr == nil && existingPin != recovery.ReportCommit {
		return nil, fmt.Errorf("preservation ref already points to a different commit")
	}
	if dryRun {
		return result, nil
	}
	// A create-only ref protects the submitted report before cancellation removes
	// the ordinary task branch. A prior equal pin is safe after an interrupted call.
	if pinErr != nil {
		if err := gw.UpdateRef(recovery.PreservationRef, recovery.ReportCommit, strings.Repeat("0", len(recovery.ReportCommit))); err != nil {
			return nil, err
		}
	}

	runLifecycleMutationTestHook(bb)
	err = bb.Modify(func(current *models.State) error {
		currentTask, err := validatePrematureIntegrationRecovery(current, taskID, pb.resolver)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(task, currentTask) || !reflect.DeepEqual(state.Goal.Integration, current.Goal.Integration) {
			return fmt.Errorf("integration recovery state changed; requery before retrying")
		}
		previous := snapshotIntegrationLifecycleState(current)
		models.AdvanceLifecycle(currentTask)
		if err := currentTask.TransitionWith(models.TaskStatusAbandoned, pb.transitions); err != nil {
			return err
		}
		releaseAgentsForTask(current, taskID)
		currentTask.AssignedTo, currentTask.LeaseExpires = nil, nil
		currentTask.ReviewingBy, currentTask.ReviewLeaseExpires = nil, nil
		currentTask.Worktree = nil
		clearAttemptState(currentTask, attemptStateRetire)
		actor := "human"
		currentTask.History = append(currentTask.History, models.TaskHistoryEntry{Time: recovery.At, Event: models.TaskEventAbandoned, Agent: &actor, Reason: &reason, Extra: map[string]any{"operation": "recover-integration", "report_commit": recovery.ReportCommit, "preservation_ref": recovery.PreservationRef}})
		current.Goal.Integration.ContributingSet = nil
		current.Goal.Integration.PrematureRecovery = recovery
		result.LifecycleOutcome = NewLifecycleOutcome("recover-integration", currentTask, models.LifecycleCompleted, "continue", "committed")
		if err := statevalidate.ValidateIntegrationLifecycleTransition(previous, current); err != nil {
			return err
		}
		return statevalidate.ValidateState(current, projectRoot, false, io.Discard)
	})
	if err != nil {
		return nil, WrapLifecycleError("recover-integration", task, fmt.Errorf("recovery refused; submitted report remains preserved at %s: %w", recovery.PreservationRef, err), models.LifecycleStateChanged, "requery", "unknown")
	}
	if err := gw.RemoveWorktreeDir(taskID); err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("worktree cleanup: %v", err))
	}
	if err := gw.DeleteBranch(paths.TaskBranchPrefix + taskID); err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("task branch cleanup: %v", err))
	}
	return result, nil
}

func validatePrematureIntegrationRecovery(state *models.State, taskID string, resolver *pipeline.Resolver) (*models.Task, error) {
	if state.Config.Mode != models.SystemModePaused {
		return nil, fmt.Errorf("integration recovery requires PAUSED mode")
	}
	l := state.Goal.Integration
	if l == nil || l.PrematureRecovery != nil || l.ContributingSet == nil || len(l.ContributingSet.Scopes) != 0 || len(l.Coverage) != 0 || len(l.GlobalGenerations) != 0 || l.Closure != nil {
		return nil, fmt.Errorf("recovery requires an empty frozen cohort without coverage, verdicts or closure")
	}
	task := state.FindTask(taskID)
	if task == nil || task.IntegrationAnalysis == nil {
		return nil, fmt.Errorf("analysis task not found")
	}
	m := task.IntegrationAnalysis
	if m.Key != "global:1" || m.Phase != models.IntegrationAnalysisPhaseGlobal || m.Generation != 1 || len(m.DescendantChanges) != 0 || len(m.RootTaskIDs) != 0 || len(task.EffectiveParentTasks()) != 0 {
		return nil, fmt.Errorf("recovery requires the premature global:1 analysis with no contributors")
	}
	submitted, err := resolver.SubmittedStatus(task.RolePair)
	if err != nil || task.RolePair != globalIntegrationRolePair || task.Status != submitted || task.ReviewCommit == nil || task.Worktree == nil {
		return nil, fmt.Errorf("recovery requires a submitted analysis with a worktree and report commit")
	}
	if len(task.Approvals) != 0 || task.ApprovedBy != nil || task.MergeCommit != nil || task.ReviewingBy != nil {
		return nil, fmt.Errorf("reviewed or actively reviewed analysis cannot be recovered")
	}
	for _, h := range task.History {
		if h.Event == models.TaskEventApproved || h.Event == models.TaskEventRejected {
			return nil, fmt.Errorf("analysis already has a review verdict")
		}
	}
	for _, q := range state.QuarantinedVerdicts {
		if q.TaskID == taskID {
			return nil, fmt.Errorf("analysis has quarantined review evidence")
		}
	}
	count := 0
	for _, t := range state.Tasks {
		if t.IntegrationAnalysis != nil {
			count++
		}
		for _, p := range append(append([]string(nil), t.EffectiveParentTasks()...), t.DependsOn...) {
			if p == taskID {
				return nil, fmt.Errorf("analysis has dependent or descendant task %s", t.ID)
			}
		}
		if t.Supersedes != nil && *t.Supersedes == taskID {
			return nil, fmt.Errorf("analysis has replacement descendants")
		}
	}
	if count != 1 {
		return nil, fmt.Errorf("recovery requires exactly one integration analysis")
	}
	// Registration is the authority boundary. A dead local PID cannot prove that
	// an owner in another namespace is quiescent; require audited owner removal.
	for id, agent := range state.Agents {
		owns := task.AssignedTo != nil && *task.AssignedTo == id || task.ReviewingBy != nil && *task.ReviewingBy == id || agent.CurrentTask != nil && *agent.CurrentTask == taskID
		if owns {
			return nil, fmt.Errorf("analysis has a registered owner %s; remove or fence that owner through supported recovery first", id)
		}
	}
	capability, err := resolver.SlicedIntegrationCapability()
	if err != nil {
		return nil, err
	}
	unsettled, err := UnsettledPreIntegrationPlanningTasks(state, capability)
	if err != nil {
		return nil, err
	}
	if len(unsettled) == 0 {
		return nil, fmt.Errorf("upstream planning is settled; premature recovery is not applicable")
	}
	return task, nil
}

func validateRecoveryReport(gw *git.Git, task *models.Task) error {
	if *task.Worktree != gw.GetWorktreePath(task.ID) && *task.Worktree != paths.WorktreesDirName+"/"+task.ID {
		return fmt.Errorf("analysis worktree path is not canonical")
	}
	report, err := gw.ResolveCommit(*task.ReviewCommit)
	if err != nil || report != *task.ReviewCommit {
		return fmt.Errorf("submitted report must name a full commit")
	}
	if _, err := gw.ResolveCommit(task.IntegrationAnalysis.SourceCommit); err != nil {
		return fmt.Errorf("analysis source commit is unavailable: %w", err)
	}
	head, err := gw.GetWorktreeHEAD(task.ID)
	if err != nil || head != report {
		return fmt.Errorf("worktree HEAD does not match submitted report")
	}
	branch, err := gw.GetWorktreeBranch(gw.GetWorktreePath(task.ID))
	if err != nil || branch != paths.TaskBranchPrefix+task.ID {
		return fmt.Errorf("analysis worktree branch does not match task")
	}
	status, err := gw.WorktreeStatusShort(gw.GetWorktreePath(task.ID))
	if err != nil {
		return err
	}
	if strings.TrimSpace(status) != "" {
		return fmt.Errorf("analysis worktree is dirty; preserve all work before recovery")
	}
	return nil
}
