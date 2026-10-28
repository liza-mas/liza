package ops

import (
	"fmt"

	lizaerrors "github.com/liza-mas/liza/internal/errors"
)

// operationalCodeGitOperation is the OperationalError.Code of a failed Git
// invocation; callers and the review-claim classes match on it.
const operationalCodeGitOperation = "git_operation"

// OperationalError indicates an infrastructure or environment failure during
// operation execution. Unlike PreconditionError (caller mistake) this represents
// "execution failed" scenarios (git failures, config loading issues, etc.).
//
// The Message field is safe to expose to agents via MCP — it contains actionable
// context without leaking internal paths or error chains. The Err field holds the
// underlying cause for logging/CLI but is NOT exposed over MCP.
type OperationalError struct {
	Code    string         // optional stable JSON error code for expected operational failures
	Phase   string         // optional operation phase for recovery diagnostics
	Message string         // safe-to-expose description (shown to agents via MCP)
	Details map[string]any // bounded, safe diagnostics for agent recovery
	Err     error          // underlying cause (NOT exposed to agents, only in logs/CLI)
}

func (e *OperationalError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Err)
	}
	return e.Message
}

func (e *OperationalError) Unwrap() error {
	return e.Err
}

// OperationalCode returns the explicit error code, if any, for envelope
// classification.
func (e *OperationalError) OperationalCode() string { return e.Code }

// OperationalMessage returns the message safe to expose, for envelope
// classification.
func (e *OperationalError) OperationalMessage() string { return e.Message }

// OperationalCause returns the wrapped cause for envelope classification.
func (e *OperationalError) OperationalCause() error { return e.Err }

var _ lizaerrors.OperationalFailure = (*OperationalError)(nil)

func (e *OperationalError) SafeDetails() map[string]any {
	if e.Details == nil && e.Code == "" && e.Phase == "" {
		return nil
	}
	details := make(map[string]any, len(e.Details)+1)
	for k, v := range e.Details {
		details[k] = v
	}
	if e.Phase != "" {
		if _, exists := details["phase"]; !exists {
			details["phase"] = e.Phase
		}
	}
	return details
}
