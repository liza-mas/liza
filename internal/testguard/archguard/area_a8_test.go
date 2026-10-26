package archguard

import "testing"

// areaA8 is the guard data of area A8 (Prompts): written only by this area's
// scopes (ADR-0218). Ratchet entries may only be removed or lowered.
var areaA8 = Area{
	ID: "A8",
	Layers: []LayerEntry{
		{Package: "internal/prompts", Layer: layerPrompts},
		{Package: "internal/prompts/rolebudget", Layer: layerTestSupport},
	},
	Allowlist: []AllowEntry{},
	Ratchet:   []RatchetEntry{},
	S2:        []S2Entry{},
}

// TestAreaA8DataIsEmpty holds the A8 closure condition: the prompts-to-ops
// crossing is gone (the allowlist is empty), the three S1 offenders are under
// threshold (the ratchet is empty) and no data-like exception is needed (S2 is
// empty).
func TestAreaA8DataIsEmpty(t *testing.T) {
	for _, r := range areaA8.Ratchet {
		t.Errorf("A8 ratchet still holds %s (ceiling %d)", r.Key, r.Ceiling)
	}
	for _, a := range areaA8.Allowlist {
		t.Errorf("A8 allowlist still holds %s -> %s", a.Importer, a.Imported)
	}
	for _, e := range areaA8.S2 {
		t.Errorf("A8 S2 holds %s", e.Key)
	}
}
