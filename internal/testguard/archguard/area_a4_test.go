package archguard

import "testing"

// areaA4 is the guard data of area A4 (Ops planning and task graph): written only by this area's
// scopes (ADR-0218). Ratchet entries may only be removed or lowered.
var areaA4 = Area{
	ID:        "A4",
	Layers:    []LayerEntry{},
	Allowlist: []AllowEntry{},
	Ratchet:   []RatchetEntry{},
	S2:        []S2Entry{},
}

// TestAreaA4RatchetIsEmpty holds the A4 boundary condition: every S1 offender
// of the area is gone, and any exception that remains is an S2 entry carrying
// the human ruling and rationale that admitted it.
func TestAreaA4RatchetIsEmpty(t *testing.T) {
	for _, r := range areaA4.Ratchet {
		t.Errorf("A4 ratchet still holds %s (ceiling %d)", r.Key, r.Ceiling)
	}
	for _, e := range areaA4.S2 {
		if e.Ruling == "" || e.Rationale == "" {
			t.Errorf("A4 S2 entry %s lacks its ruling or rationale", e.Key)
		}
	}
}
