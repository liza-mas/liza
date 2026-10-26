package archguard

import "testing"

// areaA7 is the guard data of area A7 (Presentation): written only by this area's
// scopes (ADR-0218). Ratchet entries may only be removed or lowered.
var areaA7 = Area{
	ID: "A7",
	Layers: []LayerEntry{
		{Package: "cmd/liza", Layer: layerPresentation},
		{Package: "internal/commands", Layer: layerPresentation},
		{Package: "internal/interactive", Layer: layerPresentation},
		{Package: "internal/jsonout", Layer: layerPresentation},
		{Package: "internal/tui", Layer: layerPresentation},
	},
	Allowlist: []AllowEntry{},
	Ratchet:   []RatchetEntry{},
	S2:        []S2Entry{},
}

// TestAreaA7RatchetAllowlistAndS2AreEmpty holds the A7 closure condition: C4
// deleted the last ratchet entry (the duplicate ops initialization function), and
// the allowlist (jsonout no longer imports ops) and S2 are empty.
func TestAreaA7RatchetAllowlistAndS2AreEmpty(t *testing.T) {
	for _, r := range areaA7.Ratchet {
		t.Errorf("A7 ratchet still holds %s (ceiling %d)", r.Key, r.Ceiling)
	}
	for _, a := range areaA7.Allowlist {
		t.Errorf("A7 allowlist still holds %s -> %s", a.Importer, a.Imported)
	}
	for _, e := range areaA7.S2 {
		t.Errorf("A7 S2 holds %s", e.Key)
	}
}
