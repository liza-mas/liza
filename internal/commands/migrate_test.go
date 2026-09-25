package commands

import (
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/statehygiene"
	"github.com/liza-mas/liza/internal/statevalidate"
	"github.com/liza-mas/liza/internal/testhelpers"
	"gopkg.in/yaml.v3"
)

func TestMigrateCommand_NormalizesUnderscoreRoles(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Agents = map[string]models.Agent{
		"coder-1": {
			Role:      "coder",
			Status:    models.AgentStatusIdle,
			Heartbeat: now,
			Terminal:  "term-1",
		},
		"code-reviewer-1": {
			Role:      "code_reviewer", // underscore form — should be normalized
			Status:    models.AgentStatusIdle,
			Heartbeat: now,
			Terminal:  "term-2",
		},
		"orchestrator-1": {
			Role:      "orchestrator",
			Status:    models.AgentStatusIdle,
			Heartbeat: now,
			Terminal:  "term-3",
		},
	}
	testhelpers.WriteInitialState(t, statePath, state)

	changed, err := MigrateCommand(statePath)
	if err != nil {
		t.Fatalf("MigrateCommand() error = %v", err)
	}
	if !changed {
		t.Error("MigrateCommand() changed = false, want true")
	}

	// Verify the file was updated
	bb := db.New(statePath)
	updated, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read updated state: %v", err)
	}

	agent := updated.Agents["code-reviewer-1"]
	if agent.Role != "code-reviewer" {
		t.Errorf("Agent role = %q, want %q", agent.Role, "code-reviewer")
	}
}

func TestMigrateCommand_AlreadyMigratedNoChanges(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Agents = map[string]models.Agent{
		"coder-1": {
			Role:      "coder",
			Status:    models.AgentStatusIdle,
			Heartbeat: now,
			Terminal:  "term-1",
		},
		"code-reviewer-1": {
			Role:      "code-reviewer", // already hyphenated
			Status:    models.AgentStatusIdle,
			Heartbeat: now,
			Terminal:  "term-2",
		},
	}
	testhelpers.WriteInitialState(t, statePath, state)

	// Capture file mod time before migration
	infoBefore, _ := os.Stat(statePath)

	changed, err := MigrateCommand(statePath)
	if err != nil {
		t.Fatalf("MigrateCommand() error = %v", err)
	}
	if changed {
		t.Error("MigrateCommand() changed = true, want false (already migrated)")
	}

	// File should not have been modified
	infoAfter, _ := os.Stat(statePath)
	if infoBefore.ModTime() != infoAfter.ModTime() {
		t.Error("State file was modified even though no changes were needed")
	}
}

func TestMigrateCommand_MultipleUnderscoreRoles(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Agents = map[string]models.Agent{
		"code-planner-1": {
			Role:      "code_planner",
			Status:    models.AgentStatusIdle,
			Heartbeat: now,
			Terminal:  "term-1",
		},
		"epic-plan-reviewer-1": {
			Role:      "epic_plan_reviewer",
			Status:    models.AgentStatusIdle,
			Heartbeat: now,
			Terminal:  "term-2",
		},
		"us-writer-1": {
			Role:      "us_writer",
			Status:    models.AgentStatusIdle,
			Heartbeat: now,
			Terminal:  "term-3",
		},
	}
	testhelpers.WriteInitialState(t, statePath, state)

	changed, err := MigrateCommand(statePath)
	if err != nil {
		t.Fatalf("MigrateCommand() error = %v", err)
	}
	if !changed {
		t.Error("MigrateCommand() changed = false, want true")
	}

	bb := db.New(statePath)
	updated, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read updated state: %v", err)
	}

	expectations := map[string]string{
		"code-planner-1":       "code-planner",
		"epic-plan-reviewer-1": "epic-plan-reviewer",
		"us-writer-1":          "us-writer",
	}
	for agentID, wantRole := range expectations {
		got := updated.Agents[agentID].Role
		if got != wantRole {
			t.Errorf("Agent %q role = %q, want %q", agentID, got, wantRole)
		}
	}
}

