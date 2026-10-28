package archguard

// areaA3 is the guard data of area A3 (Ops kernel and execution lifecycle): written only by this area's
// scopes (ADR-0218). Ratchet entries may only be removed or lowered.
var areaA3 = Area{
	ID: "A3",
	Layers: []LayerEntry{
		{Package: "internal/ops", Layer: layerApplication},
	},
	Allowlist: []AllowEntry{},
	Ratchet:   []RatchetEntry{},
	S2:        []S2Entry{},
}
