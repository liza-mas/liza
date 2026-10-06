package ops

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestRequestBlackboardBoundsLegacyAndOperatorAcquisitions(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"fenced", "no identity", "request only", "transition only", "invalid identity", "no context", "operator", "missing generation"} {
		t.Run(kind, func(t *testing.T) {
			_, statePath, opts := metadataLifecycleFixture(t, "assess-blocked")
			authority := &models.AgentAuthority{ID: "orchestrator-1", Generation: "caller-generation"}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			opts.RetryContext = ctx
			switch kind {
			case "no identity":
				opts.RequestID, opts.ExpectedTransition = "", ""
			case "request only":
				opts.ExpectedTransition = ""
			case "transition only":
				opts.RequestID = ""
			case "invalid identity":
				opts.RequestID = "request with whitespace"
			case "no context":
				opts.RetryContext = nil
			case "operator":
				authority = nil
			case "missing generation":
				authority.Generation = ""
			}
			testhelpers.HoldFileLock(t, statePath)
			_, err := RequestBlackboard(statePath, authority, opts).WithLockTimeout(20 * time.Millisecond).Read()
			if kind == "fenced" {
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("fenced acquisition ignored caller cancellation: %v", err)
				}
			} else if !filelock.IsLockErrorType(err, filelock.LockErrorTimeout) {
				t.Fatalf("%s acquisition did not retain its bounded timeout: %v", kind, err)
			}
		})
	}
}

func TestRequestBlackboardNeverReplaysEnteredCallback(t *testing.T) {
	t.Parallel()
	_, statePath, opts := metadataLifecycleFixture(t, "assess-blocked")
	agent := testhelpers.RegisteredTestAgent("coder")
	if err := db.For(statePath).Modify(func(state *models.State) error {
		state.Agents["coder-1"] = agent
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	authority := models.AgentAuthority{ID: "coder-1", Generation: agent.Generation}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	opts.RetryContext = ctx
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	inner := filelock.NewLockTimeout(errors.New("nested operation timed out after entering mutation"))
	calls := 0
	err = ModifyWithAgentAuthority(RequestBlackboard(statePath, &authority, opts), authority, func(state *models.State) error {
		calls++
		state.FindTask("target").Description = "uncommitted candidate"
		return inner
	})
	if !errors.Is(err, inner) || calls != 1 {
		t.Fatalf("entered callback replayed or error identity changed: calls=%d error=%v", calls, err)
	}
	after, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("failed entered callback published its candidate")
	}
}
