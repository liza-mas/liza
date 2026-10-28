package a9

import (
	"bytes"
	"testing"
)

// TestOracleA9OrchestratorNotes freezes note consumption after a turn whose
// trigger is not HUMAN_NOTE (I-12, assessed-target variant): a BLOCKED_TASKS
// turn that records an assessment consumes the rendered notes aimed at the
// assessed task and the notes for all targets, and leaves a note aimed at a task
// it did not assess unseen, so that note wakes the orchestrator again.
func TestOracleA9OrchestratorNotes(t *testing.T) {
	t.Run("i12-assessed", func(t *testing.T) {
		p, r, launch := orchRun(t, "i12-assessed")
		after := p.state(t)
		if bytes.Contains(launch, []byte("orchestrator_seen_at")) {
			t.Fatal("I-12 notes must be stamped after, not before, the turn")
		}
		if bytes.Count(after, []byte("orchestrator_seen_at:")) != 2 {
			t.Fatal("I-12 an assessing turn must stamp the assessed target's note and the note for all, and no other")
		}
		if bytes.Count(after, []byte("event: orchestrator_assessment")) != 1 {
			t.Fatal("I-12 the turn must record one assessment of the blocked task")
		}
		p.check(t, r)
	})
}
