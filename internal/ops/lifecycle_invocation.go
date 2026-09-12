package ops

import (
	"errors"
	"fmt"
	"sync"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
)

// A replay exits the blackboard callback with an error to skip serialization
// and replacement of state.yaml. The outer operation converts it to success.
var errLifecycleReplay = errors.New("lifecycle request already completed")

func isLifecycleReplay(err error) bool { return errors.Is(err, errLifecycleReplay) }

// LifecycleInvocation carries one explicit outer invocation's telemetry state.
// Construct it before acquiring operation locks and finish it after releasing
// them. Public compatibility wrappers delegate to one core entrypoint; nested
// mutations must not create another invocation for the same request.
type LifecycleInvocation struct {
	projectRoot string
	sprint      LifecycleSprintIdentity
	once        sync.Once
	warning     error
}

// NewLifecycleInvocation captures the observed sprint once. Unavailable state
// disables this observation without changing the domain operation's behavior.
func NewLifecycleInvocation(projectRoot string) *LifecycleInvocation {
	invocation := &LifecycleInvocation{projectRoot: projectRoot}
	state, err := db.For(paths.New(projectRoot).StatePath()).Read()
	if err != nil {
		invocation.warning = errors.New("lifecycle metrics unavailable: could not capture sprint identity")
		return invocation
	}
	invocation.sprint = CaptureLifecycleSprint(state.Sprint)
	return invocation
}

// Finish records a typed outcome once and returns only a diagnostic warning.
// It never derives lifecycle policy from error strings. Callers must preserve
// operation success/failure and must not retry a mutation due to this warning.
func (invocation *LifecycleInvocation) Finish(operation string, outcome models.LifecycleOutcome, operationErr error) error {
	invocation.once.Do(func() {
		if invocation.warning != nil {
			return
		}
		if operationErr != nil {
			var lifecycleErr *LifecycleError
			if !errors.As(operationErr, &lifecycleErr) {
				invocation.warning = errors.New("lifecycle metrics unavailable: operation has no typed outcome")
				return
			}
			outcome = lifecycleErr.Outcome
		}
		invocation.warning = RecordLifecycleOutcome(invocation.projectRoot, invocation.sprint, operation, outcome.Outcome)
	})
	return invocation.warning
}

// FinishResult attaches a telemetry warning to a successful result or preserves
// a failed operation's original error chain with the warning appended. The
// outcome and its safe_action are unchanged in either case.
func (invocation *LifecycleInvocation) FinishResult(operation string, outcome models.LifecycleOutcome, operationErr *error, warnings *[]string) {
	if warning := invocation.Finish(operation, outcome, *operationErr); warning != nil {
		if warnings != nil {
			*warnings = append(*warnings, warning.Error())
		} else if *operationErr != nil {
			*operationErr = fmt.Errorf("%w; %v", *operationErr, warning)
		}
	}
}
