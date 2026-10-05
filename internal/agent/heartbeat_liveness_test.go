package agent

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestBeatCompletesWhileStateLockHeldWithoutRewritingState(t *testing.T) {
	// GIVEN an assigned agent and another process holding the state lock
	statePath, _ := testhelpers.SetupLizaDir(t, t.TempDir())
	state := testhelpers.CreateValidState()
	taskID, agentID := "task-1", "coder-1"
	now := time.Now().UTC()
	lease := now.Add(time.Minute)
	state.Tasks = []models.Task{{ID: taskID, Status: models.TaskStatusImplementing, AssignedTo: &agentID, LeaseExpires: &lease}}
	state.Agents[agentID] = models.Agent{Role: "coder", Status: models.AgentStatusWorking, CurrentTask: &taskID,
		Heartbeat: now.Add(-time.Hour), LeaseExpires: &lease, Generation: "gen-1"}
	bb := testhelpers.WriteInitialState(t, statePath, state)
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	acquired, release := make(chan struct{}), make(chan struct{})
	holderDone := make(chan error, 1)
	go func() {
		holderDone <- filelock.New(statePath).WithLockOperation("holder", func() error {
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

	// WHEN the heartbeat beats
	hb := NewHeartbeat(HeartbeatConfig{Authority: models.AgentAuthority{ID: agentID, Generation: "gen-1"},
		StatePath: statePath, LeaseDuration: time.Hour})
	beaten := make(chan error, 1)
	go func() { beaten <- hb.beat() }()
	select {
	case err := <-beaten:
		if err != nil {
			t.Fatalf("beat: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("beat waited for the state lock")
	}

	// THEN state.yaml is unchanged and reads observe the renewed leases
	after, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("beat rewrote state.yaml")
	}
	observed, err := bb.ReadSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if got := observed.Agents[agentID].LeaseExpires; got == nil || !got.After(now.Add(50*time.Minute)) {
		t.Fatalf("agent lease = %v, want renewed by about an hour", got)
	}
	if got := observed.FindTask(taskID).LeaseExpires; got == nil || !got.After(now.Add(50*time.Minute)) {
		t.Fatalf("task lease = %v, want renewed by about an hour", got)
	}
}

func TestStaleGenerationBeatCannotDisplaceCurrentGeneration(t *testing.T) {
	// GIVEN generation G1 of coder-1 with a heartbeat paused after its
	// authority check
	t.Cleanup(ops.SetAgentProcessProcRootForTest(filepath.Join(t.TempDir(), "missing-proc")))
	projectRoot := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
	testhelpers.SetupPipelineConfig(t, projectRoot)
	state := testhelpers.CreateValidState()
	taskID, agentID := "task-1", "coder-1"
	state.Tasks = []models.Task{{ID: taskID, Status: models.TaskStatusImplementing}}
	bb := testhelpers.WriteInitialState(t, statePath, state)
	resolver := testResolver(t)
	authorityG1, err := registerAgentWithAuthority(bb, projectRoot, agentID, "coder", "terminal-a", 1800, "codex", "", resolver)
	if err != nil {
		t.Fatalf("register G1: %v", err)
	}
	hbG1 := NewHeartbeat(HeartbeatConfig{Authority: authorityG1, StatePath: statePath, LeaseDuration: time.Hour})

	// WHEN, during that pause, G2 takes over the ID, claims the task and
	// beats, and only then G1 publishes
	var hbG2 *Heartbeat
	var authorityG2 models.AgentAuthority
	interleaved := false
	beatBeforePublishHook = func() {
		if interleaved {
			return
		}
		interleaved = true
		if err := bb.Modify(func(s *models.State) error {
			agent := s.Agents[agentID]
			expired := time.Now().UTC().Add(-time.Minute)
			agent.LeaseExpires = &expired
			s.Agents[agentID] = agent
			return nil
		}); err != nil {
			t.Fatalf("expire G1: %v", err)
		}
		authorityG2, err = registerAgentWithAuthority(bb, projectRoot, agentID, "coder", "terminal-b", 1800, "claude", "", resolver)
		if err != nil {
			t.Fatalf("register G2: %v", err)
		}
		if err := bb.Modify(func(s *models.State) error {
			lease := time.Now().UTC().Add(time.Minute)
			id, task := agentID, taskID
			s.FindTask(taskID).AssignedTo, s.FindTask(taskID).LeaseExpires = &id, &lease
			agent := s.Agents[agentID]
			agent.Status, agent.CurrentTask = models.AgentStatusWorking, &task
			s.Agents[agentID] = agent
			return nil
		}); err != nil {
			t.Fatalf("claim task for G2: %v", err)
		}
		hbG2 = NewHeartbeat(HeartbeatConfig{Authority: authorityG2, StatePath: statePath, LeaseDuration: 2 * time.Hour})
		if err := hbG2.beat(); err != nil {
			t.Fatalf("G2 beat: %v", err)
		}
	}
	t.Cleanup(func() { beatBeforePublishHook = nil })
	if err := hbG1.beat(); err != nil {
		t.Fatalf("G1 beat: %v", err)
	}

	// THEN G1's late record exists but G2's liveness is what every read sees
	if _, err := os.Stat(db.LivenessRecordPath(statePath, agentID, authorityG1.Generation)); err != nil {
		t.Fatalf("G1 record was not published late: %v", err)
	}
	observed, err := bb.ReadSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	agent := observed.Agents[agentID]
	wantLease := hbG2.last.LeaseExpires
	if agent.Generation != authorityG2.Generation || !agent.Heartbeat.Equal(hbG2.last.Heartbeat) {
		t.Fatalf("agent generation=%q heartbeat=%v, want G2 %q at %v",
			agent.Generation, agent.Heartbeat, authorityG2.Generation, hbG2.last.Heartbeat)
	}
	if agent.LeaseExpires == nil || !agent.LeaseExpires.Equal(wantLease) {
		t.Fatalf("agent lease = %v, want G2 lease %v", agent.LeaseExpires, wantLease)
	}
	if got := observed.FindTask(taskID).LeaseExpires; got == nil || !got.Equal(wantLease) {
		t.Fatalf("task lease = %v, want G2 lease %v", got, wantLease)
	}

	// AND G1's next beat is fenced
	if err := hbG1.beat(); !ops.IsAgentAuthorityError(err) {
		t.Fatalf("G1 second beat error = %v, want generation authority error", err)
	}
}
