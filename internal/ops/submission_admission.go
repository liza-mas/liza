package ops

import (
	"errors"
	"fmt"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
)

// submissionAdmission is what a submit settled before preparing (ADR-0209):
// the admission limits its batch is governed by, the slots already held (nil
// when its classes could not be computed yet), and its in-flight marker when
// written early.
type submissionAdmission struct {
	config       models.Config
	slots        *admissionSlots
	removeMarker func()
}

func (a *submissionAdmission) release() {
	if a == nil {
		return
	}
	if a.slots != nil {
		a.slots.release()
	}
	if a.removeMarker != nil {
		a.removeMarker()
	}
}

// preAdmitSubmission waits for the admission slots the task's canonical batch
// needs before the submit locks, prepares or rebases anything, so a class that
// stays full refuses with nothing to retire and the same request is retried
// as is. When the classes cannot be computed here, execution takes them as it
// does for every other caller. A nil admission means state was unreadable.
// The slots report their wait and the batch to progress.
func preAdmitSubmission(projectRoot, taskID, agentID string, authority *models.AgentAuthority, opts LifecycleRequestOptions, progress *submitProgress) (*submissionAdmission, error) {
	bb := db.For(paths.New(projectRoot).StatePath())
	state, err := bb.ReadSnapshot()
	if err != nil {
		return nil, nil
	}
	admission := &submissionAdmission{config: models.Config{AdmissionLimits: state.Config.AdmissionLimits}}
	task := state.FindTask(taskID)
	if len(state.Config.AdmissionLimits) == 0 || task == nil || task.AssignedTo == nil || *task.AssignedTo != agentID {
		return admission, nil
	}
	// Only a submit that can still execute waits; a replay or a refused
	// transition answers from preparation without a slot.
	resolver, _, err := loadResolver(projectRoot)
	if err != nil {
		return admission, nil
	}
	if executing, err := resolver.ExecutingStatus(task.RolePair); err != nil || task.Status != executing {
		return admission, nil
	}
	commit, err := git.New(projectRoot).GetCommitSHA(state.Config.IntegrationBranch)
	if err != nil {
		return admission, nil
	}
	input, err := loadAcceptanceInput(projectRoot, state, task, commit)
	if err != nil || input == nil {
		return admission, nil
	}
	commands, _ := input.executedCommands()
	limits := models.AdmissionLimitsFor(admission.config, commands)
	admission.slots = newAdmissionSlots(input.batchTimeoutSeconds)
	admission.slots.progress = progress
	if len(limits) == 0 {
		return admission, nil
	}
	if authority != nil {
		// The marker covers the wait and the batch, which together never
		// exceed the acceptance cap.
		window := acceptanceBatchTimeout(input.batchTimeoutSeconds) + admissionSlotWait(input.batchTimeoutSeconds)
		admission.removeMarker = writeInflightSubmitMarker(projectRoot, *authority, taskID, window, time.Now().UTC())
	}
	fenceCtx, stopFence := startAcceptanceOwnershipFence(bb, taskID, task.Status, agentID, authority)
	err = admission.slots.acquire(fenceCtx, projectRoot, limits)
	lost := stopFence()
	if err == nil && !lost {
		return admission, nil
	}
	admission.release()
	observed := readLifecycleTask(projectRoot, taskID, authority, opts)
	if lost {
		if authority != nil {
			if current, readErr := bb.Read(); readErr == nil {
				if authErr := RequireAgentAuthority(current, *authority); authErr != nil {
					return nil, authErr
				}
			}
		}
		return nil, &LifecycleError{
			Outcome: NewLifecycleOutcome(integrationOperationSubmitForReview, observed, models.LifecycleStateChanged, "requery", "none"),
			Err:     fmt.Errorf("submission ownership lost while waiting for an admission slot; nothing was prepared"),
		}
	}
	refusal := admissionSlotRefusal(taskID, err)
	var evidence *AcceptanceEvidenceError
	if errors.As(refusal, &evidence) {
		return nil, &LifecycleError{
			Outcome: NewLifecycleOutcome(integrationOperationSubmitForReview, observed, models.LifecycleRetryable, "retry", "none"),
			Err:     refusal,
		}
	}
	return nil, refusal
}
