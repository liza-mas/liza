package db

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
)

const (
	livenessAgent      = "coder-1"
	livenessGeneration = "gen-current"
	livenessTask       = "task-1"
)

// writeLivenessState writes a state whose agent is assigned livenessTask with
// an hour-old heartbeat and a lease ending at lease.
func writeLivenessState(t *testing.T, lease time.Time, mutate func(*models.State)) (string, *Blackboard) {
	t.Helper()
	statePath := filepath.Join(t.TempDir(), "state.yaml")
	now := time.Now().UTC()
	agentID, taskID := livenessAgent, livenessTask
	agentLease, taskLease := lease, lease
	state := &models.State{
		Version: 1,
		Goal:    models.Goal{ID: "goal-1", Status: models.GoalStatusInProgress, Created: now},
		Tasks: []models.Task{{
			ID: taskID, Description: "task", Status: models.TaskStatusImplementing, Priority: 1,
			SpecRef: "spec.md", DoneWhen: "done", Created: now,
			AssignedTo: &agentID, LeaseExpires: &taskLease,
		}},
		Agents: map[string]models.Agent{agentID: {
			Role: "coder", Status: models.AgentStatusWorking, Generation: livenessGeneration,
			CurrentTask: &taskID, Heartbeat: now.Add(-time.Hour), LeaseExpires: &agentLease,
		}},
		Config: models.Config{IntegrationBranch: "main"},
	}
	if mutate != nil {
		mutate(state)
	}
	bb := New(statePath)
	if err := bb.Write(state); err != nil {
		t.Fatalf("write state: %v", err)
	}
	return statePath, bb
}

func publishLiveness(t *testing.T, statePath string, record LivenessRecord) {
	t.Helper()
	if record.AgentID == "" {
		record.AgentID = livenessAgent
	}
	if record.Generation == "" {
		record.Generation = livenessGeneration
	}
	if err := WriteLivenessRecord(statePath, record); err != nil {
		t.Fatalf("write liveness record: %v", err)
	}
}

// persistedState decodes state.yaml without the liveness overlay.
func persistedState(t *testing.T, statePath string) *models.State {
	t.Helper()
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}
	state, err := decodeState(data, "test")
	if err != nil {
		t.Fatalf("decode state file: %v", err)
	}
	return state
}

func assertTime(t *testing.T, label string, got *time.Time, want time.Time) {
	t.Helper()
	if got == nil || !got.Equal(want) {
		t.Fatalf("%s = %v, want %v", label, got, want)
	}
}

func TestLivenessRecordOverlaysEveryReadWithoutRewritingState(t *testing.T) {
	// GIVEN a state whose agent lease is about to expire
	base := time.Now().UTC().Truncate(time.Second)
	statePath, bb := writeLivenessState(t, base.Add(time.Minute), nil)
	if _, err := bb.ReadCached(); err != nil { // warm the cache before the beat
		t.Fatalf("warm cache: %v", err)
	}
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}

	// WHEN the agent's generation publishes a record
	heartbeat, lease := base.Add(time.Second), base.Add(time.Hour)
	publishLiveness(t, statePath, LivenessRecord{Seq: 10, Heartbeat: heartbeat, LeaseExpires: lease})

	// THEN state.yaml is untouched and every read path observes the renewal
	after, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("publishing a liveness record rewrote state.yaml")
	}
	reads := map[string]func() (*models.State, error){
		"Read": bb.Read, "ReadSnapshot": bb.ReadSnapshot, "ReadCached": bb.ReadCached,
	}
	for name, read := range reads {
		state, err := read()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		agent := state.Agents[livenessAgent]
		if !agent.Heartbeat.Equal(heartbeat) || agent.LivenessSeq != 10 {
			t.Fatalf("%s agent heartbeat=%v seq=%d, want %v and 10", name, agent.Heartbeat, agent.LivenessSeq, heartbeat)
		}
		assertTime(t, name+" agent lease", agent.LeaseExpires, lease)
		assertTime(t, name+" task lease", state.FindTask(livenessTask).LeaseExpires, lease)
	}
	if got := persistedState(t, statePath).Agents[livenessAgent].LivenessSeq; got != 0 {
		t.Fatalf("persisted liveness_seq = %d, want 0 before any mutation", got)
	}

	// AND the cache keeps no overlay: a newer record is visible on the next call
	newer := base.Add(2 * time.Hour)
	publishLiveness(t, statePath, LivenessRecord{Seq: 11, Heartbeat: base.Add(2 * time.Second), LeaseExpires: newer})
	cached, err := bb.ReadCached()
	if err != nil {
		t.Fatal(err)
	}
	assertTime(t, "cached agent lease after newer record", cached.Agents[livenessAgent].LeaseExpires, newer)
}

