package ops

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/liza-mas/liza/internal/db"
	gitpkg "github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
)

// stateLockHoldGate checks only canonical state. No prose inference may grant
// automatic recovery, and every RCA record remains an explicit recovery duty.
func stateLockHoldGate(state *models.State, task *models.Task, pr models.PipelineResolver) string {
	if task == nil || task.RepairRequest != nil {
		return "repair_required"
	}
	if _, ok := models.CurrentAwaitingHuman(task); ok {
		return "human_required"
	}
	if task.RejectionRCA != nil {
		return "rejection_rca_required"
	}
	if task.Lifecycle != nil && task.Lifecycle.Preparation != nil {
		return "preparation_pending"
	}
	if awaited, ok := currentEpisodeAwaitedTasks(task); ok && len(awaited) > 0 {
		return "awaited_tasks_pending"
	}
	seen := make(map[string]bool, len(task.DependsOn))
	for _, id := range task.DependsOn {
		if id == "" || strings.TrimSpace(id) != id || id == task.ID || seen[id] || dependencyReachesTask(state, id, task.ID, map[string]bool{}) {
			return "invalid_dependencies"
		}
		seen[id] = true
	}
	if len(models.NewDependencyResolver(state).UnmetDependencies(task, pr)) > 0 {
		return "dependencies_pending"
	}
	return ""
}

func installStateLockHold(state *models.State, task *models.Task) {
	task.StateLockHold = &models.StateLockHold{EpisodeAt: models.BlockedEpisodeAt(task), BlockerDigest: models.StateLockBlockerDigest(task), AfterSequence: state.MutationSequence + 1}
}

