package archguard

// areaA2 is the guard data of area A2 (Platform adapters and distribution): written only by this area's
// scopes (ADR-0218). Ratchet entries may only be removed or lowered.
var areaA2 = Area{
	ID: "A2",
	Layers: []LayerEntry{
		{Package: "internal/bash-policy-cli", Layer: layerInfrastructure},
		{Package: "internal/brandrender", Layer: layerInfrastructure},
		{Package: "internal/brandrender/cmd/sync-embedded", Layer: layerInfrastructure},
		{Package: "internal/codexconfig", Layer: layerInfrastructure},
		{Package: "internal/embedded", Layer: layerInfrastructure},
		{Package: "internal/functionalclusters", Layer: layerInfrastructure},
		{Package: "internal/git", Layer: layerInfrastructure},
		{Package: "internal/gitbash", Layer: layerInfrastructure},
		{Package: "internal/gitenv", Layer: layerInfrastructure},
		{Package: "internal/initcheck", Layer: layerInfrastructure},
		{Package: "internal/pairingindex", Layer: layerInfrastructure},
		{Package: "internal/precommit", Layer: layerInfrastructure},
		{Package: "internal/procscan", Layer: layerInfrastructure},
		{Package: "internal/projectdetect", Layer: layerInfrastructure},
		{Package: "internal/providers", Layer: layerInfrastructure},
		{Package: "internal/scipsearch", Layer: layerInfrastructure},
		{Package: "internal/secretmask", Layer: layerInfrastructure},
		{Package: "internal/semble", Layer: layerInfrastructure},
		{Package: "internal/sessionvalidation", Layer: layerInfrastructure},
		{Package: "internal/stacklit", Layer: layerInfrastructure},
		{Package: "internal/subprocess", Layer: layerInfrastructure},
		{Package: "internal/termutil", Layer: layerInfrastructure},
		{Package: "internal/toolchain", Layer: layerInfrastructure},
		{Package: "internal/toolresult", Layer: layerInfrastructure},
		{Package: "internal/toolresultacp", Layer: layerInfrastructure},
		{Package: "internal/updater", Layer: layerInfrastructure},
		{Package: "internal/worktreeexclude", Layer: layerInfrastructure},
	},
	Allowlist: []AllowEntry{},
	Ratchet:   []RatchetEntry{},
	S2: []S2Entry{
		{
			Key:       Key{File: "internal/toolchain/catalog.go", Func: "Catalog"},
			Category:  S2Table,
			Rationale: "declarative tool catalog: one literal entry per tool",
		},
	},
}
