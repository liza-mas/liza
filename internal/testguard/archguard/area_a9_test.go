package archguard

// areaA9 is the guard data of area A9 (Agent runtime): written only by this area's
// scopes (ADR-0218). Ratchet entries may only be removed or lowered.
var areaA9 = Area{
	ID: "A9",
	Layers: []LayerEntry{
		{Package: "internal/agent", Layer: layerRuntime},
		{Package: "internal/process", Layer: layerRuntime},
	},
	Allowlist: []AllowEntry{
		{
			Importer:  "internal/commands",
			Imported:  "internal/agent",
			Owner:     "Scope 20",
			Rationale: "baseline crossing at the starting commit; resolution owned by Scope 20 per SI-5",
		},
		{
			Importer:  "internal/tui",
			Imported:  "internal/agent",
			Owner:     "Scope 20",
			Rationale: "baseline crossing at the starting commit; resolution owned by Scope 20 per SI-5",
		},
		{
			Importer:  "internal/agent",
			Imported:  "internal/db",
			Owner:     "Scope 21",
			Rationale: "baseline crossing at the starting commit; resolution owned by Scope 21 per SI-5",
		},
	},
	Ratchet: []RatchetEntry{
		{Key: Key{File: "internal/agent/acpx_agent.go", Func: "ACPXAgent.Run"}, Ceiling: 141},
		{Key: Key{File: "internal/agent/claiming.go", Func: "claimDoerTaskWithOptionalAuthority"}, Ceiling: 126},
		{Key: Key{File: "internal/agent/claiming.go", Func: "handleApprovedMergesWithOptionalAuthority"}, Ceiling: 107},
		{Key: Key{File: "internal/agent/cli_agent.go", Func: "CLIAgent.Run"}, Ceiling: 178},
		{Key: Key{File: "internal/agent/cli_agent.go", Func: "CLIAgent.buildRunCommand"}, Ceiling: 148},
		{Key: Key{File: "internal/agent/supervisor.go", Func: "RunSupervisor"}, Ceiling: 521},
		{Key: Key{File: "internal/agent/supervisor_restart.go", Func: "exit42RestartTracker.Handle"}, Ceiling: 111},
		{Key: Key{File: "internal/agent/systemctl.go"}, Ceiling: 861},
		{Key: Key{File: "internal/agent/systemctl.go", Func: "executeAgent"}, Ceiling: 207},
		{Key: Key{File: "internal/agent/systemctl.go", Func: "verifyOrchestratorWakeChanges"}, Ceiling: 124},
		{Key: Key{File: "internal/agent/waitforwork.go", Func: "waitForWorkEventDriven"}, Ceiling: 132},
		{Key: Key{File: "internal/agent/worktree_check.go", Func: "ensureReviewerWorktreeLocked"}, Ceiling: 139},
	},
	S2: []S2Entry{},
}
