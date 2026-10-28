package archguard

import "testing"

// areaA5 is the guard data of area A5 (Ops review and acceptance): written only by this area's
// scopes (ADR-0218). Ratchet entries may only be removed or lowered.
var areaA5 = Area{
	ID:        "A5",
	Layers:    []LayerEntry{},
	Allowlist: []AllowEntry{},
	Ratchet:   []RatchetEntry{},
	S2:        []S2Entry{},
}

// TestAreaA5RatchetIsEmpty holds the A5 boundary condition: every S1 offender
// of the area is gone, and any exception that remains is an S2 entry carrying
// the human ruling and rationale that admitted it.
func TestAreaA5RatchetIsEmpty(t *testing.T) {
	for _, r := range areaA5.Ratchet {
		t.Errorf("A5 ratchet still holds %s (ceiling %d)", r.Key, r.Ceiling)
	}
	for _, e := range areaA5.S2 {
		if e.Ruling == "" || e.Rationale == "" {
			t.Errorf("A5 S2 entry %s lacks its ruling or rationale", e.Key)
		}
	}
}
