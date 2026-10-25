package archguard

// areaA5 is the guard data of area A5 (Ops review and acceptance): written only by this area's
// scopes (ADR-0218). Ratchet entries may only be removed or lowered.
var areaA5 = Area{
	ID:        "A5",
	Layers:    []LayerEntry{},
	Allowlist: []AllowEntry{},
	Ratchet:   []RatchetEntry{},
	S2:        []S2Entry{},
}
