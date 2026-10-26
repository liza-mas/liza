package archguard

// areaA8 is the guard data of area A8 (Prompts): written only by this area's
// scopes (ADR-0218). Ratchet entries may only be removed or lowered.
var areaA8 = Area{
	ID: "A8",
	Layers: []LayerEntry{
		{Package: "internal/prompts", Layer: layerPrompts},
		{Package: "internal/prompts/rolebudget", Layer: layerTestSupport},
	},
	Allowlist: []AllowEntry{},
	Ratchet: []RatchetEntry{
		{Key: Key{File: "internal/agent/prompt.go"}, Ceiling: 1401},
		{Key: Key{File: "internal/agent/prompt.go", Func: "buildTaskRoleContextData"}, Ceiling: 179},
		{Key: Key{File: "internal/agent/reference_context.go", Func: "buildReferenceContextWithRepository"}, Ceiling: 223},
	},
	S2: []S2Entry{},
}
