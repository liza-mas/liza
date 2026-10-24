package models

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The persisted vocabulary types (V6-V10) must stay plain defined strings: the
// blackboard writer treats a type with any of these methods as opaque (AC2).
func TestPersistedVocabularyTypesHaveNoSerializationMethods(t *testing.T) {
	forbidden := []string{
		"MarshalYAML", "UnmarshalYAML", "MarshalText", "UnmarshalText",
		"MarshalJSON", "UnmarshalJSON", "IsZero",
	}
	for _, typ := range []reflect.Type{
		reflect.TypeFor[TaskEventName](), reflect.TypeFor[AnomalyType](),
		reflect.TypeFor[RejectionCause](), reflect.TypeFor[RecoveryPath](), reflect.TypeFor[RestoreMode](),
		reflect.TypeFor[InheritMode](), reflect.TypeFor[DiscoverySeverity](), reflect.TypeFor[DiscoveryUrgency](),
	} {
		if typ.Kind() != reflect.String {
			t.Errorf("%s kind = %s, want string", typ, typ.Kind())
		}
		for _, candidate := range []reflect.Type{typ, reflect.PointerTo(typ)} {
			for _, name := range forbidden {
				if _, ok := candidate.MethodByName(name); ok {
					t.Errorf("%s has method %s", candidate, name)
				}
			}
		}
	}
}

func TestAnomalyTypeConstantsMatchAcceptedSet(t *testing.T) {
	wire := map[AnomalyType]string{
		AnomalyTypeRetryLoop:                "retry_loop",
		AnomalyTypeTradeOff:                 "trade_off",
		AnomalyTypeSpecAmbiguity:            "spec_ambiguity",
		AnomalyTypeExternalBlocker:          "external_blocker",
		AnomalyTypeAssumptionViolated:       "assumption_violated",
		AnomalyTypeScopeDeviation:           "scope_deviation",
		AnomalyTypeWorkaround:               "workaround",
		AnomalyTypeDebtCreated:              "debt_created",
		AnomalyTypeSpecChanged:              "spec_changed",
		AnomalyTypeHypothesisExhaustion:     "hypothesis_exhaustion",
		AnomalyTypeSpecGap:                  "spec_gap",
		AnomalyTypeReviewBudgetExhausted:    "review_budget_exhausted",
		AnomalyTypeReviewExhaustion:         "review_exhaustion",
		AnomalyTypeReviewerLoop:             "reviewer_loop",
		AnomalyTypeStaleVerdict:             "stale_verdict",
		AnomalyTypeSystemAmbiguity:          "system_ambiguity",
		AnomalyTypeProviderAuditDegraded:    "provider_audit_degraded",
		AnomalyTypeAgentDegraded:            "agent_degraded",
		AnomalyTypeSubmitVerdictFailed:      "submit_verdict_failed",
		AnomalyTypeReviewerClaimCircuitOpen: "reviewer_claim_circuit_open",
		AnomalyTypeObligationContentDrifted: "obligation_content_drifted",
		AnomalyTypePendingMergeStalled:      "pending_merge_stalled",
		AnomalyTypeRuntimeInputUnavailable:  "runtime_input_unavailable",
	}
	if len(wire) != 23 {
		t.Fatalf("declared %d anomaly types, want the 23 IsValidType accepted before typing", len(wire))
	}
	for typ, literal := range wire {
		if string(typ) != literal {
			t.Errorf("anomaly type constant = %q, want %q", typ, literal)
		}
		if !(&Anomaly{Type: typ}).IsValidType() {
			t.Errorf("IsValidType(%q) = false, want true", typ)
		}
	}
	for _, unknown := range []AnomalyType{"", "retry_loops", "RETRY_LOOP", "reviewer_claim_open"} {
		if (&Anomaly{Type: unknown}).IsValidType() {
			t.Errorf("IsValidType(%q) = true, want false", unknown)
		}
	}
}

func TestAnomalyViolationsUnknownTypeMessageUnchanged(t *testing.T) {
	violations := AnomalyViolations([]Anomaly{{Type: AnomalyTypeSpecGap}, {Type: "bogus_type"}})
	if len(violations) != 1 || violations[0].Err.Error() != "unknown anomaly type 'bogus_type' at index 1" {
		t.Fatalf("violations = %+v, want only the unknown-type message for index 1", violations)
	}
}

