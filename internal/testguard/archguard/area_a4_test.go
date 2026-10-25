package archguard

// areaA4 is the guard data of area A4 (Ops planning and task graph): written only by this area's
// scopes (ADR-0218). Ratchet entries may only be removed or lowered.
var areaA4 = Area{
	ID:        "A4",
	Layers:    []LayerEntry{},
	Allowlist: []AllowEntry{},
	Ratchet: []RatchetEntry{
		{Key: Key{File: "internal/ops/cancel_task.go", Func: "cancelTaskLifecycle"}, Ceiling: 118},
		{Key: Key{File: "internal/ops/delete_task.go", Func: "DeleteTask"}, Ceiling: 119},
		{Key: Key{File: "internal/ops/sprint_checkpoint.go", Func: "sprintCheckpoint"}, Ceiling: 120},
		{Key: Key{File: "internal/ops/supersede_task.go", Func: "supersedeTaskLifecycle"}, Ceiling: 152},
	},
	S2: []S2Entry{},
}
