package ops

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
)

// Each outcome keeps the literal --json value it replaced; the expectations are
// written out independently of the constants.
func TestAwaitResubmissionVocabularyKeepsItsValues(t *testing.T) {
	for want, got := range map[string]AwaitResubmissionOutcome{
		"RESUBMITTED": ResubmissionResubmitted,
		"TERMINAL":    ResubmissionTerminal,
		"TIMEOUT":     ResubmissionTimeout,
		"ABORTED":     ResubmissionAborted,
		"POLL":        ResubmissionPoll,
		"PAUSED":      ResubmissionPaused,
	} {
		if string(got) != want {
			t.Errorf("outcome %q has value %q", want, got)
		}
	}
}

// The result field stays a plain string, so a typed outcome reaches the wire by
// conversion and no custom marshalling is involved.
func TestAwaitResubmissionResultKeepsStringFields(t *testing.T) {
	field, ok := reflect.TypeOf(AwaitResubmissionResult{}).FieldByName("Verdict")
	if !ok || field.Type != reflect.TypeOf("") {
		t.Fatalf("AwaitResubmissionResult.Verdict is %v, want string", field.Type)
	}
	encoded, err := json.Marshal(AwaitResubmissionResult{Verdict: string(ResubmissionPaused), SafeAction: string(SafeActionStop)})
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["verdict"] != "PAUSED" || decoded["safe_action"] != "stop" {
		t.Errorf("encoded result = %s, want verdict PAUSED and safe_action stop", encoded)
	}
}

// The reclaim lease is the C1 site of this concern: its name and value are
// fixed until C1, and the ownership margin stays beside the reservation it sizes.
func TestReviewLeaseDurationsKeepTheirValues(t *testing.T) {
	if reclaimReviewLeaseDuration != 30*time.Minute {
		t.Errorf("reclaimReviewLeaseDuration = %v, want 30m", reclaimReviewLeaseDuration)
	}
	if reviewOwnershipLeaseMargin != 5*time.Minute {
		t.Errorf("reviewOwnershipLeaseMargin = %v, want 5m", reviewOwnershipLeaseMargin)
	}
}

// A retained reservation is the agent's own live WAITING hold on the task and
// nothing else: another reviewer's claim, an expired lease, a different agent
// status or a later release each leave the agent with none.
func TestRetainedReservationRequiresTheAgentsLiveHold(t *testing.T) {
	const agentID, taskID = "reviewer-1", "task-1"
	future := time.Now().Add(time.Hour)
	claimedByOther := "reviewer-2"
	build := func(mutate func(*models.State, *models.Task)) (*models.State, *models.Task) {
		current := taskID
		task := &models.Task{
			ID:                 taskID,
			ReviewingBy:        &[]string{agentID}[0],
			ReviewLeaseExpires: &future,
			History:            []models.TaskHistoryEntry{{Event: models.TaskEventRejected, Agent: &[]string{agentID}[0]}},
		}
		state := &models.State{Agents: map[string]models.Agent{
			agentID: {Status: models.AgentStatusWaiting, CurrentTask: &current},
		}}
		if mutate != nil {
			mutate(state, task)
		}
		return state, task
	}

	state, task := build(nil)
	held := retainedReservation(state, task, agentID)
	if held == nil || held.historyLen != len(task.History) {
		t.Fatalf("live hold: retained = %+v, want a reservation at history length %d", held, len(task.History))
	}

	for name, mutate := range map[string]func(*models.State, *models.Task){
		"another reviewer holds the task": func(_ *models.State, task *models.Task) { task.ReviewingBy = &claimedByOther },
		"the lease expired": func(_ *models.State, task *models.Task) {
			past := time.Now().Add(-time.Minute)
			task.ReviewLeaseExpires = &past
		},
		"the agent is not waiting": func(state *models.State, _ *models.Task) {
			agent := state.Agents[agentID]
			agent.Status = models.AgentStatusReviewing
			state.Agents[agentID] = agent
		},
		"the agent waits on another task": func(state *models.State, _ *models.Task) {
			elsewhere := "task-2"
			agent := state.Agents[agentID]
			agent.CurrentTask = &elsewhere
			state.Agents[agentID] = agent
		},
		"the agent is unknown": func(state *models.State, _ *models.Task) { delete(state.Agents, agentID) },
	} {
		state, task := build(mutate)
		if got := retainedReservation(state, task, agentID); got != nil {
			t.Errorf("%s: retained = %+v, want none", name, got)
		}
	}
}