func TestDiscoveryVocabularyMatchesValidation(t *testing.T) {
	for severity, literal := range map[DiscoverySeverity]string{
		DiscoverySeverityCritical: "critical", DiscoverySeverityHigh: "high",
		DiscoverySeverityMedium: "medium", DiscoverySeverityLow: "low",
	} {
		if string(severity) != literal || !(&Discovery{Severity: severity}).IsValidSeverity() {
			t.Errorf("severity %q: literal %q, valid = %v", severity, literal, (&Discovery{Severity: severity}).IsValidSeverity())
		}
	}
	for urgency, literal := range map[DiscoveryUrgency]string{
		DiscoveryUrgencyImmediate: "immediate", DiscoveryUrgencyDeferred: "deferred",
	} {
		if string(urgency) != literal || !(&Discovery{Urgency: urgency}).IsValidUrgency() {
			t.Errorf("urgency %q: literal %q, valid = %v", urgency, literal, (&Discovery{Urgency: urgency}).IsValidUrgency())
		}
	}
	for _, invalid := range []string{"", "Critical", "urgent", "immediately"} {
		if (&Discovery{Severity: DiscoverySeverity(invalid)}).IsValidSeverity() {
			t.Errorf("severity %q accepted", invalid)
		}
		if (&Discovery{Urgency: DiscoveryUrgency(invalid)}).IsValidUrgency() {
			t.Errorf("urgency %q accepted", invalid)
		}
	}
}

func TestInheritModeVocabularyMatchesValidation(t *testing.T) {
	for mode, literal := range map[InheritMode]string{
		InheritModeAll: "all", InheritModeSelected: "selected", InheritModeNone: "none",
	} {
		if string(mode) != literal {
			t.Errorf("inherit mode constant = %q, want %q", mode, literal)
		}
	}
	selection := []InputSelection{{UpstreamTask: "plan-1", Outputs: []int{0}}}
	if err := ValidateInheritInputs(&InheritInputs{Mode: InheritModeSelected, Selections: selection}, 0); err != nil {
		t.Errorf("selected with a selection: %v", err)
	}
	for _, mode := range []InheritMode{InheritModeAll, InheritModeNone} {
		if err := ValidateInheritInputs(&InheritInputs{Mode: mode}, 0); err != nil {
			t.Errorf("mode %q: %v", mode, err)
		}
	}
	err := ValidateInheritInputs(&InheritInputs{Mode: "everything"}, 2)
	const want = `output[2].inherit_inputs: mode must be "all", "selected" or "none" (got "everything")`
	if err == nil || err.Error() != want {
		t.Fatalf("unknown mode error = %v, want %q", err, want)
	}
}

func TestRejectionVocabularyMatchesValidation(t *testing.T) {
	for cause, literal := range map[RejectionCause]string{
		RejectionCauseProductDefect: "product_defect", RejectionCauseCapabilityFailure: "capability_failure",
		RejectionCauseLifecycleRetry: "lifecycle_retry", RejectionCauseUnknown: "unknown",
	} {
		if string(cause) != literal || !IsKnownRejectionCause(cause) {
			t.Errorf("cause %q: literal %q, known = %v", cause, literal, IsKnownRejectionCause(cause))
		}
	}
	if IsKnownRejectionCause("toolchain_drift") {
		t.Error("unrecognized cause reported as known")
	}

	type pair struct {
		path RecoveryPath
		mode RestoreMode
	}
	for literal, want := range map[string]pair{
		"implementation_correction": {RecoveryImplementationCorrection, RestoreModeClaimable},
		"capability_reroute":        {RecoveryCapabilityReroute, RestoreModeAssign},
		"lifecycle_repair":          {RecoveryLifecycleRepair, RestoreModeAssign},
		"rescope":                   {RecoveryRescope, RestoreModeNone},
		"human_override":            {RecoveryHumanOverride, RestoreModeClaimable},
	} {
		if string(want.path) != literal || !IsRecoveryPath(want.path) || RejectionRCARestoreMode(want.path) != want.mode {
			t.Errorf("recovery path %q: literal %q, restore %q, want %q", want.path, literal, RejectionRCARestoreMode(want.path), want.mode)
		}
	}
	for literal, mode := range map[string]RestoreMode{"assign": RestoreModeAssign, "claimable": RestoreModeClaimable, "none": RestoreModeNone} {
		if string(mode) != literal {
			t.Errorf("restore mode constant = %q, want %q", mode, literal)
		}
	}
	for _, invalid := range []RecoveryPath{"", "teleport", "Rescope"} {
		if IsRecoveryPath(invalid) || RejectionRCARestoreMode(invalid) != "" {
			t.Errorf("recovery path %q accepted or mapped to a restore mode", invalid)
		}
	}
}

