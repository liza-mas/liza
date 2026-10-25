package supplement

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

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
	session := &ops.ValidationSession{Environment: pinnedSessionEnvironment(), Execution: p.Execution, ToolName: "test", ConfigDigest: p.ConfigDigest, ForceCheck: true}
	var err error
	for i := 0; i < max(1, p.Repeat); i++ {
		_, err = ops.PrepareValidationPreflightContext(context.Background(), f.project, "task-1", s.Agent, filepath.Join(f.project, ".worktrees/task-1"), session)
		if (err == nil) != (s.Contains == "") {
			t.Fatalf("preflight %d: error %v, want text %q", i, err, s.Contains)
		}
		if err != nil && !strings.Contains(err.Error(), s.Contains) {
			t.Fatalf("preflight %d: error %v does not name %q", i, err, s.Contains)
		}
	}
}