func TestMigrateCommand_Idempotent(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Agents = map[string]models.Agent{
		"code-reviewer-1": {
			Role:      "code_reviewer",
			Status:    models.AgentStatusIdle,
			Heartbeat: now,
			Terminal:  "term-1",
		},
	}
	testhelpers.WriteInitialState(t, statePath, state)

	// First migration — should change
	changed1, err := MigrateCommand(statePath)
	if err != nil {
		t.Fatalf("First MigrateCommand() error = %v", err)
	}
	if !changed1 {
		t.Error("First MigrateCommand() changed = false, want true")
	}

	// Second migration — should report no changes
	changed2, err := MigrateCommand(statePath)
	if err != nil {
		t.Fatalf("Second MigrateCommand() error = %v", err)
	}
	if changed2 {
		t.Error("Second MigrateCommand() changed = true, want false")
	}
}

func TestMigrateCommand_UnknownRolePassesThrough(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Agents = map[string]models.Agent{
		"custom-1": {
			Role:      "custom_agent",
			Status:    models.AgentStatusIdle,
			Heartbeat: now,
			Terminal:  "term-1",
		},
	}
	testhelpers.WriteInitialState(t, statePath, state)

	changed, err := MigrateCommand(statePath)
	if err != nil {
		t.Fatalf("MigrateCommand() error = %v", err)
	}
	if changed {
		t.Error("MigrateCommand() changed = true, want false (unknown role should not be modified)")
	}

	// Verify the unknown role was not mutated
	data, _ := os.ReadFile(statePath)
	var updated models.State
	_ = yaml.Unmarshal(data, &updated)
	if updated.Agents["custom-1"].Role != "custom_agent" {
		t.Errorf("Unknown role was mutated: got %q, want %q", updated.Agents["custom-1"].Role, "custom_agent")
	}
}

func TestMigrateCommand_NoAgents(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)

	state := testhelpers.CreateValidState()
	state.Agents = map[string]models.Agent{}
	testhelpers.WriteInitialState(t, statePath, state)

	changed, err := MigrateCommand(statePath)
	if err != nil {
		t.Fatalf("MigrateCommand() error = %v", err)
	}
	if changed {
		t.Error("MigrateCommand() changed = true, want false (no agents to migrate)")
	}
}

func TestMigrateCommand_NormalizesLegacyAttempted(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		{
			ID:      "task-1",
			Status:  models.TaskStatusDraft,
			Created: now,
			Extra:   map[string]any{"attempted": []any{"agent-1"}},
		},
	}
	testhelpers.WriteInitialState(t, statePath, state)

	changed, err := MigrateCommand(statePath)
	if err != nil {
		t.Fatalf("MigrateCommand() error = %v", err)
	}
	if !changed {
		t.Error("MigrateCommand() changed = false, want true")
	}

	// Read raw YAML to verify persisted state
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	var updated models.State
	if err := yaml.Unmarshal(data, &updated); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	task := updated.Tasks[0]
	if task.Attempt != 2 {
		t.Errorf("task.Attempt = %d, want 2", task.Attempt)
	}
	if _, exists := task.Extra["attempted"]; exists {
		t.Error("task.Extra[\"attempted\"] still present, want deleted")
	}
}

func TestMigrateCommand_LegacyAttempted_Idempotent(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		{
			ID:      "task-1",
			Status:  models.TaskStatusDraft,
			Created: now,
			Extra:   map[string]any{"attempted": []any{"agent-1"}},
		},
	}
	testhelpers.WriteInitialState(t, statePath, state)

	// First run — should change
	changed1, err := MigrateCommand(statePath)
	if err != nil {
		t.Fatalf("First MigrateCommand() error = %v", err)
	}
	if !changed1 {
		t.Error("First MigrateCommand() changed = false, want true")
	}

	// Second run — should report no changes
	changed2, err := MigrateCommand(statePath)
	if err != nil {
		t.Fatalf("Second MigrateCommand() error = %v", err)
	}
	if changed2 {
		t.Error("Second MigrateCommand() changed = true, want false")
	}
}

