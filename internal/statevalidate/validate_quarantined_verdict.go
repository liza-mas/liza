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
	return collectErr(func(v *violations) { validateQuarantinedVerdicts(v, state) })
}

func validateQuarantinedVerdicts(v *violations, state *models.State) {
	ids := make(map[string]bool, len(state.QuarantinedVerdicts))
	for i, finding := range state.QuarantinedVerdicts {
		prefix := fmt.Sprintf("quarantined_verdicts[%d]", i)
		// delete-task prunes findings, so the index shifts; the finding ID
		// is the stable owner, the index only a fallback when it is unusable.
		owner := "quarantined verdict " + finding.ID
		if finding.ID == "" || ids[finding.ID] {
			owner = prefix
		}
		v.within(owner, func(v *violations) {
			validateQuarantinedVerdict(v, state, prefix, finding, ids)
		})
		ids[finding.ID] = true
	}
}

func validateQuarantinedVerdict(v *violations, state *models.State, prefix string, finding models.QuarantinedVerdict, ids map[string]bool) {
	// The message names the finding's index for the operator; the identity is
	// the constraint alone, owned by the finding (see validateQuarantinedVerdicts),
	// so pruning an earlier finding does not make this one's defects look new.
	add := func(constraint string) {
		v.addID(constraint, fmt.Errorf("%s%s", prefix, constraint))
	}
	if finding.ID == "" {
		add(" requires a unique nonempty id (empty)")
	} else if ids[finding.ID] {
		add(" requires a unique nonempty id (duplicate)")
	}
	if state.FindTask(finding.TaskID) == nil {
		add(" requires an existing task")
	}
	if finding.ReviewerID == "" {
		add(" requires a reviewer")
	}
	if finding.Timestamp.IsZero() {
		add(" requires a timestamp")
	}
	if !models.IsFullReviewCommit(finding.ReviewCommit) {
		add(" requires a full review commit")
	}
	if finding.Verdict != "APPROVED" && finding.Verdict != "REJECTED" {
		add(" has an invalid verdict")
	}
	if !utf8.ValidString(finding.Reason) {
		add(" requires a bounded UTF-8 reason (UTF-8)")
	}
	if len(finding.Reason) > statehygiene.MaxStateTextBytes {
		add(" requires a bounded UTF-8 reason (length)")
	}
	if finding.Verdict == "REJECTED" && strings.TrimSpace(finding.Reason) == "" {
		add(" requires a nonempty reason for rejection")
	}
	if len(finding.GenerationFingerprints) == 0 {
		add(" requires generation fingerprints")
	}
	fingerprints := make(map[string]bool, len(finding.GenerationFingerprints))
	for j, fingerprint := range finding.GenerationFingerprints {
		// Shape and uniqueness are separate: a malformed entry can still
		// repeat another.
		if _, err := hex.DecodeString(fingerprint); len(fingerprint) != 64 || err != nil {
			add(fmt.Sprintf(" requires unique SHA-256 generation fingerprints (fingerprint %d malformed)", j))
		}
		if fingerprints[fingerprint] {
			add(fmt.Sprintf(" requires unique SHA-256 generation fingerprints (fingerprint %d duplicate)", j))
		}
		fingerprints[fingerprint] = true
	}
	previous := finding.Timestamp
	for j, reconciliation := range finding.Reconciliations {
		entry := fmt.Sprintf(".reconciliations[%d]", j)
		if reconciliation.Actor == "" {
			add(entry + " requires actor")
		}
		if reconciliation.Timestamp.IsZero() {
			add(entry + " requires a chronological timestamp (zero)")
		} else if reconciliation.Timestamp.Before(previous) {
			add(entry + " requires a chronological timestamp (before previous)")
		}
		if !models.IsVerdictDisposition(reconciliation.Disposition) {
			add(entry + " requires a supported disposition")
		}
		for _, check := range []struct {
			name   string
			broken bool
		}{
			{"empty", strings.TrimSpace(reconciliation.Reason) == ""},
			{"UTF-8", !utf8.ValidString(reconciliation.Reason)},
			{"length", len(reconciliation.Reason) > statehygiene.MaxStateTextBytes},
		} {
			if check.broken {
				add(entry + " requires a bounded nonempty UTF-8 reason (" + check.name + ")")
			}
		}
		previous = reconciliation.Timestamp
	}
}
