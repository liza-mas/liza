package ops

import (
	"fmt"

	lizaerrors "github.com/liza-mas/liza/internal/errors"
)

// PreconditionError indicates a precondition check failed before the main
// operation could execute. The Reason field is intentionally client-facing
// so that agents receive actionable feedback.
type PreconditionError struct {
	Reason  string
	Details map[string]any
}

func (e *PreconditionError) Error() string {
	return fmt.Sprintf("validation failed: %s", e.Reason)
}

func (e *PreconditionError) SafeDetails() map[string]any {
	return e.Details
}

// PreconditionReason returns the client-facing reason for envelope
// classification.
func (e *PreconditionError) PreconditionReason() string { return e.Reason }

var _ lizaerrors.PreconditionFailure = (*PreconditionError)(nil)
