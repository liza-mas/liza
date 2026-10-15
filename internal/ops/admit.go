package ops

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/referencecontract"
)

// AdmitMaxWait bounds an admit slot wait: the acceptance cap.
const AdmitMaxWait = referencecontract.AcceptanceMaxTimeoutSeconds * time.Second

// AdmitInput runs one local command inside the admission slots of every class
// its text matches, from the pool canonical admission uses (ADR-0209).
type AdmitInput struct {
	Argv    []string
	Dir     string
	MaxWait time.Duration
	Stdin   io.Reader
	Stdout  io.Writer
	Stderr  io.Writer
	// Signals cancel the wait while queued; while the command runs they are
	// forwarded to its process group.
	Signals <-chan os.Signal
}

// AdmitSurvivorError reports that a descendant of the command may outlive it,
// so its slots were left held by the survivors instead of released.
type AdmitSurvivorError struct {
	Dirs []string
}

func (e *AdmitSurvivorError) Error() string {
	return fmt.Sprintf("admission slots left with surviving descendants of the command until they exit (slot locks: %s)", strings.Join(e.Dirs, ", "))
}

// admitReap kills what remains of a finished command's process group and
// reports whether none survives; tests replace it.
var admitReap = reapProcessGroup

// admitReapSupported: where a process tree cannot be confirmed gone (Windows)
// descriptors are not inherited either, so slots are released when admit ends.
var admitReapSupported = runtime.GOOS != "windows"

// RunAdmitted runs input.Argv without a shell. When its text, joined with
// spaces, matches a configured class, it first waits behind every canonical
// waiter of the class for a slot, then runs in its own process group holding
// the slots, and releases them once the group is gone. A command matching no
// class runs at once. It returns the command's exit code; on an error no
// command ran, except with AdmitSurvivorError.
func RunAdmitted(projectRoot string, input AdmitInput) (int, error) {
	if len(input.Argv) == 0 || input.Argv[0] == "" {
		return -1, &PreconditionError{Reason: "admit requires a command after --"}
	}
	if input.MaxWait < 0 || input.MaxWait > AdmitMaxWait {
		return -1, &PreconditionError{Reason: fmt.Sprintf("admit wait must be between 0 and %d seconds", int(AdmitMaxWait/time.Second))}
	}
	state, err := db.For(paths.New(projectRoot).StatePath()).ReadSnapshot()
	if err != nil {
		return -1, fmt.Errorf("admit cannot read admission limits: %w", err)
	}
	limits := models.AdmissionLimitsFor(models.Config{AdmissionLimits: state.Config.AdmissionLimits}, []string{strings.Join(input.Argv, " ")})
	slots := &admissionSlots{deadline: time.Now().Add(input.MaxWait), local: true}
	if len(limits) > 0 {
		if err := acquireUntilSignal(projectRoot, slots, limits, input.Signals); err != nil {
			return -1, err
		}
	}

	cmd := exec.CommandContext(context.Background(), input.Argv[0], input.Argv[1:]...)
	cmd.Dir = input.Dir
	cmd.Stdin, cmd.Stdout, cmd.Stderr = input.Stdin, input.Stdout, input.Stderr
	cmd.ExtraFiles = slots.files()
	configProcessGroupKill(cmd)
	cmd.WaitDelay = 5 * time.Second
	if err := cmd.Start(); err != nil {
		slots.release()
		return -1, fmt.Errorf("admit could not start the command: %w", err)
	}
	forwarded := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(forwarded)
		for {
			select {
			case sig := <-input.Signals:
				_ = signalProcessGroup(cmd.Process.Pid, sig)
			case <-done:
				return
			}
		}
	}()
	waitErr := cmd.Wait()
	close(done)
	<-forwarded
	code := 1
	if cmd.ProcessState != nil && cmd.ProcessState.ExitCode() >= 0 {
		code = cmd.ProcessState.ExitCode()
	}
	var exitErr *exec.ExitError
	if waitErr != nil && !errors.As(waitErr, &exitErr) {
		// WaitDelay expired on inherited stdio; the group is reaped below.
		code = max(code, 1)
	}
	if len(limits) == 0 {
		return code, nil
	}
	if admitReapSupported && !admitReap(cmd.Process.Pid, cmd.WaitDelay) {
		slots.abandon()
		dirs := make([]string, 0, len(limits))
		for _, limit := range limits {
			dirs = append(dirs, filepath.Join(paths.New(projectRoot).AdmissionSlotsDir(), limit.Class))
		}
		return max(code, 1), &AdmitSurvivorError{Dirs: dirs}
	}
	slots.release()
	return code, nil
}

// acquireUntilSignal takes slots' classes, cancelling the wait when a signal
// arrives; a cancelled or full wait holds nothing and leaves no ticket. A
// signal consumed here never reaches the command, so one that arrived even as
// the slots were granted still refuses the launch.
func acquireUntilSignal(projectRoot string, slots *admissionSlots, limits []models.AdmissionLimit, signals <-chan os.Signal) error {
	started := time.Now()
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	stop := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		select {
		case sig := <-signals:
			cancel(fmt.Errorf("received %s", sig))
		case <-stop:
		}
	}()
	err := slots.acquire(ctx, projectRoot, limits)
	close(stop)
	<-stopped
	if err == nil && ctx.Err() != nil {
		slots.release()
		last := limits[len(limits)-1].Class
		return &AdmissionSlotWaitError{Class: last, Waited: time.Since(started), Dir: filepath.Join(paths.New(projectRoot).AdmissionSlotsDir(), last), Err: context.Cause(ctx)}
	}
	return err
}
