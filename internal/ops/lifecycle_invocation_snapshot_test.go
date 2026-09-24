package ops

import (
	"reflect"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// D69 Step 2: lifecycle metrics capture the sprint from a lock-free snapshot,
// so a lifecycle command does not take an extra exclusive state read. Not
// parallel: it shortens the ordinary lock timeout so the pre-snapshot
// behavior fails fast.
func TestLifecycleInvocationCapturesSprintWhileStateLockHeld(t *testing.T) {
	t.Cleanup(db.SetDefaultLockTimeoutForTest(200 * time.Millisecond))
	root := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, root)
	state := testhelpers.CreateValidState()
	state.Sprint.Number = 7
	testhelpers.WriteInitialState(t, stateFile, state)
	testhelpers.HoldFileLock(t, stateFile)

	invocation := NewLifecycleInvocation(root)
	if invocation.warning != nil {
		t.Fatalf("lifecycle invocation while the state lock is held: %v", invocation.warning)
	}
	if want := CaptureLifecycleSprint(state.Sprint); !reflect.DeepEqual(invocation.sprint, want) {
		t.Fatalf("captured sprint %+v, want %+v", invocation.sprint, want)
	}
}