func stateLockHoldMaterial(state *models.State, task *models.Task, refusal string) string {
	data, _ := json.Marshal(struct {
		Fingerprint, Boundary, Refusal string
		Mode                           models.SystemMode
		Sprint                         models.SprintStatus
		Lifecycle                      *models.TaskLifecycle
		RolePair                       string
		Providers                      []models.ProviderDependency
		Worktree, BaseCommit           *string
	}{
		BuildAssessmentFingerprint(state, task, currentBlockerCandidate(state, task)), models.TaskTransitionID(task), refusal, state.Config.Mode, state.Sprint.Status, task.Lifecycle, task.RolePair, task.ProviderDependencies, task.Worktree, task.BaseCommit,
	})
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

func stateLockHoldReady(state *models.State, task *models.Task, pr models.PipelineResolver) bool {
	return models.CurrentStateLockHold(task) && state.MutationSequence > task.StateLockHold.AfterSequence && stateLockHoldGate(state, task, pr) == ""
}

func automaticStateLockHoldGate(state *models.State, task *models.Task, projectRoot string, pr *pipeline.Resolver, expected *models.StateLockHold) string {
	if !models.CurrentStateLockHold(task) || !reflect.DeepEqual(task.StateLockHold, expected) {
		return "hold_changed"
	}
	if state.MutationSequence <= task.StateLockHold.AfterSequence {
		return "publication_pending"
	}
	if state.Config.Mode != "" && state.Config.Mode != models.SystemModeRunning {
		return "system_not_running"
	}
	if state.Sprint.Status == models.SprintStatusCheckpoint || state.Sprint.Status == models.SprintStatusCompleted {
		return "sprint_not_running"
	}
	if reason := stateLockHoldGate(state, task, pr); reason != "" {
		return reason
	}
	if _, err := pr.InitialStatus(task.RolePair); err != nil {
		return "invalid_role_pair"
	}
	if err := validateUnblockDirectDependencies(state, pr, task); err != nil {
		return "invalid_dependencies"
	}
	if err := validateDependencyDirection(state, pr, task.ID, task.RolePair, task.DependsOn); err != nil {
		return "invalid_dependency_direction"
	}
	if task.Worktree != nil {
		g := gitpkg.New(projectRoot)
		if *task.Worktree != g.GetWorktreeRelPath(task.ID) || task.BaseCommit == nil || *task.BaseCommit == "" {
			return "worktree_metadata_invalid"
		}
		if _, err := g.GetCommitSHA(*task.BaseCommit); err != nil {
			return "worktree_base_invalid"
		}
		if err := g.ValidateWorktreeHealth(task.ID); err != nil {
			return "worktree_unhealthy"
		}
		branch, err := g.GetWorktreeBranch(g.GetWorktreePath(task.ID))
		if err != nil || branch != paths.TaskBranchPrefix+task.ID {
			return "worktree_branch_changed"
		}
	}
	return ""
}

// HasStateLockHoldCandidates is a cheap snapshot predicate for supervisor
// polling. It does not authorize restoration or skip the locked guard.
func HasStateLockHoldCandidates(state *models.State) bool {
	for i := range state.Tasks {
		task := &state.Tasks[i]
		if models.CurrentStateLockHold(task) && state.MutationSequence > task.StateLockHold.AfterSequence {
			return true
		}
	}
	return false
}

// RecoverStateLockHolds performs mechanical, generation-fenced continuations
// before wake selection. The snapshot-only no-work path writes nothing. Refused
// material is recorded once, independent of unrelated state publications, while
// each poll still checks filesystem health so repairing a worktree can resume it.
func RecoverStateLockHolds(ctx context.Context, bb *db.Blackboard, projectRoot string, authority models.AgentAuthority) (int, error) {
	state, err := bb.ReadSnapshot()
	if err != nil {
		return 0, err
	}
	var candidates []string
	for i := range state.Tasks {
		task := &state.Tasks[i]
		if models.CurrentStateLockHold(task) && state.MutationSequence > task.StateLockHold.AfterSequence {
			candidates = append(candidates, task.ID)
		}
	}
	if len(candidates) == 0 {
		return 0, nil
	}
	if err := RequireAgentAuthority(state, authority); err != nil {
		return 0, err
	}
	if state.Agents[authority.ID].Role != "orchestrator" {
		return 0, &PreconditionError{Reason: "state lock hold recovery requires orchestrator authority"}
	}
	pr, _, err := loadResolver(projectRoot)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, id := range candidates {
		if err := ctx.Err(); err != nil {
			return count, err
		}
		task := state.FindTask(id)
		hold := *task.StateLockHold
		if strings.HasPrefix(hold.RefusedReason, "unblock_") && hold.RefusedFingerprint == stateLockHoldMaterial(state, task, hold.RefusedReason) {
			continue
		}
		refusal := automaticStateLockHoldGate(state, task, projectRoot, pr, &hold)
		if refusal != "" {
			fingerprint := stateLockHoldMaterial(state, task, refusal)
			if hold.RefusedFingerprint == fingerprint && hold.RefusedReason == refusal {
				continue
			}
			err := recordStateLockHoldRefusal(bb, projectRoot, authority, pr, task, &hold, refusal)
			if err != nil && !errors.Is(err, errStateLockHoldUnchanged) {
				return count, err
			}
			continue
		}
		boundary := models.TaskTransitionID(task)
		request := LifecycleRequestOptions{RequestID: "state-lock-hold-" + boundary, ExpectedTransition: boundary, RetryContext: ctx}
		result, err := UnblockTaskWithAuthority(projectRoot, id, "state lock contention cleared after a later successful publication", authority, UnblockTaskOptions{Request: request, stateLockHold: &hold})
		if err != nil {
			if ctx.Err() != nil || IsAgentAuthorityError(err) {
				return count, err
			}
			var invalid *PreconditionError
			if !errors.As(err, &invalid) && !errors.Is(err, ErrLifecycleIdentityReused) {
				return count, err
			}
			if recordErr := recordStateLockHoldRefusal(bb, projectRoot, authority, pr, task, &hold, "unblock_domain_refused"); recordErr != nil && !errors.Is(recordErr, errStateLockHoldUnchanged) {
				return count, recordErr
			}
			continue
		}
		if result != nil && result.Outcome == models.LifecycleCompleted {
			count++
		} else {
			if err := recordStateLockHoldRefusal(bb, projectRoot, authority, pr, task, &hold, "unblock_no_change"); err != nil && !errors.Is(err, errStateLockHoldUnchanged) {
				return count, err
			}
		}
	}
	return count, nil
}

func recordStateLockHoldRefusal(bb *db.Blackboard, projectRoot string, authority models.AgentAuthority, pr *pipeline.Resolver, observed *models.Task, expected *models.StateLockHold, reason string) error {
	return ModifyWithAgentAuthority(bb, authority, func(state *models.State) error {
		task := state.FindTask(observed.ID)
		if task == nil || !reflect.DeepEqual(task.StateLockHold, expected) || models.TaskTransitionID(task) != models.TaskTransitionID(observed) {
			return errStateLockHoldUnchanged
		}
		if currentReason := automaticStateLockHoldGate(state, task, projectRoot, pr, expected); currentReason != "" {
			reason = currentReason
		} else if !strings.HasPrefix(reason, "unblock_") {
			return errStateLockHoldUnchanged
		}
		fingerprint := stateLockHoldMaterial(state, task, reason)
		if task.StateLockHold.RefusedReason == reason && task.StateLockHold.RefusedFingerprint == fingerprint {
			return errStateLockHoldUnchanged
		}
		task.StateLockHold.RefusedReason = reason
		task.StateLockHold.RefusedFingerprint = fingerprint
		return nil
	})
}

var errStateLockHoldUnchanged = errors.New("state lock hold observation changed or unchanged refusal")
