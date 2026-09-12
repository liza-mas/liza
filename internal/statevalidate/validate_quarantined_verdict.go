package statevalidate

import (
	"encoding/hex"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/statehygiene"
)

// ValidateQuarantinedVerdicts checks the durable evidence carrier before a
// conflict gate uses it. Malformed evidence fails closed instead of becoming
// an implicit clearance through an unknown verdict or disposition.
func ValidateQuarantinedVerdicts(state *models.State) error {
	ids := make(map[string]bool, len(state.QuarantinedVerdicts))
	for i, finding := range state.QuarantinedVerdicts {
		prefix := fmt.Sprintf("quarantined_verdicts[%d]", i)
		if finding.ID == "" || ids[finding.ID] {
			return fmt.Errorf("%s requires a unique nonempty id", prefix)
		}
		ids[finding.ID] = true
		if state.FindTask(finding.TaskID) == nil || finding.ReviewerID == "" || finding.Timestamp.IsZero() || !models.IsFullReviewCommit(finding.ReviewCommit) {
			return fmt.Errorf("%s requires an existing task, reviewer, timestamp and full review commit", prefix)
		}
		if finding.Verdict != "APPROVED" && finding.Verdict != "REJECTED" {
			return fmt.Errorf("%s has an invalid verdict", prefix)
		}
		if !utf8.ValidString(finding.Reason) || len(finding.Reason) > statehygiene.MaxStateTextBytes ||
			(finding.Verdict == "REJECTED" && strings.TrimSpace(finding.Reason) == "") {
			return fmt.Errorf("%s requires a bounded UTF-8 reason (nonempty for rejection)", prefix)
		}
		if len(finding.GenerationFingerprints) == 0 {
			return fmt.Errorf("%s requires generation fingerprints", prefix)
		}
		fingerprints := make(map[string]bool, len(finding.GenerationFingerprints))
		for _, fingerprint := range finding.GenerationFingerprints {
			_, err := hex.DecodeString(fingerprint)
			if len(fingerprint) != 64 || err != nil || fingerprints[fingerprint] {
				return fmt.Errorf("%s requires unique SHA-256 generation fingerprints", prefix)
			}
			fingerprints[fingerprint] = true
		}
		previous := finding.Timestamp
		for j, reconciliation := range finding.Reconciliations {
			if reconciliation.Actor == "" || reconciliation.Timestamp.IsZero() || reconciliation.Timestamp.Before(previous) ||
				!models.IsVerdictDisposition(reconciliation.Disposition) || strings.TrimSpace(reconciliation.Reason) == "" ||
				!utf8.ValidString(reconciliation.Reason) || len(reconciliation.Reason) > statehygiene.MaxStateTextBytes {
				return fmt.Errorf("%s.reconciliations[%d] requires actor, chronological timestamp, supported disposition and bounded nonempty UTF-8 reason", prefix, j)
			}
			previous = reconciliation.Timestamp
		}
	}
	return nil
}
