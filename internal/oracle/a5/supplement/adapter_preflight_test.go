package supplement

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/ops"
)

// pinnedSessionEnvironment is the validation session environment. The readiness
// fingerprint hashes it, so it holds no temporary or host path.
func pinnedSessionEnvironment() []string {
	return []string{"PATH=/usr/bin:/bin", "HOME=/nonexistent"}
}

func init() { adapters["preflight"] = preflightStep }

// preflightParams is the validation session a preflight step presents. The
// readiness fingerprint hashes the pinned environment, so it holds no host path.
type preflightParams struct {
	Execution    string `json:"execution"`
	ConfigDigest string `json:"config_digest,omitempty"`
	Repeat       int    `json:"repeat,omitempty"`
	// NoForce lets the process-local retry cooldown apply, as for a supervisor
	// polling an unchanged failure; the default forces every check.
	NoForce bool `json:"no_force,omitempty"`
	// FirstContains is the error text of every call but the last (default:
	// the step's Contains).
	FirstContains string `json:"first_contains,omitempty"`
	// PreparationError is the supervisor's own failure to prepare the session.
	PreparationError string `json:"preparation_error,omitempty"`
	// Ambient presents no session: the command must prove from its own process
	// environment that it runs inside the registered provider session.
	Ambient           bool   `json:"ambient,omitempty"`
	AmbientAgent      string `json:"ambient_agent,omitempty"`
	AmbientGeneration string `json:"ambient_generation,omitempty"`
	// Worktree is "" (the task's worktree), "none" (no path at all) or
	// "missing" (a path that is not a worktree).
	Worktree string `json:"worktree,omitempty"`
}

func (p preflightParams) worktreePath(f *fixture) string {
	switch p.Worktree {
	case "none":
		return ""
	case "missing":
		return filepath.Join(f.project, ".worktrees/missing")
	}
	return filepath.Join(f.project, ".worktrees/task-1")
}

func (p preflightParams) session(t *testing.T) *ops.ValidationSession {
	t.Helper()
	if p.Ambient {
		t.Setenv(brand.EnvPrefix+"_AGENT_ID", p.AmbientAgent)
		t.Setenv(generationVariable(), p.AmbientGeneration)
		return nil
	}
	session := &ops.ValidationSession{Environment: pinnedSessionEnvironment(), Execution: p.Execution, ToolName: "test", ConfigDigest: p.ConfigDigest, ForceCheck: !p.NoForce}
	if p.PreparationError != "" {
		session.PreparationError = errors.New(p.PreparationError)
	}
	return session
}

// preflightStep drives the preflight preparation the supervisor and the claim
// paths call, with an explicit session so no ambient environment decides the
// outcome. The step declares the error text it must reach (empty: success).
func preflightStep(t *testing.T, f *fixture, s step) {
	t.Helper()
	var p preflightParams
	if err := json.Unmarshal(s.Params, &p); err != nil {
		t.Fatal(err)
	}
	session := p.session(t)
	calls := max(1, p.Repeat)
	for i := 0; i < calls; i++ {
		want := s.Contains
		if i < calls-1 && p.FirstContains != "" {
			want = p.FirstContains
		}
		_, err := ops.PrepareValidationPreflightContext(context.Background(), f.project, "task-1", s.Agent, p.worktreePath(f), session)
		if (err == nil) != (want == "") {
			t.Fatalf("preflight %d: error %v, want text %q", i, err, want)
		}
		if err != nil && !strings.Contains(err.Error(), want) {
			t.Fatalf("preflight %d: error %v does not name %q", i, err, want)
		}
	}
}
