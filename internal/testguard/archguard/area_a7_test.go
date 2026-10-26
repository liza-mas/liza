package archguard

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
