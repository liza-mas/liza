package ops

import (
	"errors"
	"fmt"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/statevalidate"
)

// artifactGuardPostUpdateTestHook is a test-only hook invoked after a successful
// CAS merge and before the retained post-merge artifact validation backstop.
// Production code leaves this nil.
var artifactGuardPostUpdateTestHook func() error

// artifactGuardReadState is how the candidate artifact guard reads state. The
// read takes no state lock: a saturated lock must not turn an approved merge
// into a failure (D78). A locked read would add no guarantee here, because its
// lock is released before update-ref either way; the post-update backstop takes
// a fresh locked read for its validation and conditionally rolls back. Tests
// replace it to observe or fail the guard's reads.
var artifactGuardReadState = (*db.Blackboard).ReadSnapshot

func buildArtifactGuardHook(bb *db.Blackboard, projectRoot string, gitWrapper *git.Git, taskID string) func(candidateTreeish string) error {
	readState := func() (*models.State, error) { return artifactGuardReadState(bb) }
	return func(candidateTreeish string) error {
		err := validateCandidateArtifactRefsWithFreshState(readState, projectRoot, gitWrapper, candidateTreeish, taskID)
		var unavailable *candidateArtifactGuardUnavailableError
		if err == nil || errors.As(err, &unavailable) {
			return err
		}
		return &candidateArtifactGuardError{err: err}
	}
}

// candidateArtifactGuardError is a confirmed verdict: the candidate tree lacks
// an artifact that state references.
type candidateArtifactGuardError struct {
	err error
}

func (e *candidateArtifactGuardError) Error() string {
	return e.err.Error()
}

func (e *candidateArtifactGuardError) Unwrap() error {
	return e.err
}

// candidateArtifactGuardUnavailableError means the guard could not read the
// state it needed to reach a verdict. It says nothing about the candidate, and
// the guard runs before update-ref, so the merge published nothing.
type candidateArtifactGuardUnavailableError struct {
	err error
}

func (e *candidateArtifactGuardUnavailableError) Error() string {
	return e.err.Error()
}

func (e *candidateArtifactGuardUnavailableError) Unwrap() error {
	return e.err
}

func validateCandidateArtifactRefsWithFreshState(
	readState func() (*models.State, error),
	projectRoot string,
	lookup statevalidate.CandidateTreeLookup,
	candidateTreeish string,
	taskID string,
) error {
	state, err := readState()
	if err != nil {
		return &candidateArtifactGuardUnavailableError{err: fmt.Errorf("candidate artifact guard failed to read state: %w", err)}
	}

	firstErr := statevalidate.ValidateCandidateMergeArtifactRefs(candidateTreeish, state, projectRoot, taskID, lookup)
	if firstErr == nil {
		return nil
	}

	confirmationState, confirmationReadErr := readState()
	if confirmationReadErr != nil {
		// An unconfirmed finding may come from stale state, so it is not a verdict.
		freshnessErr := fmt.Errorf("failed to re-read state for candidate artifact guard freshness: %w", confirmationReadErr)
		return &candidateArtifactGuardUnavailableError{err: fmt.Errorf("candidate artifact guard failed and state freshness could not be verified: %w", errors.Join(firstErr, freshnessErr))}
	}

	if confirmationErr := statevalidate.ValidateCandidateMergeArtifactRefs(candidateTreeish, confirmationState, projectRoot, taskID, lookup); confirmationErr != nil {
		return confirmationErr
	}
	return nil
}
