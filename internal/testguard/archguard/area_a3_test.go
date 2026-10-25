package archguard

// areaA3 is the guard data of area A3 (Ops kernel and execution lifecycle): written only by this area's
// scopes (ADR-0218). Ratchet entries may only be removed or lowered.
var areaA3 = Area{
	ID: "A3",
	Layers: []LayerEntry{
		{Package: "internal/ops", Layer: layerApplication},
	},
	Allowlist: []AllowEntry{},
	Ratchet: []RatchetEntry{
		{Key: Key{File: "internal/ops/handoff.go", Func: "Handoff"}, Ceiling: 141},
		{Key: Key{File: "internal/ops/mode_change.go", Func: "resume"}, Ceiling: 148},
		{Key: Key{File: "internal/ops/recover_agent.go", Func: "recoverAgentLifecycle"}, Ceiling: 240},
		{Key: Key{File: "internal/ops/recover_task.go"}, Ceiling: 822},
		{Key: Key{File: "internal/ops/recover_task.go", Func: "recoverTaskWithOptions"}, Ceiling: 181},
		{Key: Key{File: "internal/ops/release_claim.go", Func: "releaseClaimLocked"}, Ceiling: 123},
		{Key: Key{File: "internal/ops/transition_attempt.go", Func: "transitionToNewAttemptLocked"}, Ceiling: 162},
	},
	S2: []S2Entry{},
}
