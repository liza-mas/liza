package archguard

// areaA4 is the guard data of area A4 (Ops planning and task graph): written only by this area's
// scopes (ADR-0218). Ratchet entries may only be removed or lowered.
var areaA4 = Area{
	ID:        "A4",
	Layers:    []LayerEntry{},
	Allowlist: []AllowEntry{},
	Ratchet: []RatchetEntry{
		{Key: Key{File: "internal/ops/add_tasks.go"}, Ceiling: 810},
		{Key: Key{File: "internal/ops/add_tasks.go", Func: "planDeliveryUnits"}, Ceiling: 122},
		{Key: Key{File: "internal/ops/amend_plan.go", Func: "AmendPlan"}, Ceiling: 145},
		{Key: Key{File: "internal/ops/apply_dependency_repair.go", Func: "applyDependencyRepairWithOptionalAuthority"}, Ceiling: 161},
		{Key: Key{File: "internal/ops/cancel_task.go", Func: "cancelTaskLifecycle"}, Ceiling: 118},
		{Key: Key{File: "internal/ops/contract_amendment.go", Func: "validateBoundedContractCorrection"}, Ceiling: 107},
		{Key: Key{File: "internal/ops/delete_task.go", Func: "DeleteTask"}, Ceiling: 119},
		{Key: Key{File: "internal/ops/narrow_inherited_dependencies.go", Func: "narrowInheritedDependenciesWithOptionalAuthority"}, Ceiling: 305},
		{Key: Key{File: "internal/ops/obligation_drift.go", Func: "detectObligationDrift"}, Ceiling: 142},
		{Key: Key{File: "internal/ops/plan_check.go", Func: "RecordPlanCheck"}, Ceiling: 129},
		{Key: Key{File: "internal/ops/repair_superseded_dependencies.go", Func: "repairSupersededDependenciesWithOptionalAuthority"}, Ceiling: 131},
		{Key: Key{File: "internal/ops/replace_task.go", Func: "ReplaceTaskWithAuthorityAndOptions"}, Ceiling: 211},
		{Key: Key{File: "internal/ops/replan.go", Func: "Replan"}, Ceiling: 345},
		{Key: Key{File: "internal/ops/retarget_dependency.go", Func: "retargetDependencyWithOptionalAuthority"}, Ceiling: 194},
		{Key: Key{File: "internal/ops/set_task_output.go", Func: "setTaskOutputWithOptionalAuthority"}, Ceiling: 193},
		{Key: Key{File: "internal/ops/sprint_checkpoint.go", Func: "sprintCheckpoint"}, Ceiling: 120},
		{Key: Key{File: "internal/ops/supersede_task.go", Func: "supersedeTaskLifecycle"}, Ceiling: 152},
	},
	S2: []S2Entry{},
}