func TestLivenessRecordRenewsOnlyLeasesTheLockedBeatRenewed(t *testing.T) {
	base := time.Now().UTC().Truncate(time.Second)
	original := base.Add(time.Minute)
	renewed := base.Add(time.Hour)
	other := "coder-2"

	cases := []struct {
		name            string
		mutate          func(*models.State)
		record          LivenessRecord
		wantTaskLease   *time.Time
		wantReviewLease *time.Time
	}{
		{
			name:          "task assigned to another agent keeps its lease",
			mutate:        func(s *models.State) { s.Tasks[0].AssignedTo = &other },
			wantTaskLease: &original,
		},
		{
			name:   "cleared task lease stays cleared",
			mutate: func(s *models.State) { s.Tasks[0].LeaseExpires = nil },
		},
		{
			name:            "review lease of the active session task is renewed",
			mutate:          reviewing(original),
			record:          LivenessRecord{ReviewTask: livenessTask, ReviewLeaseExpires: renewed, ReviewSeq: 10},
			wantReviewLease: &renewed,
		},
		{
			name:            "review lease of another session task is kept",
			mutate:          reviewing(original),
			record:          LivenessRecord{ReviewTask: "task-2", ReviewLeaseExpires: renewed, ReviewSeq: 10},
			wantReviewLease: &original,
		},
		{
			name:            "review lease without a session renewal is kept",
			mutate:          reviewing(original),
			wantReviewLease: &original,
		},
		{
			name: "folded review renewal does not apply again",
			mutate: func(s *models.State) {
				reviewing(original)(s)
				agent := s.Agents[livenessAgent]
				agent.LivenessSeq = 7
				s.Agents[livenessAgent] = agent
			},
			record:          LivenessRecord{ReviewTask: livenessTask, ReviewLeaseExpires: renewed, ReviewSeq: 7},
			wantReviewLease: &original,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			statePath, bb := writeLivenessState(t, original, tc.mutate)
			record := tc.record
			record.Seq, record.Heartbeat, record.LeaseExpires = 10, base, renewed
			publishLiveness(t, statePath, record)

			state, err := bb.ReadSnapshot()
			if err != nil {
				t.Fatal(err)
			}
			task := state.FindTask(livenessTask)
			assertTime(t, "agent lease", state.Agents[livenessAgent].LeaseExpires, renewed)
			assertOptionalTime(t, "task lease", task.LeaseExpires, tc.wantTaskLease)
			assertOptionalTime(t, "review lease", task.ReviewLeaseExpires, tc.wantReviewLease)
		})
	}
}

func reviewing(lease time.Time) func(*models.State) {
	return func(s *models.State) {
		reviewer := livenessAgent
		s.Tasks[0].Status = models.TaskStatusReviewing
		s.Tasks[0].AssignedTo, s.Tasks[0].LeaseExpires = nil, nil
		s.Tasks[0].ReviewingBy, s.Tasks[0].ReviewLeaseExpires = &reviewer, &lease
		agent := s.Agents[livenessAgent]
		agent.Role, agent.Status = "code-reviewer", models.AgentStatusReviewing
		s.Agents[livenessAgent] = agent
	}
}

func assertOptionalTime(t *testing.T, label string, got, want *time.Time) {
	t.Helper()
	if want == nil {
		if got != nil {
			t.Fatalf("%s = %v, want nil", label, got)
		}
		return
	}
	assertTime(t, label, got, *want)
}

