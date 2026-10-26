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
		{Key: Key{File: "internal/commands/watch.go"}, Ceiling: 2084},
		{Key: Key{File: "internal/commands/watch.go", Func: "RunAutoRepairAgentPool"}, Ceiling: 133},
		{Key: Key{File: "internal/ops/init_project.go", Func: "InitProject"}, Ceiling: 229},
		{Key: Key{File: "internal/tui/update.go", Func: "Model.Update"}, Ceiling: 196},
		{Key: Key{File: "internal/tui/update.go", Func: "Model.executeInlineAction"}, Ceiling: 108},
		{Key: Key{File: "internal/tui/view.go"}, Ceiling: 978},
		{Key: Key{File: "internal/tui/view.go", Func: "Model.View"}, Ceiling: 106},
		{Key: Key{File: "internal/tui/view.go", Func: "Model.renderAgentPanel"}, Ceiling: 157},
		{Key: Key{File: "internal/tui/view.go", Func: "Model.renderTaskPanel"}, Ceiling: 208},
	},
	S2: []S2Entry{},
}
