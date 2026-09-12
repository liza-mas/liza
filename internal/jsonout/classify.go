package jsonout

import (
	"errors"
	"fmt"
	"strings"

	lizaerrors "github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/ops"
)

// stringRule maps error message patterns to a code and safe message.
type stringRule struct {
	patterns []string
	code     string
	message  string
}

// stringErrorRules are evaluated in order for untyped errors.
// Order matters: not-found before validation (to avoid "not found" matching "validation failed").
var stringErrorRules = []stringRule{
	{
		patterns: []string{"not found", "does not exist"},
		code:     "not_found",
		message:  "resource not found",
	},
	{
		patterns: []string{"race condition", "changed concurrently", "concurrent modification", "sentinel replaced"},
		code:     "race_condition",
		message:  "state changed concurrently, requery current state",
	},
	{
		patterns: []string{
			"not IMPLEMENTING", "not REVIEWING", "not READY_FOR_REVIEW",
			"not CODE_READY_FOR_REVIEW", "not CODE_TO_REVIEW", "not CODE_APPROVED",
			"not APPROVED", "must be", "is required", "invalid task ID",
			"validation failed", "must include", "mandatory",
		},
		code:    "validation",
		message: "validation failed",
	},
}

// ClassifyError maps a Go error to a string error code and safe message.
// Typed errors use controlled fields. Untyped errors use fixed messages
// to prevent leaking implementation details.
func ClassifyError(err error) (code string, message string) {
	// The operation's observed outcome outranks a wrapped legacy precondition.
	// In particular, an already-transitioned task is not malformed input.
	var lifecycle *ops.LifecycleError
	if errors.As(err, &lifecycle) {
		code = strings.ToLower(lifecycle.Outcome.Outcome)
		switch lifecycle.Outcome.Outcome {
		case "INVALID_INPUT":
			code = "validation"
		case "FORBIDDEN":
			code = "permission_denied"
		}
		return code, lifecycle.Error()
	}
	var authority *ops.AgentAuthorityError
	if errors.As(err, &authority) {
		return "stale_caller", authority.Error()
	}
	// Type-based checks (preferred).
	var nfe *lizaerrors.NotFoundError
	if errors.As(err, &nfe) {
		return "not_found", "resource not found"
	}

	var projectRootErr *lizaerrors.ProjectRootError
	if errors.As(err, &projectRootErr) {
		return "project_root", projectRootErr.Error()
	}

	var pipelineConfigErr *lizaerrors.PipelineConfigError
	if errors.As(err, &pipelineConfigErr) {
		return "pipeline_config", pipelineConfigErr.Error()
	}

	var permissionErr *lizaerrors.PermissionError
	if errors.As(err, &permissionErr) {
		return "permission_denied", permissionErr.Error()
	}

	var stateSchemaErr *lizaerrors.StateSchemaError
	if errors.As(err, &stateSchemaErr) {
		return "state_schema", stateSchemaErr.Error()
	}

	var worktreeContextErr *lizaerrors.WorktreeContextError
	if errors.As(err, &worktreeContextErr) {
		return "worktree_context", worktreeContextErr.Error()
	}

	var validationErr *lizaerrors.ValidationError
	if errors.As(err, &validationErr) {
		return "validation", validationErr.Message
	}

	var cliInputErr *lizaerrors.CLIInputError
	if errors.As(err, &cliInputErr) {
		return "validation", cliInputErr.Message
	}

	var postWrite *ops.PostWriteValidationError
	if errors.As(err, &postWrite) {
		return "validation", "validation failed: precondition not met"
	}

	var precond *ops.PreconditionError
	if errors.As(err, &precond) {
		return "validation", precond.Reason
	}

	var unblockRebaseConflict *ops.UnblockRebaseConflictError
	if errors.As(err, &unblockRebaseConflict) {
		return "unblock_rebase_conflict", "unblock rebase conflict: task remains BLOCKED with repair_request"
	}

	var intErr *ops.IntegrationFailedError
	if errors.As(err, &intErr) {
		return "validation", fmt.Sprintf("integration failed: %s", intErr.Reason)
	}

	var opErr *ops.OperationalError
	if errors.As(err, &opErr) {
		// Explicit operation codes are trusted classifications. If no code was
		// supplied, preserve transient semantics from recognizable inner errors
		// before defaulting to the OperationalError's safe message.
		if opErr.Code != "" {
			return opErr.Code, opErr.Message
		}
		if inner := opErr.Unwrap(); inner != nil {
			if innerCode, innerMsg, ok := classifyFileLock(inner); ok {
				return innerCode, innerMsg
			}
			if innerCode, innerMsg := classifyUntyped(inner); innerCode != "internal" {
				return innerCode, innerMsg
			}
		}
		return "internal", opErr.Message
	}

	if lockCode, lockMsg, ok := classifyFileLock(err); ok {
		return lockCode, lockMsg
	}

	return classifyUntyped(err)
}

func classifyFileLock(err error) (code string, message string, ok bool) {
	var lockErr *filelock.LockError
	if !errors.As(err, &lockErr) {
		return "", "", false
	}
	switch lockErr.Type {
	case filelock.LockErrorTimeout:
		return "lock_timeout", "lock acquisition timed out", true
	case filelock.LockErrorPermission:
		return "permission_denied", "lock permission denied", true
	case filelock.LockErrorDiskFull:
		return "filesystem", "lock failed: no space left on device", true
	case filelock.LockErrorFilesystem:
		return "filesystem", "lock filesystem error", true
	case filelock.LockErrorStale:
		return "lock_stale", "lock owner metadata is stale", true
	default:
		return "", "", false
	}
}

type safeDetails interface {
	SafeDetails() map[string]any
}

// ErrorDetails returns bounded, explicitly safe diagnostic details for typed
// errors that opt in. Untyped errors never expose details.
func ErrorDetails(err error) map[string]any {
	var detailed safeDetails
	if errors.As(err, &detailed) {
		return detailed.SafeDetails()
	}
	return nil
}

// classifyUntyped maps untyped errors to codes via string matching.
// Returns ("internal", "internal error") when no pattern matches.
func classifyUntyped(err error) (code string, message string) {
	msg := err.Error()

	// Lock timeout: compound match (requires "lock" AND a timeout indicator).
	if strings.Contains(msg, "lock") &&
		(strings.Contains(msg, "timeout") || strings.Contains(msg, "timed out")) {
		return "lock_timeout", "lock acquisition timed out"
	}

	// String fallback rules.
	for _, rule := range stringErrorRules {
		for _, p := range rule.patterns {
			if strings.Contains(msg, p) {
				return rule.code, rule.message
			}
		}
	}

	// Default: internal error without leaking implementation details.
	return "internal", "internal error"
}
