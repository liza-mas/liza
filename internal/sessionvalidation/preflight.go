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

// ErrorCode is a safe preflight diagnostic identifier. It holds any string:
// Error renders values outside the declared set as CodePrerequisiteFailed.
type ErrorCode string

const (
	CodeEnvFileUnavailable      ErrorCode = "env_file_unavailable"
	CodeInvalidContract         ErrorCode = "invalid_contract"
	CodeEnvironmentMissing      ErrorCode = "environment_missing"
	CodeExecutableMissing       ErrorCode = "executable_missing"
	CodeProbeStartFailed        ErrorCode = "probe_start_failed"
	CodeProbeFailed             ErrorCode = "probe_failed"
	CodeProbeTimeout            ErrorCode = "probe_timeout"
	CodePreflightCanceled       ErrorCode = "preflight_canceled"
	CodeExecutionPolicyRequired ErrorCode = "execution_policy_required"
	CodeArtifactUnsupported     ErrorCode = "artifact_unsupported"
	CodeContextUnavailable      ErrorCode = "context_unavailable"
	CodeContextChanged          ErrorCode = "context_changed"
	CodeRetryPending            ErrorCode = "retry_pending"
	CodeRuntimeInputScrubbed    ErrorCode = "runtime_input_scrubbed"
	CodePrerequisiteFailed      ErrorCode = "prerequisite_failed"
)

// Error contains only diagnostic identifiers, never command text, environment
// values, probe output, or the underlying operating-system error.
type Error struct {
	Code         ErrorCode
	CommandIndex int
	CheckIndex   int
	Variable     string
}

func (e *Error) Error() string {
	code := e.Code
	switch code {
	case CodeEnvFileUnavailable, CodeInvalidContract, CodeEnvironmentMissing, CodeExecutableMissing, CodeProbeStartFailed, CodeProbeFailed, CodeProbeTimeout, CodePreflightCanceled, CodeExecutionPolicyRequired, CodeArtifactUnsupported, CodeContextUnavailable, CodeContextChanged, CodeRetryPending, CodeRuntimeInputScrubbed:
	default:
		code = CodePrerequisiteFailed
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

// Fallback limits for Check when a caller passes a non-positive limit.
const (
	defaultProbeTimeout = 10 * time.Second
	defaultTotalTimeout = 30 * time.Second
)

// Check executes cheap declared prerequisite probes in cwd with exactly env.
// Each probe gets at most probeTimeout and the whole check at most totalTimeout
// (models.EffectiveValidationPreflightLimits); a non-positive limit falls back
// to ten and thirty seconds respectively.
// Output is discarded at the child-process boundary, including on failure.
func Check(ctx context.Context, commands []string, prerequisites []models.ValidationPrerequisite, cwd string, env []string, probeTimeout, totalTimeout time.Duration) error {
	if err := models.ValidateValidationPrerequisites(commands, prerequisites); err != nil {
		return &Error{Code: CodeInvalidContract, CommandIndex: -1, CheckIndex: -1}
	}
	if probeTimeout <= 0 {
		probeTimeout = defaultProbeTimeout
	}
	if totalTimeout <= 0 {
		totalTimeout = defaultTotalTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, totalTimeout)
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
			return &Error{Code: CodePreflightCanceled, CommandIndex: commandIndex, CheckIndex: -1}
		}
		for i, variable := range prerequisite.Env {
			if environmentValue(env, variable) == "" {
				return &Error{Code: CodeEnvironmentMissing, CommandIndex: commandIndex, CheckIndex: i, Variable: variable}
			}
		}
		for i, executable := range prerequisite.Executables {
			if _, err := LookPath(executable, cwd, env); err != nil {
				return &Error{Code: CodeExecutableMissing, CommandIndex: commandIndex, CheckIndex: i}
			}
		}
		for i, argv := range prerequisite.Probes {
			if code := runProbe(ctx, argv, cwd, env, probeTimeout); code != "" {
				return &Error{Code: code, CommandIndex: commandIndex, CheckIndex: i}
			}
		}
	}
	return nil
}

func runProbe(ctx context.Context, argv []string, cwd string, env []string, timeout time.Duration) ErrorCode {
	path, err := LookPath(argv[0], cwd, env)
	if err != nil {
		return CodeExecutableMissing
	}
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(probeCtx, path, argv[1:]...)
	configureProbeCancellation(cmd)
	cmd.Dir = cwd
	cmd.Env = append([]string{}, env...)
	// Nil stdout/stderr connect directly to the null device, never a log buffer.
	if err := cmd.Start(); err != nil {
		if probeCtx.Err() != nil {
			return CodeProbeTimeout
		}
		return CodeProbeStartFailed
	}
	if err := cmd.Wait(); err != nil {
		if probeCtx.Err() != nil {
			return CodeProbeTimeout
		}
		return CodeProbeFailed
	}
	if probeCtx.Err() != nil {
		return CodeProbeTimeout
	}
	return ""
}
