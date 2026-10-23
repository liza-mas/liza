package archguard

// areaA2 is the guard data of area A2 (Platform adapters and distribution): written only by this area's
// scopes (ADR-0218). Ratchet entries may only be removed or lowered.
var areaA2 = Area{
	ID:        "A2",
	Layers:    []LayerEntry{},
	Allowlist: []AllowEntry{},
	Ratchet: []RatchetEntry{
		{Key: Key{File: "internal/embedded/embedded.go"}, Ceiling: 1842},
		{Key: Key{File: "internal/pairingindex/hooks.go"}, Ceiling: 1210},
		{Key: Key{File: "internal/providers/catalog.go"}, Ceiling: 804},
		{Key: Key{File: "internal/scipsearch/scipsearch.go"}, Ceiling: 1564},
		{Key: Key{File: "internal/semble/semble.go"}, Ceiling: 804},
		{Key: Key{File: "internal/toolchain/catalog.go", Func: "Catalog"}, Ceiling: 153},
		{Key: Key{File: "internal/toolresult/claude.go", Func: "ClaudePostToolResult"}, Ceiling: 107},
		{Key: Key{File: "internal/updater/updater.go"}, Ceiling: 1323},
	},
	S2: []S2Entry{},
}
