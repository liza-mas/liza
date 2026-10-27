// Package legal holds every case the guard accepts outside ops and db.
package legal

import (
	"bytes"
	"io"
	"os"

	"c6fixture/internal/db"
	"c6fixture/internal/models"
	"c6fixture/internal/ops"
)

type journal struct{}

// Modify belongs to a concrete type unrelated to the Blackboard.
func (journal) Modify(fn func(*models.State) error) error { return nil }

// Write belongs to a concrete type unrelated to the Blackboard.
func (journal) Write(p []byte) (int, error) { return len(p), nil }

func readOnly(bb *db.Blackboard) (string, error) {
	state, err := bb.Patient().ReadSnapshot()
	if err != nil {
		return "", err
	}
	_ = state
	return bb.GetStatePath(), nil
}

func lockCallback() error {
	return ops.WithLock(func() error { return nil })
}

func namedOperation(bb *db.Blackboard) error {
	return ops.RegisterAgent(bb, "coder-1")
}

func snapshotPredicate(state *models.State) bool {
	pending := func(s *models.State) bool { return s.Mode == "" }
	return pending(state)
}

func unrelatedWrites(f *os.File, w io.Writer) {
	var buf bytes.Buffer
	_, _ = buf.Write([]byte("x"))
	_, _ = f.Write([]byte("x"))
	_, _ = w.Write([]byte("x"))
	_, _ = journal{}.Write([]byte("x"))
	_ = journal{}.Modify(nil)
}

func livenessSideRecord() error {
	return db.WriteLivenessRecord("p", "coder-1")
}

var _ = []any{readOnly, lockCallback, namedOperation, snapshotPredicate, unrelatedWrites, livenessSideRecord}
