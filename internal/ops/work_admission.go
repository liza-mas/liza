package ops

import (
	"context"
	stderrors "errors"
	"fmt"
	"os"
	"sync/atomic"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/models"
)

// Work admission (D86, ADR-0173). In a halt mode (PAUSED,
// CIRCUIT_BREAKER_TRIPPED) every point that starts new work refuses it inside
// the transaction or lock that commits it: claim-triggered state writes, merge
// preparation, and provider start. Running provider turns and merges whose
// preparation committed before the halt finish.
//
// Provider start is the one boundary a state transaction cannot order against
// a halt, because the process starts after the check. Launches therefore hold
// the work-admission lock shared across their mode check and start, and a halt
// transition takes it exclusively after committing, as a barrier: once the
// barrier returns, every start admitted before the halt has happened and no
// later start can be admitted. The lock is never held across a provider turn,
// and no state, integration or project-lifecycle lock is taken while holding it.

// ErrSystemHalted matches every refusal caused by a halt mode.
var ErrSystemHalted = stderrors.New("system halted")

// SystemHaltedError refuses new work in a halt mode. It unwraps to a
// PreconditionError, so callers that classify refusals keep treating it as a
// refusal with no effects.
type SystemHaltedError struct {
	Operation string
	Mode      models.SystemMode
}

func (e *SystemHaltedError) reason() string {
	return fmt.Sprintf("system is %s: %s refused; running work finishes, new work waits for %q", e.Mode, e.Operation, brand.Command("resume"))
}

func (e *SystemHaltedError) Error() string { return e.Unwrap().Error() }

func (e *SystemHaltedError) Unwrap() error {
	return &PreconditionError{Reason: e.reason(), Details: map[string]any{"system_mode": string(e.Mode), "operation": e.Operation}}
}

func (e *SystemHaltedError) Is(target error) bool { return target == ErrSystemHalted }

// RequireWorkAdmitted refuses operation when state is in a halt mode. Call it
// inside the transaction that commits the operation's first effect.
func RequireWorkAdmitted(state *models.State, operation string) error {
	if state != nil && state.Config.Mode.HaltsWork() {
		return &SystemHaltedError{Operation: operation, Mode: state.Config.Mode}
	}
	return nil
}

var workAdmissionBarrierTimeoutNanos atomic.Int64

func init() {
	workAdmissionBarrierTimeoutNanos.Store(int64(60 * time.Second))
}

// SetWorkAdmissionBarrierTimeoutForTest shortens the barrier wait so a hung
// provider start can be exercised without slow tests.
func SetWorkAdmissionBarrierTimeoutForTest(timeout time.Duration) func() {
	previous := workAdmissionBarrierTimeoutNanos.Swap(int64(timeout))
	return func() { workAdmissionBarrierTimeoutNanos.Store(previous) }
}

// WorkAdmissionBarrierError reports a halt that committed while a provider
// start admitted before it had not finished within the barrier timeout. The
// halt mode stays committed; rerunning the halt command waits again.
type WorkAdmissionBarrierError struct {
	Operation string
	Timeout   time.Duration
	Err       error
}

func (e *WorkAdmissionBarrierError) Error() string {
	return fmt.Sprintf("%s committed, but a provider start admitted before it did not finish within %s; rerun %q to wait for it: %v",
		e.Operation, e.Timeout, brand.Command(e.Operation), e.Err)
}

func (e *WorkAdmissionBarrierError) Unwrap() error { return e.Err }

// WithWorkAdmissionSharedLock runs fn, a provider start's mode check and
// start, holding the work-admission lock shared.
func WithWorkAdmissionSharedLock(ctx context.Context, projectRoot, operation string, fn func() error) error {
	lock, err := projectFileLock(projectRoot, "work-admission")
	if err != nil {
		// Non-repository callers have no halt command racing them; keep their
		// own validation and error contract, as project lifecycle locking does.
		if stderrors.Is(err, os.ErrNotExist) {
			return fn()
		}
		return err
	}
	return lock.WithSharedLockOperationContext(ctx, operation, fn)
}

// AwaitWorkAdmissionBarrier waits until no provider start admitted before the
// caller's committed halt is still in progress. Callers must hold no other lock.
func AwaitWorkAdmissionBarrier(projectRoot, operation string) error {
	lock, err := projectFileLock(projectRoot, "work-admission")
	if err != nil {
		if stderrors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	timeout := time.Duration(workAdmissionBarrierTimeoutNanos.Load())
	err = lock.WithTimeout(timeout).WithLockOperation(operation+"-barrier", func() error { return nil })
	if filelock.IsLockErrorType(err, filelock.LockErrorTimeout) {
		return &WorkAdmissionBarrierError{Operation: operation, Timeout: timeout, Err: err}
	}
	return err
}
