package sessionvalidation

import (
	"errors"
	"strings"
	"testing"
)

func TestErrorFormatsAcceptedAndUnknownCodes(t *testing.T) {
	tests := []struct {
		name string
		code ErrorCode
		want string
	}{
		{"env_file_unavailable", CodeEnvFileUnavailable, "env_file_unavailable"},
		{"invalid_contract", CodeInvalidContract, "invalid_contract"},
		{"environment_missing", CodeEnvironmentMissing, "environment_missing"},
		{"executable_missing", CodeExecutableMissing, "executable_missing"},
		{"probe_start_failed", CodeProbeStartFailed, "probe_start_failed"},
		{"probe_failed", CodeProbeFailed, "probe_failed"},
		{"probe_timeout", CodeProbeTimeout, "probe_timeout"},
		{"preflight_canceled", CodePreflightCanceled, "preflight_canceled"},
		{"execution_policy_required", CodeExecutionPolicyRequired, "execution_policy_required"},
		{"artifact_unsupported", CodeArtifactUnsupported, "artifact_unsupported"},
		{"context_unavailable", CodeContextUnavailable, "context_unavailable"},
		{"context_changed", CodeContextChanged, "context_changed"},
		{"retry_pending", CodeRetryPending, "retry_pending"},
		{"runtime_input_scrubbed", CodeRuntimeInputScrubbed, "runtime_input_scrubbed"},
		{"prerequisite_failed", CodePrerequisiteFailed, "prerequisite_failed"},
		{"unknown falls back", ErrorCode("CANARY-UNKNOWN"), "prerequisite_failed"},
		{"empty falls back", ErrorCode(""), "prerequisite_failed"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := &Error{Code: tc.code, CommandIndex: 2, CheckIndex: 3}
			want := "validation preflight: " + tc.want + " (command=2 check=3); repair the configured execution context and retry"
			if got := err.Error(); got != want {
				t.Fatalf("message = %q, want %q", got, want)
			}
			if !errors.Is(err, ErrPreflight) {
				t.Fatal("errors.Is(ErrPreflight) = false")
			}
			if strings.Contains(err.Error(), "CANARY") {
				t.Fatal("unknown code disclosed")
			}
		})
	}
}

func TestErrorCodeValuesMatchWireStrings(t *testing.T) {
	// Codes persist in validation preflight records; the spelling is the contract.
	for code, want := range map[ErrorCode]string{
		CodeEnvFileUnavailable:      "env_file_unavailable",
		CodeInvalidContract:         "invalid_contract",
		CodeEnvironmentMissing:      "environment_missing",
		CodeExecutableMissing:       "executable_missing",
		CodeProbeStartFailed:        "probe_start_failed",
		CodeProbeFailed:             "probe_failed",
		CodeProbeTimeout:            "probe_timeout",
		CodePreflightCanceled:       "preflight_canceled",
		CodeExecutionPolicyRequired: "execution_policy_required",
		CodeArtifactUnsupported:     "artifact_unsupported",
		CodeContextUnavailable:      "context_unavailable",
		CodeContextChanged:          "context_changed",
		CodeRetryPending:            "retry_pending",
		CodeRuntimeInputScrubbed:    "runtime_input_scrubbed",
		CodePrerequisiteFailed:      "prerequisite_failed",
	} {
		if string(code) != want {
			t.Fatalf("code %q, want %q", code, want)
		}
	}
}

func TestErrorUnsafeVariableOmitted(t *testing.T) {
	err := &Error{Code: CodeEnvironmentMissing, Variable: "CANARY\nVALUE"}
	if strings.Contains(err.Error(), "CANARY") {
		t.Fatal("unsafe variable disclosed")
	}
	safe := &Error{Code: CodeEnvironmentMissing, Variable: "SAFE_NAME"}
	if !strings.HasSuffix(safe.Error(), "; required variable=SAFE_NAME") {
		t.Fatalf("safe variable missing: %q", safe.Error())
	}
}
