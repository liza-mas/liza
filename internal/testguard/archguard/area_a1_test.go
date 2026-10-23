package archguard

// areaA1 is the guard data of area A1 (State core): written only by this area's
// scopes (ADR-0218). Ratchet entries may only be removed or lowered.
var areaA1 = Area{
	ID:        "A1",
	Layers:    []LayerEntry{},
	Allowlist: []AllowEntry{},
	Ratchet: []RatchetEntry{
		{Key: Key{File: "internal/db/yaml_write.go", Func: "projectedWriteType"}, Ceiling: 101},
		{Key: Key{File: "internal/models/task.go"}, Ceiling: 1375},
		{Key: Key{File: "internal/pipeline/config.go", Func: "validate"}, Ceiling: 122},
		{Key: Key{File: "internal/pipeline/resolver.go"}, Ceiling: 1075},
		{Key: Key{File: "internal/referencecontract/contract.go", Func: "Parse"}, Ceiling: 112},
		{Key: Key{File: "internal/referencecontract/render.go", Func: "RenderCarriers"}, Ceiling: 169},
		{Key: Key{File: "internal/referencecontract/resolve.go", Func: "ResolveDirectReferences"}, Ceiling: 104},
		{Key: Key{File: "internal/statevalidate/validate_task.go"}, Ceiling: 1058},
		{Key: Key{File: "internal/statevalidate/validate_task.go", Func: "validateStatusFields"}, Ceiling: 117},
		{Key: Key{File: "internal/statevalidate/validate_task.go", Func: "validateTaskInvariants"}, Ceiling: 154},
	},
	S2: []S2Entry{},
}