func TestMigrateCommand_ScrubsRawProviderAuditMessage(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)

	state := testhelpers.CreateValidState()
	state.Anomalies = []models.Anomaly{
		{
			Timestamp: time.Now().UTC(),
			Task:      "task-1",
			Reporter:  "orchestrator-1",
			Type:      "provider_audit_degraded",
			Details: map[string]any{
				"provider": "codex",
				"agent_id": "orchestrator-1",
				"impact":   "provider transcript or rollout persistence may be incomplete",
				"message":  `{"type":"item.completed","item":{"type":"command_execution","aggregated_output":"raw output"}}`,
			},
		},
		{
			Timestamp: time.Now().UTC(),
			Task:      "task-2",
			Reporter:  "orchestrator-1",
			Type:      "provider_audit_degraded",
			Details: map[string]any{
				"provider": "codex",
				"agent_id": "orchestrator-1",
				"message":  "Agent coder-2 deleted: terminated via TUI",
			},
		},
	}
	data, err := yaml.Marshal(state)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if err := os.WriteFile(statePath, data, 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	changed, err := MigrateCommand(statePath)
	if err != nil {
		t.Fatalf("MigrateCommand() error = %v", err)
	}
	if !changed {
		t.Fatal("MigrateCommand() changed = false, want true")
	}

	updated, err := db.New(statePath).Read()
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	got := updated.Anomalies[0].Details["message"]
	want := "provider audit degraded; raw provider event omitted from state; inspect " + paths.ProjectDirName() + "/agent-outputs and alerts for transcript evidence"
	if got != want {
		t.Fatalf("scrubbed message = %q, want %q", got, want)
	}
	if updated.Anomalies[0].Details["message_scrubbed"] != true {
		t.Fatalf("message_scrubbed = %v, want true", updated.Anomalies[0].Details["message_scrubbed"])
	}
	if got := updated.Anomalies[1].Details["message"]; got != "Agent coder-2 deleted: terminated via TUI" {
		t.Fatalf("ordinary message changed to %q", got)
	}
}

func TestMigrateCommand_ScrubsOversizedLegacyRejectionReason(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusRejected, now)
	reason := strings.Repeat("x", statehygiene.MaxStateTextBytes+1)
	task.RejectionReason = &reason
	state.Tasks = []models.Task{task}
	data, err := yaml.Marshal(state)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if err := os.WriteFile(statePath, data, 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	changed, err := MigrateCommand(statePath)
	if err != nil {
		t.Fatalf("MigrateCommand() error = %v", err)
	}
	if !changed {
		t.Fatal("MigrateCommand() changed = false, want true")
	}

	bb := db.New(statePath)
	if err := bb.Modify(func(*models.State) error { return nil }); err != nil {
		t.Fatalf("state write after migration failed: %v", err)
	}
	updated, err := bb.Read()
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	updatedTask := updated.FindTask("task-1")
	if updatedTask == nil || updatedTask.RejectionReason == nil {
		t.Fatal("migrated rejection_reason missing")
	}
	if got := *updatedTask.RejectionReason; !strings.Contains(got, "raw state payload omitted") || len([]byte(got)) > statehygiene.MaxStateTextBytes {
		t.Fatalf("migrated rejection_reason = %q, want bounded scrub message", got)
	}
}

