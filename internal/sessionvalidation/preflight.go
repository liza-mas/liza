package sessionvalidation

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"

	"github.com/liza-mas/liza/internal/models"
)

// ErrPreflight identifies safe prerequisite failures without exposing raw errors.
var ErrPreflight = errors.New("validation preflight failed")

// Error contains only diagnostic identifiers, never command text, environment
// values, probe output, or the underlying operating-system error.
type Error struct {
	Code         string
	CommandIndex int
	CheckIndex   int
	Variable     string
}

func (e *Error) Error() string {
	code := e.Code
	switch code {
	case "env_file_unavailable", "invalid_contract", "environment_missing", "executable_missing", "probe_start_failed", "probe_failed", "probe_timeout", "preflight_canceled", "execution_policy_required", "artifact_unsupported", "context_unavailable", "context_changed", "retry_pending":
	default:
		code = "prerequisite_failed"
	}
	message := fmt.Sprintf("validation preflight: %s (command=%d check=%d); repair the configured execution context and retry", code, e.CommandIndex, e.CheckIndex)
	if validVariable(e.Variable) {
		message += "; required variable=" + e.Variable
	}
	return message
}

func (e *Error) Is(target error) bool { return target == ErrPreflight }

func validVariable(name string) bool {
	if name == "" || len(name) > 256 {
		return false
	}
	for i, c := range name {
		if c != '_' && !(c >= 'A' && c <= 'Z') && !(c >= 'a' && c <= 'z') && !(i > 0 && c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}

// Check executes cheap declared prerequisite probes in cwd with exactly env.
// Each probe gets at most ten seconds and the whole check at most thirty seconds.
// Output is discarded at the child-process boundary, including on failure.
func Check(ctx context.Context, commands []string, prerequisites []models.ValidationPrerequisite, cwd string, env []string) error {
	if err := models.ValidateValidationPrerequisites(commands, prerequisites); err != nil {
		return &Error{Code: "invalid_contract", CommandIndex: -1, CheckIndex: -1}
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	for _, prerequisite := range prerequisites {
		commandIndex := -1
		for i, command := range commands {
			if command == prerequisite.Command {
				commandIndex = i
				break
			}
		}
		if ctx.Err() != nil {
			return &Error{Code: "preflight_canceled", CommandIndex: commandIndex, CheckIndex: -1}
		}
		for i, variable := range prerequisite.Env {
			if environmentValue(env, variable) == "" {
				return &Error{Code: "environment_missing", CommandIndex: commandIndex, CheckIndex: i, Variable: variable}
			}
		}
		for i, executable := range prerequisite.Executables {
			if _, err := LookPath(executable, cwd, env); err != nil {
				return &Error{Code: "executable_missing", CommandIndex: commandIndex, CheckIndex: i}
			}
		}
		for i, argv := range prerequisite.Probes {
			if code := runProbe(ctx, argv, cwd, env); code != "" {
				return &Error{Code: code, CommandIndex: commandIndex, CheckIndex: i}
			}
		}
	}
	return nil
}

func runProbe(ctx context.Context, argv []string, cwd string, env []string) string {
	path, err := LookPath(argv[0], cwd, env)
	if err != nil {
		return "executable_missing"
	}
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(probeCtx, path, argv[1:]...)
	configureProbeCancellation(cmd)
	cmd.Dir = cwd
	cmd.Env = append([]string{}, env...)
	// Nil stdout/stderr connect directly to the null device, never a log buffer.
	if err := cmd.Start(); err != nil {
		if probeCtx.Err() != nil {
			return "probe_timeout"
		}
		return "probe_start_failed"
	}
	if err := cmd.Wait(); err != nil {
		if probeCtx.Err() != nil {
			return "probe_timeout"
		}
		return "probe_failed"
	}
	if probeCtx.Err() != nil {
		return "probe_timeout"
	}
	return ""
}
