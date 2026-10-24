package archguard

// areaA1 is the guard data of area A1 (State core): written only by this area's
// scopes (ADR-0218). Ratchet entries may only be removed or lowered.
var areaA1 = Area{
	ID: "A1",
	Layers: []LayerEntry{
		{Package: "internal/alerts", Layer: layerInfrastructure},
		{Package: "internal/analysis", Layer: layerInfrastructure},
		{Package: "internal/archiveobject", Layer: layerInfrastructure},
		{Package: "internal/atomicfile", Layer: layerInfrastructure},
		{Package: "internal/brand", Layer: layerLeaf},
		{Package: "internal/db", Layer: layerInfrastructure},
		{Package: "internal/envgate", Layer: layerInfrastructure},
		{Package: "internal/errors", Layer: layerLeaf},
		{Package: "internal/filelock", Layer: layerInfrastructure},
		{Package: "internal/identity", Layer: layerLeaf},
		{Package: "internal/log", Layer: layerLeaf},
		{Package: "internal/models", Layer: layerDomain},
		{Package: "internal/paths", Layer: layerLeaf},
		{Package: "internal/payloadschema", Layer: layerInfrastructure},
		{Package: "internal/pipeline", Layer: layerDomain},
		{Package: "internal/referencecontract", Layer: layerDomain},
		{Package: "internal/render", Layer: layerLeaf},
		{Package: "internal/rolemodels", Layer: layerInfrastructure},
		{Package: "internal/roles", Layer: layerDomain},
		{Package: "internal/runtimeinputs", Layer: layerInfrastructure},
		{Package: "internal/statehygiene", Layer: layerInfrastructure},
		{Package: "internal/statevalidate", Layer: layerInfrastructure},
		{Package: "internal/taskkind", Layer: layerDomain},
		{Package: "internal/usage", Layer: layerInfrastructure},
	},
	Allowlist: []AllowEntry{},
	Ratchet: []RatchetEntry{
		{Key: Key{File: "internal/db/yaml_write.go", Func: "projectedWriteType"}, Ceiling: 101},
		{Key: Key{File: "internal/referencecontract/contract.go", Func: "Parse"}, Ceiling: 112},
		{Key: Key{File: "internal/referencecontract/render.go", Func: "RenderCarriers"}, Ceiling: 169},
		{Key: Key{File: "internal/referencecontract/resolve.go", Func: "ResolveDirectReferences"}, Ceiling: 104},
	},
	S2: []S2Entry{},
}
