package ops

import (
	"fmt"

	lizaerrors "github.com/liza-mas/liza/internal/errors"
)

// PostWriteValidationError indicates a mutation persisted but state validation
// failed immediately afterward.
type PostWriteValidationError struct {
	Err error
}

func (e *PostWriteValidationError) Error() string {
	return fmt.Sprintf("state validation failed after write: %v", e.Err)
}

func (e *PostWriteValidationError) Unwrap() error {
	return e.Err
}

// PostWriteValidationMarker marks a post-write validation failure for envelope
// classification.
func (e *PostWriteValidationError) PostWriteValidationMarker() {}

var _ lizaerrors.PostWriteValidationFailure = (*PostWriteValidationError)(nil)