func TestLivenessRecordIgnoredUnlessCurrentGenerationAndNewer(t *testing.T) {
	base := time.Now().UTC().Truncate(time.Second)
	original := base.Add(time.Minute)

	t.Run("previous generation record", func(t *testing.T) {
		statePath, bb := writeLivenessState(t, original, nil)
		publishLiveness(t, statePath, LivenessRecord{Generation: "gen-previous", Seq: 10, Heartbeat: base, LeaseExpires: base.Add(time.Hour)})
		assertAgentLease(t, bb, original)
	})
	t.Run("seq at or below the folded seq", func(t *testing.T) {
		statePath, bb := writeLivenessState(t, original, func(s *models.State) {
			agent := s.Agents[livenessAgent]
			agent.LivenessSeq = 20
			s.Agents[livenessAgent] = agent
		})
		publishLiveness(t, statePath, LivenessRecord{Seq: 20, Heartbeat: base, LeaseExpires: base.Add(time.Hour)})
		assertAgentLease(t, bb, original)
	})
	t.Run("record body naming another generation at the current path", func(t *testing.T) {
		statePath, bb := writeLivenessState(t, original, nil)
		publishLiveness(t, statePath, LivenessRecord{Generation: "gen-previous", Seq: 10, Heartbeat: base, LeaseExpires: base.Add(time.Hour)})
		if err := os.Rename(LivenessRecordPath(statePath, livenessAgent, "gen-previous"),
			LivenessRecordPath(statePath, livenessAgent, livenessGeneration)); err != nil {
			t.Fatal(err)
		}
		assertAgentLease(t, bb, original)
	})
	t.Run("undecodable record", func(t *testing.T) {
		statePath, bb := writeLivenessState(t, original, nil)
		if err := os.WriteFile(LivenessRecordPath(statePath, livenessAgent, livenessGeneration), []byte("{torn"), 0644); err != nil {
			t.Fatal(err)
		}
		assertAgentLease(t, bb, original)
	})
}

func assertAgentLease(t *testing.T, bb *Blackboard, want time.Time) {
	t.Helper()
	state, err := bb.ReadSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	assertTime(t, "agent lease", state.Agents[livenessAgent].LeaseExpires, want)
}

func TestModifyFoldsEarlierRecordAndItsCallbackWins(t *testing.T) {
	// GIVEN a record published before the mutation reads records
	base := time.Now().UTC().Truncate(time.Second)
	statePath, bb := writeLivenessState(t, base.Add(time.Minute), nil)
	renewed := base.Add(time.Hour)
	publishLiveness(t, statePath, LivenessRecord{Seq: 10, Heartbeat: base, LeaseExpires: renewed})

	// WHEN a mutation releases the agent
	var observed *time.Time
	if err := bb.Modify(func(s *models.State) error {
		observed = s.Agents[livenessAgent].LeaseExpires
		s.ReleaseAgent(livenessAgent)
		return nil
	}); err != nil {
		t.Fatalf("Modify: %v", err)
	}

	// THEN the callback saw the record, the fold is persisted, and the release
	// is not undone by the record it folded
	assertTime(t, "lease observed by callback", observed, renewed)
	if got := persistedState(t, statePath).Agents[livenessAgent].LivenessSeq; got != 10 {
		t.Fatalf("persisted liveness_seq = %d, want 10", got)
	}
	state, err := bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	if lease := state.Agents[livenessAgent].LeaseExpires; lease != nil {
		t.Fatalf("agent lease after release = %v, want nil", lease)
	}

	// AND a later beat renews the agent lease again, as a later locked beat did
	later := base.Add(2 * time.Hour)
	publishLiveness(t, statePath, LivenessRecord{Seq: 11, Heartbeat: base.Add(time.Second), LeaseExpires: later})
	assertAgentLease(t, bb, later)
}

