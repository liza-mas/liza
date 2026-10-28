package jsonout

import (
	"errors"
	"strings"

	"github.com/liza-mas/liza/internal/filelock"
)

// stringRule maps error message patterns to a code and safe message.
type stringRule struct {
	patterns []string
	code     errorCode
	message  string
}

// stringErrorRules are evaluated in order for untyped errors.
// Order matters: not-found before validation (to avoid "not found" matching "validation failed").
var stringErrorRules = []stringRule{
	{
		patterns: []string{"not found", "does not exist"},
		code:     codeNotFound,
		message:  "resource not found",
	},
	{
		patterns: []string{"race condition", "changed concurrently", "concurrent modification", "sentinel replaced"},
		code:     codeRaceCondition,
		message:  "state changed concurrently, requery current state",
	},
	{
		patterns: []string{
			"not IMPLEMENTING", "not REVIEWING", "not READY_FOR_REVIEW",
			"not CODE_READY_FOR_REVIEW", "not CODE_TO_REVIEW", "not CODE_APPROVED",
			"not APPROVED", "must be", "is required", "invalid task ID",
			"validation failed", "must include", "mandatory",
		},
		code:    codeValidation,
		message: "validation failed",
	},
}

// ClassifyError maps a Go error to a string error code and safe message.
// Typed errors use controlled fields. Untyped errors use fixed messages
// to prevent leaking implementation details.
func ClassifyError(err error) (code string, message string) {
	for _, slot := range classificationSlots {
		if slotCode, slotMessage, ok := slot(err); ok {
			return string(slotCode), slotMessage
		}
	}
	if lockCode, lockMessage, ok := classifyFileLock(err); ok {
		return string(lockCode), lockMessage
	}
	untypedCode, untypedMessage := classifyUntyped(err)
	return string(untypedCode), untypedMessage
}

func classifyFileLock(err error) (code errorCode, message string, ok bool) {
	var lockErr *filelock.LockError
	if !errors.As(err, &lockErr) {
		return "", "", false
	}
	switch lockErr.Type {
	case filelock.LockErrorTimeout:
		return codeLockTimeout, "lock acquisition timed out", true
	case filelock.LockErrorPermission:
		return codePermissionDenied, "lock permission denied", true
	case filelock.LockErrorDiskFull:
		return codeFilesystem, "lock failed: no space left on device", true
	case filelock.LockErrorFilesystem:
		return codeFilesystem, "lock filesystem error", true
	case filelock.LockErrorStale:
		return codeLockStale, "lock owner metadata is stale", true
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

// Fragments of an untyped error message that together mark a lock timeout.
const (
	lockMessageFragment     = "lock"
	timeoutMessageFragment  = "timeout"
	timedOutMessageFragment = "timed out"
)

// classifyUntyped maps untyped errors to codes via string matching.
// Returns (codeInternal, messageInternal) when no pattern matches.
func classifyUntyped(err error) (code errorCode, message string) {
	msg := err.Error()

	// Lock timeout: compound match (requires "lock" AND a timeout indicator).
	if strings.Contains(msg, lockMessageFragment) &&
		(strings.Contains(msg, timeoutMessageFragment) || strings.Contains(msg, timedOutMessageFragment)) {
		return codeLockTimeout, "lock acquisition timed out"
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
	return codeInternal, messageInternal
}
