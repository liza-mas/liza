package testguard

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestGenerationFencedRecoveryDocumentation(t *testing.T) {
	t.Parallel()

	repoRoot := generationRecoveryRepoRoot(t)
	surfaces := []string{
		"INVARIANTS.md",
		"specs/architecture/supervision-model.md",
		"specs/build/1.4 - Worktree Isolation.md",
		"docs/liza-hardened-mas.md",
	}
	required := []string{
		"current-generation authority",
		"every agent-authenticated lifecycle write",
		"inside the same `blackboard.modify`",
		"per-agent lifecycle lock",
		"registration acquires the lifecycle lock before the blackboard lock",
		"no provider backend effect runs inside `blackboard.modify`",
		"built-in providers complete start before wait",
		"complete blocking legacy call",
		"lock timeout, state-read failure, generation mismatch, setup failure, or process-start failure",
		"no successful start event or state rewrite",
		"lease-first",
		"fresh heartbeat and unexpired lease",
		"unknown/degraded",
		"raw dead or mismatched pid observation",
		"registration and watcher diagnostics",
		"registered pid",
		"observer-visible pid",
		"correlation unavailable",
		"cannot authorize takeover",
		"live agent takeover",
		"deterministic current-generation reviewer",
		"immutable approval actors, quorum, provider-diversity evidence, review commit, and reviewer role",
		"git not advanced",
		"git already advanced while task state remains approved",
		"assigned_to, lease_expires, worktree, base_commit, physical task artifact, and reviewer affinity",
		"reuses a healthy worktree",
		"reattaches a valid task branch",
		"recreates from integration only when no reusable valid artifact exists",
		"fails closed without deleting unclassifiable artifacts",
		"does not serialize issue #129",
		"does not change rbac permissions",
	}
	forbidden := []string{
		"serializes issue #129",
		"changes rbac permissions",
		"dead or mismatched registered pids do not count as live capacity",
		"missing or corrupt worktree state is recreated from integration",
		"deletes unclassifiable artifacts",
		"recreates unclassifiable artifacts",
	}

	for _, surface := range surfaces {
		t.Run(surface, func(t *testing.T) {
			content, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(surface)))
			if err != nil {
				t.Fatalf("read documentation surface: %v", err)
			}
			text := strings.ToLower(string(content))
			for _, marker := range required {
				if !strings.Contains(text, marker) {
					t.Errorf("missing generation-recovery contract marker %q", marker)
				}
			}
			for _, marker := range forbidden {
				if strings.Contains(text, marker) {
					t.Errorf("forbidden generation-recovery claim %q", marker)
				}
			}
		})
	}
}

func generationRecoveryRepoRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
}
