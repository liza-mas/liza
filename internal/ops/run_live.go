package ops

import (
	"context"
	stderrors "errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
)

// RunLiveInput runs one local live command for a task (ADR-0169).
type RunLiveInput struct {
	TaskID         string
	Argv           []string
	Dir            string
	TimeoutSeconds int
	// Authority identifies an agent caller; nil is the operator.
	Authority *models.AgentAuthority
}

// RunLiveResult carries the child's exit code and its masked combined output.
type RunLiveResult struct {
	ExitCode int
	Output   string
}

// RunLive executes argv without a shell, with every reserved runtime-input
// name scrubbed from the caller's environment and only the task's reusable
// inputs overlaid. single_use values are never delivered here, so no local
// run can spend a fixture; running a canonical command that consumes one is
// refused before launch as a guard against confusing a local run with the
// gate's. Output is captured, masked, and only then returned.
func RunLive(projectRoot string, input RunLiveInput) (*RunLiveResult, error) {
	if len(input.Argv) == 0 || input.Argv[0] == "" {
		return nil, &PreconditionError{Reason: "run-live requires a command after --"}
	}
	timeout := input.TimeoutSeconds
	if timeout == 0 {
		timeout = 600
	}
	if timeout < 1 || timeout > 3600 {
		return nil, &PreconditionError{Reason: "timeout must be between 1 and 3600 seconds"}
	}
	bb := db.For(paths.New(projectRoot).StatePath())
	state, task, err := readTaskState(bb, input.TaskID)
	if err != nil {
		return nil, err
	}
	if input.Authority != nil {
		if err := RequireAgentAuthority(state, *input.Authority); err != nil {
			return nil, err
		}
		caller := input.Authority.ID
		if (task.AssignedTo == nil || *task.AssignedTo != caller) && (task.ReviewingBy == nil || *task.ReviewingBy != caller) {
			return nil, &PreconditionError{Reason: fmt.Sprintf("run-live is limited to the task's assigned doer or claimed reviewer; %s holds neither for task %s", caller, task.ID)}
		}
	}
	if len(task.RuntimeInputs) == 0 {
		return nil, &PreconditionError{Reason: fmt.Sprintf("task %s declares no runtime inputs; run the command directly", task.ID)}
	}
	if command, input := runLiveConsumptiveMatch(task, input.Argv); input != "" {
		return nil, &PreconditionError{Reason: fmt.Sprintf("run-live refuses canonical command %q: it uses single_use input %s, which only the submission gate may spend; rely on the gate receipt, or run a non-consumptive subset", command, input)}
	}
	reusable := runtimeInputsForRole(task.RuntimeInputs, true)
	key, keyErr := loadRuntimeInputKey(state, false)
	resolutions := resolveRuntimeInputs(projectRoot, state, task.ID, reusable, key, keyErr)
	var refusals []runtimeInputResolution
	grant := &runtimeInputGrant{deny: models.RuntimeInputDenyNames(state)}
	var overlay []string
	for _, resolution := range resolutions {
		if resolution.code != "" {
			refusals = append(refusals, resolution)
			continue
		}
		overlay = append(overlay, resolution.materialization.Environment()...)
		if resolution.declaration.Secret {
			grant.secrets = append(grant.secrets, resolution.materialization.SecretValues()...)
		}
	}
	if len(refusals) > 0 {
		return nil, runtimeInputRefusalError(task.ID, refusals, nil)
	}
	environ := os.Environ()
	mask := acceptanceExecutionMaskWith(append(slices.Clone(environ), overlay...), grant.secrets)

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()
	output := &acceptanceOutputBuffer{remaining: acceptanceOutputLimit, cancel: cancel}
	cmd := exec.CommandContext(ctx, input.Argv[0], input.Argv[1:]...)
	cmd.Dir = input.Dir
	cmd.Env = append(models.ScrubEnvironment(environ, grant.deny), overlay...)
	cmd.Stdout, cmd.Stderr = output, output
	configProcessGroupKill(cmd)
	cmd.WaitDelay = 5 * time.Second
	runErr := cmd.Run()
	if output.overflow {
		// A truncated capture could split a secret and evade masking.
		return nil, &PreconditionError{Reason: "run-live output exceeds 1 MiB; run a smaller command"}
	}
	if ctx.Err() == context.DeadlineExceeded {
		return nil, &PreconditionError{Reason: fmt.Sprintf("run-live timed out after %d seconds", timeout)}
	}
	result := &RunLiveResult{ExitCode: -1, Output: mask(output.buffer.String())}
	if cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
	}
	var exitErr *exec.ExitError
	if runErr != nil && !stderrors.As(runErr, &exitErr) {
		return nil, &PreconditionError{Reason: "run-live could not start the command: " + mask(runErr.Error())}
	}
	return result, nil
}

// runLiveConsumptiveMatch returns the canonical command argv names and the
// single_use input it uses, if argv runs one: the words joined with single
// spaces, or a `sh|bash|zsh|dash -c <command>` wrapper around it.
func runLiveConsumptiveMatch(task *models.Task, argv []string) (string, string) {
	candidates := []string{strings.Join(argv, " ")}
	if len(argv) == 3 && argv[1] == "-c" && slices.Contains([]string{"sh", "bash", "zsh", "dash"}, filepath.Base(argv[0])) {
		candidates = append(candidates, argv[2])
	}
	for _, input := range task.RuntimeInputs {
		if input.Consumption != models.RuntimeInputSingleUse {
			continue
		}
		for _, command := range input.Commands {
			normalized := strings.Join(strings.Fields(command), " ")
			for _, candidate := range candidates {
				if strings.Join(strings.Fields(candidate), " ") == normalized {
					return command, input.ID
				}
			}
		}
	}
	return "", ""
}