func TestMigrateCommand_ScrubsOversizedAlignmentSummary(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, time.Now().UTC())
	task.Description = strings.Repeat("界", statehygiene.MaxStateTextBytes)
	state.Tasks = []models.Task{task}
	entry := models.AlignmentHistory{
		Timestamp: task.Created,
		Event:     models.TaskEventPlanning,
		Summary:   "Added task task-1: " + task.Description,
	}
	state.Goal.AlignmentHistory = append(state.Goal.AlignmentHistory, entry)
	// Legacy fixtures must bypass the current write-time hygiene guard.
	data, err := yaml.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, data, 0644); err != nil {
		t.Fatal(err)
	}
	changed, err := MigrateCommand(statePath)
	if err != nil || !changed {
		t.Fatalf("MigrateCommand() = %v, %v, want true, nil", changed, err)
	}
	bb := db.New(statePath)
	updated, err := bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	if got := updated.FindTask(task.ID); got == nil || got.Description != task.Description {
		t.Fatal("migration lost or altered the task description")
	}
	if len(updated.Goal.AlignmentHistory) != len(state.Goal.AlignmentHistory) {
		t.Fatal("migration changed alignment history length")
	}
	got := updated.Goal.AlignmentHistory[len(updated.Goal.AlignmentHistory)-1]
	if got.Event != entry.Event || !got.Timestamp.Equal(entry.Timestamp) {
		t.Fatal("migration altered alignment event metadata")
	}
	if !strings.Contains(got.Summary, "raw state payload omitted") || len(got.Summary) > statehygiene.MaxStateTextBytes {
		t.Fatalf("migrated summary = %q, want bounded scrub message", got.Summary)
	}
	if err := bb.Modify(func(*models.State) error { return nil }); err != nil {
		t.Fatalf("state remains unwritable after migration: %v", err)
	}
	if changed, err := MigrateCommand(statePath); err != nil || changed {
		t.Fatalf("second migration = %v, %v, want false, nil", changed, err)
	}
}

func TestMigrateCommand_InvalidStatePath(t *testing.T) {
	_, err := MigrateCommand("/nonexistent/path/state.yaml")
	if err == nil {
		t.Error("MigrateCommand() error = nil, want error for nonexistent file")
	}
}

// A rejection that landed while its doer was already gone used to renew a lease
// on an unassigned task. The surviving half-tuple fails validation, and because
// post-mutation validation is global it blocks every later mutation — including
// the repairs that would clear it — so migration is where it gets normalized.
func TestMigrateCommand_ClearsLeaseWithoutAssignee(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	dangling := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusRejected, now)
	dangling.AssignedTo = nil
	stale := now.Add(-time.Hour)
	dangling.LeaseExpires = &stale
	owner := "coder-1"
	owned := testhelpers.BuildTaskByStatus("task-2", models.TaskStatusImplementing, now)
	owned.AssignedTo = &owner
	ownedLease := now.Add(time.Hour)
	owned.LeaseExpires = &ownedLease
	state.Tasks = []models.Task{dangling, owned}
	testhelpers.WriteInitialState(t, statePath, state)

	changed, err := MigrateCommand(statePath)
	if err != nil {
		t.Fatalf("MigrateCommand() error = %v", err)
	}
	if !changed {
		t.Fatal("MigrateCommand() changed = false, want true")
	}

	bb := db.New(statePath)
	updated, err := bb.Read()
	if err != nil {
		t.Fatalf("read migrated state: %v", err)
	}
	if lease := updated.FindTask("task-1").LeaseExpires; lease != nil {
		t.Errorf("unassigned task kept lease_expires = %v, want nil", lease)
	}
	if lease := updated.FindTask("task-2").LeaseExpires; lease == nil {
		t.Error("owned task lost its lease")
	}

	again, err := MigrateCommand(statePath)
	if err != nil {
		t.Fatalf("second MigrateCommand() error = %v", err)
	}
	if again {
		t.Error("second MigrateCommand() changed = true, want idempotent")
	}
}

