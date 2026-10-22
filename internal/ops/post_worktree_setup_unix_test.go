//go:build !windows

package ops

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
)

func TestRunPostWorktreeCmd_ReconcileUsesCallerGeneration(t *testing.T) {
	f := newSetupFixture(t)
	f.seedHealth(t, "code-reviewer-1", AgentDegradedWorktreeSetupFailed, "task-1", time.Now().UTC().Add(-time.Hour))
	authority := models.AgentAuthority{ID: "coder-1", Generation: "gen-a"}
	// FIFOs hand control back and forth with the running setup command:
	// opening one blocks until the other side opens it.
	started := filepath.Join(f.worktree, "started")
	proceed := filepath.Join(f.worktree, "proceed")
	for _, fifo := range []string{started, proceed} {
		if err := syscall.Mkfifo(fifo, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	replaced := make(chan error, 1)
	go func() {
		if _, err := os.ReadFile(started); err != nil {
			replaced <- err
			return
		}
		// The caller's registration is replaced while its setup runs.
		err := db.For(f.statePath).Modify(func(state *models.State) error {
			agent := state.Agents["coder-1"]
			agent.Generation = "gen-z"
			state.Agents["coder-1"] = agent
			return nil
		})
		if writeErr := os.WriteFile(proceed, []byte("go\n"), 0o600); err == nil {
			err = writeErr
		}
		replaced <- err
	}()

	err := RunPostWorktreeCmd(f.root, "echo up > started; cat proceed > /dev/null", f.worktree, &authority)
	if replaceErr := <-replaced; replaceErr != nil {
		t.Fatal(replaceErr)
	}

	if err != nil {
		t.Fatalf("RunPostWorktreeCmd() = %v, want the setup itself to pass", err)
	}
	if got := f.readOutcome(t); !got.OK {
		t.Errorf("outcome = %+v, want ok", got)
	}
	if _, ok := f.health(t)["code-reviewer-1"]; !ok {
		t.Fatal("a replaced caller's reconcile changed agent health")
	}

	if err := RunPostWorktreeCmd(f.root, "true", f.worktree, nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.health(t)["code-reviewer-1"]; ok {
		t.Error("an operator-path pass did not reconcile")
	}
}
