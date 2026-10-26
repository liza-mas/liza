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
	Ratchet: []RatchetEntry{
		{Key: Key{File: "internal/ops/init_project.go", Func: "InitProject"}, Ceiling: 229},
	},
	S2: []S2Entry{},
}

// TestAreaA7RatchetHoldsOnlyInitProject holds the A7 boundary condition: the one
// ratchet entry left is ops.InitProject, kept for C4 which deletes it; the
// allowlist (jsonout no longer imports ops) and S2 are empty.
func TestAreaA7RatchetHoldsOnlyInitProject(t *testing.T) {
	initProject := Key{File: "internal/ops/init_project.go", Func: "InitProject"}
	found := false
	for _, r := range areaA7.Ratchet {
		if r.Key == initProject {
			found = true
			continue
		}
		t.Errorf("A7 ratchet still holds %s (ceiling %d)", r.Key, r.Ceiling)
	}
	if !found {
		t.Errorf("A7 ratchet no longer holds %s, which C4 removes with the function", initProject)
	}
	for _, a := range areaA7.Allowlist {
		t.Errorf("A7 allowlist still holds %s -> %s", a.Importer, a.Imported)
	}
	for _, e := range areaA7.S2 {
		t.Errorf("A7 S2 holds %s", e.Key)
	}
}