// TestMigrateCommand_RetypesLegacyPendingMergeStall pins the D83 repair: the
// stall record an older reviewer wrote as an incomplete retry_loop becomes a
// pending_merge_stalled record carrying the same evidence, so whole-state
// validation — and every rejected-task reclaim gated on it — passes again.
func TestMigrateCommand_RetypesLegacyPendingMergeStall(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)

	legacy := testhelpers.LegacyPendingMergeStallAnomaly()
	genuine := models.Anomaly{
		Timestamp: legacy.Timestamp.Add(time.Minute),
		Task:      "task-1",
		Reporter:  "coder-1",
		Type:      "retry_loop",
		Details:   map[string]any{"count": 3, "error_pattern": "connection refused"},
	}
	state := testhelpers.CreateValidState()
	state.Anomalies = []models.Anomaly{legacy, genuine}
	testhelpers.WriteInitialState(t, statePath, state)

	changed, err := MigrateCommand(statePath)
	if err != nil {
		t.Fatalf("MigrateCommand() error = %v", err)
	}
	if !changed {
		t.Fatal("MigrateCommand() changed = false, want true")
	}

	updated, err := db.New(statePath).Read()
	if err != nil {
		t.Fatalf("read migrated state: %v", err)
	}
	if len(updated.Anomalies) != 2 {
		t.Fatalf("anomalies = %d, want 2", len(updated.Anomalies))
	}
	stall := updated.Anomalies[0]
	if stall.Type != "pending_merge_stalled" {
		t.Errorf("stall type = %q, want pending_merge_stalled", stall.Type)
	}
	if !stall.Timestamp.Equal(legacy.Timestamp) || stall.Reporter != legacy.Reporter || stall.Task != legacy.Task {
		t.Errorf("stall identity changed: got (%v, %q, %q), want (%v, %q, %q)",
			stall.Timestamp, stall.Reporter, stall.Task, legacy.Timestamp, legacy.Reporter, legacy.Task)
	}
	if !reflect.DeepEqual(stall.Details, legacy.Details) {
		t.Errorf("stall details = %v, want preserved %v", stall.Details, legacy.Details)
	}
	if got := updated.Anomalies[1]; got.Type != "retry_loop" || !reflect.DeepEqual(got.Details, genuine.Details) {
		t.Errorf("genuine retry_loop changed: %+v", got)
	}
	if err := statevalidate.ValidateAnomalies(updated, tmpDir, true); err != nil {
		t.Errorf("migrated anomalies fail validation: %v", err)
	}

	again, err := MigrateCommand(statePath)
	if err != nil {
		t.Fatalf("second MigrateCommand() error = %v", err)
	}
	if again {
		t.Error("second MigrateCommand() changed = true, want idempotent")
	}
}

// TestMigrateCommand_LeavesUnrecognizedMalformedRetryLoop guards the migration
// predicate: only the full legacy stall signature is retyped. A record that
// matches partly is someone else's malformed evidence and stays for
// inspection rather than becoming a stall record it cannot vouch for.
func TestMigrateCommand_LeavesUnrecognizedMalformedRetryLoop(t *testing.T) {
	cases := map[string]func(details map[string]any){
		"missing agent_id": func(d map[string]any) { delete(d, "agent_id") },
		"missing role":     func(d map[string]any) { delete(d, "role") },
		"missing rounds":   func(d map[string]any) { delete(d, "rounds") },
		"different impact": func(d map[string]any) { d["impact"] = "some other loop" },
		"partial canonical retry details": func(d map[string]any) {
			d["count"] = 21
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			tmpDir := t.TempDir()
			statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)

			record := testhelpers.LegacyPendingMergeStallAnomaly()
			mutate(record.Details)
			state := testhelpers.CreateValidState()
			state.Anomalies = []models.Anomaly{record}
			testhelpers.WriteInitialState(t, statePath, state)

			changed, err := MigrateCommand(statePath)
			if err != nil {
				t.Fatalf("MigrateCommand() error = %v", err)
			}
			if changed {
				t.Error("MigrateCommand() changed = true, want the record left alone")
			}
			updated, err := db.New(statePath).Read()
			if err != nil {
				t.Fatalf("read state: %v", err)
			}
			got := updated.Anomalies[0]
			if got.Type != "retry_loop" || !reflect.DeepEqual(got.Details, record.Details) {
				t.Errorf("record changed: got %+v, want %+v", got, record)
			}
		})
	}
}