// The fingerprint is stored on tasks and compared across versions, so the
// bytes it hashes are pinned independently of the code that produces them. The
// JSON and digest below were captured from the string-typed implementation
// before RejectionCause existed.
func TestRejectionRCAFingerprintBytesPinned(t *testing.T) {
	const (
		wantJSON = `{"schema_version":1,"summary":"two defects, one flaky infra cause","contributions":[` +
			`{"rejection_index":1,"categories":["capability_failure","unknown"],"evidence":["review-1: mismatch"]},` +
			`{"rejection_index":2,"categories":null},` +
			`{"rejection_index":3,"categories":["lifecycle_retry","product_defect","toolchain_drift"],"evidence":["verdict 3: stale caller","lease expired"]}]}`
		wantDigest = "6f2ee2fb475d8a20e676506a68dff231a85f340bfe9a87fe48ddfa0ba30e8add"
	)
	request := RejectionRCARequest{
		SchemaVersion: 1,
		Summary:       "  two defects,\tone   flaky infra cause\n",
		Contributions: []RejectionRCAContribution{
			{
				RejectionIndex: 3,
				Categories:     []RejectionCause{"lifecycle_retry", " product_defect ", "toolchain_drift", "product_defect", "  "},
				Evidence:       []string{"  verdict 3:  stale   caller ", "", "lease expired"},
			},
			{RejectionIndex: 1, Categories: []RejectionCause{"capability_failure\t", "\nunknown"}, Evidence: []string{"review-1:   mismatch"}},
			{RejectionIndex: 2, Categories: []RejectionCause{" ", ""}},
		},
	}

	data, err := json.Marshal(NormalizeRejectionRCARequest(request))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != wantJSON {
		t.Fatalf("normalized request JSON changed\n got: %s\nwant: %s", data, wantJSON)
	}
	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); got != wantDigest {
		t.Fatalf("digest of normalized bytes = %s, want %s", got, wantDigest)
	}
	if got := RejectionRCAFingerprint(request); got != wantDigest {
		t.Fatalf("RejectionRCAFingerprint = %s, want %s", got, wantDigest)
	}
}

func TestRejectionRCACategoriesNormalizeToNilWhenEmpty(t *testing.T) {
	normalized := NormalizeRejectionRCARequest(RejectionRCARequest{Contributions: []RejectionRCAContribution{
		{RejectionIndex: 1, Categories: []RejectionCause{}},
		{RejectionIndex: 2, Categories: nil},
		{RejectionIndex: 3, Categories: []RejectionCause{" \t"}},
	}})
	for _, contribution := range normalized.Contributions {
		if contribution.Categories != nil {
			t.Errorf("contribution %d categories = %#v, want nil", contribution.RejectionIndex, contribution.Categories)
		}
	}
}

// Legacy values stay tolerated: a history event or rejection cause no constant
// names loads, and persists, byte for byte.
func TestLegacyUnknownVocabularyValuesSurviveYAMLRoundTrip(t *testing.T) {
	const source = "time: 2026-01-02T03:04:05Z\nevent: retired_event_name\n"
	var entry TaskHistoryEntry
	if err := yaml.Unmarshal([]byte(source), &entry); err != nil {
		t.Fatal(err)
	}
	if entry.Event != "retired_event_name" {
		t.Fatalf("event = %q, want the legacy name verbatim", entry.Event)
	}
	out, err := yaml.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != source {
		t.Fatalf("history entry re-encoded as\n%s\nwant\n%s", out, source)
	}

	const contribution = "rejection_index: 4\ncategories:\n    - toolchain_drift\n    - product_defect\n"
	var decoded RejectionRCAContribution
	if err := yaml.Unmarshal([]byte(contribution), &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded.Categories, []RejectionCause{"toolchain_drift", RejectionCauseProductDefect}) {
		t.Fatalf("categories = %#v, want the unknown cause retained", decoded.Categories)
	}
	if out, err = yaml.Marshal(decoded); err != nil || !strings.Contains(string(out), contribution) {
		t.Fatalf("contribution re-encoded as\n%s\nerr %v", out, err)
	}
}
