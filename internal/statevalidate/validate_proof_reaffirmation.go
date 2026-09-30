package statevalidate

import (
	"fmt"
	"strings"

	"github.com/liza-mas/liza/internal/models"
)

// ValidateProofReaffirmations checks that every recorded re-affirmation names
// the allocation it was granted for and both section identities.
//
// A record missing any of them cannot be matched by the acceptance boundary,
// so it would sit in state looking like an authorization while authorizing
// nothing — the worst outcome for a record whose only job is to be auditable.
// Both identities must be full object ids and must differ: a record claiming
// the content did not move authorizes a transition that never happened.
func ValidateProofReaffirmations(state *models.State) error {
	return collectErr(func(v *violations) { validateProofReaffirmations(v, state) })
}

func validateProofReaffirmations(v *violations, state *models.State) {
	for i, record := range state.ProofReaffirmations {
		for _, field := range []struct{ name, value string }{
			{"parent_task", record.ParentTask},
			{"carrier_path", record.CarrierPath},
			{"heading", record.Heading},
			{"reference_id", record.ReferenceID},
			{"actor", record.Actor},
			{"reason", record.Reason},
		} {
			if strings.TrimSpace(field.value) == "" {
				v.add(fmt.Errorf("proof_reaffirmations[%d]: %s must not be empty", i, field.name))
			}
		}
		reviewedFull := models.IsFullReviewCommit(record.ReviewedSection)
		currentFull := models.IsFullReviewCommit(record.CurrentSection)
		if !reviewedFull {
			v.add(fmt.Errorf("proof_reaffirmations[%d]: reviewed_section must be a full object id", i))
		}
		if !currentFull {
			v.add(fmt.Errorf("proof_reaffirmations[%d]: current_section must be a full object id", i))
		}
		// Comparing identities means nothing until both are identities.
		if reviewedFull && currentFull && record.ReviewedSection == record.CurrentSection {
			v.add(fmt.Errorf("proof_reaffirmations[%d]: identities are equal, so no transition was authorized", i))
		}
		if record.Timestamp.IsZero() {
			v.add(fmt.Errorf("proof_reaffirmations[%d]: timestamp must be recorded", i))
		}
	}
}
