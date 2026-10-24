package statevalidate

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/statehygiene"
)

// Structural bounds for the rejection-RCA request payloads. Every bound below
// is evaluated from the request alone, so the preflight and mutation
// boundaries cannot disagree about a payload. A bound that needs task state —
// a rejection_index no higher than the task's durable rejection count — is a
// semantic precondition of the operation, never a structural diagnostic.
const (
	rejectionRCAMaxContributions = 32
	rejectionRCAMaxCategories    = 8
	rejectionRCAMaxCategoryBytes = 64
	rejectionRCAMaxEvidence      = 4
	rejectionRCAMaxEvidenceBytes = 256
)

// ValidateRejectionRCARequest reports the structural defects of a
// record-rejection-rca payload. A nil result means the payload is valid.
func ValidateRejectionRCARequest(request models.RejectionRCARequest) []models.FieldDiagnostic {
	diagnostics := validateRejectionRCASchemaVersion(request.SchemaVersion)
	diagnostics = append(diagnostics, validateBoundedPayloadText("/summary", request.Summary, statehygiene.MaxStateTextBytes, true)...)

	switch {
	case len(request.Contributions) == 0:
		diagnostics = append(diagnostics, payloadDiagnostic("/contributions",
			"at least one contribution is required", models.FieldValueClassMissing))
	case len(request.Contributions) > rejectionRCAMaxContributions:
		diagnostics = append(diagnostics, payloadDiagnostic("/contributions",
			fmt.Sprintf("at most %d contributions are allowed", rejectionRCAMaxContributions),
			models.FieldValueClassOutOfRange))
	default:
		diagnostics = append(diagnostics, validateRejectionRCAContributions(request.Contributions)...)
	}

	return models.NormalizeFieldDiagnostics(diagnostics)
}

// ValidateRejectionRCADispositionRequest reports the structural defects of a
// resume-rejection-rca payload. A nil result means the payload is valid.
func ValidateRejectionRCADispositionRequest(request models.RejectionRCADispositionRequest) []models.FieldDiagnostic {
	diagnostics := validateRejectionRCASchemaVersion(request.SchemaVersion)

	switch {
	case strings.TrimSpace(string(request.RecoveryPath)) == "":
		diagnostics = append(diagnostics, payloadDiagnostic("/recovery_path",
			"a non-empty value is required", models.FieldValueClassMissing))
	case !models.IsRecoveryPath(request.RecoveryPath):
		diagnostics = append(diagnostics, payloadDiagnostic("/recovery_path",
			"must name a known recovery path", models.FieldValueClassUnknownEnum))
	}

	diagnostics = append(diagnostics, validateBoundedPayloadText("/rationale", request.Rationale, statehygiene.MaxStateTextBytes,
		request.RecoveryPath == models.RecoveryHumanOverride)...)
	return models.NormalizeFieldDiagnostics(diagnostics)
}

// ValidateVerdictPayloadShape reports the structural defects of a
// submit-verdict payload, keyed by the command's flag names. An empty
// review_commit stays valid because the unauthenticated legacy call does not
// carry one; a supplied value must be the full immutable SHA reviewed.
func ValidateVerdictPayloadShape(taskID, verdict, reason, agentID string, impact pipeline.ReviewImpact, reviewCommit string) []models.FieldDiagnostic {
	var diagnostics []models.FieldDiagnostic

	if strings.TrimSpace(taskID) == "" {
		diagnostics = append(diagnostics, payloadDiagnostic("/task_id",
			"a non-empty value is required", models.FieldValueClassMissing))
	}
	if strings.TrimSpace(agentID) == "" {
		diagnostics = append(diagnostics, payloadDiagnostic("/agent_id",
			"a non-empty value is required", models.FieldValueClassMissing))
	}

	switch {
	case strings.TrimSpace(verdict) == "":
		diagnostics = append(diagnostics, payloadDiagnostic("/verdict",
			"a non-empty value is required", models.FieldValueClassMissing))
	case verdict != "APPROVED" && verdict != "REJECTED":
		diagnostics = append(diagnostics, payloadDiagnostic("/verdict",
			"must be APPROVED or REJECTED", models.FieldValueClassUnknownEnum))
	case verdict == "REJECTED":
		diagnostics = append(diagnostics, validateBoundedPayloadText("/reason", reason, statehygiene.MaxStateTextBytes, true)...)
	}

	if impact != "" && !isVerdictImpact(impact) {
		diagnostics = append(diagnostics, payloadDiagnostic("/impact",
			"must be standard, significant or architecture", models.FieldValueClassUnknownEnum))
	}
	if reviewCommit != "" && !models.IsFullReviewCommit(reviewCommit) {
		diagnostics = append(diagnostics, payloadDiagnostic("/review_commit",
			"must be the full immutable commit SHA reviewed", models.FieldValueClassMalformed))
	}

	return models.NormalizeFieldDiagnostics(diagnostics)
}

