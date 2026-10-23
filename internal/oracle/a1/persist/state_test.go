package persist

import (
	"path/filepath"
	"testing"
)

func TestOracleStateRoundTrip(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "state.yaml")
	before := fixture(t, "state.yaml")
	writeBytes(t, path, before)
	if err := persistState(path, ""); err != nil {
		t.Fatalf("persist fixture: %v", err)
	}
	first := readBytes(t, path)
	rawStateCheck(t, "state-roundtrip.txt", before, first)
	if err := persistState(path, ""); err != nil {
		t.Fatalf("persist second load: %v", err)
	}
	requireSame(t, "second persistence", first, readBytes(t, path))

	// The payload is short but transcript-shaped: size alone cannot refuse it.
	err := persistState(path, string(fixture(t, "transcript.txt")))
	requireRefusal(t, "state transcript", err)
	after := readBytes(t, path)
	requireSame(t, "refused state", first, after)
	rawStateCheck(t, "state-refusal.txt", first, jsonBytes(t, struct {
		Operation string
		Refused   bool
		State     string
	}{"state transcript", err != nil, string(after)}))
}
