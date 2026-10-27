package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestDetectProviderAuditDegraded_CodexRolloutThreadNotFound(t *testing.T) {
	output := `2026-05-05T18:03:44.961611Z ERROR codex_core::session: failed to record rollout items: thread 019e983f-f3a2-7071-8a66-aa1774db9101 not found`

	result := DetectProviderAuditDegraded(output, "codex")
	if result == nil {
		t.Fatal("expected provider audit degradation detected, got nil")
	}
	if result.Provider != "codex" {
		t.Errorf("Provider = %q, want %q", result.Provider, "codex")
	}
	if !strings.Contains(result.Message, "failed to record rollout items") {
		t.Errorf("Message = %q, want rollout persistence line", result.Message)
	}
}

func TestDetectProviderAuditDegraded_CodexACPAlias(t *testing.T) {
	output := `2026-05-05T18:03:44.961611Z ERROR codex_core::session: failed to record rollout items: thread 019e983f-f3a2-7071-8a66-aa1774db9101 not found`

	result := DetectProviderAuditDegraded(output, "codex-acp")
	if result == nil {
		t.Fatal("expected provider audit degradation detected for codex-acp, got nil")
	}
	if result.Provider != "codex" {
		t.Errorf("Provider = %q, want canonical provider %q", result.Provider, "codex")
	}
}

func TestDetectProviderAuditDegraded_IgnoresUnrelatedThreadNotFound(t *testing.T) {
	output := `thread 019e983f-f3a2-7071-8a66-aa1774db9101 not found`

	result := DetectProviderAuditDegraded(output, "codex")
	if result != nil {
		t.Errorf("expected nil for unrelated thread lookup, got %+v", result)
	}
}

func TestDetectProviderAuditDegraded_IgnoresNeedlesSplitAcrossLines(t *testing.T) {
	output := "ERROR codex_core::session: failed to record rollout items: connection timeout\nthread pool not found"

	result := DetectProviderAuditDegraded(output, "codex")
	if result != nil {
		t.Errorf("expected nil for split-line needles, got %+v", result)
	}
}

func TestDetectProviderAuditDegraded_WrongProvider(t *testing.T) {
	output := `ERROR codex_core::session: failed to record rollout items: thread 019e983f-f3a2-7071-8a66-aa1774db9101 not found`

	result := DetectProviderAuditDegraded(output, "claude")
	if result != nil {
		t.Errorf("expected nil for wrong provider, got %+v", result)
	}
}

func TestHandleProviderAuditDegraded_CanonicalizesProviderAliasInAnomaly(t *testing.T) {
	projectRoot := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
	state := testhelpers.CreateValidState()
	state.Agents["coder-1"] = models.Agent{Role: "coder", Status: models.AgentStatusIdle, Generation: "generation-current"}
	bb := testhelpers.WriteInitialState(t, statePath, state)

	output := `ERROR codex_core::session: failed to record rollout items: thread 019e983f-f3a2-7071-8a66-aa1774db9101 not found`
	handled, handleErr := handleProviderAuditDegraded(bb, SupervisorConfig{
		AgentID:     "coder-1",
		Authority:   models.AgentAuthority{ID: "coder-1", Generation: "generation-current"},
		ProjectRoot: projectRoot,
		CLIName:     "codex-acp",
	}, "task-1", output)
	if handleErr != nil {
		t.Fatalf("handleProviderAuditDegraded error = %v", handleErr)
	}
	if !handled {
		t.Fatal("handleProviderAuditDegraded returned false, want true")
	}

	alertsPath := filepath.Join(projectRoot, paths.ProjectDirName(), "alerts.log")
	data, err := os.ReadFile(alertsPath)
	if err != nil {
		t.Fatalf("failed to read alerts log: %v", err)
	}
	if !strings.Contains(string(data), "PROVIDER AUDIT DEGRADED") {
		t.Fatalf("alerts log missing provider audit degradation entry:\n%s", string(data))
	}

	updated, err := db.For(statePath).Read()
	if err != nil {
		t.Fatalf("failed to read updated state: %v", err)
	}
	if len(updated.Anomalies) != 1 {
		t.Fatalf("len(Anomalies) = %d, want 1", len(updated.Anomalies))
	}
	anomaly := updated.Anomalies[0]
	if anomaly.Type != models.AnomalyTypeProviderAuditDegraded {
		t.Fatalf("anomaly.Type = %q, want %q", anomaly.Type, models.AnomalyTypeProviderAuditDegraded)
	}
	if anomaly.Task != "task-1" {
		t.Errorf("anomaly.Task = %q, want task-1", anomaly.Task)
	}
	if anomaly.Reporter != "coder-1" {
		t.Errorf("anomaly.Reporter = %q, want coder-1", anomaly.Reporter)
	}
	if got := anomaly.Details["provider"]; got != "codex" {
		t.Errorf("provider detail = %v, want codex", got)
	}
	if got := anomaly.Details["agent_id"]; got != "coder-1" {
		t.Errorf("agent_id detail = %v, want coder-1", got)
	}
}
