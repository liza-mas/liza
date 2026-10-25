package archguard

// areaA5 is the guard data of area A5 (Ops review and acceptance): written only by this area's
// scopes (ADR-0218). Ratchet entries may only be removed or lowered.
var areaA5 = Area{
	ID:        "A5",
	Layers:    []LayerEntry{},
	Allowlist: []AllowEntry{},
	Ratchet: []RatchetEntry{
		{Key: Key{File: "internal/ops/await_resubmission.go"}, Ceiling: 834},
		{Key: Key{File: "internal/ops/await_resubmission.go", Func: "awaitResubmissionWithOptions"}, Ceiling: 214},
		{Key: Key{File: "internal/ops/await_resubmission.go", Func: "reclaimForReview"}, Ceiling: 124},
		{Key: Key{File: "internal/ops/await_verdict.go"}, Ceiling: 913},
		{Key: Key{File: "internal/ops/await_verdict.go", Func: "awaitVerdictWithOptions"}, Ceiling: 184},
		{Key: Key{File: "internal/ops/await_verdict.go", Func: "handleVerdictResult"}, Ceiling: 105},
		{Key: Key{File: "internal/ops/claim_reviewer_task.go"}, Ceiling: 825},
		{Key: Key{File: "internal/ops/claim_reviewer_task.go", Func: "claimReviewerTask"}, Ceiling: 383},
		{Key: Key{File: "internal/ops/clear_stale_review_claims.go", Func: "ClearStaleReviewClaims"}, Ceiling: 110},
		{Key: Key{File: "internal/ops/update_review_commit.go", Func: "updateReviewCommitLocked"}, Ceiling: 183},
	},
	S2: []S2Entry{},
}
