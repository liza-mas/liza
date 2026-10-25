package lifecycle

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

// TestOracleAssessBlockedVariants freezes the unchanged assessment (NO_CHANGE,
// no history, receipt or alert) when the previous assessment's awaited set or
// human ask is carried instead of restated, which TestOracleAssessBlocked does
// not reach. These scenarios use CLI and state only, no Linux process facility
// (D53), so none is Linux-scoped.
func TestOracleAssessBlockedVariants(t *testing.T) {
	assess := func(extra ...string) []string {
		return append([]string{"assess-blocked", "task-1", "--note", "oracle-note"}, extra...)
	}
	runCases(t, "assess-blocked",
		lifecycleCase{name: "unchanged-no-change", fixture: "blocked", caller: "orchestrator-1", invocations: [][]string{assess(), assess()}, mutating: true},
		lifecycleCase{name: "carried-awaited-no-change", fixture: "blocked-pending-task", caller: "orchestrator-1", invocations: [][]string{assess("--awaits", "task-2"), assess()}, mutating: true},
		lifecycleCase{name: "carried-human-ask-no-change", fixture: "blocked", caller: "orchestrator-1", invocations: [][]string{assess("--human-action", "operator must grant repository access"), assess()}, mutating: true})
}

// The size of a suppressed assessment entry varies with the nanosecond digits of
// its timestamp, so both output forms mask it; every other number stays asserted.
func TestLifecycleSuppressedEntryMask(t *testing.T) {
	m := newMask(time.Now(), nil, nil, 12345)
	if got := m.text("outcome: NO_CHANGE\nsuppressed_entry_bytes: 227\nother_bytes: 227\n"); got != "outcome: NO_CHANGE\nsuppressed_entry_bytes: <entry-bytes>\nother_bytes: 227\n" {
		t.Fatalf("human form: %q", got)
	}
	got := m.document(map[string]any{"suppressed_entry_bytes": json.Number("228"), "other_bytes": json.Number("228")})
	if !reflect.DeepEqual(got, map[string]any{"suppressed_entry_bytes": "<entry-bytes>", "other_bytes": json.Number("228")}) {
		t.Fatalf("JSON form: %v", got)
	}
}
