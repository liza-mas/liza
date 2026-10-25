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
	// budgetSeconds and retrySeconds are the batch and retry budgets an early
	// marker was published for; preparation refuses any other (ADR-0216).
	budgetSeconds, retrySeconds int
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
	admission := &submissionAdmission{config: models.Config{AdmissionLimits: state.Config.AdmissionLimits, AdmissionTimeoutRetry: state.Config.AdmissionTimeoutRetry}}
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
	admission.slots = newSubmitAdmissionSlots(input.batchTimeoutSeconds)
	admission.slots.progress = progress
	if len(limits) == 0 {
		return admission, nil
	}
	if authority != nil {
		// The marker covers the wait, the batch and any planned retry, which
		// together never exceed the acceptance cap plus the submit wait floor.
		doerRole, _ := resolver.DoerRole(task.RolePair)
		plan := planTimeoutRetry(admission.config, doerRole, task.RuntimeInputs, input.batchTimeoutSeconds, true)
		admission.budgetSeconds, admission.retrySeconds = input.batchTimeoutSeconds, plan.retrySeconds()
		window := submitAdmissionWindow(input.batchTimeoutSeconds, true, plan)
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
			Outcome: NewLifecycleOutcome(integrationOperationSubmitForReview, observed, models.LifecycleStateChanged, LifecycleSafeActionRequery, LifecycleEffectsNone),
			Err:     fmt.Errorf("submission ownership lost while waiting for an admission slot; nothing was prepared"),
		}
	}
	refusal := admissionSlotRefusal(taskID, err)
	var evidence *AcceptanceEvidenceError
	if errors.As(refusal, &evidence) {
		return nil, &LifecycleError{
			Outcome: NewLifecycleOutcome(integrationOperationSubmitForReview, observed, models.LifecycleRetryable, LifecycleSafeActionRetry, LifecycleEffectsNone),
			Err:     refusal,
		}
	}
	return nil, refusal
}

// planAdmission takes the limits, retry policy and slots pre-admission settled
// for the batch, falling back to the snapshot's configuration; a class they lack
// is taken at execution, within the same deadline (ADR-0209). An early marker
// covers one batch and retry budget, so any other is refused before anything
// changes (ADR-0216).
func (s *submissionRun) planAdmission() error {
	s.admissionConfig, s.held = s.state.Config, nil
	if s.invocation.admission != nil {
		s.admissionConfig, s.held = s.invocation.admission.config, s.invocation.admission.slots
	}
	if s.acceptance == nil {
		return nil
	}
	commands, _ := s.acceptance.executedCommands()
	s.limited = len(models.AdmissionLimitsFor(s.admissionConfig, commands)) > 0
	doerRole, _ := s.resolver.DoerRole(s.task.RolePair)
	s.retryPlan = planTimeoutRetry(s.admissionConfig, doerRole, s.task.RuntimeInputs, s.acceptance.batchTimeoutSeconds, s.limited)
	if early := s.invocation.admission; early != nil && early.removeMarker != nil &&
		(early.budgetSeconds != s.acceptance.batchTimeoutSeconds || early.retrySeconds != s.retryPlan.retrySeconds()) {
		return &LifecycleError{
			Outcome: NewLifecycleOutcome(s.request.Operation, s.task, models.LifecycleStateChanged, LifecycleSafeActionRequery, LifecycleEffectsNone),
			Err:     fmt.Errorf("task %s: the canonical admission budget changed after the submit queued; no command ran: resubmit the same commit", s.taskID),
		}
	}
	return nil
}

// acceptanceBatch is the time the final step may spend on the acceptance batch.
// A marker written after preparation must also cover a slot wait still ahead:
// wait, batch and retry together stay within the cap plus the floor.
func (s *submissionRun) acceptanceBatch() time.Duration {
	if s.acceptance == nil {
		return 0
	}
	return submitAdmissionWindow(s.acceptance.batchTimeoutSeconds, s.limited, s.retryPlan)
}
