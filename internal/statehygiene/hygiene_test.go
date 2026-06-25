package statehygiene

import (
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/models"
)

func withStateHygieneProjectDir(t *testing.T, projectDir string) {
	t.Helper()
	oldProjectDir := brand.ProjectDirName
	brand.ProjectDirName = projectDir
	t.Cleanup(func() {
		brand.ProjectDirName = oldProjectDir
	})
}

func TestValidateStateRejectsRawProviderTranscriptPayload(t *testing.T) {
	state := createState()
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
				"message":  rawProviderTranscript(),
				"log_ref": map[string]any{
					"output_file": ".liza/agent-outputs/orchestrator-1-20260515-101530.txt",
					"event_id":    "item_abc123",
				},
			},
		},
	}

	err := ValidateState(state)
	if err == nil {
		t.Fatal("ValidateState() error = nil, want raw transcript rejection")
	}
	if !strings.Contains(err.Error(), "raw provider transcript payload") {
		t.Fatalf("ValidateState() error = %v, want raw transcript rejection", err)
	}
}

func TestValidateStateRejectsPrettyPrintedRawProviderTranscriptPayload(t *testing.T) {
	state := createState()
	state.Anomalies = []models.Anomaly{
		{
			Timestamp: time.Now().UTC(),
			Task:      "task-1",
			Reporter:  "orchestrator-1",
			Type:      "provider_audit_degraded",
			Details: map[string]any{
				"provider": "codex",
				"agent_id": "orchestrator-1",
				"message": `{
  "item": {
    "aggregated_output": "raw output",
    "type": "command_execution"
  },
  "type": "item.completed"
}`,
			},
		},
	}

	err := ValidateState(state)
	if err == nil {
		t.Fatal("ValidateState() error = nil, want raw transcript rejection")
	}
	if !strings.Contains(err.Error(), "raw provider transcript payload") {
		t.Fatalf("ValidateState() error = %v, want raw transcript rejection", err)
	}
}

func TestValidateStateRejectsOversizedMessage(t *testing.T) {
	state := createState()
	state.HumanNotes = []models.HumanNote{
		{
			Timestamp: time.Now().UTC(),
			For:       "orchestrator",
			Message:   strings.Repeat("x", MaxStateTextBytes+1),
		},
	}

	err := ValidateState(state)
	if err == nil {
		t.Fatal("ValidateState() error = nil, want oversized message rejection")
	}
	if !strings.Contains(err.Error(), "exceeds 4096-byte state text limit") {
		t.Fatalf("ValidateState() error = %v, want oversized message rejection", err)
	}
}

func TestValidateStateAllowsOrdinaryShortMessages(t *testing.T) {
	state := createState()
	state.Tasks[0].History = []models.TaskHistoryEntry{
		{
			Time:  time.Now().UTC(),
			Event: models.TaskEventAbandoned,
			Note:  ptr("Agent coder-2 deleted: terminated via TUI"),
		},
	}
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
				"message":  "failed to record rollout items: thread abc not found",
			},
		},
	}

	if err := ValidateState(state); err != nil {
		t.Fatalf("ValidateState() error = %v, want nil", err)
	}
}

func TestScrubStateForMigrationPreservesAnomalyAndScrubsMessage(t *testing.T) {
	state := createState()
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
				"message":  rawProviderTranscript(),
				"log_ref": map[string]any{
					"output_file": ".liza/agent-outputs/orchestrator-1-20260515-101530.txt",
					"event_id":    "item_abc123",
				},
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
				"message":  "short operational message",
			},
		},
	}

	if !ScrubStateForMigration(state) {
		t.Fatal("ScrubStateForMigration() changed = false, want true")
	}
	if len(state.Anomalies) != 2 {
		t.Fatalf("len(Anomalies) = %d, want 2", len(state.Anomalies))
	}
	details := state.Anomalies[0].Details
	if details["message"] != scrubbedProviderAuditMessage() {
		t.Fatalf("scrubbed message = %q, want %q", details["message"], scrubbedProviderAuditMessage())
	}
	if details["message_scrubbed"] != true {
		t.Fatalf("message_scrubbed = %v, want true", details["message_scrubbed"])
	}
	if details["scrub_reason"] != "raw_provider_transcript" {
		t.Fatalf("scrub_reason = %v, want raw_provider_transcript", details["scrub_reason"])
	}
	if details["provider"] != "codex" || details["agent_id"] != "orchestrator-1" {
		t.Fatalf("routing details lost during scrub: %#v", details)
	}
	logRef, ok := details["log_ref"].(map[string]any)
	if !ok {
		t.Fatalf("log_ref = %#v, want retained map", details["log_ref"])
	}
	if logRef["output_file"] != ".liza/agent-outputs/orchestrator-1-20260515-101530.txt" {
		t.Fatalf("log_ref output_file = %v, want retained output file", logRef["output_file"])
	}
	if got := state.Anomalies[1].Details["message"]; got != "short operational message" {
		t.Fatalf("ordinary message changed to %q", got)
	}
}

