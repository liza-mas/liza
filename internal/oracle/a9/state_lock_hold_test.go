package a9

import (
	"bytes"
	"context"
	"fmt"
	"testing"
)

// TestOracleA9StateLockHoldRecovery freezes the state effects of automatic
// state-lock-hold recovery through the exported ops.RecoverStateLockHolds
// (adapter_state_lock_hold_test.go): a hold from a later publication resumes
// the task by audited unassigned continuation, and a Git-backed gate failure
// publishes only its refusal fields, once. The scenarios use disposable state
// and Git, no process facility, so none is Linux-scoped (D53).
//
//   - recovered: a task without a worktree continues unassigned with no
//     iteration, an unblocked history entry and a lifecycle receipt; a second
//     poll writes nothing.
//   - refused: the task names a worktree that is absent, so the gate's
//     ValidateWorktreeHealth fails and the refusal reason is published into the
//     hold; a second poll over the unchanged refusal writes nothing.
func TestOracleA9StateLockHoldRecovery(t *testing.T) {
	t.Run("recovered", func(t *testing.T) {
		p := newStateScenario(t, "state-lock-hold-recovered")
		count, err := recoverStateLockHolds(context.Background(), p)
		if err != nil || count != 1 {
			t.Fatalf("state lock hold recovery: count %d, error %v; want 1 continuation", count, err)
		}
		after := p.state(t)
		for _, want := range []string{"event: unblocked", "continuation: true", "new_iteration: false", "status: DRAFT_CODE"} {
			if !bytes.Contains(after, []byte(want)) {
				t.Fatalf("recovered state missing %q", want)
			}
		}
		if bytes.Contains(after, []byte("state_lock_hold:")) || bytes.Contains(after, []byte("assigned_to:")) {
			t.Fatal("recovery must clear the hold and leave the task unassigned")
		}
		requireNoRepeatWrite(t, p)
		p.check(t, invocation{code: 0, stdout: fmt.Sprintf("recovered %d\n", count)})
	})
	t.Run("refused", func(t *testing.T) {
		p := newStateScenario(t, "state-lock-hold-refused")
		count, err := recoverStateLockHolds(context.Background(), p)
		if err != nil || count != 0 {
			t.Fatalf("state lock hold refusal: count %d, error %v; want 0 continuations", count, err)
		}
		after := p.state(t)
		for _, want := range []string{"status: BLOCKED", "refused_reason: worktree_unhealthy", "refused_fingerprint:"} {
			if !bytes.Contains(after, []byte(want)) {
				t.Fatalf("refused state missing %q", want)
			}
		}
		if bytes.Contains(after, []byte("event: unblocked")) {
			t.Fatal("a refused recovery must not unblock the task")
		}
		requireNoRepeatWrite(t, p)
		p.check(t, invocation{code: 0, stdout: fmt.Sprintf("recovered %d\n", count)})
	})
}

// requireNoRepeatWrite polls again over the published state: nothing is left to
// recover or to record, so the state bytes must not change.
func requireNoRepeatWrite(t *testing.T, p *scenario) {
	t.Helper()
	before := p.state(t)
	count, err := recoverStateLockHolds(context.Background(), p)
	if err != nil || count != 0 {
		t.Fatalf("repeat poll: count %d, error %v; want 0", count, err)
	}
	if !bytes.Equal(before, p.state(t)) {
		t.Fatal("an unchanged poll wrote state")
	}
}
