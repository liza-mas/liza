package db

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	lizaerrors "github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/models"
)

// These tests set the package-level readDecodeTestHook, so none of them may
// run in parallel.

const readHoldProbeTimeout = 200 * time.Millisecond

func newReadHoldFixture(t *testing.T, goal string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "state.yaml")
	if err := New(path).Write(&models.State{Goal: models.Goal{Description: goal}}); err != nil {
		t.Fatal(err)
	}
	return path
}

func setReadDecodeTestHook(t *testing.T, hook func()) {
	t.Helper()
	previous := readDecodeTestHook
	readDecodeTestHook = hook
	t.Cleanup(func() { readDecodeTestHook = previous })
}

func TestReadDecodesAfterReleasingStateLock(t *testing.T) {
	reads := map[string]func(*Blackboard) (*models.State, error){
		"Read": (*Blackboard).Read,
		"retry-context ReadContext": func(bb *Blackboard) (*models.State, error) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			return bb.WithLockRetryContext(ctx).ReadContext(ctx)
		},
	}
	for name, read := range reads {
		t.Run(name, func(t *testing.T) {
			// GIVEN a published state and a probe that takes the state lock
			// at the moment the read starts decoding
			path := newReadHoldFixture(t, "held")
			probed := false
			var probeErr error
			setReadDecodeTestHook(t, func() {
				probed = true
				probeErr = filelock.New(path).WithTimeout(readHoldProbeTimeout).WithLockOperation("probe", func() error { return nil })
			})

			// WHEN the state is read
			state, err := read(New(path))

			// THEN another holder could take the lock during the decode
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			if !probed {
				t.Fatal("read never reached the decode seam")
			}
			if probeErr != nil {
				t.Fatalf("state lock was still held while decoding: %v", probeErr)
			}
			if state.Goal.Description != "held" {
				t.Fatalf("read returned goal %q, want %q", state.Goal.Description, "held")
			}
		})
	}
}

func TestReadReturnsPublicationCapturedUnderLock(t *testing.T) {
	// GIVEN a read that is about to decode while a writer publishes a new state
	path := newReadHoldFixture(t, "old")
	var writeErr error
	setReadDecodeTestHook(t, func() {
		writeErr = New(path).WithLockTimeout(readHoldProbeTimeout).Write(&models.State{Goal: models.Goal{Description: "new"}})
	})

	// WHEN the pending read finishes
	pending, err := New(path).Read()

	// THEN it returns the publication it read under the lock
	if err != nil {
		t.Fatalf("pending read: %v", err)
	}
	if writeErr != nil {
		t.Fatalf("writer could not publish while the read was decoding: %v", writeErr)
	}
	if pending.Goal.Description != "old" {
		t.Fatalf("pending read returned goal %q, want the captured %q", pending.Goal.Description, "old")
	}

	// AND a subsequent read sees the writer's publication
	readDecodeTestHook = nil
	next, err := New(path).Read()
	if err != nil {
		t.Fatalf("subsequent read: %v", err)
	}
	if next.Goal.Description != "new" {
		t.Fatalf("subsequent read returned goal %q, want %q", next.Goal.Description, "new")
	}
}

func TestReadReportsMalformedStateAsSchemaError(t *testing.T) {
	// GIVEN a state file that is not valid YAML
	path := filepath.Join(t.TempDir(), "state.yaml")
	if err := os.WriteFile(path, []byte("goal: [unterminated\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// WHEN it is read through the locked path
	state, err := New(path).Read()

	// THEN the read fails with the schema error, not an absent state
	var schemaErr *lizaerrors.StateSchemaError
	if !errors.As(err, &schemaErr) {
		t.Fatalf("read of malformed state: err=%v, want *errors.StateSchemaError", err)
	}
	if schemaErr.Operation != "state read" {
		t.Fatalf("schema error operation %q, want %q", schemaErr.Operation, "state read")
	}
	if state != nil {
		t.Fatalf("read of malformed state returned a state: %+v", state)
	}
}

func TestReadContextCanceledBeforeDecodeReturnsCancellation(t *testing.T) {
	// GIVEN a read whose context is canceled after its byte read
	path := newReadHoldFixture(t, "canceled")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	setReadDecodeTestHook(t, cancel)

	// WHEN the read continues to decoding
	state, err := New(path).ReadContext(ctx)

	// THEN it reports the cancellation instead of a decoded state
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("read canceled before decode: err=%v, want context.Canceled", err)
	}
	if state != nil {
		t.Fatalf("read canceled before decode returned a state: %+v", state)
	}
}
