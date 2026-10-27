package archguard

import "testing"

// areaA9 is the guard data of area A9 (Agent runtime): written only by this area's
// scopes (ADR-0218). Ratchet entries may only be removed or lowered.
var areaA9 = Area{
	ID: "A9",
	Layers: []LayerEntry{
		{Package: "internal/agent", Layer: layerRuntime},
		{Package: "internal/agentlaunch", Layer: layerInfrastructure},
		{Package: "internal/process", Layer: layerRuntime},
		{Package: "internal/providersignal", Layer: layerInfrastructure},
	},
	Allowlist: []AllowEntry{
		{
			Importer: "internal/commands",
			Imported: "internal/agent",
			Owner:    "Scope 20",
			Rationale: "commands/status reads the runtime's read-only wake projection through DetectOrchestratorWakeTriggersForProject " +
				"and OrchestratorWakeResult; keeping this adapter avoids duplicating the runtime's wake precedence or dragging ops and " +
				"prompts dependencies into infrastructure; it grants no mutation authority and uses the A8-delivered projection " +
				"records; every launch and provider-signal capability commands consumes lives in agentlaunch and providersignal",
		},
		{
			Importer: "internal/tui",
			Imported: "internal/agent",
			Owner:    "Scope 20",
			Rationale: "tui/view shows RolePauseReason, the snapshot-only pause reason the runtime displays; keeping this pure query " +
				"avoids a second role and checkpoint predicate, and no waiting, resume or mutation side effect crosses the interface; " +
				"every launch and provider-signal capability tui consumes lives in agentlaunch and providersignal",
		},
		{
			Importer: "internal/agent",
			Imported: "internal/db",
			Owner:    "Scope 21",
			Rationale: "the runtime holds the Blackboard handle for non-mutating use only: snapshot, cached and watch reads " +
				"(ReadSnapshot, ReadCached, WatchForChanges), handle construction and acquisition configuration (db.For, Patient, " +
				"WithLockRetryContext) passed to named ops operations, and generation-scoped liveness record publication " +
				"(db.WriteLivenessRecord, ADR-0177), which rewrites no canonical state; every persisted-state mutation lives in ops " +
				"(C6) and TestC6MutationGuard rejects a Blackboard mutator or mutation callback outside ops and db, so this edge " +
				"grants no write authority; no forwarding package hides it",
		},
	},
	Ratchet: []RatchetEntry{},
	S2:      []S2Entry{},
}

// TestAreaA9RatchetIsEmpty holds the A9 size closure condition: every S1
// offender of the starting commit is under threshold (the ratchet is empty) and
// no data-like exception is needed (S2 is empty). The allowlist is not part of
// it: the two retained presentation queries and C6's agent-to-db edge keep
// their rationale entries.
func TestAreaA9RatchetIsEmpty(t *testing.T) {
	for _, r := range areaA9.Ratchet {
		t.Errorf("A9 ratchet still holds %s (ceiling %d)", r.Key, r.Ceiling)
	}
	for _, e := range areaA9.S2 {
		t.Errorf("A9 S2 holds %s", e.Key)
	}
}
