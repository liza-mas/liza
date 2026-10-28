package supplement

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/paths"
)

func init() { adapters["replay"] = replayStep }

// replayContentionTimeout is the state-lock wait while the fixture holds the
// lock: long enough to be a real wait, short enough to keep the case fast.
const replayContentionTimeout = 10 * time.Millisecond

// replayParams is the reached verdict a reviewer submits while the state lock
// is contended. Generation is the registration generation the reviewer carries;
// the case state decides whether it is still current when the outbox drains.
type replayParams struct {
	Verdict    string `json:"verdict"`
	Generation string `json:"generation"`
	RequestID  string `json:"request_id"`
}

// outboxEnvelopes counts the private verdict envelopes awaiting replay.
func outboxEnvelopes(t testing.TB, project string) int {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(paths.New(project).LizaDir(), "pending-verdicts"))
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
			count++
		}
	}
	return count
}

// holdStateLock holds the project's state lock until the returned release runs.
func holdStateLock(t *testing.T, statePath string) (release func()) {
	t.Helper()
	ready, free, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		done <- filelock.New(statePath).WithLockOperation("oracle-contention", func() error {
			close(ready)
			<-free
			return nil
		})
	}()
	select {
	case <-ready:
	case err := <-done:
		t.Fatalf("hold state lock: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("state lock holder did not start")
	}
	released := false
	release = func() {
		if !released {
			released = true
			close(free)
			if err := <-done; err != nil {
				t.Errorf("release state lock: %v", err)
			}
		}
	}
	t.Cleanup(release)
	return release
}

// replayStep leaves a reached verdict in the private outbox through the exported
// submit entry while the fixture holds the state lock, then drains it with
// ReplayPendingVerdicts once the lock is free. The observation records the
// outbox before and after the drain; the state golden records the effect.
func replayStep(t *testing.T, f *fixture, s step) {
	t.Helper()
	var p replayParams
	if err := json.Unmarshal(s.Params, &p); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.SetDefaultLockTimeoutForTest(replayContentionTimeout))
	statePath := paths.New(f.project).StatePath()
	snapshot, err := db.For(statePath).ReadSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	task := snapshot.FindTask("task-1")
	if task == nil {
		t.Fatal("fixture has no task-1")
	}
	opts := ops.LifecycleRequestOptions{RequestID: p.RequestID, ExpectedTransition: models.TaskTransitionID(task)}
	authority := models.AgentAuthority{ID: s.Agent, Generation: p.Generation}

	before := outboxEnvelopes(t, f.project)
	release := holdStateLock(t, statePath)
	_, submitErr := ops.SubmitVerdictWithAuthorityAndOptions(f.project, "task-1", p.Verdict, "", authority, "", f.labels["candidate"], opts)
	if !filelock.IsLockErrorType(submitErr, filelock.LockErrorTimeout) {
		t.Fatalf("submit under contention: error %v, want a state-lock acquisition timeout", submitErr)
	}
	left := outboxEnvelopes(t, f.project)
	release()
	if err := ops.ReplayPendingVerdicts(context.Background(), f.project, s.Agent); err != nil {
		t.Fatalf("replay: %v", err)
	}
	after := outboxEnvelopes(t, f.project)
	f.outbox = fmt.Appendf(nil, "envelopes before submit: %d\nenvelopes after the contended submit: %d\nenvelopes after replay: %d\n", before, left, after)
}