func TestRecordPublishedDuringModifyAppliesAfterCommit(t *testing.T) {
	// GIVEN a mutation that has read the records and is still running
	base := time.Now().UTC().Truncate(time.Second)
	statePath, bb := writeLivenessState(t, base.Add(time.Minute), nil)
	publishLiveness(t, statePath, LivenessRecord{Seq: 10, Heartbeat: base, LeaseExpires: base.Add(time.Hour)})
	inCallback, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- bb.Modify(func(s *models.State) error {
			close(inCallback)
			<-release
			agent := s.Agents[livenessAgent]
			agent.Status = models.AgentStatusWaiting
			s.Agents[livenessAgent] = agent
			return nil
		})
	}()
	<-inCallback

	// WHEN a beat publishes before that mutation commits
	later := base.Add(2 * time.Hour)
	publishLiveness(t, statePath, LivenessRecord{Seq: 11, Heartbeat: base.Add(time.Second), LeaseExpires: later})
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("Modify: %v", err)
	}

	// THEN the commit folded only the earlier record, and reads apply the
	// newer one on top of the committed state
	if got := persistedState(t, statePath).Agents[livenessAgent].LivenessSeq; got != 10 {
		t.Fatalf("persisted liveness_seq = %d, want 10", got)
	}
	state, err := bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	agent := state.Agents[livenessAgent]
	if agent.Status != models.AgentStatusWaiting || agent.LivenessSeq != 11 {
		t.Fatalf("agent status=%s seq=%d, want WAITING and 11", agent.Status, agent.LivenessSeq)
	}
	assertTime(t, "agent lease", agent.LeaseExpires, later)
	assertTime(t, "task lease", state.FindTask(livenessTask).LeaseExpires, later)
}

func TestWriteLivenessRecordDoesNotWaitForStateLock(t *testing.T) {
	base := time.Now().UTC().Truncate(time.Second)
	statePath, bb := writeLivenessState(t, base.Add(time.Minute), nil)
	acquired, release := make(chan struct{}), make(chan struct{})
	holderDone := make(chan error, 1)
	go func() {
		holderDone <- New(statePath).fileLock.WithLockOperation("holder", func() error {
			close(acquired)
			<-release
			return nil
		})
	}()
	<-acquired
	defer func() {
		close(release)
		if err := <-holderDone; err != nil {
			t.Errorf("holder: %v", err)
		}
	}()

	written := make(chan error, 1)
	go func() {
		written <- WriteLivenessRecord(statePath, LivenessRecord{
			AgentID: livenessAgent, Generation: livenessGeneration, Seq: 10, Heartbeat: base, LeaseExpires: base.Add(time.Hour),
		})
	}()
	select {
	case err := <-written:
		if err != nil {
			t.Fatalf("WriteLivenessRecord: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("WriteLivenessRecord blocked while the state lock was held")
	}
	assertAgentLease(t, bb, base.Add(time.Hour))
}

func TestSweepLivenessRecordsKeepsCurrentRecordsAndTemporaries(t *testing.T) {
	// GIVEN current and replaced records, an in-flight temp file and an unrelated file
	statePath := filepath.Join(t.TempDir(), "state.yaml")
	state := &models.State{Agents: map[string]models.Agent{
		"coder-1": {Generation: "gen-a"},
		"coder-2": {Generation: "gen-b"},
	}}
	current := []string{
		LivenessRecordPath(statePath, "coder-1", "gen-a"),
		LivenessRecordPath(statePath, "coder-2", "gen-b"),
	}
	replaced := LivenessRecordPath(statePath, "coder-1", "gen-old")
	temporary := replaced + ".tmp.123"
	unrelated := statePath + ".claim-0123456789abcdef"
	for _, path := range append(current, replaced, temporary, unrelated) {
		if err := os.WriteFile(path, []byte("{}"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// WHEN registration sweeps
	if err := SweepLivenessRecords(statePath, state); err != nil {
		t.Fatalf("SweepLivenessRecords: %v", err)
	}

	// THEN only the replaced generation's published record is removed
	if _, err := os.Stat(replaced); !os.IsNotExist(err) {
		t.Fatalf("replaced record stat error = %v, want not exist", err)
	}
	for _, path := range append(current, temporary, unrelated) {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("%s removed by sweep: %v", filepath.Base(path), err)
		}
	}
}
