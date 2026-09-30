package db_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/statevalidate"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// These tests pin ADR-0166: a Modify transaction may not persist an anomaly
// violation it introduces, while violations its pre-image already had never
// block it and may be repaired one detail at a time (ADR-0165).

func validRetryLoop() models.Anomaly {
	return models.Anomaly{
		Timestamp: time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC),
		Reporter:  "coder-1",
		Type:      "retry_loop",
		Details:   map[string]any{"count": 3, "error_pattern": "timeout"},
	}
}

// newAnomalyFixture writes a state holding the given anomalies through Write,
// which is unchecked, so a legacy invalid record can be seeded.
func newAnomalyFixture(t *testing.T, anomalies ...models.Anomaly) (*db.Blackboard, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "state.yaml")
	bb := db.New(path)
	if err := bb.Write(&models.State{Goal: models.Goal{Description: "anomalies"}, Anomalies: anomalies}); err != nil {
		t.Fatal(err)
	}
	return bb, path
}

func readBytes(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// requireAnomalyRefused asserts the transaction was refused naming want and
// that the state file is byte-for-byte unchanged.
func requireAnomalyRefused(t *testing.T, bb *db.Blackboard, path string, fn func(*models.State) error, want string) {
	t.Helper()
	before := readBytes(t, path)
	err := bb.Modify(fn)
	if err == nil {
		t.Fatalf("Modify() = nil, want the anomaly write refused (%s)", want)
	}
	if !strings.Contains(err.Error(), "anomaly write refused") || !strings.Contains(err.Error(), want) {
		t.Fatalf("Modify() error = %v, want an anomaly write refusal naming %q", err, want)
	}
	if after := readBytes(t, path); string(after) != string(before) {
		t.Fatal("state file changed although the transaction was refused")
	}
}

// requireAnomalyAllowed asserts the transaction succeeded and returns the
// reloaded state, so callers can check the change actually persisted.
func requireAnomalyAllowed(t *testing.T, bb *db.Blackboard, fn func(*models.State) error) *models.State {
	t.Helper()
	if err := bb.Modify(fn); err != nil {
		t.Fatalf("Modify() error = %v, want the transaction allowed", err)
	}
	state, err := bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	return state
}

// R1: a new record without its type's required details is refused.
func TestModify_RefusesNewAnomalyMissingDetails(t *testing.T) {
	bb, path := newAnomalyFixture(t)
	requireAnomalyRefused(t, bb, path, func(s *models.State) error {
		s.Anomalies = append(s.Anomalies, models.Anomaly{Timestamp: time.Now().UTC(), Reporter: "coder-1", Type: "retry_loop", Details: map[string]any{}})
		return nil
	}, "retry_loop anomaly at index 0 missing required details (count)")
}

// R2: a legacy invalid record does not block an unrelated change.
func TestModify_AllowsUnrelatedChangeOverLegacyInvalidAnomaly(t *testing.T) {
	bb, _ := newAnomalyFixture(t, testhelpers.LegacyPendingMergeStallAnomaly())
	state := requireAnomalyAllowed(t, bb, func(s *models.State) error {
		s.Goal.Description = "changed"
		return nil
	})
	if state.Goal.Description != "changed" || len(state.Anomalies) != 1 {
		t.Fatalf("persisted goal %q with %d anomalies, want the change and the legacy record kept", state.Goal.Description, len(state.Anomalies))
	}
}

// R3: clearing a required detail of a valid record in place is refused.
func TestModify_RefusesInPlaceClearingOfRequiredDetail(t *testing.T) {
	bb, path := newAnomalyFixture(t, validRetryLoop())
	requireAnomalyRefused(t, bb, path, func(s *models.State) error {
		delete(s.Anomalies[0].Details, "error_pattern")
		return nil
	}, "retry_loop anomaly at index 0 missing required details (error_pattern)")
}

// R4: a required detail holding a nil pointer persists as null, so it is
// missing.
func TestModify_RefusesNilPointerRequiredDetail(t *testing.T) {
	bb, path := newAnomalyFixture(t)
	requireAnomalyRefused(t, bb, path, func(s *models.State) error {
		record := validRetryLoop()
		record.Details["error_pattern"] = (*string)(nil)
		s.Anomalies = append(s.Anomalies, record)
		return nil
	}, "retry_loop anomaly at index 0 missing required details (error_pattern)")
}

// R4b: nil and empty collections persist as [] and {}, so they are present,
// and the reloaded state satisfies validate.
func TestModify_AllowsEmptyCollectionDetailsAndTheyRoundTrip(t *testing.T) {
	bb, _ := newAnomalyFixture(t)
	state := requireAnomalyAllowed(t, bb, func(s *models.State) error {
		record := validRetryLoop()
		record.Details["count"] = []string(nil)
		record.Details["error_pattern"] = map[string]any{}
		s.Anomalies = append(s.Anomalies, record)
		return nil
	})
	if len(state.Anomalies) != 1 {
		t.Fatalf("persisted %d anomalies, want the one appended", len(state.Anomalies))
	}
	if err := statevalidate.ValidateAnomalies(state, "", true); err != nil {
		t.Fatalf("reloaded ValidateAnomalies() = %v, want the round-tripped details present", err)
	}
}

// R5: an unknown type is refused.
func TestModify_RefusesUnknownAnomalyType(t *testing.T) {
	bb, path := newAnomalyFixture(t)
	requireAnomalyRefused(t, bb, path, func(s *models.State) error {
		s.Anomalies = append(s.Anomalies, models.Anomaly{Timestamp: time.Now().UTC(), Reporter: "coder-1", Type: "not_a_type"})
		return nil
	}, "unknown anomaly type 'not_a_type' at index 0")
}

// R6: filling one of a legacy record's two missing details is a partial
// repair, allowed as ADR-0165 promises.
func TestModify_AllowsPartialRepairOfLegacyAnomaly(t *testing.T) {
	bb, _ := newAnomalyFixture(t, testhelpers.LegacyPendingMergeStallAnomaly())
	state := requireAnomalyAllowed(t, bb, func(s *models.State) error {
		s.Anomalies[0].Details["count"] = 3
		return nil
	})
	if len(state.Anomalies) != 1 || state.Anomalies[0].Details["count"] != 3 {
		t.Fatalf("persisted anomalies %#v, want the legacy record with count 3", state.Anomalies)
	}
}

// R7: a second copy of a legacy invalid record adds violations of its own.
func TestModify_RefusesDuplicateOfLegacyInvalidAnomaly(t *testing.T) {
	legacy := testhelpers.LegacyPendingMergeStallAnomaly()
	bb, path := newAnomalyFixture(t, legacy)
	requireAnomalyRefused(t, bb, path, func(s *models.State) error {
		s.Anomalies = append(s.Anomalies, testhelpers.LegacyPendingMergeStallAnomaly())
		return nil
	}, "retry_loop anomaly at index 1 missing required details (count)")
}
