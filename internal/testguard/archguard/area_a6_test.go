package archguard

import "testing"

// areaA6 is the guard data of area A6 (Ops merge and integration): written only by this area's
// scopes (ADR-0218). Ratchet entries may only be removed or lowered.
var areaA6 = Area{
	ID:        "A6",
	Layers:    []LayerEntry{},
	Allowlist: []AllowEntry{},
	Ratchet:   []RatchetEntry{},
	S2:        []S2Entry{},
}

// TestAreaA6RatchetIsEmpty holds the A6 boundary condition: every S1 offender
// of the area is gone, and any exception that remains is an S2 entry carrying
// the human ruling and rationale that admitted it.
func TestAreaA6RatchetIsEmpty(t *testing.T) {
	for _, r := range areaA6.Ratchet {
		t.Errorf("A6 ratchet still holds %s (ceiling %d)", r.Key, r.Ceiling)
	}
	for _, e := range areaA6.S2 {
		if e.Ruling == "" || e.Rationale == "" {
			t.Errorf("A6 S2 entry %s lacks its ruling or rationale", e.Key)
		}
	}
}