// isVerdictImpact reports whether impact is one of the impact classifications
// submit-verdict accepts. The ops package owns the ordering those values carry.
func isVerdictImpact(impact pipeline.ReviewImpact) bool {
	switch impact {
	case pipeline.ReviewImpactStandard, pipeline.ReviewImpactSignificant, pipeline.ReviewImpactArchitecture:
		return true
	}
	return false
}

func validateRejectionRCAContributions(contributions []models.RejectionRCAContribution) []models.FieldDiagnostic {
	var diagnostics []models.FieldDiagnostic
	seen := make(map[int]bool, len(contributions))

	for i, contribution := range contributions {
		prefix := fmt.Sprintf("/contributions/%d", i)
		diagnostics = append(diagnostics, validateRejectionIndex(prefix, contribution.RejectionIndex, seen)...)
		diagnostics = append(diagnostics, validateBoundedPayloadList(prefix+"/categories", contribution.Categories,
			rejectionRCAMaxCategories, rejectionRCAMaxCategoryBytes, true)...)
		diagnostics = append(diagnostics, validateBoundedPayloadList(prefix+"/evidence", contribution.Evidence,
			rejectionRCAMaxEvidence, rejectionRCAMaxEvidenceBytes, false)...)
	}

	return diagnostics
}

// validateRejectionIndex records each accepted index in seen, so a duplicate is
// reported on the later contribution rather than on the one that defined it.
func validateRejectionIndex(prefix string, index int, seen map[int]bool) []models.FieldDiagnostic {
	field := prefix + "/rejection_index"
	switch {
	case index < 1:
		return []models.FieldDiagnostic{payloadDiagnostic(field, "must be at least 1", models.FieldValueClassOutOfRange)}
	case seen[index]:
		return []models.FieldDiagnostic{payloadDiagnostic(field, "must be unique across contributions", models.FieldValueClassConflict)}
	}
	seen[index] = true
	return nil
}

// validateBoundedPayloadList enforces the cardinality of a string list and the
// bounds of each entry, reporting the cardinality defect alone when it applies
// so one malformed list yields one diagnostic.
func validateBoundedPayloadList[T ~string](field string, values []T, maxEntries, maxBytes int, required bool) []models.FieldDiagnostic {
	switch {
	case required && len(values) == 0:
		return []models.FieldDiagnostic{payloadDiagnostic(field,
			"at least one entry is required", models.FieldValueClassMissing)}
	case len(values) > maxEntries:
		return []models.FieldDiagnostic{payloadDiagnostic(field,
			fmt.Sprintf("at most %d entries are allowed", maxEntries), models.FieldValueClassOutOfRange)}
	}
	var diagnostics []models.FieldDiagnostic
	for i, value := range values {
		diagnostics = append(diagnostics, validateBoundedPayloadText(
			fmt.Sprintf("%s/%d", field, i), string(value), maxBytes, true)...)
	}
	return diagnostics
}

func validateRejectionRCASchemaVersion(version int) []models.FieldDiagnostic {
	if version == models.RejectionRCASchemaVersion {
		return nil
	}
	return []models.FieldDiagnostic{payloadDiagnostic("/schema_version",
		fmt.Sprintf("must be %d", models.RejectionRCASchemaVersion), models.FieldValueClassOutOfRange)}
}

// validateBoundedPayloadText enforces presence, a byte ceiling and UTF-8
// validity on one payload string, reporting the first defect only so a caller
// sees one diagnostic per field.
func validateBoundedPayloadText(field, value string, maxBytes int, required bool) []models.FieldDiagnostic {
	switch {
	case required && strings.TrimSpace(value) == "":
		return []models.FieldDiagnostic{payloadDiagnostic(field, "a non-empty value is required", models.FieldValueClassMissing)}
	case len(value) > maxBytes:
		return []models.FieldDiagnostic{payloadDiagnostic(field,
			fmt.Sprintf("must be at most %d bytes", maxBytes), models.FieldValueClassOversized)}
	case !utf8.ValidString(value):
		return []models.FieldDiagnostic{payloadDiagnostic(field, "must be valid UTF-8", models.FieldValueClassMalformed)}
	}
	return nil
}

// payloadDiagnostic names a rejected field without echoing its value. The
// schema version is stamped by the payload-schema registry, which knows which
// schema produced the diagnostic.
func payloadDiagnostic(field, constraint, valueClass string) models.FieldDiagnostic {
	return models.FieldDiagnostic{
		Field:      field,
		Constraint: constraint,
		ValueClass: valueClass,
		SafeAction: models.FieldDiagnosticCorrectInput,
	}
}
