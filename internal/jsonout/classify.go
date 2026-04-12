package jsonout

import (
	"errors"
	"fmt"
	"strings"

	lizaerrors "github.com/liza-mas/liza/internal/errors"
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
		patterns: []string{"race condition", "changed concurrently"},
		code:     "race_condition",
		message:  "state changed concurrently, retry",
	},
	{
		patterns: []string{
			"not IMPLEMENTING", "not REVIEWING", "not READY_FOR_REVIEW",
			"not CODE_READY_FOR_REVIEW", "not CODE_APPROVED",
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
	// Type-based checks (preferred).
	var nfe *lizaerrors.NotFoundError
	if errors.As(err, &nfe) {
		return "not_found", "resource not found"
	}

	var postWrite *ops.PostWriteValidationError
	if errors.As(err, &postWrite) {
		return "validation", "validation failed: precondition not met"
	}

	var precond *ops.PreconditionError
	if errors.As(err, &precond) {
		return "validation", precond.Reason
	}

	var intErr *ops.IntegrationFailedError
	if errors.As(err, &intErr) {
		return "validation", fmt.Sprintf("integration failed: %s", intErr.Reason)
	}

	var opErr *ops.OperationalError
	if errors.As(err, &opErr) {
		return "internal", opErr.Message
	}

	// String-based checks for untyped errors.
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