func TestScrubStateForMigrationUsesGenericMessageForNonProviderAnomaly(t *testing.T) {
	state := createState()
	state.Anomalies = []models.Anomaly{
		{
			Timestamp: time.Now().UTC(),
			Task:      "task-1",
			Reporter:  "coder-1",
			Type:      "retry_loop",
			Details: map[string]any{
				"count":         3,
				"error_pattern": "same error",
				"message":       strings.Repeat("x", MaxStateTextBytes+1),
			},
		},
	}

	if !ScrubStateForMigration(state) {
		t.Fatal("ScrubStateForMigration() changed = false, want true")
	}
	details := state.Anomalies[0].Details
	if details["message"] != scrubbedStateMessage() {
		t.Fatalf("scrubbed message = %q, want %q", details["message"], scrubbedStateMessage())
	}
	if details["message"] == scrubbedProviderAuditMessage() {
		t.Fatal("non-provider anomaly got provider-audit scrub message")
	}
	if details["message_scrubbed"] != true {
		t.Fatalf("message_scrubbed = %v, want true", details["message_scrubbed"])
	}
	if details["scrub_reason"] != "oversized_state_message" {
		t.Fatalf("scrub_reason = %v, want oversized_state_message", details["scrub_reason"])
	}
	if details["original_message_bytes"] != MaxStateTextBytes+1 {
		t.Fatalf("original_message_bytes = %v, want %d", details["original_message_bytes"], MaxStateTextBytes+1)
	}
	if details["error_pattern"] != "same error" {
		t.Fatalf("error_pattern = %v, want preserved routing detail", details["error_pattern"])
	}
}

func TestScrubStateForMigrationScrubsTranscriptOutsideAnomalies(t *testing.T) {
	state := createState()
	state.HumanNotes = []models.HumanNote{
		{
			Timestamp: time.Now().UTC(),
			For:       "orchestrator",
			Message:   rawProviderTranscript(),
		},
	}

	if !ScrubStateForMigration(state) {
		t.Fatal("ScrubStateForMigration() changed = false, want true")
	}
	if state.HumanNotes[0].Message != scrubbedStateMessage() {
		t.Fatalf("human note message = %q, want generic scrub message", state.HumanNotes[0].Message)
	}
}

func TestScrubStateForMigrationUsesBrandedAgentOutputsPath(t *testing.T) {
	withStateHygieneProjectDir(t, ".acme")
	state := createState()
	state.Anomalies = []models.Anomaly{
		{
			Timestamp: time.Now().UTC(),
			Task:      "task-1",
			Reporter:  "coder-1",
			Type:      "retry_loop",
			Details: map[string]any{
				"message": rawProviderTranscript(),
			},
		},
	}

	if !ScrubStateForMigration(state) {
		t.Fatal("ScrubStateForMigration() changed = false, want true")
	}
	message := state.Anomalies[0].Details["message"].(string)
	if !strings.Contains(message, ".acme/agent-outputs") {
		t.Fatalf("scrubbed message = %q, want branded agent outputs path", message)
	}
	if strings.Contains(message, ".liza/agent-outputs") {
		t.Fatalf("scrubbed message = %q, want no default project dir", message)
	}
}

func rawProviderTranscript() string {
	return `{"type":"item.completed","item":{"type":"command_execution","command":"/usr/bin/zsh -lc 'cat .liza/SUPPORT.md'","aggregated_output":"SUPPORT_FULL_TEXT","exit_code":0,"status":"completed"}}`
}

func createState() *models.State {
	now := time.Now().UTC()
	return &models.State{
		Version: 1,
		Goal: models.Goal{
			ID:      "goal-1",
			Status:  models.GoalStatusInProgress,
			Created: now,
		},
		Tasks: []models.Task{
			{
				ID:      "task-1",
				Status:  models.TaskStatusDraft,
				Created: now,
			},
		},
		Agents: map[string]models.Agent{},
		Config: models.Config{IntegrationBranch: "main"},
	}
}

func ptr(s string) *string {
	return &s
}
