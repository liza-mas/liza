package payloadschema

import (
	"strings"

	"github.com/liza-mas/liza/internal/models"
)

// ValidateHumanAction checks the ask of a human-owned block (ADR-0172). Empty
// means no ask. A present ask must be non-blank and one line, because the
// AWAITING HUMAN alert that quotes it is one alerts-log line, and within the
// state-text bound.
func ValidateHumanAction(field, ask string) []models.FieldDiagnostic {
	switch {
	case ask == "":
		return nil
	case strings.TrimSpace(ask) == "":
		return []models.FieldDiagnostic{scalarPayloadDiagnostic(field, "must not be blank", models.FieldValueClassMissing)}
	case strings.ContainsAny(ask, "\r\n"):
		return []models.FieldDiagnostic{scalarPayloadDiagnostic(field, "must be a single line", models.FieldValueClassMalformed)}
	case len(ask) > assessBlockedTextMaxBytes:
		return []models.FieldDiagnostic{scalarPayloadDiagnostic(field, "must be at most 4096 bytes", models.FieldValueClassOversized)}
	}
	return nil
}
