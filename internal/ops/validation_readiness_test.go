package ops

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/sessionvalidation"
)

// The readiness vocabulary reaches the persisted record, the alert and the
// error text as plain strings; the expectations below are independent literals.
func TestValidationReadinessVocabularyKeepsWireSpelling(t *testing.T) {
	t.Parallel()
	codes := []struct {
		code validationCode
		want string
	}{
		{validationCodeRegistrationRequired, "registration_required"},
		{validationCodeWorktreeUnavailable, "worktree_unavailable"},
		{validationCodePipelineUnavailable, "pipeline_unavailable"},
		{validationCodeIntegrationUnavailable, "integration_unavailable"},
		{validationCodeConfigurationChanged, "configuration_changed"},
		{validationCodeCurrentSessionRequired, "current_session_required"},
		{validationCodeRetryPending, "retry_pending"},
		{validationCodeArtifactUnsupported, "artifact_unsupported"},
		{validationCodeExecutionPolicyRequired, "execution_policy_required"},
		{validationCodeContextChanged, "context_changed"},
		{validationCodeOwnershipChanged, "ownership_changed"},
		{validationCodeCommitChanged, "commit_changed"},
		{validationCodeIntegrationChanged, "integration_changed"},
		{validationCodeCandidateChanged, "candidate_changed"},
		{validationCodeExecutableOwnershipRequired, "executable_ownership_required"},
	}
	for _, c := range codes {
		if string(c.code) != c.want {
			t.Errorf("code constant = %q, want %q", c.code, c.want)
		}
		want := "validation preflight: " + c.want
		if c.want == "artifact_unsupported" {
			want = "validation preflight: artifact_unsupported: validation artifacts are not implemented in this version"
		}
		err := validationError(c.code)
		if err.Error() != want {
			t.Errorf("validationError(%q) = %q, want %q", c.want, err.Error(), want)
		}
		if !errors.Is(err, sessionvalidation.ErrPreflight) {
			t.Errorf("validationError(%q) is not a preflight failure", c.want)
		}
	}
	for got, want := range map[string]string{
		string(validationResultPassed):             "passed",
		string(validationResultFailed):             "failed",
		string(validationMethodDirect):             "direct",
		validationExecutionLocal:                   "local",
		validationExecutionArtifactOnly:            "artifact-only",
		ValidationPreflightFailedCategory:          "VALIDATION PREFLIGHT FAILED",
		string(sessionvalidation.CodeRetryPending): "retry_pending",
	} {
		if got != want {
			t.Errorf("constant = %q, want %q", got, want)
		}
	}
}

// recordObservation sets only sanitized fields: a probe failure keeps the
// probe's code and indexes, a local refusal its own code, a failure of an
// unknown shape the generic code, and a pass nothing but its result.
func TestRecordObservationProjectsSanitizedFields(t *testing.T) {
	t.Parallel()
	probe := &sessionvalidation.Error{Code: sessionvalidation.CodeEnvironmentMissing, CommandIndex: 2, CheckIndex: 1, Variable: "NEEDED_VAR"}
	tests := []struct {
		name string
		err  error
		want models.ValidationReadiness
	}{
		{"pass", nil, models.ValidationReadiness{Result: "passed"}},
		{"probe failure", probe, models.ValidationReadiness{Result: "failed", Code: "environment_missing", CommandIndex: 2, CheckIndex: 1, Variable: "NEEDED_VAR"}},
		{"wrapped probe failure", fmt.Errorf("wrapped: %w", probe), models.ValidationReadiness{Result: "failed", Code: "environment_missing", CommandIndex: 2, CheckIndex: 1, Variable: "NEEDED_VAR"}},
		{"local refusal", validationError(validationCodeConfigurationChanged), models.ValidationReadiness{Result: "failed", Code: "configuration_changed"}},
		{"unknown failure", errors.New("raw process error"), models.ValidationReadiness{Result: "failed", Code: "prerequisite_failed"}},
	}
	for _, tt := range tests {
		p := &ValidationPreflight{}
		p.recordObservation(tt.err)
		if p.record != tt.want {
			t.Errorf("%s: record = %+v, want %+v", tt.name, p.record, tt.want)
		}
	}
}

// An unchanged failure is one condition; a changed code, check, variable or
// contract is a new one. A different timestamp or fingerprint is not.
func TestSameValidationObservationComparesWhatAnOperatorRepairs(t *testing.T) {
	t.Parallel()
	base := models.ValidationReadiness{Result: "failed", Code: "environment_missing", CommandIndex: 1, CheckIndex: 2, Variable: "V", Digest: "d", Fingerprint: "f1", CheckedAt: time.Unix(1, 0)}
	same := base
	same.Fingerprint, same.CheckedAt, same.Commit = "f2", time.Unix(2, 0), "other"
	if !sameValidationObservation(base, same) {
		t.Error("a new timestamp, fingerprint or commit made the same failure a new condition")
	}
	for name, mutate := range map[string]func(*models.ValidationReadiness){
		"result":   func(r *models.ValidationReadiness) { r.Result = "passed" },
		"code":     func(r *models.ValidationReadiness) { r.Code = "executable_missing" },
		"command":  func(r *models.ValidationReadiness) { r.CommandIndex++ },
		"check":    func(r *models.ValidationReadiness) { r.CheckIndex++ },
		"variable": func(r *models.ValidationReadiness) { r.Variable = "W" },
		"digest":   func(r *models.ValidationReadiness) { r.Digest = "e" },
	} {
		changed := base
		mutate(&changed)
		if sameValidationObservation(base, changed) {
			t.Errorf("a changed %s was treated as the same condition", name)
		}
	}
}
