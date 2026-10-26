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
		{Key: Key{File: "cmd/liza/cmd_launch.go"}, Ceiling: 1159},
		{Key: Key{File: "cmd/liza/cmd_system.go"}, Ceiling: 827},
		{Key: Key{File: "cmd/liza/cmd_task.go"}, Ceiling: 2005},
		{Key: Key{File: "cmd/liza/cmd_task.go", Func: "init"}, Ceiling: 199},
		{Key: Key{File: "cmd/liza/cmd_tool_result.go", Func: "newToolResultCmd"}, Ceiling: 134},
		{Key: Key{File: "internal/commands/init.go"}, Ceiling: 1684},
		{Key: Key{File: "internal/commands/init.go", Func: "InitCommandWithConfig"}, Ceiling: 471},
		{Key: Key{File: "internal/commands/init.go", Func: "InitPairingCommand"}, Ceiling: 182},
		{Key: Key{File: "internal/commands/init.go", Func: "createContractSymlinksForProviders"}, Ceiling: 192},
		{Key: Key{File: "internal/commands/inspect_tasks.go"}, Ceiling: 1018},
		{Key: Key{File: "internal/commands/repair_agent_pool.go"}, Ceiling: 1252},
		{Key: Key{File: "internal/commands/repair_agent_pool.go", Func: "FindRoleCapacity"}, Ceiling: 234},
		{Key: Key{File: "internal/commands/repair_agent_pool.go", Func: "RepairAgentPool"}, Ceiling: 139},
		{Key: Key{File: "internal/commands/setup.go", Func: "SetupCommand"}, Ceiling: 114},
		{Key: Key{File: "internal/commands/status.go"}, Ceiling: 1015},
		{Key: Key{File: "internal/commands/status.go", Func: "buildPhaseHandoffStatus"}, Ceiling: 125},
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
