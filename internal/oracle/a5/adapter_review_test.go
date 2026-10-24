package a5

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
)

// pinnedSessionEnvironment is the validation session environment. The readiness
// fingerprint hashes it, so it holds no temporary or host path.
func pinnedSessionEnvironment() []string {
	return []string{"PATH=/usr/bin:/bin", "HOME=/nonexistent"}
}

func adapterStep(t *testing.T, f *fixture, s step) {
	t.Helper()
	authority := models.AgentAuthority{ID: s.Agent, Generation: "generation-1"}
	session := &ops.ValidationSession{Environment: pinnedSessionEnvironment(), Execution: "local", ToolName: "test", ForceCheck: true}
	switch s.Adapter {
	case "claim", "no-claim":
		role := s.Agent[:strings.LastIndex(s.Agent, "-")]
		result, err := ops.ClaimReviewerTask(ops.ClaimReviewerTaskInput{ProjectRoot: f.project, AgentID: s.Agent, Role: role, Authority: &authority, Session: session})
		if s.Adapter == "no-claim" {
			// The declared outcome is the diversity refusal: no claim, no effect.
			var refusal *ops.PreconditionError
			if result != nil || !errors.As(err, &refusal) || refusal.Reason != "no reviewable tasks found" {
				t.Fatalf("diversity filter did not refuse the same-provider reviewer: claimed=%v error=%v", result != nil, err)
			}
			return
		}
		if err != nil {
			t.Fatalf("reviewer claim: %v", err)
		}
		if result == nil || result.TaskID != "task-1" {
			t.Fatal("reviewer claim did not select the unique intended candidate")
		}
	case "preflight", "preflight-missing":
		_, err := ops.PrepareValidationPreflightContext(context.Background(), f.project, "task-1", s.Agent, filepath.Join(f.project, ".worktrees/task-1"), session)
		if s.Adapter == "preflight-missing" {
			if err == nil || !strings.Contains(err.Error(), "environment_missing") {
				t.Fatalf("preflight did not reach environment_missing: %v", err)
			}
			return
		}
		if err != nil {
			t.Fatal(err)
		}
	case "acceptance-block":
		_, err := ops.ClaimTaskWithAuthority(f.project, "task-1", authority)
		var refusal *ops.AcceptanceEvidenceError
		if !errors.As(err, &refusal) || refusal.Class != ops.AcceptanceFaultContent || refusal.Claim == nil {
			t.Fatalf("claim did not produce a content refusal: %v", err)
		}
		blocked, err := ops.BlockAcceptanceRefusedTask(f.project, authority, refusal)
		if err != nil || !blocked {
			t.Fatalf("acceptance block: blocked=%v error=%v", blocked, err)
		}
	default:
		t.Fatalf("unknown adapter %s", s.Adapter)
	}
}
