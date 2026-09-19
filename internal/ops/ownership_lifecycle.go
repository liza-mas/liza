package ops

import (
	"errors"
	"log"

	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/statevalidate"
)

// ownershipInvocation is local to one command call. Commands explicitly pass
// its request to each state transaction; it never changes lock ownership.
type ownershipInvocation struct {
	operation string
	opts      LifecycleRequestOptions
	request   LifecycleRequest
	task      *models.Task
	authority *models.AgentAuthority
	metrics   *LifecycleInvocation
	effects   bool
	outcome   models.LifecycleOutcome
}

func (i *ownershipInvocation) observe(task *models.Task) {
	i.task = task
}

func (i *ownershipInvocation) finish(projectRoot string, err error) error {
	if isLifecycleReplay(err) {
		err = nil
	}
	if err != nil {
		outcome, action, effects := models.LifecycleStateChanged, "requery", "none"
		var invalid *PreconditionError
		if !i.effects && errors.As(err, &invalid) {
			outcome, action = models.LifecycleInvalidInput, "correct_input"
		}
		if i.effects {
			effects = "unknown"
		}
		var observed *models.Task
		if i.task != nil {
			observed = readLifecycleTask(projectRoot, i.task.ID, i.authority)
		}
		err = WrapLifecycleError(i.operation, observed, err, outcome, action, effects)
	}
	if i.metrics != nil {
		if metricErr := i.metrics.Finish(i.operation, i.outcome, err); metricErr != nil {
			log.Printf("WARNING: lifecycle metrics unavailable: %v", metricErr)
		}
	}
	return err
}

// checkOwnerEndingRequest checks receipt/payload/token identity while allowing
// an authorized release or recovery to invalidate pending work. Its caller
// must validate release eligibility before mutating ownership.
func checkOwnerEndingRequest(task *models.Task, request LifecycleRequest) (*models.LifecycleReceipt, error) {
	copy := *task
	copy.Lifecycle = cloneTaskLifecycle(task.Lifecycle)
	copy.Lifecycle.Preparation = nil
	// Registry omitted deliberately: the preparation was just cleared, so no
	// retirement question remains for it to answer.
	return CheckLifecycleRequest(&copy, request, nil)
}

// prepareOwnerEndingRequest reserves explicit recovery after the caller has
// checked liveness/force policy. Unlike an ordinary writer, recovery is allowed
// to invalidate a live preparation; its original request identity stays fixed.
func prepareOwnerEndingRequest(task *models.Task, request LifecycleRequest) error {
	receipt, err := checkOwnerEndingRequest(task, request)
	if err != nil {
		return err
	}
	if receipt != nil {
		return errLifecycleReplay
	}
	copy := *task
	copy.Lifecycle = cloneTaskLifecycle(task.Lifecycle)
	models.AdvanceLifecycle(&copy)
	copy.Lifecycle.Preparation = &models.LifecyclePreparation{LifecycleIdentity: request, Boundary: models.TaskTransitionID(&copy)}
	if err := statevalidate.ValidateTaskLifecycle(&copy); err != nil {
		return err
	}
	task.Lifecycle = copy.Lifecycle
	return nil
}

func withOwnershipTaskLock(projectRoot, taskID, operation string, fn func() error) error {
	return WithProjectLifecycleSharedLock(projectRoot, operation, func() error {
		lock := filelock.New(claimTaskWorktreeLockPath(paths.New(projectRoot).StatePath(), taskID))
		return lock.WithLockOperation(operation, fn)
	})
}
