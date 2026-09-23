package ops

import (
	stderrors "errors"
	"os"
	"path/filepath"
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

// The load-bearing security property: a secret the child knows and this process
// does not must not reach the error, and therefore neither logs nor state.
//
// DATABASE_URL is chosen deliberately. secretmask recognizes credential-shaped
// key names (API_KEY, *_TOKEN, *_SECRET, *_PASSWORD, ...) and DATABASE_URL
// matches none of them, so any masking-based defense would fail here. The
// guarantee comes from never capturing child output at all.
func TestRunPostWorktreeCmd_DoesNotCaptureChildOutput(t *testing.T) {
	t.Parallel()
	worktree := t.TempDir()
	const secret = "postgres://user:hunter2@db.internal/app"
	envPath := filepath.Join(worktree, ".env")
	if err := os.WriteFile(envPath, []byte("DATABASE_URL="+secret+"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if strings.Contains(strings.Join(os.Environ(), " "), secret) {
		t.Fatal("precondition: the secret must not be in the parent environment")
	}

	// A realistic failing setup command: source the env file, print a connection
	// diagnostic, then fail.
	err := RunPostWorktreeCmd(". ./.env && echo \"connecting to $DATABASE_URL\" && exit 1", worktree)
	if err == nil {
		t.Fatal("RunPostWorktreeCmd() error = nil, want failure")
	}
	var setupErr *PostWorktreeSetupError
	if !stderrors.As(err, &setupErr) {
		t.Fatalf("error = %v, want *PostWorktreeSetupError", err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Errorf("Error() = %q, want no child-known secret anywhere in the diagnostic", err.Error())
	}
	if strings.Contains(err.Error(), "hunter2") {
		t.Errorf("Error() = %q, leaks a credential component", err.Error())
	}
	// The diagnostic still identifies what to rerun and where.
	if !strings.Contains(err.Error(), worktree) {
		t.Errorf("Error() = %q, want the worktree named for reproduction", err.Error())
	}
}
