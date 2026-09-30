package statevalidate

import (
	"fmt"
	"io"
	"time"

	"github.com/liza-mas/liza/internal/models"
)

// ValidateCandidate validates a state a mutation is about to persist and
// refuses it only for violations it adds (D84, ADR-0165).
//
// A candidate violation whose identity the baseline also reports is
// pre-existing: it does not block the mutation and is written to warnWriter.
// Identities are counted, so a second occurrence of an existing violation is
// new. A violation the baseline reports and the candidate does not has been
// repaired and is not mentioned.
//
// baseline returns the state before the mutation. It is loaded only when the
// candidate has violations, so the valid path costs one validation, as before.
// Inside a Blackboard.Modify callback, bb.ReadSnapshot returns exactly that
// pre-image: the lock is held and writers publish under it. If the baseline
// cannot be loaded or validated, every candidate violation is returned (fail
// closed).
//
// Ordinary validation warnings are discarded in both passes; warnWriter
// receives only pre-existing violations. Both passes share one timestamp, so
// a lease that expires between them cannot make an unchanged violation look
// new, or a new one look old.
func ValidateCandidate(candidate *models.State, baseline func() (*models.State, error), projectRoot string, skipSpecFileCheck bool, warnWriter io.Writer) error {
	return validateCandidate(candidate, baseline, projectRoot, skipSpecFileCheck, warnWriter, time.Now)
}

func validateCandidate(candidate *models.State, baseline func() (*models.State, error), projectRoot string, skipSpecFileCheck bool, warnWriter io.Writer, clock func() time.Time) error {
	now := clock().UTC()
	found, err := collectStateViolations(candidate, projectRoot, skipSpecFileCheck, io.Discard, now)
	if err != nil {
		return err
	}
	if len(found.list) == 0 {
		return nil
	}
	before, err := baseline()
	if err != nil {
		return found.err()
	}
	known, err := collectStateViolations(before, projectRoot, skipSpecFileCheck, io.Discard, now)
	if err != nil {
		return found.err()
	}

	remaining := make(map[string]int, len(known.list))
	for _, item := range known.list {
		remaining[item.id]++
	}
	var added violations
	for _, item := range found.list {
		if remaining[item.id] > 0 {
			remaining[item.id]--
			if warnWriter != nil {
				fmt.Fprintf(warnWriter, "WARNING: pre-existing state violation, not caused by this change: %s\n", item.err)
			}
			continue
		}
		added.list = append(added.list, item)
	}
	return added.err()
}
