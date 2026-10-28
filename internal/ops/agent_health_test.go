package ops

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestClassifyInfraClaimError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		err        error
		wantInfra  bool
		wantReason AgentDegradedReason
	}{
		{
			name:       "cannot lock ref operation not permitted",
			err:        errString("failed to create worktree: git [worktree add .worktrees/task-a integration -b task/task-a] failed: fatal: cannot lock ref 'refs/heads/task/task-a': Unable to create '.git/refs/heads/task/task-a.lock': Operation not permitted"),
			wantInfra:  true,
			wantReason: AgentDegradedWorktreeCreateFailed,
		},
		{
			name:       "readonly git worktree metadata",
			err:        errString("fatal: Unable to create .git/worktrees/task-a/index.lock: Read-only file system"),
			wantInfra:  true,
			wantReason: AgentDegradedWorktreeReadonly,
		},
		{
			name:       "worktrees permission denied",
			err:        errString("mkdir .worktrees: permission denied"),
			wantInfra:  true,
			wantReason: AgentDegradedWorktreePermissionDenied,
		},
		{
			name: "post-worktree setup failure",
			err: &PostWorktreeSetupError{
				Cmd: "make sync-embedded",
				Dir: "/repo/.worktrees/task-a",
				Err: errString("exit status 2"),
			},
			wantInfra:  true,
			wantReason: AgentDegradedWorktreeSetupFailed,
		},
		{
			name: "wrapped post-worktree setup failure",
			err: fmt.Errorf("claim task-a: %w", &PostWorktreeSetupError{
				Cmd: "make sync-embedded",
				Dir: "/repo/.worktrees/task-a",
				Err: errString("exit status 2"),
			}),
			wantInfra:  true,
			wantReason: AgentDegradedWorktreeSetupFailed,
		},
		{
			name:      "generic race is not infrastructure",
			err:       errString("race condition: concurrent claim already provisioned worktree for READY task"),
			wantInfra: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyInfraClaimError(tt.err)
			if got.IsInfra != tt.wantInfra {
				t.Fatalf("IsInfra = %v, want %v", got.IsInfra, tt.wantInfra)
			}
			if got.Reason != tt.wantReason {
				t.Fatalf("Reason = %q, want %q", got.Reason, tt.wantReason)
			}
			if tt.wantInfra && got.RecoverHint == "" {
				t.Fatal("RecoverHint is empty for infra classification")
			}
			if tt.wantReason == AgentDegradedWorktreeSetupFailed &&
				!strings.Contains(got.RecoverHint, "make sync-embedded") {
				t.Fatalf("RecoverHint = %q, want the configured command named", got.RecoverHint)
			}
		})
	}
}

func TestMarkAndClearAgentDegraded(t *testing.T) {
	t.Parallel()

	projectRoot, statePath := writeAgentHealthState(t)
	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Agents["coder-1"] = models.Agent{
		Role:         "coder",
		Status:       models.AgentStatusIdle,
		Provider:     "codex",
		PID:          1234,
		Heartbeat:    now,
		RegisteredAt: now,
	}
	testhelpers.WriteInitialState(t, statePath, state)

	err := MarkAgentDegraded(MarkAgentDegradedInput{
		ProjectRoot:    projectRoot,
		AgentID:        "coder-1",
		Role:           "coder",
		Reason:         string(AgentDegradedWorktreeCreateFailed),
		LastTask:       "task-a",
		CandidateTasks: []string{"task-a", "task-b"},
		LastError:      "cannot lock ref: Operation not permitted",
		RecoverHint:    "restart elsewhere",
		DegradedBy:     "test",
	})
	if err != nil {
		t.Fatalf("MarkAgentDegraded() error = %v", err)
	}

	readState, err := db.For(statePath).Read()
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	health, ok := readState.AgentHealth["coder-1"]
	if !ok {
		t.Fatal("agent health missing for coder-1")
	}
	if health.State != models.AgentHealthDegraded {
		t.Fatalf("health state = %q, want degraded", health.State)
	}
	if health.PID != 1234 || health.Provider != "codex" || health.RegisteredAt == nil || !health.RegisteredAt.Equal(now) {
		t.Fatalf("health epoch = pid %d provider %q registered_at %v, want current agent epoch", health.PID, health.Provider, health.RegisteredAt)
	}
	if len(readState.Anomalies) != 1 || readState.Anomalies[0].Type != "agent_degraded" {
		t.Fatalf("anomalies = %+v, want one agent_degraded anomaly", readState.Anomalies)
	}

	if err := ClearAgentDegraded(projectRoot, "coder-1"); err != nil {
		t.Fatalf("ClearAgentDegraded() error = %v", err)
	}
	readState, err = db.For(statePath).Read()
	if err != nil {
		t.Fatalf("Read() after clear error = %v", err)
	}
	if _, ok := readState.AgentHealth["coder-1"]; ok {
		t.Fatal("agent health still present after clear")
	}
}

func TestMarkAgentDegradedRejectsAuthorityActorMismatch(t *testing.T) {
	t.Parallel()

	projectRoot, statePath := writeAgentHealthState(t)
	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Agents["coder-1"] = models.Agent{
		Role:         "coder",
		Status:       models.AgentStatusIdle,
		Generation:   "generation-b",
		Heartbeat:    now,
		RegisteredAt: now,
	}
	state.Agents["coder-2"] = models.Agent{
		Role:         "coder",
		Status:       models.AgentStatusIdle,
		Generation:   "other-generation",
		Heartbeat:    now,
		RegisteredAt: now,
	}
	testhelpers.WriteInitialState(t, statePath, state)

	before := readStateBytes(t, statePath)
	authority := models.AgentAuthority{ID: "coder-1", Generation: "generation-b"}
	err := MarkAgentDegraded(MarkAgentDegradedInput{
		ProjectRoot: projectRoot,
		AgentID:     "coder-2",
		Role:        "coder",
		Reason:      string(AgentDegradedWorktreeCreateFailed),
		LastError:   "cannot create worktree",
		Authority:   &authority,
	})
	if err == nil {
		t.Fatal("MarkAgentDegraded() error = nil, want authority actor mismatch")
	}
	if !strings.Contains(err.Error(), "coder-1") || !strings.Contains(err.Error(), "coder-2") {
		t.Fatalf("MarkAgentDegraded() error = %q, want both authority and actor IDs", err)
	}

	after := readStateBytes(t, statePath)
	if !bytes.Equal(after, before) {
		t.Fatal("serialized state changed after authority actor mismatch")
	}
}

type errString string

func (e errString) Error() string {
	return string(e)
}

func writeAgentHealthState(t *testing.T) (projectRoot, statePath string) {
	t.Helper()
	projectRoot = t.TempDir()
	statePath, _ = testhelpers.SetupLizaDir(t, projectRoot)
	testhelpers.SetupPipelineConfig(t, projectRoot)
	return projectRoot, statePath
}

func TestClassifyInfraClaimErrorNoSecretEcho(t *testing.T) {
	t.Parallel()

	got := ClassifyInfraClaimError(errString("failed to create .worktrees/task-a: permission denied"))
	if !got.IsInfra {
		t.Fatal("expected infrastructure classification")
	}
	if strings.Contains(got.RecoverHint, "failed to create") {
		t.Fatalf("RecoverHint should be generic, got %q", got.RecoverHint)
	}
}
