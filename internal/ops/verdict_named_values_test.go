package ops

import (
	"reflect"
	"testing"
	"time"
)

// TestVerdictNamedValuesKeepTheirValues compares each K7 named value of the
// verdict files with the literal it replaced, independently of the constant.
func TestVerdictNamedValuesKeepTheirValues(t *testing.T) {
	for name, c := range map[string]struct {
		got, want any
	}{
		"submittedVerdictApproved":        {string(submittedVerdictApproved), "APPROVED"},
		"submittedVerdictRejected":        {string(submittedVerdictRejected), "REJECTED"},
		"verdictImpactExtraKey":           {verdictImpactExtraKey, "impact"},
		"rejectionRCAThresholdKey":        {rejectionRCAThresholdKey, "threshold"},
		"rejectionRCARejectionCountKey":   {rejectionRCARejectionCountKey, "rejection_count"},
		"pendingVerdictDirName":           {pendingVerdictDirName, "pending-verdicts"},
		"pendingVerdictDirMode":           {int(pendingVerdictDirMode), 0o700},
		"pendingVerdictRetryInitialDelay": {pendingVerdictRetryInitialDelay, 100 * time.Millisecond},
		"pendingVerdictRetryMaxDelay":     {pendingVerdictRetryMaxDelay, 5 * time.Second},
		"verdictFailureErrorLimit":        {verdictFailureErrorLimit, 2048},
		"verdictFailureStackLimit":        {verdictFailureStackLimit, 4096},
		"verdictFailureAnomalyErrorLimit": {verdictFailureAnomalyErrorLimit, 1024},
		"impactOrder":                     {impactOrder, map[string]int{"standard": 0, "significant": 1, "architecture": 2}},
	} {
		if !reflect.DeepEqual(c.got, c.want) {
			t.Errorf("%s = %v, want %v", name, c.got, c.want)
		}
	}
}

// TestVerdictRequestPayloadKeepsItsIdentityFields pins the intent a verdict
// request hashes: the direct call and the durable outbox share this builder, so
// a drifted key would give a replayed verdict a different payload digest.
func TestVerdictRequestPayloadKeepsItsIdentityFields(t *testing.T) {
	got := verdictRequestPayload("APPROVED", "why", "significant", "abc")
	want := map[string]string{"verdict": "APPROVED", "reason": "why", "impact": "significant", "review_commit": "abc"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("verdict request payload = %v, want %v", got, want)
	}
}
