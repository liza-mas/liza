package ops

import (
	"strings"
	"testing"
)

// The command is operator-supplied config that reaches state.yaml through the
// agent-degraded record, so it is masked and capped.
func TestNewPostWorktreeSetupError_MasksAndBoundsCommand(t *testing.T) {
	t.Setenv("LIZA_TEST_TOKEN", "super-secret-value")
	err := newPostWorktreeSetupError(
		"deploy --token super-secret-value",
		"/repo/.worktrees/task-1",
		errString("exit status 1"),
	)
	if strings.Contains(err.Error(), "super-secret-value") {
		t.Errorf("Error() = %q, want the secret masked", err.Error())
	}
	if !strings.Contains(err.Cmd, "***") {
		t.Errorf("Cmd = %q, want redaction marker", err.Cmd)
	}

	long := newPostWorktreeSetupError(strings.Repeat("x", postWorktreeCmdMaxBytes*3), "/repo", errString("exit status 1"))
	if len(long.Cmd) > postWorktreeCmdMaxBytes+len("... [truncated]") {
		t.Errorf("len(Cmd) = %d, want capped", len(long.Cmd))
	}
	if !strings.HasSuffix(long.Cmd, "... [truncated]") {
		t.Errorf("Cmd = %q, want a truncation marker", long.Cmd)
	}
}
