package archguard

// areaA6 is the guard data of area A6 (Ops merge and integration): written only by this area's
// scopes (ADR-0218). Ratchet entries may only be removed or lowered.
var areaA6 = Area{
	ID:        "A6",
	Layers:    []LayerEntry{},
	Allowlist: []AllowEntry{},
	Ratchet: []RatchetEntry{
		{Key: Key{File: "internal/ops/reconcile_merged.go", Func: "reconcileMergedWithOptionalAuthority"}, Ceiling: 119},
		{Key: Key{File: "internal/ops/wt_create.go", Func: "createWorktree"}, Ceiling: 120},
		{Key: Key{File: "internal/ops/wt_merge.go"}, Ceiling: 1403},
		{Key: Key{File: "internal/ops/wt_merge.go", Func: "mergeWorktree"}, Ceiling: 554},
		{Key: Key{File: "internal/ops/wt_merge.go", Func: "performCASMerge"}, Ceiling: 110},
	},
	S2: []S2Entry{},
}
